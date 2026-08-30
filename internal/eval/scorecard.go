package eval

import (
	"fmt"
	"strings"
)

// The scorecard is the harness's only output that a person reads, so it is
// written for the person who has to act on it rather than for a dashboard.
//
// Three rules it is written to:
//
//   - **The two suites never touch.** Regression and capability get their own
//     sections, their own bars and their own vocabulary, because averaging
//     "still works?" with "what's new?" produces a number that answers neither
//     (EVAL-PRINCIPLES #15).
//   - **A failure names what to change.** Every failed row carries the reason —
//     which extractor missed it, or whether it was extracted and then lost in
//     the render. Those are two different bugs and the card must not blur them
//     (#1).
//   - **A skip is as loud as a failure.** A capability suite that did not run
//     says so in a banner. The alternative is a card that reads like a pass to
//     anyone skimming it, which is how an unverified claim becomes a cited one.

// ScorecardFile is the card `aubade-lab eval` writes, relative to --out.
const ScorecardFile = "scorecard.md"

// Card is everything one eval run has to report.
type Card struct {
	// DataDir and Today say which exam this was.
	DataDir string
	Today   string

	// Regression is the gated suite. Always present.
	Regression *Result

	// Grounding is the transcript check over the regression page.
	Grounding Grounding

	// Capability, Sabotage, Judge and Adversarial are the on-demand passes, nil
	// when they did not run at all. A pass that ran and skipped is non-nil and
	// Skipped — a different thing, and it prints differently.
	Capability  *Capability
	Sabotage    *Sabotage
	Judge       *Judgment
	Adversarial *Adversarial

	// Negatives turns on the detailed report of how each negative task stayed
	// out.
	Negatives bool
}

// Markdown renders the whole card.
func (c *Card) Markdown() string {
	var b strings.Builder

	b.WriteString("# aubade eval scorecard\n\n")
	fmt.Fprintf(&b, "Corpus `%s`, anchored on %s. Graded on outputs — the page and the fact base it\n", c.DataDir, c.Today)
	b.WriteString("was composed from — never on which tools were called.\n\n")

	c.writeRegression(&b)
	c.writeGrounding(&b)
	c.writeCapability(&b)
	c.writeAdversarial(&b)
	c.writeSabotage(&b)
	c.writeJudge(&b)

	b.WriteString("\n---\n")
	b.WriteString("*A task is one planted trap; a trial is one digest run. A positive task passes\n")
	b.WriteString("when a signal cites its planted evidence and the page says one of its keywords.\n")
	b.WriteString("A negative task passes when no signal claims it — absence is graded on signals\n")
	b.WriteString("rather than on words, because the page quotes the very rule that suppressed an\n")
	b.WriteString("item and lists the meeting it was told not to make a fuss about.*\n")
	return b.String()
}

func (c *Card) writeRegression(b *strings.Builder) {
	b.WriteString("## Regression suite — `aubade digest --no-llm`\n\n")
	b.WriteString("Deterministic: one trial, a 100% bar, gated by `make check`. A miss here is a\n")
	b.WriteString("regression, not a data point.\n\n")

	if c.Regression == nil {
		b.WriteString("**No regression result.** Nothing was graded.\n\n")
		return
	}
	passed, total := c.Regression.Score()
	verdict := "GREEN"
	if passed != total {
		verdict = "RED"
	}
	fmt.Fprintf(b, "**%s — %d/%d tasks passed** (mode: %s)\n\n", verdict, passed, total, c.Regression.Mode)

	writeTable(b, c.Regression.Traps)
	b.WriteString("\n")

	if fails := c.Regression.Failures(); len(fails) > 0 {
		b.WriteString("\n### What to fix\n\n")
		for _, f := range fails {
			fmt.Fprintf(b, "- **%s** (%s, expects `%s`) — %s\n", f.ID, f.Kind, f.Expected, f.Reason)
		}
		b.WriteString("\n")
	}

	if c.Negatives {
		c.writeNegatives(b)
	}
}

