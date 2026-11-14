# eva.md v1: overview

- **Component:** eva-md as a whole: its summary, how it works and its parts
- **Paths:** `cmd/eva-md`, `internal`
- **Status:** v1 as built, frozen on 2026-11-14

The blocks below are v1's design and verification text, moved here word for word when the two documents were split; [INDEX.md](INDEX.md) lists every spec.

<!-- v1 docs/design.md 001 -->
## eva.md v1 design

This is the design of eva.md v1.
Capture, resume and wiki updates are built, with system pages, standing rules, knowledge pages, pointers and prompt recall, and so are support for a wiki on a self-hosted git server, the release flow, and the install, upgrade and uninstall of a release.
They are checked offline; no live update has run yet, and the live checks still owed are listed in [verification.md](../verification.md).
Parts marked "planned" are designed but not built yet.
Host behaviour not yet observed is listed under [Open host questions](hosts.md#open-host-questions).
The agreed decisions behind it are in [decisions.md](../decisions.md), and what is checked is in [verification.md](../verification.md).

**Names:** the product is eva.md. The technical name is `eva-md` (binary, plugin, state folders). The wiki repo is `eva-wiki`.

---

## Summary

eva.md gives Claude Code and Codex memory that works without your help. When you open a new chat, run /clear, or the context compacts, and you ask "where did we stop?", the agent carries on as if it were the same chat.

How v1 does it:

- **Capture.** Hooks on both hosts save every prompt, every final reply, one line for each file change, command, read, search, web fetch and MCP call (by what it acted on, never its output), and the agent's plan updates. Secrets are redacted before anything is written. Entries go to a local inbox on each machine that is never committed.
- **Resume.** At the start of every chat, after /clear and after compaction, code (not a model) builds a short brief. It contains the branch's Now section, the last exchanges word for word, the open plan and the live git state, marked as data. So "where did we stop?" works even if no model has run yet.
- **Background update.** After a chat, a detached job runs the same host and model as the chat, headless.
  It rewrites the branch's Now section, appends dated Timeline lines, keeps the standing rules the user stated and promotes lasting facts, decisions, procedures and constraints into knowledge pages.
  The model returns strict JSON.
  Code then checks it before anything is written: every point cites its source entry, dates and speakers come from the entries, and no open item disappears without a reason.
- **System pages.** An agent digests documentation and code into one page per system through `eva-md system write`, with a map. Briefs and prompts that name a system point at its page.
- **Knowledge and recall.** A knowledge page holds one lasting statement with its sources and history.
  A prompt that strongly matches one gets the statement itself, fenced as reference notes, at most 3 a prompt; a new chat's brief lists the project's constraints.
  The brief reads one local index file, and recall reads it too, with the small repos index when it needs to know which projects share a system page; neither runs a model.
- **Storage.** Pages are written through okf, a Go library compiled in at a pinned version (v0.8.0), behind one adapter. The wiki is committed once a day with one signing attempt and pushed right after.

v1 is one Go binary plus okf, two static plugins with two skills and one MCP server, and about ten hook events.
`eva-md search` reads a rebuildable index of the inbox and scans what the index lacks and the wiki's thread, system and knowledge pages, and the binary's read-only MCP mode offers that search, `eva-md show` and a project status as tools.
Knowledge pages, the recall skill and prompt recall were built at the user's request rather than after two weeks of use (see "Changes to decisions").

Facts about okf and the hosts that shaped the design:
- okf already lets the caller set the author.
- okf rewrites a page's timestamp on every save.
- okf doesn't fsync the folder after writing.
- okf's index is unsorted and its log is newest-first in UTC, so eva-md writes both files itself.
- The author string must have no spaces.
- Claude plugin versions are strings.
- Codex's shell tool is called `Bash`.
- Codex's headless run cannot turn every tool off, so eva-md rejects any run that used a tool.

---

## How it works

```
Claude Code (CLI, desktop Code tab)            Codex (CLI, ChatGPT app Codex mode)
 plugin eva-md (own local marketplace)           plugin eva-md (own local marketplace)
   hooks/hooks.json (exec form)                    hooks/hooks.json (shell string, || echo '{}')
          \____ ~/.local/share/eva-md/bin/eva-md hook --host <h> ____/
                one Go binary, short-lived, always exits 0, no model, no network
     |                        |                                   |
  capture                 brief (built by code)            spawn detached (setsid)
  redact -> append JSONL  Now + last exchanges             eva-md update  (SessionEnd, PreCompact,
  ~/.local/state/eva-md/  + open plan + live git             60 KB backlog, 30-min gap, catch-up)
   inbox/<project>/       fenced, <= 8,000 (Codex 6,000)   eva-md maintain (first event of the day:
   <host>-<session>.jsonl -> additionalContext               commit + push, lint, code status, index; --push-only retries)
   (0600, never committed)                                        |
                                                                  v
          eva-md update: lock -> numbered entries -> same host + model, headless,
          no tools, JSON schema (claude -p ... | codex exec ...) -> validate in code
                                                                  |
                         okf adapter (the only code that calls okf):
                         lock, no-clobber create, compare-and-swap, skip unchanged,
                         actor check, body pre-check, folder fsync, own log + index
                                                                  v
      <wiki folder chosen at setup> (a private repo, for example on GitHub or a self-hosted GitLab)
        projects/<p>/now.md (overview, built by code)  projects/<p>/threads/<branch>.md (Now + Timeline)
        projects/<p>/rules.md, general/rules.md (standing rules)   systems/<slug>.md + systems/map.md
        knowledge/k<N>.md + knowledge/map.md (lasting facts, decisions, procedures, constraints)
        general/   index.md files (sorted, no timestamps)   log.md (append-only, union merge)
        .eva-md/schema.toml, .eva-md/names.tsv, .eva-md/repos.tsv, .eva-md/knowledge.tsv (the recall index)
                                                                  |
          daily: git add <paths> -> leave out inbox/tmp files and secret hits -> git commit
          (your signing, one approval) -> git push (retried later, never forced)
```

All of this is built: capture, the brief, eva-md update with its triggers and page writes, system pages, knowledge pages with prompt recall, and eva-md maintain with the daily commit and push.

Rules that hold it together:
- Hooks only append, read and spawn. The only model call is in a detached job, and code checks its output before anything is written.
- The resume path reads the inbox and git directly, so it never waits for the update.
- Nothing stays running. Every process starts from a host event and exits; the MCP server lives only as long as the chat that started it.
- The inbox is the word-for-word record and is stored only on this Mac; the background update sends each chat's new, redacted entries to that chat's own host CLI, and so to the model provider the chat already uses. The wiki is the curated view and is committed daily. Pending work is worked out from per-session watermarks, never queued.
- Wikis are kept apart by a per-machine config, a remote guard and an instance id stored in both the wiki and the config.

---

## Components

| Component | What it does |
| --- | --- |
| **eva-md binary** | One Go executable: CLI and hook handler, the updater, the daily job and system page writes, and the read-only MCP server (see "MCP server"). Installed at `~/.local/share/eva-md/bin/eva-md` with an atomic replace. Non-standard dependencies: `github.com/okf-memory/okf-agent-memory/pkg/okf` (pinned v0.8.0) and a TOML parser. Only `internal/okfadapter` imports okf. Init, validate, the doctor and the brief's page reads go through it; capture never does. |
| **Static plugins** (one per host) | Connect the hook events to the binary through an absolute path generated once per machine. The plugin folders are byte-identical across binary-only releases. Generated from embedded templates into two separate local marketplace folders, each with the digest and recall skills (see Packaging). |
| **Hook dispatcher** | Reads the payload and applies the guards (see Capture). Resolves the project from the payload's `cwd`, then redacts, appends, builds briefs, adds system pointers, recalled knowledge and the reminder to prompts, writes heartbeats, and spawns the update job and maintain detached. Internal deadlines: 1.5 s for SessionStart and for PostToolUse and PostToolUseFailure, which the plugins mark async, so Claude Code doesn't wait for them and a burst of parallel tool calls waiting its turn for the session lock is no failure (a host that did wait would wait longer on a contended lock, where it used to lose the line); 300 ms for every other event. Never exits 2. Prints nothing except a brief, a prompt's system pointers, recalled knowledge or memory reminder, a notice (including the chat-start line for a config that can't be read), an off-the-record confirmation, or `{}` on Codex Stop. |
| **Identity resolver** | Hardened git reads (`-c core.fsmonitor=false -c core.hooksPath=/dev/null --no-optional-locks`, with prompts, optional locks and lazy fetches off through `GIT_TERMINAL_PROMPT=0`, `GIT_OPTIONAL_LOCKS=0` and `GIT_NO_LAZY_FETCH=1`, and git's pathspec settings, such as `GIT_GLOB_PATHSPECS`, left out, since the code reads name paths with `--literal-pathspecs`, which git refuses beside them; 800 ms timeout). Finds the worktree root, the branch (from its full ref name, so a tag of the same name changes nothing) and the normalized origin as configured in `remote.origin.url`, without `insteadOf` rewrites: credentials stripped, host lowercased, `.git` removed. A repo without a remote is identified by its folder. Anything outside a repo goes to general. Results are cached per worktree in `~/.local/state/eva-md/cache/identity/`, one 0600 entry per worktree (or per folder outside a repo). A hook reuses an entry, with no git process, only while a fresh look at what git reads is unchanged: the folders' identity and owner, the `.git` file, HEAD, every config file git reads (system, global, the repo's, `config.worktree` and their includes), packed-refs and the branch's loose ref. The project key also depends on the redaction rules (see "The inbox"), which an upgrade can change while git's files stay the same, so each hit derives the key again from the entry's remote or folder, and an entry whose key differs resolves live and is replaced. Anything unusual, such as a symlinked path, `core.worktree` or another ref named HEAD, resolves live. |
| **Redactor** | Versioned rules (`redact.Version` 5). The text removed is the union of what the baseline rules (the frozen original rules in `internal/redact/baseline`) and the current rules remove, so no rule change can keep a secret the baseline rules remove. That is the default: a switch in `config.toml`, `redaction_current_rules_only`, which is off and belongs to v2's build (docs/v2/plan.md, decision 7), runs the current rules alone, and with it on a secret in some shapes of code that the baseline rules remove is kept. Each current rule runs only when cheap hints find what a match needs, such as a `pass` key followed by `=` or `:`, or `mysql` or `curl`, so a text without them costs a few substring scans; a command rule reads each command once, word by word, so its cost grows with the text however many command names a line holds. Identity fields (project keys, branch names) get format-only checks without the entropy rule. Used at capture, on model output and on the staged diff before each commit. That scan (`wikigit.SecretLines`) judges each line alone and flags it when redaction would replace anything but identifier-shaped words, all-caps parts such as issue keys and words ending in digits counting as words; system page writes, `eva-md show` and search snippets judge wiki lines the same way. Fails closed: an entry is stored without its text rather than unredacted. Logs keep counts only. |
| **Inbox store** | Append-only JSONL, one file per (project, host, session). Written under flock with O_APPEND, with fsync on user and assistant entries. A byte-offset watermark per file, marked "more" when the last update pass stopped inside a group, and with the file's size when the update found the rest too small to process yet. Per-session marker files: off the record, briefed, model, thread, and `prompts`, the per-prompt pointer state, kept apart from briefed because it changes on every prompt and is written in place (see "Per prompt"). |
| **Catch-up reader** | Reads a known transcript forward from a saved cursor, one 1 MiB window per pass, saving the cursor after every row, so a pass that runs out of time resumes where it stopped. A longer row is scanned in bounded chunks, honours a record control and leaves a gap event. A row of 256 KiB or more records an attempt before it is decoded. A shorter row whose decoding uses up a pass is marked slow, so the next pass loads the stored entries before decoding it and can skip or bound it. Claude user rows count as prompts only when they carry a promptId and no host wrapper; meta rows, compaction summaries, sidechains and synthetic loader rows are skipped. Codex is best-effort, because its transcript format is not a stable interface. Entries are marked `origin=catchup` and deduplicated by turn id or a text hash. Recovered rows are the main thread's and carry no agent id. A Codex `turn_context` whose cwd differs from the registered folder marks the chat as elsewhere (the recovery state's `elsewhere`): recovery reads on but stores none of that turn's rows, still honouring an off-the-record control in them, in the same pass or a later one, and stores again from a `turn_context` back in the registered folder; a registration that starts a new baseline clears the mark. Recovery is blocked only while the chat is off the record: a registration keeps a block only when the off marker is set or the block's reason is `off_record`, and a block an older version saved for a turn in another folder is lifted at the next registration, which starts again at the transcript's end. A transcript shorter than the cursor, as one truncated or rewritten in place, starts again at its end when a hook next registers the chat (at most once per 10 seconds for an unchanged registration); until then a pass fails with `transcript replaced or truncated`, which the hook treats as caught up. |
| **Brief builder** | Picks the previous chat, builds the startup, compact, resume and refresh variants within per-host budgets, shows the standing rules, the project's constraints and the Now by priority, adds the opening request, earlier chats, other branches, tool activity, live git, freshness lines, system pointers and the footer, and computes the data fence. |
| **Updater** (built, with its triggers) | One job at a time under its own lock, draining each session's backlog in chunks, at most 40 model calls per host per day. Resolves the chat's model, builds the prompt from numbered entries, runs the host CLI headless with a JSON schema, validates the result, writes the knowledge, rules and thread pages and records every pass in a daily run log. |
| **Validator** | Deterministic checks before writing (`Validate` in `internal/update`): strict schema, refs, speaker tags, pasted text, sourced dates, item accounting, standing rules, knowledge, idempotency, out-of-order guard, locked pages, caps, escaping, and redaction again. |
| **Renderer + okf adapter** | The renderer (`internal/threadpage`, built and tested, used by the update job) turns the old page plus validated operations into the new page. The Now section is rewritten and the Timeline is only appended to. The adapter is the only code that calls okf (next section). |
| **Maintain / committer** | `eva-md maintain` builds the files listing the knowledge pages again when they are out of date, then runs the daily commit and push with all guards (`internal/wikigit`, which also holds the hardened git runner init uses), then the wiki lint (`internal/lint`), the code status of the system pages (`internal/coderef`, see "Code a system page cites"), the removal of old attempt markers and of the transcript-recovery records not saved for 30 days or past a host's newest 2,048 (`internal/catchup`), and last the search index (see "Search index"); with no wiki set up it keeps the search index and removes those markers and records. The day's sweep of idle backlogs runs in the day's first catch-up job instead, so it never holds back the commit (see "Triggers"). |
| **System pages** | `internal/systempage` checks and writes system pages, the system map, the names index and the repos index (see "System page"); the hook's prompt pointers read the names index, and prompt recall the repos index. |
| **Knowledge pages** | `internal/knowledgepage` reads, renders and saves knowledge pages, and builds what lists them: the knowledge map, the folder index, the root index's line and the recall index, `.eva-md/knowledge.tsv` (see "Knowledge page"). The update writes the pages; the prompt hook, the brief, the update's input and `eva_status` read the recall index, never the pages. |
| **Code refs** | `internal/coderef` measures how far the code a system page cites has moved, with bounded, read-only git calls in a local clone, and keeps what a pass measured in `<state>/code-status.json`. The identity cache doubles as its registry of clones, since it holds an entry for every worktree a chat ran in (see "Code a system page cites"). |
| **Search index** | `internal/searchindex`: a rebuildable cache of the inbox's lines that `eva-md search` reads beside its scan, kept by the daily maintain and `eva-md index` (see "Search index"). Hooks never read or write it. |
| **init / install / uninstall / doctor** | Set up the wiki and the per-machine config. Install only through the hosts' plugin CLIs, verify, and roll back only this run's steps; record what install made and link the binary on PATH where that is safe. The doctor reports PASS, FAIL or UNKNOWN. |
| **Releases and upgrade** | `internal/release` finds the latest release, downloads an asset and checks it against `SHA256SUMS`, and orders versions; `eva-md upgrade` uses it, and `scripts/install.sh` and `scripts/uninstall.sh` find, download and check a release the same way in the shell (see "The install scripts and upgrade"). |

**Git's environment.** Every git eva-md runs starts from one environment (`identity.GitEnviron`): the identity and code reads, the brief's live git, the update's commit subjects, the wiki's runner, which the daily commit, init and setup use, and setup's read of your signing settings, so its signing line says what the daily commit will do; gh and glab get theirs from it in setup.
It is this process's, without the variables that point git at another repository (those git itself clears before it runs a command in another repository, such as `GIT_DIR`, `GIT_INDEX_FILE` and `GIT_SHALLOW_FILE`, and `GIT_NAMESPACE`), carry config (`GIT_CONFIG_PARAMETERS`, which `git -c` hands
down, `GIT_CONFIG_COUNT` with its keys and values, and `GIT_CONFIG`), change how git reads a pathspec (`GIT_LITERAL_PATHSPECS`, `GIT_GLOB_PATHSPECS`, `GIT_NOGLOB_PATHSPECS` and `GIT_ICASE_PATHSPECS`) or write a trace of what git does (every `GIT_TRACE*` and `GIT_CURL_VERBOSE`), and with prompts off
(`GIT_TERMINAL_PROMPT=0`) and git's messages in English (`LC_ALL=C`): eva-md reads them to sort a failure, and a git built with translations, such as Homebrew's, would word them in your locale's language. git's config files (`GIT_CONFIG_GLOBAL`, `GIT_CONFIG_SYSTEM`, `GIT_CONFIG_NOSYSTEM`) and what signs and signs in (`GIT_SSH_COMMAND`, `GIT_ASKPASS`, the SSH agent) pass.
The read-only calls add `GIT_OPTIONAL_LOCKS=0` and `GIT_NO_LAZY_FETCH=1`.

**Command output.** What eva-md keeps of a command's output is capped, and the cap holds however much the command prints.
A git command of the wiki's runner fails past 1 MiB, but for the daily commit's list of staged files and uninstall's count of changed files, which may take 64 MiB, a few hundred thousand files, and setup's and init's check for uncommitted changes, which only notes whether git listed any, so a list
of any length counts as changes; what git prints on stderr, read only to sort a failure, is cut at 1 MiB.
The brief keeps the first 64 KiB of each live git call.
A host's plugin inventory fails past 2 MiB, and what the host prints on stderr is cut there.
The doctor takes a host command, such as `codex features list`, that prints more than 8 MiB as unavailable, and setup fails a gh or glab read that prints more than 1 MiB.

---

<!-- /v1 -->

## Must keep

The items of eva-md as a whole are listed in [scope.md](scope.md#must-keep), and each component's in its own spec.

## Since the split

No change yet.
