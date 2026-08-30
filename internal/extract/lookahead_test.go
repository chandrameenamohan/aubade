package extract

import (
	"strings"
	"testing"
	"time"

	"github.com/chandrameenamohan/aubade/internal/model"
)

// The three behaviours the trap harness found missing (bead D1), each with the
// boundary that keeps it from becoming noise.
//
// They are tested on hand-built corpora rather than on the shared fixture day,
// because each one turns on a single fact — a CREATED timestamp, a missing
// question mark, a note dated before a mail — and a fixture corpus that
// contained all three would make it impossible to see which one fired.

// day is a fixture instant on the anchor day at offset days.
func day(t *testing.T, offset, hour, minute int) time.Time {
	t.Helper()
	loc := model.Location()
	anchor, err := ParseToday(fixtureDay, loc)
	if err != nil {
		t.Fatalf("ParseToday: %v", err)
	}
	return anchor.AddDate(0, 0, offset).Add(time.Duration(hour)*time.Hour + time.Duration(minute)*time.Minute)
}

// deepWorkBlock is the standing block the owner put on their own calendar.
func deepWorkBlock(t *testing.T, offset int) model.CalEvent {
	t.Helper()
	return model.CalEvent{
		UID: "ev-block", Summary: "Deep work — no meetings",
		Start: day(t, offset, 9, 0), End: day(t, offset, 11, 0),
		Status:    model.StatusConfirmed,
		Organizer: model.Person{Name: "avery", Email: ownerTestAddr},
		Created:   day(t, -29, 8, 0),
	}
}

// booking is a meeting someone else put over it, created at the given offset.
func booking(t *testing.T, dayOffset, createdOffset int) model.CalEvent {
	t.Helper()
	return model.CalEvent{
		UID: "ev-pipeline", Summary: "Pipeline review — Q3 forecast", Location: "Zoom",
		Start: day(t, dayOffset, 9, 30), End: day(t, dayOffset, 10, 30),
		Status:    model.StatusConfirmed,
		Organizer: model.Person{Name: "tomas", Email: "tomas@example.test"},
		Created:   day(t, createdOffset, 21, 40),
	}
}

func conflictsOf(t *testing.T, events ...model.CalEvent) model.Signals {
	t.Helper()
	c := corpusOf(nil)
	c.Events = events
	ss, err := toolboxOf(t, c, fixtureDay).Conflicts()
	if err != nil {
		t.Fatalf("Conflicts: %v", err)
	}
	return ss
}

// The profile's grievance, answered: someone booked over next Tuesday's block
// last night, and the digest that follows says so.
func TestConflictsReportANewBookingOverAFutureBlock(t *testing.T) {
	ss := conflictsOf(t, deepWorkBlock(t, 2), booking(t, 2, -1))

	if len(ss) != 1 {
		t.Fatalf("got %d conflicts, want 1:\n%+v", len(ss), ss)
	}
	s := ss[0]
	if !strings.Contains(s.Title, "protected block") {
		t.Errorf("title = %q, want it to name the protected block", s.Title)
	}
	if s.Priority != model.P1 {
		t.Errorf("priority = %s, want P1", s.Priority)
	}
	if !cites(s, model.SourceCalendar, "ev-block") || !cites(s, model.SourceCalendar, "ev-pipeline") {
		t.Errorf("both sides of the collision must be cited, got %+v", s.Citations)
	}
	// The part the profile is actually complaining about is *when* it was
	// booked, so the line has to carry it.
	if !strings.Contains(s.Detail, "was added") {
		t.Errorf("detail does not say when it was booked: %q", s.Detail)
	}
}

// "I want to know" is answered once, the morning after someone does it — not
// every morning until the meeting happens. A standing complaint is noise, and
// noise is what the suppression half of this product exists to prevent.
func TestConflictsDoNotRepeatAnOldBookingOverABlock(t *testing.T) {
	if ss := conflictsOf(t, deepWorkBlock(t, 2), booking(t, 2, -8)); len(ss) != 0 {
		t.Errorf("a booking made a week ago is not this morning's news, got %+v", ss)
	}
}

