# eva.md v2: the plan

**Status:** draft for the owner's agreement, written 2026-11-10. Nothing here is built. A field report from the one real install is still to come ([the prompt for it](field-report-prompt.md)); the plan will be revised against it before work starts.

**What this is:** an evaluation of eva-md as it stands, and the design and order of work to make it the best memory there is for AI coding agents: one that feels built into the host, where the only thing a person notices is that the agent remembers.

**Companion files:** [findings.md](findings.md) lists every audit finding by id. [working-papers/](working-papers/) holds the audits, research notes, area designs and critiques this plan was distilled from. Where they differ from this file, this file governs.

## 1. The short version

**Where v1 stands.** eva-md is careful work, and much of it should be kept as it is: hooks that cannot hurt the host, redaction before anything reaches disk, a resume brief built by code, a sealed model run, a crash-safe page store, a transactional install. But the half that makes it a memory (summarizing chats, promoting lasting facts, recalling them, committing them) has only ever run against canned model output, and nothing measures whether memory helps a chat. The audit kept 252 findings. They trace to ten structural choices, set out in section 3.2, and none of them is fixed by patching.

**What the evidence says to build.** On real coding tasks most memory systems do not beat having no memory, and they fail by storing and injecting too much. What works is narrower than what v1 attempts and narrower than what a first redesign reached for: a compact brief that hands work over, short statements whose author is known and which are checked against the code before use, the raw record kept beside them, and silence when unsure.

**The proposal.** Rebuild five things, properly, and measure each against v1:

1. **One contract for organizing.** A small model proposes operations with the exact words they rest on. Code checks every quote and decides whose words they are from the record, not from how alike two texts look.
2. **A store eva-md owns.** One small file per fact, one commit per pass, every change explainable and undoable, created with no question and backed up when you attach a remote. eva-md's own codec replaces the library it was built on; the file format stays.
3. **A worker that loses nothing.** It computes what is pending instead of trusting triggers, reads the host's transcript off the hook's path, and runs on a clock.
4. **A brief built for the work in hand.** Typed sections from a prepared pack, fresh at every boundary, the constraints restated after a compaction, and near-silence on later prompts.
5. **An install that asks nothing**, health that notices missing work, and upgrades that are one command an agent can run.

Before any of that, **four instruments** give v1 a baseline: fidelity of what is written, precision of what is retrieved, latency, and whether a second session really continues the first.

**The cut line.** A critique of the full design found it would build a second system beside the first. So v2.0 is one writer on one machine with a pushed backup, held to a size budget: the shipped code may not grow past v1's. Sync between machines, background self-install, recall at tool time, a richer lifecycle for facts, embeddings and reach beyond macOS and Linux are designed, and each waits for the measurement that would justify it (section 12).

**What it will feel like.** No questions at install. No daily signing prompt. One Codex review, ever. A chat's memory on file within minutes of its end, at a fifth of today's cost or less. A brief that fits the task. Nothing to type in an ordinary month. Section 5.4 has the numbers.

**What is needed from you.**

1. Run the [field report prompt](field-report-prompt.md) on the real install and bring the report back. The plan is revised against it before anything is sized.
2. Answer the 26 decisions in section 7. Each has a recommendation and a fallback, so "agree, except these" is enough.
3. For Stage 0: about an hour with both hosts for a host-facts sitting, one small v1 upgrade and a week of ordinary use, and leave to spend some model quota on live runs.

**What happens next.** Stage 0 measures. Stage 1 builds the instruments and ships improvements on v1 that change no data. Stage 2 is one rebuild and one cut-over, with the old wiki left untouched as the way back. Stage 3 closes small owed fixes and opens whatever a number asks for. Stage 4 is a week of proof on the real install and the release. The plan is revised at every gate.

## 2. How this plan was made

The plan rests on four passes, each checked by a second reader who was told to try to prove the first one wrong.

| Pass | What was done | Result |
| --- | --- | --- |
| Audit | 17 parts of eva-md read in full, code first and documents second, each against seven questions: memory quality, recall quality, invisibility, reliability and safety, reach, architecture, evaluation | 252 findings kept: 7 critical, 85 high, 126 medium, 34 low. Of 204 first-pass findings the second reader confirmed 131 and corrected the scope or severity of 73; it refuted none outright and added 48 of its own |
| Research | Six topics from primary sources as they stood on 2026-11-10: Claude Code and the Claude apps as a platform; Codex as a platform; retrieval that fits one static binary; memory tools for coding agents; general agent-memory systems; how memory is evaluated | 153 load-bearing facts re-checked against their sources: 121 confirmed, 31 corrected, 1 refuted |
| Design | Six area designs built on the audit and the research: the record, the wiki, organizing, recall, hosts and operations, proof | Six designs, each with its bars, contracts, migration and work packages. Taken whole: 128 work packages and 51 decisions for the owner |
| Critique | Three critics over all six designs: will it work, is it the smallest design that meets the bar, can a team of agents build it in this order | The simplicity critique found the six designs would build a second system beside the first (128 packages, 51 decisions, 43 commands) and set the cut line of section 6. The delivery critique found the order would not hold (work that was serial, v1 code deleted before the way back closed, a cut-over with no owner) and gave the stages of section 8. <!-- FEASIBILITY --> |

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

Two guards hold the line. First, a size budget: v1 ships 54,807 lines of Go outside tests, and v2.0 may not ship more. Because v2 is built beside v1 until the way back has closed, every package that adds code names the v1 files it will delete, and CI checks that shipped lines minus lines scheduled for deletion stay at or under v1's. The budget becomes a plain ceiling once the deletions land. Second, no part of v2 is switched on by default until its measurement beats v1's on the same instrument.

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
2. *Items and sessions are files.* One small Markdown file per fact, decision, constraint, procedure or standing rule, as v1's knowledge pages already are; rules and thread decisions join that family. One file per workstream. One file per organized session, holding its log and the range of the record it covers. Ids are 14 characters, sortable by time, and cannot collide between machines. A file's path never changes. Nothing volatile is committed: no timestamp is restamped on save, and indexes, maps and rendered views live in local state, rebuilt at will. These choices cost little now and are what makes sync possible later.
3. *One write path, one commit per pass.* `store.Apply` takes a pass's operations, writes the files and makes one commit whose trailers name the pass, the session and the model. The range of the record that the pass covered is stored in the tree in the same commit, so progress is what the store holds: a restored store says exactly what to replay (STO-12). `eva-md log`, `why` and `undo` read that history; the smallest thing that can be undone goes from a day to a pass.
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

