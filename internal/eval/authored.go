package eval

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/chandrameenamohan/aubade/internal/datagen"
	"github.com/chandrameenamohan/aubade/internal/model"
)

// The authored-scenario contract: what a model is allowed to hand back when it
// is asked to write a new trap, and what has to be true of it before a single
// byte of it is allowed near a corpus.
//
// # Why this is a schema and not Go
//
// A planted trap in datagen is a `Scenario func(*Script) Trap` — a script that
// emits its own artifacts and returns the answer-key entry that grades them. A
// model cannot hand back a Go function, so the adversarial suite hands it the
// declarative shape of the same thing: the four emitters become four arrays,
// and `Script.DayAt(n, h, m)` becomes a day offset and a wall-clock time. The
// datagen invariant survives the translation intact, which is the whole reason
// the shape is worth mirroring: **planted_refs are derived here, from the
// artifacts the scenario actually emitted, and are never asked of the model.**
// A trap cannot cite evidence it did not plant, for exactly the same reason it
// cannot in datagen — the citations are a return value of the emitters.
//
// # Why the validation is strict
//
// This is the one place in aubade where model output is written into a corpus
// that graders then read. Every rule below turns a plausible-looking scenario
// that would grade nothing into a rejection with a reason the model can act on
// (which is what the one retry is for):
//
//   - Anchored, not absolute. Dates are offsets from --today, so an authored
//     trap lands inside the corpus window under any anchor rather than in a
//     year the extractors never look at.
//   - No collisions. An id that already belongs to a planted artifact would
//     make *two* tasks ungradeable, not one.
//   - Keywords quotable from its own evidence. datagen holds this line for the
//     shipped catalog (TestTrapKeywordsArePlanted); an authored trap whose
//     keyword appears nowhere in the text it planted is a task no page could
//     pass, and a miss on it names nothing to fix.

// AuthoredScenario is one model-authored trap: the answer-key entry and the
// artifacts that plant it, in one object.
type AuthoredScenario struct {
	ID          string         `json:"id"`
	Kind        string         `json:"kind"`
	Description string         `json:"description"`
	MustSurface bool           `json:"must_surface"`
	Expect      datagen.Expect `json:"expect"`

	Emails []AuthoredEmail `json:"emails"`
	Events []AuthoredEvent `json:"events"`
	Notes  []AuthoredNote  `json:"notes"`
	Tasks  []AuthoredTask  `json:"tasks"`
}

// AuthoredEmail is one message, timed relative to the anchor date.
type AuthoredEmail struct {
	ID        string         `json:"id"`
	ThreadID  string         `json:"thread_id"`
	DayOffset int            `json:"day_offset"`
	Hour      int            `json:"hour"`
	Minute    int            `json:"minute"`
	From      model.Person   `json:"from"`
	To        []model.Person `json:"to"`
	Subject   string         `json:"subject"`
	Body      string         `json:"body"`
	InReplyTo string         `json:"in_reply_to,omitempty"`
	Labels    []string       `json:"labels,omitempty"`
}

// AuthoredEvent is one calendar entry. Created is optional and separate from
// the start time because provenance is a trap axis of its own: "Sam put this on
// the shared calendar at 21:04 last night" is a different fact from "the
// appointment is at 15:00", and a scenario that needs the first must be able to
// say it.
type AuthoredEvent struct {
	UID             string             `json:"uid"`
	Summary         string             `json:"summary"`
	Description     string             `json:"description,omitempty"`
	Location        string             `json:"location,omitempty"`
	DayOffset       int                `json:"day_offset"`
	StartHour       int                `json:"start_hour"`
	StartMinute     int                `json:"start_minute"`
	DurationMinutes int                `json:"duration_minutes"`
	Status          string             `json:"status"`
	Organizer       model.Person       `json:"organizer"`
	Attendees       []AuthoredAttendee `json:"attendees"`
	Created         *AuthoredStamp     `json:"created,omitempty"`
}

// AuthoredAttendee is one invitee and the RSVP that makes a decline visible.
type AuthoredAttendee struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	PartStat string `json:"partstat"`
}

