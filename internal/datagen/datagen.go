// Package datagen writes aubade's exam and its answer key in one pass.
//
// The corpus `aubade-lab generate` ships is not a random pile of plausible
// email. It is a set of **scenario scripts** (HLD §6): each planted trap is a
// first-class object that emits its own artifacts — the messages, the calendar
// events, the notes, the tasks — *and* the traps.json entry that grades them.
// The answer key is therefore a return value of the thing it grades, and cannot
// drift from it.
//
// What this package owns and what it does not:
//
//   - It owns the scenarios (the questions), the trap contract (the answer
//     key), and the invariants that keep the two honest: every trap names a
//     real extractor, cites at least one artifact, and every artifact it cites
//     exists in the plan it came back with.
//   - It does not own the filler around the traps (the ~30% newsletters and
//     internal chatter that make recall non-trivial), and it does not write
//     files. Both consume a Plan; neither can change what a trap asserts.
//
// Determinism has two halves, and they are deliberately different:
//
//   - **Today** shifts everything. Scenarios are written against the anchor
//     date — "four business days ago", "the next Tuesday" — so the corpus stays
//     evergreen and a trap keeps its meaning under any --today.
//   - **Seed** shifts nothing that is graded. It drives only interchangeable
//     detail (which phrasing a recruiter used, what minute a reply landed) and
//     the filler layer built on top. An exam whose questions move with the seed
//     could not have a golden digest, and a trap that is only sometimes present
//     is a flaky test wearing a dataset's clothes.
package datagen

import (
	"cmp"
	"errors"
	"fmt"
	"math/rand/v2"
	"slices"
	"time"

	"github.com/chandrameenamohan/aubade/internal/model"
)

// CorpusDays is the span of the synthetic corpus: 30 days of history ending on
// the anchor date (SPEC §1).
const CorpusDays = 30

// lookaheadDays is how far past the anchor date the calendar is allowed to
// reach. The digest is about today, but "someone booked over your Tuesday
// deep-work block" is next week's meeting and this week's problem.
const lookaheadDays = 14

// seedStream is the second word of the PCG seed. It is a constant so that one
// --seed always names one stream, and it is not zero so that --seed 0 is still
// a seeded generator rather than a degenerate one.
const seedStream uint64 = 0xa17bade0d1ce5eed

// Config is what a generator run needs to know.
type Config struct {
	// Seed drives interchangeable detail and the filler layer. Same seed,
	// byte-identical output.
	Seed int64
	// Today is the anchor date. A zero value means the system date in the
	// corpus zone — the CLI's documented default, resolved here so that every
	// caller gets the same normalization (midnight, America/Los_Angeles).
	Today time.Time
}

// Plan is one generated exam: every artifact the scenarios planted, and the
// answer key that grades them.
//
// It is an intermediate, not a file format. The writer turns it into
// inbox.jsonl / calendar.ics / notes/ / tasks.md / traps.json; the eval harness
// reads the same traps back out of the file it wrote.
type Plan struct {
	Seed   int64            `json:"seed"`
	Today  time.Time        `json:"today"`
	Emails []model.Email    `json:"emails"`
	Events []model.CalEvent `json:"events"`
	Notes  []model.Note     `json:"notes"`
	Tasks  []model.Task     `json:"tasks"`
	Traps  Traps            `json:"traps"`
}

// Build runs the whole catalog and returns the plan it produced.
//
// An error from Build is a bug in a scenario, not bad input: the only caller is
// our own generator and the only data is our own catalog. It is still an error
// rather than a panic, because the thing most likely to hit it is a builder
// editing a script, and a named invariant beats a stack trace.
func Build(cfg Config) (*Plan, error) { return build(cfg, Catalog()) }

func build(cfg Config, scenarios []Scenario) (*Plan, error) {
	loc := model.Location()
	today := cfg.Today
	if today.IsZero() {
		today = time.Now().In(loc)
	}
	y, m, d := today.In(loc).Date()
	today = time.Date(y, m, d, 0, 0, 0, 0, loc)

	plan := &Plan{Seed: cfg.Seed, Today: today}
	s := &Script{
		today: today,
		loc:   loc,
		rng:   rand.New(rand.NewPCG(uint64(cfg.Seed), seedStream)),
		plan:  plan,
	}

	for i, scenario := range scenarios {
		before := len(s.errs)
		trap := scenario(s)
		if trap.ID == "" {
			s.errs = append(s.errs, fmt.Errorf("scenario at index %d returned a trap with no id", i))
			continue
		}
		s.attribute(before, trap.ID)
		plan.Traps = append(plan.Traps, trap)
	}
	if err := errors.Join(s.errs...); err != nil {
		return nil, fmt.Errorf("datagen: %w", err)
	}

	plan.sort()
	if err := plan.validate(); err != nil {
		return nil, fmt.Errorf("datagen: %w", err)
	}
	return plan, nil
}

