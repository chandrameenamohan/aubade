package extract

import (
	"strings"
	"testing"

	"github.com/chandrameenamohan/aubade/internal/model"
)

// A short, direct, answerable ask from a reference customer is the archetype.
func TestDispatchablesFindTheOneReplyAsk(t *testing.T) {
	ss, err := loadFixture(t, "corpus", fixtureDay).Dispatchables()
	if err != nil {
		t.Fatalf("Dispatchables: %v", err)
	}

	s, ok := findSignal(ss, "dispatchables:e-003")
	if !ok {
		t.Fatalf("no dispatchable for Renee's yes-or-no; got: %+v", ss)
	}
	if s.Priority != model.P1 {
		t.Errorf("priority = %s, want P1 (reference customer, profile.md)", s.Priority)
	}
	if !strings.Contains(s.Detail, "yes or no") {
		t.Errorf("detail should quote the question that a reply would answer:\n%s", s.Detail)
	}
	if !cites(s, model.SourceEmail, "e-003") {
		t.Errorf("dispatchable must cite the message: %v", s.Citations)
	}
}

// A task that is itself one reply belongs in the same place as a thread that is.
func TestDispatchablesIncludeReplyShapedTasks(t *testing.T) {
	ss, _ := loadFixture(t, "corpus", fixtureDay).Dispatchables()

	s, ok := findSignal(ss, "dispatchables:task:t-renee-reply")
	if !ok {
		t.Fatalf("no dispatchable for the \"Reply to Renee\" task; got: %+v", ss)
	}
	if !cites(s, model.SourceTask, "t-renee-reply") {
		t.Errorf("task dispatchable must cite the task: %v", s.Citations)
	}

	// "Re-run the inference cost model" is work, not a reply.
	if _, found := findSignal(ss, "dispatchables:task:t-cost-model"); found {
		t.Error("a modelling task is not a one-reply item")
	}
}

// The hard negative this extractor exists to survive: a suppressed newsletter
// that ends in a perfectly well-formed question. Without suppression it would
// be indistinguishable from a real ask — the fixture's newsletter asks "Can you
// confirm you still want the Friday edition?" on purpose.
func TestDispatchablesSkipSuppressedMail(t *testing.T) {
	tb := loadFixture(t, "corpus", fixtureDay)

	supp, _ := tb.Suppressions()
	if !anyCites(supp, model.SourceEmail, "e-004") {
		t.Fatal("fixture broken: the newsletter should be suppressed")
	}

	ss, _ := tb.Dispatchables()
	if anyCites(ss, model.SourceEmail, "e-004") {
		t.Error("a suppressed newsletter reached the dispatchable list because it asked a question")
	}
	if anyCites(ss, model.SourceEmail, "e-013") {
		t.Error("an FYI reached the dispatchable list")
	}
	if anyCites(ss, model.SourceEmail, "e-010") {
		t.Error("a suppressed recruiter cold email reached the dispatchable list")
	}
}

// Shape, not urgency. A long ask is not a one-reply ask, and a stale one
// belongs to quiet-threads.
func TestDispatchablesRejectLongAndStaleAsks(t *testing.T) {
	long := msg(t, "m-1", "th-1", "2026-08-28T09:00:00-07:00", "dana", "review",
		"Can you confirm the plan? "+strings.Repeat("Background context. ", 60))
	stale := msg(t, "m-2", "th-2", "2026-08-10T09:00:00-07:00", "dana", "old ask", "Can you confirm the plan?")

	ss, err := toolboxOf(t, corpusOf([]model.Email{long, stale}), "2026-08-31").Dispatchables()
	if err != nil {
		t.Fatalf("Dispatchables: %v", err)
	}
	if len(ss) != 0 {
		t.Errorf("got %d dispatchables, want none: %+v", len(ss), ss)
	}
}

// An approval routes to the decisions section; a plain question does not.
func TestDispatchablesRouteApprovalsToDecisions(t *testing.T) {
	approve := msg(t, "m-1", "th-1", "2026-08-28T09:00:00-07:00", "ben", "signature",
		"Can you approve the amended side letter? Nothing else is blocking.")
	ss, _ := toolboxOf(t, corpusOf([]model.Email{approve}), "2026-08-31").Dispatchables()
	if len(ss) != 1 {
		t.Fatalf("got %d dispatchables, want 1: %+v", len(ss), ss)
	}
	if ss[0].SectionHint != model.SectionDecisions {
		t.Errorf("section = %s, want %s", ss[0].SectionHint, model.SectionDecisions)
	}
}
