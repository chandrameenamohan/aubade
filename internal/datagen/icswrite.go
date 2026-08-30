package datagen

import (
	"fmt"
	"strings"
	"time"

	"github.com/chandrameenamohan/aubade/internal/model"
)

// The calendar writer emits RFC 5545 — CRLF line endings, 75-octet folding,
// escaped TEXT values and a VTIMEZONE for the zone the DTSTARTs reference. The
// reader in localfs implements a documented subset and would accept a good deal
// less than this, which is exactly the reason to write the whole thing: a
// corpus that only parses in our own reader is a corpus that has never been
// checked against the standard it claims to follow.
//
// One thing does not survive the round trip, and it is worth naming rather than
// discovering. model.CalEvent carries a Calendar per event ("Avery + Sam
// (shared)"), but a VCALENDAR names itself once, in X-WR-CALNAME, and the
// reader stamps that one name onto every event it parses. So the per-event
// calendar is written as CATEGORIES, where a human reading calendar.ics can see
// which entries came off the shared calendar, and the facts the family-collision
// trap is graded on stay where they always were: the organizer (Sam) and the
// CREATED timestamp (21:04 last night).

// icsFoldWidth is RFC 5545's content-line limit, in octets, before folding.
const icsFoldWidth = 75

// calendarName is what the corpus's single calendar is called.
const calendarName = "Avery Chen"

// icsProdID identifies the writer in the file it wrote.
const icsProdID = "-//aubade//aubade-lab generate//EN"

// renderICS writes the whole calendar.
func renderICS(plan *Plan) []byte {
	var b strings.Builder
	w := &icsWriter{b: &b}

	w.line("BEGIN:VCALENDAR")
	w.line("VERSION:2.0")
	w.line("PRODID:" + icsProdID)
	w.line("CALSCALE:GREGORIAN")
	w.line("METHOD:PUBLISH")
	w.line("X-WR-CALNAME:" + escapeICSText(calendarName))
	w.line("X-WR-TIMEZONE:" + model.DefaultTimeZone)
	w.timezone()
	for _, ev := range plan.Events {
		w.event(ev)
	}
	w.line("END:VCALENDAR")
	return []byte(b.String())
}

// icsWriter folds and terminates content lines.
type icsWriter struct{ b *strings.Builder }

// line writes one content line, folded to icsFoldWidth octets and terminated
// with CRLF.
//
// Folding splits on octets, not runes, and the split point is walked back to a
// rune boundary: an em dash cut in half is a mojibake bug that only shows up in
// the one summary that happens to straddle column 75.
func (w *icsWriter) line(s string) {
	// The continuation space counts towards the limit, so every line after the
	// first has one octet less to work with.
	width := icsFoldWidth
	for len(s) > width {
		cut := width
		for cut > 1 && s[cut]&0xC0 == 0x80 {
			cut--
		}
		w.b.WriteString(s[:cut])
		w.b.WriteString("\r\n ")
		s = s[cut:]
		width = icsFoldWidth - 1
	}
	w.b.WriteString(s)
	w.b.WriteString("\r\n")
}

// timezone writes the VTIMEZONE the DTSTARTs reference. US Pacific rules are
// fixed and dateless here on purpose — the RRULEs describe the rule, so the
// block says the same thing in every year the corpus lands in.
func (w *icsWriter) timezone() {
	w.line("BEGIN:VTIMEZONE")
	w.line("TZID:" + model.DefaultTimeZone)
	w.line("X-LIC-LOCATION:" + model.DefaultTimeZone)
	w.line("BEGIN:DAYLIGHT")
	w.line("TZOFFSETFROM:-0800")
	w.line("TZOFFSETTO:-0700")
	w.line("TZNAME:PDT")
	w.line("DTSTART:19700308T020000")
	w.line("RRULE:FREQ=YEARLY;BYMONTH=3;BYDAY=2SU")
	w.line("END:DAYLIGHT")
	w.line("BEGIN:STANDARD")
	w.line("TZOFFSETFROM:-0700")
	w.line("TZOFFSETTO:-0800")
	w.line("TZNAME:PST")
	w.line("DTSTART:19701101T020000")
	w.line("RRULE:FREQ=YEARLY;BYMONTH=11;BYDAY=1SU")
	w.line("END:STANDARD")
	w.line("END:VTIMEZONE")
}

// event writes one VEVENT.
func (w *icsWriter) event(ev model.CalEvent) {
	w.line("BEGIN:VEVENT")
	w.line("UID:" + ev.UID)
	w.line("DTSTAMP:" + icsUTC(ev.Created))
	w.line("CREATED:" + icsUTC(ev.Created))
	w.line("LAST-MODIFIED:" + icsUTC(ev.Created))

	if ev.AllDay {
		w.line("DTSTART;VALUE=DATE:" + ev.Start.Format("20060102"))
		w.line("DTEND;VALUE=DATE:" + ev.End.Format("20060102"))
	} else {
		w.line(icsZoned("DTSTART", ev.Start))
		w.line(icsZoned("DTEND", ev.End))
	}

	w.line("SUMMARY:" + escapeICSText(ev.Summary))
	if ev.Description != "" {
		w.line("DESCRIPTION:" + escapeICSText(ev.Description))
	}
	if ev.Location != "" {
		w.line("LOCATION:" + escapeICSText(ev.Location))
	}
	if ev.Calendar != "" {
		w.line("CATEGORIES:" + escapeICSText(ev.Calendar))
	}
	w.line("STATUS:" + string(ev.Status))
	if ev.Organizer.Email != "" {
		w.line("ORGANIZER" + icsCN(ev.Organizer.Name) + ":mailto:" + ev.Organizer.Email)
	}
	for _, a := range ev.Attendees {
		role := a.Role
		if role == "" {
			role = "REQ-PARTICIPANT"
		}
		w.line(fmt.Sprintf("ATTENDEE%s;ROLE=%s;PARTSTAT=%s:mailto:%s",
			icsCN(a.Name), role, a.PartStat, a.Email))
	}
	w.line("END:VEVENT")
}

// icsZoned renders a local date-time with the TZID parameter that gives it
// meaning. Wall-clock time in the anchor zone is what the corpus is written in
// — "the 9am block" is 9am in March and in November, which a UTC stamp would
// quietly stop being.
func icsZoned(name string, t time.Time) string {
	return name + ";TZID=" + model.DefaultTimeZone + ":" + t.In(model.Location()).Format("20060102T150405")
}

// icsUTC renders the provenance stamps, which RFC 5545 wants in UTC.
func icsUTC(t time.Time) string { return t.UTC().Format("20060102T150405Z") }

// icsCN renders the optional common-name parameter, quoting it when the name
// carries a character that would otherwise end the parameter early.
func icsCN(name string) string {
	if name == "" {
		return ""
	}
	if strings.ContainsAny(name, `,;:"`) {
		return `;CN="` + strings.ReplaceAll(name, `"`, "") + `"`
	}
	return ";CN=" + name
}

// escapeICSText applies RFC 5545 TEXT escaping.
func escapeICSText(s string) string {
	r := strings.NewReplacer(
		`\`, `\\`,
		";", `\;`,
		",", `\,`,
		"\r\n", `\n`,
		"\n", `\n`,
	)
	return r.Replace(s)
}
