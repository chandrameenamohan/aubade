#!/usr/bin/env bash
#
# stop-gate.sh — Claude Code Stop hook.
#
# Under PW_LOOP=1 (an unattended builder loop) this refuses to let the agent
# stop on a red tree: exit 2 hands the failure output back to the model as
# something it must fix, which is the whole point of running a loop unattended.
#
# Without PW_LOOP it is a silent no-op. An interactive session should not have a
# full `make check` fired at the end of every turn — that is what the pre-commit
# hook is for, and a hook that punishes ordinary conversation gets disabled by
# the human within a day.

set -uo pipefail

if [[ "${PW_LOOP:-}" != "1" ]]; then
	exit 0
fi

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
GATE="$REPO_ROOT/.claude/hooks/gate.sh"

if [[ ! -x "$GATE" ]]; then
	echo "stop-gate: $GATE is missing or not executable — cannot verify the tree." >&2
	exit 2
fi

output="$("$GATE" 2>&1)"
status=$?

if [[ "$status" -ne 0 ]]; then
	{
		echo "BLOCKED: the verification gate is red. Fix it before stopping."
		echo
		echo "$output"
		echo
		echo "(reproduce with: make check)"
	} >&2
	exit 2
fi

exit 0
