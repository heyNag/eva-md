# eva-md audit: operations

Scope: install, uninstall and upgrade, releases, the health check, notices and the status command.

Read in full: `internal/install` (install.go, host.go, record.go, uninstall.go, templates.go, mcp.go), `internal/health` (all non-test files), `internal/release` (fetch.go, version.go), `cmd/eva-md/{upgrade,uninstall,commands,main}.go`, `scripts/install.sh`, `scripts/uninstall.sh`, `docs/releasing.md`, design.md 670-788 and 1195-1226, known-limitations "Install and uninstall", "Releases", "Health check". Read as far as needed to follow the data flow: `internal/hook/{resume,maintain,spawn,hook}.go`, `internal/mcp/instructions.go`, `internal/update/{runlog,job}.go`, `cmd/eva-md/{setup,maintain}.go`, the workflows, decisions.md. Tests were read by name and spot-checked, not run.

Every claim below is from the code unless it says "inferred" or cites a doc. Paths are relative to `the repository`.

---

## 1. How it works

### 1.1 Install (`internal/install`)

Trigger: `eva-md install [--host] [--dry-run]` (cmd/eva-md/commands.go:212-263), or `eva-md setup`, which runs it as a preflight dry run before it makes the wiki (cmd/eva-md/setup.go:325-334) and again for real (setup.go:553-556). Budget: 2 minutes for the whole install (commands.go:34), 60 s per host command (host.go:112-115), 90 s for a rollback (install.go:79).

Flow of `runInstall` (install.go:121-422):

1. Config must exist unless this is setup's preflight (install.go:139-146).
2. For each selected host, `inspect` asks the host's own CLI for its marketplaces and plugins (host.go:166-267). Nothing is read from host settings files (install.go:1-3, host.go:17).
3. Refusals before anything changes: Codex's marketplace registered with its folder gone (install.go:160-162), a registration that is installed but not valid (install.go:170-172), Codex's `hooks` feature off (install.go:179-190, 81-96), an unmanaged binary or marketplace folder (install.go:234-236, 655-657), a symlink or unexpected file in a managed folder (install.go:669-691).
4. The plugin folder for each host is rendered from embedded templates into a map of path to bytes (templates.go:34-117): marketplace manifest, plugin manifest, icon, two skills with the binary's absolute path filled in, `hooks.json`, `.mcp.json`. The folder is compared byte by byte with what is on disk (install.go:244, 640-712). `changed` drives everything after.
5. Under `install.lock` (install.go:279, 789-820) the host state, the folder state and the binary are all read again and must equal the preflight snapshot (install.go:284-319).
6. Mutations, in order: write the install marker (install.go:320); write the binary atomically to `~/.local/share/eva-md/bin/eva-md` when its bytes differ (install.go:344-348); swap each changed marketplace folder with two renames and a kept backup (install.go:351, 557-607); `claude plugin validate --strict` on both Claude folders (install.go:356-362); `plugin marketplace add` and `plugin install|add` only when the folder changed or the registration was not valid (install.go:363-391).
7. When Codex's folder changed or Codex did not have the plugin, write `<state>/needs-trust.json` (install.go:262-264, 392-403).
8. Record each host CLI's absolute path in `<state>/cli-paths.json` for detached jobs (install.go:404, 424-448; host.go:303-320).
9. Link `~/.local/bin/eva-md` only when that folder exists, is on PATH and holds no other eva-md (install.go:405-412; record.go:193-251).
10. Add to `install-record.json` (install.go:413-420; record.go:92-130).

Failure: a deferred rollback undoes only this run's steps, reconciling through fresh host inventories (install.go:323-342, 482-549). Ctrl-C lets a started host command finish and starts no new one (install.go:122-125, 626-636).

Hook wiring that install renders: Claude Code gets 10 events, Codex 7 (templates.go:71-88). Claude's command is the binary by absolute path with args; Codex's is a shell line ending `|| echo '{}'` (templates.go:91-96). Tool events are `async` (templates.go:74-75, 84).

### 1.2 Uninstall (`internal/install/uninstall.go`, `cmd/eva-md/uninstall.go`)

Preview by default. `--apply` takes `install.lock`, then `update.lock` and `maintain.lock` without waiting, and refuses while either job runs (uninstall.go:166-183). It removes each host's plugin and marketplace through the host CLI, verifies the host no longer lists eva-md, then deletes the folders, binary, marker, PATH link (only while it still points at the stable binary), `needs-trust.json` and `cli-paths.json` (uninstall.go:193-241). `--all` also removes the config, state and data folders after the person types `delete` (cmd uninstall.go:147-162; uninstall.go:242-249), and refuses when the wiki is inside them (uninstall.go:73-79). It reads what the wiki has not committed or pushed, before and after, with local git and 10 s (cmd uninstall.go:22-48), and never touches the wiki.

### 1.3 Upgrade (`cmd/eva-md/upgrade.go`, `internal/release`, `scripts/install.sh`)

Trigger: only a person running `eva-md upgrade` or the curl command. `internal/release` says so itself: "Only eva-md upgrade uses it: no hook, update or daily job looks for a release" (release/version.go:1-3). Nothing else in the tree calls `release.Latest` (grep: cmd/eva-md/upgrade.go:69 is the only caller).

