# eva.md v2: the plan

**Status:** draft for the owner's agreement, written 2026-11-10. Nothing here is built. A field report from the one real install is still to come ([the prompt for it](field-report-prompt.md)); the plan will be revised against it before work starts.

**What this is:** an evaluation of eva-md as it stands, and the design and order of work to make it the best memory there is for AI coding agents: one that feels built into the host, where the only thing a person notices is that the agent remembers.

**Companion files:** [findings.md](findings.md) lists every audit finding by id. [working-papers/](working-papers/) holds the audits, research notes, area designs and critiques this plan was distilled from. Where they differ from this file, this file governs.

<!-- PART-1 -->

## 2. How this plan was made

The plan rests on four passes, each checked by a second reader who was told to try to prove the first one wrong.

| Pass | What was done | Result |
| --- | --- | --- |
| Audit | 17 parts of eva-md read in full, code first and documents second, each against seven questions: memory quality, recall quality, invisibility, reliability and safety, reach, architecture, evaluation | 252 findings kept: 7 critical, 85 high, 126 medium, 34 low. Of 204 first-pass findings the second reader confirmed 131 and corrected the scope or severity of 73; it refuted none outright and added 48 of its own |
| Research | Six topics from primary sources as they stood on 2026-11-10: Claude Code and the Claude apps as a platform; Codex as a platform; retrieval that fits one static binary; memory tools for coding agents; general agent-memory systems; how memory is evaluated | 153 load-bearing facts re-checked against their sources: 121 confirmed, 31 corrected, 1 refuted |
| Design | Six area designs built on the audit and the research: the record, the wiki, organizing, recall, hosts and operations, proof | <!-- DESIGN-COUNT --> |
| Critique | Three critics over all six designs: will it work, is it the smallest design that meets the bar, can a team of agents build it in this order | <!-- CRITIQUE-COUNT --> |

Limits of this evidence, stated once so nobody leans on it too hard:

- **No host was run.** The work was done in a Linux cloud session with no Claude Code or Codex signed in. Every claim about eva-md's code was read from the code. Every claim about a host was read from its documentation or source on 2026-11-10. Both hosts ship several releases a month, so each work package that leans on a host fact starts by confirming it on a real host.
- **The memory half of eva-md has never met a real model.** Estimates here of how often the checker would refuse a real answer, how long an update takes or how well recall fires are inferences from the code. Stage 0 replaces them with measurements.
- **Much outside evidence is young.** Several 2026 studies are preprints with small samples, and vendor numbers are self-reported. Where studies disagree, this plan says so and takes the direction they share, not the figures.

## 3. Where v1 stands

eva-md is about 60,000 lines of Go with as many again in tests, written in 11 days. It is careful work. The audit found no pile of bugs. It found a small number of structural choices that each rule out something the goal needs, and one large absence: nothing measures whether the memory helps.

### 3.1 What is strong and stays

These are the parts a redesign must not lose. Each is a property with tests behind it. The full lists, one per part, are in [findings.md](findings.md) under "Must keep".

- **Hooks that cannot hurt the host.** A hook always exits successfully, contains every panic, never blocks, makes no network or model call, and delivers nothing if it runs out of time. Detached jobs start only after the hook's output is written.
- **Redaction before disk, failing closed.** Nothing is stored unredacted; a redactor failure stores the entry without its text. Model output and the staged diff are scanned again.
- **Stored text cannot pose as the user or the host.** One fence, one label style, speaker labels taken from record fields and never from text, and an escaper that stops stored text from forging a heading, a fence, a tag or a frontmatter key.
- **Provenance in code.** Every statement cites the entry it came from. Dates, ids and speaker tags are set by code. Decisions and constraints come only from the user's words. A suggestion or an inference is never promoted.
- **A resume brief built by code.** It needs no model and no network, and it works before any update has run. The outside evidence says this is the best-supported feature a memory can have.
- **A sealed model run.** The background run has tools off, a cleared environment, input on stdin, a strict schema, and no transcript left behind.
- **A crash-safe page store.** One lock that dies with its holder, no overwrite on create, compare-and-swap on update, atomic writes with a folder sync before any progress marker moves, and no link ever followed.
- **A transactional install and a proven release path.** Hosts are changed only through their own commands, only eva-md's own files are touched, a failed install rolls back its own steps, and nothing downloaded runs before its checksum matches.
- **Closed scope and privacy gates.** Recall is limited to the project unless the user's words widen it. Nothing is captured, recalled or summarized while a chat is paused by the user.
- **Test discipline.** Tests never touch the real machine and never depend on its speed.
- **The trust posture.** No telemetry, no account, no resident process, two dependencies, everything inspectable as Markdown and git history. The most-used third-party memory tool is losing users on exactly these points. This is an advantage to keep while other constraints are reopened.

### 3.2 The ten structural problems

Finding ids such as `CAP-01` refer to [findings.md](findings.md).

**1. Nothing measures whether memory helps, and the memory half has never run for real.**
The update, promotion to knowledge, recall, the daily commit and the MCP server have only run against canned model output. The accuracy suite scores by codename, so a wrong value or an invented item scores as perfect. Recall was tuned on the fixture that judges it. No test asks whether a resumed chat continues correctly. Without this, no redesign can be shown to be better than what it replaces. *This is why Stage 0 and Stage 1 exist.* Findings: PRD-01, PRD-05, EVL-01 to EVL-07, UPM-01, RCL-05, BRF-03, SRC-04, CAP-04, RED-05, SYS-02, MCP-05, OPS-06.