// AuthoredStamp is a wall-clock moment relative to the anchor date.
type AuthoredStamp struct {
	DayOffset int `json:"day_offset"`
	Hour      int `json:"hour"`
	Minute    int `json:"minute"`
}

// AuthoredNote is one markdown note under notes/.
type AuthoredNote struct {
	Path      string   `json:"path"`
	Title     string   `json:"title"`
	DayOffset int      `json:"day_offset"`
	Body      string   `json:"body"`
	Attendees []string `json:"attendees,omitempty"`
	Tags      []string `json:"tags,omitempty"`
}

// AuthoredTask is one line of tasks.md. DueDayOffset is a pointer because a
// task with no due date is a real and different thing from one due today.
type AuthoredTask struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	Done         bool   `json:"done"`
	Owner        string `json:"owner,omitempty"`
	DueDayOffset *int   `json:"due_day_offset,omitempty"`
}

// authored is one compiled scenario: the artifacts as the corpus models them,
// and the answer-key entry whose planted_refs point at them.
type authored struct {
	Trap   datagen.Trap
	Emails []model.Email
	Events []model.CalEvent
	Notes  []model.Note
	Tasks  []model.Task
}

// authoredPlan renders the compiled scenarios as the delta datagen.Inject
// appends to a corpus.
func authoredPlan(list []authored) *datagen.Plan {
	p := &datagen.Plan{}
	for _, a := range list {
		p.Emails = append(p.Emails, a.Emails...)
		p.Events = append(p.Events, a.Events...)
		p.Notes = append(p.Notes, a.Notes...)
		p.Tasks = append(p.Tasks, a.Tasks...)
		p.Traps = append(p.Traps, a.Trap)
	}
	return p
}

// namespace is every identifier already spoken for — by the corpus the
// scenarios are being injected into, and by the scenarios accepted before this
// one in the same batch.
type namespace struct {
	today time.Time
	loc   *time.Location

	trapIDs   map[string]bool
	emailIDs  map[string]bool
	eventUIDs map[string]bool
	notePaths map[string]bool
	taskIDs   map[string]bool

	// emailTS lets a reply be checked against the message it answers, wherever
	// that message came from.
	emailTS map[string]time.Time
}

// newNamespace reads the corpus and answer key an authored scenario has to
// coexist with.
func newNamespace(corpus *model.Corpus, traps datagen.Traps, today time.Time, loc *time.Location) *namespace {
	ns := &namespace{
		today:     today,
		loc:       loc,
		trapIDs:   map[string]bool{},
		emailIDs:  map[string]bool{},
		eventUIDs: map[string]bool{},
		notePaths: map[string]bool{},
		taskIDs:   map[string]bool{},
		emailTS:   map[string]time.Time{},
	}
	for _, t := range traps {
		ns.trapIDs[t.ID] = true
	}
	if corpus == nil {
		return ns
	}
	for _, e := range corpus.Emails {
		ns.emailIDs[e.ID] = true
		ns.emailTS[e.ID] = e.TS
	}
	for _, ev := range corpus.Events {
		ns.eventUIDs[ev.UID] = true
	}
	for _, n := range corpus.Notes {
		ns.notePaths[n.Path] = true
	}
	for _, t := range corpus.Tasks {
		ns.taskIDs[t.ID] = true
	}
	return ns
}

// claim records an accepted scenario's identifiers so the next one in the batch
// cannot reuse them.
func (ns *namespace) claim(a authored) {
	ns.trapIDs[a.Trap.ID] = true
	for _, e := range a.Emails {
		ns.emailIDs[e.ID] = true
		ns.emailTS[e.ID] = e.TS
	}
	for _, ev := range a.Events {
		ns.eventUIDs[ev.UID] = true
	}
	for _, n := range a.Notes {
		ns.notePaths[n.Path] = true
	}
	for _, t := range a.Tasks {
		ns.taskIDs[t.ID] = true
	}
}

// slug is the identifier shape the corpus uses for ids, uids and file names.
var slug = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

