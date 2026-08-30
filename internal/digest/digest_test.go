package digest

import (
	"context"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chandrameenamohan/aubade/internal/extract"
	"github.com/chandrameenamohan/aubade/internal/localfs"
	"github.com/chandrameenamohan/aubade/internal/model"
)

// fixtureDay is the anchor every test here uses: Monday 31 August 2026, the
// same day the toolbox's own fixtures are written against, so a signal that
// changes upstream shows up in this package's goldens rather than hiding.
const fixtureDay = "2026-08-31"

// update rewrites the committed golden pages. It is deliberately a flag rather
// than an environment variable, and it has its own make target
// (`make golden`), because regenerating a golden must be a thing someone
// decided to do — a golden that silently rewrites itself proves nothing, and
// the first person to find that out is whoever trusted it.
var update = flag.Bool("update", false, "rewrite the golden digests in testdata/golden")

// buildFixture composes a page from a testdata corpus, exactly as the CLI does.
//
// The data root is relative on purpose: the toolbox names the file it could not
// find in its missing-source detail, and an absolute path would put this
// machine's home directory into a committed golden.
func buildFixture(t *testing.T, name string) *Digest {
	t.Helper()

	loc := model.Location()
	day, err := extract.ParseToday(fixtureDay, loc)
	if err != nil {
		t.Fatalf("ParseToday: %v", err)
	}
	corpus, err := model.LoadCorpus(context.Background(), localfs.New(filepath.Join("testdata", name)))
	if err != nil {
		t.Fatalf("LoadCorpus(%s): %v", name, err)
	}
	tb, err := extract.New(corpus, day, loc)
	if err != nil {
		t.Fatalf("extract.New: %v", err)
	}
	signals, err := tb.All()
	if err != nil {
		t.Fatalf("toolbox: %v", err)
	}

	page, err := Build(Input{
		Corpus:  corpus,
		Signals: signals,
		Now:     tb.Now(),
		Loc:     tb.Location(),
		Owner:   tb.Owner(),
	})
	if err != nil {
		t.Fatalf("Build(%s): %v", name, err)
	}
	return page
}

// The golden test. A pinned corpus and a pinned --today produce one committed
// page, compared byte for byte.
//
// This is the cheapest real proof the composer has: it catches a re-ranking, a
// dropped citation, a reworded honesty line and a changed section order in one
// assertion, and it makes every such change show up as a reviewable diff rather
// than as nothing at all. Regenerate deliberately with `make golden`.
func TestGoldenDigests(t *testing.T) {
	cases := []struct{ corpus, golden string }{
		// The full corpus: every extractor fires, both drafting registers
		// appear, a section overflows.
		{"corpus", "digest.md"},
		// The degraded corpus: three sources missing, an inbox past the
		// freshness budget, and no profile at all — the page that has to be
		// honest about itself.
		{"stale", "stale.md"},
	}

	for _, tc := range cases {
		t.Run(tc.corpus, func(t *testing.T) {
			got := buildFixture(t, tc.corpus).Markdown()
			path := filepath.Join("testdata", "golden", tc.golden)

			if *update {
				if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
					t.Fatalf("cannot write %s: %v", path, err)
				}
				t.Logf("rewrote %s (%d bytes)", path, len(got))
				return
			}

			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("cannot read %s (run `make golden` to create it): %v", path, err)
			}
			if got != string(want) {
				t.Errorf("digest differs from %s\n%s", path, firstDiff(string(want), got))
			}
		})
	}
}

// firstDiff reports the first line that differs, which is what a reader needs;
// a full diff of a one-page document is not more informative, it is just longer.
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
			return "line " + itoa(i+1) + ":\n  want: " + wl + "\n  got:  " + gl
		}
	}
	return "(no line differs; check trailing bytes)"
}

// Same corpus, same --today, byte-identical page — six times over, from six
// independently loaded corpora. Determinism is the property the golden file and
// the whole trap eval rest on, and map iteration order is the only realistic way
// for it to break, which a single comparison would miss.
func TestCompositionIsDeterministic(t *testing.T) {
	first := buildFixture(t, "corpus").Markdown()
	for i := 0; i < 5; i++ {
		if got := buildFixture(t, "corpus").Markdown(); got != first {
			t.Fatalf("run %d differs from run 0:\n%s", i+1, firstDiff(first, got))
		}
	}
}

