package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chandrameenamohan/aubade/internal/datagen"
	"github.com/chandrameenamohan/aubade/internal/localfs"
)

// generateInto runs the real command into a directory and returns its output.
func generateInto(t *testing.T, dir string, extra ...string) string {
	t.Helper()
	args := append([]string{"generate", "--seed", "42", "--today", "2026-08-30", "--out", dir}, extra...)
	out, err := run(NewLabCmd(), args...)
	if err != nil {
		t.Fatalf("aubade-lab %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return out
}

// The command writes every file the corpus contract names, and the receipt says
// what it wrote. Generation produces no answer, only files — so the counts are
// the output, and a run that quietly wrote no calendar has to be visible.
func TestGenerateWritesTheWholeCorpus(t *testing.T) {
	t.Setenv("AUBADE_OUTPUT", "human")
	dir := t.TempDir()
	out := generateInto(t, dir)

	for _, name := range []string{
		localfs.InboxFile, localfs.CalendarFile, localfs.TasksFile,
		localfs.ProfileFile, datagen.TrapsFile,
	} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("generate did not write %s: %v", name, err)
		}
	}
	notes, err := os.ReadDir(filepath.Join(dir, localfs.NotesDir))
	if err != nil {
		t.Fatalf("read notes/: %v", err)
	}
	if len(notes) != 10 {
		t.Errorf("notes/ holds %d files, want 10", len(notes))
	}

	for _, want := range []string{"500 emails", "traps.json", "2026-08-30", "must surface"} {
		if !strings.Contains(out, want) {
			t.Errorf("the run receipt does not mention %q:\n%s", want, out)
		}
	}
}

// An AI caller gets the receipt as JSON with no flag, the same contract every
// other command in the tree honours (SPEC §9).
func TestGenerateReceiptIsJSONForAgents(t *testing.T) {
	t.Setenv("AUBADE_OUTPUT", "json")
	out := generateInto(t, t.TempDir())

	var receipt struct {
		OK     bool           `json:"ok"`
		Seed   int            `json:"seed"`
		Today  string         `json:"today"`
		Counts map[string]int `json:"counts"`
		Traps  map[string]int `json:"traps"`
	}
	if err := json.Unmarshal([]byte(out), &receipt); err != nil {
		t.Fatalf("agent-mode receipt is not valid JSON: %v\n%s", err, out)
	}
	if !receipt.OK || receipt.Seed != 42 || receipt.Today != "2026-08-30" {
		t.Errorf("unexpected receipt header: %+v", receipt)
	}
	if receipt.Counts["emails"] != datagen.TargetEmails {
		t.Errorf("receipt reports %d emails, want %d", receipt.Counts["emails"], datagen.TargetEmails)
	}
	if receipt.Traps["must_surface"] < 12 || receipt.Traps["must_not_surface"] < 4 {
		t.Errorf("receipt reports %+v traps, want at least 12 positive and 4 negative", receipt.Traps)
	}
}

// The end-to-end version of "same seed ⇒ byte-identical output": two full runs
// of the command, compared file by file. The unit test proves the generator is
// deterministic; this proves the command is, flags and all.
func TestGenerateTwiceIsByteIdentical(t *testing.T) {
	t.Setenv("AUBADE_OUTPUT", "human")
	first, second := t.TempDir(), t.TempDir()
	generateInto(t, first)
	generateInto(t, second)

	a, b := corpusBytes(t, first), corpusBytes(t, second)
	if len(a) != len(b) {
		t.Fatalf("run one wrote %d files, run two wrote %d", len(a), len(b))
	}
	for name, want := range a {
		if b[name] != want {
			t.Errorf("%s differs between two runs with the same seed", name)
		}
	}
}

// Re-running into a directory that already holds a corpus replaces it rather
// than merging with it. A note left behind by an earlier run is a source the
// digest would read and nobody could explain.
func TestGenerateReplacesAnEarlierCorpus(t *testing.T) {
	t.Setenv("AUBADE_OUTPUT", "human")
	dir := t.TempDir()
	generateInto(t, dir)

	stale := filepath.Join(dir, localfs.NotesDir, "left-behind.md")
	if err := os.WriteFile(stale, []byte("---\ntitle: stale\n---\n\n# stale\n"), 0o644); err != nil {
		t.Fatalf("plant a stale note: %v", err)
	}
	generateInto(t, dir)

	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("a note from an earlier run survived regeneration (err=%v)", err)
	}
}

// --today is parsed by the same parser the engine uses, so a bad value is a
// clear caller error rather than a corpus anchored on the zero time.
func TestGenerateRejectsABadToday(t *testing.T) {
	_, err := run(NewLabCmd(), "generate", "--today", "not-a-date", "--out", t.TempDir())
	if err == nil {
		t.Fatal("expected an error for an unparseable --today")
	}
	if !strings.Contains(err.Error(), "not-a-date") {
		t.Errorf("error %q does not name the value that was rejected", err)
	}
}

func corpusBytes(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		rel, relErr := filepath.Rel(dir, path)
		if relErr != nil {
			return relErr
		}
		out[filepath.ToSlash(rel)] = string(data)
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}
	return out
}
