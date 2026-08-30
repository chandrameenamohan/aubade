#!/usr/bin/env bash
#
# install-hooks.sh — install the local git hooks into this clone.
#
# .git/hooks is not tracked by git, so a fresh clone has no pre-commit hook and
# no gate. Run this once after cloning (or `make hooks`). CI does not need it:
# the workflow runs `make check` directly, so a contributor who never installs
# the hook is still caught at the pull request — just minutes later instead of
# seconds.
#
# Two things this script has to get right, both of which cost us a failed test
# before we noticed them:
#
#   1. core.hooksPath. This repo sets it (beads points it at .beads/hooks), and
#      when it is set git ignores .git/hooks ENTIRELY. Installing to .git/hooks
#      alone produces a hook file that looks installed, is executable, runs fine
#      by hand, and never fires on commit. So we install into the *effective*
#      hooks directory that git actually reads, and additionally into .git/hooks
#      so the hook is where a reader expects to find it.
#
#   2. Co-existence. The effective pre-commit may already be owned by another
#      tool (beads writes a marker-delimited block and falls through on success).
#      We append our block after theirs rather than replacing it — clobbering a
#      teammate's tooling to install your own gate is how gates get uninstalled.
#
# Idempotent: re-running detects the aubade-gate marker and leaves things alone.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

MARKER_BEGIN="# --- BEGIN AUBADE GATE ---"
MARKER_END="# --- END AUBADE GATE ---"

# The gate block, POSIX sh: it may be appended to another tool's /bin/sh hook.
gate_block() {
	cat <<'BLOCK'

# --- BEGIN AUBADE GATE ---
# Installed by scripts/install-hooks.sh (or `make hooks`). The gate's definition
# lives in the tracked .claude/hooks/gate.sh, so this block cannot drift from it.
_aubade_root=$(git rev-parse --show-toplevel)
_aubade_gate="$_aubade_root/.claude/hooks/gate.sh"
if [ ! -x "$_aubade_gate" ]; then
  echo "pre-commit: $_aubade_gate missing or not executable; run scripts/install-hooks.sh" >&2
  exit 1
fi
if ! "$_aubade_gate"; then
  echo >&2
  echo "pre-commit: gate is RED — commit refused. Reproduce with: make check" >&2
  echo "(escape hatch, for genuine emergencies only: git commit --no-verify)" >&2
  exit 1
fi
# --- END AUBADE GATE ---
BLOCK
}

install_into() {
	local target="$1" label="$2"

	mkdir -p "$(dirname "$target")"

	if [[ -e "$target" && ! -f "$target" ]]; then
		echo "install-hooks: $target is not a regular file; skipping $label" >&2
		return 0
	fi

	if [[ -f "$target" ]] && grep -q -- "$MARKER_BEGIN" "$target" 2>/dev/null; then
		echo "install-hooks: $label already has the gate — unchanged"
		chmod +x "$target"
		return 0
	fi

	if [[ -f "$target" && -s "$target" ]]; then
		# Something else owns this hook. Keep it, append after it.
		cp "$target" "$target.pre-aubade.bak"
		gate_block >>"$target"
		echo "install-hooks: appended gate to existing $label (backup: $(basename "$target").pre-aubade.bak)"
	else
		{
			echo "#!/usr/bin/env sh"
			echo "# aubade pre-commit hook. See VERIFICATION.md."
			gate_block
		} >"$target"
		echo "install-hooks: installed $label"
	fi

	chmod +x "$target"
}

# The directory git ACTUALLY reads (honors core.hooksPath).
EFFECTIVE_HOOKS="$(git rev-parse --git-path hooks)"
[[ "$EFFECTIVE_HOOKS" = /* ]] || EFFECTIVE_HOOKS="$REPO_ROOT/$EFFECTIVE_HOOKS"
EFFECTIVE_HOOKS="$(cd "$EFFECTIVE_HOOKS" 2>/dev/null && pwd || echo "$EFFECTIVE_HOOKS")"

GIT_DIR="$(cd "$(git rev-parse --git-dir)" && pwd)"
PLAIN_HOOKS="$GIT_DIR/hooks"

install_into "$EFFECTIVE_HOOKS/pre-commit" "pre-commit ($EFFECTIVE_HOOKS)"

if [[ "$EFFECTIVE_HOOKS" != "$PLAIN_HOOKS" ]]; then
	install_into "$PLAIN_HOOKS/pre-commit" "pre-commit ($PLAIN_HOOKS)"
	echo
	echo "install-hooks: NOTE core.hooksPath = $(git config --get core.hooksPath)"
	echo "               git reads $EFFECTIVE_HOOKS, NOT $PLAIN_HOOKS."
	echo "               Both are installed; the effective one is the one that fires."
fi

echo "install-hooks: done. Verify the gate itself with: .claude/hooks/gate.sh"
