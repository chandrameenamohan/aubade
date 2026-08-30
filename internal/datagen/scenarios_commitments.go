package datagen

import "github.com/chandrameenamohan/aubade/internal/model"

// commitmentCapTableSlip is the sample digest's opening item, planted as a
// task: Avery promised her lead investor the updated cap table, twice, and sent
// it neither time.
//
// It is the hardest shape in the corpus and the reason the commitment tracker
// exists at all — the evidence for it is a thing that did *not* happen. Two
// promises ("by wednesday", then "tonight") and no message carrying the
// artifact; a task on the list, past due, still open; and a third party holding
// the file and waiting to be told to send it.
func commitmentCapTableSlip(s *Script) Trap {
	const thread = "t-cap-table"

	s.Email(model.Email{
		ID: "m-capt-01", ThreadID: thread, TS: s.DayAt(-10, 9, 12),
		From:    Marcus,
		Subject: "cap table before we open diligence",
		Body: "Avery — before I take this to the partnership I need the updated cap table " +
			"with the new option pool. Nothing moves on the term sheet until I have it.\n\nMarcus",
		Labels: []string{"investor", "raise"},
	})
	promisedWednesday := s.Email(model.Email{
		ID: "m-capt-02", ThreadID: thread, TS: s.DayAt(-10, 21, 40),
		From: Avery, To: to(Marcus),
		Subject: "Re: cap table before we open diligence", InReplyTo: "m-capt-01",
		Body:   "on it. will get it to you by wednesday.\n\nAvery",
		Labels: []string{"investor", "raise"},
	})
	s.Email(model.Email{
		ID: "m-capt-03", ThreadID: thread, TS: s.DayAt(-3, 16, 42),
		From:    Marcus,
		Subject: "Re: cap table before we open diligence", InReplyTo: "m-capt-02",
		Body: "Still don't have the cap table. This is the piece blocking the term sheet " +
			"conversation on our side.",
		Labels: []string{"investor", "raise"},
	})
	promisedTonight := s.Email(model.Email{
		ID: "m-capt-04", ThreadID: thread, TS: s.DayAt(-3, 18, 10),
		From: Avery, To: to(Marcus), CC: to(Ben),
		Subject: "Re: cap table before we open diligence", InReplyTo: "m-capt-03",
		Body:   "tonight. ben has the latest.\n\nAvery",
		Labels: []string{"investor", "raise"},
	})
	s.Email(model.Email{
		ID: "m-capt-05", ThreadID: thread, TS: s.DayAt(-2, 9, 11),
		From: Ben, CC: to(Marcus),
		Subject: "Re: cap table before we open diligence", InReplyTo: "m-capt-04",
		Body: "I have the version from the 409A refresh — I believe that's the one you mean. " +
			"Confirm and I'll send it straight to Marcus.\n\nBen",
		Labels: []string{"legal", "raise"},
	})

	openTask := s.Task(model.Task{
		ID:    "t-cap-table",
		Title: "Send Marcus the updated cap table",
		Due:   s.DayAt(-2, 0, 0),
		Owner: "avery",
	})

	return Trap{
		ID:   "commitment-cap-table-slip",
		Kind: CommitmentSlip,
		Description: "Avery promised Marcus Webb the updated cap table twice — \"by wednesday\" ten days " +
			"ago and \"tonight\" three days ago — and no message in the thread ever carried it. " +
			"The task is past due and still open, and Ben is holding the file waiting to be told " +
			"to send it. Second slip on the same commitment to the lead investor: this is the one " +
			"thing to do first.",
		MustSurface: true,
		Expect: Expect{
			SignalKind: model.KindCommitments,
			Keywords:   []string{"cap table", "Marcus Webb"},
		},
		PlantedRefs: []model.Citation{promisedWednesday, promisedTonight, openTask},
	}
}

// commitmentBoardUpdateOverdue is the quiet version of the same failure: a
// commitment made once, in a note, to someone who will never chase it.
//
// profile.md says it in as many words — "She's patient and I owe her her
// quarterly updates and don't always send them." The evidence is spread across
// three sources on purpose (the note that records the promise, the last update
// that was actually sent, the task that went past due), because a tracker that
// only reads email would find nothing here.
func commitmentBoardUpdateOverdue(s *Script) Trap {
	cadenceNote := s.Note(model.Note{
		Path:  "notes/board-update-cadence.md",
		Title: "Board update cadence",
		Date:  s.DayAt(-25, 0, 0),
		Tags:  []string{"board", "commitments"},
		Body: "# Board update cadence\n\n" +
			"Agreed with Diane Okafor: a written board update on the 20th of every month, " +
			"whether or not there is news. She is patient and does not chase, which is exactly " +
			"why this is the one I drop.\n\n" +
			"Sent the last one today. Next one is due on the 20th.\n",
	})
	lastSent := s.Email(model.Email{
		ID: "m-board-01", ThreadID: "t-board-update", TS: s.DayAt(-25, 8, 5),
		From: Avery, To: to(Diane),
		Subject: "Tessera — monthly board update",
		Body: "diane — update below. arr at 3.2m, series a conversations open with two funds, " +
			"hiring two engineers and a designer.\n\nnext one on the 20th.\n\nAvery",
		Labels: []string{"board"},
	})
	overdueTask := s.Task(model.Task{
		ID:    "t-board-update",
		Title: "Send Diane the monthly board update",
		Due:   s.DayAt(-10, 9, 0),
		Owner: "avery",
	})

	return Trap{
		ID:   "commitment-board-update-overdue",
		Kind: CommitmentSlip,
		Description: "The board update Avery committed to in notes/board-update-cadence.md is ten days " +
			"past its due date and nothing has been sent since the update twenty-five days ago. " +
			"Diane Okafor does not chase, so nothing in the inbox will ever raise this — it has to " +
			"come from the note, the last-sent email, and the open task read together.",
		MustSurface: true,
		Expect: Expect{
			SignalKind: model.KindCommitments,
			Keywords:   []string{"board update", "Diane Okafor"},
		},
		PlantedRefs: []model.Citation{cadenceNote, lastSent, overdueTask},
	}
}
