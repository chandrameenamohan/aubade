package eval

import (
	"context"
	"strings"
	"testing"

	"github.com/chandrameenamohan/aubade/internal/runner"
	"github.com/chandrameenamohan/aubade/internal/runner/runnertest"
)

// Nothing here calls a model. The judge's interesting behaviour is in what it
// accepts as an answer and what it does with disagreement, and both are
// testable against scripted runners — which is the only kind this repo's tests
// are allowed to talk to.

func answer(grade, reasoning string) string {
	return `{"reasoning":"` + reasoning + `","grade":"` + grade + `"}`
}

func judgeWith(t *testing.T, runners ...runner.Runner) *Judgment {
	t.Helper()
	return RunJudge(context.Background(), JudgeInput{
		Page:    "# Daily Digest\nsomething to judge.",
		Voice:   "write like you talk",
		Profile: "- lowercase greetings",
		Voters:  runners,
	})
}

// One runner decides alone, silently — which is the roster on the development
// machine, so it is the well-tested path rather than the fallback nobody runs.
func TestJudgeWithOneRunnerDecidesAlone(t *testing.T) {
	j := judgeWith(t, &runnertest.Runner{RunnerName: "claude", Answers: []string{answer(GradeSample, "the opening line names the one thing")}})
	if !j.Decided || j.Grade != GradeSample {
		t.Fatalf("grade = %q decided = %v, want %q decided", j.Grade, j.Decided, GradeSample)
	}
	if len(j.Notes) != 1 || !strings.Contains(j.Notes[0], "opening line") {
		t.Errorf("the voter's reasoning must reach the card, got %v", j.Notes)
	}
}

// Two judges that disagree produce "uncertain", not a coin flip. Manufacturing
// a verdict nobody held is exactly what the escape hatch exists to prevent.
func TestJudgeSplitFallsToUncertain(t *testing.T) {
	j := judgeWith(t,
		&runnertest.Runner{RunnerName: "claude", Answers: []string{answer(GradeSample, "reads well")}},
		&runnertest.Runner{RunnerName: "codex", Answers: []string{answer(GradeMachine, "field names leak")}},
	)
	if j.Decided {
		t.Fatal("a 1–1 split has no majority")
	}
	if j.Grade != GradeUncertain {
		t.Errorf("grade = %q, want %q", j.Grade, GradeUncertain)
	}
}

// A runner that cannot answer is dropped, not counted as a dissent — the same
// treatment a 401 gets everywhere else in this codebase, and for the same
// reason.
func TestJudgeDropsARunnerThatCannotAnswer(t *testing.T) {
	j := judgeWith(t,
		&runnertest.Runner{RunnerName: "claude", Answers: []string{answer(GradeServiceable, "accurate but generic")}},
		&runnertest.Runner{RunnerName: "codex", AskErr: runner.ErrEmptyAnswer},
	)
	if !j.Decided || j.Grade != GradeServiceable {
		t.Fatalf("grade = %q decided = %v, want a decided %q", j.Grade, j.Decided, GradeServiceable)
	}
	if len(j.Voters) != 1 || j.Voters[0] != "claude" {
		t.Errorf("voters = %v, want only claude", j.Voters)
	}
}

// Reason before score is the contract, not a suggestion (#2). A grade with no
// reasoning behind it was written the wrong way round, and a vote like that is
// dropped rather than counted.
func TestJudgeRejectsAGradeWithNoReasoning(t *testing.T) {
	cases := map[string]string{
		"no reasoning":  `{"reasoning":"","grade":"serviceable"}`,
		"unknown grade": `{"reasoning":"it is fine","grade":"7/10"}`,
		"not an object": `"serviceable"`,
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := judgeKey([]byte(raw)); err == nil {
				t.Errorf("%s must not be counted as a vote", name)
			}
		})
	}
}

// With no live runner there is no judgment, and the card says so rather than
// printing a grade nobody gave.
func TestJudgeSkipsWithNoRunners(t *testing.T) {
	j := judgeWith(t)
	if !j.Skipped || j.Grade != GradeUncertain {
		t.Fatalf("skipped = %v grade = %q, want a skip", j.Skipped, j.Grade)
	}
	if !strings.Contains(j.SkipReason, "no model runner") {
		t.Errorf("the skip must say why: %q", j.SkipReason)
	}
}

// The prompt is the grader, so its load-bearing parts are asserted: the anchors,
// the escape hatch, the order it demands, and the page itself.
func TestJudgePromptIsAnchoredAndOrdered(t *testing.T) {
	fake := &runnertest.Runner{RunnerName: "claude", Answers: []string{answer(GradeSample, "good")}}
	judgeWith(t, fake)

	prompts := fake.Prompts()
	if len(prompts) != 1 {
		t.Fatalf("asked %d questions, want 1", len(prompts))
	}
	for _, want := range []string{
		"worked example",                // anchors (#4)
		"Only then, give the grade",     // reason before score (#2)
		`"uncertain" — use this`,        // the escape hatch (#5)
		"NOT grading whether the facts", // the axis it is not judging
		"write like you talk",           // the base voice
		"lowercase greetings",           // the profile's tone rules
		"something to judge",            // the page
	} {
		if !strings.Contains(prompts[0], want) {
			t.Errorf("the judge prompt is missing %q", want)
		}
	}

	schemas := fake.Schemas()
	if len(schemas) != 1 || !strings.Contains(schemas[0].JSON, `"reasoning"`) {
		t.Fatal("the judge must constrain its answer with a schema carrying reasoning")
	}
	if strings.Index(schemas[0].JSON, "reasoning") > strings.Index(schemas[0].JSON, `"grade"`) {
		t.Error("reasoning must come before grade in the schema; the order is the point")
	}
}
