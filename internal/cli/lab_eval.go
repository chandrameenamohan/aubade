package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chandrameenamohan/aubade/internal/datagen"
	"github.com/chandrameenamohan/aubade/internal/eval"
	"github.com/chandrameenamohan/aubade/internal/extract"
	"github.com/chandrameenamohan/aubade/internal/localfs"
	"github.com/chandrameenamohan/aubade/internal/model"
	"github.com/chandrameenamohan/aubade/styles"
	"github.com/spf13/cobra"
)

// `aubade-lab eval` wiring.
//
// The command grades what is already on disk. It does not compose a digest of
// its own for the regression suite, and that is the load-bearing decision here:
// `make check` drives the real binaries end to end, so the page being graded is
// the page the product wrote. A harness that re-composed the digest in-process
// would be grading a second implementation and would go green over a broken
// `aubade digest`.
//
// The two passes that *do* compose are the two that cannot read a file someone
// else wrote: sabotage needs a digest built with an extractor missing (and there
// is deliberately no product flag for that), and the capability suite needs N
// fresh agentic runs.
//
// Exit code is the whole contract for a gate: non-zero on any regression miss,
// non-zero on a sabotage alarm, zero otherwise. The capability suite and the
// judge never touch it — non-deterministic checks never gate.

// runEval executes `aubade-lab eval`.
func runEval(c *cobra.Command) error {
	opts, err := evalOptions(c)
	if err != nil {
		return err
	}

	traps, err := eval.LoadTraps(opts.data)
	if err != nil {
		return err
	}
	corpus, today, err := loadEvalCorpus(c, opts)
	if err != nil {
		return err
	}

	card := &eval.Card{
		DataDir:     filepath.Clean(opts.data),
		Today:       today.Format("2006-01-02"),
		Adversarial: opts.adversarial,
	}

	artifacts, err := eval.LoadArtifacts(opts.out)
	if err != nil {
		return fmt.Errorf("%w\nrun `aubade digest --no-llm --data %s --out %s` first", err, opts.data, opts.out)
	}
	card.Regression = eval.Grade(traps, artifacts)
	card.Grounding = eval.CheckGrounding(artifacts, corpus, model.Location())

	if opts.sabotage != "" {
		if card.Sabotage, err = eval.RunSabotage(eval.SabotageInput{
			Corpus:    corpus,
			Today:     today,
			Loc:       model.Location(),
			Traps:     traps,
			Extractor: opts.sabotage,
		}); err != nil {
			return err
		}
	}
	if opts.capability {
		card.Capability = runCapabilitySuite(c, opts, traps, corpus, today)
	}
	if opts.judge {
		page, from := judgeTarget(card, artifacts, opts.out)
		card.Judge = runVoiceJudge(c, corpus, page, from)
	}

	path := filepath.Join(opts.out, eval.ScorecardFile)
	if err := writeFile(path, []byte(card.Markdown())); err != nil {
		return err
	}
	return reportEval(c, card, path)
}

// evalOpts is the resolved flag set.
type evalOpts struct {
	data        string
	out         string
	today       string
	sabotage    string
	judge       bool
	adversarial bool
	capability  bool
	trials      int
	aubadeBin   string
}

// evalOptions reads the flag set.
//
// Ten flags is ten error checks, and a cobra lookup only fails when the *name*
// is wrong — a programming error, not a user one. So the first failure is
// remembered and the rest of the reads become no-ops: one check at the end, and
// a renamed flag still surfaces rather than being swallowed.
func evalOptions(c *cobra.Command) (*evalOpts, error) {
	r := &flags{c: c}
	o := &evalOpts{
		data:        r.str("data"),
		out:         r.str("out"),
		today:       r.str("today"),
		sabotage:    strings.TrimSpace(r.str("sabotage")),
		aubadeBin:   r.str("aubade"),
		judge:       r.boolean("judge"),
		adversarial: r.boolean("adversarial"),
		capability:  r.boolean("capability"),
		trials:      r.integer("trials"),
	}
	if r.err != nil {
		return nil, r.err
	}
	return o, nil
}

// flags reads a command's flags, keeping the first error.
type flags struct {
	c   *cobra.Command
	err error
}

func (r *flags) str(name string) string {
	v, err := r.c.Flags().GetString(name)
	r.keep(err)
	return v
}

