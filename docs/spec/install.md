# Install and uninstall

- **Component:** init / install / uninstall / doctor: install and uninstall
- **Paths:** `internal/install`, `cmd/eva-md/uninstall.go`
- **Status:** v1 as built, frozen on 2026-11-14

The blocks below are v1's design and verification text, moved here word for word when the two documents were split; [INDEX.md](INDEX.md) lists every spec.

<!-- v1 docs/design.md 015 -->
## Install and uninstall

**Preflight.**
- The config exists.
- The `claude` and `codex` CLIs are found (each host is optional).
- No other eva-md plugin copy is enabled.
- Managed settings are not read. Hook heartbeats show whether a policy blocks hooks.
- Read `codex features list` and refuse when the hooks feature is off, also with `all` (`install.ErrHooksOff`), naming both ways out: turn it on with `codex features enable hooks`, or install for Claude Code only, with `EVA_MD_HOSTS=claude` in `config.env` for setup and upgrades, or `--host claude`, after installing Claude Code when PATH has no `claude` command.
  Install records no features: the update run reads the list itself before each attempt (see "Model selection").
- A registration that needs attention, such as a disabled plugin, is refused with uninstall, then install, as the fix.
- Setup runs all of these, and the binary and folder checks below, before it makes the wiki, last in its checks of this Mac, as a preflight: a dry run that needs no config yet (see "Setup and init of a new wiki").
- After a successful install, record the absolute path of each CLI found in `<state>/cli-paths.json` for the update job and `doctor --live` (see "Model selection").

**Binary.** Copy it atomically to `~/.local/share/eva-md/bin/eva-md`, reading a symlinked executable from its real file. If the installed binary is missing under eva-md's own install marker, install restores it. It leaves the hosts alone unless a plugin folder differs, which it renders again as in any upgrade (but see "A missing Codex marketplace folder").

**Marketplaces.** Render both marketplace folders from the embedded templates.
A folder is rewritten only if its bytes would change.
Write it into a sibling folder and swap with two renames, and never delete it while the plugin is installed.
If neither folder changed and both hosts already show the plugin enabled, install registers nothing: this was a binary-only update.
It still validates the Claude folders and records the CLI paths.
Finder's metadata files (`.DS_Store`, `._*`) in a managed folder are ignored, and removed in a real run; any other unexpected file stops install, which names it and says to move it out of that folder.
So do a marketplace folder and a stable binary eva-md didn't make, which install says to move aside.

**Claude.**
1. Run `claude plugin validate --strict --json` on both the marketplace root and the plugin folder.
2. `claude plugin marketplace add ~/.local/share/eva-md/marketplace/claude --json`
3. `claude plugin install eva-md@eva-md --scope user --json`
4. Verify with `claude plugin list --json`: exactly one enabled eva-md, `readFromFolder` pointing at our folder, the expected `folderVersion`, and empty `errors`.

**Codex.**
1. `codex plugin marketplace add ~/.local/share/eva-md/marketplace/codex --json`
2. `codex plugin add eva-md@eva-md --json`
3. Verify with `codex plugin list --json --marketplace=<name>`, once for each marketplace `codex plugin marketplace list --json` names, which Codex 0.166.0 answers from this Mac.
   Its list of every plugin, `codex plugin list --json`, also asks chatgpt.com for the account's remote plugins when Codex is signed in to ChatGPT, and on a network that holds that request it waits 30 seconds before it answers without them, so eva-md never asks for it.
   A plugin in Codex's remote catalog isn't seen.
4. If the plugin folder changed, or Codex didn't have the plugin before, write a needs-trust flag.

