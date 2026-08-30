package eval

import (
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/chandrameenamohan/aubade/internal/digest"
	"github.com/chandrameenamohan/aubade/internal/model"
)

// The one check that reads a trial's transcript, and the narrow question it
// asks (EVAL-PRINCIPLES #12).
//
// The outcome graders in eval.go answer "what was produced". This answers
// "where did it come from": every citation printed on an agentic page must
// resolve to a citation in *that trial's own* signals.json. Not the previous
// trial's, and not the corpus at large — each trial is isolated (#11), so the
// fact base a page is checked against is the one written beside it.
//
// The check is deliberately not a score. It is a trip-wire on the keystone
// claim — facts enter the digest only through cited tool output — and a page
// that trips it is reporting a fabricated receipt, which is a different and
// worse thing than a missed trap.
//
// It reads the *rendered* citation spans rather than the machine dialect,
// because by the time a page is on disk the ids have already been resolved into
// the form a human reads. So the allowed set is built the same way the page was:
// every citation in the fact base, run through the same labeller the renderer
// used. If the two ever disagree about how a citation is written, this check
// says so — which is itself worth knowing.

// citationSpanRE matches a rendered citation: [email: Marcus, Aug 27 16:42],
// [cal: Q2 planning sync, added Aug 19 10:00], [note: notes/kickoff.md],
// [task: tasks.md:4] — and the raw fallback forms the labeller emits for a ref
// it cannot resolve.
//
// The source alternation is closed so ordinary bracketed prose and markdown
// links are not mistaken for receipts. A checker that cries wolf gets ignored.
var citationSpanRE = regexp.MustCompile(`\[(email|cal|calendar|note|task):[^\]\n]{1,200}\]`)

// toolInvocation is how a toolbox call appears in a runner transcript. It is
// counted for the report only — which tools the agent chose is never graded
// (#6) — but "the loop composed a page without calling the toolbox once" is a
// fact a reader of the card wants in front of them.
const toolInvocation = "aubade tool"

// Grounding is what the transcript check found in one trial.
type Grounding struct {
	// Cited is how many citation spans the page carries.
	Cited int

	// Ungrounded are the spans no citation in this trial's fact base accounts
	// for, deduplicated, in page order.
	Ungrounded []string

	// ToolCalls is how many toolbox invocations appear in the transcript, or
	// -1 when there is no transcript to read.
	ToolCalls int
}

// OK reports whether every citation on the page is grounded. A page with no
// citations at all is not OK: a digest whose every line is unverifiable is the
// exact failure this architecture exists to prevent.
func (g Grounding) OK() bool { return g.Cited > 0 && len(g.Ungrounded) == 0 }

// CheckGrounding resolves every citation on a trial's page against that trial's
// own fact base.
func CheckGrounding(a *Artifacts, corpus *model.Corpus, loc *time.Location) Grounding {
	g := Grounding{ToolCalls: -1}
	if a == nil {
		return g
	}
	if a.Transcript != "" {
		g.ToolCalls = strings.Count(a.Transcript, toolInvocation)
	}

	allowed := renderedCitations(a.Signals, corpus, loc)
	for _, span := range citationSpanRE.FindAllString(a.Digest, -1) {
		g.Cited++
		if allowed[span] || slices.Contains(g.Ungrounded, span) {
			continue
		}
		g.Ungrounded = append(g.Ungrounded, span)
	}
	return g
}

// renderedCitations is every citation in the fact base, in the form the page
// prints it.
func renderedCitations(ss model.Signals, corpus *model.Corpus, loc *time.Location) map[string]bool {
	out := map[string]bool{}
	if corpus == nil {
		return out
	}
	label := digest.NewLabeler(corpus, loc)
	for _, s := range ss {
		for _, c := range s.Citations {
			out[label.Label(c)] = true
		}
	}
	return out
}
