# eva-md

eva-md is the platform for eva.md ("Eva dot M D"), a Markdown, git-based memory for Claude Code and Codex. It captures your chats into a redacted local inbox and gives every new chat a short resume brief. Ask "where did we stop?" in a new chat, in either tool, and it can answer.

Coding assistants forget everything between chats, and after a chat is compacted. eva.md keeps that memory as plain files you own: a local inbox for recent chats and a private wiki repository in git. Nothing needs a local model, a resident service or a cloud account.

## How it works

- **Hooks.** eva-md installs native plugins for Claude Code and Codex. Their hooks run a short-lived `eva-md hook` process on chat events. A hook never blocks the host: on any failure it exits successfully, and when its config can't be read it says so in one line at a chat start.
- **Redacted inbox.** Each prompt and reply, and one line for each file change, command, read, search, web fetch and MCP call naming what it acted on but never its output, is redacted, then appended to a local inbox. When a hook misses something, eva-md recovers it from the host's transcript, within fixed limits.
- **Resume briefs.** When a chat starts, is cleared or is compacted, eva-md builds a brief from the inbox for the current project and branch. The brief is deterministic: no model writes it.
- **Wiki updates.** After a chat, a detached job runs the chat's own host CLI headless to update the branch's thread page and the project's standing rules, and to save lasting facts, decisions, procedures and constraints as knowledge pages. Code checks every line before it is written.
- **Recall.** A prompt that strongly matches knowledge pages gets those facts, at most 3, fenced as reference notes; a weak match gets nothing. It gets facts of its own project, of all projects, of projects that share a system page with it and of projects it names, never of the others, which `eva-md search` still finds. A new chat's brief lists the project's constraints. Both read local index files, with no model. Once a day the wiki is committed with your signing and pushed. A Claude chat a program runs, such as `claude -p` or the Agent SDK (told apart by the `CLAUDE_CODE_ENTRYPOINT` it inherits), is captured and searchable, but it gets no update of its own and is never the brief's previous chat. Codex `codex exec` runs can't be told apart.
- **Private wiki.** The long-term memory is a separate private git repository of Markdown pages, which `eva-md setup` creates in a folder you choose. This repo holds only the platform.
- **MCP tools.** Both plugins also declare a read-only MCP server, the same binary, which each host starts once a chat, so the agent can call `eva_search`, `eva_show` and `eva_status`: the search and show commands, and a status of the project's memory, its thread page, pending update, systems, knowledge pages and hook health. A recall skill in both plugins tells the agent when to look in memory and how.

## Status

Built: capture, resume and wiki updates. That covers the hooks, the redacted inbox, transcript recovery, resume briefs at startup and after `/clear` or compaction, recall across Claude Code and Codex, `eva-md search` over captured chats and the wiki's thread, system and knowledge pages, the background update that keeps each branch's thread page, the standing rules and the knowledge pages, recall of matching knowledge on prompts and the project's constraints in the brief, a recall skill in both plugins, system pages, which the agent writes with `eva-md system write`, and the pointers to them, how far the code each system page cites has moved since it was written (`eva-md system status`, also shown in the pointers and the doctor), the daily signed commit and push, health checks with `eva-md doctor --live`, install through each host's native commands, guided setup, also for a wiki on a self-hosted git server such as GitLab, the one-command install and upgrade from GitHub releases, and uninstall.

Once installed, the hooks start `eva-md update` in the background when a chat ends or is compacted, when a chat's unprocessed text passes 60 KB, at a prompt after a 30-minute gap, and at a chat start for the project's idle chats, and with your first chat start of the day for up to 5 chats of any project idle an hour or more; it runs the chat's own host CLI headless, which uses your subscription. The first hook event of each day starts `eva-md maintain`, which commits the day's wiki changes with your signing and pushes them. Chats captured before the update first ran, as on an upgrade from a capture-only release, are left as they are: the update starts from the install. In `~/.config/eva-md/config.toml`, `updates = false` turns the updates off, `commits = false` the daily commit and push, and `recall = false` the knowledge a prompt gets, while search, show and the brief's constraints stay on. If the config can't be read after an edit, as with a TOML mistake or a value it refuses, eva-md captures nothing and each new chat says so in one line: "eva.md: the config can't be read, so this chat isn't captured; run eva-md doctor." Unknown keys are reported, not refused.

The daily maintenance also keeps a search index of the inbox, even on a Mac without a wiki, so `eva-md search` stays fast over years of history. Running `eva-md index` builds it at once. That is optional: until the index exists, search scans the inbox, and once 4 MiB or more of it is missing, each `eva-md search` indexes up to 16 MiB of it for the next one.
Both plugins declare `eva-md mcp`, a read-only MCP server that the hosts start once a chat; `mcp = false` in the config turns its tools off while the hooks and the command line stay on. It is checked offline only, like the rest.

Not verified live yet: no live update has run, the new parts have not been checked in the real host apps, and no wiki on a self-hosted GitLab has been run end to end. Releases are published on GitHub. On one Mac, with a self-hosted GitLab, the install command has installed eva-md from one, and the uninstall script has run from one up to the removal of Claude Code's marketplace; `eva-md upgrade` has run only against a local stand-in for a release. Everything is checked offline; see [docs/verification.md](docs/verification.md).

See [docs/known-limitations.md](docs/known-limitations.md) for what eva-md doesn't do, or doesn't do well: gaps still open, limits it keeps by design, and limits of the hosts and tools it runs on.

## Requirements

- macOS, on an Apple silicon or Intel Mac. Setup, paths and the folder checks assume a Mac.
- git.
- Claude Code, Codex, or both.
- An empty private repository for the wiki, on GitHub, GitLab or any git host; where the host's admins allow no private repos, an internal one once you agree, or a bare repo on this Mac (see [A wiki kept on this Mac](#a-wiki-kept-on-this-mac)).

## Install

```sh
curl -fsSL https://github.com/heyNag/eva-md/releases/latest/download/install.sh | bash
```

This one command installs eva-md on a new Mac and upgrades it on a Mac that is set up. The script checks that it isn't running as root (never use sudo), that this is macOS on an arm64 or x86_64 Mac (a shell Rosetta runs on Apple silicon gets the arm64 binary), and that curl and shasum are there. It finds the latest release, or the one `EVA_MD_VERSION` names (below), downloads that release's binary for this Mac with its `SHA256SUMS` into a new temporary folder, which it removes at the end, and checks the binary's checksum and the version it reports; a checksum that doesn't match stops it before anything runs. Then it runs the binary's guided setup below, which does the rest: the checks, the answers file, the wiki, the plugins and the stable copy of the binary. Setup's flags go after `bash -s --`:

