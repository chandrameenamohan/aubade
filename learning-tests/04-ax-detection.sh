#!/usr/bin/env bash
#
# 04 — internal/ax: what agent detection actually does against a real environment.
#
# WHY THIS EXISTS: `internal/ax` is unit-tested through agentx.MockEnvironment,
# which by construction proves the *failure* modes (unknown caller, nil env,
# panicking detector all degrade to human) and cannot prove the success mode —
# a mock cannot tell you what Claude Code actually puts in the environment. This
# test covers the other half, and it can only be written from inside a real
# agent session. It drives bin/axprobe (learning-tests/axprobe), which prints
# ax.Caller() and ax.OutputMode() as one line of JSON.
#
# ─────────────────────────────────────────────────────────────────────────────
# WHAT WE ASSUMED
#
#   D1. agentx detection is best-effort and probably needs a handshake we do not
#       have, so aubade would usually run in human mode even under an agent.
#   D2. aubade's `fallbackEnvMarkers` table in ax.go is belt-and-braces on top of
#       agentx — nice to have, not load-bearing.
#   D3. When claude drives `aubade tool` in a loop, aubade will look like a
#       plain shell child and need an explicit `--json` to answer machine-first.
#
# WHAT IS ACTUALLY TRUE (agentx v0.2.0, Claude Code 2.1.251, measured below)
#
#   D1 IS WRONG — CORRECTED: detection is immediate and unambiguous inside a
#       Claude Code session: {"caller":"Claude Code","is_agent":true,"mode":"json"}.
#       And the guarantee that matters holds against the real world, not just the
#       mock: with `env -i` it is {"caller":"human","is_agent":false,"mode":"human"}.
#       A bare TERM_PROGRAM does NOT trip it, so ordinary terminal noise does not
#       produce false positives — the failure that would make aubade unusable for
#       the human it is written for.
#
#   D2 IS WRONG — CORRECTED: with only AGENT_ENV=sageox set, ax reports
#       caller "sageox". That name can only come from aubade's own fallback
#       table (ax.go returns the env *value* as the caller name); agentx does not
#       know the string. So the fallback layer is load-bearing for the ox/SageOx
#       hook environment aubade actually ships into, not decoration.
#
#   D3 IS WRONG, and it is the one that pays — CORRECTED: a `claude -p` run
#       exports its own agent markers into every Bash child, so `aubade tool`
#       invoked BY the orchestrator is detected and answers JSON with no flag.
#       This is measured below with our own session's markers explicitly unset
#       from claude's environment (`env -u CLAUDECODE -u …`), so it is claude
#       setting them rather than inheritance from this session.
#
#   Also confirmed: AUBADE_OUTPUT overrides detection in both directions without
#   disturbing who the caller is reported to be — the human escape hatch works.
#
# HOW TO RE-RUN:  bash learning-tests/04-ax-detection.sh
# COST/TIME:      1 model invocation (--model haiku, ~10s); the rest is local.

source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

need jq "the probe prints JSON"
need go "the probe is built from source"

header "04 · agent detection, live"

PROBE="$REPO_ROOT/bin/axprobe"
(cd "$REPO_ROOT" && go build -o "$PROBE" ./learning-tests/axprobe) || {
	inconclusive "could not build the axprobe helper"
	finish
}

field() { jq -r ".$2" <<<"$1"; }

# ── D1a: this session ──────────────────────────────────────────────────────
assumed "aubade would usually run in human mode even under an agent"

here="$("$PROBE")"
finding "in this process: $here"

if [[ -n "${CLAUDECODE:-}${CLAUDE_CODE_ENTRYPOINT:-}${AGENT_ENV:-}${CURSOR_TRACE_ID:-}${AIDER_MODEL:-}" ]]; then
	correction "detection is immediate and names the harness"
	assert_eq "an agent session is detected" "$(field "$here" is_agent)" "true"
	assert_eq "and it drives output into JSON mode" "$(field "$here" mode)" "json"
	note "caller reported as: $(field "$here" caller)"
else
	note "no agent markers in this process — you are running this by hand."
	note "the agent-session half of D1 needs an agent session to observe; run it from one."
	assert_eq "a bare terminal is treated as human" "$(field "$here" is_agent)" "false"
	assert_eq "and gets markdown" "$(field "$here" mode)" "human"
fi

# ── D1b: the environment scrubbed — the guarantee that must never regress ──
scrubbed="$(env -i PATH=/usr/bin:/bin "$PROBE")"
finding "with env -i: $scrubbed"

assert_eq "an empty environment is not an agent" "$(field "$scrubbed" is_agent)" "false"
assert_eq "an empty environment gets markdown" "$(field "$scrubbed" mode)" "human"
assert_eq "and is named human, not empty string" "$(field "$scrubbed" caller)" "human"

