# VERIFICATION — how aubade proves itself

The assignment grades whether we can *prove* the digest is good, not whether we
believe it is. This file describes the machinery that does the proving: what runs,
what it actually establishes, and — the part that matters most right now — what it
does **not** yet establish.

There is exactly one definition of "green": **`make check`**. The pre-commit hook,
the Stop hook, and CI all call that one target and nothing else. Three gates that
each define their own bar is three bars, and the loosest one wins in practice.

---

## 1 · `make check` — the gate

```
make check
  ├─ go vet ./...              static analysis, every package
  ├─ go build ./...            every package compiles
  ├─ go test ./...             unit tests
  └─ scripts/e2e-regression.sh end-to-end regression scenario   ← STUB (bead D1)
```

`make check` also builds `bin/aubade` and `bin/aubade-lab` on the way through,
because the end-to-end scenario drives the real binaries rather than calling into
the packages — the thing we ship is the thing we test.

Other targets:

| Target | What it does |
|---|---|
| `make build` | both binaries into `./bin/` |
| `make test` | unit tests only |
| `make e2e` | the end-to-end scenario only |
| `make golden` | rewrite the committed golden digests (see §2) |
| `make hooks` | install the local git hooks (see §4) |
| `make fmt` / `make fmt-check` | gofmt; `fmt-check` is advisory, not in the gate |
| `make clean` | remove `bin/` and `out/` |

---

## 2 · What is deterministic (and therefore allowed to gate)

Everything in the gate is deterministic and reproducible on any machine, with no
API keys and no network beyond the Go module cache:

- **`go vet` / `go build` / `go test`** — pure. No test in the gate talks to a
  model, a network service, or the clock.
- **Agent detection** (`internal/ax`) is tested through `agentx.MockEnvironment`,
  an injected environment, rather than by mutating the real process env. Detection
  is progressive enhancement (HLD §7), so the case under permanent guard is the
  *failure* case: unknown caller, nil environment, and detector panic all degrade
  to human mode.
- **CLI surface** (`internal/cli`) — the subcommand and flag contracts, the tool-name
  menu, argument validation, and the JSON-vs-prose error envelope. These are
  asserted because the eval harness and every agent caller bind to them; a
  silently renamed flag is a silently broken integration.
- **The trap catalog** (`internal/datagen`) — because each trap emits both its
  artifacts and its own answer-key entry, the tests assert things a hand-written
  `traps.json` could only promise: every extractor in `model.KnownKinds` has at
  least one task behind it, every `planted_ref` resolves to an artifact some
  scenario actually wrote, every keyword a trap will be graded on is quotable
  from that trap's own cited evidence, no reply predates the message it answers,
  and the same `(seed, --today)` produces a byte-identical plan. What it does
  *not* assert is that anything finds those traps: the extractors are graded
  against their own fixtures (below), and the exam and the student are only
  joined by the eval harness in bead D1.
- **The deterministic toolbox** (`internal/extract`) — the seven extractors, `thread`
  and `search`, run against hand-written fixture corpora under
  `internal/extract/testdata/` with a fixed `--today`. Both directions are asserted:
  every planted positive is found, and every hard negative stays silent (a kept
  promise, an answered thread, a suppressed newsletter that ends in a question, an
  FYI, a cancelled meeting, a long thread the owner closed). Determinism is a test of
  its own — the same corpus is loaded and extracted six times and the serialized
  bytes are compared, which is the only reliable way to catch map iteration order.
  A separate test walks every citation on every signal and fails if any ref does not
  resolve to a record in the corpus. These fixtures are deliberately *not* the
  generated corpus: an extractor graded only against the data its own teammate
  planted proves less than one graded against a corpus written to trip it.
- **The digest composer** (`internal/digest`) — scoring, sectioning, drafting
  and rendering, over two pinned fixture corpora under
  `internal/digest/testdata/`, each with a **committed golden page** compared
  byte for byte. The goldens are the cheapest real proof here: one assertion
  catches a re-ranking, a dropped citation, a reworded honesty line and a
  changed section order, and turns each of them into a reviewable diff instead
  of nothing at all. They are regenerated only through `make golden` — a
  deliberate act with a diff to read, never a silent self-heal.
  The two corpora are chosen against each other: one where every extractor
  fires, both drafting registers appear and a section overflows; one degraded —
  three sources missing, an inbox four days past the profile's freshness budget,
  no profile at all — which is the page that has to be honest about itself.
  Around the goldens, the properties that must not drift are asserted directly:
  the page is byte-identical across six independently loaded corpora, every
  rendered line carries a resolvable citation, uncertainty is routed to "I'm not
  sure" whatever the extractor hinted, contradictions render both sides with a
  citation each, and no draft is written for the person the profile protects or
  contains an answer the corpus does not.