// writeNegatives spells out how each negative task stayed out.
//
// The pass/fail bar does not move: a negative task passes when nothing claimed
// it. What this adds is the *reason*, and the reason is the interesting part —
// "held back on the user's own rule" is the suppression layer working, while
// "no extractor claimed it" is a pass the engine got for free and would keep
// getting if the rule were deleted tomorrow.
func (c *Card) writeNegatives(b *strings.Builder) {
	b.WriteString("\n### Negative half — how each suppressed task stayed out\n\n")
	b.WriteString("| Task | Owning extractor | Held back by | Evidence |\n|---|---|---|---|\n")
	for _, r := range c.Regression.Traps {
		if r.MustSurface {
			continue
		}
		held := "nothing — it simply never matched"
		if r.Suppressed {
			held = "profile rule: " + cell(r.Rule)
		}
		fmt.Fprintf(b, "| `%s` | `%s` | %s | %s |\n", r.ID, r.Expected, held, cell(r.Reason))
	}
	b.WriteString("\n")
}

func (c *Card) writeGrounding(b *strings.Builder) {
	b.WriteString("## Citation grounding\n\n")
	g := c.Grounding
	switch {
	case g.Cited == 0:
		b.WriteString("**No citations on the page.** Every line on it is unverifiable, which is the\n")
		b.WriteString("one failure this architecture exists to prevent.\n\n")
	case len(g.Ungrounded) == 0:
		fmt.Fprintf(b, "All %d citations on the page resolve to a citation in the same run's `signals.json`.\n\n", g.Cited)
	default:
		fmt.Fprintf(b, "**%d of %d citations are not in the fact base:**\n\n", len(g.Ungrounded), g.Cited)
		for _, u := range g.Ungrounded {
			fmt.Fprintf(b, "- `%s`\n", u)
		}
		b.WriteString("\n")
	}
}

func (c *Card) writeCapability(b *strings.Builder) {
	b.WriteString("## Capability suite — `aubade digest` (agentic)\n\n")
	if c.Capability == nil {
		b.WriteString("Not run. Pass `--capability` to run it, or run `make check-agentic` for the\n")
		b.WriteString("live end-to-end check.\n\n")
		return
	}
	if c.Capability.Skipped {
		b.WriteString("> **SKIPPED — the agentic digest is unverified on this machine.**\n>\n")
		fmt.Fprintf(b, "> %s\n>\n", c.Capability.SkipReason)
		b.WriteString("> This is a skip, not a pass. Nothing below was measured.\n\n")
		return
	}

	passAll, passAny, tasks := c.Capability.Rates()
	n := len(c.Capability.Trials)
	fmt.Fprintf(b, "N=%d isolated trials, one output directory each. Non-deterministic, so it never\n", n)
	b.WriteString("gates.\n\n")
	fmt.Fprintf(b, "**pass^%d: %d/%d tasks** (passed every trial — reliability)  \n", n, passAll, tasks)
	fmt.Fprintf(b, "**pass@%d: %d/%d tasks** (passed at least one trial — ceiling)\n\n", n, passAny, tasks)

	b.WriteString("| Task | Must surface | Passed | pass^N | pass@N |\n|---|---|---|---|---|\n")
	for _, a := range c.Capability.Aggregates() {
		fmt.Fprintf(b, "| `%s` | %s | %d/%d | %s | %s |\n",
			a.Trap.ID, yesNo(a.Trap.MustSurface), a.Passed, a.Trials, tick(a.PassAll), tick(a.PassAny))
	}
	b.WriteString("\n")

	b.WriteString("### Trials\n\n")
	for _, tr := range c.Capability.Trials {
		passed, total := 0, 0
		if tr.Result != nil {
			passed, total = tr.Result.Score()
		}
		fmt.Fprintf(b, "- **trial %d** (`%s`): %d/%d", tr.N, tr.Dir, passed, total)
		if tr.Result != nil && tr.Result.Mode != "" {
			fmt.Fprintf(b, ", mode %s", tr.Result.Mode)
		}
		if tr.Grounding.Cited > 0 {
			fmt.Fprintf(b, ", %d citations (%d ungrounded)", tr.Grounding.Cited, len(tr.Grounding.Ungrounded))
		}
		if tr.Grounding.ToolCalls >= 0 {
			fmt.Fprintf(b, ", %d toolbox call(s) in the transcript", tr.Grounding.ToolCalls)
		}
		if tr.Err != nil {
			fmt.Fprintf(b, "\n  - **did not produce a page:** %v", tr.Err)
			if tr.Log != "" {
				fmt.Fprintf(b, "\n  - `%s`", cell(tr.Log))
			}
		}
		for _, u := range tr.Grounding.Ungrounded {
			fmt.Fprintf(b, "\n  - **ungrounded citation:** `%s`", u)
		}
		b.WriteString("\n")
	}
	b.WriteString("\n")
}

