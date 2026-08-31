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
