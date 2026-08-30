// Package agentic composes the digest the way SPEC §5 asks for it: a model
// runner drives aubade's deterministic toolbox and writes the page, and aubade
// keeps the parts of the page that are not up for negotiation.
//
// The pipeline is four steps and each one is a different kind of thing, which is
// why they are separate files:
//
//	decide.go    two bounded decisions, majority-voted across every live runner
//	prompt.go    the orchestration prompt — the fact base, the toolbox, the rules
//	(runner)     the loop itself, bounded by wall clock, spend, and tool calls
//	validate.go  every citation on the composed page, checked against the facts
//
// The load-bearing asymmetry: the model decides what to chase, what matters and
// how the page reads, and it decides none of what is true. Facts arrive already
// cited from the toolbox, citations are checked afterwards against that same
// set, and the honesty layer is appended by this package whatever the model or a
// `--customize` prompt had to say about it. Format is the user's; truthfulness
// is the product's.
package agentic

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/chandrameenamohan/aubade/internal/digest"
	"github.com/chandrameenamohan/aubade/internal/model"
	"github.com/chandrameenamohan/aubade/internal/runner"
)

// The two modes this package can finish in. They are separate names because a
// reader must be able to tell, from the page alone, whether a model composed it.
const (
	// ModeAgentic is a page the runner composed and every citation checked out.
	ModeAgentic = "agentic"

	// ModeFallback is a page the deterministic composer wrote after the
	// runner's page was rejected. The digest says so at the top, in the footer,
	// and on stderr — three places, because a substitution nobody notices is the
	// failure this product is written against.
	ModeFallback = "agentic-fallback"
)

// Input is everything a run needs. It is passed whole rather than assembled
// inside, so a test can drive the entire pipeline with fake runners and no
// filesystem beyond a corpus.
type Input struct {
	// Corpus is the normalized source data; Signals is the toolbox's output over
	// it, exactly as written to signals.json.
	Corpus  *model.Corpus
	Signals model.Signals

	// Now is the anchor instant, Loc the zone, Owner whose morning this is.
	Now   time.Time
	Loc   *time.Location
	Owner model.Person

	// Day is the anchor date in prose ("Sunday, August 30, 2026") and Today is
	// the same date as the toolbox takes it ("2026-08-30").
	Day   string
	Today string

	// ToolBin is the absolute path to the aubade binary the loop may call,
	// DataDir the corpus it reads, WorkDir a scratch directory to run in.
	ToolBin string
	DataDir string
	WorkDir string

	// Orchestrator drives the loop. Voters answer the consensus questions — it
	// includes the orchestrator when the orchestrator is live.
	Orchestrator runner.Runner
	Voters       []runner.Runner

	// Roster is what detection concluded about every registered runner, for the
	// footer. SPEC §5 promises the footer names who voted, and "codex was
	// broken" is a fact the reader is owed.
	Roster *runner.Roster

	// Consensus turns the two decision points on. It is true by default; the
	// flag that turns it off is the frugal one.
	Consensus bool

	// Customize is the body of a --customize prompt, and CustomizePath is where
	// it came from, for the footer.
	Customize     string
	CustomizePath string

	// Transcript receives the runner's raw transcript; Log receives the loud
	// notices a human needs to see even when stdout is being redirected.
	Transcript io.Writer
	Log        io.Writer
}

// Result is a finished run.
type Result struct {
	// Markdown is the page.
	Markdown string

	// Mode is ModeAgentic or ModeFallback.
	Mode string

	// Decisions is what consensus settled, in the order the points were run.
	Decisions []Decision

	// Run is the orchestration's own account of itself, nil when it never
	// completed.
	Run *runner.Run

	// Violations is why the page was rejected, empty on a clean run.
	Violations []Violation
}

// FellBack reports whether the deterministic composer wrote this page.
func (r *Result) FellBack() bool { return r.Mode == ModeFallback }

