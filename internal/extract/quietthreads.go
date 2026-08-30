package extract

import (
	"fmt"
	"sort"

	"github.com/chandrameenamohan/aubade/internal/model"
)

// Quiet-thread detection is the second half of "what am I about to drop", and
// the profile writes the rule itself:
//
//	"Quiet investor threads. If Marcus or another VC went quiet, I want to know
//	 after three business days, not three weeks."   — What I might miss
//
// So there are two detectors, not one, because the two populations behave
// differently:
//
//   - **P0/P1 threads — an absolute threshold.** More than three business days
//     of silence on a thread that still owes an answer either way. Business
//     days, so a Friday email is not "three days old" on Monday.
//   - **Customer threads — a cadence slowdown.** A reference customer who
//     answered within a day all month and has now been silent for four is a
//     different fact from a thread that was always slow. The baseline is the
//     thread's own median gap, so the comparison is against how *this*
//     conversation normally moves, not against an average of everyone's.
//
// The cadence detector is marked unsure and routed to "I'm not sure" (SPEC §7):
// a slowdown is a genuine judgment call, and the honest rendering shows the
// thread rather than asserting urgency the data does not establish.
//
// Everything the profile suppressed stays suppressed — with one resolution
// stated in the open: the "long threads where I've already had the last word"
// rule stops at P0/P1, because the same document asks to be told about quiet
// investor threads. Both bullets are cited on the resulting signal.

// quietBusinessDays is the profile's threshold ("after three business days").
const quietBusinessDays = 3

// cadenceMinMessages is how much history a thread needs before its own rhythm
// means anything: three messages give two gaps, and two gaps give a median.
const cadenceMinMessages = 3

// cadenceMultiple is how far past its own baseline a thread has to fall before
// the silence is a fact rather than noise.
const cadenceMultiple = 2

// cadenceFloorDays keeps a fast thread from tripping the detector the moment it
// misses a beat: a thread that normally answers same-day has a baseline of 0.
const cadenceFloorDays = 2

// QuietThreads reports conversations that went silent while still owing an
// answer.
func (t *Toolbox) QuietThreads() (model.Signals, error) {
	g := newIDs()
	var out model.Signals

	for _, th := range t.threads {
		if len(th.Messages) == 0 {
			continue
		}
		last := th.Last()
		if last.TS.After(t.now) {
			continue
		}
		if _, suppressed := t.supp.thread(th.ID); suppressed {
			continue
		}
		if _, suppressed := t.supp.email(last.ID); suppressed && !th.OwnerHadLastWord {
			continue // the message that would carry it is suppressed noise
		}

		priority := t.threadPriority(th)
		highTouch := priority.Rank() <= model.P1.Rank()

		// "Open" normally means somebody asked something and nobody answered.
		// For P0/P1 it also covers a thread the *owner* opened and spoke last
		// in: the profile asks to hear about an investor who went quiet, and an
		// investor goes quiet by not replying to a message that made no demand
		// of them.
		//
		// Owner-initiated is the load-bearing half of that clause. Without it,
		// every question a co-founder asked and the owner answered would read
		// as "quiet" a week later, when what it actually is, is finished.
		ownerReachedOut := highTouch && th.OwnerHadLastWord && normAddr(th.First().From.Email) == t.ownerAddr
		if !th.AwaitingOwner && !th.AwaitingReply && !ownerReachedOut {
			continue
		}

		quiet := businessDaysBetween(last.TS, t.now, t.loc)
		switch {
		case highTouch && quiet > quietBusinessDays:
			out = append(out, t.quietSignal(g, th, priority, quiet))
		case priority.Rank() <= model.P2.Rank():
			if base, slow := t.cadenceSlowdown(th, quiet); slow {
				out = append(out, t.cadenceSignal(g, th, priority, quiet, base))
			}
		}
	}
	return out, nil
}

