# HO-13b: every release carries a signed manifest

**Plan:** section 8, Stage 1 row HO-13; section 6.5, design 8; decision 9 of section 7, built on its recommendation; section 1 item 4 (the owner makes the key). **Design:** `docs/v2/working-papers/design/hosts.md` sections 10.2 and 10.6, decision HO-D3 and risk 14. **Part of:** HO-13, after [HO-13a](HO-13a.md), beside HO-13c. **Size:** M. **Reviewers:** two: the workflows and secret; the script and docs. **Depends on:** HO-13a. **Starts after the Stage 0 readout.** Nothing in it cuts, tags or publishes a release or starts a workflow.

## Why

HO-13a defined `release.json`, its signature and the trusted keys. The workflow must now write and sign it with a key only an approved release reaches, so a binary and the install script can check it offline (HO-13c).

## What to build

1. **The manifest in every release.** `.github/workflows/release.yml`'s build job runs `scripts/release-manifest.sh "$TAG" <the tag's commit> "$out"` after it writes `SHA256SUMS`, and gives as an output whether the tagged commit's `internal/release/allowed_signers` holds a key. `release.json` is neither in `SHA256SUMS` nor attested: its signature is its proof, so the five-asset lists stay as they are. The release job uploads `release.json`, and `release.json.sig` when there is one.
2. **The signing job.** A new job, `sign`, after `attest` and before `release`, runs only when Cut a release called the workflow (`inputs.tag` set) and the build job says the keys file holds a key; until the owner commits a key, a release, its approvals included, goes as today. It has `environment: release`, `permissions: {}` and a time limit. It writes the environment secret `RELEASE_MANIFEST_KEY` (an OpenSSH ed25519 private key, no passphrase) to a file only its user reads under `$RUNNER_TEMP`, runs `ssh-keygen -Y sign -f <file> -n eva-md-release release.json`, removes the file in the same step, and never prints the key. It verifies the signature with `ssh-keygen -Y verify` against the tagged commit's `allowed_signers`; a missing secret or a bad signature fails it. `release` runs when `sign` passed or was skipped, never when it failed.
3. **Cut a release's check job**, when the planned commit's `allowed_signers` holds a key (read with its `contents: read`), checks the signature and every asset's hash and size against the manifest before anything downloaded runs.
4. **A release cut from a Mac** is signed on the Mac with the offline key (decision 9). `scripts/sign-release.sh <tag> <key file>` (new): it downloads the release and checks origin, `gh`, `SHA256SUMS` and each asset's provenance as `scripts/release.sh` does, with `--source-ref refs/tags/<tag>`; checks that the manifest's tag, commit and every asset match; signs; verifies against the tagged commit's `allowed_signers`; and uploads `release.json.sig` without `--clobber`. It refuses a release already signed. `--dry-run` stops before signing. Until it runs, whatever trusts a key refuses the release, which is safe. `scripts/release.sh` does not change.
5. **docs/releasing.md**: the manifest and the sign job; that once a key is committed a release cut from GitHub asks for two approvals of `release`, one to tag and one to sign; the Mac's step after `release.sh`; checking a release by hand with `ssh-keygen -Y verify`; yanking a tag or raising `from_min` through `scripts/release-policy` and a new release, under "Pull a bad release"; rotation (a release signed with one key ships the new `allowed_signers`); and the owner's steps, done once, with no secret in the repository:
   - make the environment key with `ssh-keygen -t ed25519 -N ''` in a temporary folder, store its private half as the `release` environment's secret `RELEASE_MANIFEST_KEY` (Settings > Environments > release), and delete the file;
   - make the offline key on the Mac, with a passphrase, off GitHub;
   - commit both public halves as lines of `internal/release/allowed_signers` and of HO-13c's blocks in `scripts/install.sh` and `scripts/uninstall.sh`. From that commit on every release must be signed, and binaries built from it refuse one that is not.
6. **docs/known-limitations.md**, "Releases": the install script trusts the keys it carries and comes from the release it checks, so a first install gains little against whoever can change a release's files; an installed binary trusts the keys it was built with.

## Owns and may touch

```paths
.github/workflows/release.yml
.github/workflows/cut-release.yml
scripts/sign-release.sh
internal/release/release_workflow_test.go
internal/release/sign_release_test.go
docs/releasing.md
docs/known-limitations.md
```

`sign-release.sh` and `sign_release_test.go` are new; in `docs/known-limitations.md`, "Releases" only.

## Must not touch

`scripts/release.sh`, `release_darwin_test.go` (macOS only: the new test brings its own fake `gh`), `scripts/install.sh`, `scripts/uninstall.sh`, anything in `cmd/eva-md`, every Go file that is not a test, and HO-13a's files but to read them. The repository's settings and secrets, which the owner makes. `AGENTS.md`: give the lead words for it.

## Accepted when

- `scripts/linux-check.sh` passes, as do `go test ./cmd/live-accuracy ./cmd/eva-eval ./internal/release` and `go test ./cmd/eva-md -run 'Design|Agents|Usage|Verification|Wording|Document'`.
- Workflow tests hold, each failing with its line removed: `sign` has `environment: release`, `permissions: {}`, a time limit, pinned actions and its two conditions; `RELEASE_MANIFEST_KEY` is named in no other job and in no echo, summary or output; the key file is removed in the step that writes it; `release` needs `sign` and runs on success or skip only; the release has `release.json` and, when made, `release.json.sig`; the check job verifies the signature when the planned commit's keys file holds a key.
- `sign-release.sh`, run in a test on Linux against a fake `gh` and a key made in `t.TempDir()`: signs a good release, and the signature passes `ssh-keygen -Y verify` and HO-13a's `VerifyRelease`; refuses, uploading nothing, a release whose manifest names another commit, whose asset hash differs, whose provenance fails, or already signed; `--dry-run` uploads nothing.
- `TestReleaseWorkflowPublishesEveryAsset` and `TestReleaseTagPatternIsTheSameEverywhere` pass with the five assets unchanged; the first may learn the manifest's two files. `TestReleaseChecksProvenanceAgainstOneSigner` also reads `sign-release.sh`, with source ref `refs/tags/$tag`.
