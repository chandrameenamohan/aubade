#!/usr/bin/env bash
#
# 03 — codex CLI, one-shot, structured answer (the second consensus voter).
#
# WHY THIS EXISTS: SPEC §5 makes consensus the default — fan a bounded one-shot
# question to every runner on the machine and majority-vote. That design assumes
# a second runner exists and answers. This test finds out whether it does.
#
# ─────────────────────────────────────────────────────────────────────────────
# WHAT WE ASSUMED
#
#   C1. `codex -p "<prompt>"` is the headless one-shot, mirroring `claude -p`.
#   C2. Schemas are passed the way claude passes them.
#   C3. A CLI on PATH that reports itself logged in is a usable runner.
#
# WHAT IS ACTUALLY TRUE (codex-cli 0.141.0, macOS, measured below)
#
#   C1 IS WRONG — CORRECTED: `-p` on codex is `--profile` (which config profile
#       to layer on), on both the root command and `exec`. Passing a prompt to
#       it would be read as a profile name. The headless one-shot is a
#       SUBCOMMAND: `codex exec [OPTIONS] [PROMPT]`. Useful extras discovered
#       with it: `--skip-git-repo-check` (exec refuses to run outside a git repo
#       otherwise), `--sandbox read-only`, `--json` for a JSONL event stream, and
#       `-o/--output-last-message <FILE>`.
#
#   C2 IS WRONG, in the exact opposite direction to claude — CORRECTED:
#       codex `--output-schema` takes a FILE PATH and rejects inline JSON
#       ("Failed to read output schema file {"type":"object"}: No such file or
#       directory"), while claude `--json-schema` takes inline JSON and rejects a
#       path (test 01). Two runners, mirrored conventions. The Runner interface
#       therefore cannot hand implementations a pre-rendered "schema argument";
#       it hands them the schema and each adapts. Cheap to know now, annoying to
#       discover inside a consensus bug.
#
#   C3 IS WRONG, and it is the finding that reshapes SPEC §5 — CORRECTED: on
#       this machine codex is installed and *says* it is fine, three different
#       ways, and is not:
#           command -v codex          → /Users/…/.local/bin/codex
#           codex login status        → "Logged in using ChatGPT", exit 0
#           codex doctor              → "✓ auth   auth is configured", exit 0
#                                       ("degraded", but 0 fail)
#           codex exec "…"            → 401 Unauthorized, "Your access token
#                                       could not be refreshed", exit 1, ~5s,
#                                       and NO --output-last-message file
#       So presence is not liveness, and neither is the vendor's own auth
#       status. Only an actual exec is. Consequence: runner detection must probe
#       with a real (cheap) call, a runner that fails is DROPPED from the vote
#       rather than counted as a dissent — a 401 is not an opinion — and the
#       digest footer must name it as unavailable, since SPEC §5 promises the
#       footer names who voted.
#
#   Also worth carrying: `codex exec` prints "Reading additional input from
#   stdin..." even with stdin redirected from /dev/null, and its diagnostics go
#   to the same stream as its prose. Parse `-o <file>`, never stdout.
#
# CONSENSUS ARITHMETIC ON THIS MACHINE: claude usable, codex present but
# unauthenticated, gemini absent (test 05) ⇒ ONE voter. Consensus does not
# degrade to two, it degrades to single-runner. See learning-tests/README.md.
#
# HOW TO RE-RUN:  bash learning-tests/03-codex-exec-oneshot.sh
# EXPECTED TODAY: INCONCLUSIVE (exit 2) — the flag-surface assertions pass, the
#                 live call cannot. If codex is ever re-authenticated this test
#                 says so loudly and this comment must be corrected again.

source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

need jq "the tests parse structured answers"

header "03 · codex exec one-shot"

if ! command -v codex >/dev/null 2>&1; then
	finding "codex is not installed on this machine at all"
	inconclusive "no codex binary — consensus loses this voter entirely"
	finish
fi
note "codex $(codex --version 2>&1 | head -1)"

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
cd "$work" || exit 1

# ── C1: -p is not the prompt flag ──────────────────────────────────────────
assumed "codex -p <prompt> is the headless one-shot, like claude -p"

root_help="$(codex --help 2>&1)"
exec_help="$(codex exec --help 2>&1)"

correction "-p is --profile; the headless one-shot is the 'codex exec' subcommand"
evidence "$(grep -E '^  -p, --profile' <<<"$root_help")"

