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

<!-- PART-5-BUDGET -->

<!-- PART-6-ON -->
