#!/usr/bin/env bash
#
# gate.sh — the single source of truth for "is the tree green?".
#
# Everything that gates calls this and nothing else: the git pre-commit hook,
# the Stop hook (under PW_LOOP=1), and CI's `make check`. One definition means
# the bar cannot be quietly different in three places, and lowering it is a
# visible one-line diff rather than a config drift.
#
# Exits with make check's exit code, unchanged.

set -uo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$REPO_ROOT" || exit 1

make check
exit $?
