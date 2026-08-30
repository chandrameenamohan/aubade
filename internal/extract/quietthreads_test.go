package extract

import (
	"strings"
	"testing"

	"github.com/chandrameenamohan/aubade/internal/model"
)

// The profile's own threshold, on the population it was written for: an
// investor thread the owner opened, which has now been silent for five business
// days.
func TestQuietThreadsFireOnTheProfileThreshold(t *testing.T) {
	ss, err := loadFixture(t, "corpus", fixtureDay).QuietThreads()
	if err != nil {
		t.Fatalf("QuietThreads: %v", err)
	}

	s, ok := findSignal(ss, "quiet-threads:t-inflection")
	if !ok {
		t.Fatalf("no quiet-thread signal for t-inflection; got %d: %+v", len(ss), ss)
	}
	if s.Priority != model.P0 {
		t.Errorf("priority = %s, want P0 (Marcus, profile.md)", s.Priority)
	}
	if s.SectionHint != model.SectionOneThingNow {
		t.Errorf("section = %s, want %s", s.SectionHint, model.SectionOneThingNow)
	}
	if !strings.Contains(s.Detail, "5 business days") {
		t.Errorf("detail should count business days, not calendar days:\n%s", s.Detail)
	}
	if !cites(s, model.SourceEmail, "e-019") || !cites(s, model.SourceEmail, "e-017") {
		t.Errorf("quiet thread should cite its last and first message: %v", s.Citations)
	}
}

// Two of the user's own rules collide on this thread — "long threads where I've
// already had the last word" must not surface, "quiet investor threads" must.
// The resolution is stated on the signal rather than buried in a precedence
// table, and it quotes both bullets.
func TestQuietThreadsExplainTheProfileOverride(t *testing.T) {
	ss, _ := loadFixture(t, "corpus", fixtureDay).QuietThreads()
	s, ok := findSignal(ss, "quiet-threads:t-inflection")
	if !ok {
		t.Fatal("missing t-inflection quiet-thread signal")
	}
	for _, want := range []string{"last word", "stops at P0/P1", "profile.md:"} {
		if !strings.Contains(s.Detail, want) {
			t.Errorf("detail does not explain the override (%q missing):\n%s", want, s.Detail)
		}
	}
}

// A customer thread that has not crossed the absolute threshold but has fallen
// off its own rhythm is a judgment call, so it renders as one.
func TestQuietThreadsCadenceSlowdownIsUnsure(t *testing.T) {
	ss, _ := loadFixture(t, "corpus", fixtureDay).QuietThreads()

	s, ok := findSignal(ss, "quiet-threads:cadence:t-northstar")
	if !ok {
		t.Fatalf("no cadence signal for t-northstar; got: %+v", ss)
	}
	if s.Confidence != model.Unsure {
		t.Errorf("confidence = %s, want unsure", s.Confidence)
	}
	if s.SectionHint != model.SectionNotSure {
		t.Errorf("section = %s, want %s", s.SectionHint, model.SectionNotSure)
	}
	if !strings.Contains(s.Detail, "baseline") {
		t.Errorf("detail should name the baseline it compared against:\n%s", s.Detail)
	}
}

// Hard negatives. Each of these is a thread that looks quiet by one crude
// measure and is not actually quiet at all.
func TestQuietThreadsHardNegatives(t *testing.T) {
	ss, _ := loadFixture(t, "corpus", fixtureDay).QuietThreads()

	for _, tc := range []struct {
		id  string
		why string
	}{
		{"quiet-threads:t-model", "the owner answered the question; the thread is finished, not quiet"},
		{"quiet-threads:t-print", "suppressed by the profile's last-word rule at P2"},
		{"quiet-threads:t-news", "a newsletter is not a conversation"},
		{"quiet-threads:t-halberd", "one business day old is not quiet"},
	} {
		if _, found := findSignal(ss, tc.id); found {
			t.Errorf("%s should not fire: %s", tc.id, tc.why)
		}
	}
}

// Business-day arithmetic is the difference between a real threshold and a
// threshold that fires every Monday. A Thursday message is one business day old
// on Friday and two on Monday — never four.
func TestQuietThreadsCountBusinessDaysNotCalendarDays(t *testing.T) {
	// The owner reaches out on Thursday and hears nothing.
	out := fromOwner(msg(t, "m-1", "th-1", "2026-08-27T09:00:00-07:00", "x", "raise", "opened the data room."), "marcus")
	c := corpusOf([]model.Email{out})
	c.Profile.People = []model.ProfilePerson{{
		Name: "marcus", Priority: model.P0, Priorities: []model.Priority{model.P0}, Line: 1,
	}}

	for _, tc := range []struct {
		today string
		want  int
	}{
		{"2026-08-31", 0}, // Mon: Fri + Mon = 2 business days, under the threshold
		{"2026-09-02", 1}, // Wed: Fri, Mon, Tue, Wed = 4 business days, over it
	} {
		ss, err := toolboxOf(t, c, tc.today).QuietThreads()
		if err != nil {
			t.Fatalf("QuietThreads(%s): %v", tc.today, err)
		}
		if len(ss) != tc.want {
			t.Errorf("today=%s: got %d quiet threads, want %d", tc.today, len(ss), tc.want)
		}
	}
}
