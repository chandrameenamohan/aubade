package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chandrameenamohan/aubade/internal/datagen"
	"github.com/chandrameenamohan/aubade/internal/eval"
	"github.com/chandrameenamohan/aubade/internal/extract"
	"github.com/chandrameenamohan/aubade/internal/model"
)

// What is under test here is the harness's wiring: what it reads, what it
// writes, and — the part a gate depends on — what it exits with. The graders
// themselves are tested in internal/eval against hand-built fact bases, and the
// whole pipeline is proved solvable there by the reference digest.

// evalCorpus writes a pinned corpus and the answer key beside it, then runs the
// deterministic digest over it. What comes back is a data directory and an out
// directory in the exact state `aubade-lab eval` expects to find them.
func evalCorpus(t *testing.T) (data, out string) {
	t.Helper()
	data, out = t.TempDir(), t.TempDir()

	if _, err := run(NewLabCmd(), "generate", "--seed", "42", "--today", corpusDay, "--out", data); err != nil {
		t.Fatalf("generate: %v", err)
	}
	if _, err := run(NewAubadeCmd(), "digest", "--no-llm", "--data", data, "--today", corpusDay, "--out", out); err != nil {
		t.Fatalf("digest --no-llm: %v", err)
	}
	return data, out
}

// The headline behaviour: a green regression run writes the scorecard, exits
// zero, and says so in both sections of the card.
func TestEvalGradesTheRunAndWritesTheScorecard(t *testing.T) {
	t.Setenv("AUBADE_OUTPUT", "human")
	data, out := evalCorpus(t)

	stdout, err := run(NewLabCmd(), "eval", "--data", data, "--today", corpusDay, "--out", out)
	if err != nil {
		t.Fatalf("eval: %v\n%s", err, stdout)
	}

	card, err := os.ReadFile(filepath.Join(out, eval.ScorecardFile))
	if err != nil {
		t.Fatalf("no scorecard written: %v", err)
	}
	md := string(card)
	for _, want := range []string{
		"## Regression suite",
		"## Capability suite",
		"## Citation grounding",
		"GREEN",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("the scorecard is missing %q", want)
		}
	}
	if strings.Contains(md, "FAIL") {
		t.Errorf("the pinned corpus must grade clean:\n%s", md)
	}
	if !strings.Contains(stdout, "wrote ") {
		t.Error("the run should say where it wrote the card")
	}
}

// The gate's whole contract: a missed trap is a non-zero exit, and the message
// names the task and the extractor rather than a number.
func TestEvalExitsNonZeroOnAMiss(t *testing.T) {
	data, out := evalCorpus(t)

	// Blind the page. The fact base still has every signal in it, so this is
	// precisely the "extracted and then lost in the render" failure — the one a
	// signal-only grader would miss.
	page := filepath.Join(out, "digest.md")
	if err := os.WriteFile(page, []byte("# Daily Digest — nothing to report.\n"), 0o644); err != nil {
		t.Fatalf("cannot blind the page: %v", err)
	}

	stdout, err := run(NewLabCmd(), "eval", "--data", data, "--today", corpusDay, "--out", out)
	if err == nil {
		t.Fatalf("a page with no findings on it must fail the regression suite:\n%s", stdout)
	}
	for _, want := range []string{"regression suite RED", "cap table"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the failure does not name %q: %v", want, err)
		}
	}
}

// A negative trap that surfaces is a failure too. Suppression is half the
// product and an eval that only measures recall would never notice it break.
func TestEvalFailsWhenASuppressedItemSurfaces(t *testing.T) {
	data, out := evalCorpus(t)

	traps, err := eval.LoadTraps(data)
	if err != nil {
		t.Fatalf("load traps: %v", err)
	}
	trap, ok := traps.ByID("negative-newsletter-stratechery")
	if !ok {
		t.Fatal("the catalog no longer contains negative-newsletter-stratechery")
	}

	// Promote the suppressed newsletter into a first-class finding, which is
	// exactly what a broken suppression layer would do.
	signalsPath := filepath.Join(out, extract.SignalsFile)
	signals, err := extract.ReadSignals(signalsPath)
	if err != nil {
		t.Fatalf("read signals: %v", err)
	}
	signals = append(signals, model.Signal{
		ID:          "quiet-threads:leaked-newsletter",
		Kind:        model.KindQuietThreads,
		Priority:    model.P2,
		Title:       "Stratechery has not heard back",
		Detail:      "a false positive",
		Citations:   trap.PlantedRefs,
		SectionHint: model.SectionUrgentToday,
		Confidence:  model.Certain,
	})
	if err := extract.WriteSignals(signalsPath, signals); err != nil {
		t.Fatalf("write signals: %v", err)
	}

	_, err = run(NewLabCmd(), "eval", "--data", data, "--today", corpusDay, "--out", out)
	if err == nil {
		t.Fatal("a surfaced negative trap must fail the suite")
	}
	if !strings.Contains(err.Error(), "negative-newsletter-stratechery") {
		t.Errorf("the failure does not name the task: %v", err)
	}
}

