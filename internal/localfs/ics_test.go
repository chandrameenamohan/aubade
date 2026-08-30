package localfs

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/chandrameenamohan/aubade/internal/model"
)

func testLoc(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(model.DefaultTimeZone)
	if err != nil {
		t.Fatalf("LoadLocation(%s): %v", model.DefaultTimeZone, err)
	}
	return loc
}

func TestParseICS(t *testing.T) {
	loc := testLoc(t)
	events, err := parseICS("calendar.ics", readFixture(t, "corpus/calendar.ics"), loc)
	if err != nil {
		t.Fatalf("parseICS: %v", err)
	}
	if len(events) != 5 {
		t.Fatalf("parsed %d events, want 5", len(events))
	}

	byUID := map[string]model.CalEvent{}
	for _, e := range events {
		byUID[e.UID] = e
	}

	t.Run("TZID times, folding and a colon in the value", func(t *testing.T) {
		e := byUID["ev-1on1-jordan"]
		want := time.Date(2026, 8, 30, 9, 0, 0, 0, loc)
		if !e.Start.Equal(want) {
			t.Errorf("start = %s, want %s", e.Start, want)
		}
		if e.Duration() != 30*time.Minute {
			t.Errorf("duration = %s, want 30m", e.Duration())
		}
		// "SUMMARY:1:1 with Jordan" — only the first unquoted colon separates.
		if e.Summary != "1:1 with Jordan" {
			t.Errorf("summary = %q, want %q", e.Summary, "1:1 with Jordan")
		}
		// The attendee address is folded across two physical lines.
		if len(e.Attendees) != 1 || e.Attendees[0].Email != "jordan@tessera.io" {
			t.Fatalf("attendees = %+v, want the unfolded jordan@tessera.io", e.Attendees)
		}
		if e.Attendees[0].PartStat != model.PartStatAccepted || e.Attendees[0].Role != "REQ-PARTICIPANT" {
			t.Errorf("attendee = %+v, want ACCEPTED/REQ-PARTICIPANT", e.Attendees[0])
		}
		if e.Calendar != "Avery (shared)" {
			t.Errorf("calendar = %q, want the X-WR-CALNAME", e.Calendar)
		}
		if want := time.Date(2026, 8, 25, 10, 0, 0, 0, loc); !e.Created.Equal(want) {
			t.Errorf("created = %s, want %s (CREATED, in the corpus zone)", e.Created, want)
		}
	})

	t.Run("UTC start, DURATION, escaped text, quoted parameter", func(t *testing.T) {
		e := byUID["ev-lumen-demo"]
		want := time.Date(2026, 8, 30, 10, 30, 0, 0, loc) // 17:30Z
		if !e.Start.Equal(want) {
			t.Errorf("start = %s, want %s", e.Start, want)
		}
		if e.Duration() != 45*time.Minute {
			t.Errorf("duration = %s, want 45m from DURATION:PT45M", e.Duration())
		}
		if e.Summary != "Lumen Analytics demo, API-focused" {
			t.Errorf("summary = %q, want the comma unescaped", e.Summary)
		}
		if e.Description != "Agenda choice still open.\nRep is waiting." {
			t.Errorf("description = %q, want the \\n unescaped", e.Description)
		}
		if e.Status != model.StatusTentative {
			t.Errorf("status = %q, want TENTATIVE", e.Status)
		}
		if e.Organizer.Name != "Lumen, Analytics" {
			t.Errorf("organizer name = %q, want the quoted CN kept whole", e.Organizer.Name)
		}
	})

	t.Run("declines are visible", func(t *testing.T) {
		e := byUID["ev-gtm-sync"]
		if !e.DeclinedBy("AVERY@tessera.io") {
			t.Errorf("DeclinedBy is case-sensitive or missed the DECLINED PARTSTAT: %+v", e.Attendees)
		}
		if e.Status != model.StatusCancelled {
			t.Errorf("status = %q, want CANCELLED", e.Status)
		}
		if got := e.PartStatOf("nobody@example.com"); got != "" {
			t.Errorf("PartStatOf(stranger) = %q, want empty", got)
		}
	})

	t.Run("all-day event defaults to one day", func(t *testing.T) {
		e := byUID["ev-deep-work"]
		if !e.AllDay {
			t.Errorf("VALUE=DATE event not marked all-day")
		}
		start := time.Date(2026, 9, 1, 0, 0, 0, 0, loc)
		if !e.Start.Equal(start) || !e.End.Equal(start.AddDate(0, 0, 1)) {
			t.Errorf("all-day span = %s..%s, want one day from %s", e.Start, e.End, start)
		}
		if e.Status != model.StatusConfirmed {
			t.Errorf("status = %q, want the CONFIRMED default when STATUS is absent", e.Status)
		}
	})

	t.Run("file order is preserved", func(t *testing.T) {
		want := []string{"ev-1on1-jordan", "ev-lumen-demo", "ev-wren-pediatrician", "ev-gtm-sync", "ev-deep-work"}
		for i, uid := range want {
			if events[i].UID != uid {
				t.Fatalf("events[%d] = %s, want %s", i, events[i].UID, uid)
			}
		}
	})
}

