# Install and uninstall

- **Component:** init / install / uninstall / doctor: install and uninstall
- **Paths:** `internal/install`, `cmd/eva-md/uninstall.go`
- **Status:** v1 as built, frozen on 2026-11-14

The blocks below are v1's verification text, moved here word for word when the two documents were split; its design text moved on 2026-11-14 to [install-design.md](install-design.md), and [INDEX.md](INDEX.md) lists every spec.

<!-- v1 docs/verification.md 010 -->
- **Install** (`internal/install`).
  Install, dry run, uninstall and rollback, with fake `claude` and `codex` commands standing in for the host CLIs.
  This covers partial failures, uncertain native writes, ownership checks, an interrupt during install, restoring a missing binary, Finder metadata files, a symlinked executable and uninstall without an install folder.
  A binary-only update leaves both plugin trees byte-identical.
  The digest skill is rendered into both plugins with its wording checked, while hooks.json stays byte-identical.
  Install records each CLI's path, keeping the other host's entry, also for a symlinked folder holding a script CLI, and uninstall removes that record of CLI paths.
  The recall skill is rendered into both plugins with hooks.json still matching its goldens, whose Claude Code tool matcher takes WebSearch beside WebFetch, and the digest skill still there; its frontmatter is exactly its name and a description of at most 200 characters with none of `:<>#"'`, its body
  at most 2,000 bytes holds the required commands and phrases, with no placeholder, version or date left (`TestRecallSkill`); install writes it, the dry run plans its render and writes nothing, a reinstall restores it and asks for Codex approval again, and uninstall removes both marketplaces (`TestRecallSkillInstallAndUninstall`).
  Codex is asked for its plugins one marketplace at a time, once for each name its marketplace list gives, and never for its list of every plugin, which fails every install test that asks for it, while Claude Code is asked for all; an eva-md copy from another Codex marketplace is still refused
  (`TestCodexIsAskedOneMarketplaceAtATime`), and a marketplace's list that keeps failing stops uninstall and install before they change anything (`TestFailedMarketplaceListChangesNothing`).
  Uninstall removes Claude Code's marketplace without `--scope`, since the fake Claude Code, as 2.1.316 does, keeps listing one removed with `--scope user` (`TestUninstallRemovesClaudesMarketplaceFromEveryScope`), and a host command runs from `/`, whatever folder eva-md runs in (`TestNativeRunnerIsBoundedAndHidesDiagnostics`).
  The fake Codex fails its lists, in Codex 0.166.0's words, while the eva-md marketplace it has registered at eva-md's folder has no manifest there: install, its dry run too, stops there before it changes anything and says to uninstall first, after which uninstall and install bring both hosts back,
  with the marketplace folder alone gone or the whole install folder (`TestInstallStopsAtAMissingCodexMarketplaceFolder`); uninstall's preview names the registration whose folder is missing, and `--apply` removes the plugin and then the marketplace
  (`TestUninstallRemovesARegistrationWhoseFolderIsGone`); and only eva-md's own folder, as given or with its symlinks resolved, and as the only marketplace Codex names, is taken for missing (`TestOnlyEvaMDsOwnFolderIsTakenForMissing`).
  A failed plugin add on a Codex that has eva-md's marketplace, with its folder, but not the plugin, is rolled back, keeping that registration and folder (`TestRollbackKeepsPreexistingInstallAndMarketplace`).

