// Package runner is aubade's model-runner abstraction: the two things the
// digest ever asks a model to do, behind one vendor- and transport-neutral
// interface.
//
// The interface is deliberately two methods wide (SPEC §5):
//
//   - **Ask** — one bounded, schema-constrained question with a structured
//     answer. This is what consensus fans out and majority-votes, so the answer
//     has to be a value rather than prose.
//   - **Orchestrate** — the tool-calling loop. The runner is handed the
//     deterministic toolbox and a goal, and gives back the composed page.
//     Facts can only enter through cited tool output, so the loop's boundary is
//     an allowlist rather than an instruction.
//
// Week one ships two CLI-backed runners. Nothing in the engine knows they are
// CLIs: an SDK-backed runner (Anthropic, OpenAI, Gemini — all of which do native
// tool calling) implements the same two methods and registers by name.
//
// Three things learning-tests/ measured against the real binaries shape this
// package, and are worth carrying in your head while reading it:
//
//  1. **There is no --max-turns on the claude CLI.** Bounding the loop is
//     aubade's job, so ClaudeRunner counts tool calls out of the streamed
//     transcript and kills the process at the cap (learning-tests/01, finding 3).
//  2. **The two CLIs are mirror images on structured output** — claude takes the
//     schema inline and rejects a path, codex takes a path and rejects inline
//     JSON. So Question carries the *schema*, never a pre-rendered flag, and
//     each implementation adapts (learning-tests/01 and 03).
//  3. **Presence is not liveness.** codex is installed here, reports itself
//     logged in three different ways, and 401s on the first real call. Detection
//     therefore probes with a capped real call, and a runner that fails is
//     dropped from the vote rather than counted as a dissent — a 401 is not an
//     opinion (learning-tests/03 and 05).
package runner

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"time"
)

// Budgets. Every model call aubade makes is bounded by one of these, because
// the digest runs unattended at 06:00 and a hung runner must not hold the job
// open. They are constants rather than flags on purpose: a knob nobody turns is
// surface, and the numbers only need to move if a runner's latency does.
const (
	// ProbeBudget caps the liveness call detection makes per runner.
	ProbeBudget = 45 * time.Second

	// AskBudget caps one consensus question.
	AskBudget = 90 * time.Second

	// OrchestrateBudget caps the whole tool-calling loop, wall clock.
	OrchestrateBudget = 6 * time.Minute

	// MaxToolCalls caps how many toolbox calls one orchestration may make.
	// The toolbox has nine tools; a page that needs three times that many
	// calls is a loop that has stopped converging.
	MaxToolCalls = 27

	// MaxBudgetUSD is the spend cap handed to a runner that honours one.
	MaxBudgetUSD = 2.0
)

// Errors a caller is expected to branch on.
var (
	// ErrNoOrchestrate is returned by a runner that can answer one-shot
	// questions but cannot drive a tool loop. It is a capability statement, not
	// a failure: such a runner is still a full consensus voter.
	ErrNoOrchestrate = errors.New("runner: answers one-shot questions only; it cannot drive the toolbox loop")

	// ErrNotInstalled means the runner's binary is not on PATH.
	ErrNotInstalled = errors.New("runner: not installed")

	// ErrToolBudget means the loop hit MaxToolCalls and was stopped.
	ErrToolBudget = errors.New("runner: tool-call budget exhausted")

	// ErrEmptyAnswer means the runner exited successfully and said nothing
	// usable, which is a contract violation rather than an opinion.
	ErrEmptyAnswer = errors.New("runner: exited 0 with no answer")
)

// Schema is the JSON Schema a one-shot answer must obey.
//
// It is carried as a document, never as a rendered command-line argument,
// because the two CLIs disagree about how a schema is passed (inline vs. file).
// Name is used for diagnostics and for the temp file a file-taking runner needs.
type Schema struct {
	Name string
	JSON string
}

// Question is one bounded, structured thing to ask a model.
type Question struct {
	// Prompt is the whole question, already grounded in signals. Runners add
	// nothing to it: a consensus vote is only meaningful if every voter was
	// asked the identical question.
	Prompt string

	// Schema constrains the answer.
	Schema Schema

	// Budget caps this call. Zero means AskBudget.
	Budget time.Duration
}

// Goal is an orchestration request: what to produce, and the toolbox to produce
// it from.
type Goal struct {
	// Prompt is the composed orchestration prompt — the fact base, the section
	// contract, and the rules the page is written to.
	Prompt string

	// ToolBin is the absolute path to the aubade binary the loop may call, and
	// ToolPrefix is the argument prefix that is allowlisted ("tool"). Together
	// they are the entire sandbox: the runner may run `<ToolBin> tool …` and
	// nothing else at all.
	ToolBin    string
	ToolPrefix string

	// WorkDir is where the loop runs. It is a scratch directory rather than the
	// user's project, because a headless run obeys the CLAUDE.md of its working
	// directory (learning-tests/01, finding 4).
	WorkDir string

	// ReadDirs are extra directories the loop is granted access to — the corpus
	// and the binary's own directory.
	ReadDirs []string

	// MaxToolCalls and Budget bound the loop. Zero means the package defaults.
	MaxToolCalls int
	Budget       time.Duration

	// Transcript, when set, receives the raw runner transcript as it streams.
	// EVAL-PRINCIPLES #12 grades outputs, not paths, but keeps the transcript so
	// a later check can ask whether tool output grounded each cited fact.
	Transcript io.Writer
}

// Run is one completed orchestration.
type Run struct {
	// Markdown is the page the runner composed. It is not trusted yet: the
	// caller validates every citation in it against the fact base before any of
	// it reaches a reader.
	Markdown string

	// ToolCalls is how many toolbox calls the loop actually made, counted from
	// the transcript rather than reported by the model.
	ToolCalls int

	// Commands is each toolbox invocation, in order — the receipt for ToolCalls.
	Commands []string

	// Turns and Duration are what the runner reported about itself.
	Turns    int
	Duration time.Duration
}

// Runner is a model that can answer a bounded question and, sometimes, drive
// the toolbox.
type Runner interface {
	// Name is the registry key and the name the digest footer prints.
	Name() string

	// Installed reports whether the runner's binary exists. It is cheap, and it
	// is never sufficient — see Probe.
	Installed() bool

	// Probe makes one cheap real call and reports whether this runner can
	// actually answer right now. Presence is not liveness.
	Probe(ctx context.Context) error

	// CanOrchestrate reports whether this runner can drive the toolbox loop.
	// It is asked before any model call is paid for: discovering that the
	// chosen orchestrator only votes, after fanning out a consensus round, is
	// an avoidable way to spend someone's money.
	CanOrchestrate() bool

	// Ask puts one schema-constrained question and returns the raw JSON answer,
	// already unwrapped from whatever envelope the transport uses.
	Ask(ctx context.Context, q Question) (json.RawMessage, error)

	// Orchestrate drives the toolbox loop toward the goal. A runner that cannot
	// do this returns ErrNoOrchestrate.
	Orchestrate(ctx context.Context, g Goal) (*Run, error)
}

// budget picks the caller's budget or the default.
func budget(want, fallback time.Duration) time.Duration {
	if want > 0 {
		return want
	}
	return fallback
}