// writeAdversarial reports the traps this repository did not write.
//
// It is a capability section and it says so twice — in the banner and in the
// exit-code note — because a "2/3 caught" line sitting under a green regression
// table is exactly the shape of number someone quotes as a failure. The tasks
// did not exist before this run, they will be different next run, and a miss is
// coverage news rather than a regression.
func (c *Card) writeAdversarial(b *strings.Builder) {
	if c.Adversarial == nil {
		return
	}
	a := c.Adversarial
	b.WriteString("## Adversarial suite — traps we did not write\n\n")
	b.WriteString("A model is shown the corpus, the profile and the existing catalog, and asked\n")
	b.WriteString("for situations that are *not* in it. The new tasks are injected into a copy of\n")
	b.WriteString("the dataset — the original is never written to — and the deterministic harness\n")
	b.WriteString("is re-run over the copy. Non-deterministic by construction, so a miss here is\n")
	b.WriteString("coverage news and never an exit code.\n\n")

	if a.Skipped {
		b.WriteString("> **SKIPPED — no new traps were authored.**\n>\n")
		fmt.Fprintf(b, "> %s\n>\n", a.SkipReason)
		b.WriteString("> This is a skip, not a pass. Nothing below was measured.\n\n")
		return
	}
	fmt.Fprintf(b, "Author: `%s`, asked for %d scenario(s) over %d attempt(s).\n\n", a.Author, a.Want, a.Attempts)

	if a.Err != nil {
		fmt.Fprintf(b, "**The run produced no grade:** %s\n\n", cell(a.Err.Error()))
		writeRejections(b, a.Rejections)
		return
	}

	caught, total := a.Caught()
	fmt.Fprintf(b, "**%d/%d authored tasks caught.** Injected copy: `%s`\n\n", caught, total, a.Dir)

	b.WriteString("| Task | Kind | Must surface | Expected | Caught | Evidence |\n")
	b.WriteString("|---|---|---|---|---|---|\n")
	for _, r := range a.Result.Traps {
		fmt.Fprintf(b, "| `%s` | %s | %s | `%s` | %s | %s |\n",
			r.ID, r.Kind, yesNo(r.MustSurface), r.Expected, caughtMissed(r.Passed), cell(r.Reason))
	}
	b.WriteString("\n")

	for _, t := range a.Traps {
		fmt.Fprintf(b, "- **%s** — %s\n", t.ID, cell(t.Description))
	}
	b.WriteString("\n")

	writeControl(b, a.Control)
	writeRejections(b, a.Rejections)
}

// writeControl reports the original answer key re-graded over the injected
// page. It is the check that keeps a miss honest: if injecting three new
// scenarios knocked over a planted trap, the corpus moved under the exam and
// the adversarial numbers are about the injection rather than about the engine.
func writeControl(b *strings.Builder, control *Result) {
	if control == nil {
		return
	}
	passed, total := control.Score()
	if passed == total {
		fmt.Fprintf(b, "Control: the %d planted tasks still pass over the injected corpus, so the new\ntasks are the only thing that changed.\n\n", total)
		return
	}
	fmt.Fprintf(b, "**Control: %d/%d planted tasks still pass over the injected corpus.** The\ninjection disturbed the exam, so read the misses above with that in mind:\n\n", passed, total)
	for _, f := range control.Failures() {
		fmt.Fprintf(b, "- `%s` — %s\n", f.ID, cell(f.Reason))
	}
	b.WriteString("\n")
}

