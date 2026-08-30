package extract

import (
	"strings"
	"testing"

	"github.com/chandrameenamohan/aubade/internal/model"
)

// The mail says 2pm, the invite says 10:30. Both sides are shown, with a
// citation each, and nothing is resolved — the profile is explicit that picking
// one and hiding the other is the failure.
func TestContradictionsKeepBothSides(t *testing.T) {
	ss, err := loadFixture(t, "corpus", fixtureDay).Contradictions()
	if err != nil {
		t.Fatalf("Contradictions: %v", err)
	}

	s, ok := findSignal(ss, "contradictions:ev-lumen-demo:e-008")
	if !ok {
		t.Fatalf("no email-vs-calendar contradiction for the Lumen demo; got: %+v", ss)
	}
	if !cites(s, model.SourceCalendar, "ev-lumen-demo") || !cites(s, model.SourceEmail, "e-008") {
		t.Errorf("both sides must be cited: %v", s.Citations)
	}
	for _, want := range []string{"14:00", "10:30", "neither was picked"} {
		if !strings.Contains(s.Detail, want) {
			t.Errorf("detail does not carry %q:\n%s", want, s.Detail)
		}
	}
	if s.SectionHint != model.SectionHonesty {
		t.Errorf("section = %s, want %s", s.SectionHint, model.SectionHonesty)
	}
}

// The harder disagreement: two sources that are each internally consistent. The
// invite is cancelled and the counterparty is still writing "see you at 9".
func TestContradictionsCatchACancelledMeetingTreatedAsLive(t *testing.T) {
	ss, _ := loadFixture(t, "corpus", fixtureDay).Contradictions()

	s, ok := findSignal(ss, "contradictions:status:ev-veritas-review:e-009")
	if !ok {
		t.Fatalf("no status contradiction for the cancelled Veritas review; got: %+v", ss)
	}
	if !strings.Contains(s.Detail, "cancelled") {
		t.Errorf("detail should say which side is cancelled:\n%s", s.Detail)
	}
	if s.Priority != model.P1 {
		t.Errorf("priority = %s, want P1 for a disagreement about today", s.Priority)
	}
}

// Hard negatives: an email that merely mentions a meeting is not a
// contradiction, and neither is one that agrees with the calendar.
func TestContradictionsStayQuietWhenSourcesAgree(t *testing.T) {
	tb := loadFixture(t, "corpus", fixtureDay)
	ss, _ := tb.Contradictions()

	if len(ss) != 2 {
		t.Fatalf("got %d contradictions, want exactly the two planted ones:\n%+v", len(ss), ss)
	}
	for _, s := range ss {
		if cites(s, model.SourceEmail, "e-013") {
			t.Error("an FYI about regressions is not a calendar disagreement")
		}
	}
}

// A time statement needs something to attach to. A number in an email that
// shares no vocabulary with any meeting is a number.
func TestContradictionsNeedTwoWordsInCommon(t *testing.T) {
	tb := loadFixture(t, "corpus", fixtureDay)
	assertion, ok := ParseTimeAssertion("the Lumen demo moved to 2pm on Monday", ts(t, "2026-08-28T09:00:00-07:00"), tb.Location())
	if !ok {
		t.Fatal("ParseTimeAssertion found nothing in a sentence stating a day and a time")
	}
	if !assertion.HasDate || !assertion.HasClock {
		t.Errorf("assertion = %+v, want both a date and a clock", assertion)
	}
	if got := assertion.At.Format("2006-01-02 15:04"); got != "2026-08-31 14:00" {
		t.Errorf("resolved to %s, want 2026-08-31 14:00 (the Monday after the mail)", got)
	}

	if _, ok := ParseTimeAssertion("thanks for the update", ts(t, "2026-08-28T09:00:00-07:00"), tb.Location()); ok {
		t.Error("a sentence with no date and no clock should assert nothing")
	}
}