// An event with no CREATED stamp is not evidence of anything. Guessing "new"
// would turn every meeting on every protected day into an alarm.
func TestConflictsIgnoreABookingWithNoProvenance(t *testing.T) {
	b := booking(t, 2, -1)
	b.Created = time.Time{}
	if ss := conflictsOf(t, deepWorkBlock(t, 2), b); len(ss) != 0 {
		t.Errorf("an undated booking must not fire the alarm, got %+v", ss)
	}
}

// A block with nothing over it is not a conflict, and neither is a meeting on a
// day with no block.
func TestConflictsLeaveAnUnviolatedBlockAlone(t *testing.T) {
	clear := booking(t, 2, -1)
	clear.Start, clear.End = day(t, 2, 14, 0), day(t, 2, 15, 0)

	if ss := conflictsOf(t, deepWorkBlock(t, 2), clear); len(ss) != 0 {
		t.Errorf("a meeting outside the block is not a violation, got %+v", ss)
	}
	if ss := conflictsOf(t, deepWorkBlock(t, 2)); len(ss) != 0 {
		t.Errorf("a block on its own is not a conflict, got %+v", ss)
	}
}

// The lookahead is bounded. A block three weeks out is not this morning's
// problem, whoever booked over it.
func TestConflictsStopAtTheLookaheadHorizon(t *testing.T) {
	beyond := blockLookaheadDays + 1
	if ss := conflictsOf(t, deepWorkBlock(t, beyond), booking(t, beyond, -1)); len(ss) != 0 {
		t.Errorf("a violation %d days out is past the horizon, got %+v", beyond, ss)
	}
}

// An approval is the smallest ask there is and it almost never arrives as a
// question. A dispatchables extractor that only reads question marks finds none
// of them.
func TestDispatchablesCatchAnApprovalWithNoQuestionMark(t *testing.T) {
	ask := msg(t, "m-expenses", "t-expenses", "2026-08-27T10:04:00-07:00", "nadia",
		"three expense reports need your approval",
		"Avery — three expense reports have been sitting in the queue since Monday. "+
			"They block this month's team payouts. One click each.\n\nNadia")

	ss, err := toolboxOf(t, corpusOf([]model.Email{ask}), fixtureDay).Dispatchables()
	if err != nil {
		t.Fatalf("Dispatchables: %v", err)
	}
	if len(ss) != 1 {
		t.Fatalf("got %d dispatchables, want 1:\n%+v", len(ss), ss)
	}
	if !cites(ss[0], model.SourceEmail, "m-expenses") {
		t.Errorf("the dispatchable must cite the ask, got %+v", ss[0].Citations)
	}
	if ss[0].SectionHint != model.SectionDecisions {
		t.Errorf("an approval belongs under decisions, got %q", ss[0].SectionHint)
	}
}

// The other direction: a statement that asks for nothing is not a dispatchable
// just because it is short and recent.
func TestDispatchablesIgnoreAMessageThatAsksNothing(t *testing.T) {
	fyi := msg(t, "m-fyi", "t-fyi", "2026-08-28T16:05:00-07:00", "tomas",
		"Fwd: EU AI Act guidance",
		"fyi, no action needed. Nothing here applies to us until next year.\n\nTomás")

	ss, err := toolboxOf(t, corpusOf([]model.Email{fyi}), fixtureDay).Dispatchables()
	if err != nil {
		t.Fatalf("Dispatchables: %v", err)
	}
	if len(ss) != 0 {
		t.Errorf("an FYI is not a one-reply item, got %+v", ss)
	}
}

