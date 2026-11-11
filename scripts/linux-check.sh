#!/bin/bash
# usage: scripts/linux-check.sh [go test arguments...]
#
# Runs the checks in AGENTS.md's "Before you finish a change" on Linux, where
# cloud sessions and CI's test-linux job work on eva-md: gofmt, go vet, the
# tests, the tests with the race detector, the build and the benchmark.
# Arguments, such as package patterns or -run, go to both test runs instead of
# ./... . eva-md runs only on macOS, where the plain commands in AGENTS.md are
# the checks. On Linux they need three things the plain commands don't give:
#
# - A TMPDIR outside /tmp and /var/tmp. eva-md's hooks skip folders under them
#   as temporary, and t.TempDir() puts every test project in TMPDIR, which is
#   /tmp on Linux (on macOS it is under /var/folders). The script makes a
#   private one and removes it at the end.
# - An ordinary user. Cloud sessions run as root, and as root about 20 tests
#   fail by design: eva-md keeps no identity cache for root, and root ignores
#   file modes. So run as root, the script runs the tests and the benchmark
#   as the user eva-md-check, which it creates if missing. Run as anyone
#   else, as in CI, it runs everything on the checkout directly.
# - For that user, a snapshot and a module cache. git refuses a repository
#   another user owns, and the tests set their own git config, so the user
#   gets a copy of the working tree, uncommitted changes included, without
#   .git, bin/ and .claude/ (where other sessions keep their worktrees),
#   committed into a fresh repository it owns. It can't read
#   root's Go module cache either, so root fills a shared read-only one,
#   /var/cache/eva-md/gomod, with the modules and the Go toolchain go.mod
#   names, and the user runs offline from it. Its build cache is the run's
#   own and goes with the snapshot: a snapshot has a path of its own, so
#   what one run builds is of no use to the next, and a cache that is kept
#   grows by about 1 GB a run until the disk is full.
#
# Git configuration in the environment, such as GIT_CONFIG_COUNT, and a commit
# identity in it, such as GIT_AUTHOR_NAME, are left as they are: the tests
# shield themselves from both, and the checks should show they do. Every step
# runs even when one fails; the script ends with a summary and fails if any
# step did.
set -u

if [ "$(uname -s)" != Linux ]; then
  echo "linux-check.sh is for Linux only; on macOS run the commands in AGENTS.md." >&2
  exit 2
fi

