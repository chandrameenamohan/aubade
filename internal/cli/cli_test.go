package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// run executes a command tree with args, capturing stdout/stderr.
func run(root *cobra.Command, args ...string) (string, error) {
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}

// The CLI surface is a contract with both the SPEC and the eval harness. If a
// subcommand or a flag silently disappears, this fails.
func TestAubadeSurface(t *testing.T) {
	wantCmds := map[string][]string{
		"digest":   {"today", "customize", "no-llm", "runner", "consensus", "out"},
		"tool":     {"json"},
		"signals":  {"today"},
		"schedule": {"design"},
	}

	root := NewAubadeCmd()
	for name, flags := range wantCmds {
		c, _, err := root.Find([]string{name})
		if err != nil || c.Name() != name {
			t.Fatalf("aubade %s missing from the command tree (err=%v)", name, err)
		}
		for _, f := range flags {
			if c.Flags().Lookup(f) == nil {
				t.Errorf("aubade %s: missing --%s", name, f)
			}
		}
	}
}

func TestLabSurface(t *testing.T) {
	wantCmds := map[string][]string{
		"generate": {"seed", "today", "out"},
		"eval":     {"sabotage", "judge", "adversarial", "out"},
	}

	root := NewLabCmd()
	for name, flags := range wantCmds {
		c, _, err := root.Find([]string{name})
		if err != nil || c.Name() != name {
			t.Fatalf("aubade-lab %s missing from the command tree (err=%v)", name, err)
		}
		for _, f := range flags {
			if c.Flags().Lookup(f) == nil {
				t.Errorf("aubade-lab %s: missing --%s", name, f)
			}
		}
	}
}

// The product binary must not carry harness tooling — that boundary is an
// architectural claim in the HLD, so it gets a test.
func TestProductCarriesNoHarnessCommands(t *testing.T) {
	root := NewAubadeCmd()
	for _, forbidden := range []string{"generate", "eval"} {
		for _, c := range root.Commands() {
			if c.Name() == forbidden {
				t.Errorf("aubade must not expose the harness command %q", forbidden)
			}
		}
	}
}

// Every stub must fail loudly and name its bead. A stub that exits 0 would let
// the gate go green over an empty binary.
func TestStubsFailAndNameTheirBead(t *testing.T) {
	cases := []struct {
		root *cobra.Command
		args []string
	}{
		{NewAubadeCmd(), []string{"digest"}},
		{NewAubadeCmd(), []string{"digest", "--no-llm"}},
		{NewAubadeCmd(), []string{"schedule", "--design"}},
		{NewLabCmd(), []string{"eval", "--judge"}},
	}

	for _, tc := range cases {
		name := strings.Join(tc.args, " ")
		_, err := run(tc.root, tc.args...)
		if err == nil {
			t.Fatalf("%s: expected a not-implemented error, got nil", name)
		}
		var se *StubError
		if !errors.As(err, &se) {
			t.Fatalf("%s: expected *StubError, got %T (%v)", name, err, err)
		}
		if se.Bead == "" {
			t.Errorf("%s: stub does not name a bead", name)
		}
		if !strings.Contains(se.Error(), "not implemented yet") {
			t.Errorf("%s: unclear stub message %q", name, se.Error())
		}
	}
}

