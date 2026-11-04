# Releasing

A release is a signed git tag on a commit of main and the GitHub release the tag's workflow publishes for it. This page says who cuts one, what the script and the workflow check, how to verify a release by hand and how to pull a bad one.

## Who releases, and how

The maintainer releases, from a Mac with a clone of `github.com/heyNag/eva-md`, push access to it, `gh` signed in to github.com and git set up to sign tags. Before each release, check both desktop apps by hand, as design.md's live resume test asks; the script doesn't check them. Then one command does it, from the checkout's root on main:

```sh
scripts/release.sh --dry-run   # check everything and show the release, then stop
scripts/release.sh             # the same, then ask, tag, push, watch and verify
```

The script shows the tag, the commit and the notes, then asks `Release <tag> from <short commit>? [y/N]` on the terminal. Only `y` or `yes` goes on, also written `Y`, `Yes` or `YES`; anything else, Enter included, stops with nothing tagged. The answer is read from the terminal itself, never from stdin, so piping `yes` into the script answers nothing. No flag skips the question, and without a terminal the script stops before asking. `--dry-run` runs every check and shows the same summary, then stops before the question.

After a yes, the script:

1. makes a signed, annotated tag on the commit (`git tag -s`), with the notes as its message;
2. pushes only that tag (`git push --no-follow-tags origin refs/tags/<tag>`), never a branch and never with force;
3. finds the release workflow's run for the tag and watches it to the end (`gh run watch --exit-status`);
4. downloads the published release into a temporary folder and checks it: every asset is there, `SHA256SUMS` lists exactly the other five and matches them, `gh attestation verify` confirms each of those five was built by this repository's release workflow, and only then the binary for this Mac's architecture runs to show it prints the tag as its version. The sums come from the same release as the assets, so only the provenance shows they are what the workflow built, and nothing downloaded runs before it verifies;
5. prints the release's URL and the install command.

It never moves, deletes or reuses a tag. When a step after the push fails, the tag stays where it is, and the script says how to look at the run or pull the release. When the push itself fails, the tag is made here only: push it with `git push --no-follow-tags origin refs/tags/<tag>`. The script then checks nothing, so when the workflow's run for the tag ends, check the release as "Verify a release by hand" shows. Running the script again stops with that same fix rather than make a second tag for the commit, and once origin has the tag it stops at "already released".

## What the script checks, and why

It stops at the first check that fails, saying what is wrong and the exact fix:

| Check | Why |
|---|---|
| It runs from the root of the eva-md checkout. | Every path it reads is relative to the root. |
| HEAD is main. | Releases come from main only; the workflow refuses a tag whose commit isn't on main. |
| The tree has no changes and no untracked files. | The tag names a commit, so what is released must be exactly what was committed. |
| HEAD has `.github/workflows/ci.yml`, `.github/workflows/release.yml`, `scripts/install.sh`, `scripts/uninstall.sh` and `scripts/LICENSES.txt`. | The workflow builds from the tagged tree and copies the install scripts and the licenses from it. |
| `user.signingkey` is set, and the program for `gpg.format` (gpg, ssh-keygen or gpgsm, or the one `gpg.<format>.program` names) is installed. | The tag is signed, so anyone can check who made it. The key itself is never printed. |
| origin is `github.com/heyNag/eva-md` (`https://github.com/heyNag/eva-md`, `git@github.com:heyNag/eva-md` or `ssh://git@github.com/heyNag/eva-md`, with or without `.git`), and so is any push URL it has. | The tag is pushed to origin and the workflow runs only on GitHub, so a tag pushed to a mirror or a fork would release nothing and stay there. The URLs are compared, never printed. |
| After `git fetch`, HEAD is origin/main. | The workflow builds what origin has; a commit only this clone has can't be released. |
| `gh` is installed and signed in to github.com (`gh auth status --hostname github.com`). | The script reads CI runs, watches the workflow and downloads the release with it. |
| The CI workflow passed for HEAD (`gh run list --workflow ci.yml --commit <HEAD>`). | Only a commit that passed every test, the race detector included, is released. |
| No release tag is on HEAD already. | One commit is released once. A release tag on HEAD that origin has means it is released. One only this clone has never left it, as after a failed push, so the script says to push it; one whose name origin has on another commit is never moved. |

## The tag

A release's tag is its version: the UTC day of the released commit's commit date, as year.month.day without zero padding, such as `2026.11.3`. That is the same day `eva-md version` prints for a source build of that commit (`2026.11.3+0915.9e4f609`). Tags have no `v` prefix.