// compile validates one authored scenario and turns it into corpus artifacts.
//
// Every problem is collected rather than the first one returned: the retry gets
// one more attempt, so it should be told everything that was wrong with the
// first, not sent back to discover the next rule one round at a time.
func compile(s AuthoredScenario, ns *namespace) (authored, error) {
	var (
		out  authored
		errs []error
		fail = func(format string, args ...any) { errs = append(errs, fmt.Errorf(format, args...)) }
	)

	switch {
	case !slug.MatchString(s.ID):
		fail("id %q must be a lowercase slug (a-z, 0-9, dot, dash, underscore)", clip(s.ID, 60))
	case ns.trapIDs[s.ID]:
		fail("trap id %q is already taken by a trap in this corpus; choose another", s.ID)
	}
	if !datagen.IsKnownCategory(s.Kind) {
		fail("kind %q is not a trap category; one of: %s", clip(s.Kind, 40), strings.Join(datagen.KnownCategories, ", "))
	}
	if strings.TrimSpace(s.Description) == "" {
		fail("description is empty")
	}
	if !model.IsKnownKind(s.Expect.SignalKind) {
		fail("expect.signal_kind %q is not an extractor; one of: %s",
			clip(s.Expect.SignalKind, 40), strings.Join(model.KnownKinds, ", "))
	}

	seen := map[string]bool{}
	claim := func(what, id string, taken map[string]bool) {
		key := what + ":" + id
		switch {
		case taken[id]:
			fail("%s %q already exists in this corpus; choose another", what, id)
		case seen[key]:
			fail("%s %q is used twice in this scenario", what, id)
		}
		seen[key] = true
	}

	var refs []model.Citation
	for i, e := range s.Emails {
		claim("email id", e.ID, ns.emailIDs)
		mail, err := e.compile(ns)
		if err != nil {
			fail("emails[%d] (%s): %w", i, e.ID, err)
			continue
		}
		out.Emails = append(out.Emails, mail)
		refs = append(refs, model.Citation{Source: model.SourceEmail, Ref: mail.ID})
	}
	for i, ev := range s.Events {
		claim("event uid", ev.UID, ns.eventUIDs)
		event, err := ev.compile(ns)
		if err != nil {
			fail("events[%d] (%s): %w", i, ev.UID, err)
			continue
		}
		out.Events = append(out.Events, event)
		refs = append(refs, model.Citation{Source: model.SourceCalendar, Ref: event.UID})
	}
	for i, n := range s.Notes {
		claim("note path", n.Path, ns.notePaths)
		note, err := n.compile(ns)
		if err != nil {
			fail("notes[%d] (%s): %w", i, n.Path, err)
			continue
		}
		out.Notes = append(out.Notes, note)
		refs = append(refs, model.Citation{Source: model.SourceNote, Ref: note.Path})
	}
	for i, t := range s.Tasks {
		claim("task id", t.ID, ns.taskIDs)
		task, err := t.compile(ns)
		if err != nil {
			fail("tasks[%d] (%s): %w", i, t.ID, err)
			continue
		}
		out.Tasks = append(out.Tasks, task)
		refs = append(refs, model.Citation{Source: model.SourceTask, Ref: task.ID})
	}
	if len(refs) == 0 {
		fail("the scenario plants no artifacts; a trap that planted nothing is a claim about the corpus, not a task in it")
	}

	// Replies are checked against the whole namespace, so a scenario may answer
	// a message the corpus already contains — which is how a cadence or
	// quiet-thread trap is written honestly.
	for _, e := range out.Emails {
		if e.InReplyTo == "" {
			continue
		}
		parent, ok := ns.emailTS[e.InReplyTo]
		if !ok {
			for _, own := range out.Emails {
				if own.ID == e.InReplyTo {
					parent, ok = own.TS, true
					break
				}
			}
		}
		switch {
		case !ok:
			fail("email %s replies to %q, which is not in this corpus and not in this scenario", e.ID, clip(e.InReplyTo, 40))
		case e.TS.Before(parent):
			fail("email %s is dated before the message it replies to; a reply that predates its parent inverts every gap a trap is measured in", e.ID)
		}
	}

	planted := strings.ToLower(out.text())
	for _, k := range s.Expect.Keywords {
		if strings.TrimSpace(k) == "" {
			fail("expect.keywords contains a blank entry")
			continue
		}
		if !strings.Contains(planted, strings.ToLower(k)) {
			fail("expect.keywords entry %q appears nowhere in the text this scenario plants; a keyword that is not quotable from its own evidence grades nothing", clip(k, 60))
		}
	}

	out.Trap = datagen.Trap{
		ID:          s.ID,
		Kind:        s.Kind,
		Description: strings.TrimSpace(s.Description),
		MustSurface: s.MustSurface,
		Expect:      s.Expect,
		PlantedRefs: refs,
	}
	// The answer-key contract, last: it re-checks several of the rules above, so
	// running it over a scenario that already failed one would report the same
	// problem twice in the reason the retry is handed.
	if len(errs) == 0 {
		if err := out.Trap.Validate(); err != nil {
			fail("%w", err)
		}
	}
	if err := errors.Join(errs...); err != nil {
		return authored{}, err
	}
	return out, nil
}

