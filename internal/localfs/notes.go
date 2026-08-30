package localfs

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/chandrameenamohan/aubade/internal/model"
)

// Notes are markdown with optional front matter, delimited by a "---" line at
// the very top and a matching "---" below it:
//
//	---
//	title: Q2 planning
//	date: 2026-08-27
//	attendees: Priya Iyer, Jordan Liu
//	tags: planning, q2
//	---
//	# Q2 planning
//	…
//
// Recognised keys are title, date, attendees and tags; anything else is kept in
// Note.Meta rather than rejected, so a generator that starts writing a new key
// does not break every reader on the same day. What *is* rejected: front matter
// that never closes, a line in it that is not "key: value", and a date that
// does not parse — those are malformed, not unfamiliar.
//
// A note's date comes from its front matter only, never from the file's mtime:
// mtime is a fact about the clone, and a staleness banner computed from it
// would say something different on every machine.

// parseNote reads one markdown note. path is corpus-relative, and becomes the
// note's citation ref.
func parseNote(path string, data []byte, loc *time.Location) (model.Note, error) {
	fail := func(line int, msg string, err error) (model.Note, error) {
		return model.Note{}, &model.ValidationError{
			Source: string(model.SourceNote), Path: path, Line: line, Msg: msg, Err: err,
		}
	}

	lines := splitLines(string(data))
	note := model.Note{Path: path}

	body := lines
	if len(lines) > 0 && strings.TrimSpace(lines[0]) == "---" {
		end := -1
		for i := 1; i < len(lines); i++ {
			if strings.TrimSpace(lines[i]) == "---" {
				end = i
				break
			}
		}
		if end < 0 {
			return fail(1, "front matter opened with --- but never closed", nil)
		}
		for i := 1; i < end; i++ {
			raw := strings.TrimSpace(lines[i])
			if raw == "" || strings.HasPrefix(raw, "#") {
				continue
			}
			key, value, ok := strings.Cut(raw, ":")
			if !ok {
				return fail(i+1, fmt.Sprintf("front matter line %q is not \"key: value\"", raw), nil)
			}
			key = strings.ToLower(strings.TrimSpace(key))
			value = strings.TrimSpace(value)
			switch key {
			case "title":
				note.Title = value
			case "date":
				t, err := parseMarkdownDate(value, loc)
				if err != nil {
					return fail(i+1, fmt.Sprintf("front matter date %q", value), err)
				}
				note.Date = t
			case "tags":
				note.Tags = splitList(value)
			case "attendees":
				note.Attendees = splitList(value)
			default:
				if note.Meta == nil {
					note.Meta = map[string]string{}
				}
				note.Meta[key] = value
			}
		}
		body = lines[end+1:]
	}

	note.Body = strings.TrimLeft(strings.Join(body, "\n"), "\n")
	if note.Title == "" {
		note.Title = firstHeading(body)
	}
	if note.Title == "" {
		note.Title = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	return note, nil
}

// firstHeading returns the text of the first markdown "# " heading.
func firstHeading(lines []string) string {
	for _, l := range lines {
		if t := strings.TrimSpace(l); strings.HasPrefix(t, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(t, "# "))
		}
	}
	return ""
}

// splitList reads a comma-separated front-matter list, tolerating the bracketed
// YAML flow form ("[a, b]").
func splitList(v string) []string {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "[")
	v = strings.TrimSuffix(v, "]")
	var out []string
	for _, part := range strings.Split(v, ",") {
		if p := strings.TrimSpace(strings.Trim(strings.TrimSpace(part), `"'`)); p != "" {
			out = append(out, p)
		}
	}
	return out
}
