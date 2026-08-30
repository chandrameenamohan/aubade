package runner

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"
)

// ClaudeRunner drives the claude CLI headless. It is the only runner that both
// votes and orchestrates, and it is the default (SPEC §5).
//
// Four flags on every invocation are load-bearing rather than stylistic, each
// one a learning-test finding:
//
//   - `--setting-sources user` — a headless `-p` run auto-discovers and obeys
//     the CLAUDE.md of its working directory. aubade runs from a scratch
//     directory near the user's corpus, which may contain any instruction file
//     at all, and the honesty floor (SPEC §7) is exactly the thing such a file
//     could talk a model out of. This shuts the door (learning-tests/01).
//   - `--allowedTools "Bash(<bin> tool:*)"` — with it the loop runs; without it
//     the identical prompt is *denied* and recorded in `.permission_denials`
//     rather than prompted for or quietly run. No bypass flag is needed, and
//     "facts can only enter through cited tool output" becomes enforced rather
//     than intended (learning-tests/02).
//   - `--output-format stream-json --verbose` — the final envelope alone says
//     nothing about what the model did. The JSONL transcript carries every
//     tool_use block, which is how the loop is counted and bounded here instead
//     of by a `--max-turns` flag that does not exist (learning-tests/01, 02).
//   - `--json-schema <inline>` — inline, not a path. A path is rejected in
//     ~0.2s before any API call (learning-tests/01).
type ClaudeRunner struct {
	// Bin is the binary to run; empty means "claude" on PATH.
	Bin string

	// ProbeModel is the cheap model liveness probes use. Detection runs on every
	// digest, so the probe pays for itself only if it is cheap; the real
	// questions run on the user's default model, because a wrong top priority
	// costs more than the model that picked it.
	ProbeModel string
}

// NewClaude builds the default claude runner.
func NewClaude() *ClaudeRunner { return &ClaudeRunner{Bin: "claude", ProbeModel: "haiku"} }

// Name implements Runner.
func (r *ClaudeRunner) Name() string { return "claude" }

func (r *ClaudeRunner) bin() string {
	if strings.TrimSpace(r.Bin) != "" {
		return r.Bin
	}
	return "claude"
}

// CanOrchestrate implements Runner: claude is the runner whose tool loop and
// allowlist behaviour aubade has actually measured.
func (r *ClaudeRunner) CanOrchestrate() bool { return true }

// Installed implements Runner.
func (r *ClaudeRunner) Installed() bool {
	_, err := exec.LookPath(r.bin())
	return err == nil
}

// Probe implements Runner: one cheap real call, because PATH proves nothing.
func (r *ClaudeRunner) Probe(ctx context.Context) error {
	if !r.Installed() {
		return fmt.Errorf("%s: %w", r.Name(), ErrNotInstalled)
	}
	ctx, cancel := context.WithTimeout(ctx, ProbeBudget)
	defer cancel()

	args := []string{"-p", "Reply with exactly: ok", "--output-format", "json", "--setting-sources", "user"}
	if m := strings.TrimSpace(r.ProbeModel); m != "" {
		args = append(args, "--model", m)
	}
	out, err := output(ctx, r.bin(), args...)
	if err != nil {
		return fmt.Errorf("%s probe: %w", r.Name(), err)
	}
	env, err := decodeClaudeEnvelope(out)
	if err != nil {
		return fmt.Errorf("%s probe: %w", r.Name(), err)
	}
	if env.Subtype != "success" {
		return fmt.Errorf("%s probe: run finished as %q", r.Name(), env.Subtype)
	}
	return nil
}

// Ask implements Runner: one-shot, schema-constrained, structured answer.
func (r *ClaudeRunner) Ask(ctx context.Context, q Question) (json.RawMessage, error) {
	if !r.Installed() {
		return nil, fmt.Errorf("%s: %w", r.Name(), ErrNotInstalled)
	}
	ctx, cancel := context.WithTimeout(ctx, budget(q.Budget, AskBudget))
	defer cancel()

	args := []string{"-p", q.Prompt, "--output-format", "json", "--setting-sources", "user"}
	if s := strings.TrimSpace(q.Schema.JSON); s != "" {
		args = append(args, "--json-schema", s)
	}
	out, err := output(ctx, r.bin(), args...)
	if err != nil {
		return nil, fmt.Errorf("%s ask: %w", r.Name(), err)
	}
	answer, err := claudeAnswer(out)
	if err != nil {
		return nil, fmt.Errorf("%s ask: %w", r.Name(), err)
	}
	return answer, nil
}