- **The end-to-end regression scenario** (once bead D1 lands) — `aubade-lab generate`
  is seeded, `aubade digest --no-llm` runs a fixed extractor order over that fixed
  corpus, and `aubade-lab eval` asserts each planted trap present and each negative
  trap absent. Same seed, same `--today`, same answer, on every machine.

The rule behind all of that: **non-deterministic checks never gate.** A flaky gate
gets bypassed, and a bypassed gate is worse than none because it still gets cited
as evidence.

---

## 3 · What is NOT verified yet — read this before trusting a green run

### 3.1 The end-to-end regression is a stub (bead D1)

`scripts/e2e-regression.sh` currently prints

```
PENDING: regression eval wired in bead D1
```

on **every** run, and exits 0. It verifies only that both binaries built and are
executable.

Why it exits 0: the pre-commit hook is installed from bead A2 onward, so an e2e
that failed before the engine existed would make it impossible to commit the
engine. The stub is deliberately *loud* rather than silent — a check that is not
yet running has to say so on every run, or the gate quietly becomes theatre and
someone eventually cites it as proof of something it never checked.

So today, a green `make check` proves: **it compiles, it vets, and the unit tests
pass** — including the toolbox's own trap-shaped tests over its hand-written
fixtures, and the digest composer's golden pages over its own. What it still
does not prove is that a digest built from the *generated* corpus catches the
planted traps: that corpus is bead B1's, and the eval that joins the exam to the
student is bead D1's. A green gate says the composer does the same thing today
that it did yesterday, over fixtures we wrote; it does not yet say the answers
are right.

