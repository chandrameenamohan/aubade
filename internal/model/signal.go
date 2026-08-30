package model

import (
	"fmt"
	"strings"
	"time"
)

// Priority is Avery's P0..P4 weighting, taken from profile.md's "People who
// matter" list and then overridden by content. P0 is the most urgent.
type Priority string

// The five priorities. The vocabulary is the profile's, not ours — it is what
// the user already writes down, so nothing has to be translated.
const (
	P0 Priority = "P0"
	P1 Priority = "P1"
	P2 Priority = "P2"
	P3 Priority = "P3"
	P4 Priority = "P4"
)

// Priorities lists every priority, most urgent first.
var Priorities = []Priority{P0, P1, P2, P3, P4}

// Valid reports whether p is one of P0..P4.
func (p Priority) Valid() bool { return p.Rank() >= 0 }

// Rank is the sort key: 0 for P0 through 4 for P4, and -1 for anything else.
// Lower sorts first, which is the order the digest reads in.
func (p Priority) Rank() int {
	for i, known := range Priorities {
		if p == known {
			return i
		}
	}
	return -1
}

// ParsePriority reads a priority from text, tolerating case and surrounding
// whitespace ("p0", " P2 "). Anything else is an error rather than a default:
// guessing a priority is guessing how much of Avery's morning something is
// worth.
func ParsePriority(s string) (Priority, error) {
	p := Priority(strings.ToUpper(strings.TrimSpace(s)))
	if !p.Valid() {
		return "", fmt.Errorf("invalid priority %q; want one of P0, P1, P2, P3, P4", s)
	}
	return p, nil
}

// Confidence is how sure a signal is of itself. There are exactly two values,
// and "unsure" is a first-class outcome: SPEC §7 requires undecidable urgency
// to render under "I'm not sure" with the thread shown rather than be guessed.
type Confidence string

// The two confidence levels.
const (
	Certain Confidence = "certain"
	Unsure  Confidence = "unsure"
)

// Valid reports whether c is "certain" or "unsure".
func (c Confidence) Valid() bool { return c == Certain || c == Unsure }

// Source is where a citation points. Every fact in the digest traces back to
// one of these four (SPEC "Binding contracts").
type Source string

// The four citable sources.
const (
	SourceEmail    Source = "email"
	SourceCalendar Source = "calendar"
	SourceNote     Source = "note"
	SourceTask     Source = "task"
)

// Valid reports whether s is a citable source.
func (s Source) Valid() bool {
	switch s {
	case SourceEmail, SourceCalendar, SourceNote, SourceTask:
		return true
	}
	return false
}

// Citation is the receipt on a signal: which source, and which record in it —
// an email id, a calendar UID, a note path, or a task id.
type Citation struct {
	Source Source `json:"source"`
	Ref    string `json:"ref"`
}

// Signal kinds. The vocabulary is exactly the toolbox's extractor names
// (SPEC §2) on purpose: traps.json, `aubade tool <name>` and signals.json all
// speak one dialect, so a trap can name the extractor that missed it without a
// translation table in between.
const (
	KindCommitments    = "commitments"
	KindQuietThreads   = "quiet-threads"
	KindConflicts      = "conflicts"
	KindContradictions = "contradictions"
	KindDispatchables  = "dispatchables"
	KindSuppressions   = "suppressions"
	KindStaleness      = "staleness"
)

// KnownKinds is the signal-emitting extractor set. `thread` and `search` are
// investigation tools for the orchestrator and emit no signals, so they are not
// here.
var KnownKinds = []string{
	KindCommitments,
	KindQuietThreads,
	KindConflicts,
	KindContradictions,
	KindDispatchables,
	KindSuppressions,
	KindStaleness,
}

// IsKnownKind reports whether kind is in the published vocabulary. Validate
// does not enforce it — an extractor may legitimately coin a kind before the
// list catches up — but the eval harness uses it to notice drift.
func IsKnownKind(kind string) bool {
	for _, k := range KnownKinds {
		if k == kind {
			return true
		}
	}
	return false
}

