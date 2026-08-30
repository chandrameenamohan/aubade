package extract

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/chandrameenamohan/aubade/internal/model"
)

// Calendar conflicts are the one extractor where the data is unambiguous — two
// intervals either overlap or they do not — so the work is entirely in saying
// which collision *matters*. Three do:
//
//   - **A double-booking.** Two live meetings on the same clock.
//   - **A meeting eating a deep-work block.** The block is on the calendar
//     because the owner put it there; something scheduled over it is a decision
//     someone else made about their morning.
//   - **A work meeting over a family commitment.** The pediatrician appointment
//     Sam added at 21:04 last night is not a calendar entry to be triaged
//     against a pipeline sync; it is the reason the pipeline sync moves.
//
// Plus one that is not an overlap at all: an **impossible transition**, two
// back-to-back events in different physical rooms with no travel time between
// them. It is on the tool's own help page, and it is the collision people
// actually walk into.
//
// Cancelled events and events the owner declined are not conflicts: a meeting
// that is not happening cannot collide with one that is. Events suppressed as
// "invites I already accepted" *are* still considered — see the note in
// suppressions.go on why a display preference must not delete a fact.

// familyWords name a personal commitment on a shared calendar. The list is
// fixed and small: this is a lexicon, not an inference, and everything it
// misses simply falls through to the plain double-booking case.
var familyWords = []string{
	"pediatrician", "paediatrician", "doctor", "dentist", "orthodontist",
	"school", "daycare", "nursery", "parent-teacher", "pta", "family",
	"anniversary", "birthday", "recital", "soccer", "swim", "vacation",
	"appointment", "therapy", "vet",
}

// deepWorkWords name a block the owner is protecting.
var deepWorkWords = []string{
	"deep work", "deep-work", "focus block", "focus time", "heads down",
	"heads-down", "no meetings", "no-meetings", "writing block", "maker time",
	"blocked", "do not schedule", "hold",
}

// virtualLocationWords mark a "location" you do not have to walk to.
var virtualLocationWords = []string{"zoom", "meet", "hangout", "teams", "webex", "phone", "call", "http", "online", "virtual", "remote"}

// transitionGap is the travel time two physically separate meetings need. Less
// than this between different rooms is an appointment nobody can keep.
const transitionGapMinutes = 15

// Conflicts reports collisions on the anchor day.
func (t *Toolbox) Conflicts() (model.Signals, error) {
	g := newIDs()
	live := t.liveEventsOn(t.day)
	var out model.Signals

	for i := 0; i < len(live); i++ {
		for j := i + 1; j < len(live); j++ {
			a, b := live[i], live[j]
			if overlaps(a, b) {
				out = append(out, t.overlapSignal(g, a, b))
				continue
			}
			if t.impossibleTransition(a, b) {
				out = append(out, t.transitionSignal(g, a, b))
			}
		}
	}
	return out, nil
}

// liveEventsOn returns the events happening on the given day that the owner is
// actually expected at, ordered by start then UID.
func (t *Toolbox) liveEventsOn(day time.Time) []*model.CalEvent {
	var live []*model.CalEvent
	for i := range t.corpus.Events {
		ev := &t.corpus.Events[i]
		if ev.Status == model.StatusCancelled || ev.DeclinedBy(t.ownerAddr) {
			continue
		}
		if ev.AllDay || !sameDay(ev.Start, day, t.loc) {
			continue
		}
		live = append(live, ev)
	}
	slices.SortFunc(live, func(a, b *model.CalEvent) int {
		if !a.Start.Equal(b.Start) {
			return a.Start.Compare(b.Start)
		}
		return strings.Compare(a.UID, b.UID)
	})
	return live
}

// overlaps reports whether two events share clock time. Half-open intervals, so
// a 09:00–09:30 and a 09:30–10:00 do not "overlap".
func overlaps(a, b *model.CalEvent) bool {
	return a.Start.Before(b.End) && b.Start.Before(a.End)
}

// overlapSignal classifies one collision and renders it.
func (t *Toolbox) overlapSignal(g *ids, a, b *model.CalEvent) model.Signal {
	kind, priority := t.classifyCollision(a, b)

	return model.Signal{
		ID:       g.next(model.KindConflicts, a.UID, b.UID),
		Kind:     model.KindConflicts,
		Priority: priority,
		Title: fmt.Sprintf("%s: %s vs %s at %s", kind,
			truncate(a.Summary, 40), truncate(b.Summary, 40), a.Start.In(t.loc).Format("15:04")),
		Detail: fmt.Sprintf("%s (%s–%s%s) overlaps %s (%s–%s%s).",
			quote(a.Summary), t.clock(a.Start), t.clock(a.End), organizerSuffix(a),
			quote(b.Summary), t.clock(b.Start), t.clock(b.End), organizerSuffix(b)),
		Citations:   []model.Citation{eventCite(a.UID), eventCite(b.UID)},
		SectionHint: model.SectionCalendar,
		Confidence:  model.Certain,
		Deadline:    timePtr(minTime(a.Start, b.Start)),
	}
}

