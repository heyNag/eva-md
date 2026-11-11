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
# more folder, so the caches only grow.
#
# With no argument it prints the space left and what uses it, and changes
# nothing. With --prune it removes, and nothing else:
#
# - files of the Go build caches (this user's, and the user eva-md-check's
#   from before linux-check.sh kept each run's cache with its snapshot)
#   that no build has used for MINUTES, 100 unless given. Go trims its own
#   cache the same way after five days. It renews the time of an entry it
#   uses once that is an hour old, so a running build's entries are younger
#   than an hour plus the build's length: do not go under 90 while checks
#   run, and any age is safe when none does;
# - snapshots that linux-check.sh and linux-gate.sh left more than three
#   hours ago, as a run that was killed cannot remove its own.
#
# It is for a cloud session, which runs as root. Run as anyone else it
# only reports: your own machine's build cache is yours to keep.
set -u

if [ "$(uname -s)" != Linux ]; then
  echo "linux-space.sh is for Linux only." >&2
  exit 2
fi

prune=0
age=100
if [ "${1:-}" = --prune ]; then
  prune=1
  if [ -n "${2:-}" ]; then
    case "$2" in
    *[!0-9]*) echo "linux-space.sh: MINUTES is a whole number, not $2" >&2; exit 2 ;;
    esac
    age=$2
  fi
elif [ $# -gt 0 ]; then
  echo "usage: scripts/linux-space.sh [--prune [MINUTES]]" >&2
  exit 2
fi

cd "$(dirname "$0")/.." || exit 2
repo=$PWD
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
  echo "Worktrees under .claude/worktrees: $(find "$repo/.claude/worktrees" -mindepth 1 -maxdepth 1 -type d 2>/dev/null | wc -l), $(size "$repo/.claude/worktrees") MB"
}

report
[ "$prune" = 1 ] || exit 0

if [ "$(id -u)" -ne 0 ]; then
  echo "Not root, so nothing is removed: on your own machine, go clean -cache is yours to run."
  exit 0
fi

before=$(free)
for cache in "$own" "${home:+$home/.cache/go-build}"; do
  [ -n "$cache" ] && [ -d "$cache" ] || continue
  # Only a Go build cache: it holds a README that says so.
  [ -f "$cache/README" ] || [ -f "$cache/trim.txt" ] || continue
  find "$cache" -mindepth 2 -type f -mmin +"$age" -delete 2>/dev/null
done
if [ -n "$home" ] && [ -d "$home" ]; then
  find "$home" -mindepth 1 -maxdepth 1 -type d \( -name 'check-??????' -o -name 'gate-??????' \) -mmin +180 \
    -exec rm -rf {} + 2>/dev/null
fi
echo "Removed build cache files unused for $age minutes and snapshots older than three hours: $(($(free) - before)) MB freed."
report