// A note and a mail can disagree with no calendar anywhere in sight, and the
// digest has to show both sides. The two are tied together by the sender's own
// domain, which is the only place the customer's name appears in the mail.
func TestContradictionsCatchANoteAgainstALaterMail(t *testing.T) {
	note := model.Note{
		Path:  "notes/customer-veritas-renewal.md",
		Title: "Veritas renewal — call notes",
		Date:  day(t, -5, 0, 0),
		Body:  "Their CFO pushed the renewal conversation to next quarter. Tomás logged it the same afternoon.",
	}
	mail := model.Email{
		ID: "m-veritas-01", ThreadID: "t-veritas", TS: day(t, -3, 11, 40),
		From:    model.Person{Name: "Luis Ferrer", Email: "luis@veritas.example"},
		To:      []model.Person{{Name: "avery", Email: ownerTestAddr}},
		Subject: "renewal paperwork",
		Body:    "Avery — confirming we're still targeting the September 30 renewal date on our side.",
	}

	c := corpusOf([]model.Email{mail})
	c.Notes = []model.Note{note}

	ss, err := toolboxOf(t, c, fixtureDay).Contradictions()
	if err != nil {
		t.Fatalf("Contradictions: %v", err)
	}
	if len(ss) != 1 {
		t.Fatalf("got %d contradictions, want 1:\n%+v", len(ss), ss)
	}
	s := ss[0]
	if !cites(s, model.SourceNote, note.Path) || !cites(s, model.SourceEmail, "m-veritas-01") {
		t.Errorf("both sides must be cited, got %+v", s.Citations)
	}
	if s.SectionHint != model.SectionHonesty {
		t.Errorf("a contradiction belongs in the honesty section, got %q", s.SectionHint)
	}
	if !strings.Contains(s.Detail, "neither was picked") {
		t.Errorf("nothing here may resolve the disagreement: %q", s.Detail)
	}
}

// One shared word is a coincidence. A fabricated contradiction is worse than a
// missed one, so the matcher needs two.
func TestContradictionsDoNotMatchOnOneSharedWord(t *testing.T) {
	note := model.Note{
		Path:  "notes/pricing.md",
		Title: "Pricing workshop notes",
		Date:  day(t, -5, 0, 0),
		Body:  "The rollout was pushed to next quarter.",
	}
	mail := model.Email{
		ID: "m-unrelated", ThreadID: "t-unrelated", TS: day(t, -3, 11, 40),
		From:    model.Person{Name: "Ines", Email: "ines@brightmoor.example"},
		To:      []model.Person{{Name: "avery", Email: ownerTestAddr}},
		Subject: "questionnaire",
		Body:    "Still on for the security questionnaire this week.",
	}

	c := corpusOf([]model.Email{mail})
	c.Notes = []model.Note{note}

	ss, err := toolboxOf(t, c, fixtureDay).Contradictions()
	if err != nil {
		t.Fatalf("Contradictions: %v", err)
	}
	if len(ss) != 0 {
		t.Errorf("two unrelated sources must not be reported as disagreeing, got %+v", ss)
	}
}

// The later source is the one still holding. A note written *after* the mail
// has superseded it, and reporting that as a disagreement would flag every
// question that got answered.
func TestContradictionsIgnoreAMailThatPredatesTheNote(t *testing.T) {
	note := model.Note{
		Path:  "notes/customer-veritas-renewal.md",
		Title: "Veritas renewal — call notes",
		Date:  day(t, -2, 0, 0),
		Body:  "Their CFO pushed the renewal conversation to next quarter.",
	}
	mail := model.Email{
		ID: "m-veritas-old", ThreadID: "t-veritas", TS: day(t, -6, 11, 40),
		From:    model.Person{Name: "Luis Ferrer", Email: "luis@veritas.example"},
		To:      []model.Person{{Name: "avery", Email: ownerTestAddr}},
		Subject: "renewal paperwork",
		Body:    "Avery — confirming we're still targeting the September 30 renewal date.",
	}

	c := corpusOf([]model.Email{mail})
	c.Notes = []model.Note{note}

	ss, err := toolboxOf(t, c, fixtureDay).Contradictions()
	if err != nil {
		t.Fatalf("Contradictions: %v", err)
	}
	if len(ss) != 0 {
		t.Errorf("the note is the later source; there is nothing to report, got %+v", ss)
	}
}
