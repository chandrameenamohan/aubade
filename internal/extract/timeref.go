package extract

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Time reasoning for the toolbox.
//
// Two jobs live here. The first is business-day arithmetic, because the profile
// writes its thresholds in business days ("after three business days, not three
// weeks") and a weekend must not age a thread. The second is turning the way
// people actually write deadlines — "tonight", "by Friday", "Sept 4" — into an
// instant, so a promise can be called overdue with a date rather than a vibe.
//
// Every resolution is relative to the message that carried the phrase, never to
// the anchor day: "tomorrow" in an email sent last Tuesday means last
// Wednesday, and reading it as tomorrow-from-today is how a digest invents a
// deadline nobody agreed to.

// endOfBusiness and endOfDay are the hours a deadline phrase resolves to. They
// are conventions, chosen once and applied everywhere: what matters downstream
// is whether a deadline has passed relative to the anchor morning, and both
// hours give the same answer on every day but the deadline's own.
const (
	endOfBusinessHour = 17
	endOfDayHour      = 23
)

// isBusinessDay reports whether t falls on a weekday. Holidays are not modelled:
// the corpus does not carry a holiday calendar, and inventing one would make
// the threshold depend on a table nobody can see from the output.
func isBusinessDay(t time.Time) bool {
	switch t.Weekday() {
	case time.Saturday, time.Sunday:
		return false
	}
	return true
}

// businessDaysBetween counts the weekdays elapsed from a to b: every Mon–Fri
// calendar day strictly after a's day, up to and including b's day.
//
// So a Friday-evening message is 1 business day old on Monday morning and 0 on
// Saturday, which is the arithmetic the profile's "three business days" rule
// assumes. b before a returns 0 rather than a negative — callers ask "how long
// has this been sitting", and a message from the future has not been sitting.
func businessDaysBetween(a, b time.Time, loc *time.Location) int {
	if loc == nil {
		loc = time.UTC
	}
	from := startOfDay(a.In(loc))
	to := startOfDay(b.In(loc))
	if !to.After(from) {
		return 0
	}
	n := 0
	for d := from.AddDate(0, 0, 1); !d.After(to); d = d.AddDate(0, 0, 1) {
		if isBusinessDay(d) {
			n++
		}
	}
	return n
}

// startOfDay is midnight on t's calendar day, in t's own zone.
func startOfDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

// sameDay reports whether two instants fall on one calendar day in loc.
func sameDay(a, b time.Time, loc *time.Location) bool {
	return startOfDay(a.In(loc)).Equal(startOfDay(b.In(loc)))
}

// businessDayPhrase renders a business-day count for prose.
func businessDayPhrase(n int) string {
	switch n {
	case 0:
		return "less than a business day"
	case 1:
		return "1 business day"
	default:
		return fmt.Sprintf("%d business days", n)
	}
}

// DueRef is a deadline phrase found in text, kept with the words that carried
// it so a signal can quote the promise rather than paraphrase it.
type DueRef struct {
	// Text is the matched phrase, verbatim from the message.
	Text string `json:"text"`
	// Deadline is the instant it resolves to, relative to the message.
	Deadline time.Time `json:"deadline"`
}

var (
	monthDayRE = regexp.MustCompile(`(?i)\b(jan|feb|mar|apr|may|jun|jul|aug|sep|sept|oct|nov|dec)[a-z]*\.?\s+(\d{1,2})(?:st|nd|rd|th)?\b`)
	isoDateRE  = regexp.MustCompile(`\b(\d{4})-(\d{2})-(\d{2})\b`)
	inNDaysRE  = regexp.MustCompile(`(?i)\b(?:in|within)\s+(\d{1,2})\s+(?:business\s+)?days?\b`)
	clockRE    = regexp.MustCompile(`(?i)\b(\d{1,2})(?::(\d{2}))?\s*(am|pm)\b|\b(\d{1,2}):(\d{2})\b`)
	byClockRE  = regexp.MustCompile(`(?i)\bby\s+(\d{1,2})(?::(\d{2}))?\s*(am|pm)\b`)
)

var monthByAbbrev = map[string]time.Month{
	"jan": time.January, "feb": time.February, "mar": time.March,
	"apr": time.April, "may": time.May, "jun": time.June,
	"jul": time.July, "aug": time.August, "sep": time.September,
	"sept": time.September, "oct": time.October, "nov": time.November,
	"dec": time.December,
}

var weekdayByName = map[string]time.Weekday{
	"monday": time.Monday, "mon": time.Monday,
	"tuesday": time.Tuesday, "tue": time.Tuesday, "tues": time.Tuesday,
	"wednesday": time.Wednesday, "wed": time.Wednesday,
	"thursday": time.Thursday, "thu": time.Thursday, "thurs": time.Thursday,
	"friday": time.Friday, "fri": time.Friday,
	"saturday": time.Saturday, "sat": time.Saturday,
	"sunday": time.Sunday, "sun": time.Sunday,
}