// Orchestrate implements Runner: the tool loop, bounded by aubade rather than
// by the CLI.
func (r *ClaudeRunner) Orchestrate(ctx context.Context, g Goal) (*Run, error) {
	if !r.Installed() {
		return nil, fmt.Errorf("%s: %w", r.Name(), ErrNotInstalled)
	}
	if strings.TrimSpace(g.ToolBin) == "" {
		return nil, errors.New("claude orchestrate: no toolbox binary to allowlist; refusing to run an unbounded loop")
	}

	maxCalls := g.MaxToolCalls
	if maxCalls <= 0 {
		maxCalls = MaxToolCalls
	}
	ctx, cancel := context.WithTimeout(ctx, budget(g.Budget, OrchestrateBudget))
	defer cancel()
	ctx, stop := context.WithCancel(ctx)
	defer stop()

	args := []string{
		"-p", g.Prompt,
		"--output-format", "stream-json", "--verbose",
		"--setting-sources", "user",
		"--allowedTools", AllowSpec(g.ToolBin, g.ToolPrefix),
		"--max-budget-usd", fmt.Sprintf("%g", MaxBudgetUSD),
	}
	for _, dir := range g.ReadDirs {
		if strings.TrimSpace(dir) != "" {
			args = append(args, "--add-dir", dir)
		}
	}

	cmd := exec.CommandContext(ctx, r.bin(), args...)
	cmd.Dir = g.WorkDir
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("claude orchestrate: %w", err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("claude orchestrate: %w", err)
	}

	// The transcript is read as it streams so the loop can be stopped the moment
	// it exceeds its budget. Counting afterwards would report an overrun that
	// had already been paid for.
	run, streamErr := readClaudeStream(stdout, g.Transcript, maxCalls, stop)
	waitErr := cmd.Wait()

	switch {
	case errors.Is(streamErr, ErrToolBudget):
		return nil, fmt.Errorf("claude orchestrate: %w after %d calls (%s)",
			ErrToolBudget, run.ToolCalls, strings.Join(run.Commands, "; "))
	case streamErr != nil:
		return nil, fmt.Errorf("claude orchestrate: %w", streamErr)
	case ctx.Err() != nil:
		return nil, fmt.Errorf("claude orchestrate: %w after %s", ctx.Err(), run.Duration)
	case waitErr != nil:
		return nil, fmt.Errorf("claude orchestrate: %w: %s", waitErr, tail(stderr.String()))
	case strings.TrimSpace(run.Markdown) == "":
		return nil, fmt.Errorf("claude orchestrate: %w", ErrEmptyAnswer)
	}
	return run, nil
}

// AllowSpec is the one permission the orchestrator is granted. It is a function
// so the exact string is testable: an allowlist that is subtly wrong fails open
// in the worst possible way — a loop that can run anything.
func AllowSpec(bin, prefix string) string {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		prefix = "tool"
	}
	return fmt.Sprintf("Bash(%s %s:*)", bin, prefix)
}

// claudeEnvelope is the `--output-format json` result object.
type claudeEnvelope struct {
	Subtype string `json:"subtype"`
	// Result is a JSON *string* when a schema was supplied, not a nested
	// object — so a schema-constrained answer is decoded twice. Getting this
	// wrong yields a type error rather than a missing field, which is the good
	// case (learning-tests/01, finding A1).
	Result     string `json:"result"`
	NumTurns   int    `json:"num_turns"`
	DurationMS int    `json:"duration_ms"`
	IsError    bool   `json:"is_error"`
}

func decodeClaudeEnvelope(b []byte) (*claudeEnvelope, error) {
	var env claudeEnvelope
	if err := json.Unmarshal(bytes.TrimSpace(b), &env); err != nil {
		return nil, fmt.Errorf("the run produced no JSON envelope: %w (%s)", err, tail(string(b)))
	}
	return &env, nil
}

