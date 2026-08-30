package model

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestEmailValidate(t *testing.T) {
	good := Email{
		ID:       "e-1",
		ThreadID: "t-1",
		TS:       time.Now(),
		From:     Person{Name: "Marcus Webb", Email: "marcus@inflectionpoint.vc"},
		To:       []Person{{Email: "avery@tessera.io"}},
	}
	if err := good.Validate(); err != nil {
		t.Fatalf("valid email rejected: %v", err)
	}

	cases := []struct {
		name   string
		mutate func(*Email)
		want   string
	}{
		{"no id", func(e *Email) { e.ID = " " }, `"id" is empty`},
		{"no thread", func(e *Email) { e.ThreadID = "" }, `"thread_id" is empty`},
		{"no ts", func(e *Email) { e.TS = time.Time{} }, `"ts" is missing`},
		{"no sender", func(e *Email) { e.From.Email = "" }, "address is empty"},
		{"sender is not an address", func(e *Email) { e.From.Email = "marcus" }, "not an email address"},
		{"no recipients", func(e *Email) { e.To = nil }, `"to" is empty`},
		{"bad recipient", func(e *Email) { e.To = []Person{{Email: "@x"}} }, "not an email address"},
		{"bad cc", func(e *Email) { e.CC = []Person{{Email: "nope"}} }, "not an email address"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := good
			tc.mutate(&e)
			err := e.Validate()
			if err == nil {
				t.Fatalf("accepted an invalid email")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

// The JSON shape is a contract other beads and the eval harness bind to, so it
// is asserted rather than assumed. `to` and `cc` keep their keys even when
// empty; only in_reply_to and labels are optional.
func TestEmailJSONContract(t *testing.T) {
	ts, err := time.Parse(time.RFC3339, "2026-08-27T16:42:00-07:00")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	e := Email{
		ID:       "e-1",
		ThreadID: "t-1",
		TS:       ts,
		From:     Person{Name: "Marcus Webb", Email: "marcus@inflectionpoint.vc"},
		To:       []Person{{Name: "Avery Chen", Email: "avery@tessera.io"}},
		Subject:  "cap table",
		Body:     "send it",
	}
	raw, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var generic map[string]any
	if err := json.Unmarshal(raw, &generic); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, key := range []string{"id", "thread_id", "ts", "from", "to", "cc", "subject", "body"} {
		if _, ok := generic[key]; !ok {
			t.Errorf("marshalled email has no %q key: %s", key, raw)
		}
	}
	for _, key := range []string{"in_reply_to", "labels"} {
		if _, ok := generic[key]; ok {
			t.Errorf("empty optional field %q was marshalled: %s", key, raw)
		}
	}
	if generic["ts"] != "2026-08-27T16:42:00-07:00" {
		t.Errorf("ts = %v, want RFC3339 with the original zone", generic["ts"])
	}

	var back Email
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("round trip: %v", err)
	}
	if !back.TS.Equal(e.TS) || back.ID != e.ID || back.From != e.From {
		t.Errorf("round trip lost data: %+v", back)
	}
}

// A ts that is not RFC3339 must fail at the decoder rather than land as a zero
// time that later reads as "1 January year 1".
func TestEmailRejectsNonRFC3339Timestamp(t *testing.T) {
	var e Email
	if err := json.Unmarshal([]byte(`{"id":"e-1","ts":"27 Aug 2026 5pm"}`), &e); err == nil {
		t.Fatalf("accepted a non-RFC3339 ts")
	}
}

func TestCalEventAttendeeHelpers(t *testing.T) {
	e := CalEvent{
		UID:   "ev-1",
		Start: time.Date(2026, 8, 30, 9, 0, 0, 0, time.UTC),
		End:   time.Date(2026, 8, 30, 9, 30, 0, 0, time.UTC),
		Attendees: []Attendee{
			{Person: Person{Email: "Avery@Tessera.io"}, PartStat: PartStatDeclined},
			{Person: Person{Email: "jordan@tessera.io"}, PartStat: PartStatAccepted},
		},
	}
	if e.Duration() != 30*time.Minute {
		t.Errorf("duration = %s, want 30m", e.Duration())
	}
	if !e.DeclinedBy("avery@tessera.io") {
		t.Errorf("DeclinedBy missed a decline that differs only in case")
	}
	if e.DeclinedBy("jordan@tessera.io") {
		t.Errorf("an accepted attendee reported as a decline")
	}
	if got := e.PartStatOf(" "); got != "" {
		t.Errorf("PartStatOf(blank) = %q, want empty", got)
	}
}

// fakeSource is a DataSource whose every method is scripted, so LoadCorpus's
// missing-vs-malformed policy can be tested without touching a disk.
type fakeSource struct {
	emailErr    error
	calendarErr error
	profileErr  error
}

func (f fakeSource) Name() string { return "fake" }

func (f fakeSource) Emails(context.Context) ([]Email, error) {
	return []Email{{ID: "e-1"}}, f.emailErr
}

func (f fakeSource) Events(context.Context) ([]CalEvent, error) {
	return nil, f.calendarErr
}

func (f fakeSource) Notes(context.Context) ([]Note, error) { return nil, nil }

func (f fakeSource) Tasks(context.Context) ([]Task, error) { return nil, nil }

func (f fakeSource) Profile(context.Context) (*Profile, error) {
	return &Profile{Owner: Person{Name: "Avery Chen"}}, f.profileErr
}

func TestLoadCorpusPolicy(t *testing.T) {
	t.Run("missing sources are recorded", func(t *testing.T) {
		src := fakeSource{
			calendarErr: &MissingSourceError{Source: "calendar", Path: "data/calendar.ics"},
			profileErr:  &MissingSourceError{Source: "profile", Path: "data/profile.md"},
		}
		c, err := LoadCorpus(context.Background(), src)
		if err != nil {
			t.Fatalf("LoadCorpus: %v", err)
		}
		if !c.IsMissing("calendar") || !c.IsMissing("profile") {
			t.Errorf("missing = %+v, want calendar and profile", c.Missing)
		}
		if c.IsMissing("email") {
			t.Errorf("a source that loaded was reported missing")
		}
		if len(c.Emails) != 1 {
			t.Errorf("the sources that did load were dropped: %+v", c.Emails)
		}
		if c.Source != "fake" {
			t.Errorf("source = %q, want the provider name", c.Source)
		}
	})

	t.Run("malformed sources abort", func(t *testing.T) {
		boom := &ValidationError{Source: "email", Path: "data/inbox.jsonl", Line: 12, Msg: "malformed JSON"}
		_, err := LoadCorpus(context.Background(), fakeSource{emailErr: boom})
		if !errors.Is(err, boom) {
			t.Fatalf("LoadCorpus error = %v, want the validation error itself", err)
		}
	})

	t.Run("a nil source is a caller bug, not a panic", func(t *testing.T) {
		if _, err := LoadCorpus(context.Background(), nil); err == nil {
			t.Fatalf("LoadCorpus(nil) returned no error")
		}
	})
}

func TestValidationErrorMessage(t *testing.T) {
	err := &ValidationError{
		Source: "email",
		Path:   "data/inbox.jsonl",
		Line:   12,
		Msg:    "malformed JSON",
		Err:    errors.New("unexpected end of input"),
	}
	want := "data/inbox.jsonl:12: email: malformed JSON: unexpected end of input"
	if err.Error() != want {
		t.Errorf("Error() = %q, want %q", err.Error(), want)
	}

	// A line-less format still names the file.
	noLine := &ValidationError{Source: "profile", Path: "data/profile.md", Msg: "no people"}
	if got := noLine.Error(); got != "data/profile.md: profile: no people" {
		t.Errorf("Error() = %q", got)
	}
}

func TestMissingSourceErrorIsSentinel(t *testing.T) {
	var err error = &MissingSourceError{Source: "note", Path: "data/notes"}
	if !errors.Is(err, ErrSourceMissing) {
		t.Fatalf("MissingSourceError does not unwrap to ErrSourceMissing")
	}
	var target *MissingSourceError
	if !errors.As(err, &target) || target.Path != "data/notes" {
		t.Fatalf("errors.As lost the path: %+v", target)
	}
}

func TestLocationIsThePacificAnchor(t *testing.T) {
	loc := Location()
	if loc == nil {
		t.Fatalf("Location() = nil")
	}
	if loc.String() != DefaultTimeZone {
		t.Fatalf("Location() = %s, want %s (time/tzdata must be embedded)", loc, DefaultTimeZone)
	}
	// August is daylight time in the Bay Area: -07:00, not -08:00.
	if _, offset := time.Date(2026, 8, 30, 6, 0, 0, 0, loc).Zone(); offset != -7*3600 {
		t.Errorf("August offset = %ds, want -25200s", offset)
	}
}

func TestPersonString(t *testing.T) {
	cases := []struct {
		in   Person
		want string
	}{
		{Person{Name: "Avery Chen", Email: "avery@tessera.io"}, "Avery Chen <avery@tessera.io>"},
		{Person{Name: "Avery Chen"}, "Avery Chen"},
		{Person{Email: "avery@tessera.io"}, "avery@tessera.io"},
		{Person{}, ""},
	}
	for _, tc := range cases {
		if got := tc.in.String(); got != tc.want {
			t.Errorf("Person%+v.String() = %q, want %q", tc.in, got, tc.want)
		}
	}
}