// text is every word the scenario planted, which is the corpus a keyword has to
// be quotable from.
func (a authored) text() string {
	var b strings.Builder
	for _, e := range a.Emails {
		b.WriteString(e.Subject)
		b.WriteString("\n")
		b.WriteString(e.Body)
		b.WriteString("\n")
	}
	for _, ev := range a.Events {
		fmt.Fprintf(&b, "%s\n%s\n%s\n", ev.Summary, ev.Description, ev.Location)
	}
	for _, n := range a.Notes {
		fmt.Fprintf(&b, "%s\n%s\n", n.Title, n.Body)
	}
	for _, t := range a.Tasks {
		fmt.Fprintf(&b, "%s\n", t.Title)
	}
	return b.String()
}

// at resolves a day offset and a wall-clock time against the anchor date.
func (ns *namespace) at(dayOffset, hour, minute int) time.Time {
	d := ns.today.AddDate(0, 0, dayOffset)
	y, m, day := d.Date()
	return time.Date(y, m, day, hour, minute, 0, 0, ns.loc)
}

// window rejects a day offset outside the corpus span.
func window(what string, offset, back, forward int) error {
	if offset < -back || offset > forward {
		return fmt.Errorf("%s day_offset %d is outside the corpus window (%d..%d)", what, offset, -back, forward)
	}
	return nil
}

func clock(hour, minute int) error {
	if hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return fmt.Errorf("time %02d:%02d is not a wall-clock time", hour, minute)
	}
	return nil
}

func (e AuthoredEmail) compile(ns *namespace) (model.Email, error) {
	var errs []error
	// Mail is history: a message dated after the anchor date has not been
	// received yet, so it is a fact the digest could not have known.
	errs = append(errs, window("email", e.DayOffset, datagen.CorpusDays, 0), clock(e.Hour, e.Minute))

	out := model.Email{
		ID:        e.ID,
		ThreadID:  e.ThreadID,
		TS:        ns.at(e.DayOffset, e.Hour, e.Minute),
		From:      e.From,
		To:        e.To,
		CC:        []model.Person{},
		Subject:   e.Subject,
		Body:      e.Body,
		InReplyTo: strings.TrimSpace(e.InReplyTo),
		Labels:    e.Labels,
	}
	if strings.TrimSpace(out.Body) == "" {
		errs = append(errs, errors.New("body is empty; an email with no text cannot carry a keyword"))
	}
	errs = append(errs, out.Validate())
	if err := errors.Join(errs...); err != nil {
		return model.Email{}, err
	}
	return out, nil
}

