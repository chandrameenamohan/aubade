package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/chandrameenamohan/aubade/internal/agentic"
	"github.com/chandrameenamohan/aubade/internal/digest"
	"github.com/chandrameenamohan/aubade/internal/extract"
	"github.com/chandrameenamohan/aubade/internal/runner"
	"github.com/chandrameenamohan/aubade/internal/runner/runnertest"
)

// Agentic mode is wired to fake runners here, and that is not a convenience:
// `go test ./...` must never call a real model. Cost, auth and non-determinism
// are three separate disqualifications from a gate (VERIFICATION.md §2), and the
// live end of this is `make check-agentic`, which is deliberately not in the
// gate.

// runStreams executes a command tree with stdout and stderr kept apart. Agentic
// mode writes its loud notices to stderr precisely so a JSON caller reading
// stdout still gets a parseable envelope, and a test that merged the two could
// not tell whether that was true.
func runStreams(root *cobra.Command, args ...string) (stdout, stderr string, err error) {
	var out, errOut bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetArgs(args)
	err = root.Execute()
	return out.String(), errOut.String(), err
}

// withRunners points the CLI at a scripted registry for one test.
func withRunners(t *testing.T, runners ...*runnertest.Runner) {
	t.Helper()
	previous := runnerRegistry
	runnerRegistry = runnertest.Registry(runners...)
	t.Cleanup(func() { runnerRegistry = previous })
}

// agenticPage is a page whose citations all resolve against the shared test
// corpus, so a run over it is accepted rather than rejected.
const agenticPage = `# Daily Digest — Monday, August 31, 2026

## If there is one thing you must do right now:
**Answer the thread that has gone quiet.** [email:%s]

## Urgent To-Do Today
- **Nothing else needs an answer before midnight.** [email:%s]
`

// pageCiting builds a page whose citations come from the corpus's own signals,
// so the fixture cannot drift away from the extractors that produced it.
func pageCiting(t *testing.T) string {
	t.Helper()
	outDir := t.TempDir()
	if _, err := run(NewAubadeCmd(), "signals", "--data", corpusDir, "--today", corpusDay, "--out", outDir); err != nil {
		t.Fatalf("signals: %v", err)
	}
	signals, err := extract.ReadSignals(filepath.Join(outDir, extract.SignalsFile))
	if err != nil {
		t.Fatalf("read signals: %v", err)
	}
	for _, s := range signals {
		for _, c := range s.Citations {
			if c.Source == "email" {
				return strings.ReplaceAll(agenticPage, "%s", c.Ref)
			}
		}
	}
	t.Fatal("the test corpus produced no email citation to build a page from")
	return ""
}

// The headline wiring: a live runner composes, aubade checks it, and three files
// land — the page, the fact base it was checked against, and the transcript.
func TestAgenticDigestWritesThePageItsFactBaseAndTheTranscript(t *testing.T) {
	t.Setenv("AUBADE_OUTPUT", "human")
	claude := &runnertest.Runner{RunnerName: "claude", Orchestrates: true, Page: pageCiting(t), ToolCalls: 2}
	withRunners(t, claude)

	outDir, args := digestArgs(t, "digest")
	out, err := run(NewAubadeCmd(), args...)
	if err != nil {
		t.Fatalf("digest: %v\n%s", err, out)
	}

	page, err := os.ReadFile(filepath.Join(outDir, digest.DigestFile))
	if err != nil {
		t.Fatalf("no digest written: %v", err)
	}
	md := string(page)
	for _, want := range []string{"# Daily Digest — ", "## Honesty", "agentic mode", "claude orchestrated"} {
		if !strings.Contains(md, want) {
			t.Errorf("the written page is missing %q", want)
		}
	}
	if !strings.Contains(out, md) {
		t.Error("the human run should print the page it wrote")
	}

	if _, err := extract.ReadSignals(filepath.Join(outDir, extract.SignalsFile)); err != nil {
		t.Errorf("no signals.json beside the digest: %v", err)
	}
	transcript, err := os.ReadFile(filepath.Join(outDir, TranscriptFile))
	if err != nil || len(transcript) == 0 {
		t.Errorf("no transcript for the eval harness: %v", err)
	}

	// The fact base exists before a model call is made, so a run that dies late
	// still leaves the grader something to grade.
	goals := claude.Goals()
	if len(goals) != 1 {
		t.Fatalf("orchestrated %d times, want once", len(goals))
	}
	if !strings.Contains(goals[0].Prompt, "--today "+corpusDay) {
		t.Error("the prompt should hand the runner the anchored toolbox command")
	}
	if !filepath.IsAbs(goals[0].ToolBin) {
		t.Errorf("ToolBin = %q, want the absolute path an allowlist can name", goals[0].ToolBin)
	}
}

