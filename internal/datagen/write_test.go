package datagen

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chandrameenamohan/aubade/internal/localfs"
	"github.com/chandrameenamohan/aubade/internal/model"
)

// writeCorpus generates and writes a corpus into a fresh directory.
func writeCorpus(t *testing.T, seed int64) string {
	t.Helper()
	plan, err := Generate(Config{Seed: seed, Today: mustDay(t, pinnedDay)})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	dir := t.TempDir()
	if err := Write(dir, plan); err != nil {
		t.Fatalf("Write: %v", err)
	}
	return dir
}

// "Same seed ⇒ byte-identical output" (SPEC §1) is a claim about files, so it is
// tested on files: generate twice, write twice, compare every byte of every
// path. A committed golden digest means nothing without it.
func TestWriteIsByteIdenticalForTheSameSeed(t *testing.T) {
	first, second := writeCorpus(t, pinnedSeed), writeCorpus(t, pinnedSeed)

	a, b := treeOf(t, first), treeOf(t, second)
	if len(a) != len(b) {
		t.Fatalf("run one wrote %d files, run two wrote %d", len(a), len(b))
	}
	for path, want := range a {
		got, ok := b[path]
		if !ok {
			t.Errorf("run two did not write %s", path)
			continue
		}
		if got != want {
			t.Errorf("%s differs between two runs with the same seed", path)
		}
	}
}

// A different seed has to produce a different corpus, or "seeded" is decoration.
func TestWriteDiffersForADifferentSeed(t *testing.T) {
	a := treeOf(t, writeCorpus(t, pinnedSeed))
	b := treeOf(t, writeCorpus(t, pinnedSeed+1))

	if a[localfs.InboxFile] == b[localfs.InboxFile] {
		t.Error("two seeds produced the same inbox")
	}
	if a[localfs.ProfileFile] != b[localfs.ProfileFile] {
		t.Error("the seed changed profile.md; the user's own document is not generated")
	}
	if a[TrapsFile] != b[TrapsFile] {
		t.Error("the seed changed the answer key")
	}
}

// The generator writes what the provider reads. Anything else and the corpus
// only exists in our own head: this loads the written files back through the
// real LocalFS provider and checks the whole plan survived the round trip.
func TestWrittenCorpusLoadsThroughLocalFS(t *testing.T) {
	plan, err := Generate(Config{Seed: pinnedSeed, Today: mustDay(t, pinnedDay)})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	dir := t.TempDir()
	if err := Write(dir, plan); err != nil {
		t.Fatalf("Write: %v", err)
	}

	corpus, err := model.LoadCorpus(context.Background(), localfs.New(dir))
	if err != nil {
		t.Fatalf("LoadCorpus: %v", err)
	}
	if len(corpus.Missing) != 0 {
		t.Errorf("the written corpus is missing sources: %+v", corpus.Missing)
	}

	if len(corpus.Emails) != len(plan.Emails) {
		t.Errorf("loaded %d emails, wrote %d", len(corpus.Emails), len(plan.Emails))
	}
	for i := range plan.Emails {
		want, got := plan.Emails[i], corpus.Emails[i]
		if got.ID != want.ID || got.ThreadID != want.ThreadID || got.Subject != want.Subject ||
			got.Body != want.Body || got.InReplyTo != want.InReplyTo || !got.TS.Equal(want.TS) {
			t.Fatalf("email %d did not survive the round trip:\nwrote %+v\nread  %+v", i, want, got)
		}
	}

	if len(corpus.Events) != len(plan.Events) {
		t.Fatalf("loaded %d events, wrote %d", len(corpus.Events), len(plan.Events))
	}
	for i := range plan.Events {
		want, got := plan.Events[i], corpus.Events[i]
		if got.UID != want.UID || got.Summary != want.Summary || got.Status != want.Status ||
			!got.Start.Equal(want.Start) || !got.End.Equal(want.End) || got.AllDay != want.AllDay {
			t.Fatalf("event %s did not survive the round trip:\nwrote %+v\nread  %+v", want.UID, want, got)
		}
		if got.PartStatOf(Avery.Email) != want.PartStatOf(Avery.Email) {
			t.Errorf("event %s: RSVP read back as %q, wrote %q",
				want.UID, got.PartStatOf(Avery.Email), want.PartStatOf(Avery.Email))
		}
	}

	if len(corpus.Notes) != len(plan.Notes) {
		t.Errorf("loaded %d notes, wrote %d", len(corpus.Notes), len(plan.Notes))
	}
	for i := range corpus.Notes {
		want, got := plan.Notes[i], corpus.Notes[i]
		if got.Path != want.Path || got.Title != want.Title || got.HasDate() != want.HasDate() {
			t.Errorf("note %s did not survive the round trip: %+v", want.Path, got)
		}
	}

	if len(corpus.Tasks) != len(plan.Tasks) {
		t.Errorf("loaded %d tasks, wrote %d", len(corpus.Tasks), len(plan.Tasks))
	}
	for i := range corpus.Tasks {
		want, got := plan.Tasks[i], corpus.Tasks[i]
		if got.ID != want.ID || got.Title != want.Title || got.Done != want.Done || !got.Due.Equal(want.Due) {
			t.Errorf("task %s did not survive the round trip: %+v", want.ID, got)
		}
	}
}

