# learning-tests — what the real dependencies actually do

Every file here started as a written-down assumption and then went and checked
it against the real binary. Where the assumption survived, the test asserts it
so it cannot rot silently. Where it did not, the header comment says
`CORRECTED` and names what changed — because the wrong belief is the useful
artefact, not the right one.

**These are not part of `make check` and never will be.** They call model CLIs:
they cost money, they need auth, they are non-deterministic. Three separate
disqualifications from a gate (VERIFICATION.md §2: *non-deterministic checks
never gate*). They are run by hand when a dependency, a CLI version, or an
assumption changes.

```
bash learning-tests/run-all.sh          # all of them, with a summary
bash learning-tests/run-all.sh 03 05    # just those two
LT_BUDGET_SECS=30 bash learning-tests/run-all.sh   # tighter model budget
```

Prerequisites: `jq`, `go`, and whichever runner CLI a given test is about.
Total cost of a full run: five `--model haiku` invocations, well under a cent.

## Exit-code contract

| Code | Meaning |
|---|---|
| 0 | **CONFIRMED** — every assertion held; the header comment matches reality |
| 1 | **CONTRADICTED** — an assertion failed; the header comment is now wrong and must be corrected |
| 2 | **INCONCLUSIVE** — the dependency was absent, unauthenticated, or over the 90s budget |

`2` is deliberately not `1`. "gemini is not installed" and "the model took
longer than 90s" are findings about the world, not defects in the test. A
timeout is data the digest orchestrator has to live with. Collapsing the two
teaches everyone to ignore red, and then a real contradiction goes unread too.

## The tests

| File | Subject | Today |
|---|---|---|
| `01-claude-oneshot-json.sh` | `claude -p` + `--json-schema`: the `Runner.Ask` contract | CONFIRMED |
| `02-claude-tool-loop.sh` | claude driving `aubade tool` under an allowlist: the `Runner.Orchestrate` contract | CONFIRMED |
| `03-codex-exec-oneshot.sh` | `codex exec` as the second consensus voter | INCONCLUSIVE — unauthenticated |
| `04-ax-detection.sh` | `internal/ax` against a real environment, including nested under claude | CONFIRMED |
| `05-runner-roster.sh` | who can actually vote, and what consensus becomes | CONFIRMED |
| `axprobe/` | tiny Go helper for 04: prints `ax.Caller()` / `ax.OutputMode()` as JSON | — |

## The findings that change the build

**1 · The runner roster is one, not three.** gemini is absent under every name
we looked for. codex is installed and *says* it is fine three different ways —
`command -v codex`, `codex login status` ("Logged in using ChatGPT", exit 0),
`codex doctor` ("✓ auth is configured", exit 0) — and `codex exec` still returns
401 in ~5s. Presence is not liveness; neither is the vendor's own auth status.
Only a real call is.

So SPEC §5's consensus runs single-runner here. That is allowed by the spec, but
it means the *degraded* path is the default path on this machine and has to be
the well-tested one. Two consequences for C2: detect runners by probing with a
capped real call, and drop a dead runner from the roster rather than counting it
as a dissenting vote — a 401 is not an opinion. The footer names live, dead and
absent, because SPEC §5 promises the footer says who voted.

Consensus stays well-defined at every roster size: at 2 a tie has no majority,
and SPEC §5 already routes runner disagreement to the "I'm not sure" section
with the thread shown. An even roster is safe *because* of the honesty layer.

**2 · The two CLIs are mirror images on structured output.** claude's
`--json-schema` takes the schema INLINE and rejects a path; codex's
`--output-schema` takes a FILE and rejects inline JSON. Same job, opposite
conventions. The `Runner` interface cannot hand implementations a pre-rendered
schema argument — it hands them the schema and each adapts.

And claude's `.result` is a JSON **string**, not a nested object: `Runner.Ask`
decodes twice.

**3 · There is no `--max-turns` on the claude CLI.** It is an Agent SDK option;
`claude --help | grep max-turns` returns nothing. SPEC §5's "bounded turns" is
therefore aubade's own job: wall-clock cap, `--max-budget-usd`, and counting
tool calls out of the `stream-json` transcript afterwards.

**4 · A headless `claude -p` run obeys the CLAUDE.md of its working directory.**
Measured with a canary file: a planted `CLAUDE.md` changed the model's
structured answer. Run from this repo — whose CLAUDE.md opens "**BLOCKING**: Run
`ox agent prime` NOW before ANY other action" — the subprocess intermittently
tried exactly that and burned a turn on a denied Bash call.

aubade runs claude from the *user's* directory, which may contain any CLAUDE.md
at all. An instruction file we do not control would otherwise get a vote in the
digest, and the honesty floor (SPEC §7) is precisely what such a file could talk
the model out of. `--setting-sources user` shuts the door and is asserted in
test 01.

**5 · `--allowedTools` alone is a real sandbox boundary.** No
`--dangerously-skip-permissions` needed: with `Bash(<abs>/bin/aubade tool:*)`
the loop runs, and with no allowlist the same prompt is *denied* and recorded in
`.permission_denials` rather than prompted for or quietly run. So C2 grants
exactly the toolbox and nothing else, and "facts can only enter through cited
tool output" becomes enforced rather than merely intended.

**6 · The AX layer serves aubade's own orchestrator first.** A `claude -p` run
exports agent markers into every Bash child — verified with this session's own
markers explicitly unset from claude's environment, so it is claude setting
them, not inheritance. `aubade tool` therefore emits its JSON envelope inside
the loop with no `--json` flag. The stub error the orchestrator actually
observed today:

```json
{"ok":false,"error":{"kind":"not_implemented","bead":"C1",
 "hint":"this subcommand is scaffolded but not built yet; do not retry"}}
```

The model called the tool once, read the envelope, and stopped. The A1 stub
contract is already working as an agent-facing contract before a single
extractor exists — and `--output-format stream-json --verbose` is what let us
count that, which is also the per-trial transcript D1 needs for
EVAL-PRINCIPLES #12.

**7 · Live agent detection, which unit tests cannot reach.** `internal/ax` is
unit-tested through `agentx.MockEnvironment`, which proves the failure modes and
cannot prove the success mode. Measured here: inside a Claude Code session,
`{"caller":"Claude Code","is_agent":true,"mode":"json"}`; under `env -i`,
`{"caller":"human","is_agent":false,"mode":"human"}`; with only
`TERM_PROGRAM=ghostty`, still human — no false positive for the human this
product is written for. And with only `AGENT_ENV=sageox` the caller comes back
named `sageox`, a name only aubade's own fallback table in `ax.go` can produce.
That fallback is load-bearing for the ox hook environment, not decoration.
