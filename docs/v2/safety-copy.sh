#!/bin/bash
# Renews the safety copy of package branches that are not on main
# (docs/v2/leading-the-build.md, "Always ready to hand over" and lesson 18).
#
# usage: docs/v2/safety-copy.sh REF SINCE [ROUNDS]
#   REF     the branch on the remote that holds the copy
#   SINCE   only branches whose tip was committed after this time (seconds
#           since 1970) are copied, so that branches merged long ago and
#           left behind are passed over
#   ROUNDS  how many times to look, 15 minutes apart (default 1)
#
# Each round, every local branch pkg/* whose tip is neither in origin/main
# nor in the last copy becomes a parent of a new commit with main's tree,
# after main and the last copy, and that commit is pushed to REF: a
# fast-forward, and no branch to merge. Its message lists each branch it
# adds and its tip, so a lead with a fresh clone finds them with
#   git fetch origin REF
#   git log --grep='^Safety copy' --format='%h %cd%n%b' FETCH_HEAD
# It reads and writes no working tree and no index, so it is safe to run
# beside builders. The lines of $COPY_TRAILER, if set, end the message, and
# the commit is signed where the checkout signs its commits.
# Run it from the root of the checkout, on bash 3.2 or newer.
ref=$1 since=$2 rounds=${3:-1}
sign=""
if [ "$(git config --get commit.gpgsign)" = true ]; then sign=-S; fi
if [ -z "$ref" ] || [ -z "$since" ]; then
  echo "usage: docs/v2/safety-copy.sh REF SINCE [ROUNDS]" >&2
  exit 2
fi
n=0
while [ "$n" -lt "$rounds" ]; do
  n=$((n + 1))
  git fetch -q origin main || { echo "cannot fetch main" >&2; exit 2; }
  git fetch -q origin "$ref" 2>/dev/null
  prev=$(git rev-parse -q --verify "origin/$ref")
  list="" parents=""
  for b in $(git for-each-ref --format='%(refname:short)' refs/heads/pkg/); do
    t=$(git rev-parse "$b") || continue
    [ "$(git log -1 --format=%ct "$t")" -gt "$since" ] || continue
    git merge-base --is-ancestor "$t" origin/main 2>/dev/null && continue
    [ -n "$prev" ] && git merge-base --is-ancestor "$t" "$prev" 2>/dev/null && continue
    parents="$parents -p $t"
    list="$list$b $(git rev-parse --short "$t")
"
  done
  if [ -n "$parents" ]; then
    msg="Safety copy of branch tips not yet on main (not for merging)

$list${COPY_TRAILER:+
$COPY_TRAILER}"
    # $parents holds one "-p <commit>" for each tip, to be split into words,
    # and $sign is -S or nothing.
    # shellcheck disable=SC2086
    c=$(git commit-tree $sign "$(git rev-parse 'origin/main^{tree}')" -p origin/main ${prev:+-p "$prev"} $parents -m "$msg") &&
      git push -q origin "$c:refs/heads/$ref" &&
      echo "$(date -u +%H:%M) copied: $(echo "$list" | tr '\n' ' ')-> $(git rev-parse --short "$c")"
  else
    echo "$(date -u +%H:%M) nothing new"
  fi
  if [ "$n" -lt "$rounds" ]; then sleep 900; fi
done
