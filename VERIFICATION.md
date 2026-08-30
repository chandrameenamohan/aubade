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
  ├─ go test ./...             unit tests, incl. the in-process reference run
  └─ scripts/e2e-regression.sh generate → digest --no-llm → eval, through the binaries
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
| `make check-agentic` | the live agentic digest against the real claude CLI — **not** in `make check` (see §3.2) |
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
  against their own fixtures (below), and the exam and the student are joined by
  the eval harness (below).
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
  Three behaviours the trap harness found missing in bead D1 have their own
  hand-built corpora, each with the boundary that keeps it from becoming noise:
  a meeting booked over a *future* protected block is reported the morning after
  it was booked and not again, is ignored when it carries no CREATED stamp, and
  stops at the lookahead horizon; an approval with no question mark
  ("three expense reports need your approval") is a dispatchable while an FYI is
  not; and a note that says something slipped against a later mail that says it
  did not is a contradiction, while one shared word or a mail that predates the
  note is not.
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
- **The agentic layer** (`internal/runner`, `internal/agentic`) — everything
  about the orchestrated digest *except* the model call itself, which is
  §3.2's. The runners are exercised through `internal/runner/runnertest`, a
  scripted `Runner`, so `go test ./...` never shells out to claude: a unit test
  that calls a model costs money, needs auth, and is non-deterministic — three
  separate disqualifications from a gate. What that leaves gradeable is most of
  the interesting surface: the consensus vote math at every roster size (one
  runner decides alone; a 1–1 split has no majority and routes to "I'm not
  sure"; a runner that errors or answers off-schema is *dropped* rather than
  counted as a dissent), detection separating live from dead from absent, the
  registry and its `--runner` menu, the transport contracts the learning tests
  pinned (the double decode of claude's `.result`, tool-call counting and the
  turn cap read off the streamed transcript, the exact allowlist string), and
  every degradation path the CLI can end in — each with a message that names
  `--no-llm`.
  The load-bearing one is the citation validator: a composed page carrying a
  single ref that is not in `signals.json` is rejected whole, and both
  directions are asserted — an injected fake ref is caught by ref, and ordinary
  markdown links are not mistaken for citations. Around it, the invariants that
  make "format is the user's, truthfulness is the product's" true rather than
  merely requested: the honesty floor is appended from the fact base whatever
  the composer wrote, it survives a `--customize` prompt that asks for it to go
  away, and the footer names who orchestrated, who voted, and who could not.