// Every planted_ref in the answer key must resolve against the corpus as it was
// *written*, not merely as it was planned. A key that only resolves in memory
// grades nothing on disk.
func TestWrittenTrapsResolveAgainstTheWrittenCorpus(t *testing.T) {
	plan, err := Generate(Config{Seed: pinnedSeed, Today: mustDay(t, pinnedDay)})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	dir := t.TempDir()
	if err := Write(dir, plan); err != nil {
		t.Fatalf("Write: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, TrapsFile))
	if err != nil {
		t.Fatalf("read %s: %v", TrapsFile, err)
	}
	var traps Traps
	if err := json.Unmarshal(raw, &traps); err != nil {
		t.Fatalf("%s is not valid JSON: %v", TrapsFile, err)
	}
	if err := traps.Validate(); err != nil {
		t.Fatalf("the written answer key is invalid: %v", err)
	}

	corpus, err := model.LoadCorpus(context.Background(), localfs.New(dir))
	if err != nil {
		t.Fatalf("LoadCorpus: %v", err)
	}
	loaded := &Plan{Emails: corpus.Emails, Events: corpus.Events, Notes: corpus.Notes, Tasks: corpus.Tasks}
	for _, trap := range traps {
		for _, ref := range trap.PlantedRefs {
			if !loaded.Resolve(ref) {
				t.Errorf("trap %s: planted_ref %s:%s resolves to nothing in the written corpus",
					trap.ID, ref.Source, ref.Ref)
			}
		}
	}
}

// profile.md is the user's document, not ours. It is written verbatim from the
// assignment's appendix, and the engine must be able to read the rules it is
// graded on out of it.
func TestProfileIsTheVerbatimAppendix(t *testing.T) {
	dir := writeCorpus(t, pinnedSeed)
	raw, err := os.ReadFile(filepath.Join(dir, localfs.ProfileFile))
	if err != nil {
		t.Fatalf("read profile.md: %v", err)
	}
	if string(raw) != Profile() {
		t.Error("the written profile is not the embedded one")
	}

	// Lines the extractors key on, quoted exactly as the appendix writes them.
	for _, line := range []string{
		"# Avery Chen — Profile",
		"I block 9-11am Tue/Thu for deep work.",
		"- **Sam Park** (partner) — anything from Sam is P0. Personal.",
		"unless three+ from the same firm in a week, then surface as a pattern, not as",
		"- Newsletters. Even the good ones. Even Stratechery. I'll read them when I want to.",
		"three business days, not three weeks.",
		"- Short. Lowercase greetings or none at all. Sign off with \"Avery\" or nothing.",
		"- For Sam: don't draft. Surface and let me write.",
		"- If the inbox data is older than 24 hours, say so.",
	} {
		if !strings.Contains(string(raw), line) {
			t.Errorf("profile.md is missing the appendix line %q", line)
		}
	}

	profile, err := localfs.New(dir).Profile(context.Background())
	if err != nil {
		t.Fatalf("the written profile does not parse: %v", err)
	}
	if profile.Owner.Name != "Avery Chen" {
		t.Errorf("profile owner = %q, want Avery Chen", profile.Owner.Name)
	}
	for name, section := range map[string][]model.Rule{
		"suppression": profile.Suppressions,
		"tone":        profile.ToneRules,
		"honesty":     profile.HonestyRules,
		"what-I-miss": profile.MissRules,
	} {
		if len(section) == 0 {
			t.Errorf("the written profile parsed with no %s rules", name)
		}
	}
	if len(profile.People) != 9 {
		t.Errorf("profile lists %d people, want the appendix's nine entries", len(profile.People))
	}
}

// calendar.ics claims to be RFC 5545, so the shape the standard actually
// mandates is checked: CRLF terminators, folded content lines, and no physical
// line longer than 75 octets.
func TestCalendarIsWellFormedICS(t *testing.T) {
	dir := writeCorpus(t, pinnedSeed)
	raw, err := os.ReadFile(filepath.Join(dir, localfs.CalendarFile))
	if err != nil {
		t.Fatalf("read calendar.ics: %v", err)
	}
	text := string(raw)

	if !strings.HasPrefix(text, "BEGIN:VCALENDAR\r\n") {
		t.Error("calendar.ics does not open with a CRLF-terminated BEGIN:VCALENDAR")
	}
	if !strings.HasSuffix(text, "END:VCALENDAR\r\n") {
		t.Error("calendar.ics does not close with END:VCALENDAR")
	}
	if strings.Contains(strings.ReplaceAll(text, "\r\n", ""), "\n") {
		t.Error("calendar.ics contains a bare LF; RFC 5545 lines end with CRLF")
	}
	if !strings.Contains(text, "BEGIN:VTIMEZONE") {
		t.Error("calendar.ics references a TZID with no VTIMEZONE to define it")
	}

	folded := 0
	for i, line := range strings.Split(strings.TrimSuffix(text, "\r\n"), "\r\n") {
		if len(line) > 75 {
			t.Errorf("line %d is %d octets, over the 75-octet fold limit: %q", i+1, len(line), line)
		}
		if strings.HasPrefix(line, " ") {
			folded++
		}
	}
	if folded == 0 {
		t.Error("nothing in calendar.ics is folded; the folding path is untested by this corpus")
	}
}

// treeOf reads a corpus directory into path -> contents, with slash-separated
// relative paths so the map is comparable across runs and platforms.
func treeOf(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		out[filepath.ToSlash(rel)] = string(data)
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}
	return out
}
