# shellcheck shell=bash
#
# lib.sh — shared plumbing for the learning tests.
#
# Sourced, never executed. Gives every test the same vocabulary so a reader can
# skim a run and know which lines are claims, which are evidence, and which are
# corrections to a claim we got wrong.
#
# EXIT CODE CONTRACT — the whole point of this file:
#
#   0  CONFIRMED    every assertion held; the header comment matches reality
#   1  CONTRADICTED an assertion failed; the header comment is now WRONG and the
#                   test must be re-read and corrected before it is trusted
#   2  INCONCLUSIVE the dependency was absent, unauthenticated, or timed out
#
# 2 is deliberately not 1. A learning test measures a real dependency we do not
# control: "the model took longer than 90s" and "gemini is not installed" are
# findings about the world, not defects in the test. Collapsing them into
# failure teaches everyone to ignore red, and then a real contradiction goes
# unread too.

set -uo pipefail

LT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$LT_DIR/.." && pwd)"

# Wall-clock cap on any single model invocation. Exceeding it is recorded as a
# finding (INCONCLUSIVE), never as a failure — a slow runner is data the digest
# orchestrator will have to live with.
LT_BUDGET_SECS="${LT_BUDGET_SECS:-90}"

if [[ -t 1 ]]; then
	_B=$'\033[1m'; _DIM=$'\033[2m'; _R=$'\033[0m'
	_GREEN=$'\033[32m'; _RED=$'\033[31m'; _YELLOW=$'\033[33m'; _CYAN=$'\033[36m'
else
	_B=''; _DIM=''; _R=''; _GREEN=''; _RED=''; _YELLOW=''; _CYAN=''
fi

_lt_pass=0
_lt_fail=0
_lt_inconclusive=0

# header prints the test's identity. The assumption itself lives in the file's
# top comment, where it can be edited when reality disagrees.
header() {
	printf '\n%s══ %s ══%s\n' "$_B" "$1" "$_R"
}

# assumed states what we believed before running. Printing it next to the
# evidence is what makes a correction obvious instead of arguable.
assumed() { printf '%s  assumed:%s %s\n' "$_DIM" "$_R" "$*"; }

# finding is a confirmed fact about the dependency.
finding() { printf '%s  finding:%s %s\n' "$_CYAN" "$_R" "$*"; }

# correction is a finding that overturned the assumption. It says what changed.
correction() { printf '%s  CORRECTION:%s %s\n' "$_YELLOW$_B" "$_R" "$*"; }

# evidence is raw command output, indented so it reads as quotation.
evidence() { sed 's/^/      │ /' <<<"$*"; }

# note is context that is neither claim nor evidence.
note() { printf '%s  note:%s %s\n' "$_DIM" "$_R" "$*"; }

assert() {
	local what="$1"; shift
	if "$@"; then
		_lt_pass=$((_lt_pass + 1))
		printf '%s  ✔ %s%s\n' "$_GREEN" "$what" "$_R"
	else
		_lt_fail=$((_lt_fail + 1))
		printf '%s  ✘ %s%s\n' "$_RED" "$what" "$_R"
	fi
}

# assert_eq / assert_contains are the two shapes every test here needs.
assert_eq() {
	local what="$1" got="$2" want="$3"
	if [[ "$got" == "$want" ]]; then
		_lt_pass=$((_lt_pass + 1))
		printf '%s  ✔ %s%s %s(%s)%s\n' "$_GREEN" "$what" "$_R" "$_DIM" "$got" "$_R"
	else
		_lt_fail=$((_lt_fail + 1))
		printf '%s  ✘ %s — got %q, want %q%s\n' "$_RED" "$what" "$got" "$want" "$_R"
	fi
}

assert_contains() {
	local what="$1" haystack="$2" needle="$3"
	if [[ "$haystack" == *"$needle"* ]]; then
		_lt_pass=$((_lt_pass + 1))
		printf '%s  ✔ %s%s\n' "$_GREEN" "$what" "$_R"
	else
		_lt_fail=$((_lt_fail + 1))
		printf '%s  ✘ %s — %q not found in output%s\n' "$_RED" "$what" "$needle" "$_R"
	fi
}

# inconclusive records that the world, not the code, stopped us. It sets the
# exit code to 2 unless a real contradiction has already claimed 1.
inconclusive() {
	_lt_inconclusive=$((_lt_inconclusive + 1))
	printf '%s  ⊘ INCONCLUSIVE: %s%s\n' "$_YELLOW" "$*" "$_R"
}

# need declares a prerequisite. A missing tool ends the test at INCONCLUSIVE
# immediately: a learning test that silently skips its own subject is worse than
# no test, because the green tally still counts it.
need() {
	local cmd="$1" why="${2:-}"
	if ! command -v "$cmd" >/dev/null 2>&1; then
		header "prerequisite missing: $cmd"
		inconclusive "$cmd is not on PATH${why:+ — $why}"
		finish
	fi
}

# run_capped runs a command under LT_BUDGET_SECS and returns 124 on timeout,
# matching coreutils `timeout`. It falls back to a bash watchdog because
# `timeout` is a homebrew/coreutils install on macOS, not a given.
run_capped() {
	local secs="$1"; shift
	if command -v timeout >/dev/null 2>&1; then
		timeout "$secs" "$@"
		return $?
	fi
	if command -v gtimeout >/dev/null 2>&1; then
		gtimeout "$secs" "$@"
		return $?
	fi

	"$@" &
	local pid=$! rc=0
	( sleep "$secs"; kill -TERM "$pid" 2>/dev/null ) &
	local watchdog=$!
	wait "$pid" || rc=$?
	kill -TERM "$watchdog" 2>/dev/null
	wait "$watchdog" 2>/dev/null || true
	# A signalled child is indistinguishable from a slow one here; the budget is
	# the only thing that kills it, so report the timeout code.
	if (( rc >= 128 )); then
		return 124
	fi
	return "$rc"
}

# timed_out is the readable form of the run_capped contract.
timed_out() { [[ "$1" -eq 124 ]]; }

# finish prints the tally and exits on the contract above.
finish() {
	printf '\n  %s%d passed, %d failed, %d inconclusive%s\n' \
		"$_B" "$_lt_pass" "$_lt_fail" "$_lt_inconclusive" "$_R"
	if (( _lt_fail > 0 )); then
		printf '%s  CONTRADICTED — the header comment in this file no longer matches reality.%s\n' "$_RED$_B" "$_R"
		printf '  Re-run by hand, then edit the comment to say what changed.\n'
		exit 1
	fi
	if (( _lt_inconclusive > 0 )); then
		printf '%s  INCONCLUSIVE — dependency absent, unauthenticated, or over budget.%s\n' "$_YELLOW" "$_R"
		exit 2
	fi
	printf '%s  CONFIRMED%s\n' "$_GREEN$_B" "$_R"
	exit 0
}