// claudeAnswer performs the two-stage decode: envelope, then the JSON string
// inside `.result`.
func claudeAnswer(b []byte) (json.RawMessage, error) {
	env, err := decodeClaudeEnvelope(b)
	if err != nil {
		return nil, err
	}
	if env.IsError || (env.Subtype != "" && env.Subtype != "success") {
		return nil, fmt.Errorf("run finished as %q: %s", env.Subtype, tail(env.Result))
	}
	answer := strings.TrimSpace(env.Result)
	if answer == "" {
		return nil, ErrEmptyAnswer
	}
	if !json.Valid([]byte(answer)) {
		return nil, fmt.Errorf("the answer is not JSON: %s", tail(answer))
	}
	return json.RawMessage(answer), nil
}

// streamEvent is the subset of the JSONL transcript this package reads.
type streamEvent struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype"`
	Message struct {
		Content []struct {
			Type  string `json:"type"`
			Name  string `json:"name"`
			Input struct {
				Command string `json:"command"`
			} `json:"input"`
		} `json:"content"`
	} `json:"message"`
	Result     string `json:"result"`
	NumTurns   int    `json:"num_turns"`
	DurationMS int    `json:"duration_ms"`
	IsError    bool   `json:"is_error"`
}

// streamBuffer is the per-line cap for the transcript scanner. Tool results
// carry whole signal sets, so the default 64KB line limit is far too small and
// its failure mode — a truncated transcript read as a finished loop — is silent.
const streamBuffer = 8 << 20

// readClaudeStream consumes the JSONL transcript, counting the loop as it goes
// and stopping it at the cap.
//
// stop is called rather than returned-through because killing the subprocess is
// the only bound available: the CLI has no --max-turns, so "bounded turns" means
// aubade watches and pulls the plug (learning-tests/01, finding A3).
func readClaudeStream(r io.Reader, tee io.Writer, maxCalls int, stop func()) (*Run, error) {
	run := &Run{}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), streamBuffer)

	var overrun bool
	for sc.Scan() {
		line := sc.Bytes()
		if tee != nil {
			_, _ = tee.Write(append(append([]byte{}, line...), '\n'))
		}
		var ev streamEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			continue // a non-JSON line is noise, not a finding
		}
		switch ev.Type {
		case "assistant":
			for _, block := range ev.Message.Content {
				if block.Type != "tool_use" {
					continue
				}
				run.ToolCalls++
				if cmd := strings.TrimSpace(block.Input.Command); cmd != "" {
					run.Commands = append(run.Commands, cmd)
				}
			}
			if run.ToolCalls > maxCalls && !overrun {
				overrun = true
				stop()
			}
		case "result":
			run.Turns = ev.NumTurns
			run.Duration = time.Duration(ev.DurationMS) * time.Millisecond
			run.Markdown = ev.Result
			if ev.IsError || (ev.Subtype != "" && ev.Subtype != "success") {
				// A loop we killed reports its own death here. The budget is the
				// real cause, so say that rather than the symptom.
				if overrun {
					return run, ErrToolBudget
				}
				return run, fmt.Errorf("the loop finished as %q: %s", ev.Subtype, tail(ev.Result))
			}
		}
	}
	if overrun {
		return run, ErrToolBudget
	}
	if err := sc.Err(); err != nil {
		return run, fmt.Errorf("reading the transcript: %w", err)
	}
	return run, nil
}

// output runs a command to completion and returns stdout, folding a non-zero
// exit into an error that carries what the process actually said. A runner that
// fails silently is indistinguishable from one that disagrees.
func output(ctx context.Context, bin string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		return stdout.Bytes(), fmt.Errorf("%w (its budget ran out before it answered)", ctx.Err())
	}
	if err != nil {
		return stdout.Bytes(), fmt.Errorf("%w: %s", err, tail(stderr.String()+stdout.String()))
	}
	return stdout.Bytes(), nil
}

// tail is the last useful line or two of a subprocess's noise, for an error
// message a human can act on.
func tail(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if n := len(lines); n > 2 {
		lines = lines[n-2:]
	}
	out := strings.TrimSpace(strings.Join(lines, " / "))
	if len(out) > 400 {
		out = out[:400] + "…"
	}
	if out == "" {
		return "(no output)"
	}
	return out
}