// Digest section hints. A signal says where it belongs; the renderer (or the
// orchestrator) decides whether it makes the page.
const (
	SectionOneThingNow = "one-thing-now"
	SectionUrgentToday = "urgent-today"
	SectionDecisions   = "decisions-approvals"
	SectionPulse       = "team-product-pulse"
	SectionCalendar    = "calendar-personal"
	SectionNotSure     = "not-sure"
	SectionHonesty     = "honesty"
)

// KnownSectionHints is the default digest's section order, top to bottom.
var KnownSectionHints = []string{
	SectionOneThingNow,
	SectionUrgentToday,
	SectionDecisions,
	SectionPulse,
	SectionCalendar,
	SectionNotSure,
	SectionHonesty,
}

// IsKnownSectionHint reports whether hint names a default-digest section.
func IsKnownSectionHint(hint string) bool {
	for _, s := range KnownSectionHints {
		if s == hint {
			return true
		}
	}
	return false
}

// Signal is one cited fact produced by the deterministic toolbox — the unit
// both the digest and the eval harness are written against.
//
// The JSON shape is the binding signals.json contract (SPEC "Binding
// contracts"): id, kind, priority, title, detail, citations[], section_hint,
// confidence, deadline?.
type Signal struct {
	ID          string     `json:"id"`
	Kind        string     `json:"kind"`
	Priority    Priority   `json:"priority"`
	Title       string     `json:"title"`
	Detail      string     `json:"detail"`
	Citations   []Citation `json:"citations"`
	SectionHint string     `json:"section_hint"`
	Confidence  Confidence `json:"confidence"`
	Deadline    *time.Time `json:"deadline,omitempty"`
}

// Validate reports the first contract violation in s.
//
// The load-bearing rule is the citation one: a signal with no citation is a
// claim with no receipt, and the whole architecture rests on facts only
// entering the digest through cited tool output (HLD §2). An uncitable signal
// is a bug in the extractor, not a signal to render carefully.
func (s Signal) Validate() error {
	if strings.TrimSpace(s.ID) == "" {
		return fmt.Errorf(`signal field "id" is empty`)
	}
	if strings.TrimSpace(s.Kind) == "" {
		return fmt.Errorf(`signal %s: field "kind" is empty`, s.ID)
	}
	if !s.Priority.Valid() {
		return fmt.Errorf(`signal %s: invalid priority %q; want one of P0, P1, P2, P3, P4`, s.ID, s.Priority)
	}
	if strings.TrimSpace(s.Title) == "" {
		return fmt.Errorf(`signal %s: field "title" is empty`, s.ID)
	}
	if strings.TrimSpace(s.SectionHint) == "" {
		return fmt.Errorf(`signal %s: field "section_hint" is empty`, s.ID)
	}
	if !s.Confidence.Valid() {
		return fmt.Errorf(`signal %s: invalid confidence %q; want "certain" or "unsure"`, s.ID, s.Confidence)
	}
	if len(s.Citations) == 0 {
		return fmt.Errorf("signal %s: no citations; every signal must cite at least one source", s.ID)
	}
	for i, c := range s.Citations {
		if !c.Source.Valid() {
			return fmt.Errorf(`signal %s: citation[%d] source %q; want one of email, calendar, note, task`, s.ID, i, c.Source)
		}
		if strings.TrimSpace(c.Ref) == "" {
			return fmt.Errorf("signal %s: citation[%d] has an empty ref", s.ID, i)
		}
	}
	return nil
}

// Signals is a set of signals — the shape of signals.json.
type Signals []Signal

// Validate checks every signal and rejects duplicate ids. Duplicate ids are
// worth their own check because the eval harness indexes by id: two signals
// sharing one would make a trap silently ungradeable.
func (ss Signals) Validate() error {
	seen := make(map[string]int, len(ss))
	for i, s := range ss {
		if err := s.Validate(); err != nil {
			return fmt.Errorf("signals[%d]: %w", i, err)
		}
		if first, dup := seen[s.ID]; dup {
			return fmt.Errorf("signals[%d]: duplicate signal id %q (first at index %d)", i, s.ID, first)
		}
		seen[s.ID] = i
	}
	return nil
}
