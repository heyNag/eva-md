# First-run experience: install command, guided setup, init, config, project identity

Audit of `the repository` at the current checkout. Read in full: `cmd/eva-md/setup.go`, `setup_repo.go`, `setup_refusals.go`, `setup_checks.go`, `internal/config/*.go`, `internal/identity/*.go`, `scripts/install.sh`, `internal/wikiinit/init.go` and `git.go`; read in part: `internal/install/install.go` and `host.go`, `cmd/eva-md/main.go`, `commands.go`, `upgrade.go`, `internal/hook/hook.go` and `maintain.go`, `internal/update/wiki.go`, `internal/wikigit/git.go` and `commit.go`, `internal/catchup/catchup.go`, the tests' names and one end-to-end test, `README.md` 1-205, `docs/design.md` 641-669, `docs/known-limitations.md` 18-34 and 237-286, `docs/decisions.md` 1-60, `docs/verification.md` 79, 153, 155.

Every claim about the code carries file:line. "Verified" means I read the line; "inferred" means it follows from what I read but I did not run it. Nothing was run but read-only shell commands.

---

## 1. How it works

### 1.1 The install command (`scripts/install.sh`, 140 lines)

`curl -fsSL .../releases/latest/download/install.sh | bash` (install.sh:4).

1. `check_mac`: refuses root, refuses any system but Darwin, needs `curl` and `shasum` (install.sh:31-44).
2. `mac_arch`: arm64, or amd64; a Rosetta shell on Apple silicon gets arm64 (install.sh:48-60).
3. `release_tag`: `EVA_MD_VERSION` when set, else the tag `releases/latest` redirects to, matched against the tag regex (install.sh:64-78). Curl limits: 20 s connect, 60 s total.
4. `download`: the binary `eva-md-darwin-<arch>` and `SHA256SUMS` into `mktemp -d ${TMPDIR:-/tmp}/eva-md-install.XXXXXX`, removed by an EXIT trap; HTTPS only, 20 s connect, 600 s per file; checks the sum, then runs `eva-md version` and requires `eva-md <tag>` (install.sh:82-110).
5. `run_setup`: runs `<tmp>/eva-md setup "$@"` reading `/dev/tty` when there is one, `/dev/null` otherwise (install.sh:114-125). On a non-zero exit it prints "Setup stopped. When it named a problem, fix it; then run the install command again."
6. Everything is in functions run from the last line, so a truncated download runs nothing (install.sh:14-15, 140).

The script installs nothing itself. The stable binary, the plugins and the PATH link are all made by `setup` -> `install.Run`.

### 1.2 Guided setup (`cmd/eva-md/setup.go`, `runSetup` at 144-626)

Inputs: flags `--remote --dir --host --git-host --no-push --yes` (setup.go:145-152); the answers file `~/.config/eva-md/config.env`, whose values fill in what no flag gave (setup.go:177-193); the existing `config.toml` (setup.go:167-172). `existing := c.WikiPath != ""` decides between the two flows (setup.go:172).

**Step A, "Checking this Mac"** (setup.go:234-334), each check stopping setup with a `Fix:` line (setup_checks.go:52-56):

| # | Check | Code | Limit |
|---|---|---|---|
| 1 | macOS | setup.go:237-241 | - |
| 2 | `git` on PATH | setup.go:242-246 | - |
| 3 | `claude` and/or `codex` on PATH | setup.go:247-272 | - |
| 4 | answers file read without error; unknown keys named on stderr | setup.go:273-283 | file <= 16 KB (answers.go:29) |
| 5 | answers template written on a new Mac when missing | setup.go:284-294 | - |
| 6 | no terminal and no folder+remote -> exit 2 | setup.go:295-305 | - |
| 7 | new Mac only: the CLI for the wiki's host (`gh`/`glab auth status --hostname`), or an information list of both | setup.go:310-319; setup_checks.go:86-120, 173-189 | 10 s per CLI call (setup_repo.go:37) |
| 8 | commit signing, information only | setup.go:320-324; 1304-1324 | 5 s (setup.go:954) |
| 9 | install's own checks as a dry run (`Preflight`) | setup.go:327-334; setup_checks.go:131-169 | 2 min (commands.go:33) |

**Step B, a new Mac** (setup.go:350-453):

- Question 1, the folder: no default; three tries (`askUntil`, setup.go:1137-1157); checked by `wikiinit.CheckFolder` and, in a terminal, by `folderWiki`/`otherRepo`/`dirty` (setup.go:651-703).
- Question 2, the remote (setup.go:421-452). `repos.offer` (setup_repo.go:306-403): with `gh` or `glab` signed in it offers "Create a private repo named eva-wiki with gh on github.com? [Y/n]" (setup_repo.go:357); with both, "Which one? [GH/glab/n]" first (setup_repo.go:334). `use` views the name, creates it private, falls back to an internal repo on consent, reads back the address (setup_repo.go:414-521). Otherwise the user pastes an address, which goes through `checkAnswer` -> `checkPrivate` (setup_repo.go:541-590) and `checkRemote` -> `peekRemote`, a shallow no-checkout clone in a temporary folder that reads only `.eva-md/schema.toml` (setup.go:708-729, 1073-1115).
- The plan is one of `planCreate`, `planInit`, `planAdoptRemote`, `planAdoptFolder` (setup.go:47-56).

**Step B', a Mac already set up** (`configuredPlan`, setup.go:764-837): keeps the wiki (`planPlugins`), or with a terminal offers to clone a missing or empty wiki folder again (`planReclone`) after `sameWiki` has read the repo (setup.go:842-858). `--dir`/`--remote` that would change the wiki are refused (setup.go:765-786).

**Step C, summary and confirmation** (setup.go:458-491): "Set up eva.md with these choices? [y/N]", or "Install or update the plugins? [y/N]" on a set-up Mac. Default is no. Skipped with `--yes` or without a terminal.

**Step D, the wiki** (setup.go:493-550): `wikiinit.Run` with a `BeforePush` callback that shows the first commit's author and whether it is signed and, in a terminal, asks "Push it to <remote>? [Y/n]" even with `--yes` (setup.go:515-523).

**Step E, the plugins** (setup.go:552-583): `install.Run` within 2 minutes; `setAside` (commands.go:278-292); the Codex `/hooks` notice when `NeedsTrust` (setup.go:566-569); the Codex switch copied from `config.env` into `config.toml` (setup.go:570-575); the answers file written for a Mac that had none (setup.go:579-583, 900-922).

**Step F, health and closing lines** (setup.go:585-625): `health.Inspect` within 30 s, a "N passed, N failed, N unknown" line, then four or five lines of instructions ending with the `doctor --live` advice.