```sh
curl -fsSL https://github.com/heyNag/eva-md/releases/latest/download/install.sh | bash -s -- --dir ~/eva-wiki
```

To install a release other than the latest, give its tag to `bash`, after the pipe. Put before `curl`, it reaches only `curl`, and the script installs the latest release:

```sh
curl -fsSL https://github.com/heyNag/eva-md/releases/latest/download/install.sh | EVA_MD_VERSION=2026.11.3 bash
```

When a check or setup stops, the last line says so: when it named a problem, fix it, then run the install command again. Answering no to setup's question stops it too, with nothing changed, except that a repo setup created for you stays, empty, for the next setup. On a new Mac without a terminal for setup's questions, the first run stops after writing the answers file, `~/.config/eva-md/config.env`; fill it in and run the command again.

Before you run it:

1. Install Claude Code or Codex (or both), and git.
2. Create an empty private repository for your wiki, on GitHub, GitLab or any git host. If gh or glab is installed and signed in, setup can create it for you, under the account that CLI is signed in as, and it refuses a public repo when it can tell. An internal repo, which everyone signed in to its host can read, it uses only once you agree (see [When the host refuses](#when-the-host-refuses)). Setup tells GitHub from GitLab by the host's name first: github.com, a name holding `github` (such as github.example.com) or ending in `.ghe.com` is GitHub, which it works with through gh; gitlab.com or a name holding `gitlab` (such as gitlab.example.com or code-gitlab.example.org) is GitLab, through glab, pointed at that host. When that CLI isn't signed in there, setup asks the other, so a GitHub Enterprise server whose name holds `gitlab` still works with gh. For a name that holds neither, or both, it asks both which is signed in there, and when both are, it asks you. Each CLI is asked as `gh auth status --hostname <host>` or `glab auth status --hostname <host>` says. A remote on GitHub's or GitLab's SSH host, ssh.github.com or altssh.gitlab.com, counts as github.com or gitlab.com. A host neither CLI is signed in to is a plain git server: give the address of a repo you created there. Name the host with `EVA_MD_GIT_HOST_URL` in the answers file below, or with `--git-host`, and setup offers to create the repo only there. Without one, it offers gh on github.com and glab on glab's default host, which is gitlab.com unless `GITLAB_HOST` or glab's config names another. Setup never hands gh or glab a token from the environment, such as `GH_TOKEN` or `GITLAB_TOKEN`: sign the CLI in once with `gh auth login` or `glab auth login`.
3. Optionally, fill in the answers file, `~/.config/eva-md/config.env` (see below), so setup doesn't ask what the file already answers.

To build eva-md from source instead, see [Development](#development).

### The guided setup

Setup starts by checking this Mac, before it asks anything, in a short block headed "Checking this Mac": one line for each check that passes, and at the first problem a line saying what is wrong, the exact fix (`Fix: ...`) and "Then run setup again." It stops there (exit status 1), so fix that and run setup again. It checks, in order:

- that this is macOS;
- git (`xcode-select --install` installs it);
- the host apps `--host` or `EVA_MD_HOSTS` asks for: Claude Code, Codex, or at least one of them for `all`;
- the answers file below, when there is one, read without errors;
- on a Mac not set up yet, the CLI for the wiki's host (see above). With only a git host, from `EVA_MD_GIT_HOST_URL` or `--git-host`, setup has to create the repo there, so gh or glab must be installed and signed in to that host, with the fix such as `glab auth login --hostname gitlab.example.com`; on a host whose name says neither GitHub nor GitLab and that no CLI is signed in to, a plain git server, it asks you to create the repo there and give its address. With a remote, from `EVA_MD_WIKI_REMOTE` or `--remote`, a CLI signed in there is used; when none is, a `!` line says setup will check the repo is private by reading it without signing in, and setup goes on. With no host known yet, the block says, as information, which of gh and glab are installed and signed in where. A Mac already set up skips this check and these lines;
- last, install's own checks, run as a preview: anything installing the plugins would refuse stops setup here, before it makes or pushes a wiki. With Codex's hooks feature off, which a managed Mac may set, the fix is to turn it on with `codex features enable hooks`, or to set `EVA_MD_HOSTS=claude` in the answers file below (or run setup with `--host claude`) to install for Claude Code only, after installing Claude Code on a Mac that doesn't have it.

Your commit signing is shown too, as a `!` line when it is off; that never stops setup. The marks are coloured (green ✓, red ✗, yellow !) only when the output is a terminal and `NO_COLOR` is unset or empty. Then setup looks before it asks: it offers to use an eva wiki already in the folder or on the repo, warns when another Mac set that wiki up, and on a Mac already set up offers to clone a missing wiki folder again. On a new Mac it asks two questions, with no default answers, unless the answers file below gives them:

1. Where the wiki should live on this Mac. Choose a new or empty folder of its own, not in Documents, Desktop or Downloads, and not in iCloud Drive or other cloud storage such as OneDrive, Box, Google Drive or Dropbox (`~/Library/CloudStorage`), which syncs into `.git`.
2. Which private repository it is pushed to, such as `git@github.com:you/eva-wiki.git`.

It shows a summary and asks you to confirm. Then it creates the wiki, installs the plugins and runs the health check. Your git setup may ask you to approve signing the first commit and the push, for example in your password manager or SSH agent. Codex asks you to approve eva-md's hooks: type `/hooks` in Codex and approve them. Then start a new chat. Chats that were already open are not restarted.

Before it pushes a wiki branch the remote has never had, such as a new wiki's first commit, setup shows the commit's author and whether it is signed, as git made it in the wiki folder. Per-folder git settings such as `includeIf` can make these differ from the Commit signing line, which comes from your home folder. In a terminal it asks "Push it to <remote>? [Y/n]", with `--yes` too. A no keeps the commit and pushes nothing. Push it later with `~/.local/share/eva-md/bin/eva-md commit --push-only`, or, to change its author, fix git's settings for the wiki folder and run `git commit --amend --reset-author` in it; the daily commit pushes it along with the next changes. Without a terminal, setup shows the line and pushes. Setup's closing lines name the command, in the line for a health check that found a problem, the one for a first commit that isn't pushed, and the last line, which names `doctor --live` (below), as `eva-md` when `~/.local/bin/eva-md` links to the installed binary, and otherwise by the binary's full path, `~/.local/share/eva-md/bin/eva-md`. When `~/.local/bin` is a folder on your PATH, installing links `~/.local/bin/eva-md` to that binary, so you can type `eva-md doctor` or `eva-md upgrade`; otherwise use the full path.

### When the host refuses

Setup never prints the messages gh, glab or git print when they refuse or fail, because those can show account details. When one of them refuses, setup recognises the refusal from what it printed, by known English wordings, and says in its own words what happened and a next step that can work, naming only the host you gave it. Creating the repo with gh or glab:

- **Private repos not allowed.** On a GitLab whose admins restrict private visibility, setup says "glab couldn't create eva-wiki: the admins of gitlab.example.com don't allow private repos there." and asks "Create eva-wiki as an internal repo instead? Everyone signed in to gitlab.example.com can read an internal repo, and the wiki holds summaries of your chats. [y/N]". Enter is no. A yes creates it with `--internal`; a no says that neither glab nor you can make a private repo there, and that you can ask an admin there to create an empty private repo for you and paste its address, keep the wiki on this Mac in a local repo only your account can read (below), or use another host that allows private repos. After that, and after any later refusal on that host, the address question asks for the repo's address or a local repo's full path, not for a private repo to create there.
- **No permission, or a limit reached.** Type a group or organization you can create repos in, as `group/eva-wiki`; for no permission, ask an owner of that group or organization to let you, and for a limit on a self-hosted host, ask its admins to raise it (on github.com and gitlab.com setup names no one for a limit); or type `n` and keep the wiki in a local repo, whose full path the address question then asks for.
- **The name is taken.** Type another name, or `n` to paste the existing repo's address, which the address question then asks for.
- **The sign-in was refused**, as for an expired or revoked token. Run `glab auth login --hostname <host>` (or `gh auth login --hostname <host>`) in another terminal, then type the name again.
- **The host couldn't be reached** (its name, the connection or its certificate). Check the network or VPN, then type the name again.

Setup asks for a name three times at most. After a refusal on the last try, its next step never says to type a name: for a taken name, a refused sign-in or an unreachable host it says to run setup again, and for no permission or a limit it keeps who to ask and the local repo. The address question follows.

Reading a repo with git, before setup uses it: a repo git can't find, or that the account it signs in with can't see ("create it first as an empty private repo, or check the address"); an SSH key the host refused (add the key to your account there, `ssh -T git@<host>` shows which account the key signs in as, or use the HTTPS address; the hints sign in with the address's own user, as git does); an SSH agent that didn't sign, as when its approval was denied (approve its request, then try again); a host key ssh doesn't know yet (run `ssh -T git@<host>` once and, if the fingerprint it shows is the host's, answer yes); a host key that isn't the one ssh knows, which can mean someone is intercepting the connection (ask the host's admins whether its key changed before you delete the line of `known_hosts` that `ssh -T git@<host>` names as the offending key; setup never tells you to accept it); a host key ssh has marked revoked, which can mean a stolen key is being used to impersonate the host (ask the host's admins about its key; removing the host's lines from `known_hosts` would keep the revocation and lose the key ssh trusts); an HTTPS address git has no sign-in for (set up git's sign-in for that host, or use the SSH address); a host it can't reach (check the network or VPN); and a local path that holds no repo. Any other refusal, or one worded differently, gets setup's general message, as before.

An internal repo you give, at the question, with `--remote` or in `EVA_MD_WIKI_REMOTE`, gets the same kind of question, "This repo is internal. Everyone signed in to <host> can read an internal repo, and the wiki holds summaries of your chats. Use it anyway? [y/N]", where setup used to refuse it; on a GitHub host it names every member of its enterprise instead. A no refuses it, as before. Without a terminal it is refused unless `EVA_MD_ALLOW_INTERNAL_REPO=true` is in the answers file. A public repo is always refused. A Mac already set up, as on an upgrade, never checks its repo's visibility again, so it is never asked.

### A wiki kept on this Mac

When no host you can use allows a private repo, as on a GitLab whose admins restrict private visibility, keep the wiki's repo on this Mac: run `mkdir -m 700 ~/eva-wiki.git && git init --bare ~/eva-wiki.git` and give its full path, such as `/Users/you/eva-wiki.git`, at the remote question, with `--remote` or in `EVA_MD_WIKI_REMOTE`. Make the folder first with `mkdir -m 700`: git keeps the mode of a folder that is there already, so only your account can read the repo. A folder `git init` makes itself is 0755, and every account on a Mac can enter your home folder, so any of them could clone the wiki from it. Setup doesn't expand `~` in a remote, there or at the question, so type the path out. The wiki then exists only on this Mac: the bare repo is its one other copy, on the same disk, so a backup such as Time Machine is the only copy elsewhere. Moving it to a hosted repo later is not a built-in step: create an empty private repo there, run `git remote set-url origin <address>` and `git push -u origin HEAD` in the wiki folder, then set `expected_remote` in `~/.config/eva-md/config.toml` to the new repo as eva-md writes it (host/owner/repo, such as `gitlab.example.com/you/eva-wiki`), change `EVA_MD_WIKI_REMOTE` in `config.env` if it names the old path, and run `eva-md doctor`.

### The answers file

`~/.config/eva-md/config.env` holds your answers to setup's questions. It sits beside `config.toml`, which is eva-md's own settings file that setup writes; you write `config.env`. On a Mac not set up yet, when it is missing, setup writes a template there (mode 0600, never over an existing file), says so in a line of its checks, and asks its questions as usual; without a terminal, and without `--dir` and `--remote`, it stops after writing it. Fill it in and run setup again. On a Mac set up before, when it is missing, setup writes it after a successful run filled in with what this Mac now uses: the wiki folder; the remote and the git host it names, unless the remote is a local path, which is left out so that a volume that isn't mounted never stops a later setup; `all` for the hosts, since a `--host` flag is for its run only; and the Codex switch as `config.toml` has it (mode 0600, never over an existing file), and says so in one line.

Each line is `KEY=value`; blank lines and lines starting with `#` are skipped, a leading `export ` is allowed, and one pair of quotes around a value is removed. Values are literal: nothing in them is expanded or run. An empty value is an answer not given, and a command-line flag wins over a value here. The keys:

- `EVA_MD_WIKI_DIR`: the wiki folder, as `--dir`: a full path, or one starting with `~/`.
- `EVA_MD_WIKI_REMOTE`: the private repo the wiki is pushed to, as `--remote`, or the full path of a bare repo on this Mac (see [A wiki kept on this Mac](#a-wiki-kept-on-this-mac); `~` isn't expanded).
- `EVA_MD_GIT_HOST_URL`: where setup may create the repo when no remote is given, as `--git-host`: the web address of github.com, gitlab.com, or a self-hosted GitLab or GitHub Enterprise server, such as `https://gitlab.example.com`. Any page on the host works, such as your profile or projects page, because setup reads only the host. The new repo goes under the account gh or glab is signed in as there, named `eva-wiki` unless you type another name at setup's question, such as `group/eva-wiki`. A user name or password in the address, an SSH repo address (`ssh://` or `git@host:path`; a repo's address goes in `EVA_MD_WIKI_REMOTE`) and a scheme other than `https://` or `http://` are refused. An HTTPS repo address is read for its host alone, like any other page, so the repo it names is not used.
- `EVA_MD_ALLOW_INTERNAL_REPO`: `true` or `false`, your answer in advance to whether setup may create or use an internal repo, which everyone signed in to its GitLab, or every member of its GitHub enterprise, can read, when the host's admins allow no private repo or the repo you give is internal. `true` is a yes for any internal repo, on any host: setup creates one without asking when the host's admins allow no private repo, and uses one you give or it finds, printing a `!` line for each repo it creates or uses, saying who can read it (everyone signed in to that GitLab, or every member of that GitHub enterprise); `false` refuses one; empty asks in a terminal and refuses one without. The template leaves it empty, and so does the file setup writes on a Mac set up before.
- `EVA_MD_HOSTS`: `claude`, `codex` or `all`, as `--host`.
- `EVA_MD_CODEX_IGNORE_USER_CONFIG`: `true` or `false` sets `codex_ignore_user_config` in `config.toml` at the end of setup (see below); empty leaves it as it is.

With the folder and the repo in the file, setup asks neither question; its summary says the answers came from the file, and it still asks you to confirm (`--yes` skips that, and without a terminal the file works as `--dir` and `--remote` do). On a Mac already set up, a folder or repo in the file must name the configured wiki and its repo, as `--dir` and `--remote` must. A key setup doesn't know is named in one line and ignored, by its line number when it doesn't look like one of setup's own (`EVA_MD_` and capitals), since such a line may hold a secret pasted by mistake; a bad value, a link, a folder, a file over 16 KB or one that isn't UTF-8 stops setup with a message naming the file and the key, never a value, except the path of a local remote that isn't there now, which the message names.

For a wiki on a self-hosted GitLab, sign glab in once with `glab auth login --hostname gitlab.example.com` (setup's checks stop with that fix until you do), then fill in:

```sh
EVA_MD_WIKI_DIR=~/eva-wiki
EVA_MD_WIKI_REMOTE=
EVA_MD_GIT_HOST_URL=https://gitlab.example.com/your-username
EVA_MD_HOSTS=all
EVA_MD_CODEX_IGNORE_USER_CONFIG=true
```

and run the install command, or `eva-md setup` on a Mac that has eva-md: it offers to create the repo with glab on gitlab.example.com, under your own account there. For a GitHub Enterprise server, sign gh in with `gh auth login --hostname github.example.com` and give that server's address the same way.

After setup, run `~/.local/share/eva-md/bin/eva-md doctor --live`, as setup's last line says. It runs the headless update command once for each installed host, with the CLI the background update starts, a real model call that writes nothing, and is the only check that updates work with this Mac's account, proxy and settings. Setup itself makes no model call. Until your first chats, many of the doctor's checks are UNKNOWN.

If Codex has MCP servers of its own in `~/.codex/config.toml`, such as one for a wiki or an issue tracker, set `EVA_MD_CODEX_IGNORE_USER_CONFIG=true` in `config.env` and run setup, or, with no value there, add `codex_ignore_user_config = true` to `~/.config/eva-md/config.toml`; a value in `config.env` sets `config.toml` on every setup. Otherwise every Codex update run starts them, and a stray tool call could send them text from your chats. The flag makes update runs skip the rest of that file too, so a custom model provider set there may not apply (not yet checked on a real machine); leave it off if Codex needs those settings, and run `~/.local/share/eva-md/bin/eva-md doctor --live` after changing it. Auth still comes from Codex's own folder. Claude update runs start none of your MCP servers.

## Upgrade

```sh
eva-md upgrade
```

or run the install command again. The install command compares no versions: it runs the setup of the latest release, or of the one `EVA_MD_VERSION` names, which installs that release over the stable binary, even over a newer build from source. `eva-md upgrade` asks GitHub for the latest release and compares it with the binary's own version. When the release is newer, it downloads the binary for this Mac and `SHA256SUMS` into a temporary folder in eva-md's data folder, checks the checksum and the version the new binary reports, and runs the new binary's setup in this terminal, which keeps the wiki and updates the stable binary and the plugins. When it isn't, it says `eva-md <version> is up to date`. On a Mac that is set up, that setup asks "Install or update the plugins? [y/N]" before it changes anything: y or yes goes on, and Enter or any other answer stops the upgrade with nothing changed. `--check` only says whether a newer release exists, `--version TAG` picks a release, and `--force` installs a release that isn't newer. A binary built from source counts as the day of its commit, so a release of that day or older replaces it only with `--force`, and a build with no version (`dev`) is replaced only with `--force`. A failure before setup runs, a checksum that doesn't match included, leaves the installed binary and the plugins as they were and says what failed, and an install inside setup that fails rolls itself back. When setup stops after its install has put the new release in place, the last line says to run setup once the problem is fixed, as `eva-md setup` when `~/.local/bin/eva-md` links to the stable binary and by the binary's full path otherwise, since `eva-md upgrade` would now find it up to date. This is the only command that uses the network itself: it talks to github.com over HTTPS, and to GitHub's asset host, where github.com sends downloads.

An upgrade that changes the plugin folders, as the ones that add the MCP server and the recall skill do, and the one that points the recall skill at the system index, re-renders both of them once, so Codex asks you to approve eva-md's hooks in `/hooks` again.

## Uninstall

```sh
eva-md uninstall
```

previews: it lists what it would remove and what it keeps, with the size of the config and the local data. `eva-md uninstall --apply` removes the program: the plugins and their marketplaces through Claude Code's and Codex's own commands, the stable binary and the `~/.local/bin/eva-md` link (only while it points at that binary). It keeps the config and the local data. `eva-md uninstall --apply --all` also removes the config folder (`config.toml` and `config.env`) and all the local data (the inbox, the search index, logs, caches and the install record) once you type `delete` at its prompt, `--yes` or not; any other answer removes nothing. Without a terminal, `--all` needs `--yes` instead. While a background update or the daily job is running, uninstall removes nothing and asks you to try again shortly.

Uninstall never removes the wiki folder or touches its remote. Its last lines say where the wiki is: deleting it is your own step. It reads the wiki with git, on this Mac only, before it removes anything, and again after the removal for its last lines. When changed pages aren't committed, or commits aren't pushed, as the daily commit leaves the pages written after it until the next day, its preview, `--all`'s question and its last lines say how many, since they are only on this Mac, and name `eva-md commit` to run before uninstalling (`eva-md commit --push-only` when only commits wait), by the binary's full path when `~/.local/bin/eva-md` isn't its link on your PATH, as setup's closing lines do. After `--apply`, or once eva-md is no longer installed, they say to commit and push them with git in the wiki folder, or install eva-md again and run that; with `--all`, which removes the config, changed files are left to git alone, since setup takes a wiki with uncommitted changes only once they are committed. When the config doesn't name the wiki, as when you moved it aside or it can't be read, they give git alone, since `eva-md commit` needs that config, and setup refuses one it can't read. When git can't read the wiki, a line says so, and uninstall goes on. Codex keeps its approval of eva-md's hooks in its own config, which eva-md never edits; to clear it, open Codex, type `/hooks` and remove eva-md's hooks there.

Without eva-md on the Mac, with a binary that no longer runs, or, for `--all` and `--yes`, with one too old to take them, the uninstall script does the same with the latest release's binary, checked as the install script checks it:

```sh
curl -fsSL https://github.com/heyNag/eva-md/releases/latest/download/uninstall.sh | bash
curl -fsSL https://github.com/heyNag/eva-md/releases/latest/download/uninstall.sh | bash -s -- --apply
curl -fsSL https://github.com/heyNag/eva-md/releases/latest/download/uninstall.sh | bash -s -- --apply --all
```

If you deleted `~/.local/share/eva-md` by hand, Codex can't list its plugins while it still has eva-md's marketplace registered there, so the install command stops and says so. Run the uninstall script with `--apply`, which removes eva-md's plugin and marketplace from Codex, then the install command again. How Claude Code takes its own deleted marketplace folder hasn't been checked (see [docs/known-limitations.md](docs/known-limitations.md)).

## Commands

- `eva-md setup [--dir FOLDER] [--remote URL] [--git-host URL] [--host claude|codex|all] [--no-push] [--yes]`: the guided setup above. It reads `~/.config/eva-md/config.env` for what the flags don't give. Without a terminal, a first run needs `--dir` and `--remote`, or both in that file. `--git-host` names the host setup may create the repo on, as `EVA_MD_GIT_HOST_URL` does, such as https://gitlab.example.com or gitlab.example.com. Flags are for their run only. `--no-push` makes the wiki's first commit without pushing it. `--yes` skips the confirmation. On a Mac that is already set up, it keeps the wiki and installs or updates the plugins, and offers to clone the wiki folder again if it is missing or empty.
- `eva-md version`: the binary's version and the wiki schema it reads and writes. A release's binary prints its tag, the UTC day of the commit it was released from, such as `2026.11.3`, or `2026.11.3.2` for a second release that day. A binary built from source prints the date of the commit it was built from, in UTC, then that commit's time and short hash, such as `2026.11.2+0915.9e4f609`, so two builds of one day sort by their time; `.dirty` marks a build of a tree with uncommitted changes, and `dev` a build Go recorded no commit for. [docs/releasing.md](docs/releasing.md) says how a release is made.
- `eva-md upgrade [--check] [--force] [--version TAG]`: install the latest release, or the one `--version` names, when it is newer than this binary (see [Upgrade](#upgrade)). `--check` only says whether a newer release exists; `--force` installs a release that isn't newer.
- `eva-md init --remote URL --dir FOLDER [--no-push]`: only the wiki step of setup. Both flags are required. It clones an empty remote or adopts an existing eva wiki, validates it, makes the first commit with your normal signing settings, pushes, and saves the local config. `--no-push` still makes the local commit.
- `eva-md validate [--dir FOLDER]`: print validation counts for a wiki. It exits with an error for errors, broken links, orphan pages, or actor and parent-index warnings. Without `--dir` it checks the configured wiki.
- `eva-md install [--host claude|codex|all] [--dry-run]`: install the plugins through each host's native commands, rolling back on failure, and link `~/.local/bin/eva-md` to the stable binary when that folder is on PATH and holds no other eva-md. It records what it made in `~/.local/share/eva-md/install-record.json`. It refuses a Codex whose hooks feature is off, also with `all`: turn the feature on, or pass `--host claude`, with Claude Code installed.
- `eva-md uninstall [--apply] [--all] [--yes]`: preview the removal of eva-md, or with `--apply` remove its plugins, the stable binary and the link, keeping the config and the local data; `--all` removes those too, after you type `delete` on a terminal, or with `--yes` without one (see [Uninstall](#uninstall)). The wiki and its remote are always kept.
- `eva-md brief [--cwd DIR] [--as startup|clear|compact|resume|fork] [--host claude-code|codex] [--session ID]`: preview the brief a new chat would get. `compact`, `resume` and `fork` need `--session`.
- `eva-md show s:<session>#<line>` or `eva-md show page[#section]`: show a captured source line, a wiki page or one section of it. `#Timeline` shows the newest 10 date groups (`--last N` or `--since YYYY-MM-DD` choose others) and `#YYYY-MM-DD` one day's group. For a system page it also prints on stderr how far the code the page cites has moved since its `as_of` day, measured in a local clone, whether the page is past its review day, and which system pages link to it; stdout stays the page. `eva-md show knowledge/map` lists the knowledge pages, and `knowledge/k<N>` shows one; `eva-md show systems/index` lists the system pages.
- `eva-md search TERMS... [--all] [--days N] [--limit N] [--cwd DIR] [--pages | --inbox]`: find earlier chat text in the inbox and lines of the wiki's thread, system and knowledge pages (`--pages` or `--inbox` searches only one). The agent runs it when it needs an earlier chat; no hook does. Each argument is a term, and a quoted argument with spaces is a phrase. It searches the current project and the general area, or every project with `--all`, over the last 90 days (`--days 0` for no limit; system and knowledge pages are always searched), and prints the 10 best hits (`--limit`, at most 50), each with a ref for `eva-md show`. A system page is found by its title, aliases and description too, and a path, a term with a slash such as `internal/x.go` or `./go.mod`, finds the system and knowledge pages that cite that file or a folder holding it, in this project's repo (any repo with `--all`). Nothing said off the record is shown. It stops after 5 seconds and says so. It reads a search index of the inbox where there is one and scans whatever the index lacks. Among equally good hits it puts lines of the wiki's pages before chat lines; then, when it used the search index, lines with rarer words, used more densely, first; last, newer ones.
- `eva-md index [--rebuild] [--json]`: bring the search index up to date with the inbox now, as the daily maintenance does, or with `--rebuild` build it again from nothing. It is optional: without it, search scans the inbox, and indexes up to 16 MiB of what is missing for the next search once 4 MiB or more is. It prints a line per project and a total, counts only, or with `--json` what the index covers.
- `eva-md inbox clear [--session ID ...] [--project KEY] [--before YYYY-MM-DD] [--apply]`: remove captured chats from the inbox, each whole, after a preview. `--session` takes a session id, the first 8 characters a ref such as `s:3f2a91c0#14` keeps, or the ref itself, once or more; `--project` a project key, the name of its folder in the inbox, which `eva-md search --all` shows on each inbox hit; `--before` the chats last written before that day. Given together, all must match, and one given an empty value is refused. Without `--apply` it lists what it would remove and how many refs in the wiki's thread pages name those chats, and changes nothing. With `--apply` it removes each chat's file with its watermark and, unless the chat is off the record, its transcript recovery record, recording the chat as removed just before its file goes, so the doctor counts those refs as removed on purpose, not lost, and then the search index of its project, which `eva-md index` or the daily maintenance builds again. A chat it leaves, as when it stops at one a hook is still writing and asks you to run it again, isn't recorded. It never changes the wiki or its history, and a chat still open is captured again from its next turn. While a background update runs it removes nothing and asks you to try again shortly. A chat whose file you deleted by hand can be recorded the same way.
- `eva-md audit capture [--session ID | --last N]`: compare what eva-md captured of each chat with the host's own record of it, and print numbers only: the prompts, final replies and tool calls found, missing and extra; exact repeats; gaps and turns with no stored reply, by cause; voice hand-overs; and chats whose host record couldn't be read, by why. `--session` takes one chat, by its id or a ref; `--last` the newest N. Without either it also counts the hosts' chats that have no eva-md file, and on Claude Code how many of them are in folders eva-md leaves out by design, though not those excluded by their remote. It reads a host's record only where transcript recovery would, or, when a chat's hooks named none or the one named is gone, the one file in the host's folder of chats named for the chat's id, under recovery's same rules; it says how many chats were paired each way, writes nothing, and counts a stretch said off the record only as paused.
- `eva-md update [--session host:id ... | --pending] [--sweep[=YYYY-MM-DD]] [--dry-run] [--foreground]`: run the background update for the sessions named (`claude-code:<id>` or `codex:<id>`, `--session` once or more) or for every session with a backlog, under one lock and the daily cap of model calls, and print one summary line. `--sweep`, which your first chat start of the day adds with its own day, also takes the day's sweep, once a day: up to 5 other sessions, of any project, idle an hour or more with a backlog, newest first, and, with no model call, up to 16 chats from before updates that couldn't be set aside yet, which it records as failed so `eva-md doctor` names them. `--foreground` runs attached with progress on stderr; without it the lines go to the day's log. `--dry-run` takes one session, prints the exact command, the names of the environment variables, the rules, the input and the schema, and makes no call. The hooks spawn it after chats; run it by hand to catch up now.
- `eva-md commit [--push-only]`: the daily commit and push now, with the guards the daily job uses. `--push-only` pushes commits made earlier. It prints one line, such as `committed (3 pages, 1 left out), push ok`, and exits 1 when the commit was refused or failed, or a `--push-only` push that ran didn't end `ok` or `up_to_date`; a run that finds another one running exits 0.
- `eva-md maintain [--push-only]`: the daily job the hooks start on the day's first event: the knowledge map and indexes built again when they are out of date, the commit and push, unless `commits = false`, then a lint of the wiki, the code status of the system pages, the removal of transcript-recovery records not saved for 30 days or past a host's newest 2,048, and the search index. Without a wiki it keeps only the search index and removes those records. With `--push-only`, which a chat start runs while a push is pending, it only pushes.
- `eva-md system write <slug> [--replace] < page.md`: write a system page from stdin, with the system map. It refuses a page with problems and names each by line. A page cites code as `<repo>@<commit>:<folder or file>`, at most 16 of them; the write checks those paths in a local clone of the repo, refuses one the cited commit lacks, and says which clone it checked, or why it couldn't. A page that lists no repo and cites no code is written with a note saying so, since no project's brief or `eva_status` then points at it. The digest skill in both plugins uses it: connect your own source of documentation to the agent, ask it to digest a space or a repo, and it writes one page per system, citing its sources. `eva-md show systems/index` lists the systems, a line each, and `eva-md show systems/map` adds each one's repos and code; the daily commit pushes the pages with the rest of the wiki.
- `eva-md system status [SLUG...] [--json]`: how far the code each system page cites has moved since its page was written, measured in the local clones eva-md knows from your chats in them, with the pages that couldn't be checked and those past their review day. It saves what it found for the prompt pointers and the doctor; the daily job runs it too.
- `eva-md doctor [--json] [--live]`: local health, the wiki folder's mode included, the native plugin inventory, a read-only check of Codex hook trust, and the state of updates, the daily commit, the push, the system pages' code, the knowledge pages and the search index. A wiki folder other accounts on the Mac can open, as one you cloned yourself keeps git's 0755 where setup makes it 0700, fails with `chmod 700 <folder>` as the fix. It has two repairs: it writes the system names and repos indexes again, and builds the knowledge map, folder index and recall index again from the pages, when they are missing or out of date. If you remove the `knowledge/` folder, the repair empties the recall index, so nothing recalls a removed page. Neither repair writes a wiki a newer eva-md set up, and neither does maintain. `--live` runs the exact headless update command once for each installed host on a small fixture, with the CLI the background update starts, a real model call that writes nothing.
- `eva-md status [--json]`: local health without asking the hosts, plus the wiki's contents (projects, threads and knowledge pages) and the update backlog. It never writes anything.
- `eva-md hook --host claude-code|codex`: the hook handler the plugins call. It is not meant to be run by hand.
- `eva-md mcp --host claude-code|codex`: the read-only MCP server the plugins start, which serves `eva_search`, `eva_show` and `eva_status` until the host closes it. It is not meant to be run by hand.

Unknown commands print "unknown command" and exit with status 2.

The plugins call a stable copy of the binary at `~/.local/share/eva-md/bin/eva-md` by its absolute path. Installing never edits your PATH or shell files: it only links `~/.local/bin/eva-md` to that binary when `~/.local/bin` is already a folder on your PATH.

## Privacy

- **Redaction before disk.** Every prompt, reply and recovered transcript line is redacted before it is written. Redaction removes the union of what two rule sets catch: a frozen set of baseline rules and the current rules. So a change to the current rules can never let through a secret the baseline rules catch. Besides provider tokens, private keys and the values of keys such as `password=` or `api_key:`, the current rules catch keys that end in `PASS` or `PWD`, such as `DB_PASS` and `MYSQL_PWD`, a MySQL or MariaDB client's `-p<password>`, curl's `-u user:password`, `sshpass -p`, `docker login -p`, `redis-cli -a` and openssl's `pass:<password>`, tokens with a known prefix such as `hf_` and `npm_`, and a password in prose, as in "my password is …", when the word after it looks like one: letters mixed with digits or symbols, never an ordinary word. Git SHAs and UUIDs are kept in ordinary text, but not as the value of a secret-looking key such as `password=`. `op://` references are kept. Redaction removes credentials, not personal details: a name, an address and the like stay in the local record, can be sent to the model, and can be written by it into pages that are committed and pushed.
- **Off the record.** Start a prompt with "off the record", or put it on a line of its own, to pause capture for that chat. Say "back on the record" to resume. Notes already captured stay saved until `eva-md inbox clear` removes the chat.
- **Exclusions.** In `~/.config/eva-md/config.toml`, `exclude = ["~/projects/private", "remote:github.com/your-org/*"]` excludes folders (path globs, where `~/` is your home folder and an entry covers everything below it) and git remotes (`remote:` globs, against the remote as eva-md normalizes it). [docs/design.md](docs/design.md) lists every config key. Chats in an excluded project are not captured and get no brief. Hooks also skip temporary folders, the wiki folder and eva-md's own state folder.
- **Local-first.** The inbox and config are stored only on your Mac. The background update sends each chat's new, redacted entries, up to 60 KB per call, to that chat's own host CLI, and so to the model provider the chat already uses; `eva-md doctor --live` sends a built-in fixture. A chat's brief, though, can quote an earlier chat from either host, so words from a Claude Code chat can reach the model a Codex chat uses, and the other way round. Only the wiki is pushed: its first commit by `setup` or `init`, then one commit a day, to the private repository you chose, or to an internal one once you agreed, when the host's admins allow no private repos or the repo you gave is internal, which everyone signed in to that GitLab, or every member of that GitHub enterprise, can read. `EVA_MD_ALLOW_INTERNAL_REPO=true` agrees in advance, for any internal repo on any host. With gh or glab signed in to the repo's host (the one the host's name says, such as gh for github.com and glab for gitlab.com, or else the other, or either when the name doesn't tell), setup also asks it about the repo, through the CLI's own sign-in and never with a token from the environment; when neither is, it tries to read the typed repo over HTTPS with no sign-in, and when that works it asks before using it (no is the default) and refuses it without a terminal.
- **Local clone paths stay local.** `eva-md show` and `eva-md system status` print the folder of the clone they measured a system page's code in, and the code status in eva-md's state folder keeps it. Nothing of it goes into the wiki.
- **Search index.** The search index holds lowercase words and line positions of the redacted, on-record inbox, never its text and never anything said off the record. It lives in eva-md's state folder with the inbox's permissions, is never committed or pushed, and can be deleted at any time. Search then scans the inbox. Once 4 MiB or more of the inbox in its scope is missing from the index, `eva-md search` indexes up to 16 MiB of it, newest first, for the next search; `eva-md index` or the daily maintenance builds the rest. The MCP server's search never writes it. `eva-md inbox clear` deletes the index of each project it removes chats from at once. A session file removed by hand has its words taken out of the index at the next `eva-md index` or daily maintenance, and the file that held them goes at the first such run 10 minutes or more after.
- **Knowledge pages and recall.** Knowledge pages live in the wiki and are committed and pushed with it; each statement cites the chat entries it came from, and a change the daily commit's secret scan would flag in any line it writes is never saved. A page you lock (`locked: true`) or mark `verified`, in any form, is never rewritten. Recall on a prompt reads local index files in the wiki, makes no network call and runs no model, and shows another project's facts only when a system page lists both projects or the prompt names that project: by its name in a chat of the same owner, by its owner's whole path too when the owner differs, as `acme/billing`, and by a local repo's key, as `local-notes-1a2b3c`. In a chat outside a repo or in a repo without a remote, the name alone names that project under any owner. A fact or procedure is saved as observed only from a command, a file or an MCP tool, never from a turn whose tool lines show a web fetch or web search; an MCP tool counts as a command whatever it reads, Codex's web searches leave no tool line, and a call whose 60 KB ends inside such a turn before its fetch can't see the fetch, so a command before it can still count, though nothing the fetch read reaches that call (see [docs/known-limitations.md](docs/known-limitations.md)). What it adds is fenced and labelled as reference notes, it never shows a line the secret scan flags, and it sends nothing off the record or in a chat a program runs.
- **No network calls in hooks.** Hooks read and write local files and run local git commands to identify the project. They never contact a server. The jobs they start detached do: the update runs the host's CLI, and the daily job pushes the wiki. eva-md itself contacts GitHub only when you run `eva-md upgrade` or the install or uninstall script, to find and download a release, which it checks against the release's `SHA256SUMS` before running it.
- **MCP tools.** The MCP server only reads: it writes no file, makes no network call, and caps what it returns at 24 KB for a search or a page, 25 KB for a whole system page, and 4 KB for a status. Claude Code may ask before each tool's first use; to skip that, add `mcp__plugin_eva-md_eva-md__*` to `permissions.allow` in your own Claude settings. eva-md never writes host settings.

## Development

To build eva-md from source, with Go 1.26 or newer, and set it up from that build:

```sh
go build -mod=readonly -o bin/eva-md ./cmd/eva-md
bin/eva-md setup
```

Setup installs the build as the stable binary, as the install command does with a release's. Its version is its commit's date, time and hash, so `eva-md upgrade` replaces it only with a release of a later day, or with `--force`.

The checks every change passes:

```sh
gofmt -l cmd internal
go vet ./...
go test ./...
go test -race ./...
```

Tests use temporary home folders and git configs, and clear any git configuration and commit identity the environment carries. They never touch your real setup. The install, upgrade and uninstall tests build eva-md as a release is built, with the go command and Go's own caches, and serve it from a local stand-in for GitHub, through a fake curl or a local test server; nothing reaches the network. If Go's default cache is not writable, set `GOCACHE` to a writable temporary folder.

The latency benchmark runs the built binary against an isolated temporary home. Targets: capture p95 under 50 ms and startup brief p95 under 300 ms. Pass a git repository outside temporary folders as `--cwd`, such as this checkout:

```sh
python3 -I scripts/benchmark.py --binary bin/eva-md --cwd "$PWD"
```

To compare two builds, `python3 -I scripts/benchmark.py --compare OLD NEW --cwd "$PWD"` runs both in alternating runs and exits 1 when the new one is slower than the machine's noise allows; a macOS job of CI runs it on every change, as a report for now. Its rule is in [docs/v2/baselines/README.md](docs/v2/baselines/README.md).

The live accuracy run, `go run ./cmd/live-accuracy --host claude-code --spend-model-quota` (or `--host codex`), sends the accuracy suite's 12 fact scripts through that host's real update command and prints the metrics against the Wiki updates stage's target. It makes 21 model calls on the host's subscription, and without `--spend-model-quota` it calls nothing. No release builds it; see [verification.md](docs/verification.md#live-accuracy-run).

### In a Linux cloud session

eva-md runs only on macOS, and setup refuses any other system, but it builds and its tests run on Linux, so you can work on it in a Linux cloud session. The tests need git 2.44 or newer and `ssh-keygen`, which Ubuntu 24.04's cloud image lacks: it ships git 2.43 and no `ssh-keygen`. This setup script for the cloud environment installs both, git from the Ubuntu Git Maintainers PPA:

```bash
#!/bin/bash
# Tools eva-md's tests need that the cloud image lacks.
apt-get update -qq || true
apt-get install -y -qq openssh-client || true
# git 2.44 or newer, from the Ubuntu Git Maintainers PPA (24.04 ships 2.43).
# Its signing key is checked before it is installed, the PPA is added only
# after that, and it is removed again if apt can't use it.
fpr=F911AB184317630C59970973E363C90F8F1B6217
key=/etc/apt/keyrings/git-core.gpg
list=/etc/apt/sources.list.d/git-core.list
tmp=$(mktemp -d) || exit 0
trap 'rm -rf "$tmp"' EXIT
export GNUPGHOME=$tmp/gnupg
mkdir -m 700 "$GNUPGHOME"
# The fingerprints of the primary keys in the file, one per line.
primaries() {
  gpg --show-keys --with-colons "$1" 2>/dev/null |
    awk -F: '$1 == "fpr" && prev == "pub" { print $10 } { prev = $1 }'
}
if curl -fsSL -o "$tmp/key.asc" "https://keyserver.ubuntu.com/pks/lookup?op=get&options=mr&search=0x$fpr" &&
  gpg --dearmor --yes -o "$tmp/key.gpg" "$tmp/key.asc" &&
  [ "$(primaries "$tmp/key.gpg")" = "$fpr" ] &&
  mkdir -p "$(dirname "$key")" &&
  install -m 644 "$tmp/key.gpg" "$key"; then
  echo "deb [signed-by=$key] http://ppa.launchpad.net/git-core/ppa/ubuntu noble main" >"$list"
  if ! { apt-get update -qq && apt-get install -y -qq git; }; then
    echo "The git PPA failed, so it is removed and the image's git kept." >&2
    rm -f "$list" "$key"
    apt-get update -qq || true
  fi
else
  echo "The git PPA's signing key could not be fetched or checked, so the image's git is kept." >&2
fi
exit 0
```

Every `apt-get update` fetches all the configured sources, here the image's archive.ubuntu.com and security.ubuntu.com and the PPA; those and keyserver.ubuntu.com are on the cloud environments' default network allowlist. The PPA's HTTPS host, ppa.launchpadcontent.net, is not, so the script uses its plain-HTTP host, ppa.launchpad.net, and apt checks the packages against the PPA's signing key. The script fetches that key over HTTPS by its full fingerprint, the one Launchpad lists for the PPA, and installs it only if the download holds that key and no other. It adds the PPA only then, and removes it again if apt can't use it, so later `apt-get update` runs still work. A setup script must exit 0, and finish within about five minutes for the environment to be cached, so nothing in it can fail the session: a mirror or key server that fails leaves the image's git rather than block it. It is safe to run again.

Then run the checks with one command from the repo root:

```sh
scripts/linux-check.sh
```

On Linux the tests need a `TMPDIR` outside `/tmp`, `/private/tmp`, `/var/tmp` and `/private/var/tmp`: eva-md's hooks skip folders under them as temporary, and `t.TempDir()` puts every test project in `TMPDIR`, which defaults to `/tmp` on Linux and is under `/var/folders` on macOS. Cloud sessions run as root, and as root about 20 tests fail by design: eva-md keeps no identity cache for root, since git's ownership checks then depend on `SUDO_UID`, and root reads and writes files whatever their modes, which the permission tests rely on. So the script makes a temporary folder of its own and, started as root, runs the tests as an unprivileged user on a snapshot of the working tree, rather than skip those tests. That user, `eva-md-check`, and the world-readable Go module cache it reads, `/var/cache/eva-md/gomod`, stay in place for the next run; remove them by hand if you ran the script with sudo on a machine of your own. CI runs the same script on Linux, but its macOS job is the test run of record.

A cloud session's disk is a fixed allowance that the checks' build caches spend; `scripts/linux-space.sh` shows what they use and `--prune` frees it. Under heavy load the benchmark can fail on sound code: run it again when the machine is quiet. AGENTS.md has the details.

Contributors and coding agents: read [AGENTS.md](AGENTS.md) first.

## Documentation

- [docs/design.md](docs/design.md): how eva-md works, and why.
- [docs/decisions.md](docs/decisions.md): the decisions the design rests on.
- [docs/verification.md](docs/verification.md): how the behaviour is checked, automatically and live.
- [docs/known-limitations.md](docs/known-limitations.md): what eva-md doesn't do, or doesn't do well: gaps still open, limits it keeps by design, and limits of the hosts and tools it runs on.
- [docs/releasing.md](docs/releasing.md): who cuts a release and how, the tag format, what the release workflow publishes, and how to verify or pull a release.

## License

MIT. See [LICENSE](LICENSE). Each release also has `LICENSES.txt`, with eva-md's license and those of the code its binaries are built with.