// classifyCollision names the collision and sets how loudly to say it.
func (t *Toolbox) classifyCollision(a, b *model.CalEvent) (string, model.Priority) {
	aFamily, bFamily := t.isPersonal(a), t.isPersonal(b)
	switch {
	case aFamily != bFamily:
		// Personal against work. The profile makes Sam P0; a family
		// commitment losing to a meeting is the collision that costs most.
		return "family collision", model.P0
	case aFamily && bFamily:
		return "double-booked (personal)", model.P1
	case isDeepWork(a) != isDeepWork(b):
		return "meeting over a protected block", model.P1
	default:
		return "double-booked", t.eventPriority(a, b)
	}
}

// transitionSignal reports two back-to-back meetings in different rooms.
func (t *Toolbox) transitionSignal(g *ids, a, b *model.CalEvent) model.Signal {
	gap := int(b.Start.Sub(a.End).Minutes())
	return model.Signal{
		ID:       g.next(model.KindConflicts, "transition", a.UID, b.UID),
		Kind:     model.KindConflicts,
		Priority: t.eventPriority(a, b),
		Title: fmt.Sprintf("impossible transition: %s → %s in %d minutes",
			truncate(a.Summary, 35), truncate(b.Summary, 35), gap),
		Detail: fmt.Sprintf("%s ends at %s in %s; %s starts at %s in %s. %d minutes is not travel time.",
			quote(a.Summary), t.clock(a.End), quote(a.Location),
			quote(b.Summary), t.clock(b.Start), quote(b.Location), gap),
		Citations:   []model.Citation{eventCite(a.UID), eventCite(b.UID)},
		SectionHint: model.SectionCalendar,
		Confidence:  model.Certain,
		Deadline:    timePtr(a.End),
	}
}

// impossibleTransition reports two consecutive events in different physical
// places with no time to move between them.
func (t *Toolbox) impossibleTransition(a, b *model.CalEvent) bool {
	if b.Start.Before(a.End) {
		return false
	}
	if int(b.Start.Sub(a.End).Minutes()) >= transitionGapMinutes {
		return false
	}
	la, lb := strings.TrimSpace(a.Location), strings.TrimSpace(b.Location)
	if la == "" || lb == "" || strings.EqualFold(la, lb) {
		return false
	}
	return !isVirtual(la) && !isVirtual(lb)
}

// isVirtual reports whether a location is a link rather than a room.
func isVirtual(loc string) bool { return containsAny(loc, virtualLocationWords) }

// isDeepWork reports whether an event is a protected block.
func isDeepWork(ev *model.CalEvent) bool {
	return containsAny(ev.Summary+" "+ev.Description, deepWorkWords)
}

// isPersonal reports whether an event is a family or personal commitment.
//
// Two routes: the summary uses one of the family words, or the organiser is a
// profile person the user marked personal ("anything from Sam is P0.
// Personal."). The second is why this works without a keyword for every kind of
// appointment a family can have.
func (t *Toolbox) isPersonal(ev *model.CalEvent) bool {
	if containsAny(ev.Summary+" "+ev.Description, familyWords) {
		return true
	}
	m := t.prio.Of(ev.Organizer, nil)
	return m.Person != nil && containsWord(m.Person.Note, "personal")
}

// eventPriority is the most urgent priority among an event's organisers.
func (t *Toolbox) eventPriority(evs ...*model.CalEvent) model.Priority {
	best := defaultPriority
	for _, ev := range evs {
		best = atMost(best, t.prio.Of(ev.Organizer, nil).Priority)
	}
	return atMost(best, model.P1)
}

// organizerSuffix renders ", organised by X" when there is an organiser worth
// naming.
func organizerSuffix(ev *model.CalEvent) string {
	if s := ev.Organizer.String(); s != "" {
		return ", organised by " + s
	}
	return ""
}

// clock formats an instant as the local wall time the calendar shows.
func (t *Toolbox) clock(at time.Time) string { return at.In(t.loc).Format("15:04") }

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