assert_contains "-p means --profile, not prompt" "$root_help" "-p, --profile"
assert_contains "exec is the non-interactive subcommand" "$root_help" "Run Codex non-interactively"
assert_contains "exec takes the prompt positionally" "$exec_help" "codex exec [OPTIONS] [PROMPT]"
assert_contains "exec can write the final message to a file" "$exec_help" "--output-last-message"

# ── C2: the schema flag is the mirror image of claude's ────────────────────
assumed "the schema is passed the way claude passes it (inline JSON)"

inline_out="$(run_capped 30 codex exec --skip-git-repo-check --sandbox read-only \
	--output-schema '{"type":"object"}' -o "$work/never.txt" "ping" </dev/null 2>&1)"
inline_rc=$?

correction "--output-schema takes a FILE; claude's --json-schema takes INLINE JSON"
evidence "$(grep -i 'output schema' <<<"$inline_out" | head -1)"

assert "inline JSON is refused by codex" test "$inline_rc" -ne 0
assert_contains "the refusal says it wanted a file" "$inline_out" "output schema file"
assert "no output file was written on refusal" test ! -f "$work/never.txt"

# ── C3: presence is not liveness ───────────────────────────────────────────
assumed "a codex on PATH that reports itself logged in will answer"

login_status="$(run_capped 30 codex login status 2>&1)"
login_rc=$?
finding "codex login status: $(head -1 <<<"$login_status") (exit $login_rc)"

cat >"$work/schema.json" <<'SCHEMA'
{"type":"object","properties":{"answer":{"type":"integer"},"confidence":{"type":"string","enum":["certain","unsure"]}},"required":["answer","confidence"],"additionalProperties":false}
SCHEMA

started=$SECONDS
exec_out="$(run_capped "$LT_BUDGET_SECS" codex exec \
	--skip-git-repo-check --sandbox read-only \
	--output-schema "$work/schema.json" \
	-o "$work/last.txt" \
	"What is 17 plus 25? Set confidence to certain only if the arithmetic is exact." \
	</dev/null 2>&1)"
exec_rc=$?
elapsed=$((SECONDS - started))

if timed_out "$exec_rc"; then
	finding "codex exec exceeded the ${LT_BUDGET_SECS}s budget"
	note "a runner too slow to answer inside the budget is a runner consensus cannot use;"
	note "that is a property of the world, so it is recorded, not failed."
	inconclusive "codex exec timed out at ${LT_BUDGET_SECS}s"
	finish
fi

if [[ "$exec_rc" -eq 0 && -s "$work/last.txt" ]]; then
	answer="$(jq -e . <"$work/last.txt" 2>/dev/null)" || answer=""
	if [[ -z "$answer" ]]; then
		finding "codex exec exited 0 in ${elapsed}s but the last message is not JSON"
		evidence "$(head -c 300 "$work/last.txt")"
		# Exit 0 with an unparseable structured answer IS a contract violation:
		# --output-schema was accepted and then not honoured.
		assert "the schema-constrained answer parses as JSON" false
		finish
	fi
	correction "codex now authenticates and answers — the C3 finding above is STALE"
	note "update this file's header and README.md: consensus gains a second voter."
	finding "codex exec succeeded in ${elapsed}s"
	evidence "$answer"
	assert_eq "codex obeys --output-schema" "$(jq -r '.answer' <<<"$answer")" "42"
	assert "confidence is one of the enum values" \
		grep -qE '^(certain|unsure)$' <<<"$(jq -r '.confidence' <<<"$answer")"
	finish
fi

finding "codex exec failed in ${elapsed}s with exit $exec_rc and wrote no answer file"
evidence "$(grep -iE '401|refresh|unauthor' <<<"$exec_out" | tail -2)"

if grep -qiE 'refresh|401|unauthor|sign in' <<<"$exec_out"; then
	correction "installed + 'logged in' + doctor-ok, and still unusable: only exec proves liveness"
	assert "the failure is loud and fast, not a hang" test "$elapsed" -lt "$LT_BUDGET_SECS"
	assert "no answer file is left behind to be mistaken for a vote" test ! -s "$work/last.txt"
	inconclusive "codex is present but unauthenticated — drop it from the vote, name it in the footer"
	finish
fi

evidence "$(tail -5 <<<"$exec_out")"
inconclusive "codex exec failed for a reason this test does not recognise (exit $exec_rc)"
finish
