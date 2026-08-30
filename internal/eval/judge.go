package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/chandrameenamohan/aubade/internal/runner"
)

// The layer-2 judge: the one axis code cannot grade.
//
// Everything in eval.go is a code grader, and code graders cover everything
// countable — did the signal fire, is the keyword on the page, does the
// citation resolve (EVAL-PRINCIPLES order of attack, step 1). What none of them
// can answer is the question the sample digest is actually judged on: *does this
// read like a page written for this person, in her voice.* A digest can score
// 20/20 on traps and still read like a database dump.
//
// So this is layer 2, and it is built to the four rules that make a model grader
// worth anything:
//
//   - **Reason before score (#2).** The schema puts `reasoning` before `grade`,
//     and the prompt asks for what works and what does not *first*. An
//     autoregressive model that emits the tag first spends the rest of its
//     answer justifying it.
//   - **Anchors (#4).** A bare "rate 1-5" floats. Each grade carries a worked
//     example written from this product's own failure modes, so the judge has a
//     calibration target rather than a vibe.
//   - **An escape hatch (#5).** `uncertain` is a legal answer and the prompt
//     says when to use it. Forced confidence on an edge case is a fabricated
//     verdict, which is exactly what the digest itself is forbidden from doing.
//   - **Consensus (#14).** Every live runner judges, and the verdict is the
//     majority. A single-runner machine gets a single-runner verdict, silently,
//     and the card names who voted.
//
// It never gates and it never enters a score. It is on demand (`--judge`),
// because a non-deterministic grader in a gate is a flaky gate, and a flaky gate
// gets bypassed.

// The grades. They are words rather than numbers on purpose: "3" invites
// averaging, and an average of a taste judgment across runs is a number with no
// referent.
const (
	GradeSample      = "reads-like-the-sample"
	GradeServiceable = "serviceable"
	GradeMachine     = "reads-like-a-machine"
	GradeUncertain   = "uncertain"
)

// judgeGrades is the closed set the judge may answer with, in order from best
// to worst, with the escape hatch last.
var judgeGrades = []string{GradeSample, GradeServiceable, GradeMachine, GradeUncertain}

// judgeSchema constrains the answer. `reasoning` is first in the property list
// and required, because the order the model writes in is the order it thinks in.
const judgeSchema = `{"type":"object","properties":{"reasoning":{"type":"string"},"grade":{"type":"string","enum":["reads-like-the-sample","serviceable","reads-like-a-machine","uncertain"]}},"required":["reasoning","grade"],"additionalProperties":false}`

// Judgment is what the judge concluded about one page.
type Judgment struct {
	// Skipped and SkipReason record a judge pass that could not run — no live
	// runner, most often. A missing judgment is reported, never inferred.
	Skipped    bool
	SkipReason string

	// Grade is the majority verdict, or GradeUncertain when the runners split.
	Grade string

	// Decided reports whether a majority was reached.
	Decided bool

	// Reason is the vote's own account of itself: who agreed, who was dropped.
	Reason string

	// Voters names the runners whose answers were counted.
	Voters []string

	// Notes are each voter's reasoning, in roster order — the part a human
	// reads when deciding whether the judge is worth listening to (#18).
	Notes []string

	// Judged names the page that was graded. A voice verdict is worthless
	// without it: the deterministic page and an agentic trial are written by
	// two different composers and read nothing like each other.
	Judged string
}

// JudgeInput is one judging pass.
type JudgeInput struct {
	// Page is the digest to judge and Judged says where it came from, for the
	// card. Voice is the drafting rules it was written under and Profile the
	// user's own tone bullets; both are handed over verbatim, because a judge
	// asked "is this in her voice" without being shown the voice is being asked
	// for its own taste.
	Page    string
	Judged  string
	Voice   string
	Profile string

	// Voters are the live runners. Empty means the pass skips.
	Voters []runner.Runner
}

