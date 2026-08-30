# DESIGN — aubade

What was built and why, what was deliberately not built and why, how the quality
claim is proven, and what week two is. The long forms live next door: `HLD.md`
(architecture), `SPEC.md` (binding contracts), `VERIFICATION.md` (the gate and
what it does *not* prove), `docs/EVAL-PRINCIPLES.md` (the eval doctrine).

## What was built

**Keystone: an agentic CLI over a deterministic toolbox.** The load-bearing
opinion is the split, not the agent.

- **The toolbox is deterministic and cited.** Seven extractors — commitments,
  quiet-threads, conflicts, contradictions, dispatchables, suppressions,
  staleness — plus `thread` and `search` for looking before ranking. Each is a
  pure Go function over the Event model, exposed as `aubade tool <name> --json`,
  and every signal it emits carries at least one citation (email id, calendar
  UID, note path, task ref). Same corpus and same `--today` ⇒ byte-identical
  output, which is what makes a trap eval possible at all.
- **The orchestration is agentic.** `aubade digest` hands a runner the toolbox
  and the profile and lets it decide what to chase and how to compose the page.
  Facts can only enter through cited tool output: before the composed page is
  written, every citation on it is checked against that run's `signals.json`,
  and a page carrying one ref that is not there is rejected whole. The model
  orchestrates; it cannot fabricate.
- **`--no-llm` is the same toolbox in fixed order through a template.** A full
  page, no network, no key — the mode a grader can always run cold, and the mode
  the gate runs.
- **Consensus is on by default** at the two bounded one-shot decisions
  (ambiguous urgency, the "one thing right now" pick): every runner that answers
  a liveness probe votes, disagreement routes the item to "I'm not sure" with the
  thread shown rather than a coin flip, and the footer names who voted and who
  could not. The user is a CEO — a wrong top priority costs more than 3× model
  spend, so quality is the default and `--consensus=off` is the frugal flag.
- **The honesty floor is not customizable.** `--customize prompt.md` reshapes the
  compose stage only; the staleness banner, contradictions and "I'm not sure" are
  appended by aubade from the signals *after* the composer is done, so a prompt
  asking for them to go away does not get them removed. Format is the user's;
  truthfulness is the product's.
- **Agents are first-class callers.** `internal/ax` (over `sageox/agentx`)
  detects an AI caller and switches output and errors to JSON with tool-use
  hints; detection failure degrades to human markdown, always.
- **Two binaries, one module.** `aubade` is the product; the exam, the answer key
  and the graders live in `aubade-lab`, which never ships. A student that can
  read the answer key proves nothing.

## Considered and rejected

- **One big LLM prompt over the whole corpus.** Unfalsifiable: no signals to
  assert against, no citation structure, hallucinated confidence indistinguishable
  from good judgment. Rejected on evaluability, which is the graded axis.
- **A hardcoded deterministic pipeline as the *only* mode.** Provable but
  judgment-free — it cannot decide that a 14-message thread needs reading before
  it can be ranked. Kept as `--no-llm`, demoted from keystone to fallback, where
  it earns its keep twice over: it is the zero-key path *and* the control the
  agentic mode is diffed against.
- **RAG / a vector store.** 500 emails fit in memory. Retrieval would add a
  recall failure mode to solve a scale problem we do not have.
- **Omnigent, or any meta-harness dependency.** Alpha, Python, and aimed at
  session orchestration — a bigger problem than ours. Its one good idea, several
  harnesses answering the same question, is kept as a ~200-line consensus layer
  over our own `Runner` interface instead of a platform dependency.
- **An in-process Anthropic SDK tool-use loop, now.** More "production", but it
  needs key handling and buys nothing this week: the CLI runners are blessed by
  the assignment and already installed. The `Runner` interface is deliberately
  vendor- and transport-neutral (`Ask` + `Orchestrate`), so an SDK runner is a
  registration, not a rewrite. Week two.
- **A web UI or Slack bot.** Explicitly not wanted: CLI in, markdown out.