// weekdayOrder fixes the scan order over weekdayByName so two runs over the same
// sentence find the same phrase first. Longest names first, so "thursday" wins
// over "thu".
var weekdayOrder = []string{
	"wednesday", "thursday", "saturday", "tuesday", "thurs", "monday", "friday",
	"sunday", "tues", "mon", "tue", "wed", "thu", "fri", "sat", "sun",
}

// ParseDueRefs finds every deadline phrase in text and resolves it against base
// (the instant the text was written) in loc.
//
// Results are ordered earliest deadline first, so a caller wanting "the
// deadline" can take the first: when someone writes "I'll send it tonight, and
// the full model by Friday", the thing they are late on is tonight.
func ParseDueRefs(text string, base time.Time, loc *time.Location) []DueRef {
	if loc == nil {
		loc = time.UTC
	}
	b := base.In(loc)
	day := startOfDay(b)
	lower := strings.ToLower(text)

	var refs []DueRef
	add := func(phrase string, at time.Time) {
		for _, r := range refs {
			if r.Deadline.Equal(at) {
				return
			}
		}
		refs = append(refs, DueRef{Text: phrase, Deadline: at})
	}

	// Relative day words.
	for _, c := range []struct {
		phrase string
		at     time.Time
	}{
		{"end of day", day.Add(endOfBusinessHour * time.Hour)},
		{"end of the day", day.Add(endOfBusinessHour * time.Hour)},
		{"eod", day.Add(endOfBusinessHour * time.Hour)},
		{"tonight", day.Add(endOfDayHour * time.Hour)},
		{"today", day.Add(endOfDayHour * time.Hour)},
		{"this afternoon", day.Add(endOfBusinessHour * time.Hour)},
		{"first thing tomorrow", day.AddDate(0, 0, 1).Add(9 * time.Hour)},
		{"tomorrow", day.AddDate(0, 0, 1).Add(endOfBusinessHour * time.Hour)},
		{"end of week", nextWeekday(day, time.Friday).Add(endOfBusinessHour * time.Hour)},
		{"end of the week", nextWeekday(day, time.Friday).Add(endOfBusinessHour * time.Hour)},
		{"eow", nextWeekday(day, time.Friday).Add(endOfBusinessHour * time.Hour)},
		{"this week", nextWeekday(day, time.Friday).Add(endOfBusinessHour * time.Hour)},
		{"next week", nextWeekday(day, time.Friday).AddDate(0, 0, 7).Add(endOfBusinessHour * time.Hour)},
	} {
		if containsWord(lower, c.phrase) {
			add(c.phrase, c.at)
		}
	}

	// Weekday names resolve to the next occurrence after the message's day.
	for _, name := range weekdayOrder {
		if containsWord(lower, name) {
			add(name, nextWeekday(day, weekdayByName[name]).Add(endOfBusinessHour*time.Hour))
			break
		}
	}

	// "Sept 4", "September 4th".
	if m := monthDayRE.FindStringSubmatch(text); m != nil {
		if mon, ok := monthByAbbrev[strings.ToLower(m[1])]; ok {
			if d, err := strconv.Atoi(m[2]); err == nil && d >= 1 && d <= 31 {
				add(collapse(m[0]), monthDayNear(b, mon, d, loc))
			}
		}
	}

	// "2026-09-04".
	if m := isoDateRE.FindStringSubmatch(text); m != nil {
		y, _ := strconv.Atoi(m[1])
		mo, _ := strconv.Atoi(m[2])
		d, _ := strconv.Atoi(m[3])
		if mo >= 1 && mo <= 12 && d >= 1 && d <= 31 {
			add(m[0], time.Date(y, time.Month(mo), d, endOfBusinessHour, 0, 0, 0, loc))
		}
	}

	// "in 3 days", "within 2 business days".
	if m := inNDaysRE.FindStringSubmatch(text); m != nil {
		if n, err := strconv.Atoi(m[1]); err == nil && n > 0 && n <= 60 {
			at := day.AddDate(0, 0, n)
			if strings.Contains(strings.ToLower(m[0]), "business") {
				at = addBusinessDays(day, n)
			}
			add(collapse(m[0]), at.Add(endOfBusinessHour*time.Hour))
		}
	}

	// "by 5pm" with no day named means today.
	if m := byClockRE.FindStringSubmatch(text); m != nil {
		if h, min, ok := parseClock(m[1], m[2], m[3]); ok {
			add(collapse(m[0]), day.Add(time.Duration(h)*time.Hour+time.Duration(min)*time.Minute))
		}
	}

	slices.SortStableFunc(refs, func(a, b DueRef) int { return a.Deadline.Compare(b.Deadline) })
	return refs
}

// nextWeekday is the next occurrence of w strictly after day, or day itself when
// day already is w — "by Friday" written on a Friday means that Friday.
func nextWeekday(day time.Time, w time.Weekday) time.Time {
	delta := (int(w) - int(day.Weekday()) + 7) % 7
	return day.AddDate(0, 0, delta)
}

