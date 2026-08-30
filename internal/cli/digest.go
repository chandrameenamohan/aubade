package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/chandrameenamohan/aubade/internal/digest"
	"github.com/chandrameenamohan/aubade/internal/extract"
	"github.com/spf13/cobra"
)

// `aubade digest` wiring.
//
// Two modes share this file's front half. `--no-llm` is built: load the corpus,
// run every extractor in the fixed order, compose the page from the signals,
// write it. Agentic mode is not, and its stub says so rather than silently
// falling back — a digest that quietly composed itself a different way than the
// user asked for is exactly the kind of thing this product exists not to do.
//
// The run writes two files, not one. `digest.md` is the product; `signals.json`
// beside it is the fact base that page was composed from, so a wrong line can
// be diagnosed as mis-ranked (it is in signals.json) or as missed (it is not) —
// two different bugs — and so the eval harness can grade both from one run.

// runDigest executes `aubade digest`.
func runDigest(c *cobra.Command) error {
	noLLM, err := c.Flags().GetBool("no-llm")
	if err != nil {
		return err
	}
	customize, err := c.Flags().GetString("customize")
	if err != nil {
		return err
	}

	// Customization reshapes the compose stage, and --no-llm has no compose
	// stage to reshape. Saying so beats quietly ignoring one of the two flags.
	if strings.TrimSpace(customize) != "" && noLLM {
		return fmt.Errorf("--customize needs the agentic composer and --no-llm does not run it; drop one of the two flags")
	}
	if !noLLM {
		return &StubError{
			Command: c.CommandPath(),
			Bead:    "C3",
			What:    "agentic orchestration over the toolbox (runners, consensus, --customize); run with --no-llm for the deterministic digest today",
		}
	}
	return runNoLLMDigest(c)
}

// runNoLLMDigest composes and writes the fallback-mode page.
func runNoLLMDigest(c *cobra.Command) error {
	corpus, tb, err := loadCorpus(c)
	if err != nil {
		return err
	}
	signals, err := tb.All()
	if err != nil {
		return err
	}

	page, err := digest.Build(digest.Input{
		Corpus:  corpus,
		Signals: signals,
		Now:     tb.Now(),
		Loc:     tb.Location(),
		Owner:   tb.Owner(),
		Mode:    digest.ModeNoLLM,
	})
	if err != nil {
		return err
	}

	outDir, err := c.Flags().GetString("out")
	if err != nil {
		return err
	}
	markdown := page.Markdown()
	digestPath := filepath.Join(outDir, digest.DigestFile)
	if err := writeFile(digestPath, []byte(markdown)); err != nil {
		return err
	}
	signalsPath := filepath.Join(outDir, extract.SignalsFile)
	if err := extract.WriteSignals(signalsPath, signals); err != nil {
		return err
	}

	w := c.OutOrStdout()
	if wantJSON(c) {
		return writeJSON(w, map[string]any{
			"ok":      true,
			"mode":    digest.ModeNoLLM,
			"today":   tb.Today().Format("2006-01-02"),
			"path":    digestPath,
			"signals": signalsPath,
			"counts": map[string]int{
				"signals": len(signals),
				"lines":   page.Stats.Rendered,
				"drafts":  page.Stats.Drafts,
			},
			"digest": markdown,
		})
	}

	// CLI in, markdown out: the page itself is the answer, and the paths are
	// the note underneath it for whoever is about to run the eval.
	fmt.Fprint(w, markdown)
	fmt.Fprintf(w, "\nwrote %s and %s — %d signals, %d lines, %d draft(s)\n",
		digestPath, signalsPath, len(signals), page.Stats.Rendered, page.Stats.Drafts)
	return nil
}

// writeFile writes data to path, creating the directory it lives in.
func writeFile(path string, data []byte) error {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("cannot create %s: %w", dir, err)
		}
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("cannot write %s: %w", path, err)
	}
	return nil
}