**A missing Codex marketplace folder.** While Codex has eva-md's marketplace registered at eva-md's folder, but the folder has no manifest, as after it was deleted by hand, Codex fails its marketplace list, its list of every plugin and its list of eva-md's marketplace, though a list of another local marketplace still answers.
Codex 0.166.0 names each marketplace it can't load on a line of its own, here "- `eva-md` at <folder>: marketplace root does not contain a supported manifest".
Inspect takes that for this state only when eva-md's own folder, as given or with the symlinks of its deepest existing folder resolved, as Codex keeps it, is the only marketplace named; Codex's text is matched only, never shown.
The status says the marketplace is registered and the folder missing, with the plugin unknown.
Install stops there before it changes anything, its dry run too, and says to run `eva-md uninstall --apply`, or the uninstall script with `--apply`, and then to install again: rendering the folder again would bring back whatever Codex kept of the plugin, disabled or not, and `plugin add` would enable it, which no rollback could tell apart from its own.
Uninstall removes the plugin and then the marketplace, which Codex allows in that state, even for a plugin it never had.
The doctor reports it as a FAIL with that fix.
Any other failure, this wording for another folder, or another marketplace failing with it, stays a failure.

Each host command runs from `/`, so no project's own settings take part: Claude Code reads the folder it runs in as a project, and from the home folder its project settings are the user settings file.
Each has 60 seconds, twice what Codex gives a request to chatgpt.com, within the 2 minutes a whole install, uninstall or setup's install has, and an install's rollback has 90 seconds; the doctor's inventory of each host has 5 seconds.
Another eva-md registration on a host is an error of its own kind (`install.ErrConflict`), which the doctor reports as a FAIL, apart from a host command or its output that failed.

On an upgrade over an installed plugin, a host's refusal to add it again is accepted when the verify step still passes, since both hosts load a local plugin's folder in place; a rollback accepts the same refusal when the old registration verifies.

**Manual step printed.** Review eva-md's hooks once in Codex /hooks, then start new chats; open chats are not restarted.
If anything fails, roll back only the steps this run took.
A rollback re-adds a plugin that a failed upgrade removed, after the old files are back.
Ctrl-C or SIGTERM lets a host command that has started finish, starts no new one, and rolls back.
The output says whether the actions were a preview, done, or failed.
A failed install's error says whether its changes were rolled back, or that none of the planned actions ran.

**Install record.** A successful install adds what it made or found to `~/.local/share/eva-md/install-record.json` (0600, written atomically under the install lock): the stable binary, each host's marketplace folder and plugin registration, the link on PATH, `config.toml` and `config.env` when they
exist, eva-md's config, data, state, log and cache folders, and the wiki folder and its remote as items that are kept.
Each item is a kind, a host, a path and a value; a later install replaces an item of the same kind, host and path and keeps the rest.
Setup's own writes of `config.toml` and `config.env` are recorded by the install it runs, and, since on a Mac set up before there was a `config.env` setup writes that file after its install, setup's command adds both files to the record as it ends, when a record exists and lacks them; without a record, the next install lists them.
A dry run writes nothing.

**Link on PATH.** After a successful install, `~/.local/bin/eva-md` is linked to the stable binary when `~/.local/bin` exists, is a folder on the PATH install runs with (compared through symlinks, so `/var` and `/private/var` match), and has no eva-md in it, or already holds eva-md's own link.
The link is made as a temporary link renamed into place, checked again just before the rename.
Anything else changes nothing, and the binary is named by its full path.
Install never makes the folder, edits shell files or changes PATH.
The result names the link, and the dry run plans it.

