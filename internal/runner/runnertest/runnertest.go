// Package runnertest provides a scripted runner.Runner for tests.
//
// It exists so that nothing under `go test ./...` ever calls a real model. That
// is not tidiness: a unit test that shells out to claude costs money, needs
// auth, and is non-deterministic — three separate disqualifications from a gate
// (VERIFICATION.md §2). The consensus vote math, the degradation paths, the
// citation validator and the whole CLI wiring are all exercised here against
// runners whose answers are written down in the test that reads them.
package runnertest

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/chandrameenamohan/aubade/internal/runner"
)

// Runner is a scripted runner. The zero value is an installed, live runner that
// cannot orchestrate and answers nothing; set the fields the test cares about.
type Runner struct {
	// RunnerName is what Name() returns.
	RunnerName string

	// Missing makes Installed() false; ProbeErr makes Probe fail. The two are
	// different states on purpose — absent and dead are reported differently and
	// the difference is in the digest footer.
	Missing  bool
	ProbeErr error

	// Orchestrates is what CanOrchestrate() reports.
	Orchestrates bool

	// Answers are returned by successive Ask calls; the last one repeats. An
	// empty list with no AskErr is a runner that returns an empty answer.
	Answers []string

	// AskErr, when set, fails every Ask — the 401 case, which must be dropped
	// from a tally rather than counted as a dissent.
	AskErr error

	// Page is what Orchestrate returns, and OrchestrateErr fails it.
	Page           string
	OrchestrateErr error
	ToolCalls      int

	mu       sync.Mutex
	asks     int
	prompts  []string
	goals    []runner.Goal
	schemata []runner.Schema
}

var _ runner.Runner = (*Runner)(nil)

// Name implements runner.Runner.
func (r *Runner) Name() string {
	if r.RunnerName == "" {
		return "fake"
	}
	return r.RunnerName
}

// Installed implements runner.Runner.
func (r *Runner) Installed() bool { return !r.Missing }

// CanOrchestrate implements runner.Runner.
func (r *Runner) CanOrchestrate() bool { return r.Orchestrates }

// Probe implements runner.Runner.
func (r *Runner) Probe(context.Context) error {
	if r.Missing {
		return fmt.Errorf("%s: %w", r.Name(), runner.ErrNotInstalled)
	}
	return r.ProbeErr
}

// Ask implements runner.Runner, returning the scripted answers in order.
func (r *Runner) Ask(_ context.Context, q runner.Question) (json.RawMessage, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.prompts = append(r.prompts, q.Prompt)
	r.schemata = append(r.schemata, q.Schema)
	if r.AskErr != nil {
		return nil, r.AskErr
	}
	if len(r.Answers) == 0 {
		return nil, runner.ErrEmptyAnswer
	}
	i := r.asks
	r.asks++
	if i >= len(r.Answers) {
		i = len(r.Answers) - 1
	}
	return json.RawMessage(r.Answers[i]), nil
}

// Orchestrate implements runner.Runner.
func (r *Runner) Orchestrate(_ context.Context, g runner.Goal) (*runner.Run, error) {
	r.mu.Lock()
	r.goals = append(r.goals, g)
	r.mu.Unlock()

	if r.OrchestrateErr != nil {
		return nil, r.OrchestrateErr
	}
	if !r.Orchestrates {
		return nil, fmt.Errorf("%s: %w", r.Name(), runner.ErrNoOrchestrate)
	}
	if g.Transcript != nil {
		_, _ = fmt.Fprintf(g.Transcript, "{\"type\":\"result\",\"runner\":%q}\n", r.Name())
	}
	return &runner.Run{Markdown: r.Page, ToolCalls: r.ToolCalls}, nil
}

// Asks is how many questions this runner was asked.
func (r *Runner) Asks() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.asks
}

// Prompts is every question this runner was asked, in order.
func (r *Runner) Prompts() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.prompts...)
}

// Schemas is every schema this runner was handed, in order.
func (r *Runner) Schemas() []runner.Schema {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]runner.Schema(nil), r.schemata...)
}

// Goals is every orchestration this runner was handed, in order.
func (r *Runner) Goals() []runner.Goal {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]runner.Goal(nil), r.goals...)
}

// LastPrompt is the most recent question, or "" if there was none.
func (r *Runner) LastPrompt() string {
	prompts := r.Prompts()
	if len(prompts) == 0 {
		return ""
	}
	return prompts[len(prompts)-1]
}

// Registry builds a registry over the given runners, in order.
func Registry(runners ...*Runner) *runner.Registry {
	reg := runner.NewRegistry()
	for _, r := range runners {
		reg.Register(r.Name(), func() runner.Runner { return r })
	}
	return reg
}
