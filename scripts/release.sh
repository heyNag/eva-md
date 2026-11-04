#!/bin/bash
# Cuts an eva-md release. It checks that main is ready, names the tag from
# the date of main's last commit, asks, then makes a signed tag, pushes only
# that tag, watches the release workflow and checks what it published.
# docs/releasing.md explains each step.
#
# Usage: scripts/release.sh [--dry-run]
#
# It never force-pushes and never moves or deletes a tag.
set -euo pipefail

repo=heyNag/eva-md
# gh talks to github.com whatever GH_HOST says.
export GH_HOST=github.com
# A release tag, as in 2026.11.3 or 2026.11.3.2.
tag_re='^[0-9]{4}\.[1-9][0-9]?\.[1-9][0-9]?(\.[2-9]|\.[1-9][0-9]+)?$'
assets="eva-md-darwin-arm64 eva-md-darwin-amd64 install.sh uninstall.sh LICENSES.txt"

usage() {
	echo "usage: scripts/release.sh [--dry-run]"
	echo "  --dry-run  check everything and show the release, then stop before tagging"
}

dry_run=false
for arg in "$@"; do
	case $arg in
	--dry-run) dry_run=true ;;
	-h | --help)
		usage
		exit 0
		;;
	*)
		usage >&2
		exit 2
		;;
	esac
done

# fail stops at a failed check, saying what is wrong and how to fix it.
fail() {
	printf '  ✗ %s\n    Fix: %s\n' "$1" "$2" >&2
	exit 1
}

ok() { printf '  ✓ %s\n' "$1"; }

tmp=""
trap 'if [ -n "$tmp" ]; then rm -rf "$tmp"; fi' EXIT

echo "Checking main is ready to release:"

top=$(git rev-parse --show-toplevel 2>/dev/null) ||
	fail "this isn't a git checkout" "cd to your eva-md checkout and run scripts/release.sh"
if [ "$top" != "$(pwd -P)" ]; then
	fail "this isn't the checkout's root" "cd \"$top\" and run scripts/release.sh"
fi
if ! grep -qx 'module github.com/heyNag/eva-md' go.mod 2>/dev/null; then
	fail "this checkout isn't eva-md" "run it from your eva-md checkout"
fi
ok "run from the root of the eva-md checkout"

branch=$(git symbolic-ref --quiet --short HEAD || true)
if [ "$branch" != main ]; then
	fail "you're on ${branch:-a detached HEAD}, not main" "git switch main"
fi
ok "on main"

if [ -n "$(git status --porcelain --untracked-files=all)" ]; then
	fail "the tree has changes or untracked files" "commit, stash or remove them (git status lists them), then run it again"
fi
ok "the tree is clean"

for f in .github/workflows/ci.yml .github/workflows/release.yml scripts/install.sh scripts/uninstall.sh scripts/LICENSES.txt; do
	git cat-file -e "HEAD:$f" 2>/dev/null ||
		fail "main has no $f, which a release needs" "add it on main, push, and run it again"
done
ok "main has the release workflow, the install scripts and the licenses"

# Only whether a key is set: its value is never shown.
if [ -z "$(git config user.signingkey || true)" ]; then
	fail "tag signing isn't set up: user.signingkey is empty" "git config --global user.signingkey <your key>, as docs/releasing.md says"
fi
format=$(git config gpg.format || echo openpgp)
case $format in
openpgp) signer=$(git config gpg.openpgp.program || git config gpg.program || echo gpg) ;;
ssh) signer=$(git config gpg.ssh.program || echo ssh-keygen) ;;
x509) signer=$(git config gpg.x509.program || echo gpgsm) ;;
*) fail "gpg.format is $format, which git can't sign with" "git config --global gpg.format ssh, or openpgp" ;;
esac
if ! command -v "$signer" >/dev/null 2>&1; then
	fail "git signs $format tags with $signer, which isn't installed" "install it, or point gpg.$format.program at the program you sign with"
fi
ok "tag signing is set up ($format)"

# origin must be GitHub's eva-md: the tag is pushed there, and the release
# workflow runs only there. The URLs are compared, never printed, since one
# can hold a token.
is_github_url() {
	local u
	u=$(printf '%s' "$1" | tr '[:upper:]' '[:lower:]')
	u=${u%/}
	u=${u%.git}
	case $u in
	https://github.com/heynag/eva-md | git@github.com:heynag/eva-md | ssh://git@github.com/heynag/eva-md) return 0 ;;
	esac
	return 1
}
origin_url=$(git config --get remote.origin.url || true)
if [ -z "$origin_url" ]; then
	fail "this checkout has no origin" "git remote add origin git@github.com:$repo.git, then run it again"
fi
if ! is_github_url "$origin_url"; then
	fail "origin isn't github.com/$repo, where releases are made" "git remote set-url origin git@github.com:$repo.git (git remote -v shows where it points now), then run it again"
fi
for u in $(git config --get-all remote.origin.pushurl || true); do
	if ! is_github_url "$u"; then
		fail "origin pushes somewhere other than github.com/$repo" "git config --unset-all remote.origin.pushurl, then run it again"
	fi
