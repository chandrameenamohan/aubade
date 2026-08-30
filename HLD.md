# HLD — "aubade": an Agent-Experience Daily Digest CLI (myRico take-home)

*ASSIGNMENT first artifact, v2 — amended per reviewer direction: Go, ox-style CLI,
agentic orchestration, agentx-style agent awareness, connector interface (Composio as the
production provider, week two).*

## 1 · Problem framing

Avery Chen (fictional founder, mid-Series-A) needs 90-second morning triage over four
sources: ~500 emails/30 days, an `.ics` calendar, 10 meeting notes, 5 tasks. The digest
must answer three questions — *what needs me today, what am I about to drop, what can I
dispatch right now* — and be honest when sources are stale or contradictory.

What is graded (the PDF's anti-patterns): **(a)** we hold architecture opinions and
defend them, **(b)** we can *prove* the digest is good, **(c)** thick engine, thin UI,
**(d)** choices survive pushback. The centerpiece is the **eval loop**: we author the
exam (synthetic data with planted traps), the student (the engine), and the answer key
(trap assertions) — all three graded.

## 2 · Keystone decision

**An agentic CLI over a deterministic toolbox.**

`aubade` is a Go CLI in the mold of `ox`: subcommands, layered config, built for the
Agent Experience (AX) — invocable by humans *and* by AI agents as first-class callers.
The split of responsibility is the load-bearing opinion:

- **The toolbox is deterministic.** Every fact-producing operation is a pure, cited Go
  function, exposed as `aubade tool <name> --json` subcommands: commitment tracking,
  quiet-thread detection, calendar conflicts, contradictions, dispatchables,
  suppression, staleness. Same functions available as a Go library.
- **The orchestration is agentic.** `aubade digest` does not hardcode a pipeline. It hands
  the model (claude CLI, headless) the toolbox and the profile, and the model decides
  which tools to run, in what order, what to chase, and how to compose the one-pager —
  the CLI *is* the model's tool surface. Facts can only enter the digest through cited
  tool output, so the model orchestrates but cannot fabricate.
- **`--no-llm` fallback**: runs every extractor in a fixed order and renders the default
  template — full digest, zero keys, so graders can always run it cold.
- **Agent-aware (agentx).** `aubade` uses `github.com/sageox/agentx` to detect when an AI
  agent is the caller and adapts: JSON-first output, machine-parseable errors, tool-use
  hints — the AX philosophy from the reference repos.
- **Swappable runners, consensus by default.** The orchestrator runs on a `Runner`
  interface (claude CLI default; codex/gemini pluggable). At bounded one-shot decision
  points (ambiguous urgency, the "one thing now" pick) aubade fans the question to every
  runner installed and majority-votes — disagreement feeds the "I'm not sure" honesty
  section rather than a coin-flip. Digest runs at 6am on a schedule, so wall-clock is
  free; and the user is a CEO — a wrong top priority costs more than 3× model spend.
  Single runner installed ⇒ silent single-runner; `--consensus=off` is the frugal flag.
  The same mechanism gives aubade-lab its multi-judge consensus grader (EVAL-PRINCIPLES
  #14).

Why defend this split: the trap-based eval stays deterministic (traps assert against
`signals.json` and the digest text, whichever orchestrator produced them), honesty stays
enforceable (citations are structural, not stylistic), the eval discipline follows the
team's `docs/EVAL-PRINCIPLES.md` (code graders first; regression vs capability suites;
pass^k trials for the non-deterministic mode; sabotage tests so the graders provably
*see*), and the agentic layer adds judgment
— e.g. deciding a 14-message thread needs reading before ranking — without adding
hallucination surface.

## 3 · CLI surface (my design — the functions the use case demands)

Two binaries from one Go module — a hard product boundary. The product never carries
harness tooling: an end user gets `aubade`; the exam, answer key, and graders live in
`aubade-lab`, internal-only.

```
aubade — the product (human + agent callers)
  aubade digest     [--today D --customize prompt.md --no-llm --out out/]   the one-pager
  aubade tool <name> [--json]                            one deterministic extractor, cited output
        commitments | quiet-threads | conflicts | contradictions |
        dispatchables | suppressions | staleness | thread <id> | search <q>
  aubade signals    [--today D]                          run all tools, emit signals.json (audit)
  aubade schedule   --design                             print the scheduling design (doc deliverable)

aubade-lab — internal dev/eval harness (never shipped to a customer)
  aubade-lab generate [--seed N --today D --out data/]   write the synthetic dataset + traps.json
  aubade-lab eval     [--sabotage=X --judge]             trap harness; non-zero exit on regression miss
```

`thread` and `search` exist *for the agent*: read-only investigation tools so the
orchestrator can look before it ranks (the Northstar "14-message thread" case from the
sample digest).

## 4 · Architecture sketch

```
                     ┌─ DataSource interface ────────────────────────────┐
data/ (synthetic) ──▶│  LocalFS provider (ships now)                     │
                     │  Composio provider (wk-2: real Gmail/Calendar)    │
                     └───────────────┬───────────────────────────────────┘
                                     ▼
        normalize → Event model → deterministic toolbox (pure, cited Go funcs)
                                     │
              ┌──────────────────────┼───────────────────────┐
              ▼                      ▼                       ▼
     aubade tool <name>         aubade signals            aubade digest
     (agent tool surface)     (signals.json audit)    ├─ agentic: claude -p orchestrates
                                                      │  `aubade tool …` calls, composes page
                                                      └─ --no-llm: fixed order + template
                                     │
                              aubade-lab eval  ──▶ scorecard (traps present, suppressions absent)
```

agentx wraps the entry point: caller detection → output mode (human markdown / agent JSON).

## 5 · Rejected alternatives (and why)

- **One big LLM prompt over the whole corpus** — unfalsifiable quality, hallucinated
  confidence, no trap assertions. Rejected on evaluability, the graded axis.
- **Hardcoded deterministic pipeline as the *only* mode** (HLD v1) — provable but
  judgment-free; can't adapt to what the data reveals. Kept as `--no-llm`, demoted from
  keystone to fallback per reviewer direction.
- **RAG / vector store** — 500 emails fit in memory; retrieval adds a recall failure mode
  to solve a scale problem we don't have.
- **In-process Anthropic SDK tool-use loop** — more "production," but needs key handling;
  claude CLI is blessed by the assignment and already the team's runtime. Wk-2 candidate.
- **Omnigent (or any meta-harness dependency)** — alpha, Python, and it solves session
  orchestration, a bigger problem than ours. We keep its one good idea — multiple
  harnesses answering the same question — as the ~200-line consensus layer over our own
  Runner interface instead of importing a platform.
- **Web UI / Slack bot** — explicitly not wanted; CLI in, markdown out.

## 6 · Data design — writing the exam

`aubade-lab generate` is seeded and built on **scenario scripts**: each planted trap is a
first-class object emitting both its emails/events/notes *and* its `traps.json` entry, so
the answer key can never drift from the data. Around traps: realistic filler (~30%
newsletters/marketing, internal chatter, customer threads, recruiter spam) over a 30-day
distribution anchored to `--today` (dataset stays evergreen). ~12–15 positive traps
covering every tool, plus 4–5 **negative traps** (must NOT surface) so we prove
suppression, not just recall.

## 7 · Failure modes

- **No claude CLI / no key** → `--no-llm`; identical facts, template prose.
- **Agent orchestrator goes off-script** → facts only enter via cited tool output; eval
  catches missed traps; a max-turns budget bounds the loop.
- **Contradictory sources** → both surfaced with citations; never auto-resolved.
- **Malformed/missing source** → loader validates; digest opens with an explicit
  stale/missing banner rather than silently thinning.
- **agentx detects nothing** → default humane markdown; detection is progressive
  enhancement, never a requirement.

## 8 · Challenge pushback (Step 0.5, honest version)

- **Load-bearing assumption:** an agentic orchestrator over deterministic tools catches
  everything the fixed pipeline would (eval enforces this — both modes must pass the same
  trap harness; that dual-mode bar is the defense).
- **What I will NOT build:** persistence/DB, config beyond profile.md + flags, scheduling
  *implementation* (design doc only, as asked), any UI, live Composio wiring.
- **Strongest objection to my own design:** two orchestration modes is more surface than
  one week strictly needs. Answer: the fallback is ~50 lines over the same toolbox, and
  it's what makes the agentic mode *testable* — the diff between modes is itself signal.

## 9 · Second week

Composio provider behind DataSource (real Gmail/Calendar, same Event model); in-process
tool-use loop via the Go Anthropic SDK; scheduled delivery implemented (GH Actions cron
05:45 PT + SMTP) per the design doc; feedback loop (mark lines useful/noise → scoring
weights); dataset mutation testing (perturb dates/senders, traps must still be caught);
graph-agent integration — `aubade mcp serve` exposing the toolbox as an MCP server (one
bridge, every MCP-aware framework: LangGraph, Claude, IDE agents), and optionally a
graph-backed Runner that replaces the free-form loop with explicit
extract→investigate→rank→compose states.

## 10 · Scheduling design (written deliverable, summary)

Recommend **GitHub Actions cron** (05:45 PT) running `aubade digest` against the connector,
committing `out/digest.md` + optional SMTP send: no laptop-asleep problem, secrets in
Actions, idempotent by date-stamped output. Alternatives discussed in DESIGN.md: local
cron/launchd (zero infra, sleeps), cloud function (overkill at n=1).

## 11 · Ten-sentence compression (the head-sized map)

We are building an exam, a student, and an answer key. The exam is a seeded synthetic
30-day dataset whose traps are scenario scripts that emit their own answer key. The
student is `aubade`, a Go CLI in the ox mold: a deterministic, cited toolbox that an agent
orchestrates. The keystone decision is that split — the model picks which tools to run
and composes the page, but facts can only enter through cited tool output. `--no-llm`
runs the same toolbox in fixed order, so graders need no keys and both modes must pass
the same eval. agentx makes the CLI agent-aware: it detects AI callers and serves them
JSON-first. The genuinely hard part is the commitment tracker and quiet-thread detector —
"about to drop" means modeling what *didn't* happen. Eval is deterministic: every planted
trap asserted present, every suppression asserted absent, non-zero exit on a miss. The
gate wraps build + unit tests + that end-to-end eval, proven to block before any builder
runs. Data access hides behind a DataSource interface — LocalFS now, Composio is week
two's real-Gmail provider.