**2. Capture keeps too little, through two paths that fight inside the hooks.**
Hooks keep the typed prompt, the final reply and one line per tool call. Mid-turn reasoning, error text, what a subagent found, and answers the user gives in the host's own question and plan dialogs never reach memory, so the knowledge a coding agent most needs (what failed and what fixed it) is lost at the source. A second path reads the host's transcript to recover what hooks missed, and it runs inside every hook in 100 ms slices: while it is behind, the hook discards its own capture and delivers no brief and no recall. About 1,200 lines exist only to make that slicing work. Entries have no stable id, so provenance rests on line numbers in one local file. A project is found by a key that changes when a repository gains a remote, is renamed or is moved, and its memory then silently leaves scope. Findings: CAP-01 to CAP-05, CUP-01, CUP-02, PRD-11, UPM-08, MCP-07, RCL-07, PGS-08, SET-02.

**3. Background work has no scheduler and no queue.**
Six kinds of hook event and a daily sweep each start a detached job and forget it. One lock means a job that finds another running simply exits, so with several chats open a chat's last trigger is often dropped. Maintenance, the commit and health checks run only when some hook happens to fire. When hooks stop, everything that would notice stops with them. Findings: UPW-04, OPS-01.

**4. The model's instructions and the checker are two different contracts.**
The checker decides who said what by word overlap with English word lists (about 1,860 lines). A model that tidies a user's instruction, converts a unit, or records "yes, do that" as an accepted decision is refused, silently, with no second chance. One verdict covers the thread page, the rules and the knowledge together, and content refused three times is skipped for good. The run uses the chat's own model at the highest effort for every kind of memory, a timeout is retried unchanged, and the next chat usually starts before the last one is remembered. Findings: UPM-02 to UPM-05, UPM-07, UPW-01, UPW-03, UPW-05, UPW-06, EVL-07.

**5. The memory model has one unit of working memory, the git branch, and facts with no lifecycle.**
Trunk-based work, and every chat outside a repository, share one page. A decision made on one branch is invisible on every other and after the merge. Promotion to lasting knowledge happens once, from one slice of one chat, capped at five. A fact has no validity: nothing checks whether the code it cites has moved, and an assistant's observation can replace what the user stated. There is no place for "we tried this and it failed because". System pages exist only when a person asks an agent to write one, and then only report their own decay. Findings: PGS-01, PGS-02, PGS-03, PGS-07, PRD-03, PRD-06, UPM-06, SYS-01.

**6. Recall looks only at the words of the typed prompt.**
It matches exact tokens against knowledge pages. A paraphrase finds nothing. Nothing is recalled when the agent opens or edits a file that a stored constraint covers, nothing reaches a subagent, and a constraint sent once is never sent again in that chat, even after a compaction has removed it from the context. Thread decisions and Timelines never reach a prompt. The tools an agent can call are the command line re-exposed: several guessed searches and a call per line to recover one memory. Search crosses project boundaries that automatic recall is careful to keep. Nothing records what was delivered into a chat, so restated memory can be saved again as new evidence. Findings: RCL-01 to RCL-04, RCL-06, RCL-08, PGS-04, PRD-04, SRC-01 to SRC-03, SRC-05, MCP-01 to MCP-03, BRF-01, BRF-02.

**7. The wiki assumes one machine.**
Nothing ever fetches or merges. Ids are counters, so two machines would give one id to different facts. Pages are rewritten whole and derived indexes are committed, so a merge would conflict on nearly every file. The wiki is also the user's own working clone, so eleven unusual git states stop backups until a person fixes them. Memory leaves the machine at most once a day, with the user's own signing prompt. No path exists to change the layout: a schema mismatch only refuses. The page library eva-md was built on supplies a small codec and decides what runs inside every save; the case against it is control, not line count. Findings: STO-01 to STO-04, SET-03, PRD-08, PRD-10, PGS-05.

**8. Starting, upgrading and staying healthy all need a person.**
Memory cannot start until a repository and a folder are chosen and several questions answered. Upgrades arrive only when a person runs a command, and since Codex 0.164.0 any change to a plugin's files switches its hooks off until they are approved again, so each release that improves a skill silently stops Codex capture. Health reports recorded failures but cannot see missing work: capture or updates can be off for days with every chat looking normal. Most remedies are a terminal command to type. It runs on macOS only, for two hosts. Findings: SET-01, PRD-07, PRD-02, OPS-01 to OPS-07, MCP-04, MCP-06, BRF-04.

**9. Redaction destroys ordinary facts and cannot repair its own misses.**
The frozen baseline rules erase non-secret facts about authentication (endpoints, header names, key paths), and because the baseline is unioned in at run time no later rule can bring them back. Some ordinary secret shapes are missed. A miss is permanent: improved rules never reach stored text, and all three gates share one detector. The costliest code in the system runs on the hook's hot path. Findings: RED-01 to RED-04.

**10. The documents cannot serve a team.**
`docs/design.md` is 390 KB of dense prose and `docs/verification.md` 182 KB, both policed by tests. An agent cannot load the design whole, and no section is safe to read alone, because invariants are scattered among rationale and host trivia. Finding: PRD-09.

### 3.3 What has run and what has not

| Has run on a real host | Has only run against fakes |
| --- | --- |
| Capture on both hosts | The background update on any real model |
| The resume brief in the CLI and the desktop app's Code tab | Promotion to rules and knowledge |
| Install, and setup of a wiki on a self-hosted GitLab | Recall on a prompt against real knowledge |
| Releases cut from a Mac and from GitHub | The daily commit and push over a working week |
| | The MCP server and both skills |
| | `eva-md upgrade` and a whole uninstall |

## 4. What the outside world tells us

### 4.1 What works in memory for coding agents

The field converged in 2026 on the shape eva-md already has: Markdown files in git, written by a background model run, with a small always-loaded part and on-demand reads. Storage form no longer sets systems apart. Fidelity of what is written, handling of stale facts, precision of what is injected and invisibility do.

The evidence is mixed, and the plan takes the direction the studies share:

