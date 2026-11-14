# Setup and init of a new wiki, part 2

- **Component:** init / install / uninstall / doctor: setup and init
- **Paths:** `internal/wikiinit`, `cmd/eva-md/setup_repo.go`, `cmd/eva-md/setup_refusals.go`
- **Status:** v1 as built, frozen on 2026-11-14

The blocks below are v1's design and verification text, moved here word for word when the two documents were split; [INDEX.md](INDEX.md) lists every spec.

<!-- v1 docs/design.md 014 -->
- **Refusals.** Setup never prints, logs or keeps the messages gh, glab or git print when they refuse or fail, which can show account details; what it prints from them is only the repo address `repo view` gives for a repo it found or created.
  `repo create`'s stdout and stderr go to one buffer that keeps their first 64 KiB, never fails a write and is read only to sort a failure into a reason, by case-insensitive substrings of known English wordings, with every run of white space read as one space (glab v1.127.0 prints errors through fang
  v2.0.1, which word-wraps them at 116 columns, indents each line and pads it with spaces, so a line break can fall inside a wording), each quoted from its source in a comment (`cmd/eva-md/setup_refusals.go`); the error that travels up carries only the reason.
  The create reasons: private (or internal) visibility restricted by the host's admins (GitLab's "<level> has been restricted by your GitLab administrator"); gh's "internal repositories can only be created within an organization"; the name taken (GitHub's "name already exists on this account",
  GitLab's "has already been taken"); a project limit (GitLab's "You cannot create projects in your personal namespace" and "You've reached your limit of"); no permission in that group or organization (GitLab's namespace "is not valid", glab's "could not find group or namespace", GitHub's "You need
  admin access to the organization" and "does not have the correct permissions to execute"); the sign-in refused (GitHub's "Bad credentials", GitLab's "401 Unauthorized" and its `invalid_token` error for an expired or revoked token); and the host unreachable (gh's "check your internet connection",
  which it prints for a name that doesn't resolve, Go's "no such host", "connection refused", "network is unreachable", "no route to host", "operation timed out", "i/o timeout", "x509: " and "tls: ").
  Each prints "<cli> couldn't create <name>: <reason in setup's words>." and a next step: for private restricted, the internal question (below) and, without a yes, that neither the CLI nor you can make a private repo there, so ask an admin there to create an empty private repo and paste its address,
  keep the wiki in a local repo only your account can read (`mkdir -m 700 ~/eva-wiki.git && git init --bare ~/eva-wiki.git`, then its full path; git keeps the mode of a folder that is there, and a 0755 one in a home folder every local account can enter would let them clone it) or use another host that
  allows private repos; for no permission, a group or organization typed as `group/eva-wiki`, an owner of that group or organization, or `n` and the local repo; for a limit, a group or organization typed as `group/eva-wiki`, the host's admins on a host other than github.com and gitlab.com (on those it
  names no one), or `n` and the local repo; for a taken name, another name or `n`; for a refused sign-in, `<cli> auth login --hostname <host>` in another terminal, then the name again; for an unreachable host, the network or VPN.
  All but private restricted ask for the name again, three times at most; on the last try the step never says to type a name: for a taken name, a refused sign-in or an unreachable host it says to run setup again, and for no permission or a limit it keeps who to ask and the local repo.
  When offer then gives no repo, the address prompt asks for what the steps named (`addressHint`): once the host refused a private repo, whatever followed, and after a limit or no permission, "Paste the repo's SSH or HTTPS address, or a local repo's full path."; after a taken name, "Paste the existing
  repo's SSH or HTTPS address."; after a repo created now whose address was lost, the address of an empty private repository; otherwise "Create an empty private repository first" as before.
  Anything else prints "<cli> couldn't create <name>." as before.
  Reading a repo (setup's shallow clone, for `checkRemote` and `sameWiki`), wikigit's runner keeps git's stderr only to sort it: besides the classes a commit's record names, which are unchanged, it sets a read reason (`Error.Read`), which nothing records: not found (GitLab's "The project you were
  looking for could not be found or you don't have permission to view it", GitHub's "Repository not found", git's "does not appear to be a git repository", "repository '…' not found" and "repository '…' does not exist"), an SSH agent that declined to sign (ssherr.c's "agent refused operation", which
  OpenSSH prints before its "Permission denied"), an SSH key refused (OpenSSH's "Permission denied (publickey"), a changed host key (sshconnect.c's "REMOTE HOST IDENTIFICATION HAS CHANGED" and "has changed and you have requested strict checking") and a revoked one ("REVOKED HOST KEY DETECTED" and "was
  revoked and you have requested strict checking", or "revoked by file" for a key in ssh's RevokedHostKeys file, where an error checking that file gives no reason), checked before the line all three end with, the banner alone being enough, an unknown host key ("Host key verification failed"), no HTTPS
  sign-in (git's "terminal prompts disabled", "could not read Username", "Authentication failed"), and an unreachable host (OpenSSH's and curl's "Could not resolve host…", "Connection refused", "Connection timed out", "Operation timed out", "Network is unreachable", "No route to host", curl's "Failed
  to connect to"), checked last, after what the host answered, with white space read as on the create side. ssh's "Control socket connect(…)" line is left out first: with ControlMaster no, its default, and a ControlPath set, ssh prints it with "Connection refused" for a control socket whose master has exited, then connects without it, so it is about a socket on the Mac, never the host.
  Setup's messages name only the host of the address you gave, and the SSH hints (`ssh -T git@<host>`, with the address's user, or none when it names none, as git signs in, and its port) only for an SSH address: not found, "create it first as an empty private repo, or check the address" (when cloning
  a wiki again, "check the address"); a refused key, add the key to your account there, `ssh -T` shows which account it signs in as, or use the HTTPS address; an agent that didn't sign, approve its request and try again; an unknown host key, run `ssh -T` once and, if the fingerprint it shows is the
  host's, answer yes; a changed host key, that this can mean someone is intercepting the connection, so ask the host's admins whether the key changed and only then delete the line of known_hosts that `ssh -T` names as the offending key (ssh names it under the name and port its config gives the host,
  which the address may not), never to accept it; a revoked host key, that this can mean a stolen key is being used to impersonate the host, so ask the host's admins about its key (removing the host's lines from known_hosts would keep the revocation and lose the key ssh trusts); no HTTPS sign-in, set
  up git's sign-in for that host or use the SSH address; unreachable, check the network or VPN; a local path with no repo, "git found no repo at <path>; make one there with git init --bare, in a folder only your account can read, or give another address".
  A reason that doesn't fit the address, or none, gives "git couldn't read this repo; check its address and that git can sign in to it", as before.
  `sameWiki` puts the remote before the message.
- **Internal repos.** An internal repo is readable by everyone signed in to its GitLab, or by its GitHub enterprise.
  Setup still creates the repo private first.
  When that fails because the host's admins restrict private visibility, it asks "Create <name> as an internal repo instead?
  Everyone signed in to <host> can read an internal repo, and the wiki holds summaries of your chats. [y/N]" ("Every member of its enterprise on <host>" for gh), and on a yes runs the same CLI's `repo create --internal`, whose failure is sorted as above; a no, Enter or no answer creates nothing internal.
  An internal repo given at the question, by `--remote`, in `EVA_MD_WIKI_REMOTE`, as the folder's wiki's origin, or found under the offered name gets "This repo is internal.
  … Use it anyway? [y/N]" (or "<name> already exists on <host> and is internal.
  …"), no being the default; a no refuses it as before.
  `EVA_MD_ALLOW_INTERNAL_REPO` in the answers file answers in advance: `true` creates or uses one without asking, printing a "!" line for each, "!
  Creating <name> as an internal repo, as EVA_MD_ALLOW_INTERNAL_REPO allows: everyone signed in to <host> can read it." before it creates one, and "!
  This repo is internal, as …" or "! <name> is internal, as …" for one it uses ("every member of its enterprise on <host>" for gh); `true` covers every internal repo setup meets, on any host, not only one on a host that refuses private repos; `false` refuses one without asking; empty asks in a terminal and, without one, refuses it, saying to set the key to allow one.
  A yes typed at the question holds for that host for the rest of the run.
  A public repo is always refused.
  A Mac already set up never checks its repo's visibility (its setup only keeps the wiki or clones it again, where gh or glab is asked only for the address), so an upgrade never asks.
- **A local repo.** An absolute path to a bare repo on this Mac is a remote setup accepts (`wikigit.RemoteIdentity`), at the question, by `--remote` or in `EVA_MD_WIKI_REMOTE`.
  Setup's words make it with `mkdir -m 700` first, so only the user's account can read it; `~` is expanded in neither, so a path starting with it is refused as an address setup can't use.
  The wiki then exists only on this Mac, with a backup such as Time Machine its only copy elsewhere.
  Moving it to a hosted repo later is not a built-in step: setup refuses a remote other than the configured one on a Mac set up, so it is done by hand (`git remote set-url origin`, a push, and `expected_remote` in `config.toml`).
  Setup doesn't offer a local repo by itself; it only names it as a way on.
- The CLIs run from `/` with git's environment minus their repo, colour, debug and prompt settings, with no stdin, stderr discarded (but for `repo create`, read only to sort a failure, above) and a 10 s limit (30 s to create). No token from the environment reaches either (above). Only setup runs them (see "Changes to decisions").

It shows a summary, with one sentence of what will happen and "(created just now)" beside a repo it made, and asks "Set up eva.md with these choices? [y/N]"; every other yes/no question has a default that Enter accepts.
Then it runs the init steps below, installs the plugins (next section) and runs the health check.
Without a terminal setup itself reads no repo's content, creates no repo, clones no missing wiki again and asks nothing: a new Mac needs `--dir` and `--remote`, or both in the answers file, whose visibility is still checked, another Mac's wiki is adopted without a warning, and a Mac already set up stops on a mismatch or a missing or empty folder.

Before init pushes a branch the remote has never had, setup prints "The first commit is by <name> <email>, signed."
(or "not signed.").
That is a new wiki's first commit, or the commit of a wiki init adopts whose branch the remote has never had, as a Ctrl-C at the push question or a config moved aside after a no leaves it.
Init judges this offline: a clone has `refs/remotes/origin/<branch>` once it has fetched or pushed that branch, a clone of an empty repo has none, and any error in that lookup asks.
It reads this from `git cat-file commit HEAD` in the wiki: the author header, and whether there is a `gpgsig` or `gpgsig-sha256` header; the signature is never verified, and a commit that can't be read is said so and setup goes on.
A wiki clone can get a different identity and signing from `includeIf gitdir:` or `hasconfig:remote.*.url:` than the home folder, where the Commit signing line is read, and eva-md never force-pushes.
With a terminal it asks, with `--yes` too, which skips only the summary's confirmation, "Push it to <remote>? [Y/n]".
A no, or no answer, keeps the commit and the config, pushes nothing, and says how to amend the author (fix git's settings for the folder, then `git commit --amend --reset-author` in it); the closing lines name `<bin> commit --push-only`.
Without a terminal it pushes as before.
With `--no-push` nothing is shown or asked.
Setup's closing lines name the command as eva-md when `~/.local/bin/eva-md` is eva-md's link to the stable binary (see "Link on PATH"), and by the stable binary's full path otherwise: "Run <bin> doctor after fixing them." when the health check found a problem, and "The wiki's first commit isn't pushed.
To push it, run <bin> commit --push-only." when it wasn't pushed.
The last line, always, is "Run <bin> doctor --live to test background updates, with one model call for each host; until your first chats, many of the doctor's checks stay unknown."
Setup itself makes no model call.

1. You create an empty private repo (named eva-wiki by convention), or setup creates it, for example on GitHub, on a self-hosted GitLab or on a GitHub Enterprise server. Where the host's admins allow no private repos, it is internal once you agree, or a bare repo on this Mac.
2. `eva-md init --remote <url> --dir <folder>` (setup passes your answers) clones it, or adopts an existing eva wiki.
   Both are required: there is no default folder.
   It refuses a non-empty folder that isn't a clone, a non-empty repo without `.eva-md/schema.toml`, your home folder itself, Documents, Desktop, Downloads, iCloud Drive, cloud storage under `~/Library/CloudStorage` (OneDrive, Box, Google Drive, Dropbox), which syncs into .git as iCloud does, and eva-md's own folders, also when reached through a symlink or written in another letter case.
   A folder typed in another letter case is the same folder: init compares the clone's top folder with the chosen one by file identity, not by its spelling, and from then on uses the case git reports, the case on disk, which the config's `wiki_path` and init's result hold; its check that the config names no other wiki compares the same way.
   Two inits are kept apart by `<locks>/init.lock`: the second gets "another initialization is running".
3. It resolves the real path, writes eva-md's own log.md and root index.md, calls `okf.InitBundle`, which then writes neither, then creates README.md, general/now.md and general/threads/default.md through the adapter. It also writes `.eva-md/schema.toml` (schema_version 1, a new instance id and when it was created), `.gitattributes`, `.gitignore` and the index files.
4. It runs okf Validate with Drift on. A fresh scaffold must have no findings at all. An adopted wiki is held to what `eva-md validate` refuses: errors, orphans, broken links, and warnings about an invalid actor or a parent index.
5. It makes the first signed commit and, unless `--no-push` or the caller declines (setup's push question), pushes with an upstream set. Running init again after a failed commit is safe: it accepts the scaffold it wrote, ignores Finder's `.DS_Store` files and names any other unexpected file.
6. It writes `~/.config/eva-md/config.toml`: wiki path, expected remote, instance id, machine label (the sanitized host name), notice mode, `updates`, `commits`, `mcp`, `recall`, daily cap, `codex_ignore_user_config` and `reminder_every`, and excludes and effort when they are set.

<!-- /v1 -->

## Must keep

The must-keep items of [findings.md](../v2/findings.md) this component touches, numbered from K01 in each list, and the tests that hold them.
An item that tests hold only in part ends with what of it no test holds, as `(untested: ...)`.

| Id | Item | Tests |
| --- | --- | --- |
| SET-K01 | Nothing gh, glab or git prints is shown, logged or kept; failures are sorted into reasons in eva's words | cmd/eva-md:TestSetupNeverPrintsWhatGitPrinted, cmd/eva-md:TestSetupSaysWhyACLICouldntCreateTheRepo, cmd/eva-md:TestPeekRemoteSaysWhyHTTPCantRead, cmd/eva-md:TestPeekRemoteSaysWhySSHCantRead |
| SET-K02 | No token from the environment reaches gh or glab, and each call names one host | cmd/eva-md:TestSetupNeverGivesTheCLIsATokenFromTheEnvironment, cmd/eva-md:TestSetupAsksGlabAboutAKnownHostDirectly |
| SET-K03 | A public repo is always refused, an internal one only on an explicit yes | cmd/eva-md:TestSetupAlwaysRefusesAPublicRepo, cmd/eva-md:TestSetupAsksAboutAGivenInternalRepo, cmd/eva-md:TestSetupAsksAboutARepoAnyoneCanRead, cmd/eva-md:TestAnyoneCanReadFailsClosed |
| SET-K04 | The probe made without signing in cannot sign in | cmd/eva-md:TestProbeCannotSignIn |

## Since the split

No change yet.
