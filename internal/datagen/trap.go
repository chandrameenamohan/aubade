package datagen

import (
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/chandrameenamohan/aubade/internal/model"
)

// Trap categories — the kinds of situation the exam plants, in the vocabulary a
// human uses to talk about them.
//
// Kind answers "what kind of question is this?" and expect.signal_kind answers
// "which extractor has to answer it". Keeping them separate is what makes a
// scorecard readable in two directions: by situation ("every commitment slip
// was caught, every calendar trap was missed") and by component ("conflicts is
// the extractor to go and fix").
const (
	CommitmentSlip      = "commitment-slip"
	QuietInvestor       = "quiet-investor"
	CadenceSlowdown     = "cadence-slowdown"
	StalledLoop         = "stalled-loop"
	CalendarOverlap     = "calendar-overlap"
	DeepWorkViolation   = "deep-work-violation"
	FamilyCollision     = "family-collision"
	SourceContradiction = "source-contradiction"
	QuickReply          = "quick-reply"
	RecruiterPattern    = "recruiter-pattern"
	StaleData           = "stale-data"

	// The negative half: situations the digest must leave alone.
	Newsletter      = "newsletter"
	VendorMarketing = "vendor-marketing"
	FYIOnly         = "fyi-only"
	LastWord        = "last-word"
	AcceptedInvite  = "accepted-invite"
	BelowThreshold  = "below-threshold"
)

// KnownCategories is every trap category the answer key may use.
var KnownCategories = []string{
	CommitmentSlip, QuietInvestor, CadenceSlowdown, StalledLoop,
	CalendarOverlap, DeepWorkViolation, FamilyCollision, SourceContradiction,
	QuickReply, RecruiterPattern, StaleData,
	Newsletter, VendorMarketing, FYIOnly, LastWord, AcceptedInvite, BelowThreshold,
}

// IsKnownCategory reports whether kind is a published trap category.
func IsKnownCategory(kind string) bool { return slices.Contains(KnownCategories, kind) }

// Trap is one entry of traps.json — the answer key for one planted task.
//
// The JSON shape is the binding contract from SPEC ("Binding contracts"):
// {id, kind, description, must_surface, expect{signal_kind, keywords[]},
// planted_refs[]}.
//
// Two fields deserve their rationale written down, because they are where an
// eval quietly goes wrong:
//
// PlantedRefs carries model.Citation values ({source, ref}) rather than bare
// strings. A bare "e-042" cannot be resolved without guessing which source it
// belongs to, and a ref that resolves against the wrong source is an answer key
// that grades nothing. With the source named, Plan.ResolveRefs is a lookup, not
// a heuristic — and the same struct is what a Signal cites, so a grader can
// compare a trap's refs to a signal's citations directly.
//
// Expect.SignalKind means different things for a positive and a negative trap,
// and both are actionable (EVAL-PRINCIPLES #1):
//
//   - MustSurface true: the extractor that must produce a signal for this trap.
//     A miss names it — "commitments missed trap commitment-cap-table-slip".
//   - MustSurface false: the extractor that owns keeping this out. A false
//     positive names it — "quiet-threads surfaced negative-avery-last-word",
//     which points at the boundary that needs the fix.
type Trap struct {
	ID          string           `json:"id"`
	Kind        string           `json:"kind"`
	Description string           `json:"description"`
	MustSurface bool             `json:"must_surface"`
	Expect      Expect           `json:"expect"`
	PlantedRefs []model.Citation `json:"planted_refs"`
}

// Expect is how a grader decides whether a trap was caught.
//
// Keywords is a disjunction by contract — SPEC says "≥1 must appear in digest
// text" — because prose is the one thing we refuse to pin: demanding an exact
// sentence would grade the renderer's phrasing instead of the engine's recall
// (EVAL-PRINCIPLES #6). Every keyword we ship is nonetheless planted verbatim
// in the trap's own artifacts, so any of them is quotable from cited evidence;
// TestTrapKeywordsArePlanted holds that line.
type Expect struct {
	SignalKind string   `json:"signal_kind"`
	Keywords   []string `json:"keywords"`
}

