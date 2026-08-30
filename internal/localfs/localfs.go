// Package localfs is the DataSource that reads aubade's corpus off disk.
//
// It is the provider that ships in week one (HLD §4): the synthetic dataset
// `aubade-lab generate` writes, and any real export a user drops into the same
// layout. A Composio-backed Gmail/Calendar provider slots in beside it later
// without the toolbox noticing, because both hand back the same model types.
//
// Layout under the corpus root:
//
//	inbox.jsonl     one JSON email per line (SPEC binding contract)
//	calendar.ics    RFC 5545, the VEVENT subset described in ics.go
//	notes/*.md      markdown notes, optional YAML-ish front matter
//	tasks.md        a markdown checklist
//	profile.md      the user's profile (people, suppressions, tone)
//
// The parsing rule throughout is the one from the package doc of model: a
// missing file is reported and survivable, a malformed file is fatal and names
// its line. Nothing is skipped quietly. We control both ends of this format —
// the generator writes it and this reads it — so a surprise here is a bug
// somewhere, and the loudest place to find out is at load.
package localfs

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/chandrameenamohan/aubade/internal/model"
)

// The corpus layout. These names are the contract between `aubade-lab generate`
// and every reader.
const (
	InboxFile    = "inbox.jsonl"
	CalendarFile = "calendar.ics"
	NotesDir     = "notes"
	TasksFile    = "tasks.md"
	ProfileFile  = "profile.md"
)

// Source is a corpus directory on disk. The zero value is not useful; use New.
type Source struct {
	// Root is the corpus directory.
	Root string
	// Loc interprets timestamps that carry no zone of their own — floating ICS
	// times, note and task dates. Defaults to the anchor zone
	// (America/Los_Angeles); injectable so tests do not depend on the machine.
	Loc *time.Location
}

// Ensure the provider actually satisfies the interface it exists to implement.
var _ model.DataSource = (*Source)(nil)

// New returns a Source reading the corpus at root.
func New(root string) *Source {
	return &Source{Root: root, Loc: model.Location()}
}

// Name identifies the provider and its origin, e.g. "localfs:data".
func (s *Source) Name() string {
	return "localfs:" + filepath.Clean(s.Root)
}

// loc is the zone to read zoneless timestamps in.
func (s *Source) loc() *time.Location {
	if s.Loc != nil {
		return s.Loc
	}
	return model.Location()
}

// path joins a corpus-relative name onto the root.
func (s *Source) path(name string) string { return filepath.Join(s.Root, name) }

// rel renders an absolute corpus path as the slash-separated, corpus-relative
// path used in citations ("notes/q2-planning.md"). Citations must be stable
// across machines, so they can never carry an absolute path.
func (s *Source) rel(p string) string {
	r, err := filepath.Rel(s.Root, p)
	if err != nil {
		return filepath.ToSlash(p)
	}
	return filepath.ToSlash(r)
}

// readFile reads one corpus file, turning a missing file into the recoverable
// MissingSourceError the honesty layer reports on.
func readFile(source, path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil, &model.MissingSourceError{Source: source, Path: path}
	case err != nil:
		return nil, &model.ValidationError{Source: source, Path: path, Msg: "cannot read", Err: err}
	}
	return data, nil
}

// Emails parses inbox.jsonl.
func (s *Source) Emails(ctx context.Context) ([]model.Email, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path := s.path(InboxFile)
	data, err := readFile(string(model.SourceEmail), path)
	if err != nil {
		return nil, err
	}
	return parseInbox(s.rel(path), data)
}

// Events parses calendar.ics.
func (s *Source) Events(ctx context.Context) ([]model.CalEvent, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path := s.path(CalendarFile)
	data, err := readFile(string(model.SourceCalendar), path)
	if err != nil {
		return nil, err
	}
	return parseICS(s.rel(path), data, s.loc())
}

// Notes parses every .md file under notes/, in lexical path order.
//
// A notes directory that exists but holds no markdown returns an empty slice
// rather than a missing source: the directory answered, and it said "nothing".
func (s *Source) Notes(ctx context.Context) ([]model.Note, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	dir := s.path(NotesDir)
	info, err := os.Stat(dir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil, &model.MissingSourceError{Source: string(model.SourceNote), Path: dir}
	case err != nil:
		return nil, &model.ValidationError{Source: string(model.SourceNote), Path: dir, Msg: "cannot read", Err: err}
	case !info.IsDir():
		return nil, &model.ValidationError{Source: string(model.SourceNote), Path: dir, Msg: "expected a directory"}
	}

	var paths []string
	walkErr := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(p) != ".md" {
			return nil
		}
		paths = append(paths, p)
		return nil
	})
	if walkErr != nil {
		return nil, &model.ValidationError{Source: string(model.SourceNote), Path: dir, Msg: "cannot walk", Err: walkErr}
	}
	// WalkDir already visits lexically; sorting the collected paths keeps that
	// guarantee ours rather than the standard library's.
	slices.Sort(paths)

	notes := make([]model.Note, 0, len(paths))
	for _, p := range paths {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		data, err := readFile(string(model.SourceNote), p)
		if err != nil {
			return nil, err
		}
		n, err := parseNote(s.rel(p), data, s.loc())
		if err != nil {
			return nil, err
		}
		notes = append(notes, n)
	}
	return notes, nil
}

// Tasks parses tasks.md.
func (s *Source) Tasks(ctx context.Context) ([]model.Task, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path := s.path(TasksFile)
	data, err := readFile(string(model.SourceTask), path)
	if err != nil {
		return nil, err
	}
	return parseTasks(s.rel(path), data, s.loc())
}

// Profile parses profile.md.
func (s *Source) Profile(ctx context.Context) (*model.Profile, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path := s.path(ProfileFile)
	data, err := readFile(profileSource, path)
	if err != nil {
		return nil, err
	}
	return parseProfile(s.rel(path), data)
}

// mdDateLayouts are the date shapes a human writes in a markdown corpus — a
// note's front matter or a task's "(due: …)". A zone-less date is read in the
// corpus zone, because a date written in this corpus means a Pacific date.
var mdDateLayouts = []string{
	time.RFC3339,
	"2006-01-02T15:04:05",
	"2006-01-02 15:04",
	"2006-01-02",
}

// parseMarkdownDate reads one of those shapes. Callers wrap the error in a
// ValidationError naming the file and line.
func parseMarkdownDate(v string, loc *time.Location) (time.Time, error) {
	for _, layout := range mdDateLayouts {
		if t, err := time.ParseInLocation(layout, v, loc); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("want YYYY-MM-DD, YYYY-MM-DD HH:MM or RFC3339")
}

// splitLines splits text into lines, normalizing line endings first: what an
// editor saved and what a generator wrote must parse the same.
func splitLines(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	return strings.Split(text, "\n")
}
