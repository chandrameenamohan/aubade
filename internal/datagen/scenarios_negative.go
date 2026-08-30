package datagen

import "github.com/chandrameenamohan/aubade/internal/model"

// The negative half of the exam.
//
// Each of these plants something a plausible engine would surface, and asserts
// that it does not. They are not filler with a flag on it: every one of them is
// a rule profile.md states in the first person, and every one of them is a
// specific false positive — the newsletter that is genuinely good, the
// marketing mail from a tool the company pays for, the thread where the only
// action is "FYI", the thread Avery already closed, the meeting she already
// accepted, the silence that has not yet earned the name.
//
// An eval with only positive tasks measures recall and calls it quality
// (EVAL-PRINCIPLES #8). A digest that surfaces everything is the inbox, which
// Avery already has.

// negativeNewsletter is the hardest suppression in the set, which is why
// profile.md names it twice: "Newsletters. Even the good ones. Even
// Stratechery." The content is genuinely relevant to the business, and it is
// still not what she asked for.
func negativeNewsletter(s *Script) Trap {
	issue := s.Email(model.Email{
		ID: "m-newsletter-01", ThreadID: "t-newsletter-stratechery", TS: s.DayAt(-1, 5, 0),
		From:    Stratechery,
		Subject: "The Weekly Update",
		Body: "This week: why vertical SaaS pricing power is a supply-chain story, and what the " +
			"last four quarters of manufacturing software M&A say about where the margin goes.\n\n" +
			"Unsubscribe at any time.",
		Labels: []string{"newsletter"},
	})

	return Trap{
		ID:   "negative-newsletter-stratechery",
		Kind: Newsletter,
		Description: "A genuinely good newsletter issue, on-topic for Tessera's market, that arrived " +
			"yesterday morning. profile.md suppresses newsletters by name and then names this one: " +
			"relevance is not the test, and \"I'll read them when I want to\" is the rule.",
		MustSurface: false,
		Expect: Expect{
			SignalKind: model.KindSuppressions,
			Keywords:   []string{"Stratechery", "The Weekly Update"},
		},
		PlantedRefs: []model.Citation{issue},
	}
}

// negativeVendorMarketing covers the clause that makes the suppression rule
// hard to implement with a domain list: "Marketing emails from SaaS tools, even
// ones we pay for." Tessera is a Pagerail customer, so the sender is a vendor
// Avery has a real relationship with, and the mail is still marketing.
func negativeVendorMarketing(s *Script) Trap {
	blast := s.Email(model.Email{
		ID: "m-vendor-01", ThreadID: "t-vendor-pagerail", TS: s.DayAt(-2, 7, 30),
		From:    Pagerail,
		Subject: "New in Pagerail: AI incident summaries",
		Body: "Your team is on the Pagerail Team plan — here's what shipped this month. " +
			"AI incident summaries now write the postmortem first draft for you.\n\n" +
			"Book a walkthrough with your account team.",
		Labels: []string{"marketing"},
	})

	return Trap{
		ID:   "negative-vendor-marketing-pagerail",
		Kind: VendorMarketing,
		Description: "Product-marketing mail from a SaaS vendor Tessera actually pays for. Being a " +
			"paying customer of the sender is what makes this tempting to surface, and " +
			"profile.md rules it out anyway.",
		MustSurface: false,
		Expect: Expect{
			SignalKind: model.KindSuppressions,
			Keywords:   []string{"Pagerail", "AI incident summaries"},
		},
		PlantedRefs: []model.Citation{blast},
	}
}

// negativeFYIOnly guards the dispatchables extractor from the other direction.
// The mail is internal, from a P1 sender, about a real regulation — everything
// a naive urgency heuristic likes — and it says in its first line that nothing
// is being asked of anyone. "Anything where the only 'action' is FYI."
func negativeFYIOnly(s *Script) Trap {
	fwd := s.Email(model.Email{
		ID: "m-fyi-01", ThreadID: "t-fyi-eu-ai-act", TS: s.DayAt(-2, 16, 5),
		From: Tomas, To: to(Avery), CC: to(Priya),
		Subject: "Fwd: EU AI Act guidance for supply-chain data processors",
		Body: "fyi, no action needed. Nothing here applies to us until we sell into the tier-2 " +
			"ecosystem next year. Parking it so we have the link when it matters.\n\nTomás",
		Labels: []string{"internal"},
	})

	return Trap{
		ID:   "negative-fyi-only",
		Kind: FYIOnly,
		Description: "An internal forward from a P1 colleague about real regulation, whose own first " +
			"line says no action is needed and nothing applies this year. It must not become a " +
			"dispatchable, a to-do, or an urgent item.",
		MustSurface: false,
		Expect: Expect{
			SignalKind: model.KindDispatchables,
			Keywords:   []string{"EU AI Act"},
		},
		PlantedRefs: []model.Citation{fwd},
	}
}

