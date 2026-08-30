#!/usr/bin/env bash
#
# e2e-regression.sh — the gate's end-to-end scenario (SPEC "End-to-end
# verification scenario").
#
# STATUS: STUB. Bead D1 replaces the body below with the real run:
#
#     aubade-lab generate --seed 42 --today "$TODAY" --out data/
#     aubade digest --no-llm --out out/
#     aubade-lab eval --out out/
#
# and this script then exits non-zero whenever a regression trap is missed.
#
# Why it exits 0 today: `make check` is wired into the pre-commit hook from bead
# A2 onward, so a red e2e before the engine exists would make it impossible to
# commit the engine. The stub is deliberately loud instead of silent — a check
# that is not yet running must say so on every single run, or the gate quietly
# becomes theatre. There is exactly one line to delete when D1 lands.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

# Pinned anchor date for the regression corpus. The dataset is generated
# relative to this, so the committed golden digest stays valid.
TODAY="${AUBADE_E2E_TODAY:-2026-08-30}"
SEED="${AUBADE_E2E_SEED:-42}"

if [[ -t 1 ]]; then
	YELLOW=$'\033[33m'; BOLD=$'\033[1m'; RESET=$'\033[0m'
else
	YELLOW=''; BOLD=''; RESET=''
fi

cat <<BANNER
${YELLOW}${BOLD}
================================================================================
  PENDING: regression eval wired in bead D1
================================================================================
  This is a STUB. The end-to-end regression scenario is NOT running yet.

  When bead D1 lands, this script will run, and fail the gate on any miss:

      bin/aubade-lab generate --seed ${SEED} --today ${TODAY} --out data/
      bin/aubade digest --no-llm --out out/
      bin/aubade-lab eval --out out/

  Until then \`make check\` proves: vet + build + unit tests only.
  Treat a green gate accordingly.
================================================================================
${RESET}
BANNER

# --- Bead D1: delete everything below this line and run the pipeline above. ---

# The one thing the stub CAN prove today: the binaries the scenario will drive
# actually exist and are runnable. A stub that checks nothing at all is worse
# than no stub, because it teaches the reader that this file is inert.
missing=0
for b in aubade aubade-lab; do
	if [[ ! -x "bin/$b" ]]; then
		echo "e2e: bin/$b not built (run 'make build')" >&2
		missing=1
	fi
done
if [[ "$missing" -ne 0 ]]; then
	exit 1
fi

echo "e2e: binaries present (bin/aubade, bin/aubade-lab); scenario pending bead D1"
exit 0
