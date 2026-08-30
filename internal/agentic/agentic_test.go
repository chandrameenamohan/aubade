package agentic

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chandrameenamohan/aubade/internal/runner"
	"github.com/chandrameenamohan/aubade/internal/runner/runnertest"
)

// What is graded here is the pipeline's promises rather than its prose: the
// honesty floor is always appended, a fabricated citation costs the model its
// whole page, the footer names who voted, and --customize reaches the compose
// stage and nothing else.

func compose(t *testing.T, in Input) *Result {
	t.Helper()
	res, err := Compose(context.Background(), in)
	if err != nil {
		t.Fatalf("Compose: %v", err)
	}
	return res
}

// A clean run: the model's page, then the honesty layer aubade owns, then the
// provenance.
func TestComposeAcceptsAPageBuiltFromTheFactBase(t *testing.T) {
	claude := composer(t, goodPage)
	res := compose(t, testInput(claude))

	if res.Mode != ModeAgentic || res.FellBack() {
		t.Fatalf("mode = %q, want a clean agentic run", res.Mode)
	}
	if !strings.Contains(res.Markdown, "Answer Marcus about the term sheet") {
		t.Error("the model's own page should be what the reader gets")
	}
	// Citations were checked as ids and are rendered as names.
	if strings.Contains(res.Markdown, "[email:e-001]") {
		t.Error("machine-dialect refs should be resolved into reader-facing spans")
	}
	if !strings.Contains(res.Markdown, "*[email: Marcus,") {
		t.Errorf("no resolved citation on the page:\n%s", res.Markdown)
	}
	if res.Run == nil || res.Run.ToolCalls != 4 {
		t.Errorf("Run = %+v, want the loop's own account of itself", res.Run)
	}
}

// The honesty floor is structural: it is appended from the fact base whatever
// the composer wrote, which is what makes "format is the user's, truthfulness is
// the product's" true rather than requested.
func TestHonestyFloorIsAppendedWhateverTheModelWrote(t *testing.T) {
	// A page that says nothing about uncertainty, and a fact base with an item
	// the runners could not agree on.
	a := voterFor("claude", `{"urgent":true,"why":"today"}`, `{"pick":"commitments:e-001","why":"the money"}`)
	b := voterFor("codex", `{"urgent":true,"why":"today"}`, `{"pick":"conflicts:evt-7","why":"the board"}`)
	claude := composer(t, goodPage)

	in := testInput(claude, a, b)
	res := compose(t, in)

	if !strings.Contains(res.Markdown, "## I'm not sure") {
		t.Fatalf("the honesty floor is missing from the page:\n%s", res.Markdown)
	}
	if !strings.Contains(res.Markdown, "disagree about what comes first") {
		t.Error("the runners' split must reach the reader")
	}
	if !strings.Contains(res.Markdown, "## Honesty") {
		t.Error("the honesty section must always be on the page")
	}
}

// The orchestrator is told not to write the honesty sections, because aubade
// appends them — a prompt rule with enforcement behind it.
func TestTheComposerIsToldTheHonestyLayerIsNotItsToWrite(t *testing.T) {
	claude := composer(t, goodPage)
	compose(t, testInput(claude))

	goals := claude.Goals()
	if len(goals) != 1 {
		t.Fatalf("orchestrated %d times, want once", len(goals))
	}
	prompt := goals[0].Prompt
	for _, want := range []string{"DO NOT WRITE", "I'm not sure", "honesty floor", "THE FACT BASE", "[email:e-0042]"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("the orchestration prompt is missing %q", want)
		}
	}
	if !strings.Contains(prompt, "commitments:e-001") {
		t.Error("the prompt must carry the fact base itself, not a description of it")
	}
}

// The loop's boundary travels with the goal: the toolbox binary, the prefix that
// gets allowlisted, and the directories it may read. Nothing else.
func TestTheLoopIsHandedTheToolboxAndNothingElse(t *testing.T) {
	claude := composer(t, goodPage)
	in := testInput(claude)
	res := compose(t, in)
	_ = res

	g := claude.Goals()[0]
	if g.ToolBin != in.ToolBin || g.ToolPrefix != "tool" {
		t.Errorf("goal = %+v, want the toolbox as the whole sandbox", g)
	}
	if spec := runner.AllowSpec(g.ToolBin, g.ToolPrefix); !strings.HasSuffix(spec, " tool:*)") {
		t.Errorf("allowlist = %q, want only the toolbox subcommand", spec)
	}
	if len(g.ReadDirs) == 0 || g.ReadDirs[0] != in.DataDir {
		t.Errorf("ReadDirs = %v, want the corpus", g.ReadDirs)
	}
}