done
ok "origin is github.com/$repo"

git fetch --quiet origin >/dev/null 2>&1 ||
	fail "git fetch origin failed" "check your network and access to origin (git fetch origin), then run it again"
head=$(git rev-parse HEAD)
short=$(git rev-parse --short HEAD)
upstream=$(git rev-parse -q --verify refs/remotes/origin/main || true)
if [ -z "$upstream" ]; then
	fail "origin has no main branch" "check that origin is github.com/$repo (git remote -v)"
elif [ "$head" != "$upstream" ]; then
	if git merge-base --is-ancestor "$upstream" "$head"; then
		fail "main has commits origin/main doesn't" "git push origin main, wait for CI to pass, then run it again"
	elif git merge-base --is-ancestor "$head" "$upstream"; then
		fail "main is behind origin/main" "git pull --ff-only, then run it again"
	else
		fail "main and origin/main have diverged" "bring main in line with origin/main (git pull --rebase), push, then run it again"
	fi
fi
ok "main is origin/main ($short)"

command -v gh >/dev/null 2>&1 ||
	fail "gh, the GitHub CLI, isn't installed" "install it from https://cli.github.com, then gh auth login --hostname github.com"
gh auth status --hostname github.com >/dev/null 2>&1 ||
	fail "gh isn't signed in to github.com" "gh auth login --hostname github.com"
ok "gh is signed in to github.com"

runs=$(gh run list --repo "$repo" --workflow ci.yml --commit "$head" --limit 20 \
	--json status,conclusion,databaseId --jq '.[] | "\(.status)/\(.conclusion)/\(.databaseId)"' 2>/dev/null) ||
	fail "gh couldn't list the CI runs" "check gh run list --repo $repo --workflow ci.yml works, then run it again"
ci=none
running=""
for run in $runs; do
	case $run in
	completed/success/*)
		ci=passed
		break
		;;
	completed/*) [ "$ci" = none ] && ci=failed ;;
	*)
		ci=running
		running=${run##*/}
		;;
	esac
done
case $ci in
none) fail "CI hasn't run for $short" "push main and wait for CI: gh run list --repo $repo --workflow ci.yml" ;;
running) fail "CI is still running for $short" "gh run watch $running --repo $repo, then run it again" ;;
failed) fail "CI failed for $short" "fix it on main and push; release once CI passes" ;;
esac
ok "CI passed for $short"

# The tags origin has, by name, and those on HEAD's commit.
remote=$(git ls-remote --tags origin 2>/dev/null) ||
	fail "git couldn't list origin's tags" "check your access to origin (git ls-remote --tags origin), then run it again"
remote_names=$(printf '%s\n' "$remote" | awk '{ print $2 }' | sed -e 's#^refs/tags/##' -e 's#\^{}$##' | sort -u)
remote_head=$(printf '%s\n' "$remote" | awk -v h="$head" '$1 == h { print $2 }' | sed -e 's#^refs/tags/##' -e 's#\^{}$##' | sort -u)
# A release tag on HEAD that origin has is a release. One only this clone
# has never left it, as when the push after tagging failed: it is pushed,
# not made again under another name.
for t in $remote_head; do
	if [[ $t =~ $tag_re ]]; then
		fail "$short is already released as $t" "nothing to release; merge new work to main first"
	fi
done
for t in $(git tag --points-at HEAD); do
	[[ $t =~ $tag_re ]] || continue
	if printf '%s\n' "$remote_names" | grep -qxF "$t"; then
		fail "the tag $t is on $short here but on another commit on origin" "a tag is never moved; look at both (git ls-remote --tags origin $t), then merge new work to main and release that"
	fi
	fail "$short is tagged $t here, but origin doesn't have the tag" "git push --no-follow-tags origin refs/tags/$t, then check the release as docs/releasing.md says"
done
ok "$short isn't released yet"

# The tag: the commit's day in UTC, the same day eva-md version prints for a
# source build of it, then .2, .3 and so on when the day's tag is taken here
# or on origin.
day=$(TZ=UTC git -c log.showSignature=false show -s --format=%cd --date=format-local:%Y.%m.%d HEAD)
IFS=. read -r y m d <<<"$day"
base="$y.$((10#$m)).$((10#$d))"
taken() {
	git rev-parse -q --verify "refs/tags/$1" >/dev/null || printf '%s\n' "$remote_names" | grep -qxF "$1"
}
tag=$base
n=1
while taken "$tag"; do
	n=$((n + 1))
	tag="$base.$n"
done
if ! [[ $tag =~ $tag_re ]]; then
	fail "the tag $tag isn't a release tag" "report this; the commit's date may be wrong (git show -s --format=%cd HEAD)"
fi

# The notes: the subjects of the commits since the last release, or of every
# commit for the first.
prev=""
for t in $(git tag --merged HEAD --sort=-v:refname); do
	if [[ $t =~ $tag_re ]]; then
		prev=$t
		break
	fi
done
if [ -n "$prev" ]; then
	notes=$(git -c log.showSignature=false log --format='- %s' "$prev..HEAD")
	since="since $prev"