func (r *flags) boolean(name string) bool {
	v, err := r.c.Flags().GetBool(name)
	r.keep(err)
	return v
}

func (r *flags) integer(name string) int {
	v, err := r.c.Flags().GetInt(name)
	r.keep(err)
	return v
}

func (r *flags) keep(err error) {
	if err != nil && r.err == nil {
		r.err = err
	}
}

// loadEvalCorpus reads the corpus the traps were planted in. The harness needs
// it for two things a page and a signal set cannot answer on their own:
// resolving a citation into the span the page prints, and composing the
// sabotaged digest.
func loadEvalCorpus(c *cobra.Command, o *evalOpts) (*model.Corpus, time.Time, error) {
	loc := model.Location()
	today := time.Now().In(loc)
	if strings.TrimSpace(o.today) != "" {
		var err error
		if today, err = extract.ParseToday(o.today, loc); err != nil {
			return nil, time.Time{}, err
		}
	}
	if err := checkCorpusDir(o.data); err != nil {
		return nil, time.Time{}, err
	}
	corpus, err := model.LoadCorpus(c.Context(), localfs.New(o.data))
	if err != nil {
		return nil, time.Time{}, err
	}
	return corpus, today, nil
}

// runCapabilitySuite drives N agentic digests through the product binary.
func runCapabilitySuite(c *cobra.Command, o *evalOpts, traps datagen.Traps, corpus *model.Corpus, today time.Time) *eval.Capability {
	ctx := c.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	return eval.RunCapability(ctx, eval.CapabilityInput{
		Bin:    aubadeBinary(o.aubadeBin),
		Data:   o.data,
		Today:  today.Format("2006-01-02"),
		OutDir: o.out,
		Trials: o.trials,
		Corpus: corpus,
		Loc:    model.Location(),
		Traps:  traps,
	})
}

// runVoiceJudge probes the runners and asks them the anchored voice question.
//
// The judge is shown the base voice and the user's own tone rules, and nothing
// else from the profile: it is grading how the page sounds, and the priority
// list and the suppression rules would only invite it to grade the content it
// was told not to grade.
func runVoiceJudge(c *cobra.Command, corpus *model.Corpus, page, from string) *eval.Judgment {
	ctx := c.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	return eval.RunJudge(ctx, eval.JudgeInput{
		Page:    page,
		Judged:  from,
		Voice:   styles.DefaultVoice,
		Profile: toneRules(corpus),
		Voters:  runnerRegistry.Detect(ctx).Live(),
	})
}

// judgeTarget picks which page the voice judge reads.
//
// An agentic trial when there is one, because that is the page a user gets by
// default and the one whose voice is actually in question; the deterministic
// page otherwise, where the judge is grading the template's prose. The card
// says which, since a voice verdict without its subject means nothing.
func judgeTarget(card *eval.Card, fallback *eval.Artifacts, outDir string) (page, from string) {
	if card.Capability != nil && !card.Capability.Skipped {
		if page, dir := card.Capability.FirstPage(); page != "" {
			return page, dir
		}
	}
	return fallback.Digest, outDir
}

// toneRules renders the profile's own tone bullets, with the line numbers a
// reader can go and check.
func toneRules(corpus *model.Corpus) string {
	if corpus == nil || corpus.Profile == nil {
		return ""
	}
	var b strings.Builder
	for _, r := range corpus.Profile.ToneRules {
		fmt.Fprintf(&b, "- %s (%s:%d)\n", r.Text, corpus.Profile.Path, r.Line)
	}
	return b.String()
}

// aubadeBinary resolves which `aubade` the capability suite should drive.
//
// The default is the one sitting beside this binary: `make build` puts both in
// bin/, and a harness that silently graded a different build than the one just
// compiled would be the most confusing possible failure. PATH is the fallback,
// and --aubade overrides both.
func aubadeBinary(override string) string {
	if strings.TrimSpace(override) != "" {
		return override
	}
	self, err := os.Executable()
	if err != nil {
		return "aubade"
	}
	sibling := filepath.Join(filepath.Dir(self), "aubade")
	if info, err := os.Stat(sibling); err == nil && !info.IsDir() {
		return sibling
	}
	return "aubade"
}