| What the evidence says | Where it comes from | What it means here |
| --- | --- | --- |
| A compact handoff note is the best-supported feature: a successor needs 20 to 59% fewer steps and 42 to 63% fewer prompt tokens. Switching between tasks costs 12 to 14 points of success on both hosts, and routing each task its own history recovers most of it | Handoff and task-switching studies, 2026 | The brief and per-workstream state come first. They must be fresh at every boundary |
| Unsolicited injection is the riskiest feature. Near-miss context hurts more than none. One vendor's per-prompt recall made agents worse until it became a single cached synthesis | Context-rot and distractor studies; vendor report | Silence is the default. Inject only on strong, structural matches, and measure harm, not only hits |
| On real repository tasks most memory products did not beat no memory (11 of 12 pairings in one benchmark), though some did in another, at lower cost. Where they failed it was the form of the record, not ranking | Two coding-memory benchmarks, 2026 | Short, structured records anchored to a path or identifier. Never transcript chunks |
| Model-written context files and self-written skills do not help and can hurt (skills: 8 to 11 points worse on both hosts). Human-curated ones help | Repository-context and skills studies, 2026 | Store only what the repository cannot say. No generated overviews. A procedure becomes a host skill only after a person reviews it |
| Facts cited to code and checked before use are the one design with published outcome gains (merge rate 83% to 90%). Wrong seeded memories were caught and corrected | GitHub Copilot Memory | Every code-bound fact carries its anchors, and code checks them before the fact is shown |
| Lossy extraction is the main measured failure. Systems that keep the raw record beside derived facts keep over 90% of the detail; extraction-only systems about 40%. One vendor dropped its update-and-delete reconciliation because it destroyed context | LongMemEval; staged diagnostics; Mem0's 2026 rewrite | The raw record is the truth. Derived pages point back to it and can be rebuilt from it |
| A model that rewrites accumulated state collapses it (18,282 tokens to 122 in one run). Small deltas merged by code do not | ACE | The model proposes operations on items. Code applies them. No page is rewritten by a model |
| Agents do not reliably choose to look: a skill went unused in 56% of runs where an 8 KB always-present index scored 100% | Vercel agent evals | A small always-present map in the brief. Tools and skills are the deep path, never the only path |
| Shown rules are obeyed about half the time, whether all rules or only the relevant ones are shown. Both hosts' documents say memory is not the place for rules that must hold | Rule-compliance study; host docs | Checkable rules need enforcement at the tool call, and the host's own instruction files, not better retrieval |
| Memory is an injection path: stored payloads steered later sessions in most trials, and "install this dependency" from a preferences file succeeded 80 to 90% | Poisoning studies; both vendors' warnings | Trust tiers. Text that entered through a tool, a web page or a file may be recorded as what happened; it may not become a rule or a fact without the user |
| Lexical search is a strong baseline once tokens respect identifiers; keys written at write time lift it further; static embeddings sit below it on technical text; graphs cost more than they return | Retrieval studies; Mem0 removing its graph store | One lexical core with write-time cues. Embeddings only if our own measurement shows a gap they close. No graph, no vector store |
| Quota is the loudest complaint on both hosts' trackers. Codex's own memory writer uses a small model at low effort and stands down under 25% of the rate limit remaining | Host issue trackers; Codex source | A small default model for the per-chat pass, quota and idle gates, and a cost figure the user can read |

### 4.2 Where the hosts are

Read from documentation and source on 2026-11-10. Sources are in [working-papers/](working-papers/). Each is confirmed on a real host before anything is built on it.

**Claude Code (2.1.321)**

- 33 hook events. Hooks fire inside subagents too. Context can be added at session start, on a prompt, before and after a tool call, and at a subagent's start, up to 10,000 characters a string. Past that the text is silently replaced by a file path the model is not asked to read.
- Compaction can be steered: what a `PreCompact` hook prints becomes instructions for the summary, `PostCompact` receives the summary, and `SessionStart` fires again with source `compact`, which is the only way to put context back.
- A hook can run in the background and deliver its context on the next turn. A hook can call a tool on an MCP server the host already runs.
- Session end gives plugin hooks 1.5 seconds in all. A prompt hook also fires for prompts no person typed.
- The transcript format is declared unstable and files are deleted after 30 days.
- Mods, released 2026-10-01, are JavaScript in a plugin that runs inside Claude Code: they see every conversation row, add context, call a model on the user's plan, run timers and draw small UI. The API has changed in most releases since, can be switched off remotely or by an administrator, and is partly blocked on team and enterprise logins. It is an enhancement to consider later, never a foundation.
- One plugin folder now installs in Claude Code, Cowork and Chat, each loading a subset: Chat takes only skills and remote MCP connectors, and a top-level `bin/` folder blocks install on Chat and Cowork. Cloud sessions and Cowork never read `~/.claude`. Plugins enabled on the account sync to the terminal and, by changelog evidence, to cloud sessions.
- Claude Code's own auto memory is on by default: Markdown notes per repository on one machine, an index of 200 lines or 25 KB loaded every session. "Remember this" goes there. Its folder can be moved with a setting, which is the vendor's stated answer to portability.
- Headless `claude -p` uses the subscription today. `--bare`, which cannot, is documented as its future default. A billing change for programmatic use was announced and paused in June 2026.

**Codex (0.167.0)**

- Hooks are generally available, on by default, with 14 events. Context can be added at session start (again after a compaction), on a prompt, before and after a tool call, and at a subagent's start, about 2,500 tokens a message. Hook-added context does not survive compaction.
- Every hook a person did not get from an administrator must be reviewed once. Since 0.164.0 a plugin's approval is bound to a hash of every file in the plugin folder, computed again on each hook run: any changed or added file, even a stray `.DS_Store`, switches the plugin's hooks off until they are reviewed again. A user-level hook is bound only to its own declaration.
- A hook that exits in time may leave a detached helper running. Codex has no file-read tool for a hook to match: reads are shell commands, edits are patches.
- Rollout files are not an interface: the format is changing, files are compressed after 7 days and some threads are not on disk. The app server's thread API is the documented reader.
- Codex's own memories are off by default: a small model extracts from idle chats, a second pass consolidates, and one summary cut to 2,500 tokens is loaded at the start of a thread.
- `codex exec` has an output schema and an ephemeral mode and spends the plan allowance. Plugins with hooks are barred from the public plugin directory. The IDE extension has no plugins.

