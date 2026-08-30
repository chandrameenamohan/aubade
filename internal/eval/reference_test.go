package eval

import (
	"context"
	"flag"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/chandrameenamohan/aubade/internal/datagen"
	"github.com/chandrameenamohan/aubade/internal/extract"
	"github.com/chandrameenamohan/aubade/internal/localfs"
	"github.com/chandrameenamohan/aubade/internal/model"
)

// The reference solution (EVAL-PRINCIPLES #7).
//
// "0% pass on a task often means the task is broken, not that the agent is
// weak." The only way to tell the two apart is to ship a known-good output and
// grade it: if the reference digest catches a trap, the trap is catchable, and
// a model that misses it has missed something real. If the reference misses it,
// the exam is wrong and no amount of prompt work will fix it.
//
// This is also the end-to-end regression run in unit-test form. It generates the
// pinned corpus, composes the deterministic page from it, and grades the result
// against the answer key the generator wrote — the whole
// generate → digest → eval pipeline, in-process, with no binaries and no model.
// `scripts/e2e-regression.sh` runs the same scenario through the real binaries;
// this one runs on every `go test` and fails inside the package that broke it.

// The pinned exam. Seed and anchor day are constants because a committed
// reference page is meaningless beside a corpus that moves.
const (
	referenceSeed  = 42
	referenceToday = "2026-08-30"
)

// update rewrites the committed reference digest. A flag rather than an
// environment variable, and with its own make target, because regenerating a
// reference must be something a person decided to do and then read the diff of.
var update = flag.Bool("update", false, "rewrite the reference digest in testdata/golden")

// reference is one generated exam and the reference answer to it.
type reference struct {
	Traps     datagen.Traps
	Corpus    *model.Corpus
	Artifacts *Artifacts
	Day       time.Time
	Loc       *time.Location

	// Dir is the corpus on disk. The adversarial suite copies it, so its tests
	// need the directory and not only the loaded corpus.
	Dir string
}

// buildReference generates the pinned corpus and composes the deterministic
// page from it, exactly as `aubade digest --no-llm` does.
func buildReference(t *testing.T) reference {
	t.Helper()

	loc := model.Location()
	day, err := extract.ParseToday(referenceToday, loc)
	if err != nil {
		t.Fatalf("ParseToday: %v", err)
	}
	plan, err := datagen.Generate(datagen.Config{Seed: referenceSeed, Today: day})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	dir := t.TempDir()
	if err := datagen.Write(dir, plan); err != nil {
		t.Fatalf("write corpus: %v", err)
	}
	corpus, err := model.LoadCorpus(context.Background(), localfs.New(dir))
	if err != nil {
		t.Fatalf("load corpus: %v", err)
	}

	a, err := Compose(corpus, day, loc, "")
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	return reference{Traps: plan.Traps, Corpus: corpus, Artifacts: a, Day: day, Loc: loc, Dir: dir}
}

// The reference digest catches every planted trap and surfaces none of the
// negatives. This is the assertion that says the exam is answerable.
func TestReferenceDigestSolvesEveryTask(t *testing.T) {
	ref := buildReference(t)
	res := Grade(ref.Traps, ref.Artifacts)

	passed, total := res.Score()
	if !res.Passed() {
		for _, f := range res.Failures() {
			t.Errorf("task %s (%s, expects %s): %s", f.ID, f.Kind, f.Expected, f.Reason)
		}
		t.Fatalf("the reference digest scored %d/%d; a task the reference cannot pass is a broken task, not a weak agent", passed, total)
	}

	positives, negatives := 0, 0
	for _, trap := range ref.Traps {
		if trap.MustSurface {
			positives++
		} else {
			negatives++
		}
	}
	if positives < 12 || negatives < 4 {
		t.Fatalf("graded %d positive and %d negative tasks; SPEC §1 asks for at least 12 and 4", positives, negatives)
	}
}