Flow (upgrade.go:46-167): read the latest tag from where `releases/latest` redirects (fetch.go:48-78, 30 s); compare calendar versions (version.go:86-118); download the binary and `SHA256SUMS` into `<share>/.upgrade-*` (5 minutes each, 128 MiB and 64 KiB caps, redirects only to github.com or `*.githubusercontent.com`; fetch.go:28-37, 83-138); check the sum (fetch.go:155-185) and that the binary reports the tag (upgrade.go:148-152); then run the new binary's `setup` attached to the terminal (upgrade.go:156-166). On a Mac that is set up, setup runs its checklist, then asks "Install or update the plugins? [y/N]" (setup.go:477-490), then installs, then runs the doctor's checks (setup.go:585-603). Without a terminal it does not ask (setup.go:477).

`install.sh` does the same in bash 3.2: checks macOS, not root, arch; reads the tag; downloads; checks the sum and the reported version; runs `setup` reading `/dev/tty` (install.sh:31-125).

### 1.4 Releases (`.github/workflows`, `scripts/release.sh`, docs/releasing.md)

A release is an annotated tag on a commit of main. Two ways: the Cut a release workflow (plan, approval in the `release` environment, unsigned tag, called release workflow, check job) or `scripts/release.sh` from a Mac (signed tag). The release workflow builds two darwin binaries with `CGO_ENABLED=0 -trimpath -mod=readonly -ldflags "-X main.version=<tag>"` (.github/workflows/release.yml:109-114), verifies on a macOS runner, attests build provenance (release.yml:151-163) and publishes six assets. Tags are the commit's UTC day, `2026.11.3`, then `.2`, `.3` (version.go:12-14). Tags are never moved, reused or deleted (releasing.md:72, 148).

### 1.5 Health (`internal/health`)

Three surfaces read the same on-disk records:

- **`QuickNotice`** (health.go:53-119), run by the SessionStart hook and the first-prompt fallback (internal/hook/resume.go:173-176) and by the MCP server (internal/mcp/instructions.go:177, internal/mcp/status.go:101). It returns the first problem found, as one line, in this order: unknown config keys; root `index.md` unreadable; wiki schema newer; instance differs; needs-trust flag; the other host's capture freshness; hook failures; transcript drift; then `backgroundNotice` (background.go:407-447): a held host, 3 failures in a row, a quarantined session, a diverged push, a commit failing over 48 h, a push pending or failing over 72 h (thresholds background.go:36-40). It reads at most 64 KB of each of two days' run log and 16 KB of each of two months' commit records (background.go:27-33).
- **`eva-md doctor`** (`Inspect`, health.go:149-211): 51 to 55 rows on a Mac with both hosts, each PASS, FAIL or UNKNOWN with a one-line fix. It asks the hosts (5 s inventory each, 5 s for `codex features list`, 8 s for the read-only `codex app-server` hooks/list probe, 3 s for the MCP probe; hosts.go:37-41, mcp.go:83). Overall 30 s (commands.go:34), though its two repairs each wait up to 60 s for the wiki lock (known-limitations 328). The two repairs: the names and repos indexes (health.go:325-364) and the files built from knowledge pages (knowledge.go:34-78).
- **`eva-md status`** (status.go:30-39): the same local rows without asking hosts and without repairs, plus wiki contents and the update backlog.

Delivery of a notice: the hook puts it in `systemMessage`, and into `additionalContext` only when there is no brief (resume.go:179-184, 95-105). Claude Code shows `systemMessage` as a line; Codex shows it as a warning (design.md:564).

Heartbeats: each captured hook event writes `<state>/heartbeats/<host>/<event>.json` with its last 20 runs (resume.go:204, 228-257). Capture freshness compares the newest heartbeat with directory modification times in `~/.claude/projects` or `~/.codex/sessions`, never opening a transcript, within 35 ms (watchdog.go:121, 137-184).

### 1.6 What triggers background work

There is no scheduler. Every background job is spawned by a hook after it has written its output (internal/hook/spawn.go:19-69): the day's `maintain` by the first captured hook event of a calendar day (internal/hook/maintain.go:55-81), a push retry by a chat start at most every 30 minutes while a push is pending (maintain.go:83-105), and updates by their own triggers. `queueMaintain` is reached only for captured events (internal/hook/hook.go:339-360).

---

## 2. What must be kept

Invariants and hard-won fixes. Each cost a debugging round (see `git log -- internal/install`: three host-quirk commits on 2026-11-03 alone).

