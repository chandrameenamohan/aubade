package agentic

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/chandrameenamohan/aubade/internal/model"
	"github.com/chandrameenamohan/aubade/internal/runner"
)

// There are exactly two decision points, and the number is the design.
//
// SPEC §5 puts consensus on bounded one-shot decisions — ambiguous-thread
// urgency, and the "one thing right now" pick — and nowhere else. Fanning the
// whole page out to every runner would be three times the spend for an
// unmergeable answer; fanning out one boolean and one id is three opinions on
// the two judgments that actually cost something when they are wrong. Everything
// else on the page is either a deterministic fact or a matter of arrangement.
//
// Both points share the same failure behaviour, and it is the honest one: no
// majority is not a coin flip, it is an item routed to "I'm not sure" with its
// thread shown (SPEC §7).

// Decision-point names, as they appear in the run's provenance.
const (
	PointUrgency  = "ambiguous-thread-urgency"
	PointOneThing = "one-thing-now"
)

// Caps on how much a morning is allowed to vote about. Each ambiguous item is a
// call to every runner, so the cost of an unbounded roster of unsure signals is
// multiplied by the roster size; the signal set arrives sorted most-urgent-first,
// so a cap takes the items worth paying for.
const (
	maxAmbiguous  = 4
	maxCandidates = 6
)

// consensusKind is the kind on a signal that reports a disagreement. It is
// deliberately not one of model.KnownKinds: no extractor produced it, and the
// eval harness must be able to tell an extractor's finding from the
// orchestrator's own admission.
const consensusKind = "consensus"

// Decision is one settled (or unsettled) decision point.
type Decision struct {
	// Point is which decision this was.
	Point string

	// Subject names the signal or signals it was about.
	Subject []string

	// Outcome is the vote itself, votes and tally included.
	Outcome runner.Outcome

	// Instruction is what the orchestrator is told about the result, or empty
	// when there is nothing for it to honour.
	Instruction string

	// Note is the one-line account for the digest footer.
	Note string
}

// urgencyAnswer is the schema-constrained answer at the urgency decision point.
type urgencyAnswer struct {
	Urgent bool   `json:"urgent"`
	Why    string `json:"why"`
}

// pickAnswer is the schema-constrained answer at the one-thing decision point.
type pickAnswer struct {
	Pick string `json:"pick"`
	Why  string `json:"why"`
}

const urgencySchema = `{"type":"object","properties":{"urgent":{"type":"boolean"},"why":{"type":"string"}},"required":["urgent","why"],"additionalProperties":false}`

const pickSchema = `{"type":"object","properties":{"pick":{"type":"string"},"why":{"type":"string"}},"required":["pick","why"],"additionalProperties":false}`

// Decide runs both decision points and returns the signal set the page is
// composed from, plus what was decided.
//
// The fact base is never mutated: decisions produce a *composed* set — the same
// facts, with the two judgments applied and any disagreement added as its own
// uncertain signal. signals.json on disk stays the toolbox's own answer, so a
// grader can always tell what the extractors said from what the runners made of
// it.
func Decide(ctx context.Context, in Input) (model.Signals, []Decision) {
	composed := append(model.Signals(nil), in.Signals...)
	if !in.Consensus || len(in.Voters) == 0 {
		return composed, nil
	}

	var decisions []Decision
	composed, decisions = decideUrgency(ctx, in, composed, decisions)
	composed, decisions = decideOneThing(ctx, in, composed, decisions)
	return composed, decisions
}

// decideUrgency asks the roster about each item the toolbox could not rank.
func decideUrgency(ctx context.Context, in Input, composed model.Signals, decisions []Decision) (model.Signals, []Decision) {
	asked := 0
	for i := range composed {
		if asked >= maxAmbiguous {
			break
		}
		s := composed[i]
		if s.Confidence != model.Unsure || s.Kind == consensusKind {
			continue
		}
		asked++

		out := runner.Poll(ctx, in.Voters, runner.Question{
			Prompt: urgencyPrompt(in, s),
			Schema: runner.Schema{Name: "urgency", JSON: urgencySchema},
		}, urgencyKey)

		d := Decision{Point: PointUrgency, Subject: []string{s.ID}, Outcome: out}
		switch {
		case !out.Decided:
			// The item was already unsure; it stays unsure, and now the page can
			// say something sharper than "the extractor could not tell" —
			// the runners could not either, and here is the split.
			composed[i].SectionHint = model.SectionNotSure
			composed[i].Detail = appendSentence(s.Detail,
				fmt.Sprintf("Runners disagree about whether this needs you today (%s), so it is here with its thread rather than ranked.", out.Reason))
			d.Note = fmt.Sprintf("%s: no majority (%s)", shortID(s.ID), out.Reason)
		case out.Key == urgencyToday:
			composed[i].Confidence = model.Certain
			composed[i].SectionHint = model.SectionUrgentToday
			composed[i].Detail = appendSentence(s.Detail, consensusSentence("needs you today", out))
			d.Instruction = fmt.Sprintf("Signal %q is urgent today — %s. Rank it under Urgent To-Do Today.", s.ID, out.Reason)
			d.Note = fmt.Sprintf("%s: urgent today (%s)", shortID(s.ID), out.Reason)
		default:
			composed[i].Confidence = model.Certain
			composed[i].SectionHint = model.SectionPulse
			composed[i].Detail = appendSentence(s.Detail, consensusSentence("does not need you today", out))
			d.Instruction = fmt.Sprintf("Signal %q does not need Avery today — %s. Keep it out of the urgent section.", s.ID, out.Reason)
			d.Note = fmt.Sprintf("%s: not urgent today (%s)", shortID(s.ID), out.Reason)
		}
		decisions = append(decisions, d)
	}
	return composed, decisions
}

