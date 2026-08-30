#!/usr/bin/env bash
#
# agentic-e2e.sh — the live end of bead C3, run by `make check-agentic`.
#
# This drives the REAL claude CLI: it costs money, needs auth, and is
# non-deterministic. Those are three separate disqualifications from a gate
# (VERIFICATION.md §2: non-deterministic checks never gate), so this target is
# deliberately NOT part of `make check` and never will be.
#
# What it proves that no unit test can:
#
#   - claude actually accepts the flags aubade hands it, on the version
#     installed here — the learning tests pinned those contracts once; this
#     notices when one of them moves;
#   - the allowlisted loop really calls `aubade tool`, which is the whole
#     keystone claim (facts enter only through cited tool output);
#   - the composed page survives aubade's own citation check, so the model
#     cited real refs rather than plausible ones;
#   - --customize reshapes the page and the honesty floor is still there
#     afterwards.
#
# It SKIPS LOUDLY when claude is absent. A check that quietly does nothing is
# worse than no check, because someone eventually cites it as evidence.
#
# Run:   make check-agentic
# Cost:  two agentic digests over the seeded corpus — each one is up to five
#        one-shot consensus votes plus one bounded tool loop, so budget a few
#        minutes and a few cents. Nothing here is cached.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

TODAY="${AUBADE_E2E_TODAY:-2026-08-30}"
SEED="${AUBADE_E2E_SEED:-42}"

if [[ -t 1 ]]; then
	RED=$'\033[31m'; GREEN=$'\033[32m'; YELLOW=$'\033[33m'; BOLD=$'\033[1m'; RESET=$'\033[0m'
else
	RED=''; GREEN=''; YELLOW=''; BOLD=''; RESET=''
fi

fail() {
	echo "${RED}${BOLD}check-agentic FAIL:${RESET} $*" >&2
	exit 1
}
pass() { echo "  ${GREEN}ok${RESET}  $*"; }
step() { echo "${BOLD}==> $*${RESET}"; }

# ── skip, loudly ────────────────────────────────────────────────────────────
if ! command -v claude >/dev/null 2>&1; then
	cat <<BANNER
${YELLOW}${BOLD}
================================================================================
  SKIPPED: the agentic capability check needs the claude CLI
================================================================================
  claude is not on PATH, so nothing below ran. This is a SKIP, not a pass:
  the agentic digest is unverified on this machine.

  The deterministic half is covered by \`make check\`, which needs no runner.
================================================================================
${RESET}
BANNER
	exit 0
fi

step "claude $(claude --version 2>&1 | head -1)"

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
DATA="$WORK/data"
OUT="$WORK/out"
CUSTOM="$WORK/prompt.md"

step "building binaries"
make build >/dev/null

step "generating the seeded corpus (seed $SEED, today $TODAY)"
./bin/aubade-lab generate --seed "$SEED" --today "$TODAY" --out "$DATA" >/dev/null

# ── 1 · the default agentic digest ──────────────────────────────────────────
step "aubade digest (agentic, consensus on)"
if ! ./bin/aubade digest --today "$TODAY" --data "$DATA" --out "$OUT" >"$WORK/run.log" 2>"$WORK/run.err"; then
	tail -20 "$WORK/run.log" "$WORK/run.err" >&2
	fail "the agentic digest exited non-zero"
fi

PAGE="$OUT/digest.md"
[[ -s "$PAGE" ]] || fail "no digest at $PAGE"
[[ -s "$OUT/signals.json" ]] || fail "no signals.json beside the digest"
[[ -s "$OUT/transcript.jsonl" ]] || fail "no transcript.jsonl for the eval harness"
pass "wrote digest.md, signals.json and transcript.jsonl"

head -1 "$PAGE" | grep -q '^# Daily Digest — ' || fail "the page does not open with the digest heading"
pass "the page has the expected heading"

# The keystone claim, observed rather than asserted: the loop called the
# toolbox, and it called nothing else (anything else would have been denied).
if ! grep -q 'aubade tool' "$OUT/transcript.jsonl"; then
	fail "the transcript shows no 'aubade tool' call — the model composed without touching the toolbox"
fi
pass "the loop drove the toolbox: $(grep -c 'aubade tool' "$OUT/transcript.jsonl") transcript line(s) mention it"

# A fallback is a real outcome and a loud one: it means the model cited
# something that is not in signals.json. Outside the gate, that is a finding
# worth failing on rather than a flake to shrug at.
if grep -q 'This page was not composed by' "$PAGE"; then
	grep -m1 'This page was not composed by' "$PAGE" >&2
	fail "the composed page was rejected by aubade's citation check (fabricated ref)"
fi
pass "every citation on the page resolved against signals.json"

grep -q '## Honesty' "$PAGE" || fail "the honesty floor is missing from the page"
grep -q 'in agentic mode' "$PAGE" || fail "the footer does not say the page was composed agentically"
grep -q 'Consensus o' "$PAGE" || fail "the footer does not name the consensus roster"
pass "honesty floor and runner footer are on the page"

# ── 2 · --customize reshapes the format and not the truth ───────────────────
cat >"$CUSTOM" <<'PROMPT'
Write the digest as a single markdown table with three columns: When, What, Who.
One row per item, most urgent first. No section headings of any kind, and no
prose paragraphs. Keep it under twenty rows.
PROMPT

step "aubade digest --customize"
CUSTOM_OUT="$WORK/out-custom"
if ! ./bin/aubade digest --today "$TODAY" --data "$DATA" --out "$CUSTOM_OUT" \
	--customize "$CUSTOM" >"$WORK/custom.log" 2>"$WORK/custom.err"; then
	tail -20 "$WORK/custom.log" "$WORK/custom.err" >&2
	fail "the customized digest exited non-zero"
fi

CUSTOM_PAGE="$CUSTOM_OUT/digest.md"
[[ -s "$CUSTOM_PAGE" ]] || fail "no customized digest written"
grep -q "Format customized by $CUSTOM" "$CUSTOM_PAGE" || fail "the footer does not say the page was customized"
grep -q '## Honesty' "$CUSTOM_PAGE" || fail "customization removed the honesty floor — the invariant floor failed"
if grep -q 'This page was not composed by' "$CUSTOM_PAGE"; then
	fail "the customized page was rejected by the citation check"
fi
if diff -q "$PAGE" "$CUSTOM_PAGE" >/dev/null; then
	fail "the customized page is byte-identical to the default one; --customize did nothing"
fi
pass "the customized page differs from the default and still carries the honesty floor"

# ── 3 · the refusal that needs no model ─────────────────────────────────────
step "aubade digest --customize --no-llm (must refuse)"
if ./bin/aubade digest --customize "$CUSTOM" --no-llm --data "$DATA" --today "$TODAY" --out "$WORK/never" >/dev/null 2>&1; then
	fail "--customize with --no-llm should be refused: there is no compose stage to reshape"
fi
pass "refused, as it should be"

printf '\n%s%scheck-agentic: GREEN%s\n' "$GREEN" "$BOLD" "$RESET"