// Compose runs the pipeline.
//
// It returns an error only when there is no page to give: a runner that cannot
// be driven, or a fact base that will not compose. A page the runner wrote and
// aubade then rejected is not an error — it is a Result in ModeFallback, because
// the user asked for a digest and there is an honest one available.
func Compose(ctx context.Context, in Input) (*Result, error) {
	if in.Corpus == nil {
		return nil, fmt.Errorf("agentic: no corpus")
	}
	if in.Orchestrator == nil {
		return nil, fmt.Errorf("agentic: no runner to orchestrate with")
	}
	if err := in.Signals.Validate(); err != nil {
		return nil, fmt.Errorf("agentic: refusing to compose from an invalid fact base: %w", err)
	}

	composed, decisions := Decide(ctx, in)
	res := &Result{Decisions: decisions}

	// The deterministic page is built either way. It is the source of the
	// honesty floor on a good run and the whole page on a rejected one — the
	// same code in both cases, so the reader cannot tell from an honesty line
	// which composer was in charge.
	page, err := digest.Build(digest.Input{
		Corpus:  in.Corpus,
		Signals: composed,
		Now:     in.Now,
		Loc:     in.Loc,
		Owner:   in.Owner,
		Mode:    digest.ModeNoLLM,
	})
	if err != nil {
		return nil, fmt.Errorf("agentic: %w", err)
	}

	prompt, err := BuildPrompt(PromptInput{
		Day:       in.Day,
		Owner:     in.Owner,
		Signals:   composed,
		Profile:   in.Corpus.Profile,
		ToolBin:   in.ToolBin,
		DataDir:   in.DataDir,
		Today:     in.Today,
		MaxCalls:  runner.MaxToolCalls,
		Customize: in.Customize,
		Decisions: decisions,
	})
	if err != nil {
		return nil, err
	}

	run, err := in.Orchestrator.Orchestrate(ctx, runner.Goal{
		Prompt:     prompt,
		ToolBin:    in.ToolBin,
		ToolPrefix: "tool",
		WorkDir:    in.WorkDir,
		ReadDirs:   readDirs(in),
		Transcript: in.Transcript,
	})
	if err != nil {
		// No page at all. The deterministic composer is one flag away and the
		// message says so, but taking it on the user's behalf would be composing
		// the digest a different way than they asked for.
		return nil, fmt.Errorf("%s could not compose the digest: %w\nrun `aubade digest --no-llm` for the deterministic page over these same signals", in.Orchestrator.Name(), err)
	}
	res.Run = run

	// The fact base for validation is the toolbox's own output. Consensus may
	// add a sentence to a detail line, and it never adds a citation that the
	// extractors did not already produce.
	facts := NewFactBase(in.Signals)
	if v := facts.Validate(run.Markdown); len(v) > 0 {
		res.Violations = v
		res.Mode = ModeFallback
		res.Markdown = fallbackPage(in, page, res)
		notice(in.Log, "aubade: %s composed a page whose citations do not resolve against the fact base (%s).\naubade: that page was thrown away whole; the deterministic composer wrote the digest instead.\n",
			in.Orchestrator.Name(), summarize(v))
		return res, nil
	}

	res.Mode = ModeAgentic
	res.Markdown = agenticPage(in, page, run, decisions)
	return res, nil
}

// agenticPage assembles an accepted page: the model's body, the honesty floor
// aubade owns, and the provenance footer.
func agenticPage(in Input, page *digest.Digest, run *runner.Run, decisions []Decision) string {
	label := digest.NewLabeler(in.Corpus, in.Loc)

	var b strings.Builder
	b.WriteString(strings.TrimRight(Resolve(run.Markdown, label), "\n"))
	b.WriteString("\n\n")
	b.WriteString(page.HonestyFloor())
	b.WriteString(footer(in, run, decisions, nil))
	return b.String()
}

// fallbackPage assembles a rejected run's page: the notice first, because a
// reader who stops after one line must still learn that the page in front of
// them is not the one they asked for.
func fallbackPage(in Input, page *digest.Digest, res *Result) string {
	var b strings.Builder
	fmt.Fprintf(&b, "> **This page was not composed by %s.** %s, so aubade threw the whole page away rather than print a receipt nobody can follow, and the deterministic composer wrote what follows instead: the same facts, template prose.\n\n",
		in.Orchestrator.Name(), rejectionReason(res.Violations))
	b.WriteString(page.Markdown())
	b.WriteString(footer(in, res.Run, res.Decisions, res.Violations))
	return b.String()
}

