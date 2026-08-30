package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chandrameenamohan/aubade/internal/extract"
	"github.com/chandrameenamohan/aubade/internal/model"
)

// The corpus these tests drive. It is deliberately tiny: what is under test
// here is the wiring — flags, output shape, exit behaviour — not the
// extractors, which are graded against their own fixtures in internal/extract.
const (
	corpusDir = "testdata/corpus"
	corpusDay = "2026-08-31"
)

func toolArgs2(rest ...string) []string {
	return append(rest, "--data", corpusDir, "--today", corpusDay)
}

// `aubade tool <name> --json` emits the signal array itself, not an envelope,
// so signals.json and tool output stay one dialect and `| jq '.[0].citations'`
// works without ceremony.
func TestToolJSONEmitsTheSignalContract(t *testing.T) {
	t.Setenv("AUBADE_OUTPUT", "human") // prove --json alone is enough

	out, err := run(NewAubadeCmd(), toolArgs2("tool", "commitments", "--json")...)
	if err != nil {
		t.Fatalf("tool commitments --json: %v\n%s", err, out)
	}

	var signals model.Signals
	if err := json.Unmarshal([]byte(out), &signals); err != nil {
		t.Fatalf("output is not a JSON signal array: %v\n%s", err, out)
	}
	if len(signals) == 0 {
		t.Fatal("expected the overdue cap-table promise")
	}
	if err := signals.Validate(); err != nil {
		t.Errorf("tool output does not satisfy the signal contract: %v", err)
	}
	if signals[0].Kind != model.KindCommitments {
		t.Errorf("kind = %q, want %q", signals[0].Kind, model.KindCommitments)
	}
}

// An AI caller gets JSON with no flag at all. Agent detection already
// established who is asking (SPEC §9), and learning-tests/04 confirmed this
// against the real claude CLI.
func TestToolDefaultsToJSONForAnAgentCaller(t *testing.T) {
	t.Setenv("AUBADE_OUTPUT", "json")

	out, err := run(NewAubadeCmd(), toolArgs2("tool", "conflicts")...)
	if err != nil {
		t.Fatalf("tool conflicts: %v\n%s", err, out)
	}
	if !json.Valid([]byte(strings.TrimSpace(out))) {
		t.Errorf("an agent caller should get JSON without --json:\n%s", out)
	}
}

// A human gets markdown, and it carries the two things that make a signal
// checkable: what it claims and what it cites.
func TestToolRendersMarkdownForHumans(t *testing.T) {
	t.Setenv("AUBADE_OUTPUT", "human")

	out, err := run(NewAubadeCmd(), toolArgs2("tool", "commitments")...)
	if err != nil {
		t.Fatalf("tool commitments: %v\n%s", err, out)
	}
	for _, want := range []string{"# commitments", "[P0]", "cites:", "email:e-002"} {
		if !strings.Contains(out, want) {
			t.Errorf("human output missing %q:\n%s", want, out)
		}
	}
	if json.Valid([]byte(strings.TrimSpace(out))) {
		t.Error("human output should not be JSON")
	}
}

// The two investigation tools return their own payloads, and both are shaped
// for an agent that will cite what it reads.
func TestToolThreadAndSearch(t *testing.T) {
	t.Setenv("AUBADE_OUTPUT", "human")

	out, err := run(NewAubadeCmd(), toolArgs2("tool", "thread", "t-capt", "--json")...)
	if err != nil {
		t.Fatalf("tool thread: %v\n%s", err, out)
	}
	var view extract.ThreadView
	if err := json.Unmarshal([]byte(out), &view); err != nil {
		t.Fatalf("thread output is not a ThreadView: %v\n%s", err, out)
	}
	if view.MessageCount != 2 || view.ThreadID != "t-capt" {
		t.Errorf("unexpected thread view: %+v", view)
	}

	out, err = run(NewAubadeCmd(), toolArgs2("tool", "search", "cap table", "--json")...)
	if err != nil {
		t.Fatalf("tool search: %v\n%s", err, out)
	}
	var res extract.SearchResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("search output is not a SearchResult: %v\n%s", err, out)
	}
	if res.Total == 0 {
		t.Error("search found nothing for a phrase in the corpus")
	}
}