// The bead's headline guarantee: one invented citation and the whole page is
// thrown away, loudly, for the deterministic one.
func TestAFabricatedCitationCostsTheModelItsWholePage(t *testing.T) {
	bad := composer(t, goodPage+"\n- **Legal cleared the SOC2 report.** [email:e-999]\n")
	var log strings.Builder

	in := testInput(bad)
	in.Log = &log
	res := compose(t, in)

	if res.Mode != ModeFallback || !res.FellBack() {
		t.Fatalf("mode = %q, want the fallback", res.Mode)
	}
	if len(res.Violations) != 1 || res.Violations[0].Ref != "email:e-999" {
		t.Errorf("violations = %v, want the fabricated ref named", res.Violations)
	}
	// Loud in all three places a reader could be looking.
	if !strings.Contains(log.String(), "e-999") {
		t.Errorf("stderr said nothing useful: %q", log.String())
	}
	if !strings.HasPrefix(res.Markdown, "> **This page was not composed by claude.**") {
		t.Errorf("the page must open by saying it is not the one that was asked for:\n%.200s", res.Markdown)
	}
	if !strings.Contains(res.Markdown, "e-999") {
		t.Error("the notice should name what was fabricated")
	}
	// And it is a real page, not a stub: the deterministic composer wrote it.
	for _, want := range []string{"# Daily Digest — ", "## Urgent To-Do Today", "## Honesty"} {
		if !strings.Contains(res.Markdown, want) {
			t.Errorf("the fallback page is missing %q", want)
		}
	}
	if strings.Contains(res.Markdown, "Legal cleared the SOC2 report") {
		t.Error("no part of a rejected page may survive into the digest")
	}
}

// A page that cites nothing at all is rejected on the same grounds: every line
// on it is unverifiable.
func TestAnUncitedPageIsRejectedToo(t *testing.T) {
	bare := composer(t, "# Daily Digest\n\nAll quiet this morning.\n")
	res := compose(t, testInput(bare))

	if !res.FellBack() {
		t.Fatal("an uncited page must not be published")
	}
	if len(res.Violations) != 1 || res.Violations[0].Kind != violationNone {
		t.Errorf("violations = %v, want the uncited page named as such", res.Violations)
	}
	if !strings.Contains(res.Markdown, "no citations at all") {
		t.Errorf("the notice should say what was wrong in its own terms:\n%.300s", res.Markdown)
	}
}

// SPEC §5: the footer names the runners that participated — and the ones that
// could not, because a roster of one reads differently depending on why.
func TestFooterNamesWhoComposedAndWhoVoted(t *testing.T) {
	live := &runnertest.Runner{RunnerName: "claude", Orchestrates: true, Page: goodPage, ToolCalls: 3,
		Answers: []string{`{"urgent":true,"why":"today"}`, `{"pick":"commitments:e-001","why":"money"}`}}
	dead := &runnertest.Runner{RunnerName: "codex", ProbeErr: errors.New("401 Unauthorized")}
	roster := runnertest.Registry(live, dead).Detect(context.Background())

	in := testInput(live)
	in.Roster = roster
	res := compose(t, in)

	for _, want := range []string{
		"claude orchestrated aubade's deterministic toolbox",
		"3 toolbox calls",
		"answered: claude",
		"codex unavailable",
		"honesty layer is appended by aubade",
		"Every factual line above carries its own citation",
	} {
		if !strings.Contains(res.Markdown, want) {
			t.Errorf("the footer is missing %q:\n%s", want, footerOf(res.Markdown))
		}
	}
}

func TestFooterSaysWhenConsensusWasTurnedOff(t *testing.T) {
	claude := composer(t, goodPage)
	in := testInput(claude)
	in.Consensus = false
	res := compose(t, in)

	if !strings.Contains(res.Markdown, "Consensus off") {
		t.Errorf("the footer should say the vote was skipped:\n%s", footerOf(res.Markdown))
	}
	if claude.Asks() != 0 {
		t.Errorf("asked %d questions with consensus off", claude.Asks())
	}
}

