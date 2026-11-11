#!/bin/bash
# usage: scripts/linux-gate.sh [BASE [benchmark.py arguments...]]
#
# Runs the latency gate on Linux, where cloud sessions work on eva-md:
# scripts/benchmark.py --compare, on the binary built from the commit BASE
# and the one built from the working tree, uncommitted changes included.
# BASE is by default the commit this branch left origin/main at, or the
# commit before HEAD when the branch has not left it. Further arguments go
# to the benchmark, as --rounds 3.
#
# It needs what scripts/linux-check.sh needs for the benchmark, and gets it
# the same way: started as root, it runs the gate as the user eva-md-check
# on a snapshot of the working tree, outside temporary folders. It prints
# the gate's table, keeps the full report in bin/latency-gate.json, and
# exits as the gate does: 0 for pass and for not judged, 1 for fail, 2 when
# it could not run. docs/v2/baselines/README.md holds the gate's rule.
#
# The gate times hooks, so it wants a quiet machine. On a busy one it says
# "not judged" and proves nothing: run it again when the load is low. CI's
# macOS job is the run of record.
set -u

if [ "$(uname -s)" != Linux ]; then
  echo "linux-gate.sh is for Linux only; on macOS run scripts/benchmark.py --compare OLD NEW." >&2
  exit 2
fi

cd "$(dirname "$0")/.." || exit 2
repo=$PWD

base=${1:-}
[ $# -gt 0 ] && shift
if [ -z "$base" ]; then
  base=$(git merge-base HEAD origin/main 2>/dev/null) || base=
  if [ -z "$base" ] || [ "$base" = "$(git rev-parse HEAD)" ]; then
    base=HEAD~1
  fi
fi
asked=$base
base=$(git rev-parse --verify --quiet "$asked^{commit}") ||
  { echo "linux-gate.sh: there is no commit $asked to compare with" >&2; exit 2; }

user=eva-md-check
work=
cleanup() {
  cd /
  [ -n "$work" ] && rm -rf "$work"
}
trap cleanup EXIT
trap 'exit 130' INT TERM

if [ "$(id -u)" -eq 0 ]; then
  if ! id "$user" >/dev/null 2>&1; then
    useradd --create-home --user-group --shell /usr/sbin/nologin "$user" || exit 2
  fi
  home=$(getent passwd "$user" | cut -d: -f6)
  work=$(mktemp -d "$home/gate-XXXXXX") || exit 2
else
  mkdir -p "${XDG_CACHE_HOME:-$HOME/.cache}" &&
    work=$(mktemp -d "${XDG_CACHE_HOME:-$HOME/.cache}/eva-md-gate-XXXXXX") || exit 2
fi
case "$(realpath "$work")/" in
/tmp/* | /private/tmp/* | /var/tmp/* | /private/var/tmp/*)
  echo "linux-gate.sh: $work is under a temporary folder, which eva-md's hooks skip; set HOME or XDG_CACHE_HOME elsewhere." >&2
  exit 2
  ;;
esac
src=$work/src
mkdir "$src" "$work/tmp" "$work/base" || exit 2

# The two binaries: the base's from its files as git holds them, the new one
# from the working tree.
echo "Building $(git rev-parse --short "$base") and the working tree"
(
  set -o pipefail
  git archive "$base" | tar -C "$work/base" -xf -
) || { echo "linux-gate.sh: reading the files of $base failed" >&2; exit 2; }
(cd "$work/base" && go build -mod=readonly -o "$work/eva-md-old" ./cmd/eva-md) || exit 2
go build -mod=readonly -o "$work/eva-md-new" ./cmd/eva-md || exit 2

# The snapshot the hooks run in: a repository the user owns, with two
# commits, as scripts/linux-check.sh makes it.
(
  set -o pipefail
  tar -C "$repo" --exclude=./.git --exclude=./bin --exclude=./.claude -cf - . | tar -C "$src" -xf -
) || { echo "linux-gate.sh: copying the working tree failed" >&2; exit 2; }
snapshot='
  git init -q &&
  git add -A &&
  git -c user.name=snapshot -c user.email=snapshot@example.invalid -c commit.gpgsign=false \
    commit -q --no-verify -m snapshot &&
  git -c user.name=snapshot -c user.email=snapshot@example.invalid -c commit.gpgsign=false \
    commit -q --no-verify --allow-empty -m "snapshot again"'
if [ "$(id -u)" -eq 0 ]; then
  chown -R "$user:$user" "$work" || exit 2
  as_user() { runuser -u "$user" -- env HOME="$home" TMPDIR="$work/tmp" "$@"; }
else
  as_user() { env TMPDIR="$work/tmp" "$@"; }
fi
(cd "$src" && as_user sh -c "$snapshot") || exit 2

echo "Load average: $(cut -d' ' -f1-3 /proc/loadavg). The gate needs a quiet machine."
(cd "$src" && as_user python3 -I scripts/benchmark.py --compare "$work/eva-md-old" "$work/eva-md-new" \
  --cwd "$src" --output "$work/gate.json" "$@")
code=$?
if [ -f "$work/gate.json" ]; then
  mkdir -p "$repo/bin" && cp "$work/gate.json" "$repo/bin/latency-gate.json" &&
    echo "The full report is in bin/latency-gate.json"
fi
exit $code
