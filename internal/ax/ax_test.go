package ax

import "testing"

import "github.com/sageox/agentx"

// The load-bearing guarantee of this package: an unknown caller — no agent env
// vars at all — degrades to human mode. Detection is progressive enhancement
// (HLD §7), so this is the case that must never regress.
func TestUnknownCallerDegradesToHuman(t *testing.T) {
	env := agentx.NewMockEnvironment(map[string]string{})

	name, isAgent := CallerWithEnv(env)
	if isAgent {
		t.Fatalf("unknown caller reported as agent (name=%q)", name)
	}
	if name != "human" {
		t.Fatalf("Caller() name = %q, want %q", name, "human")
	}
	if got := OutputModeWithEnv(env); got != ModeHuman {
		t.Fatalf("OutputMode() = %q, want %q", got, ModeHuman)
	}
}

// A nil environment is the pathological case (a caller wired us up wrong). It
// must still be human mode, not a panic.
func TestNilEnvironmentDegradesToHuman(t *testing.T) {
	name, isAgent := CallerWithEnv(nil)
	if isAgent || name != "human" {
		t.Fatalf("nil env: got (%q, %v), want (\"human\", false)", name, isAgent)
	}
	if got := OutputModeWithEnv(nil); got != ModeHuman {
		t.Fatalf("nil env OutputMode() = %q, want %q", got, ModeHuman)
	}
}

// Noise that looks agent-adjacent but is not a detection marker must not flip
// us into JSON mode — false positives make aubade unusable for humans.
func TestUnrelatedEnvIsNotAnAgent(t *testing.T) {
	env := agentx.NewMockEnvironment(map[string]string{
		"TERM":       "xterm-256color",
		"EDITOR":     "vim",
		"CLAUDECODE": "0", // explicitly off
		"AGENT_ENV":  "",
	})
	if name, isAgent := CallerWithEnv(env); isAgent {
		t.Fatalf("unrelated env detected as agent %q", name)
	}
}

func TestDetectsClaudeCode(t *testing.T) {
	env := agentx.NewMockEnvironment(map[string]string{"CLAUDECODE": "1"})

	name, isAgent := CallerWithEnv(env)
	if !isAgent {
		t.Fatal("CLAUDECODE=1 not detected as an agent caller")
	}
	if name == "" || name == "human" {
		t.Fatalf("detected agent has unusable name %q", name)
	}
	if got := OutputModeWithEnv(env); got != ModeJSON {
		t.Fatalf("agent caller OutputMode() = %q, want %q", got, ModeJSON)
	}
}

// The fallback table covers harnesses agentx may not know. AUBADE_AGENT is the
// explicit escape hatch any caller can set.
func TestFallbackMarkerDetectsUnknownHarness(t *testing.T) {
	env := agentx.NewMockEnvironment(map[string]string{"AUBADE_AGENT": "1"})
	if _, isAgent := CallerWithEnv(env); !isAgent {
		t.Fatal("AUBADE_AGENT=1 should force agent detection")
	}
}

func TestOutputOverrideBeatsDetection(t *testing.T) {
	agentEnv := agentx.NewMockEnvironment(map[string]string{
		"CLAUDECODE":    "1",
		"AUBADE_OUTPUT": "human",
	})
	if got := OutputModeWithEnv(agentEnv); got != ModeHuman {
		t.Fatalf("AUBADE_OUTPUT=human ignored: got %q", got)
	}

	humanEnv := agentx.NewMockEnvironment(map[string]string{"AUBADE_OUTPUT": "json"})
	if got := OutputModeWithEnv(humanEnv); got != ModeJSON {
		t.Fatalf("AUBADE_OUTPUT=json ignored: got %q", got)
	}

	// Garbage in the override is ignored, not fatal.
	junkEnv := agentx.NewMockEnvironment(map[string]string{"AUBADE_OUTPUT": "yaml-please"})
	if got := OutputModeWithEnv(junkEnv); got != ModeHuman {
		t.Fatalf("unrecognised AUBADE_OUTPUT should fall through to detection: got %q", got)
	}
}