// writeRejections lists what the contract threw out. A rejected scenario is a
// finding about the schema and the prompt rather than about the engine, and it
// is the first thing to read when the suite authored nothing.
func writeRejections(b *strings.Builder, rejections []Rejection) {
	if len(rejections) == 0 {
		return
	}
	b.WriteString("### Rejected by the contract\n\n")
	for _, r := range rejections {
		fmt.Fprintf(b, "- attempt %d, `%s` — %s\n", r.Attempt, r.ID, cell(r.Reason))
	}
	b.WriteString("\n")
}

func (c *Card) writeSabotage(b *strings.Builder) {
	if c.Sabotage == nil {
		return
	}
	s := c.Sabotage
	b.WriteString("## Sabotage — can the graders see?\n\n")
	basePassed, baseTotal := s.Baseline.Score()
	brokePassed, _ := s.Broken.Score()

	fmt.Fprintf(b, "Extractor `%s` disabled. Baseline %d/%d → sabotaged %d/%d (drop: %d).\n\n",
		s.Extractor, basePassed, baseTotal, brokePassed, baseTotal, s.Drop())

	if s.Alarm {
		b.WriteString("> **ALARM — the score did not drop.**\n>\n")
		fmt.Fprintf(b, "> Disabling `%s` changed nothing the graders can see, so the tasks behind it\n", s.Extractor)
		b.WriteString("> are being scored by something else. This suite would stay green through a\n")
		b.WriteString("> total failure of that extractor.\n\n")
		for _, blind := range s.Blind {
			fmt.Fprintf(b, "- %s\n", blind)
		}
		b.WriteString("\n")
		return
	}

	b.WriteString("The score fell, so the graders read this extractor. Tasks it took with it:\n\n")
	for _, f := range s.Broken.Failures() {
		if r, ok := s.Baseline.Get(f.ID); ok && !r.Passed {
			continue // already failing before the sabotage; not this run's news
		}
		fmt.Fprintf(b, "- `%s` — %s\n", f.ID, f.Reason)
	}
	b.WriteString("\n")
}

func (c *Card) writeJudge(b *strings.Builder) {
	if c.Judge == nil {
		return
	}
	j := c.Judge
	b.WriteString("## Judge — does it read like the sample, in her voice?\n\n")
	b.WriteString("Layer 2, on demand, never gating. Anchored with worked examples,\n")
	b.WriteString("reason-before-score, with `uncertain` as a first-class answer.\n\n")
	if j.Judged != "" {
		fmt.Fprintf(b, "Judged the page in `%s`.\n\n", j.Judged)
	}

	if j.Skipped {
		fmt.Fprintf(b, "> **SKIPPED.** %s\n\n", j.SkipReason)
		return
	}
	fmt.Fprintf(b, "**%s** — %s\n\n", j.Grade, j.Reason)
	for _, n := range j.Notes {
		fmt.Fprintf(b, "- %s\n", cell(n))
	}
	b.WriteString("\n")
}

// writeTable renders the per-task rows.
//
// The evidence column is not optional. A row that says only PASS or FAIL is the
// vanity version of this table: the whole value is in "surfaced by quiet-threads
// where the key expected commitments" and "surfaced in signals.json and lost in
// the render".
func writeTable(b *strings.Builder, rows []TrapResult) {
	b.WriteString("| Task | Kind | Must surface | Expected | Verdict | Evidence |\n")
	b.WriteString("|---|---|---|---|---|---|\n")
	for _, r := range rows {
		fmt.Fprintf(b, "| `%s` | %s | %s | `%s` | %s | %s |\n",
			r.ID, r.Kind, yesNo(r.MustSurface), r.Expected, verdict(r.Passed), cell(r.Reason))
	}
}

func verdict(passed bool) string {
	if passed {
		return "PASS"
	}
	return "**FAIL**"
}

// caughtMissed is the adversarial table's verdict column. It deliberately does
// not say PASS/FAIL: those words belong to a suite with a bar, and this one has
// none.
func caughtMissed(passed bool) string {
	if passed {
		return "caught"
	}
	return "**missed**"
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func tick(b bool) string {
	if b {
		return "yes"
	}
	return "—"
}

// cell makes a string safe to sit in a markdown table cell: no pipes, no
// newlines, and short enough to read.
func cell(s string) string {
	s = strings.ReplaceAll(s, "|", "\\|")
	s = strings.Join(strings.Fields(s), " ")
	return clip(s, 220)
}