### 1.3 Init (`internal/wikiinit/init.go`, `Run` at 46-266)

1. Folder rules (`allowedLocation`, init.go:443-482): not the home folder, not Documents/Desktop/Downloads, not iCloud Drive or `~/Library/CloudStorage`, not eva-md's own config/state/share folders, also through symlinks and letter case.
2. `init.lock` (init.go:67-71, 532-541); refuses a config that names another wiki (init.go:72-78).
3. `git clone --no-local -- <remote> <folder>` into a missing or empty folder, then `chmod 0700` (init.go:79-98). Each git call has 120 s (wikiinit/git.go:12).
4. Requires a standalone clone at the repository root whose origin and every push URL name the requested remote (init.go:99-134); refuses a symlinked `.eva-md` (init.go:135-142).
5. New wiki: instance = `<machine label>-<8 hex>` (init.go:164-172), scaffold through the okf adapter (init.go:268-309), okf validation with no findings (init.go:185-197), `git add` of the scaffold paths only, refusing any other staged path, and `git commit -m "Initialize eva.md wiki"` with the user's signing (init.go:215-233).
   Adopted wiki: schema version 1 and a matching instance (init.go:177-184); a leftover okf lock is removed and any uncommitted change refuses it (init.go:198-213).
6. Push with upstream unless `NoPush` or `BeforePush` says no (init.go:235-255).
7. `config.Save` of `wiki_path`, `expected_remote`, `instance`, `machine_label` and the defaults (init.go:256-264).

### 1.4 What is written where

| What | Where | By |
|---|---|---|
| answers template or filled-in answers, 0600, never over a file | `~/.config/eva-md/config.env` | setup.go:291, 579; answers.go:343-363 |
| config, 0600, atomic | `~/.config/eva-md/config.toml` | init.go:262; config.go:121-133 |
| wiki clone, 0700, scaffold, first commit | the chosen folder | init.go:90-95, 268-309 |
| a private (or internal) repo `eva-wiki` | the git host, through `gh`/`glab repo create` | setup_repo.go:788-801 |
| stable binary, plugin marketplaces, install marker, install record | `~/.local/share/eva-md/` | install.go:320, 345, 351, 418 |
| link on PATH | `~/.local/bin/eva-md` when that folder is on PATH | install.go:269-273, 407-412 |
| `cli-paths.json`, `needs-trust.json`, `locks/init.lock`, `cache/identity/*.json` | `~/.local/state/eva-md/` | install.go:399, 427-448; cache.go:629-653 |
| temporary folders, removed | `$TMPDIR/eva-md-install.*`, `eva-md-setup-*`, `eva-md-probe-*` | install.sh:89-90; setup.go:1074-1078; setup_repo.go:850-854 |
| plugin registrations | each host's own store, through `claude plugin ...` and `codex plugin ...` | install.go:358-378 |

### 1.5 Config (`internal/config`)

- `config.toml` (config.go:50-79): the machine binding (`wiki_path`, `expected_remote`, `instance`, `machine_label`) and the preferences (`exclude`, `notice`, `updates`, `commits`, `mcp`, `recall`, `daily_cap` 40, `effort`, `codex_ignore_user_config`, `reminder_every` 5) in one file. Defaults at config.go:82-84; absent keys keep defaults, an explicit 0 stays 0 (config.go:100-104). Read strictly: `O_NOFOLLOW|O_NONBLOCK`, regular file, <= 1 MiB (config.go:312-338). Unknown keys are reported, never saved (config.go:76-78, 108-110).
- `Excluded` (config.go:201-243): path globs and `remote:` globs, matched against the target and every parent (`matchUp`, config.go:341-352), the lexical and the symlink-resolved path (config.go:229-230, 259-278), case-folded on macOS (config.go:44).
- `config.env` (answers.go): six `EVA_MD_*` keys (answers.go:19-26), literal values, strict read, an unknown key named only when it looks like one of setup's own (answers.go:80-90, 255-263).
- The hook loads the config on every event; a missing file is `Default()` and capture goes on, any other error turns capture and briefs off and a chat start says so in one line (main.go:132-148; hook.go:191-196).

### 1.6 Project identity (`internal/identity`)

- `Resolve` (identity.go:82-155): up to four read-only git calls sharing one 800 ms limit (identity.go:44, 86): `rev-parse --show-toplevel`, `rev-parse --symbolic-full-name HEAD` (plus `--verify --quiet HEAD` for an unborn branch) and `config --get remote.origin.url`.
- Three kinds (identity.go:29-34): `repo` (key from the normalized origin, `_`->`_u`, `/`->`__`, key.go:13-22), `local` (key `local-<folder>-<hash6 of the absolute root>`, key.go:28-35), `general` (one fixed key for every folder outside a repository, identity.go:73-75, 91-108).
- Fail closed: a git failure other than "not a repository" returns an error so a remote exclusion can't be bypassed (identity.go:80-81, 98-105, 126-137); the hook then drops the event (hook.go:320-325).
- Hardened git: fsmonitor off, hooks path `/dev/null`, no optional locks, no lazy fetch, a scrubbed environment, `LC_ALL=C`, output caps of 64 KiB and 4 KiB (identity.go:267-298; gitenv.go:21-68).
- The cache (cache.go, 653 lines, with gitconfig.go, 276 lines): one entry per worktree, used only while a fingerprint of everything git reads to answer `Resolve` is unchanged: the top level, git dir and common dir by device, inode, mode and owner; `.git` file, `HEAD`, `commondir` by content; every config file git reads and every file they include, parsed as git's `config.c` parses them; `packed-refs`; the branch's loose ref (cache.go:236-373; gitconfig.go:84-124). A cache hit starts no git process (cache.go:94-96). The answer is stored only when nothing changed while git ran and it agrees with what `HEAD` predicts (cache.go:97-113).

### 1.7 Triggers

- Setup runs when a person runs the install command, `eva-md setup`, or `eva-md upgrade`, which downloads the new binary and runs its `setup` with no flags (upgrade.go:156).
- Nothing runs setup, init, install or upgrade by itself. There is no self-update.

---

## 2. The walk-through: install command to the first chat that remembers

Happy path: a Mac with Claude Code and Codex CLIs, `gh` signed in to github.com, an SSH agent or credential helper that asks for approval, commit signing on, in a terminal.

### Prerequisites (before the one command)