// Sabotage is a check on the exam rather than on the commit, but its exit code
// still has to work: disabling an extractor no graded task depends on must
// alarm, and an alarm is a non-zero exit with a message that says why.
func TestEvalSabotageAlarmsAndExitsNonZero(t *testing.T) {
	data, out := evalCorpus(t)

	// The whole key: every extractor is depended on, so nothing alarms.
	if _, err := run(NewLabCmd(), "eval", "--data", data, "--today", corpusDay, "--out", out, "--sabotage", "commitments"); err != nil {
		t.Fatalf("disabling commitments should drop the score, not alarm: %v", err)
	}

	// A key with one task on it, hung on a different extractor: the score
	// cannot fall, so the alarm has to fire.
	keyPath := filepath.Join(data, datagen.TrapsFile)
	traps, err := eval.LoadTraps(data)
	if err != nil {
		t.Fatalf("load traps: %v", err)
	}
	one, ok := traps.ByID("commitment-cap-table-slip")
	if !ok {
		t.Fatal("the catalog no longer contains commitment-cap-table-slip")
	}
	raw, err := json.Marshal(datagen.Traps{one})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if err := os.WriteFile(keyPath, raw, 0o644); err != nil {
		t.Fatalf("write key: %v", err)
	}

	_, err = run(NewLabCmd(), "eval", "--data", data, "--today", corpusDay, "--out", out, "--sabotage", "staleness")
	if err == nil {
		t.Fatal("a sabotage that does not move the score must alarm")
	}
	if !strings.Contains(err.Error(), "ALARM") {
		t.Errorf("the alarm must say so: %v", err)
	}
}

// An unknown extractor is a caller error with the menu in it, not a stack
// trace three layers down.
func TestEvalRejectsAnUnknownSabotageTarget(t *testing.T) {
	data, out := evalCorpus(t)

	_, err := run(NewLabCmd(), "eval", "--data", data, "--today", corpusDay, "--out", out, "--sabotage", "vibes")
	if err == nil {
		t.Fatal("an unknown extractor must be refused")
	}
	if !strings.Contains(err.Error(), "commitments") {
		t.Errorf("the refusal should list the alternatives: %v", err)
	}
}

// Nothing to grade is an error that says what to run, not an empty pass.
func TestEvalWithNoRunToGradeSaysWhatToDo(t *testing.T) {
	data, _ := evalCorpus(t)

	_, err := run(NewLabCmd(), "eval", "--data", data, "--today", corpusDay, "--out", t.TempDir())
	if err == nil {
		t.Fatal("an empty out directory must not grade as a pass")
	}
	if !strings.Contains(err.Error(), "aubade digest --no-llm") {
		t.Errorf("the error should name the command that produces the artefacts: %v", err)
	}
}

// The JSON envelope is the agent-facing surface, and the harness is a thing
// agents run. The shape is asserted because something binds to it.
func TestEvalJSONReportsTheSuitesSeparately(t *testing.T) {
	data, out := evalCorpus(t)

	stdout, err := run(NewLabCmd(), "eval", "--data", data, "--today", corpusDay, "--out", out, "--json")
	if err != nil {
		t.Fatalf("eval --json: %v\n%s", err, stdout)
	}

	var payload struct {
		OK         bool   `json:"ok"`
		Scorecard  string `json:"scorecard"`
		Regression struct {
			Passed int      `json:"passed"`
			Total  int      `json:"total"`
			Mode   string   `json:"mode"`
			Misses []string `json:"misses"`
		} `json:"regression"`
		Grounding struct {
			Citations  int      `json:"citations"`
			Ungrounded []string `json:"ungrounded"`
		} `json:"grounding"`
	}
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, stdout)
	}
	if !payload.OK || payload.Regression.Passed != payload.Regression.Total {
		t.Errorf("expected a clean run, got %d/%d", payload.Regression.Passed, payload.Regression.Total)
	}
	if payload.Regression.Mode != "no-llm" {
		t.Errorf("mode = %q, want no-llm — the card must know which composer it graded", payload.Regression.Mode)
	}
	if payload.Regression.Total < 16 {
		t.Errorf("graded %d tasks; the catalog carries at least 16", payload.Regression.Total)
	}
	if payload.Grounding.Citations == 0 || len(payload.Grounding.Ungrounded) != 0 {
		t.Errorf("the deterministic page must be fully grounded: %d cited, %v ungrounded",
			payload.Grounding.Citations, payload.Grounding.Ungrounded)
	}
}

// The capability suite needs a model runner. On a machine without one it must
// skip in a way nobody can mistake for a pass, and it must not touch the exit
// code either way.
func TestEvalCapabilitySkipsLoudlyWithoutARunner(t *testing.T) {
	if eval.ClaudePresent() {
		t.Skip("claude is installed here; the loud-skip path is what this test is about")
	}
	t.Setenv("AUBADE_OUTPUT", "human")
	data, out := evalCorpus(t)

	stdout, err := run(NewLabCmd(), "eval", "--data", data, "--today", corpusDay, "--out", out, "--capability")
	if err != nil {
		t.Fatalf("a skipped capability suite must not fail the run: %v", err)
	}
	for _, want := range []string{"SKIPPED", "This is a skip, not a pass"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("the skip is not loud enough; missing %q", want)
		}
	}
}

// The adversarial pass adds evidence, never a different bar: it says how each
// negative task stayed out.
func TestEvalAdversarialReportsHowNegativesStayedOut(t *testing.T) {
	t.Setenv("AUBADE_OUTPUT", "human")
	data, out := evalCorpus(t)

	stdout, err := run(NewLabCmd(), "eval", "--data", data, "--today", corpusDay, "--out", out, "--adversarial")
	if err != nil {
		t.Fatalf("eval --adversarial: %v\n%s", err, stdout)
	}
	for _, want := range []string{"Adversarial pass", "Held back by", "negative-newsletter-stratechery"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("the adversarial section is missing %q", want)
		}
	}
}

// The transcript filename is a contract between the digest that writes it and
// the harness that reads it. Two constants, one meaning.
func TestTranscriptFileNamesAgree(t *testing.T) {
	if TranscriptFile != eval.TranscriptFile {
		t.Errorf("the digest writes %q and the harness reads %q", TranscriptFile, eval.TranscriptFile)
	}
}
