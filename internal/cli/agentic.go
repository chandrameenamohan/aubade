package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/chandrameenamohan/aubade/internal/agentic"
	"github.com/chandrameenamohan/aubade/internal/digest"
	"github.com/chandrameenamohan/aubade/internal/extract"
	"github.com/chandrameenamohan/aubade/internal/runner"
	"github.com/spf13/cobra"
)

// `aubade digest` in its default, agentic mode.
//
// The order of operations here is the argument. Everything that can fail for
// free fails before anything is paid for: the customize prompt is read, the
// corpus is loaded, and the toolbox runs — so `signals.json` exists on disk
// before a single model call is made. Only then is a runner probed, and only
// then does the loop start. A run that dies at the last step still leaves the
// grader the fact base it would have been composed from.

// TranscriptFile is the runner's raw transcript, written beside the digest.
// EVAL-PRINCIPLES #12 grades outputs rather than paths, and keeps this so a
// later check can ask whether tool output grounded each cited fact.
const TranscriptFile = "transcript.jsonl"

// runnerRegistry is the set of runners this binary can use. It is a package
// variable so tests can vote with fakes instead of spending money on a real
// model in `go test`.
var runnerRegistry = runner.Default()

// runAgenticDigest executes `aubade digest` without --no-llm.
func runAgenticDigest(c *cobra.Command) error {
	customizePath, err := c.Flags().GetString("customize")
	if err != nil {
		return err
	}
	customize, err := agentic.LoadCustomize(customizePath)
	if err != nil {
		return err
	}
	consensusOn, err := consensusFlag(c)
	if err != nil {
		return err
	}
	runnerName, err := c.Flags().GetString("runner")
	if err != nil {
		return err
	}
	outDir, err := c.Flags().GetString("out")
	if err != nil {
		return err
	}

	corpus, tb, err := loadCorpus(c)
	if err != nil {
		return err
	}
	signals, err := tb.All()
	if err != nil {
		return err
	}
	signalsPath := filepath.Join(outDir, extract.SignalsFile)
	if err := extract.WriteSignals(signalsPath, signals); err != nil {
		return err
	}

	ctx := c.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	orch, voters, roster, err := selectRunners(ctx, runnerName, consensusOn)
	if err != nil {
		return err
	}

	toolBin, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot locate the aubade binary to hand the runner: %w", err)
	}
	// The loop runs in a scratch directory rather than wherever the user is
	// standing: a headless run reads the working directory it is given, and the
	// digest's honesty floor is exactly what a stranger's instruction file would
	// talk a model out of.
	workDir, err := os.MkdirTemp("", "aubade-digest-")
	if err != nil {
		return fmt.Errorf("cannot create a working directory for the runner: %w", err)
	}
	defer os.RemoveAll(workDir)

	transcriptPath := filepath.Join(outDir, TranscriptFile)
	transcript, err := createFile(transcriptPath)
	if err != nil {
		return err
	}
	defer transcript.Close()

	res, err := agentic.Compose(ctx, agentic.Input{
		Corpus:        corpus,
		Signals:       signals,
		Now:           tb.Now(),
		Loc:           tb.Location(),
		Owner:         tb.Owner(),
		Day:           tb.Today().Format("Monday, January 2, 2006"),
		Today:         tb.Today().Format("2006-01-02"),
		ToolBin:       toolBin,
		DataDir:       dataDir(c),
		WorkDir:       workDir,
		Orchestrator:  orch,
		Voters:        voters,
		Roster:        roster,
		Consensus:     consensusOn,
		Customize:     customize,
		CustomizePath: customizePath,
		Transcript:    transcript,
		Log:           c.ErrOrStderr(),
	})
	if err != nil {
		return err
	}

	digestPath := filepath.Join(outDir, digest.DigestFile)
	if err := writeFile(digestPath, []byte(res.Markdown)); err != nil {
		return err
	}
	return reportAgenticRun(c, res, tb.Today().Format("2006-01-02"), digestPath, signalsPath, transcriptPath, len(signals))
}