| # | Prerequisite | Evidence | Built-in product would need it? |
|---|---|---|---|
| P1 | macOS | install.sh:35-37; setup.go:237 | No: only five `runtime.GOOS` sites in the non-test code (config.go:44; init.go:500, 522; hook.go:633; setup.go:99), plus the darwin-only asset name (release/fetch.go:44) |
| P2 | A terminal | install.sh:116-120; setup.go:295-305 | No |
| P3 | `git` (Xcode command line tools) | setup.go:242-245 | Only for sync |
| P4 | `claude` or `codex` on PATH | setup.go:248-267; install/host.go:171, 308-309 | No: a desktop-app-only user has no such command |
| P5 | `gh` or `glab` installed and signed in, or a private repo created by hand in a browser | setup_checks.go:86-120; setup_refusals.go:259 | No |
| P6 | git able to sign in to that host (SSH key on the account, or a credential helper for HTTPS) | known-limitations.md:240; setup_repo.go:523-537 | No |
| P7 | Codex's hooks feature on | install.go:179-190 | Host constraint |

### Questions

| # | Question | Default | Evidence |
|---|---|---|---|
| Q1 | "Where should the wiki live on this Mac?" | none; a path must be typed, and five locations are refused | setup.go:379-387; init.go:458-467 |
| Q2 | "Create a private repo named eva-wiki with gh on github.com? [Y/n]" (with both CLIs signed in, "Which one? [GH/glab/n]" first) | yes | setup_repo.go:334, 357 |
| Q3 | "Set up eva.md with these choices? [y/N]" | **no** | setup.go:478-490 |
| Q4 | "Push it to <remote>? [Y/n]", asked with `--yes` too | yes | setup.go:515-523 |

Further questions on other paths: "Is it GitHub or GitLab? [gh/glab]" (setup_repo.go:279); "Create ... as an internal repo instead? [y/N]" (setup_repo.go:496); "... already has content: use it as your wiki? [Y/n]" (setup_repo.go:439); "This wiki was set up on X ... Use it? [y/N]" (setup.go:756); "git read this repo over HTTPS without signing in ... Use it anyway? [y/N]" (setup_repo.go:829); "Clone it again ...?" (setup.go:808-812).

Note the order: the repo is created at Q2 (setup.go:425 -> setup_repo.go:460) before the confirmation at Q3 (setup.go:478). A no at Q3 leaves the new repo behind ("The new repo stays, empty", setup.go:484-486).

### Approvals outside setup's own questions

| # | Approval | When | Evidence |
|---|---|---|---|
| A1 | SSH agent or password manager, for the clone | Step D | setup.go:505-508 |
| A2 | Signing the first commit | Step D | setup.go:505-506; init.go:231 |
| A3 | SSH agent, for the push | Step D | init.go:251 |
| A4 | One more for the peek when an address is pasted | Step B | setup.go:709; known-limitations.md:253 |
| A5 | Codex: open Codex, type `/hooks`, approve eva-md | after setup, and again after any upgrade that changes the plugin folder | setup.go:566-569; install.go:262-264; README.md:144 |
| A6 | Claude Code may ask before each MCP tool's first use (three tools) | first uses | README.md:204 |
| A7 | One signing approval a day for the daily commit, and the push | every day after | decisions.md:54-57 (decision 7) |

### Waits

Download of the binary; the checklist (two to four `gh`/`glab` calls at up to 10 s each, a host inventory for each host within 2 min); clone, commit and push (each git call up to 120 s); the plugin install (within 2 min; each host is inventoried again before the lock, under the lock and after registration, install.go:153, 285, 379); the health check (within 30 s; it asks the hosts once more, setup.go:128-130). The host inventory is therefore taken five times per host in one setup. The total wall time is not measured anywhere (see finding setup-11).

### Notices and noise

The intro paragraph (setup.go:227-232), "Wrote <file>, a template for setup's answers" and "Fill in <file> and run setup again, or answer here." (setup.go:291-292, 356-359), the commit-signing line, the summary, "Your git setup may ask you to approve..." (setup.go:505-508), the plugin action lines (setup.go:557-559), the "N passed, N failed, N unknown" line (setup.go:594), and four or five closing lines (setup.go:611-625). About 45 to 50 lines in all (inferred from the print statements).

### After setup

1. Approve Codex's hooks (A5). Until then Codex captures nothing (known-limitations.md:263).
2. Start a new chat: chats already open are not picked up (setup.go:567-568).
3. Optional, advised by the last line: `eva-md doctor --live`, one model call per host (setup.go:624).
4. The first chat after setup is captured but has nothing to be told: recovery "never discovers old chats or imports anything preceding the first hook" (catchup.go:1-2). **The first chat that remembers is the second chat in the same project.** Wiki pages exist only after the first background update has run after a chat.

### Count

- 7 prerequisites, 1 command, 4 questions (one a typed path with no default, one a typed `y`), up to 4 git approvals during setup, 1 Codex approval in another app, up to 3 MCP permission prompts later, 1 or 2 approvals every day, 2 manual follow-ups.
- Without `gh`/`glab`: Q2 becomes "leave the terminal, create an empty private repo in a browser, copy its address, paste it", with one more network read and one more approval.
- Without a terminal: the first run exits 2 after writing the template; the user edits `config.env` and runs the command again (setup.go:295-305; release_test.go:512-531).

### What a product that feels built in would not have

All of Q1 to Q4; P2, P4 (as a PATH requirement), P5 and P6; A1 to A4 at install time; the answers-file notice; the `doctor --live` advice; the "unknown" count; the daily approval; manual upgrades. What remains is what the hosts impose: installing a plugin, Codex's one-time hook trust, and that only new chats load hooks.

---

## 3. What must be kept

Invariants and hard-won fixes that a redesign must carry over, each with where it lives.

**Privacy and safety of setup**

1. Nothing `gh`, `glab` or `git` prints is ever shown, logged or kept: stderr goes to the null device or to a buffer read only to sort a failure into a reason (setup_repo.go:890-926; setup_refusals.go:15-18, 121-127; wikigit/git.go:92-109).
2. No token from the environment reaches `gh` or `glab`, and each call points the CLI at one host (setup_repo.go:936-958).
3. A public repo is always refused; an internal one is used only on an explicit yes whose default is no (setup_repo.go:418-430, 880-888; setup_refusals.go:397-415).
4. The no-sign-in probe truly cannot sign in: no credential helper, no `.netrc`, no Kerberos, no proxy, no prompt (setup_repo.go:864-878).
5. Look before touching: a chosen repo is read from a shallow copy in a temporary folder, and a slow read is never taken for an empty repo (setup.go:1073-1126).
6. A missing wiki folder is never re-cloned from an empty repo or from another wiki (setup.go:842-858; init.go:153-156).
7. Install's checks run before any wiki is made or pushed (setup_checks.go:122-169); a failed install rolls itself back (install.go:323-342).
8. The first commit's author and signing are shown before the first push; nothing is force-pushed; a failed signature or push is never retried (setup.go:515-523, 1259-1296; init.go:45).
9. Setup makes no model call (setup.go:623).

