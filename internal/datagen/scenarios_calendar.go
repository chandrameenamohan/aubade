package datagen

import (
	"time"

	"github.com/chandrameenamohan/aubade/internal/model"
)

// sharedCalendar is the calendar Sam can write to. Which calendar an event came
// from is load-bearing for the family-collision trap: the same event on Avery's
// work calendar would be something she scheduled.
const sharedCalendar = "Avery + Sam (shared)"

// conflictDoubleBooked is the plain double-booking: two confirmed meetings on
// today's calendar that overlap by half an hour, with Avery on both invites.
//
// It is the easiest conflict in the corpus on purpose. If an eval only contains
// hard cases it cannot tell "the extractor is subtly wrong" from "the extractor
// does not run", and a reference solution has to be able to pass something
// (EVAL-PRINCIPLES #7).
func conflictDoubleBooked(s *Script) Trap {
	demo := s.Event(model.CalEvent{
		UID:       "ev-lumen-demo",
		Summary:   "Lumen Analytics demo",
		Location:  "Zoom",
		Start:     s.DayAt(0, 10, 30),
		End:       s.DayAt(0, 11, 15),
		Organizer: Lumen,
		Attendees: Attendees(model.PartStatNeedsAction, Avery),
		Created:   s.DayAt(-9, 16, 0),
		Description: "Standard demo or API-focused walkthrough — the rep is waiting on an agenda " +
			"choice.",
	})
	prep := s.Event(model.CalEvent{
		UID:       "ev-diligence-prep",
		Summary:   "Series A diligence prep — Ben",
		Location:  "Zoom",
		Start:     s.DayAt(0, 10, 45),
		End:       s.DayAt(0, 11, 45),
		Organizer: Ben,
		Attendees: Attendees(model.PartStatAccepted, Avery),
		Created:   s.DayAt(-4, 9, 30),
	})

	return Trap{
		ID:   "conflict-double-booked",
		Kind: CalendarOverlap,
		Description: "Two confirmed meetings on today's calendar overlap from 10:45 to 11:15: the Lumen " +
			"Analytics demo and the Series A diligence prep with Ben. Avery is on both invites and " +
			"has accepted one of them.",
		MustSurface: true,
		Expect: Expect{
			SignalKind: model.KindConflicts,
			Keywords:   []string{"Lumen Analytics", "diligence prep"},
		},
		PlantedRefs: []model.Citation{demo, prep},
	}
}

// conflictDeepWorkBlock plants the rule profile.md states as a grievance: "I
// block 9-11am Tue/Thu for deep work. Anyone scheduling over that block without
// asking is doing something wrong, and I want to know."
//
// The block is planted as a standing calendar fact across the whole corpus
// window, not as a single event beside the violation, because a detector that
// only sees one block is really matching a summary string. The violating
// meeting was created last night, so it is news this morning.
//
// The dates come from NextWeekday rather than a day offset: the rule is written
// in weekdays, and an offset would put the violation on a Thursday for one
// anchor date and a Sunday for the next.
func conflictDeepWorkBlock(s *Script) Trap {
	var violated model.Citation
	target := s.NextWeekday(time.Tuesday)

	for day := s.Days(-28); !day.After(s.Days(10)); day = day.AddDate(0, 0, 1) {
		if day.Weekday() != time.Tuesday && day.Weekday() != time.Thursday {
			continue
		}
		ref := s.Event(model.CalEvent{
			UID:       "ev-deep-work-" + day.Format("20060102"),
			Summary:   "Deep work — no meetings",
			Start:     s.At(day, 9, 0),
			End:       s.At(day, 11, 0),
			Organizer: Avery,
			Attendees: Attendees(model.PartStatAccepted, Avery),
			Created:   s.DayAt(-29, 8, 0),
		})
		if day.Equal(target) {
			violated = ref
		}
	}

	booked := s.Event(model.CalEvent{
		UID:       "ev-pipeline-review",
		Summary:   "Pipeline review — Q3 forecast",
		Location:  "Zoom",
		Start:     s.At(target, 9, 30),
		End:       s.At(target, 10, 30),
		Organizer: Tomas,
		Attendees: Attendees(model.PartStatNeedsAction, Avery, Jordan),
		// Added at 21:40 last night, without asking.
		Created:     s.DayAt(-1, 21, 40),
		Description: "Forecast review ahead of the board update.",
	})

	return Trap{
		ID:   "conflict-deep-work-block",
		Kind: DeepWorkViolation,
		Description: "Tomás booked the Pipeline review inside the standing Tuesday 9-11am deep-work " +
			"block, and added it at 21:40 last night without asking. profile.md is explicit that " +
			"Avery wants to be told when this happens.",
		MustSurface: true,
		Expect: Expect{
			SignalKind: model.KindConflicts,
			Keywords:   []string{"deep work", "Pipeline review"},
		},
		PlantedRefs: []model.Citation{violated, booked},
	}
}

// conflictFamilyCollision is the sample digest's closing item: Sam put "Wren —
// pediatrician" on the shared calendar at 21:04 last night, over the middle of
// Avery's afternoon.
//
// profile.md ranks this above the work conflicts — "Family first. Always." —
// and forbids drafting a reply to Sam, so the trap is deliberately not a
// dispatchable: the digest surfaces it and Avery writes the text herself. The
// event's CREATED timestamp is the fact that makes it new information, which is
// why the emitter's default (a week before the start) is overridden here.
func conflictFamilyCollision(s *Script) Trap {
	planning := s.Event(model.CalEvent{
		UID:       "ev-q2-planning-sync",
		Summary:   "Q2 planning sync",
		Location:  "Zoom",
		Start:     s.DayAt(0, 14, 30),
		End:       s.DayAt(0, 15, 30),
		Organizer: Priya,
		Attendees: Attendees(model.PartStatAccepted, Avery, Jordan, Tomas),
		Created:   s.DayAt(-11, 10, 0),
	})
	pediatrician := s.Event(model.CalEvent{
		UID:       "ev-wren-pediatrician",
		Summary:   "Wren — pediatrician",
		Location:  "Oakland Pediatrics",
		Start:     s.DayAt(0, 15, 0),
		End:       s.DayAt(0, 16, 0),
		Organizer: Sam,
		Attendees: Attendees(model.PartStatNeedsAction, Avery),
		Created:   s.DayAt(-1, 21, 4),
		Calendar:  sharedCalendar,
	})
	s.Event(model.CalEvent{
		UID:       "ev-interview-prep",
		Summary:   "Backend candidate interview prep",
		Start:     s.DayAt(0, 16, 0),
		End:       s.DayAt(0, 16, 45),
		Organizer: Jordan,
		Attendees: Attendees(model.PartStatAccepted, Avery),
		Created:   s.DayAt(-3, 14, 0),
	})

	return Trap{
		ID:   "conflict-family-collision",
		Kind: FamilyCollision,
		Description: "Sam added \"Wren — pediatrician\" to the shared calendar at 21:04 last night. " +
			"It overlaps the 14:30 Q2 planning sync and runs to the start of the 16:00 interview " +
			"prep. Family first, per profile.md — and no drafted reply to Sam, ever.",
		MustSurface: true,
		Expect: Expect{
			SignalKind: model.KindConflicts,
			Keywords:   []string{"Wren", "pediatrician"},
		},
		PlantedRefs: []model.Citation{pediatrician, planning},
	}
}
