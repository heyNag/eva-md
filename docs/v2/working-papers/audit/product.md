# eva-md: the product as a whole

Audit of the goal, decisions, scope, roadmap, risks and known limitations, checked against the code.
Read in full: README.md, docs/decisions.md, docs/known-limitations.md, docs/design.md lines 1-109 and 1195-1508, plus docs/verification.md lines 64-160 and the okf adapter section of the design (lines 110-197). Code claims carry file:line. "Verified" means I read the code; "inferred" means I reason from documents or from host behaviour I could not observe here.

Sizes that frame everything below: 54,807 lines of non-test Go and about 64,600 lines of tests, written in 55 commits over 9 calendar days (2026-10-30 to 2026-11-07, 31 of them on 2026-11-04); docs/design.md is 394 KB in 1,508 lines.

---

## 1. What the product promises, and what it is today

**The promise** (docs/decisions.md:5-7): memory you can depend on in Claude Code and Codex, which "saves what happens, keeps it organized and brings the right context back on its own. You never curate, review or trigger it." The test is that "where did we stop?" continues as if it were the same chat.

**What is actually delivered and proven**: the resume half. Hooks capture each chat into a redacted local inbox, and code with no model builds a brief at chat start, after /clear and after compaction. That half has been checked live on Claude Code CLI, Codex CLI, across the two hosts, and in the Claude desktop app's Code tab (docs/verification.md:68-74).

