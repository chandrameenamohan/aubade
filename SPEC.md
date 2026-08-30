# SPEC — aubade (Daily Digest CLI)

**Thesis:** an agent-orchestrated, deterministically-grounded morning digest whose quality
is proven by a planted-trap eval, shipped as an ox-style Go CLI that treats AI agents as
first-class callers.

Companion: `HLD.md` (architecture + rejected alternatives). Contracts here are binding
for all builders.

## Features (observable acceptance)

1. **Synthetic dataset generation (internal harness, `aubade-lab` binary — not product
   surface)** — `aubade-lab generate --seed 42 --today 2026-08-30 --out data/`
   writes `inbox.jsonl` (~500 emails, 30 days, threaded, realistic mix incl. ~30% noise),
   `calendar.ics` (RFC 5545, 30 days of meetings/declines/blocks), `notes/` (10 markdown
   meeting notes), `tasks.md` (5 tasks), `profile.md` (Avery's profile, verbatim from the
   PDF appendix), and `traps.json`. Same seed ⇒ byte-identical output. ≥12 positive traps
   spanning every extractor category; ≥4 negative traps.
2. **Deterministic toolbox** — `aubade tool <name> --json` (commitments, quiet-threads,
   conflicts, contradictions, dispatchables, suppressions, staleness, thread <id>,
   search <q>) each emit signals as JSON; every signal carries ≥1 citation
   (email id / event UID / note path). Pure: same data + same `--today` ⇒ same output.
3. **Signals audit** — `aubade signals --today D` runs all extractors, writes
   `out/signals.json`.
4. **Digest, fallback mode** — `aubade digest --no-llm` renders the one-page markdown
   digest (sections mirroring the PDF sample: one-thing-now, urgent today, decisions &
   approvals, team/product pulse, calendar & personal, honesty/staleness banner when
   warranted) from signals via templates. No network, no keys.
5. **Digest, agentic mode** — `aubade digest` (default) drives claude CLI headless with the
   toolbox as its tool surface; the model chooses tool calls (bounded turns), composes
   the page, and drafts replies with a two-layer voice: the product's built-in default
   voice (`styles/default-voice.md`, distilled Shaan Puri drafting principles) as the
   base, with the user's `profile.md` tone rules OVERRIDING it wherever they speak (for
   Avery: short, lowercase greetings, no "hope this finds you well"; investors slightly
   polished; never draft for Sam — so the graded dataset always renders in Avery's
   voice).
   Every factual line traces to a tool citation. Output shape = same section contract.
   - **Runner interface:** the orchestration loop runs on a swappable `Runner`
     (`--runner=claude` default; codex/gemini as one-shot-capable runners). The
     interface is vendor- and transport-neutral — two methods: `Ask` (one-shot
     structured question, used by consensus) and `Orchestrate` (tool-calling loop).
     SDK-backed runners (Anthropic SDK, Claude Agent SDK, OpenAI SDK, Gemini SDK — all
     of which support native tool calling) implement the same interface and register by
     name; week one ships the CLI-backed runners, but nothing in the engine knows the
     difference.
   - **Consensus at decision points, DEFAULT ON:** for bounded one-shot decisions
     (ambiguous-thread urgency; the "one thing right now" pick), aubade fans the same
     signal-grounded question to every runner detected on the machine and majority-votes.
     Runner disagreement routes the item to the "I'm not sure" section with the thread
     shown. One runner installed ⇒ single-runner silently; `--consensus=off` opts out.
     The digest footer names the runners that participated. Rationale: the user is a
     CEO — a wrong top-priority costs more than 3× model spend; quality is the default,
     frugality is the flag.
6. **Customization** — `aubade digest --customize prompt.md` reshapes the digest per the
   prompt (agentic mode only; without the flag, default format; `--customize` +
   `--no-llm` errors with a clear message). Customization touches ONLY the compose
   stage: extraction, citations, and trap detection are identical. Invariant floor: the
   honesty layer (staleness banner, contradictions, "I'm not sure") cannot be
   customized away — format is the user's, truthfulness is the product's. Eval carries
   a fixture prompt.md asserting (a) customized output differs from default and (b)
   every must-surface trap still appears in it.