// reportEval prints the run and decides the exit code.
func reportEval(c *cobra.Command, card *eval.Card, path string) error {
	w := c.OutOrStdout()
	passed, total := card.Regression.Score()

	if wantJSON(c) {
		payload := map[string]any{
			"ok":        card.Regression.Passed(),
			"scorecard": path,
			"today":     card.Today,
			"regression": map[string]any{
				"passed": passed,
				"total":  total,
				"mode":   card.Regression.Mode,
				"misses": failureIDs(card.Regression),
			},
			"grounding": map[string]any{
				"citations":  card.Grounding.Cited,
				"ungrounded": card.Grounding.Ungrounded,
			},
		}
		if card.Capability != nil {
			passAll, passAny, tasks := card.Capability.Rates()
			payload["capability"] = map[string]any{
				"skipped":     card.Capability.Skipped,
				"skip_reason": card.Capability.SkipReason,
				"trials":      len(card.Capability.Trials),
				"pass_all":    passAll,
				"pass_any":    passAny,
				"tasks":       tasks,
			}
		}
		if card.Sabotage != nil {
			payload["sabotage"] = map[string]any{
				"extractor": card.Sabotage.Extractor,
				"drop":      card.Sabotage.Drop(),
				"alarm":     card.Sabotage.Alarm,
			}
		}
		if card.Judge != nil {
			payload["judge"] = map[string]any{
				"grade":   card.Judge.Grade,
				"decided": card.Judge.Decided,
				"skipped": card.Judge.Skipped,
				"reason":  card.Judge.Reason,
			}
		}
		if err := writeJSON(w, payload); err != nil {
			return err
		}
		return evalExit(card)
	}

	fmt.Fprint(w, card.Markdown())
	fmt.Fprintf(w, "\nwrote %s — regression %d/%d\n", path, passed, total)
	return evalExit(card)
}

// evalExit is the harness's contract with the gate: a regression miss or a
// sabotage alarm is a non-zero exit, and nothing else is. The capability suite
// and the judge are reported and never gate (VERIFICATION.md §2).
func evalExit(card *eval.Card) error {
	if !card.Regression.Passed() {
		fails := card.Regression.Failures()
		var b strings.Builder
		fmt.Fprintf(&b, "regression suite RED: %d task(s) missed", len(fails))
		for _, f := range fails {
			fmt.Fprintf(&b, "\n  %s (%s) — %s", f.ID, f.Expected, f.Reason)
		}
		return fmt.Errorf("%s", b.String())
	}
	if card.Sabotage != nil && card.Sabotage.Alarm {
		passed, total := card.Sabotage.Broken.Score()
		return fmt.Errorf("sabotage ALARM: disabling %s did not move the score (%d/%d both ways) — the graders cannot see that extractor",
			card.Sabotage.Extractor, passed, total)
	}
	return nil
}

// failureIDs is the JSON list of missed tasks. It starts non-nil so the
// envelope carries `[]` rather than `null` on a clean run: an agent branching
// on the field should not have to handle two shapes for "nothing missed".
func failureIDs(r *eval.Result) []string {
	out := []string{}
	for _, f := range r.Failures() {
		out = append(out, f.ID)
	}
	return out
}

// evalFlags binds the harness's flag set.
//
// --data, --today and --out are named exactly as the product names them: a
// reader who knows `aubade digest --data X --out Y` already knows how to point
// the harness at the run it just produced, and an eval graded against a
// different anchor day than the digest it is reading would score a different
// morning.
func evalFlags(c *cobra.Command) {
	f := c.Flags()
	f.String("data", defaultDataDir, "corpus directory holding traps.json and the sources it was planted in")
	f.String("today", "", "anchor date, YYYY-MM-DD (must match the digest being graded)")
	f.String("out", defaultOutDir, "directory holding digest.md and signals.json; scorecard.md is written here")
	f.String("sabotage", "", "disable one extractor by name and alarm if the score does not drop")
	f.Bool("judge", false, "run the optional layer-2 model judge for voice and readability")
	f.Bool("adversarial", false, "report how each negative task stayed out: the rule, the extractor, the evidence")
	f.Bool("capability", false, "run the agentic capability suite (needs the claude CLI; skips loudly without it)")
	f.Int("trials", eval.DefaultTrials, "trials per task in the capability suite; each one is a paid model run")
	f.String("aubade", "", "the aubade binary the capability suite drives (default: the one beside this one)")
	f.Bool("json", false, "emit the run as JSON (default when an AI agent caller is detected)")
}
