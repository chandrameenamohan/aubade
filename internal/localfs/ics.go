package localfs

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/chandrameenamohan/aubade/internal/model"
)

// The calendar reader implements the RFC 5545 subset aubade actually needs, and
// rejects the rest rather than half-supporting it. We control both ends — the
// generator writes this file and this reads it — so an unsupported construct
// means the corpus is not what we think it is.
//
// Supported: line folding, quoted parameters, VCALENDAR/VEVENT nesting, and on
// a VEVENT — UID, DTSTART, DTEND (or DURATION), SUMMARY, DESCRIPTION, LOCATION,
// STATUS, ORGANIZER, ATTENDEE (with CN, PARTSTAT, ROLE), CREATED and DTSTAMP.
// X-WR-CALNAME on the VCALENDAR names the calendar. Properties of other
// components (VTIMEZONE, VALARM, VTODO…) are skipped by design: they are not
// part of the Event model, and skipping a whole component we do not model is a
// different act from skipping a field we do.
//
// Timestamps: `…Z` is UTC, `TZID=` names a zone, `VALUE=DATE` is all-day, and a
// floating time is read in the corpus zone (America/Los_Angeles by default).

// icsLine is one *logical* line — unfolded — with the physical line number it
// started on, so an error can point at the file the user can actually see.
type icsLine struct {
	text string
	num  int
}

// icsProp is a parsed content line: NAME;PARAM=value:VALUE.
type icsProp struct {
	name   string
	params map[string]string
	value  string
	line   int
}

// param returns a parameter value by (upper-case) name.
func (p icsProp) param(name string) string { return p.params[name] }

// parseICS reads calendar.ics into events, in file order.
func parseICS(path string, data []byte, loc *time.Location) ([]model.CalEvent, error) {
	fail := func(line int, msg string, err error) error {
		return &model.ValidationError{
			Source: string(model.SourceCalendar),
			Path:   path,
			Line:   line,
			Msg:    msg,
			Err:    err,
		}
	}

	lines, err := unfoldICS(path, data)
	if err != nil {
		return nil, err
	}

	var (
		stack   []string
		events  []model.CalEvent
		builder *icsEvent
		calName string
		seenUID = map[string]int{}
	)

	for _, ln := range lines {
		prop, err := parseContentLine(path, ln)
		if err != nil {
			return nil, err
		}

		switch prop.name {
		case "BEGIN":
			comp := strings.ToUpper(strings.TrimSpace(prop.value))
			if comp == "" {
				return nil, fail(ln.num, "BEGIN with no component name", nil)
			}
			if comp == "VEVENT" {
				if len(stack) > 0 && stack[len(stack)-1] == "VEVENT" {
					return nil, fail(ln.num, "VEVENT nested inside a VEVENT", nil)
				}
				if len(stack) == 0 || stack[len(stack)-1] != "VCALENDAR" {
					return nil, fail(ln.num, "VEVENT outside a VCALENDAR", nil)
				}
				builder = &icsEvent{line: ln.num}
			}
			stack = append(stack, comp)
			continue

		case "END":
			comp := strings.ToUpper(strings.TrimSpace(prop.value))
			if len(stack) == 0 {
				return nil, fail(ln.num, fmt.Sprintf("END:%s with no matching BEGIN", comp), nil)
			}
			if open := stack[len(stack)-1]; open != comp {
				return nil, fail(ln.num, fmt.Sprintf("END:%s closes an open %s", comp, open), nil)
			}
			stack = stack[:len(stack)-1]
			if comp != "VEVENT" {
				continue
			}
			ev, err := builder.build(path, loc)
			if err != nil {
				return nil, err
			}
			if first, dup := seenUID[ev.UID]; dup {
				return nil, fail(builder.line, fmt.Sprintf("duplicate event UID %q (first seen on line %d)", ev.UID, first), nil)
			}
			seenUID[ev.UID] = builder.line
			events = append(events, ev)
			builder = nil
			continue
		}

		if len(stack) == 0 {
			return nil, fail(ln.num, fmt.Sprintf("property %s outside any component", prop.name), nil)
		}
		switch stack[len(stack)-1] {
		case "VEVENT":
			if err := builder.set(path, prop, loc); err != nil {
				return nil, err
			}
		case "VCALENDAR":
			if prop.name == "X-WR-CALNAME" {
				calName = unescapeICSText(prop.value)
			}
		}
	}

	if len(stack) != 0 {
		return nil, fail(0, fmt.Sprintf("unterminated component %s: no END", stack[len(stack)-1]), nil)
	}
	// X-WR-CALNAME belongs to the calendar, not to an event, and nothing
	// requires it to appear before the events it names — so it is stamped on
	// once the whole file has been read.
	for i := range events {
		events[i].Calendar = calName
	}
	return events, nil
}

