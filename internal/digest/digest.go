// Package digest turns a cited signal set into the one-page morning digest.
//
// This is the `--no-llm` half of the keystone decision (HLD §2): the same
// deterministic toolbox, run in a fixed order, composed by template instead of
// by a model. It exists for three reasons, in increasing order of importance —
// it works with no keys and no network, it is what a grader can always run
// cold, and it is what makes the agentic mode *testable*, because both modes
// must pass the same trap harness and the diff between them is itself signal.
//
// Three rules the composition is written to:
//
//   - **Nothing enters the page that did not come from a signal.** Every
//     factual line is rendered from a model.Signal and carries that signal's
//     citations, inline, in the sample digest's own form —
//     *[email: Marcus, May 19 16:42]*. The only prose this package authors is
//     section scaffolding and the honest sentence that says a section is empty.
//   - **The honesty layer is structural.** A stale inbox opens the page as a
//     banner; a contradiction renders both sides with a citation each and picks
//     neither; anything the toolbox marked `unsure` goes under "I'm not sure"
//     with its thread shown, and never gets promoted into an assertion by the
//     renderer. SPEC §7 makes fabricated certainty an eval failure, so certainty
//     is something only an extractor is allowed to claim.
//   - **The output is a pure function of its input.** Same corpus, same signals,
//     same --today produce byte-identical markdown; that is what a committed
//     golden digest is worth anything for. Nothing here reads the clock, the
//     network, or a map in iteration order.
//
// The drafting voice is the one place the page writes sentences somebody might
// send. It is two-layered on purpose (voice.go) and it will not invent the one
// thing it cannot know — the answer.
package digest

import (
	"fmt"
	"strings"
	"time"

	"github.com/chandrameenamohan/aubade/internal/model"
)

// DigestFile is the page `aubade digest` writes, relative to --out.
const DigestFile = "digest.md"

// Input is everything the composer reads. It is the toolbox's own view of the
// world — the corpus it ran over, the signals it produced, and the anchor
// instant it reasoned from — passed explicitly so a test can build a page
// without a filesystem.
type Input struct {
	// Corpus is the normalized source data. Citations resolve against it, and
	// the calendar section reads today's events from it directly, because an
	// agenda is a list of facts no extractor needs to have an opinion about.
	Corpus *model.Corpus

	// Signals is the toolbox's output, already sorted and validated.
	Signals model.Signals

	// Now is the anchor instant: the --today day at 06:00 in Loc, the moment
	// the digest is written for.
	Now time.Time

	// Loc is the zone every time on the page is expressed in.
	Loc *time.Location

	// Owner is whose morning this is.
	Owner model.Person

	// Mode names how the page was composed, for the footer.
	Mode string
}

// Digest is the composed page, before it is markdown.
//
// The intermediate form is deliberate: sectioning and scoring are the
// interesting decisions and they get their own tests, while Markdown stays a
// dumb, total rendering of whatever this struct says. It is also what a
// customized format (SPEC §6) would re-render without touching a fact.
type Digest struct {
	// Day is the anchor day, at midnight in Loc.
	Day   time.Time
	Loc   *time.Location
	Owner model.Person
	Mode  string

	// Banner is the honesty layer's opening: stale or missing sources, stated
	// before anything that depends on them.
	Banner []Item

	// Sections are in reading order, always the same shape.
	Sections []Section

	// Voice is the two-layer drafting voice that produced the drafts.
	Voice *Voice

	// Stats is what the footer reports about the run.
	Stats Stats
}

// Section is one heading and its contents.
type Section struct {
	ID      string
	Heading string

	// Empty is the honest sentence rendered when Items is empty. A section with
	// no Empty text is omitted entirely when it has nothing in it; a section
	// with one is always shown, because "nothing needs you today" is an answer
	// and a missing heading is not.
	Empty string

	// Paragraph renders the items as prose rather than as a list — the shape
	// the sample digest opens with.
	Paragraph bool

	Items []Item

	// Overflow is how many items the section held back to stay a one-pager.
	Overflow int
}

// Item is one rendered line: a bold lead, a body, its citations, and anything
// hanging off it.
type Item struct {
	// SignalID is the signal this line came from, empty for a calendar entry
	// (which is a fact read straight off the corpus).
	SignalID string

	Lead string
	Body string

	Citations []model.Citation

	// Refs are those citations already resolved against the corpus and rendered
	// the way a reader reads them — "[email: Marcus, May 19 16:42]" — one per
	// citation, in the same order.
	//
	// They are resolved at composition time rather than at render time so a
	// Digest is self-contained: a customized renderer (SPEC §6) works from this
	// struct alone and therefore cannot reach past it to add, drop, or re-word
	// a fact.
	Refs []string

	// Sides are the two halves of a contradiction, each with its own citation.
	// The renderer shows both and resolves neither.
	Sides []Side

	// Draft is the reply this item can be closed with, when it can.
	Draft *Draft
}

// Side is one half of a disagreement: what that source says, and the receipt
// for it.
type Side struct {
	Text     string
	Citation model.Citation
	Ref      string
}

// CiteSpan is the italic citation span that closes a factual line, or empty
// when there is nothing to cite.
func (it Item) CiteSpan() string { return citeSpan(it.Refs) }

// citeSpan joins rendered refs into one italic span:
// *[email: Marcus, May 19 16:42] [email: Ben, May 18 09:11]*.
func citeSpan(refs []string) string {
	if len(refs) == 0 {
		return ""
	}
	return "*" + strings.Join(refs, " ") + "*"
}

// Stats is the footer's account of the run.
type Stats struct {
	Signals    int
	Rendered   int
	Drafts     int
	Suppressed int
	Sources    []string
	Missing    []string
}

// Build composes the page.
//
// The order is fixed and each step is a function of the last: index the corpus
// so citations can be resolved, score every signal, route it to a section,
// then draft what can be dispatched. Nothing later can add a fact.
func Build(in Input) (*Digest, error) {
	if in.Corpus == nil {
		return nil, fmt.Errorf("digest: Build called with a nil corpus")
	}
	if in.Now.IsZero() {
		return nil, fmt.Errorf("digest: Build called with a zero anchor instant")
	}
	if err := in.Signals.Validate(); err != nil {
		return nil, fmt.Errorf("digest: refusing to compose from an invalid signal set: %w", err)
	}
	loc := in.Loc
	if loc == nil {
		loc = model.Location()
	}
	mode := in.Mode
	if mode == "" {
		mode = ModeNoLLM
	}

	local := in.Now.In(loc)
	d := &Digest{
		Day:   time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc),
		Loc:   loc,
		Owner: in.Owner,
		Mode:  mode,
		Voice: LoadVoice(in.Corpus.Profile),
	}

	idx := newIndex(in.Corpus, loc)
	c := &composer{in: in, idx: idx, loc: loc, now: in.Now, day: d.Day, voice: d.Voice}
	c.compose(d)
	return d, nil
}

// ModeNoLLM names the fixed-order, template-rendered composition.
const ModeNoLLM = "no-llm"