**Init**

10. Only a standalone clone at its repository root, whose origin and every push URL name the requested remote, ever receives the scaffold (init.go:99-134).
11. Only scaffold paths are staged for the first commit; any other staged path refuses it (init.go:218-230).
12. Folder rules: not home, not the macOS-protected folders (background jobs can't write there), not cloud-synced folders (they sync into `.git`), not eva-md's own folders, checked through symlinks and letter case (init.go:443-482). The wiki folder is 0700 (init.go:93).
13. A rerun after a failed first commit resumes; `.DS_Store` is tolerated; a folder typed in another letter case is the same folder (init.go:84-88, 113-115, 375-379, 509-519).
14. A leftover okf lock file is removed before a wiki is judged dirty (setup.go:1054-1065; init.go:198-206).
15. Two inits are kept apart by a lock (init.go:532-541).

**Config**

16. Strict reads: never through a link, never a FIFO, size-capped, values literal and never echoed in an error; a line that may hold a pasted secret is named by number only (answers.go:80-90, 169-191, 215-219; config.go:312-338).
17. The answers file is written 0600 with `O_EXCL`, never over anything (answers.go:343-363).
18. Exclusions match the lexical and the resolved path, every parent, and are case-folded on macOS; remote patterns are normalized as remotes are (config.go:201-278). In the hook, path exclusion comes before identity and remote exclusion before any marker is written (hook.go:305-329).
19. An unreadable config stops capture rather than capturing an excluded project (main.go:132-148). The fail-closed intent is right; finding setup-9 is about how much it stops.

**Identity**

20. A git failure never falls back to the general area (identity.go:80-81, 98-105, 126-137).
21. Read-only git is hardened and bounded (identity.go:267-298; gitenv.go:21-68).
22. Key escaping keeps distinct remotes distinct (`_u`, `__`, key.go:13-16; identity_test.go:441); a key that looks like a token is hashed so the inbox doesn't refuse the project (key.go:17-20; identity.go:157-168); the cache re-derives keys under the current redaction rules (cache.go:89-96; key.go:38-51).
23. The cache is used only on an unchanged fingerprint, stores only an answer that agrees with a second look, resolves unusual layouts live, and is checked against real git by a differential test (cache.go:26-33, 97-113, 236-250; cache_test.go:762).
24. The full ref name is read, since `--abbrev-ref` answers `heads/<name>` when a tag shares the branch's name (identity.go:115-117).

**Install script**

25. Functions run from the last line; checksum and reported version checked before anything runs; root refused; HTTPS only; Rosetta gets arm64 (install.sh:14-15, 31-37, 48-60, 82, 93-109).

**Process**

26. Every time limit is a variable tests raise once, so no test depends on machine speed (setup.go:951-955; setup_repo.go:36-44; identity.go:44-48).
27. Only eva's name shows in output.

---

## 4. Where it falls short

Ranked by consequence for the goal.

### setup-1 (critical, invisibility): the first run is gated on a hosted git repository

**What the code shows.** Capture and the brief need no wiki: a missing config is `Default()` and the hook goes on (main.go:132-135); the hooks even run maintain for the search index with no wiki (hook/maintain.go:27-33), and the benchmark calls that state "a capture-only install" (benchmark.py:146). But `install.Run` refuses without a config, "no wiki is set up; run eva-md setup first" (install.go:139-146), setup has no plan without a wiki (setup.go:47-56), a run without a terminal needs both the folder and the remote (setup.go:295-305), and the wiki is made and pushed before the plugins are installed (setup.go:524 before 555). `init` requires an origin (init.go:116-123).

**Cost.** Four questions, up to four approvals, a `gh`/`glab` or browser prerequisite and a signed, pushed commit stand between the user and a hook that would work without any of them. Every failure of the git host, the SSH key, the signer or the network is a failure to install memory at all.

**Direction.** Local-first. The install registers the plugins and starts capturing with no question. The wiki is created locally in a default place under eva-md's own data folder, as a plain git repository with no remote. Off-machine sync becomes a separate, optional, deferred capability that eva-md offers once, in the agent's own surface, and attaches in the background. The remote requirement sits in few places: init.go:116-134, wikigit/commit.go:256, health/health.go:260 and setup. **Action: rebuild** (the setup flow), evolve init. **Basis: verified in code.** Not in known-limitations.

### setup-2 (high, memory-quality): day one is empty

**What the code shows.** Recovery "never discovers old chats or imports anything preceding the first hook" (catchup.go:1-2). Chats captured before updates first ran are set aside and never summarized (commands.go:273-292; known-limitations.md:188). The hosts' own memory files are left alone (decisions.md:58, decision 8). So the first chat after install gets no brief content, and the first chat that remembers is the second in the same project.

**Cost.** The user's months of transcripts already on disk in the hosts' folders contribute nothing. The product's first impression is "nothing happened".

**Direction.** A consented, bounded bootstrap: on install, read the hosts' existing transcripts for the projects the user works in, redact them through the same pipeline, and build thread pages and a first brief from them, newest first, within a stated budget of model calls, with a clear off switch. **Action: add.** **Basis: verified in code.** The import gap itself is not in known-limitations.

### setup-3 (high, recall-quality): project identity is the origin URL or the folder path, with no continuity

**What the code shows.**
- Every folder outside a git repository is one project, `general`, with one thread page (identity.go:73-75, 91-108; update/wiki.go:77-80).
- A repository with a remote is keyed by `remote.origin.url` as configured (identity.go:139-150; key.go:13-22). A renamed or moved repository, a changed host, or a clone through a URL alias gets a new key, a new inbox folder and, because the wiki folder is proved by the remote its now page names (update/wiki.go:104, 129-134), a new wiki folder.
- A repository without a remote is keyed by a hash of its absolute path (key.go:28-35): moving or re-cloning the folder starts a new project, and adding a remote later changes its kind and key.
- No alias or migration exists anywhere (a search of `internal/identity`, `internal/inbox`, `internal/update/wiki.go` and `internal/resume/read.go` finds none).
- An identity failure drops the event (hook.go:320-325).

**Cost.** Memory silently forks whenever a project's address changes, and everything done outside git lands in one undifferentiated thread. A later chat needs the project's history and gets a fresh, empty project.

**Direction.** Give each project a stable identity record that eva-md owns: an id minted at first sight, with the remote(s), root path(s) and the root commit as evidence that can change, matched by any of them; non-git folders keyed by folder; an alias table in the wiki so an old key resolves to the project. Keep the fail-closed rule and the hardened reads. **Action: rebuild** (the identity model), keep the git reader. **Basis: verified in code.** The alias-URL case is known (known-limitations.md:21); rename, move and the general bucket are not.

### setup-4 (high, reach): one wiki per machine, and the guard is a prompt keyed on the host name

**What the code shows.** Using another Mac's wiki is discouraged by a question whose default is no (setup.go:752-757), decided by comparing the host name without its domain (setup.go:744-750). Adopting copies the wiki's instance into the config (init.go:177-184, 256), so the "instance guard" on every write (update/wiki.go:49-52) no longer tells the two machines apart. Nothing ever fetches or merges: a rejected push is `diverged`, "fixed by hand" (wikigit/commit.go:37, 495-499), and the only merge provision is `log.md merge=union` (init.go:300). The inbox is never committed (init.go:301), so source refs resolve only on the machine that captured them (init.go:281). Without a terminal another Mac's wiki is adopted with no warning (known-limitations.md:254), and renaming the Mac makes its own wiki look foreign (known-limitations.md:255). The machine label, usually the owner's name, is committed in `.eva-md/schema.toml` (init.go:164-172).

**Cost.** A person with a laptop and a desktop has two memories. A team has none in common. Two Macs with the same name, or a yes at the prompt, produce a diverged wiki that only a person can repair.

**Direction.** Decide multi-device as a first-class design: a generated machine id in eva-md's state (not the host name), a wiki layout in which machines never write the same lines (per-machine journals merged into pages, or pages rebuilt from mergeable records), fetch and rebase before push, and a defined path for sources that live on another machine. **Action: rebuild** (the sync model), **add** (machine id). **Basis: verified in code.** The rule is a stated decision (decisions.md:28, decision 1; design.md:1240), not a listed limitation.

### setup-5 (high, reach): only a Mac terminal with host CLIs on PATH can install it

**What the code shows.** macOS only (install.sh:35-37; setup.go:237-240; release/fetch.go:44), though only five `runtime.GOOS` sites exist in the non-test code (config.go:44; init.go:500, 522; hook.go:633; setup.go:99) and the tests already run on Linux. The hosts are found only as `claude` and `codex` on PATH (setup.go:248-267; install/host.go:171, 308-309). Questions are read from `/dev/tty` (install.sh:116-120). The plugins are rendered on the machine with the binary's absolute path and registered from a local marketplace folder (install/templates.go:58; install.go:351, 368), so eva-md cannot be installed from a host's own plugin interface. Upgrades are a manual command (upgrade.go:38-46).

**Cost.** Linux workstations, dev containers, SSH hosts and cloud sessions, where much agent work runs, are excluded by a gate rather than by the architecture. A person who uses only a desktop app cannot install.

**Direction.** Make the host's plugin marketplace the front door: a published plugin whose first hook bootstraps the verified binary, so "install" is one action inside the host. Lift the macOS gate for Linux behind the existing test suite. Find a host's CLI from its app bundle as well as PATH. **Action: add** (in-host install, Linux), **evolve** (host discovery). **Basis: verified in code**; whether a hook may bootstrap a binary under each host's rules is **inferred** and needs a check. macOS-only and no Homebrew are known (known-limitations.md:265); the desktop-app-only gap and the in-host install are not.

### setup-6 (high, architecture): setup is also the upgrade mechanism, with two config files and no record of what the user chose

**What the code shows.**
- The install command and `eva-md upgrade` both run the whole guided setup (install.sh:114-120; upgrade.go:156). On a set-up Mac that prints the checklist again and asks "Install or update the plugins? [y/N]", where Enter stops the upgrade (setup.go:477-490).
- Setup's inputs live in a second file, `config.env`, because setup must be re-runnable (setup.go:177-193). The hosts chosen are stored nowhere: `Config` has no such field (config.go:50-79) and the answers file setup writes hard-codes `all` (setup.go:908), so an upgrade adds the other host's plugin, or stops when that host can't take it (known-limitations.md:274).
- One setting has two sources of truth: `EVA_MD_CODEX_IGNORE_USER_CONFIG` overwrites `codex_ignore_user_config` in `config.toml` on every setup (setup.go:570-575; known-limitations.md:247).
- On a new Mac answered interactively, the blank template stays forever, still saying "fill them in, then run eva-md setup" (setup.go:901-903; answers.go:33).
- `config.toml` mixes the machine binding and the preferences, and saving it drops comments and unknown keys. Setup guards one write against that (setup.go:935-938) but `wikiinit.Run` saves with no guard (init.go:262), which the re-clone path reaches.
- Several errors tell the user to "move <config> aside to start over" (setup.go:790, 836, 853, 855).

**Cost.** Upgrades need a person, a terminal and a typed `y`; a wrong Enter leaves the old version. State is spread over two hand-editable files whose precedence the user must learn.

**Direction.** Separate three things: program-owned state (binding, chosen hosts, machine id) in the state folder, never hand-edited; user preferences in one file the program never rewrites; and an idempotent, question-free `reconcile` step that install and upgrade both run. Self-update on a schedule with rollback, the plugin folder kept byte-stable so no host re-approval follows. Remove `config.env`. **Action: rebuild.** **Basis: verified in code.** Parts are known (known-limitations.md:247, 248, 274, 276).

### setup-7 (medium, architecture): half of the CLI is repo-hosting orchestration held together by English wordings

**What the code shows.** The four setup files are 2,930 of the 5,619 non-test lines in `cmd/eva-md` (52 percent), with 3,929 lines of setup tests. They exist to drive `gh` and `glab`: guess GitHub or GitLab from a substring of the host name (setup_repo.go:108-122), ask both CLIs (setup_repo.go:222-295), create, view and read back a repo, and classify failures by 22 substrings of English output from gh, glab, the hosts' APIs and Go's net package (setup_refusals.go:38-100), down to un-wrapping glab's word-wrap at 116 columns for one named version (setup_refusals.go:102-112). git's and ssh's read failures are sorted the same way, by 24 more wordings (wikigit/git.go:128 onward). The classification is used only for the peek: when init's own clone, commit or push fails, setup prints the generic "Git clone failed; files are preserved..." with no reason, although the error carries one (setup.go:525-526; wikigit/git.go:109). The whole path has only ever run against fake scripts and one self-hosted GitLab; "Real gh ... unchecked" (verification.md:153).

**Cost.** The largest and most patched part of the CLI (12 of 55 commits touch setup.go) serves a step a built-in product would not have, depends on third-party wording that changes without notice, and its commonest path, gh on github.com, has never run for real.

**Direction.** Once the remote is optional (setup-1), eva-md needs only "attach this address", done with plain git and one honest error. Repo creation becomes a short suggestion to the user or a thin optional helper, not a guided dialogue. Keep the privacy rules (must-keep 1 to 6). **Action: remove** most of `setup_repo.go` and `setup_refusals.go`; **rebuild** a small attach command. **Basis: verified in code.** The wording dependence is known (known-limitations.md:249).

### setup-8 (medium, invisibility): what the user still has to do after setup says "eva.md is ready"

**What the code shows.** Codex's hooks need approval in `/hooks` after install and after every upgrade that changes the plugin folder (setup.go:566-569; install.go:262-264; README.md:144), and capture nothing until then (known-limitations.md:263). Open chats are not picked up (setup.go:567-568). Claude Code may ask before each MCP tool's first use (README.md:204). The last line tells the user to run `doctor --live` (setup.go:624). The daily commit costs a signing approval each day for anyone whose signer asks (decisions.md:54-57, decision 7).

**Cost.** "Ready" is followed by three or four more actions, one recurring daily, and one whose omission silently disables a host.

**Direction.** Make the plugin folder permanently byte-stable (a shim that never changes; behaviour lives in the binary) so Codex trust is asked once in the product's life. Detect a host whose hooks are silent and say so in the other host's brief and in that host's next start. Run the live check automatically with the first real update instead of asking for it. Keep the core on hooks, which need no per-tool permission. Decide whether machine-written daily commits must be signed. **Action: evolve.** **Basis: verified in code** for the steps; the byte-stable shim's feasibility is **inferred**. Mostly known (known-limitations.md:262-263).

### setup-9 (medium, reliability-safety): one bad line of config turns memory off on both hosts

**What the code shows.** Any TOML error, an out-of-range value or one malformed exclude entry makes `LoadContext` return an error (config.go:104-116, 148-152); the hook then captures nothing and gives no brief, saying so only at a chat start (main.go:132-148; hook.go:191-196). A read cut off by the hook's time limit says nothing (main.go:144-146). Setup also refuses to run on such a config (setup.go:167-171), so an upgrade is blocked too.

**Cost.** A typo while adding an exclusion silently loses every chat until someone notices a one-line notice.

**Direction.** Keep fail-closed for privacy, but against the last config that validated: snapshot a validated config into state, keep using it when the file breaks, and report the break. Change settings through a validating command. Split the file as in setup-6. **Action: evolve.** **Basis: verified in code.** Known (known-limitations.md:22).

### setup-10 (medium, reliability-safety): the remote's privacy is checked once, and passes when it can't be told

**What the code shows.** `checkPrivate` returns nil when the signed-in CLI's `repo view` fails for any reason, with no fallback probe (setup_repo.go:579-582); with no CLI, a repo passes unless an anonymous HTTPS read succeeds (setup_repo.go:568-578, 846-858), so an SSH alias, another port, a proxy or a transient failure reads as private (known-limitations.md:239, 244). A set-up Mac never checks again: `configuredPlan` makes no visibility call (setup.go:764-837) and setup is the only place either CLI runs (setup_repo.go:23-27). The daily push continues for as long as the wiki exists.

**Cost.** A repo made public or internal later, or one that was never private, keeps receiving summaries of the user's chats every day.

**Direction.** State the guarantee honestly ("private as far as eva-md could tell on <date>") and re-check where a cheap check exists: the anonymous probe before a push, on a schedule, pausing the push and telling the user when it starts to succeed. **Action: evolve.** **Basis: verified in code.** Largely known (known-limitations.md:239); the `repo view` failure passing is not listed.

### setup-11 (medium, evaluation): nothing measures the first run

**What the code shows.** The benchmark measures hook latency only (benchmark.py; no setup or install measurement). No test or doc states a budget for questions, approvals, steps or wall time, and nothing measures time from the install command to the first brief with content. Every setup test uses fake `gh`, `glab` and host scripts; the live record is one Mac on one self-hosted GitLab, where "setup's health check and Codex's hook approval after it were not recorded" (verification.md:153, 155). An Intel Mac is unchecked (verification.md:155).

**Cost.** "Feels built in" has no number, so each added question or check is free, and the first-run path regresses without notice.

**Direction.** A first-run scorecard run in CI on a clean macOS (and Linux) image with real host CLIs: count of prompts, approvals and lines printed on the happy path, wall time by phase, time to first non-empty brief, and success rate across a matrix (no gh, gh HTTPS, SSH agent that asks, signing on or off, one host only). A test that fails when the happy path asks more than the budget. Contract tests that replay recorded real output of pinned gh and glab versions through the classifiers that remain. **Action: add.** **Basis: verified in code** (absence checked by search).

### setup-12 (low, reliability-safety): the checks don't test what most often fails

**What the code shows.** The checklist looks for commands and a CLI sign-in (setup.go:237-334) but never tests that git can sign in to the remote, that a commit identity exists, or that the signer works. When setup creates the repo, no git operation touches the remote before init's clone (setup.go:425-452; setup_repo.go:503-520), so the first test of git's sign-in comes after a repo has been created on the host, and its failure is the generic message of setup-7 followed by "the new repo stays for the next setup" (setup.go:526-533). gh's default git protocol is HTTPS, which needs a credential helper (setup_repo.go:523-537; known-limitations.md:240).

**Cost.** The likeliest first-run failures arrive late, after a side effect, with the least helpful message.

**Direction.** In a local-first design these become failures of the optional sync step, reported where the user can act, never failures of installing memory. If a guided attach remains, test `git ls-remote` and a throwaway signed object before creating anything. **Action: evolve.** **Basis: verified in code**; that a Mac without a configured identity fails the first commit is **inferred** (git may derive one from the account).

---

## 5. Essential and accidental complexity

**Essential.** The privacy rules around a repository that will hold summaries of every chat: refuse public, ask about internal, never echo CLI output, never pass tokens, never re-clone the wrong wiki. Init's guards on what receives the first commit. Fail-closed identity with hardened, bounded git. The folder rules that keep macOS privacy protection and cloud sync away from the wiki. Strict reads of files a person edits.

**Accidental.**
- Making a hosted repository a precondition of capture, which then requires gh/glab discovery, GitHub-or-GitLab guessing, creation, visibility checks, internal-repo consent, refusal classification and address hints: 1,417 lines in setup_repo.go and setup_refusals.go alone, plus setup_checks.go and the remote half of setup.go, each layer answering the previous patch.
- Running the whole setup as the upgrade, which created `config.env`, its template, its filled-in variant and its precedence rules.
- The host name as machine identity, which needed the domain-stripping rule (setup.go:740-750) and still breaks on rename.
- The identity cache re-implements git's repository discovery and its config-include parser (929 lines in cache.go and gitconfig.go) to avoid one git process on a hook. It is sound and differential-tested, so it is a keep; but an identity record owned by eva-md (setup-3) and resolved once per chat would need far less of it.

---

## 6. Overall call

**Rebuild the first-run flow; keep init's guards, the config readers and the git reader; rebuild the identity model.**

The code is careful and its guards are right. What is wrong is the shape: memory cannot start until a hosted repository exists, and half of the CLI is the consequence. Patching it further adds questions. A local-first install with a question-free reconcile step, sync attached later, program-owned state apart from user preferences, and a stable project identity removes most of this code and most of the user's steps at once.

---

## 7. Open questions

1. Must an off-machine copy of the wiki exist on day one, or may the wiki start local and sync be attached later? Decision 1 says the memory lives in a separate private repo; it does not say when.
2. Must eva-md's own daily commits be signed? Signing costs a daily approval for users whose signer asks.
3. Is one wiki shared by several machines, or by a team, in scope? It needs a merge model the wiki's layout does not have.
4. May eva-md import transcripts the hosts wrote before it was installed? They were not captured under its redaction or its off-the-record rule, and summarizing them costs model calls.
5. Which front doors must work: each host's plugin marketplace, Homebrew, a managed rollout? May a plugin's hook download and verify a binary under each host's rules?
6. Is Linux in scope now that only a gate excludes it? Windows?
7. Does Codex re-ask for hook trust on any byte change in the plugin folder, and can that folder be frozen for good?
8. Where do the desktop apps keep their CLIs when none is on PATH?
9. Should a folder outside git be its own project, and should a renamed remote be followed automatically or confirmed?

---

## Verification

A second reader tried to refute each finding at commit 51b241c, by reading the cited lines and what they call. Nothing was run but read-only shell commands (grep, wc, sed, git log). Result: 7 confirmed, 5 partly, 0 refuted. No finding rests on code that isn't there; the corrections are to scope, to "silent" and "every", and to severity.

### Verdicts

| Id | Verdict | Their severity | Mine | Evidence and correction |
|---|---|---|---|---|
| setup-1 | partly | critical | high | The cited lines hold: install refuses without a config (install.go:139-146), the wiki is made and pushed at setup.go:524 before the plugins at 555, and the hook captures with no config (main.go:132-135; hook/maintain.go:27-33; hook.go:268-366 needs no wiki). The four questions are at setup.go:379, 478, 520 and setup_repo.go:357. But "hosted" is wrong: a remote may be a bare repo on the Mac (wikigit/git.go:275-281; README.md:35, 101-103; decisions.md:26; setup_refusals.go:183), and then no host, key or network can block, only git and the signer. What is required is a remote, of any kind, before the plugins. Nothing is lost or broken, so high, not critical. The direction reopens decision 1 and decision 16 ("Wiki location: chosen by you during setup; there is no default", decisions.md:104). |
| setup-2 | confirmed | high | medium | catchup.go:1-2 and commands.go:273-292 say what is claimed, and design.md:830 ("No backfill") agrees. Two corrections. decisions.md:58 is the wrong cite: decision 8 is about the hosts' own memory features. The right one is decisions.md:116, "Ruled out: History backfill on install", with the reason at design.md:1403 (host transcript formats are unstable). So this is a ruled-out feature, not an oversight, and "add" reopens it. The cost is once per project. |
| setup-3 | confirmed | high | high | identity.go:73-108, key.go:13-35 and update/wiki.go:126-134 show it; resume/read.go:613-635 proves a folder the same way; no project alias or migration exists (the only "alias" code is system page names). It is decision 10 as built (decisions.md:63-65). One narrowing: a rename on the host forks memory only once the clone's origin URL changes or the repo is cloned again. Two cases to add: a repo without a remote gives each linked worktree its own key, since localKey hashes the worktree's top level (identity.go:98-113; key.go:28-30); and the same repo keys twice when its clones' origins differ in host or path form, as ssh.github.com against github.com or Bitbucket's /scm/ path, because NormalizeRemote maps no host (identity.go:178-214; setup's apiHost at setup_repo.go:146-155 is not used there). |
| setup-4 | confirmed | high | high | setup.go:744-757, init.go:177-184 and 256, update/wiki.go:49-52 and wikigit/commit.go:497-499 show it. No git fetch, pull, rebase or merge is run anywhere in the non-test code. madeHere compares host labels only (setup.go:744-750), so a second Mac of the same name adopts the wiki with no question (setup.go:716-717). It is decision 1 as built (decisions.md:28). Parts are listed: known-limitations.md:223, 254, 255, so "known" is partly true. |
| setup-5 | confirmed | high | medium | The gates (install.sh:35-37; setup.go:237), the PATH lookups (setup.go:248-267; install/host.go:171, 308-309) and the local render (install/templates.go:58, 91-93) are as cited, and there are five runtime.GOOS sites. Corrections: Linux is also shut out by darwin-only release assets (release/fetch.go:44), and cloud sessions by the hook itself (hook.go:284-286), not by the gate alone. The desktop-app-only gap is inferred: no code or doc shows where the apps keep their CLIs. A first hook that downloads the binary would break the rule that hooks make no network calls (AGENTS.md, "Hooks fail closed and stay fast"). macOS-only is the stated scope (README.md:32; known-limitations.md:265), so medium. |
| setup-6 | partly | high | medium | True: upgrade runs the whole setup (upgrade.go:156), config.env overwrites config.toml (setup.go:570-575), setup writes Hosts "all" (setup.go:908), Save drops unknown keys and comments (config.go:76-78, 121-133; init.go:262). Wrong: "every upgrade needs a person, a terminal and a typed y". The question is asked only in a terminal (setup.go:477); without one a set-up Mac's setup asks nothing and goes on (design.md:659), and the install command takes --yes. What is true is that `eva-md upgrade` passes no flag, so in a terminal it always asks. Also wrong: "chosen hosts are stored nowhere". install-record.json lists each host's marketplace and plugin (install/record.go:113-118; install.go:413-418); setup just never reads it back. EVA_MD_HOSTS in config.env is the intended record (known-limitations.md:274). "Self-update" is ruled out (decisions.md:117). |
| setup-7 | partly | medium | low | The counts hold: 2,930 of 5,619 non-test lines, 22 wordings, 12 of 55 commits, and real gh is unchecked (verification.md:153). The title overstates. gh and glab are driven by exit status and JSON (setup_repo.go:734-783); the wordings only choose the hint after a failed `repo create`, and anything unmatched falls back to a general line (setup_refusals.go:111-119; setup_repo.go:479-487). A reworded error degrades a message; it does not break setup. The one behaviour that hangs on a wording is the internal-repo question (createRestricted, setup_repo.go:489-501). The gh/glab code is the 1,417 lines of setup_repo.go and setup_refusals.go, a quarter of the CLI, not half. It is listed (known-limitations.md:249), so "known" is true. |
| setup-8 | partly | medium | low | The steps exist as cited (setup.go:566-569, 624; install.go:262-264; README.md:204; decisions.md:56). But a skipped Codex approval is not silent on a Mac with both hosts: every Claude Code chat start carries "eva.md: review its hooks in Codex /hooks" while needs-trust.json is newer than Codex's SessionStart heartbeat (health/health.go:74-97; hook/resume.go:173-184), and a host with chats but no capture is named too (health.go:98-107). The MCP instructions and eva_status carry the same notice (mcp/instructions.go:177-178). So "say so in the other host's brief" is built. It is near silent only for a Codex-only user. Binary-only upgrades already leave the plugin bytes, and Codex's trust, alone (install/templates.go:11-12; install.go:244, 262); only an upgrade that changes the skills, hooks or MCP manifest asks again (README.md:144). |
| setup-9 | confirmed | medium | medium | config.go:104-116 and 148-152, main.go:132-148, hook.go:191-196 and setup.go:167-171 show it. An unknown key alone does not fail the load (config.go:108-110). Each chat start on both hosts, the doctor and status say so (known-limitations.md:22). A flaw in the direction: falling back to the last config that validated would capture the very folder whose new exclusion had the typo, which is the case the impact names, so it is not fail-closed. |
| setup-10 | confirmed | medium | medium | setup_repo.go:579-582 returns nil when `repo view` fails for any reason, a time-out included, with no probe and no line printed; a public repo then passes if git can read it at the peek. 568-578 and 846-858 pass whatever the anonymous read can't reach. configuredPlan (setup.go:764-837) makes no visibility call, and README.md:99 says so in words. known-limitations.md:239 and 244 list the detection limits, not the `repo view` pass or the missing re-check. |
| setup-11 | partly | medium | low | No test or doc sets a budget for questions, approvals or time, and no run uses real host CLIs (verification.md:153-155). Two corrections. benchmark.py also times MCP calls, search, code status and recall, though nothing of setup. And a new question is not unseen: the setup tests script the answers and assert the prompts (setup_test.go:228-261), and one asserts that nothing is asked without a terminal (setup_test.go:290). The direction's CI with real host CLIs needs signed-in accounts, against AGENTS.md's offline-tests rule. |
| setup-12 | confirmed | low | low | A repo setup creates comes back from `use` with no read of it (setup_repo.go:503-520), so init's clone is git's first contact; setup.go:526 prints only plain(err), dropping the Read reason and Class that wikigit.Error carries (wikigit/git.go:84-109). Mitigations they left out: sayHTTPS names the credential fix before the clone for an HTTPS address (setup_repo.go:525-537), the first commit's author and signing are shown before the push (setup.go:515-523), and a rerun reuses the repo and resumes the clone (setup_test.go:1149). |

### Directions that reopen a decision or a rule

docs/decisions.md is to be reopened only with a strong reason (AGENTS.md). These directions do, and the report doesn't say so:

- setup-1: decisions 1 and 16 (a separate private repo; no default wiki folder).
- setup-2: "History backfill on install" is ruled out (decisions.md:116; design.md:830, 1403).
- setup-3: decision 10 (the remote URL is the project; chats outside a repo go to the general area).
- setup-4: decision 1 ("the memory of one wiki never mixes with another's").
- setup-5: hooks make no network calls, so a hook can't bootstrap the binary.
- setup-6: "Telemetry and auto-update" is ruled out (decisions.md:117).
- setup-9: the last-good fallback is not fail-closed for a new exclusion.

### What they missed

**missed-1 (medium, reliability-safety): setup asks four questions about the repo and says nothing about what the product then does by default.** From the first chat, every project is captured, and background updates run the host's CLI on the user's subscription: `Updates: true` and 40 model calls a host a day (config.go:34, 83), at the highest effort unless set (config.go:187-197). Setup's intro (setup.go:227-232), summary (460-474) and closing lines (611-624) name none of this: no word on which projects are captured, on `exclude`, on `updates = false` or on the model calls, beyond `doctor --live`'s one. The answers file has no key for any of it (answers.go:19-26). Only the README says it (README.md:21, 198). A question-free install makes this worse, not better. Direction: one plain statement at install and in the first brief of what is captured, what is sent to the model and how to exclude a project or turn updates off, and a deliberate default for effort. Add. Not in known-limitations. Verified in code.

**missed-2 (medium, architecture): once set, the wiki's remote can't be changed or attached by any command.** Setup refuses another remote on a Mac set up (setup.go:773-786), init refuses a config that names another remote (init.go:76-78), the daily commit refuses when origin isn't the configured one (wikigit/commit.go:256-258), and the doctor fails the same check (health/health.go:260-265). README.md:103 says moving a local wiki to a hosted repo "is not a built-in step". So the documented local fallback is a dead end, and a renamed wiki repo or changed host means hand edits to config.toml and to origin. Local-first with sync attached later (setup-1's direction) needs exactly this path, and it touches more than the "few places" setup-1 lists. Direction: one command that re-points the binding, with setup's privacy check and peek, a push of the full history, and the config and origin changed together. Add. Listed (known-limitations.md:250). Verified in code.

**missed-3 (low, reliability-safety, inferred): the git check can't fail on a Mac without the developer tools.** The check is a PATH lookup (setup.go:242-245). macOS ships /usr/bin/git as a stub that asks to install the tools and exits non-zero, so the lookup succeeds and "git" passes. The first real git call is the signing read, which turns any failure into "Commit signing off" (setup.go:1304-1318). Setup then goes on, may create the repo with gh, and fails at the peek or the clone with "git couldn't read this repo" or "Git clone failed" (setup.go:1080-1090, 525-526), never "git isn't installed; xcode-select --install". The lookup and the swallowed error are verified in code; the stub's behaviour is macOS's, not run here. Direction: run `git --version` for the check. Evolve. Not in known-limitations.