// Validate reports the first contract violation in t.
func (t Trap) Validate() error {
	if strings.TrimSpace(t.ID) == "" {
		return fmt.Errorf(`trap field "id" is empty`)
	}
	if !IsKnownCategory(t.Kind) {
		return fmt.Errorf("trap %s: kind %q is not a trap category; want one of %s",
			t.ID, t.Kind, strings.Join(KnownCategories, ", "))
	}
	if strings.TrimSpace(t.Description) == "" {
		return fmt.Errorf("trap %s: description is empty", t.ID)
	}
	if !model.IsKnownKind(t.Expect.SignalKind) {
		return fmt.Errorf("trap %s: expect.signal_kind %q is not an extractor; want one of %s",
			t.ID, t.Expect.SignalKind, strings.Join(model.KnownKinds, ", "))
	}
	if len(t.Expect.Keywords) == 0 {
		return fmt.Errorf("trap %s: expect.keywords is empty; a trap with no keyword cannot be graded against the digest text", t.ID)
	}
	for i, k := range t.Expect.Keywords {
		if strings.TrimSpace(k) == "" {
			return fmt.Errorf("trap %s: expect.keywords[%d] is blank", t.ID, i)
		}
	}
	if len(t.PlantedRefs) == 0 {
		return fmt.Errorf("trap %s: no planted_refs; a trap that planted nothing is a claim about the corpus, not a task in it", t.ID)
	}
	for i, r := range t.PlantedRefs {
		if !r.Source.Valid() {
			return fmt.Errorf(`trap %s: planted_refs[%d] source %q; want one of email, calendar, note, task`, t.ID, i, r.Source)
		}
		if strings.TrimSpace(r.Ref) == "" {
			return fmt.Errorf("trap %s: planted_refs[%d] has an empty ref", t.ID, i)
		}
	}
	return nil
}

// Traps is the whole answer key — the shape of traps.json.
type Traps []Trap

// Validate checks every trap and rejects duplicate ids. Duplicates get their
// own check because the harness indexes the key by id: two traps sharing one
// would make a task silently ungradeable, which is the failure mode an answer
// key exists to prevent.
func (ts Traps) Validate() error {
	seen := make(map[string]int, len(ts))
	for i, t := range ts {
		if err := t.Validate(); err != nil {
			return fmt.Errorf("traps[%d]: %w", i, err)
		}
		if first, dup := seen[t.ID]; dup {
			return fmt.Errorf("traps[%d]: duplicate trap id %q (first at index %d)", i, t.ID, first)
		}
		seen[t.ID] = i
	}
	return nil
}

// Positive returns the traps that must surface in the digest.
func (ts Traps) Positive() Traps { return ts.filter(true) }

// Negative returns the traps that must not surface — the suppression half of
// the exam. An eval with only positive tasks teaches an engine to say yes to
// everything (EVAL-PRINCIPLES #8), which is exactly the digest Avery already
// gets from her inbox.
func (ts Traps) Negative() Traps { return ts.filter(false) }

func (ts Traps) filter(mustSurface bool) Traps {
	out := make(Traps, 0, len(ts))
	for _, t := range ts {
		if t.MustSurface == mustSurface {
			out = append(out, t)
		}
	}
	return out
}

// SignalKinds returns the distinct expect.signal_kind values in the key, in the
// canonical model.KnownKinds order. The catalog is asserted to cover every
// extractor: an extractor with no task behind it is an extractor nobody would
// notice breaking.
func (ts Traps) SignalKinds() []string {
	present := make(map[string]bool, len(ts))
	for _, t := range ts {
		present[t.Expect.SignalKind] = true
	}
	out := make([]string, 0, len(present))
	for _, k := range model.KnownKinds {
		if present[k] {
			out = append(out, k)
		}
	}
	return out
}

// ByID finds a trap by id.
func (ts Traps) ByID(id string) (Trap, bool) {
	for _, t := range ts {
		if t.ID == id {
			return t, true
		}
	}
	return Trap{}, false
}

// EncodeTraps writes the answer key as traps.json: indented, with a trailing
// newline, and with HTML escaping off so a subject line containing "&" survives
// the round trip as itself. Byte-stability matters here as much as anywhere —
// the same seed must produce the same key, or "same seed, byte-identical
// output" is a claim about only part of the output.
func EncodeTraps(w io.Writer, traps Traps) error {
	if err := traps.Validate(); err != nil {
		return fmt.Errorf("datagen: refusing to write an invalid answer key: %w", err)
	}
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(traps)
}
