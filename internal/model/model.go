// Package model is aubade's normalized Event model and the DataSource contract
// that fills it.
//
// Everything downstream — the deterministic toolbox, the digest renderer, the
// eval harness — reads these types and nothing else. That is the whole point of
// the layer: a provider (LocalFS today, Composio in week two, HLD §4) is
// responsible for turning whatever its wire format is into these structs, and
// for saying so loudly when it cannot. A field that reached this package is a
// field that was validated on the way in.
//
// Two failure modes are deliberately kept apart, because the digest treats them
// differently (HLD §7):
//
//   - A *missing* source is survivable. It surfaces as a MissingSourceError,
//     LoadCorpus records it on Corpus.Missing, and the honesty banner tells the
//     reader which source was not there.
//   - A *malformed* source is not. It surfaces as a ValidationError naming the
//     file and line, and it stops the load. Silently dropping a line we could
//     not parse is how a digest quietly stops mentioning the thing that
//     mattered.
package model

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	_ "time/tzdata" // embed the tz database: Location() must work on any machine
)

// DefaultTimeZone is the digest's anchor zone (SPEC "Dates"). Avery's day is a
// Pacific day, and "today" is a Pacific today.
const DefaultTimeZone = "America/Los_Angeles"

var (
	locOnce sync.Once
	loc     *time.Location
)

// Location returns the anchor timezone. time/tzdata is embedded above so this
// resolves identically on a developer laptop, in CI, and in a scratch container
// with no system zoneinfo — determinism is the property the whole eval rests on.
func Location() *time.Location {
	locOnce.Do(func() {
		l, err := time.LoadLocation(DefaultTimeZone)
		if err != nil {
			// Unreachable with tzdata embedded; UTC beats a panic in a 6am cron.
			l = time.UTC
		}
		loc = l
	})
	return loc
}

// Person is a named mailbox. Both halves are optional in the wild — a
// calendar ATTENDEE often has only an address, an email "from" usually has both
// — so consumers should read Email first and fall back to Name.
type Person struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

// String renders a person the way a citation or a draft would.
func (p Person) String() string {
	switch {
	case p.Name != "" && p.Email != "":
		return fmt.Sprintf("%s <%s>", p.Name, p.Email)
	case p.Name != "":
		return p.Name
	default:
		return p.Email
	}
}

// Email is one message. The JSON shape is the binding inbox.jsonl contract
// (SPEC "Binding contracts"): id, thread_id, ts (RFC3339 with a zone),
// from{name,email}, to[], cc[], subject, body, in_reply_to?, labels[]?.
//
// `to` and `cc` keep their keys even when empty because the contract lists them
// unconditionally; only in_reply_to and labels are optional.
type Email struct {
	ID        string    `json:"id"`
	ThreadID  string    `json:"thread_id"`
	TS        time.Time `json:"ts"`
	From      Person    `json:"from"`
	To        []Person  `json:"to"`
	CC        []Person  `json:"cc"`
	Subject   string    `json:"subject"`
	Body      string    `json:"body"`
	InReplyTo string    `json:"in_reply_to,omitempty"`
	Labels    []string  `json:"labels,omitempty"`
}

// Validate reports the first contract violation in e. Providers call it after
// decoding and wrap the result in a ValidationError carrying the file and line,
// so the message a user sees points at the byte that is wrong.
func (e Email) Validate() error {
	if strings.TrimSpace(e.ID) == "" {
		return fmt.Errorf(`field "id" is empty`)
	}
	if strings.TrimSpace(e.ThreadID) == "" {
		return fmt.Errorf(`field "thread_id" is empty (email %s)`, e.ID)
	}
	if e.TS.IsZero() {
		return fmt.Errorf(`field "ts" is missing (email %s); want RFC3339 with a zone`, e.ID)
	}
	if err := validAddress(e.From.Email); err != nil {
		return fmt.Errorf(`field "from": %w (email %s)`, err, e.ID)
	}
	if len(e.To) == 0 {
		return fmt.Errorf(`field "to" is empty (email %s)`, e.ID)
	}
	for i, p := range e.To {
		if err := validAddress(p.Email); err != nil {
			return fmt.Errorf(`field "to[%d]": %w (email %s)`, i, err, e.ID)
		}
	}
	for i, p := range e.CC {
		if err := validAddress(p.Email); err != nil {
			return fmt.Errorf(`field "cc[%d]": %w (email %s)`, i, err, e.ID)
		}
	}
	return nil
}