// The section contract. The eval harness and every reader bind to these
// headings in this order; a silently renamed section is a silently broken
// integration.
func TestSectionContract(t *testing.T) {
	page := buildFixture(t, "corpus")

	want := []string{
		model.SectionOneThingNow,
		model.SectionUrgentToday,
		model.SectionDecisions,
		model.SectionPulse,
		model.SectionCalendar,
		model.SectionNotSure,
		model.SectionHonesty,
	}
	if len(page.Sections) != len(want) {
		t.Fatalf("got %d sections, want %d", len(page.Sections), len(want))
	}
	for i, id := range want {
		if page.Sections[i].ID != id {
			t.Errorf("section %d is %q, want %q", i, page.Sections[i].ID, id)
		}
	}

	md := page.Markdown()
	for _, heading := range []string{
		"## If there is one thing you must do right now:",
		"## Urgent To-Do Today",
		"## Decisions & Approvals Needed",
		"## Team & Product Pulse",
		"## Calendar & Personal",
		"## Honesty",
	} {
		if !strings.Contains(md, heading) {
			t.Errorf("the page is missing %q", heading)
		}
	}
}

// A content section with nothing in it still renders, and says so. A missing
// heading reads as a lost section; "nothing needs you today" is an answer.
func TestEmptySectionsStillRender(t *testing.T) {
	page := buildFixture(t, "stale")
	md := page.Markdown()

	if !strings.Contains(md, "## Team & Product Pulse\nNo team or product signal") {
		t.Errorf("an empty content section should render its honest sentence:\n%s", md)
	}
	if strings.Contains(md, "## I'm not sure") {
		t.Error(`"I'm not sure" with nothing in it should be omitted, not rendered empty`)
	}
}

// Every factual line carries its receipt. The architecture's whole claim is
// that facts enter the page only through cited tool output, so a rendered line
// that came from a signal and shows no citation is that claim failing.
func TestEveryFactualLineIsCited(t *testing.T) {
	page := buildFixture(t, "corpus")

	for _, s := range page.Sections {
		for _, it := range s.Items {
			if len(it.Citations) == 0 {
				t.Errorf("section %s: %q has no citation", s.ID, it.Lead)
				continue
			}
			if len(it.Refs) != len(it.Citations) {
				t.Errorf("section %s: %q resolved %d of %d citations", s.ID, it.Lead, len(it.Refs), len(it.Citations))
			}
			for _, ref := range it.Refs {
				if !strings.HasPrefix(ref, "[") || !strings.HasSuffix(ref, "]") {
					t.Errorf("section %s: malformed citation %q", s.ID, ref)
				}
			}
		}
	}
	for _, it := range page.Banner {
		if len(it.Citations) == 0 {
			t.Errorf("banner line %q has no citation", it.Lead)
		}
	}
}

// Citations render the way the sample digest renders them: who said it and
// when, not an opaque record id.
func TestCitationsNameThePersonAndTheMoment(t *testing.T) {
	md := buildFixture(t, "corpus").Markdown()

	for _, want := range []string{
		"[email: Marcus, Aug 27 16:42]",       // the sample's own shape: who, and when
		"*[note: notes/sprint-aug-week4.md]*", // a note cites its path, in an italic span
		"[task: tasks.md:5]",                  // a task cites its line
	} {
		if !strings.Contains(md, want) {
			t.Errorf("the page never renders %s", want)
		}
	}
	if strings.Contains(md, "[email: e-001]") {
		t.Error("a resolvable email citation should not render as a raw id")
	}
}

// Build refuses input it cannot compose honestly.
func TestBuildValidatesItsInput(t *testing.T) {
	if _, err := Build(Input{}); err == nil {
		t.Error("Build(nil corpus) should be an error")
	}
	if _, err := Build(Input{Corpus: &model.Corpus{}}); err == nil {
		t.Error("Build with a zero anchor instant should be an error")
	}

	bad := model.Signals{{ID: "x", Kind: "commitments", Priority: model.P0, Title: "t",
		SectionHint: model.SectionUrgentToday, Confidence: model.Certain}}
	_, err := Build(Input{Corpus: &model.Corpus{}, Signals: bad, Now: mustNow(t)})
	if err == nil {
		t.Error("Build should refuse a signal with no citations")
	}
}

// mustNow is the anchor instant the fixture day resolves to: 06:00 Pacific.
func mustNow(t *testing.T) time.Time {
	t.Helper()
	day, err := extract.ParseToday(fixtureDay, model.Location())
	if err != nil {
		t.Fatalf("ParseToday: %v", err)
	}
	return day.Add(6 * time.Hour)
}