When that day's tag already exists, here or on origin, the next release of the day is `2026.11.3.2`, then `2026.11.3.3`, and so on. The day comes from the commit, not the clock: a commit made late on 2 November in a zone behind UTC may already be 3 November in UTC, and a commit released a day after it was made keeps its own day.

As a regular expression, a release tag is:

```
^[0-9]{4}\.[1-9][0-9]?\.[1-9][0-9]?(\.[2-9]|\.[1-9][0-9]+)?$
```

The notes are the subjects of the commits since the previous release tag on main, one line each, or of every commit for the first release.

## What the workflow builds and publishes

`.github/workflows/release.yml` runs when a tag is pushed. Its jobs each have only the permissions they need, a time limit, and actions pinned to full commit SHAs.

1. **build** (Ubuntu, `contents: read`). It fails unless the tag matches the format above exactly, is an annotated tag, its commit is on main, and no release for it exists yet. It writes the notes from the tag's annotation, without its signature, and builds `eva-md-darwin-arm64` and `eva-md-darwin-amd64` with `CGO_ENABLED=0`, `-trimpath`, `-mod=readonly` and `-ldflags "-X main.version=<tag>"`. It copies `scripts/install.sh`, `scripts/uninstall.sh` and `scripts/LICENSES.txt` from the tagged tree and writes `SHA256SUMS` for those five files, one `shasum -a 256` line each.
2. **verify** (macOS, no permissions). On an Apple silicon runner, it checks `SHA256SUMS` and that `eva-md-darwin-arm64 version` prints `eva-md <tag>`.
3. **attest** (`id-token: write`, `attestations: write`). It records the build provenance of the five files with `actions/attest-build-provenance`.
4. **release** (`contents: write`). It checks again, now able to see drafts, that no release exists for the tag, then creates the release named by the tag, with the notes and six assets: the two binaries, `install.sh`, `uninstall.sh`, `LICENSES.txt` and `SHA256SUMS`. It never replaces a release.

`LICENSES.txt` holds eva-md's license, then the license of each module the binaries are built with, Go's license, and the notices in Go's own code that ask to go with a copy of the program: those of the fdlibm code in package math and, for the Intel binary, of Inferno's code in the runtime. The binaries contain all of that code, and each of those licenses and notices asks to go with every copy. `TestReleaseLicenses` fails when `scripts/LICENSES.txt` lacks one of them, as after a module is added or upgraded or a new Go brings a notice, or has a section for a module the binaries aren't built with, and its message gives the text to add. It reads Go's license and notices from the Go that runs it, so the file holds those of each Go the tests run with, the release's included.

The assets are plain files, not archives. The newest release's are at `https://github.com/heyNag/eva-md/releases/latest/download/<name>`, and a given release's at `https://github.com/heyNag/eva-md/releases/download/<tag>/<name>`.

## Verify a release by hand

In a new, empty folder:

```sh
tag=2026.11.3
gh release download "$tag" --repo heyNag/eva-md
shasum -a 256 -c SHA256SUMS
gh attestation verify eva-md-darwin-arm64 --repo heyNag/eva-md \
  --signer-workflow heyNag/eva-md/.github/workflows/release.yml
chmod +x eva-md-darwin-arm64 && ./eva-md-darwin-arm64 version
```

Use `eva-md-darwin-amd64` on an Intel Mac. Without gh, download the assets with `curl -fsSLO https://github.com/heyNag/eva-md/releases/download/$tag/<name>` and check them with `shasum`; checking the provenance needs gh. `git tag -v <tag>` checks the tag's signature when your git knows the maintainer's key.

## Pull a bad release

Hide it first, then release a fix:

```sh
gh release edit 2026.11.3 --repo heyNag/eva-md --draft=true   # hide it; it can be published again
gh release delete 2026.11.3 --repo heyNag/eva-md --yes        # or remove the release; the tag stays
```

A draft or deleted release drops out of `releases/latest`, so new installs get the previous release until the fix is out. Fix main, let CI pass, and run `scripts/release.sh` again. A tag is never reused, moved or deleted: the fix gets the next tag, such as `2026.11.3.2` on the same day.

## How versions read

- **A release build** prints its tag: `eva-md version` says `eva-md 2026.11.3`.
- **A source build** prints the UTC day of its commit, then as build metadata the commit's UTC time and short hash, such as `2026.11.3+0915.9e4f609`, with `.dirty` for a build of a changed tree, or `dev` when Go recorded no commit, as for a test binary.

A release and a source build of the same commit print the same day.