Bead D1 replaces the stub body with the real scenario (SPEC "End-to-end
verification scenario") and the script starts failing the gate on any regression
miss:

```
bin/aubade-lab generate --seed 42 --today 2026-08-30 --out data/
bin/aubade digest --no-llm --out out/
bin/aubade-lab eval --out out/          # non-zero exit on any regression miss
```

### 3.2 Deliberately outside the gate, permanently

| Not gated | Why |
|---|---|
| **Capability suite** (agentic digest, N=3 trials, `pass^3` / `pass@3`) | Non-deterministic and needs a model runner. Runs locally when the claude CLI is present, with a loud SKIP otherwise — never a silent one. |
| **Sabotage runs** (`aubade-lab eval --sabotage=<extractor>`) | On-demand and periodic. It proves the *graders* can see (score must drop when an extractor is disabled); it is a check on the exam, not on the commit. |
| **Judge grader** (`aubade-lab eval --judge`) | Model-scored voice/readability. Layer 2, by definition not binary, so it informs but never blocks. |
| **`make fmt-check`** | Advisory. Formatting noise should not be able to block a correctness fix. |
| **Learning tests** (`learning-tests/`) | They drive the real claude and codex CLIs: cost, auth, and non-determinism — three separate disqualifications. See §3.4. |

### 3.3 Currently stubbed commands

`aubade tool` and `aubade signals` are real as of bead C1: they load a corpus,
run the extractors, and emit cited signals (`aubade signals` writes
`out/signals.json`). `aubade digest --no-llm` is real as of bead C2: it composes
the full one-pager from those signals and writes `out/digest.md` with
`out/signals.json` beside it, with no network and no keys.

`aubade digest` **without** `--no-llm` — agentic mode — still exits 1 and names
its bead (C3), and it does not fall back to the template. Composing the page a
different way than the user asked for would be exactly the quiet substitution
this product exists not to make. `--customize` with `--no-llm` is refused for
the same reason: customization reshapes the compose stage, and `--no-llm` has no
compose stage to reshape.

The remaining stubs still exit 1 with `not implemented yet (bead X)` and name
the bead that will build them: `aubade schedule` (E1), `aubade-lab generate`
(B1), `aubade-lab eval` (D1). Stubs exit **non-zero** on purpose — a stub that
exits 0 lets a gate go green over an empty binary.

### 3.4 Learning tests — the dependencies the gate cannot touch

`make check` says nothing about the claude CLI, the codex CLI, or what agent
detection does against a real environment, because none of those can be asserted
deterministically without a key and a network. `learning-tests/` covers them,
outside the gate, run by hand:

```
bash learning-tests/run-all.sh
```

Each script writes down the assumption it started with, runs the real binary,
and asserts the result — correcting its own header comment where reality
disagreed. Exit codes are three-valued: 0 CONFIRMED, 1 CONTRADICTED (a header
comment is now wrong), 2 INCONCLUSIVE (dependency absent, unauthenticated, or
over the 90s budget). A timeout is a finding, not a failure.

What they establish today, in one line each — details and consequences in
`learning-tests/README.md`:

- The consensus roster on this machine is **one** runner, not three: gemini is
  absent, and codex is installed and reports itself logged in while `codex exec`
  returns 401. Presence is not liveness.
- claude's `--json-schema` takes inline JSON, codex's `--output-schema` takes a
  file; claude's `.result` is a JSON string requiring a second decode.
- There is no `--max-turns` on the claude CLI — bounded turns is aubade's job.
- A headless `claude -p` obeys the CLAUDE.md of its cwd; `--setting-sources
  user` is what keeps a stranger's instruction file out of the digest.
- `--allowedTools` alone is a real boundary: unallowlisted calls are denied, so
  the toolbox is enforced rather than merely intended.
- Agent detection is live and correct in both directions, including nested
  under a claude-driven tool loop — where `aubade tool` returns JSON with no
  `--json` flag.

---

## 4 · Local hooks

### `.claude/hooks/gate.sh`
`cd` to the repo root, run `make check`, exit with its code. The single source of
truth. Everything below calls this.

### The pre-commit hook
Runs `gate.sh`; a red gate refuses the commit and prints how to reproduce it
(`make check`). `.git/hooks` is not tracked by git, so **a fresh clone has no
hook and no gate until someone installs it**:

```
./scripts/install-hooks.sh     # or: make hooks
```

The installed hook is a small block that calls the tracked
`.claude/hooks/gate.sh`, so it cannot drift from the real definition. It is
idempotent: re-running detects its own marker and changes nothing.

**`core.hooksPath` — the trap that cost us a false green.** This repo sets
`core.hooksPath` to `.beads/hooks` (beads does this at `bd init`), and when that
setting is present **git ignores `.git/hooks` entirely**. The first version of
this installer wrote only to `.git/hooks/pre-commit`: the file was there, it was
executable, it ran correctly by hand — and it never fired on commit. A deliberately
broken tree committed cleanly.

So the installer resolves the directory git *actually* reads
(`git rev-parse --git-path hooks`, which honors `core.hooksPath`) and installs
there, plus into `.git/hooks` so the hook is also where a reader expects to find
it. It prints which one is live. If `core.hooksPath` is ever changed or unset,
re-run the installer.

**Co-existence.** The effective `pre-commit` was already owned by beads, which
writes a marker-delimited block and falls through on success. The installer
*appends* the aubade gate block after it rather than replacing the file (backing
the original up to `pre-commit.pre-aubade.bak`). Clobbering a teammate's tooling
to install your own gate is how gates get uninstalled.

A contributor who never runs the installer is not unprotected — CI runs the same
`make check` on the pull request. The hook makes the feedback arrive in seconds
instead of minutes; CI makes it unskippable.

**Proven, not asserted.** Before the A2 commit, the gate was tested against a red
tree twice: a syntax error (caught by `go vet`) and a deliberately failing unit
test. Both refused the commit with exit 1 and left `HEAD` untouched. That is the
test that matters — a gate nobody has watched fail is a gate nobody knows works.

`git commit --no-verify` bypasses the hook. That is a genuine emergency valve and
the hook says so out loud; using it to get a red commit in is the failure mode
this whole file exists to make embarrassing.

### `.claude/hooks/stop-gate.sh` (Claude Code Stop hook)
Registered under `Stop` in `.claude/settings.json`, alongside the pre-existing
SageOx hook (which was preserved, not replaced).

- With `PW_LOOP=1` (an unattended builder loop): runs the gate. Red ⇒ exit 2 with
  the failure output on stderr, which hands the failure back to the model as work
  it must finish rather than letting it stop on a broken tree. Green ⇒ exit 0.
- Without `PW_LOOP`: silent no-op, exit 0. An interactive session should not have
  a full `make check` fired at the end of every turn; a hook that punishes
  ordinary conversation gets switched off within a day, and then it protects
  nothing.

---

## 5 · CI mirror

`.github/workflows/ci.yml`:

- **`check`** — on `pull_request` and pushes to `main`. `actions/checkout@v4`,
  `actions/setup-go@v5` (Go `1.26.x`), then `make check`. Same target as the local
  hook: CI and the laptop cannot disagree about what green means.
  Uploads `out/digest.md` and `out/scorecard.md` as artifacts with
  `if-no-files-found: ignore`, because until D1 neither file is produced and a
  missing artifact must not turn the gate red.
- **`capability-eval`** — `workflow_dispatch` only. Today it echoes
  `capability suite runs locally; wired in bead D1`; it is a documented
  placeholder. It stays manual permanently: the capability suite is
  non-deterministic and never gates a pull request.

---

## 6 · Reproducing everything locally

```
./scripts/install-hooks.sh   # once per clone (see the core.hooksPath note in §4)
make check                   # the gate
make build && ./bin/aubade --help
```

To watch the gate block, rather than trusting that it would:

```
printf 'package cli\n\nfunc broken() { this is not go }\n' > internal/cli/tmp_break.go
git add internal/cli/tmp_break.go && git commit -m "should be refused"   # exits 1
git reset -q internal/cli/tmp_break.go && rm internal/cli/tmp_break.go
```

## 7 · Provenance of this gate

Written in bead A2, before the engine existed, on purpose: a gate added after the
code is a gate shaped to let the existing code through. This one was proven to
block first — the pre-commit hook ran against the A2 commit itself.
