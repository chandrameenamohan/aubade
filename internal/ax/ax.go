// Package ax is aubade's Agent-Experience layer: it answers one question —
// "is a human or an AI coding agent on the other end of this pipe?" — and turns
// that answer into an output mode.
//
// Detection is progressive enhancement, never a requirement (HLD §7): if
// anything about detection fails, is unavailable, or is ambiguous, we degrade to
// human mode. A human reading JSON is a papercut; an agent parsing prose is a
// broken contract, but a crashed CLI is worse than both.
//
// Implementation note (bead A1): this wraps github.com/sageox/agentx v0.2.0,
// the real library. Its API is agentx.NewDetectorWithEnv(env).Detect(ctx) plus
// the blank import of agentx/setup to populate the default agent registry — not
// the shape we guessed at planning time, so this package adapts to the library
// rather than the other way round. Everything the rest of aubade sees is the
// two-function surface below, so swapping the detector is a one-file change.
package ax

import (
	"context"
	"os"
	"strings"
	"time"

	"github.com/sageox/agentx"
	_ "github.com/sageox/agentx/setup" // registers the built-in agent detectors
)

// Mode is how aubade should shape its output for the current caller.
type Mode string

const (
	// ModeHuman renders markdown/prose for a person at a terminal.
	ModeHuman Mode = "human"
	// ModeJSON renders machine-parseable output for an AI agent caller.
	ModeJSON Mode = "json"
)

func (m Mode) String() string { return string(m) }

// detectTimeout bounds detection. Some agentx detectors may shell out; aubade
// must never hang in a Stop hook or a 6am cron because of a probe.
const detectTimeout = 2 * time.Second

// Caller reports the human-readable name of the AI coding agent invoking
// aubade, and whether an agent was detected at all.
//
// When no agent is detected — or detection errors, panics, or times out — it
// returns ("human", false). Callers can rely on that: the boolean is the only
// thing that ever gates behaviour.
func Caller() (string, bool) {
	return CallerWithEnv(agentx.NewSystemEnvironment())
}

// CallerWithEnv is Caller against an injected agentx.Environment. It exists so
// detection can be unit-tested (agentx.NewMockEnvironment) without mutating the
// process environment, and so a future connector can detect on behalf of
// another process.
func CallerWithEnv(env agentx.Environment) (name string, isAgent bool) {
	// Detection must never take the CLI down with it. A third-party detector
	// that panics degrades to human mode like any other failure.
	defer func() {
		if r := recover(); r != nil {
			name, isAgent = "human", false
		}
	}()

	if env == nil {
		return "human", false
	}

	ctx, cancel := context.WithTimeout(context.Background(), detectTimeout)
	defer cancel()

	agent, err := agentx.NewDetectorWithEnv(env).Detect(ctx)
	if err != nil || agent == nil {
		// Second layer: agentx knows a lot of agents, but an unknown harness
		// that sets an obvious marker still deserves JSON. This is deliberately
		// a tiny table, not a competing detector.
		if n, ok := fallbackDetect(env); ok {
			return n, true
		}
		return "human", false
	}

	n := strings.TrimSpace(agent.Name())
	if n == "" {
		n = string(agent.Type())
	}
	if n == "" {
		return "human", false
	}
	return n, true
}

// fallbackEnvMarkers are env vars whose mere presence (non-empty) identifies an
// AI caller. Kept as a backstop for harnesses agentx does not yet know about;
// agentx remains the primary detector.
var fallbackEnvMarkers = []struct {
	env  string
	name string
}{
	{"CLAUDECODE", "Claude Code"},
	{"CLAUDE_CODE_ENTRYPOINT", "Claude Code"},
	{"CURSOR_TRACE_ID", "Cursor"},
	{"AIDER_MODEL", "Aider"},
	{"AUBADE_AGENT", "agent"}, // explicit opt-in escape hatch for any caller
}

func fallbackDetect(env agentx.Environment) (string, bool) {
	for _, m := range fallbackEnvMarkers {
		if v := strings.TrimSpace(env.GetEnv(m.env)); v != "" && v != "0" {
			return m.name, true
		}
	}
	// AGENT_ENV is the cross-tool convention (ox sets it in hooks).
	if v := strings.TrimSpace(env.GetEnv("AGENT_ENV")); v != "" {
		return v, true
	}
	return "", false
}

// OutputMode returns the output shape for the current caller: ModeJSON for a
// detected AI agent, ModeHuman otherwise.
//
// AUBADE_OUTPUT overrides detection entirely (values: "json", "human"), so a
// human can force either mode and an agent can ask for prose. An unrecognised
// value is ignored rather than fatal — detection never fails loudly.
func OutputMode() Mode {
	return outputMode(os.Getenv("AUBADE_OUTPUT"), func() bool {
		_, isAgent := Caller()
		return isAgent
	})
}

// OutputModeWithEnv is OutputMode against an injected environment.
func OutputModeWithEnv(env agentx.Environment) Mode {
	override := ""
	if env != nil {
		override = env.GetEnv("AUBADE_OUTPUT")
	}
	return outputMode(override, func() bool {
		_, isAgent := CallerWithEnv(env)
		return isAgent
	})
}

func outputMode(override string, isAgent func() bool) Mode {
	switch strings.ToLower(strings.TrimSpace(override)) {
	case "json":
		return ModeJSON
	case "human", "markdown", "md":
		return ModeHuman
	}
	if isAgent() {
		return ModeJSON
	}
	return ModeHuman
}
