package datagen

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/chandrameenamohan/aubade/internal/localfs"
	"github.com/chandrameenamohan/aubade/internal/model"
)

// The writer turns a Plan into the corpus on disk. It is the other half of
// localfs: that package defines what a corpus is and how it is read, so the
// file names come from there rather than being spelled out again here — a
// generator and a reader that disagree about whether the calendar is
// "calendar.ics" fail at load, which is a bad way to find out.
//
// Everything is rendered into memory and written once. The corpus is a few
// hundred kilobytes, and a half-written inbox.jsonl that a reader then rejects
// with a line number is a confusing way to report a disk error.

// TrapsFile is the answer key's name inside the corpus directory. It lives with
// the data it grades: a trap that cites m-capt-02 is meaningless beside a
// different corpus, so the two travel together.
const TrapsFile = "traps.json"

// Write renders the plan into a corpus directory, creating it if needed.
func Write(dir string, plan *Plan) error {
	if plan == nil {
		return fmt.Errorf("datagen: Write called with no plan")
	}
	notesDir := filepath.Join(dir, localfs.NotesDir)
	if err := os.MkdirAll(notesDir, 0o755); err != nil {
		return fmt.Errorf("datagen: create %s: %w", notesDir, err)
	}
	// A note that a previous run wrote and this one does not is still a note as
	// far as the reader is concerned, and it would age into a source nobody can
	// explain. Clearing the directory keeps "same seed, same corpus" true of
	// the directory and not merely of the files we happen to write.
	if err := clearNotes(notesDir); err != nil {
		return err
	}

	inbox, err := renderInbox(plan.Emails)
	if err != nil {
		return err
	}
	traps, err := renderTraps(plan.Traps)
	if err != nil {
		return err
	}

	files := []struct {
		path string
		data []byte
	}{
		{filepath.Join(dir, localfs.InboxFile), inbox},
		{filepath.Join(dir, localfs.CalendarFile), renderICS(plan)},
		{filepath.Join(dir, localfs.TasksFile), renderTasks(plan.Tasks)},
		{filepath.Join(dir, localfs.ProfileFile), []byte(Profile())},
		{filepath.Join(dir, TrapsFile), traps},
	}
	for _, n := range plan.Notes {
		files = append(files, struct {
			path string
			data []byte
		}{filepath.Join(dir, filepath.FromSlash(n.Path)), renderNote(n)})
	}

	for _, f := range files {
		if err := os.WriteFile(f.path, f.data, 0o644); err != nil {
			return fmt.Errorf("datagen: write %s: %w", f.path, err)
		}
	}
	return nil
}

// clearNotes removes the markdown a previous run left behind. It touches
// nothing else in the directory, and nothing outside it.
func clearNotes(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("datagen: read %s: %w", dir, err)
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".md" {
			continue
		}
		if err := os.Remove(filepath.Join(dir, e.Name())); err != nil {
			return fmt.Errorf("datagen: remove stale note %s: %w", e.Name(), err)
		}
	}
	return nil
}

// renderInbox writes one compact JSON email per line.
//
// HTML escaping is off so a subject containing "&" survives as itself; the
// contract in SPEC is a JSON object per line, and & is a different string
// to every reader that is not a browser.
func renderInbox(emails []model.Email) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	for _, e := range emails {
		if err := enc.Encode(e); err != nil {
			return nil, fmt.Errorf("datagen: encode email %s: %w", e.ID, err)
		}
	}
	return buf.Bytes(), nil
}

func renderTraps(traps Traps) ([]byte, error) {
	var buf bytes.Buffer
	if err := EncodeTraps(&buf, traps); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// renderTasks writes tasks.md in the checklist dialect localfs parses:
// "- [ ] title (due: …) (id: …) (owner: …)".
func renderTasks(tasks []model.Task) []byte {
	var b strings.Builder
	b.WriteString("# Tasks\n\nEverything I owe someone, in one place.\n\n")
	for _, t := range tasks {
		box := " "
		if t.Done {
			box = "x"
		}
		fmt.Fprintf(&b, "- [%s] %s", box, t.Title)
		if t.HasDue() {
			fmt.Fprintf(&b, " (due: %s)", markdownDate(t.Due))
		}
		fmt.Fprintf(&b, " (id: %s)", t.ID)
		if t.Owner != "" {
			fmt.Fprintf(&b, " (owner: %s)", t.Owner)
		}
		// Map iteration order is not defined, and a corpus that reorders itself
		// between runs is not byte-identical.
		for _, k := range slices.Sorted(maps.Keys(t.Meta)) {
			fmt.Fprintf(&b, " (%s: %s)", k, t.Meta[k])
		}
		b.WriteString("\n")
	}
	return []byte(b.String())
}

// renderNote writes one note with the front matter localfs reads back.
//
// A note with no date of its own gets no date line, deliberately: that absence
// is the whole of the undated-note trap, and inventing a date here would delete
// the question.
func renderNote(n model.Note) []byte {
	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "title: %s\n", n.Title)
	if n.HasDate() {
		fmt.Fprintf(&b, "date: %s\n", n.Date.Format("2006-01-02"))
	}
	if len(n.Attendees) > 0 {
		fmt.Fprintf(&b, "attendees: %s\n", strings.Join(n.Attendees, ", "))
	}
	if len(n.Tags) > 0 {
		fmt.Fprintf(&b, "tags: [%s]\n", strings.Join(n.Tags, ", "))
	}
	for _, k := range slices.Sorted(maps.Keys(n.Meta)) {
		fmt.Fprintf(&b, "%s: %s\n", k, n.Meta[k])
	}
	b.WriteString("---\n\n")
	b.WriteString(strings.TrimRight(n.Body, "\n"))
	b.WriteString("\n")
	return []byte(b.String())
}

// markdownDate renders a date the way tasks.md and note front matter carry one:
// midnight is a plain date, anything else keeps its clock.
func markdownDate(t time.Time) string {
	if t.Hour() == 0 && t.Minute() == 0 {
		return t.Format("2006-01-02")
	}
	return t.Format("2006-01-02 15:04")
}
