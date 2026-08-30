package agentic

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/chandrameenamohan/aubade/internal/model"
	"github.com/chandrameenamohan/aubade/internal/runner/runnertest"
)

// Both decision points are graded on the same two questions: does agreement
// change the page, and does disagreement route to honesty rather than to a coin
// flip.

// voterFor scripts a runner's answers for the two decision points, in the order
// Decide runs them: urgency first, then the one-thing pick.
func voterFor(name string, answers ...string) *runnertest.Runner {
	return &runnertest.Runner{RunnerName: name, Answers: answers}
}

func find(t *testing.T, ss model.Signals, id string) model.Signal {
	t.Helper()
	for _, s := range ss {
		if s.ID == id {
			return s
		}
	}
	t.Fatalf("no signal %q in %d signals", id, len(ss))
	return model.Signal{}
}

func decisionAt(t *testing.T, ds []Decision, point string) Decision {
	t.Helper()
	for _, d := range ds {
		if d.Point == point {
			return d
		}
	}
	t.Fatalf("no decision at %q; got %d decisions", point, len(ds))
	return Decision{}
}

// Agreement resolves the ambiguity the toolbox could not, and says on the page
// who agreed — "a model said so" and "both models said so" are different claims.
func TestUrgencyAgreementPromotesTheItemAndNamesTheVoters(t *testing.T) {
	a := voterFor("claude", `{"urgent":true,"why":"the deadline is today"}`, `{"pick":"commitments:e-001","why":"money"}`)
	b := voterFor("codex", `{"urgent":true,"why":"it expires today"}`, `{"pick":"commitments:e-001","why":"money"}`)

	composed, decisions := Decide(context.Background(), testInput(nil, a, b))

	got := find(t, composed, "quiet-threads:kickoff")
	if got.Confidence != model.Certain {
		t.Errorf("confidence = %q, want the agreed item promoted to certain", got.Confidence)
	}
	if got.SectionHint != model.SectionUrgentToday {
		t.Errorf("section = %q, want urgent-today", got.SectionHint)
	}
	if !strings.Contains(got.Detail, "claude, codex") {
		t.Errorf("detail = %q, want the voters named on the page", got.Detail)
	}

	d := decisionAt(t, decisions, PointUrgency)
	if !d.Outcome.Decided || d.Instruction == "" {
		t.Errorf("decision = %+v, want a decided outcome with an instruction for the composer", d)
	}
}

// Disagreement is the case the honesty layer exists for: the item stays unsure,
// moves to "I'm not sure", and the page states the split.
func TestUrgencyDisagreementRoutesToImNotSure(t *testing.T) {
	a := voterFor("claude", `{"urgent":true,"why":"it is due today"}`, `{"pick":"commitments:e-001","why":"money"}`)
	b := voterFor("codex", `{"urgent":false,"why":"Friday is not today"}`, `{"pick":"commitments:e-001","why":"money"}`)

	composed, decisions := Decide(context.Background(), testInput(nil, a, b))

	got := find(t, composed, "quiet-threads:kickoff")
	if got.Confidence != model.Unsure {
		t.Errorf("confidence = %q, want it left unsure", got.Confidence)
	}
	if got.SectionHint != model.SectionNotSure {
		t.Errorf("section = %q, want not-sure", got.SectionHint)
	}
	if !strings.Contains(got.Detail, "disagree") || !strings.Contains(got.Detail, "no majority") {
		t.Errorf("detail = %q, want the split stated rather than resolved", got.Detail)
	}

	d := decisionAt(t, decisions, PointUrgency)
	if d.Outcome.Decided {
		t.Error("a 1-1 split must not be a decision")
	}
	if d.Instruction != "" {
		t.Errorf("instruction = %q, want nothing for the composer to honour", d.Instruction)
	}
}