1. **Host registration only through the hosts' own CLIs.** No host settings or trust record is read or written, and Codex trust is never granted (install.go:1-3; host.go:17; hosts.go:158 sends only `initialize` and `hooks/list`).
2. **Host diagnostics are never shown**, only matched, since they can name paths or credentials (host.go:35-41, 129-134).
3. **Host commands run from `/`**, so no project's settings take part (host.go:121-125), and Claude's marketplace is removed from every scope (install.go:474-480).
4. **Codex is asked for plugins one marketplace at a time.** Its list of every plugin calls chatgpt.com and can wait 30 s (host.go:269-301). Host timeout is twice that (host.go:108-115).
5. **Plugin folder bytes carry no version or date**, so a binary-only release keeps Codex's trust; a folder is rewritten only when its bytes change (templates.go:11-12; mcp.go:10-15; install.go:241-265).
6. **Ownership markers.** Install and uninstall refuse a binary or marketplace folder eva-md did not make, a symlink, or an unexpected file; `.DS_Store` and `._*` are ignored and cleaned (install.go:224-240, 640-712; uninstall.go:92-101).
7. **Rollback undoes only this run's steps**, reconciles through inventories, and re-adds a plugin a failed upgrade removed (install.go:482-549). A started host command finishes on Ctrl-C (install.go:626-636).
8. **Snapshots are rechecked under the lock** (install.go:284-319). Uninstall holds the update and maintain locks and refuses while either runs (uninstall.go:171-183).
9. **Codex's missing-folder state**: install stops, uninstall clears it (host.go:45-73; install.go:157-162; uninstall.go:194-200).
10. **Codex hooks feature off is refused with both ways out** (install.go:81-96).
11. **Hooks call a stable absolute path**, and the binary there is replaced atomically, so a running hook keeps its old file (install.go:344-348). Codex's hook line ends `|| echo '{}'` (templates.go:93).
12. **CLI paths are resolved through symlinked version-manager folders** so a detached job finds the CLI (host.go:303-320).
13. **The PATH link is never forced**: no folder is made, no shell file edited, no other eva-md replaced (record.go:199-251).
14. **Uninstall never touches the wiki or its remote**, warns of unsaved commits and files before and after, and `--all` needs `delete` typed on a terminal (cmd uninstall.go:95-189).
15. **Nothing downloaded runs before its checksum and reported version match**; HTTPS only; redirects only to GitHub's hosts; size caps; the script body is in functions so a cut-off download runs nothing (fetch.go:83-138; upgrade.go:131-152; install.sh:14-15, 82, 94-109).
16. **Release discipline**: main only, green CI, recorded human approval or a signed tag, provenance verified against one signer workflow before any downloaded binary runs, actions pinned to SHAs, tags never moved (releasing.md:26-38, 76-89, 113).
17. **`QuickNotice` is bounded, local and content-free** (health.go:51-52), reading only file tails (background.go:27-33).
18. **Capture freshness reads metadata only**, bounded at 35 ms and 16,384 entries, and the uncaptured stamp stops a false alarm from an excluded folder (watchdog.go:121-128, 150-152, 193-202).
19. **The trust notice is satisfied only by a real Codex SessionStart heartbeat newer than the flag**, and the running Codex hook is not warned (health.go:90-96).
20. **The trust probe is read-only, wants exactly the 7 events, and rejects duplicates and diagnostics** (hosts.go:113, 253-279). Each host's inventory has its own budget so a stalled listing cannot hide trust evidence (hosts.go:49-55).
21. **`status` never writes; the doctor's repairs touch only derived indexes, under the wiki lock, only in a wiki of schema 1 and this instance** (health.go:239, 309-311; knowledge.go:84-87).
22. **Only eva's name is shown, and counts rather than content** (main.go:203-204).

---

## 3. The three questions asked

### 3.1 How a user learns that memory has silently stopped

| What stopped | Signal eva-md gives | Where it comes from |
|---|---|---|
| Hooks on the only host the user uses (plugin disabled, policy, host update) | None from the hook, which is not running. The MCP server warns the model, not the user, only when no SessionStart ever ran or no hook ran for 7 days. | health.go:98-107 checks only the *other* host; mcp/instructions.go:33, 168-176 |
| Hooks on host A, user also chats on host B within 24 h | One line at B's next chat start | watchdog.go:172-175; health.go:103-106 |
| Plugin removed or disabled by the host | Nothing: hooks and MCP server are both gone | inferred from the wiring: both are declared by the plugin (templates.go:114-115) |
| Codex trust lapsed after an upgrade | One line at install; one line at each *Claude* chat start; the MCP instructions, which Codex "may not show at all" (known-limitations 309) | install.go:392-403; health.go:74-97 |
| Updates failing | One line at the next chat start on either host, only for a hold today, 3 failures in a row within today and yesterday, or a quarantine today | background.go:411-432; update/runlog.go:227-237 |
| Updates not running at all (nothing recorded) | Nothing. The row is UNKNOWN "no update recorded today or yesterday"; the backlog row is PASS however old | background.go:106-110, 132-133; status.go:116-128 |
| Daily commit not running at all | Nothing. `commitRow` shows the last record with no age check | background.go:281-308 |
| Commit refused or failing | One line after 48 h | background.go:441-442 |
| Push pending or refused | One line after 72 h; diverged at once | background.go:439-444 |

In every case the line ends "run eva-md doctor", which is a terminal step.

### 3.2 How upgrades reach users

They do not, unless the person runs `eva-md upgrade` or the curl command. No code path checks for a release (release/version.go:1-3). "Telemetry and auto-update" is a ruled-out decision (decisions.md:117; design.md:1179, 1360). The upgrade itself is interactive: a y/N question whose default is no (setup.go:477-490). Any upgrade that changes a byte of Codex's plugin folder asks for hook approval in Codex again (install.go:262-264; known-limitations 263).

### 3.3 What a user must do by hand over a month

From the fixes the code prints, and the flows above:

