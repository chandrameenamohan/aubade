#!/usr/bin/env bash
#
# 02 — claude CLI driving a tool loop over `aubade tool` (the real orchestration shape).
#
# WHY THIS EXISTS: SPEC §5 and HLD §3 put the whole product on one bet — the
# model orchestrates but cannot fabricate, because facts enter only through
# `aubade tool <name>`. Before C2 builds that loop, three things have to be true
# of the real CLI: it must be able to run our binary without a "dangerously"
# flag, it must be *unable* to run anything else, and it must be able to read
# what our binary says back. This test measures all three against the stubs that
# exist today, which is exactly why the finding is a structured
# not-implemented error rather than a digest.
#
# ─────────────────────────────────────────────────────────────────────────────
# WHAT WE ASSUMED
#
#   B1. Letting the model run a command headlessly needs `--permission-mode
#       bypassPermissions` or `--dangerously-skip-permissions`; a per-command
#       allowlist is an interactive-mode nicety.
#   B2. `aubade tool` would have to be called with `--json` for the model to get
#       machine-parseable output, since aubade defaults to markdown for humans.
#   B3. `--output-format json` is enough to see what the model did with the
#       toolbox.
#
# WHAT IS ACTUALLY TRUE (claude 2.1.251, macOS, measured below)
#
#   B1 IS WRONG — CORRECTED: `--allowedTools "Bash(<prefix>:*)"` alone is
#       sufficient in `-p` mode, and it is *tight*: with no allowlist the same
#       prompt comes back with the call recorded in `.permission_denials` and
#       the model reporting "The command failed with an approval requirement
#       before it could execute." Headless denies rather than prompting or
#       quietly running. So aubade never needs a bypass flag: the orchestrator
#       gets `Bash(<abs path>/bin/aubade tool:*)` and nothing else, and the
#       toolbox becomes a real sandbox boundary instead of an honour system.
#
#   B2 IS WRONG, and it is the best news in this file — CORRECTED: the claude
#       subprocess exports its own agent markers into every Bash child, so
#       `internal/ax` detects "Claude Code" and `aubade tool` emits the JSON
#       error envelope with no `--json` flag at all. Test 04 shows this survives
#       scrubbing our own session's markers out of claude's environment, so it
#       is claude setting them, not inheritance. Consequence: the AX layer
#       (SPEC §9) is not only for third-party agent callers — it is what makes
#       aubade legible to its own orchestrator, for free.
#
#   B3 IS INCOMPLETE — CORRECTED: `--output-format json` returns only the final
#       envelope. `--output-format stream-json --verbose` returns JSONL with the
#       full transcript: `assistant` events carrying `tool_use` blocks and a
#       `user` event carrying the `tool_result`, which includes "Exit code 1"
#       and aubade's JSON verbatim. That is the artefact D1 needs for its
#       transcript check (EVAL-PRINCIPLES #12: did tool output ground each cited
#       fact) — and it is why this test can count tool calls instead of asking
#       the model how many it made.
#
# THE FINDING THAT IS THE POINT: the loop closes on a stub. The model calls the
# tool, receives
#     {"ok":false,"error":{"kind":"not_implemented","bead":"C1","hint":"…do not retry"}}
# and stops. The stub error contract from A1 is therefore already doing its job
# as an agent-facing contract, before any extractor exists.
#
# HOW TO RE-RUN:  bash learning-tests/02-claude-tool-loop.sh
# COST/TIME:      2 invocations, --model haiku, ~25s total.

source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

need jq "the tests parse the CLI's JSON envelope"
need claude "this test's entire subject"

header "02 · claude driving the aubade toolbox"

AUBADE="$REPO_ROOT/bin/aubade"
if [[ ! -x "$AUBADE" ]]; then
	note "building bin/aubade first"
	(cd "$REPO_ROOT" && make build) >/dev/null || {
		inconclusive "could not build bin/aubade"
		finish
	}
fi
note "subject: $AUBADE tool commitments"

# Run the model from a scratch directory, not the repo: aubade's real caller is
# a user's own directory, and test 01 showed the cwd's CLAUDE.md gets a vote.
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

PROMPT="Run this exact command with the Bash tool: $AUBADE tool commitments
It is expected to fail. Do not retry it, do not fix anything, do not read any files.
Then say in one sentence what happened."

