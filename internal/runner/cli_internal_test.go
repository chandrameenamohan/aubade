package runner

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// These are the parts of the two CLI runners that can be graded without calling
// a model: the transport contracts the learning tests pinned down. Each case
// below is a finding from learning-tests/ turned into an assertion, so a CLI
// that changes its shape under us fails here rather than in a digest.

// learning-tests/01, finding A1: `.result` is a JSON *string*, so a
// schema-constrained answer is decoded twice. Reading it once yields a type
// error rather than a missing field, which is why this is worth a test.
func TestClaudeAnswerDecodesTheResultStringTwice(t *testing.T) {
	envelope := `{"subtype":"success","num_turns":2,"duration_ms":1200,
	  "result":"{\"answer\":42,\"confidence\":\"certain\"}"}`

	got, err := claudeAnswer([]byte(envelope))
	if err != nil {
		t.Fatalf("claudeAnswer: %v", err)
	}
	if string(got) != `{"answer":42,"confidence":"certain"}` {
		t.Errorf("answer = %s, want the inner document", got)
	}
}

func TestClaudeAnswerRejectsWhatCannotBeVoted(t *testing.T) {
	cases := []struct {
		name     string
		envelope string
		want     string
	}{
		{"not an envelope at all", "Error: --json-schema is not valid JSON", "no JSON envelope"},
		{"the run failed", `{"subtype":"error_during_execution","result":"boom"}`, "error_during_execution"},
		{"exited 0 saying nothing", `{"subtype":"success","result":"  "}`, "no answer"},
		{"the answer is prose", `{"subtype":"success","result":"forty two"}`, "not JSON"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := claudeAnswer([]byte(tc.envelope))
			if err == nil {
				t.Fatal("expected an error: a vote needs a value, not prose")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

// The transcript is how the loop is counted, because there is no --max-turns on
// this CLI (learning-tests/01, finding A3) and asking the model how many calls
// it made is not evidence (learning-tests/02, finding B3).
func TestReadClaudeStreamCountsToolCallsAndKeepsThePage(t *testing.T) {
	stream := strings.Join([]string{
		`{"type":"system","subtype":"init"}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Bash","input":{"command":"/bin/aubade tool commitments --json"}}]}}`,
		`not json at all, which is noise rather than a finding`,
		`{"type":"user","message":{"content":[{"type":"tool_result","content":"[]"}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Bash","input":{"command":"/bin/aubade tool staleness --json"}}]}}`,
		`{"type":"result","subtype":"success","num_turns":4,"duration_ms":8000,"result":"# Daily Digest\n"}`,
	}, "\n")

	var tee strings.Builder
	run, err := readClaudeStream(strings.NewReader(stream), &tee, MaxToolCalls, func() {
		t.Error("the loop was inside its budget; nothing should have been stopped")
	})
	if err != nil {
		t.Fatalf("readClaudeStream: %v", err)
	}
	if run.ToolCalls != 2 {
		t.Errorf("ToolCalls = %d, want 2", run.ToolCalls)
	}
	if len(run.Commands) != 2 || !strings.Contains(run.Commands[0], "tool commitments") {
		t.Errorf("Commands = %v, want the receipt for both calls", run.Commands)
	}
	if run.Turns != 4 || run.Duration == 0 {
		t.Errorf("Turns/Duration = %d/%s, want what the runner reported", run.Turns, run.Duration)
	}
	if run.Markdown != "# Daily Digest\n" {
		t.Errorf("Markdown = %q", run.Markdown)
	}
	if !strings.Contains(tee.String(), "tool_use") {
		t.Error("the transcript should be teed out verbatim for the eval harness")
	}
}

// Bounded turns is aubade's job, and "bounded" means the process is stopped
// rather than the overrun reported after it has been paid for.
func TestReadClaudeStreamStopsALoopThatWillNotConverge(t *testing.T) {
	var lines []string
	for i := 0; i < 6; i++ {
		lines = append(lines, `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Bash","input":{"command":"/bin/aubade tool search x"}}]}}`)
	}

	stopped := 0
	run, err := readClaudeStream(strings.NewReader(strings.Join(lines, "\n")), nil, 3, func() { stopped++ })

	if !errors.Is(err, ErrToolBudget) {
		t.Fatalf("err = %v, want ErrToolBudget", err)
	}
	if stopped != 1 {
		t.Errorf("the loop was stopped %d times, want exactly once", stopped)
	}
	if run.ToolCalls < 4 {
		t.Errorf("ToolCalls = %d, want the overrun to be reported", run.ToolCalls)
	}
}

func TestReadClaudeStreamReportsAFailedLoop(t *testing.T) {
	stream := `{"type":"result","subtype":"error_max_turns","is_error":true,"result":"gave up"}`
	if _, err := readClaudeStream(strings.NewReader(stream), nil, MaxToolCalls, func() {}); err == nil ||
		!strings.Contains(err.Error(), "error_max_turns") {
		t.Fatalf("err = %v, want the failure named", err)
	}
}

// learning-tests/02, finding B1: the allowlist is the entire sandbox. An
// allowlist that is subtly wrong fails open, which is the worst direction, so
// its exact text is pinned.
func TestAllowSpecGrantsTheToolboxAndNothingElse(t *testing.T) {
	if got := AllowSpec("/opt/aubade/bin/aubade", "tool"); got != "Bash(/opt/aubade/bin/aubade tool:*)" {
		t.Errorf("AllowSpec = %q", got)
	}
	if got := AllowSpec("/opt/aubade/bin/aubade", ""); got != "Bash(/opt/aubade/bin/aubade tool:*)" {
		t.Errorf("AllowSpec with no prefix = %q, want it to default to the toolbox", got)
	}
}

// An orchestration with no binary to allowlist would be an unbounded loop, so it
// is refused before the process starts rather than run with everything granted.
func TestClaudeRefusesToOrchestrateWithoutAnAllowlist(t *testing.T) {
	r := &ClaudeRunner{Bin: "sh"} // exists on PATH, so Installed() is not the guard under test
	_, err := r.Orchestrate(context.Background(), Goal{Prompt: "compose"})
	if err == nil || !strings.Contains(err.Error(), "allowlist") {
		t.Fatalf("err = %v, want a refusal naming the missing allowlist", err)
	}
}

// codex votes; it does not drive the toolbox. That is a capability statement,
// and the caller is expected to branch on it rather than parse a message.
func TestCodexIsAskOnly(t *testing.T) {
	r := NewCodex()
	if r.CanOrchestrate() {
		t.Fatal("codex must not claim it can orchestrate")
	}
	if _, err := r.Orchestrate(context.Background(), Goal{}); !errors.Is(err, ErrNoOrchestrate) {
		t.Fatalf("err = %v, want ErrNoOrchestrate", err)
	}
}

// A runner that is not installed says so with the sentinel the CLI branches on,
// without making a call to find out.
func TestUninstalledRunnersFailFast(t *testing.T) {
	missing := "aubade-no-such-runner-binary"
	for _, r := range []Runner{&ClaudeRunner{Bin: missing}, &CodexRunner{Bin: missing}} {
		if r.Installed() {
			t.Fatalf("%s: Installed() is true for a binary that does not exist", r.Name())
		}
		if err := r.Probe(context.Background()); !errors.Is(err, ErrNotInstalled) {
			t.Errorf("%s: Probe err = %v, want ErrNotInstalled", r.Name(), err)
		}
		if _, err := r.Ask(context.Background(), Question{Prompt: "?"}); !errors.Is(err, ErrNotInstalled) {
			t.Errorf("%s: Ask err = %v, want ErrNotInstalled", r.Name(), err)
		}
	}
}