- **The eval harness** (`internal/eval`) — the graders themselves, tested against
  hand-built fact bases rather than against the corpus that already passes. What
  is pinned is the *semantics*: a positive task needs both halves (a signal
  citing its planted evidence **and** a keyword on the page, so "extracted and
  then lost in the render" is a distinct, named failure); the expected extractor
  is reported and not enforced, so an engine that finds the right item by a
  better route is not punished; a `suppressions` signal in the honesty section is
  the record of *not* surfacing something and does not fail a negative task,
  while the same kind in any other section does. Around them: the grounding
  check catches an injected citation and is not fooled by markdown links, the
  pass^N / pass@N arithmetic is asserted on fabricated trials, and the judge is
  driven by scripted runners — one runner decides alone, a split falls to
  `uncertain`, a runner that errors is dropped, and a grade with no reasoning
  behind it is refused because reason-before-score is the contract.
- **The reference solution** (`internal/eval/reference_test.go`) — the
  generate → digest → eval pipeline, in-process, on every `go test`: the pinned
  corpus (seed 42, `--today 2026-08-30`) is generated, the deterministic page is
  composed from it, and it is graded against the answer key the generator wrote.
  Every positive trap is caught and every negative one stays out, so a task an
  agent misses later is a task that was provably catchable. The page is
  committed as `internal/eval/testdata/golden/digest.md` and compared byte for
  byte, and every citation on it is asserted to resolve against the fact base it
  was composed from. Sabotage is asserted here too, in both directions: disabling
  each of the seven extractors in turn must drop the score, and disabling one
  that no graded task depends on must raise the alarm — otherwise the
  "graders can see" claim is decoration.
- **The end-to-end regression scenario** — the same run again, through the real
  binaries: `aubade-lab generate --seed 42 --today 2026-08-30 --out data/` →
  `aubade digest --no-llm --out out/` → `aubade-lab eval --out out/`, exiting
  non-zero on any missed trap. It is slower and coarser than the in-process
  version and it proves the thing that one cannot: that the commands can be
  driven from a shell and write the files they promise. Same seed, same
  `--today`, same answer, on every machine.

The rule behind all of that: **non-deterministic checks never gate.** A flaky gate
gets bypassed, and a bypassed gate is worse than none because it still gets cited
as evidence.

---

## 3 · What is NOT verified yet — read this before trusting a green run

### 3.1 What the gate now proves, and what it still does not

The e2e stub is gone. As of bead D1 a green `make check` proves the thing the
assignment actually grades: a digest built from the **generated** corpus catches
every planted trap and surfaces none of the negatives. The exam and the student
are joined, in two independent places — in-process on every `go test`, and
through the real binaries in `scripts/e2e-regression.sh`, which exits non-zero on
any miss.

What a green gate still does not prove, and the honest list is short:

- **Nothing about the agentic page.** Every gated check runs the `--no-llm`
  composer. The agentic digest is the capability suite's business and the
  capability suite never gates; run it with
  `bin/aubade-lab eval --capability` (a loud SKIP when claude is absent) or
  `make check-agentic`.
- **Nothing about how the page reads.** The code graders answer "is the finding
  there and is it cited". "Does it read like the sample, in Avery's voice" is
  the judge's question, and the judge informs rather than blocks.
- **Nothing about a corpus other than the pinned one.** Seed 42 at
  `--today 2026-08-30` is one exam. Dataset mutation testing — perturb the dates
  and senders, the traps must still be caught — is explicitly week two.
- **The score can be right for the wrong reason.** The graders assert outcomes,
  not paths (EVAL-PRINCIPLES #6): a trap surfaced by an extractor other than the
  one the answer key expected still passes, and the mismatch is printed rather
  than failed. What closes that hole is sabotage, which is asserted for all
  seven extractors in `go test` and available on demand per extractor.

**Observed, not assumed.** Before the D1 commit the gate was watched failing:
`Conflicts()` was stubbed to return nothing, and `make e2e` went red naming both
calendar tasks, the extractor that missed them, and the reason. A gate nobody
has watched fail is a gate nobody knows works.

### 3.2 Deliberately outside the gate, permanently

| Not gated | Why |
|---|---|
| **Capability suite** (`aubade-lab eval --capability`: agentic digest, N=3 isolated trials in `out/trial-N/`, `pass^3` / `pass@3`) | Non-deterministic and needs a model runner. Runs when the claude CLI is present, with a loud SKIP otherwise — never a silent one — and it never touches the exit code either way. Observed on 2026-08-30 against claude 2.1.251: 3/3 trials composed agentically, 20/20 tasks at pass^3, every citation on every page grounded in that trial's own `signals.json`. |
| **`make check-agentic`** (`scripts/agentic-e2e.sh`) | The live agentic digest against the real claude CLI: two model-driven runs over the seeded corpus. It proves what no unit test can — that claude accepts the flags aubade hands it on the version installed here, that the allowlisted loop really calls `aubade tool`, that the composed page survived aubade's own citation check, and that `--customize` reshaped the page while the honesty floor stayed on it. Skips **loudly** when claude is absent, with a banner saying the agentic digest is unverified on this machine. |
| **Sabotage runs** (`aubade-lab eval --sabotage=<extractor>`) | On-demand and periodic. It proves the *graders* can see: the score must drop when an extractor is disabled, and an ALARM (banner plus non-zero exit) says it did not. A check on the exam, not on the commit — a gate that goes red because a deliberately broken engine scored badly teaches people to ignore the gate. Observed drops on the pinned corpus: commitments −1, quiet-threads −4, conflicts −3, contradictions −1, dispatchables −2, suppressions −1, staleness −1. |
| **Judge grader** (`aubade-lab eval --judge`) | Model-scored voice/readability. Layer 2, by definition not binary, so it informs but never blocks. It judges the agentic trial when the capability suite ran and the deterministic page otherwise, and says which on the card. Observed on 2026-08-30: it graded the `--no-llm` page `reads-like-a-machine` — evidence the anchors discriminate rather than sitting at the top. |
| **`make fmt-check`** | Advisory. Formatting noise should not be able to block a correctness fix. |
| **Learning tests** (`learning-tests/`) | They drive the real claude and codex CLIs: cost, auth, and non-determinism — three separate disqualifications. See §3.4. |

### 3.3 Currently stubbed commands

`aubade tool` and `aubade signals` are real as of bead C1: they load a corpus,
run the extractors, and emit cited signals (`aubade signals` writes
`out/signals.json`). `aubade-lab generate` is real as of bead B3: it writes the
whole corpus — `inbox.jsonl`, `calendar.ics`, `notes/`, `tasks.md`, `profile.md`
and `traps.json` — and the same `(seed, --today)` produces byte-identical files.
`aubade digest --no-llm` is real as of bead C2: it composes the full one-pager
from those signals and writes `out/digest.md` with `out/signals.json` beside it,
with no network and no keys.

`aubade digest` **without** `--no-llm` — agentic mode — is real as of bead C3:
it runs the toolbox, majority-votes the two bounded decisions across every
runner that answers a probe, hands the toolbox to the chosen runner behind an
allowlist, checks every citation on what comes back, and writes
`out/digest.md`, `out/signals.json` and `out/transcript.jsonl`.

Two substitutions it will **not** make quietly, and they are different on
purpose:

- A runner that cannot be driven at all — absent, unauthenticated, or a loop
  that blew its budget — is an **error** naming `--no-llm`. There is no page,
  and composing one a different way than the user asked for is the quiet
  substitution this product exists not to make.
- A page the runner *did* compose and aubade then **rejected** — because a
  citation on it is not in `signals.json` — falls back to the deterministic
  composer, and says so in three places: a blockquote at the top of the page,
  the footer, and stderr. There the facts exist and only the page is untrusted,
  so refusing to print anything would serve nobody; what matters is that nobody
  can mistake which composer wrote it.

`--customize` with `--no-llm` is still refused: customization reshapes the
compose stage, and `--no-llm` has no compose stage to reshape. `--customize`
cannot reach the honesty layer either — the banner, contradictions and "I'm not
sure" are appended by aubade from the signals after the composer is done, so a
prompt that asks for them to go away simply does not get them removed.

`aubade-lab eval` is real as of bead D1: it loads `traps.json`, grades
`out/digest.md` against the `out/signals.json` beside it, writes
`out/scorecard.md` with the regression and capability sections kept apart, and
exits non-zero on any regression miss or sabotage alarm.

One stub is left, and it exits 1 with `not implemented yet (bead E1)` naming the
bead that will build it: `aubade schedule`. Stubs exit **non-zero** on purpose —
a stub that exits 0 lets a gate go green over an empty binary.

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
  Uploads `out/digest.md` and `out/scorecard.md` as artifacts — both are now
  written by the e2e run, so a failed gate leaves the scorecard that explains it
  attached to the run.
- **`capability-eval`** — `workflow_dispatch` only, and it stays manual
  permanently: the capability suite is non-deterministic and never gates a pull
  request. It runs the pipeline and then `aubade-lab eval --capability
  --sabotage=<extractor>`; without a claude CLI on the runner the capability half
  skips loudly and the sabotage half still runs, which is the useful part to have
  on a button.

---

## 6 · Reproducing everything locally

```
./scripts/install-hooks.sh   # once per clone (see the core.hooksPath note in §4)
make check                   # the gate
make build && ./bin/aubade --help
```

The passes that are deliberately outside the gate, in the order you would reach
for them:

```
bin/aubade-lab eval --adversarial            # how each negative task stayed out
bin/aubade-lab eval --sabotage=conflicts     # can the graders still see? (non-zero on ALARM)
bin/aubade-lab eval --capability             # agentic, N=3 trials, pass^3 / pass@3
bin/aubade-lab eval --judge                  # layer 2: does it read like the sample
make check-agentic                           # the live agentic digest, end to end
```

All four read the run in `out/`, so produce one first (`make e2e`, or the two
commands it runs). The last three need the claude CLI and say so loudly when it
is missing.

To watch the gate block, rather than trusting that it would:

```
printf 'package cli\n\nfunc broken() { this is not go }\n' > internal/cli/tmp_break.go
git add internal/cli/tmp_break.go && git commit -m "should be refused"   # exits 1
git reset -q internal/cli/tmp_break.go && rm internal/cli/tmp_break.go
```

To watch the *eval* block, which is the half that is new:

```
make e2e                                     # green
printf '# Daily Digest — nothing to report.\n' > out/digest.md
bin/aubade-lab eval                          # RED, naming every task and the extractor that owes it
```

## 7 · Provenance of this gate

Written in bead A2, before the engine existed, on purpose: a gate added after the
code is a gate shaped to let the existing code through. This one was proven to
block first — the pre-commit hook ran against the A2 commit itself.
