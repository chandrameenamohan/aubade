package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chandrameenamohan/aubade/internal/ax"
	"github.com/chandrameenamohan/aubade/internal/extract"
	"github.com/chandrameenamohan/aubade/internal/localfs"
	"github.com/chandrameenamohan/aubade/internal/model"
	"github.com/spf13/cobra"
)

// This file wires the CLI to the deterministic toolbox. It holds the two
// commands that are the toolbox's public surface — `aubade tool` and
// `aubade signals` — and the corpus loading they share.
//
// Two behaviours here are contracts rather than conveniences:
//
//   - **JSON is the default for an AI caller.** `aubade tool commitments`
//     invoked from inside a claude tool loop returns JSON with no --json flag,
//     because agent detection already established who is asking (SPEC §9, and
//     the behaviour learning-tests/04 confirmed against the real CLI).
//   - **An absent corpus is an error, not an empty digest.** A directory with no
//     data in it would otherwise produce a valid, cited, entirely empty answer,
//     which is the most expensive kind of wrong.

// defaultDataDir is where `aubade-lab generate --out data/` puts the corpus, so
// it is where the product looks unless told otherwise.
const defaultDataDir = "data"

// defaultOutDir is where run artefacts land.
const defaultOutDir = "out"

// corpusFlags adds the flags every toolbox-backed command needs. It is shared
// so the digest command (bead C2) binds to the same names and defaults.
func corpusFlags(c *cobra.Command) {
	f := c.Flags()
	f.String("data", defaultDataDir, "corpus directory (inbox.jsonl, calendar.ics, notes/, tasks.md, profile.md)")
	f.String("today", "", "anchor date, YYYY-MM-DD (default: system date, America/Los_Angeles)")
}

// loadCorpus reads the corpus named by the command's flags and binds it to the
// anchor day.
//
// It returns both halves because the two callers need different ones: the
// toolbox commands want the extractors, and the digest wants the corpus too —
// an agenda is a list of facts no extractor needs an opinion about.
func loadCorpus(c *cobra.Command) (*model.Corpus, *extract.Toolbox, error) {
	root, err := c.Flags().GetString("data")
	if err != nil {
		return nil, nil, err
	}
	todayFlag, err := c.Flags().GetString("today")
	if err != nil {
		return nil, nil, err
	}

	loc := model.Location()
	today := time.Now().In(loc)
	if strings.TrimSpace(todayFlag) != "" {
		if today, err = extract.ParseToday(todayFlag, loc); err != nil {
			return nil, nil, err
		}
	}

	if err := checkCorpusDir(root); err != nil {
		return nil, nil, err
	}
	corpus, err := model.LoadCorpus(c.Context(), localfs.New(root))
	if err != nil {
		return nil, nil, err
	}
	tb, err := extract.New(corpus, today, loc)
	if err != nil {
		return nil, nil, err
	}
	return corpus, tb, nil
}

// loadToolbox is loadCorpus for the callers that only need the extractors.
func loadToolbox(c *cobra.Command) (*extract.Toolbox, error) {
	_, tb, err := loadCorpus(c)
	return tb, err
}

// checkCorpusDir fails early and helpfully when there is no corpus to read.
func checkCorpusDir(root string) error {
	info, err := os.Stat(root)
	switch {
	case err != nil && os.IsNotExist(err):
		return fmt.Errorf("no corpus at %s: run `aubade-lab generate --out %s/` first, or pass --data", root, filepath.Clean(root))
	case err != nil:
		return fmt.Errorf("cannot read corpus at %s: %w", root, err)
	case !info.IsDir():
		return fmt.Errorf("corpus path %s is not a directory", root)
	}
	if _, err := os.Stat(filepath.Join(root, localfs.InboxFile)); err != nil && os.IsNotExist(err) {
		return fmt.Errorf("no %s in %s: that directory does not look like an aubade corpus", localfs.InboxFile, root)
	}
	return nil
}

// wantJSON reports whether this invocation should emit JSON: either --json was
// passed, or the caller was detected as an AI agent.
func wantJSON(c *cobra.Command) bool {
	if f := c.Flags().Lookup("json"); f != nil {
		if on, err := c.Flags().GetBool("json"); err == nil && on {
			return true
		}
	}
	return ax.OutputMode() == ax.ModeJSON
}

// runTool executes `aubade tool <name> [target]`.
func runTool(c *cobra.Command, args []string) error {
	tb, err := loadToolbox(c)
	if err != nil {
		return err
	}
	arg := ""
	if len(args) > 1 {
		arg = args[1]
	}
	res, err := tb.Run(args[0], arg)
	if err != nil {
		return err
	}

	out := c.OutOrStdout()
	if wantJSON(c) {
		return writeJSON(out, res.Payload())
	}
	renderResult(out, tb, res)
	return nil
}

