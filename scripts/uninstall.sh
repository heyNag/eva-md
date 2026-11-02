#!/bin/bash
# Preview the removal of eva-md from a Mac, or remove it:
#
#   curl -fsSL https://github.com/heyNag/eva-md/releases/latest/download/uninstall.sh | bash
#   curl -fsSL https://github.com/heyNag/eva-md/releases/latest/download/uninstall.sh | bash -s -- --apply
#   curl -fsSL https://github.com/heyNag/eva-md/releases/latest/download/uninstall.sh | bash -s -- --apply --all
#
# It runs the installed binary's eva-md uninstall with these arguments. When
# that binary is missing or doesn't run, or is too old for --all and --yes,
# it downloads the latest release's, checks it as install.sh does, and runs
# that one's. The wiki is never removed. Everything is in functions run from
# the last line, so a download cut short runs nothing.
set -euo pipefail

EVA_MD_REPO=https://github.com/heyNag/eva-md
EVA_MD_TAG_RE='^[0-9]{4}\.[1-9][0-9]?\.[1-9][0-9]?(\.[2-9]|\.[1-9][0-9]+)?$'
EVA_MD_TMP=

say() { printf '%s\n' "$*"; }

# stop says what is wrong and what to do, and ends the script.
stop() {
	printf 'eva-md uninstall: %s\n' "$1" >&2
	printf 'Fix: %s, then run the uninstall command again.\n' "$2" >&2
	exit 1
}

check_mac() {
	if [ "$(id -u)" = 0 ]; then
		stop "this runs as root" "run it as yourself, without sudo"
	fi
	if [ "$(uname -s)" != Darwin ]; then
		stop "eva-md runs on macOS only" "run it on a Mac"
	fi
	local tool
	for tool in curl shasum; do
		if ! command -v "$tool" >/dev/null 2>&1; then
			stop "$tool isn't found" "make sure /usr/bin is on PATH, where macOS keeps $tool"
		fi
	done
}

# mac_arch prints the architecture the release assets name: arm64, also in
# a shell Rosetta runs on an Apple silicon Mac, or amd64.
mac_arch() {
	case "$(uname -m)" in
	arm64) say arm64 ;;
	x86_64)
		if [ "$(sysctl -n sysctl.proc_translated 2>/dev/null || true)" = 1 ]; then
			say arm64
		else
			say amd64
		fi
		;;
	*) stop "this Mac's processor, $(uname -m), isn't one eva-md is built for" "run it on an Apple silicon or Intel Mac" ;;
	esac
}

# latest_tag prints the latest release's tag, read from where
# releases/latest redirects.
latest_tag() {
	local to tag
	to=$(curl -fsS --proto '=https' --connect-timeout 20 --max-time 60 -o /dev/null -w '%{redirect_url}' "$EVA_MD_REPO/releases/latest") ||
		stop "couldn't reach GitHub to find the latest release" "check the connection"
	tag=${to##*/releases/tag/}
	if [ "$tag" = "$to" ] || ! [[ $tag =~ $EVA_MD_TAG_RE ]]; then
		stop "GitHub's answer for the latest release names no release" "try again in a few minutes"
	fi
	say "$tag"
}

# fetch saves the release asset $2 of tag $1 at $3.
fetch() {
	curl -fsSL --proto '=https' --proto-redir '=https' --connect-timeout 20 --max-time 600 -o "$3" "$EVA_MD_REPO/releases/download/$1/$2" ||
		stop "couldn't download $2 of eva-md $1" "check the connection"
}

# download puts the checked binary of tag $1 for arch $2 at $EVA_MD_TMP/eva-md.
download() {
	local tag=$1 asset=eva-md-darwin-$2
	EVA_MD_TMP=$(mktemp -d "${TMPDIR:-/tmp}/eva-md-uninstall.XXXXXX")
	trap 'rm -rf "$EVA_MD_TMP"' EXIT
	fetch "$tag" "$asset" "$EVA_MD_TMP/eva-md"
	fetch "$tag" SHA256SUMS "$EVA_MD_TMP/SHA256SUMS"
	local want got
	want=$(awk -v name="$asset" '$2 == name || $2 == "*" name { print $1; exit }' "$EVA_MD_TMP/SHA256SUMS")
	if ! [[ $want =~ ^[0-9a-fA-F]{64}$ ]]; then
		stop "SHA256SUMS of eva-md $tag has no line for $asset; nothing was run" "try again in a few minutes"
	fi
	got=$(shasum -a 256 "$EVA_MD_TMP/eva-md" | awk '{ print $1 }')
	if [ "$(printf '%s' "$want" | tr 'A-F' 'a-f')" != "$got" ]; then
		stop "the checksum of $asset doesn't match SHA256SUMS; nothing was run" "run the uninstall command again; if it fails the same way, report it"
	fi
	say "Checksum verified"
	chmod 700 "$EVA_MD_TMP/eva-md"
	local reported
	reported=$("$EVA_MD_TMP/eva-md" version 2>/dev/null | sed -n 1p) ||
		stop "the downloaded eva-md doesn't run on this Mac" "check that macOS is up to date"
	if [ "$reported" != "eva-md $tag" ]; then
		stop "the downloaded binary says it is ${reported:-nothing}, not eva-md $tag" "run the uninstall command again"
	fi
}

# knows_all says whether binary $1's uninstall takes --all: an eva-md from
# before --all calls it an invalid argument, with status 2, where a newer
# one previews. Only a preview runs, so nothing is removed.
knows_all() {
	local status=0
	"$1" uninstall --all </dev/null >/dev/null 2>&1 || status=$?
	[ "$status" -ne 2 ]
}

main() {
	check_mac
	local binary="$HOME/.local/share/eva-md/bin/eva-md" why=
	if ! { [ -x "$binary" ] && "$binary" version >/dev/null 2>&1; }; then
		why="is missing or doesn't run"
	else
		local arg
		for arg in "$@"; do
			case "$arg" in
			--all | --yes)
				if ! knows_all "$binary"; then
					why="is too old for --all and --yes"
				fi
				break
				;;
			esac
		done
	fi
	if [ -n "$why" ]; then
		local arch tag
		arch=$(mac_arch)
		tag=$(latest_tag)
		say "The installed eva-md $why; using eva-md $tag for $arch to uninstall"
		download "$tag" "$arch"
		binary=$EVA_MD_TMP/eva-md
		say
	fi
	# The terminal, when there is one, answers uninstall --all's question,
	# since the script itself is on stdin.
	local status=0
	if (exec </dev/tty) 2>/dev/null; then
		"$binary" uninstall "$@" </dev/tty || status=$?
	else
		"$binary" uninstall "$@" </dev/null || status=$?
	fi
	exit "$status"
}

main "$@"