// decideOneThing votes on what opens the page.
func decideOneThing(ctx context.Context, in Input, composed model.Signals, decisions []Decision) (model.Signals, []Decision) {
	candidates := oneThingCandidates(composed)
	if len(candidates) < 2 {
		return composed, decisions // nothing to disagree about
	}

	ids := make([]string, 0, len(candidates))
	for _, c := range candidates {
		ids = append(ids, c.ID)
	}
	out := runner.Poll(ctx, in.Voters, runner.Question{
		Prompt: oneThingPrompt(in, candidates),
		Schema: runner.Schema{Name: "one-thing", JSON: pickSchema},
	}, pickKey(ids))

	d := Decision{Point: PointOneThing, Subject: ids, Outcome: out}
	if !out.Decided {
		composed = append(composed, disagreementSignal(candidates, out))
		d.Instruction = "The runners could not agree on what comes first this morning, so open the page with the highest-priority item and do not claim it outranks the others; aubade states the split in its own section."
		d.Note = "one thing now: no majority (" + out.Reason + ")"
		decisions = append(decisions, d)
		return composed, decisions
	}

	for i := range composed {
		if composed[i].ID != out.Key {
			continue
		}
		composed[i].SectionHint = model.SectionOneThingNow
		composed[i].Detail = appendSentence(composed[i].Detail, consensusSentence("is what comes first this morning", out))
		break
	}
	d.Instruction = fmt.Sprintf("Open the page with signal %q — %s.", out.Key, out.Reason)
	d.Note = fmt.Sprintf("one thing now: %s (%s)", shortID(out.Key), out.Reason)
	decisions = append(decisions, d)
	return composed, decisions
}

// oneThingCandidates is the shortlist the top of the page is chosen from: the
// certain items the toolbox routed somewhere that could plausibly lead. The set
// arrives already sorted most-urgent-first, so taking a prefix takes the items
// worth a vote.
func oneThingCandidates(ss model.Signals) model.Signals {
	out := make(model.Signals, 0, maxCandidates)
	for _, s := range ss {
		if s.Confidence != model.Certain {
			continue
		}
		switch s.SectionHint {
		case model.SectionOneThingNow, model.SectionUrgentToday, model.SectionDecisions:
		default:
			continue
		}
		out = append(out, s)
		if len(out) == maxCandidates {
			break
		}
	}
	return out
}

// The two values the urgency vote is counted on. They are words rather than
// booleans because the tally is rendered into a sentence a reader sees.
const (
	urgencyToday    = "needs you today"
	urgencyNotToday = "does not need you today"
)

// urgencyKey reads a vote at the urgency decision point.
func urgencyKey(raw json.RawMessage) (string, error) {
	var a urgencyAnswer
	if err := json.Unmarshal(raw, &a); err != nil {
		return "", fmt.Errorf("answer does not match the urgency schema: %w", err)
	}
	if a.Urgent {
		return urgencyToday, nil
	}
	return urgencyNotToday, nil
}

// pickKey reads a vote at the one-thing decision point, rejecting a pick that
// is not one of the candidates.
//
// A runner that invents an id has not disagreed, it has failed to answer the
// question — and Poll drops a failed vote instead of tallying it, which is the
// same treatment a 401 gets and for the same reason.
func pickKey(ids []string) runner.KeyFunc {
	allowed := make(map[string]bool, len(ids))
	for _, id := range ids {
		allowed[id] = true
	}
	return func(raw json.RawMessage) (string, error) {
		var a pickAnswer
		if err := json.Unmarshal(raw, &a); err != nil {
			return "", fmt.Errorf("answer does not match the pick schema: %w", err)
		}
		pick := strings.TrimSpace(a.Pick)
		if !allowed[pick] {
			return "", fmt.Errorf("picked %q, which is not one of the candidates", clip(pick, 60))
		}
		return pick, nil
	}
}

