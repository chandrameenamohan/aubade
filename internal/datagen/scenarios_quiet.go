package datagen

import (
	"fmt"

	"github.com/chandrameenamohan/aubade/internal/model"
)

// quietInvestorAperture plants the three-business-day rule from profile.md:
// "If Marcus or another VC went quiet, I want to know after three business
// days, not three weeks."
//
// The thread opens at a one-day cadence so the silence is a change rather than
// a personality, and Avery's unanswered question is timed with BusinessDaysAgo
// so the trap sits on the far side of the threshold for every --today. Its
// mirror image, negativeQuietBelowThreshold, sits two business days out and
// must stay off the page: together they pin the boundary from both sides.
func quietInvestorAperture(s *Script) Trap {
	const thread = "t-aperture"

	opened := s.Email(model.Email{
		ID: "m-aperture-01", ThreadID: thread, TS: s.DayAt(-16, 10, 20),
		From:    David,
		Subject: "good conversation today",
		Body: "Avery — enjoyed that. Send me the metrics deck and I'll turn the diligence list " +
			"around quickly.\n\nDavid Kim\nAperture Capital",
		Labels: []string{"investor", "raise"},
	})
	s.Email(model.Email{
		ID: "m-aperture-02", ThreadID: thread, TS: s.DayAt(-15, 7, 30),
		From: Avery, To: to(David),
		Subject: "Re: good conversation today", InReplyTo: "m-aperture-01",
		Body:   "deck attached. numbers are through last month.\n\nAvery",
		Labels: []string{"investor", "raise"},
	})
	s.Email(model.Email{
		ID: "m-aperture-03", ThreadID: thread, TS: s.DayAt(-15, 15, 5),
		From:    David,
		Subject: "Re: good conversation today", InReplyTo: "m-aperture-02",
		Body:   "Got it — reading this week. Will come back with the diligence list.",
		Labels: []string{"investor", "raise"},
	})
	unanswered := s.Email(model.Email{
		ID: "m-aperture-04", ThreadID: thread, TS: s.At(s.BusinessDaysAgo(4), 6, 38),
		From: Avery, To: to(David),
		Subject: "Re: good conversation today", InReplyTo: "m-aperture-03",
		Body:   "any movement on the diligence list? we're trying to close the round in three weeks.\n\nAvery",
		Labels: []string{"investor", "raise"},
	})

	return Trap{
		ID:   "quiet-investor-aperture",
		Kind: QuietInvestor,
		Description: "David Kim at Aperture Capital replied within a day twice, then went silent on " +
			"Avery's direct question — four business days ago, during an active raise. " +
			"profile.md wants this after three business days, not three weeks.",
		MustSurface: true,
		Expect: Expect{
			SignalKind: model.KindQuietThreads,
			Keywords:   []string{"Aperture Capital", "David Kim"},
		},
		PlantedRefs: []model.Citation{opened, unanswered},
	}
}

// cadenceSlowdownNorthstar plants the customer-health signal: nothing is wrong,
// nobody complained, no ticket was filed — the replies just got slower.
//
// This is the trap that argues for a corpus rather than a fixture. One message
// carries no signal at all; the finding only exists in the gaps between ten of
// them, spread over four weeks and interleaved with everything else in the
// timeline. The minute each reply landed comes from the seed, because nothing
// about the finding depends on it.
func cadenceSlowdownNorthstar(s *Script) Trap {
	const thread = "t-northstar"

	// Day offsets for the exchange: Dana answers the same day or the next one
	// for the first fortnight, then takes five days, then stops answering.
	// The hour of each message is scripted — Dana writes in her working day,
	// Avery in the three windows profile.md describes — so that a reply never
	// lands before the message it answers. Only the minute comes from the seed.
	type beat struct {
		day      int
		hour     int
		fromDana bool
		subject  string
		body     string
	}
	beats := []beat{
		{-27, 9, true, "pilot rollout questions", "Avery — three questions on the pilot before we sign off internally.\n\nDana"},
		{-27, 21, false, "Re: pilot rollout questions", "answers inline. shout if any of it is unclear.\n\nAvery"},
		{-26, 8, true, "Re: pilot rollout questions", "Perfect, thank you. One more on the audit export.\n\nDana"},
		{-26, 12, false, "Re: pilot rollout questions", "shipping in the next release. i'll confirm the date.\n\nAvery"},
		{-25, 10, true, "Re: pilot rollout questions", "Great. I'll take this to our ops leads today.\n\nDana"},
		{-20, 6, false, "Re: pilot rollout questions", "audit export is live. anything back from the ops leads?\n\nAvery"},
		{-15, 14, true, "Re: pilot rollout questions", "Sorry for the delay — still working through it here.\n\nDana"},
		{-14, 6, false, "Re: pilot rollout questions", "no rush. want me to join a call with them?\n\nAvery"},
		{-9, 16, true, "Re: pilot rollout questions", "Maybe later in the month. Things are busy on our side.\n\nDana"},
		{-5, 6, false, "Re: pilot rollout questions", "checking in — is the rollout still where we left it?\n\nAvery"},
	}

	var lastFromDana, lastFromAvery model.Citation
	for i, b := range beats {
		sender, recipients := Dana, to(Avery)
		if !b.fromDana {
			sender, recipients = Avery, to(Dana)
		}
		var inReplyTo string
		if i > 0 {
			inReplyTo = fmt.Sprintf("m-northstar-%02d", i)
		}
		// The gap between messages is the trap, and no trap turns on :14
		// versus :47.
		minute := s.Rand().IntN(60)
		ref := s.Email(model.Email{
			ID:       fmt.Sprintf("m-northstar-%02d", i+1),
			ThreadID: thread,
			TS:       s.DayAt(b.day, b.hour, minute),
			From:     sender,
			To:       recipients,
			Subject:  b.subject,
			Body:     b.body, InReplyTo: inReplyTo,
			Labels: []string{"customer"},
		})
		if b.fromDana {
			lastFromDana = ref
		} else {
			lastFromAvery = ref
		}
	}

	healthNote := s.Note(model.Note{
		Path:  "notes/customer-health.md",
		Title: "Customer health — reference accounts",
		Date:  s.DayAt(-2, 0, 0),
		Tags:  []string{"customers"},
		Body: "# Customer health — reference accounts\n\n" +
			"- **Halberd**: Renee replies same day, every time. No concerns.\n" +
			"- **Northstar**: reply cadence has stretched from about a day to about five days " +
			"over the last fortnight. No support tickets open, no complaint, nothing said. " +
			"Could be nothing; it is also exactly what the last churn looked like.\n" +
			"- **Veritas**: renewal conversation keeps moving. See the renewal note.\n",
	})

	return Trap{
		ID:   "cadence-slowdown-northstar",
		Kind: CadenceSlowdown,
		Description: "Dana Whitfield at Northstar Foods replied inside a day for the first fortnight of " +
			"the thread and now takes five, and has not answered Avery's last check-in at all. " +
			"No ticket, no complaint — the only evidence is the widening gap between messages in " +
			"a reference customer's thread.",
		MustSurface: true,
		Expect: Expect{
			SignalKind: model.KindQuietThreads,
			Keywords:   []string{"Northstar", "reply cadence"},
		},
		PlantedRefs: []model.Citation{lastFromDana, lastFromAvery, healthNote},
	}
}

