// Package eval is aubade's trap harness: the thing that grades the digest
// against the answer key `aubade-lab generate` wrote beside the corpus.
//
// The vocabulary is EVAL-PRINCIPLES', and it is used literally rather than
// loosely: a **task** is one planted trap, a **trial** is one digest run, a
// **grader** is the logic that scores a trial, the **harness** is this package,
// and a **suite** is a set of tasks measuring one capability area. Two suites
// exist and they are never averaged into one number (#15):
//
//   - The **regression suite** grades the `--no-llm` page. It is deterministic,
//     it runs one trial, its bar is 100%, and it gates `make check`.
//   - The **capability suite** grades the agentic page over N isolated trials
//     (#10, #11) and reports pass^N and pass@N. It is non-deterministic, so it
//     never gates; it runs when the claude CLI is present and skips loudly when
//     it is not.
//
// # What a grader here is allowed to look at
//
// Outputs, never paths (#6). A trial is graded on two artefacts: the page
// (`digest.md`) and the fact base it was composed from (`signals.json`).
// Nothing here asserts which tools the agent called or in what order — an agent
// that reaches the right page by a route we did not anticipate has not failed a
// task. The transcript is read for one narrow question (transcript.go) and
// never for the score.
//
// # The one predicate everything is built on
//
// A trap is **surfaced** when some signal cites one of the artefacts that trap
// planted, and that signal is not the suppression audit record:
//
//	surfaced(trap) := ∃ s ∈ signals :
//	    s cites a planted_ref of trap  ∧  ¬(s.kind = suppressions ∧ s.section_hint = honesty)
//
// The exception is not a loophole, it is the point. A `suppressions` signal in
// the honesty section is aubade's record that it *saw* an item and dropped it on
// the user's own rule — the opposite of surfacing it, and a stronger claim than
// "it never appeared" (extract/suppressions.go says so in as many words). The
// recruiter-pattern trap proves the exception is narrow: its expected signal is
// also a `suppressions` one, but it is emitted into the pulse section, so it
// counts as surfaced exactly as it should.
//
// Positive and negative tasks are then the same predicate, inverted:
//
//   - **must_surface: true** passes when the trap is surfaced AND at least one
//     of its expected keywords appears in the page. Both halves are needed: a
//     signal nobody rendered is a fact the user never saw, and a keyword with no
//     signal behind it is prose with no receipt.
//   - **must_surface: false** passes when the trap is not surfaced.
//
// # Why the negative half does not grade keywords
//
// SPEC's keyword contract exists because we refuse to pin the renderer's
// phrasing: a positive trap is *found* in prose by any one of several words it
// planted. Absence is a different question, and the same instrument answers it
// wrongly. Three worked examples from this corpus:
//
//   - "Stratechery" is the keyword of the newsletter trap and it appears in the
//     digest inside the profile rule that suppressed it — "Newsletters. Even the
//     good ones. Even Stratechery." Quoting the rule is how aubade shows its
//     work; it is not surfacing the newsletter.
//   - "Weekly leadership sync" is the keyword of the accepted-invite trap, and
//     the page lists today's agenda. The answer key forbids that meeting being
//     surfaced *as a conflict or a decision*, which is what its expected signal
//     kind says; it does not forbid the calendar showing the calendar.
//   - Keywords are frequently surnames and company names that also appear as
//     citation labels on unrelated lines.
//
// Each of those would be a red gate that names nothing to fix, and a grader
// whose failure is not actionable should be cut (#1). The signal-level
// assertion is the one the answer key actually makes — "the extractor that owns
// keeping this out; a false positive names it" — and it is strictly more
// precise, because it is anchored to the artefacts the trap planted rather than
// to a word that may belong to anything.
//
// # Why the expected extractor is reported and not enforced
//
// `expect.signal_kind` names the extractor the answer key thinks should answer.
// The grader records which extractor actually surfaced the trap and prints the
// mismatch, but does not fail on it — grading paths rather than outcomes
// punishes an engine for finding the right answer by a better route (#6). The
// blind spot that opens up (an extractor could die while its traps stay green
// through another route) is exactly what `--sabotage` is for, and sabotage runs
// against every extractor by name.
package eval

import (
	"fmt"
	"strings"

	"github.com/chandrameenamohan/aubade/internal/datagen"
	"github.com/chandrameenamohan/aubade/internal/model"
)

// TrapResult is one task's verdict in one trial.
//
// Every field past Passed exists to make a failure actionable (#1): which
// extractor did surface it (or none), whether the item was deliberately
// suppressed, which keyword was found. A card that says only "failed" sends the
// reader back to the corpus to work out what happened.
type TrapResult struct {
	// ID, Kind and MustSurface are the trap's own identity, copied so a result
	// can be read without the answer key beside it.
	ID          string
	Kind        string
	MustSurface bool

	// Expected is the extractor the answer key expected to answer.
	Expected string

	// Passed is the verdict.
	Passed bool

	// Surfaced reports whether any non-audit signal cited the trap's artefacts.
	Surfaced bool

	// By is every signal kind that surfaced it, in extractor order. Empty when
	// nothing did.
	By []string

	// Suppressed reports whether aubade recorded holding the item back on one
	// of the profile's own rules.
	Suppressed bool

	// Rule is the profile rule that held it back, when Suppressed.
	Rule string

	// Keyword is the expected keyword found in the page, empty when none was.
	Keyword string

	// Reason is the one line a reader acts on.
	Reason string
}