else
	notes=$(git -c log.showSignature=false log --format='- %s' HEAD)
	since="the first release"
fi

echo
echo "Release $tag"
echo "  commit: $short $(git -c log.showSignature=false show -s --format=%s HEAD)"
echo "  notes, $since:"
printf '%s\n' "$notes" | sed 's/^/    /'
echo

if $dry_run; then
	echo "Dry run: nothing was tagged or pushed."
	exit 0
fi

if ! (: </dev/tty) 2>/dev/null; then
	fail "there's no terminal to ask on" "run it in a terminal, or pass --dry-run to only check"
fi
printf 'Release %s from %s? [y/N] ' "$tag" "$short" >/dev/tty
answer=""
read -r answer </dev/tty || true
case $answer in
y | Y | yes | Yes | YES) ;;
*)
	echo "Not released: nothing was tagged or pushed."
	exit 1
	;;
esac

tmp=$(mktemp -d "${TMPDIR:-/tmp}/eva-md-release.XXXXXX")
printf '%s\n' "$notes" >"$tmp/notes"
git tag -s --cleanup=verbatim -F "$tmp/notes" "$tag" "$head" ||
	fail "git couldn't make the signed tag $tag" "check your signing setup (user.signingkey, gpg.format), then run it again"
ok "made the signed tag $tag"
git push --quiet --no-follow-tags origin "refs/tags/$tag" ||
	fail "pushing $tag to origin failed; the tag is made here only" "fix your access to origin, then git push --no-follow-tags origin refs/tags/$tag; the release workflow starts when it lands"
ok "pushed $tag to origin"

# The workflow's run for the tag can take a moment to appear.
run_id=""
i=0
while [ $i -lt 30 ]; do
	run_id=$(gh run list --repo "$repo" --workflow release.yml --branch "$tag" --commit "$head" --event push --limit 1 \
		--json databaseId --jq '.[0].databaseId // empty' 2>/dev/null || true)
	[ -n "$run_id" ] && break
	i=$((i + 1))
	sleep 5
done
if [ -z "$run_id" ]; then
	fail "the release workflow hasn't started for $tag" "open https://github.com/$repo/actions/workflows/release.yml; when its run for $tag ends, check the release as docs/releasing.md says"
fi
echo "Watching the release workflow (run $run_id):"
gh run watch "$run_id" --repo "$repo" --exit-status ||
	fail "the release workflow failed for $tag" "read its log (gh run view $run_id --repo $repo --log-failed); the tag stays, so fix main and release again under the next tag"
ok "the release workflow passed"

dl="$tmp/assets"
mkdir "$dl"
gh release download "$tag" --repo "$repo" --dir "$dl" >/dev/null ||
	fail "gh couldn't download the release $tag" "gh release view $tag --repo $repo, and check it by hand as docs/releasing.md says"
for a in $assets SHA256SUMS; do
	[ -f "$dl/$a" ] || fail "the release $tag has no $a" "make the release a draft (gh release edit $tag --repo $repo --draft=true) and release a fix"
done
listed=$(awk '{ print $2 }' "$dl/SHA256SUMS" | sort | tr '\n' ' ')
# shellcheck disable=SC2086 # assets is a list of names, split on purpose.
want=$(printf '%s\n' $assets | sort | tr '\n' ' ')
if [ "$listed" != "$want" ] || ! (cd "$dl" && shasum -a 256 -c SHA256SUMS >/dev/null 2>&1); then
	fail "the release's SHA256SUMS doesn't match its assets" "make the release a draft (gh release edit $tag --repo $repo --draft=true) and release a fix"
fi
ok "SHA256SUMS matches the assets"

# The sums come from the same release as the assets, so only the provenance
# shows they are what the workflow built. It is checked before anything
# downloaded runs.
for a in $assets; do
	gh attestation verify "$dl/$a" --repo "$repo" --signer-workflow "$repo/.github/workflows/release.yml" >/dev/null 2>&1 ||
		fail "$a's build provenance doesn't verify" "gh attestation verify $a --repo $repo shows why; make the release a draft and release a fix"
done
ok "the build provenance verifies"

case $(uname -s)/$(uname -m) in
Darwin/arm64) bin=eva-md-darwin-arm64 ;;
Darwin/x86_64) bin=eva-md-darwin-amd64 ;;
*) fail "this isn't a Mac, so the release's binary can't run here" "check it on a Mac as docs/releasing.md says" ;;
esac
chmod +x "$dl/$bin"
got=$("$dl/$bin" version 2>/dev/null | head -n 1 || true)
if [ "$got" != "eva-md $tag" ]; then
	fail "$bin says \"$got\", not \"eva-md $tag\"" "make the release a draft (gh release edit $tag --repo $repo --draft=true) and release a fix"
fi
ok "$bin version prints $tag"

echo
echo "Released $tag: https://github.com/$repo/releases/tag/$tag"
echo "Install it with:"
echo "  curl -fsSL https://github.com/$repo/releases/latest/download/install.sh | bash"
