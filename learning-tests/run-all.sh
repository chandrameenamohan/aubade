#!/usr/bin/env bash
#
# run-all.sh — run every learning test and tally the findings.
#
# These are NOT part of `make check` and never will be. They call real model
# CLIs: they cost money, they need auth, and they are non-deterministic — three
# separate disqualifications from a gate (VERIFICATION.md §2, "non-deterministic
# checks never gate"). Run them by hand when a dependency, a CLI version, or an
# assumption changes.
#
#   bash learning-tests/run-all.sh          # everything
#   bash learning-tests/run-all.sh 03 05    # just those
#
# Exit 1 only when a test is CONTRADICTED — reality diverged from a header
# comment, and the comment has to be corrected. INCONCLUSIVE (a dependency that
# is absent, unauthenticated, or over budget) is a finding, and findings are the
# output of this directory, not its failure mode.

set -uo pipefail

LT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

if [[ -t 1 ]]; then
	B=$'\033[1m'; R=$'\033[0m'; GREEN=$'\033[32m'; RED=$'\033[31m'; YELLOW=$'\033[33m'
else
	B=''; R=''; GREEN=''; RED=''; YELLOW=''
fi

tests=()
if (( $# > 0 )); then
	for want in "$@"; do
		for f in "$LT_DIR/${want}"*.sh; do
			[[ -f "$f" ]] && tests+=("$f")
		done
	done
else
	for f in "$LT_DIR"/[0-9][0-9]-*.sh; do
		[[ -f "$f" ]] && tests+=("$f")
	done
fi

if (( ${#tests[@]} == 0 )); then
	echo "run-all: no learning tests matched" >&2
	exit 1
fi

names=()
codes=()
contradicted=0

for t in "${tests[@]}"; do
	bash "$t"
	rc=$?
	names+=("$(basename "$t")")
	codes+=("$rc")
	(( rc == 1 )) && contradicted=$((contradicted + 1))
done

printf '\n%s══ learning-tests summary ══%s\n' "$B" "$R"
for i in "${!names[@]}"; do
	case "${codes[$i]}" in
		0) printf '  %sCONFIRMED   %s %s\n' "$GREEN" "$R" "${names[$i]}" ;;
		1) printf '  %sCONTRADICTED%s %s\n' "$RED" "$R" "${names[$i]}" ;;
		2) printf '  %sINCONCLUSIVE%s %s\n' "$YELLOW" "$R" "${names[$i]}" ;;
		*) printf '  %sERROR (%s)  %s %s\n' "$RED" "${codes[$i]}" "$R" "${names[$i]}" ;;
	esac
done

if (( contradicted > 0 )); then
	printf '\n%s%d test(s) contradicted their own header comment. Read them and correct the comment.%s\n' \
		"$RED$B" "$contradicted" "$R"
	exit 1
fi
printf '\n%sNo contradictions. See learning-tests/README.md for what the findings mean.%s\n' "$B" "$R"
exit 0