## How the quality claim is proven

We wrote the **exam**, the **student** and the **answer key**, and graded all
three. Each planted trap is a scenario script that emits both its emails/events/
notes *and* its `traps.json` entry, so the key cannot drift from the data.

- **Code graders first.** A positive task needs both halves — a signal citing the
  evidence that trap planted **and** an expected keyword on the page — so
  "extracted and then lost in the render" is a distinct, named failure. Negative
  tasks are graded on signals rather than words, because the page quotes the very
  profile rule that suppressed an item. Outputs, never tool paths: a trap found by
  a better route than the key expected passes, and the mismatch is printed.
- **Regression and capability are never one number.** `--no-llm` is the
  regression suite: deterministic, one trial, 100% bar, gated by `make check`
  (currently 20/20). Agentic mode is the capability suite: N=3 isolated trials
  reported as **pass^3** and **pass@3**, run on demand, loud SKIP without a
  runner (observed 2026-08-30: 3/3 trials composed agentically, 20/20 at pass^3).
- **A reference solution proves the exam is passable.** The golden digest for
  seed 42 / `--today 2026-08-30` is committed and compared byte for byte, so a
  trap an agent misses later is provably a trap that was catchable.
- **Sabotage proves the graders can see.** Disabling each extractor in turn must
  drop the score, and an ALARM (banner, non-zero exit) says it did not. Observed
  drops: commitments −1, quiet-threads −4, conflicts −3, contradictions −1,
  dispatchables −2, suppressions −1, staleness −1.
- **A judge for the one axis code cannot grade** — "does it read like the sample,
  in Avery's voice" — anchored, reason-before-score, with an `uncertain` escape
  hatch. It informs and never blocks; it graded the `--no-llm` page
  `reads-like-a-machine`, which is evidence the anchors discriminate.

## Second week

Composio provider behind the existing `DataSource` interface (real Gmail and
Calendar, same Event model, same extractors); SDK-backed runners registered
beside the CLI ones; `aubade mcp serve` exposing the toolbox as an MCP server —
one bridge, every MCP-aware framework — and optionally a graph-backed `Runner`
that replaces the free-form loop with explicit extract→investigate→rank→compose
states; dataset mutation testing (perturb dates and senders; the traps must still
be caught) to answer VERIFICATION's "one exam" gap; and scheduled delivery,
implemented per the design below.

## Scheduling design

`aubade digest` is a batch job with a deadline. It is not interactive, it reads one
corpus and writes one page, it runs once a day, and the page has to exist before
Avery opens the laptop at 06:00 PT. A morning digest that arrives at 09:00 is not a
late digest; it is a digest nobody reads. That shape decides everything below.

**Recommendation: a hosted cron — GitHub Actions, 05:45 PT** — running the same
command a grader runs, writing a date-stamped page, uploading it as a run artifact
and (week two) mailing it. The repo is already the deployment unit: CI builds this
binary on every push, so the scheduled run costs one workflow file and no new
infrastructure.

```yaml
# .github/workflows/digest.yml — the shape. Not shipped: scheduling
# implementation is an explicit week-one non-goal (SPEC "Non-goals").
on:
  schedule:
    - cron: "45 12 * * *"   # 05:45 America/Los_Angeles while PDT
    - cron: "45 13 * * *"   # 05:45 America/Los_Angeles while PST
  workflow_dispatch:        # the same job, on a button, for a missed morning

permissions:
  contents: read

jobs:
  digest:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: "1.26.x"

      # GitHub cron is UTC and has never heard of daylight saving, so both
      # entries fire and the one that is not 05:45 in Los Angeles stands down.
      - id: when
        run: |
          if [[ "$(TZ=America/Los_Angeles date +%H)" == "05" ]]; then
            echo "run=yes" >> "$GITHUB_OUTPUT"
          else
            echo "run=no"  >> "$GITHUB_OUTPUT"
          fi

      - name: Compose the digest
        if: steps.when.outputs.run == 'yes'
        env:
          CLAUDE_CODE_OAUTH_TOKEN: ${{ secrets.CLAUDE_CODE_OAUTH_TOKEN }}
          COMPOSIO_API_KEY: ${{ secrets.COMPOSIO_API_KEY }}
        run: |
          make build
          DAY="$(TZ=America/Los_Angeles date +%F)"
          ./bin/aubade digest --today "$DAY" --out out \
            || ./bin/aubade digest --today "$DAY" --out out --no-llm
          cp out/digest.md "out/digest-$DAY.md"

      - name: Deliver
        if: steps.when.outputs.run == 'yes'
        uses: actions/upload-artifact@v4
        with:
          name: digest
          path: out/digest-*.md
          retention-days: 7   # the page quotes a private inbox
```

