package datagen

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/chandrameenamohan/aubade/internal/extract"
	"github.com/chandrameenamohan/aubade/internal/localfs"
	"github.com/chandrameenamohan/aubade/internal/model"
)

// The exam has to be answerable and it has to be the only thing on the paper.
// Both halves are checked here, against the real toolbox reading the real
// written corpus — the closest thing to the eval harness that can exist before
// the eval harness does (bead D1).
//
// This is the test that earns the filler's constraints. Everything in filler.go
// about closed threads and non-overlapping meetings is there to keep this green:
// a filler artifact showing up in a finding means the corpus is asking a
// question nobody wrote an answer key for, and every one of those makes the
// scorecard less able to say what went wrong.

// toolboxOver loads a written corpus through the provider and binds the
// extractors to the anchor day.
func toolboxOver(t *testing.T, dir string) *extract.Toolbox {
	t.Helper()
	corpus, err := model.LoadCorpus(context.Background(), localfs.New(dir))
	if err != nil {
		t.Fatalf("LoadCorpus: %v", err)
	}
	tb, err := extract.New(corpus, mustDay(t, pinnedDay), model.Location())
	if err != nil {
		t.Fatalf("extract.New: %v", err)
	}
	return tb
}

// Every extractor has to find something in the generated corpus. An extractor
// that returns nothing here is either broken or has no task behind it, and both
// are worth failing over before a scorecard reports a zero as a pass.
func TestEveryExtractorFiresOnTheGeneratedCorpus(t *testing.T) {
	tb := toolboxOver(t, writeCorpus(t, pinnedSeed))
	signals, err := tb.All()
	if err != nil {
		t.Fatalf("All: %v", err)
	}

	byKind := map[string]int{}
	for _, s := range signals {
		byKind[s.Kind]++
	}
	for _, kind := range model.KnownKinds {
		if byKind[kind] == 0 {
			t.Errorf("extractor %q produced nothing on the generated corpus", kind)
		}
	}
}

// The filler is not allowed to answer a question the scenarios did not ask.
//
// Suppressions are the deliberate exception: listing the newsletters and cold
// mail it held back is that extractor's entire output, and the filler is where
// most of that noise comes from.
func TestFillerRaisesNoFindingsOfItsOwn(t *testing.T) {
	for _, seed := range []int64{1, pinnedSeed, 2026} {
		t.Run(fmt.Sprintf("seed-%d", seed), func(t *testing.T) {
			tb := toolboxOver(t, writeCorpus(t, seed))
			signals, err := tb.All()
			if err != nil {
				t.Fatalf("All: %v", err)
			}
			for _, s := range signals {
				if s.Kind == model.KindSuppressions {
					continue
				}
				for _, c := range s.Citations {
					if strings.HasPrefix(c.Ref, "f-") {
						t.Errorf("%s signal cites the filler artifact %s:%s — %s",
							s.Kind, c.Source, c.Ref, s.Title)
					}
				}
			}
		})
	}
}
