package datagen

import (
	"time"

	"github.com/chandrameenamohan/aubade/internal/model"
)

// contradictionDiligenceCallDay is profile.md's own worked example: "If two
// sources disagree (e.g., calendar says I'm meeting Marcus Friday but email
// says we moved to Monday), tell me — don't pick one and hide it."
//
// The calendar still holds the Friday slot; the email thread moved it and both
// sides agreed. Nothing in the corpus says which is true, and that is the
// point: the honest answer renders both with citations, and any engine that
// silently picks one has failed the task even when it picks the right one.
func contradictionDiligenceCallDay(s *Script) Trap {
	friday := s.NextWeekday(time.Friday)

	scheduled := s.Event(model.CalEvent{
		UID:       "ev-marcus-diligence-call",
		Summary:   "Marcus Webb — Series A diligence call",
		Location:  "Zoom",
		Start:     s.At(friday, 11, 0),
		End:       s.At(friday, 12, 0),
		Organizer: Marcus,
		Attendees: Attendees(model.PartStatAccepted, Avery),
		Created:   s.DayAt(-12, 17, 15),
	})

	const thread = "t-diligence-call"
	moved := s.Email(model.Email{
		ID: "m-dcall-01", ThreadID: thread, TS: s.DayAt(-2, 15, 20),
		From:    Marcus,
		Subject: "diligence call — moving it",
		Body: "Friday no longer works on my side. Can we take the diligence call on the Monday " +
			"after instead, same time? I'll leave the invite to you.",
		Labels: []string{"investor", "raise"},
	})
	agreed := s.Email(model.Email{
		ID: "m-dcall-02", ThreadID: thread, TS: s.DayAt(-2, 19, 2),
		From: Avery, To: to(Marcus),
		Subject: "Re: diligence call — moving it", InReplyTo: "m-dcall-01",
		Body:   "monday works. same time.\n\nAvery",
		Labels: []string{"investor", "raise"},
	})

	return Trap{
		ID:   "contradiction-diligence-call-day",
		Kind: SourceContradiction,
		Description: "The calendar still has the Series A diligence call with Marcus on Friday; the email " +
			"thread moved it to the Monday after and both sides agreed. Neither source was updated " +
			"to match the other. Both sides must be rendered with citations — picking one and " +
			"hiding the other is the failure profile.md names by hand.",
		MustSurface: true,
		Expect: Expect{
			SignalKind: model.KindContradictions,
			Keywords:   []string{"diligence call", "Monday"},
		},
		PlantedRefs: []model.Citation{scheduled, moved, agreed},
	}
}

// contradictionVeritasRenewal is the same shape across a different pair of
// sources — a note against an email — so that the extractor cannot pass by
// special-casing calendar-versus-inbox.
//
// It also carries a second-order fact the digest may use but must not invent:
// this is the second time Veritas has moved the renewal date.
func contradictionVeritasRenewal(s *Script) Trap {
	renewalNote := s.Note(model.Note{
		Path:      "notes/customer-veritas-renewal.md",
		Title:     "Veritas renewal — call notes",
		Date:      s.DayAt(-5, 0, 0),
		Tags:      []string{"customers", "renewal"},
		Attendees: []string{"Avery Chen", "Tomás Reyes"},
		Body: "# Veritas renewal — call notes\n\n" +
			"Their CFO pushed the renewal conversation to next quarter. Tomás logged it the " +
			"same afternoon and did not escalate.\n\n" +
			"This is the second time Veritas has moved a renewal date.\n",
	})
	stillOn := s.Email(model.Email{
		ID: "m-veritas-01", ThreadID: "t-veritas-renewal", TS: s.DayAt(-3, 11, 40),
		From:    Luis,
		Subject: "renewal paperwork",
		Body: "Avery — confirming we're still targeting the September 30 renewal date on our " +
			"side. Send the paperwork whenever you're ready.\n\nLuis",
		Labels: []string{"customer", "renewal"},
	})
	s.Task(model.Task{
		ID:    "t-veritas-renewal",
		Title: "Confirm the Veritas renewal date with Luis",
		Due:   s.DayAt(2, 0, 0),
		Owner: "avery",
	})

	return Trap{
		ID:   "contradiction-veritas-renewal",
		Kind: SourceContradiction,
		Description: "The Veritas renewal note says their CFO pushed the conversation to next quarter; " +
			"their procurement lead emailed two days later confirming the September 30 date is " +
			"still on. A note and an email disagree, and the digest must show both.",
		MustSurface: true,
		Expect: Expect{
			SignalKind: model.KindContradictions,
			Keywords:   []string{"Veritas", "renewal"},
		},
		PlantedRefs: []model.Citation{renewalNote, stillOn},
	}
}

// stalenessUndatedModelNote plants the one thing the honesty layer cannot infer
// from a timestamp: a source that has no timestamp at all.
//
// The note carries no date in its front matter, so nothing can age it —
// model.Note.HasDate is false and the loader keeps it that way deliberately
// rather than falling back to the file's mtime, which would say something
// different on every clone. An open task points straight at it, so the digest
// has a reason to cite it, and citing it as if it were current is exactly the
// fabricated certainty SPEC §7 calls an eval failure.
func stalenessUndatedModelNote(s *Script) Trap {
	undated := s.Note(model.Note{
		Path:  "notes/series-a-model.md",
		Title: "Series A model — inference cost line",
		Tags:  []string{"raise", "model"},
		Body: "# Series A model — inference cost line\n\n" +
			"Gross margin in the model assumes the inference pricing we were on before the " +
			"volume change. Marcus will ask about this line in diligence.\n\n" +
			"Re-run before the diligence call. Do not send the deck with these numbers.\n",
	})
	rerun := s.Task(model.Task{
		ID:    "t-cost-model",
		Title: "Re-run the inference cost model",
		Owner: "avery",
		Meta:  map[string]string{"source": "series-a-model"},
	})

	return Trap{
		ID:   "staleness-undated-model-note",
		Kind: StaleData,
		Description: "notes/series-a-model.md has no date in its front matter, so nothing can tell how " +
			"old it is. An open task points at it and it speaks to a diligence question, so the " +
			"digest has every reason to cite it — and must say it cannot age the source rather " +
			"than presenting it as current.",
		MustSurface: true,
		Expect: Expect{
			SignalKind: model.KindStaleness,
			Keywords:   []string{"series-a-model", "inference cost model"},
		},
		PlantedRefs: []model.Citation{undated, rerun},
	}
}