// runSignals executes `aubade signals`, writing the audit trail.
func runSignals(c *cobra.Command) error {
	tb, err := loadToolbox(c)
	if err != nil {
		return err
	}
	signals, err := tb.All()
	if err != nil {
		return err
	}

	outDir, err := c.Flags().GetString("out")
	if err != nil {
		return err
	}
	path := filepath.Join(outDir, extract.SignalsFile)
	if err := extract.WriteSignals(path, signals); err != nil {
		return err
	}

	w := c.OutOrStdout()
	if wantJSON(c) {
		return writeJSON(w, map[string]any{
			"ok":      true,
			"path":    path,
			"today":   tb.Today().Format("2006-01-02"),
			"count":   len(signals),
			"by_kind": countByKind(signals),
			"signals": signals,
		})
	}

	fmt.Fprintf(w, "wrote %s — %d signals for %s\n\n", path, len(signals), tb.Today().Format("2006-01-02"))
	counts := countByKind(signals)
	for _, kind := range extract.Kinds() {
		fmt.Fprintf(w, "  %-16s %d\n", kind, counts[kind])
	}
	return nil
}

func countByKind(ss model.Signals) map[string]int {
	counts := map[string]int{}
	for _, k := range extract.Kinds() {
		counts[k] = 0
	}
	for _, s := range ss {
		counts[s.Kind]++
	}
	return counts
}

// writeJSON emits an indented JSON document with a trailing newline — the shape
// an agent pipes into jq without thinking about it.
func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// renderResult prints a tool result as markdown for a human reader.
func renderResult(w io.Writer, tb *extract.Toolbox, res *extract.Result) {
	switch {
	case res.Thread != nil:
		renderThread(w, res.Thread)
	case res.Search != nil:
		renderSearch(w, res.Search)
	default:
		renderSignals(w, tb, res)
	}
}

func renderSignals(w io.Writer, tb *extract.Toolbox, res *extract.Result) {
	fmt.Fprintf(w, "# %s — %d signal(s) for %s\n\n", res.Tool, len(res.Signals), tb.Today().Format("2006-01-02"))
	if len(res.Signals) == 0 {
		fmt.Fprintln(w, "nothing to report.")
	}
	for _, s := range res.Signals {
		fmt.Fprintf(w, "## [%s] %s\n", s.Priority, s.Title)
		fmt.Fprintf(w, "%s\n", s.Detail)
		if s.Deadline != nil {
			fmt.Fprintf(w, "deadline: %s\n", s.Deadline.In(tb.Location()).Format("Mon 2 Jan 15:04"))
		}
		refs := make([]string, 0, len(s.Citations))
		for _, cite := range s.Citations {
			refs = append(refs, string(cite.Source)+":"+cite.Ref)
		}
		fmt.Fprintf(w, "cites: %s\nsection: %s · confidence: %s\n\n", strings.Join(refs, ", "), s.SectionHint, s.Confidence)
	}
	if res.Tool == "suppressions" {
		for _, r := range tb.UnhandledSuppressions() {
			fmt.Fprintf(w, "note: profile rule not applied (%s:%d): %s\n", tb.ProfilePath(), r.Line, r.Text)
		}
	}
}

func renderThread(w io.Writer, v *extract.ThreadView) {
	fmt.Fprintf(w, "# %s (%s)\n\n%d message(s) · waiting on %s · quiet for %s\n\n",
		v.Subject, v.ThreadID, v.MessageCount, v.WaitingOn, v.QuietFor)
	for _, m := range v.Messages {
		who := m.From.String()
		if m.FromOwner {
			who += " (you)"
		}
		fmt.Fprintf(w, "## %s — %s\n%s\n\n%s\n\n", m.TS.Format("Mon 2 Jan 15:04"), who, m.Subject, strings.TrimSpace(m.Body))
		if m.Suppress != nil {
			fmt.Fprintf(w, "held back by %s (line %d): %s\n\n", m.Suppress.Why, m.Suppress.Line, m.Suppress.Rule)
		}
	}
}

func renderSearch(w io.Writer, r *extract.SearchResult) {
	fmt.Fprintf(w, "# search %q — %d hit(s)\n\n", r.Query, r.Total)
	for _, h := range r.Hits {
		fmt.Fprintf(w, "- [%s] %s (%s:%s, score %d)\n  %s\n", h.Source, h.Title, h.Source, h.Ref, h.Score, h.Snippet)
	}
	if r.Total > len(r.Hits) {
		fmt.Fprintf(w, "\n%d more hit(s) not shown.\n", r.Total-len(r.Hits))
	}
}
