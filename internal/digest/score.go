package digest

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/chandrameenamohan/aubade/internal/model"
)

// Ranking is where the digest stops being a list and becomes a page.
//
// The toolbox already sorts its output — priority, then deadline, then
// extractor, then id — which is the right order for an audit file. It is the
// wrong order for a morning, because it cannot see that a P1 promise that went
// overdue on Friday outranks a P0 thread that has been quiet since June. So the
// page re-ranks on three axes the profile actually cares about:
//
//   - **Priority.** Who this is, per profile.md's "People who matter". It is
//     the heaviest axis by design: the user wrote that list precisely so a
//     machine would stop deciding who matters.
//   - **Deadline proximity.** Overdue beats today beats this week beats
//     someday. Overdue-ness keeps accruing, but only to a cap: something six
//     weeks late is not six times more urgent than something a week late, it is
//     a different conversation.
//   - **Recency.** How fresh the evidence is. It is the lightest axis and it
//     only ever breaks ties between things the first two agree on — a stale
//     source cannot promote itself past a live deadline.
//
// The weights are round numbers on purpose. They are a policy, not a model:
// anyone can read this file and predict the order, which is the property that
// lets a reviewer argue with the ranking instead of shrugging at it. Every
// score also carries Why, so the reason a line is at the top is inspectable
// rather than asserted.

// Priority weights. P0 scores 1000 and each step down loses 200.
//
// The spacing is chosen against the other two axes rather than picked out of
// the air: two steps (400) exceed the largest deadline bonus (360), so a P0
// always outranks a P2 however late the P2 is — one step does not, so a P1 that
// went overdue on Friday can outrank a P0 due next month. That is the intended
// reading of the profile: the list says who matters, the deadline says what is
// on fire.
const (
	priorityStep = 200
	priorityBase = 200
)

// Deadline weights.
const (
	deadlineOverdue    = 300 // already past
	deadlineOverdueDay = 10  // per business day past, capped
	deadlineOverdueCap = 6   // days of overdue-ness that still add urgency
	deadlineToday      = 250
	deadlineTomorrow   = 160
	deadlineThisWeek   = 120 // minus deadlineWeekDecay per day out
	deadlineWeekDecay  = 15
	deadlineHorizon    = 7 // days past which a deadline stops lifting a line
)

// Recency weights, keyed off the freshest citation on the signal.
const (
	recencyToday    = 100
	recencyThisWeek = 70
	recencyLastWeek = 40
	recencyOlder    = 20
	recencyStaleAge = 14 // days past which evidence adds nothing
)

// Score is why an item ranked where it did.
type Score struct {
	Total    int    `json:"total"`
	Priority int    `json:"priority"`
	Deadline int    `json:"deadline"`
	Recency  int    `json:"recency"`
	Why      string `json:"why"`
}

// scoreOf ranks one signal against the anchor morning.
func scoreOf(s model.Signal, idx *index, now, day time.Time, loc *time.Location) Score {
	var (
		sc    Score
		parts []string
	)

	if rank := s.Priority.Rank(); rank >= 0 {
		sc.Priority = priorityBase + (len(model.Priorities)-1-rank)*priorityStep
		parts = append(parts, fmt.Sprintf("%s +%d", s.Priority, sc.Priority))
	}

	if s.Deadline != nil {
		var why string
		sc.Deadline, why = deadlineWeight(*s.Deadline, now, day, loc)
		if sc.Deadline != 0 {
			parts = append(parts, fmt.Sprintf("%s +%d", why, sc.Deadline))
		}
	}

	if at, ok := freshestCitation(s, idx); ok {
		var why string
		sc.Recency, why = recencyWeight(at, now)
		if sc.Recency != 0 {
			parts = append(parts, fmt.Sprintf("%s +%d", why, sc.Recency))
		}
	}

	sc.Total = sc.Priority + sc.Deadline + sc.Recency
	sc.Why = strings.Join(parts, ", ")
	return sc
}