**What a third-party layer must add.** Both hosts now remember on their own, per repository, on one machine. What neither does is what users ask for in the open: one memory across both agents and across machines, a view of what was loaded and why, facts checked against the code, per-branch work state, team sharing with review, and measured results. That is the ground eva.md should hold. It must not repeat what the host already injects.

## 5. What v2 is

### 5.1 The promise

When you open a new chat, clear one, come back after a compaction, switch branch or sit down at another machine, the agent carries on as if it were the same chat. It knows what was decided and why, what was tried and failed, and what must hold. It checks what it remembers against the code before it leans on it. You never set it up beyond one command, never curate it, never approve anything for it day to day, and never see it unless you ask what it remembers and why.

### 5.2 Principles

Seven principles decide the design. Each comes from a finding or from outside evidence, and each is used below to settle a choice.

- **A. The raw record is the truth.** What was said and done is kept, redacted, on the machine. Everything else (pages, indexes, briefs) is derived from it, points back to it and can be built again.
- **B. Code owns structure; a model proposes.** Ids, dates, speakers, provenance and page layout are set by code. A model returns small operations on items, each with the exact words it rests on, and code checks and applies them. No model rewrites a page.
- **C. Nothing ships unless a measurement moves.** Every change names the number it must move and is judged with memory off as a baseline. A change that cannot show its effect waits.
- **D. Check before telling, and stay silent when unsure.** A fact about code is checked against the code before it is shown. A weak match adds nothing. Wrong context costs more than missing context.
- **E. Use what the host offers, as a ladder.** Each host capability is a rung: where a rung is missing, blocked by an administrator or broken by a release, the product gets plainer and keeps working.
- **F. It runs itself.** It schedules, repairs and upgrades itself, and speaks only when a person alone can act, in one plain sentence.
- **G. Trust is a feature.** Local first. No telemetry, no account, no process left running. What was loaded, and why, can always be seen.

### 5.3 What it will not be

- No graph database, no vector store, no resident server, no local model.
- No model call or network call inside a hook.
- No telemetry, and nothing about your work sent anywhere but to the model provider the chat already uses.
- No generated overviews of a repository, and no memory of what the code already says.
- No rule, fact or skill promoted from text that entered through a tool, a web page or a file, without your word.
- No copy of what the host's own memory already loads.

### 5.4 What "invisible" means, in numbers

These are the bars v2.0 is built to. They are proposals until Stage 0 gives v1's real figures; where v1's column says "not measured", Stage 0 measures it.

| Moment | v1 today | v2.0 bar |
| --- | --- | --- |
| First run | 4 questions, up to 4 approvals, about 45 lines printed, a repository and a folder to choose | 0 questions, 0 approvals, at most 12 lines, one statement of what is captured and what a model is sent |
| Steps the host imposes | Codex asks for a hook review, and again after most upgrades | one Codex review, then none for any release |
| Upgrade | find out, run a command, answer a question | a daily check and one command that asks nothing, which an agent can run. Background install later, if you agree (decision 9) |
| Commands typed in an ordinary month | upgrade, doctor, and whatever the doctor names | 0 |
| A chat start | up to 8,000 characters chosen by recency; one visible line | about 2,000 tokens chosen for the work in hand; a brief reaches at least 99.5% of starts, clears and compactions; under 150 ms at the 95th percentile |
| A prompt | capture under 50 ms; recall by word overlap | capture and delivery together under 50 ms; most prompts add nothing |
| A chat's memory on file after it ends | minutes to the next day; not measured | within 90 seconds for half of chats and 5 minutes for nine in ten |
| What the background run costs | the chat's own model at the highest effort, up to 40 calls a host a day; never totalled | at most a fifth of v1's measured tokens per chat, inside a budget shown as one line |
| A daily signing or sign-in prompt | possible every day | never |
| Memory stops working | never noticed by a person with one host | noticed at the next check by a part that still runs, and said in one line |
| Notices on a healthy install | one at every Claude Code start while Codex is unapproved, for good | none in a replayed healthy month |
| Wrong context added to a chat | not measured | at most 1% of additions on a held-out set; no measurable harm on tasks memory cannot help |

## 6. The target design

The audit, the research and six area designs agree on the shape. A critique of the six designs then found that, taken whole, they would build a second system beside the first: 128 work packages, 51 decisions for the owner, 43 commands against v1's 19, and a codebase that would nearly double before anything was deleted. The outside evidence says memory systems fail by doing too much. So this plan draws a line.

**v2.0 is what the evidence asks for.** A brief that hands work over cleanly. Small, cited statements whose author is decided by the record and checked by code. A store that commits every change and can explain and undo it. A background worker that loses nothing. An install that asks nothing. And the instruments that show each of these is better than v1. One writer, one machine, with a pushed backup.

**Everything else waits for a number.** Sync between machines, background self-install, a richer lifecycle for facts, recall while a tool runs, lexical recall on prompts, embeddings, tools that let an agent write memory, rule enforcement, and reach beyond macOS and Linux each have a place in the design and a trigger in section 12 that would open them. None is built on a guess.

Two guards hold the line. The shipped code is no larger at the end of any stage than v1 is today (54,807 lines of Go outside tests), checked in CI, and every rebuild names the v1 files it deletes. And no part of v2 is switched on by default until its measurement beats v1's on the same instrument.

### 6.0 The parts

