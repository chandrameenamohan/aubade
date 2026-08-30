package digest

import (
	"fmt"
	"strings"
	"time"

	"github.com/chandrameenamohan/aubade/internal/model"
)

// Citations are the load-bearing furniture of this page, so they get their own
// file.
//
// A signal cites a record by id — "email:e-001". That is the right thing to put
// in signals.json, where a grader resolves it, and the wrong thing to put in
// front of a human at 6am, who needs to know *who said it and when* without
// opening anything. So every citation is resolved against the corpus and
// rendered in the sample digest's own form:
//
//	*[email: Marcus, May 19 16:42]*   *[note: board-update-cadence.md]*
//
// A ref that does not resolve renders as the raw ref rather than being dropped.
// A citation nobody can follow is a bug worth seeing; a line with its receipt
// silently removed is a claim with no receipt, which is the one thing this
// architecture does not allow.

// citeTimeFormat is how a citation stamps a moment: "May 19 16:42", the sample
// digest's own form — enough to find the message, short enough to sit at the
// end of a sentence.
const citeTimeFormat = "Jan 2 15:04"

// taskFile is the conventional corpus-relative name of the task list. A task
// has no id a reader would recognise, so it cites the line it lives on — and
// the filename has to be the one the toolbox names in its own details, or the
// same task reads as two different places.
const taskFile = "tasks.md"

// index resolves citation refs back to the records they name.
type index struct {
	loc    *time.Location
	emails map[string]*model.Email
	events map[string]*model.CalEvent
	notes  map[string]*model.Note
	tasks  map[string]*model.Task
}

func newIndex(c *model.Corpus, loc *time.Location) *index {
	x := &index{
		loc:    loc,
		emails: make(map[string]*model.Email, len(c.Emails)),
		events: make(map[string]*model.CalEvent, len(c.Events)),
		notes:  make(map[string]*model.Note, len(c.Notes)),
		tasks:  make(map[string]*model.Task, len(c.Tasks)),
	}
	for i := range c.Emails {
		x.emails[c.Emails[i].ID] = &c.Emails[i]
	}
	for i := range c.Events {
		x.events[c.Events[i].UID] = &c.Events[i]
	}
	for i := range c.Notes {
		x.notes[c.Notes[i].Path] = &c.Notes[i]
	}
	for i := range c.Tasks {
		x.tasks[c.Tasks[i].ID] = &c.Tasks[i]
	}
	return x
}

// label renders one citation for the end of a line.
func (x *index) label(c model.Citation) string {
	switch c.Source {
	case model.SourceEmail:
		if e, ok := x.emails[c.Ref]; ok {
			return fmt.Sprintf("[email: %s, %s]", shortName(e.From), e.TS.In(x.loc).Format(citeTimeFormat))
		}
	case model.SourceCalendar:
		if ev, ok := x.events[c.Ref]; ok {
			return fmt.Sprintf("[cal: %s, %s]", clip(ev.Summary, 40), x.eventStamp(ev))
		}
	case model.SourceNote:
		// The path is the reference, whether or not the note is in the corpus:
		// a missing-source citation names a file that was not there, and it
		// still points at exactly what the reader should go looking for.
		return "[note: " + c.Ref + "]"
	case model.SourceTask:
		if task, ok := x.tasks[c.Ref]; ok {
			return fmt.Sprintf("[task: %s:%d]", taskFile, task.Line)
		}
	}
	return fmt.Sprintf("[%s: %s]", c.Source, c.Ref)
}

// labels resolves a whole citation list, one rendered ref per citation, in
// order.
func (x *index) labels(cs []model.Citation) []string {
	if len(cs) == 0 {
		return nil
	}
	out := make([]string, 0, len(cs))
	for _, c := range cs {
		out = append(out, x.label(c))
	}
	return out
}

// describe renders what one citation actually says — the half of a
// contradiction that this source is asserting.
func (x *index) describe(c model.Citation) string {
	switch c.Source {
	case model.SourceEmail:
		if e, ok := x.emails[c.Ref]; ok {
			return fmt.Sprintf("the mail: %s wrote %q on %s",
				e.From.String(), clip(firstLine(e.Body), 110), e.TS.In(x.loc).Format(citeTimeFormat))
		}
	case model.SourceCalendar:
		if ev, ok := x.events[c.Ref]; ok {
			return fmt.Sprintf("the calendar: %q is %s for %s",
				clip(ev.Summary, 60), strings.ToLower(string(ev.Status)), x.window(ev))
		}
	case model.SourceNote:
		if n, ok := x.notes[c.Ref]; ok {
			return fmt.Sprintf("the note %s: %q", n.Path, clip(firstLine(n.Body), 110))
		}
	case model.SourceTask:
		if task, ok := x.tasks[c.Ref]; ok {
			return fmt.Sprintf("the task list: %q, %s:%d", clip(task.Title, 80), taskFile, task.Line)
		}
	}
	return string(c.Source) + " " + c.Ref
}

// at is when the record a citation names happened — the input to the recency
// half of the score. A record with no time of its own (a task, an undated note)
// reports none rather than a guess.
func (x *index) at(c model.Citation) (time.Time, bool) {
	switch c.Source {
	case model.SourceEmail:
		if e, ok := x.emails[c.Ref]; ok {
			return e.TS, true
		}
	case model.SourceCalendar:
		if ev, ok := x.events[c.Ref]; ok {
			// Created, not Start: "Sam added this at 21:04 last night" is what
			// makes a calendar fact recent, and the meeting itself may be days
			// out or already past.
			if !ev.Created.IsZero() {
				return ev.Created, true
			}
			return ev.Start, true
		}
	case model.SourceNote:
		if n, ok := x.notes[c.Ref]; ok && n.HasDate() {
			return n.Date, true
		}
	}
	return time.Time{}, false
}

// eventStamp is when a calendar fact was recorded, falling back to when the
// event runs.
func (x *index) eventStamp(ev *model.CalEvent) string {
	at := ev.Created
	if at.IsZero() {
		return ev.Start.In(x.loc).Format(citeTimeFormat)
	}
	return "added " + at.In(x.loc).Format(citeTimeFormat)
}

// window renders an event's span the way a calendar shows it.
func (x *index) window(ev *model.CalEvent) string {
	start := ev.Start.In(x.loc)
	if ev.AllDay {
		return start.Format("Mon 2 Jan") + ", all day"
	}
	return fmt.Sprintf("%s–%s", start.Format("Mon 2 Jan 15:04"), ev.End.In(x.loc).Format("15:04"))
}

// shortName is what a citation calls someone: their first name, or their
// mailbox when they have no name. The sample digest cites "Marcus", not
// "Marcus Webb <marcus@inflectionpoint.example>".
func shortName(p model.Person) string {
	if name := strings.TrimSpace(p.Name); name != "" {
		if i := strings.IndexAny(name, " \t"); i > 0 {
			return name[:i]
		}
		return name
	}
	addr := strings.TrimSpace(p.Email)
	if i := strings.Index(addr, "@"); i > 0 {
		return addr[:i]
	}
	return addr
}

// firstLine is the first non-empty line of a body, for a one-line quotation.
func firstLine(body string) string {
	for _, line := range strings.Split(body, "\n") {
		if t := strings.TrimSpace(line); t != "" {
			return t
		}
	}
	return ""
}

// clip shortens text to n runes on a word boundary. It works in runes rather
// than bytes because names in this corpus are not all ASCII and half a rune is
// not a shorter string, it is a broken one.
func clip(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	cut := string(r[:n])
	if i := strings.LastIndex(cut, " "); i > n/2 {
		cut = cut[:i]
	}
	return cut + "…"
}
