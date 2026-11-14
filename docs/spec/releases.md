# Releases, the install scripts and upgrade

- **Component:** Releases and upgrade
- **Paths:** `internal/release`, `cmd/eva-md/upgrade.go`, `scripts/install.sh`, `scripts/uninstall.sh`
- **Status:** v1 as built, frozen on 2026-11-14

The blocks below are v1's design and verification text, moved here word for word when the two documents were split; [INDEX.md](INDEX.md) lists every spec.

<!-- v1 docs/design.md 016 -->
## The install scripts and upgrade

**Releases.** The tag format, the assets and their addresses are in "Making a release". A release's binary prints its tag as its version, such as `eva-md 2026.11.3`.

**`install.sh`** is the one command for a first install and for an upgrade: `curl -fsSL https://github.com/heyNag/eva-md/releases/latest/download/install.sh | bash`.
It runs on macOS's `/bin/bash` 3.2 with `set -euo pipefail`, and its whole body is in functions run from its last line, so a download cut short runs nothing.
Each check stops it with what is wrong and the fix, "then run the install command again": not root (never sudo); macOS; an arm64 or x86_64 Mac, where a shell Rosetta runs on Apple silicon (`sysctl.proc_translated` is 1) gets the arm64 binary; curl and shasum present.
The version is `EVA_MD_VERSION` when it is set, which must match the tag format, or else the latest release's tag.
The variable reaches the script only when it is given to bash, after the pipe, as in `curl -fsSL https://github.com/heyNag/eva-md/releases/latest/download/install.sh | EVA_MD_VERSION=2026.11.3 bash`; put before curl, it reaches only curl, and the latest release is installed.
The latest tag is read from where `releases/latest` redirects without following it.
It downloads the binary for this Mac and `SHA256SUMS` from that release, over HTTPS only, into a new temporary folder that an exit trap removes, checks the binary's sum against its line (a sum that differs, or no line, stops it and nothing runs), checks that the binary runs and prints
`eva-md <tag>`, then runs its `setup` with the script's arguments (`bash -s -- --dir ~/eva-wiki`), reading `/dev/tty` when there is one, since stdin is the script, and `/dev/null` otherwise.
It prints "Installing eva-md <tag> for <arch>" and "Checksum verified", then setup's own output, and exits with setup's status; when setup stopped, one closing line says to fix the problem when it named one, and run the install command again; a no to setup's question stops it with nothing changed, and the same line follows.
On a Mac that is set up the same command is the upgrade.

**`uninstall.sh`** has the same shell rules and checks.
It runs `~/.local/share/eva-md/bin/eva-md uninstall` with its arguments, reading the terminal; when that binary is missing or doesn't run, or, with `--all` or `--yes` among the arguments, its `uninstall --all` preview fails with a usage error (status 2), as an eva-md from before those flags does, it
downloads and checks the latest release's binary as `install.sh` does and runs that one's uninstall.
`| bash` previews; `| bash -s -- --apply` (or `--apply --all`) removes.