```
 host hook event
      │
      ▼
 eva-md hook ── guards · pause switch · redact · append ──► THE RECORD (local, redacted, never committed)
      │                                                       read through one reader: typed events,
      │  after its output is written                          stable ids, whose words each text is
      ├─► wake ─────────────────────────────────────────────┐
      │                                                      ▼
      │                                      THE WORKER (one at a time, nothing lost)
      │                                        · reads the host's transcript to fill what hooks missed
      │                                        · organizes each settled part of a chat, once:
      │                                          a small model proposes operations with exact quotes,
      │                                          code checks every quote and decides whose words it is
      │                                        · commits, pushes, checks health, looks for a release
      │                                                      │
      │                                                      ▼
      │                                      THE STORE (a git repository eva-md owns)
      │                                        one small file per fact, decision, constraint, procedure;
      │                                        one file per workstream; one per organized session;
      │                                        one commit per pass
      │                                                      │
      │                                                      ▼
      │                                      THE PACK (local, compiled from the store, rebuilt at will)
      ▼                                                      │
 what the chat is given  ◄───────────────────────────────────┘
   at start, after a clear or a compaction, on the first prompt, on an exact match;
   two read tools for everything else
```

What happens to each part of v1:

| v1 part | v2.0 | Later, on a trigger |
| --- | --- | --- |
| Hook shell, guards, latency rules | keep | |
| Capture of prompts, replies, tool lines | keep; add what the user decides in host dialogs, what subagents report, and short failure digests | text between tool calls |
| Transcript recovery inside hooks | move to the worker; delete the slicing machinery | a held spool and a new file layout |
| Inbox files | keep; read through one reader with stable ids | session-keyed layout |
| Redaction rules and tests | keep; new single-pass driver, measured rates, the baseline as a test oracle | secret handles, custom patterns |
| Project key | keep; add an identity record with aliases | |
| Six update triggers and the daily sweep | replace with one worker that computes what is pending | |
| The model's prompt and the checker | rebuild as one contract | a second, reviewing model pass |
| Thread, rules and knowledge pages | rebuild as items and workstreams | candidates, disputes, archive by disuse |
| System pages | freeze; reachable on request | a decision after measurement |
| The page library (okf) and its adapter | replace with eva-md's own codec; the format stays | |
| The wiki as your working clone, one commit a day | replace with a repository eva-md owns, one commit per pass | sync between machines |
| The brief | rebuild as typed sections from a prepared pack | |
| Recall on prompts by word overlap | replace with first-prompt routing and exact keys | lexical matching; recall at tool time |
| Search and show tools | evolve into two read tools on one tokenizer and one scope rule | |
| Guided setup | replace with a first run that asks nothing | |
| Plugins | keep on Claude Code; on Codex freeze the hooks in a part that never changes | |
| Health, doctor, status | rebuild small: lag against a budget, one line, one verdict | |
| Upgrade | signed manifest, daily check, one command that asks nothing | background install |
| Install core, release pipeline | keep | |
| Accuracy suite, benchmark | rebuild the scorer; add retrieval, continuation and latency gates | further suites as decisions need them |
| `docs/design.md`, `docs/verification.md` | freeze and split into specs per component and dated decision records | |

### 6.1 The record

**Goal.** One local, redacted, ordered account of every chat that the later stages can trust: each event has a stable id, a known author and an honest note of what was cut. A hook waits on nothing but its own small write. Nothing said while capture is paused is ever readable. A project keeps its memory through a rename, a first push or a moved folder.

**Bars** (hard gates in CI on recorded host fixtures): no text stored at or after a pause, under any pattern of missed hooks; no text stored as the user's typed words that was not; dialog answers and plan decisions captured with the right author in every fixture; a project keeps one identity through a scripted rename, first push, move and worktree. On the real install: at least 99.5% of turns and 99% of tool calls in the host's own record are in eva-md's.

**Design.**

1. *One reader.* Everything that reads the record goes through one package. It yields typed events with an id (session and sequence), an author, a set of exposure marks, and marked cuts. Five separate decoders in v1 become one. For v1's files the sequence is the line number, so every reference already written in a wiki still resolves.
2. *Whose words a text is, by allowlist.* Each event carries an author: typed by the user, pasted, chosen by the user from options the agent wrote, written by the agent, by a subagent, by the host, by a program, or unknown. It is assigned only on positive evidence, so a new host wrapper is unknown and never "typed". Separately, code marks a turn as exposed to the web, the network, an MCP result, eva-md's own memory or a subagent, from that turn's tool records. Promotion rules key on both. This replaces a deny-list that defaults to "the user typed it" (CAP-02).
3. *What is kept.* Added in v2.0, all through hooks: the question and the answer when the user picks an option in the host's dialog; a plan's text with approved or rejected; a subagent's task and final report, as the subagent's; a bounded, strictly redacted digest of a command that failed; and a record of what eva-md itself delivered into the chat (ids and hashes, not text), so restated memory is never taken for new evidence. Still never kept: the output of a call that succeeded, file contents, credentials in URLs.
4. *Hooks are the floor; the transcript only fills gaps.* The hook's payload is the documented contract on both hosts and the transcript is not, so at the end of a turn the hook's copy of the reply wins. Reading the transcript moves out of the hook into the worker, which removes about 1,200 lines that exist only to slice that work into 100 ms pieces.
5. *The pause switch as a property of order.* The reader exposes a session only up to the point the worker has checked for a pause typed in a prompt a hook missed. Later events exist on disk but no reader returns them until they are checked, and a pause found late removes them. The guarantee no longer makes every hook wait, and it holds in one place instead of five. The phrases are tightened (only a prompt the user typed can pause; resuming must be the whole prompt), there is an explicit command, both hosts show a confirmation, and a fork or resume inherits the state.
6. *Project identity.* The store holds an identity record for each project: an id, with every remote, root commit and old key that has named it. A chat's key resolves through it. Two kinds of evidence are needed before two keys are joined automatically; a person can join or split by command. Each folder outside git is its own project for new chats.
7. *Redaction.* The rules and their several hundred test cases stay. The driver is rebuilt as one pass that returns spans, runs to a deadline and can stop with a redacted prefix. A labelled corpus gives precision and recall per class for the first time. Then rules change one at a time under it: values are judged by character class and length (today one in five random 32-character hex keys is kept), `key: value` lines are read as structure, and the last gate before a push uses the same detector as capture. With your agreement (decision 7), the frozen baseline becomes a test that no true secret it removes is ever kept, instead of a second engine unioned in on every call; that is what lets non-secret facts about authentication survive.
8. *Forgetting.* By session, by turn or by event, from the command line, with a preview. A tombstone removes the text, and items that rest only on it are retracted.