// disagreementSignal turns a failed vote into a first-class uncertain signal, so
// it flows to the "I'm not sure" section through the same path every other
// uncertainty takes — and carries the contested items' own citations, so the
// reader can go and settle it.
func disagreementSignal(candidates model.Signals, out runner.Outcome) model.Signal {
	titles := make([]string, 0, len(candidates))
	var cites []model.Citation
	for _, c := range candidates {
		titles = append(titles, `"`+clip(c.Title, 60)+`"`)
		for _, cite := range c.Citations {
			if !hasCitation(cites, cite) {
				cites = append(cites, cite)
			}
		}
	}
	if len(cites) > maxDisagreementCites {
		cites = cites[:maxDisagreementCites]
	}

	detail := fmt.Sprintf("aubade asked every model runner on this machine which of these to open with, and %s: %s. The page opens with the highest-priority one by rank, not by judgment — all of them are shown, and the call is yours.",
		out.Reason, strings.Join(titles, ", "))

	return model.Signal{
		ID:          "consensus:one-thing-now",
		Kind:        consensusKind,
		Priority:    candidates[0].Priority,
		Title:       "The runners disagree about what comes first this morning.",
		Detail:      detail,
		Citations:   cites,
		SectionHint: model.SectionNotSure,
		Confidence:  model.Unsure,
	}
}

// maxDisagreementCites bounds the receipts on a disagreement line.
const maxDisagreementCites = 6

// consensusSentence is how a decided vote is stated on the page. It names the
// voters, because "a model said so" and "both models said so" are different
// claims and the reader is entitled to know which one they are reading.
func consensusSentence(verdict string, out runner.Outcome) string {
	voters := out.Voters()
	if len(voters) == 1 {
		return fmt.Sprintf("%s reads this as something that %s.", voters[0], verdict)
	}
	return fmt.Sprintf("The runners (%s) agree this %s.", strings.Join(voters, ", "), verdict)
}

// urgencyPrompt is the grounded question at the urgency decision point. Every
// runner is asked this byte-for-byte identical text: a vote across differently
// worded questions is not a vote.
func urgencyPrompt(in Input, s model.Signal) string {
	body, _ := json.MarshalIndent(s, "", "  ")
	var b strings.Builder
	fmt.Fprintf(&b, "You are helping rank %s's morning digest for %s.\n\n", ownerName(in.Owner), in.Day)
	b.WriteString("aubade's deterministic toolbox produced the signal below and marked it unsure: it could not tell whether this needs attention today.\n\n```json\n")
	b.Write(body)
	fmt.Fprintf(&b, "\n```\n\nDoes this need %s's attention today, %s?\n\n", ownerName(in.Owner), in.Day)
	b.WriteString("Judge only from the signal above. If it does not say enough to be sure, answer false — an honest \"no\" costs less than a wrong \"yes\" at the top of someone's morning. Keep `why` to one short sentence.")
	return b.String()
}

// oneThingPrompt is the grounded question at the one-thing decision point.
func oneThingPrompt(in Input, candidates model.Signals) string {
	body, _ := json.MarshalIndent(candidates, "", "  ")
	var b strings.Builder
	fmt.Fprintf(&b, "You are helping rank %s's morning digest for %s.\n\n", ownerName(in.Owner), in.Day)
	b.WriteString("These are the candidates for the single most important thing to do first this morning, produced by aubade's deterministic toolbox and already cited:\n\n```json\n")
	b.Write(body)
	b.WriteString("\n```\n\nWhich one must be done first? Answer with its `id`, copied verbatim from the list — any other value is not an answer. Keep `why` to one short sentence.")
	return b.String()
}

// appendSentence adds a sentence to a detail line without doubling its
// punctuation.
func appendSentence(detail, sentence string) string {
	detail = strings.TrimSpace(detail)
	if detail == "" {
		return sentence
	}
	if !strings.HasSuffix(detail, ".") && !strings.HasSuffix(detail, "!") && !strings.HasSuffix(detail, "?") {
		detail += "."
	}
	return detail + " " + sentence
}

// shortID trims the kind prefix off a signal id for a footer line, where the
// kind is already obvious from context.
func shortID(id string) string {
	if i := strings.Index(id, ":"); i > 0 && i < len(id)-1 {
		return id[i+1:]
	}
	return id
}

// hasCitation reports whether a citation is already in the list.
func hasCitation(list []model.Citation, want model.Citation) bool {
	for _, c := range list {
		if c == want {
			return true
		}
	}
	return false
}

// clip shortens text to n runes, for a message that has to fit on a line.
func clip(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