// unfoldICS splits the file into logical lines, joining RFC 5545 folded
// continuations (a line beginning with a space or tab continues the previous
// one). Line endings are normalized first: real calendars use CRLF, fixtures
// and editors often do not, and disagreeing about that is not a corpus error.
func unfoldICS(path string, data []byte) ([]icsLine, error) {
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")

	var out []icsLine
	for i, raw := range strings.Split(text, "\n") {
		num := i + 1
		if strings.TrimSpace(raw) == "" {
			continue
		}
		if raw[0] == ' ' || raw[0] == '\t' {
			if len(out) == 0 {
				return nil, &model.ValidationError{
					Source: string(model.SourceCalendar), Path: path, Line: num,
					Msg: "continuation line before any content line",
				}
			}
			out[len(out)-1].text += raw[1:]
			continue
		}
		out = append(out, icsLine{text: raw, num: num})
	}
	return out, nil
}

// parseContentLine splits NAME;PARAM=value:VALUE, honouring quoted parameter
// values (which may contain ':' and ';').
func parseContentLine(path string, ln icsLine) (icsProp, error) {
	fail := func(msg string) (icsProp, error) {
		return icsProp{}, &model.ValidationError{
			Source: string(model.SourceCalendar), Path: path, Line: ln.num, Msg: msg,
		}
	}

	sep := -1
	inQuotes := false
	for i := 0; i < len(ln.text) && sep < 0; i++ {
		switch ln.text[i] {
		case '"':
			inQuotes = !inQuotes
		case ':':
			if !inQuotes {
				sep = i
			}
		}
	}
	if sep < 0 {
		return fail("content line has no ':' separator")
	}

	head, value := ln.text[:sep], ln.text[sep+1:]
	parts := splitUnquoted(head, ';')
	name := strings.ToUpper(strings.TrimSpace(parts[0]))
	if name == "" {
		return fail("content line has an empty property name")
	}

	params := make(map[string]string, len(parts)-1)
	for _, p := range parts[1:] {
		k, v, ok := strings.Cut(p, "=")
		if !ok {
			return fail(fmt.Sprintf("parameter %q on %s has no '='", p, name))
		}
		params[strings.ToUpper(strings.TrimSpace(k))] = strings.Trim(strings.TrimSpace(v), `"`)
	}
	return icsProp{name: name, params: params, value: value, line: ln.num}, nil
}

