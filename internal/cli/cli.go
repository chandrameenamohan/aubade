// Package cli builds aubade's two command trees — the product (`aubade`) and
// the internal harness (`aubade-lab`) — and the shared plumbing they need:
// the not-implemented-yet stub error, and AX-aware error rendering.
//
// The trees live here rather than in cmd/*/main.go so they are unit-testable:
// help text and flag contracts are graded UX, so they get tests like anything
// else.
package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/chandrameenamohan/aubade/internal/ax"
	"github.com/spf13/cobra"
)

// Version is the build version, overridable at link time:
//
//	go build -ldflags "-X github.com/chandrameenamohan/aubade/internal/cli.Version=1.2.3"
var Version = "0.1.0-dev"

// StubError is returned by a command whose behaviour is not built yet. It names
// the bead that will implement it, so a `not implemented` message is a pointer
// into the plan rather than a dead end. Bead ids track the orchestrator's plan:
// B1 dataset generator, C1 deterministic toolbox, C2 digest renderer,
// D1 eval harness, E1 scheduling design.
type StubError struct {
	Command string // e.g. "aubade digest"
	Bead    string // e.g. "C2"
	What    string // one line on what will land there
}

func (e *StubError) Error() string {
	return fmt.Sprintf("%s: not implemented yet (bead %s) — %s", e.Command, e.Bead, e.What)
}

// stub returns a RunE that fails with a StubError, and records the bead on the
// command so `--help` can advertise its status honestly.
func stub(bead, what string) func(*cobra.Command, []string) error {
	return func(c *cobra.Command, _ []string) error {
		return &StubError{Command: c.CommandPath(), Bead: bead, What: what}
	}
}

// annotate marks a command as an unimplemented stub for help rendering.
func annotate(c *cobra.Command, bead string) *cobra.Command {
	if c.Annotations == nil {
		c.Annotations = map[string]string{}
	}
	c.Annotations["aubade.bead"] = bead
	c.Annotations["aubade.status"] = "stub"
	return c
}

// RenderError writes err in the shape the current caller expects: a JSON
// envelope for a detected AI agent (machine-parseable errors are the whole
// point of the AX layer, SPEC §9), plain prose for a human.
func RenderError(w io.Writer, err error) {
	if err == nil {
		return
	}
	if ax.OutputMode() != ax.ModeJSON {
		fmt.Fprintf(w, "aubade: %v\n", err)
		return
	}

	payload := map[string]any{
		"ok":    false,
		"error": map[string]any{"message": err.Error(), "kind": "error"},
	}
	if se, isStub := err.(*StubError); isStub {
		payload["error"] = map[string]any{
			"kind":    "not_implemented",
			"message": se.Error(),
			"command": se.Command,
			"bead":    se.Bead,
			"hint":    "this subcommand is scaffolded but not built yet; do not retry",
		}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if encErr := enc.Encode(payload); encErr != nil {
		// Never let error rendering swallow the error itself.
		fmt.Fprintf(w, "aubade: %v\n", err)
	}
}

// newRoot builds a root command with the house style: no usage dump on a
// runtime error (usage noise buries the actual message, and agents parse the
// last line), errors printed by main via RenderError.
func newRoot(use, short, long string) *cobra.Command {
	c := &cobra.Command{
		Use:           use,
		Short:         short,
		Long:          strings.TrimSpace(long),
		Version:       Version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	c.SetVersionTemplate("{{.Name}} {{.Version}}\n")
	return c
}
