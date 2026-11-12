# HO-13c: an upgrade that checks the signature, asks nothing and keeps the previous binary

**Plan:** section 8, Stage 1 row HO-13 (HO-25 shrunk into it); section 6.5, design 8; section 5.4, row "Upgrade"; decision 9 (section 7), on its recommendation; section 12. **Design:** `docs/v2/working-papers/design/hosts.md` sections 10.2 and 10.6. **Part of:** HO-13, after [HO-13a](HO-13a.md), beside HO-13b. **Size:** M. **Reviewers:** two: the contract, and one who tries to get a bad binary run. **Depends on:** HO-13a; HO-11, for `uninstall.go`. **Starts after the Stage 0 readout.**

## Why

`eva-md upgrade` (`cmd/eva-md/upgrade.go`) trusts `SHA256SUMS` from the same release, runs the new binary's setup on the terminal, which asks "Install or update the plugins? [y/N]", and keeps nothing to go back to; the install script checks alike. Decision 9 wants an upgrade an agent can run. Dropping setup from upgrade, and background installs, come later.

## What to build

1. **The signature.** When `release.TrustedSigners()` returns a key, `upgrade` downloads the tag's `release.json` and `release.json.sig` (64 KB at most each, by `release.Download`), checks them with `release.VerifyRelease`, and requires the manifest's tag to be the tag and this Mac's asset to match the binary's size and sha256, before the binary runs; its reported version is still checked. A missing or bad signature, or a `TrustedSigners` error, stops it, nothing changed. `--version T` is refused when the latest release's manifest yanks T; a `from_min` newer than this binary's version (a release, not a source build) is refused, naming `from_min` to install first. With no key, it checks as today. The keys file decides this, not a switch, so no config turns the check off.
2. **The switch.** `internal/release/upgrades/switch.go` (new; no network code, so health can import it): `Switch = config.NewWordSwitch("upgrades", "off", "off", "check")`. "off" is v1's upgrade, and a config that cannot be read counts as off. The top-level `updates` key is unrelated.
3. **Asks nothing**, with `upgrades = "check"`: the new binary's `setup` runs with no standard input (`exec` gives it the null device), whatever `in` is, so it asks nothing; one that needs a person stops with its own line, as with no terminal today.
4. **The previous binary is kept**, with `upgrades = "check"`: before setup runs, the stable binary is copied to `<share>/versions/previous.new/eva-md` (0700, through `paths.OpenRoot` and `paths.WriteFileAtomicAt`). When the stable binary then reports the new tag, `previous.new` replaces `<share>/versions/previous`; otherwise it is removed, so a failed upgrade leaves the one slot as it was.
5. **`eva-md upgrade --previous`**, offline, whatever the switch says: it checks the kept binary reports a version, copies the stable binary aside, runs the kept binary's `setup` with no standard input, which installs it, and keeps the replaced one in the slot. With no slot, or across a change of plugin files that install refuses, it changes nothing. The synopsis in `upgrade.go`'s `register` gains the flag; `runUpgrade`'s comment drops "the only network call".
6. **The scripts.** `scripts/install.sh` and `scripts/uninstall.sh` each hold signer lines between `# eva-md release signers: begin` and `# eva-md release signers: end`, equal to `internal/release/allowed_signers` (a test holds the three equal). When the block holds a key, `download` also fetches the manifest and signature, writes the block to a file in its temporary folder, runs `ssh-keygen -Y verify -f <file> -I eva-md-release -n eva-md-release -s release.json.sig < release.json`, finds the tag's and the asset's lines by HO-13a's layout, and refuses unless exactly one of each matches and the binary's sha256 and size agree; `check_mac` then also requires `ssh-keygen`. Nothing downloaded runs first. With an empty block they do as today. bash 3.2 only.
7. **The stand-in**, in `cmd/eva-md/release_test.go`: `publish`'s copies of the two scripts and `script` put the fake Mac's key (HO-13a) in the block, so the suite passes whatever keys are committed.
8. **Uninstall** removes `<share>/versions` with the stable binary, through the share folder's root, never following a link; its dry run names it.
9. **Docs**: README's "Install", "Upgrade", "Commands" and "Privacy"; design.md's "The install scripts and upgrade", or, once EV-10a has merged, the `## Since the split` of the spec `docs/spec/INDEX.md` names for that heading.

## Owns and may touch

`switch.go` and `release_signed_test.go` are new; in the rest, the parts named above.

```paths
cmd/eva-md/upgrade.go
cmd/eva-md/upgrade_test.go
cmd/eva-md/release_signed_test.go
cmd/eva-md/release_test.go
internal/release/upgrades/switch.go
scripts/install.sh
scripts/uninstall.sh
internal/install/uninstall.go
README.md
docs/design.md
```

## Must not touch

HO-13a's files, but to call them; `setup.go`; the workflows; `internal/install` but the one removal; `internal/hook`; the install command's line that `TestInstallScriptVersionPinThroughThePipe` holds equal in README, `install.sh` and design.md.

## Accepted when

- `scripts/linux-check.sh` passes; the existing tests of `upgrade` and the scripts, and `TestUpgradeFrom2026_11_5`, are unchanged.
- With a made-up key in `allowed_signers` and both blocks, `scripts/linux-check.sh -run 'Upgrade|Script' ./cmd/eva-md` passes; the builder reports it and removes the key.
- With the fake Mac's key trusted: a signed release installs through `upgrade` and the install script; an unsigned one, and one with a byte flipped in the manifest, the signature or the binary, is refused, and the stand-in binary never ran, by test. A yanked tag and a too-new `from_min` are refused. With no key trusted and an empty block, an unsigned release installs as today.
- A unit test of the setup command `upgrade` builds: switch on, its standard input is nil when `in` is an `*os.File`; off, it is `in`.
- Switch on, 2026.11.2 upgraded to 2026.11.3 leaves the slot reporting 2026.11.2; `--previous`, with the release server closed, puts 2026.11.2 back and keeps 2026.11.3 in the slot; a checksum failure and a setup stopped before its install leave the slot as it was; switch off, no `versions` folder.
- Uninstall's dry run names `versions`; `--apply` removes it, leaving the wiki and inbox.
- Each of these mutations fails a test: the tag check dropped, the size check dropped, `in` passed with the switch on, the slot replaced before setup succeeded.
