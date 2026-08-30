package extract

import (
	"strings"
	"testing"

	"github.com/chandrameenamohan/aubade/internal/model"
)

// A reply that is nothing but a deadline, to a message that asked for
// something, is a promise. This is the case the sample digest turns on — Avery
// answered "tonight." and never sent the cap table — and the one a
// phrase-matcher looking only for "I'll" would miss entirely.
func TestCommitmentsBareDeadlineReplyIsAPromise(t *testing.T) {
	tb := loadFixture(t, "corpus", fixtureDay)
	ss, err := tb.Commitments()
	if err != nil {
		t.Fatalf("Commitments: %v", err)
	}

	s, ok := findSignal(ss, "commitments:e-002")
	if !ok {
		t.Fatalf("no commitment for e-002; got %d signals", len(ss))
	}
	if s.Priority != model.P0 {
		t.Errorf("priority = %s, want P0 (owed to Marcus, profile.md)", s.Priority)
	}
	if s.SectionHint != model.SectionOneThingNow {
		t.Errorf("section = %s, want %s for an overdue P0", s.SectionHint, model.SectionOneThingNow)
	}
	if s.Deadline == nil || !s.Deadline.Before(tb.Now()) {
		t.Errorf("deadline = %v, want an instant before the anchor morning", s.Deadline)
	}
	// The promise, the ask it answered, and the list entry still tracking it.
	for _, want := range []struct {
		source model.Source
		ref    string
	}{
		{model.SourceEmail, "e-002"},
		{model.SourceEmail, "e-001"},
		{model.SourceTask, "t-cap-table"},
	} {
		if !cites(s, want.source, want.ref) {
			t.Errorf("commitment does not cite %s:%s (citations: %v)", want.source, want.ref, s.Citations)
		}
	}
}

// An explicit promise with a relative deadline resolves against the day the
// promise was written, not the day the digest runs.
func TestCommitmentsExplicitPromiseWithRelativeDeadline(t *testing.T) {
	tb := loadFixture(t, "corpus", fixtureDay)
	ss, _ := tb.Commitments()

	s, ok := findSignal(ss, "commitments:e-007")
	if !ok {
		t.Fatal("no commitment for e-007 (\"sending the Q1 board update this week\")")
	}
	if s.Priority != model.P1 {
		t.Errorf("priority = %s, want P1 (Diane, profile.md)", s.Priority)
	}
	// "this week" written on Monday 24 August means Friday 28 August.
	if got := s.Deadline.In(tb.Location()).Format("2006-01-02"); got != "2026-08-28" {
		t.Errorf("deadline = %s, want 2026-08-28 (the Friday of the week it was written)", got)
	}
	if !cites(s, model.SourceTask, "t-board-update") {
		t.Errorf("commitment should cite the open task it matches: %v", s.Citations)
	}
}

// A promise recorded in a meeting note is the "promise I made and didn't put on
// a todo list" the profile asks for help with. With no date to hold it to it is
// unsure by construction, and it says so.
func TestCommitmentsFromNoteAreUnsureAndCiteTheNote(t *testing.T) {
	tb := loadFixture(t, "corpus", fixtureDay)
	ss, _ := tb.Commitments()

	s, ok := findSignal(ss, "commitments:note:notes/staffing-sync.md")
	if !ok {
		t.Fatal("no commitment extracted from notes/staffing-sync.md")
	}
	if s.Confidence != model.Unsure {
		t.Errorf("confidence = %s, want unsure for a commitment with no date", s.Confidence)
	}
	if s.SectionHint != model.SectionNotSure {
		t.Errorf("section = %s, want %s", s.SectionHint, model.SectionNotSure)
	}
	if !cites(s, model.SourceNote, "notes/staffing-sync.md") {
		t.Errorf("note commitment must cite the note path: %v", s.Citations)
	}
	if s.Priority != model.P0 {
		t.Errorf("priority = %s, want P0 (owed to Priya, named in the note)", s.Priority)
	}
}