// Result is one trial graded against the whole answer key.
//
// It carries no suite tag. The two suites are kept apart by living in different
// fields of the Card, which is a boundary the compiler enforces — a label on a
// struct that both suites share would be a convention, and the one thing that
// must never happen to these numbers is being added together.
type Result struct {
	// Mode is what composed the page, read from its own footer where it says so.
	Mode string

	Traps []TrapResult
}

// Passed reports whether every task in this trial passed. The regression bar is
// 100% and nothing else (#15), so this is the gate's whole question.
func (r *Result) Passed() bool {
	for _, t := range r.Traps {
		if !t.Passed {
			return false
		}
	}
	return true
}

// Score is how many tasks passed, out of how many were graded.
func (r *Result) Score() (passed, total int) {
	for _, t := range r.Traps {
		total++
		if t.Passed {
			passed++
		}
	}
	return passed, total
}

// Failures returns the tasks that failed, in answer-key order.
func (r *Result) Failures() []TrapResult {
	var out []TrapResult
	for _, t := range r.Traps {
		if !t.Passed {
			out = append(out, t)
		}
	}
	return out
}

// Get returns the result for one trap id.
func (r *Result) Get(id string) (TrapResult, bool) {
	for _, t := range r.Traps {
		if t.ID == id {
			return t, true
		}
	}
	return TrapResult{}, false
}

// Grade runs every code grader over one trial's artefacts.
//
// It never returns an error: a trial with no digest and no signals is a trial
// where every task failed, which is a score rather than a crash. The caller
// that could not produce artefacts at all is the one that reports why.
func Grade(traps datagen.Traps, a *Artifacts) *Result {
	res := &Result{Mode: a.Mode()}
	idx := newSignalIndex(signalsOf(a))
	page := strings.ToLower(pageOf(a))

	for _, trap := range traps {
		res.Traps = append(res.Traps, gradeTrap(trap, idx, page))
	}
	return res
}

func signalsOf(a *Artifacts) model.Signals {
	if a == nil {
		return nil
	}
	return a.Signals
}

func pageOf(a *Artifacts) string {
	if a == nil {
		return ""
	}
	return a.Digest
}

// gradeTrap is the whole code grader for one task.
func gradeTrap(trap datagen.Trap, idx *signalIndex, lowerPage string) TrapResult {
	ev := idx.evidence(trap.PlantedRefs)
	out := TrapResult{
		ID:          trap.ID,
		Kind:        trap.Kind,
		MustSurface: trap.MustSurface,
		Expected:    trap.Expect.SignalKind,
		Surfaced:    len(ev.by) > 0,
		By:          ev.by,
		Suppressed:  ev.suppressed,
		Rule:        ev.rule,
		Keyword:     findKeyword(lowerPage, trap.Expect.Keywords),
	}

	if trap.MustSurface {
		out.Passed = out.Surfaced && out.Keyword != ""
		out.Reason = positiveReason(out, trap)
		return out
	}
	out.Passed = !out.Surfaced
	out.Reason = negativeReason(out)
	return out
}

// positiveReason says what happened, in the terms the person fixing it needs.
func positiveReason(r TrapResult, trap datagen.Trap) string {
	switch {
	case !r.Surfaced && r.Suppressed:
		return fmt.Sprintf("no signal surfaced it — it was suppressed as %q; %s should have caught it first",
			r.Rule, r.Expected)
	case !r.Surfaced:
		return fmt.Sprintf("no signal cites any of its planted refs; %s missed it", r.Expected)
	case r.Keyword == "":
		return fmt.Sprintf("%s produced the signal but the page never says any of %s — surfaced in signals.json and lost in the render",
			strings.Join(r.By, ", "), quoteList(trap.Expect.Keywords))
	case !contains(r.By, r.Expected):
		return fmt.Sprintf("surfaced by %s and found in the page as %q — the answer key expected %s",
			strings.Join(r.By, ", "), r.Keyword, r.Expected)
	default:
		return fmt.Sprintf("%s surfaced it and the page says %q", r.Expected, r.Keyword)
	}
}

// negativeReason says how an item stayed out, which is as interesting as
// whether it did: "held back by the user's own rule" and "nothing ever looked at
// it" are both passes and they are not the same news.
func negativeReason(r TrapResult) string {
	switch {
	case r.Surfaced:
		return fmt.Sprintf("%s surfaced it; it must not appear", strings.Join(r.By, ", "))
	case r.Suppressed:
		return fmt.Sprintf("held back on the profile rule %q", r.Rule)
	default:
		return "no extractor claimed it"
	}
}

// findKeyword returns the first expected keyword present in the page.
//
// Matching is case-insensitive substring, deliberately: SPEC's contract is that
// the keyword "appears in digest text", and the page is prose written for a
// human — it capitalises a sentence, it pluralises "expense report" into
// "expense reports". Requiring a word boundary or a case match here would grade
// the renderer's grammar rather than the engine's recall.
func findKeyword(lowerPage string, keywords []string) string {
	for _, k := range keywords {
		if k = strings.TrimSpace(k); k != "" && strings.Contains(lowerPage, strings.ToLower(k)) {
			return k
		}
	}
	return ""
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func quoteList(ss []string) string {
	parts := make([]string, 0, len(ss))
	for _, s := range ss {
		parts = append(parts, `"`+s+`"`)
	}
	return strings.Join(parts, " or ")
}

// clip shortens text to n runes — for a card line, or for a prompt that has a
// budget. Runes rather than bytes: half a rune is not a shorter string, it is a
// broken one.
func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