// validAddress is deliberately not RFC 5322. A full parser would reject
// addresses that work and accept ones that do not; what we actually need to
// catch is an empty or obviously non-address value where a mailbox belongs.
func validAddress(addr string) error {
	a := strings.TrimSpace(addr)
	if a == "" {
		return fmt.Errorf("email address is empty")
	}
	at := strings.Index(a, "@")
	if at <= 0 || at == len(a)-1 || strings.ContainsAny(a, " \t") {
		return fmt.Errorf("%q is not an email address", addr)
	}
	return nil
}

// EventStatus is the RFC 5545 STATUS of a VEVENT.
type EventStatus string

// The VEVENT status values aubade accepts. Anything else in a calendar is a
// malformed source, not a new state to guess at.
const (
	StatusConfirmed EventStatus = "CONFIRMED"
	StatusTentative EventStatus = "TENTATIVE"
	StatusCancelled EventStatus = "CANCELLED"
)

// Valid reports whether s is one of the three statuses aubade models.
func (s EventStatus) Valid() bool {
	switch s {
	case StatusConfirmed, StatusTentative, StatusCancelled:
		return true
	}
	return false
}

// PartStat is an attendee's RFC 5545 PARTSTAT — the field that makes "declined
// meetings" (a graded trap category) visible at all.
type PartStat string

// The participation statuses aubade accepts.
const (
	PartStatNeedsAction PartStat = "NEEDS-ACTION"
	PartStatAccepted    PartStat = "ACCEPTED"
	PartStatDeclined    PartStat = "DECLINED"
	PartStatTentative   PartStat = "TENTATIVE"
	PartStatDelegated   PartStat = "DELEGATED"
)

// Valid reports whether p is a participation status aubade models.
func (p PartStat) Valid() bool {
	switch p {
	case PartStatNeedsAction, PartStatAccepted, PartStatDeclined, PartStatTentative, PartStatDelegated:
		return true
	}
	return false
}

// Attendee is one invitee on an event, with the RSVP state that makes a decline
// detectable.
type Attendee struct {
	Person
	PartStat PartStat `json:"partstat"`
	Role     string   `json:"role,omitempty"`
}

// CalEvent is one normalized VEVENT.
//
// Created is the provenance timestamp (CREATED, else DTSTAMP): "Sam added this
// to the shared calendar at 21:04 last night" is a different fact from "the
// meeting is at 15:00", and the digest needs both.
type CalEvent struct {
	UID         string      `json:"uid"`
	Summary     string      `json:"summary"`
	Description string      `json:"description,omitempty"`
	Location    string      `json:"location,omitempty"`
	Start       time.Time   `json:"start"`
	End         time.Time   `json:"end"`
	AllDay      bool        `json:"all_day,omitempty"`
	Status      EventStatus `json:"status"`
	Organizer   Person      `json:"organizer"`
	Attendees   []Attendee  `json:"attendees,omitempty"`
	Created     time.Time   `json:"created"`
	Calendar    string      `json:"calendar,omitempty"`
}

// Duration is how long the event runs.
func (e CalEvent) Duration() time.Duration { return e.End.Sub(e.Start) }

// DeclinedBy reports whether the given address declined this event. Address
// comparison is case-insensitive, because calendars are inconsistent about it
// and a missed decline is exactly the kind of thing the digest exists to catch.
func (e CalEvent) DeclinedBy(addr string) bool {
	return e.PartStatOf(addr) == PartStatDeclined
}

// PartStatOf returns the RSVP state of the given address, or "" if that address
// is not on the invite.
func (e CalEvent) PartStatOf(addr string) PartStat {
	a := strings.TrimSpace(strings.ToLower(addr))
	if a == "" {
		return ""
	}
	for _, at := range e.Attendees {
		if strings.ToLower(at.Email) == a {
			return at.PartStat
		}
	}
	return ""
}

// Note is one markdown note from the notes directory.
//
// Date is what the note claims about itself (front matter), never the file's
// mtime: mtime is a property of the clone, not of the corpus, and a staleness
// banner computed from it would say something different on every machine.
type Note struct {
	Path      string            `json:"path"` // corpus-relative, slash-separated
	Title     string            `json:"title"`
	Date      time.Time         `json:"date"`
	Tags      []string          `json:"tags,omitempty"`
	Attendees []string          `json:"attendees,omitempty"`
	Meta      map[string]string `json:"meta,omitempty"`
	Body      string            `json:"body"`
}