// Argument validation for the toolbox is real behaviour, not a stub: an agent
// that guesses a tool name gets the menu back.
func TestToolArgValidation(t *testing.T) {
	cases := []struct {
		args    []string
		wantSub string
	}{
		{[]string{"tool"}, "requires an extractor name"},
		{[]string{"tool", "nonsense"}, `unknown tool "nonsense"`},
		{[]string{"tool", "thread"}, "requires an argument"},
		{[]string{"tool", "search"}, "requires an argument"},
		{[]string{"tool", "commitments", "extra"}, "takes no arguments"},
		{[]string{"tool", "thread", "a", "b"}, "takes exactly one argument"},
	}

	for _, tc := range cases {
		_, err := run(NewAubadeCmd(), tc.args...)
		if err == nil {
			t.Fatalf("%v: expected a validation error", tc.args)
		}
		if !strings.Contains(err.Error(), tc.wantSub) {
			t.Errorf("%v: error %q does not contain %q", tc.args, err.Error(), tc.wantSub)
		}
	}

	// Every advertised tool name must be accepted by the validator and reach
	// the engine. `thread x` and `search x` find nothing in this corpus, which
	// is a lookup error, not a validation one — the distinction is the point.
	for _, name := range toolNames {
		args := []string{"tool", name, "--data", corpusDir, "--today", corpusDay}
		if _, needsArg := toolsTakingArg[name]; needsArg {
			args = append([]string{"tool", name, "nope"}, args[2:]...)
		}
		_, err := run(NewAubadeCmd(), args...)
		if err == nil {
			continue
		}
		for _, validation := range []string{"unknown tool", "requires an extractor", "takes no arguments"} {
			if strings.Contains(err.Error(), validation) {
				t.Errorf("tool %s rejected by argument validation: %v", name, err)
			}
		}
	}
}

// Help is graded UX. These assertions are deliberately about substance (does the
// help explain the keystone split, does it list the tools) not about wording.
func TestHelpIsSubstantive(t *testing.T) {
	out, err := run(NewAubadeCmd(), "--help")
	if err != nil {
		t.Fatalf("--help failed: %v", err)
	}
	for _, want := range []string{"digest", "tool", "signals", "schedule", "--no-llm", "cited"} {
		if !strings.Contains(out, want) {
			t.Errorf("aubade --help does not mention %q", want)
		}
	}

	labOut, err := run(NewLabCmd(), "--help")
	if err != nil {
		t.Fatalf("lab --help failed: %v", err)
	}
	for _, want := range []string{"generate", "eval", "traps.json"} {
		if !strings.Contains(labOut, want) {
			t.Errorf("aubade-lab --help does not mention %q", want)
		}
	}

	// Every tool name must appear in the tool help — that page is the agent's menu.
	toolOut, err := run(NewAubadeCmd(), "tool", "--help")
	if err != nil {
		t.Fatalf("tool --help failed: %v", err)
	}
	for _, name := range toolNames {
		if !strings.Contains(toolOut, name) {
			t.Errorf("aubade tool --help does not document %q", name)
		}
	}
}

// An AI caller gets a parseable error envelope; a human gets prose. This tests
// the JSON branch directly via AUBADE_OUTPUT, which is the supported override.
func TestRenderErrorJSONForAgents(t *testing.T) {
	t.Setenv("AUBADE_OUTPUT", "json")

	var buf bytes.Buffer
	RenderError(&buf, &StubError{Command: "aubade digest", Bead: "C2", What: "the renderer"})

	var payload struct {
		OK    bool `json:"ok"`
		Error struct {
			Kind    string `json:"kind"`
			Message string `json:"message"`
			Bead    string `json:"bead"`
		} `json:"error"`
	}
	if err := json.Unmarshal(buf.Bytes(), &payload); err != nil {
		t.Fatalf("agent-mode error is not valid JSON: %v\n%s", err, buf.String())
	}
	if payload.OK {
		t.Error("error envelope reports ok=true")
	}
	if payload.Error.Kind != "not_implemented" || payload.Error.Bead != "C2" {
		t.Errorf("unexpected error envelope: %+v", payload.Error)
	}
}

func TestRenderErrorProseForHumans(t *testing.T) {
	t.Setenv("AUBADE_OUTPUT", "human")

	var buf bytes.Buffer
	RenderError(&buf, errors.New("boom"))

	got := buf.String()
	if !strings.HasPrefix(got, "aubade: boom") {
		t.Errorf("human-mode error = %q, want prose", got)
	}
	if json.Valid(bytes.TrimSpace(buf.Bytes())) {
		t.Error("human-mode error should not be JSON")
	}
}

func TestRenderErrorNilIsSilent(t *testing.T) {
	var buf bytes.Buffer
	RenderError(&buf, nil)
	if buf.Len() != 0 {
		t.Errorf("RenderError(nil) wrote %q", buf.String())
	}
}
