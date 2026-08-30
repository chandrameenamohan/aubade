package cli

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/chandrameenamohan/aubade/internal/datagen"
	"github.com/chandrameenamohan/aubade/internal/extract"
	"github.com/chandrameenamohan/aubade/internal/localfs"
	"github.com/chandrameenamohan/aubade/internal/model"
	"github.com/spf13/cobra"
)

// `aubade-lab generate` writes the exam and its answer key.
//
// Two details here are contracts rather than conveniences:
//
//   - **--today is parsed by the product's own parser** (extract.ParseToday).
//     The generator anchors the corpus to a day and the engine anchors its
//     reasoning to a day, and if those two ever disagreed about what
//     "2026-08-30" means, every trap timed in business days would be off by one
//     in a way no test would explain.
//   - **The receipt names what was written.** Generation is the one command that
//     produces no answer, only files, so the counts are the output: a corpus
//     with four hundred emails and no calendar is a broken run, and the way to
//     find that out is on the line that says so.

// runGenerate builds the corpus and writes it to disk.
func runGenerate(c *cobra.Command) error {
	f := c.Flags()
	seed, err := f.GetInt("seed")
	if err != nil {
		return err
	}
	todayFlag, err := f.GetString("today")
	if err != nil {
		return err
	}
	out, err := f.GetString("out")
	if err != nil {
		return err
	}

	cfg := datagen.Config{Seed: int64(seed)}
	if strings.TrimSpace(todayFlag) != "" {
		if cfg.Today, err = extract.ParseToday(todayFlag, model.Location()); err != nil {
			return err
		}
	}

	plan, err := datagen.Generate(cfg)
	if err != nil {
		return err
	}
	if err := datagen.Write(out, plan); err != nil {
		return err
	}
	return renderGenerated(c, out, plan)
}

// renderGenerated writes the receipt: JSON for an agent, a short table for a
// human.
func renderGenerated(c *cobra.Command, out string, plan *datagen.Plan) error {
	w := c.OutOrStdout()
	dir := filepath.Clean(out)
	today := plan.Today.Format("2006-01-02")
	positive, negative := len(plan.Traps.Positive()), len(plan.Traps.Negative())

	if wantJSON(c) {
		return writeJSON(w, map[string]any{
			"ok":    true,
			"out":   dir,
			"seed":  plan.Seed,
			"today": today,
			"files": map[string]string{
				"inbox":    filepath.Join(dir, localfs.InboxFile),
				"calendar": filepath.Join(dir, localfs.CalendarFile),
				"notes":    filepath.Join(dir, localfs.NotesDir),
				"tasks":    filepath.Join(dir, localfs.TasksFile),
				"profile":  filepath.Join(dir, localfs.ProfileFile),
				"traps":    filepath.Join(dir, datagen.TrapsFile),
			},
			"counts": map[string]int{
				"emails":  len(plan.Emails),
				"threads": countThreads(plan),
				"events":  len(plan.Events),
				"notes":   len(plan.Notes),
				"tasks":   len(plan.Tasks),
				"traps":   len(plan.Traps),
			},
			"traps": map[string]int{"must_surface": positive, "must_not_surface": negative},
		})
	}

	fmt.Fprintf(w, "wrote %s — seed %d, anchored on %s\n\n", dir, plan.Seed, today)
	fmt.Fprintf(w, "  %-14s %4d emails in %d threads over %d days\n",
		localfs.InboxFile, len(plan.Emails), countThreads(plan), datagen.CorpusDays)
	fmt.Fprintf(w, "  %-14s %4d events\n", localfs.CalendarFile, len(plan.Events))
	fmt.Fprintf(w, "  %-14s %4d notes\n", localfs.NotesDir+"/", len(plan.Notes))
	fmt.Fprintf(w, "  %-14s %4d tasks\n", localfs.TasksFile, len(plan.Tasks))
	fmt.Fprintf(w, "  %-14s %4d lines, verbatim\n", localfs.ProfileFile, countLines(datagen.Profile()))
	fmt.Fprintf(w, "  %-14s %4d traps — %d must surface, %d must not\n",
		datagen.TrapsFile, len(plan.Traps), positive, negative)
	return nil
}

// countThreads counts distinct conversations in the corpus.
func countThreads(plan *datagen.Plan) int {
	seen := make(map[string]bool, len(plan.Emails))
	for _, e := range plan.Emails {
		seen[e.ThreadID] = true
	}
	return len(seen)
}

func countLines(s string) int { return strings.Count(strings.TrimRight(s, "\n"), "\n") + 1 }
