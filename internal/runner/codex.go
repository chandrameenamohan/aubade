package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// CodexRunner is the second consensus voter: Ask-only, by measurement rather
// than by preference.
//
// Its syntax is not a guess. learning-tests/03 pinned every part of it against
// codex-cli 0.141.0:
//
//   - `-p` is `--profile`, not the prompt. The headless one-shot is the `exec`
//     SUBCOMMAND, which takes the prompt positionally.
//   - `--output-schema` takes a FILE and rejects inline JSON — the exact mirror
//     image of claude's `--json-schema`. That mirroring is why Question carries
//     a schema document and not a rendered flag.
//   - `--skip-git-repo-check` is required: exec refuses to run outside a git
//     repository, and aubade's scratch directory is not one.
//   - Diagnostics and prose share a stream, and exec announces "Reading
//     additional input from stdin…" even with stdin closed. So the answer is
//     read from `-o <file>`, never from stdout.
//
// No Orchestrate: aubade has never verified codex driving a tool loop against
// an allowlist, and an unverified sandbox boundary is not a boundary. It returns
// ErrNoOrchestrate, which costs it nothing as a voter — consensus only ever
// calls Ask.
type CodexRunner struct {
	// Bin is the binary to run; empty means "codex" on PATH.
	Bin string
}

// NewCodex builds the default codex runner.
func NewCodex() *CodexRunner { return &CodexRunner{Bin: "codex"} }

// Name implements Runner.
func (r *CodexRunner) Name() string { return "codex" }

func (r *CodexRunner) bin() string {
	if strings.TrimSpace(r.Bin) != "" {
		return r.Bin
	}
	return "codex"
}

// CanOrchestrate implements Runner: no. See the type comment — an unverified
// sandbox boundary is not a boundary.
func (r *CodexRunner) CanOrchestrate() bool { return false }

// Installed implements Runner.
func (r *CodexRunner) Installed() bool {
	_, err := exec.LookPath(r.bin())
	return err == nil
}

// Probe implements Runner.
//
// This is the runner that taught the project why probing is not optional: on the
// development machine codex is on PATH, `codex login status` says "Logged in
// using ChatGPT" and exits 0, `codex doctor` says auth is configured — and
// `codex exec` returns 401 in about five seconds. Only a real call is evidence.
func (r *CodexRunner) Probe(ctx context.Context) error {
	if !r.Installed() {
		return fmt.Errorf("%s: %w", r.Name(), ErrNotInstalled)
	}
	ctx, cancel := context.WithTimeout(ctx, ProbeBudget)
	defer cancel()

	if _, err := r.exec(ctx, "Reply with exactly: ok", ""); err != nil {
		return fmt.Errorf("%s probe: %w", r.Name(), err)
	}
	return nil
}

// Ask implements Runner.
func (r *CodexRunner) Ask(ctx context.Context, q Question) (json.RawMessage, error) {
	if !r.Installed() {
		return nil, fmt.Errorf("%s: %w", r.Name(), ErrNotInstalled)
	}
	ctx, cancel := context.WithTimeout(ctx, budget(q.Budget, AskBudget))
	defer cancel()

	dir, err := os.MkdirTemp("", "aubade-codex-")
	if err != nil {
		return nil, fmt.Errorf("codex ask: %w", err)
	}
	defer os.RemoveAll(dir)

	schemaPath := ""
	if s := strings.TrimSpace(q.Schema.JSON); s != "" {
		schemaPath = filepath.Join(dir, "schema.json")
		if err := os.WriteFile(schemaPath, []byte(s), 0o600); err != nil {
			return nil, fmt.Errorf("codex ask: %w", err)
		}
	}

	answer, err := r.exec(ctx, q.Prompt, schemaPath)
	if err != nil {
		return nil, fmt.Errorf("codex ask: %w", err)
	}
	if !json.Valid(answer) {
		return nil, fmt.Errorf("codex ask: the schema-constrained answer is not JSON: %s", tail(string(answer)))
	}
	return json.RawMessage(answer), nil
}

// Orchestrate implements Runner: codex votes, it does not drive the toolbox.
func (r *CodexRunner) Orchestrate(context.Context, Goal) (*Run, error) {
	return nil, fmt.Errorf("codex: %w", ErrNoOrchestrate)
}

// exec runs one `codex exec` and returns the final message, read from the file
// codex was told to write it to.
func (r *CodexRunner) exec(ctx context.Context, prompt, schemaPath string) ([]byte, error) {
	dir, err := os.MkdirTemp("", "aubade-codex-out-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	last := filepath.Join(dir, "last.txt")

	args := []string{"exec", "--skip-git-repo-check", "--sandbox", "read-only", "-o", last}
	if schemaPath != "" {
		args = append(args, "--output-schema", schemaPath)
	}
	args = append(args, prompt)

	cmd := exec.CommandContext(ctx, r.bin(), args...)
	cmd.Dir = dir
	cmd.Stdin = nil
	combined, runErr := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return nil, fmt.Errorf("%w (its budget ran out before it answered)", ctx.Err())
	}
	if runErr != nil {
		return nil, fmt.Errorf("%w: %s", runErr, tail(string(combined)))
	}

	// Exit 0 with no answer file is a contract violation, not an empty opinion:
	// a runner that says nothing must never be counted as having voted.
	data, err := os.ReadFile(last)
	if err != nil || len(strings.TrimSpace(string(data))) == 0 {
		return nil, fmt.Errorf("%w (exited 0 and wrote no %s)", ErrEmptyAnswer, filepath.Base(last))
	}
	return []byte(strings.TrimSpace(string(data))), nil
}
