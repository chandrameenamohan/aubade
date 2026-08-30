#!/usr/bin/env bash
#
# e2e-regression.sh — the gate's end-to-end scenario (SPEC "End-to-end
# verification scenario").
#
#     aubade-lab generate --seed 42 --today <fixed> --out data/
#     aubade digest --no-llm --out out/
#     aubade-lab eval --out out/
#
# Three binaries' worth of behaviour, joined: the exam, the student, and the
# grader. Everything upstream of this is verified against fixtures its own
# author chose — this is the only check that says the answers are right.
#
# Why it drives the real binaries instead of calling into the packages: the
# thing we ship is the thing we test. `go test` already runs the same scenario
# in-process (internal/eval/reference_test.go), which is faster and fails inside
# the package that broke; this one additionally proves the two commands can be
# driven from a shell, that they write the files they promise, and that the
# harness exits non-zero when it should.
#
# It is deterministic and needs no key, no network and no model, which is what
# earns it a place in `make check`. The agentic capability suite is deliberately
# not here: non-deterministic checks never gate (VERIFICATION.md §2). Run it with
# `bin/aubade-lab eval --capability`, or `make check-agentic` for the live
# end-to-end.
#
# Runtime: a few seconds. If this ever stops being fast, it stops being run.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

# Pinned anchor date for the regression corpus. The dataset is generated
# relative to this, so the committed reference digest stays valid.
TODAY="${AUBADE_E2E_TODAY:-2026-08-30}"
SEED="${AUBADE_E2E_SEED:-42}"

# The scenario writes where CI looks for its artifacts: out/digest.md and
# out/scorecard.md are uploaded from a run of this script.
DATA="${AUBADE_E2E_DATA:-data}"
OUT="${AUBADE_E2E_OUT:-out}"

if [[ -t 1 ]]; then
	RED=$'\033[31m'; GREEN=$'\033[32m'; BOLD=$'\033[1m'; RESET=$'\033[0m'
else
	RED=''; GREEN=''; BOLD=''; RESET=''
fi

fail() {
	printf '%s%se2e FAIL:%s %s\n' "$RED" "$BOLD" "$RESET" "$*" >&2
	exit 1
}
step() { printf '%s    %s%s\n' "$BOLD" "$*" "$RESET"; }

for b in aubade aubade-lab; do
	[[ -x "bin/$b" ]] || fail "bin/$b not built (run 'make build')"
done

# ── 1 · write the exam ──────────────────────────────────────────────────────
step "generate --seed $SEED --today $TODAY --out $DATA/"
./bin/aubade-lab generate --seed "$SEED" --today "$TODAY" --out "$DATA" >/dev/null \
	|| fail "the generator did not run"

# ── 2 · sit it ──────────────────────────────────────────────────────────────
step "digest --no-llm --data $DATA/ --out $OUT/"
./bin/aubade digest --no-llm --data "$DATA" --today "$TODAY" --out "$OUT" >/dev/null \
	|| fail "the deterministic digest did not run"

[[ -s "$OUT/digest.md" ]]   || fail "no page at $OUT/digest.md"
[[ -s "$OUT/signals.json" ]] || fail "no fact base at $OUT/signals.json"

# ── 3 · grade it ────────────────────────────────────────────────────────────
# The harness prints the whole scorecard, which is what a reader of a failed CI
# run wants in the log. Its exit code is the gate: non-zero on any missed trap.
step "eval --data $DATA/ --out $OUT/"
if ! ./bin/aubade-lab eval --data "$DATA" --today "$TODAY" --out "$OUT"; then
	fail "the regression suite is RED — see the scorecard above and $OUT/scorecard.md"
fi

[[ -s "$OUT/scorecard.md" ]] || fail "the harness exited 0 without writing $OUT/scorecard.md"

printf '%s    e2e: GREEN — %s and %s written%s\n' "$GREEN" "$OUT/digest.md" "$OUT/scorecard.md" "$RESET"
