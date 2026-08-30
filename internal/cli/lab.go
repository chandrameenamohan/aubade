package cli

import (
	"strings"

	"github.com/spf13/cobra"
)

const labLong = `
aubade-lab is the internal harness: it writes the exam, and it grades it.

The product binary ("aubade") is the student. It carries no harness tooling and
never sees this binary — a hard boundary, because a customer should not be able
to run the grader over their own inbox, and because a student that can read the
answer key proves nothing.

  generate  writes the seeded synthetic corpus and the traps.json answer key.
            Every planted trap is a scenario script that emits both its data and
            its answer-key entry, so the key cannot drift from the exam. Same
            seed, byte-identical output.

  eval      runs the trap harness. Every positive trap must be present, every
            negative trap must be absent, and a miss names the extractor that
            missed it. --no-llm is the regression suite (deterministic, one
            trial, 100% bar, gated). Agentic mode is the capability suite
            (N=3 trials, reported as pass^3 and pass@3, never one number).

Examples:
  aubade-lab generate --seed 42 --today 2026-08-30 --out data/
  aubade-lab eval                             regression suite, exits non-zero on a miss
  aubade-lab eval --sabotage=commitments      disable one extractor; alarm if the score holds
  aubade-lab eval --judge                     add the layer-2 voice/readability judge`

// NewLabCmd builds the internal harness command tree. Nothing here ships to a
// customer.
func NewLabCmd() *cobra.Command {
	root := newRoot(
		"aubade-lab",
		"Internal harness: write the exam, grade the digest (never shipped)",
		labLong,
	)

	root.AddCommand(
		newGenerateCmd(),
		newEvalCmd(),
	)
	return root
}

func newGenerateCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "generate",
		Short: "Write the seeded synthetic corpus and its traps.json answer key",
		Long: strings.TrimSpace(`
Write the synthetic 30-day corpus and the answer key that grades it:
inbox.jsonl (~500 threaded emails, roughly 30% realistic noise), calendar.ics,
notes/, tasks.md, profile.md, and traps.json.

Seeded and reproducible: the same --seed produces byte-identical output, which is
what lets a committed golden digest mean anything. Dates anchor to --today so the
dataset stays evergreen rather than rotting into last year.`),
		Args: cobra.NoArgs,
		RunE: stub("B1", "scenario-script generator emitting the corpus and its own answer key"),
	}

	f := c.Flags()
	f.Int("seed", 42, "PRNG seed; same seed produces byte-identical output")
	f.String("today", "", "anchor date, YYYY-MM-DD (default: system date, America/Los_Angeles)")
	f.String("out", "data/", "directory to write the corpus and traps.json into")

	annotate(c, "B1")
	return c
}

func newEvalCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "eval",
		Short: "Run the trap harness and write the scorecard",
		Long: strings.TrimSpace(`
Grade a digest against traps.json and write out/scorecard.md, with the regression
and capability sections kept separate. Exits non-zero on any regression miss.

Graders assert on outputs, never on which tools the agent chose to call: a
positive trap passes when its signal kind appears in signals.json AND at least one
expected keyword appears in the digest text; a negative trap passes when it is
absent from both.

  --sabotage  disable one extractor, then ALARM if the score does not drop. A
              grader that cannot see a broken extractor is not a grader.
  --judge     add the layer-2 model judge for the one axis code cannot grade —
              "does this read like the sample, in the user's voice" — anchored,
              reason-before-score, with an "uncertain" escape hatch.`),
		Args: cobra.NoArgs,
		RunE: stub("D1", "the trap harness, scorecard, and the make check regression run"),
	}

	f := c.Flags()
	f.String("sabotage", "", "disable one extractor by name and alarm if the score does not drop")
	f.Bool("judge", false, "run the optional layer-2 model judge for voice and readability")
	f.Bool("adversarial", false, "run the adversarial pass: negative traps and suppression pressure")
	f.String("out", "out/", "directory for scorecard.md and per-trial artifacts")

	annotate(c, "D1")
	return c
}
