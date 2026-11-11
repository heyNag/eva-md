# HO-13a: the signed release manifest, and its check in Go

**Plan:** section 8, Stage 1 row HO-13 (HO-25 shrunk into it); section 6.5, design 8; decision 9 of section 7, built on its recommendation; section 12 for what of HO-25 is put off. **Design:** `docs/v2/working-papers/design/hosts.md` section 10.2 and row HO-13 (section 21); `docs/v2/working-papers/audit/ops.md`, section 2 items 15 and 16, and ops-10. **Part of:** HO-13: a (this), then b and c side by side, then d. **Size:** M. **Reviewers:** two: the contract, and one who tries to make a manifest or signature pass that should not. **Starts after the Stage 0 readout**, where the owner answers decision 9; nothing here starts before it.

## Why

`eva-md upgrade` and the install script check a download only against `SHA256SUMS` from the same release (`release.Verify` in `internal/release/fetch.go`), so whoever can change a release's files can change both. Decision 9 signs a manifest of each release with a key GitHub's files do not hold. This part is the format, its writer, its reader and the check, used by no command yet. It changes what `./cmd/eva-md` builds (an embedded file), and no hook runs any of it.

## What to build

1. **The format**, in `internal/release/SPEC.md` (new). `release.json`: `format` (1), `tag`, `commit` (40 lower-case hex), `from_min` (a tag, or empty), `yanked` (tags), and `assets`, by file name, each `{"sha256": <64 lower-case hex>, "size": <bytes>}`. One layout only: two-space indent, keys in that order, `yanked` on one line, one asset a line, sorted by name. A reader with no JSON parser finds the tag in the line `  "tag": "<tag>",` and an asset in the line `    "<name>": {"sha256": "<hex>", "size": <n>},` (no comma on the last). A field added later makes a new format number, which a format 1 reader refuses.
2. **The signature**, `release.json.sig`: an SSH signature as `ssh-keygen -Y sign -n eva-md-release` writes it, ed25519 only, over the manifest's bytes. The trusted keys are `internal/release/allowed_signers` (new), embedded with `go:embed`, in the format `ssh-keygen -Y verify -f` reads, principal `eva-md-release` and the option `namespaces="eva-md-release"` on every line. It is committed with comment lines only, so it trusts none until the owner adds the keys (HO-13b says how). No private key is ever in the repository.
3. **The reader**, `internal/release/manifest.go` and `sshsig.go` (new), standard library only: `(Manifest).Encode() []byte`, which writes the one layout; `ParseManifest(b []byte) (Manifest, error)`, which refuses any format but 1 and any bytes that differ from `Encode` of what it read; `VerifySSHSig(msg, sig []byte, namespace string, keys []ed25519.PublicKey) error`; `var TrustedSigners func() ([]ed25519.PublicKey, error)`, which by default reads the embedded file on its first call, not at start, and returns a reading error, never panics; tests replace it as they replace `Base`; and `VerifyRelease(manifest, sig []byte) (Manifest, error)`, which checks the signature against `TrustedSigners()` and then parses. Refused: a key of another type, another namespace, a hash other than sha512 or sha256, a signing key that is not trusted, anything left after the armour.
4. **The writer**, `scripts/release-manifest.sh <tag> <commit> <folder>` (new), POSIX shell with `shasum -a 256` and `wc -c`: it writes `<folder>/release.json` over the five files the release workflow builds (`eva-md-darwin-arm64`, `eva-md-darwin-amd64`, `install.sh`, `uninstall.sh`, `LICENSES.txt`), ignores any other file there, and stops when one is missing. `from_min` and `yanked` come from `scripts/release-policy` (new): `#` comments, `from_min <tag>` at most once, `yank <tag>` lines; committed with comments only. A line it cannot read, a tag not in the release format, a `from_min` newer than the tag, and the tag yanking itself stop it.
5. **A signer for tests only**, `internal/release/releasetest` (new): makes an ed25519 key, gives its `allowed_signers` line, and signs bytes in the SSH signature format.
6. **The stand-in**, in `cmd/eva-md/release_test.go`: `newFakeMac` makes a key with `releasetest` and sets `release.TrustedSigners` to it for the test; `publish` runs the writer over the release it puts out and signs `release.json` with that key, so the suite keeps passing once the owner commits real keys. A test wanting it unsigned or tampered with changes the files `publish` returns. Every existing test passes unchanged.

## Owns and may touch

The new files of `internal/release` named above and their tests; `internal/release/releasetest/**` (new); the two new scripts; in `cmd/eva-md/release_test.go`, `newFakeMac` and `publish` only.

```paths
internal/release/SPEC.md
internal/release/allowed_signers
internal/release/manifest.go
internal/release/manifest_test.go
internal/release/sshsig.go
internal/release/sshsig_test.go
internal/release/releasetest/**
scripts/release-manifest.sh
scripts/release-policy
cmd/eva-md/release_test.go
```

## Must not touch

`fetch.go`, `version.go` and the tests there now; `cmd/eva-md/upgrade.go`; the workflows and the other scripts; `TestInstallScriptVersionPinThroughThePipe`, whose one check EV-10a owns. No new module.

## Accepted when

- `scripts/linux-check.sh` passes, and so does `go test ./cmd/live-accuracy ./cmd/eva-eval ./internal/release`.
- Signatures `ssh-keygen -Y sign` makes in the test, with a key `ssh-keygen` made in `t.TempDir()`, over made-up manifests, pass `VerifySSHSig`; signatures `releasetest` makes pass `ssh-keygen -Y verify` with its `allowed_signers` line. Both ways for an empty message and one of 1 MB.
- Each is refused by a test: one byte flipped in the manifest, in the armour, in the signature bytes and in the key; namespace `git`; an RSA key; trusted keys that lack the signer; two signatures joined; a keys file with a line that breaks the format.
- The writer's output on made-up files parses, its hashes and sizes are the files', and writing it again gives the same bytes; a manifest with a space added, keys reordered, an asset out of order or a field added is refused.
- A test reads the committed `allowed_signers` and `release-policy` and fails on a line it cannot read; a copy with comments only reads as no keys and no policy.
- A test fails when a Go file that is not a test imports `releasetest`.
- A reviewer's own mutations (the namespace check dropped, the key match dropped, the layout check dropped, a panic on a bad keys file) are each noticed by a test.
