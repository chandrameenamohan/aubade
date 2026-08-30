package cli

import (
	_ "embed"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// `aubade schedule` wiring — the written deliverable, and nothing else.
//
// Scheduling *implementation* is an explicit non-goal for week one (SPEC
// "Non-goals"), so this command has exactly one job: print the design that was
// asked for, in the shape the caller can use — markdown for a human, a JSON
// envelope for an agent.
//
// Two decisions here are worth their lines:
//
//   - **The text is embedded, not read from disk.** A design a shipped binary
//     cannot print is a design that goes stale the first time the file moves.
//     Same reasoning as styles.DefaultVoice.
//   - **`aubade schedule` with no flag is an error.** The obvious alternative —
//     print the design anyway — hands an agent that asked aubade to *schedule*
//     something a document and a zero exit code, which reads exactly like
//     success. Nothing was scheduled, so nothing exits 0.

// scheduleDesign is internal/cli/schedule_design.md verbatim: the scheduling
// design deliverable. DESIGN.md carries the same section for a reader who is
// browsing the repo rather than running the binary, and schedule_test.go fails
// if the two ever drift — the same discipline as the committed golden digests.
//
//go:embed schedule_design.md
var scheduleDesign string

// designDocPath is where a human reads the same text.
const designDocPath = "DESIGN.md"

// runSchedule executes `aubade schedule`.
func runSchedule(c *cobra.Command) error {
	design, err := c.Flags().GetBool("design")
	if err != nil {
		return err
	}
	if !design {
		return fmt.Errorf("aubade schedule has no implementation to run: scheduling is a design deliverable this week — `aubade schedule --design` prints it (also in %s)", designDocPath)
	}

	w := c.OutOrStdout()
	doc := strings.TrimSpace(scheduleDesign) + "\n"
	if wantJSON(c) {
		return writeJSON(w, map[string]any{
			"ok":       true,
			"kind":     "scheduling_design",
			"format":   "markdown",
			"doc":      designDocPath,
			"document": doc,
		})
	}
	fmt.Fprint(w, doc)
	return nil
}