// negativeAveryLastWord is the false positive a quiet-thread detector reaches
// for first: a thread with no reply for nine days. Nobody replied because Avery
// ended it — "Long threads where I've already had the last word and nobody else
// has replied."
func negativeAveryLastWord(s *Script) Trap {
	const thread = "t-offsite"

	s.Email(model.Email{
		ID: "m-offsite-01", ThreadID: thread, TS: s.DayAt(-9, 15, 40),
		From: Priya, To: to(Avery), CC: to(Tomas, Jordan),
		Subject: "offsite venue options",
		Body: "Three offsite venue options costed out: Presidio, Sausalito, and the one in " +
			"Berkeley with the terrible parking. All fit the budget.\n\nPriya",
		Labels: []string{"internal"},
	})
	closed := s.Email(model.Email{
		ID: "m-offsite-02", ThreadID: thread, TS: s.DayAt(-9, 20, 15),
		From: Avery, To: to(Priya), CC: to(Tomas, Jordan),
		Subject: "Re: offsite venue options", InReplyTo: "m-offsite-01",
		Body:   "the presidio one. book it.\n\nAvery",
		Labels: []string{"internal"},
	})

	return Trap{
		ID:   "negative-avery-last-word",
		Kind: LastWord,
		Description: "Nine days of silence on a thread — because Avery closed it with a decision and " +
			"there is nothing left to say. A quiet-thread detector that only measures elapsed " +
			"time surfaces this one, and every one like it, until the section is noise.",
		MustSurface: false,
		Expect: Expect{
			SignalKind: model.KindQuietThreads,
			Keywords:   []string{"offsite venue", "Presidio"},
		},
		PlantedRefs: []model.Citation{closed},
	}
}

// negativeAcceptedInvite is the anti-pattern profile.md phrases as a complaint
// about bad digests: "Calendar invites I already accepted. I don't need a
// reminder that I have a meeting at 2pm; my calendar app does that."
//
// It sits on today's calendar, accepted, overlapping nothing — so a conflicts
// extractor has nothing to say about it and a renderer that lists the day's
// meetings back at her has everything to say about it.
func negativeAcceptedInvite(s *Script) Trap {
	sync := s.Event(model.CalEvent{
		UID:       "ev-leadership-sync",
		Summary:   "Weekly leadership sync",
		Location:  "Zoom",
		Start:     s.DayAt(0, 12, 0),
		End:       s.DayAt(0, 12, 45),
		Organizer: Priya,
		Attendees: Attendees(model.PartStatAccepted, Avery, Jordan, Tomas),
		Created:   s.DayAt(-21, 9, 0),
	})

	return Trap{
		ID:   "negative-accepted-invite",
		Kind: AcceptedInvite,
		Description: "A recurring meeting on today's calendar that Avery accepted three weeks ago and " +
			"that conflicts with nothing. There is no decision, no conflict and no news in it, so " +
			"it must not be surfaced as one.",
		MustSurface: false,
		Expect: Expect{
			SignalKind: model.KindConflicts,
			Keywords:   []string{"Weekly leadership sync"},
		},
		PlantedRefs: []model.Citation{sync},
	}
}

// negativeQuietBelowThreshold is quiet-investor-aperture's mirror: the same
// shape of silence, two business days old instead of four.
//
// The pair is the point. One trap on its own is passed by an extractor that
// flags every unanswered message; the pair can only be passed by one that
// implements the threshold profile.md actually states, and a scorecard showing
// this one surfaced names the boundary as the thing to fix.
func negativeQuietBelowThreshold(s *Script) Trap {
	const thread = "t-brightmoor"

	asked := s.Email(model.Email{
		ID: "m-brightmoor-01", ThreadID: thread, TS: s.At(s.BusinessDaysAgo(4), 13, 5),
		From:    Ines,
		Subject: "security questionnaire before procurement",
		Body: "Avery — our security questionnaire needs to be filled in before we can move you " +
			"to procurement. Attached.\n\nInes Marchetti\nBrightmoor Logistics",
		Labels: []string{"prospect"},
	})
	pending := s.Email(model.Email{
		ID: "m-brightmoor-02", ThreadID: thread, TS: s.At(s.BusinessDaysAgo(2), 6, 40),
		From: Avery, To: to(Ines),
		Subject: "Re: security questionnaire before procurement", InReplyTo: "m-brightmoor-01",
		Body:   "filled in and back to you. shout if anything's missing.\n\nAvery",
		Labels: []string{"prospect"},
	})

	return Trap{
		ID:   "negative-quiet-below-threshold",
		Kind: BelowThreshold,
		Description: "Avery's last message on the Brightmoor thread has gone unanswered for two business " +
			"days. The rule is three, so this is silence that has not earned a line in the digest " +
			"yet — the near-miss that proves quiet-thread detection implements a threshold rather " +
			"than flagging every unanswered message.",
		MustSurface: false,
		Expect: Expect{
			SignalKind: model.KindQuietThreads,
			Keywords:   []string{"security questionnaire", "Brightmoor"},
		},
		PlantedRefs: []model.Citation{asked, pending},
	}
}