// splitUnquoted splits on sep, ignoring separators inside double quotes.
func splitUnquoted(s string, sep byte) []string {
	var (
		out      []string
		start    int
		inQuotes bool
	)
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '"':
			inQuotes = !inQuotes
		case sep:
			if !inQuotes {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	return append(out, s[start:])
}

// unescapeICSText reverses RFC 5545 TEXT escaping.
func unescapeICSText(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 >= len(s) {
			b.WriteByte(s[i])
			continue
		}
		i++
		switch s[i] {
		case 'n', 'N':
			b.WriteByte('\n')
		case '\\', ',', ';':
			b.WriteByte(s[i])
		default:
			// Not an escape we know: keep both bytes rather than eat one.
			b.WriteByte('\\')
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

// icsEvent accumulates one VEVENT's properties before validation.
type icsEvent struct {
	line int // line of the BEGIN:VEVENT, for whole-event errors

	uid         string
	uidLine     int
	summary     string
	description string
	location    string
	status      model.EventStatus
	statusLine  int

	start     time.Time
	startLine int
	end       time.Time
	endLine   int
	allDay    bool
	duration  time.Duration
	hasDur    bool

	organizer model.Person
	attendees []model.Attendee
	created   time.Time
	dtstamp   time.Time
}

// set records one property of the event under construction.
func (e *icsEvent) set(path string, p icsProp, loc *time.Location) error {
	fail := func(msg string, err error) error {
		return &model.ValidationError{
			Source: string(model.SourceCalendar), Path: path, Line: p.line, Msg: msg, Err: err,
		}
	}

	switch p.name {
	case "UID":
		if e.uid != "" {
			return fail("duplicate UID in one VEVENT", nil)
		}
		e.uid, e.uidLine = strings.TrimSpace(p.value), p.line

	case "SUMMARY":
		e.summary = unescapeICSText(p.value)
	case "DESCRIPTION":
		e.description = unescapeICSText(p.value)
	case "LOCATION":
		e.location = unescapeICSText(p.value)

	case "STATUS":
		st := model.EventStatus(strings.ToUpper(strings.TrimSpace(p.value)))
		if !st.Valid() {
			return fail(fmt.Sprintf("unknown STATUS %q; want CONFIRMED, TENTATIVE or CANCELLED", p.value), nil)
		}
		e.status, e.statusLine = st, p.line

	case "DTSTART":
		t, allDay, err := parseICSTime(path, p, loc)
		if err != nil {
			return err
		}
		e.start, e.allDay, e.startLine = t, allDay, p.line

	case "DTEND":
		t, allDay, err := parseICSTime(path, p, loc)
		if err != nil {
			return err
		}
		e.end, e.endLine = t, p.line
		if allDay {
			e.allDay = true
		}

	case "DURATION":
		d, err := parseICSDuration(p.value)
		if err != nil {
			return fail(fmt.Sprintf("invalid DURATION %q", p.value), err)
		}
		e.duration, e.hasDur = d, true

	case "CREATED":
		t, _, err := parseICSTime(path, p, loc)
		if err != nil {
			return err
		}
		e.created = t

	case "DTSTAMP":
		t, _, err := parseICSTime(path, p, loc)
		if err != nil {
			return err
		}
		e.dtstamp = t

	case "ORGANIZER":
		person, err := parseCalAddress(path, p)
		if err != nil {
			return err
		}
		e.organizer = person

	case "ATTENDEE":
		person, err := parseCalAddress(path, p)
		if err != nil {
			return err
		}
		partstat := model.PartStat(strings.ToUpper(strings.TrimSpace(p.param("PARTSTAT"))))
		if partstat == "" {
			partstat = model.PartStatNeedsAction
		}
		if !partstat.Valid() {
			return fail(fmt.Sprintf("unknown PARTSTAT %q on ATTENDEE", p.param("PARTSTAT")), nil)
		}
		e.attendees = append(e.attendees, model.Attendee{
			Person:   person,
			PartStat: partstat,
			Role:     strings.ToUpper(strings.TrimSpace(p.param("ROLE"))),
		})
	}
	// Any other property on a VEVENT is not part of the Event model and is
	// ignored on purpose: this is a documented subset, not a full parser.
	return nil
}

// build validates the accumulated VEVENT and freezes it into the model.
func (e *icsEvent) build(path string, loc *time.Location) (model.CalEvent, error) {
	fail := func(line int, msg string) (model.CalEvent, error) {
		return model.CalEvent{}, &model.ValidationError{
			Source: string(model.SourceCalendar), Path: path, Line: line, Msg: msg,
		}
	}

	if e.uid == "" {
		return fail(e.line, "VEVENT has no UID")
	}
	if e.start.IsZero() {
		return fail(e.line, fmt.Sprintf("VEVENT %s has no DTSTART", e.uid))
	}

	end := e.end
	switch {
	case !end.IsZero():
		// DTEND wins over DURATION when both are present, which RFC 5545 does
		// not allow in the first place; preferring the explicit end is the
		// less surprising of two readings of a file that is already wrong.
	case e.hasDur:
		end = e.start.Add(e.duration)
	case e.allDay:
		// An all-day event with no DTEND is one day long (RFC 5545 §3.6.1).
		end = e.start.AddDate(0, 0, 1)
	default:
		return fail(e.line, fmt.Sprintf("VEVENT %s has neither DTEND nor DURATION", e.uid))
	}
	if end.Before(e.start) {
		line := e.endLine
		if line == 0 {
			line = e.line
		}
		return fail(line, fmt.Sprintf("VEVENT %s ends before it starts", e.uid))
	}

	status := e.status
	if status == "" {
		// RFC 5545 leaves STATUS optional; an event on a calendar with nothing
		// said about it is a confirmed event.
		status = model.StatusConfirmed
	}

	created := e.created
	if created.IsZero() {
		created = e.dtstamp
	}

	ev := model.CalEvent{
		UID:         e.uid,
		Summary:     e.summary,
		Description: e.description,
		Location:    e.location,
		Start:       e.start.In(loc),
		End:         end.In(loc),
		AllDay:      e.allDay,
		Status:      status,
		Organizer:   e.organizer,
		Attendees:   e.attendees,
	}
	if !created.IsZero() {
		ev.Created = created.In(loc)
	}
	return ev, nil
}

// parseICSTime reads a DATE-TIME or DATE property value. It reports whether the
// value was a date (all-day).
func parseICSTime(path string, p icsProp, loc *time.Location) (time.Time, bool, error) {
	fail := func(msg string, err error) (time.Time, bool, error) {
		return time.Time{}, false, &model.ValidationError{
			Source: string(model.SourceCalendar), Path: path, Line: p.line, Msg: msg, Err: err,
		}
	}

	v := strings.TrimSpace(p.value)
	if v == "" {
		return fail(fmt.Sprintf("%s has an empty value", p.name), nil)
	}

	if strings.EqualFold(p.param("VALUE"), "DATE") || len(v) == 8 {
		t, err := time.ParseInLocation("20060102", v, loc)
		if err != nil {
			return fail(fmt.Sprintf("%s: %q is not a YYYYMMDD date", p.name, v), err)
		}
		return t, true, nil
	}

	if strings.HasSuffix(v, "Z") {
		t, err := time.ParseInLocation("20060102T150405Z", v, time.UTC)
		if err != nil {
			return fail(fmt.Sprintf("%s: %q is not a UTC date-time", p.name, v), err)
		}
		return t, false, nil
	}

	zone := loc
	if tzid := strings.TrimSpace(p.param("TZID")); tzid != "" {
		z, err := time.LoadLocation(tzid)
		if err != nil {
			return fail(fmt.Sprintf("%s: unknown TZID %q", p.name, tzid), err)
		}
		zone = z
	}
	t, err := time.ParseInLocation("20060102T150405", v, zone)
	if err != nil {
		return fail(fmt.Sprintf("%s: %q is not a YYYYMMDDTHHMMSS date-time", p.name, v), err)
	}
	return t, false, nil
}

// parseCalAddress reads an ORGANIZER/ATTENDEE line: a mailto: value plus an
// optional CN parameter for the display name.
func parseCalAddress(path string, p icsProp) (model.Person, error) {
	v := strings.TrimSpace(p.value)
	addr := v
	if len(v) >= 7 && strings.EqualFold(v[:7], "mailto:") {
		addr = strings.TrimSpace(v[7:])
	}
	if addr == "" || !strings.Contains(addr, "@") {
		return model.Person{}, &model.ValidationError{
			Source: string(model.SourceCalendar), Path: path, Line: p.line,
			Msg: fmt.Sprintf("%s value %q is not a mailto: address", p.name, p.value),
		}
	}
	return model.Person{Name: strings.TrimSpace(p.param("CN")), Email: addr}, nil
}

// parseICSDuration reads the RFC 5545 duration subset that can describe a
// meeting: [+-]P[nW][nD][T[nH][nM][nS]].
func parseICSDuration(s string) (time.Duration, error) {
	v := strings.ToUpper(strings.TrimSpace(s))
	if v == "" {
		return 0, fmt.Errorf("empty duration")
	}

	sign := time.Duration(1)
	switch v[0] {
	case '-':
		sign, v = -1, v[1:]
	case '+':
		v = v[1:]
	}
	if v == "" || v[0] != 'P' {
		return 0, fmt.Errorf("duration must start with P")
	}
	v = v[1:]

	units := map[byte]time.Duration{
		'W': 7 * 24 * time.Hour,
		'D': 24 * time.Hour,
		'H': time.Hour,
		'M': time.Minute,
		'S': time.Second,
	}

	var (
		total  time.Duration
		digits string
		inTime bool
		seen   bool
	)
	for i := 0; i < len(v); i++ {
		c := v[i]
		switch {
		case c >= '0' && c <= '9':
			digits += string(c)
		case c == 'T':
			if inTime {
				return 0, fmt.Errorf("duration has a second 'T'")
			}
			if digits != "" {
				return 0, fmt.Errorf("duration has a number with no unit before 'T'")
			}
			inTime = true
		default:
			// 'M' is months before the 'T' and minutes after it; aubade models
			// meetings, and a month-long VEVENT duration is not a thing we can
			// place on a morning digest.
			if c == 'M' && !inTime {
				return 0, fmt.Errorf("month durations are not supported")
			}
			unit, ok := units[c]
			if !ok {
				return 0, fmt.Errorf("unknown duration unit %q", string(c))
			}
			if digits == "" {
				return 0, fmt.Errorf("duration unit %q has no number", string(c))
			}
			n, err := strconv.Atoi(digits)
			if err != nil {
				return 0, fmt.Errorf("duration number %q: %w", digits, err)
			}
			total += time.Duration(n) * unit
			digits, seen = "", true
		}
	}
	if digits != "" {
		return 0, fmt.Errorf("duration ends with a number that has no unit")
	}
	if !seen {
		return 0, fmt.Errorf("duration has no components")
	}
	return sign * total, nil
}
