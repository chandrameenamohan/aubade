package datagen

import (
	_ "embed"
	"errors"
	"fmt"
	"math/rand/v2"

	"github.com/chandrameenamohan/aubade/internal/model"
)

// This file assembles the whole corpus: the scenarios' exam (Build), plus the
// filler that makes finding it work, plus the one artifact that is neither —
// profile.md, which is the user's own document and is reproduced verbatim.

// TargetEmails is the size of the inbox the SPEC asks for ("~500 emails, 30
// days"). It is an exact target rather than an approximate one because "the
// same seed produces byte-identical output" is easier to believe about a corpus
// whose size is a constant than about one whose size is an emergent property of
// a loop.
const TargetEmails = 500

// noisePercent is how much of the inbox is newsletters, marketing, recruiter
// cold mail and machine notifications (SPEC §1, "realistic mix incl. ~30%
// noise"). It is a property of the *whole* inbox, scenario messages included,
// so the filler subtracts what the negative traps already planted.
const noisePercent = 30

// fillerStream is the second word of the filler's PCG seed. The filler draws
// from its own stream rather than sharing the scenarios' generator so that
// adding a scenario re-rolls the exam and not four hundred unrelated
// newsletters — a diff nobody can review is a diff nobody does review.
const fillerStream uint64 = 0xf111e40d1ce5eed1

//go:embed assets/profile.md
var profileMarkdown string

// Profile is profile.md: Avery's own document, reproduced verbatim from the
// assignment's appendix.
//
// It is embedded rather than generated, and that is the point. Every other file
// in the corpus is ours to invent; this one is the user's, it is what the engine
// is graded against ("in Avery's voice", "the suppressions she actually wrote"),
// and a paraphrase of it would quietly move the goalposts — the digest would be
// scored against rules we wrote for ourselves. The seed does not touch it and
// --today does not touch it.
func Profile() string { return profileMarkdown }

// Generate builds the corpus `aubade-lab generate` writes: the scenario scripts'
// planted traps, then the filler woven around them.
//
// The two layers are kept apart deliberately (see the package doc). Build owns
// every graded artifact and its answer key; the filler owns volume, and is only
// allowed to add artifacts no trap depends on. Nothing here can change what a
// trap asserts, which is why the plan is re-validated afterwards: if the filler
// ever collided with a scenario id or wandered outside the corpus window, the
// generator refuses to write rather than shipping a corpus whose answer key is
// subtly wrong.
func Generate(cfg Config) (*Plan, error) {
	plan, err := Build(cfg)
	if err != nil {
		return nil, err
	}

	s := &Script{
		today: plan.Today,
		loc:   model.Location(),
		rng:   rand.New(rand.NewPCG(uint64(cfg.Seed), fillerStream)),
		plan:  plan,
	}
	newFiller(s).run()
	if err := errors.Join(s.errs...); err != nil {
		return nil, fmt.Errorf("datagen: filler: %w", err)
	}

	plan.sort()
	if err := plan.validate(); err != nil {
		return nil, fmt.Errorf("datagen: filler broke the plan: %w", err)
	}
	return plan, nil
}