**Kept:** the fail-closed hook shell, the guard list and its order, redaction before disk, path confinement, the transcript trust rules, the row classification rules and their tests, the baseline package byte for byte, the removal protocol.

**Removed:** the recovery pass inside hooks and the gate that withholds a brief while it is behind; the scanner's saved state, the slow-row ladder and windowed redaction; the flattened tool line and every parser of it.

**Later, on a trigger:** a held spool and a session-keyed file layout (if the counted week shows 2% or more of events held back, or under 95% of tool calls captured); text between tool calls; forgetting by value; expiry of working detail.

Detail: [working-papers/design/ledger.md](working-papers/design/ledger.md).

### 6.2 The store

**Goal.** Memory that is on disk seconds after it is derived, off the machine minutes later, never waiting for a person to repair git, and always able to answer "why does it think that" and "undo that".

**Bars:** no question and no network call before the store exists; a pass is committed within 2 seconds and pushed within 10 minutes; one reader of frontmatter where there are six; a lock a person wrote is honoured in every form YAML allows; no state in which a person must repair git by hand; a deleted store comes back from its remote and replays exactly what is missing; migration of a v1 wiki leaves no unit of text unaccounted for.

**Design.**

1. *eva-md owns the repository.* It lives in eva-md's data folder and is created at the first run with no question and no remote. Only the engine writes it, so it can clear its own locks, abort its own interrupted work and re-clone. A remote is attached, changed or removed later with one command that keeps setup's privacy checks (public refused, internal only on an explicit yes).
2. *Items and sessions are files.* One small Markdown file per fact, decision, constraint or procedure, as v1's knowledge pages already are; standing rules and thread decisions join that family. One file per workstream. One file per organized session, holding its log and the range of the record it covers. Ids are 14 characters, sortable by time, and cannot collide between machines. A file's path never changes. Nothing volatile is committed: no timestamp is restamped on save, and indexes, maps and rendered views live in local state, rebuilt at will. These choices cost little now and are what makes sync possible later.
3. *One write path, one commit per pass.* `store.Apply` takes a pass's operations, writes the files and makes one commit whose trailers name the pass, the session and the model. The range of the record that the pass covered is stored in the tree in the same commit, so progress is what the store holds: a restored store says exactly what to replay (STO: replay). `eva-md log`, `why` and `undo` read that history; the smallest thing that can be undone goes from a day to a pass.
4. *Protection lives in the store and fails closed.* A file a person locked or verified, or one that cannot be read, refuses the one operation aimed at it. The pass and every other file go on. Today such a page stops a whole branch's memory, and `locked: true  # note` is read as not locked.
5. *eva-md's own codec.* One line-preserving reader and writer for every file, which keeps every line a person wrote. The page format stays conformant with OKF v0.2, as a written profile and a test. The okf module is dropped (decision 1): the case is control, since a merge by field and fail-closed protection need a reader that keeps lines, which the library's parse-to-struct model cannot give. The binary then carries one third-party module.
6. *Commits are eva-md's, not yours.* Authored as "eva.md on <machine>", and never asking for your signature. Whether they are signed with a key eva-md makes for the machine is settled in Stage 0, once the git server says what it accepts (decision 3).
7. *A migration path before a new layout.* A deterministic, resumable step turns a v1 wiki into a v2 store, proven on generated wikis and on a dry run over a copy of yours that prints counts only. The old clone is never touched and stays as the way back. A binary older than the store's minimum reader version reads nothing, writes nothing and says so once.

**Kept:** the hardened git runner and the remote guard, no-clobber create and compare-and-swap, the durability order, path and link safety, the secret gate on every line written, setup's privacy rules, the escaper.

**Removed:** the okf module and adapter, `log.md`, every committed index and map, the daily lump commit and its markers, eight of eleven commit guard refusals, five of six frontmatter readers.

**Later, on a trigger:** fetch and merge between machines, with typed merge rules per field and a trust rule for commits that did not come from an enrolled machine; a human checkout with rendered pages; purging history; short excerpts committed beside items.

Detail: [working-papers/design/store.md](working-papers/design/store.md).

### 6.3 Organizing

**Goal.** Every settled part of every chat is organized exactly once into small, cited, typed records, soon enough that the next chat starts from them, at a cost you do not notice. Nothing is passed over without a record of why and a way to try again. What you stated is changed by nothing but you.

**Bars:** no part of the record left neither organized, skipped with a reason, refused with a reason, nor pending; no wake lost; nothing of the agent's, a tool's, a paste's or an outside source's stored under your authority; at least 90% of planted facts stored with the right value on a real model; at most 5% of faithful statements refused; at least 80% of decisions made by agreeing to a proposal become durable; after a host change that breaks output, at most 3 model calls burned and nothing lost.

**Design.**