// Hard negative: a promise that was kept is silent. Three ways of keeping it,
// three silences — a digest that lists delivered promises is noise, and noise
// is what gets the whole page ignored.
func TestCommitmentsKeptPromisesAreSilent(t *testing.T) {
	ask := func() model.Email {
		return msg(t, "m-1", "th-1", "2026-08-25T09:00:00-07:00", "marcus", "the deck", "Can you send the deck?")
	}
	promise := func() model.Email {
		e := msg(t, "m-2", "th-1", "2026-08-25T10:00:00-07:00", "x", "Re: the deck", "I'll send the deck tomorrow.")
		e.InReplyTo = "m-1"
		return fromOwner(e, "marcus")
	}

	cases := []struct {
		name   string
		corpus *model.Corpus
		want   int
	}{
		{
			name:   "undelivered",
			corpus: corpusOf([]model.Email{ask(), promise()}),
			want:   1,
		},
		{
			name: "owner delivered in thread",
			corpus: corpusOf([]model.Email{ask(), promise(), fromOwner(
				msg(t, "m-3", "th-1", "2026-08-26T09:00:00-07:00", "x", "Re: the deck", "Attached."), "marcus")}),
			want: 0,
		},
		{
			name: "counterparty acknowledged",
			corpus: corpusOf([]model.Email{ask(), promise(),
				msg(t, "m-3", "th-1", "2026-08-26T09:00:00-07:00", "marcus", "Re: the deck", "Got it, thanks.")}),
			want: 0,
		},
		{
			name: "closed on the task list",
			corpus: corpusOf([]model.Email{ask(), promise()},
				model.Task{ID: "t-deck", Title: "Send Marcus the board deck", Done: true, Line: 1}),
			want: 0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ss, err := toolboxOf(t, tc.corpus, "2026-08-31").Commitments()
			if err != nil {
				t.Fatalf("Commitments: %v", err)
			}
			if len(ss) != tc.want {
				t.Fatalf("got %d commitments, want %d: %+v", len(ss), tc.want, ss)
			}
		})
	}
}

// Hard negative: a promise with no deadline in the same sentence is not a
// commitment this extractor will date. "I'll take a look" is a courtesy, not an
// obligation with a clock on it, and a digest that treats it as one cries wolf.
func TestCommitmentsRequireADeadlineInTheSameSentence(t *testing.T) {
	cases := []struct {
		name string
		body string
		want int
	}{
		{"no deadline at all", "I'll take a look.", 0},
		{"deadline in a different sentence", "I'll take a look. The board meeting is Thursday.", 0},
		{"deadline in the promise", "I'll take a look by Thursday.", 1},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := fromOwner(msg(t, "m-1", "th-1", "2026-08-25T10:00:00-07:00", "x", "review", tc.body), "marcus")
			ss, err := toolboxOf(t, corpusOf([]model.Email{e}), "2026-08-31").Commitments()
			if err != nil {
				t.Fatalf("Commitments: %v", err)
			}
			if len(ss) != tc.want {
				t.Fatalf("got %d commitments, want %d: %+v", len(ss), tc.want, ss)
			}
		})
	}
}

// A bare deadline is only a promise when it answers something. A one-word reply
// to a message that asked nothing is small talk.
func TestCommitmentsBareDeadlineNeedsATrigger(t *testing.T) {
	statement := msg(t, "m-1", "th-1", "2026-08-25T09:00:00-07:00", "marcus", "note", "The board packet is out.")
	reply := fromOwner(msg(t, "m-2", "th-1", "2026-08-25T10:00:00-07:00", "x", "Re: note", "tonight."), "marcus")
	reply.InReplyTo = "m-1"

	ss, err := toolboxOf(t, corpusOf([]model.Email{statement, reply}), "2026-08-31").Commitments()
	if err != nil {
		t.Fatalf("Commitments: %v", err)
	}
	if len(ss) != 0 {
		t.Fatalf("a bare deadline answering nothing is not a promise; got %+v", ss)
	}
}

// The detail line has to name the words that fired, or a false positive is a
// mystery instead of an argument.
func TestCommitmentDetailQuotesTheEvidence(t *testing.T) {
	tb := loadFixture(t, "corpus", fixtureDay)
	ss, _ := tb.Commitments()
	s, ok := findSignal(ss, "commitments:e-007")
	if !ok {
		t.Fatal("missing e-007 commitment")
	}
	for _, want := range []string{"sending the Q1 board update this week", "profile.md:"} {
		if !strings.Contains(s.Detail, want) {
			t.Errorf("detail does not mention %q:\n%s", want, s.Detail)
		}
	}
}
