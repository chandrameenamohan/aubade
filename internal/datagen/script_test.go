package datagen

import (
	"math/rand/v2"
	"strings"
	"testing"
	"time"

	"github.com/chandrameenamohan/aubade/internal/model"
)

func newTestScript(t *testing.T, day string) *Script {
	t.Helper()
	today := mustDay(t, day)
	return &Script{
		today: today,
		loc:   model.Location(),
		rng:   rand.New(rand.NewPCG(pinnedSeed, seedStream)),
		plan:  &Plan{Seed: pinnedSeed, Today: today},
	}
}

// BusinessDaysAgo is the helper every quiet-thread trap is timed with, so it
// gets a table of its own — including the anchors where a naive day offset and
// a business-day count disagree.
func TestBusinessDaysAgo(t *testing.T) {
	cases := []struct {
		today string
		n     int
		want  string
	}{
		{"2026-08-30", 0, "2026-08-30"}, // Sunday: no business days have elapsed
		{"2026-08-30", 1, "2026-08-27"}, // only Friday falls between here and Sunday
		{"2026-08-30", 2, "2026-08-26"},
		{"2026-08-30", 4, "2026-08-24"},
		{"2026-09-03", 1, "2026-09-02"}, // Thursday: yesterday was a business day
		{"2026-09-03", 4, "2026-08-28"}, // the count stops on a Sunday; the date walks back to Friday
		{"2026-09-07", 1, "2026-09-04"}, // Monday: back over the weekend to Friday
	}
	for _, tc := range cases {
		s := newTestScript(t, tc.today)
		got := s.BusinessDaysAgo(tc.n)
		if want := mustDay(t, tc.want); !got.Equal(want) {
			t.Errorf("today %s: BusinessDaysAgo(%d) = %s, want %s",
				tc.today, tc.n, got.Format("2006-01-02"), tc.want)
		}
	}
}

func TestNextWeekday(t *testing.T) {
	cases := []struct {
		today   string
		weekday time.Weekday
		want    string
	}{
		{"2026-08-30", time.Tuesday, "2026-09-01"},
		{"2026-08-30", time.Friday, "2026-09-04"},
		// Strictly after today: a Tuesday anchor gets next Tuesday, not itself.
		{"2026-09-01", time.Tuesday, "2026-09-08"},
	}
	for _, tc := range cases {
		s := newTestScript(t, tc.today)
		got := s.NextWeekday(tc.weekday)
		if want := mustDay(t, tc.want); !got.Equal(want) {
			t.Errorf("today %s: NextWeekday(%s) = %s, want %s",
				tc.today, tc.weekday, got.Format("2006-01-02"), tc.want)
		}
	}
}

func TestEmailDefaults(t *testing.T) {
	s := newTestScript(t, pinnedDay)
	ref := s.Email(model.Email{
		ID: "m-1", ThreadID: "t-1", TS: s.DayAt(-1, 9, 0),
		From: Marcus, Subject: "cap table", Body: "send it",
	})
	if len(s.errs) != 0 {
		t.Fatalf("valid email rejected: %v", s.errs)
	}
	if ref != (model.Citation{Source: model.SourceEmail, Ref: "m-1"}) {
		t.Errorf("citation = %+v, want an email citation for m-1", ref)
	}

	got := s.plan.Emails[0]
	if len(got.To) != 1 || got.To[0] != Avery {
		t.Errorf("To = %v, want Avery by default", got.To)
	}
	if got.CC == nil {
		t.Error("CC is nil; the inbox.jsonl contract wants an array, not a null")
	}
}

func TestEventDefaults(t *testing.T) {
	s := newTestScript(t, pinnedDay)
	s.Event(model.CalEvent{
		UID: "ev-1", Summary: "1:1 with Jordan",
		Start: s.DayAt(0, 9, 0), End: s.DayAt(0, 9, 30),
	})
	if len(s.errs) != 0 {
		t.Fatalf("valid event rejected: %v", s.errs)
	}

	got := s.plan.Events[0]
	if got.Status != model.StatusConfirmed {
		t.Errorf("Status = %q, want CONFIRMED by default", got.Status)
	}
	if got.Organizer != Avery {
		t.Errorf("Organizer = %v, want Avery by default", got.Organizer)
	}
	if want := got.Start.AddDate(0, 0, -7); !got.Created.Equal(want) {
		t.Errorf("Created = %s, want a week before the start (%s)", got.Created, want)
	}
}

// The emitters are the last line of defence against a scenario that writes
// something the loader would reject at read time. They record rather than
// panic, so one run reports every problem in the script.
func TestEmittersRejectBadArtifacts(t *testing.T) {
	cases := []struct {
		name string
		emit func(*Script)
		want string
	}{
		{"email with no thread", func(s *Script) {
			s.Email(model.Email{ID: "m-1", TS: s.DayAt(-1, 9, 0), From: Marcus, Subject: "x", Body: "x"})
		}, "thread_id"},
		{"event with no uid", func(s *Script) {
			s.Event(model.CalEvent{Summary: "x", Start: s.DayAt(0, 9, 0), End: s.DayAt(0, 10, 0)})
		}, "no UID"},
		{"event that ends before it starts", func(s *Script) {
			s.Event(model.CalEvent{UID: "ev-1", Summary: "x", Start: s.DayAt(0, 10, 0), End: s.DayAt(0, 9, 0)})
		}, "ends at or before"},
		{"event with a nonsense rsvp", func(s *Script) {
			s.Event(model.CalEvent{
				UID: "ev-1", Summary: "x", Start: s.DayAt(0, 9, 0), End: s.DayAt(0, 10, 0),
				Attendees: []model.Attendee{{Person: Avery, PartStat: "MAYBE"}},
			})
		}, "partstat"},
		{"note outside notes/", func(s *Script) {
			s.Note(model.Note{Path: "q2.md", Title: "Q2", Body: "body"})
		}, "must be notes/"},
		{"note with no body", func(s *Script) {
			s.Note(model.Note{Path: "notes/q2.md", Title: "Q2"})
		}, "no body"},
		{"task with no id", func(s *Script) {
			s.Task(model.Task{Title: "Send the cap table"})
		}, "no id"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestScript(t, pinnedDay)
			tc.emit(s)
			if len(s.errs) == 0 {
				t.Fatal("emitter accepted an artifact it should have refused")
			}
			if got := s.errs[0].Error(); !strings.Contains(got, tc.want) {
				t.Errorf("error %q does not mention %q", got, tc.want)
			}
		})
	}
}

// Pick has to be a function of the seed and of nothing else — same seed, same
// sequence of choices — or "byte-identical output" would depend on the order
// the Go runtime happened to run in.
func TestPickIsSeeded(t *testing.T) {
	choices := []string{"a", "b", "c", "d", "e"}
	var first, second []string
	for _, s := range []*Script{newTestScript(t, pinnedDay), newTestScript(t, pinnedDay)} {
		var got []string
		for range 20 {
			got = append(got, s.Pick(choices...))
		}
		if first == nil {
			first = got
			continue
		}
		second = got
	}
	if strings.Join(first, "") != strings.Join(second, "") {
		t.Errorf("Pick sequences differ across two scripts with the same seed: %v vs %v", first, second)
	}
	if s := newTestScript(t, pinnedDay); s.Pick() != "" {
		t.Error("Pick with no choices should return the empty string")
	}
}
