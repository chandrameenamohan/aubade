package datagen

import (
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/chandrameenamohan/aubade/internal/model"
)

// The filler is the other half of the exam: the thirty days of ordinary mail,
// meetings and notes the planted traps have to be found *inside*.
//
// It is a separate layer from the scenarios on purpose (package doc). A trap is
// scripted and never rolled; filler is rolled and never scripted, and the rule
// that keeps the two honest is that the filler may only add artifacts no trap
// depends on. Nothing here returns a Trap, nothing here touches an id a scenario
// used, and Generate re-validates the plan afterwards so a violation is a build
// failure rather than a subtly wrong answer key.
//
// The filler also has a second, less obvious job: to not answer the exam by
// accident. Every extractor is a detector, and a detector fires on whatever
// matches — so filler that ends a thread with an unanswered question from a P0
// colleague plants a quiet-thread finding nobody scripted, and the digest fills
// with items the scorecard has no opinion about. The shapes below are therefore
// deliberately *closed*:
//
//   - a conversation is always opened by the counterpart, never by Avery, so it
//     can never read as "the owner reached out and nobody answered";
//   - it always ends with a message that asks for nothing — Avery's short
//     acknowledgement, or the other side's;
//   - noise is single-message and labelled, so the priority map floors it at P4
//     and the suppression rules the profile actually wrote pick it up;
//   - a filler calendar event is never allowed to overlap anything already on
//     the calendar, and never puts a physical location on the anchor day.
//
// The result is volume that makes recall hard without making precision
// unfalsifiable.

// noiseLabels are the corpus label vocabulary for mail nobody has to act on.
// They are the same words the priority map floors at P4, which is the point:
// the noise is noisy because it is labelled the way real bulk mail is, not
// because the generator marked it as filler.
var noiseLabels = []string{"newsletter", "marketing", "recruiter", "automated"}

// businessDayWeight is how much more mail arrives on a working day than on a
// Saturday. Avery's inbox does not stop at the weekend, it thins out.
const businessDayWeight = 6

// filler weaves the ordinary corpus around the planted traps.
type filler struct {
	s *Script

	// Sequence counters, one per artifact kind, so ids are stable and readable
	// ("f-m-0107") and never collide with a scenario's. refSeq numbers the
	// tracking references unique() falls back on.
	mailSeq, threadSeq, eventSeq, refSeq int

	// seen keys a message by subject and body. Two identical messages in an
	// inbox of five hundred is what a template-generated corpus looks like from
	// the outside, and a digest that learns "this shape is always noise" from a
	// degenerate corpus has learned nothing.
	seen map[string]bool

	// days and weights drive the arrival distribution over the corpus window.
	days    []time.Time
	weights []int
	total   int

	// recentRecruiters counts recruiter cold mail per firm inside the profile's
	// one-week pattern window. The Kestrel Search trap is the only firm allowed
	// to reach three: a second accidental pattern would be a finding nobody
	// scripted and nobody grades.
	recentRecruiters map[string]int
}

func newFiller(s *Script) *filler {
	f := &filler{s: s, seen: map[string]bool{}, recentRecruiters: map[string]int{}}
	for i := CorpusDays - 1; i >= 0; i-- {
		day := s.Days(-i)
		w := 1
		if isBusinessDay(day) {
			w = businessDayWeight
		}
		f.days = append(f.days, day)
		f.weights = append(f.weights, w)
		f.total += w
	}
	for _, e := range s.plan.Emails {
		f.seen[dedupeKey(e.Subject, e.Body)] = true
	}
	return f
}

// run writes the whole filler layer.
func (f *filler) run() {
	f.notes()
	f.calendar()
	f.mail()
}

func (f *filler) rng() *rand.Rand { return f.s.rng }

// pick returns one of the choices. It is Script.Pick over any element type —
// the filler picks publications and topics as often as it picks sentences — and
// it is a function rather than a method because Go does not allow methods to
// introduce type parameters.
func pick[T any](f *filler, choices []T) T {
	return choices[f.rng().IntN(len(choices))]
}

// day returns a day in the corpus window, weighted towards working days.
func (f *filler) day() time.Time {
	r := f.rng().IntN(f.total)
	for i, w := range f.weights {
		if r < w {
			return f.days[i]
		}
		r -= w
	}
	return f.days[len(f.days)-1]
}

