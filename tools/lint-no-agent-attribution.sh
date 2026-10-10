#!/usr/bin/env bash
# Fail if a commit message carries a Co-Authored-By trailer or an AI agent's session marker or generated-by footer.
#
# Why: this repo's commits are authored by the people who own them. An agent that helped write a change is a tool, like an editor
# or a compiler, and tools do not take authorship credit. A co-author trailer also feeds GitHub's contributor graph and
# `git shortlog`, which are read as statements about who maintains the project.
#
# Every Co-Authored-By trailer is refused, whoever it names, except the one GitHub itself writes for dependabot when a dependabot
# PR is squash-merged: no person or agent wrote that line, and refusing it would fail the push of every dependency bump to main.
#
# Two entry points, because the two ways a commit reaches main need different checks:
#   msg <file>    a single commit-message file, for the local commit-msg hook
#   range <base>..<head>   every commit in a range, for CI
#
# The range form is the one that matters most: the offending commits this gate was written for were SQUASH MERGES composed by
# GitHub from a PR body, which never pass through a local hook at all.
set -euo pipefail

# Patterns are built so this script's own source does not match them, which would make the scanner unable to live in the repo it
# scans. Each is assembled from fragments rather than written out whole.
CO_AUTHOR="^[[:space:]]*Co-Authored-By:"
SESSION="^""Claude""-Session:"
GENERATED="Generated with \[""Claude"" Code\]"
PATTERN="${CO_AUTHOR}|${SESSION}|${GENERATED}"
# The one co-author trailer allowed: GitHub's own, on a squash-merged dependabot PR.
ALLOWED="Co-Authored-By: dependabot\[bot\] <49699333\+dependabot\[bot\]@users\.noreply\.github\.com>"

# offending prints the numbered lines of a message that the gate refuses. Matching ignores case, because git and GitHub write the
# trailer key in more than one casing.
offending() {
  { grep -niE "$PATTERN" || true; } | { grep -viE ":[[:space:]]*${ALLOWED}[[:space:]]*$" || true; }
}

fail() {
  echo "error: commit message carries a co-author trailer or agent attribution" >&2
  echo >&2
  printf '%s\n' "$1" | sed 's/^/    /' >&2
  echo >&2
  echo "This repo takes no Co-Authored-By trailers and does not list agents as authors. Remove the line and commit again." >&2
  echo "If Claude Code is adding it automatically, set \"attribution\": {\"commit\": \"\", \"pr\": \"\", \"sessionUrl\": false} in .claude/settings.json." >&2
  exit 1
}

case "${1:-}" in
  msg)
    file="${2:?usage: $0 msg <commit-message-file>}"
    # Strip the comment block git appends to the template; it is not part of the message.
    body=$(grep -v '^#' "$file" || true)
    offending=$(printf '%s\n' "$body" | offending)
    # An `if` rather than `[ ... ] && fail`: as the last statement in the branch, a false test would be the script's exit status
    # under `set -e`, so a CLEAN message would report failure. That inversion is exactly the bug a gate must not have.
    if [ -n "$offending" ]; then
      fail "$offending"
    fi
    ;;
  range)
    range="${2:?usage: $0 range <base>..<head>}"
    found=0
    while read -r sha; do
      [ -z "$sha" ] && continue
      offending=$(git log -1 --format='%B' "$sha" | offending)
      if [ -n "$offending" ]; then
        found=1
        echo "  $(git log -1 --format='%h %s' "$sha")" >&2
        printf '%s\n' "$offending" | sed 's/^/      /' >&2
      fi
    done < <(git rev-list "$range")
    if [ "$found" -ne 0 ]; then
      echo >&2
      echo "error: the commits above carry a co-author trailer or agent attribution; this repo takes neither" >&2
      exit 1
    fi
    ;;
  *)
    echo "usage: $0 msg <commit-message-file> | $0 range <base>..<head>" >&2
    exit 2
    ;;
esac