The `||` on the compose step is the one substitution in this design, and it belongs
to the operator rather than to the product: `aubade` itself refuses to quietly
compose a page a different way than it was asked to, and the fallback page says in
its own footer that the deterministic composer wrote it. A morning with no runner is
still a morning with a page; it is never a morning with a page that lies about who
wrote it.

**Why not local cron or launchd.** Zero infrastructure, no credentials leaving the
laptop, and the mail account is already authenticated there — genuinely the cheapest
thing that works, and the right answer for a developer running this on a desktop
that stays awake. It loses on the one property that makes a morning digest worth
having: at 05:45 the laptop is asleep. `cron` simply does not fire; launchd's
`StartCalendarInterval` fires late, on wake, which is 08:40 on the days it matters
most. There is also no run history to look at when a morning is missing, and no
second place the page exists. Recommended only as the local development loop —
which is what `make e2e` already is.

**Why not a cloud function.** Lambda plus EventBridge (or Cloud Run plus Scheduler)
is the reflex answer and it is over-built at n=1: packaging, an IAM role, a secrets
manager, log retention and an alarm — all to buy a timer we already own. It earns
its keep at the point where delivery fans out: many users, per-user schedules,
per-user credentials, retries that must not double-send. That is a queue and a
worker, and it is a different product than this one. Revisit then, not now.

**Secrets.** Three, and none of them is in the repo: the model runner's token
(`CLAUDE_CODE_OAUTH_TOKEN` or `ANTHROPIC_API_KEY`), the Composio key that reaches
real Gmail and Calendar in week two, and SMTP credentials for delivery. They live as
GitHub Actions repository secrets, scoped to the one step that needs them via `env:`
rather than job-wide, so a step added later does not silently inherit them. The
workflow keeps `permissions: contents: read`; nothing about a digest needs a write
token. The output is the sensitive part, not just the input — the page quotes a
private inbox verbatim — so the artifact carries a short retention and the
repository stays private. And the whole zero-key path stays available as the
degraded mode: `--no-llm` needs no secret at all, which is why an expired token
costs a plainer page rather than a missing one.

**Idempotency, by date-stamped output.** The unit of work is a day, so the artifact
is `digest-YYYY-MM-DD.md`, stamped in the user's timezone rather than the runner's.
Re-running the job for a day that already has a page overwrites it with a page
composed from the same corpus and the same `--today`: in `--no-llm` mode that is
byte-identical by construction, which is the property `make check` already asserts.
Agentic mode is not byte-stable and pretending otherwise would be a lie, so
idempotency there is about *delivery*, not bytes: the date stamp is the dedupe key,
and a send is skipped when a receipt already exists for that date. Re-composing is
cheap and harmless; sending Avery two different digests for one Tuesday is neither.

**When it fails, it says so.** A failed scheduled run is an email from GitHub with a
log, and the missed morning is visible in the run history — which is exactly what
the laptop-local options cannot offer. The design deliberately has no `aubade
schedule --install`: a CLI that writes crontabs is a footgun, and the workflow file
is reviewable, diffable and code-reviewed like everything else here. Week two
implements delivery (`--deliver=smtp` behind a small sender interface, shaped like
`DataSource`) and commits this file; week one prints the design and says it is a
design.
