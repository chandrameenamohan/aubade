# aubade

An agent-orchestrated morning digest, grounded in a deterministic cited toolbox,
and proven by a planted-trap eval. Four sources in — ~500 emails over 30 days, an
`.ics` calendar, meeting notes, tasks — one page out: *what needs me today, what
am I about to drop, what can I dispatch right now*, and an honest banner whenever
a source is stale, missing, or contradicts another.

CLI in, markdown out. Humans and AI agents are both first-class callers.

## Run everything in five minutes — no API key

Go 1.26+ is the only requirement. **Nothing in this section needs an API key, a
model, or any network beyond the Go module download.**

```bash
git clone https://github.com/chandrameenamohan/aubade && cd aubade
make check
```

`make check` is the whole proof in one command (~10 seconds): `go vet`,
`go build`, the unit tests, and then the end-to-end regression — it generates the
seeded corpus, composes the digest with `--no-llm`, grades it against the answer
key that corpus shipped with, and exits non-zero on a single missed trap.

To watch the same thing step by step and read the artifacts:

```bash
make build                       # → ./bin/aubade and ./bin/aubade-lab

./bin/aubade-lab generate --seed 42 --today 2026-08-30 --out data/
#   → data/{inbox.jsonl, calendar.ics, notes/, tasks.md, profile.md, traps.json}
#     14 traps that must surface, 6 that must not. Same seed ⇒ byte-identical.
#     A committed copy of this exact dataset is browsable at datasets/seed-42/
#     (byte-identical to what the command above regenerates — that's the determinism claim).

./bin/aubade digest --no-llm --today 2026-08-30    # ← the zero-key path
#   → out/digest.md, plus the out/signals.json it was composed from

./bin/aubade-lab eval --today 2026-08-30
#   → out/scorecard.md — every trap, PASS/FAIL, and which extractor owes a miss
```

`--no-llm` is not a toy mode: it runs the same seven extractors over the same
corpus and renders the full page from templates. It is what the gate runs, and it
is the answer to "can I try this without credentials". If you are going to commit
anything here, run `./scripts/install-hooks.sh` once — that is the pre-commit
gate, and it runs the same `make check`.

## With a model runner (optional)

The default mode is agentic: aubade hands the toolbox to a runner (claude CLI),
which decides what to chase and composes the page. Every citation on what comes
back is checked against `signals.json` first — a page citing anything else is
rejected whole, and the deterministic composer writes the digest instead, saying
so in the page, the footer and on stderr.

```bash
./bin/aubade digest --today 2026-08-30           # agentic; consensus on
echo "Write it as one markdown table: When, What, Who." > prompt.md
./bin/aubade digest --today 2026-08-30 --customize prompt.md
./bin/aubade-lab eval --capability               # N=3 trials, pass^3 / pass@3
./bin/aubade-lab eval --sabotage=conflicts       # can the graders still see?
make check-agentic                               # the live end-to-end
```

`--customize` reshapes the compose stage and nothing else: the staleness banner,
contradictions and "I'm not sure" are appended by aubade from the signals after
the composer is done. Format is the user's; truthfulness is the product's.
Everything in this section skips **loudly** when the claude CLI is absent — a
skip nobody sees is indistinguishable from a pass.

## Consensus — every live runner votes, and disagreement is reported, not resolved

Consensus is **on by default**; `--consensus=off` is the frugal flag, not the
other way round. The digest runs on a schedule at 06:00, wall-clock is free, and
a wrong top priority costs more than three times the model spend.

At exactly two bounded decision points — *ambiguous-thread urgency* (items the
deterministic toolbox marked `unsure`) and the *"one thing right now"* pick —
aubade fans one byte-identical, schema-constrained question to every runner that
answered a real liveness probe (presence on PATH proves nothing; the probe is a
cheap capped call), then majority-votes the JSON answers. Three rules, each from
a finding rather than a preference:

1. **Only live runners count.** A runner that errors or breaks the schema is
   dropped, never tallied as a dissent — a 401 is not an opinion.
2. **A strict majority decides.** One live runner means single-runner, silently.
3. **No majority is an answer, not a coin flip.** A split routes the item to
   "I'm not sure" with its thread shown and the tally stated.

The vote is auditable in the digest footer, which names who voted, who was dead
or absent, and what each decision concluded. From a real two-voter run
(verified 2026-08-31, claude + codex both live):

> *Consensus on (runners — answered: claude, codex): cadence:t-northstar: not
> urgent today (all 2 runners agreed); … one thing now: m-capt-04 (all 2
> runners agreed).*

Degradation was exercised, not assumed: with codex removed from PATH the same
command exits 0 and the footer says so —

> *Consensus on (runners — answered: claude; codex not installed; a single
> runner, so consensus is a formality this morning): … (claude answered alone).*

`signals.json` on disk stays the toolbox's own answer — votes are applied to a
*composed* copy — so a grader can always tell what the extractors said from what
the runners made of it. Registered voters are `claude` and `codex`
(`internal/runner/registry.go`); `--runner=<anything else>` fails fast with the
menu: `unknown runner "cursor"; one of: claude, codex`.

