package runner_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/chandrameenamohan/aubade/internal/runner"
	"github.com/chandrameenamohan/aubade/internal/runner/runnertest"
)

// The registry is what keeps the engine ignorant of vendors, so what is asserted
// here is the contract a future SDK-backed runner binds to: register by name,
// resolve by name, and be told the menu when the name is wrong.

func TestRegistryResolvesByNameAndListsTheMenu(t *testing.T) {
	reg := runnertest.Registry(
		&runnertest.Runner{RunnerName: "claude", Orchestrates: true},
		&runnertest.Runner{RunnerName: "codex"},
	)

	if got := strings.Join(reg.Names(), ","); got != "claude,codex" {
		t.Errorf("Names() = %q, want registration order", got)
	}
	r, err := reg.New("codex")
	if err != nil {
		t.Fatalf("New(codex): %v", err)
	}
	if r.Name() != "codex" {
		t.Errorf("New(codex).Name() = %q", r.Name())
	}

	_, err = reg.New("gemni")
	if err == nil {
		t.Fatal("a mistyped runner name should be an error")
	}
	for _, want := range []string{`"gemni"`, "claude", "codex"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q — the menu is the whole answer", err, want)
		}
	}
}

// The shipped registry is the one the product uses; gemini is deliberately not
// in it, because its headless syntax has never been verified against the real
// binary (learning-tests/05) and an unprobed runner is not a voter.
func TestDefaultRegistryShipsTheVerifiedRunnersOnly(t *testing.T) {
	names := runner.Default().Names()
	if strings.Join(names, ",") != "claude,codex" {
		t.Fatalf("Default().Names() = %v, want exactly the two runners with learning tests behind them", names)
	}

	claude, err := runner.Default().New("claude")
	if err != nil {
		t.Fatalf("New(claude): %v", err)
	}
	if !claude.CanOrchestrate() {
		t.Error("claude must be able to drive the toolbox loop; it is the default orchestrator")
	}
	codex, err := runner.Default().New("codex")
	if err != nil {
		t.Fatalf("New(codex): %v", err)
	}
	if codex.CanOrchestrate() {
		t.Error("codex must not claim it can drive the toolbox loop: that boundary has never been measured")
	}
}

// Detection is the learning tests' sharpest finding turned into code: presence
// is not liveness. A runner on PATH that cannot answer is dead, not live, and
// the footer has to be able to tell a reader which.
func TestDetectSeparatesLiveDeadAndAbsent(t *testing.T) {
	live := &runnertest.Runner{RunnerName: "claude", Orchestrates: true}
	dead := &runnertest.Runner{RunnerName: "codex", ProbeErr: errors.New("codex probe: 401 Unauthorized")}
	absent := &runnertest.Runner{RunnerName: "gemini", Missing: true}

	roster := runnertest.Registry(live, dead, absent).Detect(context.Background())

	if got := strings.Join(roster.LiveNames(), ","); got != "claude" {
		t.Fatalf("LiveNames() = %q, want only the runner that answered", got)
	}
	if _, ok := roster.Get("codex"); ok {
		t.Error("a runner that failed its probe must not be handed out as live")
	}

	want := map[string]runner.State{"claude": runner.Live, "codex": runner.Dead, "gemini": runner.Absent}
	for name, state := range want {
		st, ok := roster.StatusOf(name)
		if !ok {
			t.Fatalf("no status for %s", name)
		}
		if st.State != state {
			t.Errorf("%s is %q, want %q", name, st.State, state)
		}
	}

	// SPEC §5 promises the footer names who voted; "codex was broken" is a fact
	// the reader is owed, and so is "gemini was never here".
	desc := roster.Describe()
	for _, want := range []string{"answered: claude", "codex unavailable", "401", "gemini not installed"} {
		if !strings.Contains(desc, want) {
			t.Errorf("Describe() = %q, missing %q", desc, want)
		}
	}
}

// An empty roster is a real state on a machine with nothing installed, and it
// must be reportable rather than a panic.
func TestDetectWithNothingInstalled(t *testing.T) {
	roster := runnertest.Registry(
		&runnertest.Runner{RunnerName: "claude", Missing: true},
		&runnertest.Runner{RunnerName: "codex", Missing: true},
	).Detect(context.Background())

	if n := len(roster.Live()); n != 0 {
		t.Fatalf("Live() = %d runners, want none", n)
	}
	if !strings.Contains(roster.Describe(), "answered: none") {
		t.Errorf("Describe() = %q, want it to say plainly that nobody answered", roster.Describe())
	}
}