- First install: a terminal command, the folder and remote questions, "Set up eva.md with these choices? [y/N]", git signing and push approvals, "Push it to …? [Y/n]" (setup.go:477-522).
- Codex: open `/hooks` and approve 7 hooks (commands.go:259-261; setup.go:566-569). Again after each upgrade that changes the plugin folder.
- Claude Code: allow each MCP tool on first use, or edit settings (known-limitations 311).
- Learn of each release some other way, run `eva-md upgrade`, answer y.
- On any notice: open a terminal, run `eva-md doctor`, read about 50 rows, then one of: `claude auth login` / `codex login` (background.go:152-160); `eva-md update --pending --foreground` (background.go:113-114); `eva-md install` again to record a moved CLI path (background.go:115-120); `eva-md commit` and approve a signature (background.go:329-336); merge a diverged wiki by hand (background.go:355); delete a damaged watermark file by hand (background.go:129; status.go:125); `chmod 700` the wiki (health.go:232); trim a thread page over 512 KB by hand (background.go:471); `eva-md system write <slug> --replace` to rebuild the map (background.go:598); ask a chat to digest moved systems again (code.go:163); `eva-md doctor` to rebuild a stale names index (health.go:337).
- Disk: nothing prunes the inbox; `eva-md inbox clear` is manual (design.md:1216).

---

## 4. Findings, ranked

### ops-1. A silent stop is seen only as a recorded failure, never as missing work (critical; reliability-safety; verified)

The health model answers "did a job record a failure?" It never asks "has the work that should have happened, happened?"

- Capture freshness for a host is checked only from the *other* host's chat start (health.go:98-107). A person who uses one host gets no freshness alarm, ever.
- The MCP server's own-host warning needs 7 days without any heartbeat (mcp/instructions.go:33, 174-176), goes to the model, and vanishes with the plugin.
- `backgroundNotice` knows three update states, all needing records in today's or yesterday's log (background.go:411-432; update/runlog.go:227-237). A hold lasts "today" only (runlog.go:341-349), so the first chat of the next day is silent until a new failure lands.
- Updates that never start leave no record: UNKNOWN, no notice (background.go:132-133).
- The update backlog row is PASS at any size or age (status.go:116-128), and only `status` has it; the doctor does not (health.go:149-211).
- The daily commit has no staleness check: a last success three weeks old reads PASS (background.go:281-308).

Cost: capture, updates or pushes can be off for days while every chat looks normal. The wiki goes stale, briefs go stale, and chats made meanwhile are not captured, with no backfill (decisions.md:116).

Direction: rebuild the signal as a freshness ledger. One small state file holds, per host and per stage, the time of the last expected-and-done step: last host activity, last capture, last update that drained, last commit, last push, oldest backlog entry. Health is lag between adjacent stages against a budget. The ledger is evaluated by something that does not depend on the failing stage (see ops-4), and the alert goes out of band (an OS notification, the other host, a status line), not only through the hook that may be dead.

Known: no. Known-limitations 322-331 lists bounds of the existing checks, not this gap.

### ops-2. Any change to Codex's plugin folder turns Codex capture off until the user re-approves (critical for Codex users; invisibility; verified, host behaviour from docs)

Install flags trust for any byte change in the folder (install.go:262-264), and the folder holds everything: both skills' text, the MCP manifest, the icon and `hooks.json` (templates.go:34-117). Design.md:1419 records that "a manifest-only change invalidated it in live CLI checks". Known-limitations 263 admits the recall-skill upgrade did this: "its hooks capture nothing until then".

A Codex-only user's signals: one printed line at upgrade time. The hooks do not run, so they cannot say; the MCP instructions may not be shown (known-limitations 309).

Cost: every release that improves a skill or a matcher silently stops Codex memory until a manual step the product cannot make for the user.

Direction: rebuild the packaging so the trust-bearing bytes never change. A frozen hooks plugin: one generic command for every event, catch-all matchers, no skills, no MCP, no version. Routing, matchers and timeouts move into the binary. Skills and the MCP declaration go in a second plugin that changes freely. First establish exactly which bytes Codex hashes (open question 1).

Known: yes, in part (known-limitations 263; decisions.md:75, 108).

### ops-3. Upgrades never reach users, while the hosts update themselves underneath (high; invisibility; verified)

No release check exists outside `eva-md upgrade` (release/version.go:1-3). Auto-update is ruled out (decisions.md:117). Meanwhile eva-md's host integration is fitted to exact host versions (ops-9), and the fix for transcript drift is literally "update eva-md to a release that reads this … version's transcripts" (watchdog.go:101). The docs name four releases in the first week: 2026.11.2, 2026.11.2.2, 2026.11.5 and 2026.11.6 (docs/verification.md:77-78; this clone holds no tags to check them against).

The upgrade asks a question whose default is no (setup.go:477-490), re-runs setup's whole checklist, and without `--host` installs for every host found (known-limitations 274).

Cost: the installed base drifts behind the hosts; fixes for capture breakage wait for a person to notice and act.

Direction: add a background update channel. A scheduled tick reads a signed release manifest, stages the binary, verifies it offline (ops-10), swaps it when neither job lock is held, keeps the previous binary, and rolls back by itself if the freshness ledger regresses after the swap. Binary-only updates are silent. An update that would change trust-bearing bytes is held and asked for once. This reverses a recorded decision, so it needs the owner's yes.

Known: no (it is a decision, not a listed limitation).

### ops-4. All background work rides on hook events; there is no scheduler (high; architecture; verified)

`maintain` starts at the first captured hook event of a calendar day (hook/maintain.go:55-81). Push retries start only at a chat start (maintain.go:39-47, 83-105). Design.md:1360 rules out "daemons or sleeping jobs".

Consequences, all from the trigger logic:

