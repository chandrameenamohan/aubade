package agentic

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/chandrameenamohan/aubade/internal/digest"
	"github.com/chandrameenamohan/aubade/internal/model"
)

// This is where the keystone claim stops being an intention.
//
// HLD §2: "facts can only enter the digest through cited tool output, so the
// model orchestrates but cannot fabricate." A prompt saying so is a wish. What
// makes it true is this file: after the model composes the page, every citation
// on it is resolved against the fact base the toolbox produced, and a page
// carrying one ref that is not in signals.json is rejected whole — not patched,
// not trimmed to the good lines.
//
// Rejecting the whole page rather than the offending line is deliberate. A
// fabricated citation is evidence that the composer was willing to invent a
// receipt; the sentences around it were written by the same pass and have not
// earned more trust than the one that got caught. The deterministic composer is
// right there, it works with no model at all, and it is the honest thing to fall
// back to — loudly, saying exactly what happened.

// citeRE matches the machine-dialect citation the orchestrator is asked to
// write: [email:e-0042], [calendar:evt-17], [note:notes/kickoff.md], [task:t-3].
//
// The source alternation is closed on purpose. A looser pattern would swallow
// ordinary bracketed prose and start reporting fabrications that are really
// markdown, and a citation checker that cries wolf gets turned off.
var citeRE = regexp.MustCompile(`\[(email|calendar|note|task):([^\]\n]{1,200})\]`)

// Citation problems, in the order a reader cares about them.
type violationKind string

const (
	// violationUnknown is a ref that is not in the fact base — a fabrication.
	violationUnknown violationKind = "not in the fact base"
	// violationNone means the page cited nothing at all.
	violationNone violationKind = "no citations at all"
)

// Violation is one reason a composed page was rejected.
type Violation struct {
	Kind violationKind
	Ref  string
	Line string
}

func (v Violation) String() string {
	if v.Ref == "" {
		return string(v.Kind)
	}
	return fmt.Sprintf("[%s] %s — in: %s", v.Ref, v.Kind, clip(v.Line, 90))
}

// FactBase is the set of citations a composed page is allowed to make: exactly
// what the toolbox put in signals.json, and nothing else.
type FactBase struct {
	allowed map[model.Citation]bool
	count   int
}

// NewFactBase indexes a signal set for validation.
func NewFactBase(ss model.Signals) *FactBase {
	fb := &FactBase{allowed: map[model.Citation]bool{}}
	for _, s := range ss {
		for _, c := range s.Citations {
			if !fb.allowed[c] {
				fb.allowed[c] = true
				fb.count++
			}
		}
	}
	return fb
}

// Size is how many distinct citations the page may draw on.
func (fb *FactBase) Size() int { return fb.count }

// Has reports whether a citation is in the fact base.
func (fb *FactBase) Has(c model.Citation) bool { return fb.allowed[c] }

// Validate reports every citation in a composed page that the fact base does
// not support.
//
// An uncited page is a violation of its own. A digest with no receipts anywhere
// is not a page that happened to cite nothing; it is a page whose every line is
// unverifiable, which is the exact failure this architecture exists to make
// impossible.
func (fb *FactBase) Validate(markdown string) []Violation {
	var out []Violation
	seen := 0

	for _, line := range strings.Split(markdown, "\n") {
		for _, m := range citeRE.FindAllStringSubmatch(line, -1) {
			seen++
			cite := model.Citation{Source: model.Source(m[1]), Ref: strings.TrimSpace(m[2])}
			if !fb.Has(cite) {
				out = append(out, Violation{Kind: violationUnknown, Ref: m[1] + ":" + cite.Ref, Line: strings.TrimSpace(line)})
			}
		}
	}
	if seen == 0 {
		out = append(out, Violation{Kind: violationNone})
	}
	return out
}

// Resolve rewrites the machine-dialect citations into the spans a reader reads,
// using the same labeller the template page uses:
//
//	[email:e-0042]  ->  *[email: Marcus, May 19 16:42]*
//
// It runs only after Validate has passed, and that order is the point: the id is
// what can be checked, the name is what can be read, and swapping them the other
// way round would mean checking the thing nobody wrote down.
func Resolve(markdown string, l *digest.Labeler) string {
	if l == nil {
		return markdown
	}
	return citeRE.ReplaceAllStringFunc(markdown, func(token string) string {
		m := citeRE.FindStringSubmatch(token)
		if m == nil {
			return token
		}
		cite := model.Citation{Source: model.Source(m[1]), Ref: strings.TrimSpace(m[2])}
		return "*" + l.Label(cite) + "*"
	})
}

// maxShown is how many violations a summary spells out before it starts
// counting. A reader needs to see that a fabrication happened and an example of
// it, not forty lines of the same failure.
const maxShown = 3

// summarize renders the violations for the operator: the refs *and* the lines
// they were found on, which is what someone debugging a runner needs.
func summarize(vs []Violation) string { return join(vs, Violation.String) }

// summarizeRefs renders the violations for the page itself: the refs alone.
//
// The rejected page's own sentences are deliberately not quoted here. They were
// written by a pass that has just been caught inventing a receipt, and a reader
// skimming a digest should not have to work out that the words in the notice at
// the top are the ones that were thrown away.
func summarizeRefs(vs []Violation) string {
	return join(vs, func(v Violation) string {
		if v.Ref == "" {
			return string(v.Kind)
		}
		return "`" + v.Ref + "`"
	})
}

func join(vs []Violation, render func(Violation) string) string {
	parts := make([]string, 0, maxShown+1)
	for i, v := range vs {
		if i == maxShown {
			parts = append(parts, fmt.Sprintf("and %d more", len(vs)-maxShown))
			break
		}
		parts = append(parts, render(v))
	}
	return strings.Join(parts, "; ")
}