// quietSignal is the absolute-threshold case: someone who matters has gone
// quiet for longer than the profile is willing to tolerate.
func (t *Toolbox) quietSignal(g *ids, th *Thread, priority model.Priority, quiet int) model.Signal {
	last := th.Last()
	who, side := t.waitingSide(th)

	detail := fmt.Sprintf("%s for %s; %s. Threshold is %d business days for P0/P1 (%s).",
		side, businessDayPhrase(quiet), quote(truncate(last.Body, 140)),
		quietBusinessDays, t.missRuleRef())
	// The profile contradicts itself here, and the resolution is shown rather
	// than hidden: the last-word suppression stops at P0/P1.
	if bullet, ok := t.supp.byRule[ruleLastWord]; ok && th.OwnerHadLastWord && !th.AwaitingReply &&
		len(th.Messages) >= lastWordMinMessages {
		detail += fmt.Sprintf(" Your profile also says %s (%s); that rule stops at P0/P1, so this one is shown.",
			quote(bullet.Text), t.ruleRef(bullet))
	}

	section := model.SectionUrgentToday
	if priority == model.P0 {
		section = model.SectionOneThingNow
	}

	return model.Signal{
		ID:          g.next(model.KindQuietThreads, th.ID),
		Kind:        model.KindQuietThreads,
		Priority:    priority,
		Title:       fmt.Sprintf("%s quiet %s — %s", who, businessDayPhrase(quiet), truncate(th.Subject, 60)),
		Detail:      detail,
		Citations:   dedupeCitations([]model.Citation{emailCite(last.ID), emailCite(th.First().ID)}),
		SectionHint: section,
		Confidence:  model.Certain,
	}
}

// cadenceSignal is the relative case: this conversation is moving much slower
// than it has been.
func (t *Toolbox) cadenceSignal(g *ids, th *Thread, priority model.Priority, quiet, baseline int) model.Signal {
	last := th.Last()
	who, side := t.waitingSide(th)

	return model.Signal{
		ID:       g.next(model.KindQuietThreads, "cadence", th.ID),
		Kind:     model.KindQuietThreads,
		Priority: priority,
		Title:    fmt.Sprintf("%s has slowed down — %s", who, truncate(th.Subject, 60)),
		// The sentence names what was measured. "Waiting on them for five days"
		// is a fact about one gap; the finding is that the reply cadence on this
		// thread has stretched, and a line that never says so leaves the reader
		// to infer the only thing the signal actually knows.
		Detail: fmt.Sprintf("%s for %s against a %s baseline over %d messages, so the reply cadence has stretched; %s. A slowdown is not proof of a problem — the thread is shown rather than ranked.",
			side, businessDayPhrase(quiet), businessDayPhrase(baseline), len(th.Messages),
			quote(truncate(last.Body, 140))),
		Citations:   dedupeCitations([]model.Citation{emailCite(last.ID), emailCite(th.First().ID)}),
		SectionHint: model.SectionNotSure,
		Confidence:  model.Unsure,
	}
}

// waitingSide names who the thread is waiting on, for the title and the detail.
func (t *Toolbox) waitingSide(th *Thread) (who, side string) {
	who = "thread"
	if len(th.Counterparts) > 0 {
		who = th.Counterparts[0].Name
		if who == "" {
			who = th.Counterparts[0].Email
		}
	}
	if th.AwaitingOwner {
		return who, "waiting on you"
	}
	return who, "waiting on them"
}

// cadenceSlowdown compares the current silence against the thread's own median
// gap and reports the baseline it used.
func (t *Toolbox) cadenceSlowdown(th *Thread, quiet int) (int, bool) {
	if len(th.Messages) < cadenceMinMessages {
		return 0, false
	}
	gaps := make([]int, 0, len(th.Messages)-1)
	for i := 1; i < len(th.Messages); i++ {
		gaps = append(gaps, businessDaysBetween(th.Messages[i-1].TS, th.Messages[i].TS, t.loc))
	}
	sort.Ints(gaps)
	baseline := gaps[len(gaps)/2]

	threshold := baseline * cadenceMultiple
	if threshold < cadenceFloorDays {
		threshold = cadenceFloorDays
	}
	return baseline, quiet > threshold
}

// missRuleRef cites the profile bullet that set the three-business-day
// threshold, or names the default when the profile has no such section.
func (t *Toolbox) missRuleRef() string {
	p := t.corpus.Profile
	if p == nil {
		return "aubade default"
	}
	for _, r := range p.MissRules {
		if containsWord(r.Text, "quiet") || containsWord(r.Text, "business days") {
			return t.ruleRef(r)
		}
	}
	return "aubade default"
}