1. *One worker, nothing queued.* Pending work is computed: what the record holds, minus what the store says is covered. A hook's wake is only a hint, so a wake that finds the worker busy loses nothing. The worker decides when a part of a chat is ripe (the chat ended, went quiet, grew long, or was asked for). This replaces six triggers, a daily sweep and state rebuilt by scanning two days of logs.
2. *One contract between the model and the checker.* The model is given whole turns and returns small operations on items it was shown by handle: add, supersede, retire or confirm an item; add or close a next step or an open question; one status line for the workstream; notes of what was done, found or failed. Silence means keep. It never writes an id, a date or a page.
3. *Evidence is exact words.* Every operation cites one to three spans: the entry and the exact words in it. Code checks each by exact match. Who said the words comes from the record's author field, never from how alike two texts look. That deletes about 2,100 lines of word-overlap matching with English word lists, and works in any language. A short "yes" after a proposal, a dialog choice and a plan approval count as you accepting the agent's words, with both spans kept (decision 10).
4. *Acceptance per operation.* One refused operation no longer discards a whole answer. A refusal carries a code, and the model gets one chance to repair it. An answer is rejected whole only if it is not the schema's JSON.
5. *Admission: default to nothing.* An item is stored only with authority: you stated it, you accepted it, or a tool record shows it. Each add names what it is (an instruction, a decision, an invariant, a fact of the environment, a pointer, a failure, a command). Anything derivable from the repository, and anything from a turn exposed to outside text without your word, stays a cited note in the session's log, where it can be found on request and is never pushed. "Wrote nothing" is a counted answer.
6. *Who may change what.* Only your later words change an item under your authority. An observation that contradicts it is recorded beside it and shown as a disagreement; it does not replace it (today it can: PGS-07).
7. *Workstreams.* A workstream is bound to its branch where a branch other than the default exists. Work on the default branch and outside git gets a workstream per folder or topic. When a branch merges, its decisions fold into the project by code.
8. *A small model, measured.* The default is the cheapest model and effort that meets the fidelity bar on the frontier measured in Stage 0, with one step up when the checker refuses (decision 5). The runner reads the host's event stream, so a tool call or a different answering model cannot pass unseen, and it records the host's version. A timeout is a load signal: retry smaller and lower. A failure nobody recognizes is the host's, not the chat's: three in a row pause organizing for that host version, and a range is blamed only if another range succeeded in between.
9. *System pages* are frozen: no new ones are prompted, existing ones stay reachable on request, and they leave the default brief and prompts. They are model-written overviews of a repository, the kind the evidence found unhelpful, and nothing here has measured them.

**Kept:** the sealed run (stdin, cleared environment, tools off twice over, no transcript left), the strict schema both hosts accept, the anti-forgery rendering, the rule that history from before updates is never summarized by surprise.

**Removed:** the word-overlap provenance and its word lists; the watermark that moves past refused content; quarantine that ends in a skipped chunk; the six trigger thresholds and the sweep; the full-state protocol with a "keep" for every id; model-written summaries; the chat's model at the highest effort as the default.

**Later, on a trigger:** items held as candidates until they recur, expiry and archive by disuse, lessons from failures and a preference kind (when the corpus counters show a need); a second, reviewing model pass (when near-duplicates and disputes exist that code cannot settle); an opt-in replay of old chats under the new contract.

Detail: [working-papers/design/derive.md](working-papers/design/derive.md).

### 6.4 Recall

**Goal.** The agent has the right piece of memory in its context at the moment it matters, in a form it can use, and nothing else. A new, cleared, compacted or resumed chat continues the work. A person can see what a chat was given and why.

**Bars:** from a start brief of at most 2,000 tokens, a successor needs at least 40% fewer steps and 50% fewer prompt tokens than with the repository alone, and solves no fewer tasks; corrections per task at least halved on tasks with a hidden decision; a brief reaches at least 99.5% of starts, clears and compactions under injected failures; no item of another project in anything pushed; at least 95% of claims about code that has moved are held back or labelled.

**Design.**

1. *A compiled pack.* A background step compiles the store into a small local file set per project: the brief's sections, exact keys (paths, symbols, commands), and rows for the read tools. Hooks read only the pack and the record's session catalog. No git, no page parsing, no scan of raw files, no model. Chat-start cost stops growing with the inbox.
2. *The brief as typed sections.* A legend, standing rules, the constraints that fit the work, the workstream's state (status, next steps, open questions, decisions in force), other active workstreams, a handoff built by code from the last session (opening request, files edited, open tasks, the last check and its result), the last exchange word for word, and a short map of what else exists. Sections have floors and ceilings in tokens and are fitted by priority, so nothing parses the brief's own text back. The handoff needs no model, which is what makes the state fresh at every boundary. Output is byte-stable when nothing changed, so it does not break the host's prompt cache.
3. *Each kind of chat gets what it lacks.* New and cleared chats get the start brief. A compacted chat gets what the host's own summary does not carry, and the constraints again. A resumed chat gets only what other sessions changed. A subagent, and a chat a program runs, get rules and constraints and nothing else (decision 14). A brief that a start failed to deliver is owed and is delivered on the next prompt, whatever its kind.
4. *The first prompt routes; later prompts are nearly silent.* The first prompt of a chat picks the workstream that fits it and brings up to five statements. After that a prompt adds only what another session changed, and items whose exact key (a path, a symbol, a command) appears. There is no matching by loose word overlap in v2.0. The evidence ranks these far apart: a compact start is the best-supported feature, and per-prompt injection the riskiest.
5. *What a chat holds.* A set of item ids per context window, reset on compaction, replaces the "sent once per chat" list that today suppresses a constraint for good once a compaction has removed it.
6. *Checked before told.* Each item's code anchors are checked against the repository in the background, as v1 already does for system pages. An item whose code is gone is held back; one whose code moved is labelled. Held and disputed items are never pushed.
7. *Two read tools.* One finds (a question, a path or an error, staged from this project outward, with a token budget and paging) and one opens by handle with its neighbours and evidence. They replace three tools that re-expose the command line. Chat search moves to the same tokenizer, matches across a turn instead of a line, and loses its 90-day window.
8. *One scope rule for every surface.* Hooks, the command line and the MCP server read through one access table. Reading across projects is closed by default: this project, items marked for all projects, and projects you have explicitly shared (decision 13).
9. *Receipts.* Every delivery is recorded as ids and hashes. `eva-md receipts` shows what a chat was given and what was held back.
10. *Beside the host's own memory.* eva-md never writes or configures it. The two divide the work: the host's memory keeps preferences on one machine; eva-md keeps work state, decisions and constraints across both agents. Overlap is counted, and the host's memory is a condition in every experiment.

