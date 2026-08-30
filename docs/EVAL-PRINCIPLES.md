# Eval Principles

A working set of principles for designing evals that produce **actionable**
signal — not vibes, not vanity numbers.

Sources:
- *Evals for taste: Hill-climbing a slide-generation agent* (Anthropic talk)
- *Demystifying Evals for AI Agents* ([Anthropic engineering blog](https://www.anthropic.com/engineering/demystifying-evals-for-ai-agents))

Hindi-language notes are in `EVAL-PRINCIPLES.hi.md` (gitignored).

---

## Shared vocabulary

Talk to evals precisely or you'll talk past each other.

| Term | Meaning |
|---|---|
| **Task** | One test case — defined input + success criteria. |
| **Trial** | One run of a task. Multiple trials per task to account for non-determinism. |
| **Grader** | The logic that scores a trial. A task can have several. |
| **Outcome** | The final environmental state after a trial (the produced artifact). |
| **Transcript** | The full trace — tool calls, reasoning, intermediate steps. |
| **Harness** | The infrastructure that runs trials, records, grades, aggregates. |
| **Suite** | A collection of tasks measuring a capability area. |

---

## Designing graders

1. **Every grader must be actionable.** If the score doesn't tell you *what to change*, cut the grader.
2. **Reason before score.** LLMs are autoregressive — score-first prompts rationalize their pick. Force pros/cons reasoning *before* the final tag.
3. **Code graders vs. model graders.** Countable → code (fast, deterministic, brittle). Taste → judge (nuanced, expensive, non-deterministic). Use both.
4. **Anchor your judges.** A bare "rate 0–5" lets the judge float. Inline worked examples for each level so the model has a calibration target.
5. **Give judges an escape hatch.** Forced confidence on edge cases breeds bad judgments. Allow the judge to abstain ("unknown" / "uncertain") so it doesn't fabricate a verdict.
6. **Grade outputs, not paths.** Agents find valid approaches you didn't anticipate. Pinning tool-call sequences makes evals brittle and penalizes creativity.

## Designing tasks

7. **Reference solutions prove tasks are solvable.** Each task should ship with a known-good output. 0% pass on a task often means the task is broken, not that the agent is weak.
8. **Class balance — test occurrence and non-occurrence.** Test "should succeed" *and* "should refuse." One-sided eval sets push the agent into one-sided behavior.
9. **Unambiguous specs.** Two reviewers given the task should reach the same pass/fail verdict independently.

## Running evals

10. **Multiple trials per task — pass@k vs pass^k.** Single-run completion rates lie on non-deterministic systems. Run each task N times. Report **pass^N** (all trials passed → reliability) AND **pass@N** (at least one passed → capability ceiling).
11. **Isolated environments per trial.** Shared state contaminates non-determinism measurements and lets one trial's artifacts leak into the next.
12. **Grade outcome AND transcript.** Outcome tells you *what was produced*. Transcript tells you *how* — did the agent self-correct, skip a check, take a brittle shortcut? Two different signals.

## Judge techniques

13. **Pairwise comparison is underrated.** When "good" is fuzzy, drop absolute scoring. Show the judge two outputs, ask which is better and why. Win-rates track improvement without saturating.
14. **Multi-judge consensus for high-stakes graders.** Three judges, majority wins. Spends compute to buy determinism.

## Eval health

15. **Capability evals vs regression evals — distinct purposes.** Regression = "still works?" — pass bar 100%, alarm on any drop. Capability = "what's new?" — start low, hill-climb. Don't mix into one number.
16. **Evals saturate.** Same score every run means the eval has stopped *measuring* and started *certifying*. Evals are a living artifact, not ground truth.
17. **Detect saturation with a regression test.** Deliberately break the agent. If the score doesn't drop, your judge is blind.
18. **Read transcripts regularly.** "You won't know if your graders are working unless you read the transcripts." Manual review every week catches what aggregate metrics miss.

## Process

19. **Design graders from observed failure modes.** Run the agent, inspect outputs, list specific failures, *then* encode each as a grader. Top-down design produces evals that miss what's actually broken.
20. **Adversarial critic agents beat passive review.** Instruct the critic: "Assume there are problems. Find them. This is a bug hunt, not a confirmation step."
21. **Eval-writing is a forcing function for the spec.** If you can't write the eval, you don't understand what success looks like.
22. **Convert vibes into eval cases.** "Something feels off" is a signal, not actionable. Press for a specific example, then make it a permanent case.
23. **Eval-driven development.** Define the eval for a planned capability *before* the agent has it. Shape what you're building.

## Strategic

24. **Generic benchmarks don't measure your product.** SWE-bench, ARC-AGI, terminal-bench say nothing about your specific agent on your specific task. Build your own.

---

## Order of attack (cheap → expensive)

1. Code graders first — cover everything countable.
2. Anchored judge graders for nuance — reason-before-score in the prompt.
3. Pairwise when absolute scores saturate.
4. Critic / QA loop when judges miss qualitative failures.
5. Multi-judge consensus for high-stakes / high-variance graders.

## Saturation playbook

**Three detectors:**
1. **Variance check.** Same eval, five runs. Scores within ±0.1 = judge is stuck.
2. **Regression test.** Sabotage the agent. Score doesn't fall = judge is blind.
3. **Human spot-check.** Sample 30% of judge calls. Disagreement > 15% = prompt needs work.

**Five resolutions (cheap → expensive):**
1. Tighten anchors.
2. Make the rubric finer.
3. Switch to pairwise.
4. Add harder cases.
5. Upgrade the judge model.

---

## How this maps onto `ai-reel-agent`

| Principle | Where in the repo |
|---|---|
| #1 Actionable | Receipt grading is binary: `source.includes(source_span)`. Failure → fabrication. Directly actionable. |
| #2 Reason before score | `evals/fidelity-runner.ts:81` — judge prompt emits `reason` first, then `best_tag`. |
| #3 Code + judge | Layer 1 (`String.includes`) catches structural fabrication. Layer 2 (Sonnet judge) catches honest-tag drift. Both run on every eval. |
| #4 Anchors | Judge prompt embeds six worked examples — one per tag, mined from real prior failures. |
| #5 Escape hatch | Not yet wired. Judge prompt forces one of 4 tags. **Gap:** add "uncertain" for edge cases. |
| #10 Trials + pass^k | **Big gap.** Currently each URL runs once. Opus is non-deterministic; single-run completion rates overstate reliability. Should run N=3 and report pass^3. |
| #11 Isolated trials | Each run uses a unique `output/<slug>/` dir; subprocess isolation is fine. `template-reel/public/brand/` cache is a minor shared-state concern. |
| #12 Outcome + transcript | We grade `plan.json` + `reel.mp4` (outcome). `log.txt` (transcript) exists but is barely graded. **Gap:** add transcript checks (did agent call check-receipts? self-correct on failure?). |
| #13 Pairwise | Not wired. Opportunity for prompt-variant A/B testing. |
| #15 Capability vs Regression | **Gap.** Happy-path URLs (6) are implicitly regression (passing). Ambiguity/failure cases are implicitly capability. Should split sections in `EVAL-URLS.txt` and report rates separately. |
| #16/#17 Saturation | `evals/fidelity-runner.ts --sabotage=<section>` strips a named H1 from `src/system-prompt.md` before invoking the agent (via `REEL_SABOTAGE_SECTION` env passed to the child process — see `src/claude.ts:stripSection`). The runner compares this run's **scope-honesty degradation rate** (scope_creep+confabulated per rendered reel) against a **matched baseline** — the same slugs in a prior clean run (`--baseline-dir`, or most-recent on disk). Summary emits `sabotage.alarm=true` if delta < 0.15. **Why scope-level, not receipt-level:** an earlier receipt-fabrication metric cried wolf — the baseline was contaminated by URL-attribution false-positives, and the sabotage's real harm surfaces as framing-level confabulation, not broken receipts. **Finding (2026-05-28):** the judge IS sensitive (run `20260528-074816`: sabotage flipped a baseline-honest reel to `confabulated` — "Claude clicks through the app", not in source — and the judge caught it). But removing `# Receipts` alone is a *weak* sabotage: honesty is over-determined (schema receipts field + 2 worked examples + Voice all reinforce it), so a trials=1 run often shows 0 degradation (`--sabotage` warns below trials=3). A reliable trip-wire needs a stronger multi-section sabotage. **Not for routine runs.** |
| #19 Failure-mode-first | The graceful-failure cases (404, 500, DNS-failure) were added *after* observing the agent's behavior on degenerate inputs. |
| #22 Vibes → cases | The promo URL started as a vibes-level worry ("agent might fabricate non-shipped features"); now a permanent eval fixture with its own classification path. |
