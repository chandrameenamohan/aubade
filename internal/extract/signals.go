package extract

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/chandrameenamohan/aubade/internal/model"
)

// SignalsFile is the audit artefact `aubade signals` writes, relative to --out.
const SignalsFile = "signals.json"

// sourcePaths are the conventional corpus-relative filenames, keyed by source.
//
// They are what a detail line or a missing-source citation names, in preference
// to the absolute path a provider reports: a reference has to mean the same
// thing on every machine (localfs makes the same guarantee for note paths), and
// "calendar.ics" is what a reader goes looking for. The absolute path still
// appears in the detail text, where it helps whoever is debugging the run and
// misleads nobody about provenance.
var sourcePaths = map[string]string{
	string(model.SourceEmail):    "inbox.jsonl",
	string(model.SourceCalendar): "calendar.ics",
	string(model.SourceNote):     "notes/",
	string(model.SourceTask):     "tasks.md",
	"profile":                    "profile.md",
}

// ids hands out stable, unique signal ids.
//
// An id is "<kind>:<ref>" — derived from the record the signal is about, not
// from a counter — so the same corpus produces the same id on every machine and
// a trap can be written against it. A genuine collision (two commitments in one
// email) gets a "#2" suffix in discovery order rather than a random tail,
// because a random tail would break the eval's ability to name what it saw.
type ids struct {
	seen map[string]int
}

func newIDs() *ids { return &ids{seen: map[string]int{}} }

func (g *ids) next(kind string, parts ...string) string {
	base := kind
	for _, p := range parts {
		base += ":" + strings.TrimSpace(p)
	}
	g.seen[base]++
	if n := g.seen[base]; n > 1 {
		return base + "#" + strconv.Itoa(n)
	}
	return base
}

// kindRank orders the extractors for the tie-break in SortSignals. Unknown
// kinds sort after the published set rather than being dropped: an extractor
// may coin a kind before model.KnownKinds catches up.
func kindRank(kind string) int {
	if i := slices.Index(model.KnownKinds, kind); i >= 0 {
		return i
	}
	return len(model.KnownKinds)
}

// SortSignals puts a signal set into the digest's reading order: most urgent
// first, then soonest deadline, then extractor order, then id.
//
// The last key is what makes this a *total* order. Two signals can share a
// priority, a deadline and a kind; they cannot share an id, so there is exactly
// one correct output for any input and no run-to-run wobble to debug.
func SortSignals(ss model.Signals) {
	sort.SliceStable(ss, func(i, j int) bool {
		a, b := ss[i], ss[j]
		if ar, br := a.Priority.Rank(), b.Priority.Rank(); ar != br {
			return ar < br
		}
		ad, bd := deadlineKey(a), deadlineKey(b)
		if !ad.Equal(bd) {
			return ad.Before(bd)
		}
		if ar, br := kindRank(a.Kind), kindRank(b.Kind); ar != br {
			return ar < br
		}
		return a.ID < b.ID
	})
}

// deadlineKey sorts signals without a deadline after those with one.
func deadlineKey(s model.Signal) time.Time {
	if s.Deadline == nil {
		return time.Date(9999, time.January, 1, 0, 0, 0, 0, time.UTC)
	}
	return *s.Deadline
}

// WriteSignals writes a signal set to path as the signals.json contract: a JSON
// array of Signal objects, indented, with a trailing newline.
//
// It is a function rather than an inline json.Marshal because the eval harness
// reads the same file: one writer and one reader means the two cannot drift
// into disagreeing about the shape.
func WriteSignals(path string, ss model.Signals) error {
	if ss == nil {
		ss = model.Signals{}
	}
	if err := ss.Validate(); err != nil {
		return fmt.Errorf("refusing to write %s: %w", path, err)
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("cannot create %s: %w", dir, err)
		}
	}
	data, err := json.MarshalIndent(ss, "", "  ")
	if err != nil {
		return fmt.Errorf("cannot encode signals: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("cannot write %s: %w", path, err)
	}
	return nil
}

// ReadSignals reads a signals.json written by WriteSignals.
func ReadSignals(path string) (model.Signals, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", path, err)
	}
	var ss model.Signals
	if err := json.Unmarshal(data, &ss); err != nil {
		return nil, fmt.Errorf("cannot decode %s: %w", path, err)
	}
	if err := ss.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return ss, nil
}

// emailCite is the citation for one message.
func emailCite(id string) model.Citation {
	return model.Citation{Source: model.SourceEmail, Ref: id}
}

// eventCite is the citation for one calendar event.
func eventCite(uid string) model.Citation {
	return model.Citation{Source: model.SourceCalendar, Ref: uid}
}

// noteCite is the citation for one note, by its corpus-relative path.
func noteCite(path string) model.Citation {
	return model.Citation{Source: model.SourceNote, Ref: path}
}

// taskCite is the citation for one task.
func taskCite(id string) model.Citation {
	return model.Citation{Source: model.SourceTask, Ref: id}
}

// dedupeCitations keeps the first occurrence of each source/ref pair, so a
// signal that reaches the same email by two routes cites it once.
func dedupeCitations(cs []model.Citation) []model.Citation {
	out := make([]model.Citation, 0, len(cs))
	for _, c := range cs {
		if !slices.Contains(out, c) {
			out = append(out, c)
		}
	}
	return out
}

// timePtr copies t onto the heap for Signal.Deadline.
func timePtr(t time.Time) *time.Time { return &t }
