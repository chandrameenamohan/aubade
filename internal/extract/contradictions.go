package extract

import (
	"fmt"
	"time"

	"github.com/chandrameenamohan/aubade/internal/model"
)

// Contradictions are the honesty extractor. The profile is explicit about what
// it wants here — "If two sources disagree, tell me — don't pick one and hide
// it" — so nothing in this file resolves anything. Both sides go into the
// signal with their own citation, and the renderer shows both.
//
// Two disagreements are detectable from the corpus:
//
//  1. **The mail and the calendar name different times for the same meeting.**
//     Someone writes "moved to Thursday at 2" and the invite still says
//     Wednesday 15:00. Matching an email to an event is the risky half — a
//     false match invents a contradiction, which is worse than missing one — so
//     it takes two distinctive words in common *and* a time statement in a
//     sentence that is itself about the meeting.
//  2. **The mail says a meeting is on and the calendar says it is not.** The
//     invite is CANCELLED, or the owner declined it, and someone is still
//     writing "see you at 3". That one is a genuine trap: both sources are
//     internally consistent and only disagree with each other.
//
// The horizon is a week each side of the anchor day. Beyond that, an email
// about "Thursday" is almost certainly about a different Thursday.

// meetingHorizon bounds which events are checked against the mail.
const meetingHorizon = 7 * 24 * time.Hour

// meetingTokenOverlap is how many distinctive words an email and an event have
// to share before they are talking about the same meeting.
const meetingTokenOverlap = 2

// schedulingVerbs mark a sentence that is stating when something happens.
var schedulingVerbs = []string{
	"moved", "moving", "move", "pushed", "push", "rescheduled", "reschedule",
	"starts", "start", "begins", "is at", "shifted", "bumped",
	"see you", "confirmed for", "kicks off", "runs from",
}

// stillOnPhrases are someone treating a meeting as live.
var stillOnPhrases = []string{
	"see you at", "see you there", "still on", "confirming", "confirmed for",
	"looking forward to", "talk then", "on for", "we're on",
}

// Contradictions reports sources that disagree, keeping both sides.
func (t *Toolbox) Contradictions() (model.Signals, error) {
	g := newIDs()
	var out model.Signals

	for i := range t.corpus.Events {
		ev := &t.corpus.Events[i]
		if ev.AllDay {
			continue
		}
		if d := ev.Start.Sub(t.now); d > meetingHorizon || d < -meetingHorizon {
			continue
		}
		want := distinctiveTokens(ev.Summary)
		if len(want) < meetingTokenOverlap {
			continue
		}
		dead := ev.Status == model.StatusCancelled || ev.DeclinedBy(t.ownerAddr)

		for j := range t.corpus.Emails {
			e := &t.corpus.Emails[j]
			if e.TS.After(t.now) {
				continue
			}
			if _, suppressed := t.supp.email(e.ID); suppressed {
				continue
			}
			if overlapCount(distinctiveTokens(e.Subject+" "+e.Body), want) < meetingTokenOverlap {
				continue
			}
			if dead {
				if s := t.deadMeetingContradiction(g, ev, e); s != nil {
					out = append(out, *s)
				}
				continue
			}
			if s := t.timeContradiction(g, ev, e, want); s != nil {
				out = append(out, *s)
			}
		}
	}
	return out, nil
}

// timeContradiction compares what an email says about a meeting's time against
// what the invite says.
func (t *Toolbox) timeContradiction(g *ids, ev *model.CalEvent, e *model.Email, want []string) *model.Signal {
	for _, sentence := range sentences(e.Body) {
		// The sentence has to be about the meeting, not merely in an email
		// that mentions it once.
		if overlapCount(distinctiveTokens(sentence), want) == 0 && !containsAny(sentence, schedulingVerbs) {
			continue
		}
		claim, ok := ParseTimeAssertion(sentence, e.TS, t.loc)
		if !ok {
			continue
		}
		start := ev.Start.In(t.loc)
		disagreement := ""
		switch {
		case claim.HasDate && !sameDay(claim.At, start, t.loc):
			if absDuration(claim.At.Sub(start)) > meetingHorizon {
				continue // a different instance of a recurring thing
			}
			disagreement = fmt.Sprintf("the mail says %s, the invite says %s",
				claim.At.Format("Mon 2 Jan"), start.Format("Mon 2 Jan"))
		case claim.HasClock && sameDay(claim.At, start, t.loc) && claim.At.Format("15:04") != start.Format("15:04"):
			disagreement = fmt.Sprintf("the mail says %s, the invite says %s",
				claim.At.Format("15:04"), start.Format("15:04"))
		default:
			continue
		}

		return &model.Signal{
			ID:       g.next(model.KindContradictions, ev.UID, e.ID),
			Kind:     model.KindContradictions,
			Priority: t.contradictionPriority(ev, e),
			Title:    fmt.Sprintf("email and calendar disagree on %s", truncate(ev.Summary, 60)),
			Detail: fmt.Sprintf("%s — %s wrote %s. Both sides are shown; neither was picked.",
				disagreement, e.From.String(), quote(sentence)),
			Citations:   []model.Citation{eventCite(ev.UID), emailCite(e.ID)},
			SectionHint: model.SectionHonesty,
			Confidence:  model.Certain,
			Deadline:    timePtr(minTime(start, claim.At)),
		}
	}
	return nil
}

// deadMeetingContradiction catches mail that treats a cancelled or declined
// meeting as live.
func (t *Toolbox) deadMeetingContradiction(g *ids, ev *model.CalEvent, e *model.Email) *model.Signal {
	if !e.TS.Before(ev.Start) {
		return nil
	}
	sentence := ""
	for _, s := range sentences(e.Body) {
		if containsAny(s, stillOnPhrases) {
			sentence = s
			break
		}
	}
	if sentence == "" {
		return nil
	}

	state := "cancelled"
	if ev.Status != model.StatusCancelled {
		state = "declined by you"
	}
	return &model.Signal{
		ID:       g.next(model.KindContradictions, "status", ev.UID, e.ID),
		Kind:     model.KindContradictions,
		Priority: t.contradictionPriority(ev, e),
		Title:    fmt.Sprintf("%s is %s on the calendar but live in the mail", truncate(ev.Summary, 50), state),
		Detail: fmt.Sprintf("The invite is %s; %s wrote %s at %s. Both sides are shown; neither was picked.",
			state, e.From.String(), quote(sentence), e.TS.In(t.loc).Format("Mon 2 Jan 15:04")),
		Citations:   []model.Citation{eventCite(ev.UID), emailCite(e.ID)},
		SectionHint: model.SectionHonesty,
		Confidence:  model.Certain,
		Deadline:    timePtr(ev.Start),
	}
}

// contradictionPriority takes the more urgent of the two parties, and floors a
// disagreement about today's calendar at P1: whichever side is right, the owner
// is about to be in the wrong place.
func (t *Toolbox) contradictionPriority(ev *model.CalEvent, e *model.Email) model.Priority {
	base := atMost(t.prio.Of(e.From, e.Labels).Priority, t.prio.Of(ev.Organizer, nil).Priority)
	if sameDay(ev.Start, t.day, t.loc) {
		return atMost(base, model.P1)
	}
	return base
}

func absDuration(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}
