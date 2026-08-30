package datagen

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chandrameenamohan/aubade/internal/localfs"
	"github.com/chandrameenamohan/aubade/internal/model"
)

// Inject adds questions to a corpus that has already been written, so what it
// has to prove is narrow and total: everything that was there is still there,
// unchanged, and everything added is readable by the loader that will grade it.

// delta builds a one-of-each plan anchored on the pinned day.
func delta(t *testing.T) *Plan {
	t.Helper()
	day := mustDay(t, pinnedDay)
	at := func(offset, hour, min int) time.Time {
		d := day.AddDate(0, 0, offset)
		y, m, dd := d.Date()
		return time.Date(y, m, dd, hour, min, 0, 0, model.Location())
	}
	start := at(2, 14, 0)

	return &Plan{
		Emails: []model.Email{{
			ID:       "inj-e-1",
			ThreadID: "inj-t-1",
			TS:       at(-4, 10, 30),
			From:     model.Person{Name: "Rosa Vidal", Email: "rosa@vidal.example"},
			To:       []model.Person{Avery},
			CC:       []model.Person{},
			Subject:  "The pallet of hexagonal washers",
			Body:     "Confirming the pallet of hexagonal washers ships Thursday.",
		}},
		Events: []model.CalEvent{{
			UID:       "inj-ev-1",
			Summary:   "Washer logistics sync",
			Start:     start,
			End:       start.Add(30 * time.Minute),
			Status:    model.StatusConfirmed,
			Organizer: Avery,
			Created:   at(-1, 21, 4),
			Attendees: Attendees(model.PartStatAccepted, Avery),
		}},
		Notes: []model.Note{{
			Path:  "notes/inj-washers.md",
			Title: "Washer logistics",
			Date:  at(-4, 0, 0),
			Body:  "Rosa confirmed the pallet.",
		}},
		Tasks: []model.Task{{ID: "inj-task-1", Title: "Sign the washer PO", Due: at(1, 0, 0)}},
		Traps: Traps{{
			ID:          "inj-trap-1",
			Kind:        FYIOnly,
			Description: "An injected shipping confirmation that needs nothing from anyone.",
			MustSurface: false,
			Expect:      Expect{SignalKind: model.KindSuppressions, Keywords: []string{"hexagonal washers"}},
			PlantedRefs: []model.Citation{{Source: model.SourceEmail, Ref: "inj-e-1"}},
		}},
	}
}

// Everything that was in the corpus is still in it, and everything injected is
// there too — read back through the loader that will grade it, not through the
// writer that produced it.
func TestInjectAddsToTheCorpusWithoutDisturbingIt(t *testing.T) {
	dir := writeCorpus(t, pinnedSeed)

	before, err := model.LoadCorpus(context.Background(), localfs.New(dir))
	if err != nil {
		t.Fatalf("load before: %v", err)
	}
	if err := Inject(dir, delta(t)); err != nil {
		t.Fatalf("Inject: %v", err)
	}
	after, err := model.LoadCorpus(context.Background(), localfs.New(dir))
	if err != nil {
		t.Fatalf("the injected corpus no longer loads: %v", err)
	}

	for _, c := range []struct {
		what         string
		before, want int
	}{
		{"emails", len(before.Emails), len(after.Emails)},
		{"events", len(before.Events), len(after.Events)},
		{"notes", len(before.Notes), len(after.Notes)},
		{"tasks", len(before.Tasks), len(after.Tasks)},
	} {
		if c.want != c.before+1 {
			t.Errorf("%s: %d before, %d after; want exactly one more", c.what, c.before, c.want)
		}
	}

	// The existing artifacts are untouched, not merely still counted.
	for _, e := range before.Emails {
		found := false
		for _, got := range after.Emails {
			if got.ID == e.ID {
				found = true
				if got.Subject != e.Subject || !got.TS.Equal(e.TS) {
					t.Errorf("email %s changed under injection", e.ID)
				}
			}
		}
		if !found {
			t.Errorf("email %s disappeared under injection", e.ID)
		}
	}

	// And the answer key grew by exactly the injected entry.
	raw, err := os.ReadFile(filepath.Join(dir, TrapsFile))
	if err != nil {
		t.Fatalf("read traps: %v", err)
	}
	var traps Traps
	if err := json.Unmarshal(raw, &traps); err != nil {
		t.Fatalf("decode traps: %v", err)
	}
	if _, ok := traps.ByID("inj-trap-1"); !ok {
		t.Error("the injected trap is not in the answer key")
	}
	if err := traps.Validate(); err != nil {
		t.Errorf("the merged answer key is invalid: %v", err)
	}
}

// The VEVENT goes inside the VCALENDAR. A component appended after the closing
// line is not RFC 5545 and not something a reader is obliged to see.
func TestInjectKeepsTheCalendarWellFormed(t *testing.T) {
	dir := writeCorpus(t, pinnedSeed)
	if err := Inject(dir, delta(t)); err != nil {
		t.Fatalf("Inject: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, localfs.CalendarFile))
	if err != nil {
		t.Fatalf("read calendar: %v", err)
	}
	ics := string(raw)
	if !strings.HasSuffix(ics, "END:VCALENDAR\r\n") {
		t.Error("the calendar no longer ends with its closing line")
	}
	if strings.Count(ics, "END:VCALENDAR") != 1 {
		t.Error("the injection added a second VCALENDAR rather than a VEVENT")
	}
	at, closing := strings.Index(ics, "UID:inj-ev-1"), strings.Index(ics, "END:VCALENDAR")
	if at < 0 || at > closing {
		t.Error("the injected event is not inside the calendar that contains it")
	}
}

// A note path that already exists is a refusal, not an overwrite: silently
// replacing a note some other trap cites would make that trap ungradeable.
func TestInjectRefusesToOverwriteANote(t *testing.T) {
	dir := writeCorpus(t, pinnedSeed)
	corpus, err := model.LoadCorpus(context.Background(), localfs.New(dir))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(corpus.Notes) == 0 {
		t.Fatal("the corpus has no notes to collide with")
	}

	d := delta(t)
	d.Notes[0].Path = corpus.Notes[0].Path
	err = Inject(dir, d)
	if err == nil {
		t.Fatal("injecting over an existing note must be refused")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("the refusal does not say why: %v", err)
	}
}
