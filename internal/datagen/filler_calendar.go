package datagen

import (
	"strings"
	"time"

	"github.com/chandrameenamohan/aubade/internal/model"
)

// The calendar filler: the standing meetings, one-offs, declines and blocks
// that make a founder's month look like a founder's month.
//
// Two rules keep it from answering the exam by accident, and both are enforced
// by place() rather than by care:
//
//  1. **Nothing overlaps.** A collision on the calendar is a finding, and every
//     collision this corpus is graded on was scripted by a scenario. A filler
//     event that would overlap a live event is dropped, not moved — moving it
//     would make the drop invisible and the calendar subtly wrong instead of
//     slightly emptier.
//  2. **Nothing physical lands on the anchor day.** The conflicts extractor also
//     reports impossible transitions — two rooms, fifteen minutes — so a filler
//     meeting with an address on today's calendar could invent one. Today's
//     filler is on Zoom or it is not there.
//
// The Tue/Thu 9-11 deep-work blocks are *not* here: they are scripted by
// conflictDeepWorkBlock, which needs them to exist across the whole window for
// its violation to mean anything. The filler schedules around them like any
// other calendar entry, which is exactly what a colleague is supposed to do.

// virtualPlaces are locations you do not have to walk to.
var virtualPlaces = []string{"zoom", "meet", "phone", "call", "online"}

// recurring is one standing entry in the week.
type recurring struct {
	slug      string
	summary   string
	weekday   time.Weekday // ignored when daily is set
	daily     bool         // every business day
	biweekly  bool         // every other week, counted from the anchor week
	from, to  int          // minutes past midnight
	location  string
	organizer model.Person
	attendees []model.Person
	calendar  string
}

// standingWeek is Avery's recurring calendar. It is deliberately dense: the
// digest's job is to find the one thing that matters in a week that already has
// fourteen meetings in it, and a sparse calendar makes that trivial.
var standingWeek = []recurring{
	{slug: "standup", summary: "Engineering standup", daily: true, from: 8*60 + 30, to: 8*60 + 45,
		location: "Zoom", organizer: Jordan, attendees: []model.Person{Avery, Priya, Jordan}},
	{slug: "1on1-priya", summary: "1:1 — Priya", weekday: time.Monday, from: 9*60 + 30, to: 10 * 60,
		location: "Zoom", organizer: Avery, attendees: []model.Person{Avery, Priya}},
	{slug: "metrics", summary: "Weekly metrics review", weekday: time.Monday, from: 11 * 60, to: 11*60 + 30,
		location: "Zoom", organizer: Nadia, attendees: []model.Person{Avery, Nadia, Tomas}},
	{slug: "pipeline", summary: "GTM pipeline walk", weekday: time.Tuesday, from: 14 * 60, to: 14*60 + 45,
		location: "Zoom", organizer: Tomas, attendees: []model.Person{Avery, Tomas, Nadia}},
	{slug: "1on1-jordan", summary: "1:1 — Jordan", weekday: time.Wednesday, from: 15 * 60, to: 15*60 + 30,
		location: "Zoom", organizer: Avery, attendees: []model.Person{Avery, Jordan}},
	{slug: "1on1-tomas", summary: "1:1 — Tomás", weekday: time.Thursday, from: 13*60 + 30, to: 14 * 60,
		location: "Zoom", organizer: Avery, attendees: []model.Person{Avery, Tomas}},
	{slug: "product-review", summary: "Product review", weekday: time.Thursday, from: 15 * 60, to: 16 * 60,
		location: "Zoom", organizer: Priya, attendees: []model.Person{Avery, Priya, Jordan, Tomas}},
	{slug: "all-hands", summary: "All-hands", weekday: time.Friday, biweekly: true, from: 11 * 60, to: 11*60 + 45,
		location: "Zoom", organizer: Nadia, attendees: []model.Person{Avery, Priya, Jordan, Tomas, Nadia}},
	{slug: "daycare-pickup", summary: "Wren — daycare pickup", weekday: time.Wednesday, from: 17*60 + 15, to: 18 * 60,
		location: "Willow Street Daycare", organizer: Sam, attendees: []model.Person{Avery},
		calendar: sharedCalendar},
	{slug: "swim", summary: "Wren — swim lesson", weekday: time.Saturday, from: 9 * 60, to: 9*60 + 45,
		location: "Oakland Rec Center", organizer: Sam, attendees: []model.Person{Avery},
		calendar: sharedCalendar},
}

