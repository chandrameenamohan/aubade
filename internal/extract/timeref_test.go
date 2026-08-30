package extract

import (
	"testing"
	"time"

	"github.com/chandrameenamohan/aubade/internal/model"
)

// The profile writes its thresholds in business days, so the weekend must not
// age anything. These are the cases where a naive day-count is wrong.
func TestBusinessDaysBetween(t *testing.T) {
	loc := model.Location()
	cases := []struct {
		from, to string
		want     int
	}{
		{"2026-08-27T09:00:00-07:00", "2026-08-28T09:00:00-07:00", 1}, // Thu → Fri
		{"2026-08-28T17:00:00-07:00", "2026-08-31T06:00:00-07:00", 1}, // Fri evening → Mon morning
		{"2026-08-28T17:00:00-07:00", "2026-08-30T06:00:00-07:00", 0}, // Fri → Sun: no business day passed
		{"2026-08-24T09:00:00-07:00", "2026-08-31T06:00:00-07:00", 5}, // Mon → Mon
		{"2026-08-31T09:00:00-07:00", "2026-08-31T23:00:00-07:00", 0}, // same day
		{"2026-08-31T09:00:00-07:00", "2026-08-24T09:00:00-07:00", 0}, // backwards is not negative
	}
	for _, tc := range cases {
		got := businessDaysBetween(ts(t, tc.from), ts(t, tc.to), loc)
		if got != tc.want {
			t.Errorf("businessDaysBetween(%s, %s) = %d, want %d", tc.from, tc.to, got, tc.want)
		}
	}
}

// Deadlines resolve against the moment the promise was written, never against
// the anchor day. "tomorrow" in last Tuesday's email means last Wednesday.
func TestParseDueRefsResolveAgainstTheMessage(t *testing.T) {
	loc := model.Location()
	base := ts(t, "2026-08-24T10:00:00-07:00") // Monday

	cases := []struct {
		text string
		want string
	}{
		{"I'll send it tonight.", "2026-08-24 23:00"},
		{"by end of day", "2026-08-24 17:00"},
		{"tomorrow works", "2026-08-25 17:00"},
		{"sending the update this week", "2026-08-28 17:00"},
		{"you'll have it next week", "2026-09-04 17:00"},
		{"by Thursday", "2026-08-27 17:00"},
		{"the Sept 4 rollout", "2026-09-04 17:00"},
		{"due 2026-09-15", "2026-09-15 17:00"},
		{"in 3 days", "2026-08-27 17:00"},
		{"by 5pm", "2026-08-24 17:00"},
	}
	for _, tc := range cases {
		refs := ParseDueRefs(tc.text, base, loc)
		if len(refs) == 0 {
			t.Errorf("ParseDueRefs(%q) found no deadline", tc.text)
			continue
		}
		if got := refs[0].Deadline.In(loc).Format("2006-01-02 15:04"); got != tc.want {
			t.Errorf("ParseDueRefs(%q) = %s, want %s", tc.text, got, tc.want)
		}
	}

	if refs := ParseDueRefs("thanks for the note", base, loc); len(refs) != 0 {
		t.Errorf("a sentence with no deadline should produce none, got %+v", refs)
	}
}

// When a sentence names two deadlines, the operative one is the earlier: the
// thing you are late on is the thing due first.
func TestParseDueRefsAreOrderedEarliestFirst(t *testing.T) {
	loc := model.Location()
	refs := ParseDueRefs("draft tonight and the full model by Friday", ts(t, "2026-08-24T10:00:00-07:00"), loc)
	if len(refs) < 2 {
		t.Fatalf("expected two deadlines, got %+v", refs)
	}
	if !refs[0].Deadline.Before(refs[1].Deadline) {
		t.Errorf("deadlines not ordered earliest first: %+v", refs)
	}
}

// A bare month/day resolves to the nearest such date, not blindly to this year.
func TestMonthDayRollsToTheNearestYear(t *testing.T) {
	loc := model.Location()
	december := ts(t, "2026-12-20T10:00:00-08:00")
	refs := ParseDueRefs("let's aim for Jan 8", december, loc)
	if len(refs) == 0 {
		t.Fatal("no deadline found")
	}
	if got := refs[0].Deadline.In(loc).Format("2006-01-02"); got != "2027-01-08" {
		t.Errorf("resolved to %s, want 2027-01-08", got)
	}
}

func TestBusinessDayPhrase(t *testing.T) {
	cases := map[int]string{0: "less than a business day", 1: "1 business day", 4: "4 business days"}
	for n, want := range cases {
		if got := businessDayPhrase(n); got != want {
			t.Errorf("businessDayPhrase(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestIsBusinessDay(t *testing.T) {
	loc := model.Location()
	for _, tc := range []struct {
		day  string
		want bool
	}{
		{"2026-08-28", true},  // Friday
		{"2026-08-29", false}, // Saturday
		{"2026-08-30", false}, // Sunday
		{"2026-08-31", true},  // Monday
	} {
		d, err := time.ParseInLocation("2006-01-02", tc.day, loc)
		if err != nil {
			t.Fatal(err)
		}
		if got := isBusinessDay(d); got != tc.want {
			t.Errorf("isBusinessDay(%s) = %v, want %v", tc.day, got, tc.want)
		}
	}
}
