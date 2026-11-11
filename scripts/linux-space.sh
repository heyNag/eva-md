#!/bin/bash
# usage: scripts/linux-space.sh [--prune [MINUTES]]
#
# Shows what eva-md's checks have left on the disk of a Linux cloud session,
# and with --prune frees it. A cloud session has a fixed allowance of disk,
# far smaller than the disk df names: when it is spent every write fails,
# in the middle of whatever was running, and the machine may be restarted.
# Checks are what spend it: Go's build cache holds each package under the
# folder it was built in, every run of linux-check.sh builds in a snapshot
# with a folder of its own, and each worktree an agent works in is one
# more folder, so the caches only grow. Run from the main checkout or from
# any of its worktrees, it counts the worktrees of the main checkout.
#
# With no argument it prints the space left and what uses it, and changes
# nothing. With --prune it removes, and nothing else:
#
# - entries of the Go build caches, as Go's own trim takes them: in each of
#   the folders 00 to ff, a <hash>-a or <hash>-d that no build has used for
#   MINUTES, 100 unless given. A cache is this user's and the user
#   eva-md-check's shared one, which linux-check.sh from before commit
#   7d824ae still fills, as builders on older branches run it. A folder is
#   taken for a cache only when it is no link and its README is the one Go
#   writes. The fuzz corpus Go keeps beside the entries, and anything else
#   in the folder, stays. Go trims its own cache the same way after five
#   days. It renews the time of an entry it uses once that is an hour old,
#   so a running build's entries are younger than an hour plus the build's
#   length: the script refuses MINUTES under 90 while a go command runs,
#   and any age is safe when none does;
# - snapshots that linux-check.sh and linux-gate.sh left more than three
#   hours ago, as a run that was killed cannot remove its own.
#
# It prints the size of what it removed, not the change in free space,
# which other sessions change as it runs.
#
# It is for a cloud session, which runs as root. Run as anyone else it
# only reports: your own machine's build cache is yours to keep.
set -u

if [ "$(uname -s)" != Linux ]; then
  echo "linux-space.sh is for Linux only." >&2
  exit 2
fi

usage() {
  echo "usage: scripts/linux-space.sh [--prune [MINUTES]]" >&2
  exit 2
}

prune=0
age=100
if [ "${1:-}" = --prune ]; then
  [ $# -le 2 ] || usage
  prune=1
  if [ $# -eq 2 ]; then
    case "$2" in
    '' | *[!0-9]*) echo "linux-space.sh: MINUTES is a whole number, not '$2'" >&2; exit 2 ;;
    ??????*) echo "linux-space.sh: MINUTES is at most 99999, not $2" >&2; exit 2 ;;
    esac
    age=$((10#$2))
  fi
elif [ $# -gt 0 ]; then
  usage
fi

cd "$(dirname "$0")/.." || exit 2
repo=$PWD
# The worktrees are under the main checkout, also when this runs in one of
# them.
main=$(git -C "$repo" rev-parse --path-format=absolute --git-common-dir 2>/dev/null)
case "$main" in
*/.git) main=${main%/.git} ;;
*) main=$repo ;;
esac
user=eva-md-check
home=$(getent passwd "$user" 2>/dev/null | cut -d: -f6)
own=$(go env GOCACHE 2>/dev/null)

free() { df --output=avail -BM / | tail -n 1 | tr -dc 0-9; }
size() { [ -e "$1" ] && du -xsm "$1" 2>/dev/null | cut -f1 || echo 0; }

report() {
  echo "Free: $(free) MB"
  echo "This user's Go build cache: $(size "${own:-/nonexistent}") MB"
  if [ -n "$home" ]; then
    echo "The check user's old shared build cache: $(size "$home/.cache/go-build") MB"
    echo "Snapshots of checks, running or left behind: $(find "$home" -mindepth 1 -maxdepth 1 -type d \( -name 'check-??????' -o -name 'gate-??????' \) 2>/dev/null | wc -l), $(size "$home") MB with the cache"
  fi
  echo "Worktrees under $main/.claude/worktrees: $(find "$main/.claude/worktrees" -mindepth 1 -maxdepth 1 -type d 2>/dev/null | wc -l), $(size "$main/.claude/worktrees") MB"
}

report
[ "$prune" = 1 ] || exit 0

if [ "$(id -u)" -ne 0 ]; then
  echo "Not root, so nothing is removed: on your own machine, go clean -cache is yours to run."
  exit 0
fi

# A go command that is running may use entries younger than an hour plus
# its own length. pgrep missing counts as one running.
if [ "$age" -lt 90 ] && { ! command -v pgrep >/dev/null || pgrep -x go >/dev/null; }; then
  echo "linux-space.sh: a go command is running, and with MINUTES under 90 the prune could remove what it is using. Use 90 or more." >&2
  exit 2
fi

removed_kb=0
# Removes each path find prints, NUL-separated, and adds up its size.
remove_listed() {
  local p kb
  while IFS= read -r -d '' p; do
    kb=$(du -xsk -- "$p" 2>/dev/null | cut -f1)
    rm -rf -- "$p" 2>/dev/null && removed_kb=$((removed_kb + ${kb:-0}))
  done
}

for cache in "$own" "${home:+$home/.cache/go-build}"; do
  [ -n "$cache" ] && [ -d "$cache" ] && [ ! -L "$cache" ] || continue
  grep -qs '^This directory holds cached build artifacts from the Go build system\.$' "$cache/README" || continue
  for sub in "$cache"/[0-9a-f][0-9a-f]; do
    [ -d "$sub" ] && [ ! -L "$sub" ] || continue
    # An entry is a file, or a folder when it holds an executable; Go renews
    # the time of the folder, not of the file in it.
    kb=$(find "$sub" -mindepth 1 -maxdepth 1 -type f -regextype posix-extended \
      -regex '.*/[0-9a-f]{64}-[ad]' -mmin +"$age" -printf '%k\n' -delete 2>/dev/null |
      awk '{ s += $1 } END { print s + 0 }')
    removed_kb=$((removed_kb + kb))
    remove_listed < <(find "$sub" -mindepth 1 -maxdepth 1 -type d -regextype posix-extended \
      -regex '.*/[0-9a-f]{64}-d' -mmin +"$age" -print0 2>/dev/null)
  done
done
if [ -n "$home" ] && [ -d "$home" ] && [ ! -L "$home" ]; then
  remove_listed < <(find "$home" -mindepth 1 -maxdepth 1 -type d \
    \( -name 'check-??????' -o -name 'gate-??????' \) -mmin +180 -print0 2>/dev/null)
fi
echo "Removed $((removed_kb / 1024)) MB: build cache entries unused for $age minutes, and snapshots older than three hours."
report