// Every citation on the reference page resolves to a citation in the fact base
// it was composed from — the same check the harness runs over an agentic trial,
// run here against the composer that cannot fabricate, so a failure here means
// the checker is wrong rather than the page.
func TestReferenceDigestIsFullyGrounded(t *testing.T) {
	ref := buildReference(t)

	g := CheckGrounding(ref.Artifacts, ref.Corpus, ref.Loc)
	if !g.OK() {
		t.Fatalf("the reference page carries %d citation(s) of %d that the fact base does not support: %s",
			len(g.Ungrounded), g.Cited, strings.Join(g.Ungrounded, ", "))
	}
}

// The committed reference page, byte for byte.
//
// It is the artefact a reader opens to see what a good answer looks like, and it
// turns any change in ranking, wording or citation into a reviewable diff rather
// than into nothing at all. Regenerate deliberately: `make golden`.
func TestReferenceDigestMatchesGolden(t *testing.T) {
	ref := buildReference(t)
	path := filepath.Join("testdata", "golden", "digest.md")

	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("cannot create testdata/golden: %v", err)
		}
		if err := os.WriteFile(path, []byte(ref.Artifacts.Digest), 0o644); err != nil {
			t.Fatalf("cannot write %s: %v", path, err)
		}
		t.Logf("rewrote %s (%d bytes)", path, len(ref.Artifacts.Digest))
		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("cannot read %s (run `make golden` to create it): %v", path, err)
	}
	if ref.Artifacts.Digest != string(want) {
		t.Errorf("the reference digest differs from %s\n%s", path, firstDiff(string(want), ref.Artifacts.Digest))
	}
}

// Sabotage has to fire on every extractor the answer key names. An extractor
// the graders cannot see going dark is an extractor that could break silently,
// and this is the test that says none of the seven can.
func TestSabotageDropsTheScoreForEveryExtractor(t *testing.T) {
	ref := buildReference(t)

	for _, kind := range extract.Kinds() {
		t.Run(kind, func(t *testing.T) {
			s, err := RunSabotage(SabotageInput{
				Corpus: ref.Corpus, Today: ref.Day, Loc: ref.Loc,
				Traps: ref.Traps, Extractor: kind,
			})
			if err != nil {
				t.Fatalf("sabotage %s: %v", kind, err)
			}
			if s.Alarm {
				t.Errorf("disabling %s did not move the score (drop %d); the graders cannot see it: %s",
					kind, s.Drop(), strings.Join(s.Blind, "; "))
			}
		})
	}
}

// And the alarm has to be able to fire, or the previous test is decoration.
// Disabling an extractor no task depends on must leave the score untouched and
// raise the alarm — the exact shape of a blind grader.
func TestSabotageAlarmsWhenNothingDependsOnTheExtractor(t *testing.T) {
	ref := buildReference(t)

	// One task, hung on an extractor other than the one we break.
	trap, ok := ref.Traps.ByID("commitment-cap-table-slip")
	if !ok {
		t.Fatal("the catalog no longer contains commitment-cap-table-slip")
	}
	s, err := RunSabotage(SabotageInput{
		Corpus: ref.Corpus, Today: ref.Day, Loc: ref.Loc,
		Traps: datagen.Traps{trap}, Extractor: "staleness",
	})
	if err != nil {
		t.Fatalf("sabotage: %v", err)
	}
	if !s.Alarm {
		t.Errorf("expected an alarm: the only graded task does not depend on staleness, so the score cannot fall (drop %d)", s.Drop())
	}
}

// firstDiff reports the first line that differs. A full diff of a one-page
// document is not more informative, only longer.
func firstDiff(want, got string) string {
	w, g := strings.Split(want, "\n"), strings.Split(got, "\n")
	for i := 0; i < len(w) || i < len(g); i++ {
		wl, gl := "", ""
		if i < len(w) {
			wl = w[i]
		}
		if i < len(g) {
			gl = g[i]
		}
		if wl != gl {
			return "line " + strconv.Itoa(i+1) + ":\n  want: " + wl + "\n  got:  " + gl
		}
	}
	return "(no line differs; check trailing bytes)"
}