**Kept:** the fence and the forgery defences with their tests, strict bounded reads of anything injected, fail-closed silence on any error, the privacy gates, the hard caps under each host's limit.

**Removed:** the periodic reminder line; every injected line that hands the agent a shell command; recall by word overlap and project-name detection in prompts; sibling scope inferred from an agent-written list; the recall skill and the `eva_status` tool.

**Later, on a trigger:** delivery when the agent edits a path or runs a command (a Claude Code experiment first); lexical matching on prompts (only if a held-out set shows precision of 0.95); embeddings (a numeric trigger, never in a hook); generated rules files and reviewed skills in the host's own folders; tools that let an agent write to memory; asking the host to confirm a tool call that breaks a rule you armed.

Detail: [working-papers/design/deliver.md](working-papers/design/deliver.md).

### 6.5 Hosts and operations

**Goal.** A person installs eva-md with one action and answers nothing. From then on it keeps itself current, repairs what a program can repair, and speaks only when a person alone can act.

**Design.**

1. *Hosts behind one interface.* What is known about a host (its capabilities by version, its quirks, where its CLI is) sits in one adapter with a table. Host names leave the rest of the code, where they appear in 29 files today. A third host becomes a table and an adapter, not edits across packages.
2. *Codex: the hooks in a part that never changes.* Codex binds a plugin's approval to a hash of every file in it. So the hooks go in a two-file plugin that is frozen, with all routing in the binary behind a fixed path, and the skills and the MCP entry go in a second plugin that changes freely. The v1 plugin is already approved and the v2 binary can run behind it, so the move to v2 costs no review. Nothing is ever written inside a plugin's folder. Claude Code binds no approval to plugin bytes, so one plugin stays there.
3. *An install manifest.* Each install records the files it rendered, with hashes, so any release can replace or remove another release's files and still refuse files it never wrote (OPS-07).
4. *A first run that asks nothing.* One command registers the plugins and creates the store locally. It prints, once, what is captured, what a model is sent, and how to exclude a project or stop. Preferences a person edits are kept apart from state the program owns, and a broken preferences file falls back to the last one that validated instead of switching memory off.
5. *A clock.* With your agreement (decision 4), one short-lived run every 15 minutes, started by the operating system, wakes the worker when no chat is open: it pushes, checks health and looks for a release. Nothing stays running. Without it, hook wakes carry the same steps while a chat is open, as v1 does today.
6. *Health that sees missing work.* For each host and each stage (host activity, capture, organizing, commit, push) eva-md keeps the time of the last success and compares the lag with a budget. That is what turns "nothing recorded a failure" into "capture has been silent for two days while the host was in use". The most severe condition that is still relevant is shown, once a day, as one line prepared outside the hook. A condition for a host you no longer use fades. Repairs a program can make are made; the rest is one plain ask.
7. *`status` is a verdict.* Six lines at most, with a cost line in tokens and share of budget. The 50-row doctor moves behind a flag. The command surface a person needs comes down to about a dozen verbs.
8. *Upgrades that can be trusted.* Every release carries a manifest signed in the release workflow and verified offline with the standard library. The worker checks daily. `eva-md upgrade` asks nothing, so an agent can run it when a notice says a release is waiting. The previous binary is kept.
9. *Linux.* Release assets for Linux, which already builds and passes the tests. Setup stops refusing it.

**Kept:** the transactional install and rollback, registration only through the hosts' own commands, never granting Codex trust, checksum before run, the release pipeline and its provenance.

**Removed:** the guided setup dialogue and its repository-hosting code (about 1,000 lines), `config.env`, setup as the upgrade mechanism, the trust flag that never expires, heartbeat files written on every hook.

**Later, on a trigger:** installing releases in the background with probation and rollback; a floor for hosts that have MCP and no hooks (which reaches the Claude desktop app's Chat tab); cloud sessions; a kit for managed machines; an operating-system notification as a last resort.

Detail: [working-papers/design/hosts.md](working-papers/design/hosts.md).

### 6.6 Proof

**Goal.** Anyone can tell whether eva-md's memory is good, getting better or getting worse, from numbers a command prints.

The harness lives in the repository and gates releases. No release binary contains it, and the product ships only counters that hold no content. Four instruments are built first, and v1 gets a row on each before anything is rebuilt:

| Instrument | What it measures | Runs |
| --- | --- | --- |
| Fidelity | Scripted chats with planted facts, decisions, corrections, dates and reversals. A black-box scorer reads only the store's output and scores each fact by value, with a precision side that counts what no span supports. v1's 12 scripts plus about six families the new contract changes, half held out | offline in CI with recorded model output; live on each host with your leave |
| Retrieval | Labelled prompts against a real index, with paraphrases, near misses, sibling-project distractors and prompts that should bring nothing. Precision, recall, false additions, silence | offline |
| Latency | Hook time at realistic sizes, and for a whole turn of 50 tool calls: processes started and milliseconds summed, on each host's launch path. Fails a build on macOS | offline |
| Continuation | 20 tasks split across two sessions: 10 handoffs and 10 with a decision hidden in the first session. Run with memory off, with eva-md, and with a hand-placed ideal record; the host's own memory as a fourth condition on a signed-in Mac. Graded by hidden tests, steps, tokens and corrections, never by a model | live, with your leave and inside a budget you set |

Further suites (task switching, rule compliance, drifted code, poisoning, aging over 200 sessions, a corpus audit) are each built when a decision in this plan waits on its number, not before.

**What "best" will mean.** There is no accepted ranking for coding-agent memory, so the claim is an evidence package: resuming is cheaper than with memory off and no worse than the host's own memory; decisions stick; nothing of another author is stored under your name; nothing is lost; and every number has a command that reproduces it. It will not be a rank against named products or a score on a conversational benchmark.

Detail: [working-papers/design/evaluate.md](working-papers/design/evaluate.md).

<!-- PART-7-ON -->