- Pages the updates write after the day's first event are committed and pushed at the *next* day's first event. Friday afternoon's memory stays uncommitted on one disk until Monday's first chat.
- When hooks stop, maintenance, commit, push, health and any future update check all stop with them. The watchdog shares the failure it should watch.
- A chat in an excluded folder queues nothing (hook.go:339-360).

Direction: add one OS-scheduled tick (launchd `StartCalendarInterval`, a systemd user timer, Task Scheduler). It is a short-lived run, not a resident process. It owns maintenance, commit-after-quiescence, push retry, the freshness ledger, self-repair and the update check. Hooks only capture and enqueue.

Known: no.

### ops-5. The trust notice never expires and hides every other notice (high; invisibility; verified)

`needs-trust.json` is written whenever Codex's folder changed or Codex lacked the plugin (install.go:262-264, 392-403). Only uninstall removes it (uninstall.go:238). It is satisfied only by a Codex SessionStart heartbeat newer than the file (health.go:93-96).

Setup installs for every host CLI it finds by default (`--host` defaults to `all`, setup.go:149, 247-255; the answers template writes `EVA_MD_HOSTS=all`, internal/config/answers.go:57). So a person who has Codex installed but works in Claude Code sees "eva.md: review its hooks in Codex /hooks; run eva-md doctor to verify." at every Claude start, resume, clear and compact (resume.go:173-176), indefinitely. Because `QuickNotice` returns at the first problem (health.go:95), the checks after it never run: hook failures, drift, held updates, failing commits, diverged pushes (health.go:108-118).

The same first-wins rule lets an unknown config key (health.go:58-60) hide a diverged wiki.

Tests pin the behaviour as intended (health_test.go:149-163).

Direction: part of the health rebuild. Typed conditions with severity, a first-seen time, relevance (no alarm for a host with no activity), decay and acknowledgement. Show the most severe, not the first in code order.

Known: no.

### ops-6. Every remedy is a terminal command and a hand fix; the doctor is a 50-row dump (high; invisibility; verified)

Each notice ends "run eva-md doctor". The doctor prints every row flat (commands.go:327-335) with no verdict line. Rows that can never pass or are usually UNKNOWN: "claude managed hook policy" is always UNKNOWN (hosts.go:100); each of 17 per-event rows turns UNKNOWN after 24 idle hours (health.go:195-198) or stays UNKNOWN for rare events (health.go:180-182).

Repairs are split by accident: `maintain` rebuilds the knowledge files (cmd/eva-md/maintain.go:84-86) but only the doctor rebuilds a stale names index (`systempage.RefreshNames` has one caller, health.go:315, 326). Until someone runs the doctor, "prompts may miss pointers" (health.go:411).

The hand fixes are listed in 3.3. Several are mechanical and safe for a program: re-recording a CLI path, rebuilding an index, retrying, fixing a mode.

The same state is worded four times by four functions: `updateRow`, `updateState`, `backgroundNotice`, `liveFix` for updates; `commitRow`, `commitState`, `backgroundNotice`, `RetryPush` for commits (background.go:89-138, 281-364, 391-447; mcp.go:268-310).

Direction: evolve. One computed list of conditions, each with a remediation: automatic (run by the tick), agent-run (a repair skill or `eva-md repair`), or one plain ask to the person (log in, approve). The doctor's default output becomes a verdict and the open asks; the rows move behind `--verbose`.