// An AI caller gets the run as JSON, with the counts and the paths it needs.
func TestAgenticDigestSpeaksJSONToAnAgentCaller(t *testing.T) {
	t.Setenv("AUBADE_OUTPUT", "json")
	withRunners(t, &runnertest.Runner{RunnerName: "claude", Orchestrates: true, Page: pageCiting(t), ToolCalls: 3})

	_, args := digestArgs(t, "digest")
	out, err := run(NewAubadeCmd(), args...)
	if err != nil {
		t.Fatalf("digest: %v\n%s", err, out)
	}

	var payload struct {
		OK         bool   `json:"ok"`
		Mode       string `json:"mode"`
		Transcript string `json:"transcript"`
		FellBack   bool   `json:"fell_back"`
		Counts     struct {
			ToolCalls int `json:"tool_calls"`
		} `json:"counts"`
		Digest string `json:"digest"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("agent-mode output is not JSON: %v\n%s", err, out)
	}
	if !payload.OK || payload.Mode != agentic.ModeAgentic || payload.FellBack {
		t.Errorf("unexpected envelope: %+v", payload)
	}
	if payload.Counts.ToolCalls != 3 || payload.Transcript == "" {
		t.Errorf("the envelope should report the loop: %+v", payload)
	}
	if !strings.HasPrefix(payload.Digest, "# Daily Digest — ") {
		t.Errorf("the envelope should carry the page itself, got %.60q", payload.Digest)
	}
}

// A fabricated citation reaches the CLI as a page in fallback mode, written and
// reported as such rather than as a success.
func TestAgenticDigestFallsBackLoudlyOnAFabricatedCitation(t *testing.T) {
	t.Setenv("AUBADE_OUTPUT", "json")
	withRunners(t, &runnertest.Runner{RunnerName: "claude", Orchestrates: true,
		Page: "# Daily Digest\n\n- **Legal cleared the report.** [email:e-does-not-exist]\n"})

	outDir, args := digestArgs(t, "digest")
	out, errOut, err := runStreams(NewAubadeCmd(), args...)
	if err != nil {
		t.Fatalf("a rejected page is still a digest, not an error: %v", err)
	}
	if !strings.Contains(errOut, "e-does-not-exist") || !strings.Contains(errOut, "thrown away") {
		t.Errorf("stderr should say loudly what happened, got %q", errOut)
	}

	var payload struct {
		Mode       string   `json:"mode"`
		FellBack   bool     `json:"fell_back"`
		Violations []string `json:"violations"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if payload.Mode != agentic.ModeFallback || !payload.FellBack {
		t.Errorf("mode = %q, fell_back = %v, want the substitution declared", payload.Mode, payload.FellBack)
	}
	if len(payload.Violations) != 1 || !strings.Contains(payload.Violations[0], "e-does-not-exist") {
		t.Errorf("violations = %v, want the fabricated ref named", payload.Violations)
	}

	page, err := os.ReadFile(filepath.Join(outDir, digest.DigestFile))
	if err != nil {
		t.Fatalf("no digest written: %v", err)
	}
	if !strings.HasPrefix(string(page), "> **This page was not composed by claude.**") {
		t.Errorf("the page must open by saying so:\n%.200s", page)
	}
}

// Degradation. Each of these is a state the development machine has actually
// been in, and each one has to end in a sentence that says what still works.
func TestAgenticDigestDegradesWithAMessageThatNamesTheWayOut(t *testing.T) {
	cases := []struct {
		name    string
		runners []*runnertest.Runner
		args    []string
		want    []string
	}{{
		name:    "no runner installed",
		runners: []*runnertest.Runner{{RunnerName: "claude", Orchestrates: true, Missing: true}},
		want:    []string{"not installed", "--no-llm"},
	}, {
		// learning-tests/03: installed, says it is logged in, and 401s.
		name:    "installed but not answering",
		runners: []*runnertest.Runner{{RunnerName: "claude", Orchestrates: true, ProbeErr: errors.New("401 Unauthorized")}},
		want:    []string{"did not answer", "401", "--no-llm"},
	}, {
		name:    "an ask-only runner cannot compose",
		runners: []*runnertest.Runner{{RunnerName: "claude", Orchestrates: true}, {RunnerName: "codex"}},
		args:    []string{"--runner", "codex"},
		want:    []string{"cannot drive the toolbox loop", "--runner=claude", "--no-llm"},
	}, {
		name:    "an unknown runner gets the menu",
		runners: []*runnertest.Runner{{RunnerName: "claude", Orchestrates: true}},
		args:    []string{"--runner", "gemni"},
		want:    []string{`unknown runner "gemni"`, "claude"},
	}, {
		name:    "the loop failed",
		runners: []*runnertest.Runner{{RunnerName: "claude", Orchestrates: true, OrchestrateErr: runner.ErrToolBudget}},
		want:    []string{"could not compose", "budget", "--no-llm"},
	}}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withRunners(t, tc.runners...)
			_, args := digestArgs(t, append([]string{"digest"}, tc.args...)...)

			_, err := run(NewAubadeCmd(), args...)
			if err == nil {
				t.Fatal("expected an error rather than a quiet substitution")
			}
			for _, want := range tc.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not mention %q", err, want)
				}
			}
		})
	}
}