// A single live runner decides alone, silently — SPEC §5, and the default path
// on the development machine.
func TestSingleRunnerDecidesWithoutCeremony(t *testing.T) {
	only := voterFor("claude", `{"urgent":false,"why":"it can wait"}`, `{"pick":"conflicts:evt-7","why":"the board meets first"}`)

	composed, decisions := Decide(context.Background(), testInput(nil, only))

	if got := find(t, composed, "quiet-threads:kickoff"); got.Confidence != model.Certain {
		t.Errorf("confidence = %q, want the lone runner's answer taken", got.Confidence)
	}
	pick := find(t, composed, "conflicts:evt-7")
	if pick.SectionHint != model.SectionOneThingNow {
		t.Errorf("section = %q, want the picked item to open the page", pick.SectionHint)
	}
	if d := decisionAt(t, decisions, PointOneThing); !strings.Contains(d.Outcome.Reason, "alone") {
		t.Errorf("reason = %q, want it to say the runner answered alone", d.Outcome.Reason)
	}
}

// A tie for the top of the page becomes its own uncertain signal, so it reaches
// the reader through exactly the path every other uncertainty takes.
func TestOneThingDisagreementBecomesAnUncertainSignal(t *testing.T) {
	a := voterFor("claude", `{"urgent":true,"why":"today"}`, `{"pick":"commitments:e-001","why":"the money"}`)
	b := voterFor("codex", `{"urgent":true,"why":"today"}`, `{"pick":"conflicts:evt-7","why":"the board"}`)

	composed, decisions := Decide(context.Background(), testInput(nil, a, b))

	got := find(t, composed, "consensus:one-thing-now")
	if got.Confidence != model.Unsure || got.SectionHint != model.SectionNotSure {
		t.Errorf("signal = %+v, want an unsure signal routed to not-sure", got)
	}
	if err := got.Validate(); err != nil {
		t.Errorf("the disagreement signal must obey the signal contract: %v", err)
	}
	if len(got.Citations) == 0 {
		t.Error("the disagreement must carry the contested items' own citations, so the reader can settle it")
	}
	if !strings.Contains(got.Detail, "Answer Marcus") || !strings.Contains(got.Detail, "Board sync") {
		t.Errorf("detail = %q, want both contested items named", got.Detail)
	}

	if d := decisionAt(t, decisions, PointOneThing); d.Outcome.Decided {
		t.Error("a 1-1 pick must not be a decision")
	}
	for _, s := range composed {
		if s.ID != "consensus:one-thing-now" && s.SectionHint == model.SectionOneThingNow {
			t.Errorf("signal %s was promoted to the top of the page despite the split", s.ID)
		}
	}
}

// A runner that answers with an id nobody offered has not disagreed, it has
// failed to answer — and a failed vote is dropped, exactly like a 401.
func TestAPickOutsideTheCandidatesIsDroppedNotCounted(t *testing.T) {
	sane := voterFor("claude", `{"urgent":true,"why":"today"}`, `{"pick":"commitments:e-001","why":"the money"}`)
	inventive := voterFor("codex", `{"urgent":true,"why":"today"}`, `{"pick":"commitments:e-999","why":"a signal that does not exist"}`)

	composed, decisions := Decide(context.Background(), testInput(nil, sane, inventive))

	if got := find(t, composed, "commitments:e-001"); got.SectionHint != model.SectionOneThingNow {
		t.Errorf("section = %q, want the valid vote to carry the decision", got.SectionHint)
	}
	d := decisionAt(t, decisions, PointOneThing)
	if len(d.Outcome.Failed) != 1 || d.Outcome.Failed[0].Runner != "codex" {
		t.Errorf("Failed = %+v, want the invented pick dropped", d.Outcome.Failed)
	}
}

// Every runner that cannot answer is dropped, and when that is all of them the
// page still gets composed — from a fact base nobody voted on.
func TestNoRunnerAnswersLeavesTheFactBaseUntouched(t *testing.T) {
	broken := &runnertest.Runner{RunnerName: "codex", AskErr: errors.New("401 Unauthorized")}

	composed, decisions := Decide(context.Background(), testInput(nil, broken))

	if got := find(t, composed, "quiet-threads:kickoff"); got.Confidence != model.Unsure {
		t.Errorf("confidence = %q, want the toolbox's own answer left alone", got.Confidence)
	}
	d := decisionAt(t, decisions, PointUrgency)
	if d.Outcome.Decided || !strings.Contains(d.Outcome.Reason, "no runner answered") {
		t.Errorf("outcome = %+v, want it to say plainly that nobody voted", d.Outcome)
	}
}