**`eva-md upgrade [--check] [--force] [--version TAG]`** finds the latest release (the redirect) or takes `--version`, and compares it with this binary's version: release versions by year, month, day and the number that day; a source build (`<day>+<HHMM>.<hash>`, with `.dirty`) counts as its day
with number 0, and only a release of a later day replaces it without `--force`; a build with no version (`dev`) is replaced only with `--force`.
When the release isn't newer, it says `eva-md <version> is up to date` (or what `--force` would do) and downloads nothing; `--check` only says whether a newer release exists.
Otherwise it downloads the binary for this Mac (arm64 also for an amd64 binary under Rosetta) and `SHA256SUMS` into a new temporary folder inside the data folder, removed at the end, checks the sum and the version the new binary reports, and runs the new binary's `setup` attached to this terminal,
which on a set-up Mac keeps the wiki and updates the stable binary and the plugins, and exits with setup's status.
When setup stopped, a closing line says to fix the problem when it named one, then run `eva-md upgrade` again, or, when the stable binary now reports the new tag because setup stopped after its install, to run `eva-md setup` (the stable binary's full path when the link on PATH isn't eva-md's), since `eva-md upgrade` would find it up to date.
A failure before setup leaves the installed binary and plugins as they were and says what failed; setup's install rolls back its own failure.
This is the only network call eva-md makes itself, and only in this command (never in hooks, the update or the daily job): through Go's `net/http`, to github.com over HTTPS, following a download's redirect only to the same host or, over HTTPS, a `*.githubusercontent.com` host, where GitHub serves release files.
The lookup has 30 seconds and each download 5 minutes; both limits, and the 30-second version check, are package variables that tests raise, and tests point the release address at a local test server through a package variable, never an environment variable.

<!-- v1 docs/verification.md 011 -->
- **Releases, install, upgrade and uninstall** (`internal/release`, `internal/install`, `cmd/eva-md`, `scripts`).
  Release tags are checked against the release contract's format, and versions order by day, then the number that day, with a source build as its day with number 0, which only a later day's release replaces unforced, and `dev` before all (`TestVersionsOrder`).
  `SHA256SUMS` is read as shasum writes it, and a missing line or another sum fails (`TestVerify`).
  The latest tag comes from the redirect without following it, a download follows a redirect on the same host but refuses one to another host, and an error status or an asset over its limit fails (`TestLatestAndDownload`).
  Install records what it made, 0600, adding to the record without repeating an item, with config.env once it exists and the wiki and its remote marked kept, and a dry run writes none (`TestInstallRecordsWhatItMade`); setup's command adds a `config.env` written after the install to an existing record
  once, and writes no record without an install (`TestRecordConfigFiles`); install links `~/.local/bin/eva-md` only when that folder exists and is on PATH with no eva-md or eva-md's own link in it, never over a file or another link and never making the folder (`TestInstallLinksOnPath`); `--all`
  removes the config, state and data folders but refuses outright, removing nothing, when the wiki is inside one of them (`TestUninstallAll`); and after `--apply` the install folder keeps only the record (`TestUninstallRemovesEmptyFolders`).
  End to end, offline, with a temporary home, stateful fake `claude` and `codex` scripts, and a release stand-in built in the test with `go build -tags evamd_testlimits -ldflags "-X main.version=<tag>"`, its `SHA256SUMS`, the two scripts and the licenses, served by a fake `curl` (with fake `id`,
  `uname` and `sysctl`) for the scripts and by a local test server for `eva-md upgrade`: `install.sh` on a new Mac stops at the answers file with the template written and a closing line worded to fit a stop that named no problem, then, with it filled in and a local bare repo as the remote, finishes
  setup with the release's stable binary, the plugins registered with both fake hosts, the record and the link, leaves no temporary folder, and keeps the wiki and its remote unchanged when run again (`TestInstallScriptOnANewMac`); on a Mac set up before there was a `config.env`, the one setup writes
  after its install ends up in the record (`TestInstallScriptRecordsALateConfigEnv`); a checksum that doesn't match, or a missing line, runs nothing (`TestInstallScriptChecksumMismatch`); root and another OS are refused by both scripts before any download
  (`TestInstallScriptRefusesRootAndOtherSystems`); an Intel Mac gets the amd64 binary and Rosetta on Apple silicon the arm64 one (`TestInstallScriptPicksTheMacsBinary`); `EVA_MD_VERSION` installs that release without asking for the latest, a malformed one is refused before any download, and a binary
  that reports another version than its tag never reaches setup (`TestInstallScriptVersionPin`); the pinning command README shows, with the tag given to bash after the pipe, run whole as a person types it, installs that release though a newer one is the latest, and `install.sh`'s header and design.md
  show the same command (`TestInstallScriptVersionPinThroughThePipe`); `uninstall.sh` runs the installed binary with no download, and with it gone downloads and checks the latest release's, removing everything but the wiki with `--apply --all --yes` (`TestUninstallScript`); an installed eva-md that
  answers `--all` with a usage error is set aside for the latest release's binary, after a preview only, while a current one previews `--all` itself with no download (`TestUninstallScriptWithAnOlderBinary`).
  The stand-in is built with the `evamd_testlimits` tag, so the setup, install and uninstall it runs in child processes start with the limits `TestMain` raises, and in a home of its own with `GOENV=off`, so no Go settings of the real home apply, and with Go's telemetry off there, so no go command
  leaves a process writing into it; it shares Go's build and module caches with `go test` itself (`TestReleaseStandInBuild`).
  `scripts/LICENSES.txt` starts with eva-md's license and has a section for each module the release binaries are built with, as `go list -deps` gives them for darwin with cgo off and no build tags on arm64 and amd64, the architectures the release workflow's build loop names, and no other, holding the
  license file in the module's folder under its path and version, then the license of the Go that runs the test, then each notice in Go's files of those builds that starts at a copyright line of someone other than the Go Authors, but for those of the Cephes library and fiat-crypto, which ask nothing
  of a copy of the program (`TestReleaseLicenses`); a notice runs to the end of its comment or to a line of = signs, and a copyright line outside a `//` comment fails the test (`TestFileNotices`); a module replaced by a folder goes under a heading naming the folder, and its license is read from there (`TestListReleaseReadsAReplacedModulesFolder`).
  `eva-md upgrade` says when it is up to date, with `--check`, `--version` and an older tag, `--check` only says that a newer release is available, also with `--force`, and a same-day source build or `dev` needs `--force`, all with nothing downloaded (`TestUpgradeSaysWhenUpToDate`); a newer release is
  downloaded, checked and its setup run, which replaces the stable binary and keeps the wiki, and `--force` replaces a same-day source build (`TestUpgradeInstallsTheNewRelease`); when setup stops after its install put the release in place, the closing line names `eva-md setup`, which finishes once the
  problem is fixed, since `eva-md upgrade` now says it is up to date (`TestUpgradeSetupStoppedAfterItsInstall`), and when it stops before, `eva-md upgrade` again (`TestUpgradeSetupStoppedBeforeItsInstall`); a checksum that differs, a failed download of the binary or `SHA256SUMS`, a failed lookup and a binary reporting another version each change nothing (`TestUpgradeFailuresChangeNothing`).
  Uninstall's preview removes nothing and lists what it would remove and keep, with sizes, the wiki last (`TestUninstallPreviewRemovesNothing`); `--apply` keeps the config, the data, the record of what is left and the wiki (`TestUninstallApplyKeepsConfigDataAndWiki`); `--all` removes the config and
  data only after `delete` is typed, on a terminal with `--yes` too, removes nothing at all after another answer, and without a terminal needs `--yes` (`TestUninstallAllAsksForDelete`); without the record it uses the known layout (`TestUninstallWithoutTheRecord`); a link to another program is kept by
  install and uninstall (`TestUninstallKeepsALinkToSomethingElse`); and while the update or the daily job holds its lock it removes nothing (`TestUninstallWaitsForTheUpdateAndDailyJob`).
  The wiki folder and its bare remote are compared before and after in every case that touches them.

<!-- v1 docs/verification.md 045 -->
- **Releases** (`internal/release`).
  The tests of `scripts/release.sh` run on macOS only, against a temporary repo of the release files whose origin has GitHub's URL, which git config rewrites to a bare origin, with a fake gh on PATH and git signing tags through a fake ssh signing program, so no key, remote or GitHub is used, and none of the git environment go test runs in.
  `scripts/release.sh` stops at each failed check with its exact fix and tags nothing: run from a subfolder or another module, another branch or a detached HEAD, a changed or untracked file, a missing install script, no signing key, a missing signing program or an unknown `gpg.format`, no origin,
  origin or its push URL somewhere other than `github.com/heyNag/eva-md`, main ahead of, behind or diverged from origin, no gh or gh signed out, CI not run, still running (its run named) or failed, HEAD already released on origin, HEAD tagged here only (as after a failed push, with the command that
  pushes the tag) or tagged here under a name origin has on another commit, and no terminal to ask on (`TestReleaseStopsAtTheFirstFailedCheck`).
  The tag is the commit's UTC day without zero padding, with `TZ` a zone where it is still the day before, `.2` when the day's tag exists here and `.3` when `.2` exists only on origin, with the notes since the last release or every commit for the first, and the same tag and notes for a signed commit
  with `log.showSignature` on; `--dry-run` shows it all, asks nothing and leaves both repos' tags and the signer untouched, and `--plan` shows the same, ending with its own line, writes the tag, the full commit, `since` and the notes to `$GITHUB_OUTPUT` as GitHub reads them, the notes between lines of
  a random `EVA_MD_NOTES_<32 hex>` delimiter, and leaves the tags untouched too (`TestReleaseNamesTheTag`).
  A plan runs with no signing key and an unknown `gpg.format`, and no terminal, signs nothing, calls gh only to read the repository and list CI's runs, keeps what earlier steps wrote to the output file, and without that file only shows the release (`TestReleasePlanNeedsNoSigningOrTerminal`); it stops
  on another branch, main behind origin, gh that can't read the repository (with the fix to set `GH_TOKEN`), failed CI and HEAD already released, each with the release's own problem and fix, and writes no output or annotation; with `GITHUB_ACTIONS=true` a failed plan also ends stdout with
  `::error::<problem>. Fix: <fix>`, a `%` in the branch's name encoded as `%25`, and its stderr is unchanged; and `--plan` with `--dry-run`, in either order, or twice is a usage error (`TestReleasePlanStopsAtTheFirstFailedCheck`).
  On a new pseudo-terminal, with stdin saying yes, no, Enter, other words and Control-D at the question make nothing (`TestReleaseDeclinedMakesNothing`); yes makes the signed, annotated tag with the notes as its message, pushes only it with `push.followTags` on, in one push without force that names
  only the tag, so origin receives the one ref update creating the tag and nothing else, even an update leaving main where it is, finds the workflow's run once origin has the tag, watches it, downloads the release, checks the sums, verifies each asset's provenance against the release workflow with the
  tag as its source ref, runs the binary to check its version, prints the URL and the install command and leaves no temporary files (`TestReleasePublishesTheTag`).
  A failed workflow, a missing or changed asset, `SHA256SUMS` missing a line or listing one too many, a binary of another version and provenance that doesn't verify each stop with their fix, and the pushed tag stays; with provenance that doesn't verify, no downloaded binary has run (`TestReleaseStopsWhenThePublishedReleaseIsWrong`).
  `GIT_INDEX_FILE` and config given through `GIT_CONFIG_COUNT` or `GIT_CONFIG_PARAMETERS` in the environment reach neither the test repos nor the script (`TestReleaseIgnoresTheOuterGitEnvironment`).
  The workflows are checked by reading them, on any system.
  Every action in every workflow is pinned to a 40-character commit with its version, or is one of this repository's workflows, called by its path (`TestReleaseWorkflowPinsEveryAction`).
  The release workflow sums, attests and publishes the same files, each list read from the one line or block that gives it: the binaries its build loop makes and the files it copies from `scripts/`, which `scripts/release.sh` checks main has and names as the assets it checks once they are published,
  as Cut a release's check job names them, with the line count the workflow checks `SHA256SUMS` for (`TestReleaseWorkflowPublishesEveryAsset`).
  Cut a release is run by hand with a boolean `dry_run` that defaults to false, may do nothing by default, runs one at a time, refuses off main, reads the `release` environment's required reviewers and runs `scripts/release.sh --plan`; its jobs are plan, approve, tag, publish and check, in a line;
  only approve has a condition, that the run isn't a dry run, and an environment, `release`, and no later job reads `dry_run`; each job's permissions are exactly plan's `actions: read` and `contents: read`, none for approve, tag's `contents: write`, check's `contents: read` and `attestations: read`,
  and publish's `contents: write`, `id-token: write` and `attestations: write`, which are exactly what the release workflow's jobs ask for together; every job but publish, which calls `release.yml`, has a time limit; the tag job checks out the planned commit, makes an unsigned annotated tag with who
  started the run as its tagger, and is the only job that pushes, once, without force; when origin has the tag already, it fetches it and skips making and pushing a tag, through its step's output and the push step's condition, only when the tag is annotated, peels to the planned commit and has the
  planned notes, and otherwise fails saying a tag is never moved, and either way it then checks origin's tag is the local one; and the release workflow runs on a pushed tag and when called with a required `tag`, reading the tag from its input or the pushed ref for its checkout and its concurrency
  group, never from `github.ref`, and only when called adds the line linking the run to the notes file it uploads and creates the release from (`TestReleaseCutWorkflowNeedsAnApproval`).
  Cut a release's `run-name` is "Cut a release (dry run)" for a dry run and "Cut a release" otherwise, right after its `name:`; its approve and tag jobs are named "Approve the release" and "Tag the release"; and no job of either workflow has `${{` in its name, since GitHub shows a skipped job's name unevaluated (`TestReleaseWorkflowJobNamesAreFixed`).
  `scripts/release.sh` and Cut a release each name the provenance's signer once, and both name `heyNag/eva-md/.github/workflows/release.yml`, which attests exactly once; each names one source ref, `refs/tags/$tag` in the script and `refs/heads/main` in Cut a release (`TestReleaseChecksProvenanceAgainstOneSigner`).
  The release tag's pattern is the same in `scripts/release.sh`, the release workflow and Cut a release's tag job, and it and `ValidTag` agree on valid tags and on tags with a `.1` or `.0` suffix, zero padding, a `v` prefix or no day (`TestReleaseTagPatternIsTheSameEverywhere`).

<!-- /v1 -->

## Must keep

The must-keep items of [findings.md](../v2/findings.md) this component touches, numbered from K01 in each list, and the tests that hold them.
An item that tests hold only in part ends with what of it no test holds, as `(untested: ...)`.

| Id | Item | Tests |
| --- | --- | --- |
| SET-K18 | The install script runs from its last line, checks the checksum and version first, refuses root, and uses HTTPS only | cmd/eva-md:TestInstallScriptChecksumMismatch, cmd/eva-md:TestInstallScriptRefusesRootAndOtherSystems, cmd/eva-md:TestInstallScriptVersionPinThroughThePipe |
| OPS-K14 | Nothing downloaded runs before its checksum and version match; HTTPS only, bounded, and a cut download runs nothing | internal/release:TestVerify, internal/release:TestLatestAndDownload, cmd/eva-md:TestUpgradeFailuresChangeNothing, cmd/eva-md:TestInstallScriptChecksumMismatch |
| OPS-K15 | Releases need main, green checks and an approval or signed tag; provenance is checked against one signer; actions are pinned | internal/release:TestReleaseChecksProvenanceAgainstOneSigner, internal/release:TestReleaseWorkflowPinsEveryAction, internal/release:TestReleaseCutWorkflowNeedsAnApproval, internal/release:TestReleaseStopsAtTheFirstFailedCheck, internal/release:TestReleasePlanStopsAtTheFirstFailedCheck |

## Since the split

No change yet.
