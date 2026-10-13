#!/bin/sh
# Validate only the subject; Git manages commit trailers and body formatting.
set -eu
if [ "$#" -ne 1 ] || [ ! -f "$1" ]; then
  echo "Usage: check-commit-message.sh <message-file>" >&2
  exit 2
fi
subject=$(sed -n '1p' "$1")
# Preserve Git-generated merge and revert subjects.
case "$subject" in
  "Merge "*|'Revert "'*) exit 0 ;;
esac
if printf '%s\n' "$subject" | LC_ALL=C grep -Eq '^(feat|fix|docs|style|refactor|perf|test|build|ci|chore|revert)(\([a-zA-Z0-9_./-]+\))?!?: [^[:space:]].*$'; then
  exit 0
fi
cat >&2 <<'MESSAGE'
Use a Conventional Commit subject:
  <type>[optional scope][!]: <description>
Examples:
  feat(espn): import league rosters
  fix(sleeper): handle missing matchup
  chore(repo): configure repository defaults
Types: feat, fix, docs, style, refactor, perf, test, build, ci, chore, revert
MESSAGE
exit 1