// deadlineWeight scores how close a deadline is to the anchor morning.
func deadlineWeight(deadline, now, day time.Time, loc *time.Location) (int, string) {
	d := deadline.In(loc)
	switch {
	case d.Before(now):
		over := calendarDaysBetween(d, now, loc)
		if over > deadlineOverdueCap {
			over = deadlineOverdueCap
		}
		return deadlineOverdue + over*deadlineOverdueDay, "overdue"
	case sameDay(d, day, loc):
		return deadlineToday, "due today"
	default:
		out := calendarDaysBetween(day, d, loc)
		switch {
		case out <= 1:
			return deadlineTomorrow, "due tomorrow"
		case out <= deadlineHorizon:
			return deadlineThisWeek - out*deadlineWeekDecay, fmt.Sprintf("due in %d days", out)
		}
		return 0, ""
	}
}

// recencyWeight scores how fresh the evidence behind a signal is. Evidence from
// the future — a meeting later today — counts as fresh: it has not aged.
func recencyWeight(at, now time.Time) (int, string) {
	if at.After(now) {
		return recencyToday, "ahead"
	}
	days := int(now.Sub(at).Hours() / 24)
	switch {
	case days < 1:
		return recencyToday, "today"
	case days <= 3:
		return recencyThisWeek, fmt.Sprintf("%dd old", days)
	case days <= 7:
		return recencyLastWeek, fmt.Sprintf("%dd old", days)
	case days <= recencyStaleAge:
		return recencyOlder, fmt.Sprintf("%dd old", days)
	}
	return 0, ""
}

// freshestCitation is the most recent moment any of a signal's citations points
// at. Freshest rather than oldest: a thread's newest message is what makes the
// thread current, and a signal that cites both ends of one should be ranked by
// the end that is still moving.
func freshestCitation(s model.Signal, idx *index) (time.Time, bool) {
	var (
		best  time.Time
		found bool
	)
	for _, c := range s.Citations {
		at, ok := idx.at(c)
		if !ok {
			continue
		}
		if !found || at.After(best) {
			best, found = at, true
		}
	}
	return best, found
}

// ranked is a signal with its score, which is the unit everything downstream
// sorts and renders.
type ranked struct {
	Signal model.Signal
	Score  Score
}

// rank scores every signal and orders them for the page.
//
// The comparison ends on the signal id, which is unique, so the order is total:
// there is exactly one correct page for any input, and no run-to-run wobble for
// the golden file to catch and nobody to explain.
func rank(ss model.Signals, idx *index, now, day time.Time, loc *time.Location) []ranked {
	out := make([]ranked, 0, len(ss))
	for _, s := range ss {
		out = append(out, ranked{Signal: s, Score: scoreOf(s, idx, now, day, loc)})
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Score.Total != b.Score.Total {
			return a.Score.Total > b.Score.Total
		}
		if ad, bd := deadlineKey(a.Signal), deadlineKey(b.Signal); !ad.Equal(bd) {
			return ad.Before(bd)
		}
		return a.Signal.ID < b.Signal.ID
	})
	return out
}

// deadlineKey sorts signals without a deadline after those with one.
func deadlineKey(s model.Signal) time.Time {
	if s.Deadline == nil {
		return time.Date(9999, time.January, 1, 0, 0, 0, 0, time.UTC)
	}
	return *s.Deadline
}

// sameDay reports whether two instants fall on one calendar day in loc.
func sameDay(a, b time.Time, loc *time.Location) bool {
	return a.In(loc).Format("2006-01-02") == b.In(loc).Format("2006-01-02")
}

// calendarDaysBetween counts whole calendar days from a to b, never negative.
//
// Calendar days rather than business days: a deadline does not care that it
// slipped over a weekend, and the profile's business-day arithmetic belongs to
// the extractors that quote the rule it came from. The result is rounded
// because a span crossing a daylight-saving boundary is 23 or 25 hours long,
// and truncating that would make one day a year disappear.
func calendarDaysBetween(a, b time.Time, loc *time.Location) int {
	from := startOfDay(a.In(loc))
	to := startOfDay(b.In(loc))
	if to.Before(from) {
		return 0
	}
	return int(math.Round(to.Sub(from).Hours() / 24))
}

func startOfDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}
