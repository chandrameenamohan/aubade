#!/usr/bin/env bash
#
# 05 — the runner roster: who can actually vote, and what consensus becomes.
#
# WHY THIS EXISTS: SPEC §5 turns consensus on by default and fans each bounded
# one-shot decision to "every runner detected on the machine". That sentence
# hides two questions this test answers with numbers: how many runners are there
# really, and what does "detected" have to mean. This is the prototype of the
# detection routine C2 will ship.
#
# ─────────────────────────────────────────────────────────────────────────────
# WHAT WE ASSUMED
#
#   E1. Three runners (claude, codex, gemini) are available here, so consensus
#       is a 3-way majority vote — an odd number, no ties, comfortable.
#   E2. "Detected" means the binary is on PATH.
#
# WHAT IS ACTUALLY TRUE (this machine, measured below)
#
#   E1 IS WRONG, twice over — CORRECTED:
#       • gemini is not installed at all, under any of the names we looked for
#         (gemini, gemini-cli, google-gemini). Nothing to fall back to.
#       • codex IS installed and still cannot vote: its token will not refresh
#         (test 03 has the 401 in full).
#       So the roster is not 3 and not 2. It is ONE. Consensus degrades to
#       single-runner, which SPEC §5 already allows ("one runner installed ⇒
#       single-runner silently") — the important part is that we now know the
#       default path on this machine is the *degraded* one, so it must be the
#       well-tested path rather than the fallback nobody exercises.
#
#   E2 IS WRONG — CORRECTED: presence is not liveness (test 03: PATH, `codex
#       login status`, and `codex doctor` all say fine while `codex exec` 401s).
#       Detection has to make a real, cheap call and time it out. That is what
#       this script does, and it is what C2 should do.
#
# THE DESIGN CONSEQUENCE WORTH KEEPING: an even roster (2 voters) has ties, and
# a tie has no majority. That is not a hole here — SPEC §5 routes runner
# disagreement to the "I'm not sure" section with the thread shown, so a 1–1
# split resolves to honesty rather than a coin flip. Consensus is therefore
# well-defined at N=1, 2 and 3; what changes with N is only how often the
# honest-uncertainty path fires. A dead runner must be DROPPED from the roster,
# never counted as a dissenting vote — a 401 is not an opinion.
#
# HOW TO RE-RUN:  bash learning-tests/05-runner-roster.sh
# COST/TIME:      one cheap call per installed runner, ~15s total.

source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

need jq "the claude probe returns a JSON envelope"

header "05 · runner roster and consensus arithmetic"

# A liveness probe must be cheap enough to run on every digest and short enough
# that a hung runner cannot hold the 6am job open.
PROBE_SECS="${LT_PROBE_SECS:-45}"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

live=()
dead=()
absent=()

# ── gemini: is it here at all, under any name we know ──────────────────────
assumed "three runners are installed, so consensus is a 3-way majority vote"

gemini_bin=""
for n in gemini gemini-cli google-gemini; do
	if command -v "$n" >/dev/null 2>&1; then
		gemini_bin="$n"
		break
	fi
done

if [[ -n "$gemini_bin" ]]; then
	correction "gemini IS installed now (as '$gemini_bin') — this file's header is STALE"
	note "its headless syntax has never been verified here, so it is NOT counted below:"
	note "an unprobed runner is not a voter. Write its learning test, then re-do the arithmetic."
	inconclusive "gemini appeared; write learning-tests/06 for it"
else
	correction "gemini is absent under every name we looked for"
	evidence "tried: gemini, gemini-cli, google-gemini — none on PATH"
	absent+=("gemini")
	assert "gemini is not installed on this machine" test -z "$gemini_bin"
fi

# ── claude: installed, and does it answer ──────────────────────────────────
assumed "a runner on PATH is a runner that can vote"

if ! command -v claude >/dev/null 2>&1; then
	absent+=("claude")
	finding "claude is not installed"
else
	out="$(run_capped "$PROBE_SECS" claude -p 'Reply with exactly: ok' \
		--model haiku --setting-sources user --output-format json 2>&1)"
	rc=$?
	subtype="$(jq -r '.subtype // "«none»"' <<<"$out" 2>/dev/null || echo "«none»")"
	if timed_out "$rc"; then
		finding "claude: installed, timed out at ${PROBE_SECS}s"
		dead+=("claude(timeout)")
	elif [[ "$rc" -eq 0 && "$subtype" == "success" ]]; then
		finding "claude: installed and answering (subtype=$subtype)"
		live+=("claude")
	else
		finding "claude: installed, probe failed (exit $rc, subtype=$subtype)"
		evidence "$(tail -2 <<<"$out")"
		dead+=("claude")
	fi
fi

# ── codex: installed, and does it answer ───────────────────────────────────
if ! command -v codex >/dev/null 2>&1; then
	absent+=("codex")
	finding "codex is not installed"
else
	out="$(run_capped "$PROBE_SECS" codex exec --skip-git-repo-check --sandbox read-only \
		-o "$work/codex.txt" 'Reply with exactly: ok' </dev/null 2>&1)"
	rc=$?
	if timed_out "$rc"; then
		finding "codex: installed, timed out at ${PROBE_SECS}s"
		dead+=("codex(timeout)")
	elif [[ "$rc" -eq 0 && -s "$work/codex.txt" ]]; then
		correction "codex now answers — test 03's unauthenticated finding is STALE"
		finding "codex: installed and answering"
		live+=("codex")
	else
		finding "codex: installed, probe failed (exit $rc)"
		evidence "$(grep -iE '401|refresh|unauthor|error' <<<"$out" | tail -1)"
		dead+=("codex")
	fi
fi

# ── the arithmetic ─────────────────────────────────────────────────────────
# Expanding an empty array is an unbound-variable error under `set -u` on
# bash 3.2, which is still what /bin/bash is on macOS — so guard the join.
names() { if (( $# == 0 )); then echo none; else echo "$*"; fi; }

printf '\n'
finding "live    (${#live[@]}): $(names ${live[@]+"${live[@]}"})"
finding "dead    (${#dead[@]}): $(names ${dead[@]+"${dead[@]}"})"
finding "absent  (${#absent[@]}): $(names ${absent[@]+"${absent[@]}"})"

case "${#live[@]}" in
	0) verdict="consensus impossible — digest must fall back to --no-llm" ;;
	1) verdict="single-runner: consensus is silently a no-op (SPEC §5), so this is the DEFAULT path here" ;;
	2) verdict="two voters: ties are possible and resolve to the \"I'm not sure\" section, not a coin flip" ;;
	*) verdict="${#live[@]} voters: a real majority vote" ;;
esac
finding "consensus on this machine: $verdict"

assert "at least one runner binary is installed" test $(( ${#live[@]} + ${#dead[@]} )) -ge 1
assert "at least one installed runner can actually answer" test "${#live[@]}" -ge 1

note "consequence for C2: probe, do not trust PATH; cap the probe; name live, dead"
note "and absent runners in the digest footer, because SPEC §5 promises the footer"
note "says who voted — and 'codex was broken' is a fact the reader is owed."

finish
