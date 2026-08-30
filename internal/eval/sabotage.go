package eval

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/chandrameenamohan/aubade/internal/datagen"
	"github.com/chandrameenamohan/aubade/internal/digest"
	"github.com/chandrameenamohan/aubade/internal/extract"
	"github.com/chandrameenamohan/aubade/internal/model"
)

// Sabotage is the check on the checkers (EVAL-PRINCIPLES #17).
//
// An eval that always says 100% has stopped measuring and started certifying
// (#16). The only way to tell the difference from the outside is to break the
// engine on purpose and watch the number: disable one extractor, re-grade, and
// if the score does not fall then the graders cannot see that extractor at all.
// That is an ALARM, not a pass — it means the tasks behind that extractor were
// being scored by something else, and it would have gone on being green through
// a total failure of the component.
//
// It runs on demand, never in `make check`. Sabotage is a check on the exam,
// not on the commit, and a gate that fails because a deliberately broken engine
// scored badly teaches people to ignore the gate.
//
// # Why this composes in-process
//
// The regression suite grades what the real binaries wrote, because the thing
// we ship is the thing we test. Sabotage cannot: there is no `aubade digest
// --break-one-extractor` flag and there must not be — a customer-facing binary
// that can disable its own honesty layer is a liability, and a flag that exists
// only for the harness is harness tooling smuggled into the product (the same
// boundary that keeps `eval` out of `aubade`).
//
// So the harness composes both sides itself, through the toolbox's own public
// surface: `Toolbox.Run(kind, "")` is exactly the code path `aubade tool <kind>`
// takes, and running every kind but one reproduces `Toolbox.All()` minus that
// one. Both the baseline and the sabotaged page are built the same way, so the
// comparison is like for like — which is the only property the alarm depends on.

// Sabotage is one sabotage run: the same corpus graded twice, once whole and
// once with an extractor disabled.
type Sabotage struct {
	// Extractor is the disabled one.
	Extractor string

	// Baseline and Broken are the two graded results.
	Baseline *Result
	Broken   *Result

	// Alarm is true when the score did not fall.
	Alarm bool

	// Blind names the tasks that passed anyway despite expecting the disabled
	// extractor — the specific reason the alarm fired, or the near-misses worth
	// reading when it did not.
	Blind []string
}

// Drop is how far the score fell, in tasks.
func (s *Sabotage) Drop() int {
	base, _ := s.Baseline.Score()
	broke, _ := s.Broken.Score()
	return base - broke
}

// SabotageInput is what a sabotage run needs. It is the corpus and the anchor
// day — everything else it builds itself, twice.
type SabotageInput struct {
	Corpus *model.Corpus
	Today  time.Time
	Loc    *time.Location
	Traps  datagen.Traps

	// Extractor is the kind to disable, by its `aubade tool` name.
	Extractor string
}

// RunSabotage grades the corpus whole, then again with one extractor disabled.
func RunSabotage(in SabotageInput) (*Sabotage, error) {
	if !slices.Contains(extract.Kinds(), in.Extractor) {
		return nil, fmt.Errorf("unknown extractor %q; one of: %s",
			in.Extractor, strings.Join(extract.Kinds(), ", "))
	}

	baseline, err := Compose(in.Corpus, in.Today, in.Loc, "")
	if err != nil {
		return nil, err
	}
	broken, err := Compose(in.Corpus, in.Today, in.Loc, in.Extractor)
	if err != nil {
		return nil, err
	}

	s := &Sabotage{
		Extractor: in.Extractor,
		Baseline:  Grade(in.Traps, baseline),
		Broken:    Grade(in.Traps, broken),
	}
	s.Alarm = s.Drop() <= 0

	// The tasks the answer key hangs on the disabled extractor are the ones the
	// alarm is really about: if they still pass with it gone, the grader was
	// never reading it.
	for _, trap := range in.Traps {
		if trap.Expect.SignalKind != in.Extractor || !trap.MustSurface {
			continue
		}
		if r, ok := s.Broken.Get(trap.ID); ok && r.Passed {
			s.Blind = append(s.Blind, fmt.Sprintf("%s still passes (%s)", trap.ID, r.Reason))
		}
	}
	return s, nil
}

// Compose builds the deterministic page and the fact base it was composed from,
// optionally with one extractor left out.
//
// An empty skip runs `Toolbox.All` — byte for byte the path `aubade digest
// --no-llm` takes — so the reference digest this produces is the product's own
// output rather than a lookalike. A named skip runs every other extractor
// through `Toolbox.Run`, which is the path `aubade tool <kind>` takes, so the
// sabotaged half differs from the baseline in exactly one thing: the extractor
// that did not run.
func Compose(corpus *model.Corpus, today time.Time, loc *time.Location, skip string) (*Artifacts, error) {
	tb, err := extract.New(corpus, today, loc)
	if err != nil {
		return nil, err
	}

	var signals model.Signals
	if skip == "" {
		if signals, err = tb.All(); err != nil {
			return nil, err
		}
	} else {
		for _, kind := range extract.Kinds() {
			if kind == skip {
				continue
			}
			res, err := tb.Run(kind, "")
			if err != nil {
				return nil, fmt.Errorf("extractor %s: %w", kind, err)
			}
			signals = append(signals, res.Signals...)
		}
		extract.SortSignals(signals)
	}

	page, err := digest.Build(digest.Input{
		Corpus:  corpus,
		Signals: signals,
		Now:     tb.Now(),
		Loc:     tb.Location(),
		Owner:   tb.Owner(),
		Mode:    digest.ModeNoLLM,
	})
	if err != nil {
		return nil, err
	}
	return &Artifacts{Digest: page.Markdown(), Signals: signals}, nil
}
