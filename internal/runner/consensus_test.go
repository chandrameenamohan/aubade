package runner_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/chandrameenamohan/aubade/internal/runner"
	"github.com/chandrameenamohan/aubade/internal/runner/runnertest"
)

// The vote math is the part of consensus that has to be right at three in the
// morning with two runners installed and one of them broken, so it is tested
// away from anything that could make a network call.

func vote(name, key string) runner.Vote { return runner.Vote{Runner: name, Key: key} }

func failed(name string) runner.Vote {
	return runner.Vote{Runner: name, Err: errors.New("401 Unauthorized")}
}

func TestTally(t *testing.T) {
	cases := []struct {
		name     string
		votes    []runner.Vote
		decided  bool
		key      string
		voters   int
		dropped  int
		inReason string
	}{{
		// SPEC §5: one runner installed means single-runner, silently. On the
		// development machine this is the default path, not the exotic one.
		name:     "one runner decides alone",
		votes:    []runner.Vote{vote("claude", "urgent")},
		decided:  true,
		key:      "urgent",
		voters:   1,
		inReason: "answered alone",
	}, {
		name:     "unanimous",
		votes:    []runner.Vote{vote("claude", "urgent"), vote("codex", "urgent")},
		decided:  true,
		key:      "urgent",
		voters:   2,
		inReason: "all 2 runners agreed",
	}, {
		// A tie has no majority, and SPEC §5 routes that to the honesty layer
		// rather than to a coin flip. This is why an even roster is safe.
		name:     "two runners split",
		votes:    []runner.Vote{vote("claude", "urgent"), vote("codex", "later")},
		decided:  false,
		voters:   2,
		inReason: "no majority",
	}, {
		name:     "majority of three",
		votes:    []runner.Vote{vote("claude", "a"), vote("codex", "a"), vote("gemini", "b")},
		decided:  true,
		key:      "a",
		voters:   3,
		inReason: "2 of 3 runners agreed",
	}, {
		name:    "three-way split has no majority",
		votes:   []runner.Vote{vote("claude", "a"), vote("codex", "b"), vote("gemini", "c")},
		decided: false,
		voters:  3,
	}, {
		// A 401 is not an opinion (learning-tests/03): the dead runner is
		// dropped, and the one that answered decides.
		name:     "a dead runner is dropped, not counted as dissent",
		votes:    []runner.Vote{vote("claude", "urgent"), failed("codex")},
		decided:  true,
		key:      "urgent",
		voters:   1,
		dropped:  1,
		inReason: "dropped",
	}, {
		name:     "nobody answered",
		votes:    []runner.Vote{failed("claude"), failed("codex")},
		decided:  false,
		voters:   0,
		dropped:  2,
		inReason: "no runner answered",
	}}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := runner.Tally(tc.votes)
			if got.Decided != tc.decided {
				t.Fatalf("Decided = %v, want %v (reason: %s)", got.Decided, tc.decided, got.Reason)
			}
			if got.Key != tc.key {
				t.Errorf("Key = %q, want %q", got.Key, tc.key)
			}
			if len(got.Votes) != tc.voters {
				t.Errorf("counted %d votes, want %d", len(got.Votes), tc.voters)
			}
			if len(got.Failed) != tc.dropped {
				t.Errorf("dropped %d runners, want %d", len(got.Failed), tc.dropped)
			}
			if tc.inReason != "" && !strings.Contains(got.Reason, tc.inReason) {
				t.Errorf("Reason = %q, want it to mention %q", got.Reason, tc.inReason)
			}
			if got.Reason == "" {
				t.Error("every outcome must be able to say why it came out that way")
			}
		})
	}
}

// The reason on an undecided outcome is shown to the reader under "I'm not
// sure", so it has to name both sides and count them.
func TestUndecidedOutcomeShowsTheSplit(t *testing.T) {
	out := runner.Tally([]runner.Vote{vote("claude", "needs you today"), vote("codex", "does not need you today")})
	for _, want := range []string{"needs you today", "does not need you today", "no majority"} {
		if !strings.Contains(out.Reason, want) {
			t.Errorf("reason %q does not mention %q", out.Reason, want)
		}
	}
}

// A decided outcome names its voters in roster order, because the page states
// who agreed — "a model said so" and "both models said so" are different claims.
func TestDecidedOutcomeNamesItsVotersInOrder(t *testing.T) {
	out := runner.Tally([]runner.Vote{
		{Runner: "claude", Key: "urgent", Raw: json.RawMessage(`{"why":"board meets today"}`)},
		{Runner: "codex", Key: "urgent", Raw: json.RawMessage(`{"why":"the deadline is today"}`)},
	})
	if !out.Decided {
		t.Fatalf("expected a decision, got %s", out.Reason)
	}
	if voters := strings.Join(out.Voters(), ","); voters != "claude,codex" {
		t.Errorf("Voters() = %q, want both in roster order", voters)
	}
}

// Poll asks every runner the identical question — a vote across differently
// worded questions is not a vote — and drops the ones that cannot answer.
func TestPollAsksEveryRunnerTheSameQuestionAndDropsFailures(t *testing.T) {
	good := &runnertest.Runner{RunnerName: "claude", Answers: []string{`{"pick":"a"}`}}
	broken := &runnertest.Runner{RunnerName: "codex", AskErr: errors.New("401 Unauthorized")}

	q := runner.Question{Prompt: "which one?", Schema: runner.Schema{Name: "pick", JSON: `{"type":"object"}`}}
	out := runner.Poll(context.Background(), []runner.Runner{good, broken}, q, func(raw json.RawMessage) (string, error) {
		var a struct {
			Pick string `json:"pick"`
		}
		if err := json.Unmarshal(raw, &a); err != nil {
			return "", err
		}
		return a.Pick, nil
	})

	if !out.Decided || out.Key != "a" {
		t.Fatalf("outcome = %+v, want the live runner to decide", out)
	}
	if got := good.LastPrompt(); got != q.Prompt {
		t.Errorf("prompt = %q, want the question verbatim", got)
	}
	if got := broken.LastPrompt(); got != q.Prompt {
		t.Errorf("the failing runner was asked %q, want the identical question", got)
	}
	if len(out.Failed) != 1 || out.Failed[0].Runner != "codex" {
		t.Errorf("Failed = %+v, want codex dropped", out.Failed)
	}
}

// A malformed answer is not a dissent either: a runner that ignores the schema
// has failed to answer, and Poll treats it exactly like the one that 401'd.
func TestPollDropsAnAnswerThatIgnoresTheSchema(t *testing.T) {
	sane := &runnertest.Runner{RunnerName: "claude", Answers: []string{`{"pick":"a"}`}}
	garbled := &runnertest.Runner{RunnerName: "codex", Answers: []string{`{"unexpected":true}`}}

	out := runner.Poll(context.Background(), []runner.Runner{sane, garbled}, runner.Question{Prompt: "q"},
		func(raw json.RawMessage) (string, error) {
			var a struct {
				Pick string `json:"pick"`
			}
			_ = json.Unmarshal(raw, &a)
			if a.Pick == "" {
				return "", errors.New("no pick in the answer")
			}
			return a.Pick, nil
		})

	if !out.Decided || out.Key != "a" {
		t.Fatalf("outcome = %+v, want the schema-obeying answer to decide", out)
	}
	if len(out.Failed) != 1 {
		t.Fatalf("Failed = %+v, want the garbled answer dropped", out.Failed)
	}
}
