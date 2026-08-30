// Package cli builds aubade's two command trees — the product (`aubade`) and
// the internal harness (`aubade-lab`) — and the shared plumbing they need,
// which is now just AX-aware error rendering.
//
// Every command in both trees is implemented. The scaffolding that used to
// announce the unbuilt ones — a StubError naming the bead that would land it,
// rendered as a `not_implemented` envelope for agent callers — went out with
// the last stub in bead D3, because machinery with nothing left to announce is
// machinery a reader has to rule out.
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