Known: partly (known-limitations 207 for the hand trim; 328 for the doctor's wait).

### ops-7. macOS only, two hard-coded hosts, no desktop chat, no cloud sessions (high; reach; verified)

- Setup and both scripts refuse anything but macOS (setup.go:237-240; install.sh:35-37). Releases build darwin only (release.yml:109-114).
- A Windows build fails to compile: `syscall.Flock`, `O_NOFOLLOW` and others in `internal/paths/lock.go:23-38`, `internal/okfadapter/adapter.go:702, 878-906` (checked with `GOOS=windows go build`). 36 non-test files use unix-only syscalls.
- Linux already builds and passes the tests (AGENTS.md), and the folder layout is XDG-shaped (internal/paths/paths.go:43-47). Only policy and release assets keep it out.
- The host is not an abstraction: 116 lines in 38 non-test files branch on `"claude"`, `"codex"` or `"claude-code"`, 54 of them in install and health, with two naming schemes for one host (health/background.go:163-169; hosts.go:24-31).
- Design.md:1358 puts "Claude Desktop Chat tab, regular ChatGPT chat, Claude Code cloud sessions, and the Codex IDE extension" out of scope. The owner's stated goal names Claude Desktop.

Direction: evolve. A host adapter interface with declared capabilities (hooks, plugin registration, transcript store, headless run, MCP only) and a small platform layer (locks, detach, paths). Ship Linux builds first, since cloud and devcontainer sessions are Linux. An MCP-only adapter serves surfaces without hooks.

Known: yes for the OS (known-limitations 265, 289); no for the host coupling.

### ops-8. Nothing measures operations on real hosts or over time (high; evaluation; verified from docs and tests)

- Install, upgrade and uninstall are tested against fake hosts. Known-limitations 264: "checked with fake hosts only". Known-limitations 266: the install command ran from a real release "on one Mac only"; `eva-md upgrade` "has run only against a local stand-in".
- CI has no job with a real `claude` or `codex` CLI (.github/workflows/ci.yml:11-48).
- No test upgrades from the previous release's on-disk state.
- No measure of time to detect a silent stop or of false alarms, though the freshness check has documented false FAILs (known-limitations 322-324).
- With no telemetry (decisions.md:117) the maintainer cannot know whether installed copies are capturing.

Direction: add three things. A nightly canary on a macOS runner with the newest real host CLIs: install, a scripted chat, heartbeats, `doctor --json` green, upgrade from N-1, uninstall. A fault-injection suite that asserts, for each silent-stop scenario in 3.1, which signal the person gets and within how many chat starts. A local health history from which `status` shows each stage's uptime, exportable only by choice.

Known: partly (264, 266).

### ops-9. Host integration is fitted to exact host versions, with no version awareness (medium; architecture; verified)

- The missing-folder state is recognised by one Codex 0.166.0 sentence (host.go:47-73).
- Marketplace roots are found by guessing keys: `root`, `path`, `installLocation`, `marketplaceSource.source`, `source.path`, `source.source` (host.go:243-251).
- Claude's plugin must report `folderVersion == "1"` (host.go:220).
- `codex features list` is parsed by column position (install.go:184-189; hosts.go:82-88).
- Trust comes from an experimental app-server call and must be exactly 7 events (hosts.go:113, 219).
- No code reads a host CLI's version; versions appear only in comments (install.go:474; host.go:47; update/classify.go:11).

When a host rewords something, install, uninstall and the doctor stop (known-limitations 269), and only a new eva-md release fixes it, which ops-3 says will not arrive by itself.

Direction: evolve into the adapter of ops-7, with capability probing, the host version recorded in heartbeats and the doctor, and the wording tables shipped as signed data that can update without a new binary.

Known: partly (267, 269).

### ops-10. A download is checked only against a file from the same release; binaries are unsigned (medium now, high with auto-update; reliability-safety; verified)

`release.Verify` compares the binary with `SHA256SUMS` downloaded beside it (fetch.go:152-185; upgrade.go:131-143; install.sh:91-101). Build provenance is checked once, at publish time (releasing.md:32). Binaries are not Developer ID signed or notarized (known-limitations 289).

Direction: add an offline-verifiable signature over `SHA256SUMS`, made in the release workflow, checked by the binary with the standard library (`crypto/ed25519`, so no new dependency) and by the script. Sign and notarize for any distribution that passes through Gatekeeper.

Known: yes (271, 289).

### ops-11. The plugin is a per-machine generated marketplace installed from a terminal (medium; invisibility; verified)

Install renders a local marketplace per host and registers it through four to six CLI calls per host (install.go:356-391), because hooks must name the binary by absolute path (templates.go:91-93). The plugin version is frozen at "1" (templates/claude-plugin.json), so a host's own plugin screen cannot tell releases apart or update it. About 1,740 lines (install.go 820, host.go 351, uninstall.go 310, record.go 261) exist to make that transactional.

Direction: evolve. Keep the transactional core. Explore a host-native marketplace entry whose hook is a tiny launcher that finds or fetches the binary, so install can start inside the host (open question 3).

Known: no.

### ops-12. The host CLI's path is a snapshot taken at install (medium; reliability; verified)

Detached jobs prefer `<state>/cli-paths.json`, written only by install (install.go:424-448). When the CLI moves, updates fail `cli_missing` and the doctor's fix is "run eva-md install again to record the CLI's path" (background.go:115-120).

Direction: evolve. A hook runs inside the host, so each SessionStart can refresh the path at no cost. The fix becomes automatic.

Known: no.

---

## 5. Essential and accidental complexity

Essential: registering with two hosts without writing their settings; replacing a binary and a folder under live hooks with rollback; release provenance; bounded health reads inside a hook's time limit.

Accidental:

- The whole plugin folder as the unit of Codex trust (ops-2).
- Host quirks handled by wording and key guessing instead of an adapter with versions (ops-9).
- Hook-driven triggers standing in for a scheduler (ops-4), which then need attempt markers, retry markers and "first event of the day" logic (hook/maintain.go:55-105).
- Four formatters over the same health state, and 50 lines that only word index staleness (health.go:402-443).
- Upgrade implemented as "run all of setup again" (upgrade.go:156).

## 6. Overall call

Evolve, with two rebuilds and three additions.

- Keep: the transactional install core, uninstall's guarantees, the release pipeline, the bounded health readers.
- Rebuild: the health signalling model (ops-1, ops-5, ops-6) and the plugin packaging around frozen trust-bearing bytes (ops-2).
- Add: a scheduled tick (ops-4), a signed background update channel (ops-3, ops-10), a real-host canary and fault-injection suite (ops-8).
- Evolve: hosts and platforms into adapters (ops-7, ops-9, ops-11, ops-12).

Three of these reverse recorded decisions: no auto-update, no scheduled jobs, macOS and two hosts only. The owner's goal is the strong reason AGENTS.md asks for, but each needs an explicit yes.

## 7. Open questions

1. Which bytes does Codex's hook trust cover: `hooks.json`, the plugin manifest, or the whole folder? Design.md:1419 says more than the command. The answer decides whether a frozen hooks plugin makes upgrades approval-free.
2. Will the owner reopen "no auto-update", "no daemons or sleeping jobs", "Claude Desktop Chat tab out" and "macOS only"?
3. Can a host-native marketplace carry eva-md, given the absolute-path hook? Does Codex expand a plugin-root variable in hook commands as Claude Code does?
4. Which out-of-band alert channel is acceptable: a macOS notification, a status line, the other host?
5. Where does a release signing key live, and will the project take an Apple Developer ID for notarization?
6. Can a nightly canary have host accounts and model quota?
7. Does Codex's own startup warning for untrusted hooks (design.md:1419) appear in the desktop app? Unverified there.

---

## Verification

A second reader tried to refute each finding against the code. Read-only: no test, install, update or other restricted command was run. The one command run beyond reads was `GOOS=windows go build -o /dev/null ./cmd/eva-md`, which fails as ops-7 says.

Result: 7 confirmed, 5 partly, 0 refuted. No cited line failed to show what was claimed; the "partly" verdicts are about scope and severity.

| id | verdict | their severity | mine |
|---|---|---|---|
| ops-1 | confirmed | critical | critical |
| ops-2 | partly | critical | high |
| ops-3 | confirmed | high | high |
| ops-4 | partly | high | medium |
| ops-5 | confirmed | high | high |
| ops-6 | partly | high | medium |
| ops-7 | confirmed | high | high |
| ops-8 | confirmed | high | high |
| ops-9 | confirmed | medium | medium |
| ops-10 | confirmed | medium | medium |
| ops-11 | partly | medium | low |
| ops-12 | partly | medium | low |

### ops-1: confirmed, critical

- `health.go:98-107` checks freshness for the other host only. The doctor and `status` check both (`health.go:201`), but only from a terminal.
- `mcp/instructions.go:168-178`: the server warns when no SessionStart ever ran or none for 7 days. Under 7 days it calls `QuickNotice(ctx, s.Dirs, c)` with no host, so the freshness comparison at `health.go:98` never runs for the server's own host. The server is the one process that starts with a chat whether or not hooks fire, and it does not use that.
- `update.Recent` reads yesterday's and today's log only (`update/runlog.go:227-237`); a hold is per day (`runlog.go:341-349`).
- `status.go:116-128` is PASS at any backlog unless a watermark is damaged, and `Inspect` never calls it. `commitRow` (`background.go:281-308`) has no age check.
- Added evidence: an update job that fails before its first record (config invalid, `cmd/eva-md/update.go:103-110`; wiki or run log unreadable, `update/job.go:411-419`) writes only to the detached job's log. The run log, which every notice reads, stays empty.
- No backfill: `catchup.RecoverProject` "never scans the host's transcript stores" (`catchup/catchup.go:1077-1078`).

### ops-2: partly, high

- Confirmed in code: `install.go:262-264` flags trust on any byte change; `templates.go:34-117` renders skills, MCP manifest and hooks into one folder.
- Stronger than they said: `git log -- internal/install/templates.go internal/install/templates internal/install/mcp.go` shows 5 of the 6 commits that touched the templates changed plugin bytes, two of them skill text only. Nearly every release sets the flag.
- Not established by code: that Codex drops trust for a skill-only change. The sources are docs (design.md:1419, a manifest-only change) and the message of commit 9e4f609. The finding's own open question 1 says so; the title states it as fact.
- Severity: today the lapse happens only as the result of a person's own upgrade, and setup prints the `/hooks` step at that moment (`setup.go:566-569`). Claude chats then carry the notice, and design.md:1419 lists Codex's own startup warning. It turns critical only if upgrades run in the background (ops-3).

### ops-3: confirmed, high

- `release/version.go:1-3`; `upgrade.go:69` is the only caller of `release.Latest`; decisions.md:117 and design.md:1360 rule out auto-update.
- The y/N question (`setup.go:476-481`) is asked only with a terminal (`env.interactive && !*yes`); from a shell with no terminal the upgrade goes ahead unasked.
- There is no passive "a newer release exists" line either: the upgrade is "the only network call eva-md makes itself" (`upgrade.go:45`).

### ops-4: partly, medium

- Triggers confirmed: `hook.go:360` (after every guard), `hook/maintain.go:55-81`, push retry at chat starts only (`maintain.go:39-47`), design.md:616, 815, 1360.
- Impact overstated. Uncommitted pages are no more exposed than the inbox, which never leaves the Mac by design. A day's commit lag loses nothing, and a wiki is one Mac's (`commitFix`, "another machine's wiki is never committed").
- The real cost is the second half: nothing independent of hooks evaluates health, and the day's marker is made before the spawn (`maintain.go:70-81`; `spawn.go:34` ignores the error), so a failed spawn or an early exit skips the day with no record.
- For the design: `eva-md mcp` already runs once per open chat, independent of hooks. A launchd agent is a new thing to install, uninstall and have macOS announce as a background item.

### ops-5: confirmed, high

- Early return at `health.go:74-97`; flag rewritten by each such install (`install.go:392-403`); removed only by uninstall (`uninstall.go:238`); run at every SessionStart source (`resume.go:174-175`); `--host` defaults to `all` (`setup.go:149`); pinned by `health_test.go:148-163`.
- Added: only a captured Codex SessionStart writes the heartbeat that satisfies the flag. The uncaptured paths (`hook.go:308-340`) write a stamp, not a heartbeat, so a person whose Codex chats are all in excluded folders keeps the notice in Claude chats too.

### ops-6: partly, medium

- Confirmed: flat rows and an exit code, no verdict (`commands.go:327-339`); `hosts.go:100` always UNKNOWN; 10 + 7 event rows go UNKNOWN after 24 h (`health.go:195-198`); the fixes cited.
- Correction: setup's closing health check already prints "N passed, M failed, K unknown" and only the FAIL rows (`setup.go:586-602`). The verdict form exists; the doctor does not use it.
- Correction: the names index is written by every system page write (`systempage/write.go:328-334`). The doctor-only repair covers a hand edit, a pull or a failed write, not normal use.
- "A dozen manual commands a month" is the union of every failure path, not an expected month. The routine hand steps are the upgrade, Codex's `/hooks` after it and the MCP tool permission.

### ops-7: confirmed, high

- `setup.go:236-239`; `install.sh:35-37`; `release.yml:107-114`; the Windows build fails at `paths/lock.go:23-38`; design.md:1359.
- A narrower grep (`== "claude"`, `case "codex"`, `update.HostClaude` and the like) finds 64 lines in 29 non-test files. Their 116 in 38 used a wider pattern; the point holds.
- Correction: the desktop Code tab is in scope by design (design.md:39, 1133, 1330), checked by hand only. Only the Chat tab is out, and it has no hooks.
- Correction to the direction: cloud sessions are skipped by an explicit guard (`hook.go:284`, `CLAUDE_CODE_REMOTE`) and have no lasting disk. A Linux build alone gives them no memory; the inbox and wiki would have to arrive and leave with the session.

### ops-8: confirmed, high

- `ci.yml`: both jobs run `go test` with fake hosts. known-limitations 264, 266; verification.md:154-155.
- `upgrade_test.go:130-160` upgrades between two stand-ins built from the same source, so no earlier release's on-disk state or plugin layout is exercised. See miss-1 for what that hides.

### ops-9: confirmed, medium

`host.go:47-73`, `243-251` (six keys), `220`; `install.go:184-189`; `hosts.go:82-88`, `113`, `219`. No non-test code runs a host's `--version`.

### ops-10: confirmed, medium

`fetch.go:152-185`; `upgrade.go:131-143`; `install.sh:89-101`; known-limitations 271, 289.

### ops-11: partly, low

- Facts confirmed: `install.go:356-391`; `templates.go:91-96`; `claude-plugin.json` version "1"; 1,742 lines.
- "Install cannot start inside the host" is too strong. Without a terminal setup asks nothing and takes `--dir` and `--remote` or `config.env` (`setup.go:476`; known-limitations 278), so an agent's shell can run it. The host's plugin screen cannot.
- The absolute path and the frozen version are deliberate (design.md:553; `templates.go:11-12`). A launcher hook that fetches a binary breaks the rule that hooks make no network calls.

### ops-12: partly, low

- `install.go:424-448` is the only writer, as claimed.
- A recorded path that is gone falls back to a PATH lookup (`update/process.go:86-94`), and PATH is on the job's allowlist (`process.go:45`). Updates fail only when the spawning host's PATH also lacks the CLI.
- It is not silent: `cli_missing` counts toward the 3-in-a-row notice (`background.go:423`).
- It is listed: known-limitations 206. `known` should be true.
- The direction buys little: a hook can only record what its own PATH finds, which is the lookup the fallback already makes. It helps only a later job spawned from an app whose PATH is shorter.

### Missed

**miss-1. Install refuses eva-md's own plugin files from another release's layout (high; architecture; verified).** `treeChanged` returns an error for any file or folder in the managed marketplace that the running binary's `render` does not produce (`install.go:672-691`), and `runInstall` stops on it (`install.go:244-247`); uninstall runs the same check (`uninstall.go:106-112`). A release that removes or renames a plugin file cannot install over the previous one, and a rollback past a release that added one (the recall skill, commit 9e4f609) is refused with "unexpected directory ... move it out of that folder". No test covers it. This blocks the migration ops-2 proposes (splitting the plugin) and the rollback ops-3 proposes. Fix direction: install writes a manifest of the files it rendered beside the ownership marker, so any release can tell eva-md's earlier files from foreign ones and replace them.

**miss-2. Hook failures are pooled across a host's events, so one event that fails every time is never flagged (medium; reliability-safety; verified).** `hookRuns` merges every event's history and judges the newest 20 (`watchdog.go:44-65`); `hooksFailing` needs half of them (`watchdog.go:38`). The per-event doctor row is PASS on any heartbeat under 24 h old and never reads its `Failed` (`health.go:179-199`), though a timed-out run writes one (`resume.go:338-351`). A SessionStart that always runs out of time (no brief, ever) or a Stop that always does, among async PostToolUse runs, reads "2 of the last 20 runs failed", PASS, with no notice. Capture freshness also counts a failed run's heartbeat as fresh (`watchdog.go:417-432`).

**miss-3. Every notice and fix names a bare `eva-md`, which install puts on PATH only sometimes (medium; invisibility; verified).** The link is made only when `~/.local/bin` already exists and is on PATH (`record.go:202-230`). `install.Command`, which gives the full path otherwise, is used by upgrade and uninstall only (`upgrade.go:113`; `cmd/eva-md/uninstall.go:130`); 28 strings in `internal/health` (21), `internal/mcp` (6) and `internal/hook` (1) say "run eva-md doctor", and the doctor's fixes name `eva-md install`, `eva-md commit` and `eva-md update` the same way. The brief and pointers do use the absolute path (`resume/lines.go:303`, `hook/pointers.go:222`), because "a chat's PATH may not reach eva-md" (design.md:553). On such a Mac the one remedy every notice names is "command not found", and the doctor has no row for the link.