func TestParseICSRejectsMalformed(t *testing.T) {
	cases := []struct {
		name   string
		file   string
		line   int
		substr string
	}{
		{"no UID", "malformed/calendar-no-uid.ics", 3, "no UID"},
		{"end before start", "malformed/calendar-end-before-start.ics", 6, "ends before it starts"},
		{"unterminated component", "malformed/calendar-unterminated.ics", 8, "closes an open VEVENT"},
		{"unknown status", "malformed/calendar-bad-status.ics", 8, "unknown STATUS"},
		{"no end and no duration", "malformed/calendar-no-end.ics", 3, "neither DTEND nor DURATION"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseICS("calendar.ics", readFixture(t, tc.file), testLoc(t))
			wantValidationError(t, err, tc.line, tc.substr)
		})
	}
}

// A property with no ':' is not a content line, and a VEVENT outside a
// VCALENDAR is not a calendar. Both are cheap to detect and expensive to guess
// at.
func TestParseICSRejectsStructuralJunk(t *testing.T) {
	cases := []struct {
		name   string
		body   string
		substr string
	}{
		{"no separator", "BEGIN:VCALENDAR\nJUST SOME TEXT\nEND:VCALENDAR\n", "no ':' separator"},
		{"event outside calendar", "BEGIN:VEVENT\nUID:x\nEND:VEVENT\n", "outside a VCALENDAR"},
		{"unterminated calendar", "BEGIN:VCALENDAR\nVERSION:2.0\n", "unterminated component"},
		{"leading continuation", " folded\nBEGIN:VCALENDAR\nEND:VCALENDAR\n", "continuation line"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseICS("calendar.ics", []byte(tc.body), testLoc(t))
			if err == nil {
				t.Fatalf("want an error for %q", tc.body)
			}
			var ve *model.ValidationError
			if !errors.As(err, &ve) {
				t.Fatalf("want *model.ValidationError, got %T: %v", err, err)
			}
			if !strings.Contains(err.Error(), tc.substr) {
				t.Errorf("error %q does not mention %q", err.Error(), tc.substr)
			}
		})
	}
}

func TestParseICSDuration(t *testing.T) {
	ok := []struct {
		in   string
		want time.Duration
	}{
		{"PT45M", 45 * time.Minute},
		{"PT1H30M", 90 * time.Minute},
		{"P1D", 24 * time.Hour},
		{"P1W", 7 * 24 * time.Hour},
		{"PT30S", 30 * time.Second},
		{"-PT15M", -15 * time.Minute},
	}
	for _, tc := range ok {
		got, err := parseICSDuration(tc.in)
		if err != nil {
			t.Errorf("parseICSDuration(%q): %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("parseICSDuration(%q) = %s, want %s", tc.in, got, tc.want)
		}
	}

	bad := []string{"", "45M", "PT", "P1M", "PT1X", "PT1H2", "PTTM"}
	for _, in := range bad {
		if _, err := parseICSDuration(in); err == nil {
			t.Errorf("parseICSDuration(%q) accepted a duration it should reject", in)
		}
	}
}

// CRLF is what a real calendar exports; LF is what an editor saves. Neither is
// a corpus error.
func TestParseICSAcceptsCRLF(t *testing.T) {
	loc := testLoc(t)
	lf := readFixture(t, "corpus/calendar.ics")
	crlf := []byte(strings.ReplaceAll(string(lf), "\n", "\r\n"))

	a, err := parseICS("calendar.ics", lf, loc)
	if err != nil {
		t.Fatalf("LF: %v", err)
	}
	b, err := parseICS("calendar.ics", crlf, loc)
	if err != nil {
		t.Fatalf("CRLF: %v", err)
	}
	if len(a) != len(b) {
		t.Fatalf("LF parsed %d events, CRLF parsed %d", len(a), len(b))
	}
	for i := range a {
		if a[i].UID != b[i].UID || !a[i].Start.Equal(b[i].Start) || a[i].Summary != b[i].Summary {
			t.Fatalf("event %d differs between LF and CRLF: %+v vs %+v", i, a[i], b[i])
		}
	}
}