// rejectionReason is the half-sentence that says what was wrong with the page
// the reader is not getting.
func rejectionReason(vs []Violation) string {
	if len(vs) == 1 && vs[0].Kind == violationNone {
		return "It carried no citations at all, so not one line on it could be checked"
	}
	return fmt.Sprintf("It cited %s that %s not in `signals.json` (%s)",
		plural(len(vs), "source", "sources"), isAre(len(vs)), summarizeRefs(vs))
}

// footer says who composed the page, who voted on it, and what the honesty
// layer is doing there.
//
// SPEC §5 requires the footer to name the runners that participated. It names
// the ones that could not, too: a roster of one reads very differently
// depending on whether the other two were absent or broken, and the reader is
// the one who has to decide how much to trust a single opinion.
func footer(in Input, run *runner.Run, decisions []Decision, violations []Violation) string {
	var b strings.Builder
	b.WriteString("\n---\n*")

	if len(violations) > 0 {
		fmt.Fprintf(&b, "Runner provenance: %s drove the toolbox and its page was rejected by aubade's citation check, so the page above is the deterministic one.",
			in.Orchestrator.Name())
	} else {
		calls := 0
		if run != nil {
			calls = run.ToolCalls
		}
		fmt.Fprintf(&b, "Composed by `aubade digest` in agentic mode: %s orchestrated aubade's deterministic toolbox over %s, making %s.",
			in.Orchestrator.Name(), plural(len(in.Signals), "signal", "signals"), plural(calls, "toolbox call", "toolbox calls"))
	}

	b.WriteString(" " + consensusLine(in, decisions))

	if path := strings.TrimSpace(in.CustomizePath); path != "" {
		fmt.Fprintf(&b, " Format customized by %s; the honesty layer is not customizable and was appended by aubade from the same signals.", path)
	} else {
		b.WriteString(" The honesty layer is appended by aubade from the same signals, whatever the composer wrote.")
	}
	b.WriteString(" Every factual line above carries its own citation, resolved against `signals.json`.*\n")
	return b.String()
}

// consensusLine reports the vote: who was asked, who answered, and what each
// decision point concluded.
func consensusLine(in Input, decisions []Decision) string {
	if !in.Consensus {
		return "Consensus off (`--consensus=off`): one runner, one opinion, no vote."
	}
	roster := "runners: " + strings.Join(names(in.Voters), ", ")
	if in.Roster != nil {
		roster = "runners — " + in.Roster.Describe()
	}
	if len(in.Voters) == 1 {
		roster += "; a single runner, so consensus is a formality this morning"
	}

	notes := make([]string, 0, len(decisions))
	for _, d := range decisions {
		if n := strings.TrimSpace(d.Note); n != "" {
			notes = append(notes, n)
		}
	}
	if len(notes) == 0 {
		return fmt.Sprintf("Consensus on (%s); no decision point needed a vote.", roster)
	}
	return fmt.Sprintf("Consensus on (%s): %s.", roster, strings.Join(notes, "; "))
}

// readDirs is what the loop is allowed to read: the corpus and the directory the
// toolbox binary lives in. Nothing else — the model's access to the world is the
// allowlisted command, not the filesystem.
func readDirs(in Input) []string {
	var out []string
	for _, dir := range []string{in.DataDir, filepath.Dir(in.ToolBin)} {
		if d := strings.TrimSpace(dir); d != "" && d != "." {
			out = append(out, d)
		}
	}
	return out
}

// notice writes a loud message to the run's log, when there is one.
func notice(w io.Writer, format string, args ...any) {
	if w == nil {
		return
	}
	fmt.Fprintf(w, format, args...)
}

// names lists runner names in order.
func names(rs []runner.Runner) []string {
	out := make([]string, 0, len(rs))
	for _, r := range rs {
		out = append(out, r.Name())
	}
	if len(out) == 0 {
		return []string{"none"}
	}
	return out
}

// plural renders "1 signal" / "3 signals".
func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

// isAre agrees the verb with a count.
func isAre(n int) string {
	if n == 1 {
		return "is"
	}
	return "are"
}
