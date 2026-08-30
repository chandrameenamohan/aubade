package datagen

import (
	"fmt"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/chandrameenamohan/aubade/internal/model"
)

// Scenario is one planted trap: a script that writes its own artifacts into the
// corpus and hands back the answer-key entry that grades them.
//
// This signature is the load-bearing decision of the whole generator (HLD §6).
// The obvious alternative — generate a corpus, then write traps.json against it
// — puts the exam and the answer key in two places that have to be kept in
// agreement by hand, and they will not stay in agreement: someone re-times an
// email, the key still says the thread went quiet on Tuesday, and the eval
// starts grading a question the corpus no longer asks. Here a trap cannot
// describe evidence it did not plant, because the citation it returns *is* the
// value the emitter handed back.
type Scenario func(*Script) Trap

// Script is the pen a scenario writes with: the corpus clock, a deterministic
// source of variation, and the four emitters. Every emitter returns the
// model.Citation that points at what it just wrote, so a trap's planted_refs
// are assembled from the artifacts themselves rather than typed out again.
type Script struct {
	today time.Time // midnight, anchor zone, on --today
	loc   *time.Location
	rng   *rand.Rand
	plan  *Plan

	// errs collects scripting bugs. Build attributes each one to the scenario
	// that was running when it was recorded, so a stray date says which script
	// to go and look at.
	errs []error
}

// Today is midnight on the anchor date, in the corpus zone.
func (s *Script) Today() time.Time { return s.today }

// Days is midnight n days from today; negative is the past.
func (s *Script) Days(n int) time.Time { return s.today.AddDate(0, 0, n) }

// At is a wall-clock time on the given day, in the corpus zone.
func (s *Script) At(day time.Time, hour, min int) time.Time {
	y, m, d := day.Date()
	return time.Date(y, m, d, hour, min, 0, 0, s.loc)
}

// DayAt is At(Days(n), hour, min) — the form most scenario lines want.
func (s *Script) DayAt(n, hour, min int) time.Time { return s.At(s.Days(n), hour, min) }

// BusinessDaysAgo returns the most recent *business* day before today with
// exactly n business days (Mon–Fri) in the interval after it up to and
// including today. n of zero is today itself.
//
// Landing on a business day rather than on whatever date the count stops at is
// deliberate on both counts: it keeps the arithmetic honest (walking back over
// a weekend adds no business days, so the count is unchanged) and it keeps the
// corpus plausible, because the messages timed with this are ones colleagues
// and investors sent during a working week.
//
// Quiet-thread traps are timed with this rather than with a raw day offset, and
// the difference is the whole trap. "Three business days" is what profile.md
// says and what the extractor must implement; a script that plants "six days
// ago" happens to mean four business days when today is a Sunday and three when
// it is a Thursday, so the same corpus would sit on either side of the
// threshold depending on the anchor. Counted in the unit the rule is written
// in, a trap stays the trap it was written to be for every --today.
func (s *Script) BusinessDaysAgo(n int) time.Time {
	d := s.today
	if n <= 0 {
		return d
	}
	elapsed := 0
	for elapsed < n {
		if isBusinessDay(d) {
			elapsed++
		}
		d = d.AddDate(0, 0, -1)
	}
	for !isBusinessDay(d) {
		d = d.AddDate(0, 0, -1)
	}
	return d
}

// NextWeekday is the next occurrence of w strictly after today.
//
// The deep-work traps use it because "Tue/Thu 9–11" is a rule about weekdays,
// not about offsets: pinning the violation to "four days from now" would land
// it on a Thursday for one anchor and a Sunday for the next.
func (s *Script) NextWeekday(w time.Weekday) time.Time {
	d := s.today.AddDate(0, 0, 1)
	for d.Weekday() != w {
		d = d.AddDate(0, 0, 1)
	}
	return d
}

func isBusinessDay(d time.Time) bool {
	switch d.Weekday() {
	case time.Saturday, time.Sunday:
		return false
	}
	return true
}

// Rand is the seeded source. Scenarios use it only for detail that no trap
// depends on — which of three interchangeable phrasings a recruiter used, what
// minute past the hour a reply landed. The questions themselves are scripted,
// never rolled: an exam whose answers move with the seed cannot have a golden
// digest, and a trap that is only sometimes present is a flaky test wearing a
// dataset's clothes.
func (s *Script) Rand() *rand.Rand { return s.rng }

// Pick returns one of the choices, chosen deterministically from the seed.
func (s *Script) Pick(choices ...string) string {
	if len(choices) == 0 {
		return ""
	}
	return choices[s.rng.IntN(len(choices))]
}