The designs differed on five points of the memory model. These are settled here, so the store's format can be specified first:

| Point | Ruling for v2.0 |
| --- | --- |
| Kinds of item | Five, matching what v1 holds: `fact`, `decision`, `constraint`, `procedure`, and `rule`. A rule is a standing instruction to the agent and comes only from words you typed. A constraint is a limit on the work or the system |
| Working items of a workstream | Next steps and open questions, each a line with an id |
| What a workstream file says | One cited status line that the model maintains from the previous one, then its next steps, open questions and the decisions in force. No model-written paragraph |
| What a session file holds | A log of notes built from operations (did, found, failed, left) and the range of the record it covers. No model-written summary |
| States of a workstream | `active`, `merged`, `archived` |
| Ids of a workstream | Minted by the store from the project and the branch where there is one, so the same branch is the same workstream everywhere |

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
2. *Codex: the hooks in a part that never changes.* Codex binds a plugin's approval to a hash of every file in it. So the hooks go in a two-file plugin that is frozen, with all routing in the binary behind a fixed path, and the skills and the MCP entry go in a second plugin that changes freely. Releases before the cut-over change no plugin byte, so they cost no review. The frozen plugin is installed once, when you run the upgrade to v2.0, and that review is the last. Nothing is ever written inside a plugin's folder. Claude Code binds no approval to plugin bytes, so one plugin stays there.
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

## 7. Decisions for the owner

Each row changes a recorded decision or a rule in `AGENTS.md`, or is a product choice only you can make. Every one has a recommendation and a designed fallback, so "agree with all recommendations except these" is a complete answer. Decisions that only matter for work deferred past v2.0 are in section 12 beside the item they belong to.