<!-- v1 docs/verification.md 038 -->
- **Setup's install checks, the live check's CLI, what uninstall finds unsaved, and the wiki's mode** (`internal/install`, `internal/update`, `internal/wikigit`, `internal/health`, `cmd/eva-md`).
  Install as a preflight, a dry run that needs no config, plans what a dry run plans and changes nothing on the hosts or on disk, while every other run still needs the config (`TestPreflightNeedsNoConfig`); a Codex with its hooks feature off is refused by install and the preflight alike, with `all`
  and `codex`, naming both ways out, turning it on, or `EVA_MD_HOSTS=claude` or `--host claude`, after installing Claude Code when PATH has no claude command, and for Claude Code only Codex's features are never read (`TestHooksOffNamesBothWaysOut`); the feature's state is read from Codex 0.167's
  `hooks stable` line and the `hooks experimental` one before it, by install (`TestHooksFeatureInEitherStage`) and by the doctor, whose row fails off with `codex features enable hooks` and is unknown for a list without it (`TestCodexHooksFeatureRow`); a failed host command is named, also when wrapped (`TestFailedCommand`).
  Setup runs those checks last in its checks of this Mac, the real ones against fake claude and codex commands: Codex's hooks feature off stops it with both ways out, the answers file's or `--host`, the second after installing Claude Code on a Mac with no claude command, but not when `--host codex`
  only leaves Claude Code out, and a failed host command, a registration that needs attention, another eva-md copy and a binary eva-md didn't install each stop it with their fix, and a list install can't read with the general one, exit 1, before any question or wiki, while a pass names the apps and
  setup goes on (`TestSetupStopsWhereInstallWouldRefuse`); end to end, `install.sh` on a new Mac whose fake Codex has its hooks feature off stops there with nothing made, pushed or registered, and with `EVA_MD_HOSTS=claude` installs for Claude Code only (`TestInstallScriptStopsBeforeTheWikiWhenCodexHooksAreOff`).
  Setup's last line names `doctor --live`, the command as eva-md or by its full path, whether the health check passed or not (`TestSetupNamesTheInstalledBinary`, `TestSetupNamesTheCommandOnPath`).
  `doctor --live` starts the CLI install recorded, in a folder off PATH where a lookup finds none, on both hosts (`TestLiveCheckStartsTheRecordedCLI`) and through the doctor (`TestDoctorLive`).
  `wikigit.Unsaved` counts nothing after setup's push; changed files under the commit paths, but not JSONL, temporary files or other paths when the wiki's `.gitignore` is gone; commits no push took, also with no upstream and for a first commit never pushed; none for a wiki with no commit; it never
  fetches or changes the clone, and fails for a folder git can't read (`TestUnsavedCountsWhatTheRemoteLacks`).
  Uninstall says what it found in its preview's last lines, `--all`'s question and after `--apply`, not calling the remote pushed and naming `eva-md commit`, or `--push-only` for commits alone, and says a wiki git can't read is so without stopping, the wiki and its remote unchanged (`TestUninstallSaysWhatTheWikiHasNotSaved`).
  It names the stable binary by its full path where `~/.local/bin` isn't on PATH; once eva-md is gone, after `--apply` and in a later preview and question, it offers git or a new install, never a commit before uninstalling; and for `--all`, in its preview, its question and after it, git alone for changed files, while a commit alone keeps both (`TestUninstallNamesOnlyWhatCanSaveTheWiki`).
  With the config moved aside, or with one that can't be read and a commit alone, it gives git alone in its previews, `--all`'s question and after `--apply`, never `eva-md commit` or a new install (`TestUninstallNamesGitAloneWithoutTheConfig`); it takes the wiki from the config, another than the
  record's too, and from the record when the config names none, can't be read or is missing, says which, and keeps the config's answer through an `--all` that removes it (`TestUninstallSaysWhetherTheConfigNamedTheWiki`).
  Its last lines read the wiki again after the removal: after `eva-md commit` run at `--all`'s prompt they say it is pushed, and after a page written at the prompt, or by a fake Claude Code while it removes the plugin, with `--apply` or `--apply --all --yes`, that it isn't committed (`TestUninstallReadsTheWikiAgainForItsLastLines`).
  `install.Command` says eva-md only for eva-md's link in a folder on PATH, and the full path with no link, a link to something else, or the folder off PATH (`TestCommandNamesTheLinkOnPath`).
  The line's exact words, for one and many, with either name, before and after the removal, with and without `--all`, and for a wiki the config doesn't name (`TestUnsavedLine`).
  The wiki permissions row fails 0755, 0750 and 0711 with `chmod 700` and the folder quoted for a shell, and passes 0700 (`TestWikiPermissionsRow`).

<!-- /v1 -->

## Must keep

The must-keep items of [findings.md](../v2/findings.md) this component touches, numbered from K01 in each list, and the tests that hold them.
An item that tests hold only in part ends with what of it no test holds, as `(untested: ...)`.

