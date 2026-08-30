package eval

import (
	"strings"
	"testing"
	"time"

	"github.com/chandrameenamohan/aubade/internal/datagen"
	"github.com/chandrameenamohan/aubade/internal/model"
)

// groundingCorpus is one email, so the label the page must carry is known
// exactly: [email: Marcus, Aug 27 16:42].
func groundingCorpus(t *testing.T) (*model.Corpus, model.Signals) {
	t.Helper()
	loc := model.Location()
	e := model.Email{
		ID: "m-1", ThreadID: "t-1", TS: time.Date(2026, 8, 27, 16, 42, 0, 0, loc),
		From:    model.Person{Name: "Marcus Webb", Email: "marcus@inflectionpoint.vc"},
		To:      []model.Person{{Name: "Avery Chen", Email: "avery@tessera.io"}},
		Subject: "cap table",
		Body:    "still waiting",
	}
	if err := e.Validate(); err != nil {
		t.Fatalf("fixture email is invalid: %v", err)
	}
	corpus := &model.Corpus{Source: "test", Emails: []model.Email{e}}
	return corpus, model.Signals{signal(model.KindCommitments, model.SectionUrgentToday, cite(model.SourceEmail, "m-1"))}
}

// The keystone claim, as a check: a citation on the page that is not in the
// page's own fact base is a fabricated receipt.
func TestGroundingCatchesAFabricatedCitation(t *testing.T) {
	corpus, signals := groundingCorpus(t)

	real := "*[email: Marcus, Aug 27 16:42]*"
	g := CheckGrounding(&Artifacts{Digest: "the cap table is late " + real, Signals: signals}, corpus, model.Location())
	if !g.OK() || g.Cited != 1 {
		t.Fatalf("a real citation must ground: cited=%d ungrounded=%v", g.Cited, g.Ungrounded)
	}

	g = CheckGrounding(&Artifacts{
		Digest:  "the cap table is late " + real + " and so is this *[email: Nobody, Jan 1 00:00]*",
		Signals: signals,
	}, corpus, model.Location())
	if g.OK() {
		t.Fatal("an invented citation must not ground")
	}
	if len(g.Ungrounded) != 1 || !strings.Contains(g.Ungrounded[0], "Nobody") {
		t.Errorf("the fabricated ref must be named, got %v", g.Ungrounded)
	}
}

// Ordinary prose and markdown links are not receipts. A checker that cries wolf
// on "[see below](#x)" gets switched off, and then it catches nothing at all.
func TestGroundingIgnoresOrdinaryBrackets(t *testing.T) {
	corpus, signals := groundingCorpus(t)
	digest := "a [link](https://example.com) and a [note to self] and *[email: Marcus, Aug 27 16:42]*"

	g := CheckGrounding(&Artifacts{Digest: digest, Signals: signals}, corpus, model.Location())
	if !g.OK() {
		t.Errorf("prose must not read as citations: %v", g.Ungrounded)
	}
	if g.Cited != 1 {
		t.Errorf("counted %d citations, want 1", g.Cited)
	}
}

// A page with no citations at all is not a page that happened to cite nothing.
func TestGroundingRejectsAnUncitedPage(t *testing.T) {
	corpus, signals := groundingCorpus(t)
	g := CheckGrounding(&Artifacts{Digest: "trust me", Signals: signals}, corpus, model.Location())
	if g.OK() {
		t.Error("an uncited page must not pass the grounding check")
	}
}

// The toolbox call count comes off the transcript and is reported, never
// scored: which tools the agent chose is not the harness's business (#6).
func TestGroundingCountsToolCallsFromTheTranscript(t *testing.T) {
	corpus, signals := groundingCorpus(t)
	a := &Artifacts{
		Digest:     "*[email: Marcus, Aug 27 16:42]*",
		Signals:    signals,
		Transcript: `{"cmd":"/x/aubade tool commitments"}` + "\n" + `{"cmd":"/x/aubade tool conflicts"}` + "\n",
	}
	if g := CheckGrounding(a, corpus, model.Location()); g.ToolCalls != 2 {
		t.Errorf("counted %d toolbox calls, want 2", g.ToolCalls)
	}

	a.Transcript = ""
	if g := CheckGrounding(a, corpus, model.Location()); g.ToolCalls != -1 {
		t.Errorf("no transcript must report -1, not 0 — they are different facts")
	}
}

// pass^N and pass@N are two numbers and they mean different things. A task that
// passes two trials of three is a reliability problem; a task that passes none
// is a capability problem, and one percentage cannot say which.
func TestCapabilityReportsBothRates(t *testing.T) {
	flaky := positiveTrap()
	flaky.ID = "flaky"
	solid := positiveTrap()
	solid.ID = "solid"
	broken := positiveTrap()
	broken.ID = "broken"
	traps := datagen.Traps{flaky, solid, broken}

	suite := &Capability{Traps: traps}
	for i, passes := range [][]string{
		{"flaky", "solid"},
		{"solid"},
		{"flaky", "solid"},
	} {
		res := &Result{}
		for _, trap := range traps {
			res.Traps = append(res.Traps, TrapResult{ID: trap.ID, Passed: contains(passes, trap.ID)})
		}
		suite.Trials = append(suite.Trials, &Trial{N: i + 1, Result: res})
	}

	want := map[string]struct{ all, any bool }{
		"flaky":  {all: false, any: true},
		"solid":  {all: true, any: true},
		"broken": {all: false, any: false},
	}
	for _, a := range suite.Aggregates() {
		w := want[a.Trap.ID]
		if a.PassAll != w.all || a.PassAny != w.any {
			t.Errorf("%s: pass^N=%v pass@N=%v, want %v/%v", a.Trap.ID, a.PassAll, a.PassAny, w.all, w.any)
		}
	}

	passAll, passAny, tasks := suite.Rates()
	if passAll != 1 || passAny != 2 || tasks != 3 {
		t.Errorf("rates = %d/%d of %d, want 1/2 of 3", passAll, passAny, tasks)
	}
}

// A capability suite that could not run says so. A skip that reads like a pass
// is how an unverified claim becomes a cited one.
func TestCapabilitySkipIsLoudInTheCard(t *testing.T) {
	card := &Card{
		DataDir:    "data",
		Today:      "2026-08-30",
		Regression: Grade(datagen.Traps{negativeTrap()}, page("a page")),
		Capability: &Capability{Skipped: true, SkipReason: "the claude CLI is not on PATH"},
	}
	md := card.Markdown()
	if !strings.Contains(md, "SKIPPED") || !strings.Contains(md, "not a pass") {
		t.Errorf("the card does not report the skip loudly:\n%s", md)
	}
}

// The card's regression section has to be readable as pass or fail at a glance,
// and a failure has to name what to change.
func TestCardNamesWhatToFix(t *testing.T) {
	card := &Card{
		DataDir:    "data",
		Today:      "2026-08-30",
		Regression: Grade(datagen.Traps{positiveTrap()}, page("a page about nothing")),
	}
	md := card.Markdown()
	for _, want := range []string{"RED", "What to fix", "commitments missed it"} {
		if !strings.Contains(md, want) {
			t.Errorf("the card is missing %q:\n%s", want, md)
		}
	}
}
