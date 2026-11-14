# Setup and init of a new wiki, part 1

- **Component:** init / install / uninstall / doctor: setup and init
- **Paths:** `internal/wikiinit`, `cmd/eva-md/setup.go`, `cmd/eva-md/setup_checks.go`
- **Status:** v1 as built, frozen on 2026-11-14

The blocks below are v1's design and verification text, moved here word for word when the two documents were split; [INDEX.md](INDEX.md) lists every spec.

<!-- v1 docs/design.md 013 -->
## Setup and init of a new wiki

`eva-md setup` is the first-run experience, run in a terminal. It starts with a checklist, before any question, so a person fixes everything first and runs setup again:
- **Checking this Mac.** A block headed "Checking this Mac" runs the checks in order and stops at the first that fails: macOS (`runtime.GOOS`); git on PATH; the host apps `--host` or `EVA_MD_HOSTS` asks for (the `claude` and `codex` commands; for `all`, at least one, and only those found get the
  plugins); the answers file, when present, read without errors (a bad value, a remote setup can't use, a link, a folder, a file over 16 KB or not UTF-8); and on a Mac not set up yet, when the wiki's host is known, the CLI for it (below).
  Each check that passes prints "✓ <what was found>"; the first that fails prints "✗ <what is wrong>", then an indented "Fix: <the exact command or step>" and "Then run setup again.", and setup exits 1 having asked and changed nothing after it.
  The marks are coloured (green ✓, red ✗, yellow !) only when stdout is a terminal and `NO_COLOR` is unset or empty; otherwise the text is the same without escape codes.
  Lines name only the paths and host names a person typed to name their folder, repo and host, never another value from the answers file.
  Commit signing is information, read from the home folder as git reads a boolean (`git config --get --type=bool commit.gpgsign`, so true, yes, on, 1 and a bare key are on), with `gpg.format` naming the kind, `openpgp` when unset: "✓ Commit signing on (<kind>)", or "!
  Commit signing off: …", which never stops setup.
  Last come install's own checks, on every run: install as a preflight (`install.Options.Preflight`), a dry run that needs no config yet, with this binary and the hosts setup will install for, within the 2 minutes an install has, so that anything install would refuse before it changes anything stops
  setup there, before it asks anything or makes or pushes a wiki: Codex's hooks feature off, a host command that fails, another eva-md registration, a Codex marketplace registered without its folder, a registration that needs attention, or eva-md's folders holding files it didn't make.
  It passes as "✓ The plugins can be installed for <apps>".
  Codex's hooks feature off is "✗ Codex's hooks feature is off, so eva-md can't capture Codex chats", with "Fix: run codex features enable hooks, or set EVA_MD_HOSTS=claude in <answers file> to install for Claude Code only", or "run setup with --host claude" when `--host` chose the hosts; on a Mac
  with no `claude` command, which setup looks for whatever `--host` chose, the second way starts "install Claude Code, then".
  A host command that fails says to run that command, whose own words install never shows; any other refusal is "✗ The plugins can't be installed: <what install says>", with install's fix, or for a conflicting registration, to remove it with that app's plugin commands, and otherwise "fix that first; setup makes no wiki until install's checks pass".
  A broken `config.toml`, and flags setup can't parse, stop it before the block, as before.
- **The wiki host's CLI.** The host is the remote's, from `--remote` or `EVA_MD_WIKI_REMOTE`, or else the git host, from `--git-host` or `EVA_MD_GIT_HOST_URL`; a local remote has none.
  A remote on GitHub's or GitLab's SSH host, ssh.github.com or altssh.gitlab.com, on any port, is read as on github.com or gitlab.com before the name rule (below); a git host is used as it is named.
  An alias from `~/.ssh/config` is never resolved: ssh reads that file from the account's own home folder whatever `HOME` says, so setup neither reads it nor runs `ssh -G`.
  The CLIs are asked as "GitHub or GitLab" (below) says.
  One signed in there passes ("✓ glab signed in to gitlab.example.com"); both pass in a terminal, where setup asks later which it is, and fail without one ("Fix: run setup in a terminal to choose").
  With none ready and only the git host, setup would have to create the repo and can't, so the check fails with the fix for the CLI the host's name says: "✗ glab isn't installed" with "Fix: install glab (brew install glab), then run glab auth login --hostname <host>", "✗ glab isn't signed in
  to <host>" with "Fix: glab auth login --hostname <host>", or one that didn't answer in time; for a name that says neither, a plain git server, "✗ Setup can't create the repo on <host>: no gh or glab is signed in there" with "Fix: create the repo yourself and give its address in EVA_MD_WIKI_REMOTE or --remote".
  With none ready and a remote given, the CLI is no prerequisite: a "!" line says what isn't ready and that "setup will check the repo is private by reading it without signing in", such as "! gh isn't signed in to github.com: …" or "!
  No gh or glab is signed in to <host>: …", and setup goes on with the read with no sign-in below, as it did before it had checks.
  With no host known yet, that is, with no remote and no git host given, as in a terminal with no answers, the block lists as information which of gh (at github.com) and glab (at its default host) are installed and signed in where, saying "glab can't name its default host" when `glab config get host`
  fails or names one setup can't use, and setup goes on; what they said is kept, so the remote question asks neither again.
  A Mac already set up skips this check and these lines: there gh or glab only reads, when it can, the address to clone the wiki again from.

Then it looks before it asks, and asks only what it can't work out:
- **A Mac already set up** keeps its wiki.
  A healthy one prints "eva-md is set up here: …" and goes straight to the plugins question, with no summary.
  A missing or empty wiki folder (one holding only Finder's `.DS_Store` counts as empty) has its repo read, as a chosen repo is, before the question below, or after a yes to it when no is its default: an empty repo, or one with another eva wiki, stops setup, since cloned again either would become a new wiki in place of the configured one; init refuses that too.
  Otherwise it gets "Clone it again from <address> into <dir>? [Y/n]", defaulting to no when the folder's parent is missing too; that yes is the only confirmation, the clone is never pushed, and the address is `--remote`, a local remote's path, the address gh or glab gives for the same repo, or else `git@<host>:<path>.git`.
  Any other mismatch (not a clone, another origin, no schema, another instance, a file in the folder's place, a folder that can't be read) stops with one sentence ending "restore your wiki there, or move <config> aside to start over".
  A `--dir` or `--remote` that would change the wiki is refused.
- **A new Mac** gets the two choices that have no default: the folder for the wiki, and the remote it is pushed to.
  A folder that already holds an eva wiki is offered for use, unless it has a commit and uncommitted changes, which init would refuse (setup first removes the lock file a save killed midway leaves in the wiki's root, which is never yours, and says so instead when it can't, or when the wiki stays
  busy); a wiki whose first commit failed, as when the signature was declined, has its scaffold staged and no HEAD, so it is offered ("Use it? [Y/n]"), and init then checks the scaffold, stages it and makes the first commit; a repository with commits that isn't an eva wiki, a clone or not, and one
  with no commits and no remote, are refused, and so is an eva wiki with no remote (add it back with `git remote add origin <address>`), with a remote setup can't use (point it at the wiki's repo with `git remote set-url origin <address>`), with a local remote path that isn't there now, as on a volume
  that isn't mounted (mount or restore it), or whose schema names no instance; a `file://` remote is one setup can't use.
  A folder holding only Finder's `.DS_Store` counts as empty.
  A chosen repo is read before the summary, from a shallow copy in a temporary folder (`git clone --no-local --depth 1 --no-checkout`, with only `.eva-md/schema.toml` checked out, removed afterwards), so the wiki folder is never touched: an empty repo gets a new wiki, an eva wiki made on this Mac is
  used, one made on another Mac, by its machine label compared without the domain, asks "Use it? [y/N]", and other content or a wiki of a schema this eva-md can't use is refused.
  A git that runs out of time reading the repo fails the read, saying so, so a repo that is slow to read is never taken for an empty one or for one with other content.
- **The answers file.** `config.env` in eva-md's config folder (`~/.config/eva-md/config.env`, beside `config.toml`, which stays eva-md's own settings file that setup writes) holds answers a person fills in for setup.
  It is read strictly: a regular file, never through a link (`O_NOFOLLOW`, `O_NONBLOCK`), at most 16 KB, UTF-8.
  Lines are `KEY=value`; blank lines and lines starting with `#` are skipped; a leading `export ` is allowed; the value is everything after the first `=`, trimmed, with one pair of matching double or single quotes removed, and taken literally, with no escapes and no variable or command expansion.
  A key given twice keeps its last value.
  The keys are `EVA_MD_WIKI_DIR` (as `--dir`: a full path or `~/...`), `EVA_MD_WIKI_REMOTE` (as `--remote`), `EVA_MD_GIT_HOST_URL` (as `--git-host`, which takes the same values: a web address, `https://` or `http://`, with any path, query or fragment, such as a profile or projects page, or a bare host
  name with or without a path; setup uses only the host, in lower case, keeping a port and dropping a trailing dot, with www.github.com and www.gitlab.com read as github.com and gitlab.com; a user name or password in the address, another scheme or a repo's address, `ssh://` or `git@host:path`, whose
  message says a repo's address goes in `EVA_MD_WIKI_REMOTE`, and an empty or malformed host are refused, naming the key or the flag, never the value), `EVA_MD_HOSTS` (as `--host`), `EVA_MD_ALLOW_INTERNAL_REPO` (`true` or `false`, any case, the answer in advance to the internal question; see "Internal
  repos" below; written empty in the template and in the file setup fills in) and `EVA_MD_CODEX_IGNORE_USER_CONFIG` (`true` or `false`, any case, sets `codex_ignore_user_config` in `config.toml` after the plugins are installed, saying so when it changes it, and refusing to rewrite a config that holds keys eva-md doesn't know).
  An empty value is an answer not given, and a flag wins over the file.
  A key setup doesn't know is named, never its value, in one line on stderr and otherwise ignored; only a key shaped like setup's own (`EVA_MD_` then capitals, digits and underscores) is named, and any other line by its number, since it may hold a secret pasted by mistake, such as a base64 token whose
  `=` padding makes it look like `KEY=`; a bad value (an unknown host name, a switch that isn't true or false, a git host address it refuses, a relative folder, a remote setup can't use, or a line that isn't `KEY=value`, named by its number) stops setup with a message naming the key or line and the file.
  A remote that is a local path that isn't there now gets its own message, "<key> in <file> names <path>, which isn't there now; mount or restore it, or give an SSH or HTTPS address"; on a Mac already set up such a path is no error, and is matched with the configured remote by its path, as a missing local origin is, so a volume that isn't mounted never stops setup there.
  On a new Mac, the file's folder and repo are setup's answers: neither question is asked, the summary adds "Answers from <file>" and the confirmation is asked as before; `--yes` and a run without a terminal treat them as `--dir` and `--remote`.
  When the file is missing on a new Mac, setup writes the template (0600, `O_EXCL`, so it never replaces anything, a link included) once the checks before it pass, and says so in the checks' next line, "✓ Wrote <file>, a template for setup's answers", so a later check that stops setup never leaves it
  unsaid; in a terminal with a question to ask it adds "Fill in <file> and run setup again, or answer here." and asks as before; without a terminal and without `--dir` and `--remote` it stops (exit 2) saying it wrote the template.
  On a Mac already set up, a missing file is written after a successful setup (once the plugins are installed), filled in with what this Mac now uses: the wiki folder as the config has it, under `~/` when it is in the home folder; the address its clone's origin is pushed to, or the remote setup used,
  when it names the configured repo, but never a local remote, left empty so that a volume that isn't mounted never stops a later run; the git host that address names, as `EVA_MD_GIT_HOST_URL=https://<host>`; the hosts as `all`, the template's value, since a flag such as `--host` is for its run only; and the Codex switch as `config.toml` has it, `true` or `false`.
  It is written as the template is, 0600 and never over anything, in the template's words with a header saying setup filled it in, a value quoted when reading it back would change it and one with a line break refused, and setup says so in one line.
  On a Mac already set up, a folder or repo in the file must name the configured wiki and remote, as the flags must, and a mismatch stops setup naming the file and the key.
- **GitHub or GitLab.** The host's name says first, ignoring a port and letter case: github.com, a name holding `github` (as a label or part of one) or one ending in `.ghe.com` is GitHub, worked with through gh pointed at that host; gitlab.com or a name holding `gitlab`, such as gitlab.example.com or code-gitlab.example.org, is GitLab, through glab pointed at that host.
  When the name tells, setup asks that CLI first, and the other only when that one isn't ready there; one signed in there is used, so a GitHub Enterprise server whose name holds gitlab, or a GitLab whose name holds github, works with the CLI signed in to it.
  When the named CLI is ready, the other is never run, and when neither is, the check's line names the CLI the name says (above).
  A name holding both, or neither, is unclear: setup asks `gh auth status --hostname <host>` and `glab auth status --hostname <host>` at once, each within its time limit.
  When exactly one is signed in, setup uses it; when neither is, the host is a plain git server; when both are, it asks "Is it GitHub or GitLab? [gh/glab]" in a terminal, after a line saying both are signed in and with "Type gh or glab" after an answer it doesn't understand, three of which stop setup; without a terminal it stops saying it can't tell.
  Every call about the repo points the CLI at the host, `GH_HOST=<host>` for gh and `GITLAB_HOST=<host>` for glab, in that CLI's environment for that call only, never in eva-md's own; glab is asked about a known host directly, `glab auth status --hostname <host>` with `GITLAB_HOST=<host>`, and never
  for its default host then; `glab config get host`, which gets no host from setup, is asked only for the information lines when no host is known.
  No token from the environment reaches either CLI, on any call and whatever the host: `GH_TOKEN`, `GITHUB_TOKEN`, `GH_ENTERPRISE_TOKEN`, `GITHUB_ENTERPRISE_TOKEN`, `GITLAB_TOKEN`, `GITLAB_ACCESS_TOKEN`, `OAUTH_TOKEN` and `CI_JOB_TOKEN` are left out of its environment, so each uses only its own
  sign-in (`gh auth login`, `glab auth login`), which `auth status --hostname <host>` checks, and a token meant for one host is never sent to another that setup points the CLI at.
  This choice is made wherever setup picks a CLI: to check a typed or `--remote` repo, to read a repo's address to clone a missing wiki again (where the question, its explanation and its hint go to setup's output, and running out of answers stops setup with nothing cloned), and to offer to create the repo.
- **Creating the repo.** With a git host given (`--git-host` or `EVA_MD_GIT_HOST_URL`), setup offers to create the repo only there, with the CLI chosen as above, which the checks have found ready.
  The repo goes under the account that CLI is signed in as, named `eva-wiki` or what the user types, such as `group/eva-wiki`; the path of the git host's address is never used.
  Without one, when gh (github.com) or glab (its default host) is installed and signed in, setup offers to create a private repo there, `eva-wiki` or a typed name; with both, "Which one? [GH/glab/n]", where Enter picks gh.
  An existing repo of that name is used when it is private, or internal after the internal question below, at once when empty and after "use it as your wiki? [Y/n]" and the read above when not; a public one is refused, and so is an internal one without a yes.
  Setup reads back the SSH address, or HTTPS when that CLI's git protocol is https, and then says how git can sign in.
  Any failure, a timeout included, falls back to the address prompt with one line saying why, or, for a refusal setup recognises (see "Refusals" below), to the name question again when another name or a fix can work.
  A create that timed out counts as possibly made, and when setup stops after making a repo it says the repo stays for the next setup. glab's default host is the one `glab config get host` names, from `GITLAB_HOST` or glab's own config, or gitlab.com when neither sets one; for a self-hosted GitLab, name it as the git host instead.
  When HTTPS is gh's protocol on a host other than github.com, the sign-in hint is `gh auth setup-git --hostname <host>`.
- **A typed or `--remote` address** is refused when the CLI for its host, chosen as above, says the repo is public, and asked about when it says internal (see "Internal repos" below); a remote on ssh.github.com or altssh.gitlab.com is asked about on github.com or gitlab.com.
  When no CLI is signed in to the host, git tries to read the repo over HTTPS with nothing that could sign it in, within 10 s: none of your git config, so no credential helper, header or URL rewrite; an empty home folder, so no `.netrc`; no Kerberos ticket and no empty-user authentication that would offer one; no proxy and no prompt.
  Setup says first which repo it checks ("Checking whether <host/path> can be read without signing in.").
  A repo it reads may be public: with a terminal setup asks "Use it anyway? [y/N]", since a host can let its own network read private repos too, and without one it refuses it.
  An internal repo there, and local paths, are not checked.

<!-- /v1 -->

## Must keep

The must-keep items of [findings.md](../v2/findings.md) this component touches, numbered from K01 in each list, and the tests that hold them.

| Id | Item | Tests |
| --- | --- | --- |
| STO-K15 | The user's signing and sign-in settings are never changed, and a failed signature is not retried in a loop | internal/wikigit:TestDeclinedSignatureFails, internal/wikigit:TestMissingSignerFails, internal/wikiinit:TestSignatureFailureIsNotRetried |
| STO-K16 | A commit the remote never had is shown before the first push | internal/wikiinit:TestBeforePush |
| SET-K05 | A chosen repo is read from a temporary shallow copy, and a slow read is never taken for an empty repo | cmd/eva-md:TestPeekRemote, cmd/eva-md:TestPeekRemoteOutOfTime, cmd/eva-md:TestSetupUsesARepoWithContent |
| SET-K06 | A missing wiki folder is never cloned again from an empty repo or another wiki | cmd/eva-md:TestSetupClonesAMissingWikiAgain, internal/wikiinit:TestInitKeepsTheConfiguredWikiOfAnEmptyRepo |
| SET-K07 | Install's checks run before any wiki is made or pushed, and a failed install rolls back | cmd/eva-md:TestSetupStopsWhereInstallWouldRefuse, cmd/eva-md:TestSetupChecklistStopsAtTheFirstProblem, internal/install:TestPreflightNeedsNoConfig, internal/install:TestInterruptRollsBackInstall |
| SET-K08 | The first commit's author and signing are shown before the first push; no force push, no retried signature or push | cmd/eva-md:TestSetupShowsTheFirstCommitBeforePushing, internal/wikiinit:TestBeforePush, internal/wikiinit:TestSignatureFailureIsNotRetried |
| SET-K09 | Only a standalone clone at its root whose remotes name the requested one gets the scaffold, and only its paths are staged | internal/wikiinit:TestRefusals, internal/wikiinit:TestGitIgnoresInheritedRepository, cmd/eva-md:TestSetupRefusesAFolderThatIsAnotherClone |
| SET-K10 | The wiki folder is not home, a protected or synced folder, or eva-md's own, judged through links and case; it is 0700 | internal/wikiinit:TestProtectedLocations, internal/wikiinit:TestCheckFolder, internal/wikiinit:TestInitAcceptsAFolderTypedInAnotherCase, cmd/eva-md:TestSetupRefusesProtectedFolderFlag |
| SET-K11 | A rerun after a failed first commit resumes; .DS_Store is tolerated; a leftover save lock is removed first | cmd/eva-md:TestSetupResumesAWikiWhoseFirstCommitFailed, internal/wikiinit:TestRerunAfterFailedSignatureToleratesDSStore, internal/wikiinit:TestAFolderWithOnlyDSStoreIsEmpty, internal/wikiinit:TestAdoptionRemovesTheSavesLockFile |
| SET-K19 | Setup makes no model call | none yet |
| SET-K20 | Every time limit of setup is a variable tests raise once | cmd/eva-md:TestMain, internal/identity:TestMain |

## Since the split

No change yet.
