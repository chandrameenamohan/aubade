package localfs

import (
	"testing"
	"time"
)

func TestParseNoteWithFrontMatter(t *testing.T) {
	loc := testLoc(t)
	n, err := parseNote("notes/board-update-cadence.md", readFixture(t, "corpus/notes/board-update-cadence.md"), loc)
	if err != nil {
		t.Fatalf("parseNote: %v", err)
	}

	if n.Path != "notes/board-update-cadence.md" {
		t.Errorf("path = %q, want the corpus-relative path used in citations", n.Path)
	}
	if n.Title != "Board update cadence" {
		t.Errorf("title = %q, want the front-matter title", n.Title)
	}
	if want := time.Date(2026, 8, 24, 0, 0, 0, 0, loc); !n.Date.Equal(want) {
		t.Errorf("date = %s, want %s read in the corpus zone", n.Date, want)
	}
	if !n.HasDate() {
		t.Errorf("HasDate() = false on a dated note")
	}
	if len(n.Tags) != 2 || n.Tags[0] != "board" {
		t.Errorf("tags = %v, want [board cadence] from the bracketed list", n.Tags)
	}
	if len(n.Attendees) != 2 || n.Attendees[1] != "Diane Okafor" {
		t.Errorf("attendees = %v, want the comma-separated list", n.Attendees)
	}
	// An unrecognised key is kept, not rejected: a newer generator writing a
	// new key must not break every reader on the same day.
	if n.Meta["source"] != "quarterly sync" {
		t.Errorf("meta = %v, want the unknown key preserved", n.Meta)
	}
	if got := n.Body; got == "" || got[0] != '#' {
		t.Errorf("body = %q, want the markdown after the front matter", got)
	}
}

// A note with no front matter is still a note. Its title comes from the H1, and
// it simply has no date — which the honesty layer reports rather than guesses.
func TestParseNoteWithoutFrontMatter(t *testing.T) {
	n, err := parseNote("notes/sprint-aug-week4.md", readFixture(t, "corpus/notes/sprint-aug-week4.md"), testLoc(t))
	if err != nil {
		t.Fatalf("parseNote: %v", err)
	}
	if n.Title != "Sprint — August week 4" {
		t.Errorf("title = %q, want the first H1", n.Title)
	}
	if n.HasDate() {
		t.Errorf("date = %s, want the zero time: the note carries none", n.Date)
	}
}

// With neither front matter nor a heading, the filename is the only honest
// title left.
func TestParseNoteFallsBackToFilename(t *testing.T) {
	n, err := parseNote("notes/loose-thoughts.md", []byte("just some prose\n"), testLoc(t))
	if err != nil {
		t.Fatalf("parseNote: %v", err)
	}
	if n.Title != "loose-thoughts" {
		t.Errorf("title = %q, want the filename stem", n.Title)
	}
}

func TestParseNoteRejectsMalformed(t *testing.T) {
	cases := []struct {
		name   string
		file   string
		line   int
		substr string
	}{
		{"unterminated front matter", "malformed/note-unterminated-frontmatter.md", 1, "never closed"},
		{"unparseable date", "malformed/note-bad-date.md", 3, "front matter date"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseNote("notes/x.md", readFixture(t, tc.file), testLoc(t))
			wantValidationError(t, err, tc.line, tc.substr)
		})
	}
}

// A front-matter line that is not "key: value" is malformed, not decorative.
func TestParseNoteRejectsJunkFrontMatterLine(t *testing.T) {
	_, err := parseNote("notes/x.md", []byte("---\ntitle: ok\njust some prose\n---\n"), testLoc(t))
	wantValidationError(t, err, 3, "not \"key: value\"")
}