// --consensus=off is the frugal flag: no votes, no spend, and the toolbox's own
// answers all the way through.
func TestConsensusOffAsksNobody(t *testing.T) {
	only := voterFor("claude", `{"urgent":true,"why":"today"}`)

	in := testInput(nil, only)
	in.Consensus = false
	composed, decisions := Decide(context.Background(), in)

	if only.Asks() != 0 {
		t.Errorf("asked %d questions with consensus off, want none", only.Asks())
	}
	if len(decisions) != 0 {
		t.Errorf("decisions = %v, want none", decisions)
	}
	if got := find(t, composed, "quiet-threads:kickoff"); got.Confidence != model.Unsure {
		t.Error("with no vote, an unsure signal stays unsure")
	}
}

// The questions are grounded in the fact base, and every runner gets the same
// bytes — a vote across differently worded questions is not a vote.
func TestEveryVoterIsAskedTheIdenticalGroundedQuestion(t *testing.T) {
	a := voterFor("claude", `{"urgent":true,"why":"today"}`, `{"pick":"commitments:e-001","why":"money"}`)
	b := voterFor("codex", `{"urgent":true,"why":"today"}`, `{"pick":"commitments:e-001","why":"money"}`)

	Decide(context.Background(), testInput(nil, a, b))

	pa, pb := a.Prompts(), b.Prompts()
	if len(pa) != 2 || len(pb) != 2 {
		t.Fatalf("asked %d and %d questions, want one per decision point", len(pa), len(pb))
	}
	for i := range pa {
		if pa[i] != pb[i] {
			t.Errorf("question %d differed between runners", i)
		}
	}
	if !strings.Contains(pa[0], "Northstar migration plan") {
		t.Errorf("the urgency question is not grounded in the signal: %q", pa[0])
	}
	if !strings.Contains(pa[1], "commitments:e-001") || !strings.Contains(pa[1], "conflicts:evt-7") {
		t.Errorf("the pick question does not carry the candidates: %q", pa[1])
	}
}

// The vote is bounded: one item that could not be ranked is worth a round of
// calls, forty are not, and the set arrives most-urgent-first so a cap takes the
// ones worth paying for.
func TestUrgencyVotingIsCapped(t *testing.T) {
	in := testInput(nil, voterFor("claude", `{"urgent":false,"why":"no"}`))
	var many model.Signals
	for _, s := range testSignals() {
		many = append(many, s)
	}
	for i := 0; i < 10; i++ {
		s := many[2]
		s.ID = s.ID + "#" + string(rune('a'+i))
		many = append(many, s)
	}
	in.Signals = many

	_, decisions := Decide(context.Background(), in)

	urgencyVotes := 0
	for _, d := range decisions {
		if d.Point == PointUrgency {
			urgencyVotes++
		}
	}
	if urgencyVotes != maxAmbiguous {
		t.Errorf("ran %d urgency votes over 11 unsure signals, want the cap of %d", urgencyVotes, maxAmbiguous)
	}
}

// The schema is handed over as a document, never as a rendered flag, because the
// two CLIs take it in opposite ways — claude inline, codex from a file
// (learning-tests/01 and 03). Each decision point supplies its own.
func TestDecisionsCarryASchemaForTheRunnerToAdapt(t *testing.T) {
	only := voterFor("claude", `{"urgent":true,"why":"today"}`, `{"pick":"commitments:e-001","why":"money"}`)
	Decide(context.Background(), testInput(nil, only))

	got := only.Schemas()
	if len(got) != 2 {
		t.Fatalf("saw %d schemas, want one per decision point", len(got))
	}
	if got[0].Name != "urgency" || !strings.Contains(got[0].JSON, `"urgent"`) {
		t.Errorf("urgency schema = %+v", got[0])
	}
	if got[1].Name != "one-thing" || !strings.Contains(got[1].JSON, `"pick"`) {
		t.Errorf("pick schema = %+v", got[1])
	}
	for _, s := range got {
		if strings.Contains(s.JSON, "--json-schema") || strings.Contains(s.JSON, "--output-schema") {
			t.Errorf("schema %q is a rendered flag; the interface hands over the document and each runner adapts", s.JSON)
		}
	}
}