# ── B1/B3: the allowed loop, observed through the transcript ────────────────
assumed "a headless tool loop needs a bypass-permissions flag"

stream="$work/stream.jsonl"
(cd "$work" && run_capped "$LT_BUDGET_SECS" claude -p "$PROMPT" \
	--output-format stream-json --verbose --model haiku \
	--allowedTools "Bash($AUBADE tool:*)" \
	--add-dir "$REPO_ROOT" \
	--setting-sources user) >"$stream" 2>"$work/stream.err"
rc=$?

if timed_out "$rc"; then
	inconclusive "the tool loop exceeded the ${LT_BUDGET_SECS}s budget — a bound the orchestrator must own (there is no --max-turns)"
	finish
fi

if [[ ! -s "$stream" ]]; then
	assert "the run produced a transcript" false
	evidence "$(tail -5 "$work/stream.err")"
	finish
fi

correction "--allowedTools alone is enough, and it is a real boundary"

tool_calls="$(jq -r 'select(.type=="assistant") | .message.content[]? | select(.type=="tool_use") | .name' "$stream" | wc -l | tr -d ' ')"
bash_cmds="$(jq -r 'select(.type=="assistant") | .message.content[]? | select(.type=="tool_use" and .name=="Bash") | .input.command' "$stream")"
tool_result="$(jq -r 'select(.type=="user") | .message.content[]? | select(.type=="tool_result") | (.content|tostring)' "$stream")"
final="$(jq -r 'select(.type=="result") | .subtype' "$stream")"
turns="$(jq -r 'select(.type=="result") | .num_turns' "$stream")"
duration="$(jq -r 'select(.type=="result") | .duration_ms' "$stream")"

finding "transcript: tool_use blocks=$tool_calls num_turns=$turns duration_ms=$duration subtype=$final"
evidence "$bash_cmds"

assert_eq "the loop completed" "$final" "success"
assert_eq "the model called the toolbox exactly once" "$tool_calls" "1"
assert_contains "it called the command we allowed" "$bash_cmds" "$AUBADE tool commitments"

# ── B2: what came back, and in what shape ──────────────────────────────────
assumed "aubade needs --json before an orchestrator can parse it"

correction "ax detected the claude subprocess, so the JSON envelope came out unasked"
evidence "$(head -c 400 <<<"$tool_result")"

assert_contains "the model saw the non-zero exit" "$tool_result" "Exit code 1"
assert_contains "the error is machine-parseable, with no --json flag" "$tool_result" '"kind": "not_implemented"'
assert_contains "it names the bead that will implement it" "$tool_result" '"bead": "C1"'
assert_contains "it tells the caller not to retry" "$tool_result" "do not retry"
assert_eq "and the model obeyed the hint (no retry)" "$tool_calls" "1"

# ── B1 again, from the other side: deny by default ─────────────────────────
denied_out="$(cd "$work" && run_capped "$LT_BUDGET_SECS" claude -p "$PROMPT" \
	--output-format json --model haiku \
	--add-dir "$REPO_ROOT" --setting-sources user 2>&1)"
denied_rc=$?

if timed_out "$denied_rc"; then
	inconclusive "the deny-by-default run exceeded the ${LT_BUDGET_SECS}s budget"
	finish
fi

denied_count="$(jq -r '.permission_denials | length' <<<"$denied_out" 2>/dev/null || echo 0)"
denied_tool="$(jq -r '.permission_denials[0].tool_name // "«none»"' <<<"$denied_out" 2>/dev/null || echo "«none»")"

finding "without an allowlist the same prompt is refused, not prompted for"
evidence "permission_denials=$denied_count tool=$denied_tool"
evidence "$(jq -r '.result' <<<"$denied_out" 2>/dev/null | head -c 200)"

assert "the unallowlisted call was denied" test "$denied_count" -ge 1
assert_eq "the denial names the tool it blocked" "$denied_tool" "Bash"

note "consequence for C2: allowlist exactly Bash(<abs>/bin/aubade tool:*), capture"
note "stream-json as the per-trial transcript, and read tool_result as JSON without"
note "passing --json. Bound the loop on wall clock and --max-budget-usd."

finish