## The toolbox (also the agent's tool surface)

```bash
./bin/aubade tool commitments --json      # promises owed, and whether kept
./bin/aubade tool quiet-threads           # threads gone silent still owing a reply
./bin/aubade tool thread t-cap-table      # read a thread before ranking it
./bin/aubade tool search "cap table"
./bin/aubade signals --today 2026-08-30   # every extractor → out/signals.json
./bin/aubade schedule --design            # the scheduling design (also DESIGN.md)
```

Extractors: `commitments`, `quiet-threads`, `conflicts`, `contradictions`,
`dispatchables`, `suppressions`, `staleness`, plus `thread <id>` and
`search <q>`. Each signal carries at least one citation, and the same corpus with
the same `--today` always produces the same output — that purity is what makes
the trap eval possible. When an AI agent is the caller, these commands emit JSON
with no `--json` flag, because detection already established who is asking.

## Works with other agent CLIs (tested)

The agent-caller claim above is tested, not aspirational — verified 2026-08-31
against Cursor's CLI (`cursor-agent`) and OpenAI's Codex CLI. Reproduce each in
under a minute (after `make build` and a `generate`):

```bash
# Detection alone: an agent env marker flips output to JSON — no flag needed
CURSOR_AGENT=1 ./bin/aubade tool suppressions | head -3

# Cursor drives the toolbox headlessly
cursor-agent -p "Run ./bin/aubade tool commitments --json in this directory and report the signal count"

# Codex drives it (its read-only sandbox is fine — the tools only read data/)
codex exec --skip-git-repo-check "Run ./bin/aubade tool commitments --json and report the signal count"
```

Observed: both CLIs were auto-detected and served the JSON envelope with no
`--json` flag, and both read the tools' output correctly (signal counts and
citations quoted back). Codex is additionally a registered consensus voter
(`--runner` knows `claude` and `codex`); a Cursor voter is a one-file adapter
against the same Runner interface — a natural next addition, not yet shipped.

## How this product is built

The process is as deliberate as the code, and it is visible in the repo:

- **Issue tracking is [beads](https://github.com/steveyegge/beads) (`bd`),
  backed by DoltDB.** The work graph lives in `.beads/` — a Dolt database
  (`.beads/dolt/`), versioned like the code it tracks. The whole build is four
  epics with dependency edges: **A** Foundation & harness (scaffold,
  `make check` gate proven red/green, learning tests against the real claude
  and codex CLIs), **B** The exam (synthetic dataset + trap catalog, born
  together with its answer key), **C** The student (deterministic toolbox,
  `--no-llm` renderer, agentic loop + consensus), **D** Answer key & delivery
  (eval harness, docs, craft pass). `bd list --all` shows the graph; every task
  closed against the gate, not against a feeling.
- **Verification precedes features.** `make check` (vet, build, unit tests,
  end-to-end trap regression) existed before the engine did, and the pre-commit
  hook runs the same command — there is no path to a commit that skips it.
- **Assumptions about external CLIs are asserted, not believed.**
  `learning-tests/` pins what claude and codex actually do headlessly; the
  gemini runner is *absent* from the registry precisely because it was never
  probed against a real binary.

### Contributing

1. `git clone` … `cd aubade && make check` — green in ~10 seconds, no API key.
2. `./scripts/install-hooks.sh` once. This is the pre-commit gate; a PR that
   fails `make check` is not reviewable, it is red.
3. Track your work in beads: `bd list --all` for the graph,
   `bd create` for a new issue, close it when the gate is green. The Dolt-backed
   database in `.beads/` travels with the repo.
4. Respect the two load-bearing invariants: extractors stay **pure** (same
   corpus + same `--today` ⇒ byte-identical signals — the trap eval depends on
   it), and any change to trap behaviour updates the answer key in the same
   commit (`aubade-lab eval` is the judge).
5. Adding a model runner is a one-file adapter against the `Runner` interface
   plus a `Register` call in `internal/runner/registry.go` — but only after a
   learning test against the real binary. An unprobed runner is not a voter.
6. Model-dependent tests skip **loudly** when a CLI is absent; if your change
   touches the agentic path, run `make check-agentic` with a live runner before
   the PR.

## Where to read next

| File | What it answers |
|---|---|
| `DESIGN.md` | What was built and why, what was rejected, how it is proven, week two, and the scheduling design |
| `SPEC.md` | Binding contracts: features, data shapes, non-goals |
| `HLD.md` | Architecture and the keystone decision |
| `VERIFICATION.md` | What `make check` proves — and, more usefully, what it does not |
| `docs/EVAL-PRINCIPLES.md` | The eval doctrine the harness is built to |
| `learning-tests/` | What the real claude and codex CLIs actually do, asserted rather than assumed |

Two binaries from one module, and the boundary is deliberate: `aubade` is the
product a customer runs, `aubade-lab` is the internal harness that writes the
exam and grades it. A student that can read the answer key proves nothing.