cd "$(dirname "$0")/.." || exit 2
repo=$PWD
[ $# -eq 0 ] && set -- ./...

user=eva-md-check
modcache=/var/cache/eva-md/gomod
work=
cleanup() {
  cd /
  [ -n "$work" ] && rm -rf "$work"
}
trap cleanup EXIT
trap 'exit 130' INT TERM

# Warn about tools some tests need, naming what will fail. Nothing is skipped.
git_version=$(git --version 2>/dev/null | awk '{print $3}')
if [ -z "$git_version" ]; then
  echo "warning: git is not installed, so most tests will fail." >&2
elif [ "$(printf '%s\n' 2.44 "$git_version" | sort -V | head -n1)" != 2.44 ]; then
  echo "warning: git $git_version is older than 2.44, so the partial-clone tests" \
    "(TestNoLazyFetchInAPartialClone in internal/coderef, TestBriefFetchesNothingInAPartialClone" \
    "in internal/resume) will fail; see docs/known-limitations.md." >&2
fi
if ! command -v ssh-keygen >/dev/null; then
  echo "warning: ssh-keygen is not installed, so the WithShowSignature tests in" \
    "internal/coderef, internal/resume and internal/update will fail." >&2
fi
if ! command -v python3 >/dev/null; then
  echo "warning: python3 is not installed, so the benchmark will fail." >&2
fi

if [ "$(id -u)" -eq 0 ]; then
  if ! id "$user" >/dev/null 2>&1; then
    useradd --create-home --user-group --shell /usr/sbin/nologin "$user" || exit 2
  fi
  home=$(getent passwd "$user" | cut -d: -f6)
  # A run that is killed outright, as by SIGKILL, can't remove its snapshot,
  # so remove those left more than three hours ago. A folder's time is when
  # its run started, or reached the benchmark, the last step, and a full run
  # takes 6 to 10 minutes, so no run still going, here or in another
  # session on this machine, is ever that old, even on a slow machine, while
  # a killed run's leftovers don't stay long.
  find "$home" -mindepth 1 -maxdepth 1 -type d -name 'check-??????' -mmin +180 \
    -exec rm -rf {} + || true
  # A cloud session has a fixed allowance of disk. A run needs about 1.5 GB
  # of it for its snapshot, its temporary files and its build cache, and
  # holds it until it ends; with less it would fail part way, with errors
  # that say nothing of the disk. So it asks for 2,500 MB, to leave a
  # margin, and 2,000 MB more for each check whose snapshot is there and
  # under three hours old, as each of those may still be running.
  free_mb=$(df --output=avail -BM "$home" 2>/dev/null | tail -n 1 | tr -dc 0-9)
  others=$(find "$home" -mindepth 1 -maxdepth 1 -type d -name 'check-??????' -mmin -180 2>/dev/null | wc -l)
  need=$((2500 + 2000 * others))
  if [ -n "$free_mb" ] && [ "$free_mb" -lt "$need" ]; then
    echo "linux-check.sh: $free_mb MB of disk is left, and a run needs 2,500 MB plus 2,000 for each of" \
      "the $others other checks whose snapshots are here, $need in all. Wait for those to end, or free" \
      "some with scripts/linux-space.sh --prune, then run this again." >&2
    exit 2
  fi
  work=$(mktemp -d "$home/check-XXXXXX") || exit 2
  src=$work/src
  mkdir "$src" "$work/tmp" "$work/gocache" || exit 2
  # pipefail, here only, so a failure in the tar that reads fails the copy too.
  (
    set -o pipefail
    tar -C "$repo" --exclude=./.git --exclude=./bin --exclude=./.claude -cf - . | tar -C "$src" -xf -
  ) || { echo "linux-check.sh: copying the working tree failed" >&2; exit 2; }
  chown -R "$user:$user" "$work" || exit 2
  # The second, empty commit lets the benchmark time the briefs that count
  # the commits since HEAD~1.
  (cd "$src" && runuser -u "$user" -- env HOME="$home" sh -c '
    git init -q &&
    git add -A &&
    git -c user.name=snapshot -c user.email=snapshot@example.invalid -c commit.gpgsign=false \
      commit -q --no-verify -m snapshot &&
    git -c user.name=snapshot -c user.email=snapshot@example.invalid -c commit.gpgsign=false \
      commit -q --no-verify --allow-empty -m "snapshot again"') || exit 2

  echo "Filling the shared module cache $modcache"
  mkdir -p "$modcache" &&
    GOMODCACHE=$modcache go mod download &&
    GOMODCACHE=$modcache go version >/dev/null &&
    chmod -R a+rX "$(dirname "$modcache")" || exit 2

  # Runs a command in the snapshot as the user, offline. -count=1 because
  # the run's build cache goes with it, so no run can use another's test
  # results, and go test would only write a log of each to the cache, a
  # few hundred MB a run, for nothing. Every go command ignores a flag in
  # GOFLAGS that it doesn't have, so the go commands the tests start take
  # it too.
  in_snapshot() {
    (cd "$src" && runuser -u "$user" -- env HOME="$home" TMPDIR="$work/tmp" \
      GOMODCACHE="$modcache" GOPROXY=off GOFLAGS='-mod=readonly -count=1' \
      GOCACHE="$work/gocache" GOPATH="$home/go" "$@")
  }
  bench_cwd=$src
else
  work=$(mktemp -d "${XDG_CACHE_HOME:-$HOME/.cache}/eva-md-check-XXXXXX" 2>/dev/null) ||
    { mkdir -p "${XDG_CACHE_HOME:-$HOME/.cache}" &&
      work=$(mktemp -d "${XDG_CACHE_HOME:-$HOME/.cache}/eva-md-check-XXXXXX"); } || exit 2
  mkdir "$work/tmp" || exit 2
  in_snapshot() { "$@"; }
  bench_cwd=$repo
fi

export TMPDIR=$work/tmp
case "$(realpath "$TMPDIR")/" in
/tmp/* | /private/tmp/* | /var/tmp/* | /private/var/tmp/*)
  echo "linux-check.sh: TMPDIR $TMPDIR is under a temporary folder, which eva-md's hooks skip; set HOME or XDG_CACHE_HOME elsewhere." >&2
  exit 2
  ;;
esac

summary=()
failed=0
step() {
  local name=$1
  shift
  echo
  echo "==> $name"
  if "$@"; then
    summary+=("ok    $name")
  else
    summary+=("FAIL  $name")
    failed=1
  fi
}

# The gofmt of the Go toolchain go.mod names, which go vet, test and build
# switch to, and not whichever gofmt is first on PATH, which may be older.
gofmt_check() {
  local goroot out
  goroot=$(go env GOROOT)
  if [ -z "$goroot" ] || [ ! -x "$goroot/bin/gofmt" ]; then
    echo "found no gofmt in the Go toolchain go.mod names (GOROOT ${goroot:-unknown})"
    return 1
  fi
  echo "$goroot/bin/gofmt, $(go env GOVERSION)"
  out=$("$goroot/bin/gofmt" -l cmd internal) || return 1
  [ -z "$out" ] || { echo "$out"; return 1; }
}

built=0
build() {
  go build -mod=readonly -o bin/eva-md ./cmd/eva-md && built=1
}

benchmark() {
  [ "$built" = 1 ] || { echo "not run: the build failed"; return 1; }
  if [ "$(id -u)" -eq 0 ]; then
    cp bin/eva-md "$work/eva-md" && chown "$user:$user" "$work/eva-md" || return 1
    in_snapshot python3 -I scripts/benchmark.py --binary "$work/eva-md" --cwd "$bench_cwd"
  else
    python3 -I scripts/benchmark.py --binary bin/eva-md --cwd "$bench_cwd"
  fi
}

step "gofmt -l cmd internal" gofmt_check
step "go vet ./..." go vet ./...
step "go test $*" in_snapshot go test "$@"
step "go test -race $*" in_snapshot go test -race "$@"
step "go build -mod=readonly -o bin/eva-md ./cmd/eva-md" build
step "benchmark" benchmark

echo
printf '%s\n' "${summary[@]}"
exit $failed
