# eva.md v2: the plan

**Status:** the final plan, 2026-11-10. It rests on an audit, outside research, six area designs, three critiques, a field report from the one real install (section 13) and the owner's own note about that install, checked point by point against the code (section 14). From here the plan changes only at a stage gate, and the first is the Stage 0 readout. Section 15 is where the team starts.

**The go was given on 2026-11-10.** The owner gave standing leave: the build works on `main` directly, with no pull request and no further approval, and every recommendation in section 7 is taken as agreed until the owner says otherwise. Cutting a release stays the owner's to ask for, and no push to `main` cuts one. Building started that day; the cards in [cards/](cards/README.md) show what is in hand.

**What this is:** an evaluation of eva-md as it stands, and the design and order of work to make it the best memory there is for AI coding agents: one that feels built into the host, where the only thing a person notices is that the agent remembers.

**Companion files:** [findings.md](findings.md) lists every audit finding by id. [working-papers/](working-papers/) holds the audits, research notes, area designs and critiques this plan was distilled from. Where they differ from this file, this file governs.

## 1. The short version

**Where v1 stands.** eva-md is careful work, and much of it should be kept as it is: hooks that cannot hurt the host, redaction before anything reaches disk, a resume brief built by code, a sealed model run, a crash-safe page store, a transactional install. But the half that makes it a memory (summarizing chats, promoting lasting facts, recalling them, committing them) is tested only against canned model output, and nothing measures whether memory helps a chat. On the one real install it has now run for about six days, and the first field report (section 13) shows it working, slowly and at a high cost, with the numbers that would say how well still missing. The audit kept 252 findings. They trace to ten structural choices, set out in section 3.2, which patching would not fix.

**How it is really used.** The one real install is used almost wholly in Codex, in its desktop app and partly by voice, with most sessions in folders outside git, on investigations that cross repositories. The first design assumed typed chats in one repository on a branch. Section 14 checks the plan against the real use, and changes it where it would have fallen short: speech becomes its own kind of author, a project gets an overview that code keeps current, and the instruments gain voice, work outside git and work across repositories.

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

