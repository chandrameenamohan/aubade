package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Consensus is the ~200-line layer that keeps the one good idea from a
// meta-harness without importing a platform (HLD §5): ask every runner on the
// machine the same grounded question and majority-vote the answers.
//
// It is ON by default and it is the frugality flag that is opt-in, not the
// quality. The user is a CEO, the digest runs on a schedule at 06:00 so
// wall-clock is free, and a wrong top priority costs more than three times the
// model spend.
//
// The vote math has exactly three rules, and all three exist because of a
// finding rather than a preference:
//
//  1. **Only live runners are counted.** A runner that errors is dropped, never
//     tallied as a dissenting vote — codex 401s here while reporting itself
//     logged in, and a 401 is not an opinion (learning-tests/03).
//  2. **A decision needs a strict majority of the answers received.** One voter
//     therefore decides alone, which is the SPEC's "single runner ⇒ silently
//     single" — and it is the default path on the development machine, so it is
//     the well-tested one rather than the fallback nobody exercises.
//  3. **No majority is an answer, not a coin flip.** A 1–1 split or a three-way
//     split routes the item to the "I'm not sure" section with its thread shown
//     (SPEC §5, §7). An even roster is safe precisely because the honesty layer
//     is there to catch it.

// Vote is one runner's answer.
type Vote struct {
	Runner string
	// Key is the normalized value being voted on — the thing two runners can be
	// said to agree about. Empty when Err is set.
	Key string
	// Raw is the runner's whole structured answer, kept for diagnostics — on a
	// dropped vote it is what says whether the runner was broken or merely
	// ignoring the schema.
	Raw json.RawMessage
	Err error
}

// Outcome is what a decision point concluded.
type Outcome struct {
	// Decided is true when a strict majority of the answers agreed.
	Decided bool

	// Key is the winning value, empty when undecided.
	Key string

	// Votes are every answer received, in roster order; Failed are the runners
	// that could not answer and were dropped from the tally.
	Votes  []Vote
	Failed []Vote

	// Tally counts the votes by key.
	Tally map[string]int

	// Reason says, in one line, why the outcome came out the way it did. It is
	// written for the digest page, not for a log: on a disagreement this
	// sentence is what the reader is shown under "I'm not sure".
	Reason string
}

// Voters names the runners whose answers were counted.
func (o Outcome) Voters() []string {
	out := make([]string, 0, len(o.Votes))
	for _, v := range o.Votes {
		out = append(out, v.Runner)
	}
	return out
}

// KeyFunc turns a structured answer into the value being voted on. It returns
// an error when the answer does not obey the schema, and such a runner is
// dropped exactly like one that never answered: a malformed answer is not a
// dissent either.
type KeyFunc func(json.RawMessage) (string, error)

// Poll asks every runner the same question, in parallel, and majority-votes the
// answers.
//
// Parallel because the runners are independent and the slowest one already
// bounds the call; the per-runner budget in Question is what keeps a hung
// runner from holding the whole decision open.
func Poll(ctx context.Context, runners []Runner, q Question, key KeyFunc) Outcome {
	votes := make([]Vote, len(runners))

	var wg sync.WaitGroup
	for i, r := range runners {
		wg.Add(1)
		go func(i int, r Runner) {
			defer wg.Done()
			raw, err := r.Ask(ctx, q)
			if err != nil {
				votes[i] = Vote{Runner: r.Name(), Err: err}
				return
			}
			k, err := key(raw)
			if err != nil {
				votes[i] = Vote{Runner: r.Name(), Raw: raw, Err: err}
				return
			}
			votes[i] = Vote{Runner: r.Name(), Key: k, Raw: raw}
		}(i, r)
	}
	wg.Wait()

	return Tally(votes)
}

// Tally is the vote math, separated from the asking so it can be tested without
// a model anywhere near it.
func Tally(all []Vote) Outcome {
	out := Outcome{Tally: map[string]int{}}
	for _, v := range all {
		if v.Err != nil {
			out.Failed = append(out.Failed, v)
			continue
		}
		out.Votes = append(out.Votes, v)
		out.Tally[v.Key]++
	}

	if len(out.Votes) == 0 {
		out.Reason = fmt.Sprintf("no runner answered (%s)", failedSummary(out.Failed))
		return out
	}

	// Ties are resolved by key rather than left to map order, so an undecided
	// outcome is undecided identically on every machine.
	keys := make([]string, 0, len(out.Tally))
	for k := range out.Tally {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	top, count := keys[0], out.Tally[keys[0]]
	for _, k := range keys[1:] {
		if out.Tally[k] > count {
			top, count = k, out.Tally[k]
		}
	}

	if count*2 <= len(out.Votes) {
		out.Reason = fmt.Sprintf("%s split %s with no majority", plural(len(out.Votes), "runner", "runners"), tallyPhrase(out.Tally, keys))
		return out
	}

	out.Decided = true
	out.Key = top
	switch {
	case len(out.Votes) == 1:
		out.Reason = fmt.Sprintf("%s answered alone", out.Votes[0].Runner)
	case count == len(out.Votes):
		out.Reason = fmt.Sprintf("all %d runners agreed", count)
	default:
		out.Reason = fmt.Sprintf("%d of %d runners agreed", count, len(out.Votes))
	}
	if len(out.Failed) > 0 {
		out.Reason += fmt.Sprintf(" (%s dropped: %s)", plural(len(out.Failed), "runner", "runners"), failedSummary(out.Failed))
	}
	return out
}

// tallyPhrase renders a split the way the honesty section states it:
// `"urgent" 1, "not urgent" 1`.
func tallyPhrase(tally map[string]int, keys []string) string {
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%q %d", k, tally[k]))
	}
	return strings.Join(parts, ", ")
}

// failedSummary names the dropped runners and why, because "who did not vote"
// is a fact the reader is owed.
func failedSummary(failed []Vote) string {
	parts := make([]string, 0, len(failed))
	for _, v := range failed {
		parts = append(parts, v.Runner+": "+shortReason(v.Err))
	}
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, "; ")
}

// plural renders "1 runner" / "3 runners".
func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}
