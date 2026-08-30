package datagen

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/chandrameenamohan/aubade/internal/localfs"
	"github.com/chandrameenamohan/aubade/internal/model"
)

// Inject is Write's counterpart for a corpus that already exists: Write renders
// a whole exam into a directory, Inject adds questions to one that has already
// been written.
//
// It exists for the adversarial suite, where new traps are authored against a
// corpus that has already been generated and graded. Regenerating the whole
// corpus with the new scenarios folded in would be the obvious alternative and
// it is the wrong one twice over: the surviving artifacts would be re-rendered
// rather than preserved (the per-event calendar name does not survive an ICS
// round trip — see icswrite.go), and the pinned exam that every committed
// golden is anchored to would silently become a different exam.
//
// So this appends, in the file dialects localfs reads back, and touches nothing
// that is already there. The caller is responsible for pointing it at a *copy*:
// this function will happily append to the corpus it is given, and the harness
// is the one that owes the original its immutability.
//
// Only the artifact slices and Traps of add are read. Seed and Today belong to
// the run that produced the directory, not to the delta.
func Inject(dir string, add *Plan) error {
	if add == nil {
		return fmt.Errorf("datagen: Inject called with no plan")
	}
	if err := injectEmails(filepath.Join(dir, localfs.InboxFile), add.Emails); err != nil {
		return err
	}
	if err := injectEvents(filepath.Join(dir, localfs.CalendarFile), add.Events); err != nil {
		return err
	}
	if err := injectNotes(dir, add.Notes); err != nil {
		return err
	}
	if err := injectTasks(filepath.Join(dir, localfs.TasksFile), add.Tasks); err != nil {
		return err
	}
	return injectTraps(filepath.Join(dir, TrapsFile), add.Traps)
}

// injectEmails appends JSONL records. Order in the file is not the order the
// engine reads them in — extract sorts every thread by timestamp — so appending
// is enough, and it keeps the existing lines byte-for-byte where they were.
func injectEmails(path string, emails []model.Email) error {
	if len(emails) == 0 {
		return nil
	}
	for _, e := range emails {
		if err := e.Validate(); err != nil {
			return fmt.Errorf("datagen: inject email: %w", err)
		}
	}
	lines, err := renderInbox(emails)
	if err != nil {
		return err
	}
	return appendToFile(path, lines)
}

// injectEvents splices VEVENT blocks in before the calendar's END:VCALENDAR.
//
// A VEVENT appended after that line is a component outside the VCALENDAR that
// contains it — not RFC 5545, and not something a reader is obliged to see. The
// insertion point is the *last* END:VCALENDAR because a file with several
// calendars in it ends with the one that would otherwise swallow the addition.
func injectEvents(path string, events []model.CalEvent) error {
	if len(events) == 0 {
		return nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("datagen: inject events: %w", err)
	}
	const closing = "END:VCALENDAR"
	at := bytes.LastIndex(raw, []byte(closing))
	if at < 0 {
		return fmt.Errorf("datagen: inject events: %s has no %s line to insert before", path, closing)
	}

	var b strings.Builder
	w := &icsWriter{b: &b}
	for _, ev := range events {
		w.event(ev)
	}

	out := make([]byte, 0, len(raw)+b.Len())
	out = append(out, raw[:at]...)
	out = append(out, b.String()...)
	out = append(out, raw[at:]...)
	return os.WriteFile(path, out, 0o644)
}

// injectNotes writes each note as its own file. A path that already exists is
// an error rather than an overwrite: the corpus is evidence, and silently
// replacing a note some other trap cites would make that trap ungradeable.
func injectNotes(dir string, notes []model.Note) error {
	for _, n := range notes {
		path := filepath.Join(dir, filepath.FromSlash(n.Path))
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("datagen: inject note: %s already exists in the corpus", n.Path)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return fmt.Errorf("datagen: inject note: %w", err)
		}
		if err := os.WriteFile(path, renderNote(n), 0o644); err != nil {
			return fmt.Errorf("datagen: inject note: %w", err)
		}
	}
	return nil
}

// injectTasks appends checklist lines in the dialect localfs parses.
func injectTasks(path string, tasks []model.Task) error {
	if len(tasks) == 0 {
		return nil
	}
	var b strings.Builder
	for _, t := range tasks {
		b.WriteString(renderTaskLine(t))
	}
	return appendToFile(path, []byte(b.String()))
}

// injectTraps merges the new answer-key entries into the existing key and
// rewrites it. The whole key is re-validated on the way out, so a duplicate id
// between an authored trap and a planted one fails here rather than making one
// of the two silently ungradeable.
func injectTraps(path string, add Traps) error {
	if len(add) == 0 {
		return nil
	}
	traps := Traps{}
	raw, err := os.ReadFile(path)
	switch {
	case err == nil:
		if err := json.Unmarshal(raw, &traps); err != nil {
			return fmt.Errorf("datagen: inject traps: cannot decode %s: %w", path, err)
		}
	case !os.IsNotExist(err):
		return fmt.Errorf("datagen: inject traps: %w", err)
	}

	traps = append(traps, add...)
	var buf bytes.Buffer
	if err := EncodeTraps(&buf, traps); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

// appendToFile adds data to the end of a text file, creating it if it is not
// there and terminating a final line that lacks its newline. A missing newline
// would otherwise weld the first appended record onto the last existing one,
// which in inbox.jsonl means two emails vanish and a parse error appears
// somewhere else entirely.
func appendToFile(path string, data []byte) error {
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("datagen: inject into %s: %w", path, err)
	}
	out := existing
	if len(out) > 0 && out[len(out)-1] != '\n' {
		out = append(out, '\n')
	}
	out = append(out, data...)
	if err := os.WriteFile(path, out, 0o644); err != nil {
		return fmt.Errorf("datagen: inject into %s: %w", path, err)
	}
	return nil
}