1. Answer the eleven questions of section 13.6, a line each. The field report and your earlier note are both folded in.
2. Answer the 33 decisions in section 7. Each has a recommendation and a fallback, so "agree, except these" is enough. Fourteen are needed before any code; the rest at the end of Stage 0.
3. For Stage 0: Part 1 of the host sittings' run-book, three short sittings and a hand-over (about 180 minutes of steps), in an evaluation home (a separate user account on your Mac, with both hosts signed in) to record how the hosts really behave, with one voice call in Codex's desktop app, and its Part 2 later, group by group, when a package needs it; approval of a scratch plugin's hooks in Codex there, and one scratch repository on your git server with two pushes to it, deleted right after; the merges decision 27 keeps for you (`AGENTS.md`, the plugin's hash test, the latency gate and its baseline); cutting the counters release, then one small v1 upgrade, with one Codex hook review because the install is two releases behind, a week of ordinary use and a second report; the live gates on v1, which you run yourself from a run-book (84 calls, two `doctor --live` calls and a few chats by hand); the two API keys of decision 28, if you take its recommendation; and leave, with a ceiling you name, for the model comparison (about 25 million tokens by the design's estimate) and the pilot (about 60 million).
4. After Stage 0: cut every later release (at least five more: Stage 1's, the shadow release, the cut-over, the deletions and v2.0); make the key that signs release manifests; go on merging what decision 27 keeps for you; use the shadow release for a week and bring back its counts; run the migration's dry run on a copy of your wiki, watch the first real migration, give Codex its one hook review and review once which projects share memory (decision 13); approve each stage gate; and give v2 a last week of ordinary use before the release.

**What happens next.** Section 15 gives the order, day by day. In short: Stage 0 measures. Stage 1 builds the instruments and ships improvements on v1 that change no data. Stage 2 is one rebuild and one cut-over, with the old wiki left untouched as the way back. Stage 3 closes small owed fixes and opens whatever a number asks for. Stage 4 is a week of proof on the real install and the release. The plan is revised at every gate.

## 2. How this plan was made

The plan rests on four passes, each checked by a second reader who was told to try to prove the first one wrong.

| Pass | What was done | Result |
| --- | --- | --- |
| Audit | 17 parts of eva-md read in full, code first and documents second, each against seven questions: memory quality, recall quality, invisibility, reliability and safety, reach, architecture, evaluation | 252 findings kept: 7 critical, 85 high, 126 medium, 34 low. Of 204 first-pass findings the second reader confirmed 131 and corrected the scope or severity of 73; it refuted none outright and added 48 of its own |
| Research | Six topics from primary sources as they stood on 2026-11-10: Claude Code and the Claude apps as a platform; Codex as a platform; retrieval that fits one static binary; memory tools for coding agents; general agent-memory systems; how memory is evaluated | 153 load-bearing facts re-checked against their sources: 121 confirmed, 31 corrected, 1 refuted |
| Design | Six area designs built on the audit and the research: the record, the wiki, organizing, recall, hosts and operations, proof | Six designs, each with its bars, contracts, migration and work packages. Taken whole: 128 work packages and 51 decisions for the owner |
| Critique | Three critics over all six designs: will it work, is it the smallest design that meets the bar, can a team of agents build it in this order | The simplicity critique found the six designs would build a second system beside the first (128 packages, 51 decisions, 43 commands) and set the cut line of section 6. The delivery critique found the order would not hold (work that was serial, v1 code deleted before the way back closed, a cut-over with no owner) and gave the stages of section 8. The feasibility critique found where the designs would break a guarantee (an agent able to borrow the user's authority, a quote that proves words exist but not that they support the statement, a migration that could push what v1's secret gate held back) and those rulings are in sections 6 and 12 |

Two inputs came from the real install after those four passes.

| Input | What was done | Result |
| --- | --- | --- |
| Field report | A prompt run once on the real install returned counts, durations and statuses for about six days of use, with nothing private | Section 13: a reading against each threshold written beforehand, twelve changes to the plan, and two places where the audit's inference did not hold |
| The owner's note | A note an agent on that install wrote four days earlier, without the source: 44 points. Five readers checked each against the code and this plan, a second reader tried to prove each wrong, and a researcher read Codex's source for how its voice mode works | Section 14: 10 defects in v1, 8 things v1 has no form of, 8 that already work, 5 designed delays, 11 gaps in tests or documents, and 2 the code cannot settle. Four new decisions, one new package and 33 amended gates |

Limits of this evidence, stated once so nobody leans on it too hard:

- **No host was run.** The work was done in a Linux cloud session with no Claude Code or Codex signed in. Every claim about eva-md's code was read from the code. Every claim about a host was read from its documentation or source on 2026-11-10. Both hosts ship several releases a month, so each work package that leans on a host fact starts by confirming it on a real host.
- **The audit was written before the memory half of eva-md had been seen with a real model.** Its estimates of how often the checker would refuse a real answer, how long an update takes or how well recall fires are inferences from the code. The first field report has since replaced some of them with measurements from one install, one host and about six days, and where it disagrees with the audit, section 13.4 says so. The rest of Stage 0 replaces the others.
- **Much outside evidence is young.** Several 2026 studies are preprints with small samples, and vendor numbers are self-reported. Where studies disagree, this plan says so and takes the direction they share, not the figures.

## 3. Where v1 stands

eva-md is about 55,000 lines of Go with 65,000 more in tests, written in nine days. It is careful work. The audit found no pile of bugs. It found a small number of structural choices that each rule out something the goal needs, and one large absence: nothing measures whether the memory helps.

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
- **The trust posture.** No telemetry, no account, no resident process, two dependencies, everything inspectable as Markdown and git history. The most-starred third-party memory tool has an open, unanswered report that it uploaded 30 days of prompts without consent, and its most-discussed issues are failures of its worker, port and installer. Stars are weak evidence of use, but those are the points this posture avoids. It is an advantage to keep while other constraints are reopened.

### 3.2 The ten structural problems

Finding ids such as `CAP-01` refer to [findings.md](findings.md).

**1. Nothing measures whether memory helps, and the memory half has never run for real.**
In the repository's own tests the update, promotion to knowledge, recall, the daily commit and the MCP server run only against canned model output; on the real install they have run for about six days with no score (section 13). The accuracy suite scores by codename, so a wrong value or an invented item scores as perfect. Recall was tuned on the fixture that judges it. No test asks whether a resumed chat continues correctly. Without this, no redesign can be shown to be better than what it replaces. *This is why Stage 0 and Stage 1 exist.* Findings: PRD-01, PRD-05, EVL-01 to EVL-07, UPM-01, RCL-05, BRF-03, SRC-04, CAP-04, RED-05, SYS-02, MCP-05, OPS-06.

**2. Capture keeps too little, through two paths that fight inside the hooks.**
Hooks keep the typed prompt, the final reply and one line per tool call. Mid-turn reasoning, error text, what a subagent found, and answers the user gives in the host's own question and plan dialogs never reach memory, so the knowledge a coding agent most needs (what failed and what fixed it) is lost at the source. A second path reads the host's transcript to recover what hooks missed, and it runs inside every hook in 100 ms slices: while it is behind, the hook discards its own capture and delivers no brief and no recall. About 1,200 lines exist only to make that slicing work. Entries have no stable id, so provenance rests on line numbers in one local file. A project is found by a key that changes when a repository gains a remote, is renamed or is moved, and its memory then silently leaves scope. Findings: CAP-01 to CAP-05, CUP-01, CUP-02, PRD-11, UPM-08, MCP-07, RCL-07, PGS-08, SET-02.

**3. Background work has no scheduler and no queue.**
Five kinds of hook event and a daily sweep each start a detached job and forget it. One lock means a job that finds another running simply exits, so with several chats open a chat's last trigger is often dropped. Maintenance, the commit and health checks run only when some hook happens to fire. When hooks stop, everything that would notice stops with them. Findings: UPW-04, OPS-01.

**4. The model's instructions and the checker are two different contracts.**
The checker decides who said what by word overlap with English word lists, in three files of about 1,860 lines that also check dates, duplicates and ids. A model that tidies a user's instruction or converts a unit is refused. A decision the user made by accepting a proposal ("yes, do that") is kept on the thread page but refused as lasting knowledge. Refusals are counted in the run log and shown to nobody, and there is no second chance. One verdict covers the thread page, the rules and the knowledge together, and a chunk whose answer fails three times in a row, and once more the next day, is skipped for good. The run uses the chat's own model at the highest effort for every kind of memory, a timeout is retried unchanged, and the next chat usually starts before the last one is remembered. Findings: UPM-02 to UPM-05, UPM-07, UPW-01, UPW-03, UPW-05, UPW-06, EVL-07. On the real install none of 61 answers was refused whole, and about one proposed rule or knowledge statement in six was refused (section 13.4). How much is lost in use is still to be measured.

**5. The memory model has one unit of working memory, the git branch, and facts with no lifecycle.**
Trunk-based work, and every chat outside a repository, share one page. A decision made on one branch is invisible on every other and after the merge. Promotion to lasting knowledge happens once, from one slice of one chat, capped at five. A fact has no validity: nothing checks whether the code it cites has moved, and an assistant's observation can replace what the user stated. There is no place for "we tried this and it failed because". System pages exist only when a person asks an agent to write one, and then only report their own decay. Findings: PGS-01, PGS-02, PGS-03, PGS-07, PRD-03, PRD-06, UPM-06, SYS-01.

**6. Recall looks only at the words of the typed prompt.**
It matches exact tokens against knowledge pages. A paraphrase finds nothing. Nothing is recalled when the agent opens or edits a file that a stored constraint covers, nothing reaches a subagent, and a constraint sent once is never sent again in that chat, even after a compaction has removed it from the context. Thread decisions and Timelines never reach a prompt. The tools an agent can call are the command line re-exposed: several guessed searches and a call per line to recover one memory. Search crosses project boundaries that automatic recall is careful to keep. Nothing records what was delivered into a chat, so restated memory can be saved again as new evidence. Findings: RCL-01 to RCL-04, RCL-06, RCL-08, PGS-04, PRD-04, SRC-01 to SRC-03, SRC-05, MCP-01 to MCP-03, BRF-01, BRF-02.

**7. The wiki assumes one machine.**
Nothing ever fetches or merges. Ids are counters, so two machines would give one id to different facts. Pages are rewritten whole and derived indexes are committed, so a merge would conflict on nearly every file. The wiki is also the user's own working clone, so eleven unusual git states stop backups until a person fixes them. Memory leaves the machine at most once a day, with the user's own signing prompt. No path exists to change the layout: a schema mismatch only refuses. The page library eva-md was built on supplies a small codec and decides what runs inside every save; the case against it is control, not line count. Findings: STO-01 to STO-04, SET-03, PRD-08, PRD-10, PGS-05.

**8. Starting, upgrading and staying healthy all need a person.**
Memory cannot start until a repository and a folder are chosen and several questions answered. Upgrades arrive only when a person runs a command, and since Codex 0.164.0 any change to a plugin's files switches its hooks off until they are approved again, so each release that changes a plugin file stops Codex capture until the hooks are reviewed again. Today that follows a person's own upgrade, which prints the step; with upgrades that install themselves it would be silent. Health reports recorded failures but cannot see missing work: capture or updates can be off for days with every chat looking normal. Most remedies are a terminal command to type. It runs on macOS only, for two hosts. Findings: SET-01, PRD-07, PRD-02, OPS-01 to OPS-07, MCP-04, MCP-06, BRF-04.

**9. Redaction destroys ordinary facts and cannot repair its own misses.**
Both rule sets erase non-secret facts about authentication when they are written as `key: value` lines or as identifiers near a trigger word (endpoints, header names, key paths); the same facts in prose survive. Because the frozen baseline is unioned in at run time, changing the current rules alone cannot bring them back. Some ordinary secret shapes are missed. A miss is permanent: improved rules never reach stored text, and all three gates share one detector. The costliest code in the system runs on the hook's hot path. Findings: RED-01 to RED-04. The real install shows six redaction markers in six days and none in the wiki, so the loss of facts is not yet seen in use (section 13.4).

**10. The documents cannot serve a team.**
`docs/design.md` is 390 KB of dense prose and `docs/verification.md` 182 KB, both policed by tests. An agent cannot load the design whole, and no section is safe to read alone, because invariants are scattered among rationale and host trivia. Finding: PRD-09.

### 3.3 What has run and what has not

| Has run on a real host | Has only run against fakes |
| --- | --- |
| Capture on both hosts | The background update on Claude Code (every run there was skipped, because a program had started the chat) |
| The resume brief in the CLI and the desktop app's Code tab | Capture of a person's chat on Claude Code on the real install (no Claude Code session captured there is known to be a person's chat) |
| Install, and setup of a wiki on a self-hosted GitLab | `eva-md upgrade` and a whole uninstall |
| Releases cut from a Mac and from GitHub | The digest skill |
| Since the audit, on the real install with Codex (section 13): the background update, 61 calls on a real model; promotion to rules and knowledge; recall on a prompt; the MCP server and the recall skill; the daily commit and push, three times | Whether `doctor --live` or the live accuracy run has been used is a question for the owner |

Running is not the same as measured. None of these runs has a score: nobody has checked what the update wrote against what was said, or whether a recalled statement helped.

## 4. What the outside world tells us

### 4.1 What works in memory for coding agents

The field converged in 2026 on the shape eva-md already has: Markdown files in git, written by a background model run, with a small always-loaded part and on-demand reads. Storage form no longer sets systems apart. Fidelity of what is written, handling of stale facts, precision of what is injected and invisibility do.

The evidence is mixed, and the plan takes the direction the studies share:

| What the evidence says | Where it comes from | What it means here |
| --- | --- | --- |
| A compact handoff note is the best-supported feature: a successor given one needs 20 to 46% fewer steps and 42 to 63% fewer prompt tokens (the raw trace saves 57 to 59% of steps, at about 87,000 characters; neither host was tested). Switching between tasks cost 12 to 14.5 points of success on both hosts. Routing each task its own history recovered most of that loss, but the fix was run only on another agent with open models, not on either host | Handoff and task-switching studies, 2026 | The brief and per-workstream state come first. They must be fresh at every boundary |
| Unsolicited injection is the riskiest feature. Near-miss context hurts more than none. One vendor's per-prompt recall made agents worse until it became a single cached synthesis | Context-rot and distractor studies; vendor report | Silence is the default. Inject only on strong, structural matches, and measure harm, not only hits |
| On real repository tasks most memory products did not beat no memory (11 of 12 pairings in one benchmark), though some did in another, at lower cost. Where they failed it was the form of the record, not ranking | Two coding-memory benchmarks, 2026 | Short, structured records anchored to a path or identifier. Never transcript chunks |
| Model-written context files and self-written skills do not help and can hurt (skills: 8 to 11 points worse on both hosts). Human-curated ones help | Repository-context and skills studies, 2026 | Store only what the repository cannot say. No generated overviews. A procedure becomes a host skill only after a person reviews it |
| Facts cited to code and checked before use are the one design with published outcome gains (merge rate 83% to 90%). Wrong seeded memories were caught and corrected | GitHub Copilot Memory | Every code-bound fact carries its anchors, and code checks them before the fact is shown |
| Lossy extraction is the main measured failure. The two systems that keep the original sessions beside derived facts keep over 90% of the detail; the two weakest keep about 40%, for reasons the study does not give. One vendor dropped its update-and-delete reconciliation because it destroyed context | LongMemEval; staged diagnostics; Mem0's 2026 rewrite | The raw record is the truth. Derived pages point back to it and can be rebuilt from it |
| A model that rewrites accumulated state collapses it (18,282 tokens to 122 in one run). Small deltas merged by code do not | ACE | The model proposes operations on items. Code applies them. No page is rewritten by a model |
| Agents do not reliably choose to look: a skill went unused in 56% of runs where an 8 KB always-present index scored 100% | Vercel agent evals | A small always-present map in the brief. Tools and skills are the deep path, never the only path |
| Shown rules are obeyed about half the time, whether all rules or only the relevant ones are shown. Both hosts' documents say memory is not the place for rules that must hold | Rule-compliance study; host docs | Checkable rules need enforcement at the tool call, and the host's own instruction files, not better retrieval |
| Memory is an injection path: stored payloads steered later sessions in most trials, and "install this dependency" from a preferences file succeeded 80 to 90% | Poisoning studies; both vendors' warnings | Trust tiers. Text that entered through a tool, a web page or a file may be recorded as what happened; it may not become a rule or a fact without the user |
| Lexical search is a strong baseline once tokens respect identifiers; keys written at write time lift it further; static embeddings sit below it on technical text; graphs cost more than they return | Retrieval studies; Mem0 removing its graph store | One lexical core with write-time cues. Embeddings only if our own measurement shows a gap they close. No graph, no vector store |
| Quota is the loudest complaint on both hosts' trackers. Codex's own memory writer uses a small model at low effort and stands down under 25% of the rate limit remaining | Host issue trackers; Codex source | A small default model for the per-chat pass, a quota gate (and no gate on the host being idle: section 6.3), and a cost figure the user can read |

Each row's source, with its link and its fact-check verdict, is in `working-papers/research/`: `evaluation-science.md` (handoff, task switching, benchmarks), `coding-memory-tools.md` (products, Copilot Memory), `memory-frameworks.md` (ACE, Mem0, LongMemEval, the agent-evals result) and `local-retrieval.md` (retrieval).

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
- Rollout files are not an interface: the format is changing, compression of files older than 7 days is in the code behind a flag that is still off, and some threads are not on disk. The app server's thread API is the documented reader.
- Codex's own memories are off by default: a small model extracts from idle chats, a second pass consolidates, and one summary cut to 2,500 tokens is loaded at the start of a thread.
- `codex exec` has an output schema and an ephemeral mode and spends the plan allowance. Plugins with hooks are barred from the public plugin directory. The IDE extension has no plugins.

**What a third-party layer must add.** Both hosts now remember on their own, per repository, on one machine. What neither does is what users ask for in the open: one memory across both agents and across machines, a view of what was loaded and why, facts checked against the code, per-branch work state, team sharing with review, and measured results. That is the ground eva.md should hold. It must not repeat what the host already injects.

## 5. What v2 is

### 5.1 The promise

When you open a new chat, clear one, come back after a compaction or switch branch, the agent carries on as if it were the same chat. It knows what was decided and why, what was tried and failed, and what must hold. It checks what it remembers against the code before it leans on it. You never set it up beyond one command, never curate it, never approve anything for it day to day, and never see it unless you ask what it remembers and why. Sitting down at another machine is part of the aim and not of v2.0: it waits on sync (section 12, decision 21).

### 5.2 Principles

Seven principles decide the design. Each comes from a finding or from outside evidence, and each is used below to settle a choice.

- **A. The raw record is the truth.** What was said and done is kept, redacted, on the machine. Everything else (pages, indexes, briefs) is derived from it, points back to it and can be built again.
- **B. Code owns structure; a model proposes.** Ids, dates, speakers, provenance and page layout are set by code. A model returns small operations on items, each with the exact words it rests on, and code checks and applies them. No model rewrites a page.
- **C. Nothing ships unless a measurement moves.** Every change names the number it must move and is judged with memory off as a baseline. A change that cannot show its effect waits.
- **D. Check before telling, and stay silent when unsure.** A fact about code is checked against the code before it is shown. A weak match adds nothing. Wrong context costs more than missing context.
- **E. Use what the host offers, as a ladder.** Each host capability is a rung: where a rung is missing, blocked by an administrator or broken by a release, the product gets plainer and keeps working.
- **F. It runs itself.** It schedules, repairs and upgrades itself, and speaks only when a person alone can act, in one plain sentence.
- **G. Trust is a feature.** Local first. No telemetry, no account, no process left running. What was loaded, and why, can always be seen.

**Terms used below.** *Pack*: the small set of local files, compiled from the store, that hooks read. *Episode*: a settled part of a chat, a run of whole turns, which is organized once. *Fast pass*: the worker's small-model run over one episode, soon after it settles. *Segment ids*: the fallback form of evidence if small models alter quotes: code numbers the sentences of each entry, the model cites a number, and code copies the words. *Held spool*: a per-session file that hooks append to and that no reader sees until the worker has checked and released it. *The resume test*: v1's hand-run check that a new chat on a real host returns four planted items from its brief. *Card*: the lead's one-page order for a package (section 9). *Evaluation home*: a dedicated folder used as HOME on your Mac, or a second account, with both hosts signed in and nothing of yours in it.

### 5.3 What it will not be

- No graph database, no vector store, no resident server, no local model.
- No model call or network call inside a hook.
- No telemetry, and nothing about your work sent anywhere but to the model provider the chat already uses.
- No generated overviews of a repository, and no memory of what the code already says.
- No rule, fact or skill promoted from text that entered through a tool, a web page or a file, without your word.
- No copy of what the host's own memory already loads.

### 5.4 What "invisible" means, in numbers

These are the bars v2.0 is built to. They are proposals until Stage 0 gives v1's real figures; where v1's column says "not measured", Stage 0 measures it. Figures marked "on the real install" are from the first field report (section 13).

| Moment | v1 today | v2.0 bar |
| --- | --- | --- |
| First run | 4 questions, up to 4 approvals, about 45 lines printed, a repository and a folder to choose | 0 questions, 0 approvals, at most 12 lines, one statement of what is captured and what a model is sent |
| Steps the host imposes | Codex asks for a hook review, and again after most upgrades | one Codex review, then none for any release |
| Upgrade | find out, run a command, answer a question | a daily check and one command that asks nothing, which an agent can run. Background install later, if you agree (decision 9) |
| Commands typed in an ordinary month | upgrade, doctor, and whatever the doctor names | 0 |
| A chat start | up to 8,000 characters chosen by recency; one visible line. On the real install the brief was 4.8 KB and took 0.92 s to build by command; the hook's own time is not recorded | about 2,000 tokens chosen for the work in hand; a brief reaches at least 99.5% of starts, clears and compactions; under 150 ms at the 95th percentile |
| A prompt | capture under 50 ms; recall by word overlap | capture and delivery together under 50 ms; most prompts add nothing |
| A chat's memory on file after it ends | on the real install, half within 158 s, one in twenty later than 88 minutes, the longest 64 hours | within 90 seconds for half of chats and 5 minutes for nine in ten |
| A long chat that has not ended | its state is refreshed when 60 KB is waiting at a turn's end that carries a reply, at a compaction, at a prompt after a 30-minute pause, or when another chat starts in its project once it has been idle 2 minutes; there is no limit in time | a settled turn is on file within the clock's interval plus 5 minutes of the chat going quiet, or at the next hook event of any chat without the clock; a chat that never goes quiet is organized every eight settled turns |
| What the background run costs | the chat's own model at the highest effort, up to 40 calls a host a day; never totalled by eva-md. On the real install: 13 to 18 calls a day, about 41,000 tokens a call and about 120,000 or more for each chat organized, a median of 124 s a call, and two calls stopped at the 600 s limit | at most a fifth of v1's measured tokens per chat, inside a budget shown as one line |
| A daily signing or sign-in prompt | possible every day | never |
| Memory stops working | never noticed by a person with one host | noticed at the next check by a part that still runs, and said in one line |
| Notices on a healthy install | one at every Claude Code start while Codex is unapproved, for good | none in a replayed healthy month |
| Wrong context added to a chat | not measured | at most 1% of additions on a held-out set; no measurable harm on tasks memory cannot help |

## 6. The target design

The audit, the research and six area designs agree on the shape. A critique of the six designs then found that, taken whole, they would build a second system beside the first: 128 work packages, 51 decisions for the owner, 43 commands against v1's 20, and a codebase that would nearly double before anything was deleted. The outside evidence says memory systems fail by doing too much. So this plan draws a line.

**v2.0 is what the evidence asks for.** A brief that hands work over cleanly. Small, cited statements whose author is decided by the record and checked by code. A store that commits every change and can explain and undo it. A background worker that loses nothing. An install that asks nothing. And the instruments that show each of these is better than v1. One writer, one machine, with a pushed backup.

**Everything else waits for a number.** Sync between machines, background self-install, a richer lifecycle for facts, recall while a tool runs, lexical recall on prompts, embeddings, tools that let an agent write memory, rule enforcement, and reach beyond macOS and Linux each have a place in the design and a trigger in section 12 that would open them. None is built on a guess.

Two guards hold the line. First, a size budget: v1's macOS binary is built from 53,694 lines of this repository's Go outside tests (54,807 counting an accuracy harness and a test helper that no release contains), and v2.0 may not be built from more. The count is what `go list -deps ./cmd/eva-md` builds, so the evaluation harness is outside it on both sides. Because v2 is built beside v1 until the way back has closed, every package that adds code names the v1 files it will delete, and CI checks that the lines built into the binary, minus v1 lines scheduled for deletion, stay at or under v1's. The budget becomes a plain ceiling once the deletions land. Second, no part of v2 is switched on by default until its measurement beats v1's on the same instrument.

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
| Thread, rules and knowledge pages | rebuild as items and workstreams | candidates, archive by disuse |
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

**Bars** (hard gates in CI on recorded host fixtures): no text stored at or after a pause, under any pattern of missed hooks; no text stored as the user's typed words that was not; dialog answers and plan decisions captured with the right author in every fixture; a project keeps one identity through a scripted rename, first push, move and worktree. On the real install: at least 99.5% of turns and 99% of tool calls in the host's own record are in eva-md's, where a turn is a prompt that reached the agent.

**Design.**

1. *One reader.* Everything that reads the record goes through one package. It yields typed events with an id (session and sequence), an author, a set of exposure marks, and marked cuts. Five separate decoders in v1 become one. For v1's files the sequence is the line number, so every reference already written in a wiki still resolves.
2. *Whose words a text is, by allowlist.* Each event carries an author: typed by the user, pasted, chosen by the user from options the agent wrote, written by the agent, by a subagent, by the host, by a program, or unknown. It is assigned only on positive evidence, so a new host wrapper is unknown and never "typed". Separately, code marks where in a turn outside text entered (the web, the network, an MCP result, a subagent), with the position of the tool call that brought it, so text written before a fetch is told from text written after. Promotion rules key on both. Whether a statement only echoes what eva-md itself delivered is decided per statement, against the record of what was delivered, never per turn: every chat gets a brief, so a per-turn mark would make all of it echo. This replaces a deny-list that defaults to "the user typed it" (CAP-02).

   Speech is its own author. In a voice call the host hands the agent a wrapper that holds a request, which in the voice versions read is the front model's wording, and the lines each side spoke. eva-md stores each line under its real speaker, the person's as spoken and never as typed, and the front model's request never carries the person's authority (decision 29). What counts as positive evidence is named for each host. Claude Code marks how a chat was started. Codex's hook payload says nothing about where a prompt came from, so there the evidence is the host's own record of what started the turn, read by the worker; HF-00 must confirm it, and what a prompt is when that record cannot be read is settled at the Stage 0 readout (section 8).
3. *What is kept.* Added in v2.0, all through hooks: the question and the answer when the user picks an option in the host's dialog; a plan's text with approved or rejected; a subagent's task and final report, as the subagent's; a bounded, strictly redacted digest of a command that failed; and a record of what eva-md itself delivered into the chat (ids and hashes, not text), so restated memory is never taken for new evidence. Still never kept: the output of a call that succeeded, file contents, credentials in URLs.
4. *Hooks are the floor; the transcript only fills gaps.* The hook's payload is the documented contract on both hosts and the transcript is not, so at the end of a turn the hook's copy of the reply wins. Reading the transcript moves out of the hook into the worker, which removes about 1,200 lines that exist only to slice that work into 100 ms pieces.
5. *The pause switch as a property of order.* The reader exposes a session only up to the point the worker has checked for a pause typed in a prompt a hook missed. Later events exist on disk but no reader returns them until they are checked, and a pause found late removes them. The guarantee no longer makes every hook wait, and it holds in one place instead of five. The phrases are tightened, and unevenly on purpose: a pause is honoured on any prompt not proven to be someone else's, because a false pause is cheap and visible; resuming needs a prompt proven typed, and must be the whole prompt. There is an explicit command, both hosts show a confirmation, and a fork or resume inherits the state. Hooks read the pause state without waiting on the worker. At a chat start, if the previous session's last turn is not yet checked, the hook waits a bounded time for the check, as v1 does today, and otherwise says in the handoff that a turn is still held; how often that happens is counted. A pause phrase spoken as the person's line in a voice hand-over pauses capture from that line on, and nothing inside a hand-over resumes it.
6. *Project identity.* The store holds an identity record for each project: an id derived from the first key that named it, so the same project gets the same id on any machine, with every remote, root commit and old key that has named it since. A chat's key resolves through it. Two kinds of evidence are needed before two keys are joined automatically; a person can join or split by command. Each folder outside git is its own project for new chats.
7. *Redaction.* The rules and their several hundred test cases stay. The driver is rebuilt as one pass that returns spans and runs to a deadline. Stopped there it keeps nothing: today's rules are not local to a line, so no prefix redacted alone can be shown safe, and a redacted prefix waits for rules that are. A labelled corpus gives precision and recall per class for the first time. Then rules change one at a time under it: values are judged by character class and length (today one in five random 32-character hex keys is kept), `key: value` lines are read as structure, and the last gate before a push uses the same detector as capture. With your agreement (decision 7), the frozen baseline becomes a test that no true secret it removes is ever kept, instead of a second engine unioned in on every call; that is what lets non-secret facts about authentication survive.
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
3. *One write path, one commit per pass.* `store.Apply` takes a pass's operations, writes the files and makes one commit whose trailers name the pass, the session and the model. The range of the record that the pass covered is stored in the tree in the same commit, so progress is what the store holds: a restored store says exactly what to replay (STO-12). `eva-md log`, `why` and `undo` read that history; the smallest thing that can be undone goes from a day to a pass. A session that was skipped at no model cost is recorded in local state and makes no commit, so a script's short runs do not fill the repository.
4. *Protection lives in the store and fails closed.* A file a person locked or verified, or one that cannot be read, refuses the one operation aimed at it. The pass and every other file go on. Today such a page stops a whole branch's memory, and `locked: true  # note` is read as not locked. A file changed in the store folder by anything but the engine is kept, marked unverified, and never treated as your words until you acknowledge it: an agent with a file tool must not be able to write memory under your name.
5. *eva-md's own codec.* One line-preserving reader and writer for every file, which keeps every line a person wrote. The page format stays conformant with OKF v0.2, as a written profile and a test. The okf module is dropped (decision 1): the case is control, since a merge by field and fail-closed protection need a reader that keeps lines, which the library's parse-to-struct model cannot give. The binary then carries one third-party module. No file in a store names the library, as the project's naming rule requires, and every git call pins the settings that could otherwise let your own git configuration change bytes or start a signing app.
6. *Commits are eva-md's, not yours.* Authored as "eva.md on <machine>", and never asking for your signature. Whether they are signed with a key eva-md makes for the machine is settled in Stage 0, once the git server says what it accepts (decision 3).
7. *A migration path before a new layout.* A deterministic, resumable step turns a v1 wiki into a v2 store, proven on generated wikis and on a dry run over a copy of yours that prints counts only. The old clone is never touched and stays as the way back. Files that v1's secret gate had held back from a commit are held back by the migration too: its commits pass the same gate, so the upgrade can never push what v1 refused to. A binary older than the store's minimum reader version reads nothing, writes nothing and says so once.

**Kept:** the hardened git runner and the remote guard, no-clobber create and compare-and-swap, the durability order, path and link safety, the secret gate on every line written, setup's privacy rules, the escaper.

**Removed:** the okf module and adapter, `log.md`, every committed index and map, the daily lump commit and its markers, eight of eleven commit guard refusals, five of six frontmatter readers.

**Later, on a trigger:** fetch and merge between machines, with typed merge rules per field and a trust rule for commits that did not come from an enrolled machine; a human checkout with rendered pages; purging history; short excerpts committed beside items.

Detail: [working-papers/design/store.md](working-papers/design/store.md).

### 6.3 Organizing

**Goal.** Every settled part of every chat is organized exactly once into small, cited, typed records, soon enough that the next chat starts from them, at a cost you do not notice. Nothing is passed over without a record of why and a way to try again. What you stated is changed by nothing but you.

**Bars:** no part of the record left neither organized, skipped with a reason, refused with a reason, nor pending with the rule or gate it waits on; no wake lost; nothing of the agent's, a tool's, a paste's or an outside source's stored under your authority; at least 90% of planted facts stored with the right value on a real model; at most 5% of faithful statements refused; at least 80% of decisions made by agreeing to a proposal become durable; after a host change that breaks output, at most 3 model calls burned and nothing lost.

**Design.**

1. *One worker, nothing queued.* Pending work is computed: what the record holds, minus what the store says is covered. A hook's wake is only a hint, so a wake that finds the worker busy loses nothing. The worker decides when a part of a chat is ripe (the chat ended, went quiet, grew long, or was asked for). This replaces six triggers, a daily sweep and state rebuilt by scanning two days of logs. Work that waits carries the rule or gate it waits on, so a person can be told why. No gate waits for the host to be idle: a chat that never goes quiet is organized as it grows. A range has a user turn when it holds a typed or a spoken prompt, so a chat led by voice is organized like any other.
2. *One contract between the model and the checker.* The model is given whole turns and returns small operations on items it was shown by handle: add, supersede, retire or confirm an item; add or close a next step or an open question; one status line for the workstream; notes of what was done, found or failed. Silence means keep. It never writes an id, a date or a page.
3. *Evidence is exact words.* Every operation cites one to three spans: the entry and the exact words in it. Code checks each by exact match. Who said the words comes from the record's author field, never from how alike two texts look. That removes the word-overlap matching and its English word lists from the checker's three files (about 1,860 lines, part of which is date, duplicate and id checking that stays in some form), and works in any language. A dialog choice and a plan approval count as you accepting the agent's words, with both spans kept; a short typed "yes" counts only once Stage 0 has measured how often a refusal would be taken for assent (decision 10).

   A quote proves that words exist, not that they support the sentence written over them. So for anything stored under your authority, the values in the statement (numbers, quoted strings, identifiers, names) must themselves appear in the cited span, and a rule or constraint is stored in your own words, with the model's sentence beside it as a gloss. Those words are committed with the item (decision 33).
4. *Acceptance per operation.* One refused operation no longer discards a whole answer. A refusal carries a code, and the model gets one chance to repair it. An answer is rejected whole only if it is not the schema's JSON.
5. *Admission: default to nothing.* An item is stored only with authority: you stated it, you accepted it, or a tool record shows it. Each add names what it is (an instruction, a decision, an invariant, a fact of the environment, a pointer, a failure, a command). Anything derivable from the repository, and anything written after outside text entered its turn, without your word, stays a cited note in the session's log, where it can be found on request. A note is pushed in one case only: the last session's notes marked found and failed ride in the next chat's handoff for the same workstream, marked as the agent's report (decision 31). A note written after outside text entered its turn is never pushed. "Wrote nothing" is a counted answer.
6. *Who may change what.* Only your later words change an item under your authority. An observation that contradicts it is recorded beside it and shown as a disagreement; it does not replace it (today it can: PGS-07).
7. *Workstreams.* A workstream is bound to its branch where a branch other than the default exists. Work on the default branch and outside git gets a workstream per folder or topic. When a branch merges, its decisions fold into the project by code. A merge is recognized by ancestry and also by patch equivalence, so a squash or rebase merge counts. A branch that is gone and not recognizably merged pauses its workstream and keeps its items, labelled; nothing is retired because a branch disappeared.
8. *A small model, measured.* The default is the cheapest model and effort that meets the fidelity bar on the frontier measured in Stage 0, with one step up when the checker refuses (decision 5). The runner reads the host's event stream, so a tool call or a different answering model cannot pass unseen, and it records the host's version. A timeout is a load signal: retry smaller and lower. A failure nobody recognizes is the host's, not the chat's: three in a row pause organizing for that host version, and a range is blamed only if another range succeeded in between.
9. *System pages* are frozen: no new ones are prompted, existing ones stay reachable on request, and they leave the default brief and prompts. They are model-written overviews of a repository, the kind the evidence found unhelpful, and nothing here has measured them. A person can still ask an agent for a digest, which writes one as today, and the daily check of their cited code goes on: `internal/systempage` and `internal/coderef` are not among the packages the deletions remove. What takes their place by default is an overview that code renders from the items (section 6.4, design 11).

What code guarantees whatever the model writes, and what is only measured:

| Guaranteed by code | Only measured |
| --- | --- |
| Every stored statement cites a span that exists in the record, word for word | That the statement is a fair reading of the span |
| Authority comes from the span's author in the record; nothing typed by an agent, a tool or a page can carry yours | That the model cites the right span |
| Values in a statement under your authority appear in its span | That nothing important was left out |
| Nothing from a paused chat, and nothing written after outside text entered its turn, without your word, becomes a rule or a fact | How much useful memory that rule costs |
| Only your later words change what you stated | That a contradiction was noticed |

The designs differed on six points of the memory model. These are settled here, so the store's format can be specified first:

| Point | Ruling for v2.0 |
| --- | --- |
| Tags of an item | As in the organizing design's section 5.5, set by code from the author of the cited span |
| Kinds of item | Five, matching what v1 holds: `fact`, `decision`, `constraint`, `procedure`, and `rule`. A rule is a standing instruction to the agent and comes only from words you typed. A constraint is a limit on the work or the system |
| Working items of a workstream | Next steps and open questions, each a line with an id |
| What a workstream file says | One cited status line that the model maintains from the previous one, then its next steps, open questions and the decisions in force. No model-written paragraph |
| What a session file holds | A log of notes built from operations (did, found, failed, left) and the range of the record it covers. No model-written summary |
| States of a workstream | `active`, `merged`, `archived` |
| Ids of a workstream | Minted by the store from the project and the branch where there is one, so the same branch is the same workstream everywhere |

**Kept:** the sealed run (stdin, cleared environment, tools off twice over, no transcript left), the strict schema both hosts accept, the anti-forgery rendering, the rule that history from before updates is never summarized by surprise.

**Removed:** the word-overlap provenance and its word lists; the watermark that moves past refused content; quarantine that ends in a skipped chunk; the six trigger thresholds and the sweep; the full-state protocol with a "keep" for every id; model-written summaries; the chat's model at the highest effort as the default.

**Later, on a trigger:** items held as candidates until they recur, expiry and archive by disuse (when the corpus counters show a need); lessons from failures and a preference kind (when the hidden-decision tasks show a gain once failure digests exist); a second, reviewing model pass (when near-duplicates and disputes exist that code cannot settle); an opt-in replay of old chats under the new contract.

Detail: [working-papers/design/derive.md](working-papers/design/derive.md).

### 6.4 Recall

**Goal.** The agent has the right piece of memory in its context at the moment it matters, in a form it can use, and nothing else. A new, cleared, compacted or resumed chat continues the work. A person can see what a chat was given and why.

**Bars:** from a start brief of at most 2,000 tokens, a successor needs at least 40% fewer steps and 50% fewer prompt tokens than with the repository alone, and solves no fewer tasks; corrections per task at least halved on tasks with a hidden decision; a brief reaches at least 99.5% of starts, clears and compactions under injected failures; no item of another project in anything pushed; at least 95% of claims about code that has moved are held back or labelled.

**Design.**

1. *A compiled pack.* A background step compiles the store into a small local file set per project: the brief's sections, exact keys (paths, symbols, commands), and rows for the read tools. Hooks read only the pack and the record's session catalog. No git, no page parsing, no scan of raw files, no model. Chat-start cost stops growing with the inbox. A hook accepts a pack by its format, redaction and tokenizer versions, not by which build made it, so a long chat that outlives an upgrade keeps working.
2. *The brief as typed sections.* A legend, standing rules, the constraints that fit the work, the workstream's state (status, next steps, open questions, decisions in force), other active workstreams, a handoff built by code from the last session (opening request, files edited, open tasks, the last check and its result), the last exchange word for word, and a short map of what else exists. Sections have floors and ceilings in tokens and are fitted by priority, so nothing parses the brief's own text back. The handoff needs no model, which is what makes the state fresh at every boundary; only the last session's findings in it wait until that session has been organized. Output is byte-stable when nothing changed, so it does not break the host's prompt cache.
3. *Each kind of chat gets what it lacks.* New and cleared chats get the start brief. A compacted chat gets what the host's own summary does not carry, and the constraints again. A resumed chat gets only what other sessions changed. A subagent, and a chat a program runs, get rules and constraints and nothing else (decision 14). A brief that a start failed to deliver is owed and is delivered on the next prompt, whatever its kind.
4. *The first prompt routes; later prompts are nearly silent.* The first prompt of a chat picks the workstream by exact keys in the prompt (a path, a symbol, a command, a branch name), with the branch and recency as priors, and brings up to five statements that carry those keys. A first prompt with no key keeps the workstream the brief showed and adds nothing. After that a prompt adds only what another session changed, and items whose exact key (a path, a symbol, a command) appears. There is no matching by loose word overlap in v2.0. The evidence ranks these far apart: a compact start is the best-supported feature, and per-prompt injection the riskiest.
5. *What a chat holds.* A set of item ids per context window, reset on compaction, replaces the "sent once per chat" list that today suppresses a constraint for good once a compaction has removed it.
6. *Checked before told.* Each item's code anchors are checked against the repository in the background, as v1 already does for system pages. An item whose code is gone is held back; one whose code moved is labelled. A held item is never pushed. A disputed item is not pushed either, unless it is under your authority or protected: that one is still delivered, with the words that a later observation disagrees, so a disagreement can never silence what you said.
7. *Two read tools.* One finds (a question, a path or an error, staged from this project outward, with a token budget and paging) and one opens by handle with its neighbours and evidence. They replace three tools that re-expose the command line. Chat search moves to the same tokenizer, matches across a turn instead of a line, and loses its 90-day window.
8. *One scope rule for every surface.* Hooks, the command line and the MCP server read through one access table. Reading across projects is closed by default: this project, items marked for all projects, and projects you have explicitly shared (decision 13). Your authority cannot be borrowed: a command that would assert it (forgetting for good, sharing a project, confirming an item) acts only when run on a terminal with no host marker in its environment. Run from an agent's shell it previews and asks.
9. *Receipts.* Every delivery is recorded as ids and hashes. `eva-md receipts` shows what a chat was given and what was held back.
10. *Beside the host's own memory.* eva-md never writes or configures it. The two divide the work: the host's memory keeps preferences on one machine; eva-md keeps work state, decisions and constraints across both agents. Overlap is counted, and the host's memory is a condition in every experiment.
11. *An overview that cannot go stale.* For each project, `eva-md show` and the open tool return one page that code renders from the store when the pack is compiled: what is in force, what changed lately and what it replaced, what is uncertain, and what the agent reported and nobody confirmed. No model writes it, so it cannot disagree with the items. It is the rendered view decision 22 promises, and the answer to the stale system page the owner's note found (section 14).

**Kept:** the fence and the forgery defences with their tests, strict bounded reads of anything injected, fail-closed silence on any error, the privacy gates, the hard caps under each host's limit.

**Removed:** the periodic reminder line; every injected line that hands the agent a shell command; recall by word overlap and project-name detection in prompts; sibling scope inferred from an agent-written list; the recall skill and the `eva_status` tool.

**Later, on a trigger:** delivery when the agent edits a path or runs a command (a Claude Code experiment first); lexical matching on prompts (only if a held-out set shows precision of 0.95); embeddings (a numeric trigger, never in a hook); generated rules files and reviewed skills in the host's own folders; tools that let an agent write to memory; asking the host to confirm a tool call that breaks a rule you armed.

Detail: [working-papers/design/deliver.md](working-papers/design/deliver.md).

### 6.5 Hosts and operations

**Goal.** A person installs eva-md with one action and answers nothing. From then on it keeps itself current, repairs what a program can repair, and speaks only when a person alone can act.

**Design.**

1. *Hosts behind one interface.* What is known about a host (its capabilities by version, its quirks, where its CLI is) sits in one adapter with a table. Host names leave the rest of the code, where they appear in 29 files today. A third host becomes a table and an adapter, not edits across packages.
2. *Codex: the hooks in a part that never changes.* Codex binds a plugin's approval to a hash of every file in it. So the hooks go in a two-file plugin that is frozen, with all routing in the binary behind a fixed path, and the skills and the MCP entry go in a second plugin that changes freely. Releases after the counters release and before the cut-over change no plugin byte, so they cost no review; the counters release itself is expected to cost one on an install still on 2026.11.5 (decision 17). The frozen plugin is installed once, when you run the upgrade to v2.0, and that review is the last. Nothing is ever written inside a plugin's folder. Claude Code binds no approval to plugin bytes, so one plugin stays there.
3. *An install manifest.* Each install records the files it rendered, with hashes, so any release can replace or remove another release's files and still refuse files it never wrote (OPS-07).
4. *A first run that asks nothing.* One command registers the plugins and creates the store locally. It prints, once, what is captured, what a model is sent, and how to exclude a project or stop. Preferences a person edits are kept apart from state the program owns, and a broken preferences file falls back to the last one that validated instead of switching memory off.
5. *A clock.* With your agreement (decision 4), one short-lived run every 15 minutes, started by the operating system, wakes the worker when no chat is open: it pushes, checks health and looks for a release. Nothing stays running. Without it, hook wakes carry the same steps while a chat is open, as v1 does today.
6. *Health that sees missing work.* For each host and each stage (host activity, capture, organizing, commit, push) eva-md keeps the time of the last success and compares the lag with a budget. That is what turns "nothing recorded a failure" into "capture has been silent for two days while the host was in use". The most severe condition that is still relevant is shown, once a day, as one line prepared outside the hook. A condition for a host you no longer use fades. Repairs a program can make are made; the rest is one plain ask. The stages include the two waits v2 itself adds: events checked for release, and the pack compiled. `status`, with every row shown, gives each lag beside its bar (section 5.4 for organizing, section 6.2 for commit and push, ten minutes for the release check) and, for work that waits past it, the worker's reason; the six-line verdict names only the stage furthest past its bar. A notice is raised only past a coarser budget, so a healthy day stays silent. No row reads as healthy while a counted loss is past its bar.
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
| Continuation | 28 tasks split across two sessions: 10 handoffs, 10 with a decision hidden in the first session, and 8 control tasks that memory cannot help, from which the "no measurable harm" bar of section 5.4 is read. Run with memory off, with eva-md, and with a hand-placed ideal record; the host's own memory as a fourth condition on a signed-in Mac. Graded by hidden tests, steps, tokens and corrections, never by a model | live, with your leave and inside a budget you set |

Further suites (task switching, rule compliance, drifted code, poisoning, aging over 200 sessions, a corpus audit) are each built when a decision in this plan waits on its number, not before.

**What "best" will mean.** There is no accepted ranking for coding-agent memory, so the claim is an evidence package: resuming is cheaper than with memory off and no worse than the host's own memory; decisions stick; nothing of another author is stored under your name; nothing is lost; and every number has a command that reproduces it. It will not be a rank against named products or a score on a conversational benchmark.

Detail: [working-papers/design/evaluate.md](working-papers/design/evaluate.md).

## 7. Decisions for the owner

Each row changes a recorded decision or a rule in `AGENTS.md`, or is a product choice only you can make. Every one has a recommendation and a designed fallback, so "agree with all recommendations except these" is a complete answer. Decisions that only matter for work deferred past v2.0 are in section 12 beside the item they belong to.

In the last column, "Decision N" means decision N of v1, in `docs/decisions.md`. Everywhere else in this plan, "decision N" means row N of this table.

| # | Decision | Recommendation | If you say no | What it reopens (v1's decision numbers) |
| --- | --- | --- | --- | --- |
| 1 | Drop the okf module and own the page codec, keeping the format conformant with OKF v0.2 | Yes | Each file is saved through okf behind today's adapter. Comments and hand-written frontmatter are not preserved, its lock file sits in the store, and every okf release stays a risk | Decision 15; the rule that okf stays pinned; two dependencies become one |
| 2 | eva-md owns the memory repository in its own data folder, created with no question and no remote; a remote is attached later | Yes | The store stays the folder you chose and setup keeps its questions. The guard refusals stay, and a stray lock still stops memory until a person acts | Decisions 1 and 16 |
| 3 | Commits are made as "eva.md on <machine>", one per pass, and never ask for your signature. Sign them with a key eva-md makes for the machine if the git server accepts that; otherwise leave them unsigned | Yes, settled by Stage 0's check of the server | Per-pass commits stay local and are squashed into one commit with your identity and signing at each push, as v1's daily commit is. The daily prompt stays and single-pass undo ends at the push | Decision 7 |
| 4 | One short-lived run every 15 minutes, started by the operating system (a launchd user agent; a systemd user timer on Linux) | Yes | Hook wakes carry the same steps while a chat is open. Nothing runs with every chat closed, so Friday's memory is pushed on Monday and a silent stop is noticed only at the next chat | "No sleeping jobs" |
| 5 | Organize with a small model at low effort, chosen from a cost and fidelity frontier measured on real hosts, with one step up when the checker refuses | Yes | The chat's own model at a low default effort, with the token budget kept. Several times the tokens per chat | Decisions 5 and 17 |
| 6 | Keep more of a chat locally: answers given in the host's dialogs and plan decisions (always), a subagent's task and report, and a bounded, strictly redacted digest of a command that failed | Yes, behind one setting | Dialog answers and plan decisions only. Lessons from failures and what subagents found reach memory only when a final reply restates them | Decision 16 (tool activity without output) |
| 7 | Stop running the old frozen redaction rules on every text. Today each text is redacted twice, by the frozen rules and by the current ones, and whatever either removes is removed. Instead a test in CI would prove, on a labelled corpus and on generated secrets, that the current rules remove every true secret the frozen rules remove, and only the current rules would run. The frozen rules' files are never touched | Yes, once that test shows no secret kept. What you give up: a secret of a shape that is in neither the corpus nor the generators, and that only the frozen rules would have caught, would be kept | Both sets keep running on every text. Ordinary facts about authentication (header names, endpoints, key paths written as `key: value`) stay masked for good, redaction takes about twice the time in every hook, and a quoted key header still deletes the rest of an entry | The `AGENTS.md` rule on `redact.Redact` |
| 8 | A session file may be rewritten by one maintenance step that keeps every event id, so one line can be forgotten now, and a secret missed by an old rule can be repaired at rest later (section 12) | Yes | Forgetting stays whole-session, and a missed secret stays on disk until its whole chat is removed | "The inbox is never rewritten" |
| 9 | Upgrades: a signed manifest, a daily check, one line when a release has waited a week, and `eva-md upgrade` that asks nothing. The manifest is signed with a key held as a secret in the approval-gated release environment, with a second, offline key on your Mac for releases cut from a Mac and for rotation. Installing in the background is deferred | Check now; decide on background install when there are users whose fixes are waiting | Upgrades stay as v1: a person finds out, runs a command and answers a question. With no key, a release is checked only against a file from the same release, as v1 | "No auto-update", only if background install is later chosen |
| 10 | A proposal you accepted becomes a durable decision, with both spans kept as evidence: always for a dialog choice or a plan approval; for a short typed yes only if Stage 0 shows a refusal is not taken for assent | Yes for decisions and procedures, never for standing rules | Typed words only. A decision made by accepting a proposal stays on its workstream, as in v1, and never becomes a lasting decision | Decision 5's promotion bar |
| 11 | Hold back an item whose cited code is gone, label one whose code moved, and record a contradicting observation beside an item. Nothing but your words changes an item under your authority | Yes | Label only. Facts about deleted code keep arriving with only a date to warn the agent | "Never changed without the user" (narrowed to: never changed, may be held back) |
| 12 | System pages: stop prompting for new ones, keep existing ones reachable and writable on request, remove them from the default brief and prompts, and measure before investing in them. The overview that decision 22 already promises (DL-26), one page per project rendered by code from the items in force, takes their place by default | Freeze the pages | System pages are not frozen: their pointers stay in the default brief and in prompts as in v1, unmeasured. The overview is built either way | The system pages stage of the roadmap |
| 13 | Reading across projects is closed by default: this project, items for all projects, and projects you share explicitly. v1's inferred sharing becomes a one-time review | Closed, with the review. On the real install most sessions are in folders outside git, so this review is what lets something learned in a working folder reach chats in the repositories it is about. Sharing is for reading; a chat in one project can change another's item only with your own words, and otherwise leaves a disagreement beside it | As v1: an agent may search every project, with the project named on each hit | A deliberate v1 choice |
| 14 | Subagents, and chats a program runs, get standing rules and constraints at their start, and nothing else | Yes | Nothing for either. Delegated work starts without the project's constraints | |
| 15 | Per-component specs and dated decision records replace `docs/design.md` and `docs/verification.md` as the source of truth, and the tests that police those two files' prose are replaced | Yes | The two files are split so agents can read them, but stay the truth and stay policed. Every behaviour change still edits shared files | Two lines of `AGENTS.md` |
| 16 | Live evaluation runs with standing leave inside a ceiling you set, instead of being asked for each time | Yes; set the ceiling after the Stage 0 pilot prices one session | Asked each time. Packages wait, integrated and unproven, until someone answers | The `AGENTS.md` rule on live runs |
| 17 | Install one v1 point release that adds only content-free counters and changes no plugin byte of its own, use it for a week, and bring back a second report. The real install is on 2026.11.5, two releases behind, so this upgrade also carries the two skill files changed in 2026.11.7, which is expected to cost one Codex review, and that release's changes to v1's behaviour (section 8) | Yes | The first report only. Capture loss and freshness are then decided on inference | |
| 18 | A candidate build may be replayed over your own local record, on your machine, printing counts only | Yes | Synthetic scenarios and the field report only. A rule that misbehaves on real phrasing is found after release | |
| 19 | Tighten the pause switch, unevenly on purpose: a pause is honoured on any prompt not proven to be someone else's; only a prompt proven typed, and only as the whole prompt, resumes capture; both hosts show a confirmation, there is an explicit command, and a fork inherits the state | Yes | As v1: a pasted line can resume capture, and recovery can pause a chat without telling you | Decision 16's off switch |
| 20 | Each project gets an identity record with aliases, and each folder outside git is its own project | Yes. On the real install 20 folders share one project today. They become 20, and decision 13's review says which of them share memory | Aliases only. Renames and first pushes are fixed; folders outside git still share one area | Decision 10 |
| 21 | v2.0 is one writer on one machine with a pushed backup, built so sync can be added; sync between machines waits (section 12) | Yes | Sync moves into v2.0 and adds a large package, a multi-machine test suite and the signing rule of decision 3 above as a security boundary | Decision 1 (one wiki per machine), later |
| 22 | Rendered pages (a workstream's state, a project overview): local views, or committed so they read well on a git host | Local views; the repository holds items and sessions. Tell us if you read the wiki on the git host | A small set is committed and regenerated after every change, with commits caused only by rendering | Decision 2's "inspectable as plain Markdown" (kept either way through `eva-md show`) |
| 23 | Which systems v2.0 runs on | macOS and Linux | macOS only | "macOS only" |
| 24 | Beside the hosts' own memories: never write or configure them, divide the work, count the overlap, and test against them | Yes | If integration is wanted: read the host's memory as an input, later. Cost: dependence on an undocumented format | Decision 8 |
| 25 | The host asks permission the first time an agent uses eva-md's read tool | Write nothing in the host's files. `status` prints the one rule you may add yourself | If you want it written: one command shows the rule, writes it with a backup, and removes it on uninstall | "Never write host settings" |
| 26 | The cut line itself: v2.0 is section 6, held to the size budget, and section 12 waits for its triggers | Yes | Name what moves in. Each item in section 12 has its cost beside it | |
| 27 | Standing leave for the build team, all of whom are agents, the lead included. Agents may commit, push package branches and open pull requests. The lead merges a package into `main` when CI is green, its diff stays inside its card's paths and a second agent's review is closed; no person reads it first. You alone merge a change to any of these: `AGENTS.md`; the files under `.github/workflows/`; `scripts/release.sh`, `install.sh` and `uninstall.sh`; a file that holds an accepted baseline for an instrument or for the latency gate; anything under `internal/redact/baseline`; the frozen Codex plugin's two files. You cut every release | Yes | Every commit, push and merge waits for your yes, as `AGENTS.md` says today. Nothing reaches `main` unread, and the build moves at the speed of those answers | The `AGENTS.md` rule "don't commit or push unless asked", for the build only |
| 28 | Where live evaluation runs, and who pays | Containers, with two API keys that you make for evaluation only, one for each vendor, and hand to the lead as secrets. Those runs are billed to the keys at API prices, not to your plan; the lead states the price of the model comparison and of the pilot in money before either starts. The signed-in evaluation home on your Mac only for what needs a plan login | Everything in the evaluation home on your plan: no keys to make and nothing billed beyond the plan, but runs are serial, draw on your plan's allowance, need the Mac awake, and you start each one | The `AGENTS.md` rule on live runs |
| 29 | What you say in a voice call. The host hands the agent a wrapper holding a request (in the voice versions read, the front model's wording) and the lines each side spoke. Either way eva.md stores each line under its real speaker, as spoken and not as typed, and never gives the front model's wording your authority | Your spoken line can make a decision, a fact or a procedure yours. A value heard in speech (a name, a number, an identifier) is kept as heard and never replaces one you typed. A standing rule or a constraint from speech waits until you type or confirm it | Speech is evidence below typing: what you decide by voice stays a proposal on its workstream until you type or confirm it. Less is built, and work led by voice leaves little lasting memory | |
| 30 | How much of a voice call is kept | What each hand-over carries: the request and the talk since the last one, within the host's limit (4 KiB a field in the source read, so a longer stretch can lose its start). Nothing after the last hand-over reaches a hook. eva.md says so where it states what is captured. Reading the rest from the host's own record waits for two host facts (section 12) | The record reader also takes a call's lines from the host's session file (M, inside LG-21), which rests on a file format the host calls unstable | |
| 31 | What the agent finds by reading code, when you neither state nor confirm it. v1 stores it as an observed fact: 15 of the 39 knowledge pages on the real install. Three courses: (a) it stays a dated note, found only on request; (b) the note also travels: the last session's findings ride in the next chat's handoff for the same workstream, marked as the agent's report, and the overview lists them; (c) it becomes an item marked as the agent's reading | (b), which is S, inside DL-10 and DL-26. One word from you makes a finding an item | (a). Nothing is built, and what an investigation found reaches a later chat only when the agent goes looking. (c) is not on offer at the readout: it waits in section 12 until the investigation tasks show a gap and anchors can name another repository | For (b): section 6.3's rule that a note is never pushed, and section 5.3's line on what the code already says. For (c): section 6.3's admission rule as well |
| 32 | What redaction is for. Today, and in v2 as planned, it removes credentials. It does not remove personal details, your own or other people's, which stay in the local record, can be sent to the model, and can be written by it into memory pages that are committed and pushed to your git server | Credentials only, said plainly in the README now and in the first-run statement later; your own patterns (S) if your answer to question 5 of section 13.6 asks for them | Classes of personal data are added to the rules and the corpus (M). More text is masked, which is the loss of ordinary facts that problem 9 describes, and the classes work for English only | |
| 33 | A standing rule or a constraint is stored, committed and pushed in your own words (redacted, at most 240 characters), with the model's sentence beside it | Yes | Only the model's sentence is committed, checked to hold the values of the span it cites. Your words are shown from the local record, so they do not survive a restore from the remote | "No captured text is ever committed"; section 12 keeps the wider case, excerpts beside every item |

**When each is needed.** Before any code: 1, 2, 5, 6, 7, 15, 16, 17, 20, 22, 26, 27, 28 and 33, because they fix the formats, the method, where live runs happen and what the redaction driver can promise. Decision 5 joined this list after the first field report: a pass at the highest effort took a median of 124 s and two were stopped at the time limit, which crosses the threshold written in section 8, so a lower default is no longer a choice to weigh at the readout. Only which model, and at which effort, still waits for DV-03. At the Stage 0 readout, when Stage 2 is sized: 3, 4, 8 to 14, 18, 19, 21, 23, 24, 25 and 29 to 32. Until a decision is made, the build assumes whichever column builds less: the "if you say no" column for every decision but 12, 21, 22, 24, 25, 26, 30, 31 and 32, where it assumes the recommendation, because there the other column is the larger build. For decision 12 the overview is built either way, since decision 22 already promises it. For decision 31 the build assumes (b), because DL-10 builds the handoff once. No builder waits on an answer.

**Where the decisions stand.** On 2026-11-10 the owner answered decision 27 more widely than its recommendation: the team works on `main` directly, with no pull request and no approval for any file, the files that row lists included. Releases are the exception and stay with the owner. The owner also said no further approval is needed, so every other row is taken at its recommendation and can be changed by a word from the owner before the release that carries it. Two rows cannot move on leave alone, because they need something only the owner has: decision 28's keys, without which no live evaluation runs in containers, and anything decision 16 covers that must run on the owner's machines or accounts. On 2026-11-13 the size check's first reading (EV-11's card) showed the binary built from 63,236 lines, 9,542 more than v1's, and two questions on decision 26 went to the owner, who left the judgment to the lead. The lead ruled, and a word from the owner changes it: (1) the ceiling stays 53,694 built lines and binds at CUT-4, where section 9 already calls it plain; until then the net figure is reported at every merge and fails nothing, and the lead brings it, with a ledger of what leaves and when, to each stage's readout. The ledger is [size-ledger.md](size-ledger.md) (EV-11b, merged on 2026-11-14): by its reading that day the end state is 47,030 lines against the ceiling's 53,694, which leaves about 6,700 lines for what v2 still builds into the binary. This takes the place of "at any merge" in sections 6 and 9, which Stage 1 could not meet, since its packages delete nothing before the cut-over. (2) The instruments of the counters release are temporary: `audit shapes`, `audit capture`, `doctor --report` and the counters behind `status --updates` and the two doctor rows stay in the release while v1 is the product, so that a field evaluation runs as it does today, and each leaves the binary at CUT-4 unless a v2 package has by then taken its work over inside the budget.

Choices the lead will make unless you object, because they are technical and reversible: v2 is built on the main branch in new packages behind one mode key that defaults to v1's behaviour; no model judges any gated number; ids are 14 characters; an evaluation-only build tag lets a scripted session count as a person's chat, with tests that no release contains it.

## 8. The roadmap

Five stages. Each ends at a gate, and the plan is revised at each gate against what was measured. Sizes are rough agent effort: S is up to two days, M about a week, L more. They are for ordering, not promises.

Package ids match the area designs in [working-papers/design/](working-papers/design/), so the detail for each is one search away. Five ids are this plan's own and have no row in a design: SEAM-0, HF-00, IF-01, EV-04 and DL-26. CUT-1 to CUT-4 come from the delivery critique. For those the plan's row is all there is, and the lead's card must carry the rest. "(shrunk)" marks a package cut down from its design by the critique; the plan's wording governs.

### Stage 0: ground truth

No rebuild. The aim is to replace inference with measurement on the one real install and on real hosts, and then fix the cut line. It also lays the seams that let many packages touch the same files safely. It holds back four Stage 1 packages: LG-13 (the host sitting), DV-11 (the live gates), EV-17 (the pilot) and HO-13 (the readout). IF-01 waits for SEAM-0. EV-14 is built at once, but takes v1's row from DV-02's recordings and its voice families from HF-00's fixture. It decides the size and order of Stage 2.

| Id | Package | What it gives | Needs from you |
| --- | --- | --- | --- |
| SEAM-0 | Seams and guards | A command table with one file per command, a handler list for the hook, and registries for status rows, config keys and benchmark scenarios, so adding one is a new file and not an edit to a shared one. To make that true, the tests in `cmd/eva-md/docs_test.go` that tie each command and file to one sentence of `docs/design.md` and one line of `AGENTS.md` are replaced by a check against the command table, and that line of `AGENTS.md` is rewritten once to point at it. A hash test over the whole Codex plugin folder, rendered with a fixed binary path and pinned to the bytes `main` renders today, which are 2026.11.7's and are what the counters release writes; 2026.11.5, the release on the real install, differs from them in the two skill files. An upgrade test from 2026.11.5, from a fixture of its installed files made once in a temporary home and committed. A gate that, with every new flag off, v1's own tests pass unchanged, and that the old and the new binary, run alternately on the macOS job through EV-16's comparison step, which lands first because the macOS job does not run the benchmark today, differ on the benchmark by no more than two runs of the old one do | Decisions 15 and 27, and one merge of `AGENTS.md` |
| EV-00 | Field report folded in | Each Stage 0 measurement has a value or a stated unknown. Done for the first report on 2026-11-10: section 13 holds the readings, and the rows below carry what they changed | Your answers to the questions in section 13.6 |
| HF-00 | Host facts in one sitting per host, with recorded fixtures (LG-03, HO-01, ST-00, DL-01 merged) | First, one fact sheet in which the host facts of the six designs (ledger section 13, deliver section 12, hosts section 14, store section 16, derive section 11, evaluate section 14) are merged and numbered once, each marked for the sitting, for DV-02 and DV-03, or for EV-03; the run-book is written from it. Then real hook payloads and transcripts from scripted sessions with made-up content, for both hosts, regenerated from the recordings' field names and shapes with made-up values so nothing of an account or a machine reaches a public repository. Every sitting fact marked on the sheet by the lead's rule of 2026-11-11: the seven core facts confirmed or refuted by the run-book's Part 1, each with its fallback, and every other one marked later (asked for, by the run-book's Part 2, when the package that needs it is about to be built), by the second field report, or from published source. Of the three named here: how often a reply's row is missing from the transcript when its hook fires is sampled by every Stop of Part 1; whether a message typed while the agent works fires a prompt hook is counted on Codex by the second report and waits for Part 2 on Claude Code; whether work started by the operating system's scheduler can reach the model and the git server waits for Part 2's group J, so decision 4 is taken at the readout as "yes, confirmed by group J before HO-22". What the two field inputs added is listed below the table | On your Mac, an evaluation home: a separate user account. Then Part 1 of the run-book, which states its length before you start: three short sittings and a hand-over, about 180 minutes of steps (about 220 with a margin). The evaluation home; one scratch repository that you create on the git server, two pushes to it from the evaluation home, and its deletion right after, then Claude Code; then Codex, with one voice call in its desktop app. You sign both hosts in with your own subscription and approve a scratch plugin's hooks in Codex. Made-up content only. The rest (dialog answers, plan approvals, a compaction, timed hook runs, a second voice call and more) is Part 2, asked for group by group when a package needs it. The real install is touched only by read-only reports |
| LG-01, LG-02, DV-01, DL-02, HO-02 | One v1 point release with counters only, built as one package after SEAM-0 and after EV-16's baseline | How often v1 holds an event back; turn and tool coverage against the host's own record; the share of turns in which outside text entered, by class; what the checker and the triggers cost; how often a brief is stale; how health and approvals behaved. It changes none of 2026.11.7's behaviour, on which it is built. It adds one read-only command, one flag on `status`, a report mode on the doctor and two doctor rows. The first report asked for exactly this, and the full list of what it counts is below the table. It adds no plugin byte of its own, but the real install is on 2026.11.5, cut before commit 51b241c (2026-11-07) changed the plugin's two skills, so the upgrade is expected to cost one Codex review, done before the week starts. That review, or its absence, is the first reading of the "Codex reviews needed after upgrades" row below | One upgrade, a week of ordinary use, and a second report from a second prompt, written with this package, that reads the new counters (decision 17) |
| EV-16 | Latency that fails a build, and a macOS baseline | Hook time at realistic sizes and for a whole turn of 50 tool calls, measured on macOS before any hook changes; a build that slows a hook fails. One scenario has the real install's shape: 600 session files, one of 2 MB and the other 599 sharing 2.3 MB, with a wiki of 54 pages. There the brief took 0.92 s to build by command, three times v1's own target for a chat start, and the benchmark has no scenario that would show it. Its baseline is taken on the commit before SEAM-0 merges, since SEAM-0 turns the hook into a handler list; the new scenario is added through SEAM-0's registry afterwards. Merged before the counters package. Until the results store exists the baseline is a file in the repository, taken from the macOS job's own output, since builders work in Linux sessions | One merge: the gate and its baseline file are yours to merge (decision 27) |
| DV-02, EV-01 | Live gates on v1 | v1's update meets real models on both hosts, at the highest and at a low effort; the output is recorded as fixtures; the resume test is run with the update on | You run them, from a checkout on the Mac, following the lead's run-book. Leave for 21 calls per host and effort, 84 in all, plus two `doctor --live` calls and a few chats run by hand |
| DV-03 | Contract spike and the cost and fidelity frontier | The new contract's open bets settled by measurement: default model and effort, exact quotes against segment ids, whether a repair round pays, and how often a refusal or hedge is cited as assent, on scripts with planted refusals, typed and spoken, the spoken ones built on the wrapper HF-00 records and holding planted mishearings. A throwaway prototype, deleted when DV-21 lands; the lead names the three model tiers per host before the run | Leave for about 1,900 small calls, roughly 25 million tokens and 3 hours over both hosts, and about a tenth more for the spoken scripts (the design's estimate; the lead states the exact plan before the run) |
| EV-03 | Pilot (shrunk) | Ten tasks under memory off, v1 and an ideal record: variance, tokens and minutes per session, and whether the tasks can show a difference at all. The lead writes the ten tasks; they run on EV-17's driver and its v1 condition, which were built first (the lead's ruling of 2026-11-13, in the cards' index), so only the tasks and their export are thrown away; where the sessions run follows decision 28 | Leave for about 180 sessions, roughly 60 million tokens and a day (the design's estimate). You name a ceiling for the pilot itself, since decision 16's ceiling does not exist until the pilot has run |
| EV-04 | The cut line fixed | This plan revised against the table below | Your agreement to the revised plan |

**What the counters release counts.** It is a v1 release cut from `main`, and every counter is free of content: some read stored text to count it, and none writes or prints any. Beside what its row names:

- every session by what v1's own files show of it (a subagent's id, a program's start on Claude Code, an end with no start, no turn at all), and prompts that arrive inside a voice hand-over counted apart, by their prefix. v1 has nothing on Codex that says a prompt was typed, so the count of typed prompts and the class of a Codex session come from the second report's prompt, which reads the host's own record with the mark HF-00 found and is finished after the sittings;
- what 2026.11.7's redaction rules would catch in the record stored under 2026.11.5, by class;
- what was added to each prompt and to each kind of start, by route (rules, constraints, state, exchanges, recalled statements, pointers);
- a week's distribution of each hook's run time (2026.11.7 keeps only the last 20 runs of each event, and 2026.11.5 none);
- the trigger that started each update, each job that left because another was running, with its session, and each skip with its reason, so that the time from a chat's boundary to memory on file can be read by trigger;
- the age of the oldest event not yet organized, read from the event, and past v1's limit of the 64 newest files of a folder;
- turns that end with no stored reply, by cause; exact repeats by kind, and how many of them pair a hook's line with a recovered one;
- recoveries that found no host record, split into those whose payload named none and those whose path was named and refused;
- sessions in the host's own store that have no eva-md file, counted by file name, with those left out by design counted apart;
- the model a run reported using, where a pause came from, runs of `eva-md system write`, edits under the wiki folder, and code-status entries with no clone.

Three sentences go into v1's documents with it: that the daily commit saves what had been written by the day's first event; that a voice hand-over is stored as the user's words, and talk that is not handed over is captured only if the host later carries it in a hand-over; and that redaction removes credentials and not personal details, which stay in the local record, can be sent to the model, and can be written by it into pages that are committed and pushed.

**What the counted week measures.** The real install runs 2026.11.5. The counters release is built on 2026.11.7, which already added a daily sweep of idle chats, a project scope on recall and rules for passwords on command lines. So the week measures 2026.11.7's behaviour and the second report says so. Its figures are compared with the first report's only where those three changes cannot explain a difference. Text stored under 2026.11.5 keeps whatever its rules missed; the counters release counts what the newer rules would catch in the stored record, which gives that number with no content (decision 17).

**What the host sitting must settle.** Beside the fact sheet's own list, the two field inputs added the items below. Since the lead's cut of 2026-11-11 ([the run-book](host-sitting-runbook.md)), Part 1 settles only what is core among them, as marked after each; the rest are marked on the sheet as later (Part 2's groups), by the second report, or from source.

- On Codex, what marks a prompt as typed. The hook payload has no field for where a prompt came from. The first candidate is the mark in the host's record of what started the turn; the sitting records it for a typed prompt, a voice hand-over, a subagent and a scheduled run, and whether that row is on disk when the prompt hook fires. Core (F-121), in Part 1, but the subagent case: Codex's published source shows that a subagent's turn carries its parent turn's mark and names its parent turn, and the second report counts by that. Whether the row is on disk is read in source (F-122), and Part 1 records it at no added minute.
- A voice call in Codex's desktop app, with made-up content: two hand-overs, one of them while the agent is still working; an exchange that is not handed over; a stretch of talk with no hand-over long enough to pass the host's 4 KiB limit; a spoken correction; a spoken yes to a spoken proposal; the pause phrase, spoken; the call ended normally, and once by closing the app. The recording keeps every hook payload with its turn id, and the row types of the session file. The sitting also settles which voice version the app uses, and so whether a hand-over's request is the front model's wording or the person's transcribed words; whether a task that voice starts by itself gets its first prompt inside the wrapper; whether a hand-over made while the agent works shares its turn's id; and whether talk past the limit and the end of a call are written to the session file as wrapper-shaped rows that no hook saw. Core where it decides how a spoken line is stored and under whose authority (F-142 to F-144): Part 1's one call. The stretch past the limit and the call ended by closing the app are Part 2's group L, and the second report counts wrapper-shaped rows that no hook saw.
- Which sessions end with no start. Codex's source says any thread that is opened and never run does. The sitting confirms it, and whether the desktop app's threads name a session file eva-md can read. By the second report (F-138, F-137); group B has the step if it leaves them open.
- One session a program drives, on each host, and what tells it from a person's chat. Codex's in Part 1 (X-26), for F-121; otherwise by the second report (F-018), and Claude Code's in Part 2's group A.
- Whether the record of a read or a search in another repository names that repository. Later: Part 2's group G, before DV-23 (F-015).
- Where the time of an MCP call goes, against the same search by command; and whether changing a skill file alone makes Codex ask for a new hook review. Later: Part 2's group F, before DL-12 (F-042), and the second report, then group H, before HO-20 (F-150).

**The release train.** You cut every release. The first carries the counters, SEAM-0's seams, which change no behaviour by SEAM-0's own gate, and whatever Stage 1 packages have merged by then, each switched off. The upgrade does change the real install's behaviour, from 2026.11.5's to 2026.11.7's (see above). No release that changes behaviour beyond that goes to the real install until the second report has been read, so the counted week measures 2026.11.7 and not a half-changed v1.

What each result changes. The thresholds are written down now so the reading cannot be bent to fit the plan. They were written before the first field report came back and have not been edited since; its readings against them are in section 13.2.

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
| Typed refusals cited as assent, on the planted scripts | any at the default model and effort | Decision 10 stays at dialog choices and plan approvals; a short typed yes does not count |
| Pilot: an ideal record against memory off | under 30% fewer steps | The tasks are rebuilt before any result is trusted |

Three rows were added on 2026-11-10, after the owner's note and before any of them was measured:

| Measurement | If | Then |
| --- | --- | --- |
| A person's Codex prompts for which the host's record proves typing (counted by the second report's prompt in the host's own record, with the mark HF-00 found) | under 95% | Author by allowlist falls back, on Codex only, to "typed unless it carries a known wrapper", and every such prompt is counted |
| | 95% or more | Allowlist only: a prompt with no proof is unknown |
| A person's prompts that arrive inside a voice hand-over | 10% or more | A voice hand-over joins DL-04's labelled events and at least two of EV-17's tasks, beside the voice fixtures EV-14 and DV-03 already have |
| | under 2% | Voice stays at what the rows already hold (decoding in LG-13, the spoken families of EV-14 and DV-03, the spoken clauses of LG-24, DV-21, DV-23 and DV-24). No voice case is added to DL-04 or EV-17 |
| Lines of a voice call in the host's record that never reach a hand-over (counted the same way, by row type alone) | a fifth or more | Decision 30's second option moves into LG-21 |

### Stage 1: instruments, and changes that ship on v1

Nothing here changes a user's data. Each instrument gives v1 a row that Stage 2 must beat. The product changes evolve packages that v2 keeps (redaction, install, health, and a worker around v1's own jobs). Each sits behind a switch that defaults to today's behaviour, and is turned on in a release only after the second report has been read and its own gate is green.

| Id | Package | Outcome and gate | Size | Depends on |
| --- | --- | --- | --- | --- |
| EV-10 | Specs and decision records (shrunk) | `docs/design.md` and `docs/verification.md` frozen and split into a spec per component of at most 24 KB and dated decision records; each spec lists the must-keep items it touches and the tests that hold them | M | decision 15 |
| EV-11 | Ownership and size checks (shrunk) | CI fails a change that touches paths outside its package's list, and fails when the Go built into the binary, minus the lines of v1 files that merged cards have scheduled for deletion, exceeds v1's 53,694; a plain ceiling after CUT-4 | S | EV-10 |
| EV-12 | Results store and gates (shrunk) | Every result is a row with commit, host version and model; "better or worse than the accepted baseline" is one command | M | |
| EV-14 | Fidelity scorer, with v1's score (EV-13 shrunk into it) | A black-box, value-exact scorer with a precision side, on v1's 12 scripts and about six new families, half held out. One family plants instructions meant for the moment, which must not become standing rules, because the field report read several saved rules as situational. Three more extend the families to what the owner's note asks about. A finding with a stated limit (not found in a sample, tested and not deployed, tried and refused, a hypothesis): dropping the limit, or storing the hypothesis as a fact, is a wrong write. A value the user corrects, followed by the old value in the agent's reply, in a front model's line and in a misheard spoken line: the corrected value must be the one in force. A proposal the user asks about or quotes without agreeing, typed and spoken: storing it under the user's authority is a violation. Gate: it catches a wrong value, a dropped date and an invented item that v1's scorer passes | L | LG-10; DV-02 for v1's row; HF-00 for the voice families |
| DL-04 | Labelled retrieval set and scorer | Precision, recall, false additions and silence, with near misses and a held-out half; v1's recall is the recorded baseline. The set holds name events: an alias that resolves to one item, with near misses where the name is an ordinary word | M | |
| EV-17 | Continuation harness (shrunk) | 28 tasks, three conditions, hidden tests; v1's row recorded. Sessions run in Linux containers, so the evaluation build lifts the macOS-only gate first. At least four of the handoff tasks are investigations across two repositories, run from a folder outside both, because that is how the real install is used. At least two of the hidden-decision tasks hide a stated fact about a name. Each time the driver has to supply a planted fact counts as a correction; where a delivery record exists it is also reported as asked again when the record shows the chat was given that fact. v1's row, which has no such record, reports corrections only | L | EV-03, EV-12 |
| IF-01 | Fakes and the contract matrix | Each track's interface is written by that track's own first package: the reader's by LG-10, the store's by ST-01 and ST-02, the runner's by DV-11, the retrieval core's by DL-04's scorer. IF-01 adds a fake of each that other tracks' tests can import, and a matrix that maps every need between tracks to the package that provides it; a card whose need has no provider fails its check | M | SEAM-0; LG-10 and ST-01 for the types |
| LG-10 | Event model and the one reader | Typed events with stable ids over v1's files; a v1 inbox read through it yields the same text at the same reference. Every session has a class (a person's chat, a subagent's thread, a program's run, a thread that was opened and never run, the host's own housekeeping, or unknown), set from positive evidence as authorship is; a thread whose prompts arrive through a voice hand-over is a person's chat. The author values include two for a voice call: spoken by the user, and the front model's, for its request and its spoken lines. The spec states the key that makes two events one: the host's row id where there is one, otherwise the turn, the kind and the tool call's id or the line's position, and never text alone. Read over v1's files, the reader marks a second copy and counts them for each session | M | |
| LG-13 | Hook payloads through adapters; author by allowlist | Every fixture payload decodes to its labelled events; nothing is filed as typed without positive evidence. The spec names that evidence for each host (section 6.1). A voice hand-over decodes to separate events: its request as the front model's, each line as its speaker's, the person's lines as spoken. A person's line that only repeats the request is unknown unless the host's record confirms it as speech, and no fixture hand-over yields a typed event. On Codex the hook files a prompt as unknown and LG-21 sets its author | M | HF-00, LG-10 |
| LG-11 | Redaction driver with spans, same behaviour | `Apply(Scan(x))` equals v1's output on every test case and 150,000 generated inputs; a cancelled scan keeps nothing. (The design asked for a redacted prefix. The reviewers showed two inputs on which a prefix redacted alone keeps a password that the whole text's redaction removes, because the rules are not local to a line. A safe prefix waits for rules that are, in LG-26.) Both engines still run | M | |
| LG-12 | Redaction corpus, rates and the oracle | CI prints recall by class, the rate of facts lost and of hex keys kept; oracle violations are 0. With that and decision 7 agreed, the baseline leaves the run-time path and time at 16 KiB at least halves; without decision 7 both engines still run and a large reply is cut at the hook | M | LG-11, decision 7 |
| DV-12 | One worker, no lost wake | v1's jobs run under one worker with a journal; a wake that finds it busy loses nothing (hard gate). Its status call gives, for work that waits past its bar or is held, one reason from a short closed list: waiting for the size or quiet rule; behind a running job; deferred by the day's budget; held after a failure, with its class and the next try; waiting for sign-in; switched off, paused by the owner or excluded | M | SEAM-0, for its command, hook handler and status row |
| DV-11 | Runner with a probe and typed outcomes | Host failures classified from structured fields; a run cannot pass with a tool call or another model unseen; the host's version is recorded | M | DV-02 |
| DV-13 | Cost ledger, token budget, small default | What organizing spends is one number and a budget the worker obeys. Until DV-03 names a default, the chat's model at a low effort; then the default it names. After the first report this is the first behaviour change to ship once the counted week is read (section 13.3) | S | DV-12, decision 5 |
| HO-10 | Host adapter and capability table | One place knows a host; host names leave the rest of the code as packages are touched | M | |
| HO-11 | Install manifest | Any release can replace or remove another release's rendered files, proven by an upgrade test from each earlier layout | M | HO-10 |
| HO-12 | Health that sees missing work (shrunk) | Lag against a budget per stage, measured from the oldest event still waiting and not from a file's date; the most severe relevant condition, one line, prepared outside the hook; no notice in a replayed healthy month. A host nobody uses has no rows that say UNKNOWN, and no file kept in the state folder can read as a fault when health is good (section 13.4). The reverse holds too: no row reads as healthy while a counted loss (events held back, events kept as a name only, turns with no reply) is past its bar. `status` with every row shown gives each stage's lag beside its bar, the worker's reason for work that waits, files written and not yet committed, and that what the search index lacks is still searched. The short verdict names only the stage furthest past its bar, with its lag and that reason | M | HO-10; DV-12 for the reason |
| HO-13 | Signed release manifest, daily check, an upgrade that asks nothing (HO-25 shrunk into it) | A binary and the install script verify a release offline; `eva-md upgrade` runs with no question; the previous binary is kept | M | decision 9; starts after the Stage 0 readout |
| HO-14 | Fake hosts that fail the way real ones do | Tests can exercise hash-bound trust, host versions and policy blocks offline | S | |
| ST-01 | Store format specification and the OKF profile | Every file format, id rule and protection rule written down, with the kind and state tables as ruled in section 6.3 and the tag table of the organizing design's section 5.5 (`stated`, `accepted`, `observed`, `suggested`, `external`, `echo`); a hand-made fixture store passes the profile checker. It says where a frozen system page lives in the store, which DV-35 needs. The tables say that a disagreement is a stored record in v2.0 and name its fields, carry the author value for speech, and keep a label for an item that is the agent's reading, which the migration needs whatever decision 31 says. They also say that a hand edit carries no authority until it is acknowledged, which overrides the store design | M | decisions 1, 2, 22; written to cover both answers to decision 3 |
| ST-03 | One line-preserving, fail-closed codec | `Write(Read(b)) == b` over a corpus of hand edits; every unreadable protection value protects; parse equality with okf on v1 pages | M | ST-01 |
| ST-04 | Migrator and minimum reader version (shrunk) | One idempotent, resumable step with a dry run; a crash at any point reruns to the same tree; an older binary reads nothing and says so once | M | ST-01 |
| ST-02 | Storage properties as tests (shrunk) | Crash safety, exactly once, protection, restore and replay stated as properties and run against a reference model, each killed by a broken variant | M | ST-01 |