**What is built but unproven**: the memory half. The background update that turns chats into thread, rules and knowledge pages, per-prompt recall of knowledge, the daily signed commit and push from a detached job, the MCP server on real hosts, and both skills have never run on a real host (docs/verification.md:141-160; README.md:26; AGENTS.md's opening paragraph says the same). The design's own gate for knowledge and recall, "after two weeks of daily v1 use", was skipped at the owner's request (docs/design.md:22, 1395, 1448).

So the product today is a proven session-resume tool with an unproven long-term memory attached. The owner's goal, "the best memory system for AI agents like Claude Desktop or Codex, invisible, as if built in", is mostly about the unproven half and about surfaces the scope excludes.

---

## 2. How it works (from the code)

### Data flow

1. **Capture.** Each host plugin registers hooks that run `~/.local/share/eva-md/bin/eva-md hook --host <h>` (internal/install/testdata/claude-hooks.golden.json, codex-hooks.golden.json). Claude Code: SessionStart, UserPromptSubmit, Stop, StopFailure, PostToolUse and PostToolUseFailure (async, matcher `Bash|Edit|Write|NotebookEdit|TaskCreate|TaskUpdate|TodoWrite|Read|Grep|Glob|WebFetch|WebSearch|mcp__.*`), PreCompact, PostCompact, PostModelSwitch, SessionEnd. Codex: SessionStart, UserPromptSubmit, Stop, PostToolUse (`Bash|apply_patch|update_plan|mcp__.*`), PreCompact, SessionEnd, Interrupt.
2. `hook.Run` (internal/hook/hook.go:119-221) reads at most 2 MiB of payload (hook.go:30), always returns 0, recovers panics, and gives each event 300 ms, or 1.5 s for SessionStart and the tool events (hook.go:229-235). It returns nothing inside eva-md's own update runs (`EVA_MD_INNER=1`, hook.go:281) and in Claude Code cloud sessions (`CLAUDE_CODE_REMOTE=true`, hook.go:284).
3. It resolves the project from the payload's cwd through the identity cache (hook.go:320), applies exclusions (hook.go:308, 326), takes the session lock (hook.go:361), runs up to 100 ms of transcript recovery (hook.go:432-441), and appends one redacted JSONL entry to `~/.local/state/eva-md/inbox/<project>/<host>-<session>.jsonl` (hook.go:422, 578). Stored kinds: prompt, final reply, one-line tool record, plan, host compaction summary, events. Assistant text between tool calls and all tool output are never stored; transcript recovery also keeps final replies only (internal/catchup/parser.go:131-132, 169, 189).
4. **Brief.** On SessionStart, or on the first prompt when no brief was delivered, `resume.Build` (internal/resume/resume.go:78-173) picks the previous chat on the same branch, reads the branch's thread page, standing rules, constraints, system pointers and live git, and fences it under a label that calls it reference data (resume.go:52). Hard caps: 8,000 characters on Claude Code and 6,000 on Codex; 6,000 and 4,500 after compaction; 1,500 for a resumed chat; 2,500 for a refresh (resume.go:177-202). A chat gets at most 2 refreshes of a changed Now (internal/hook/resume.go:36).
5. **Per prompt.** A typed prompt may get up to 3 system-page pointers by name match (internal/hook/pointers.go:36-45), up to 3 recalled knowledge statements in at most 1,000 characters (internal/hook/recall.go:31; internal/knowledgepage/recall.go:14-37), or, every 5th prompt by default, a one-line reminder of the search command (internal/config/config.go:38; pointers.go:221-234).
6. **Background update.** Hooks queue a detached `eva-md update` (setsid, stdio to the day's log, allowlisted environment; internal/hook/spawn.go:47-70) on: SessionEnd or PreCompact (hook.go:568-570); a Stop with 60 KB or more unprocessed (internal/hook/trigger.go:19, 98-104); a prompt 30 minutes or more after the file last changed with 600 bytes or more pending (trigger.go:20-21, 111-116); SessionStart catch-up of up to 5 idle sessions of the project (trigger.go:22-23, 151-168); and once a day a sweep of up to 5 sessions of any project idle an hour (internal/update/job.go:138-142).
7. The job takes one global lock, cuts each session's backlog into chunks of at most 60 KB (internal/update/input.go:223), and for each chunk runs the chat's own host CLI headless: `claude -p --safe-mode --tools "" --strict-mcp-config --no-session-persistence --permission-prompts none --model <m> --effort <e> --system-prompt <rules> --output-format json --json-schema <schema>` (internal/update/claude.go:29-42), or `codex exec` with tool features disabled by name (internal/update/codex.go:25-41). The child environment is an allowlist that drops API keys and every `CLAUDE_CODE_*` variable (internal/update/process.go:45-66). One call returns the thread summary, Now item actions, Timeline lines, rule changes and knowledge changes together (internal/update/schema.go:21-33 and the schema below it).
8. Code validates the answer (internal/update/validate.go, knowledge.go, rules.go), then writes knowledge pages, rules pages and the thread page through the okf adapter, and only then advances the session's watermark.
9. **Daily job.** The first hook event of a day spawns `eva-md maintain` (internal/hook/maintain.go:39-48): rebuild knowledge indexes, commit with the user's signing and `git push origin HEAD` (internal/wikigit/commit.go:96-101, 481), lint, code status of system pages, pruning, search index.
10. **Agent-initiated recall.** `eva-md search` and `eva-md show` on the command line, and the same through three read-only MCP tools, `eva_search`, `eva_show`, `eva_status` (internal/mcp/tools.go:63, 81-110).

### Caps, budgets and time limits (verified)

| What | Value | Where |
| --- | --- | --- |
| Hook budget | 300 ms; 1.5 s for SessionStart and tool events | internal/hook/hook.go:229-235 |
| Hook payload | 2 MiB, larger is dropped | hook.go:30 |
| Brief | 8,000 / 6,000 chars (Claude / Codex); compact 6,000 / 4,500 | internal/resume/resume.go:186-202 |
| Standing rules in a brief | 600 chars | internal/resume/rules.go:19 |
| Constraints in a brief | 3, in 600 chars | internal/resume/knowledge.go:25-27 |
| Recall per prompt | 3 facts, 1,000 chars, floor 2.5 | knowledgepage/recall.go:16, 37; hook/recall.go:31 |
| Recall index | 512 KiB (about 1,678 rows) | docs/known-limitations.md:160; docs/verification.md:110 |
| Pointers | 3 a prompt, 12 a chat | internal/hook/pointers.go:37-38 |
| Update chunk | 60 KB; user entry 6 KB, other entry 8 KB | internal/update/input.go:223-225 |
| Knowledge shown to the model | 12 lines, 2,400 chars | input.go:230-231 |
| Knowledge changes kept per pass | 5 | internal/update/knowledge.go:17 |
| Model calls | 40 per host per day | internal/config/config.go:34 |
| Update effort | "highest" by default, which is `max` on Claude | config.go:187-197; update/claude.go:14 |
| Update timeout | 180 s to 600 s by effort | internal/update/runner.go:62-65 |
| Quarantine, backoff | 3 failures in a row; 5 to 60 min | internal/update/runlog.go:62-64 |
| Now page | 10 next, 10 open, 40 decisions, 8 done | internal/threadpage/page.go:100-106 |
| Thread page | lint fails over 512 KB; nothing trims the Timeline | internal/lint/lint.go:31; internal/health/background.go:471 |
| MCP results | 24 KB search or show, 4 KB status; 5 s and 3 s deadlines | internal/mcp/tools.go:15-23 |

### What it writes where

- `~/.local/state/eva-md/`: inbox (JSONL, 0600, never committed), session markers, watermarks, locks, heartbeats, run logs, search index, code status.
- `~/.config/eva-md/`: `config.toml` (eva-md's own) and `config.env` (the user's setup answers).
- `~/.local/share/eva-md/`: the stable binary, the two local plugin marketplaces, the install record.
- The wiki folder (a git clone the user chose): `projects/<slug>/threads/<branch>.md`, `projects/<slug>/rules.md`, `general/...`, `systems/<slug>.md`, `knowledge/k<N>.md`, maps, index files, `log.md`, `.eva-md/*.tsv` (docs/design.md:203-225).

---

## 3. What must be kept

These are the invariants and hard-won fixes a redesign must not lose. Each is in the code today.

1. **Hooks fail closed and stay fast.** Always exit 0, never block, recover panics, bounded payload read, an output phase reserved even when capture runs out of time, Codex Stop always gets `{}` (internal/hook/hook.go:119-221). No model and no network in a hook. Measured: capture p95 about 25-30 ms, brief about 100-150 ms (docs/verification.md:99-100).
2. **Redaction before disk, with a frozen floor.** The text removed is the union of the frozen baseline rules and the current rules (internal/redact/union.go; AGENTS.md "Never change internal/redact/baseline"); an entry is stored without its text rather than unredacted; redaction runs again on model output and on the staged diff (docs/design.md:89).
3. **The inbox is the word-for-word truth and stays local.** Append-only JSONL under flock with fsync, 0600, never committed; the search index and the recall index are rebuildable caches of it and of the pages (AGENTS.md "The search index is a cache, never the truth").
4. **Resume works with no model.** The brief is built by code from the inbox, the plan and live git (docs/design.md:1233). This is the one part proven live.
5. **Provenance that code enforces, not the model.** Speakers come from the entry's role, never its text; dates come from entry timestamps; every page line cites its source entries; the tags stated, accepted, suggested, observed and inferred are checked against the cited entries; pasted text, host prompts, subagent prompts and program-run chats never count as the user's words; a turn that read the web settles no observed knowledge (internal/update/schema.go:21-33; internal/update/knowledge.go:72-83; internal/hook/hook.go:518-521, 732-737).
6. **Validate before write, and never lose silently.** Watermark only after a validated save and folder fsync; compare-and-swap; no-clobber create; supersede with history instead of delete; locked and human-verified pages are never rewritten (docs/design.md:1236-1237).
7. **Injected memory is fenced and labelled as data**, with speaker headers that stored text cannot forge (internal/resume/resume.go:52; internal/freetext), and cross-project recall is scoped to the project, all-projects facts, projects sharing a system page, and projects the prompt names (internal/knowledgepage/recall.go:137; internal/hook/recall.go:105-109).
8. **Privacy switches.** Off the record, exclusions by path and remote, markers-only capture while transcript recovery is behind (hook.go:494-512), and a config that cannot be read captures nothing and says so (hook.go:191-196).
9. **No recursion and no tools in update runs.** `EVA_MD_INNER=1`, an allowlisted environment, an empty run folder, tools off on Claude and tool use rejected on Codex (internal/update/process.go:45-66; claude.go:29-42).
10. **Wiki isolation and git manners.** Instance id and remote guard on every write, commit and push; never force, never rewrite, signing never changed; explicit-path staging with a secret scan (internal/wikigit/commit.go:83-101).
11. **Codex hook trust survives upgrades** only because the plugin folder is byte-identical across binary-only releases and calls a binary at a stable path (docs/design.md:1404).
12. **Hardened git environment** shared by every git call (docs/design.md:104) and the identity cache that makes capture meet 50 ms (docs/verification.md:117).
13. **Test discipline.** Tests never touch the real machine and never depend on machine speed (AGENTS.md "Rules"); the accuracy suite's fact scripts with code-derived obligations (internal/update/accuracy) are the right seed for a live evaluation.
14. **Only eva's name shows**, with its own names for plugin, command, MCP server and folders, so it never interferes with another memory tool (docs/decisions.md:77).
15. **Uninstall never removes the wiki**, and every destructive command previews first.

---

## 4. Where it falls short of the goal

Ranked by consequence for "the best memory for AI agents, invisible, as if built in".

### F1. The memory half has never run on a real host (evaluation, critical, known)

- **Evidence.** docs/verification.md:145 "No update has run live yet"; :149 "the update rules at version 4 have not been run by a real model"; :146 the daily commit from a detached job is unchecked; :160 the MCP server on real hosts is unchecked; :148 neither skill has been seen to trigger. The accuracy suite scores canned outputs written by the developers (internal/update/accuracy_test.go:19-63, 190, 569). The recall floor was tuned on 36 fixture rows and 73 labelled prompts (internal/knowledgepage/recall.go:27-37).
- **Why it matters.** The update depends on host flags (`--safe-mode`, `--permission-prompts none`, `--json-schema` with `--tools ""`), on host error wording (internal/update/classify.go:11-46), on 25 Codex feature names pinned to one release (internal/update/codex.go:35-41), on the entrypoint value each Claude surface sets (hook.go:732-737; docs/design.md:1489), and on a model following about 3,600 characters of rules. None is observed. If the desktop app reported an SDK entrypoint, every desktop chat would silently get no update.
- **Direction.** Make the live gates the first milestone of the plan, before any redesign: `doctor --live`, the live accuracy run on both hosts, the live resume test with updates on, and a week of dogfooding with the results written down. What they show decides what is kept. Action: add.

### F2. Nothing measures whether memory helps a chat (evaluation, high, not in the docs)

- **Evidence.** There is no end-to-end benchmark of "fact planted in session A changes the answer in session B" beyond the hand-run resume check; grep finds no LoCoMo or LongMemEval style suite. Recall leaves no record but a per-chat soft counter (internal/hook/recall.go:114-115; pointers.go:113-120), so nobody can later ask "what was injected, and was it used?". No knowledge page tracks use, age or confirmation (grep for decay, usage, feedback in internal/knowledgepage and internal/hook finds nothing). There is no canary against new host releases.
- **Why it matters.** "Best" is unfalsifiable without a number. Every tuning decision (the floor, the caps, the promotion bar) is a guess, and a host release can break capture or updates with only the doctor to notice.
- **Direction.** Build an evaluation harness as a first-class component: scripted multi-session tasks scored end to end on real hosts, a labelled retrieval set drawn from real use, a local ledger of what was injected and whether the agent used it, and a nightly canary that runs the hook contract and the update command against the newest host versions. Action: add.

### F3. Reach: bound to the hook plugins of two CLIs (reach, critical, known as scope)

- **Evidence.** docs/decisions.md:32-36 and docs/design.md:1359 exclude Claude Desktop's Chat tab, regular ChatGPT chat, Claude Code cloud sessions and the Codex IDE extension. The code enforces it: hooks return at once in a cloud session (internal/hook/hook.go:284); chats started by the Agent SDK or `claude -p` are treated as a program's, stored as host prompts and never summarized (hook.go:518-521, 732-737); the MCP server refuses any host but the two (cmd/eva-md/mcp.go:22-24) and offers three read-only tools (internal/mcp/tools.go:81-110).
- **Why it matters.** The owner names Claude Desktop. The architecture assumes a host that fires lifecycle hooks with a cwd and a transcript path; surfaces without hooks get nothing, and agent-run work (subagents, SDK orchestration, cloud sessions) is where coding agents are heading.
- **Direction.** Separate a host-neutral memory core (ingest an event, store, retrieve, build a brief) from thin adapters. Hook adapters stay for Claude Code and Codex. Add a read and write MCP adapter for surfaces that only speak MCP, and a repo-carried adapter for cloud sessions (the wiki fetched in the session). Each surface's real extension points need research before design. Action: evolve the core, add adapters.

### F4. Reach: one Mac, one wiki, one person (reach, high, known)

- **Evidence.** Setup asks before adopting a wiki another Mac made and says "One wiki per machine: use it here only if <mac> no longer pushes to it" (cmd/eva-md/setup.go:752-757). The daily job only pushes; it never fetches or merges, and a remote with other commits is `diverged`, "left for a person to fix" (internal/wikigit/commit.go:96-101, 481; docs/known-limitations.md:223). docs/design.md:1240 lists "no syncing between machines" as a guarantee. The inbox is machine-local by decision. Setup refuses any OS but macOS (cmd/eva-md/setup.go:99, 117), though the binary builds and its tests pass on Linux (AGENTS.md "Working in a Linux cloud session"). 41 non-test files use `syscall` (flock, setsid, O_NOFOLLOW), so Windows needs a platform layer.
- **Why it matters.** A person with a laptop and a desktop has two memories. A team has none. Linux developers, a large share of coding-agent users, are refused at the first check.
- **Direction.** Design the wiki as a multi-writer store: fetch and merge during maintain, merge-safe formats (one fact per file already is; indexes and maps are rebuilt, never merged), machine and author provenance on every line, then scoped sharing (personal, project, team). Ship Linux first since it is nearly free; plan Windows as a platform layer. Action: evolve.

### F5. Promotion is one-shot and batch-local; what it misses is gone (memory-quality, high, known in pieces)

- **Evidence.** One model call per 60 KB chunk produces the thread summary, Now actions, Timeline, rules and knowledge at once (internal/update/schema.go:21-33). A statement can cite only that batch's entries, so a fact settled across two chats is never promoted (docs/known-limitations.md:148). A pass keeps 5 knowledge changes; the rest are lost because the watermark moves on (internal/update/knowledge.go:17; known-limitations.md:171). For supersession the model sees at most 12 rows chosen by shared words with the batch (internal/update/input.go:864-907), so a contradiction worded differently leaves the old fact standing. A stated fact must share half its distinctive words with the user's prompt, so the gist of a long instruction, or "yes, do that", is refused (known-limitations.md:149). Chats from before the first update are set aside forever (internal/update/baseline.go:44-52). The input never includes the assistant's intermediate reasoning or any tool output (internal/catchup/parser.go:131-132; docs/design.md:1391).
- **Why it matters.** The knowledge base will be thin, and what is in it will go stale without anything noticing. Nothing revisits a fact, checks it against the code, merges near-duplicates over time or retires what stopped being true.
- **Direction.** Treat the inbox as the source and every page as a rebuildable derivation. Split the single call into stages: a per-chat episode summary, then a periodic consolidation that retrieves all related knowledge, merges, supersedes and retires, and can be re-run over history when the rules improve (which also gives an opt-in backfill). Keep the validator's provenance checks; replace the lexical grounding test with one that tolerates paraphrase. Action: rebuild the promotion stage, keep the validator.

### F6. A thread is a branch, and everything outside git is one thread (memory-quality, high, not in the docs)

- **Evidence.** The thread is the branch name (internal/hook/hook.go:394-409). Every chat outside a git repo goes to one page, `general/threads/default` (internal/identity/identity.go:73-75; internal/update/wiki.go:79). A thread's state is always `active` (wiki.go:313; docs/design.md:294 says idle, merged and gone are planned). Nothing trims a Timeline; the doctor's fix at 512 KB is "trim each page's Timeline by hand; automatic rotation is not built yet" (internal/health/background.go:471). The Now holds at most 10 next steps and 10 open questions (internal/threadpage/page.go:104-105).
- **Why it matters.** Someone who works on main, or outside a repo, has every workstream rewriting one Now page; feature-branch users collect one dead page per merged branch (docs/design.md:1447). The model cannot serve a surface with no git at all, which is exactly the desktop chat the owner wants.
- **Direction.** Make the episodic unit a workstream derived from sessions, with branch, files touched and topic as attributes, not as the key. Build the project's current state by consolidation over workstreams, and archive finished ones automatically. Action: rebuild the thread model.

### F7. Per-prompt recall is exact-word matching over knowledge pages only (recall-quality, high, known)

- **Evidence.** `Recall` matches lowercased whole tokens of 3 or more characters against a row's title and statement, needs at least 2 shared tokens and either the whole title or 3 tokens, and a score of 2.5 (internal/knowledgepage/recall.go:71-154). The stoplist is English (recall.go:249-271); there is no stemming, so "dependency" misses "dependencies" (docs/known-limitations.md:155). The prompt hook reads only the knowledge index (internal/hook/recall.go:91-117): Timeline decisions, rules beyond the brief's 600 characters, and system page content are reached only if the agent chooses to search. A fact is recalled once per chat (known-limitations.md:161). The brief is built at SessionStart, before any prompt exists (internal/resume/resume.go:78-173), so its 8,000 characters are the previous chat's tail whatever the user is about to ask.
- **Why it matters.** Recall that fires only on near-verbatim wording will feel absent. The ruled-out list (docs/decisions.md:110-119: local models, vector databases, a model call on every turn) closes every route to matching by meaning.
- **Direction.** One retrieval layer over all memory types, with normalisation, field weights and an optional semantic index that is an embedded, rebuildable file computed off the hot path (during the background update). Make the first-prompt brief query-aware. Reopen the ruling as "no resident server", not "no vectors". Tune on a labelled set from real use (F2). Action: rebuild recall, keep the scoping and fencing.

### F8. A dozen manual steps and approvals (invisibility, high, known individually)

Verified in code or documents:

1. Create or choose a private repo and a wiki folder, with gh or glab signed in if setup is to create it (README.md:59-61; cmd/eva-md/setup.go).
2. Answer setup's questions and confirm; a no-terminal first run stops to have `config.env` filled in (README.md:55).
3. Approve eva-md's hooks in Codex `/hooks` after install, and again after any upgrade that changes the plugin folder; Codex captures nothing until then (cmd/eva-md/setup.go:567; docs/known-limitations.md:263).
4. Run `doctor --live` by hand to learn whether updates work at all (setup.go:624).
5. Approve the daily commit's signature, and perhaps the SSH push, every day; a declined signature waits a day (docs/design.md:1399, 1508; known-limitations.md:224).
6. Allow the MCP tools by editing your own Claude settings, or answer a permission prompt per tool (README.md:204). Inferred: the Bash call to `eva-md search` that the reminder suggests meets the same permission prompt.
7. Run `eva-md upgrade` yourself and answer "Install or update the plugins? [y/N]" (setup.go:480); auto-update is ruled out (docs/decisions.md:117).
8. Merge by hand when the wiki's remote diverged (known-limitations.md:223).
9. Trim a Timeline by hand past 512 KB (internal/health/background.go:471).
10. Ask the agent to digest documentation before any system page exists; nothing creates or refreshes one on its own (README.md:183; known-limitations.md:113).
11. Every detected problem ends in "run eva-md doctor" (internal/health/health.go:53-113), and the doctor has only two repairs.
12. Fix a wrong fact by editing Markdown and locking the page.

- **Direction.** Design for zero questions: a local wiki by default with a remote added later, self-repair instead of instructions, binary-only auto-upgrade behind the byte-identical plugin folder, a commit path that never prompts (for example a dedicated key or deferred signing), and host allow-rules written with one consent at install. Several of these reopen ruled-out options (auto-update, writing host settings). Action: evolve.

### F9. Default cost in quota and context (invisibility, medium, partly in the design's Risks)

- **Evidence.** The update runs the chat's own model at "highest" effort unless configured (internal/config/config.go:187-197), which is `max` on Claude (internal/update/claude.go:14), up to 40 calls per host per day (config.go:34) of up to 60 KB each (internal/update/input.go:223), each allowed up to 10 minutes (internal/update/runner.go:62-65), on the user's subscription and sharing its rate limits (docs/known-limitations.md:180). Every chat start injects up to 8,000 characters (internal/resume/resume.go:186-202); the reminder carries the binary's absolute path (internal/hook/pointers.go:221-234); the MCP tool list costs about 500 tokens a chat and one idle 13 MB process (known-limitations.md:305, 317).
- **Why it matters.** A user who hits a usage limit earlier in the day because of a memory tool will notice the tool. Context spent on a brief the chat did not need is noise.
- **Direction.** Give memory an explicit budget: a small default summarizer model and effort, escalation only for consolidation, token accounting visible in status, a short stable session header and relevance-driven additions. Allow a direct API backend so machines that bill through an API key, Bedrock or Vertex work at all (today they fail as `auth` or `host_error`, known-limitations.md:195). Action: evolve.

### F10. okf is a compiled-in dependency used for parsing and saving, wrapped in 13 workarounds (architecture, medium, not framed as debt in the docs)

- **Evidence.** The adapter calls `ParseConcept`, `SerializeConcept`, `SaveConcept`, `ExtractFrontmatter`, `ValidateConceptID`, `IsValidActor`, `LoadBundle`, `Validate` and `InitBundle`, which "writes nothing" (internal/okfadapter/adapter.go:137, 146, 180, 216, 236, 370, 379, 423, 566, 583, 767, 830; docs/design.md:1402). Search, log, index, relate, update, registry and MCP are never called (design.md:128). The design lists 13 protections and 13 gaps to watch (design.md:131-171), and says of the exit: "eva-md then writes its own pages. That takes a few hundred lines" (design.md:188). Meanwhile eva-md has its own page models for threads, system pages and knowledge pages (internal/threadpage/page.go, internal/systempage/frontmatter.go, internal/knowledgepage/page.go), and the adapter is 1,038 lines with 1,397 lines of tests. okf's behaviour leaks into the product: its lock file and temp-file names (docs/known-limitations.md:321), a body check that turns a user's page "damaged" (design.md:1434), orphan rules that dictate the link structure (design.md:245-248), and a redirect to `knowledge/` when the root index is missing (design.md:117). "7 of 20 releases changed behaviour in functions the adapter calls" (design.md:185).
- **Direction.** Own the page store: a small typed Markdown and frontmatter codec, atomic write with fsync, one lock. Keep okf-format compatibility as an export target and a test, not a runtime dependency. This reopens decision 15. Action: rebuild the storage layer.

### F11. The documents cannot serve as a working spec for a team (architecture, high, not in the docs)

- **Evidence.** docs/design.md is 394 KB in 1,508 lines; 21 lines exceed 2 KB and line 654 is 7.4 KB. docs/verification.md is 182 KB with one 22.6 KB line. docs/known-limitations.md is 81 KB, about 300 bullets, unranked, mixing a 1% redaction miss with "no update has run". README.md is 57 KB, most of it setup edge cases. design.md changed in 26 of 55 commits and known-limitations.md in 24. Tests tie the docs to the code only for command names, flags and file lists (cmd/eva-md/docs_test.go:66-232). Behaviour, rationale, history ("as before", "where setup used to refuse it"), host-version trivia and test inventories share the same paragraphs.
- **Why it matters.** A coding agent cannot load the design whole (about 100K tokens), and a section cannot be trusted alone because invariants are scattered. Builders will either miss a hard-won rule or pay the whole document on every task.
- **Direction.** Rewrite as: a product spec of a few pages (goal, non-goals, guarantees); one spec per component beside its package, stating its interface, invariants and limits; dated decision records replacing decisions.md plus "Changes to decisions"; a table of caps and time limits generated from the code's constants; and limitations ranked by impact with an owner. Action: rebuild the documents.

### F12. No way to tell memory something, or take something back, from the chat (memory-quality, medium, not framed in the docs)

- **Evidence.** The MCP server is read-only (internal/mcp/tools.go:63, 81-110). Only the background update writes knowledge (docs/design.md:98). "Remember this" becomes a rule or a fact only after a later trigger and model run, and only if the wording passes the lexical grounding check; "forget that" alone drops nothing (docs/known-limitations.md:149). Removal is whole sessions from the inbox; the wiki, its git history and the pushed remote keep everything (known-limitations.md:100).
- **Direction.** Add validated write operations the agent can call (remember, correct, forget) through the same provenance checks, effective at once, with a tombstone that reaches the inbox, pages, indexes and, on request, history. Action: add.

### Also noted (not in the twelve)

- **Safety defaults on Codex are the weaker ones** (known): pasted text counts as the user's words and can become a standing rule (known-limitations.md:211); `codex exec` prompts are stored as the user's (:27); the user's MCP servers start on update runs unless `codex_ignore_user_config = true` and can receive chat text (:193; docs/decisions.md:106).
- **Cross-provider flow** (known): a brief in Codex can quote a Claude Code chat, and the other way round (README.md:199).
- **Standing rules arrive as "reference data"**: the brief's label says the block is reference data and that the user and the code are current (internal/resume/resume.go:52), while a rule is meant to be obeyed. Whether agents follow recalled rules has never been measured (inferred risk; belongs under F2).
- **Freshness**: a fact stated now reaches other projects' chats only after a trigger and a model run of up to 10 minutes, and a trigger that finds the one global job running simply exits (known-limitations.md:186).

---

## 5. Decisions and ruled-out options: which hold the product back

**Strengths to keep**: platform separate from the wiki (decision 1); plain Markdown and git, inspectable (2, 11); word-for-word capture with no model on the write path (4); a deterministic brief (6); provenance tags and supersede-not-delete (5); hooks never block (12); own names (13).

**Holding the product back, and worth reopening**:

| Decision or ruling | Where | Cost to the goal |
| --- | --- | --- |
| Hosts in scope: two CLIs and their desktop code tabs | decisions.md:32-36 | F3 |
| "Vector databases", "local models", "a model call on every turn" ruled out | decisions.md:112-114 | F7: no matching by meaning |
| "History backfill on install" ruled out | decisions.md:116 | Empty memory on day one; no re-derivation (F5) |
| "Telemetry and auto-update" ruled out | decisions.md:117 | F8 item 7; and no local observability either (F2), which telemetry need not mean |
| "Writing host settings files directly" ruled out | decisions.md:118 | F8 item 6 |
| One commit a day with the user's signing | decisions.md:54-57 | F8 item 5; a day's pages exist only on one disk until then |
| Built-in memories left as they are | decisions.md:58 | Two memories inject side by side; no reconciliation with CLAUDE.md, AGENTS.md or the hosts' own memory |
| okf at the core | decisions.md:82-90 | F10 |
| One thread per branch | decisions.md:94 | F6 |
| Update on the chat's own host and model, highest effort | decisions.md:42, 107 | F9; fails on API-key, Bedrock and Vertex setups |
| Nothing stays running | design.md:75 | Scheduling is rebuilt from hook events: five trigger kinds, sweeps, watermarks, state re-read from run logs |

---

## 6. Where the complexity is, and where the patching went

- **Essential**: redaction, provenance validation, hook latency and fail-closed behaviour, transcript recovery, fencing against stored injection.
- **Accidental**: (a) scheduling without a scheduler, so budgets, holds, backoff and quarantine are recomputed from run-log files on each start (internal/update/job.go:25-35; runlog.go:62-66) and the day's sweep is claimed across time zones (job.go:283-327); (b) using host CLIs as the model API, which needs error-text matching (internal/update/classify.go:11-46), a feature list pinned to one Codex release (codex.go:35-41) and effort step-down retries (runner.go:142-200); (c) the okf adapter (F10); (d) setup's handling of gh, glab, internal repos, SSH aliases and refusal wordings (cmd/eva-md/setup.go 1,324 lines, setup_repo.go 974, setup_refusals.go 443).
- **Git history** (`git log --oneline -- <path>`): commits touching setup.go 12, internal/install 14, scripts 14, .github 12, internal/release 9; against internal/update 10, internal/knowledgepage 5, internal/redact 4, internal/searchindex 2. Since the first two feature commits, most iterations went to installing, releasing and one self-hosted GitLab, not to the quality of memory. The memory core was written in a few large commits and has not yet met real use.

---

## 7. What has never been run for real

From docs/verification.md:141-160, confirmed by AGENTS.md:

- any background update on either host; `doctor --live`; the live accuracy run; the update rules read by a real model;
- knowledge promotion and prompt recall on a real index; the recall floor;
- the daily signed commit and push from a detached job, and how many approvals it takes;
- the MCP server in either host; the digest and recall skills loading and triggering;
- the Codex desktop app; a week of everyday use; the day's sweep on real chats;
- `eva-md upgrade` and a whole uninstall from a real release; the Intel binary;
- the paste wrapper in a real payload; the entrypoint each Claude surface sets; whether `codex exec` can be told apart;
- `eva-md inbox clear` on a real inbox; setup with real gh or a GitHub Enterprise server;
- on a self-hosted GitLab: a day's commit and push, and the instance guard refusing another machine's wiki.

Run live and passed: the brief on Claude Code CLI, Codex CLI, cross-host and the Claude desktop Code tab; native plugin install and Codex hook trust; transcript recovery; Codex's feature list; releases; the install command on one Mac.

---

## 8. Overall call

**Evolve, with three rebuilds.** Keep the capture path, redaction, the inbox, the deterministic brief, the validator's provenance rules and the test discipline: they are the proven and the principled parts. Rebuild (1) the promotion pipeline into staged, re-runnable consolidation, (2) recall into one retrieval layer over all memory, and (3) the storage layer without okf, and replace the branch-keyed thread model. Add an evaluation harness and a host-neutral core with adapters before anything else, and rewrite the documents so a team can build from them. Do not redesign before the existing pipeline has run live: its real behaviour is the cheapest evidence available.

---

## 9. Open questions for the owner

1. Which Claude Desktop surfaces matter (Chat tab, Cowork, Code tab), and may the plan spend time researching what each exposes (MCP only, extensions, hooks)?
2. Is "nothing stays running" still a hard rule, or is a small on-demand agent (launchd, systemd user unit) acceptable for timeliness and reliability?
3. May the summarizer use a direct API backend (API key, Bedrock, Vertex) beside the host CLI, and a smaller default model than the chat's?
4. Is an embedded semantic index (a file, no server) acceptable, reopening "vector databases" and "local models"?
5. Is one person on several machines a requirement for the next version? Is team sharing?
6. Is a daily signing approval acceptable, or may eva-md commit with a key of its own or defer signing?
7. May eva-md write host allow-rules with one consent at install, and auto-upgrade binary-only releases?
8. Is an opt-in backfill from the existing inbox, or from host transcripts, acceptable?
9. Is okf-format compatibility a requirement, or only "Markdown in git that a person can read"?
10. What share of a subscription may memory use per day?
11. Has anyone confirmed the entrypoint value the Claude desktop app sets? The code assumes `claude-desktop` (internal/hook/hook.go:728-731); an SDK value would stop every desktop update.
12. Should eva-md reconcile with the hosts' own memory (Claude Code auto memory, CLAUDE.md, Codex memories) or keep ignoring it?

---

## Verification

Second reader's attempt to refute each finding against the code and documents at HEAD (51b241c, 2026-11-07). Read-only; no test or restricted command was run. Verdicts: 10 confirmed, 2 partly, 0 refuted. Line numbers are as read on this pass.

### Verdicts

| Id | Verdict | Severity I would give | Evidence and corrections |
| --- | --- | --- | --- |
| eval-1 | confirmed | critical | docs/verification.md:145 "No update has run live yet", :146, :149, :160; README.md:26; docs/known-limitations.md:176. internal/update/accuracy_test.go:190 and :569 are the canned outputs of the 12 fact scripts. docs/design.md:22, 1395, 1448 record that knowledge and recall skipped the two-week gate. One correction: not every host flag is unobserved. docs/design.md:877 says `--json-schema` with `--tools ""` was seen to work on Claude 2.1.311, and verification.md:75 checked Codex's feature names on 0.166.0 with no model call. What has never run is the whole command with eva-md's rules and schema, on either host. |
| reach-1 | partly | high | The exclusions are real: docs/decisions.md:32-36, docs/design.md:1359; internal/hook/hook.go:284 returns on `CLAUDE_CODE_REMOTE=true`; hook.go:732-737 classes `sdk-cli`, `sdk-ts`, `sdk-py` as a program's; cmd/eva-md/mcp.go:22-24 refuses other hosts; internal/mcp/tools.go:63-110 is three read-only tools. Two overstatements. (1) Agent SDK and `claude -p` chats do not "get nothing": they get the startup brief (docs/known-limitations.md:29) and their replies and tool lines are captured (docs/design.md:459); what they lose is their prompts' text, an update of their own, and being the next brief's previous chat (hook.go:518-520, 568, 582). (2) "The owner names Claude Desktop": the desktop app's Code tab is in scope and its brief passed live (docs/verification.md:72). Only the Chat tab, cloud sessions and the Codex IDE extension get nothing. Whether the Chat tab matters is the report's own open question 1, so critical is not yet earned. |
| memq-1 | confirmed | high | internal/update/schema.go:21-33 plus the schema: one call returns summary, items, timeline, rules and knowledge. internal/update/knowledge.go:17, 112-113, 168-169: the sixth change is counted `OverCap` and dropped; docs/known-limitations.md:171 says the watermark moves past it. internal/update/input.go:864-907: at most 12 rows, chosen by shared content tokens, and only shown ids may be changed. internal/update/baseline.go:44-52: chats from before updates are set aside for good. internal/catchup/parser.go:131-133, 169-171, 189: final replies only. Nuance for the team: what is "gone" is promotion to knowledge. The decision still sits in the branch's Timeline and Now and in the inbox, where search finds it. |
| recall-1 | confirmed | high | internal/knowledgepage/recall.go:71-154 (whole lowercased tokens, 2 minimum, whole title or 3 tokens, floor 2.5), :249-271 (English stoplist), no stemming (known-limitations.md:155). internal/hook/recall.go:91-117 reads the knowledge index, and the repos index only for siblings. internal/resume/resume.go:30-37: `Options` has no prompt field, so no brief is query-aware, the first-prompt fallback included. Two corrections. System pages do get per-prompt relevance of a kind: up to 3 name-matched pointers (internal/hook/pointers.go:36-45), a pointer and not content. "A non-English prompt recalls nothing" is too strong: `eachWord` (recall.go:213-244) tokenises any Unicode letters, so a space-delimited language matches its own words, with worse precision because its function words are not on the stoplist; scripts written without spaces do match nothing. |
| eval-2 | confirmed | high | recall.go:27-37 says the floor was tuned on fixtures. The only record of recall is the per-chat soft state `promptState{Recalled, Pages}` (internal/hook/pointers.go:54-59, hook/recall.go:114-115). scripts/benchmark.py measures latency only; .github/workflows has no scheduled job; internal/coderef does not import knowledgepage, so no knowledge page is checked against its code. What exists and is worth keeping: `cmd/live-accuracy` (never run) scores extraction, and the hand-run planted-fact checks in verification.md:66-72 are the seed of an end-to-end test. |
| memq-2 | confirmed | high | internal/hook/hook.go:394-409 (thread = branch; detached keeps the session's last). internal/update/wiki.go:77-82 (`general/threads/default` for every chat outside git). wiki.go:313 (`state` always `active`). internal/health/background.go:471 (trim by hand). Partly known already: docs/design.md:1447 lists threads piling up after merges, and known-limitations.md:207 the Timeline. Not listed anywhere: one page for all non-git chats. Search's default scope also adds the general area to every project (internal/resume/search.go:315-322). |
| invis-1 | confirmed | high | cmd/eva-md/setup.go:480, 567, 624; README.md:204; internal/health/health.go:53-113 (all but one of the chat-start notices name `eva-md doctor`); known-limitations.md:223, 224, 263. Caveat for the direction: three of the twelve are consent gates the hosts or the user own, not eva-md's choices: Codex hook trust, Claude Code's MCP permission prompt, and the approval the user's own signing setup asks for. A design that removes them has to work inside those gates (design.md:1404 already does for binary-only releases), and "a commit path that never prompts" changes what signs the wiki, which decisions.md:57 fixes as "signing stays as configured". |
| reach-2 | confirmed | high | cmd/eva-md/setup.go:755 ("One wiki per machine"); internal/wikigit/commit.go:96-101, 479-483 (push only; no fetch, pull, merge or rebase call exists in wikigit or cmd/eva-md); design.md:1240. Corrections: the macOS gate is setup.go:237-238 (99 and 117 only carry the value), and scripts/install.sh:35 refuses non-Darwin too. "Linux first, nearly free" is optimistic: releases build darwin binaries only (.github/workflows/release.yml:110-119), and setup's checklist and the install scripts are macOS-shaped. |
| docs-1 | confirmed | high | Sizes check out: design.md 394,093 bytes in 1,508 lines; verification.md line 55 is 22,608 bytes; known-limitations.md has 277 bullets in 17 sections; design.md changed in 26 of 55 commits and known-limitations.md in 24. Stronger evidence than the report gives: an agent's standard file reader cuts a line at 2,000 characters, and 21 lines of design.md (17.7 KB past the cut) and 28 of verification.md's 160 lines (91.7 KB past the cut, half the file) exceed that, so an agent reading them the usual way silently loses the tails. Corrections: 18 lines exceed 2,048 bytes, 21 exceed 2,000 characters. Besides cmd/eva-md/docs_test.go there are three more spot checks (docs_test.go:204 and :232 on verification.md, internal/redact/passwords_test.go:662-694 on the design's examples, internal/update/sweep_test.go:311-323 on one limit), which does not change the conclusion. Keep: AGENTS.md (68 lines, no long line) and the "Reliability guarantees" table (design.md:1227-1245) already work as short specs. |
| invis-2 | partly | medium | The code is as cited: config.go:187-197, update/claude.go:14, config.go:34, input.go:223, runner.go:62-65, resume.go:186-202. Two scope errors. (1) It is known: docs/design.md:1425 lists "Background updates use your subscription quota on large models, more so at the default highest effort" with its mitigations, decisions.md:107 chose highest effort on purpose, and the doctor already shows calls against the cap (internal/health/background.go:111). (2) The brief is not a comparable cost: 8,000 characters is about 2,000 tokens, sized under Claude's 10,000-character hook limit (resume.go:182-185). The real cost is the update: up to 40 max-effort calls a host a day on the chat's own model. A smaller summarizer is a quality trade that only the live accuracy run can price, since decisions.md:18 records extraction errors as the reason for the effort. |
| arch-1 | confirmed | medium | internal/okfadapter uses 17 okf symbols, all parse, serialize, save, validate, load, init and types (adapter.go, head.go; 1,038 lines, 1,397 of tests). design.md:129 lists what is never called, 135-147 the 13 protections, 159-171 the 13 gaps, 187-190 the exit ("a few hundred lines") and its trigger, "the adapter keeps growing past its protections". That trigger has arguably fired: commits 29bf92f and 6bf7b5f (2026-11-06) both add handling for okf's leftover lock file. |
| control-1 | confirmed | medium | internal/mcp/tools.go:63-110; design.md:98 ("The update writes the pages"); known-limitations.md:100, 149. One correction to the title: there is an agent-callable write path, `eva-md system write`, used by the digest skill, but only for system pages. Rules and knowledge have none, and "off the record" is the only control a user has from inside a chat. The proposed tombstone "on request, history" conflicts with the kept invariant that eva-md never rewrites git history (report section 3, item 10); the plan must choose. |

### What the audit missed (checked in code)

1. **truth-1. The inbox is the source of truth, and it exists on one disk only** (architecture, high, not in known-limitations). docs/design.md:423-427 puts it in `~/.local/state/eva-md/inbox`, outside every repo; decisions.md:39 and :119 rule out committing it; no export, backup or sync function exists in cmd or internal; `uninstall --apply --all` deletes it (known-limitations.md:282). Every page line cites it as `s:<id>#<line>`, and internal/lint/lint.go:44-46, 234, 247-250 already treats a ref to a chat "another Mac captured" as unresolvable. This matters more for the redesign than for today: memq-1's direction ("inbox as source, pages as rebuildable derivations"), reach-2's (several machines) and control-1's (forget everywhere) each assume something about the raw record that the others contradict. A second machine cannot re-derive or even verify pages built from the first machine's inbox. The plan needs one decision on the raw record: where it lives, whether an encrypted or redacted form may sync, and what a page's provenance means on a machine that lacks the source.

2. **capture-1. Capture keeps too little of agentic work, and nothing of what agents say to each other** (memory-quality, high, not in known-limitations). A turn is stored as the typed prompt, the final reply only (internal/hook/hook.go:528-535, design.md:450) and one line per tool call without output (hook.go:781-790, design.md:451). The PostToolUse matcher (internal/install/templates.go:74-75) has no Agent or Task tool, so spawning a subagent leaves no line, and no SubagentStop hook is registered. A subagent's report, a teammate's message and a task notification are stored as the textless event `host_prompt` (hook.go:518-520, 679-693), and transcript recovery skips sidechain and agent rows (internal/catchup/parser.go:78-82). In an orchestrated session, which is how the owner says the next phase will be built, memory holds the person's prompts and the orchestrator's last message; what the agents found is nowhere. Promotion and recall cannot improve on what capture never kept, so the redesign should set the capture contract first: attributed agent-to-agent records (stored as agent text, never as the user's), and a bounded, redacted digest of findings between tool calls.

3. **access-1. Scope is enforced for automatic recall only; agent-initiated reads cross every project** (reliability-safety, medium, not in known-limitations). Prompt recall is limited to the project, all-project facts, siblings and named projects (internal/knowledgepage/recall.go:119, 137). `eva_search` takes `all` and any `cwd` from the caller (internal/mcp/tools.go:83-92, internal/mcp/calls.go:225-248, internal/resume/search.go:315-324), `eva_show` reads any `s:` ref whatever its project (calls.go:295), and README.md:204 suggests allow-listing all three tools, which removes the one prompt in the way. So text injected into a chat in one project can pull another project's chats into context, and into the other provider's model. Today that is a modest addition to what a local agent can already read from disk. It becomes central once the plan adds write tools, surfaces beyond the two CLIs and team sharing: memory then needs an explicit access model (which surface and project may read or write which scope) before any of those ship.