// sort puts the plan in the order the corpus is read in.
//
// Emails go in timeline order because that is what "woven into the timeline"
// has to mean once several scenarios are running at once: a thread that went
// quiet is only detectable as quiet if the messages around it are interleaved
// by time rather than grouped by the script that wrote them. Ties break on id
// so the order is total, and therefore reproducible.
func (p *Plan) sort() {
	slices.SortStableFunc(p.Emails, func(a, b model.Email) int {
		if c := a.TS.Compare(b.TS); c != 0 {
			return c
		}
		return cmp.Compare(a.ID, b.ID)
	})
	slices.SortStableFunc(p.Events, func(a, b model.CalEvent) int {
		if c := a.Start.Compare(b.Start); c != 0 {
			return c
		}
		return cmp.Compare(a.UID, b.UID)
	})
	// Notes sort by path because that is the order localfs walks them in;
	// tasks keep catalog order, which is the order they will be written to
	// tasks.md.
	slices.SortStableFunc(p.Notes, func(a, b model.Note) int { return cmp.Compare(a.Path, b.Path) })
}

// validate holds the invariants that make the plan an exam rather than a pile
// of fixtures: unique artifact ids, a valid answer key, every planted_ref
// resolving to something that was actually planted, and every artifact inside
// the corpus window.
func (p *Plan) validate() error {
	var errs []error

	byID := make(map[string]model.Email, len(p.Emails))
	for _, e := range p.Emails {
		if _, dup := byID[e.ID]; dup {
			errs = append(errs, fmt.Errorf("duplicate email id %q", e.ID))
		}
		byID[e.ID] = e
		errs = append(errs, p.inWindow("email "+e.ID, e.TS, CorpusDays, 1))
	}
	// A reply that predates the message it answers reads as a plausible thread
	// and is not one: it inverts every gap a quiet-thread or cadence trap is
	// measured in. Two scenario messages sharing a day and drawing their hour
	// from the seed is all it takes, so the invariant is checked rather than
	// remembered.
	for _, e := range p.Emails {
		if e.InReplyTo == "" {
			continue
		}
		parent, ok := byID[e.InReplyTo]
		if !ok {
			errs = append(errs, fmt.Errorf("email %s replies to %q, which nobody planted", e.ID, e.InReplyTo))
			continue
		}
		if e.TS.Before(parent.TS) {
			errs = append(errs, fmt.Errorf("email %s (%s) replies to %s, which was sent later (%s)",
				e.ID, e.TS.Format(time.RFC3339), parent.ID, parent.TS.Format(time.RFC3339)))
		}
	}
	seenEvent := map[string]bool{}
	for _, ev := range p.Events {
		if seenEvent[ev.UID] {
			errs = append(errs, fmt.Errorf("duplicate event UID %q", ev.UID))
		}
		seenEvent[ev.UID] = true
		errs = append(errs, p.inWindow("event "+ev.UID, ev.Start, CorpusDays, lookaheadDays))
	}
	seenNote := map[string]bool{}
	for _, n := range p.Notes {
		if seenNote[n.Path] {
			errs = append(errs, fmt.Errorf("duplicate note path %q", n.Path))
		}
		seenNote[n.Path] = true
		if n.HasDate() {
			errs = append(errs, p.inWindow("note "+n.Path, n.Date, CorpusDays, 0))
		}
	}
	seenTask := map[string]bool{}
	for _, t := range p.Tasks {
		if seenTask[t.ID] {
			errs = append(errs, fmt.Errorf("duplicate task id %q", t.ID))
		}
		seenTask[t.ID] = true
	}

	if err := p.Traps.Validate(); err != nil {
		errs = append(errs, err)
	}
	for _, trap := range p.Traps {
		for _, ref := range trap.PlantedRefs {
			if !p.Resolve(ref) {
				errs = append(errs, fmt.Errorf("trap %s: planted_ref %s:%s resolves to nothing in the corpus",
					trap.ID, ref.Source, ref.Ref))
			}
		}
	}
	return errors.Join(errs...)
}

// inWindow rejects a timestamp outside the corpus span. It exists to catch the
// typo — a year or a month off — that would otherwise produce a corpus the
// extractors quietly never look at, and an eval that quietly always passes.
func (p *Plan) inWindow(what string, ts time.Time, backDays, forwardDays int) error {
	earliest := p.Today.AddDate(0, 0, -backDays)
	latest := p.Today.AddDate(0, 0, forwardDays+1)
	if ts.Before(earliest) || !ts.Before(latest) {
		return fmt.Errorf("%s is dated %s, outside the corpus window %s..%s",
			what, ts.Format(time.RFC3339), earliest.Format("2006-01-02"), latest.Format("2006-01-02"))
	}
	return nil
}

// Resolve reports whether a citation points at an artifact in this plan. It is
// the check that keeps the answer key answerable: a key that cites evidence the
// corpus does not contain grades a question nobody was asked.
func (p *Plan) Resolve(c model.Citation) bool {
	switch c.Source {
	case model.SourceEmail:
		return slices.ContainsFunc(p.Emails, func(e model.Email) bool { return e.ID == c.Ref })
	case model.SourceCalendar:
		return slices.ContainsFunc(p.Events, func(e model.CalEvent) bool { return e.UID == c.Ref })
	case model.SourceNote:
		return slices.ContainsFunc(p.Notes, func(n model.Note) bool { return n.Path == c.Ref })
	case model.SourceTask:
		return slices.ContainsFunc(p.Tasks, func(t model.Task) bool { return t.ID == c.Ref })
	}
	return false
}