// HasDate reports whether the note carried a date of its own. Notes without one
// cannot be aged, and the honesty layer should say so rather than assume fresh.
func (n Note) HasDate() bool { return !n.Date.IsZero() }

// Task is one line of tasks.md.
type Task struct {
	ID    string            `json:"id"`
	Title string            `json:"title"`
	Done  bool              `json:"done"`
	Due   time.Time         `json:"due"`
	Owner string            `json:"owner,omitempty"`
	Meta  map[string]string `json:"meta,omitempty"`
	Line  int               `json:"line"` // 1-based line in tasks.md, for citation
}

// HasDue reports whether the task carried a due date.
func (t Task) HasDue() bool { return !t.Due.IsZero() }

// MissingSource records a source that was not there. It is data, not an error,
// because a missing source is a thing the digest reports rather than a thing
// that stops it (HLD §7).
type MissingSource struct {
	Source string `json:"source"` // "email" | "calendar" | "note" | "task" | "profile"
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// Corpus is everything one DataSource had to offer, normalized.
type Corpus struct {
	Source  string          `json:"source"` // provider name, e.g. "localfs:data"
	Emails  []Email         `json:"emails"`
	Events  []CalEvent      `json:"events"`
	Notes   []Note          `json:"notes"`
	Tasks   []Task          `json:"tasks"`
	Profile *Profile        `json:"profile,omitempty"`
	Missing []MissingSource `json:"missing,omitempty"`
}

// IsMissing reports whether the named source ("email", "calendar", "note",
// "task", "profile") was absent from this load.
func (c *Corpus) IsMissing(source string) bool {
	for _, m := range c.Missing {
		if m.Source == source {
			return true
		}
	}
	return false
}

// DataSource is where a corpus comes from. LocalFS implements it now; a
// Composio-backed Gmail/Calendar provider implements it in week two without
// anything above this line changing (HLD §4, §9).
//
// It is deliberately per-source rather than one Load(): a remote provider
// should not have to fetch 500 messages to answer `aubade tool conflicts`,
// which only ever reads the calendar.
//
// Contract for implementations:
//   - A source that does not exist returns an error satisfying
//     errors.Is(err, ErrSourceMissing) — recoverable, reported by the digest.
//   - A source that exists but does not parse returns a *ValidationError
//     naming the file and, where the format has lines, the line. Never drop a
//     record and continue.
//   - Order is deterministic: the same bytes produce the same slice.
type DataSource interface {
	// Name identifies the provider and its origin, e.g. "localfs:data".
	Name() string
	Emails(ctx context.Context) ([]Email, error)
	Events(ctx context.Context) ([]CalEvent, error)
	Notes(ctx context.Context) ([]Note, error)
	Tasks(ctx context.Context) ([]Task, error)
	Profile(ctx context.Context) (*Profile, error)
}

// LoadCorpus reads every source from ds into one Corpus.
//
// Missing sources are recorded on Corpus.Missing and the load continues — a
// digest with no notes is still a useful digest, as long as it admits the notes
// were not there. Any other error aborts: a malformed source is a bug in the
// corpus or in the provider, and pretending otherwise produces a digest that is
// quietly wrong, which is the one outcome worse than no digest.
func LoadCorpus(ctx context.Context, ds DataSource) (*Corpus, error) {
	if ds == nil {
		return nil, fmt.Errorf("model: LoadCorpus called with a nil DataSource")
	}
	c := &Corpus{Source: ds.Name()}

	emails, err := ds.Emails(ctx)
	if !c.record("email", err) {
		return nil, err
	}
	c.Emails = emails

	events, err := ds.Events(ctx)
	if !c.record("calendar", err) {
		return nil, err
	}
	c.Events = events

	notes, err := ds.Notes(ctx)
	if !c.record("note", err) {
		return nil, err
	}
	c.Notes = notes

	tasks, err := ds.Tasks(ctx)
	if !c.record("task", err) {
		return nil, err
	}
	c.Tasks = tasks

	profile, err := ds.Profile(ctx)
	if !c.record("profile", err) {
		return nil, err
	}
	c.Profile = profile

	return c, nil
}

// record folds one source's error into the corpus. It returns false when the
// error is fatal and the load must stop.
func (c *Corpus) record(source string, err error) bool {
	if err == nil {
		return true
	}
	var missing *MissingSourceError
	if !errors.As(err, &missing) {
		return false
	}
	c.Missing = append(c.Missing, MissingSource{
		Source: source,
		Path:   missing.Path,
		Reason: missing.Error(),
	})
	return true
}