// --customize touches the compose stage and nothing else: the format changes,
// the fact base does not, and the honesty floor is appended regardless.
func TestCustomizeReshapesTheFormatAndNotTheTruth(t *testing.T) {
	claude := composer(t, goodPage)
	in := testInput(claude)
	in.Customize = "Write it as a single table with one row per item. No headings."
	in.CustomizePath = "prompt.md"

	res := compose(t, in)

	prompt := claude.Goals()[0].Prompt
	if !strings.Contains(prompt, customizeHeading) || !strings.Contains(prompt, "single table") {
		t.Error("the user's prompt should reach the composer")
	}
	if strings.Contains(prompt, "Urgent To-Do Today\n      Up to 6 bullets") {
		t.Error("the default section contract should be replaced, not stacked on top of the user's")
	}
	// The parts that are not the user's to shape are still there.
	for _, want := range []string{"THE FACT BASE", "DO NOT WRITE", "aubade checks every one of them"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("customization removed %q from the prompt", want)
		}
	}
	if !strings.Contains(res.Markdown, "## Honesty") || !strings.Contains(res.Markdown, "## I'm not sure") {
		t.Error("the honesty floor must survive customization")
	}
	if !strings.Contains(res.Markdown, "Format customized by prompt.md") {
		t.Errorf("the footer should say the page was customized:\n%s", footerOf(res.Markdown))
	}
	if !strings.Contains(res.Markdown, "honesty layer is not customizable") {
		t.Error("the footer should say what customization could not reach")
	}
}

// An unreadable or empty --customize file is an error, because the user asked
// for a different page and quietly giving them the standard one is the
// substitution this product exists not to make.
func TestLoadCustomize(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "prompt.md")
	if err := os.WriteFile(good, []byte("  one table, no headings  "), 0o644); err != nil {
		t.Fatal(err)
	}
	empty := filepath.Join(dir, "empty.md")
	if err := os.WriteFile(empty, []byte("   \n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if body, err := LoadCustomize(good); err != nil || body != "one table, no headings" {
		t.Errorf("LoadCustomize(good) = %q, %v", body, err)
	}
	if body, err := LoadCustomize(""); err != nil || body != "" {
		t.Errorf("LoadCustomize(\"\") = %q, %v — no flag is not an error", body, err)
	}
	if _, err := LoadCustomize(filepath.Join(dir, "nope.md")); err == nil ||
		!strings.Contains(err.Error(), "--customize") {
		t.Errorf("a missing prompt file should be a clear error, got %v", err)
	}
	if _, err := LoadCustomize(empty); err == nil || !strings.Contains(err.Error(), "nothing to ask for") {
		t.Errorf("an empty prompt file should be an error, got %v", err)
	}
}

// A runner that cannot compose at all is an error, not a silent fallback: the
// user asked for the agentic page, and the flag that gives them the other one is
// one word long.
func TestAFailedLoopIsAnErrorThatNamesTheWayOut(t *testing.T) {
	broken := &runnertest.Runner{RunnerName: "claude", Orchestrates: true, OrchestrateErr: runner.ErrToolBudget}

	_, err := Compose(context.Background(), testInput(broken))
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "--no-llm") {
		t.Errorf("error %q should name the mode that always works", err)
	}
	if !errors.Is(err, runner.ErrToolBudget) {
		t.Errorf("error %v should carry why the loop failed", err)
	}
}

func TestComposeRefusesAnInvalidFactBase(t *testing.T) {
	in := testInput(composer(t, goodPage))
	in.Signals[0].Citations = nil // a claim with no receipt

	if _, err := Compose(context.Background(), in); err == nil ||
		!strings.Contains(err.Error(), "invalid fact base") {
		t.Fatalf("err = %v, want a refusal to compose from an uncitable signal set", err)
	}
}

// footerOf is the tail of a page, for a readable failure message.
func footerOf(page string) string {
	if i := strings.LastIndex(page, "\n---\n"); i >= 0 {
		return page[i:]
	}
	return page
}
