package eval

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/chandrameenamohan/aubade/internal/datagen"
	"github.com/chandrameenamohan/aubade/internal/model"
	"github.com/chandrameenamohan/aubade/internal/runner"
)

// The capability suite: the agentic digest, run N times, reported as two
// numbers that are never averaged (EVAL-PRINCIPLES #10).
//
//	pass^N  every trial passed this task — reliability. This is the number that
//	        says whether the behaviour can be depended on.
//	pass@N  at least one trial passed it — the capability ceiling. This is the
//	        number that says whether the engine can do it at all.
//
// A task at pass@3 = 1 and pass^3 = 0 is a task the engine *can* do and does
// not do reliably, which is a completely different piece of work from a task at
// pass@3 = 0. Collapsing them into one percentage destroys the only distinction
// that tells you what to go and fix.
//
// Every trial gets its own output directory (#11). Sharing one would let trial
// 1's digest.md be graded as trial 2's when trial 2 dies halfway, which turns a
// crash into a pass and a non-determinism measurement into fiction.

// DefaultTrials is N. Three is the smallest number that can distinguish "always"
// from "sometimes" from "never", and each trial is a paid model run.
const DefaultTrials = 3

// trialBudget caps one agentic trial. The orchestration loop has its own
// budgets; this is the harness refusing to wait forever on a runner that hung
// somewhere they do not reach.
const trialBudget = 12 * time.Minute

// Capability is the whole capability suite for one run.
type Capability struct {
	// Skipped and SkipReason record a suite that did not run. A skip is
	// reported as loudly as a failure: a capability suite that quietly does
	// nothing eventually gets cited as evidence that it passed.
	Skipped    bool
	SkipReason string

	// Trials are the graded trials, in order. A trial that failed to produce a
	// page is still here, with every task failed and Err saying why.
	Trials []*Trial

	// Traps is the answer key, kept so the aggregate can be computed per task.
	Traps datagen.Traps
}

// Trial is one agentic run and everything the harness learned from it.
type Trial struct {
	N         int
	Dir       string
	Result    *Result
	Grounding Grounding

	// Artifacts is what this trial wrote, kept so a later pass — the judge —
	// can read the page the product actually produces rather than the
	// deterministic stand-in. Nil on a trial that produced nothing.
	Artifacts *Artifacts

	// Err is why this trial produced nothing, empty on a trial that ran.
	Err error

	// Log is the tail of the run's own output, for a trial that failed.
	Log string
}

// Aggregate is one task's capability numbers across the trials.
type Aggregate struct {
	Trap    datagen.Trap
	Passed  int
	Trials  int
	PassAll bool // pass^N
	PassAny bool // pass@N
}

// Aggregates computes the per-task numbers, in answer-key order.
func (c *Capability) Aggregates() []Aggregate {
	out := make([]Aggregate, 0, len(c.Traps))
	for _, trap := range c.Traps {
		a := Aggregate{Trap: trap, Trials: len(c.Trials)}
		for _, tr := range c.Trials {
			if tr.Result == nil {
				continue
			}
			if r, ok := tr.Result.Get(trap.ID); ok && r.Passed {
				a.Passed++
			}
		}
		a.PassAll = a.Trials > 0 && a.Passed == a.Trials
		a.PassAny = a.Passed > 0
		out = append(out, a)
	}
	return out
}

// Rates is the suite's two headline numbers: the share of tasks that passed
// every trial, and the share that passed at least one.
func (c *Capability) Rates() (passAll, passAny, tasks int) {
	for _, a := range c.Aggregates() {
		tasks++
		if a.PassAll {
			passAll++
		}
		if a.PassAny {
			passAny++
		}
	}
	return passAll, passAny, tasks
}

// CapabilityInput is what a capability run needs to drive the product binary.
type CapabilityInput struct {
	// Bin is the aubade binary to run, Data the corpus, Today the anchor date.
	Bin   string
	Data  string
	Today string

	// OutDir is the parent directory; each trial gets OutDir/trial-N.
	OutDir string

	// Trials is N; zero means DefaultTrials.
	Trials int

	// Corpus and Loc are what the grounding check resolves citations against.
	Corpus *model.Corpus
	Loc    *time.Location

	// Traps is the answer key.
	Traps datagen.Traps
}

// ClaudePresent reports whether the runner the capability suite needs is on
// this machine. It asks the product's own runner rather than looking for a
// binary name, so the harness and the digest cannot disagree about what
// "installed" means.
func ClaudePresent() bool { return runner.NewClaude().Installed() }

// RunCapability runs the agentic digest N times and grades each trial.
//
// It skips — loudly, and as a first-class outcome rather than a silent nil —
// when the claude CLI is absent. There is no meaningful capability number for a
// machine that cannot run the agent, and a suite that reports one anyway is
// lying about what it measured.
func RunCapability(ctx context.Context, in CapabilityInput) *Capability {
	suite := &Capability{Traps: in.Traps}
	if !ClaudePresent() {
		suite.Skipped = true
		suite.SkipReason = "the claude CLI is not on PATH, so the agentic digest could not be run on this machine"
		return suite
	}

	n := in.Trials
	if n <= 0 {
		n = DefaultTrials
	}
	for i := 1; i <= n; i++ {
		suite.Trials = append(suite.Trials, runTrial(ctx, in, i))
	}
	return suite
}

// runTrial is one isolated agentic digest, graded.
func runTrial(ctx context.Context, in CapabilityInput, n int) *Trial {
	tr := &Trial{N: n, Dir: filepath.Join(in.OutDir, "trial-"+strconv.Itoa(n))}

	runCtx, cancel := context.WithTimeout(ctx, trialBudget)
	defer cancel()

	args := []string{"digest", "--data", in.Data, "--out", tr.Dir}
	if in.Today != "" {
		args = append(args, "--today", in.Today)
	}
	cmd := exec.CommandContext(runCtx, in.Bin, args...)
	// The page goes to disk; stdout is only a copy of it, and nothing here
	// reads it. stderr is kept because it is where a failed run says why.
	var errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = io.Discard, &errOut

	if err := cmd.Run(); err != nil {
		tr.Err = fmt.Errorf("trial %d: %s %v: %w", n, in.Bin, args, err)
		tr.Log = tail(errOut.String(), 400)
		tr.Result = Grade(in.Traps, nil)
		return tr
	}

	a, err := LoadArtifacts(tr.Dir)
	if err != nil {
		tr.Err = fmt.Errorf("trial %d: %w", n, err)
		tr.Result = Grade(in.Traps, nil)
		return tr
	}

	tr.Artifacts = a
	tr.Result = Grade(in.Traps, a)
	tr.Grounding = CheckGrounding(a, in.Corpus, in.Loc)
	return tr
}

// FirstPage is the page from the earliest trial that produced one, or "" when
// none did. It is what the judge reads when the capability suite ran: the
// deterministic page is a stand-in, and the agentic page is what a user gets.
func (c *Capability) FirstPage() (page, dir string) {
	for _, tr := range c.Trials {
		if tr.Artifacts != nil && strings.TrimSpace(tr.Artifacts.Digest) != "" {
			return tr.Artifacts.Digest, tr.Dir
		}
	}
	return "", ""
}

// tail keeps the last n characters of a runner's output, which is where the
// error is.
func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "…" + s[len(s)-n:]
}