// reportAgenticRun prints the run in the shape its caller expects.
func reportAgenticRun(c *cobra.Command, res *agentic.Result, today, digestPath, signalsPath, transcriptPath string, signals int) error {
	w := c.OutOrStdout()
	if wantJSON(c) {
		return writeJSON(w, map[string]any{
			"ok":         true,
			"mode":       res.Mode,
			"today":      today,
			"path":       digestPath,
			"signals":    signalsPath,
			"transcript": transcriptPath,
			"counts": map[string]int{
				"signals":    signals,
				"tool_calls": toolCalls(res),
				"decisions":  len(res.Decisions),
			},
			"fell_back":  res.FellBack(),
			"violations": violationStrings(res),
			"digest":     res.Markdown,
		})
	}

	fmt.Fprint(w, res.Markdown)
	fmt.Fprintf(w, "\nwrote %s, %s and %s — %d signals, %d toolbox call(s), %d consensus decision(s)\n",
		digestPath, signalsPath, transcriptPath, signals, toolCalls(res), len(res.Decisions))
	return nil
}

func toolCalls(res *agentic.Result) int {
	if res.Run == nil {
		return 0
	}
	return res.Run.ToolCalls
}

func violationStrings(res *agentic.Result) []string {
	out := make([]string, 0, len(res.Violations))
	for _, v := range res.Violations {
		out = append(out, v.String())
	}
	return out
}

// selectRunners resolves --runner and the consensus roster.
//
// With consensus off there is one runner and one probe. With it on, every
// registered runner is probed and the live ones vote — which is where the
// learning tests' finding bites: a runner that is installed and says it is
// logged in still has to answer a real call before it is allowed near the
// digest.
func selectRunners(ctx context.Context, name string, consensus bool) (runner.Runner, []runner.Runner, *runner.Roster, error) {
	chosen, err := runnerRegistry.New(name)
	if err != nil {
		return nil, nil, nil, err
	}
	if !chosen.CanOrchestrate() {
		return nil, nil, nil, fmt.Errorf("--runner=%s answers one-shot questions but cannot drive the toolbox loop; it votes in consensus, it cannot compose the page — use --runner=claude, or --no-llm", name)
	}

	if !consensus {
		if !chosen.Installed() {
			return nil, nil, nil, notInstalled(name)
		}
		if err := chosen.Probe(ctx); err != nil {
			return nil, nil, nil, fmt.Errorf("%s is installed but did not answer: %w\nrun `aubade digest --no-llm` for the deterministic page", name, err)
		}
		return chosen, []runner.Runner{chosen}, nil, nil
	}

	roster := runnerRegistry.Detect(ctx)
	orch, live := roster.Get(name)
	if !live {
		if st, known := roster.StatusOf(name); known && st.State == runner.Dead {
			return nil, nil, nil, fmt.Errorf("--runner=%s is installed but did not answer (%s); roster — %s\nrun `aubade digest --no-llm` for the deterministic page", name, st.Detail, roster.Describe())
		}
		return nil, nil, nil, notInstalled(name)
	}
	return orch, roster.Live(), roster, nil
}

// notInstalled is the message a missing runner gets. It names the fallback,
// because "not installed" without "here is what still works" is a dead end.
func notInstalled(name string) error {
	return fmt.Errorf("--runner=%s is not installed on this machine; install it, pick another with --runner, or run `aubade digest --no-llm` for the deterministic page", name)
}

// consensusFlag reads --consensus. The vocabulary is two words, and anything
// else is refused rather than guessed: silently reading an unrecognised value as
// "off" would turn quality off by typo.
func consensusFlag(c *cobra.Command) (bool, error) {
	v, err := c.Flags().GetString("consensus")
	if err != nil {
		return false, err
	}
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", "on":
		return true, nil
	case "off":
		return false, nil
	default:
		return false, fmt.Errorf("invalid --consensus %q; want on or off", v)
	}
}

// dataDir is the corpus directory as an absolute path, because it is handed to
// a subprocess running somewhere else entirely.
func dataDir(c *cobra.Command) string {
	dir, err := c.Flags().GetString("data")
	if err != nil || strings.TrimSpace(dir) == "" {
		dir = defaultDataDir
	}
	if abs, err := filepath.Abs(dir); err == nil {
		return abs
	}
	return dir
}

// createFile opens a file for writing, creating its directory.
func createFile(path string) (*os.File, error) {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("cannot create %s: %w", dir, err)
		}
	}
	f, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("cannot write %s: %w", path, err)
	}
	return f, nil
}