7. **Honesty** — stale (>24h-old) or missing sources produce an explicit banner;
   contradictions render both sides with citations; undecidable urgency renders under
   "I'm not sure" with the thread reference. Fabricated certainty = eval failure.
8. **Eval** — designed per `docs/EVAL-PRINCIPLES.md` (binding). Vocabulary: each trap is
   a **task**; a digest run is a **trial**; `aubade-lab eval` is the **harness**. Eval,
   like generation, lives in the internal `aubade-lab` binary — the customer-facing
   `aubade` carries no harness tooling.
   - **Code graders first (#3, #1):** every positive trap asserted present (matching
     signal kind in `signals.json` AND ≥1 expected keyword in digest text), every
     negative trap asserted absent. Binary, actionable: a miss names the extractor.
   - **Regression vs capability (#15):** `--no-llm` mode is the **regression suite** —
     deterministic, 1 trial, 100% pass bar, gated in `make check`. Agentic mode is the
     **capability suite** — non-deterministic, N=3 trials per run (#10), reported as
     **pass^3** (reliability) and **pass@3** (ceiling), trials in isolated out dirs
     (#11); runs when claude CLI is present, loud SKIP otherwise. Never one number.
   - **Reference solution (#7):** a committed golden digest for the pinned seed proves
     every trap is catchable before the agent is blamed.
   - **Sabotage mode (#17):** `aubade-lab eval --sabotage=<extractor>` disables one extractor;
     the harness ALARMS if the score does not drop — proves the graders can see. Run in
     CI periodically, not in `make check`.
   - **Outputs, not paths (#6) + transcript signal (#12):** graders assert on digest +
     signals, never on which tools the agent called; the agentic transcript is saved per
     trial and gets one light check (did any tool output ground each cited fact).
   - **Judge grader, layer 2 (#2, #4, #5):** optional `--judge` pass for the one
     ungradeable-by-code axis — "reads like the PDF sample, in Avery's voice": anchored
     with worked examples, reason-before-score, with an "uncertain" escape hatch.
   - Writes `out/scorecard.md` (regression and capability sections separated); non-zero
     exit on any regression miss.
9. **Agent awareness (AX)** — via `github.com/sageox/agentx`: when an AI agent is the
   caller, errors and default output go machine-parseable (JSON), with tool-use hints;
   humans get markdown. Detection failure degrades to human mode.
10. **Scheduling design** — `aubade schedule --design` prints the written scheduling design
    (also in DESIGN.md). No implementation.

## Binding contracts

- **Email (inbox.jsonl, one per line):** `{id, thread_id, ts (RFC3339, tz), from {name,email},
  to[], cc[], subject, body, in_reply_to?, labels[]?}`
- **Signal (signals.json / tool output):** `{id, kind, priority (P0..P4), title, detail,
  citations[] ({source: email|calendar|note|task, ref}), section_hint, confidence
  (certain|unsure), deadline?}`
- **Trap (traps.json):** `{id, kind, description, must_surface (bool), expect
  {signal_kind, keywords[] (≥1 must appear in digest text)}, planted_refs[]}`
- **Dates:** generator and engine anchor to `--today` (default: system date, tz
  America/Los_Angeles).

## Non-goals

DB/persistence, UI of any kind, live Composio/Gmail wiring (interface + design only),
scheduling implementation, auth/multi-user, email sending.

## Deliberately not building yet

In-process Anthropic SDK loop (claude CLI suffices and is assignment-blessed), config
files beyond flags + profile.md, dataset mutation testing, feedback-weight loop — all
week-two: each adds surface without moving the graded axes (opinions, evaluability,
engine depth).

## End-to-end verification scenario (the gate's e2e)

`make check` ⇒ `go vet` + `go build` + `go test ./...` + then the **regression suite**:
`aubade-lab generate --seed 42 --today <fixed> --out data/` → `aubade digest --no-llm` →
`aubade-lab eval` exits 0 with all regression traps green in `out/scorecard.md`. The
capability suite (agentic, N=3 trials, pass^3/pass@3) runs when claude CLI is present —
skipped with a loud SKIP otherwise, never silently. Sabotage and judge passes are
on-demand, outside the gate (non-deterministic checks never gate).