func (e AuthoredEvent) compile(ns *namespace) (model.CalEvent, error) {
	var errs []error
	errs = append(errs, window("event", e.DayOffset, datagen.CorpusDays, datagen.LookaheadDays), clock(e.StartHour, e.StartMinute))
	if e.DurationMinutes <= 0 || e.DurationMinutes > 24*60 {
		errs = append(errs, fmt.Errorf("duration_minutes %d must be between 1 and 1440", e.DurationMinutes))
	}
	if strings.TrimSpace(e.Summary) == "" {
		errs = append(errs, errors.New("summary is empty"))
	}
	if !slug.MatchString(e.UID) {
		errs = append(errs, fmt.Errorf("uid %q must be a lowercase slug", clip(e.UID, 60)))
	}

	start := ns.at(e.DayOffset, e.StartHour, e.StartMinute)
	created := start.AddDate(0, 0, -7)
	if e.Created != nil {
		errs = append(errs, window("event created", e.Created.DayOffset, datagen.CorpusDays, datagen.LookaheadDays), clock(e.Created.Hour, e.Created.Minute))
		created = ns.at(e.Created.DayOffset, e.Created.Hour, e.Created.Minute)
	}

	out := model.CalEvent{
		UID:         e.UID,
		Summary:     e.Summary,
		Description: e.Description,
		Location:    e.Location,
		Start:       start,
		End:         start.Add(time.Duration(e.DurationMinutes) * time.Minute),
		Status:      model.EventStatus(e.Status),
		Organizer:   e.Organizer,
		Created:     created,
	}
	if !out.Status.Valid() {
		errs = append(errs, fmt.Errorf("status %q; want CONFIRMED, TENTATIVE or CANCELLED", clip(e.Status, 40)))
	}
	for i, a := range e.Attendees {
		at := model.Attendee{
			Person:   model.Person{Name: a.Name, Email: a.Email},
			PartStat: model.PartStat(a.PartStat),
			Role:     "REQ-PARTICIPANT",
		}
		if !at.PartStat.Valid() {
			errs = append(errs, fmt.Errorf("attendees[%d] partstat %q; want NEEDS-ACTION, ACCEPTED, DECLINED, TENTATIVE or DELEGATED", i, clip(a.PartStat, 40)))
		}
		if strings.TrimSpace(at.Email) == "" {
			errs = append(errs, fmt.Errorf("attendees[%d] has no email address", i))
		}
		out.Attendees = append(out.Attendees, at)
	}
	if err := errors.Join(errs...); err != nil {
		return model.CalEvent{}, err
	}
	return out, nil
}

func (n AuthoredNote) compile(ns *namespace) (model.Note, error) {
	var errs []error
	errs = append(errs, window("note", n.DayOffset, datagen.CorpusDays, 0))

	name := strings.TrimPrefix(n.Path, "notes/")
	if !strings.HasPrefix(n.Path, "notes/") || !strings.HasSuffix(name, ".md") || !slug.MatchString(name) {
		errs = append(errs, fmt.Errorf("path %q must be notes/<lowercase-slug>.md", clip(n.Path, 80)))
	}
	if strings.TrimSpace(n.Title) == "" {
		errs = append(errs, errors.New("title is empty"))
	}
	if strings.TrimSpace(n.Body) == "" {
		errs = append(errs, errors.New("body is empty"))
	}
	if err := errors.Join(errs...); err != nil {
		return model.Note{}, err
	}
	return model.Note{
		Path:      n.Path,
		Title:     n.Title,
		Date:      ns.at(n.DayOffset, 0, 0),
		Body:      n.Body,
		Attendees: n.Attendees,
		Tags:      n.Tags,
	}, nil
}

func (t AuthoredTask) compile(ns *namespace) (model.Task, error) {
	var errs []error
	if !slug.MatchString(t.ID) {
		errs = append(errs, fmt.Errorf("id %q must be a lowercase slug", clip(t.ID, 60)))
	}
	if strings.TrimSpace(t.Title) == "" {
		errs = append(errs, errors.New("title is empty"))
	}
	out := model.Task{ID: t.ID, Title: t.Title, Done: t.Done, Owner: t.Owner}
	if t.DueDayOffset != nil {
		errs = append(errs, window("task due", *t.DueDayOffset, datagen.CorpusDays, datagen.LookaheadDays))
		out.Due = ns.at(*t.DueDayOffset, 0, 0)
	}
	if err := errors.Join(errs...); err != nil {
		return model.Task{}, err
	}
	return out, nil
}
