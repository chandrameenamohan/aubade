package extract

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/chandrameenamohan/aubade/internal/localfs"
	"github.com/chandrameenamohan/aubade/internal/model"
)

// fixtureDay is the anchor every fixture test uses: Monday 31 August 2026.
//
// A Monday on purpose. The profile's thresholds are in business days, so an
// anchor day whose weekend is behind it is the case where getting the
// arithmetic wrong is most visible: the Friday emails in the corpus are one
// business day old, not three calendar days.
const fixtureDay = "2026-08-31"

// loadFixture reads a testdata corpus and binds it to an anchor day.
func loadFixture(t *testing.T, name, today string) *Toolbox {
	t.Helper()

	loc := model.Location()
	day, err := ParseToday(today, loc)
	if err != nil {
		t.Fatalf("ParseToday(%q): %v", today, err)
	}
	corpus, err := model.LoadCorpus(context.Background(), localfs.New(filepath.Join("testdata", name)))
	if err != nil {
		t.Fatalf("LoadCorpus(%s): %v", name, err)
	}
	tb, err := New(corpus, day, loc)
	if err != nil {
		t.Fatalf("New(%s): %v", name, err)
	}
	return tb
}

// byKind indexes a signal set by extractor.
func byKind(ss model.Signals) map[string]model.Signals {
	out := map[string]model.Signals{}
	for _, s := range ss {
		out[s.Kind] = append(out[s.Kind], s)
	}
	return out
}

// findSignal returns the first signal whose id matches, and whether it exists.
func findSignal(ss model.Signals, id string) (model.Signal, bool) {
	for _, s := range ss {
		if s.ID == id {
			return s, true
		}
	}
	return model.Signal{}, false
}

// cites reports whether a signal cites the given source and ref.
func cites(s model.Signal, source model.Source, ref string) bool {
	for _, c := range s.Citations {
		if c.Source == source && c.Ref == ref {
			return true
		}
	}
	return false
}

// anyCites reports whether any signal in the set cites source:ref.
func anyCites(ss model.Signals, source model.Source, ref string) bool {
	for _, s := range ss {
		if cites(s, source, ref) {
			return true
		}
	}
	return false
}

// ts parses a fixture timestamp.
func ts(t *testing.T, v string) time.Time {
	t.Helper()
	at, err := time.Parse(time.RFC3339, v)
	if err != nil {
		t.Fatalf("bad fixture timestamp %q: %v", v, err)
	}
	return at
}

// msg builds one email for an in-memory corpus. Hand-built corpora are how the
// hard negatives are written: proving an extractor stays quiet needs a corpus
// that differs from a positive one in exactly one respect, which is easier to
// read as four lines of Go than as two testdata directories.
func msg(t *testing.T, id, thread, at, from, subject, body string) model.Email {
	t.Helper()
	return model.Email{
		ID:       id,
		ThreadID: thread,
		TS:       ts(t, at),
		From:     model.Person{Name: from, Email: from + "@example.test"},
		To:       []model.Person{{Name: "avery", Email: ownerTestAddr}},
		Subject:  subject,
		Body:     body,
	}
}

// fromOwner flips a message so the owner is the sender and to is the named
// counterparty.
func fromOwner(e model.Email, to string) model.Email {
	counterpart := e.From
	e.From = model.Person{Name: "avery", Email: ownerTestAddr}
	if to != "" {
		counterpart = model.Person{Name: to, Email: to + "@example.test"}
	}
	e.To = []model.Person{counterpart}
	return e
}

// ownerTestAddr is the owner in hand-built corpora.
const ownerTestAddr = "avery@example.test"

// corpusOf wraps emails in a corpus whose owner is ownerTestAddr and which has
// no profile — so priorities fall to the default and nothing is suppressed.
func corpusOf(emails []model.Email, tasks ...model.Task) *model.Corpus {
	return &model.Corpus{
		Source:  "test",
		Emails:  emails,
		Tasks:   tasks,
		Profile: &model.Profile{Path: "profile.md", Owner: model.Person{Name: "avery", Email: ownerTestAddr}},
	}
}

// toolboxOf binds a hand-built corpus to an anchor day.
func toolboxOf(t *testing.T, c *model.Corpus, today string) *Toolbox {
	t.Helper()
	loc := model.Location()
	day, err := ParseToday(today, loc)
	if err != nil {
		t.Fatalf("ParseToday(%q): %v", today, err)
	}
	tb, err := New(c, day, loc)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return tb
}
