package extract

import (
	"strings"
	"testing"

	"github.com/chandrameenamohan/aubade/internal/model"
)

// A corpus missing three of its five sources has to say so, loudly, before it
// says anything else. A digest that quietly thins is the failure mode this
// extractor exists to prevent (HLD §7).
func TestStalenessReportsMissingSources(t *testing.T) {
	ss, err := loadFixture(t, "thin", fixtureDay).Staleness()
	if err != nil {
		t.Fatalf("Staleness: %v", err)
	}

	cases := []struct {
		id       string
		ref      string
		source   model.Source
		priority model.Priority
	}{
		{"staleness:missing:calendar", "calendar.ics", model.SourceCalendar, model.P0},
		{"staleness:missing:note", "notes/", model.SourceNote, model.P0},
		{"staleness:missing:profile", "profile.md", model.SourceNote, model.P1},
	}
	for _, tc := range cases {
		s, ok := findSignal(ss, tc.id)
		if !ok {
			t.Errorf("missing %s", tc.id)
			continue
		}
		if !cites(s, tc.source, tc.ref) {
			t.Errorf("%s cites %v, want %s:%s — a citation must mean the same on every machine",
				tc.id, s.Citations, tc.source, tc.ref)
		}
		if s.Priority != tc.priority {
			t.Errorf("%s priority = %s, want %s", tc.id, s.Priority, tc.priority)
		}
	}
}

// The profile's own budget: "If the inbox data is older than 24 hours, say so."
func TestStalenessReportsAnOldInbox(t *testing.T) {
	ss, _ := loadFixture(t, "thin", fixtureDay).Staleness()

	s, ok := findSignal(ss, "staleness:inbox")
	if !ok {
		t.Fatalf("an eleven-day-old inbox was not reported stale; got: %+v", ss)
	}
	if !cites(s, model.SourceEmail, "e-100") {
		t.Errorf("staleness should cite the newest message it found: %v", s.Citations)
	}
	if !strings.Contains(s.Title, "days") {
		t.Errorf("title = %q; an eleven-day gap should be reported in days, not hours", s.Title)
	}

	// The fresh fixture must not trip it.
	fresh, _ := loadFixture(t, "corpus", fixtureDay).Staleness()
	if _, found := findSignal(fresh, "staleness:inbox"); found {
		t.Error("a twelve-hour-old inbox is not stale")
	}
}

// A note with no date cannot be aged, and anything drawn from it inherits that.
func TestStalenessReportsUndatedNotes(t *testing.T) {
	ss, _ := loadFixture(t, "corpus", fixtureDay).Staleness()

	s, ok := findSignal(ss, "staleness:undated:notes/sprint-aug-week4.md")
	if !ok {
		t.Fatalf("the undated note was not reported; got: %+v", ss)
	}
	if s.SectionHint != model.SectionHonesty {
		t.Errorf("section = %s, want %s", s.SectionHint, model.SectionHonesty)
	}
	// The dated note is fine.
	if _, found := findSignal(ss, "staleness:undated:notes/staffing-sync.md"); found {
		t.Error("a note with a date in its front matter is not undated")
	}
}

// A calendar whose last event predates the anchor day is likelier a stale
// export than an empty week — and that guess is marked as a guess.
func TestStalenessFlagsAnEmptyForwardCalendar(t *testing.T) {
	ss, _ := loadFixture(t, "corpus", "2026-09-30").Staleness()

	s, ok := findSignal(ss, "staleness:calendar")
	if !ok {
		t.Fatalf("a calendar with nothing in the future was not flagged; got: %+v", ss)
	}
	if s.Confidence != model.Unsure {
		t.Errorf("confidence = %s, want unsure: an empty day is possible", s.Confidence)
	}
}