// oneOff is a single meeting at a fixed offset from the anchor day. The offsets
// are written down rather than drawn so the month has a shape — interviews
// clustered around the hiring loop, customer calls spread across it — instead of
// the uniform sprinkle a random draw produces.
type oneOff struct {
	day, from, to int
	summary       string
	location      string
	organizer     model.Person
	attendees     []model.Person
	partStat      model.PartStat
	status        model.EventStatus
}

var oneOffMeetings = []oneOff{
	{-26, 13 * 60, 13*60 + 45, "Halberd quarterly business review", "Zoom", Renee, []model.Person{Avery, Tomas}, model.PartStatAccepted, ""},
	{-24, 16 * 60, 16*60 + 30, "Advisor call — pricing", "Zoom", Diane, []model.Person{Avery}, model.PartStatAccepted, ""},
	{-23, 10 * 60, 11 * 60, "Vendor pitch — Northwind CRM", "Zoom", model.Person{Name: "Northwind CRM", Email: "hello@northwindcrm.example"}, []model.Person{Avery}, model.PartStatDeclined, ""},
	{-21, 15 * 60, 16 * 60, "Backend candidate — founder call", "Zoom", Jordan, []model.Person{Avery, Jordan}, model.PartStatAccepted, ""},
	{-19, 12 * 60, 12*60 + 45, "Northstar pilot check-in", "Zoom", Dana, []model.Person{Avery, Tomas}, model.PartStatAccepted, ""},
	{-18, 17 * 60, 17*60 + 30, "Design review — settings", "Zoom", Priya, []model.Person{Avery, Priya}, model.PartStatAccepted, model.StatusCancelled},
	{-16, 13 * 60, 14 * 60, "Aperture Capital — intro call", "Zoom", David, []model.Person{Avery}, model.PartStatAccepted, ""},
	{-14, 16 * 60, 16*60 + 45, "Podcast interview — industrial data", "Zoom", model.Person{Name: "Freight Lines Podcast", Email: "booking@freightlines.example"}, []model.Person{Avery}, model.PartStatDeclined, ""},
	{-12, 14 * 60, 15 * 60, "Veritas renewal call", "Zoom", Luis, []model.Person{Avery, Tomas}, model.PartStatAccepted, ""},
	{-11, 10 * 60, 10*60 + 30, "Dentist", "Grand Ave Dental", Sam, []model.Person{Avery}, model.PartStatAccepted, ""},
	{-9, 16 * 60, 17 * 60, "Series A — partner meeting prep", "Zoom", Marcus, []model.Person{Avery, Ben}, model.PartStatAccepted, ""},
	{-7, 12 * 60, 12*60 + 30, "Brightmoor Logistics — discovery", "Zoom", Ines, []model.Person{Avery, Tomas}, model.PartStatAccepted, ""},
	{-6, 15 * 60, 16 * 60, "Industry panel — logistics summit", "Moscone West", model.Person{Name: "Logistics Summit", Email: "program@logisticssummit.example"}, []model.Person{Avery}, model.PartStatDeclined, ""},
	{-5, 13 * 60, 13*60 + 45, "Designer candidate — portfolio round", "Zoom", Tomas, []model.Person{Avery, Tomas}, model.PartStatAccepted, ""},
	{-4, 17 * 60, 17*60 + 30, "Partner intro — Kanto", "Zoom", model.Person{Name: "Kanto Analytics", Email: "partners@kanto.example"}, []model.Person{Avery}, model.PartStatAccepted, model.StatusCancelled},
	{2, 13 * 60, 14 * 60, "Northstar business review", "Zoom", Dana, []model.Person{Avery, Tomas}, model.PartStatNeedsAction, ""},
	{3, 16 * 60, 16*60 + 45, "Ben Schaffer — closing checklist", "Zoom", Ben, []model.Person{Avery}, model.PartStatAccepted, ""},
	{6, 14 * 60, 15 * 60, "Board prep — Diane", "Zoom", Diane, []model.Person{Avery}, model.PartStatNeedsAction, ""},
	{9, 12 * 60, 12*60 + 45, "Halberd rollout retro", "Zoom", Renee, []model.Person{Avery, Jordan}, model.PartStatNeedsAction, ""},
}

// allDayBlocks are the entries that take a day rather than an hour.
var allDayBlocks = []struct {
	day, days int
	summary   string
	organizer model.Person
	calendar  string
}{
	{-20, 1, "Wren — daycare closed", Sam, sharedCalendar},
	{-13, 2, "Team offsite — Presidio", Nadia, ""},
	{7, 1, "Company holiday — office closed", Nadia, ""},
	{11, 2, "Sam away — solo parenting", Sam, sharedCalendar},
}