// addBusinessDays advances day by n weekdays.
func addBusinessDays(day time.Time, n int) time.Time {
	d := day
	for n > 0 {
		d = d.AddDate(0, 0, 1)
		if isBusinessDay(d) {
			n--
		}
	}
	return d
}

// monthDayNear resolves a bare month/day against the message's year, rolling
// into the next year only when that would otherwise put the date more than six
// months in the past. "Sept 4" written in August means this September; written
// in December it means next September.
func monthDayNear(base time.Time, mon time.Month, day int, loc *time.Location) time.Time {
	at := time.Date(base.Year(), mon, day, endOfBusinessHour, 0, 0, 0, loc)
	switch {
	case at.Before(base.AddDate(0, -6, 0)):
		return at.AddDate(1, 0, 0)
	case at.After(base.AddDate(0, 6, 0)):
		return at.AddDate(-1, 0, 0)
	default:
		return at
	}
}

// parseClock turns a matched clock into 24-hour parts.
func parseClock(hour, minute, meridiem string) (int, int, bool) {
	h, err := strconv.Atoi(hour)
	if err != nil {
		return 0, 0, false
	}
	m := 0
	if minute != "" {
		if m, err = strconv.Atoi(minute); err != nil || m > 59 {
			return 0, 0, false
		}
	}
	switch strings.ToLower(meridiem) {
	case "am":
		if h == 12 {
			h = 0
		}
	case "pm":
		if h < 12 {
			h += 12
		}
	default:
		if h > 23 {
			return 0, 0, false
		}
	}
	if h > 23 {
		return 0, 0, false
	}
	return h, m, true
}

// TimeAssertion is a date and/or clock a message states about something —
// "the demo moved to Thursday at 2pm". It is what the contradiction extractor
// compares against the calendar.
type TimeAssertion struct {
	Text     string    `json:"text"`
	At       time.Time `json:"at"`
	HasClock bool      `json:"has_clock"`
	HasDate  bool      `json:"has_date"`
}

// ParseTimeAssertion reads the date and clock a sentence states, relative to
// base. It returns false when the sentence pins nothing down — most sentences.
//
// Deliberately conservative: a bare weekday or a bare clock is enough, but the
// caller is expected to have already established that the sentence is talking
// about the meeting in question. A time with nothing to attach it to is not a
// contradiction, it is a number.
func ParseTimeAssertion(sentence string, base time.Time, loc *time.Location) (TimeAssertion, bool) {
	if loc == nil {
		loc = time.UTC
	}
	b := base.In(loc)
	day := startOfDay(b)
	lower := strings.ToLower(sentence)

	var (
		out     TimeAssertion
		dateSet bool
	)

	setDate := func(phrase string, d time.Time) {
		if dateSet {
			return
		}
		dateSet = true
		out.HasDate = true
		out.At = startOfDay(d)
		out.Text = phrase
	}

	if m := isoDateRE.FindStringSubmatch(sentence); m != nil {
		y, _ := strconv.Atoi(m[1])
		mo, _ := strconv.Atoi(m[2])
		d, _ := strconv.Atoi(m[3])
		if mo >= 1 && mo <= 12 && d >= 1 && d <= 31 {
			setDate(m[0], time.Date(y, time.Month(mo), d, 0, 0, 0, 0, loc))
		}
	}
	if m := monthDayRE.FindStringSubmatch(sentence); m != nil {
		if mon, ok := monthByAbbrev[strings.ToLower(m[1])]; ok {
			if d, err := strconv.Atoi(m[2]); err == nil && d >= 1 && d <= 31 {
				setDate(collapse(m[0]), monthDayNear(b, mon, d, loc))
			}
		}
	}
	if !dateSet {
		switch {
		case containsWord(lower, "tomorrow"):
			setDate("tomorrow", day.AddDate(0, 0, 1))
		case containsWord(lower, "today") || containsWord(lower, "this afternoon") || containsWord(lower, "this morning"):
			setDate("today", day)
		default:
			for _, name := range weekdayOrder {
				if containsWord(lower, name) {
					setDate(name, nextWeekday(day, weekdayByName[name]))
					break
				}
			}
		}
	}

	if m := clockRE.FindStringSubmatch(sentence); m != nil {
		hour, minute, meridiem := m[1], m[2], m[3]
		if hour == "" {
			hour, minute, meridiem = m[4], m[5], ""
		}
		if h, mi, ok := parseClock(hour, minute, meridiem); ok {
			out.HasClock = true
			base := out.At
			if !dateSet {
				base = day
			}
			out.At = base.Add(time.Duration(h)*time.Hour + time.Duration(mi)*time.Minute)
			if out.Text == "" {
				out.Text = collapse(m[0])
			} else {
				out.Text += " " + collapse(m[0])
			}
		}
	}

	if !out.HasDate && !out.HasClock {
		return TimeAssertion{}, false
	}
	return out, true
}