### Stage 2: one core rebuild and one cut-over

The new store, the new contract and the new brief are built in five tracks (record, store, organizing, recall, hosts; the proof track gates them) against the interfaces and fakes of Stage 1, behind one mode key with three states: `v1`, `shadow` and `v2`. Everything is additive. No package deletes a v1 path until the way back has closed. In the table, a dependency on another track's Stage 2 package (ST-05, LG-22, LG-23) means its Stage 1 interface and fake for building, and the package itself only for integration. Integration is three packages the lead owns: record into organizing, store into organizing and recall, and the whole pipeline on fixtures (EV-20). The cut-over is its own set of four packages with one owner: CUT-1, the mode key, lands first in this stage, before any track's package merges; CUT-2 to CUT-4 come at the end.

| Id | Package | Outcome and gate | Size | Depends on |
| --- | --- | --- | --- | --- |
| LG-23 | Decisions and delegated work in the record (shrunk) | Dialog answers, plan decisions, subagent tasks and reports, failure digests and delivery records are typed events, all through hooks. Gate: every fixture decision captured with the right author; a successful command stores no output. A tool event carries the head commit of the repository it ran in where the hook can read it inside its budget, and none otherwise | M | LG-13, decision 6 |
| LG-21 | Recovery out of the hook (shrunk) | The transcript is read by the worker; the reader exposes a session only up to the checked point. Gate: no leak after a pause under generated patterns of dropped and delayed hooks; no duplicates, and no real event dropped as one, on fixtures with two hand-overs in one turn, two replies in one turn and one line repeated across two hand-overs. With the Stop hook dropped, the final reply is in the record exactly once, taken from the host's record by the worker with no later hook of that session; where no host record can be read the turn is closed by a marked gap, and such turns are counted for each host. The age of the oldest event not yet checked is reported, against a bar of ten minutes. On Codex the worker sets each prompt's author from the host's record of what started its turn, the evidence HF-00 names; a prompt whose record cannot be read is unknown, or typed under the fallback of section 8's threshold, and both are counted | L | LG-10, LG-13, DV-12 |
| LG-22 | Project identity and aliases | One id through a scripted first push, rename, move and worktree; a fork under a new remote is never joined automatically | M | LG-10, decision 20 |
| LG-24 | A pause switch that can be seen | Strict phrases, an explicit command, confirmation on both hosts, inheritance on fork. The pause phrase as a person's line in a voice hand-over pauses capture from that line on; nothing in a hand-over resumes it | M | LG-21, decision 19 |
| LG-26 | Redaction that keeps facts and finds more secrets (shrunk to two policies) | Under the corpus: at most a fifth of v1's lost facts, under 1% of hex keys kept, the gate before a push flags everything capture replaces | M | LG-12, decision 7 |
| ST-05 | Store core | An owned repository, `Apply`, one commit per pass, coverage in the tree, protection in the store. Gate: the single-machine properties pass on the real store; a locked item is byte-identical after 1,000 passes aimed at it; a pass is durable in under 2 s. A file changed by anything but the engine is changed only by an operation that carries the user's typed words, or by acknowledgement; any other operation aimed at it is refused and recorded | L | ST-03, ST-04 |
| ST-06 | Attach, change and remove a remote; push each pass | A local store can be backed up and re-pointed with one command; the age of the oldest change not yet off the machine is reported | S | ST-05 |
| ST-07 | Migrate a v1 wiki | Every unit of v1 text accounted for exactly once on generated wikis and on a dry run over a copy of the real one; the old clone byte-identical afterwards; no planted secret that v1's gate held back is reachable from anything pushed; the same wiki migrated twice gives the same tree | L | ST-05, LG-22 |
| ST-08 | Audit, undo, restore and replay | `log`, `why` and `undo` per pass; a deleted store restores from its remote and queues exactly the uncovered ranges | M | ST-05 |
| DV-20 | Episodes and a cursor | The model's input is whole turns with stated context, read through the one reader; no file is rescanned. The open next steps of the project's other active workstreams are among what a pass is shown, so a step opened in one and finished in another can be closed. Where decision 13's review shares another project with this one, that project's items that carry a key of the episode are shown too, each marked as another project's, so that DV-23 can change one on the user's words or leave a disagreement | L | LG-10; decision 13 for the shared items |
| DV-21 | Extraction contract and its checker | Delta operations, exact quotes, authority from the record, acceptance per operation, one repair round. An assent may be cited as a whole entry however short, so a one-word yes can be evidence. Gate: nothing misattributed, and on the voice fixtures no quote from a front model's line or from a hand-over's request supports anything under the user's authority; fidelity above v1's row; at most 5% of faithful statements refused | L | DV-03, DV-20, LG-13 |
| DV-22 | Failure semantics (shrunk) | Two priorities, one daily cap, one hold with backoff, a pause after three host-shaped failures; nothing skipped without a record. No gate waits for the host to be idle: the hold after a rate limit is the gate, one episode at a time. A timeout is retried smaller. Work the budget deferred is computed again the next day | M | DV-11, DV-12 |
| DV-23 | Items and workstreams (shrunk) | The five kinds of section 6.3 (v1's four and `rule`); authority rules; branch binding and folding on merge; an observation never replaces what the user stated. An operation refused by a lock, a verified mark, an unacknowledged hand edit, the user's authority, or because the item belongs to another project and the operation does not carry the user's words, leaves a dated disagreement beside the item. A spoken span never replaces an item that rests on a typed one. If HF-00 shows that a tool record names the repository a path belongs to, an item's anchors name that project and the head recorded when the tool ran, or no commit and the label unmeasured where none was recorded; otherwise such a path is a cue and not an anchor | M | ST-05, DV-21 |
| DV-24 | The fast pass, end to end | A chat's memory on file within minutes of its end, at a fraction of v1's cost, measured live. A range has a user turn when it holds a typed or a spoken prompt. With a fake runner and an injected clock, a session that never ends and has an event every minute has its first eight settled turns covered | M | DV-20 to DV-23, DL-06 |
| DV-25 | Migration of v1 progress and meaning | Nothing organized twice and no statement lost; v1's observed items that rested on the assistant's report are relabelled, not removed | M | ST-07 |
| DL-05 | One tokenizer and one strict reader | Every match in the product uses the same tokens; every injected file is opened by the same checked reader | S | DL-04 |
| DL-06 | Retrieval core (shrunk) | One package serves three callers on the one tokenizer: exact-key lookup for prompts, the find tool's staged search with the search index's scorer, and the items the organizing pass shows the model. Gate: v1's row on the retrieval set is not worse. That gate covers DL-04's name events. If v1's row on them is the better one, a cue found word for word in the user's own words of its item, and shaped like a name, counts as an exact key, at a precision of 0.95 with at most 1% false additions on the held-out half | M | DL-04, DL-05, DL-08 |
| DL-07 | One access layer | Hooks, command line and MCP return the same item set for the same scope, by test; the one-time review, which lists v1's inferred sharing and each project that decision 20 makes from a folder outside git, and lets the owner share each with the repositories it is about | M | LG-22, ST-05, decision 13 |
| DL-08 | The pack | Hooks read a small compiled file set; chat-start cost no longer grows with the inbox | M | ST-05 |
| DL-09 | What a chat holds (shrunk) | A set of ids per context window, reset on compaction, and an owed-brief flag | S | LG-23 |
| DL-10 | The typed brief | New, cleared, resumed and forked chats each get the brief defined for them; after a compaction the start brief is delivered again until DL-16 replaces it. The handoff carries the last session's findings, the notes marked found and failed, cited and marked as the agent's report (decision 31), leaving out any note written after outside text entered its turn; the gate plants one and finds it in no brief. Gate: the handoff suite not worse than v1's row; under 150 ms | L | DL-08, DL-09 |
| DL-11 | Delivery wired into the hook | Start, first prompt and exact keys, beside capture with its own budget and failure count; a failed delivery is owed and retried | M | DL-06, DL-10, LG-13 |
| DL-12 | The two read tools | An agent reaches a memory in one or two calls, inside its scope, with paging. Repeats of one statement take one place in a result, and a call has a time bar on both routes, command and MCP, set from HF-00's measurement. One call lists a project's open disagreements with both sides. A frozen system page is shown under a heading that code writes: its day, how far its cited code has moved, and the newest items learned since | M | DL-06, DL-07, DL-08; DV-23 for the disagreements |
| DL-13 | Chat search on the same tokenizer (shrunk) | Matches across a turn, needs no 90-day window; the search index's own scorer is kept. A result shows one speaker's words under that speaker's name. An event released after the last index build is found with no refresh, by test | M | DL-05, LG-10; LG-21 for the release test |
| HO-20 | Codex hooks in a frozen plugin (shrunk) | Frozen once, after HF-00's measurements, with a test on the bundle's hash; skills and the MCP entry in a second plugin | M | HF-00, HO-11, LG-23 |
| HO-21 | The first run | Install asks nothing, says once what it does; a project can be excluded or memory stopped with one command. The statement names each surface that is captured only in part (for a voice call: what was handed to the agent and the talk the host carried with it, within the host's own limits, and nothing that no hand-over carried) and says what redaction does not remove (decision 32) | M | HO-11, ST-05 |
| HO-22 | The clock | The worker is woken on a schedule; hook wakes are the fallback | M | DV-12, decision 4 |
| HO-23 | Health on v2's evidence (shrunk) | Each silent stop in the fault table is noticed by a part that still runs; three channels: a chat-start line, one line in a read tool's result, `status`. The stages include events checked for release and the pack compiled, each with its lag | M | HO-12, HO-22; LG-21 and DL-08 for the two added stages |
| HO-24, HO-26 | The cost line and the command surface | `status` is a verdict of at most six lines with a cost line; about a dozen verbs | S | DV-13 |
| HO-27 | The move of a v1 install (shrunk) | The upgrade adopts a v1 install: its files into the manifest, `config.env` into preferences, old state cleaned, in an order a crash can resume | M | HO-11, HO-20, HO-21, ST-07 |
| HO-40 | Linux release assets (shrunk) | eva-md installs and runs on Linux | S | HO-22, decision 23 |
| EV-20 | Gates as areas land | Each rebuild judged against v1's row on the same instrument. Two gates run the whole pipeline. One recorded ordinary turn for each host is followed from hook payload to the record, a search hit, a stored item and a commit; the merge gate asserts the order and the counts, and the times are printed. And a privacy matrix: one made-up secret of each tier and one harmless marker are planted at every way in; a byte search of every way out (the record, the chat index, the pack, the model's input, commits and what is pushed, hook output, both read tools, logs and status) finds no secret, and finds the marker only where the capture contract keeps text. Then the same secret is written raw behind each gate in turn (a record line, a store file, a pack row, the fake runner's output), and a search of every way out after that gate finds none. Receipts and the one report join the matrix when DL-20 and EV-23 land in Stage 3. v1 gets a row on the same matrix first | M | the packages above |
| EV-21 | Migration rehearsal | The move from v1 proven on generated installs before any user runs it | M | ST-07, DV-25 |

**The cut-over.** Four packages, one owner (the lead), in order:

| Id | Package | Outcome and gate |
| --- | --- | --- |
| CUT-1 | The mode key | One key with a state table for every area. In `v1` the product behaves exactly as today, by test |
| CUT-2 | The shadow release and a week of real use | In `shadow`, v2 computes and logs counts beside v1 on the real install. Shadow organizing runs under its own small budget, with your leave, and writes to a scratch store that is discarded afterwards. The readout is the gate: coverage, zero leaks after a pause, zero misattribution, the refusal rate, and the precision of what the new brief would have shown |
| CUT-3 | The cut-over release (runs HO-27, ST-07 and DV-25) | One upgrade you run: the migration, the new plugins and their one Codex review, mode `v2`. v1's paths stay in the binary and the record is the same files, so a test abandons after simulated v2 use and finds every chat readable by v1, and v1's daily commit then pushes to the remote with no person's help. This release is the bridge: it holds both stacks, and any later release that has dropped v1's code refuses a v1 layout with one line naming it. The first real migration is run with you watching. Gates into it: the rehearsal green, your dry run reporting nothing unaccounted, fidelity above v1's row, handoff not worse, latency inside its ceilings |
| CUT-4 | The deletions (ST-13, DV-26, and the record's, recall's and hosts' own) | One release after the 14-day way back has closed: v1's update, the thread and knowledge page packages, okf, triggers, in-hook recovery and guided setup are removed. Gates: shipped lines at or under v1's, and every gate still green |

### Stage 3: small fixes that are owed, and whatever a number asks for

Owed, because each closes a confirmed finding at small cost:

| Id | Package | Outcome | Size |
| --- | --- | --- | --- |
| DL-16 | A compacted chat keeps its footing | The constraints and a complement of the host's summary are delivered after compaction; a lost one is retried. A probe on each host plants a decision, a constraint, an open next step and an open question before the compaction and finds all four after it; turns not yet organized are named as pending | S |
| DL-15 | Subagents and program-run chats read the rules and constraints | Delegated work starts with the standing rules and constraints and nothing else | S |
| DL-26 | The overview (a new id; the working paper on the note calls it DV-36, and DL-14 in the recall design is recall at tool time, which section 12 defers) | `eva-md show` and the open tool return one page for each project, rendered by code at pack compile from the store alone: what is in force; what changed in the last 30 days and what it replaced; what is uncertain (open questions, open disagreements, items whose code moved or is unmeasured); what the agent reported and nobody confirmed; and a link to any frozen system page, with its day. One line on the brief's map names it. Gate: every line is an item, an open question or a cited note, with its id; replacing an item moves the old statement under changed in the same compile; output is byte-stable when nothing changed | M |
| DL-18 | Stale claims held back or labelled | Drift of an item's code anchors decides whether and how it is delivered. A label carries the day it was measured and the clone's branch; a saved measurement is used only for the head it was taken at; an anchor whose own commit lacks its path reads as unmeasured, never as gone. An item held back or labelled is shown, in that state, to the next pass whose chat touches its path, so that detection ends in a confirmed, replaced or retired statement, or, for an item under the user's authority that the user has not spoken to, in a disagreement recorded beside it | M |
| DL-20 | Receipts | A person can see what a chat was given and what was held back. One content-free counter says how often the user had to state again a value the store already held, split by whether the item had been delivered to that chat. It is read from the organizing pass's confirmations and from dialog answers, never by matching words | S |
| LG-30 | Forget one line (shrunk) | Forget by session, turn or event; items that rest only on it are retracted | M |
| DV-30 | Housekeeping (shrunk) | Exact duplicates merged | S |
| DV-32 | Corpus counters | Numbers that say whether a richer lifecycle or a reviewing pass is needed. They include items in force by the age of their last confirmation, the share with a code anchor and the share whose anchor is unmeasured | S |
| DV-35 | System pages frozen | They cost nothing unasked and can be measured. `eva-md system write` still works when a person asks for a digest, and the daily code status is still taken for them. Before CUT-4 removes okf, `internal/systempage`, which reads and writes through it in five files, moves onto the store's codec and `Apply`, by test, and ST-01 says where a system page lives in the store | M |
| EV-23 | One content-free report | After v2 the field report is one command's output | S |

Asked for by a number: each item in section 12 whose trigger has fired by then.

### Stage 4: proof and release

| Id | Package | Outcome |
| --- | --- | --- |
| LG-50, DV-50, HO-51 | A week on the real install | The bars of section 5.4 that only real use can show are met: a brief at 99.5% of starts, clears and compactions; memory on file within 90 seconds for half of chats and 5 minutes for nine in ten; at most a fifth of v1's tokens per chat; no signing or sign-in prompt; no command typed; no notice while healthy. Or the release notes say which were not met and why |
| EV-51 | Releases read the gates | A release cannot be cut over a red or stale gate |

The cut-over release (CUT-3) is the first build of v2 and is what section 6.5 means by the upgrade to v2.0. The release cut here, after Stage 3's owed packages, the deletions and the week of proof, is the one announced as v2.0.

### What runs in parallel

Six tracks own the paths below, so their packages can be built at once. The lead owns the files every track touches: `go.mod`, `AGENTS.md`, the hook list in `internal/install/templates.go`, the command table in `cmd/eva-md/main.go`, and the docs index. These rulings make the tracks disjoint:

- `internal/hook`: the record track owns `hook.go` and the handler list. Each handler file belongs to the track that needs it: `trigger.go`, `spawn.go` and `maintain.go` to organizing; `resume.go`, `pointers.go` and `recall.go` to recall.
- `cmd/eva-md`: each command file belongs to the track whose package it calls: `update.go`, `maintain.go`, `knowledge.go` and `system.go` to organizing; `commit.go` to store; `code.go`, `index.go` and `mcp.go` to recall; `inbox.go` to record; the rest to hosts.
- `internal/host` is two packages: `internal/host/capture` (record) and `internal/host/adapter` (hosts).
- `internal/install` is hosts', except the hook list (the lead's) and the recall skill's template (recall's to delete). `internal/config` is record's, except `answers.go`, which is hosts' to delete. `internal/update/accuracy` is proof's.
- Packages the table does not name: `internal/identity`, `internal/catchup`'s parsers and `internal/inbox`'s writer and removal go to record; `internal/wikigit`'s runner and push to store; `internal/threadpage` and the rest of `internal/knowledgepage` to organizing, frozen until CUT-4 deletes them; `internal/systempage` to organizing, frozen and kept (section 6.3, design 9); `internal/searchindex`, `internal/freetext` and `internal/resume`'s search and show to recall, which keeps them; `internal/release` to hosts; `internal/testlimits`, `internal/testgitenv` and `scripts/benchmark.py` to proof.

| Track | Owns | v1 code it freezes, then deletes |
| --- | --- | --- |
| Record | `internal/ledger` (new), `internal/hook`, `internal/config`'s core, `internal/paths`, `internal/session`, `internal/redact` | `internal/catchup`'s hook slicing, `internal/inbox`'s readers |
| Store | `internal/store` (new), `internal/gitrun` (new) | `internal/okfadapter`, `internal/wikigit`'s commit path, `internal/wikiinit` |
| Organizing | `internal/work` (new), `internal/derive` (new) | `internal/update`, `internal/hook/trigger.go`, `internal/lint` |
| Recall | `internal/pack`, `internal/brief`, `internal/retrieve` (new), `internal/mcp`, `internal/coderef` | `internal/resume`, `internal/hook/pointers.go` and `recall.go`, `internal/knowledgepage`'s recall and scope |
| Hosts | `internal/host` (new; its capture half is the record track's), `internal/install`, `internal/health`, `cmd/eva-md` | the guided setup, `config.env`, the doctor's rows |
| Proof | `cmd/eva-eval`, `internal/eval` (new; never in a release) | `cmd/live-accuracy`, `internal/update/accuracy` |

The critical path is short: the one reader (LG-10), then episodes and the contract (DV-20, DV-21) on one side and the codec and store core (ST-03, ST-05) on the other, meeting at the fast pass (DV-24), the migration (ST-07) and the brief (DL-10).

**The calendar is set by waits on real use, not by agent effort.** v1's 120,000 lines were written in nine days, so building is not the limit. Four waits are fixed and come in sequence: the counted week, the shadow week, the 14 days in which the cut-over can be abandoned, and the week of proof. That is about five weeks that no number of agents shortens. Work in progress is capped by review and integration, at about six packages building at once.

## 9. How the build is run

The build is done by a team of coding agents working in parallel, with one lead that orchestrates. You decide, approve each stage gate, and run whatever needs your accounts or your machine.

**Roles.**

| Role | Does |
| --- | --- |
| Owner | Answers the decisions of section 7. Approves each stage gate. Runs the field report, the host-facts sitting and upgrades on the real install. Runs the Stage 0 live gates from a checkout on the Mac, following the lead's run-book. Gives leave for live runs and sets their ceiling. Cuts every release and merges the changes only the owner may merge (decision 27) |
| Lead | Writes each package's card. Reviews specs, contracts between tracks, decision records and any change to a shared file. Integrates. Prepares the live evidence (run-book, commands, reading) and runs it wherever decision 28 gives it a signed-in host or a key. Revises this plan at each gate and reports what was measured |
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

**How a package is accepted.** One of three ways, so nobody invents a metric where none belongs. *Moves*: a named measure beats its baseline. *Holds*: every gate stays green while something is replaced or deleted. *Guards*: a new gate exists and fails on a fault seeded to trip it. Every card splits its acceptance in two. The *merge gate* is deterministic and offline, something a reviewer can run with the project's checks. One timed check is also a merge gate: EV-16's comparison of the old and the new binary in alternating runs on the macOS job. It runs outside `go test`, compares two binaries on one machine in one run, and may block a merge only after an unchanged binary has passed it 20 times with no fail between them. A run the gate could not judge counts neither way, since it blocks nothing, but fewer than one in four of those runs may end so. The *prove step* is anything live, and any ceiling in wall-clock time on a path that has no old binary to compare with (ST-05's 2 s, DL-10's 150 ms, DL-12's time bar): the lead measures those on the evaluation Mac, and no test asserts them.

**Rules that keep parallel work safe.**

- v2 is built on `main`, in new packages beside the old, behind one mode key that defaults to v1's behaviour. `main` stays releasable, and the real install can run shadow modes from ordinary releases.
- Before the way back has closed, no package deletes or changes a v1 path that is scheduled for deletion. Packages that v2 keeps evolve in place, each change behind a switch that defaults to today's behaviour. With the key at `v1` and every switch off, v1's own tests and benchmark are unchanged on every merge.
- A diff stays inside the paths on its card. The lead checks it at the merge, until the ownership check (EV-11) does.
- v1 packages scheduled for deletion are frozen from the day the counters release is cut. Nobody improves code that is going away. Exceptions, each named on its card: DV-11, DV-12 and DV-13, which put v1's update behind the new runner, worker and budget, each behind a switch that defaults to today's behaviour. Before the freeze, Stage 0's counters add content-free fields in `internal/update`, `internal/resume`, `internal/catchup` and `internal/hook`.
- Shared files have one writer, the lead. The seams of SEAM-0 keep that list short: a new command, hook handler, status row, config key or benchmark scenario is a new file.
- Each track's interface lands in Stage 1 as types and signatures. After that a contract changes only through a decision record.
- The hook declaration changes once, in HO-20, and is installed at the cut-over. A package whose acceptance needs an event Codex does not yet declare is proven in the evaluation home, and its card says so.
- Every flag has an owner and the package that removes it.
- Every rule in `AGENTS.md` holds unless a decision in section 7 changed it. In particular: `internal/redact/baseline` is never edited, tests never touch the real machine or depend on its speed, hooks never call a model or the network, and only eva's name is shown.
- The code built into the binary, net of v1 lines scheduled for deletion, is no larger than v1 at any merge. After CUT-4 the ceiling is plain.

**Gates on a release.** Every merge: both CI jobs and every hard gate. A release that changes the contract or the model policy: fidelity on held-out scenarios on both hosts. A release that changes the brief: the continuation tasks. The cut-over: the migration rehearsal green and your dry run reporting nothing unaccounted. A hard gate is never waived; another gate only by you, in a dated decision record.

## 10. Migration and the way back

Nothing a user has is lost, and at every step there is a way back.

1. **Stage 1 changes no data.** Its releases run on v1's inbox and wiki. After the counters release they change no plugin byte, so Codex asks for nothing more.
2. **Shadow before cut-over.** From an ordinary release, the new contract and brief run against a scratch store and log counts only. A week of shadow on the real install comes before any switch, and its readout is the gate. The scratch store is then discarded: the real store starts from the v1 wiki.
3. **The cut-over is one upgrade you run.** In order: a dry run of the migration prints counts and stops if any v1 text is unaccounted for; the store is created in eva-md's data folder as a clone of the v1 wiki, with its whole history, and the migration is one commit on top; v1's progress markers become coverage, so nothing is organized twice; the pack is compiled; the new plugins are installed, which asks for one Codex hook review, the last; and you are shown, once, the projects v1 treated as sharing memory, to keep or drop each, and the folders that shared v1's one general area, to say which repositories each shares memory with.
4. **What stays untouched.** The old wiki clone, byte for byte. The inbox, which the new reader reads in place; every reference already written still resolves. Pages a person locked or verified become protected files. Pages the migrator cannot read are carried under `legacy/` as they are.
5. **The way back.** For 14 days `eva-md migrate --abandon` points eva-md back at the old clone. v1's paths are still in the binary, and the record is the same files, so no chat captured since the cut-over is lost. Only after those 14 days does a later release delete v1's code. The old clone is still there afterwards, and nothing ever deletes it.
6. **A remote.** If the v1 wiki had one, it becomes the store's remote. For the 14 days the store pushes to a branch of its own there, so the branch v1 pushes to stays where the old clone left it, and an abandon leaves v1 able to push as before. When the 14 days close, the v1 branch is moved up to the store's head, which is a fast-forward. Nothing is forced.

## 11. Risks

| Risk | What limits it |
| --- | --- |
| The hosts move weekly: hook payloads, transcript formats, trust rules, and how headless runs are billed | One adapter per host with recorded fixtures per version. The brief needs no model, so memory degrades to "less organized", never to "nothing". Organizing pauses itself on an unrecognized host and resumes on a version change |
| A cut-over of the store, the contract and the brief at once | Shadow on a scratch store first, a rehearsal on generated installs, a dry run on a copy of the real wiki, the old clone untouched, 14 days to abandon |
| Most captured sessions may not be chats: on the real install at most 47 of 600 have a recorded start and how many hold a chat is not counted, so a rate taken over sessions flatters or hides whatever it measures | Every session has a class, every counter is reported by class, and each bar in section 5.4 is read over chats a person had |
| The plan was designed for typed chats in one repository on a branch, and the real install also works by voice, from folders outside git, across repositories | The host sitting records a voice call. Speech is its own author. The instruments gain voice fixtures, tasks outside git and investigations across two repositories. Decisions 13, 20 and 29 to 31 are the owner's to set for that use (section 14) |
| The plan grows back into a second system | The cut line, the size budget checked in CI, a trigger written down for every deferred item |
| A small model alters quotes, so little is stored | Measured in Stage 0 before anything is built on it; a fallback to segment ids; one step up to a larger model |
| A faithful quote sits under an unfaithful statement | Value-exact scoring in the fidelity instrument; items under your authority are shown with the quote they rest on when asked |
| Privacy regresses in the rebuild (the pause switch, redaction) | Hard gates with seeded leaks; a differential test against v1's redaction on 150,000 inputs; the baseline as an oracle |
| Evaluation costs more than it returns, or variance hides the effect | Four instruments only; a pilot prices a session before any ceiling is set; gates are on steps, tokens and corrections, which can be measured, not on small solve-rate differences |
| One real install, so the migration cannot be rehearsed widely | Property tests over generated wikis; the first real run is watched; the old clone is the way back |
| Stage 0 asks a lot of you: two reports, a sitting per host, a point release and a week, leave for live runs, 33 decisions, and cutting each release | Stated here so it can be planned. Four Stage 1 packages wait on it: LG-13 on the host-facts sitting, DV-11 on the live gates, EV-17 on the pilot and HO-13 on the readout. ST-01 starts from the fallback of decision 3 and is revised at the readout; DV-13 runs on its interim default until DV-03 names one. The rest of Stage 1 does not wait |
| Memory is an injection path | Items carry whose words they are and where in the turn outside text entered; nothing written after it is promoted without your word; held and unverified items are never pushed, and a disputed one only when it is under your authority or protected, with the disagreement said (section 6.4, design 6) |
| The hosts' own memories improve | Tested against in every experiment. What stays eva.md's: one memory across both agents, work state per workstream, checks against the code, receipts, and files you own |
| Agents building in parallel collide or drift from the design | Paths per package checked in CI, contracts first, v1 frozen, one writer for shared files |

## 12. Later, each with its trigger

None of these is in v2.0. Each has a design in the working papers and opens when its trigger fires.

| Item | Opens when | Rough cost | Needs from you |
| --- | --- | --- | --- |
| Sync between your machines (fetch, merge by field, enrolled writers) | You have a second machine, and single-machine v2.0 has run for a month | L, plus a multi-machine test suite | Decision 21. Signing then becomes a security boundary: commits from a machine not enrolled are merged but held from delivery, and one ask covers the history migrated from v1. The engine merges every file both sides changed with its own rules, never git's text merge |
| A held spool and a session-keyed layout for the record | The counted week shows 2% or more of events held back, or under 95% of tool calls captured | L | |
| Text between tool calls kept in the record | An evaluation arm shows organizing does better with it | S, plus an expiry rule | A change to "kept with no time limit" for that text |
| A richer lifecycle: candidates that must recur, expiry, archive by disuse | The corpus counters show noise, duplicates or stale items above their thresholds | M to L | |
| Lessons from failures, and preferences, as kinds | The hidden-decision tasks show a gain once failure digests exist | M | |
| A reviewing model pass over the store | Near-duplicates and disputes exist that code cannot settle | L | A share of the daily budget |
| A voice call's lines read from the host's own record | HF-00 shows the desktop app's session file holds a line for each utterance and is one eva-md can read, and the second report shows a fifth or more of a call's lines never reach a hand-over | M, inside LG-21 | Decision 30 |
| A setting for each stage's lag budget | You find a bar of section 5.4 too loose or too tight in use | S | |
| The agent's reading of code kept as items | The investigation tasks show a gap that the handoff's findings do not close, and anchors can name another repository | M to L | Decision 31 |
| Which version of a library an application really uses, as against the library's newest branch | A continuation task or real use shows advice drawn from the wrong version | S to M | |
| Recall when the agent edits a path or runs a command | The continuation tasks show a gap the brief does not close, and one hook start is cheap enough on that host | M | One more Codex hook review |
| Lexical matching on prompts | The held-out retrieval set shows precision of 0.95 with at most 1% false additions | M | |
| Embeddings | Recall on paraphrases stays under 0.7 after the tokenizer, cues and keys, and a static model run offline lifts it 10 points without hurting exact matches | M | "No local models"; a model file to ship; never in a hook |
| Installing releases in the background, with probation and rollback | There are users whose fixes are waiting for someone to run a command | L | "No auto-update" |
| Tools that let an agent write to memory | Real use shows people asking the agent to remember or forget mid-chat and the typed path missing it | M | "The MCP server only reads". Settled in advance: the server never applies a write itself. A write is queued and applied only when its tool event is released for a chat that is not paused, it never carries your authority, and it is not offered on a host with no hooks |
| Asking the host to confirm a tool call that breaks a rule you armed | A rule-compliance measurement shows rules are seen and not obeyed | M | "A hook never returns a decision". Claude Code only: Codex has no way for a hook to ask |
| Generated rules files or reviewed skills in the host's own folders | An experiment shows they beat delivery through hooks | S to M | "Never write host settings" |
| A floor for hosts with MCP and no hooks, which reaches the Claude desktop app's Chat tab | You want memory there | S to M | Decision 3 of the v1 decisions (hosts in scope) |
| Cloud sessions | Sync exists, and six host facts hold | M | |
| A kit for managed machines | The field report shows managed settings restricting hooks or plugins | S | |
| Repair at rest: stored text scanned again when the rules improve, and a miss replaced in place | The redaction corpus shows a rule change that would catch text already stored | M | Decision 8 |
| Forgetting by value; expiry of working detail | You ask to forget a value everywhere, or the record's size becomes a cost you notice | S to M | A change to "kept with no time limit" |
| Secret handles; custom redaction patterns | The corpus shows repeated secrets that masking makes ambiguous, or you need a pattern of your own | S each | |
| A checkout of rendered pages for reading on the git host | You answer decision 22 with "committed" after v2.0 | S | Decision 22 |
| An operating-system notification as a last resort | A silent stop goes unseen in real use although a chat-start line was shown | S | Seeing one before agreeing |
| System pages rebuilt as views over items | A measurement with them on shows a gain (decision 12) | L | |
| Systems beyond macOS and Linux | A user asks, and both hosts' hooks are confirmed there | M | "macOS only" |
| Replay of old chats under the new contract; reading chats from before the install | You ask, after seeing a token estimate | M | "No history backfill" |
| Short excerpts committed beside items | Sync, since evidence must then travel | S | "No captured text is ever committed" |
| Removing something from pushed history | A secret is found in pushed memory | M | The one exception to "never force" |
| An API key as a second way to run the host's CLI | A host stops allowing headless runs on a plan login | S | |
| Claude Code mods | The host declares the API stable | | |
| Team memory | Not designed. Nothing here is built for it | | |
| The claim run, and the word "best" | Every v2.0 gate is green on both hosts | M, plus quota | What it must beat (the recommendation: memory off and the host's own memory) |

## 13. The first field report: what it showed and what it changed

The [field report prompt](field-report-prompt.md) was run once on the real install, on 2026-11-10, by the agent the owner uses there. It returned counts, durations, statuses and general judgements, and nothing private. The report itself is not in this repository. This section carries what the plan needs from it, and the lead's reading of it.

It covers about six days of use of release 2026.11.5, on one machine, almost all of it in Codex. It is one sample. Where a figure below rests on a handful of cases, the text says so.

### 13.1 What the install looks like

| Area | Measured |
| --- | --- |
| Releases | eva-md 2026.11.5, Codex 0.167.0, Claude Code 2.1.319. Two later eva-md releases exist, and v1 does not check for one |
| Which host is used | Codex: 589 of 600 captured sessions. The chat that wrote the report ran in its desktop app; the report does not count by app. Claude Code: 11 sessions; every heartbeat it left came from a run a program started, the last on 6 November, and all 6 of its update records were skipped for that reason |
| The inbox | 600 sessions, 8,417 entries, 4.4 MB: 7,085 tool rows, 502 prompts, 142 replies, 688 events. 3,129 entries (37%) carry a subagent's id |
| What a session is | 581 sessions have an end event, and only 47 start events were recorded. 568 sessions are outside any repository, in the one shared area, which holds 20 different folders. Of 580 recovery records, 545 say no transcript was available |
| Gaps the record admits | 12: four final replies missing, eight transcript rows too large. In the largest session the host's own file has 235 user rows and 72 final replies, against 198 and 61 in the inbox, and the inbox holds 30 exact repeats. The two files are not like for like, so this is a lead and not a coverage figure |
| The background update | 61 calls in four active days, all on Codex, each asking for the chat's own model at the highest effort. 57 succeeded, two were stopped at the 600 s limit, two failed on a host error, and none was rejected as an invalid answer. Median 124 s, 95th percentile 248 s |
| From a chat's boundary to memory on file | Half within 158 s. One in twenty later than 88 minutes. The longest 64 hours |
| What it cost | 2.52 million tokens: about 41,000 a call, 13 to 18 calls a day against a cap of 40, and about 120,000 or more for each session it worked on (21 at most) |
| What the checker did | On thread pages: 40 operations refused, 34 tags downgraded, 20 bad references removed. Rules: 9 added, 1 edited, 3 refused. Knowledge: 35 added, 1 superseded, 6 refused, 7 paths dropped |
| The wiki | 54 pages: 39 knowledge pages (15 facts, 10 decisions, 9 constraints, 5 procedures; 24 on the user's words and 15 observed; 2 cite a code path), 4 thread pages, 1 rules page, 4 system pages and 6 pages of other kinds. A statement is 274 characters at the median, and the largest pages are 38 KB. The validator reports no error |
| The brief | It arrived, matched the folder and branch, and was labelled as reference. Built by command it took 0.92 s and came to 4.8 KB. Its saved state was older than the exchanges appended beneath it, and it said so |
| Search | Four questions with a known answer: three found at rank 1, and one of the two paraphrases missed. Two questions with no answer came back empty, correctly. By command 62 to 287 ms; by MCP tool 1.4 to 2.1 s for the same results |
| The read tools | Present, and working with no permission question. Before this chat they were used in 8 sessions: 76 calls by command and 5 by MCP tool |
| Health | `status`: 34 PASS, 0 FAIL, 13 UNKNOWN. The doctor: 37, 0 and 14. Every UNKNOWN row but one is about Claude Code. A heartbeat records a time and no duration |
| Commit and push | Three commits made and pushed by the daily job, all SSH-signed, with no failure recorded. 14 files were uncommitted when the report was made |
| The hosts' own memory | Codex's is switched on and holds several hundred files. Claude Code's folder for the project is empty |
| Redaction | Six markers in the inbox, none in the wiki |

What the agent there judged, in general terms. Worth keeping: the labelling of the brief as reference with its sources; state, open questions and dated pointers tied to the branch; the command line and the MCP tools agreeing on results; the validator, the update's reason codes and the checker's counters, which made diagnosis possible without contacting anything; previews that are bounded and uncertainty that is stated. Its ten shortfalls, in its order: a result labelled as the user's can hold other speakers' words; recovery often has no transcript to read; a paraphrase can miss a known answer; freshness is split between the saved state and the newer exchanges; health surfaces can seem to disagree; the rates that matter have no denominators; the same evidence repeats and fills a result; long dated histories bury the current statement; a run records the model asked for and not the model used; timing and backlog are not recorded well enough to reason about.

### 13.2 The readings against section 8's thresholds

| Measurement | Reading | What follows |
| --- | --- | --- |
| Share of passes whose whole answer is refused | 0 of 61 calls rejected as an invalid answer. 4 of 61 (7%) failed for other reasons | Neither threshold is met yet. None of the 61 answers was refused whole, so the new contract does not have to be built first at full size. The threshold that would shrink it needs two things: fewer than 10% of answers refused whole, which holds, and at least 0.9 of planted facts stored correctly as the new scorer counts them, which cannot be known until that scorer exists. So the contract's place in Stage 2 is set once EV-14 has scored the output DV-02 records from v1 |
| Rule and knowledge adds the checker refused | 9 refused against 44 added: 17% (rules 3 of 12, knowledge 6 of 41) | Between the two thresholds, and close to the lower one. Spans are still built. The repair round stays a bet for DV-03 to settle |
| Chat starts whose state was stale or pending | v1 does not record it. The one chat observed had a stale state | Open until the second report |
| Seconds and tokens per pass at the highest effort | Median 124 s over every record, skipped ones included, so the median of the calls that ran is higher. Two calls stopped at the limit | Crossed on both counts. The small-model default is no longer optional (decision 5) |
| Events held back; turn and tool coverage | v1 does not record them. Leads only: 12 admitted gaps, one session with 84% of the host's user rows, 545 of 580 recoveries with no transcript | Open until the second report |
| One hook start on Codex, 95th percentile | Not recorded. The binary alone starts in 7 ms at the median | Open. HF-00's timed hook runs give it: the run time that 2026.11.7 records, and that the counters release keeps for a week, leaves out the process's start |
| Knowledge pages in the real wiki | 39 | Between "a handful" and "hundreds". The migration is small, so its property test need not come first. Recall has a real index, of a size worth scoring |
| Daily commit outcomes; signing prompts | No failure in five ledger records. Whether a prompt appears is the owner's to say (question 3 below) | Open on that answer |
| Codex reviews needed after upgrades | None recorded. The install has not been upgraded | Open. The counters release is the first upgrade and changes two skill files, so it is also the first measurement |
| The MCP server and skills | Connected and working. Used without being asked in at most 6 sessions | Not crossed, read strictly. The plan's stance stands on the outside evidence as before: nothing in v2.0 depends on the agent choosing to call a tool |
| The live resume test; typed refusals taken for assent; the pilot | Not run | Open (DV-02, DV-03, EV-03) |

### 13.3 What the report changed

1. **Decision 5 is needed before code, and a lower default ships first.** A pass at the highest effort is slow and costly on the real install, and two were cut off. DV-13's interim default, the chat's model at a low effort, becomes the first behaviour change released once the counted week has been read. v1 already has a setting for this (`effort`, per host), so the owner can lower it today with no new release. The lead's advice is to wait for DV-02, which compares the two efforts on recorded scripts, unless the allowance is a problem now: a blind change could write poorer pages into the wiki that v2 will migrate.
2. **A session gets a class.** At most 47 of the 600 sessions have a recorded start, and the report observed that many held very little, so "600 sessions" says nothing about how many chats were had. The record gives every session a class from positive evidence (LG-10), every counter is reported by class, and the bars of section 5.4 are read over chats a person had. Which of Codex's sessions these are is not known; section 13.5 has the candidates.
3. **The counters release counts what the report could not.** Its own closing wish was a content-free health export with event durations, delivery counters and backlog ages. That is this package, and the items it listed as unavailable are now named in the package's row in section 8.
4. **Most chats are remembered quickly, a few wait very long, and the long waits are the problem.** In an ordinary case nearly all of the wait is the model call itself: half of chats are on file within 158 s, and the call's own median is 124 s. One in twenty waits over 88 minutes. The worker that loses no wake (DV-12) and the clock (HO-22) are there to shorten the long waits; the small model is there to shorten the ordinary one. Health measures lag from the oldest waiting event, because the report found a file's date understating it by a day (HO-12).
5. **The brief is over v1's own time target at real size.** 0.92 s by command against 300 ms. The hook's own time is unrecorded, so this is a lead, but the benchmark has no scenario of this shape and gets one before any hook changes (EV-16). If the hook is as slow, the compiled pack (DL-08) is owed for speed as well as for content.
6. **Who said it comes first in the record track.** The agent's first shortfall was a result labelled as the user's that held other speakers' words, and 37% of entries carry a subagent's id. Author by allowlist (LG-13) is the first record package built once HF-00's fixtures exist, and chat search shows one speaker under one name (DL-13). Section 14 found the larger cause: a voice hand-over is stored whole as the user's prompt, with the front model's lines inside it.
7. **Health for a host nobody uses.** Thirteen rows about Claude Code say UNKNOWN, and nothing clears them short of chatting in Claude Code. HO-12's gate now covers it.
8. **Results that repeat.** In the search tests, repeats of one answer filled most of a result. The read tools give one place to one statement (DL-12), and a second copy of an event is told by the key LG-10's spec states, not by its id, and is dropped when the worker reconciles the two sources (LG-21).
9. **Rules that are not rules.** The agent read several saved rules as instructions for a moment. The fidelity scorer plants such instructions and counts each one saved as a rule against the contract (EV-14).
10. **Codex first where a package must choose.** Nearly all real use is in Codex, and no Claude Code session captured on the real install is known to be a person's chat. Claude Code is not dropped: for now HF-00's sitting is the only evidence the plan has for it.
11. **Sizes.** The migration is small: 54 pages. Checking a statement's cited code before it is shown (DL-18, decision 11) covers 2 of 39 statements today, so it stays small and stays in Stage 3. Decision 20 gains a count: 20 folders now share one project.
12. **Decision 24 is no longer hypothetical.** Codex's own memory is on beside eva-md on the real install, so the continuation instrument's fourth condition is the one that matters there.

What did not change: the cut line, the five stages, the four instruments, and the order of the critical path.

### 13.4 Where the report disagrees with the audit, and where it misread

**Against the audit.** The audit inferred that v1's checker would often reject a real model's whole answer. It rejected none of 61. It does trim: about one proposed rule or knowledge statement in six is refused, and 34 tags were downgraded. Whether what it refused was right, and whether what it kept was true, nobody has measured. So problem 4 of section 3.2 stands as a design fault (two contracts, and authorship decided by word overlap) and is not yet shown as a loss in use. The audit also held that redaction destroys facts. With six markers in a week, and none in the wiki, the field shows no sign of that at this scale. The redaction packages stay for their other reasons: the oracle is what lets a rule change safely, and two engines run in every hook.

**Misreadings, checked against the code.** Three things the report noted are not faults:

- A file named `push-retry` exists although the last push succeeded. Every check sets that file's date, due or not. It is a timer.
- `needs-trust.json` still says true although the hooks are trusted. That is by design. Install writes the file once and nothing changes it afterwards. Health does not go by its value: it treats the hooks as waiting for trust only until a Codex chat-start hook has run after the file was written.
- `eva-md brief --as resume`, `compact` and `fork` exited 2. Those kinds need `--session`, which the prompt left out. The second prompt passes it.

The first two are still findings about legibility. An agent diagnosing a healthy install tripped on both, which is why HO-12 now says that no stored file may read as a fault when health is good.

### 13.5 Leads for the host sitting and the second report

- **Sessions with an end and no start.** Read in Codex's source since this section was first written: a thread that is opened and never run fires an end hook with no start, and so does a thread that only had a voice call in which nothing was handed over. A desktop app that loads threads in order to show them would produce exactly this. Subagents are ruled out, because they report under their parent's session. HF-00 confirms it on the real host. A check that reads no content: whether such a session's file holds any turn.
- **No transcript in 545 of 580 recoveries.** Most of these are probably the same sessions. If a person's chats are among them, recovery is not the safety net v1 takes it for, and moving it to the worker (LG-21) does not help until the adapter knows where that host keeps the file.
- **Long chats may lose the most.** One session holds about half the inbox's bytes. In the one long session sampled, the host's file has more user rows and replies than the inbox. Long chats are where memory earns its keep, so the counters report coverage by session size.
- **Two days with git activity in the project and no captured session**, out of six. Either nobody chatted on those days or a chat was missed.
- **What runs Claude Code without a person there.** Two of those runs made about a hundred tool calls each, and the update skipped them all, so whatever they found reached memory only if a later chat repeated it (decisions 6 and 14).
- **Search by MCP tool costs over a second more than by command**, for the same result. HF-00 finds where the time goes.

### 13.6 What only the owner can answer

The report ends with nine questions for you. The tenth and eleventh are the lead's. A line each is enough.

1. Since installing, which commands and which signing, SSH, hook-review or tool-permission approvals have you done by hand, and about how often?
2. Which notices has each host shown you, and did any stay on or seem wrong?
3. Does the daily commit or push ask you to sign or sign in? Is the git server reachable only on a private network, limited to certain pushers, or set to require signed commits?
4. Have you seen recall help, an obvious miss, stale advice, a decision reopened for no reason, or recalled material that was beside the point?
5. Has anything been kept that should not have been?
6. Has a second machine used this wiki, and do you read or edit its pages on the git host?
7. Are branches usually merged by squash, rebase or merge commit?
8. Has `eva-md doctor --live` or a live accuracy run been used, and what did it report?
9. How was each upgrade done, and what approvals, interruptions or breakages came with it?
10. What starts Claude Code without a person on that machine, and do you also chat with it there yourself?
11. Did you ask the assistant to write or correct memory pages by hand during the work, and do you want to be able to?

## 14. The owner's note of 6 November: what holds, and what it adds

Four days before the field report, an agent on the real install wrote a note for the owner: what it had seen of eva-md in use, how it thought each stage of memory should be timed, eight proposals and eighteen acceptance tests. It was written without the source and says so. The note is private and is not in this repository.

Every point in it, 44 in all, was checked against v1's code and against this plan by five readers, each followed by a second reader told to prove the first wrong. A researcher read Codex's source at release 0.167.0 for how its voice mode hands work to the agent. Nothing was run on a real host, so what is said here about Codex is a lead until the host sitting confirms it. The detail, point by point with file and line, is in [working-papers/field/note-evaluation.md](working-papers/field/note-evaluation.md).

### 14.1 The verdict

| Verdict | Points |
| --- | --- |
| A defect in v1 | 10 |
| Something v1 has no form of | 8 |
| Already works in v1 | 8 |
| The designed delay, not a fault | 5 |
| Behaves as designed, and nothing tells the owner | 6 |
| Probably right, and nothing measures it | 5 |
| Cannot be settled from the code, or lies with the agent or the host | 2 |

The eight proposals, and what this plan now does about each:

| Proposal | Verdict | What the plan does |
| --- | --- | --- |
| A. When new evidence changes the picture of a system, find what it affects and bring it up to date | Valid, and the plan as it stood did not meet it. v1 never touches a system page once written. The plan froze those pages and built nothing in their place | The overview, rendered by code from the items (DL-26). A statement whose code has moved is shown to the next pass that touches that code (DL-18). A disagreement is a stored record that can be listed (DV-23, DL-12) |
| B. Show each stage, what waits at it and why | Valid. v1 shows a count of waiting chats, read from only the 64 newest files of a folder, with no age and no reason | The worker gives a reason for work that waits (DV-12, DV-22). Status shows each stage's lag beside its bar (HO-12, HO-23). A long chat that does not end has a bar of its own (section 5.4) |
| C. Capture all of a voice call | Possible only in part. Talk that stays with the front model reaches no hook; eva-md sees it only inside the next hand-over, within the host's limit | Decision 30: keep what the hand-overs carry and say so. The rest waits for two host facts (section 12) |
| D. Inside a voice hand-over, keep each speaker and each correction straight | Valid, and the most serious finding. v1 stores the whole wrapper as the user's typed prompt, so a front model's sentence can become the user's decision or a standing rule | Speech is its own author and the wrapper is decoded line by line (LG-13, LG-10). Decision 29 says what a spoken line may do |
| E. Use what is already known before asking again | Partly eva-md's to fix. v1 cannot match a name on one word, and nothing tells whether a fact was given and ignored | Name events in the retrieval set, and names as exact keys if the number asks for it (DL-04, DL-06). "Asked again" is counted (EV-17, DL-20) |
| F. Keep the relations between systems in several repositories | New. An item belongs to one project, and what the agent learns by reading code is a note and not an item | Decision 31: the findings travel in the next chat's handoff and are listed in the overview. Anchors that name the right repository, if the host's records allow it (DV-23). Decision 13's review of which projects share memory |
| G. Keep uncertainty and replaced statements as what they are | Mostly met by v2's kinds and authority. A limit stated in the evidence can still be dropped on the way | A planted family in the fidelity scorer (EV-14). Disagreements are stored in v2.0 (ST-01) |
| H. Test privacy at every way in | Valid as a gap in tests. Each gate is tested alone and none end to end | The privacy matrix (EV-20). Decision 32 says what redaction is for |

### 14.2 What the note shows about how eva.md is really used

The audit, the research and the designs assumed a person typing in a coding agent, in one repository, on a branch. The note, with the field report, shows something else on the one real install:

1. **Voice.** Part of the work is done by talking to a front model that hands tasks to the agent. The agent's prompt is then a wrapper the host writes, holding the front model's request and the lines both sides spoke.
2. **Outside git.** 568 of 600 sessions sit in folders that are not repositories. How many of them are empty, and how much real work is among them, are things the counted week settles.
3. **Across repositories.** The work is investigation that follows how the parts of several systems connect. The findings are about several repositories at once, and are expensive to work out again.
4. **Through subagents.** 37% of entries carry a subagent's id.
5. **Beside the host's own memory**, which is switched on.

So the plan is checked against that use, and section 14.4 lists where it would have done worse than v1.

### 14.3 What v1 does wrong that the audit had missed or understated

- **A front model's words are stored as the user's.** On Codex a voice hand-over arrives as an ordinary prompt with no field that says where it came from. v1 stores the whole wrapper as the user's typed prompt, shows it to the organizing model as the person's own words, and lets the checker credit any sentence in it to the user. A suggestion the front model made can become a stated decision, lasting knowledge or a standing rule. Outside a repository such a rule is stored for every project.
- **The pause switch by voice is a matter of chance.** A line inside the wrapper that is only the pause phrase pauses, and one that is only the resume phrase resumes, whoever wrote it. The phrase as the person's own spoken line, which the host prefixes with the speaker, does nothing, so what follows is stored.
- **A second prompt in one turn can be lost.** Recovery treats a second user row under one turn id as a copy of the first and drops it, so such a prompt survives only through its own hook. A hand-over or a message sent while the agent is working may be exactly that: Codex's source steers it into the running turn, and whether its row carries the same turn id is for the host sitting to confirm. It may be why the largest session holds fewer prompts than the host's file.
- **The count of waiting chats is a floor.** It reads the 64 newest files of each folder and does not say so. The real install has 568 files in one folder.
- **A conflict with a protected page disappears.** The protection holds, and the only trace of the conflict is a counter in a log.

### 14.4 Where v2 as first planned would have done worse than v1

| What | Why | What changed |
| --- | --- | --- |
| On Codex the design could not tell a typed prompt from any other | "Typed" needed positive evidence, the design's evidence was a field only Claude Code has, and the voice wrapper was on no list. Either every Codex prompt was unknown, so nothing could be stored under the user's authority and a chat led by voice was skipped; or the wrapper was filed as typed again, as in v1 | The evidence is named for each host, HF-00 must find it on Codex, and a threshold in section 8 sets the fallback. Speech is an author, and a range with a spoken prompt has a user turn (LG-13, LG-21, DV-24) |
| A person's name would have brought nothing | v2.0 routes on paths, symbols and commands, and drops matching by loose word overlap | Name events are scored first; a name becomes an exact key if v1 does better on them (DL-04, DL-06) |
| What was learned in a working folder would not have reached chats in the repositories it is about | Each folder outside git becomes its own project, and reading across projects is closed | The owner is told at decisions 13 and 20; the one-time review is where it is set |
| What an investigation found would have reached a later chat only if the agent went looking | Findings from reading code were to be notes in a session's log, in no brief | The last session's findings ride in the next chat's handoff, and the overview lists them (decision 31, DL-10, DL-26) |
| A finding made in a working folder could not correct what a repository's project holds | A pass changes only its own project's items, in v1 and in the v2 design, and decision 20 makes 20 projects of one | With the user's own words a chat can change a shared project's item; without them the change is kept as a disagreement beside it (decision 13, DV-23) |
| A captured line could be found at once in v1, and in v2 only after the worker has checked it | The pause guarantee moved from every hook to one place | The age of the oldest unchecked event is reported against a bar of ten minutes (LG-21, HO-23). v1's side of the trade is in part a permanent loss: while its in-hook recovery is behind it keeps only an event's name; prompts and replies come back from the host's record on a later pass where one can be read, and tool lines and host summaries never do |

### 14.5 What the note asked that already works, or is by design

- **No index run is needed before a search.** Search scans whatever the index does not cover, and a test holds it. The health row says "covers 88%" without saying the rest is searched, which is why an agent ran the command. Status now says so (HO-12).
- **A queue that is not empty is not a fault.** v1 organizes a chat at its end, at a compaction, when 60 KB is waiting at a turn's end, at a prompt after a 30-minute pause, and at the next chat start in its project. Nothing else starts it, and the release on the real install has no daily sweep. That is the designed delay, and also the tail the field report measured.
- **A commit does not mean every chat was organized.** v1 commits once a day, when the first hook of the day fires, and that commit holds only the pages that had been written by then. A chat organized later in the day waits for the next day's commit. One commit for each pass, naming what it covers, replaces it (ST-05).
- **Stale code is detected and then only labelled.** That is v1's design. The plan now carries detection through to a corrected statement or a recorded disagreement (DL-18).
- **When the day's budget is spent, waiting work is safe and still shown.** What was missing is the reason beside it.

### 14.6 What changed in this plan

- **Decisions.** A review of this plan added decision 33, on committing the user's own words. The note's check added four for the owner, 29 to 32: what a spoken line may do, how much of a voice call is kept, what becomes of the agent's own findings, and what redaction is for. Decision 12 is now only about freezing system pages; the overview is built either way. Decisions 13, 17 and 20 say what they mean on the real install.
- **One new package.** DL-26, the overview, in Stage 3. It is the rendered view decision 22 already promised and no row had built. DV-35 grew from S to M, because system pages must move off the page library before it is removed.
- **Stage 0.** The host sitting records a voice call, a program-driven session and what marks a typed prompt on Codex. The counters release counts hand-overs, triggers, repeats and turns with no reply, and its week is read as 2026.11.7's behaviour, not 2026.11.5's. Three thresholds were added before anything was measured.
- **Gates.** 33 package rows gained a sentence. Stage 0: the counters release, HF-00 and DV-03. Stage 1: LG-10, LG-13, EV-14, DL-04, EV-17, DV-12, HO-12, ST-01. Stage 2: LG-21, LG-23, LG-24, ST-05, DV-20 to DV-24, DL-06, DL-07, DL-10, DL-12, DL-13, HO-21, HO-23, EV-20. Stage 3: DL-16, DL-18, DV-32, DV-35, DL-20.
- **The design.** Section 6.1 has speech as an author and a pause that can be spoken. Section 6.3 gives waiting work a reason and drops any gate on the host being idle. Section 6.4 keeps a disputed statement of the user's in view and adds the overview. Section 6.5 shows lag beside a bar.

What the note asked for and the plan still leaves out, each on purpose: a model pass that rewrites pages to reconcile them (the evidence is against it, and the overview needs none); a graph of relations (a relation is an item that names two projects); a check before the agent asks a question (it would need recall at tool time, which waits in section 12); latency expectations that the owner sets (the bars of section 5.4 are fixed in v2.0, and a setting for each stage is a new row in section 12); and the version of a library that an application really uses (a new row in section 12).

## 15. From here: the order of work

This is where the team starts. It adds no design. It puts sections 7, 8 and 9 in the order things happen, and says who moves first.

### 15.1 What starts the build

Three things from the owner, in one message:

1. **Agreement to this plan**, as "agree" or "agree, except these", on the decisions of section 7 that are needed before code: 1, 2, 5, 6, 7, 15, 16, 17, 20, 22, 26, 27, 28 and 33. Each has a recommendation, and a fallback the build can start on.
2. **The eleven one-line answers** of section 13.6.
3. **Leave to build** (decision 27): agents commit, push package branches and open pull requests; the lead merges what is green; the owner merges the protected files and cuts every release.

Until then the lead writes cards and run-books, which are documents, and nothing else moves.

The go came on 2026-11-10, wider than asked: work on `main`, no pull requests, no approvals, every recommendation taken as agreed. The eleven answers are still welcome and block nothing.

### 15.2 The first wave

Everything in this table starts on the day the owner sends the message of section 15.1, which the table calls the go, except the counters release, which starts when SEAM-0 and EV-16 have merged. Nothing in it waits for a measurement.

| Work | Who | Needs | What it unblocks |
| --- | --- | --- | --- |
| One change to `AGENTS.md`: the answers to decisions 15, 16, 27 and 28 written into its rules (who may commit, push and merge during the build; who may make live runs, and where), and this plan added to "Read first" | The lead writes it; the owner merges it | The go | Every builder and reviewer. Until it is merged, `AGENTS.md` still tells each agent not to commit or push unless asked |
| A card for every Stage 0 and Stage 1 package (section 9) | Lead | The go | Every builder |
| SEAM-0: seams and guards | One builder, one reviewer | Decisions 15 and 27; a second merge of `AGENTS.md` by the owner, for its layout line; EV-16's comparison step | Every package that adds a command, a hook handler, a status row, a config key or a benchmark scenario; and IF-01, the fakes and the contract matrix |
| EV-16: the latency gate and a macOS baseline, with the scenario shaped like the real install | One builder | The owner's merge of the gate and its baseline; SEAM-0 for the new scenario | The counters release; every later change to a hook |
| HF-00, first half: the one fact sheet and the run-book for the host sitting, voice call included | Lead | Nothing | The owner's sitting; then LG-13, HO-20 |
| The counters release (LG-01, LG-02, DV-01, DL-02, HO-02), and the second report's prompt | One builder, two reviewers | SEAM-0 and EV-16 merged | The counted week |
| Run-books for the live gates (DV-02, EV-01), the plan for the contract spike (DV-03) and the ten pilot tasks (EV-03) | Lead, one builder for the throwaway prototype | Decisions 16 and 28 | The owner's live runs; then DV-11, DV-21, EV-17 |
| Stage 1 packages that wait on nothing in Stage 0: EV-10, EV-12, DL-04, LG-10, LG-11, DV-12, HO-10, HO-14, ST-01. DV-12 builds its own package at once and adds its command, its wake in the hook and its status reason through SEAM-0's tables once SEAM-0 has merged | One builder each, at most six at once | Their decisions (15 for EV-10; 1, 2 and 22 for ST-01) | EV-11, EV-14, LG-12, DV-13, HO-11, HO-12, ST-02, ST-03, ST-04 |

Four Stage 1 packages wait on Stage 0: LG-13 on the host sitting, DV-11 on the live gates, EV-17 on the pilot, and HO-13, which starts after the readout.

### 15.3 The owner's calendar

In order. Only the week of ordinary use takes long.

1. **Day 0.** The message of section 15.1. Done on 2026-11-10; the lead has since made the change to `AGENTS.md`. If you take decision 28's recommendation, make the two keys.
2. **While the first wave builds.** Nothing: under the owner's leave the lead merges what decision 27 would have kept for the owner.
3. **When the counters release is ready.** Cut it. Upgrade the real install with one command. Give Codex its one hook review. The counted week starts.
4. **During that week, Part 1 of the [host sittings' run-book](host-sitting-runbook.md)**: three short sittings and a hand-over, about 180 minutes of steps (about 220 with a margin). Make the evaluation home; create one scratch repository on the git server, push to it from the evaluation home, which is the check decision 3 waits for, and delete it at once, then the Claude Code facts; then the Codex facts, with one voice call in its desktop app; then hand the recording over. Part 2 of the run-book is asked for later, group by group. Also the live gates on v1 (84 calls), and leave, with a ceiling, for the contract spike and the pilot.
5. **At the end of the week.** Run the second report's prompt on the real install and bring the report back.
6. **The Stage 0 readout.** The lead brings the revised plan with every threshold of section 8 read. The owner agrees it and answers the decisions that waited for it: 3, 4, 8 to 14, 18, 19, 21, 23, 24, 25 and 29 to 32.

No release that changes behaviour beyond 2026.11.7's reaches the real install before step 6.

### 15.4 What exists when Stage 0 ends

- A number, by class of session, for every row of the table in section 8, or a stated reason why it cannot be had.
- Recorded fixtures of what both hosts really send, a voice hand-off among them, with nothing private in them.
- v1's row on the latency gate, and v1's real output from both hosts at two efforts, recorded for the fidelity scorer.
- A default model and effort for organizing, chosen from a measured frontier.
- A price for one evaluation session, and from it the ceiling of decision 16.
- This plan, revised once more, with Stage 2 sized.

### 15.5 After that

Stage 1's switches are turned on one release at a time, each after its own gate. Stage 2 builds behind the mode key. The shadow week, the cut-over, the fourteen days in which it can be abandoned, and the week of proof follow, as section 8 sets out. The lead reports at each gate what was measured, what it changed and what is asked of the owner next.