// arrivalWindow is a span of the day one kind of sender writes in, in minutes
// past midnight.
type arrivalWindow struct{ from, to int }

// ownerWindows are the three windows profile.md describes: "I read email
// between 6:15-6:45am, then 12:30-1pm, then once after Wren is asleep."
var ownerWindows = []arrivalWindow{{6*60 + 15, 6*60 + 45}, {12*60 + 30, 13 * 60}, {21 * 60, 22*60 + 30}}

// workWindows are everybody else's working day.
var workWindows = []arrivalWindow{{8 * 60, 18*60 + 30}}

// bulkWindows are when machines and mailing lists send: early, and all day.
var bulkWindows = []arrivalWindow{{5 * 60, 7 * 60}, {9 * 60, 17 * 60}}

// slotAfter returns the first plausible sending time strictly after t for the
// given sender.
//
// Always moving forward is what makes a generated thread valid by construction:
// every reply lands after the message it answers, whatever hour the seed picked
// for either, and the plan's reply-ordering invariant can never be violated by
// an unlucky draw.
func (f *filler) slotAfter(t time.Time, windows []arrivalWindow, businessOnly bool) time.Time {
	day := f.s.At(t, 0, 0)
	for range CorpusDays + lookaheadDays {
		if businessOnly && !isBusinessDay(day) {
			day = day.AddDate(0, 0, 1)
			continue
		}
		for _, w := range windows {
			lo, hi := f.s.At(day, w.from/60, w.from%60), f.s.At(day, w.to/60, w.to%60)
			if !t.Before(lo) {
				lo = t.Add(time.Minute)
			}
			if lo.Before(hi) {
				span := int(hi.Sub(lo) / time.Minute)
				return lo.Add(time.Duration(f.rng().IntN(span+1)) * time.Minute)
			}
		}
		day = day.AddDate(0, 0, 1)
	}
	f.s.failf("no sending slot within the corpus window after %s", t.Format(time.RFC3339))
	return t.Add(time.Hour)
}

// lastInstant is the latest a filler message may arrive: the end of the anchor
// day's last sending window. The corpus stops there because the digest is
// written on the anchor morning, and mail dated after it is mail nobody could
// have received.
func (f *filler) lastInstant() time.Time { return f.s.At(f.s.Today(), 23, 0) }

// mailID returns the next filler message id.
func (f *filler) mailID() string {
	f.mailSeq++
	return fmt.Sprintf("f-m-%04d", f.mailSeq)
}

// threadID returns the next filler thread id.
func (f *filler) threadID() string {
	f.threadSeq++
	return fmt.Sprintf("f-t-%04d", f.threadSeq)
}

// eventUID returns the next filler event UID.
func (f *filler) eventUID(slug string) string {
	f.eventSeq++
	return fmt.Sprintf("f-ev-%03d-%s", f.eventSeq, slug)
}

// dedupeKey identifies a message by what a reader would actually see.
func dedupeKey(subject, body string) string { return subject + "\x00" + body }

// unique returns a body that no other message in the corpus already carries.
//
// The re-roll is bounded and then gives up into a fallback that is itself
// plausible mail — a thread reference line — because a generator that loops
// until it wins is a generator that hangs on the day the pools get small.
func (f *filler) unique(subject string, body func() string) string {
	for range 12 {
		b := body()
		if !f.seen[dedupeKey(subject, b)] {
			f.seen[dedupeKey(subject, b)] = true
			return b
		}
	}
	f.refSeq++
	b := fmt.Sprintf("%s\n\n[ref TSR-%04d]", body(), f.refSeq)
	f.seen[dedupeKey(subject, b)] = true
	return b
}

// noiseCount counts mail nobody has to act on, so the filler can top the corpus
// up to the share the SPEC asks for rather than guessing at it.
func noiseCount(emails []model.Email) int {
	n := 0
	for _, e := range emails {
		if isNoise(e) {
			n++
		}
	}
	return n
}

// isNoise reports whether a message is labelled as bulk mail.
func isNoise(e model.Email) bool {
	for _, l := range e.Labels {
		for _, n := range noiseLabels {
			if l == n {
				return true
			}
		}
	}
	return false
}