// --consensus takes two words, and anything else is refused rather than guessed:
// reading an unrecognised value as "off" would turn quality off by typo.
func TestConsensusFlagVocabulary(t *testing.T) {
	claude := &runnertest.Runner{RunnerName: "claude", Orchestrates: true, Page: pageCiting(t)}
	withRunners(t, claude)

	_, args := digestArgs(t, "digest", "--consensus", "sometimes")
	_, err := run(NewAubadeCmd(), args...)
	if err == nil || !strings.Contains(err.Error(), "invalid --consensus") {
		t.Fatalf("err = %v, want a refusal naming the flag", err)
	}
	if len(claude.Goals()) != 0 {
		t.Error("a bad flag must fail before anything is paid for")
	}

	// And --consensus=off runs, without asking anybody anything.
	_, offArgs := digestArgs(t, "digest", "--consensus", "off")
	if _, err := run(NewAubadeCmd(), offArgs...); err != nil {
		t.Fatalf("--consensus=off: %v", err)
	}
	if claude.Asks() != 0 {
		t.Errorf("asked %d consensus questions with the vote turned off", claude.Asks())
	}
}

// --customize is read before a model call is made: a prompt file that is not
// there should cost nothing to discover.
func TestCustomizeIsValidatedBeforeAnythingIsPaidFor(t *testing.T) {
	claude := &runnertest.Runner{RunnerName: "claude", Orchestrates: true, Page: pageCiting(t)}
	withRunners(t, claude)

	_, args := digestArgs(t, "digest", "--customize", filepath.Join(t.TempDir(), "nope.md"))
	_, err := run(NewAubadeCmd(), args...)
	if err == nil || !strings.Contains(err.Error(), "--customize") {
		t.Fatalf("err = %v, want a clear error about the prompt file", err)
	}
	if len(claude.Goals()) != 0 {
		t.Error("the runner was called before the prompt file was even read")
	}
}

// SPEC §6: --customize reshapes the compose stage, and the honesty floor is not
// part of what it can reshape.
func TestCustomizeReachesTheComposerAndNotTheHonestyLayer(t *testing.T) {
	t.Setenv("AUBADE_OUTPUT", "human")
	claude := &runnertest.Runner{RunnerName: "claude", Orchestrates: true, Page: pageCiting(t)}
	withRunners(t, claude)

	promptPath := filepath.Join(t.TempDir(), "prompt.md")
	if err := os.WriteFile(promptPath, []byte("One table. No headings. Nothing about uncertainty."), 0o644); err != nil {
		t.Fatal(err)
	}

	outDir, args := digestArgs(t, "digest", "--customize", promptPath)
	if _, err := run(NewAubadeCmd(), args...); err != nil {
		t.Fatalf("digest --customize: %v", err)
	}

	if !strings.Contains(claude.Goals()[0].Prompt, "One table. No headings.") {
		t.Error("the user's prompt should reach the composer")
	}
	page, err := os.ReadFile(filepath.Join(outDir, digest.DigestFile))
	if err != nil {
		t.Fatal(err)
	}
	md := string(page)
	if !strings.Contains(md, "## Honesty") {
		t.Error("the honesty floor must survive a prompt that asked for it to go away")
	}
	if !strings.Contains(md, "Format customized by "+promptPath) {
		t.Error("the footer should say the page was customized, and by what")
	}
}