| Id | Item | Tests |
| --- | --- | --- |
| MCP-K07 | Plugin folders hold no version or build id and render the same each time, so a binary-only release keeps Codex's trust | internal/install:TestPluginFolderBytes, internal/install:TestBinaryOnlyReinstallKeepsCodexTrust |
| MCP-K08 | Plugins load in place: rendered again only when bytes change, swapped atomically, never deleted while installed | internal/install:TestBinaryOnlyReinstallKeepsCodexTrust, internal/install:TestUpgradeWhenTheHostRefusesToAddAnInstalledPlugin, internal/install:TestFailedUpgradeRestoresOldPluginBytes, internal/install:TestIncompleteRollbackKeepsLoadableFiles |
| MCP-K15 | eva-md never writes host settings and never grants Codex hook trust | internal/install:TestFirstInstallOnTheFakeHosts, internal/install:TestUpgradeFrom2026_11_5OnTheFakeHosts |
| SET-K07 | Install's checks run before any wiki is made or pushed, and a failed install rolls back | cmd/eva-md:TestSetupStopsWhereInstallWouldRefuse, cmd/eva-md:TestSetupChecklistStopsAtTheFirstProblem, internal/install:TestPreflightNeedsNoConfig, internal/install:TestInterruptRollsBackInstall |
| OPS-K01 | Hosts are registered only through their own CLIs; host settings and trust records are never read or written | internal/install:TestFirstInstallOnTheFakeHosts, internal/install:TestInstallRunsTheAdaptersCommands, internal/health:TestTrustProtocolIsReadOnly |
| OPS-K02 | A host command's diagnostics are matched, never shown | internal/install:TestNativeRunnerIsBoundedAndHidesDiagnostics, internal/health:TestDuplicateAndDiagnosticsNeverPassTrust |
| OPS-K03 | Host commands run from /, and Claude Code's marketplace is removed from every scope | internal/install:TestNativeRunnerIsBoundedAndHidesDiagnostics, internal/install:TestUninstallRemovesClaudesMarketplaceFromEveryScope |
| OPS-K04 | Codex is asked for plugins one marketplace at a time, never for its global list | internal/install:TestCodexIsAskedOneMarketplaceAtATime |
| OPS-K05 | Plugin folders carry no version or date and are rewritten only when bytes change | internal/install:TestPluginFolderBytes, internal/install:TestBinaryOnlyReinstallKeepsCodexTrust |
| OPS-K06 | A binary or marketplace folder eva-md did not make, links and unknown files are refused; Finder's files are cleaned | internal/install:TestOwnershipAndSymlinkRefusals, internal/install:TestOSMetadataFilesAreIgnoredAndCleaned, internal/install:TestUninstallRefusesAnUnmanagedFolderOfAnyHostsMarketplace |
| OPS-K07 | Rollback undoes only this run's steps, adds back a plugin a failed upgrade removed, and lets a started command finish | internal/install:TestRollbackPartialAndUncertainWrites, internal/install:TestRollbackKeepsPreexistingInstallAndMarketplace, internal/install:TestRollbackReaddsPluginAFailedUpgradeRemoved, internal/install:TestFinishStartedLetsARunningCommandFinish, internal/install:TestInterruptRollsBackInstall |
| OPS-K08 | Install works under its lock, and uninstall refuses while the update or daily job runs | internal/install:TestNewInstallLockTakesConcurrentCreators, cmd/eva-md:TestUninstallWaitsForTheUpdateAndDailyJob |
| OPS-K09 | A missing Codex marketplace folder stops install and uninstall clears it; Codex hooks off is refused with both ways out | internal/install:TestInstallStopsAtAMissingCodexMarketplaceFolder, internal/install:TestUninstallRemovesARegistrationWhoseFolderIsGone, internal/install:TestMissingCodexMarketplaceFolderOnTheFakeHosts, internal/install:TestHooksOffNamesBothWaysOut |
| OPS-K10 | Hooks call one stable absolute path whose binary is replaced atomically, and Codex's hook line prints {} on failure | internal/install:TestGeneratedHookContracts, internal/install:TestInstallRestoresMissingBinary, internal/install:TestInstallThroughSymlinkedExecutable |
| OPS-K11 | Host CLI paths for detached jobs are resolved through a version manager's links | internal/install:TestInstallRecordsTheCLIPaths |
| OPS-K12 | The PATH link is never forced: no folder made, no shell file edited, no other eva-md replaced | internal/install:TestInstallLinksOnPath |
| OPS-K13 | Uninstall never touches the wiki or its remote, warns of unsaved work, and --all needs a typed confirmation | cmd/eva-md:TestUninstallApplyKeepsConfigDataAndWiki, cmd/eva-md:TestUninstallSaysWhatTheWikiHasNotSaved, cmd/eva-md:TestUninstallAllAsksForDelete, cmd/eva-md:TestUninstallPreviewRemovesNothing, internal/install:TestUninstallAll |

## Since the split

**2026-11-14, HO-11: the install manifest, behind a switch that is off.**
With the switch `install_manifest` in `config.toml` on (it is off by default and belongs to v2's build), install keeps `install-manifest.json` in eva-md's data folder, 0600.
For each unit (`claude-code/plugin`, and `codex/hooks` at generation 0) it lists the files install rendered in that host's marketplace folder, each with its SHA-256 and size.
A file in a plugin folder that the manifest lists is eva-md's: one this release no longer renders goes with the folder swap, and a dry run names each file it would remove.
A file or folder no manifest lists still stops install and uninstall.
With the switch on and no manifest, the eight paths releases 2026.11.5 to 2026.11.7 render in each folder are the list, so a v1 folder is adopted.
A manifest on disk is read and kept current whatever the switch says.
One this release cannot read stops install and uninstall with one line naming it; that includes one that lays out a unit this release renders at another folder, host or plugin, or a later generation.
A unit of a later release whose folder lies inside a plugin folder this release renders also stops them, with one line naming that folder; a later unit's folder beside eva-md's is left alone and stays listed.
A failed install puts the manifest back as it was, or removes it; one whose rollback stops part way leaves the manifest written before its first change, which lists the files of both releases.
Uninstall removes it with the plugin folders, keeping only the units of a later release that it does not render; with `--all`, which removes the data folder, it names the manifest and those units' folders, and leaves their host registrations.
With the switch off and no manifest on disk, install and uninstall are as the blocks above say, and no manifest is written.
