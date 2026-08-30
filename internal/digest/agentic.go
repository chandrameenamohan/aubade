package digest

import (
	"strings"
	"time"

	"github.com/chandrameenamohan/aubade/internal/model"
)

// This file is the whole surface the agentic composer (bead C3) is allowed to
// reach into this package for. It is deliberately two things and no more.
//
// The agentic page is written by a model and the fallback page is written by
// this package, and a reader must not be able to tell which one produced a
// citation or an honesty line. So the two parts of the page that are *not* the
// user's to shape — the honesty floor and the form a citation takes — are
// rendered here, by the same code that renders them in `--no-llm`, rather than
// re-implemented next to the orchestrator. A second implementation of "how
// aubade admits it is unsure" is a second thing to keep honest.

// HonestyFloor renders the parts of the page that customization cannot reach:
// the stale-or-missing-source banner, whatever could not be ranked, and
// everything the page cannot vouch for.
//
// SPEC §6 makes this structural rather than a rule in a prompt: "format is the
// user's, truthfulness is the product's". A prompt can ask for any shape at all
// and this text is appended regardless, because a `--customize` file that could
// remove the staleness banner would be a `--customize` file that can make the
// digest lie.
func (d *Digest) HonestyFloor() string {
	var b strings.Builder
	d.writeBanner(&b)
	for _, s := range d.Sections {
		if s.ID == model.SectionNotSure || s.ID == model.SectionHonesty {
			d.writeSection(&b, s)
		}
	}
	return b.String()
}

// Labeler resolves a citation ref against the corpus into the span a reader
// reads — *[email: Marcus, May 19 16:42]* — rather than the id a grader reads.
//
// The orchestrator asks the model to cite in the machine dialect ("[email:e-42]")
// precisely so every citation can be checked against signals.json before anyone
// sees it; this turns the checked refs into the page's own form afterwards. The
// order matters: validate the id, then render the name.
type Labeler struct{ idx *index }

// NewLabeler builds a labeler over a corpus.
func NewLabeler(c *model.Corpus, loc *time.Location) *Labeler {
	if loc == nil {
		loc = model.Location()
	}
	return &Labeler{idx: newIndex(c, loc)}
}

// Label renders one citation. An unresolvable ref renders as itself rather than
// disappearing: a citation nobody can follow is a bug worth seeing, and a line
// with its receipt silently removed is a claim with no receipt.
func (l *Labeler) Label(c model.Citation) string { return l.idx.label(c) }
