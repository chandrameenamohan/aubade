package datagen

import "github.com/chandrameenamohan/aubade/internal/model"

// The notes filler brings notes/ up to the ten meeting notes the SPEC asks for.
// Five of the ten are scripted by scenarios — the board-update cadence, customer
// health, the Veritas renewal, hiring status, and the undated Series A model
// note — and each of those is evidence for a trap. These five are not evidence
// for anything, which is the constraint they are written under:
//
//   - every one carries a date, so the staleness extractor's "notes that cannot
//     be aged" finding stays the scripted one;
//   - none of them records a promise with a deadline attached, so the
//     commitment tracker's findings stay the scripted ones;
//   - none of them restates a meeting time, so nothing here can disagree with
//     the calendar by accident.
//
// What they are for is context. A digest that cites notes/pricing-experiment.md
// while explaining a customer thread is doing the thing the corpus exists to
// make possible, and it can only do that if there are notes worth citing.

// fillerNotes are dated meeting notes with no trap attached. Days are offsets
// from the anchor date.
var fillerNotes = []struct {
	path, title string
	day         int
	tags        []string
	attendees   []string
	body        string
}{
	{
		path: "notes/sprint.md", title: "Sprint review — ingest and exports", day: -3,
		tags:      []string{"engineering", "sprint"},
		attendees: []string{"Avery Chen", "Priya Iyer", "Jordan Liu"},
		body: "# Sprint review — ingest and exports\n\n" +
			"Shipped: per-tenant ingest caps, the CSV export with a header row, and the first " +
			"half of the API reference rewrite.\n\n" +
			"Not shipped: search inside the docs. It was scoped optimistically and the team said " +
			"so at the start of the sprint.\n\n" +
			"Discussion was mostly about the ingest caps. The retry storm that caused the latency " +
			"spike would have been absorbed by the cap, and nobody could name a customer the cap " +
			"would inconvenience.\n\n" +
			"Velocity is roughly flat over three sprints. Jordan reads that as healthy for a team " +
			"of four; nobody argued.\n",
	},
	{
		path: "notes/q2-planning.md", title: "Q2 planning", day: -11,
		tags:      []string{"planning", "q2"},
		attendees: []string{"Avery Chen", "Priya Iyer", "Tomás Reyes", "Nadia Boulos"},
		body: "# Q2 planning\n\n" +
			"Three themes came out of the session: depth in the plant-floor integrations, a real " +
			"onboarding path that does not need a human, and enough security paperwork to stop " +
			"losing mid-market deals at procurement.\n\n" +
			"The argument was about the second one. Tomás wants the onboarding work first because " +
			"it unblocks self-serve; Priya wants the integrations first because they are what the " +
			"reference customers actually renew on. Both are right and only one can be first.\n\n" +
			"Resolution: integrations lead, onboarding follows in the same quarter with a smaller " +
			"scope than originally drawn.\n\n" +
			"Headcount assumption behind all of it is two engineers and a designer. If the designer " +
			"loop stays where it is, the onboarding scope is the thing that gives.\n",
	},
	{
		path: "notes/pricing-experiment.md", title: "Pricing experiment — read-out", day: -18,
		tags:      []string{"pricing", "gtm"},
		attendees: []string{"Avery Chen", "Tomás Reyes"},
		body: "# Pricing experiment — read-out\n\n" +
			"Ran the simplified three-tier page against the old five-tier page for four weeks.\n\n" +
			"Trial starts went up 22%. Trial-to-paid was flat. Average contract value fell about " +
			"9%, entirely because the middle tier absorbed accounts that used to land on the top " +
			"one.\n\n" +
			"Read: the page is better and the tiering is worse. The middle tier is priced too " +
			"close to the top for anyone to feel the difference.\n\n" +
			"Nothing here is conclusive at this volume. Four weeks and sixty trials is a direction, " +
			"not a result.\n",
	},
	{
		path: "notes/support-escalations.md", title: "Support escalations — monthly look", day: -6,
		tags:      []string{"support", "customers"},
		attendees: []string{"Avery Chen", "Nadia Boulos"},
		body: "# Support escalations — monthly look\n\n" +
			"Nineteen escalations this month, down from twenty-six. Two thirds of them were the " +
			"same three questions: export formats, connector permissions, and what the sync window " +
			"actually guarantees.\n\n" +
			"All three are documentation problems wearing support tickets as a disguise.\n\n" +
			"One escalation was a genuine bug in the audit export and it was fixed inside a day.\n\n" +
			"No account escalated twice. That is the number worth watching, and it is the one that " +
			"looked bad in the spring.\n",
	},
	{
		path: "notes/investor-pipeline.md", title: "Investor pipeline — where each one stands", day: -13,
		tags:      []string{"raise"},
		attendees: []string{"Avery Chen"},
		body: "# Investor pipeline — where each one stands\n\n" +
			"Written for my own head, not for anyone else.\n\n" +
			"Inflection Point is the live one. Marcus is doing the work and the partnership " +
			"conversation is the next gate.\n\n" +
			"Aperture is warm and slow. David has the deck and reads carefully.\n\n" +
			"Two others passed politely and both said the same thing about gross margin, which is " +
			"a pattern rather than a coincidence.\n\n" +
			"The honest summary is that there is one process and one option, and everything about " +
			"how I run the next month should reflect that.\n",
	},
}

// notes plants the filler notes.
func (f *filler) notes() {
	for _, n := range fillerNotes {
		f.s.Note(model.Note{
			Path:      n.path,
			Title:     n.title,
			Date:      f.s.Days(n.day),
			Tags:      n.tags,
			Attendees: n.attendees,
			Body:      n.body,
		})
	}
}