// stalledDesignerLoop plants the hiring rule: "if a candidate is mid-loop and
// nobody has moved them in 5+ days, that's on me."
//
// The candidate himself sent the last message, which is what makes it bite —
// the thread is not waiting on information, it is waiting on Avery, and the
// person waiting is one the company is trying to hire.
func stalledDesignerLoop(s *Script) Trap {
	const thread = "t-designer-loop"

	handoff := s.Email(model.Email{
		ID: "m-hiring-01", ThreadID: thread, TS: s.DayAt(-14, 11, 25),
		From:    Tomas,
		Subject: "Ravi Desai — portfolio round done",
		Body: "Ravi cleared the portfolio round with both designers. Next step is the founder " +
			"call with you. I'll get it scheduled this week.\n\nTomás",
		Labels: []string{"hiring"},
	})
	s.Email(model.Email{
		ID: "m-hiring-02", ThreadID: thread, TS: s.DayAt(-14, 20, 2),
		From: Avery, To: to(Tomas),
		Subject: "Re: Ravi Desai — portfolio round done", InReplyTo: "m-hiring-01",
		Body:   "good. don't let this one sit — he has other loops running.\n\nAvery",
		Labels: []string{"hiring"},
	})
	candidateWaiting := s.Email(model.Email{
		ID: "m-hiring-03", ThreadID: thread, TS: s.DayAt(-8, 7, 12),
		From: Ravi, To: to(Avery), CC: to(Tomas),
		Subject: "Re: Ravi Desai — portfolio round done", InReplyTo: "m-hiring-02",
		Body: "Hi Avery — following up on the founder call. Happy to work around your calendar; " +
			"is there anything else you'd like to see from me first?\n\nRavi",
		Labels: []string{"hiring"},
	})

	statusNote := s.Note(model.Note{
		Path:  "notes/hiring-status.md",
		Title: "Hiring status",
		Date:  s.DayAt(-7, 0, 0),
		Tags:  []string{"hiring"},
		Body: "# Hiring status\n\n" +
			"- Backend #1: offer signed, Mei Tanaka starts next month.\n" +
			"- Backend #2: two candidates in the technical round, Jordan owns.\n" +
			"- Designer loop: Ravi Desai is mid-loop, waiting on the founder call. " +
			"Tomás owns the next step and it has not moved since the portfolio round.\n",
	})
	s.Task(model.Task{
		ID:    "t-offer-letter",
		Title: "Sign Mei Tanaka's offer letter",
		Done:  true,
		Due:   s.DayAt(-12, 0, 0),
		Owner: "avery",
	})

	return Trap{
		ID:   "stalled-designer-loop",
		Kind: StalledLoop,
		Description: "The designer loop has not moved in eight days and the last message in it is the " +
			"candidate's own follow-up, addressed to Avery. profile.md: a candidate mid-loop with " +
			"nobody moving them for five or more days is on her.",
		MustSurface: true,
		Expect: Expect{
			SignalKind: model.KindQuietThreads,
			Keywords:   []string{"Ravi Desai", "designer loop"},
		},
		PlantedRefs: []model.Citation{handoff, candidateWaiting, statusNote},
	}
}
