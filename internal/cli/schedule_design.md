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