**Uninstall.** `eva-md uninstall` previews: it lists what `--apply` would remove and what it keeps, the config folder and the local data with their sizes, and the wiki folder and its remote last.
It reads the wiki folder, when it is there, with local git only and within 10 s (`wikigit.Unsaved`), before it removes anything, for the preview and `--all`'s question, and again after the removal, on every path, for its last lines, since the wiki may change while the question waits or the hosts
remove the plugins: the commits on HEAD that no branch of origin's holds, as far as this clone knows, which it never fetches, and the files changed under the daily commit's paths that no commit holds, but for those a commit leaves out by their names, JSONL and temporary files.
When either isn't zero, the wiki's closing line gives the remote as "its remote is <remote>" in place of "pushed to <remote>", and the next line says, as in "In the wiki, 2 changed files aren't committed and 1 commit isn't pushed, so they are only on this Mac", to run `<bin> commit` before
uninstalling to commit and push them, or `<bin> commit --push-only` for commits alone, in the preview and in `--all`'s question.
`<bin>` is named as setup's closing lines name it, as this Mac has it before anything is removed (`install.Command`): eva-md when the link on PATH reaches the stable binary, and its full path otherwise.
After `--apply`, and in a preview or question with no stable binary there to run, as after an earlier `--apply`, when the uninstall script runs the latest release's, it says to commit and push them with git in the wiki folder, or install eva-md again and run that.
Then, for `--all`, which removes the config, changed files get git alone: setup takes a wiki with changes no commit holds only with the config that names it, while one with commits alone it takes and pushes.
When the config doesn't name the wiki, as when it was moved aside or can't be read, and uninstall takes the wiki from the record, the lines give git alone, for commits alone too, in the preview, the question and after `--apply`: `<bin> commit` works only with the config that names the wiki, and setup refuses a config it can't read.
Whether the config names the wiki is read before anything is removed, so after `--all` the last lines still go by it.
A read git can't make is one line saying so, and never stops uninstall.
`--apply` runs in this order:
1. under the install lock, take the update's and the daily job's locks without waiting; while either is held, remove nothing and say to try again shortly (both locks are held until uninstall ends, so neither job starts meanwhile)
2. `claude plugin uninstall eva-md@eva-md --scope user` and `codex plugin remove eva-md@eva-md`
3. remove each host's marketplace entry right after its plugin, so Claude's plugin and entry go before Codex's: `claude plugin marketplace remove eva-md`, without `--scope`, since Claude Code 2.1.316 keeps listing a marketplace removed with `--scope user` while another scope declares it, and `codex plugin marketplace remove eva-md`
4. delete the marketplace folders
5. delete the binary, then the `bin` and `marketplace` folders install created, if they are empty
6. delete the link on PATH, only while it points at the stable binary, and the state folder's `needs-trust.json` and CLI paths
7. remove the install record's program items, and the data folder if nothing is left in it

It keeps the inbox, the config, the record of what is left, and the wiki.
`--all` with `--apply` also removes eva-md's whole config folder (`config.toml` and `config.env`), state folder (inbox, markers, locks, logs, caches and the search index) and data folder (the install record), after the person types `delete` at a prompt on the terminal, with `--yes` too; any other answer removes nothing at all.
Without a terminal, `--all` needs `--yes` instead.
`--all` refuses, removing nothing, when the wiki is inside one of those folders.
Uninstall never removes the wiki folder or touches its remote: its last lines say where the wiki is and that deleting it is the person's own step, after a line saying that Codex keeps its approval of eva-md's hooks in its own config, which eva-md never edits, and that `/hooks` in Codex clears it.

Uninstall reads the link from the record, and falls back to the known layout (`~/.local/bin/eva-md`) when the record is missing, can't be read or names no link; it names the wiki and its remote as the config has them, or as the record has them when the config can't be read or names no wiki.
It removes eva-md's folders by their known paths, never by a path the record names, and nothing outside them but the link and the host registrations, through the hosts' native commands.
Without the install folder, or with the folder but none of eva-md's files in it, uninstall still asks both hosts and removes the `eva-md@eva-md` plugin and the `eva-md` marketplace, and the link on PATH while it points at the stable binary's path, even with no binary there; and when Codex has that
marketplace registered without its folder, uninstall removes Codex's plugin and marketplace too (see "A missing Codex marketplace folder"); it deletes no other file but the state folder's `needs-trust.json` and CLI paths, unless `--all` is given.
With eva-md's files but no marker, it refuses.

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

<!-- /v1 -->

## Since the split

No change yet.