// calendar writes the whole filler calendar.
func (f *filler) calendar() {
	f.standing()
	for _, m := range oneOffMeetings {
		status := m.status
		if status == "" {
			status = model.StatusConfirmed
		}
		attendees := make([]model.Attendee, 0, len(m.attendees))
		for _, p := range m.attendees {
			stat := model.PartStatAccepted
			if p == Avery {
				stat = m.partStat
			}
			attendees = append(attendees, model.Attendee{Person: p, PartStat: stat, Role: "REQ-PARTICIPANT"})
		}
		f.place(model.CalEvent{
			UID:       f.eventUID(slugOf(m.summary)),
			Summary:   m.summary,
			Location:  m.location,
			Start:     f.s.DayAt(m.day, m.from/60, m.from%60),
			End:       f.s.DayAt(m.day, m.to/60, m.to%60),
			Status:    status,
			Organizer: m.organizer,
			Attendees: attendees,
			// An invite is created before it is sent, and never after today —
			// a future CREATED would make "Tomás added this at 21:40 last
			// night" indistinguishable from an ordinary booking.
			Created: f.s.DayAt(min(m.day-2-f.rng().IntN(9), -1), 9, 0),
		})
	}
	for _, b := range allDayBlocks {
		start := f.s.Days(b.day)
		f.place(model.CalEvent{
			UID:       f.eventUID(slugOf(b.summary)),
			Summary:   b.summary,
			Start:     start,
			End:       start.AddDate(0, 0, b.days),
			AllDay:    true,
			Organizer: b.organizer,
			Calendar:  b.calendar,
			Created:   f.s.DayAt(b.day-14, 9, 0),
		})
	}
}

// standing expands the recurring week across the corpus window and the
// lookahead. The lookahead matters: "someone booked over your Tuesday block" is
// next week's meeting and this week's problem, so next week has to have a
// calendar at all.
func (f *filler) standing() {
	for offset := -CorpusDays + 1; offset <= lookaheadDays; offset++ {
		day := f.s.Days(offset)
		for _, r := range standingWeek {
			switch {
			case r.daily && !isBusinessDay(day):
				continue
			case !r.daily && day.Weekday() != r.weekday:
				continue
			case r.biweekly && weeksApart(f.s.Today(), day)%2 != 0:
				continue
			}
			f.place(model.CalEvent{
				UID:       f.eventUID(r.slug + "-" + day.Format("20060102")),
				Summary:   r.summary,
				Location:  r.location,
				Start:     f.s.At(day, r.from/60, r.from%60),
				End:       f.s.At(day, r.to/60, r.to%60),
				Status:    model.StatusConfirmed,
				Organizer: r.organizer,
				Attendees: Attendees(model.PartStatAccepted, r.attendees...),
				Calendar:  r.calendar,
				Created:   f.s.DayAt(-CorpusDays+1, 8, 0),
			})
		}
	}
}

// place plants an event unless it would invent a conflict. It reports whether
// the event was planted, so a caller that cares can say so; today's callers do
// not, because a dropped standing meeting is a slightly thinner week and
// nothing else.
func (f *filler) place(ev model.CalEvent) bool {
	holdsTime := ev.Status != model.StatusCancelled && !ev.DeclinedBy(Avery.Email) && !ev.AllDay

	if holdsTime && sameCorpusDay(ev.Start, f.s.Today()) && !isVirtual(ev.Location) {
		return false
	}
	if holdsTime {
		for _, other := range f.s.plan.Events {
			if other.Status == model.StatusCancelled || other.DeclinedBy(Avery.Email) || other.AllDay {
				continue
			}
			if ev.Start.Before(other.End) && other.Start.Before(ev.End) {
				return false
			}
		}
	}
	f.s.Event(ev)
	return true
}

// isVirtual reports whether a location is somewhere you dial into.
func isVirtual(location string) bool {
	l := strings.ToLower(location)
	for _, v := range virtualPlaces {
		if strings.Contains(l, v) {
			return true
		}
	}
	return false
}

// sameCorpusDay compares two instants by calendar day in the anchor zone.
func sameCorpusDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

// weeksApart counts whole weeks between the two days' Mondays, which is what a
// fortnightly meeting is actually counted in.
func weeksApart(a, b time.Time) int {
	weeks := int(weekOf(b).Sub(weekOf(a)).Hours() / 24 / 7)
	if weeks < 0 {
		weeks = -weeks
	}
	return weeks
}

// slugOf turns a summary into the readable half of a UID.
func slugOf(summary string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(summary) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		case !dash && b.Len() > 0:
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.Trim(b.String(), "-")
}
