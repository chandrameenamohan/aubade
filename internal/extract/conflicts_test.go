package extract

import (
	"strings"
	"testing"

	"github.com/chandrameenamohan/aubade/internal/model"
)

// The three collisions the fixture day is built around, and nothing else.
func TestConflictsClassifiesEachCollision(t *testing.T) {
	ss, err := loadFixture(t, "corpus", fixtureDay).Conflicts()
	if err != nil {
		t.Fatalf("Conflicts: %v", err)
	}
	if len(ss) != 3 {
		t.Fatalf("got %d conflicts, want 3:\n%+v", len(ss), ss)
	}

	cases := []struct {
		id       string
		title    string
		priority model.Priority
	}{
		{"conflicts:ev-gtm-sync:ev-wren-pediatrician", "family collision", model.P0},
		{"conflicts:ev-lumen-demo:ev-deep-work", "protected block", model.P1},
		{"conflicts:transition:ev-wren-pediatrician:ev-halberd-prep", "impossible transition", model.P0},
	}
	for _, tc := range cases {
		s, ok := findSignal(ss, tc.id)
		if !ok {
			t.Errorf("missing conflict %s", tc.id)
			continue
		}
		if !strings.Contains(s.Title, tc.title) {
			t.Errorf("%s title = %q, want it to say %q", tc.id, s.Title, tc.title)
		}
		if s.Priority != tc.priority {
			t.Errorf("%s priority = %s, want %s", tc.id, s.Priority, tc.priority)
		}
		if len(s.Citations) != 2 {
			t.Errorf("%s cites %d events, want both sides of the collision", tc.id, len(s.Citations))
		}
	}
}

// A meeting that is not happening cannot collide with one that is. The fixture
// puts a CANCELLED review at 09:00 on top of the 1:1 precisely so this stays
// true.
func TestConflictsIgnoreCancelledAndDeclinedEvents(t *testing.T) {
	ss, _ := loadFixture(t, "corpus", fixtureDay).Conflicts()
	if anyCites(ss, model.SourceCalendar, "ev-veritas-review") {
		t.Error("a cancelled event was treated as a live conflict")
	}
}

// Suppression is scoped. "Calendar invites I already accepted" is a display
// preference — it must not delete the accepted meeting from conflict detection,
// or the profile would be able to hide a collision from the person who wrote it.
func TestConflictsSeeSuppressedButAcceptedInvites(t *testing.T) {
	tb := loadFixture(t, "corpus", fixtureDay)

	supp, err := tb.Suppressions()
	if err != nil {
		t.Fatalf("Suppressions: %v", err)
	}
	if !anyCites(supp, model.SourceCalendar, "ev-gtm-sync") {
		t.Fatal("fixture broken: ev-gtm-sync should be suppressed as an accepted invite")
	}

	conflicts, _ := tb.Conflicts()
	if !anyCites(conflicts, model.SourceCalendar, "ev-gtm-sync") {
		t.Error("an accepted-and-suppressed invite disappeared from conflict detection")
	}
}

// Only the anchor day. Tomorrow's double-booking is tomorrow's problem, and an
// all-day marker is not a meeting.
func TestConflictsAreScopedToTheAnchorDay(t *testing.T) {
	ss, _ := loadFixture(t, "corpus", "2026-09-07").Conflicts()
	if len(ss) != 0 {
		t.Errorf("got %d conflicts on a day holding only an all-day marker: %+v", len(ss), ss)
	}
}

// Back-to-back is only impossible when the rooms are real. Two Zoom calls in a
// row are a normal Tuesday.
func TestConflictsIgnoreVirtualBackToBack(t *testing.T) {
	if isVirtual("Zoom") != true || isVirtual("https://meet.example/x") != true {
		t.Error("a link is not a room")
	}
	if isVirtual("Bayview Pediatrics") {
		t.Error("a clinic is not a link")
	}
}