// RunJudge asks every live runner the same anchored question and majority-votes.
func RunJudge(ctx context.Context, in JudgeInput) *Judgment {
	if len(in.Voters) == 0 {
		return &Judgment{
			Skipped:    true,
			SkipReason: "no model runner answered a probe on this machine, so the voice judge did not run",
			Grade:      GradeUncertain,
			Judged:     in.Judged,
		}
	}
	if strings.TrimSpace(in.Page) == "" {
		return &Judgment{
			Skipped:    true,
			SkipReason: "there is no page to judge",
			Grade:      GradeUncertain,
			Judged:     in.Judged,
		}
	}

	out := runner.Poll(ctx, in.Voters, runner.Question{
		Prompt: judgePrompt(in),
		Schema: runner.Schema{Name: "voice-judge", JSON: judgeSchema},
	}, judgeKey)

	j := &Judgment{
		Decided: out.Decided,
		Grade:   out.Key,
		Reason:  out.Reason,
		Voters:  out.Voters(),
		Judged:  in.Judged,
	}
	if !out.Decided {
		// A split among judges is exactly the case the escape hatch exists for.
		// Picking one of them by tie-break would manufacture a verdict nobody
		// held.
		j.Grade = GradeUncertain
	}
	for _, v := range out.Votes {
		j.Notes = append(j.Notes, v.Runner+": "+judgeReasoning(v.Raw))
	}
	return j
}

// judgeKey reads one vote, rejecting anything outside the closed grade set. A
// runner that invents a grade has not disagreed, it has failed to answer — and
// Poll drops a failed vote rather than tallying it.
func judgeKey(raw json.RawMessage) (string, error) {
	var a struct {
		Reasoning string `json:"reasoning"`
		Grade     string `json:"grade"`
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return "", fmt.Errorf("answer does not match the judge schema: %w", err)
	}
	grade := strings.TrimSpace(strings.ToLower(a.Grade))
	for _, g := range judgeGrades {
		if grade == g {
			// An empty reasoning field means the model answered grade-first and
			// backfilled, which is the failure mode the schema exists to
			// prevent. Drop it rather than count it.
			if strings.TrimSpace(a.Reasoning) == "" {
				return "", fmt.Errorf("answered %q with no reasoning; reason-before-score is the contract", grade)
			}
			return grade, nil
		}
	}
	return "", fmt.Errorf("answered %q, which is not one of the grades", clip(a.Grade, 40))
}

// judgeReasoning pulls one voter's prose back out for the card.
func judgeReasoning(raw json.RawMessage) string {
	var a struct {
		Reasoning string `json:"reasoning"`
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return "(unreadable answer)"
	}
	return clip(strings.TrimSpace(a.Reasoning), 400)
}

// judgePrompt is the anchored, reason-before-score question. Every runner is
// asked this byte-for-byte identical text.
func judgePrompt(in JudgeInput) string {
	var b strings.Builder
	b.WriteString(`You are grading one axis of a morning digest, and one only: does it read like a
page written for this person, in her voice.

You are NOT grading whether the facts are right, whether anything is missing, or
whether the citations resolve. Those are checked by code elsewhere and are none
of your business here. Judge the writing.

Work in this order, and write it in this order:

1. Quote the two or three lines that most decide your answer.
2. Say what works about them and what does not.
3. Only then, give the grade.

The grades, with a worked example of each:

  "reads-like-the-sample" — the opening line names the one thing that matters
    and why it matters now; the sentences are short and human; drafts are in her
    register. Example of this grade: "Unkept promise to Marcus Webb on the cap
    table — overdue. You answered a direct ask with a deadline and nothing else,
    and the deadline has passed." A person wrote that.

  "serviceable" — accurate, readable, nothing embarrassing, but generic. It
    could be about anyone's morning. Example: "There are 3 urgent items and 2
    calendar conflicts today. Please review the items below."

  "reads-like-a-machine" — field names, ids, priority codes or extractor
    vocabulary leaking onto the page; sentences assembled from a template with
    the seams visible; a draft that sounds like nobody. Example: "SIGNAL
    commitments:m-capt-02 [P0] section=urgent-today confidence=certain — promise
    unkept, deadline exceeded."

  "uncertain" — use this, without hesitation, when the page is genuinely
    between two grades, when it is too short to judge, or when you would be
    guessing. An honest "I cannot tell" is worth more here than a confident
    verdict you do not hold. It is not a failure to answer; it is an answer.

`)

	if v := strings.TrimSpace(in.Voice); v != "" {
		b.WriteString("The drafting voice the page is written under:\n\n<voice>\n")
		b.WriteString(clip(v, 4000))
		b.WriteString("\n</voice>\n\n")
	}
	if p := strings.TrimSpace(in.Profile); p != "" {
		b.WriteString("The user's own profile, whose tone rules override the voice above wherever they speak:\n\n<profile>\n")
		b.WriteString(clip(p, 4000))
		b.WriteString("\n</profile>\n\n")
	}

	b.WriteString("The page to judge:\n\n<digest>\n")
	b.WriteString(in.Page)
	b.WriteString("\n</digest>\n\nWrite `reasoning` first, then `grade`.")
	return b.String()
}
