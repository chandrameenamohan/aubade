package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// toolNames is the deterministic toolbox surface (SPEC §2 / HLD §3). It is the
// agent's tool menu, so it is validated here rather than deep in the engine: an
// agent that guesses a name gets a listed set of alternatives, immediately.
var toolNames = []string{
	"commitments",
	"quiet-threads",
	"conflicts",
	"contradictions",
	"dispatchables",
	"suppressions",
	"staleness",
	"thread",
	"search",
}

// toolsTakingArg are the read-only investigation tools that need a target —
// they exist so the orchestrator can look before it ranks.
var toolsTakingArg = map[string]string{
	"thread": "<thread-id>",
	"search": "<query>",
}

const aubadeLong = `
aubade turns a morning's worth of email, calendar, notes, and tasks into one page
that answers three questions: what needs me today, what am I about to drop, and
what can I dispatch right now.

The engine is split on purpose. Every fact-producing operation is a pure, cited
Go function exposed as "aubade tool <name>" — same data and same --today always
give the same answer. Orchestration is agentic: "aubade digest" hands a model the
toolbox and lets it decide what to chase and how to compose the page. Facts can
only enter the digest through cited tool output, so the model orchestrates but it
cannot fabricate. "--no-llm" runs the same toolbox in a fixed order and renders
from templates — the full digest, no API keys, so it always runs cold.

AI agents are first-class callers: when one is detected, output and errors go
JSON-first with tool-use hints. Detection failure degrades to human markdown,
always.

Examples:
  aubade digest --today 2026-08-30        compose the one-pager
  aubade digest --no-llm                  same facts, zero keys, fixed template
  aubade tool commitments --json          one extractor, cited JSON signals
  aubade tool thread t-0042 --json        read a thread before ranking it
  aubade signals --today 2026-08-30       run every extractor, write signals.json
  aubade schedule --design                print the scheduling design`

// NewAubadeCmd builds the product command tree. Everything a customer touches
// lives here; nothing from the eval harness does.
func NewAubadeCmd() *cobra.Command {
	root := newRoot(
		"aubade",
		"Agent-orchestrated morning digest, grounded in a deterministic cited toolbox",
		aubadeLong,
	)

	root.AddCommand(
		newDigestCmd(),
		newToolCmd(),
		newSignalsCmd(),
		newScheduleCmd(),
	)
	return root
}

func newDigestCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "digest",
		Short: "Compose the one-page morning digest",
		Long: strings.TrimSpace(`
Compose the one-page digest: one thing right now, urgent today, decisions and
approvals, team and product pulse, calendar and personal — plus an honesty
banner whenever a source is stale, missing, or self-contradictory.

Default mode is agentic: a runner drives the deterministic toolbox and composes
the page, and every factual line traces to a tool citation. --no-llm runs the
same extractors in a fixed order through the default template instead: identical
facts, no network, no keys.

Consensus is on by default. At bounded one-shot decision points (ambiguous
urgency, the "one thing right now" pick) aubade asks every runner that answers a
liveness probe and majority-votes; disagreement routes the item to "I'm not sure"
with the thread shown, rather than a coin flip. A runner that is installed but
cannot answer is dropped rather than counted as a dissent, and the footer names
it. One runner answering means single-runner, silently. --consensus=off is the
frugal flag.

The honesty layer is not customizable: --customize reshapes format, never
truthfulness. The staleness banner, contradictions and "I'm not sure" are
appended by aubade from the signals whatever the composer wrote.

Either mode writes out/digest.md and, beside it, the out/signals.json it was
composed from — so a wrong line can be diagnosed as mis-ranked or as missed,
which are two different bugs. Agentic mode also writes out/transcript.jsonl, the
loop's own record of which tools it called. Every citation on a composed page is
checked against signals.json before it is written; a page that cites anything
else is rejected whole and the deterministic composer writes the digest instead,
loudly.`),
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error { return runDigest(c) },
	}

	f := c.Flags()
	f.String("data", defaultDataDir, "corpus directory (inbox.jsonl, calendar.ics, notes/, tasks.md, profile.md)")
	f.String("today", "", "anchor date, YYYY-MM-DD (default: system date, America/Los_Angeles)")
	f.String("customize", "", "path to a prompt.md that reshapes the digest format (agentic mode only)")
	f.Bool("no-llm", false, "fixed-order extractors + default template: no network, no API keys")
	f.String("runner", "claude", "orchestration runner: claude (codex votes in consensus but cannot drive the toolbox)")
	f.String("consensus", "on", "fan one-shot decisions to every installed runner and majority-vote: on|off")
	f.String("out", defaultOutDir, "directory for digest.md and the run's artifacts")
	f.Bool("json", false, "emit the run as JSON (default when an AI agent caller is detected)")

	return c
}

func newToolCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "tool <name> [target]",
		Short: "Run one deterministic extractor and print its cited signals",
		Long: strings.TrimSpace(`
Run a single extractor from the deterministic toolbox and print the signals it
produced, each carrying at least one citation (email id, calendar UID, note path,
or task ref). Pure: the same data and the same --today always produce the same
output, which is what makes the trap eval possible.

This is also the agent's tool surface — the orchestrator composes the digest by
calling these, and "thread" and "search" exist so it can investigate before it
ranks.

Extractors:
  commitments      promises made and owed, and whether they were kept
  quiet-threads    threads that went silent while still owing an answer
  conflicts        calendar double-bookings and impossible transitions
  contradictions   sources that disagree; both sides kept, never auto-resolved
  dispatchables    items that can be handled right now with one reply
  suppressions     items deliberately held back, with the reason
  staleness        sources older than their freshness budget
  thread <id>      read one full thread
  search <query>   search the corpus`),
		Args:      toolArgs,
		ValidArgs: toolNames,
		RunE:      runTool,
	}

	c.Flags().Bool("json", false, "emit JSON signals (default when an AI agent caller is detected)")
	corpusFlags(c)

	return c
}

// toolArgs validates the extractor name up front. An unknown name is a caller
// error worth a precise message, not a stack trace three layers down.
func toolArgs(c *cobra.Command, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("tool requires an extractor name; one of: %s", strings.Join(toolNames, ", "))
	}

	name := args[0]
	known := false
	for _, n := range toolNames {
		if n == name {
			known = true
			break
		}
	}
	if !known {
		return fmt.Errorf("unknown tool %q; one of: %s", name, strings.Join(toolNames, ", "))
	}

	if placeholder, needsArg := toolsTakingArg[name]; needsArg {
		if len(args) < 2 {
			return fmt.Errorf("tool %s requires an argument: aubade tool %s %s", name, name, placeholder)
		}
		if len(args) > 2 {
			return fmt.Errorf("tool %s takes exactly one argument, got %d", name, len(args)-1)
		}
		return nil
	}
	if len(args) > 1 {
		return fmt.Errorf("tool %s takes no arguments, got %d", name, len(args)-1)
	}
	return nil
}

func newSignalsCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "signals",
		Short: "Run every extractor and write the signals.json audit trail",
		Long: strings.TrimSpace(`
Run every extractor over the corpus and write out/signals.json: the complete,
cited fact base the digest is composed from.

This is the audit surface. If a line in the digest is wrong, it is either here
and mis-ranked, or it is not here at all — and those are two different bugs.`),
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error { return runSignals(c) },
	}

	corpusFlags(c)
	c.Flags().String("out", defaultOutDir, "directory for signals.json")
	c.Flags().Bool("json", false, "emit the signal set on stdout too (default when an AI agent caller is detected)")

	return c
}

func newScheduleCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "schedule",
		Short: "Print the scheduling design (design deliverable; no implementation)",
		Long: strings.TrimSpace(`
Print the written scheduling design: how a 05:45 PT run gets built, where the
secrets live, and why a laptop-local cron loses to a hosted schedule.

Design only, by intent — scheduling implementation is an explicit non-goal for
week one (SPEC "Non-goals").`),
		Args: cobra.NoArgs,
		RunE: stub("E1", "prints the scheduling design shared with DESIGN.md"),
	}

	c.Flags().Bool("design", false, "print the scheduling design document")

	annotate(c, "E1")
	return c
}