# ── which markers actually decide ──────────────────────────────────────────
one() { env -i PATH=/usr/bin:/bin "$1=$2" "$PROBE"; }

claudecode="$(one CLAUDECODE 1)"
entrypoint="$(one CLAUDE_CODE_ENTRYPOINT cli)"
agentenv="$(one AGENT_ENV sageox)"
terminal="$(one TERM_PROGRAM ghostty)"

finding "CLAUDECODE=1              → $claudecode"
finding "CLAUDE_CODE_ENTRYPOINT    → $entrypoint"
finding "AGENT_ENV=sageox          → $agentenv"
finding "TERM_PROGRAM=ghostty      → $terminal"

assert_eq "CLAUDECODE alone is enough" "$(field "$claudecode" is_agent)" "true"
assert_eq "CLAUDE_CODE_ENTRYPOINT alone is enough" "$(field "$entrypoint" is_agent)" "true"
assert_eq "TERM_PROGRAM alone is NOT (no false positive for humans)" "$(field "$terminal" is_agent)" "false"

assumed "the fallback marker table in ax.go is decoration on top of agentx"
correction "AGENT_ENV's value comes back as the caller name — only ax.go's table does that"
assert_eq "AGENT_ENV is detected" "$(field "$agentenv" is_agent)" "true"
assert_eq "and the fallback names the caller from the value" "$(field "$agentenv" caller)" "sageox"

# ── the override, both directions ──────────────────────────────────────────
forced_human="$(env -i PATH=/usr/bin:/bin CLAUDECODE=1 AUBADE_OUTPUT=human "$PROBE")"
forced_json="$(env -i PATH=/usr/bin:/bin AUBADE_OUTPUT=json "$PROBE")"

finding "agent + AUBADE_OUTPUT=human → $forced_human"
finding "human + AUBADE_OUTPUT=json  → $forced_json"

assert_eq "an agent can ask for prose" "$(field "$forced_human" mode)" "human"
assert_eq "without lying about who is calling" "$(field "$forced_human" is_agent)" "true"
assert_eq "a human can ask for JSON" "$(field "$forced_json" mode)" "json"

# ── D3: detection under a claude-driven tool loop ──────────────────────────
assumed "aubade invoked by the orchestrator looks like a plain shell child"

if ! command -v claude >/dev/null 2>&1; then
	inconclusive "claude is not installed — the nested-detection half of this test cannot run"
	finish
fi

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

NESTED_SCHEMA='{"type":"object","properties":{"caller":{"type":"string"},"is_agent":{"type":"boolean"},"mode":{"type":"string"}},"required":["caller","is_agent","mode"],"additionalProperties":false}'
NESTED_PROMPT="Run this exact command with the Bash tool: $PROBE
It prints one line of JSON. Report the caller, is_agent and mode fields from that line, verbatim. Do not read any files."

# Every marker this session sets is removed from claude's own environment, so a
# positive result can only mean claude re-established them for its children.
nested="$(cd "$work" && run_capped "$LT_BUDGET_SECS" env \
	-u CLAUDECODE -u CLAUDE_CODE_ENTRYPOINT -u CLAUDE_CODE_SESSION_ID \
	-u CLAUDE_CODE_EXECPATH -u AGENT_ENV -u AGENT_VERSION -u AI_AGENT \
	-u SAGEOX_AGENT_ID -u SAGEOX_SESSION_ID -u CLAUDE_PID \
	claude -p "$NESTED_PROMPT" \
	--output-format json --model haiku \
	--allowedTools "Bash($PROBE:*)" \
	--add-dir "$REPO_ROOT" \
	--setting-sources user \
	--json-schema "$NESTED_SCHEMA" 2>&1)"
nested_rc=$?

if timed_out "$nested_rc"; then
	inconclusive "the nested-detection run exceeded the ${LT_BUDGET_SECS}s budget"
	finish
fi

nested_result="$(jq -r '.result // ""' <<<"$nested" 2>/dev/null)"
if ! jq -e . <<<"$nested_result" >/dev/null 2>&1; then
	evidence "$(tail -3 <<<"$nested")"
	assert "the nested run returned a structured answer" false
	finish
fi

correction "claude sets its own agent markers for Bash children; ax detects them"
evidence "$nested_result"

assert_eq "aubade is seen as agent-called inside the loop" \
	"$(field "$nested_result" is_agent)" "true"
assert_eq "so tool output is JSON with no --json flag" \
	"$(field "$nested_result" mode)" "json"

note "consequence: the AX layer (SPEC §9) serves aubade's own orchestrator first."
note "C1's tool output and C2's error handling can both assume JSON in the loop."

finish