// Email plants one message and returns the citation that points at it.
//
// To defaults to Avery, because almost every planted message is addressed to
// her; CC is normalized to an empty slice so the JSON contract's `cc` key is an
// array rather than a null.
func (s *Script) Email(e model.Email) model.Citation {
	if len(e.To) == 0 {
		e.To = to(Avery)
	}
	if e.CC == nil {
		e.CC = []model.Person{}
	}
	if err := e.Validate(); err != nil {
		s.failf("email: %w", err)
	}
	s.plan.Emails = append(s.plan.Emails, e)
	return model.Citation{Source: model.SourceEmail, Ref: e.ID}
}

// Event plants one calendar event and returns its citation.
//
// Status defaults to CONFIRMED and Created to a week before the start: invites
// in this corpus go out a week ahead unless a scenario says otherwise, and the
// scenarios that care — Sam adding Wren's pediatrician at 21:04 last night —
// say otherwise, because for those the provenance timestamp *is* the fact.
func (s *Script) Event(ev model.CalEvent) model.Citation {
	if ev.Status == "" {
		ev.Status = model.StatusConfirmed
	}
	if ev.Organizer.Email == "" {
		ev.Organizer = Avery
	}
	if ev.Created.IsZero() {
		ev.Created = ev.Start.AddDate(0, 0, -7)
	}
	switch {
	case strings.TrimSpace(ev.UID) == "":
		s.failf("event %q has no UID", ev.Summary)
	case strings.TrimSpace(ev.Summary) == "":
		s.failf("event %s has no summary", ev.UID)
	case ev.Start.IsZero() || ev.End.IsZero():
		s.failf("event %s has no start or end", ev.UID)
	case !ev.End.After(ev.Start):
		s.failf("event %s ends at or before it starts", ev.UID)
	case !ev.Status.Valid():
		s.failf("event %s has status %q", ev.UID, ev.Status)
	}
	for _, a := range ev.Attendees {
		if !a.PartStat.Valid() {
			s.failf("event %s: attendee %s has partstat %q", ev.UID, a.Email, a.PartStat)
		}
	}
	s.plan.Events = append(s.plan.Events, ev)
	return model.Citation{Source: model.SourceCalendar, Ref: ev.UID}
}

// Note plants one markdown note and returns its citation. The path is the
// citation ref, so it is corpus-relative and lives under notes/.
func (s *Script) Note(n model.Note) model.Citation {
	if !strings.HasPrefix(n.Path, "notes/") || !strings.HasSuffix(n.Path, ".md") {
		s.failf("note path %q must be notes/<name>.md", n.Path)
	}
	if strings.TrimSpace(n.Title) == "" {
		s.failf("note %s has no title", n.Path)
	}
	if strings.TrimSpace(n.Body) == "" {
		s.failf("note %s has no body", n.Path)
	}
	s.plan.Notes = append(s.plan.Notes, n)
	return model.Citation{Source: model.SourceNote, Ref: n.Path}
}

// Task plants one tasks.md item and returns its citation.
//
// Line is deliberately left unset: it is a fact about the file the writer lays
// out, not about the plan, and a number invented here would be a citation that
// points at the wrong row the moment the file gains a heading.
func (s *Script) Task(t model.Task) model.Citation {
	if strings.TrimSpace(t.ID) == "" {
		s.failf("task %q has no id", t.Title)
	}
	if strings.TrimSpace(t.Title) == "" {
		s.failf("task %s has no title", t.ID)
	}
	s.plan.Tasks = append(s.plan.Tasks, t)
	return model.Citation{Source: model.SourceTask, Ref: t.ID}
}

// Attendees builds an attendee list where everyone shares one participation
// status — the common case. Traps that turn on one person's RSVP spell it out.
func Attendees(status model.PartStat, people ...model.Person) []model.Attendee {
	out := make([]model.Attendee, 0, len(people))
	for _, p := range people {
		out = append(out, model.Attendee{Person: p, PartStat: status, Role: "REQ-PARTICIPANT"})
	}
	return out
}

// failf records a scripting bug. Emitters keep going and return a usable
// citation so the scenario finishes and every other problem in it surfaces in
// the same run; Build refuses to hand back a plan that collected any.
func (s *Script) failf(format string, args ...any) {
	s.errs = append(s.errs, fmt.Errorf(format, args...))
}

// attribute names the scenario responsible for every error recorded since
// index from. The trap id is only known once the scenario returns, which is why
// this happens afterwards rather than in failf.
func (s *Script) attribute(from int, trapID string) {
	for i := from; i < len(s.errs); i++ {
		s.errs[i] = fmt.Errorf("scenario %s: %w", trapID, s.errs[i])
	}
}