| # | Decision | Recommendation | If you say no | What it reopens |
| --- | --- | --- | --- | --- |
| 1 | Drop the okf module and own the page codec, keeping the format conformant with OKF v0.2 | Yes | Each file is saved through okf behind today's adapter. Comments and hand-written frontmatter are not preserved, its lock file sits in the store, and every okf release stays a risk | Decision 15; the rule that okf stays pinned; two dependencies become one |
| 2 | eva-md owns the memory repository in its own data folder, created with no question and no remote; a remote is attached later | Yes | The store stays the folder you chose and setup keeps its questions. The guard refusals stay, and a stray lock still stops memory until a person acts | Decisions 1 and 16 |
| 3 | Commits are made as "eva.md on <machine>", one per pass, and never ask for your signature. Sign them with a key eva-md makes for the machine if the git server accepts that; otherwise leave them unsigned | Yes, settled by Stage 0's check of the server | Per-pass commits stay local and are squashed into one commit with your identity and signing at each push, as v1's daily commit is. The daily prompt stays and single-pass undo ends at the push | Decision 7 |
| 4 | One short-lived run every 15 minutes, started by the operating system (a launchd user agent; a systemd user timer on Linux) | Yes | Hook wakes carry the same steps while a chat is open. Nothing runs with every chat closed, so Friday's memory is pushed on Monday and a silent stop is noticed only at the next chat | "No sleeping jobs" |
| 5 | Organize with a small model at low effort, chosen from a cost and fidelity frontier measured on real hosts, with one step up when the checker refuses | Yes | The chat's own model at a low default effort, with the token budget kept. Several times the tokens per chat | Decisions 5 and 17 |
| 6 | Keep more of a chat locally: answers given in the host's dialogs and plan decisions (always), a subagent's task and report, and a bounded, strictly redacted digest of a command that failed | Yes, behind one setting | Dialog answers and plan decisions only. Lessons from failures and what subagents found reach memory only when a final reply restates them | Decision 16 (tool activity without output) |
| 7 | The frozen redaction baseline becomes a test oracle (no true secret it removes may ever be kept, as a hard gate) instead of a second engine unioned in at run time. The baseline package itself is never touched | Yes, once the corpus shows zero oracle violations | The union stays. Non-secret facts about authentication stay masked for good, both engines run on every hook, and a quoted key header still deletes the rest of an entry | The `AGENTS.md` rule on `redact.Redact` |
| 8 | A session file may be rewritten by one maintenance step that keeps every event id, so one line can be forgotten and a secret missed by an old rule can be repaired at rest | Yes | Forgetting stays whole-session, and a missed secret stays on disk until its whole chat is removed | "The inbox is never rewritten" |
| 9 | Upgrades: a signed manifest, a daily check, one line when a release has waited a week, and `eva-md upgrade` that asks nothing. Installing in the background is deferred | Check now; decide on background install when there are users whose fixes are waiting | Upgrades stay as v1: a person finds out, runs a command and answers a question | "No auto-update", only if background install is later chosen |
| 10 | A proposal you accepted (a short yes right after it, a dialog choice, a plan approval) becomes a durable decision, with both spans kept as evidence | Yes for decisions and procedures, never for standing rules | Typed words only. Such decisions stay as open questions on a workstream, as in v1 | Decision 5's promotion bar |
| 11 | Hold back an item whose cited code is gone, label one whose code moved, and record a contradicting observation beside an item. Nothing but your words changes an item under your authority | Yes | Label only. Facts about deleted code keep arriving with only a date to warn the agent | "Never changed without the user" (narrowed to: never changed, may be held back) |
| 12 | System pages: stop prompting for new ones, keep existing ones reachable on request, remove them from the default brief and prompts, and measure before investing further | Freeze and measure | Rebuild a system page as a view over cited items for its paths: a large package, for a feature the evidence does not favour | The system pages stage of the roadmap |
| 13 | Reading across projects is closed by default: this project, items for all projects, and projects you share explicitly. v1's inferred sharing becomes a one-time review | Closed, with the review | As v1: an agent may search every project, with the project named on each hit | A deliberate v1 choice |
| 14 | Subagents, and chats a program runs, get standing rules and constraints at their start, and nothing else | Yes | Nothing for either. Delegated work starts without the project's constraints | |
| 15 | Per-component specs and dated decision records replace `docs/design.md` and `docs/verification.md` as the source of truth, and the tests that police those two files' prose are replaced | Yes | The two files are split so agents can read them, but stay the truth and stay policed. Every behaviour change still edits shared files | Two lines of `AGENTS.md` |
| 16 | Live evaluation runs with standing leave inside a ceiling you set, instead of being asked for each time | Yes; set the ceiling after the Stage 0 pilot prices one session | Asked each time. Packages wait, integrated and unproven, until someone answers | The `AGENTS.md` rule on live runs |
| 17 | Install one v1 point release that adds only content-free counters and changes no plugin byte, use it for a week, and bring back a second report | Yes | The first report only. Capture loss and freshness are then decided on inference | |
| 18 | A candidate build may be replayed over your own local record, on your machine, printing counts only | Yes | Synthetic scenarios and the field report only. A rule that misbehaves on real phrasing is found after release | |
| 19 | Tighten the pause switch: only a prompt you typed can pause capture, resuming must be the whole prompt, both hosts show a confirmation, there is an explicit command, and a fork inherits the state | Yes | As v1: a pasted line can resume capture, and recovery can pause a chat without telling you | Decision 16's off switch |
| 20 | Each project gets an identity record with aliases, and each folder outside git is its own project | Yes | Aliases only. Renames and first pushes are fixed; folders outside git still share one area | Decision 10 |
| 21 | Is a second machine needed in v2.0? | No. v2.0 is one writer with a pushed backup, built so sync can be added | If yes: sync moves into v2.0 and adds a large package, a multi-machine test suite and the signing rule of decision 3 as a security boundary | Decision 1 (one wiki per machine), later |
| 22 | Rendered pages (a workstream's state, a project overview): local views, or committed so they read well on a git host | Local views; the repository holds items and sessions. Tell us if you read the wiki on the git host | A small set is committed and regenerated after every change, with commits caused only by rendering | Decision 2's "inspectable as plain Markdown" (kept either way through `eva-md show`) |
| 23 | Which systems v2.0 runs on | macOS and Linux | macOS only | "macOS only" |
| 24 | Beside the hosts' own memories: never write or configure them, divide the work, count the overlap, and test against them | Yes | If integration is wanted: read the host's memory as an input, later. Cost: dependence on an undocumented format | Decision 8 |
| 25 | The host asks permission the first time an agent uses eva-md's read tool | Write nothing in the host's files. `status` prints the one rule you may add yourself | If you want it written: one command shows the rule, writes it with a backup, and removes it on uninstall | "Never write host settings" |
| 26 | The cut line itself: v2.0 is section 6, held to the size budget, and section 12 waits for its triggers | Yes | Name what moves in. Each item in section 12 has its cost beside it | |

| 27 | Standing leave for the build team: agents may commit, push package branches and open pull requests; the lead merges packages whose checks and review are green; you alone merge changes to the release workflows and scripts, the gates and their accepted baselines, the frozen redaction baseline, the frozen plugin's files and `AGENTS.md`; and you cut every release | Yes | Every commit, push and merge is asked for. The build then moves at the speed of those answers | The `AGENTS.md` rule "don't commit or push unless asked", for the build only |

**When each is needed.** Before any code: 1, 2, 6, 15, 16, 17, 20, 22, 26 and 27, because they fix the formats and the method. At the Stage 0 readout, when Stage 2 is sized: 3, 4, 5, 7 to 14, 18, 19, 21, 23, 24 and 25. Until a decision is made, the build assumes its "if you say no" column, so no builder waits on an answer.

Choices the lead will make unless you object, because they are technical and reversible: v2 is built on the main branch in new packages behind flags that default to v1's behaviour; no model judges any gated number; ids are 14 characters; an evaluation-only build tag lets a scripted session count as a person's chat, with tests that no release contains it.

## 8. The roadmap

Five stages. Each ends at a gate, and the plan is revised at each gate against what was measured. Sizes are rough agent effort: S is up to two days, M about a week, L more. They are for ordering, not promises.

Package ids match the area designs in [working-papers/design/](working-papers/design/), so the detail for each is one search away. "(shrunk)" marks a package cut down from its design by the critique; the plan's wording governs.

### Stage 0: ground truth

No rebuild. The aim is to replace inference with measurement on the one real install and on real hosts, and then fix the cut line. It also lays the seams that let many packages touch the same files safely. It does not block Stage 1's instruments and specs; it decides the size and order of Stage 2.

| Id | Package | What it gives | Needs from you |
| --- | --- | --- | --- |
| SEAM-0 | Seams and guards | A command table with one file per command, a handler list for the hook, and registries for status rows, config keys and benchmark scenarios, so adding one is a new file and not an edit to a shared one. A hash test over the whole Codex plugin folder, an upgrade test from the last published release, and a gate that v1's own tests and benchmark are unchanged while every new flag is off | Nothing |
| EV-00 | Field report folded in | Each Stage 0 measurement has a value or a stated unknown | Run [the prompt](field-report-prompt.md) once |
| HF-00 | Host facts in one sitting per host, with recorded fixtures (LG-03, HO-01, ST-00, DL-01 merged) | Real hook payloads and transcripts from scripted sessions with made-up content, scrubbed for a public repository, for both hosts; every host fact the designs lean on marked confirmed or refuted, each with its fallback | A separate evaluation home (a made-up user and a scratch project) with both hosts signed in, and about one sitting per host. The real install is touched only by read-only reports and by one push test to a scratch repository on the git server, if you agree |
| LG-01, LG-02, DV-01, DL-02, HO-02 | One v1 point release with counters only, built as one package | How often v1 holds an event back; turn and tool coverage against the host's own record; what the checker and the triggers cost; how often a brief is stale; how health and approvals behaved. No plugin byte changes, so Codex's approval holds | One upgrade, a week of ordinary use, the report again (decision 17) |
| EV-16 | Latency that fails a build, and a macOS baseline | Hook time at realistic sizes and for a whole turn of 50 tool calls, measured on macOS before any hook changes; a build that slows a hook fails | Nothing |
| DV-02, EV-01 | Live gates on v1 | v1's update meets real models on both hosts, at the highest and at a low effort; the output is recorded as fixtures; the resume test is run with the update on | Leave to spend model quota: 21 calls per host and effort |
| DV-03 | Contract spike and the cost and fidelity frontier | The new contract's open bets settled by measurement: default model and effort, exact quotes against segment ids, whether a repair round pays | Quota, inside the same leave |
| EV-03 | Pilot (shrunk) | Ten tasks under memory off, v1 and an ideal record: variance, tokens and minutes per session, and whether the tasks can show a difference at all | Quota; sets the ceiling of decision 16 |
| EV-04 | The cut line fixed | This plan revised against the table below | Your agreement to the revised plan |

**The release train.** You cut every release. The first carries the counters and nothing else. No release that changes behaviour goes to the real install until the second report has been read, so the counted week measures v1 and not a half-changed v1.

What each result changes. The thresholds are written down now so the reading cannot be bent to fit the plan.

| Measurement | If | Then |
| --- | --- | --- |
| Share of v1 passes whose whole answer is refused | 30% or more | The new contract is first in Stage 2 at full size |
| | under 10%, with at least 0.9 of planted facts stored correctly on the new scorer | The contract shrinks to exact spans for items under your authority; recall work moves ahead of it |
| Rule and knowledge adds the checker refused | half or more | Word-overlap attribution is confirmed as the loss |
| | 15% or less | Spans are still built; the repair round is dropped |
| Chat starts whose state was stale or pending | 30% or more | The worker and the fast pass go before any recall work |
| | under 10% | The clock is argued on reliability alone |
| Seconds and tokens per pass at the highest effort | median over 120 s, or any timeout | The small-model default is no longer optional |
| Events v1 holds back; turn and tool coverage | under 0.5% held and coverage at least 99% | Recovery still moves out of the hook, for ids, order and missed decisions; no spool is built |
| | 2% or more held, or tool coverage under 95% | The held spool and the new layout move from section 12 into Stage 3 |
| One hook start on Codex, 95th percentile | over 100 ms | No hook on Codex's shell tool beyond what v1 declares |
| Knowledge pages in the real wiki | none or a handful | Recall has no real index to be judged on yet; the migration is small |
| | hundreds | The migration's property test is first in Stage 2 |
| Daily commit outcomes; signing prompts | prompts daily, or pushes failing | Decision 3 as recommended; if the server refuses an unknown committer, its fallback |
| Codex reviews needed after upgrades | any | The frozen hooks plugin is the first hosts package in Stage 2 |
| The MCP server and skills | not connected, or never used unprompted | Nothing is built that depends on the agent choosing to call a tool |
| The live resume test with the update on | the model ignores or misreads the brief | The typed brief moves first in Stage 2 |
| Pilot: an ideal record against memory off | under 30% fewer steps | The tasks are rebuilt before any result is trusted |

### Stage 1: instruments, and changes that ship on v1

Nothing here changes a user's data. Each change replaces what it improves, and each instrument gives v1 a row that Stage 2 must beat.

| Id | Package | Outcome and gate | Size | Depends on |
| --- | --- | --- | --- | --- |
| EV-10 | Specs and decision records (shrunk) | `docs/design.md` and `docs/verification.md` frozen and split into a spec per component of at most 24 KB and dated decision records; each spec lists the must-keep items it touches and the tests that hold them | M | decision 15 |
| EV-11 | Ownership and size checks (shrunk) | CI fails a change that touches paths outside its package's list, and fails when shipped Go outside tests exceeds v1's 54,807 lines | S | EV-10 |
| EV-12 | Results store and gates (shrunk) | Every result is a row with commit, host version and model; "better or worse than the accepted baseline" is one command | M | |
| EV-14 | Fidelity scorer, with v1's score (EV-13 shrunk into it) | A black-box, value-exact scorer with a precision side, on v1's 12 scripts and about six new families, half held out. Gate: it catches a wrong value, a dropped date and an invented item that v1's scorer passes | L | LG-10 |
| DL-04 | Labelled retrieval set and scorer | Precision, recall, false additions and silence, with near misses and a held-out half; v1's recall is the recorded baseline | M | |
| EV-17 | Continuation harness (shrunk) | 20 tasks, three conditions, hidden tests; v1's row recorded. Sessions run in Linux containers, so the evaluation build lifts the macOS-only gate first | L | EV-03, EV-12 |
| IF-01 | Interfaces, fakes and the contract matrix | Each track's interface as types and signatures, with a fake other tracks' tests can import: the reader over fixture events, the store's reference model, a fake model runner, a fake retrieval core. A matrix maps every need between tracks to the package that provides it, and a card whose need has no provider fails its check | M | SEAM-0 |
| LG-10 | Event model and the one reader | Typed events with stable ids over v1's files; a v1 inbox read through it yields the same text at the same reference | M | |
| LG-13 | Hook payloads through adapters; author by allowlist | Every fixture payload decodes to its labelled events; nothing is filed as typed without positive evidence | M | HF-00, LG-10 |
| LG-11 | Redaction driver with spans, same behaviour | `Apply(Scan(x))` equals v1's output on every test case and 150,000 generated inputs; time at 16 KiB at least halves; a cancelled scan returns a redacted prefix | M | |
| LG-12 | Redaction corpus, rates and the oracle | CI prints recall by class, the rate of facts lost and of hex keys kept; oracle violations are 0 | M | LG-11 |
| DV-12 | One worker, no lost wake | v1's jobs run under one worker with a journal; a wake that finds it busy loses nothing (hard gate) | M | |
| DV-11 | Runner with a probe and typed outcomes | Host failures classified from structured fields; a run cannot pass with a tool call or another model unseen; the host's version is recorded | M | DV-02 |
| DV-13 | Cost ledger, token budget, small default | What organizing spends is one number and a budget the worker obeys; the default model and effort follow DV-03 | S | DV-12, decision 5 |
| HO-10 | Host adapter and capability table | One place knows a host; host names leave the rest of the code as packages are touched | M | |
| HO-11 | Install manifest | Any release can replace or remove another release's rendered files, proven by an upgrade test from each earlier layout | M | HO-10 |
| HO-12 | Health that sees missing work (shrunk) | Lag against a budget per stage; the most severe relevant condition, one line, prepared outside the hook; no notice in a replayed healthy month | M | HO-10 |
| HO-13 | Signed release manifest, daily check, an upgrade that asks nothing (HO-25 shrunk into it) | A binary and the install script verify a release offline; `eva-md upgrade` runs with no question; the previous binary is kept | M | decision 9 |
| HO-14 | Fake hosts that fail the way real ones do | Tests can exercise hash-bound trust, host versions and policy blocks offline | S | |
| ST-01 | Store format specification and the OKF profile | Every file format, id rule and protection rule written down, with the kind, tag and state tables as ruled in section 6.3; a hand-made fixture store passes the profile checker | M | decisions 1, 2, 3, 22 |
| ST-03 | One line-preserving, fail-closed codec | `Write(Read(b)) == b` over a corpus of hand edits; every unreadable protection value protects; parse equality with okf on v1 pages | M | ST-01 |
| ST-04 | Migrator and minimum reader version (shrunk) | One idempotent, resumable step with a dry run; a crash at any point reruns to the same tree; an older binary reads nothing and says so once | M | ST-01 |
| ST-02 | Storage properties as tests (shrunk) | Crash safety, exactly once, protection, restore and replay stated as properties and run against a reference model, each killed by a broken variant | M | ST-01 |

### Stage 2: one core rebuild and one cut-over

The new store, the new contract and the new brief are built in five tracks against the interfaces and fakes of Stage 1, behind one mode key with three states: `v1`, `shadow` and `v2`. Everything is additive. No package deletes or changes a live v1 path until the way back has closed. The cut-over is its own set of four packages with one owner, at the end of this stage.

| Id | Package | Outcome and gate | Size | Depends on |
| --- | --- | --- | --- | --- |
| LG-23 | Decisions and delegated work in the record (shrunk) | Dialog answers, plan decisions, subagent tasks and reports, failure digests and delivery records are typed events, all through hooks. Gate: every fixture decision captured with the right author; a successful command stores no output | M | LG-13, decision 6 |
| LG-21 | Recovery out of the hook (shrunk) | The transcript is read by the worker; the reader exposes a session only up to the checked point. Gate: no leak after a pause under generated patterns of dropped and delayed hooks; no duplicates | L | LG-10, DV-12 |
| LG-22 | Project identity and aliases | One id through a scripted first push, rename, move and worktree; a fork under a new remote is never joined automatically | M | LG-10, decision 20 |
| LG-24 | A pause switch that can be seen | Strict phrases, an explicit command, confirmation on both hosts, inheritance on fork | M | LG-21, decision 19 |
| LG-26 | Redaction that keeps facts and finds more secrets (shrunk to two policies) | Under the corpus: at most a fifth of v1's lost facts, under 1% of hex keys kept, the gate before a push flags everything capture replaces | M | LG-12, decision 7 |
| ST-05 | Store core | An owned repository, `Apply`, one commit per pass, coverage in the tree, protection in the store. Gate: the single-machine properties pass on the real store; a locked item is byte-identical after 1,000 passes aimed at it; a pass is durable in under 2 s | L | ST-03, ST-04 |
| ST-06 | Attach, change and remove a remote; push each pass | A local store can be backed up and re-pointed with one command; the age of the oldest change not yet off the machine is reported | S | ST-05 |
| ST-07 | Migrate a v1 wiki | Every unit of v1 text accounted for exactly once on generated wikis and on a dry run over a copy of the real one; the old clone byte-identical afterwards | L | ST-05, LG-22 |
| ST-08 | Audit, undo, restore and replay | `log`, `why` and `undo` per pass; a deleted store restores from its remote and queues exactly the uncovered ranges | M | ST-05 |
| DV-20 | Episodes and a cursor | The model's input is whole turns with stated context, read through the one reader; no file is rescanned | L | LG-10 |
| DV-21 | Extraction contract and its checker | Delta operations, exact quotes, authority from the record, acceptance per operation, one repair round. Gate: nothing misattributed; fidelity above v1's row; at most 5% of faithful statements refused | L | DV-03, DV-20, LG-13 |
| DV-22 | Failure semantics (shrunk) | Two priorities, one daily cap, one hold with backoff, a pause after three host-shaped failures; nothing skipped without a record | M | DV-11, DV-12 |
| DV-23 | Items and workstreams (shrunk) | v1's four kinds; authority rules; branch binding and folding on merge; an observation never replaces what the user stated | M | ST-05, DV-21 |
| DV-24 | The fast pass, end to end | A chat's memory on file within minutes of its end, at a fraction of v1's cost, measured live | M | DV-20 to DV-23 |
| DV-25 | Migration of v1 progress and meaning | Nothing organized twice and no statement lost; v1's observed items that rested on the assistant's report are relabelled, not removed | M | ST-07 |
| DL-05 | One tokenizer and one strict reader | Every match in the product uses the same tokens; every injected file is opened by the same checked reader | S | DL-04 |
| DL-07 | One access layer | Hooks, command line and MCP return the same item set for the same scope, by test | M | LG-22, ST-05, decision 13 |
| DL-08 | The pack | Hooks read a small compiled file set; chat-start cost no longer grows with the inbox | M | ST-05 |
| DL-09 | What a chat holds (shrunk) | A set of ids per context window, reset on compaction, and an owed-brief flag | S | LG-23 |
| DL-10 | The typed brief | New, cleared, compacted, resumed and forked chats each get the brief defined for them. Gate: the handoff suite not worse than v1's row; under 150 ms | L | DL-08, DL-09 |
| DL-11 | Delivery wired into the hook | Start, first prompt and exact keys, beside capture with its own budget and failure count; a failed delivery is owed and retried | M | DL-10, LG-13 |
| DL-12 | The two read tools | An agent reaches a memory in one or two calls, inside its scope, with paging | M | DL-07, DL-08 |
| DL-13 | Chat search on the same tokenizer (shrunk) | Matches across a turn, needs no 90-day window; the search index's own scorer is kept | M | DL-05, LG-10 |
| HO-20 | Codex hooks in a frozen plugin (shrunk) | Frozen once, after HF-00's measurements, with a test on the bundle's hash; skills and the MCP entry in a second plugin | M | HF-00, HO-11, LG-23 |
| HO-21 | The first run | Install asks nothing, says once what it does; a project can be excluded or memory stopped with one command | M | HO-11, ST-05 |
| HO-22 | The clock | The worker is woken on a schedule; hook wakes are the fallback | M | DV-12, decision 4 |
| HO-23 | Health on v2's evidence (shrunk) | Each silent stop in the fault table is noticed by a part that still runs; three channels: a chat-start line, one line in a read tool's result, `status` | M | HO-12, HO-22 |
| HO-24, HO-26 | The cost line and the command surface | `status` is a verdict of at most six lines with a cost line; about a dozen verbs | S | DV-13 |
| HO-40 | Linux release assets (shrunk) | eva-md installs and runs on Linux | S | HO-22, decision 23 |
| EV-20 | Gates as areas land | Each rebuild judged against v1's row on the same instrument | M | the packages above |
| EV-21 | Migration rehearsal | The move from v1 proven on generated installs before any user runs it | M | ST-07, DV-25 |

**The cut-over.** Four packages, one owner (the lead), in order:

| Id | Package | Outcome and gate |
| --- | --- | --- |
| CUT-1 | The mode key | One key with a state table for every area. In `v1` the product behaves exactly as today, by test |
| CUT-2 | The shadow release and a week of real use | In `shadow`, v2 computes and logs counts beside v1 on the real install. Shadow organizing runs under its own small budget, with your leave, and writes to a scratch store that is discarded afterwards. The readout is the gate: coverage, zero leaks after a pause, zero misattribution, the refusal rate, and the precision of what the new brief would have shown |
| CUT-3 | The cut-over release (HO-27, ST-07, DV-25) | One upgrade you run: the migration, the new plugins and their one Codex review, mode `v2`. v1's paths stay in the binary and the record is the same files, so a test abandons after simulated v2 use and finds every chat readable by v1. Gates into it: the rehearsal green, your dry run reporting nothing unaccounted, fidelity above v1's row, handoff not worse, latency inside its ceilings |
| CUT-4 | The deletions (ST-13, DV-26, and the record's, recall's and hosts' own) | One release after the 14-day way back has closed: v1's update, page packages, okf, triggers, in-hook recovery and guided setup are removed. Gates: shipped lines at or under v1's, and every gate still green |

### Stage 3: small fixes that are owed, and whatever a number asks for

Owed, because each closes a confirmed finding at small cost:

| Id | Package | Outcome | Size |
| --- | --- | --- | --- |
| DL-16 | A compacted chat keeps its footing | The constraints and a complement of the host's summary are delivered after compaction; a lost one is retried | S |
| DL-15 | Subagents and program-run chats read the constraints | Delegated work starts with the standing constraints and nothing else | S |
| DL-18 | Stale claims held back or labelled | Drift of an item's code anchors decides whether and how it is delivered | M |
| DL-20 | Receipts | A person can see what a chat was given and what was held back | S |
| LG-30 | Forget one line (shrunk) | Forget by session, turn or event; items that rest only on it are retracted | M |
| DV-30 | Housekeeping (shrunk) | Exact duplicates merged; a merged branch's decisions folded into the project | S |
| DV-32 | Corpus counters | Numbers that say whether a richer lifecycle or a reviewing pass is needed | S |
| DV-35 | System pages frozen | They cost nothing unasked and can be measured | S |
| EV-23 | One content-free report | After v2 the field report is one command's output | S |

Asked for by a number: each item in section 12 whose trigger has fired by then.

### Stage 4: proof and release

| Id | Package | Outcome |
| --- | --- | --- |
| LG-50, DV-50, HO-51 | A week on the real install | The starred bars of section 5.4 are met, or the release notes say which were not and why |
| EV-51 | Releases read the gates | A release cannot be cut over a red or stale gate |

### What runs in parallel

Six tracks own disjoint paths, so their packages can be built at once. The lead owns the files every track touches: `go.mod`, `AGENTS.md`, the hook declarations in `internal/install/templates`, the command dispatch in `cmd/eva-md/main.go`, and the docs index.

| Track | Owns | v1 code it freezes, then deletes |
| --- | --- | --- |
| Record | `internal/ledger` (new), the capture body of `internal/hook`, `internal/redact` | `internal/catchup`'s hook slicing, `internal/inbox`'s readers |
| Store | `internal/store` (new), `internal/gitrun` (new) | `internal/okfadapter`, `internal/wikigit`'s commit path, `internal/wikiinit` |
| Organizing | `internal/work` (new), `internal/derive` (new) | `internal/update`, `internal/hook/trigger.go`, `internal/lint` |
| Recall | `internal/pack`, `internal/brief`, `internal/retrieve` (new), `internal/mcp` | `internal/resume`, `internal/hook/pointers.go` and `recall.go`, `internal/knowledgepage`'s recall and scope |
| Hosts | `internal/hostadapter` (new), `internal/install`, `internal/health`, the setup files of `cmd/eva-md` | the guided setup, `config.env`, the doctor's rows |
| Proof | `cmd/eva-eval`, `internal/eval` (new; never in a release) | `cmd/live-accuracy`, `internal/update/accuracy` |

The critical path is short: the one reader (LG-10), then episodes and the contract (DV-20, DV-21) on one side and the codec and store core (ST-03, ST-05) on the other, meeting at the fast pass (DV-24), the migration (ST-07) and the brief (DL-10).

**The calendar is set by waits on real use, not by agent effort.** v1's 120,000 lines were written in 11 days, so building is not the limit. Four waits are fixed and come in sequence: the counted week, the shadow week, the 14 days in which the cut-over can be abandoned, and the week of proof. That is about five weeks that no number of agents shortens. Work in progress is capped by review and integration, at about six packages building at once.

## 9. How the build is run

The build is done by a team of coding agents working in parallel, with one lead that orchestrates. You decide, approve each stage gate, and run whatever needs your accounts or your machine.

**Roles.**

| Role | Does |
| --- | --- |
| Owner | Answers the decisions of section 7. Approves each stage gate. Runs the field report, the host-facts sitting and upgrades on the real install. Gives leave for live runs and sets their ceiling |
| Lead | Writes each package's card. Reviews specs, contracts between tracks, decision records and any change to a shared file. Integrates. Runs the live evidence. Revises this plan at each gate and reports what was measured |
| Builder | One agent per package, in its own worktree and branch. Writes the spec where the package defines a contract, then code and tests. Never spends model quota |
| Reviewer | A fresh agent given only the card, the spec and the diff, told to break it. Privacy-bearing packages (the record, redaction, the pause switch, store protection, anything that puts text in a chat) get two |

**Documents a team can work from.** `docs/design.md` and `docs/verification.md` are frozen and split. Each component gets a spec of at most 24 KB beside its package: its interface, its invariants, the must-keep items it touches and the tests that hold them. Each decision that changes a rule gets a dated record in `docs/decisions/`. A builder loads one spec, not 390 KB. This plan stays the map; the specs become the truth as each is written.

**A package's life.**

1. *Card.* The lead writes it from this plan: the finding ids it closes, the paths it owns and may touch, what it depends on, the exact commands that accept it, and the v1 files it deletes.
2. *Spec and review*, only for a package that defines a contract another track builds on. A second agent checks it against the invariants, the project rules and the smallest design that meets the bar.
3. *Build* on a branch from `main`, with characterization tests around any v1 behaviour being replaced.
4. *Checks.* The project's own checks (`scripts/linux-check.sh`; the macOS job is the record), then the instruments its acceptance names.
5. *Adversarial review.* Every finding fixed, or recorded and accepted by the lead.
6. *Integrate.* The lead merges in dependency order. A package whose diff stays inside its paths, whose gates are green and whose review is closed needs no further reading.
7. *Prove*, where the package needs a live measurement: batched by the lead inside the ceiling of decision 16. Until it passes, the feature stays in shadow.

**How a package is accepted.** One of three ways, so nobody invents a metric where none belongs. *Moves*: a named measure beats its baseline. *Holds*: every gate stays green while something is replaced or deleted. *Guards*: a new gate exists and fails on a fault seeded to trip it. Every card splits its acceptance in two. The *merge gate* is deterministic and offline, something a reviewer can run with the project's checks. The *prove step* is anything live or timed, run by the lead in a batch. Wall-clock ceilings are judged on macOS by comparing the old and the new binary in alternating runs, never inside `go test`.

**Rules that keep parallel work safe.**

- v2 is built on `main`, in new packages beside the old, behind one mode key that defaults to v1's behaviour. `main` stays releasable, and the real install can run shadow modes from ordinary releases.
- Before the way back has closed, no package deletes or changes a live v1 path. With the key at `v1`, v1's own tests and benchmark are unchanged on every merge.
- A diff stays inside the paths on its card. CI checks it.
- v1 packages scheduled for deletion are frozen. Nobody improves code that is going away.
- Shared files have one writer, the lead. The seams of SEAM-0 keep that list short: a new command, hook handler, status row, config key or benchmark scenario is a new file.
- Each track's interface lands in Stage 1 as types and signatures. After that a contract changes only through a decision record.
- The hook declaration changes once, in HO-20, and is installed at the cut-over. A package whose acceptance needs an event Codex does not yet declare is proven in the evaluation home, and its card says so.
- Every flag has an owner and the package that removes it.
- Every rule in `AGENTS.md` holds unless a decision in section 7 changed it. In particular: `internal/redact/baseline` is never edited, tests never touch the real machine or depend on its speed, hooks never call a model or the network, and only eva's name is shown.
- The shipped code is no larger than v1 at the end of a stage.

**Gates on a release.** Every merge: both CI jobs and every hard gate. A release that changes the contract or the model policy: fidelity on held-out scenarios on both hosts. A release that changes the brief: the continuation tasks. The cut-over: the migration rehearsal green and your dry run reporting nothing unaccounted. A hard gate is never waived; another gate only by you, in a dated decision record.

## 10. Migration and the way back

Nothing a user has is lost, and at every step there is a way back.

1. **Stage 1 changes no data.** Its releases run on v1's inbox and wiki. They change no plugin byte, so Codex asks for nothing.
2. **Shadow before cut-over.** From an ordinary release, the new contract and brief run against a scratch store and log counts only. A week of shadow on the real install comes before any switch, and its readout is the gate. The scratch store is then discarded: the real store starts from the v1 wiki.
3. **The cut-over is one upgrade you run.** In order: a dry run of the migration prints counts and stops if any v1 text is unaccounted for; the store is created in eva-md's data folder as a clone of the v1 wiki, with its whole history, and the migration is one commit on top; v1's progress markers become coverage, so nothing is organized twice; the pack is compiled; the new plugins are installed, which asks for one Codex hook review, the last.
4. **What stays untouched.** The old wiki clone, byte for byte. The inbox, which the new reader reads in place; every reference already written still resolves. Pages a person locked or verified become protected files. Pages the migrator cannot read are carried under `legacy/` as they are.
5. **The way back.** For 14 days `eva-md migrate --abandon` points eva-md back at the old clone. v1's paths are still in the binary, and the record is the same files, so no chat captured since the cut-over is lost. Only after those 14 days does a later release delete v1's code. The old clone is still there afterwards, and nothing ever deletes it.
6. **A remote.** If the v1 wiki had one, it becomes the store's remote, and the new commits are pushed as a fast-forward of the old history. Nothing is forced.

## 11. Risks

| Risk | What limits it |
| --- | --- |
| The hosts move weekly: hook payloads, transcript formats, trust rules, and how headless runs are billed | One adapter per host with recorded fixtures per version. The brief needs no model, so memory degrades to "less organized", never to "nothing". Organizing pauses itself on an unrecognized host and resumes on a version change |
| A cut-over of the store, the contract and the brief at once | Shadow on a scratch store first, a rehearsal on generated installs, a dry run on a copy of the real wiki, the old clone untouched, 14 days to abandon |
| The plan grows back into a second system | The cut line, the size budget checked in CI, a trigger written down for every deferred item |
| A small model alters quotes, so little is stored | Measured in Stage 0 before anything is built on it; a fallback to segment ids; one step up to a larger model |
| A faithful quote sits under an unfaithful statement | Value-exact scoring in the fidelity instrument; items under your authority are shown with the quote they rest on when asked |
| Privacy regresses in the rebuild (the pause switch, redaction) | Hard gates with seeded leaks; a differential test against v1's redaction on 150,000 inputs; the baseline as an oracle |
| Evaluation costs more than it returns, or variance hides the effect | Four instruments only; a pilot prices a session before any ceiling is set; gates are on steps, tokens and corrections, which can be measured, not on small solve-rate differences |
| One real install, so the migration cannot be rehearsed widely | Property tests over generated wikis; the first real run is watched; the old clone is the way back |
| Stage 0 asks a lot of you: a report, an hour of host facts, a point release and a week, leave for live runs, 26 decisions | Stated here so it can be planned; nothing in Stage 1 waits on it |
| Memory is an injection path | Items carry whose words they are and what the turn was exposed to; nothing exposed to outside text is promoted without your word; held, disputed and unverified items are never pushed |
| The hosts' own memories improve | Tested against in every experiment. What stays eva.md's: one memory across both agents, work state per workstream, checks against the code, receipts, and files you own |
| Agents building in parallel collide or drift from the design | Paths per package checked in CI, contracts first, v1 frozen, one writer for shared files |

## 12. Later, each with its trigger

None of these is in v2.0. Each has a design in the working papers and opens when its trigger fires.

| Item | Opens when | Rough cost | Needs from you |
| --- | --- | --- | --- |
| Sync between your machines (fetch, merge by field, enrolled writers) | You have a second machine, and single-machine v2.0 has run for a month | L, plus a multi-machine test suite | Decision 21. Signing then becomes a security boundary: commits from a machine not enrolled are merged but held from delivery |
| A held spool and a session-keyed layout for the record | The counted week shows 2% or more of events held back, or under 95% of tool calls captured | L | |
| Text between tool calls kept in the record | An evaluation arm shows organizing does better with it | S, plus an expiry rule | A change to "kept with no time limit" for that text |
| A richer lifecycle: candidates that must recur, expiry, archive by disuse | The corpus counters show noise, duplicates or stale items above their thresholds | M to L | |
| Lessons from failures, and preferences, as kinds | The hidden-decision tasks show a gain once failure digests exist | M | |
| A reviewing model pass over the store | Near-duplicates and disputes exist that code cannot settle | L | A share of the daily budget |
| Recall when the agent edits a path or runs a command | The continuation tasks show a gap the brief does not close, and one hook start is cheap enough on that host | M | One more Codex hook review |
| Lexical matching on prompts | The held-out retrieval set shows precision of 0.95 with at most 1% false additions | M | |
| Embeddings | Recall on paraphrases stays under 0.7 after the tokenizer, cues and keys, and a static model run offline lifts it 10 points without hurting exact matches | M | "No local models"; a model file to ship; never in a hook |
| Installing releases in the background, with probation and rollback | There are users whose fixes are waiting for someone to run a command | L | "No auto-update"; where the signing key lives |
| Tools that let an agent write to memory | Real use shows people asking the agent to remember or forget mid-chat and the typed path missing it | M | "The MCP server only reads" |
| Asking the host to confirm a tool call that breaks a rule you armed | A rule-compliance measurement shows rules are seen and not obeyed | M | "A hook never returns a decision" |
| Generated rules files or reviewed skills in the host's own folders | An experiment shows they beat delivery through hooks | S to M | "Never write host settings" |
| A floor for hosts with MCP and no hooks, which reaches the Claude desktop app's Chat tab | You want memory there | S to M | Decision 3 of the v1 decisions (hosts in scope) |
| Cloud sessions | Sync exists, and six host facts hold | M | |
| A kit for managed machines | The field report shows managed settings restricting hooks or plugins | S | |
| Replay of old chats under the new contract; reading chats from before the install | You ask, after seeing a token estimate | M | "No history backfill" |
| Short excerpts committed beside items | Sync, since evidence must then travel | S | "No captured text is ever committed" |
| Removing something from pushed history | A secret is found in pushed memory | M | The one exception to "never force" |
| An API key as a second way to run the host's CLI | A host stops allowing headless runs on a plan login | S | |
| Claude Code mods | The host declares the API stable | | |
| Team memory | Not designed. Nothing here is built for it | | |
| The claim run, and the word "best" | Every v2.0 gate is green on both hosts | M, plus quota | What it must beat (the recommendation: memory off and the host's own memory) |

## 13. What the field report is for

The [field report prompt](field-report-prompt.md) asks the agent on the real install for counts, durations, statuses and general judgements, and nothing private. Its answers fill the Stage 0 table in section 8: how often a pass is refused, how stale a brief is at a chat start, how often capture is held back, what a pass costs, whether daily commits and pushes succeed, whether Codex has asked for re-approval, whether the read tools are used at all, and what you have had to do by hand. When it comes back, the lead folds it in (EV-00) and revises this plan before anything in Stage 2 is sized.
