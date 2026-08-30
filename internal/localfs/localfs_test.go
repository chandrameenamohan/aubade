package localfs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/chandrameenamohan/aubade/internal/model"
)

func TestLoadCorpusFromDisk(t *testing.T) {
	src := New(filepath.Join("testdata", "corpus"))
	corpus, err := model.LoadCorpus(context.Background(), src)
	if err != nil {
		t.Fatalf("LoadCorpus: %v", err)
	}

	if len(corpus.Emails) != 4 {
		t.Errorf("loaded %d emails, want 4", len(corpus.Emails))
	}
	if len(corpus.Events) != 5 {
		t.Errorf("loaded %d events, want 5", len(corpus.Events))
	}
	if len(corpus.Notes) != 2 {
		t.Errorf("loaded %d notes, want 2", len(corpus.Notes))
	}
	if len(corpus.Tasks) != 5 {
		t.Errorf("loaded %d tasks, want 5", len(corpus.Tasks))
	}
	if corpus.Profile == nil || corpus.Profile.Owner.Name != "Avery Chen" {
		t.Fatalf("profile not loaded: %+v", corpus.Profile)
	}
	if len(corpus.Missing) != 0 {
		t.Errorf("complete corpus reported missing sources: %+v", corpus.Missing)
	}

	// Notes come back in lexical path order, and their paths are
	// corpus-relative — a citation must mean the same thing on every machine.
	if corpus.Notes[0].Path != "notes/board-update-cadence.md" || corpus.Notes[1].Path != "notes/sprint-aug-week4.md" {
		t.Errorf("note paths = %q, %q; want corpus-relative and lexically ordered",
			corpus.Notes[0].Path, corpus.Notes[1].Path)
	}
	if corpus.Source != "localfs:testdata/corpus" {
		t.Errorf("source = %q, want localfs:testdata/corpus", corpus.Source)
	}
}

// The same bytes must produce the same corpus every time: the entire eval rests
// on it.
func TestLoadCorpusIsDeterministic(t *testing.T) {
	src := New(filepath.Join("testdata", "corpus"))
	a, err := model.LoadCorpus(context.Background(), src)
	if err != nil {
		t.Fatalf("first load: %v", err)
	}
	b, err := model.LoadCorpus(context.Background(), src)
	if err != nil {
		t.Fatalf("second load: %v", err)
	}
	for i := range a.Emails {
		if a.Emails[i].ID != b.Emails[i].ID {
			t.Fatalf("email order differs between loads at %d", i)
		}
	}
	for i := range a.Events {
		if a.Events[i].UID != b.Events[i].UID {
			t.Fatalf("event order differs between loads at %d", i)
		}
	}
	for i := range a.Notes {
		if a.Notes[i].Path != b.Notes[i].Path {
			t.Fatalf("note order differs between loads at %d", i)
		}
	}
}

// A missing source is survivable and reported. This is the honesty layer's
// input: the digest opens with a banner naming what was not there, instead of
// quietly thinning (HLD §7).
func TestMissingSourcesAreReportedNotFatal(t *testing.T) {
	dir := t.TempDir()
	src := New(dir)

	corpus, err := model.LoadCorpus(context.Background(), src)
	if err != nil {
		t.Fatalf("LoadCorpus over an empty directory failed instead of reporting: %v", err)
	}
	for _, want := range []string{"email", "calendar", "note", "task", "profile"} {
		if !corpus.IsMissing(want) {
			t.Errorf("%s not reported missing: %+v", want, corpus.Missing)
		}
	}
	if len(corpus.Emails) != 0 || corpus.Profile != nil {
		t.Errorf("empty corpus came back non-empty: %+v", corpus)
	}

	// And each per-source call says so in a way callers can test for.
	if _, err := src.Emails(context.Background()); !errors.Is(err, model.ErrSourceMissing) {
		t.Errorf("Emails() error = %v, want ErrSourceMissing", err)
	}
	var missing *model.MissingSourceError
	if _, err := src.Profile(context.Background()); !errors.As(err, &missing) {
		t.Errorf("Profile() error = %v, want *model.MissingSourceError", err)
	}
}

// A malformed source is fatal, and LoadCorpus stops rather than handing back a
// half-corpus that looks complete.
func TestMalformedSourceIsFatal(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, InboxFile), string(readFixture(t, "malformed/inbox-duplicate-id.jsonl")))

	if _, err := model.LoadCorpus(context.Background(), New(dir)); err == nil {
		t.Fatalf("LoadCorpus accepted a malformed inbox")
	} else {
		var ve *model.ValidationError
		if !errors.As(err, &ve) {
			t.Fatalf("error = %T (%v), want *model.ValidationError", err, err)
		}
	}
}

// A notes directory that exists but holds nothing answered the question — it
// said "nothing" — so it is not a missing source.
func TestEmptyNotesDirIsNotMissing(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, NotesDir), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	notes, err := New(dir).Notes(context.Background())
	if err != nil {
		t.Fatalf("Notes(): %v", err)
	}
	if len(notes) != 0 {
		t.Fatalf("notes = %+v, want none", notes)
	}
}

// Non-markdown files in notes/ are not notes.
func TestNotesIgnoresNonMarkdown(t *testing.T) {
	dir := t.TempDir()
	notesDir := filepath.Join(dir, NotesDir)
	if err := os.Mkdir(notesDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeFile(t, filepath.Join(notesDir, "real.md"), "# Real\n")
	writeFile(t, filepath.Join(notesDir, "notes.txt"), "not a note\n")

	notes, err := New(dir).Notes(context.Background())
	if err != nil {
		t.Fatalf("Notes(): %v", err)
	}
	if len(notes) != 1 || notes[0].Title != "Real" {
		t.Fatalf("notes = %+v, want just the markdown one", notes)
	}
}

// A cancelled context stops a load rather than reading the whole corpus anyway.
func TestCancelledContextStopsTheLoad(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := model.LoadCorpus(ctx, New(filepath.Join("testdata", "corpus"))); !errors.Is(err, context.Canceled) {
		t.Fatalf("LoadCorpus error = %v, want context.Canceled", err)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