// `aubade signals` writes the audit trail where the eval harness will look for
// it, in the shape ReadSignals expects.
func TestSignalsWritesTheAuditTrail(t *testing.T) {
	t.Setenv("AUBADE_OUTPUT", "human")
	outDir := t.TempDir()

	out, err := run(NewAubadeCmd(), "signals", "--data", corpusDir, "--today", corpusDay, "--out", outDir)
	if err != nil {
		t.Fatalf("signals: %v\n%s", err, out)
	}

	path := filepath.Join(outDir, extract.SignalsFile)
	signals, err := extract.ReadSignals(path)
	if err != nil {
		t.Fatalf("ReadSignals(%s): %v", path, err)
	}
	if len(signals) == 0 {
		t.Fatal("signals.json is empty")
	}
	if !strings.Contains(out, path) {
		t.Errorf("the run should say where it wrote:\n%s", out)
	}

	// Same corpus, same --today, byte-identical file.
	second := t.TempDir()
	if _, err := run(NewAubadeCmd(), "signals", "--data", corpusDir, "--today", corpusDay, "--out", second); err != nil {
		t.Fatalf("second run: %v", err)
	}
	a, _ := os.ReadFile(path)
	b, _ := os.ReadFile(filepath.Join(second, extract.SignalsFile))
	if string(a) != string(b) {
		t.Error("two runs over the same corpus and the same --today produced different files")
	}
}

// The profile reaches the toolbox through the CLI: Marcus is P0 because
// profile.md says so, and the newsletter is held back for the same reason.
func TestSignalsHonourTheProfile(t *testing.T) {
	t.Setenv("AUBADE_OUTPUT", "human")
	outDir := t.TempDir()

	if _, err := run(NewAubadeCmd(), "signals", "--data", corpusDir, "--today", corpusDay, "--out", outDir); err != nil {
		t.Fatalf("signals: %v", err)
	}
	signals, err := extract.ReadSignals(filepath.Join(outDir, extract.SignalsFile))
	if err != nil {
		t.Fatal(err)
	}

	var sawP0Commitment, sawSuppression bool
	for _, s := range signals {
		if s.Kind == model.KindCommitments && s.Priority == model.P0 {
			sawP0Commitment = true
		}
		if s.Kind == model.KindSuppressions {
			for _, c := range s.Citations {
				if c.Ref == "e-003" {
					sawSuppression = true
				}
			}
		}
	}
	if !sawP0Commitment {
		t.Error("the profile's P0 for Marcus did not reach the commitment signal")
	}
	if !sawSuppression {
		t.Error("the profile's newsletter rule did not reach the suppression signals")
	}
}

// An absent corpus is an error, not an empty digest. A directory with no data
// in it would otherwise produce a valid, cited, entirely empty answer — the
// most expensive kind of wrong.
func TestMissingCorpusIsALoudError(t *testing.T) {
	empty := t.TempDir()

	cases := []struct {
		dir  string
		want string
	}{
		{filepath.Join(empty, "nope"), "no corpus at"},
		{empty, "does not look like an aubade corpus"},
	}
	for _, tc := range cases {
		_, err := run(NewAubadeCmd(), "tool", "commitments", "--data", tc.dir, "--today", corpusDay)
		if err == nil {
			t.Fatalf("--data %s should fail", tc.dir)
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("error for %s = %q, want it to contain %q", tc.dir, err, tc.want)
		}
	}
}

// --today is an input to the answer, so a malformed one has to stop the run
// rather than fall back to the clock.
func TestBadTodayIsRejected(t *testing.T) {
	_, err := run(NewAubadeCmd(), "tool", "commitments", "--data", corpusDir, "--today", "31/08/2026")
	if err == nil || !strings.Contains(err.Error(), "invalid --today") {
		t.Errorf("expected a --today validation error, got %v", err)
	}
}
