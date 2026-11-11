# Host facts: the one sheet

eva.md v2 rests on things only the hosts can tell: what a hook payload holds, when a transcript row is on disk, how a voice call reaches a hook, what a plugin change costs in reviews. This sheet lists each such fact once, with a number, what settles it, and what the design does if it is wrong.

Who uses it: the owner needs only [the run-book](host-sitting-runbook.md), which is written from this sheet. The lead fills in each fact's Result after the sittings. A builder reads a fact and its fallback before relying on it. The card [HF-00](cards/HF-00.md) describes the recorder the run-book needs.

## How to read it

- **Where the facts come from.** Each of the six design papers lists the host facts it assumes: ledger section 13, deliver section 12, hosts section 14, store section 16, derive section 11 and evaluate section 14, all in [working-papers/design/](working-papers/design/). This sheet merges them, with the facts the papers assume without listing them and the ones [the plan](plan.md) added (Stage 0, "What the host sitting must settle", and the HF-00 row). It is the first half of the plan's HF-00 row.
- **One fact, one number.** A fact two papers need is one fact that names both. A fact is a sentence that a recording or a reading can confirm or refute. Numbers run from F-001 and never change; a fact found wrong is marked refuted, not removed.
- **Grouped by host, then by what is looked at.** First the facts that hold for both hosts, then Claude Code's, then Codex's, then the git server, the operating system and the model. Last come the facts no Stage 0 route can settle.
- **Needed by** names the papers, the label each paper gives the fact (a row of its host-facts section, or "unlisted" with the section where it is assumed, or "plan item"), and the packages, decisions and gates that lean on it. "Plan decision N" is row N of the plan's section 7.
- **Settled by** is exactly one route:
  - **Sitting 0**, **the Claude Code sitting**, **the Codex sitting**, or **both host sittings** (each host's half in its own): the owner, in the evaluation home, following the run-book. These are the sittings of the run-book's first version, in which sitting 0 set up the evaluation home and settled the machine's and the git server's facts. Since the run-book was cut to a core path, each such fact also has a mark (next item).
  - **DV-02**: the live gates on v1, whose recorded headless runs answer facts about `claude -p` and `codex exec`.
  - **DV-03**: the contract spike, for facts only its many calls can measure.
  - **EV-03**: the pilot, for facts about running agent sessions in containers and through a pseudo-terminal.
  - **Published source**: a host's, or the tool's, published documents or source code, read at the version the sittings record.
  - **None in Stage 0**: the facts of the plan's section 12 items, which wait for their trigger.
- **Mark**, on each of the 141 facts settled by a sitting, says whether the owner is asked for it now. It follows the lead's rule of 2026-11-11.
  - **Core**: a package the plan builds in Stage 0 or Stage 1, or a decision the Stage 0 readout takes, would be built or decided differently on it; building on the fallback and confirming later would cost more than about a day of rework, or put a privacy guarantee at risk (text kept under the wrong author, text said off the record kept, a pause missed, a person's words given to a model's wording); and nothing cheaper settles it. The run-book's Part 1 is there for these. The voice facts of decisions 29 and 30 are core where they decide how a spoken line is stored and under whose authority.
  - **Later**: it stays here with its steps, which the run-book's Part 2 runs, group by group, when the package that needs it is about to be built. The mark names the group.
  - **By the field report**: the second field report can settle it on the real install, read-only, from the shapes of eva-md's and the hosts' records (which row types exist, which fields a row has, counts, orders and gaps in time) and the owner's one-line answers, never from contents, names or paths. A fact that needs a scripted action (a fork made on purpose, a spoken phrase, a plugin changed) is never marked so.
  - **From source**: a host's or a tool's published source or documents settle it. A Claude Code behaviour that the plan's section 4.2 took from the documents, and has confirmed on a host before anything is built on it, is marked later instead.
  - A fact that is not core but that Part 1's steps record anyway keeps its mark; its Result can come from Part 1's recording. Where a fact's two hosts go different ways, the mark gives each half.
- **What settles it** says what is recorded or read. The recorder keeps hook payloads whole, since everything typed and spoken in a sitting is made up; of the hosts' own files it keeps only row types, field names, sizes and short enum-like values, never text.
- **If refuted** is the fallback each paper names, or the plan's where the plan has one. "None named" means the paper names none.
- **Result** is empty until the fact is settled. It is then filled with one of confirmed, refuted or not settled, the date, and the host's version (and the voice version, for voice facts). A fact on both hosts has a result for each.
- **Results about the real project, server or machine say only which way the design goes.** This repository is public. For F-176, F-177, the real-branch half of F-183, F-202, and the git and macOS versions that F-185, F-189 and F-204 read, the Result says only "the primary path holds" or "the fallback applies", in general terms, and a version floor as "at or above the floor" or "below the floor". It never states the server's settings, a device-management profile, the Mac's macOS or git version, or anything else of the one real install. The owner's answers stay in the recording, outside the repository. Host versions (each CLI's, each desktop app's, the voice version) are the hosts' public release names and are written as they are; nothing names which model the owner chose.

## The counts

The six papers' lists, with what they assume without listing and the plan's own items, came to 289 entries. Merged, they are 216 facts.

| Settled by | Facts |
| --- | --- |
| Sitting 0 (the evaluation home, the machine and the git server) | 26 |
| The Claude Code sitting | 27 |
| The Codex sitting | 37 |
| Both host sittings | 51 |
| All the sittings together | 141 |
| DV-02 | 13 |
| DV-03 | 2 |
| EV-03 | 12 |
| Published source | 39 |
| None in Stage 0 (plan section 12) | 9 |
| **All** | **216** |

So the Claude Code sitting, as first written, gave evidence on 78 facts (27 of its own and 51 shared), and the Codex sitting on 88 (37 and 51), in about ten hours over three sittings.

The marks of the 141 facts settled by a sitting, after the cut:

| Settled by | Core | Later | By the field report | From source | All |
| --- | --- | --- | --- | --- | --- |
| Sitting 0 | 1 | 8 | 9 | 8 | 26 |
| The Claude Code sitting | 0 | 22 | 4 | 1 | 27 |
| The Codex sitting | 4 | 3 | 16 | 14 | 37 |
| Both host sittings | 2 | 13 | 22 | 14 | 51 |
| **All** | **7** | **46** | **51** | **37** | **141** |

F-016 and F-033 are counted as core, for their Claude Code halves; F-010 is counted by the field report, for its Codex half, and its Claude Code half is later. The run-book's Part 1 is there for the 7 core facts and records 43 others at no added minute; its Part 2 holds the steps of every later fact, and of the facts marked for the field report or from source, for when the report or the source leaves one open.

| Group | Numbers | Facts |
| --- | --- | --- |
| A. Both hosts | F-001 to F-074 | 74 |
| B. Claude Code | F-075 to F-119 | 45 |
| C. Codex | F-120 to F-174 | 55 |
| D. The git server | F-175 to F-184 | 10 |
| E. The operating system | F-185 to F-204 | 20 |
| F. The model | F-205 to F-207 | 3 |
| G. Held for later | F-208 to F-216 | 9 |

## A. Both hosts

### Hook payloads

#### F-001. A turn id on every event of a turn

- **Fact.** Each host's hook payloads carry a turn id (`prompt_id` on Claude Code, `turn_id` on Codex) that is the same on every event of one turn, and the same value is on that turn's rows in the host's record.
- **Needed by.** ledger §13 row 1 (`prompt_id`) and §3.3 `tid` (unlisted, Codex): R3, R4, reconciliation 6.3, LG-13, LG-20, LG-21; hosts §14 #6 (turn ids recorded).
- **Settled by.** Both host sittings, each host's half in its own. **Mark: by the field report.**
- **What settles it.** Every payload of a turn with two tool calls, a subagent, a compaction and a stop; per event name, whether the field is there and whether its value is equal across the turn and found on the record's rows.
- **If refuted.** Pair by order of receipt inside the session; R4 becomes "a prompt hook was seen since the last Stop".
- **Result, Claude Code.** Not yet recorded.
- **Result, Codex.** Not yet recorded.

#### F-002. A tool-use id on tool events, and on the record's row

- **Fact.** Tool hook events (PostToolUse, and PostToolUseFailure on Claude Code) carry the host's tool-use id, and the same id is on the tool call's row in the host's record.
- **Needed by.** ledger §13 row 2: reconciliation 6.3, the dedupe key 6.2, the `tool_coverage` and `dupes` bars, LG-13, LG-21, LG-23.
- **Settled by.** Both host sittings, each host's half in its own. **Mark: from source.**
- **What settles it.** A read, an edit and a shell command, one of which fails: the id field's name on each payload, and whether its value is on the matching record row.
- **If refuted.** Pair by tool name and a hash of the input.
- **Result, Claude Code.** Not yet recorded.
- **Result, Codex.** Not yet recorded.

#### F-003. The question, plan-approval and delegation tools reach PostToolUse whole

- **Fact.** The host's question tool, plan-approval tool and delegation tool each fire PostToolUse with their input and their result in the payload, so an answer picked from options and a plan approval are recorded with the option or plan text, apart from an answer the person typed.
- **Needed by.** ledger §13 row 3: `decision_capture`, LG-23, LG-13, LG-D1 (plan decision 6); derive unlisted (5.2, 5.5 `chosen`): the `accepted` tag, DV-D4 (plan decision 10), `accepted_captured`, DV-21.
- **Settled by.** Both host sittings, each host's half in its own. **Mark: later** (the run-book's Part 2, group C).
- **What settles it.** A multiple-choice question answered once by picking and once by typing, a plan approved and a plan rejected, a task delegated: for each, whether PostToolUse fires, and whether the payload holds the options, the plan text, the choice and the report. Which of the three tools each host has at all.
- **If refuted.** ledger: take them from the record's tool rows; without a record they are lost and counted. derive: decision 10's "if no" column, typed words only.
- **Result, Claude Code.** Not yet recorded.
- **Result, Codex.** Not yet recorded.

#### F-004. The Stop payload carries the final reply

- **Fact.** The Stop hook's payload carries the turn's final reply as `last_assistant_message` on both hosts, as the host delivered it.
- **Needed by.** ledger unlisted (6.3; cap `reply_in_stop` 6.5): reconciliation, the `reply` kind, `turn_coverage`, LG-20, LG-21.
- **Settled by.** Both host sittings, each host's half in its own. **Mark: by the field report.**
- **What settles it.** Several turns ended normally and one by interrupting: whether Stop fires, whether the field is there, and whether its text equals the record's reply row.
- **If refuted.** None named; the record's reply row would be the only source.
- **Result, Claude Code.** Not yet recorded.
- **Result, Codex.** Not yet recorded.

#### F-005. Payloads name the session's record file

- **Fact.** Hook payloads on both hosts name the path of the session's transcript or rollout file, and the file exists at hook time.
- **Needed by.** ledger unlisted (6.2 step 2; §21 item 5): the ingester's locate step, `record.state`, LG-21.
- **Settled by.** Both host sittings, each host's half in its own. **Mark: by the field report.**
- **What settles it.** Per event name, whether a path field is there and whether the named file existed when the hook ran.
- **If refuted.** The session is `unavailable` and falls to the hook floor (R4).
- **Result, Claude Code.** Not yet recorded.
- **Result, Codex.** Not yet recorded.

#### F-006. SessionStart names its source, SessionEnd its reason

- **Fact.** SessionStart carries one of five sources (startup, resume, fork, clear, compact) and SessionEnd carries a reason, on both hosts.
- **Needed by.** ledger unlisted (3.4 `life`, 7.3): privacy lineage, LG-24, the session class of LG-10; deliver unlisted (7.2 table, V-H1): DL-10's variants, DL-09's epochs.
- **Settled by.** Both host sittings, each host's half in its own. **Mark: from source.**
- **What settles it.** A chat started, resumed, forked, cleared, compacted and ended: the source and reason values each payload holds.
- **If refuted.** deliver 7.2: when the parent is unknown, the compact variant is used once; ledger: an unknown source falls under the unknown-lineage rule.
- **Result, Claude Code.** Not yet recorded.
- **Result, Codex.** Not yet recorded.

#### F-007. A failed tool call's payload holds the error and the exit status

- **Fact.** For each tool call the hook payload or the record gives the tool's name, its typed input (the command, the path), its status, and for a failed call the error text and the exit status (PostToolUseFailure on Claude Code, PostToolUse on Codex).
- **Needed by.** ledger unlisted (6.3, 3.4 `tool`): failure digests, LG-23, LG-D1 (plan decision 6); derive unlisted (4, 5.5, 5.6, 6.7): the `observed` tag, 5.6 acceptance, the trail, DV-20, DV-21, DV-31.
- **Settled by.** Both host sittings, each host's half in its own. **Mark: from source.**
- **What settles it.** A command that succeeds, one that fails with made-up output, an edit and a read: which event fires, the field names, and whether status, exit code and error text are there.
- **If refuted.** Take them from the record's tool result row. Without a status, done next steps, procedures and lessons cannot pass on tool evidence (derive 5.6).
- **Result, Claude Code.** Not yet recorded.
- **Result, Codex.** Not yet recorded.

#### F-008. An interrupted or failed turn is marked as ended

- **Fact.** When a person interrupts a turn, or the turn fails, a hook fires or a record row marks the turn as ended.
- **Needed by.** derive unlisted (4, 3.4, 3.5, 6.7): episode cuts, ripeness, the idle gate, the trail, DV-20, DV-24; ledger C-H1 (Codex Interrupt as an ingest signal).
- **Settled by.** Both host sittings, each host's half in its own. **Mark: from source.**
- **What settles it.** One turn interrupted part way on each host: which hooks fired, their field names, and the row types written.
- **If refuted.** None named; the turn would stay open and its range would wait.
- **Result, Claude Code.** Not yet recorded.
- **Result, Codex.** Not yet recorded.

#### F-009. A typed slash command or skill reaches the prompt hook before the model

- **Fact.** A typed plugin slash command (Claude Code) or skill invocation (Codex) reaches UserPromptSubmit with its name in the prompt before any model call.
- **Needed by.** ledger §13 row 11: LG-24's explicit command, LG-D7 (plan decision 19).
- **Settled by.** Both host sittings, each host's half in its own. **Mark: later** (the run-book's Part 2, group D).
- **What settles it.** The scratch plugin's command or skill typed on each host: whether UserPromptSubmit fires, the exact prompt text, and whether a model reply came before the hook ran.
- **If refuted.** The pause phrase remains the control; the command file only tells the model.
- **Result, Claude Code.** Not yet recorded.
- **Result, Codex.** Not yet recorded.

#### F-010. A message typed while the agent works fires a prompt hook

- **Fact.** On both hosts a message typed while the agent is still working fires UserPromptSubmit and gets its own turn or prompt id.
- **Needed by.** ledger §13 row 8 (Claude Code) and the plan's HF-00 row (Codex): R4, LG-21; derive plan item: episode cuts, the tags of 5.5, DV-20.
- **Settled by.** Both host sittings, each host's half in its own. **Mark: by the field report** for Codex's half; Claude Code's half later (the run-book's Part 2, group B, C-14), since the real install has no person's Claude Code chat in which to count it.
- **What settles it.** A second made-up message typed while a slow task runs: whether a prompt hook fires, its id, and how the record holds it.
- **If refuted.** R4 stays fail-closed as written; only the count of reduced events changes.
- **Result, Claude Code.** Not yet recorded.
- **Result, Codex.** Not yet recorded.

#### F-011. The desktop app's spare session starts again in the real folder

- **Fact.** When a desktop app opens a spare session in advance, SessionStart fires again with the real folder once the person starts working in it.
- **Needed by.** deliver §12 row 17: DL-09 (owed brief), DL-10.
- **Settled by.** Both host sittings, each host's half in its own. **Mark: later** (the run-book's Part 2, group A).
- **What settles it.** Each desktop app opened, a made-up project folder chosen: the SessionStart events before and at the first prompt, with their count, folder and source.
- **If refuted.** The first prompt pays the owed brief, as designed.
- **Result, Claude Code.** Not yet recorded.
- **Result, Codex.** Not yet recorded.

#### F-012. Each host reports its own version

- **Fact.** Each host reports its version in the hook payload, the hook's environment or the record's rows.
- **Needed by.** ledger unlisted (6.2 step 1; 6.5): adapter profiles and their canary, LG-13, LG-21.
- **Settled by.** Both host sittings, each host's half in its own. **Mark: from source.**
- **What settles it.** Where a version value appears (payload field, environment variable name, record row) and its value.
- **If refuted.** None named; the newest profile with its canary applies.
- **Result, Claude Code.** Not yet recorded.
- **Result, Codex.** Not yet recorded.

#### F-013. A session id is safe in a file name

- **Fact.** Each host's session id holds no `.` or `/`, so `<host>.<session>.<writer>.md` names one session unambiguously.
- **Needed by.** store unlisted (5.1, 5.5): episodes and coverage, ST-05, ST-08.
- **Settled by.** Both host sittings, each host's half in its own. **Mark: by the field report.**
- **What settles it.** The character set and form of the session id in every recorded payload (typed chat, fork, subagent, program, voice on Codex).
- **If refuted.** None named.
- **Result, Claude Code.** Not yet recorded.
- **Result, Codex.** Not yet recorded.

#### F-014. The session id survives a clear

- **Fact.** A session keeps the same session id across the host's clear command.
- **Needed by.** deliver §12 row 10: DL-09 (epochs), DL-10 (handoff of the cleared chat).
- **Settled by.** Both host sittings, each host's half in its own. **Mark: later** (the run-book's Part 2, group E).
- **What settles it.** The session ids of the events before and after a clear.
- **If refuted.** None needed; the epoch follows the event, not the id.
- **Result, Claude Code.** Not yet recorded.
- **Result, Codex.** Not yet recorded.

#### F-015. A read or search in another repository names that repository

- **Fact.** The hook payload or the record of a read or a search in another repository names that repository, or a path from which it and its head can be found.
- **Needed by.** plan's sitting list item 5; ledger (3.4 `paths`); deliver (DL-18 through DV-23); store (5.3 `anchors`, ST-01); derive (6.2 anchors, H4, DV-23, DV-31); plan DV-23 and decision 31.
- **Settled by.** Both host sittings, each host's half in its own. **Mark: later** (the run-book's Part 2, group G).
- **What settles it.** From a folder outside two scratch repositories, a read and a search in one of them: the target and path fields of the tool payload and of the record.
- **If refuted.** Plan DV-23: such a path is a cue, not an anchor.
- **Result, Claude Code.** Not yet recorded.
- **Result, Codex.** Not yet recorded.

### Prompts and who wrote them

#### F-016. A prompt the host writes is marked as the host's

- **Fact.** Every prompt the host writes in a person's place (a scheduled or loop prompt, a task notification or subagent report, bash-mode input and output, an interrupt notice, a command banner, a message from another session, a compaction summary or resume note) is marked in the hook payload or the record, by a known wrapper or a field, so it is never read as typed.
- **Needed by.** ledger unlisted (3.6, one list of wrappers): provenance by allowlist, `pv_false_typed`, LG-13; deliver §12 row 12: DL-11, DL-09, V-H2; derive unlisted (5.5, legend of 4): DV-20, DV-21; plan's Codex fallback "typed unless it carries a known wrapper".
- **Settled by.** Both host sittings, each host's half in its own. **Mark: core** for Claude Code's half (the run-book's Part 1); Codex's half from source and the field report.
- **What settles it.** Each host-written kind the run-book can make (a command banner, a bash-mode line, an interrupt, a scheduled prompt where offered, a subagent report, a compaction, a resume): the first characters and the field names of each prompt, as the hook and the record carry it. Part 1 settles the kinds that reach the prompt hook, on which LG-13's hook-side rule rests (C-15, C-17, C-19, C-27). The kinds found only in the record, a compaction summary and a subagent's report, are made in Part 2's groups C (C-12) and E (C-25), before LG-21, which reads the record; for a user row whose text matches no payload, the recorder keeps the name of a leading tag even when it is not yet on the vocabulary, so a wrapper no one listed still shows. A message from another session is not made in the sittings, since the run-book has no way to send one; that kind keeps deliver's fallback (unknown is treated as not typed) until a host shows it.
- **If refuted.** Unrecognized prompts are stored as `unknown`, which supports only `external` (derive 5.5); deliver: unknown is treated as not typed.
- **Result, Claude Code.** Not yet recorded.
- **Result, Codex.** Not yet recorded.

#### F-017. Pasted text is marked on Claude Code and not on Codex

- **Fact.** Claude Code marks which part of a prompt was pasted, in the payload or the record; Codex does not.
- **Needed by.** ledger unlisted (3.6 `pasted`; the Codex adapter declares `paste_marking: false`): LG-13, C-D5; derive unlisted (5.5, D-L1): the `stated` and `external` tags, `misattributed`, DV-21.
- **Settled by.** Both host sittings, each host's half in its own. **Mark: later** (the run-book's Part 2, group D).
- **What settles it.** A prompt mixing typed words and a pasted made-up block, and a wholly pasted prompt: whether the payload or the record marks the pasted span.
- **If refuted.** None named.
- **Result, Claude Code.** Not yet recorded.
- **Result, Codex.** Not yet recorded.

#### F-018. A program's session can be told from a person's chat

- **Fact.** On each host, a session a program drives (`claude -p`, `codex exec`) can be told from a person's chat by a field in the hook payload or in the host's record.
- **Needed by.** plan's sitting list item 4; ledger (3.6 `program`): LG-10's session class, LG-13; deliver (7.2, "SessionStart with a program's entrypoint"): DL-15, DL-10, plan decision 14; derive (3.4 `skipped: program`): DV-24; evaluate (4.3, 4.4, the actor seam): EV-D4, E-L2, EV-17.
- **Settled by.** Both host sittings, each host's half in its own. **Mark: by the field report.**
- **What settles it.** One headless session with a made-up prompt beside a typed chat on each host: every payload field and record row that differs.
- **If refuted.** None named; such prompts are `unknown`.
- **Result, Claude Code.** Not yet recorded.
- **Result, Codex.** Not yet recorded.

### Transcripts and session files

#### F-019. The reply row is on disk when Stop fires

- **Fact.** At the moment a turn's Stop hook fires, the turn's reply row is already in the host's record. Counted at every Stop: how often it is missing (Claude Code is expected to write its transcript asynchronously).
- **Needed by.** plan's HF-00 row (how often a reply's row is missing); ledger §13 row 6: R3, LG-21's gate (the final reply exactly once with Stop dropped).
- **Settled by.** Both host sittings, each host's half in its own. **Mark: later**; the run-book's Part 1 records it at no added minute.
- **What settles it.** At each Stop and each UserPromptSubmit, the record's size and whether the turn's reply or prompt row was present; the count of misses out of all Stops.
- **If refuted.** No change; R3 is safe either way.
- **Result, Claude Code.** Not yet recorded.
- **Result, Codex.** Not yet recorded.

#### F-020. The record is appended in order

- **Fact.** The host appends record rows in order, so a row written after an event's receipt means earlier rows are on disk, and the file's modified time advances at each write.
- **Needed by.** ledger unlisted (6.4 R3): LG-21 "exposes a session only up to the checked point", `otr_leaks`.
- **Settled by.** Both host sittings, each host's half in its own. **Mark: by the field report.**
- **What settles it.** The record's size and modified time at each hook; row order in the file against hook order.
- **If refuted.** None named.
- **Result, Claude Code.** Not yet recorded.
- **Result, Codex.** Not yet recorded.

#### F-021. Each record row carries its time, folder and branch

- **Fact.** Each row of the host's record carries its own time, working folder and git branch.
- **Needed by.** ledger unlisted (3.3 `t`; 6.2 steps 4 and 8): ordering, `ctx` events, the catchup-8 fix in LG-21.
- **Settled by.** Both host sittings, each host's half in its own. **Mark: by the field report.**
- **What settles it.** Per row type, whether a time, a folder and a branch field are there; one change of folder or branch mid-chat and whether later rows follow it.
- **If refuted.** None named.
- **Result, Claude Code.** Not yet recorded.
- **Result, Codex.** Not yet recorded.

#### F-022. Text between tool calls and each tool's status are separate rows

- **Fact.** The host's record holds assistant text written between tool calls, and each tool call with its result status, as separate rows.
- **Needed by.** ledger unlisted (6.2 step 4; `segment` 3.4): the `segment` kind (4.1, LG-D1), `tool_coverage`, LG-21, LG-23.
- **Settled by.** Both host sittings, each host's half in its own. **Mark: by the field report.**
- **What settles it.** A turn in which the agent writes, calls a tool, writes again and replies: the row types in order and whether each tool row has a status.
- **If refuted.** None named.
- **Result, Claude Code.** Not yet recorded.
- **Result, Codex.** Not yet recorded.

#### F-023. The store root comes from the host's environment

- **Fact.** The host's record folder is given by `CLAUDE_CONFIG_DIR` or `CODEX_HOME`, and a hook can read that variable in its environment.
- **Needed by.** ledger unlisted (6.2 step 2; catchup-11): the transcript trust checks, LG-21.
- **Settled by.** Both host sittings, each host's half in its own. **Mark: from source.**
- **What settles it.** With the variable set to a made-up folder for one session, in which the scratch plugins were registered first (and unset for the others): whether the name is in the hook's environment, and whether the named record lies under it.
- **If refuted.** None named.
- **Result, Claude Code.** Not yet recorded.
- **Result, Codex.** Not yet recorded.

#### F-024. Each chat writes a session file even with no hook

- **Fact.** Each host writes or touches a session file under its own folders whenever a chat happens, so a scan of file names and times shows activity even when no hook ran.
- **Needed by.** hosts unlisted (7.2, 7.4): `capture.silent`, relevance, fault matrix rows 1 to 4.
- **Settled by.** Both host sittings, each host's half in its own. **Mark: by the field report.**
- **What settles it.** A made-up chat held while the scratch hooks are unapproved (Codex) or failing (Claude Code): which file under the host's folders changed, by path shape and time only.
- **If refuted.** None named.
- **Result, Claude Code.** Not yet recorded.
- **Result, Codex.** Not yet recorded.

### Subagents, programs and the processes hooks start

#### F-025. SubagentStop carries the report and the agent id

- **Fact.** SubagentStop fires on both hosts, and its payload carries `last_assistant_message` (the subagent's final report) and the subagent's agent id.
- **Needed by.** ledger §13 row 4: `sub_report`, LG-23, C-H1, LG-D1 (plan decision 6).
- **Settled by.** Both host sittings, each host's half in its own. **Mark: by the field report.**
- **What settles it.** One task delegated to a subagent that ends normally: whether SubagentStop fired, and the names of its last-message and agent-id fields.
- **If refuted.** Use the delegation tool's result as the report.
- **Result, Claude Code.** Not yet recorded.
- **Result, Codex.** Not yet recorded.

#### F-026. SubagentStart can add context, and tells the host's own agents apart

- **Fact.** A SubagentStart hook can add context the subagent sees, and the payload tells the host's own agent types (an empty type, for one) from agents a person or a plugin defined.
- **Needed by.** deliver §12 row 7: DL-15, DL-D7 (plan decision 14), V-H1.
- **Settled by.** Both host sittings, each host's half in its own. **Mark: later** (the run-book's Part 2, group C).
- **What settles it.** A subagent told to repeat a planted word from SubagentStart's output, and its answer; the agent-type field for one built-in and one custom agent.
- **If refuted.** Subagents are covered by tool time only; with tool time deferred (plan section 12), they get no delivery.
- **Result, Claude Code.** Not yet recorded.
- **Result, Codex.** Not yet recorded.

#### F-027. A subagent's hook events carry its agent id

- **Fact.** Hook events fired inside a subagent, pre-tool ones included, fire for the subagent's own tool calls and carry its agent id, and its record rows are marked as the subagent's (in the main file with a sidechain flag, or in a file of their own).
- **Needed by.** ledger unlisted (3.3 `agent`; 6.2 step 4; 3.6): provenance, LG-13, LG-21, LG-23; deliver unlisted (7.6 subagent row): DL-14, DL-15, DL-19; evaluate unlisted (6.4 delegated variant): EV-30.
- **Settled by.** Both host sittings, each host's half in its own. **Mark: by the field report.**
- **What settles it.** A delegated task that edits a made-up file: whether PreToolUse and PostToolUse fired for it, the agent-id field on each, and where its record rows are.
- **If refuted.** None named.
- **Result, Claude Code.** Not yet recorded.
- **Result, Codex.** Not yet recorded.

#### F-028. A hook inherits the host's environment

- **Fact.** A hook process inherits the environment of the host process that started it, interactive or headless: a made-up variable set before starting the host, a secret-like name included, is present in every hook's environment.
- **Needed by.** ledger unlisted (10.3 T1): LG-D6, LG-31; derive §11 row 7 (`EVA_MD_INNER=1` on `codex exec` reaches eva-md's hooks): 8.1, the `skipped: program` coverage, DV-11.
- **Settled by.** Both host sittings, each host's half in its own. **Mark: by the field report.**
- **What settles it.** Variables with made-up names and values set before an interactive and a headless start; the recorder notes only whether each name is present, never a value.
- **If refuted.** derive: the run folder is a temporary folder, which the hooks already skip. ledger: rules only.
- **Result, Claude Code.** Not yet recorded.
- **Result, Codex.** Not yet recorded.

#### F-029. A hook's environment: home, shell, agent socket, path

- **Fact.** A hook process, and a job a hook starts, has `HOME` set, runs under a known shell, and sees the SSH agent's socket; a chat's shell finds a linked command on its PATH.
- **Needed by.** hosts unlisted (7.5; H-S2, H-V6; field questions 9 and 17): sync retried from a chat's worker, command names, `eva-md repair`.
- **Settled by.** Both host sittings, each host's half in its own. **Mark: by the field report.**
- **What settles it.** The names (never values) of the hook's and its detached child's environment variables, the parent shell, whether `SSH_AUTH_SOCK` is set; the agent asked to run `command -v` on a made-up linked name.
- **If refuted.** None named.
- **Result, Claude Code.** Not yet recorded.
- **Result, Codex.** Not yet recorded.

#### F-030. A detached child outlives its hook

- **Fact.** A detached child process started by a hook that exits within its time limit keeps running to completion, at the end of a turn and at the end of a session, on both hosts, even when the host is closed right after.
- **Needed by.** ledger §13 row 13 (both halves): the ingest spawn 6.1, C-D1, LG-21; hosts §14 #8: the wake and the fallback tick, D-H1, plan DV-12 and HO-22; derive §11 row 10: wakes (3.1), `end_to_saved`, DV-12, D-H2.
- **Settled by.** Both host sittings, each host's half in its own. **Mark: by the field report.**
- **What settles it.** The Stop and SessionEnd hooks start a detached child that writes a timestamped file 10 seconds later; a turn ended, a chat closed, and the host quit right after a turn: whether each file appears.
- **If refuted.** The tick alone wakes the worker; freshness falls to the next hook, and `end_to_saved` rises to the tick interval.
- **Result, Claude Code.** Not yet recorded.
- **Result, Codex.** Not yet recorded.

#### F-031. A job a hook starts can reach the git server

- **Fact.** A background job started from a hook can run git against the network on each host; the host's sandbox does not block it.
- **Needed by.** store §16 row 8: ST-00, ST-06, S-H2, 8.2.
- **Settled by.** Both host sittings, each host's half in its own. **Mark: by the field report.**
- **What settles it.** A Stop hook's detached job runs `git ls-remote` against the scratch repository with prompts off: the exit class per host and whether a sandbox refusal appeared.
- **If refuted.** None named; the OS-scheduled tick is the other route.
- **Result, Claude Code.** Not yet recorded.
- **Result, Codex.** Not yet recorded.

#### F-032. Each host runs headless on a plan login

- **Fact.** Each host allows headless runs on a plan login, at about ten small calls and eight short sessions a day.
- **Needed by.** evaluate unlisted (6.9, risk 7, EV-D1): the canary's signed-in tier, EV-22, release gate R2.
- **Settled by.** Both host sittings, each host's half in its own. **Mark: by the field report.**
- **What settles it.** One headless session per host on the plan login: whether it runs, and any message about the plan or billing. The vendor's published terms for scheduled use, read and dated.
- **If refuted.** None named in the paper; plan section 12 holds "An API key as a second way to run the host's CLI".
- **Result, Claude Code.** Not yet recorded.
- **Result, Codex.** Not yet recorded.

### Compaction, forks and resume

#### F-033. A fork or resume names its parent

- **Fact.** A forked or resumed session, and one started by a compaction, names its parent session id in the SessionStart payload or the first rows of its record.
- **Needed by.** ledger §13 row 9: privacy lineage 7.3, LG-24; deliver unlisted (7.2 forked row, V-L1): DL-09, DL-10; derive unlisted (6.6 rule 2, D-L1): workstream binding, DV-23, DV-40.
- **Settled by.** Both host sittings, each host's half in its own. **Mark: core** for Claude Code's half (the run-book's Part 1); Codex's half from source.
- **What settles it.** A chat resumed and a chat forked: the SessionStart payloads and the new record's first rows, and every field that holds the earlier session's id.
- **If refuted.** ledger: the unknown-lineage rule for every fork (start with `was_off` true). deliver: the compact variant, once. derive: rule 3 of 6.6.
- **Result, Claude Code.** Not yet recorded.
- **Result, Codex.** Not yet recorded.

#### F-034. A compaction leaves a hook or a record row

- **Fact.** Each host fires a hook (PreCompact, PostCompact, SessionStart `compact`) or writes a record row when a chat is compacted, by hand or automatically.
- **Needed by.** derive unlisted (3.4 `ended` rule, P2, 6.6 rule 2): DV-24.
- **Settled by.** Both host sittings, each host's half in its own. **Mark: by the field report.**
- **What settles it.** One compaction by command on each host, and one automatic compaction where the run-book can force it: which hooks fired, their field names, and the row types added.
- **If refuted.** None named; the `idle` and `long` rules still make the range ripe.
- **Result, Claude Code.** Not yet recorded.
- **Result, Codex.** Not yet recorded.

### MCP servers and tools

#### F-035. An MCP tool's name names its server, and other tools are told apart

- **Fact.** A tool record names the tool well enough to tell a web fetch or search, a network command, an MCP tool with the name of its server, and a subagent call.
- **Needed by.** ledger unlisted (3.7 `mcp` taint, `self`): C-L3; derive unlisted (5.5 taint): `misattributed`, `stored_with_authority`, C-D5, DV-21.
- **Settled by.** Both host sittings, each host's half in its own. **Mark: from source.**
- **What settles it.** A public page fetched, a network command, a call to the scratch MCP server, a subagent: each record's and payload's name fields, and any server or agent id.
- **If refuted.** None named.
- **Result, Claude Code.** Not yet recorded.
- **Result, Codex.** Not yet recorded.

#### F-036. PostToolUse for an MCP tool holds its result text

- **Fact.** On both hosts the PostToolUse payload for an MCP tool call carries the tool's result text.
- **Needed by.** deliver §12 row 13 part 3: DL-12 (the pull token), DL-20, V-L3, V-D5.
- **Settled by.** Both host sittings, each host's half in its own. **Mark: from source.**
- **What settles it.** A scratch tool whose result ends with a made-up token: whether the token is in the PostToolUse payload.
- **If refuted.** Pulls are joined to chats by time.
- **Result, Claude Code.** Not yet recorded.
- **Result, Codex.** Not yet recorded.

#### F-037. An MCP server's instructions reach the model, within a size

- **Fact.** Both hosts give the model an MCP server's instructions: Claude Code up to 4,096 characters with tools deferred by default, Codex with its first 512 characters standing alone.
- **Needed by.** deliver §12 row 13 part 2: DL-12, DL-22; hosts unlisted (3.3 mcp row; H-V4; field question 12): byte budgets.
- **Settled by.** Both host sittings, each host's half in its own. **Mark: from source.**
- **What settles it.** Made-up instructions longer than 4,096 characters, with planted words at 400, 3,900 and 5,000 characters: which words the agent can quote.
- **If refuted.** The brief's legend names the tool.
- **Result, Claude Code.** Not yet recorded.
- **Result, Codex.** Not yet recorded.

#### F-038. The first use of an MCP tool asks once

- **Fact.** The first call of each MCP tool raises one permission question, the person can allow it for good, and a plugin cannot declare its own allow rule.
- **Needed by.** deliver §12 row 14: DL-D9, DL-12; hosts unlisted (5.4): HO-D5, `status --all`'s printed rule.
- **Settled by.** Both host sittings, each host's half in its own. **Mark: by the field report.**
- **What settles it.** Each scratch tool called for the first time and again in a new chat: the questions shown, whether "allow always" is offered, and once per what.
- **If refuted.** DL-D9: nothing is written to host files, and the doctor prints the rule a person may add.
- **Result, Claude Code.** Not yet recorded.
- **Result, Codex.** Not yet recorded.

#### F-039. A tool result has a size limit

- **Fact.** Each host has a size limit on an MCP tool's result, past which it cuts or spills the result.
- **Needed by.** deliver unlisted (10.2): DL-12's budget and pinned tool list.
- **Settled by.** Both host sittings, each host's half in its own. **Mark: from source.**
- **What settles it.** A scratch tool returning made-up results of 10, 25, 50 and 100 KB with a planted word at the end: the size at which the word stops reaching the model.
- **If refuted.** None named; the cap is an adapter constant.
- **Result, Claude Code.** Not yet recorded.
- **Result, Codex.** Not yet recorded.

#### F-040. An MCP server is not told which chat it serves

- **Fact.** The host does not tell an MCP server which session a call belongs to; the server knows only its folder.
- **Needed by.** deliver unlisted (10.3, 11, 17): DL-12 (the pull token), DL-17, V-L3.
- **Settled by.** Both host sittings, each host's half in its own. **Mark: from source.**
- **What settles it.** The scratch server logs the request fields and the names of its environment variables; whether any holds a session id.
- **If refuted.** None needed; a session id would let pulls be joined directly.
- **Result, Claude Code.** Not yet recorded.
- **Result, Codex.** Not yet recorded.

#### F-041. Whether a host reads an MCP server's resources

- **Fact.** Each host lists an MCP server's resources and lets the model read them.
- **Needed by.** deliver unlisted (10.3 Resources): DL-22 (deferred in plan section 12).
- **Settled by.** Both host sittings, each host's half in its own. **Mark: from source.**
- **What settles it.** A scratch resource holding a planted word: whether the host lists it and the agent can quote the word.
- **If refuted.** No resources on that host, as the paper says.
- **Result, Claude Code.** Not yet recorded.
- **Result, Codex.** Not yet recorded.

#### F-042. Where the time of an MCP call goes

- **Fact.** The time of one MCP tool call, split into the host's call, the server's receipt and the reply, against the same lookup run as a command.
- **Needed by.** plan's sitting list item 6 and section 13.5 (search by MCP costs over a second more); deliver: DL-12's time bar on both routes.
- **Settled by.** Both host sittings, each host's half in its own. **Mark: later** (the run-book's Part 2, group F).
- **What settles it.** One made-up lookup through the scratch MCP tool and the same lookup by command, each three times, with stamps at PreToolUse, the server's receipt, its reply and PostToolUse.
- **If refuted.** None named.
- **Result, Claude Code.** Not yet recorded.
- **Result, Codex.** Not yet recorded.

#### F-043. A plugin's MCP server starts even when its hooks do not run

- **Fact.** A plugin's MCP server is started with each chat even when that host's hooks are unapproved or failing.
- **Needed by.** hosts §14 #10: 7.6 same-host observer, fault matrix rows 1 and 13, HO-23, HO-D7.
- **Settled by.** Both host sittings, each host's half in its own. **Mark: later** (the run-book's Part 2, group I).
- **What settles it.** A chat started with the scratch hooks unapproved (Codex) or failing (Claude Code): the server's start line and its time against any hook line.
- **If refuted.** That case falls to the tick and the other host.
- **Result, Claude Code.** Not yet recorded.
- **Result, Codex.** Not yet recorded.

#### F-044. One MCP server process per chat, for the chat's life

- **Fact.** Each chat starts its own MCP server process, which runs as long as the chat is open, may start a child process, and is not restarted when the binary file is replaced.
- **Needed by.** hosts unlisted (6, 10.5): the fallback tick, the N and N-1 rule, HO-25, D-H1; derive unlisted (3.8): the no-tick fallback, DV-12.
- **Settled by.** Both host sittings, each host's half in its own. **Mark: later** (the run-book's Part 2, group I).
- **What settles it.** The scratch server writes its process id and a time every minute and starts one short child; two chats, one left open and idle for 25 minutes; the binary swapped: the ids, whether the stamps are continuous, and whether a new server started.
- **If refuted.** Hook wakes alone carry the timed steps (derive 3.8).
- **Result, Claude Code.** Not yet recorded.
- **Result, Codex.** Not yet recorded.

#### F-045. SessionStart follows the MCP server's start within 90 seconds

- **Fact.** When a host's hooks work, its SessionStart hook fires within 90 seconds of its MCP server's initialize, even if the person types nothing.
- **Needed by.** hosts unlisted (7.4, 7.6): `capture.silent`, fault matrix rows 1 and 2, HO-23.
- **Settled by.** Both host sittings, each host's half in its own. **Mark: later** (the run-book's Part 2, group I).
- **What settles it.** A chat opened on each host and left two minutes without typing: the times of the server's initialize line and of SessionStart.
- **If refuted.** None named.
- **Result, Claude Code.** Not yet recorded.
- **Result, Codex.** Not yet recorded.

### What a hook's output reaches

#### F-046. A `systemMessage` is shown to the person and not to the model

- **Fact.** A `systemMessage` a hook returns is shown to the person, as one plain line on Claude Code and as a warning on Codex, in the terminal and in the desktop app, and is not given to the model.
- **Needed by.** hosts unlisted (3.3 notice row, 7.6, 5.2, H-V1): HO-12, HO-23; ledger §13 row 10 (Codex): LG-24, LG-D7 (plan decision 19); deliver §12 row 18 (Codex) and unlisted (13, Claude Code): DL-20, V-H2.
- **Settled by.** Both host sittings, each host's half in its own. **Mark: by the field report.**
- **What settles it.** A SessionStart and a prompt hook return a made-up `systemMessage`: how it shows in the terminal and the desktop app (a written note or a screenshot of made-up content), and whether the agent says it saw the text.
- **If refuted.** Codex: no start line there, as designed; ledger: say it in the model's context and in status, as today.
- **Result, Claude Code.** Not yet recorded.
- **Result, Codex.** Not yet recorded.

#### F-047. SessionStart is synchronous

- **Fact.** SessionStart hooks are synchronous on both hosts: the first model response waits for the hook's output.
- **Needed by.** deliver unlisted (7.6 chat start row): DL-10, DL-11, the start bar of section 1.
- **Settled by.** Both host sittings, each host's half in its own. **Mark: by the field report.**
- **What settles it.** A SessionStart hook that sleeps one second and returns a planted word: whether the first answer knows the word, and the hook's time against the first reply's.
- **If refuted.** None named.
- **Result, Claude Code.** Not yet recorded.
- **Result, Codex.** Not yet recorded.

#### F-048. Several pre-tool hooks can run at once

- **Fact.** Several PreToolUse hooks of one session can run at the same time when the agent makes parallel tool calls.
- **Needed by.** deliver unlisted (6.1): DL-09 (the lock on deliver.json, `busy`), DL-14.
- **Settled by.** Both host sittings, each host's half in its own. **Mark: later** (the run-book's Part 2, group M).
- **What settles it.** Several parallel made-up edits asked for, with a pre-tool hook that sleeps two seconds: the start and end times, and any overlap.
- **If refuted.** None needed; the lock then costs nothing.
- **Result, Claude Code.** Not yet recorded.
- **Result, Codex.** Not yet recorded.

#### F-049. The host adds its own context at a chat start

- **Fact.** At a chat start each host injects some things of its own (a git status snapshot, instruction files, its own memory), and the list differs by host.
- **Needed by.** deliver §12 row 11: DL-10's git section, DL-23, DL-D4, V-H2.
- **Settled by.** Both host sittings, each host's half in its own. **Mark: from source.**
- **What settles it.** A chat started in a made-up repository: the names of the blocks the host added, from Claude Code's request records and Codex's thread items, by field and row names only.
- **If refuted.** Keep the git section short on that host.
- **Result, Claude Code.** Not yet recorded.
- **Result, Codex.** Not yet recorded.

#### F-050. A missing hook binary leaves the chat unharmed

- **Fact.** When a hook's command names a binary that does not exist, the host shows nothing that interrupts the chat, blocks no prompt, and the chat goes on.
- **Needed by.** hosts §14 #9 second half (Claude Code exec form): 4.6, uninstall, SSH sessions; evaluate unlisted (6.9 item 5, both hosts): the canary's weekly fault set, EV-22, claim row 12.
- **Settled by.** Both host sittings, each host's half in its own. **Mark: from source.**
- **What settles it.** One declared hook pointed at a path that does not exist, and a made-up prompt typed: what the person sees, and whether the reply comes.
- **If refuted.** hosts: a guarded shell form for Claude Code's hooks; evaluate: the full fault matrix stays offline with fake hosts.
- **Result, Claude Code.** Not yet recorded.
- **Result, Codex.** Not yet recorded.

### Read from the hosts' published documents

#### F-051. A changing hook output costs a cache miss

- **Fact.** Hook output that changes between model requests costs a full prompt-cache miss on both hosts.
- **Needed by.** deliver unlisted (7.2): DL-10 (a byte-stable start brief).
- **Settled by.** Published source.
- **What settles it.** The changelog entries that say so, at the recorded versions.
- **If refuted.** None named; the byte-stability gate stays either way.
- **Result.** Not yet recorded.

#### F-052. A skill's description is in context on every turn

- **Fact.** A skill's description is placed in the model's context on every turn, at a cost in tokens.
- **Needed by.** deliver unlisted (9): DL-12 (the recall skill is removed).
- **Settled by.** Published source.
- **What settles it.** Each host's skill documentation at the recorded version.
- **If refuted.** None named.
- **Result.** Not yet recorded.

#### F-053. A failed turn's rate limit is reported to hooks

- **Fact.** When a person's own turn fails on a rate limit, the host reports it in a hook payload or its record, with a reason that reads as a rate limit.
- **Needed by.** derive unlisted (3.5, the quota gate's hour hold): DV-24.
- **Settled by.** Published source.
- **What settles it.** Each host's hook documentation and source for a failed-turn event and its reason field; any such payload met in a sitting is kept.
- **If refuted.** None named; the budget and the hold after the runner's own `rate_limit` still apply.
- **Result.** Not yet recorded.

#### F-054. Which instruction files each host loads

- **Fact.** Claude Code loads `CLAUDE.md` and the files it imports, and Codex loads `AGENTS.md`.
- **Needed by.** derive unlisted (5.9): the `in_instruction_file` refusal, DV-21.
- **Settled by.** Published source.
- **What settles it.** Each host's documentation and source on instruction files at the recorded version.
- **If refuted.** None named.
- **Result.** Not yet recorded.

#### F-055. Chat tab and IDE extension load local MCP servers

- **Fact.** The Claude desktop app's Chat tab on the person's computer and the Codex IDE extension load local MCP servers, and the Codex IDE extension loads no plugins or hooks.
- **Needed by.** hosts unlisted (11.1, 3.3 surfaces row): the MCP-only floor 11.3, HO-41, plan section 12's floor.
- **Settled by.** Published source.
- **What settles it.** Each host's documentation at the recorded version.
- **If refuted.** None named.
- **Result.** Not yet recorded.

### Plugins, install and trust

#### F-056. Plugins register and are removed with no question

- **Fact.** Registering, enabling and removing a local-marketplace plugin with each host's own commands runs to the end with no question, with stdin closed and no terminal.
- **Needed by.** hosts unlisted (5.1, 3.2): HO-21, HO-31's scorecard, HO-11.
- **Settled by.** Sitting 0 (the evaluation home, the machine and the git server). **Mark: later** (the run-book's Part 2, group N).
- **What settles it.** Each register and remove command for the scratch plugins run with stdin closed: the exit status and any prompt.
- **If refuted.** None named.
- **Result.** Not yet recorded.

#### F-057. Each CLI reports its state read-only

- **Fact.** Each host CLI reports, read-only, with no model call and without signing in: its version, its flags and effort levels, whether its hooks feature is on (Codex: `codex features list`), each plugin's registered and enabled state, and what policy has switched off.
- **Needed by.** hosts unlisted (3.2, 3.3, 12): the HO-10 probe, `host.version_untested`, `policy.blocked`, HO-43; derive unlisted (8.1, 3.6, 3.7): the probe, breaker resets, the canary, replay on a CLI change, DV-11, DV-22.
- **Settled by.** Both host sittings, each host's half in its own. **Mark: by the field report.**
- **What settles it.** Each read-only command, with the network off once, with the scratch plugin enabled, disabled and removed: the field names and wording of each state, and the time each took. Policy states cannot be made in the evaluation home; they stay not settled.
- **If refuted.** None named; v1's ladder of refusals is what the probe replaces.
- **Result, Claude Code.** Not yet recorded.
- **Result, Codex.** Not yet recorded.

### Headless runs

#### F-058. A headless run reports how much allowance is left

- **Fact.** A headless run of either host CLI reports, in its output, how much of the plan's allowance is left or when it resets.
- **Needed by.** derive §11 row 4: the quota gate's 25% rule (3.5), `Outcome.Quota` (8.1), DV-11, DV-24.
- **Settled by.** DV-02.
- **What settles it.** In every recorded headless run, the field names in the envelope, the stream and stderr, and any field for remaining allowance or reset time (names only).
- **If refuted.** Only the budget, the hold after a rate limit and the idle gate.
- **Result.** Not yet recorded.

#### F-059. A rate limit and a signed-out login are told apart

- **Fact.** When a headless run hits a rate limit or finds the login signed out, or the model asked is not the one that answered, it ends with a structured field that tells these apart from an answer, and a rate limit says when it resets.
- **Needed by.** derive unlisted (3.5, 3.6, 8.1): the `auth` and `rate_limit` classes, the quota hold, D-H3, DV-11, DV-22; evaluate unlisted (5.3, 13.1): the row `status`, EV-15, E-D2.
- **Settled by.** DV-02.
- **What settles it.** One sealed call with one host signed out, then the other: the envelope or stream fields and exit status, scrubbed; a rate-limit answer only if one occurs.
- **If refuted.** Unrecognized output is class `host` and counts toward the breaker; with no reset time the hold is 30 minutes, doubling to 6 hours.
- **Result.** Not yet recorded.

#### F-060. Headless runs report the model that answered

- **Fact.** Each host's headless run reports the model, and the effort, that actually answered, apart from the model asked.
- **Needed by.** store unlisted (5.3, 7.1, 7.2): the commit trailers, `eva-md log` and `why`, TestOKFProfile's actor check; derive unlisted (3.3, 8.1, Codex): the journal's `model_answered`, the policy table's fallback row, replay; evaluate §14 row 12: every row's `model_answered`, stopping on a fallback, cache keys, E-D2.
- **Settled by.** DV-02.
- **What settles it.** In every recorded run on both hosts: the field and event that carry the model and effort, beside what was asked, and whether they ever differ.
- **If refuted.** evaluate: rows marked "model unconfirmed" and kept out of claims.
- **Result.** Not yet recorded.

#### F-061. Usage, cached input apart, before the end of the stream

- **Fact.** Each host's headless output reports tokens in, cached tokens in and tokens out, and its stream carries usage before the final message, so a run killed part way still shows what it used.
- **Needed by.** derive unlisted (8.1, 3.5, 9): the budget gate, cost accounting, DV-13, DV-11; evaluate §14 row 4 (usage half): every Level 2 row, cost (6.8), EV-33.
- **Settled by.** DV-02.
- **What settles it.** In every recorded run, the usage field names and where in the stream they first appear; one made-up run killed after its first event, and what usage it printed.
- **If refuted.** evaluate: take tokens from the final envelope.
- **Result.** Not yet recorded.

#### F-062. The host's own memory can be off for one run

- **Fact.** Each host's own memory can be turned off for one headless run, by a flag, a setting or a variable, without writing to the host's settings (on Claude Code, `--settings '{"autoMemoryEnabled": false}'` or `CLAUDE_CODE_DISABLE_AUTO_MEMORY=1`).
- **Needed by.** derive unlisted (8.1): the sealed run, DV-11; evaluate §14 row 3: the `off`, `eva`, `control`, `oracle` arms, cached baselines, EV-17, EV-33.
- **Settled by.** DV-02.
- **What settles it.** Each host's memory seeded with one made-up line; one sealed call with memory off: whether the answer shows the line, and whether the memory store or the settings changed (by hash).
- **If refuted.** evaluate: a clean config folder per run.
- **Result.** Not yet recorded.

#### F-063. The strict schema mode's rules

- **Fact.** Both hosts' strict schema modes accept a schema in which every key is required and nullable, no object takes another key and there is no `maxLength`, and refuse one that breaks those rules.
- **Needed by.** derive unlisted (5.3): the generated schema, DV-21, DV-03.
- **Settled by.** DV-02.
- **What settles it.** Each host accepts the schema as sent; one made-up copy with a `maxLength` added, and how each host refuses it.
- **If refuted.** None named.
- **Result.** Not yet recorded.

#### F-064. Structured output enforces bounds and enums

- **Fact.** Both hosts' structured-output modes enforce the schema's array bounds, enums and nullable types, so an answer that breaks them is never returned.
- **Needed by.** derive §11 row 8: how rarely the parse fails (5.3), the repair round (5.8), DV-21.
- **Settled by.** DV-03.
- **What settles it.** The count of answers that break a bound, an enum or a nullable type; one made-up script that invites more than 24 operations.
- **If refuted.** The repair round.
- **Result.** Not yet recorded.

#### F-065. A shared prefix is cached

- **Fact.** On each host's headless path, a second call whose input starts with the first call's input, byte for byte, reports most of that shared start as cached input.
- **Needed by.** derive §11 row 9 and 5.8: the cost of repair, the budget weights, DV-03's repair arm, DV-21, risk 7.
- **Settled by.** DV-03.
- **What settles it.** Each repair call run right after its first call: tokens in, cached tokens in and tokens out for both calls, per host and model tier.
- **If refuted.** Repair only refused operations under the user's tag.
- **Result.** Not yet recorded.

#### F-066. Fixed instructions by flag on Claude Code only

- **Fact.** `claude -p` accepts fixed instructions through `--system-prompt`, and `codex exec` has no flag for fixed instructions.
- **Needed by.** derive unlisted (5.1): the prompt layout, DV-21, DV-03.
- **Settled by.** Published source.
- **What settles it.** Each CLI's help text and documentation at the recorded version.
- **If refuted.** None named; if Codex gains such a flag, the boundary line becomes optional.
- **Result.** Not yet recorded.

#### F-067. A headless run with only an API key

- **Fact.** Each host CLI runs headless with only an API key in its environment and no plan login, in a container, bills that key and not the plan, and still accepts the sealed flags.
- **Needed by.** derive unlisted (8.2, DV-D3; the API-key backend, deferred by plan section 12); evaluate unlisted (4.3): EV-D1 and plan decision 28, the Codex driver, EV-17.
- **Settled by.** EV-03.
- **What settles it.** One headless call per host in a throwaway container with an evaluation key: whether it answers, how sign-in was made (method name only), and the billing or key-source field.
- **If refuted.** EV-D1's "if no": the plan login only, suites in the evaluation home, packs halve; derive: plan login only.
- **Result.** Not yet recorded.

#### F-068. Agent sessions stream their tool events

- **Fact.** Claude Code's `--output-format stream-json` and `codex exec --json` emit an event for each tool call.
- **Needed by.** evaluate §14 row 4 (tool half): agent events in every Level 2 row, suites A and C, EV-17, EV-33.
- **Settled by.** EV-03.
- **What settles it.** One short headless session per host that makes one tool call: which event types hold tool calls (field names only).
- **If refuted.** Count tool events from eva-md's own record.
- **Result.** Not yet recorded.

#### F-069. A turn bound, and resumed runs fire the prompt hook

- **Fact.** `--max-turns` bounds a headless run, and `claude -p --resume <id>` and `codex exec resume` continue a session with the prompt hook firing on each resumed prompt.
- **Needed by.** evaluate §14 row 5: task bounds and corrections in suites A and C, EV-17, EV-34.
- **Settled by.** EV-03.
- **What settles it.** A three-prompt scripted session per host with a small turn bound, then one resume; a hook marker per prompt.
- **If refuted.** A wall-clock limit with the process group killed; single-attempt tasks.
- **Result.** Not yet recorded.

#### F-070. A terminal driven through a pseudo-terminal

- **Fact.** Each host's terminal interface can be driven through a pseudo-terminal so that a typed prompt is seen as typed and `/clear` and `/compact` work.
- **Needed by.** evaluate §14 row 10: the canary's interactive path (6.9), driver parity (4.4), EV-D4, EV-22.
- **Settled by.** EV-03.
- **What settles it.** One pty-driven session per host against the recorder: a typed prompt, `/clear`, `/compact`; whether the prompt carries the host's typed marking, and whether the events fired.
- **If refuted.** Those checks stay by hand per release, as in v1.
- **Result.** Not yet recorded.

#### F-071. Hook context reaches the model the same way headless

- **Fact.** Context a hook returns reaches the model the same way in a headless session as in an interactive one.
- **Needed by.** evaluate unlisted (4.4, 6.9 item 4): driver parity, EV-D4, EV-22, risk 2.
- **Settled by.** EV-03.
- **What settles it.** A hook returns a made-up marker at the start and on a prompt; the model asked to repeat it, once headless and once through the pty.
- **If refuted.** Driver parity fails the canary; EV-D4's "if no": pty only, suites shrink by about half.
- **Result.** Not yet recorded.

#### F-072. A headless run with the network closed but for the provider

- **Fact.** Each host CLI runs a headless session in a container whose network is closed except to the model provider.
- **Needed by.** evaluate unlisted (6.5, risk 11): suite I's act stage, EV-31.
- **Settled by.** EV-03.
- **What settles it.** In a throwaway container allowing only the provider's API host, one headless session per host: whether it completes, and which other hosts it tried (names only).
- **If refuted.** None named.
- **Result.** Not yet recorded.

### The evaluation home

#### F-073. Each host signs in under another HOME and stays signed in

- **Fact.** With `HOME` set to another folder, each host signs in there and stays signed in there across runs and days.
- **Needed by.** evaluate §14 row 8: the evaluation home (4.3), `eva-eval home init` and `home check`, EV-D1, the canary, every sitting.
- **Settled by.** Sitting 0 (the evaluation home, the machine and the git server). **Mark: later** (the run-book's Part 2, group K).
- **What settles it.** Both hosts signed in with `HOME` set to a folder inside the evaluation account; checked again at the start of each later sitting: still signed in, and their folders made under that `HOME` (names only).
- **If refuted.** A dedicated macOS account.
- **Result.** Not yet recorded.

#### F-074. A sign-in under another HOME leaves the account's own sign-in alone

- **Fact.** Signing a host in with `HOME` set to another folder neither reads nor replaces the sign-in of the account's own home (for example a sign-in kept in the login keychain, which does not follow `HOME`).
- **Needed by.** evaluate unlisted (4.3): the evaluation home, every sitting, the canary; the plan's HF-00 (the real install is never touched).
- **Settled by.** Sitting 0 (the evaluation home, the machine and the git server). **Mark: later** (the run-book's Part 2, group K).
- **What settles it.** Inside the evaluation account only: whether the account's own sign-in still holds after a sign-in under the other `HOME`, and the names (never values) of login keychain items added or changed.
- **If refuted.** A dedicated macOS account, which the run-book uses in any case.
- **Result.** Not yet recorded.

## B. Claude Code

### Hook payloads

#### F-075. A plugin can declare every event v2 uses

- **Fact.** A Claude Code plugin can declare SessionStart, UserPromptSubmit, PreToolUse, PostToolUse, PostToolUseFailure, PostToolBatch, SubagentStart, SubagentStop, Stop, StopFailure, PreCompact, PostCompact, PostModelSwitch and SessionEnd with no tool-name filter, and each fires.
- **Needed by.** ledger unlisted (C-H1): LG-20, HO-20.
- **Settled by.** The Claude Code sitting. **Mark: later**; the run-book's Part 1 records it at no added minute.
- **What settles it.** The recorder declares all of them; which fire during the sitting.
- **If refuted.** None named.
- **Result.** Not yet recorded.

#### F-076. StopFailure fires on a failed turn

- **Fact.** Claude Code fires StopFailure when a turn ends in an error, with the error's type in the payload.
- **Needed by.** ledger unlisted (3.4 `reply` end and `life` `stop_failure`; C-H1): LG-20.
- **Settled by.** The Claude Code sitting. **Mark: later** (the run-book's Part 2, group B).
- **What settles it.** One turn sent with the network off: whether StopFailure fires, and its fields.
- **If refuted.** None named.
- **Result.** Not yet recorded.

#### F-077. PostModelSwitch fires on a model change

- **Fact.** Claude Code fires PostModelSwitch when the model is changed mid-chat.
- **Needed by.** ledger unlisted (C-H1; 3.4 `life` `model_switch`; `ctx`).
- **Settled by.** The Claude Code sitting. **Mark: later** (the run-book's Part 2, group C).
- **What settles it.** One model change: whether the event fires and its field names.
- **If refuted.** None named.
- **Result.** Not yet recorded.

#### F-078. The task tool's payload holds the whole list

- **Fact.** Claude Code's task tool reaches PostToolUse with the whole task list, each task with its id, subject and status.
- **Needed by.** ledger unlisted (3.4 `tasks`; 4.1): the `tasks` kind, the manifest's `tasks` (3.5), LG-23.
- **Settled by.** The Claude Code sitting. **Mark: later** (the run-book's Part 2, group C).
- **What settles it.** Three made-up tasks created and one completed: each task tool's PostToolUse payload, and whether it holds one task or the list.
- **If refuted.** None named.
- **Result.** Not yet recorded.

#### F-079. The entrypoint tells a person's chat from a program's

- **Fact.** Claude Code reports an entrypoint value, in the hook payload or its environment, that tells an interactive chat from one a program runs: `claude -p` gives a value on the program list, and no desktop surface does.
- **Needed by.** ledger unlisted (3.5 `entry`, 3.6): provenance, `pv_false_typed`, LG-13, LG-10's session class; evaluate unlisted (4.3, 4.4): the actor seam, EV-D4, E-L2; evaluate unlisted (10 row 7): the capture allowlist (LG-13).
- **Settled by.** The Claude Code sitting. **Mark: by the field report.**
- **What settles it.** The entrypoint value and where it comes from, for a typed chat in the terminal, a `claude -p` run, and a chat in the desktop app's Code tab.
- **If refuted.** ledger: prompts would be `unknown`. evaluate 10 row 7: a v1 fix and an allowlist fixture first.
- **Result.** Not yet recorded.

#### F-080. UserPromptSubmit also fires for prompts no one typed

- **Fact.** Claude Code's UserPromptSubmit fires for scheduled tasks, subagent reports and messages from other sessions as well as for typed prompts.
- **Needed by.** deliver unlisted (7.6 prompt row): DL-11, V-H2.
- **Settled by.** The Claude Code sitting. **Mark: later**; the run-book's Part 1 records it at no added minute.
- **What settles it.** Which of those kinds fire UserPromptSubmit, where the run-book can make them.
- **If refuted.** None named; fewer such prompts means less to filter.
- **Result.** Not yet recorded.

#### F-081. PreToolUse holds the path or the command

- **Fact.** Claude Code's PreToolUse payload for the edit tools holds the file path, and for the shell tool the command string, in the tool input.
- **Needed by.** deliver unlisted (7.6): DL-14, DL-19.
- **Settled by.** The Claude Code sitting. **Mark: from source.**
- **What settles it.** The PreToolUse payloads of one made-up Edit, one Write and one Bash call.
- **If refuted.** None named.
- **Result.** Not yet recorded.

#### F-082. PostToolBatch names each call of the batch

- **Fact.** Claude Code's PostToolBatch payload names each tool call in the batch, with its input such as the paths read.
- **Needed by.** deliver §12 row 6: DL-21.
- **Settled by.** The Claude Code sitting. **Mark: later** (the run-book's Part 2, group M).
- **What settles it.** The PostToolBatch payload after a turn that reads two made-up files: which fields name the calls and their paths.
- **If refuted.** A synchronous read hook, or no read-time delivery.
- **Result.** Not yet recorded.

#### F-083. A resume's SessionStart says how long it has been

- **Fact.** On a resume, Claude Code's SessionStart payload carries the seconds since the last response and a mark that the prompt cache has probably expired.
- **Needed by.** deliver §12 row 19: DL-10.
- **Settled by.** The Claude Code sitting. **Mark: later**; the run-book's Part 1 records it at no added minute.
- **What settles it.** A made-up session resumed after a few minutes: the SessionStart `resume` payload's field names.
- **If refuted.** The resume brief carries the delta only.
- **Result.** Not yet recorded.

### Transcripts and session files

#### F-084. Transcript rows carry ids and flags

- **Fact.** Claude Code transcript rows carry a row id, a prompt id, a host version, and flags for meta, sidechain, origin and compaction summary.
- **Needed by.** ledger §13 row 7: `typed` evidence (3.6), `pv_false_typed`, LG-13, LG-21, the adapter's canary (6.5).
- **Settled by.** The Claude Code sitting. **Mark: by the field report.**
- **What settles it.** The field names on user, assistant, tool and summary rows, and the flag values (booleans and short enum values only) for a typed prompt and a host-written one.
- **If refuted.** The profile's canary fails and the session falls to the hook floor.
- **Result.** Not yet recorded.

#### F-085. Transcripts hold system prompts and request records

- **Fact.** Claude Code transcripts hold system prompts, tool definitions and request records beside the conversation rows.
- **Needed by.** ledger unlisted (4.2): row classification in LG-21.
- **Settled by.** The Claude Code sitting. **Mark: by the field report.**
- **What settles it.** The row types of the sitting's transcripts, and which hold system prompts, tool definitions or request records (names and sizes only).
- **If refuted.** None named.
- **Result.** Not yet recorded.

#### F-086. Claude Code deletes old transcripts

- **Fact.** Claude Code deletes old transcripts after a retention period, with a setting and a default.
- **Needed by.** ledger unlisted (4.1): the priority of segment capture, LG-33's backfill window (LG-D5).
- **Settled by.** Published source.
- **What settles it.** The host's documents on its cleanup setting and default period.
- **If refuted.** None named.
- **Result.** Not yet recorded.

#### F-087. The memory index is a local file

- **Fact.** Claude Code's memory index for a project is a local file eva-md can find and read.
- **Needed by.** deliver unlisted (14 point 3): DL-23.
- **Settled by.** The Claude Code sitting. **Mark: by the field report.**
- **What settles it.** One made-up note written through the host's memory: the index file's path shape and format (names only).
- **If refuted.** None named; without the file, no overlap count.
- **Result.** Not yet recorded.

### What a hook's output reaches

#### F-088. Injected text: whole up to 10,000 characters

- **Fact.** Claude Code gives the model a hook's added text whole up to 10,000 characters, and for a longer string gives a file path with a 2,000-character preview.
- **Needed by.** deliver §12 row 1: DL-03, DL-09, DL-10, DL-D8, V-H2; hosts unlisted (3.3 hooks row): `host.Caps` (H-V3).
- **Settled by.** The Claude Code sitting. **Mark: later** (the run-book's Part 2, group E).
- **What settles it.** A start hook returns a made-up text of 9,900 and of 10,100 characters, each with a planted word at the end: whether the agent finds the word, and whether the transcript shows a path and a preview.
- **If refuted.** The cap becomes an adapter constant set to what was observed.
- **Result.** Not yet recorded.

#### F-089. PreToolUse context arrives beside the result

- **Fact.** On Claude Code a PreToolUse hook can add context, and it reaches the model beside the tool's result, after the call has run.
- **Needed by.** deliver §12 row 3: DL-14, DL-19, DL-21 (tool time is deferred in plan section 12).
- **Settled by.** The Claude Code sitting. **Mark: later** (the run-book's Part 2, group M).
- **What settles it.** A pre-tool hook on a made-up edit returns a planted word; the word asked for at the next step; where the context sits against the tool result; the hook's time.
- **If refuted.** Deliver after the call instead.
- **Result.** Not yet recorded.

#### F-090. An async hook's context arrives a turn later

- **Fact.** On Claude Code the context an asynchronous hook returns reaches the model only one turn later.
- **Needed by.** deliver unlisted (7.6): DL-21, DL-14's choice of a synchronous hook; hosts unlisted (3.3): H-V3.
- **Settled by.** The Claude Code sitting. **Mark: later** (the run-book's Part 2, group M).
- **What settles it.** An async PostToolUse hook returns a planted word; asked for in the same turn and in the next.
- **If refuted.** None named; async delivery could then be used here too.
- **Result.** Not yet recorded.

#### F-091. A subagent gets instruction files and not the start context

- **Fact.** A subagent on Claude Code gets the instruction-file hierarchy and nothing a SessionStart hook injected.
- **Needed by.** deliver unlisted (7.6 Subagents): DL-15, DL-D7.
- **Settled by.** The Claude Code sitting. **Mark: later** (the run-book's Part 2, group C).
- **What settles it.** SessionStart injects a planted word and the folder's instruction file holds another; the main agent and then a subagent asked for both.
- **If refuted.** None named; the subagent brief may be redundant.
- **Result.** Not yet recorded.

#### F-092. A hook past its timeout shows a notice

- **Fact.** Claude Code shows the person a notice when a hook runs past its timeout.
- **Needed by.** deliver §12 row 15: DL-11, DL-09's late path, the internal deadlines.
- **Settled by.** The Claude Code sitting. **Mark: later** (the run-book's Part 2, group E).
- **What settles it.** A hook that sleeps past its declared timeout: what the person sees.
- **If refuted.** None needed; the deadlines stay well under the timeout.
- **Result.** Not yet recorded.

#### F-093. An MCP tool can load at start

- **Fact.** On Claude Code an MCP tool can ask to be loaded at start, not deferred behind tool search.
- **Needed by.** deliver §12 row 13 part 1: DL-12, V-H3.
- **Settled by.** The Claude Code sitting. **Mark: later** (the run-book's Part 2, group F).
- **What settles it.** A scratch tool marked to load at start: whether it is in the agent's tool list at the first turn or only after a tool search.
- **If refuted.** The brief's legend names the tool.
- **Result.** Not yet recorded.

#### F-094. A user-level rules file with a path pattern

- **Fact.** A user-level Claude Code rules file with a `paths:` pattern applies in every project with a matching folder; and it is found whether the pattern can be absolute, when the file reloads, whether subagents get it, and whether it survives a compaction.
- **Needed by.** deliver §12 row 20: DL-24, DL-D1 (generated rules files are deferred in plan section 12).
- **Settled by.** The Claude Code sitting. **Mark: later** (the run-book's Part 2, group M).
- **What settles it.** One rules file with a path pattern and a planted word; two made-up projects with that folder, a subagent and a compacted chat: where the word is known.
- **If refuted.** The experiment is dropped.
- **Result.** Not yet recorded.

#### F-095. A pre-tool hook can ask, and Codex cannot

- **Fact.** A Claude Code PreToolUse hook can answer with the host's own permission question and a reason the person sees, a subagent included; Codex offers no such answer.
- **Needed by.** deliver unlisted (8, DL-D2): DL-19's ask tier; evaluate unlisted (6.4): suite G, claim row 8; plan section 12 ("Claude Code only").
- **Settled by.** Published source.
- **What settles it.** Each host's hook documentation and source at the recorded version: the decision values a pre-tool hook may return.
- **If refuted.** Warn only (DL-D2's "if no").
- **Result.** Not yet recorded.

#### F-096. A hook can wake an idle session

- **Fact.** A hook can wake an idle Claude Code session to deliver late context.
- **Needed by.** deliver unlisted (7.6): nothing built; it explains a choice not taken.
- **Settled by.** Published source.
- **What settles it.** The host's hook documentation at the recorded version.
- **If refuted.** None needed.
- **Result.** Not yet recorded.

#### F-097. The hook reference: events, cap and async

- **Fact.** Claude Code has 33 hook events, caps a hook's added text at 10,000 characters a string, and delivers an async hook's context a turn later.
- **Needed by.** hosts unlisted (3.3 capability table): `host.Caps` (H-V3), deliver's V-H2.
- **Settled by.** Published source.
- **What settles it.** Claude Code's hook reference at the recorded version (the cap and the async timing are also observed in the sitting).
- **If refuted.** None named.
- **Result.** Not yet recorded.

### Compaction, forks and resume

#### F-098. PostCompact carries the summary

- **Fact.** Claude Code's PostCompact payload carries the compaction summary text in `compact_summary`.
- **Needed by.** ledger §13 row 5 (Claude Code): the `summary` kind, 4.1, LG-20, LG-23; deliver §12 row 8 part 2: DL-16, DL-10.
- **Settled by.** The Claude Code sitting. **Mark: later** (the run-book's Part 2, group E).
- **What settles it.** One `/compact`: whether PostCompact fires and whether the summary field is there and not empty (its size only).
- **If refuted.** Presumably the record's compaction-summary row; the paper names none for this host.
- **Result.** Not yet recorded.

#### F-099. PreCompact steers the summary

- **Fact.** On Claude Code the output of a PreCompact hook steers what the compaction summary keeps.
- **Needed by.** deliver §12 row 8 part 1: DL-16.
- **Settled by.** The Claude Code sitting. **Mark: later** (the run-book's Part 2, group E).
- **What settles it.** A compaction while PreCompact prints a planted instruction to keep a made-up path; whether the summary keeps it (asked of the agent).
- **If refuted.** No steering.
- **Result.** Not yet recorded.

#### F-100. PostCompact fires before SessionStart `compact`

- **Fact.** On Claude Code PostCompact fires before SessionStart with source `compact`.
- **Needed by.** deliver §12 row 8 part 3: DL-09, DL-16.
- **Settled by.** The Claude Code sitting. **Mark: later** (the run-book's Part 2, group E).
- **What settles it.** The arrival order and times of PreCompact, PostCompact and SessionStart `compact` in one compaction.
- **If refuted.** The complement does not depend on the order.
- **Result.** Not yet recorded.

#### F-101. SessionStart `compact` output joins the new context

- **Fact.** On Claude Code the output of a SessionStart `compact` hook joins the compacted context the model sees.
- **Needed by.** deliver §12 row 8 part 4: DL-16, DL-10, DL-09.
- **Settled by.** The Claude Code sitting. **Mark: later** (the run-book's Part 2, group E).
- **What settles it.** The SessionStart `compact` hook returns a planted word; after the compaction, the word asked for.
- **If refuted.** None named; this is the design's only re-injection point.
- **Result.** Not yet recorded.

#### F-102. Session end gives hooks 1.5 seconds

- **Fact.** At session end, Claude Code gives all plugin hooks 1.5 seconds in total before it stops them.
- **Needed by.** derive §11 row 11 (Claude Code half): the `ended` rule (3.4), P2, `end_to_saved`, DV-24.
- **Settled by.** The Claude Code sitting. **Mark: later** (the run-book's Part 2, group G).
- **What settles it.** The SessionEnd hook writes a line every 100 ms for 5 seconds; a chat ended by `/exit` and one by closing the terminal window: the last line's time after the hook started.
- **If refuted.** The `idle` rule covers it.
- **Result.** Not yet recorded.

### Plugins, install and trust

#### F-103. A changed hooks file is used with no review

- **Fact.** A changed hooks file in a local-marketplace Claude Code plugin is used at the next session with no approval or review step.
- **Needed by.** hosts §14 #9 first half: the single Claude Code unit (4.2, 4.6), content releases (10.3).
- **Settled by.** The Claude Code sitting. **Mark: later** (the run-book's Part 2, group I).
- **What settles it.** One timeout changed in the scratch plugin's hooks file and the plugin updated: whether any prompt appears and whether the hook fires under the new declaration.
- **If refuted.** Claude Code gets the Codex split.
- **Result.** Not yet recorded.

#### F-104. Hooks run in exec form with the plugin root expanded

- **Fact.** Claude Code runs a plugin hook command in exec form with no shell, and expands `${CLAUDE_PLUGIN_ROOT}` in it.
- **Needed by.** hosts unlisted (4.6, 11.4): HO-42.
- **Settled by.** The Claude Code sitting. **Mark: later** (the run-book's Part 2, group I).
- **What settles it.** A hook declared in exec form naming `${CLAUDE_PLUGIN_ROOT}/scripts/...`: whether it ran, and its parent process.
- **If refuted.** None named.
- **Result.** Not yet recorded.

#### F-105. The desktop app's Code tab runs its own Claude Code

- **Fact.** The Claude desktop app's Code tab reads and writes the same files as the CLI but runs its own bundled Claude Code, whose version may differ.
- **Needed by.** hosts unlisted (11.1, 3.3; risk 9; field question 11): HO-10's spec ranges, capture from the Code tab.
- **Settled by.** The Claude Code sitting. **Mark: later** (the run-book's Part 2, group A).
- **What settles it.** A made-up chat in the Code tab: whether the scratch plugin's hooks fire, the version the record names, and the CLI's version.
- **If refuted.** None named.
- **Result.** Not yet recorded.

#### F-106. A top-level bin folder stops install in Chat and Cowork

- **Fact.** A top-level `bin/` folder stops a Claude plugin from installing in Chat and Cowork, while executables under `scripts/` do not.
- **Needed by.** hosts unlisted (4.1 row 10, 4.6, 11.4): the portable plugin, HO-42, HO-44.
- **Settled by.** Published source.
- **What settles it.** Claude's plugin reference at the date of the sitting.
- **If refuted.** None named.
- **Result.** Not yet recorded.

#### F-107. A third-party marketplace does not auto-update

- **Fact.** A third-party Claude Code marketplace does not auto-update by default.
- **Needed by.** hosts unlisted (4.1 row 12, 4.6).
- **Settled by.** Published source.
- **What settles it.** Claude's marketplace documentation.
- **If refuted.** None named.
- **Result.** Not yet recorded.

#### F-108. A desktop extension bundle may name a native command

- **Fact.** A desktop extension bundle for the Claude desktop app may name a native command as its server.
- **Needed by.** hosts §14 #12: the Chat tab floor (11.3), HO-41, plan section 12's floor.
- **Settled by.** Published source.
- **What settles it.** The bundle format's manifest reference.
- **If refuted.** The printed entry only.
- **Result.** Not yet recorded.

#### F-109. Chat on the web takes only skills and remote connectors

- **Fact.** Chat on the web and on mobile takes only skills and remote connectors from a plugin.
- **Needed by.** hosts unlisted (11.5): HO-D6.
- **Settled by.** Published source.
- **What settles it.** Claude's plugin documentation.
- **If refuted.** None named.
- **Result.** Not yet recorded.

#### F-110. An admin can deploy a managed marketplace

- **Fact.** An administrator can deploy a managed marketplace and enabled-plugin entries for Claude Code that a user plugin then runs under.
- **Needed by.** hosts unlisted (12): HO-43, HO-D8.
- **Settled by.** Published source.
- **What settles it.** Claude Code's managed settings documentation.
- **If refuted.** None named.
- **Result.** Not yet recorded.

#### F-111. The sign-in command

- **Fact.** `claude auth login` is the command that signs Claude Code in again.
- **Needed by.** hosts unlisted (7.5): the ask wording table.
- **Settled by.** Published source.
- **What settles it.** `claude --help` at the recorded version.
- **If refuted.** None named.
- **Result.** Not yet recorded.

### Headless runs

#### F-112. The sealed `claude -p` call runs from a background job

- **Fact.** `claude -p` started from a detached background job, on the plan login and no API key, accepts `--safe-mode --tools "" --strict-mcp-config --no-session-persistence --json-schema`, a model alias and `--effort`, and returns an answer shaped by the schema.
- **Needed by.** derive §11 row 1: the default Claude backend (8.2), DV-11, DV-24, DV-D2 (plan decision 5), DV-03's frontier rows.
- **Settled by.** DV-02.
- **What settles it.** One sealed call on a made-up script from a detached job, with a small model at low effort and a larger one at medium (the lead names both): CLI version, exit status, envelope field names, model reported, schema match; any refusal's shape, scrubbed.
- **If refuted.** The API-key backend (DV-D3); without it, the chat's model at low effort.
- **Result.** Not yet recorded.

#### F-113. The stream's opening message names tools and model

- **Fact.** With streaming JSON output, `claude -p` first prints an opening message naming the tools on offer, the model and where its key came from, and its last message holds the structured answer.
- **Needed by.** derive §11 row 2: UPM-14's checks (8.1), the `tool_used` class, the journal's `model_answered`, DV-11.
- **Settled by.** DV-02.
- **What settles it.** The same sealed call streamed, saved with values replaced: the first message's type and field names, whether its tool list is empty, and which message carries the answer.
- **If refuted.** Keep the single envelope, read `permission_denials`, and take the model from the keys of `modelUsage`.
- **Result.** Not yet recorded.

#### F-114. `--bare` is not the default, and safe mode keeps the plan login

- **Fact.** `--bare` is not yet the default of `claude -p`; a run with `--safe-mode` still uses the plan login; and when bare becomes the default a flag keeps plugins and hooks loaded.
- **Needed by.** derive §11 row 3: the default backend, DV-D3, the probe, the `auth` class; evaluate §14 row 2: the headless driver and arms, EV-03, EV-17, the canary EV-22 (checked again at every host release).
- **Settled by.** DV-02.
- **What settles it.** From the recorded `--safe-mode` run: that it completed on the plan login and the field that says so; `claude --help`'s text for `--bare`, with the CLI version.
- **If refuted.** The breaker opens on `auth` and DV-D3's key backend takes over where a key is set; evaluate: the evaluation home on the plan login.
- **Result.** Not yet recorded.

#### F-115. `claude -p` under an API key fires a plugin's hooks

- **Fact.** `claude -p` run without `--bare`, signed in only with an API key, loads a plugin given by `--plugin-dir` and fires its hooks.
- **Needed by.** evaluate §14 row 1 and §10 row 15: the headless driver and the arms on Claude Code (4.4, 4.5), EV-03, EV-17, E-H1, EV-D1, plan decision 28.
- **Settled by.** EV-03.
- **What settles it.** In a throwaway container with an evaluation key only, a scratch plugin whose every hook writes a marker: the CLI version and which events wrote one.
- **If refuted.** Suites move to the evaluation home on the plan login and the pty driver; packs halve.
- **Result.** Not yet recorded.

#### F-116. Memory writes in a pty-driven session

- **Fact.** Claude Code's own memory writes memory files during an interactive session driven through a pseudo-terminal.
- **Needed by.** evaluate §14 row 9: the `host` arm, EV-17's baselines, claim row 3, EV-D5.
- **Settled by.** EV-03.
- **What settles it.** One pty-driven session in the evaluation home that states made-up facts: the count of files in the host's memory folder before and after.
- **If refuted.** The `host` arm is run by hand per claim release.
- **Result.** Not yet recorded.

#### F-117. `claude plugin eval` exists

- **Fact.** A `claude plugin eval` command exists and offers a no-plugin baseline.
- **Needed by.** evaluate §14 row 11 and §16: a possible driver for the `off` and `eva` arms.
- **Settled by.** EV-03.
- **What settles it.** The command's help at the recorded version and one run on a scratch plugin.
- **If refuted.** The harness's own driver.
- **Result.** Not yet recorded.

### The host's own memory

#### F-118. Auto memory is on by default

- **Fact.** Claude Code's auto memory is on by default in an interactive, signed-in session, keeps notes per repository on one machine, loads an index of up to 200 lines or 25 KB into every session, reloads it after compaction, and is where "remember this" goes.
- **Needed by.** deliver unlisted (14): DL-23, DL-D4, V-E1; evaluate unlisted (4.5): what the `eva` and `host` arms mean, section 10 row 11, claim row 3.
- **Settled by.** Published source.
- **What settles it.** The host's memory documentation at the recorded version; the setting's value in a fresh sign-in, read without changing it.
- **If refuted.** None named.
- **Result.** Not yet recorded.

#### F-119. Auto memory is off in containers

- **Fact.** Claude Code's own memory is off in self-hosted container environments and cannot be turned on from a background session.
- **Needed by.** evaluate unlisted (4.3): the `host` arm placed in the evaluation home, EV-17, section 9's budget.
- **Settled by.** Published source.
- **What settles it.** The host's documentation at the recorded version.
- **If refuted.** None named; the `host` arm could then run in containers too.
- **Result.** Not yet recorded.

## C. Codex

### Hook payloads

#### F-120. A plugin can declare every event v2 uses

- **Fact.** A Codex plugin can declare, and Codex fires, SessionStart, UserPromptSubmit, PreToolUse, PostToolUse for every tool, SubagentStart, SubagentStop, Stop, PreCompact, PostCompact, Interrupt and SessionEnd, with the sync or async, timeout and context-limit values of the hosts paper's generation 1 table and no matcher.
- **Needed by.** ledger unlisted (C-H1; 6.1 Interrupt): HO-20, LG-20, LG-21; hosts §14 #6 first half: generation 1, HO-30, H-V5, plan HO-20 and LG-23; hosts unlisted (4.3 generation 1 table): HO-30, H-V5.
- **Settled by.** The Codex sitting. **Mark: from source.**
- **What settles it.** The recorder declares them all; which arrived and with what turn ids, Interrupt on an interrupted turn and PostToolUse on a tool that is not the shell included.
- **If refuted.** hosts: a row that does not fire leaves generation 1 before the freeze.
- **Result.** Not yet recorded.

#### F-121. The typed-prompt mark in the record

- **Fact.** Codex's record of what started a turn carries a mark that tells a typed prompt from a voice hand-over, a subagent and a scheduled run.
- **Needed by.** plan's sitting list item 1 and 6.1; ledger plan item: LG-13, LG-21, the plan's 95% threshold, the second report; deliver plan item: DL-11; derive plan item: the tags of 5.5, `skipped: no_user_turn`, DV-24; evaluate plan item: the actor seam on Codex (EV-D4), EV-17.
- **Settled by.** The Codex sitting. **Mark: core** (the run-book's Part 1).
- **What settles it.** The turn-start row's type, field names and enum-shaped values for one typed prompt (in the CLI and in the desktop app), one program's run, one voice hand-over and one scheduled run where the app offers one; the field that differs. The recorder keeps `turn_attribution.turn_trigger` and the turn-start row's other enum values (the card's item 2). The subagent case is read in Codex's published source (0.167.0): a subagent's first turn is started with its parent turn's `turn_trigger`, so it carries the same mark as the turn that spawned it, and is told apart by `turn_attribution.parent_turn_id`, by a `root_turn_id` that differs from its `turn_id`, and by its thread's source, a subagent's. The second report therefore counts typed turns only where no parent turn is named, in threads whose source is not a subagent's. Part 2's X-12 (group C) confirms it on the host if the report finds the source wrong.
- **If refuted.** Plan section 8: under 95% proved typed, on Codex only, "typed unless it carries a known wrapper", every such prompt counted; a prompt whose record cannot be read is unknown.
- **Result.** Not yet recorded.

#### F-122. That row is on disk when the prompt hook fires

- **Fact.** The Codex row that records what started a turn is already on disk when the prompt hook fires.
- **Needed by.** plan's sitting list item 1; ledger plan item: LG-21, LG-13; deliver and evaluate plan items.
- **Settled by.** The Codex sitting. **Mark: from source.**
- **What settles it.** At each UserPromptSubmit, whether the turn's start row is already in the rollout.
- **If refuted.** None named; the worker reads it later (LG-21).
- **Result.** Not yet recorded.

#### F-123. PreToolUse adds context, and an edit matcher matches `apply_patch`

- **Fact.** Codex's PreToolUse hook can add context through its JSON output, an Edit or Write matcher matches `apply_patch`, and the tool input holds the patch text with its file headers and, for the shell tool, the command string.
- **Needed by.** deliver §12 row 4: DL-14, DL-19, V-H1, HO-20.
- **Settled by.** The Codex sitting. **Mark: later** (the run-book's Part 2, group H).
- **What settles it.** PreToolUse payloads for one made-up `apply_patch` edit and one shell command, with an Edit|Write matcher and a shell matcher declared; which matcher fired; the hook returns a planted word, asked for after.
- **If refuted.** Parse what the payload holds; if nothing usable, no tool-time delivery on Codex.
- **Result.** Not yet recorded.

#### F-124. An async PostToolUse's context arrives before the next request

- **Fact.** On Codex the context an asynchronous PostToolUse hook returns reaches the model before the next model request in the same turn.
- **Needed by.** deliver §12 row 5: DL-21; hosts §14 #6 second half: the PostToolUse row of 4.3, H-V3, deliver's V-H1.
- **Settled by.** The Codex sitting. **Mark: later** (the run-book's Part 2, group H).
- **What settles it.** The agent prints a made-up file; an async PostToolUse hook returns a planted word; asked for in the same turn.
- **If refuted.** No read-time delivery on Codex; the row leaves generation 1 before the freeze.
- **Result.** Not yet recorded.

#### F-125. A hook message: about 2,500 tokens, 3,000 declared

- **Fact.** Codex gives the model a hook message up to about 2,500 tokens, up to 3,000 when the plugin declares that limit, and writes the rest to a file.
- **Needed by.** deliver §12 row 2: DL-03, DL-09, DL-10, V-H1, HO-20.
- **Settled by.** The Codex sitting. **Mark: from source.**
- **What settles it.** The planted-word probe at 7,000, 8,300, 11,000 and 14,000 characters, each once with the declared limit and once without, so the ladder straddles both limits whether Codex counts about three or about four characters a token: whether the word reaches the model, and the count of spill files at each size (a count only). The limit is recorded as found, between the last size that came whole and the first that did not.
- **If refuted.** Lower the Codex cap.
- **Result.** Not yet recorded.

#### F-126. Session end on close and after 30 idle minutes

- **Fact.** Codex fires its session-end hook when a chat is closed, and after 30 idle minutes in a chat left open.
- **Needed by.** derive §11 row 11 (Codex half): the `ended` rule, P2, `end_to_saved`, DV-24.
- **Settled by.** The Codex sitting. **Mark: by the field report.**
- **What settles it.** One made-up chat closed, and one left open and idle for 35 minutes with its hooks approved throughout, in the CLI and in the desktop app (a thread left untouched until the app is quit): whether and when the end hook fired.
- **If refuted.** The `idle` rule covers it.
- **Result.** Not yet recorded.

#### F-127. A managed worktree as the working folder

- **Fact.** The `cwd` in a Codex hook payload may be a managed worktree on a detached HEAD.
- **Needed by.** ledger §13 row 14: project identity (8.2), `identity_continuity`, LG-22.
- **Settled by.** The Codex sitting. **Mark: by the field report.**
- **What settles it.** A desktop-app task started in a worktree of the scratch repository: the hook's `cwd`, whether HEAD is detached there, and the git common directory.
- **If refuted.** None named.
- **Result.** Not yet recorded.

#### F-128. Codex has no file-read tool

- **Fact.** Codex has no file-read tool a hook matcher can match; it reads files through the shell tool.
- **Needed by.** deliver unlisted (7.6, 15 point 10): DL-14, DL-21.
- **Settled by.** Published source.
- **What settles it.** Codex's published tool list at the recorded version; the tool a read used in the sitting is kept beside it.
- **If refuted.** None named; a read tool would make read-time delivery simpler.
- **Result.** Not yet recorded.

#### F-129. PreCompact cannot steer the summary

- **Fact.** On Codex a PreCompact hook can only allow or stop a compaction and cannot steer the summary.
- **Needed by.** deliver unlisted (7.5, 7.6): DL-16 (steering is Claude Code's only).
- **Settled by.** Published source.
- **What settles it.** Codex's hook documentation at the recorded version.
- **If refuted.** None needed; steering would be an added arm.
- **Result.** Not yet recorded.

#### F-130. The statusMessage and other events

- **Fact.** On Codex a hook's `statusMessage` is shown in the interface, and the host has a PermissionRequest event and two model-request events.
- **Needed by.** hosts unlisted (4.3): what the declaration leaves out.
- **Settled by.** Published source.
- **What settles it.** Codex's hook documentation and source at the recorded version.
- **If refuted.** None named.
- **Result.** Not yet recorded.

#### F-131. An `mcp_tool` hook marks outside context

- **Fact.** On Codex an `mcp_tool` hook marks the thread as having used outside context, which can keep it out of Codex's own memory, and since 0.164.0 such a hook can hold an event for its whole timeout.
- **Needed by.** deliver unlisted (14 point 4, 15 point 4): nothing built; the choice not to build a warm server.
- **Settled by.** Published source.
- **What settles it.** Codex's source or changelog at the recorded version.
- **If refuted.** None needed.
- **Result.** Not yet recorded.

### Transcripts and session files

#### F-132. The rollout is written in a known history mode

- **Fact.** The Codex version in use writes a rollout file, in a history mode eva-md's profile can read.
- **Needed by.** ledger §13 row 12 part a: the Codex record profile, LG-21, LG-03's fixtures.
- **Settled by.** The Codex sitting. **Mark: by the field report.**
- **What settles it.** The version Codex reports, the history setting in force, and the rollout's row types and field names.
- **If refuted.** Compressed and non-local threads are `unavailable`; the hook floor applies.
- **Result.** Not yet recorded.

#### F-133. Rollout rows have a stable position

- **Fact.** Codex rollout rows carry an ordinal, an id or a time that gives each row a stable position.
- **Needed by.** ledger §13 row 12 part b: LG-21's order and dedupe, `order_agreement`, `dupes`.
- **Settled by.** The Codex sitting. **Mark: by the field report.**
- **What settles it.** Whether rows carry a sequence number, id or time, and whether the values strictly increase.
- **If refuted.** Position in the file; otherwise the hook floor.
- **Result.** Not yet recorded.

#### F-134. The opening row names the session and folder, and can be large

- **Fact.** A Codex rollout's opening row names the session id and the working folder, and can be larger than 64 KiB because it holds instructions.
- **Needed by.** ledger unlisted (6.2 step 2): the Codex record's trust check, LG-21.
- **Settled by.** The Codex sitting. **Mark: by the field report.**
- **What settles it.** The opening row's field names and size in bytes, in a folder with long made-up instructions.
- **If refuted.** None named.
- **Result.** Not yet recorded.

#### F-135. PostCompact carries only the trigger

- **Fact.** Codex's PostCompact payload carries only the trigger and no summary text.
- **Needed by.** ledger §13 row 5 (Codex): the `summary` kind on Codex, LG-23, LG-21.
- **Settled by.** The Codex sitting. **Mark: from source.**
- **What settles it.** One compaction: the PostCompact payload's field names.
- **If refuted.** The summary from the record, or not at all.
- **Result.** Not yet recorded.

#### F-136. The rollout holds the compaction summary

- **Fact.** Codex writes its compaction summary into its own rollout file, as a row eva-md can read.
- **Needed by.** ledger unlisted (4.1; 3.4 `summary` from the record): LG-21, LG-23; deliver unlisted (7.5): DL-16, the ledger's summary event.
- **Settled by.** The Codex sitting. **Mark: by the field report.**
- **What settles it.** After a compaction, the rollout's row types around it and whether one holds summary text (its size only).
- **If refuted.** Not at all (ledger §13 row 5's fallback); the complement does not need it.
- **Result.** Not yet recorded.

#### F-137. Desktop threads name a session file eva-md can read

- **Fact.** Threads in Codex's desktop app name a session file, under Codex's folders, that eva-md can read.
- **Needed by.** plan's sitting list item 3; ledger plan item: LG-21's record reader, the voice trigger of section 12; hosts plan item: the activity scan (7.2), field question 11; deliver and derive plan items.
- **Settled by.** The Codex sitting. **Mark: by the field report.**
- **What settles it.** A short chat in the desktop app: whether the hooks name a transcript path, and whether the file is in the store root and readable (path shape only).
- **If refuted.** The session is `unavailable`.
- **Result.** Not yet recorded.

#### F-138. A thread opened and never run ends with no start

- **Fact.** A Codex thread that is opened and never run fires a session-end hook with no session start, and its file holds no turn.
- **Needed by.** plan's sitting list item 3 and 13.5; ledger plan item: LG-10's session class, `life`; deliver plan item: DL-09, DL-10; derive plan item: the `ended` rule, skip coverage, `ranges_unaccounted`.
- **Settled by.** The Codex sitting. **Mark: by the field report.**
- **What settles it.** An older thread opened in the desktop app and closed without running anything, and a new thread left unsent: which hooks fire, and whether its file holds any turn (row types only).
- **If refuted.** None named.
- **Result.** Not yet recorded.

#### F-139. Rollouts are compressed after about a week

- **Fact.** Codex compresses rollout files after about 7 days, so a session's record may be found compressed.
- **Needed by.** ledger §13 row 12 part c and §15: the Codex profile, LG-21, LG-33's backfill (LG-D5).
- **Settled by.** Published source.
- **What settles it.** Codex's source on when and how rollouts are compressed; the evaluation home's rollout folder listed for any compressed file.
- **If refuted.** Compressed threads are `unavailable`; the hook floor applies.
- **Result.** Not yet recorded.

#### F-140. A thread API that reads a thread's history

- **Fact.** Codex offers a documented app-server thread API that can read a thread's history.
- **Needed by.** ledger unlisted (§15 option b; risk 4): the remedy if R4 costs too much on Codex.
- **Settled by.** Published source.
- **What settles it.** Codex's published documents and source for the thread API.
- **If refuted.** None named.
- **Result.** Not yet recorded.

### Voice

#### F-141. A hand-over reaches the prompt hook as a wrapper

- **Fact.** Each voice hand-over in Codex's desktop app fires UserPromptSubmit with a wrapper that holds a request and the lines each side spoke, and that can be told from a typed prompt.
- **Needed by.** plan's sitting list item 2 and decisions 29 and 30; deliver plan item: DL-11, DL-04; evaluate plan item: EV-14's voice families, DV-03's spoken scripts, EV-17; ledger: LG-13.
- **Settled by.** The Codex sitting. **Mark: by the field report.**
- **What settles it.** Every hook payload of two made-up voice calls, with its turn id, and the session file's row types.
- **If refuted.** deliver: by plan LG-13 no hand-over yields a typed event, so a hand-over gets no prompt push.
- **Result.** Not yet recorded.

#### F-142. The request is the front model's wording

- **Fact.** In the voice version the app uses, a hand-over's request is the front model's wording, not the person's transcribed words.
- **Needed by.** plan's decision 29 and 6.1; ledger plan item: LG-13, DV-21, DV-23, DV-24, EV-14, DV-03; derive plan item: the tags of 5.5, DV-21.
- **Settled by.** The Codex sitting. **Mark: core** (the run-book's Part 1).
- **What settles it.** A request said in known made-up words; the hand-over's request field compared with what was said; the app's version from its About window, the voice setting the app shows, and any version field the wrapper or the session file carries.
- **If refuted.** None needed for authority: under decision 29 the front model's wording never carries the person's authority. Only its label changes.
- **Result.** Not yet recorded.

#### F-143. Each spoken line keeps its speaker

- **Fact.** A hand-over keeps each spoken line under its speaker, apart from the request, including a spoken correction, a spoken yes to a spoken proposal, and the pause phrase spoken.
- **Needed by.** plan's sitting list item 2, decision 29, LG-24's voice clause; derive plan item: the `accepted` tag, DV-D4, DV-03, DV-21, DV-23; ledger plan item: LG-24.
- **Settled by.** The Codex sitting. **Mark: core** (the run-book's Part 1).
- **What settles it.** A proposal answered yes aloud, a made-up value corrected aloud, and "off the record" spoken before a hand-over: where each line sits in the payload and under which speaker.
- **If refuted.** Decision 29's "if no": speech counts below typing, and a decision made by voice stays a proposal until typed or confirmed.
- **Result.** Not yet recorded.

#### F-144. A task voice starts by itself begins inside the wrapper

- **Fact.** A task that voice starts in a new thread gets its first prompt inside the voice wrapper.
- **Needed by.** plan's sitting list item 2 and 6.3 rule 1; ledger plan item: LG-13, LG-10's session class; derive plan item: `skipped: no_user_turn`, DV-24.
- **Settled by.** The Codex sitting. **Mark: core** (the run-book's Part 1).
- **What settles it.** A voice call started in a new, empty thread that asks for a task: the first UserPromptSubmit payload and whether it is wrapper-shaped.
- **If refuted.** None named.
- **Result.** Not yet recorded.

#### F-145. A hand-over while the agent works shares its turn's id

- **Fact.** A voice hand-over made while the agent is still working fires a prompt hook that shares that turn's id.
- **Needed by.** plan's sitting list item 2; ledger plan item: LG-21, LG-13, LG-10's key; derive plan item: episode cuts, DV-20.
- **Settled by.** The Codex sitting. **Mark: from source.**
- **What settles it.** A second hand-over asked for while the agent works on the first: whether a prompt hook fires, and both turn ids.
- **If refuted.** None named.
- **Result.** Not yet recorded.

#### F-146. Each field of a hand-over holds at most 4 KiB

- **Fact.** Each field of a voice hand-over is limited to 4 KiB, so a longer stretch of talk loses its start.
- **Needed by.** plan's decision 30: what eva.md states it captures.
- **Settled by.** The Codex sitting. **Mark: from source.**
- **What settles it.** Talk with no hand-over well past the limit (a made-up story of 8,626 bytes, read aloud), then a hand-over: the field sizes, the talk's byte count, and whether the stretch's first made-up line is missing.
- **If refuted.** None named.
- **Result.** Not yet recorded.

#### F-147. Talk past the limit and the call's end are rows no hook saw

- **Fact.** Talk past the hand-over limit, and the end of a call, are written to the session file as wrapper-shaped rows that no hook saw.
- **Needed by.** plan's sitting list item 2 and section 12; ledger plan item: decision 30's second option, LG-21, the threshold of a fifth of a call's lines; derive plan item.
- **Settled by.** The Codex sitting. **Mark: by the field report.**
- **What settles it.** After the long stretch and after each call's end (normal and by quitting the app): the session file's row types, and the count of wrapper-shaped rows with no matching payload.
- **If refuted.** Decision 30's recommendation: keep only what each hand-over carries, and say so.
- **Result.** Not yet recorded.

#### F-148. Nothing after the last hand-over reaches a hook

- **Fact.** Nothing said in a voice call after the last hand-over reaches any hook, whether the call ends normally or the app is quit.
- **Needed by.** plan's decision 30: the capture statement.
- **Settled by.** The Codex sitting. **Mark: by the field report.**
- **What settles it.** Made-up lines said after the last hand-over, then the call ended, once normally and once by quitting the app: every hook that fires afterwards and its fields.
- **If refuted.** None named.
- **Result.** Not yet recorded.

#### F-149. The session file holds a line for each utterance

- **Fact.** The desktop app's session file holds a line for each utterance of a voice call, local, uncompressed and in a form eva-md can read.
- **Needed by.** plan section 12 ("A voice call's lines read from the host's own record") and decision 30.
- **Settled by.** The Codex sitting. **Mark: by the field report.**
- **What settles it.** The count of speech rows in the session file against the script's lines, and whether the file is local, uncompressed and of a known format (row types only).
- **If refuted.** Decision 30's first option: keep only what hand-overs carry.
- **Result.** Not yet recorded.

### Plugins, install and trust

#### F-150. Approval is bound to every byte of the hooks plugin

- **Fact.** Codex checks a plugin's hook approval against a hash of every file in its root: the approval holds across restarts and days while no byte changes, and a change to any file, a skill file alone included, sends every hook of that plugin back to review, with capture stopped until then.
- **Needed by.** hosts unlisted (4.1 row 1): the frozen unit (4.2, 4.3), HO-20, HO-30, SEAM-0's hash test; hosts plan item (a skill inside the hooks plugin): HO-20, the threshold "Codex reviews needed after upgrades"; deliver and evaluate plan items ("a skill file alone"); evaluate §14 row 7: the Codex canary tier, EV-22, R2, section 10 row 10; ledger C-H1.
- **Settled by.** The Codex sitting. **Mark: by the field report.**
- **What settles it.** Approval checked after the days between sittings; then one byte of a skill file in the approved hooks plugin changed and the plugin installed again: `hooks/list`, whether `/hooks` asks, and whether a hook fires before the new approval.
- **If refuted.** One review per plugin change, counted as a canary result; the split into two plugins is the first hosts package in Stage 2.
- **Result.** Not yet recorded.

#### F-151. A change to a plugin with no hooks costs no review

- **Fact.** On Codex, changing any byte of a second plugin that declares no hooks leaves every hook of the separate hooks plugin trusted, with no new review.
- **Needed by.** hosts §14 #1: the unit split (4.2), HO-20, HO-30, H-V4, H-V5, `codex_reapprovals_per_release`; deliver unlisted (18 point 5): DL-12 (tool names in the content plugin).
- **Settled by.** The Codex sitting. **Mark: from source.**
- **What settles it.** One byte of a skill in the scratch tools plugin changed and that plugin installed again: `hooks/list` before and after, whether `/hooks` asks, whether the hooks still fire.
- **If refuted.** One frozen plugin that holds everything, and no skills on Codex.
- **Result.** Not yet recorded.

#### F-152. A changed hook declaration asks for a new review

- **Fact.** Changing the hook declaration of an installed Codex plugin makes Codex ask for a new hook review.
- **Needed by.** ledger unlisted (C-H1): HO-20, the threshold "Codex reviews needed after upgrades"; deliver unlisted (7.6): V-H1, HO-20.
- **Settled by.** The Codex sitting. **Mark: from source.**
- **What settles it.** One timeout changed in the scratch hooks plugin and the plugin installed again: whether a review is asked for.
- **If refuted.** None named; the cost would only be lower.
- **Result.** Not yet recorded.

#### F-153. A new binary behind an approved hook keeps the approval

- **Fact.** Replacing the binary file an approved hook's command names, with the plugin's own files unchanged, leaves the approval intact and the hook firing.
- **Needed by.** hosts §14 #2: every upgrade, 4.3 generation 0, section 15, HO-25, HO-27, `codex_reapprovals_per_release`.
- **Settled by.** The Codex sitting. **Mark: from source.**
- **What settles it.** The recorder's binary swapped for another build at the same path: `hooks/list` before and after, the hook's own log line, and whether `/hooks` asked.
- **If refuted.** None short of managed hooks; v1 already relies on it.
- **Result.** Not yet recorded.

#### F-154. A stray file unapproves; the same bytes again restore it

- **Fact.** A stray file added to Codex's cached copy of an approved plugin reports its hooks as modified, a symlink there makes hashing fail, and installing the plugin again from identical source bytes turns the hooks back to trusted with no review.
- **Needed by.** hosts §14 #3: the automatic repair (4.4), `trust.review`, `capture.silent`, HO-20, HO-23; hosts unlisted (4.1 row 4): "nothing is written inside a plugin root", the property test.
- **Settled by.** The Codex sitting. **Mark: from source.**
- **What settles it.** A made-up `.DS_Store` added to the cached copy: `hooks/list`; then a made-up symbolic link added there: `hooks/list`; the plugin installed again from the unchanged source: `hooks/list` and whether `/hooks` asks.
- **If refuted.** The repair becomes an ask.
- **Result.** Not yet recorded.

#### F-155. Codex runs a cached copy of the plugin

- **Fact.** Codex installs a plugin by copying it to `$CODEX_HOME/plugins/cache/<marketplace>/<plugin>/<version>/` and runs and hashes that copy, not the marketplace folder.
- **Needed by.** hosts §14 #4: 4.4, the reinstall repair, HO-20, field question 3.
- **Settled by.** The Codex sitting. **Mark: from source.**
- **What settles it.** The cache folder's path shape after install (names made up); the folder a hook ran from.
- **If refuted.** eva-md's own folder is the root: keep v1's cleaning of system metadata there.
- **Result.** Not yet recorded.

#### F-156. One approval holds for the CLI and the desktop app

- **Fact.** Codex stores a hook approval in the user's config, so one approval holds for the Codex CLI and the desktop app alike.
- **Needed by.** hosts §14 #5: 5.2's statement line, 7.5's ask wording, HO-21, HO-30.
- **Settled by.** The Codex sitting. **Mark: by the field report.**
- **What settles it.** Hooks approved in the CLI's `/hooks`, then a chat in the desktop app: whether the hooks fire there, `hooks/list` from each, and whether the app asks.
- **If refuted.** The ask names both places.
- **Result.** Not yet recorded.

#### F-157. Hooks load only from `.codex-plugin/plugin.json`

- **Fact.** Codex loads hooks only from a plugin's `.codex-plugin/plugin.json` manifest; a manifest at the plugin's root loads none.
- **Needed by.** hosts unlisted (4.1 row 5): the unit layout (4.2), HO-20.
- **Settled by.** The Codex sitting. **Mark: from source.**
- **What settles it.** The manifest the scratch hooks plugin uses, and whether its hooks appear in `hooks/list`.
- **If refuted.** None named.
- **Result.** Not yet recorded.

#### F-158. Two plugins from one marketplace

- **Fact.** Codex lets two plugins with different ids be registered from one local marketplace at once.
- **Needed by.** hosts unlisted (4.2): the unit split, additive units, HO-20.
- **Settled by.** Sitting 0 (the evaluation home, the machine and the git server). **Mark: from source.**
- **What settles it.** The scratch hooks plugin and the scratch tools plugin registered from one scratch marketplace: Codex's plugin list.
- **If refuted.** None named.
- **Result.** Not yet recorded.

#### F-159. A second hooks plugin leaves the first approved

- **Fact.** Adding a second plugin with hooks of its own leaves the first plugin's approval, so its hooks keep firing while the second waits for review.
- **Needed by.** hosts unlisted (4.3 additive units; 18 item 2): H-V5, plan section 12 (one more Codex hook review).
- **Settled by.** The Codex sitting. **Mark: from source.**
- **What settles it.** A second scratch plugin with one hook registered; a chat started before approving it: `hooks/list` and which hooks fired.
- **If refuted.** None named.
- **Result.** Not yet recorded.

#### F-160. `hooks/list` reports each hook's trust

- **Fact.** Codex's read-only `hooks/list` call reports, for each declared hook, whether it is trusted, managed, modified or untrusted.
- **Needed by.** hosts unlisted (4.4, 3.2, 7.4, 13.2): the trust probe, `trust.review`, `capture.silent`, HO-20, HO-23.
- **Settled by.** The Codex sitting. **Mark: by the field report.**
- **What settles it.** `hooks/list` field names and states for the scratch plugin unapproved, approved, after a stray file and after a changed declaration.
- **If refuted.** None named.
- **Result.** Not yet recorded.

#### F-161. Approving in `/hooks` is all it takes

- **Fact.** A person can approve a plugin's hooks in Codex's `/hooks` screen in the evaluation home, and that alone is needed: no copied `auth.json` and no made-up trust record.
- **Needed by.** evaluate unlisted (4.3): the evaluation home, the canary, every Codex sitting.
- **Settled by.** Sitting 0 (the evaluation home, the machine and the git server). **Mark: by the field report.**
- **What settles it.** The scratch plugin installed and approved in `/hooks`; the hooks then fire, and which screen asked.
- **If refuted.** None named.
- **Result.** Not yet recorded.

#### F-162. Hook commands run through a non-login shell

- **Fact.** Codex runs a plugin hook's command through a non-login shell, so `<binary> hook ... || echo {}` works, and a Stop hook answered with `{}` is accepted.
- **Needed by.** hosts unlisted (4.3 the command): the frozen command, C-H2, H-L3; deliver unlisted (7.6): fact of the start cost, V-H1, HO-20.
- **Settled by.** The Codex sitting. **Mark: from source.**
- **What settles it.** The hook's parent process and whether its shell is a login shell (flags only); the recorder made to fail once: whether `{}` came back and the turn went on; what a Stop hook's `{}` does.
- **If refuted.** None named; a login shell would raise the start cost.
- **Result.** Not yet recorded.

#### F-163. Codex fires managed hooks without review

- **Fact.** Managed Codex hooks need no review, cannot be turned off by the user, are reported as managed by the trust probe, can call the stable binary through the shell with `$HOME`, and `codex exec` fires them in a container this project runs.
- **Needed by.** evaluate §14 row 6: the Codex driver in containers, EV-D1, plan decision 28, EV-17; hosts unlisted (4.1 row 8, 12): HO-43, HO-D8, the probe.
- **Settled by.** EV-03.
- **What settles it.** In a throwaway container, a managed requirements file declaring one scratch hook that writes a marker; one `codex exec`: whether the marker is written and whether a review was asked. The rest from Codex's managed configuration documents.
- **If refuted.** The evaluation home with one review, and user-level hooks there.
- **Result.** Not yet recorded.

#### F-164. What the hook hash covers, and the approval key

- **Fact.** Codex's hook declaration hash covers event, matcher, command, timeout, async, statusMessage and a context limit that is not the default, and the stored approval key is positional.
- **Needed by.** hosts unlisted (4.1 row 2, 4.3, 16 row 1): the declaration choices, HO-30, the bundle-hash golden test.
- **Settled by.** Published source.
- **What settles it.** Codex's source for the hook key and the hook hash, at the sitting's Codex version.
- **If refuted.** None named.
- **Result.** Not yet recorded.

#### F-165. User-level hooks are approved on their declaration only

- **Fact.** Hooks in a user-level `~/.codex/hooks.json` are approved on their declaration only, with no hash of other files.
- **Needed by.** hosts unlisted (4.1 row 7, 16 row 1): the fallback of section 16, risk 1.
- **Settled by.** Published source.
- **What settles it.** Codex's source for hook discovery at the sitting's version.
- **If refuted.** None named.
- **Result.** Not yet recorded.

#### F-166. The plugin bundle hash

- **Fact.** Codex's plugin bundle hash is `codex-plugin-bundle-v1`: a sorted walk hashing the path, kind, size and bytes of each entry.
- **Needed by.** hosts unlisted (4.3): the golden bundle-hash test, HO-20, SEAM-0's hash test, HO-30.
- **Settled by.** Published source.
- **What settles it.** Codex's plugin fingerprint source at the sitting's version.
- **If refuted.** None named.
- **Result.** Not yet recorded.

### Headless runs

#### F-167. The sealed `codex exec` call runs from a background job

- **Fact.** `codex exec --ephemeral --output-schema` with a named small model and `-c model_reasoning_effort=low` runs on the plan login from a detached job; the run also shows which effort names the CLI takes and how it words a refusal.
- **Needed by.** derive §11 row 5: the default Codex backend (8.2), the policy table (8.3), DV-11, DV-03's frontier rows, DV-D2.
- **Settled by.** DV-02.
- **What settles it.** One sealed call on a made-up script from a detached job with the small model the lead names, then each effort name and one model that does not exist: CLI version, exit statuses, event types, each refusal's shape, scrubbed.
- **If refuted.** The host's default model with no effort flag.
- **Result.** Not yet recorded.

#### F-168. A sealed run has no tools and no global instructions

- **Fact.** A `codex exec` run with every tool feature off gives the model no tool it can call, and does not load the user's global instructions file.
- **Needed by.** derive §11 row 6: the sealed run (8.1), the `tool_used` class, DV-11.
- **Settled by.** DV-02.
- **What settles it.** A made-up global instructions file with a canary order; one sealed call whose input also asks to list files: whether the answer obeys the canary and whether any tool item is in the stream.
- **If refuted.** The reader still kills the run on a tool item, and `--ignore-user-config` is passed by default.
- **Result.** Not yet recorded.

#### F-169. One run with hooks and plugins off

- **Fact.** `codex exec --disable hooks --disable plugins` turns hooks and plugins off for that run without writing to Codex's settings.
- **Needed by.** evaluate unlisted (4.5): the `off`, `oracle` and `host` arms on Codex, cached baselines, EV-17.
- **Settled by.** EV-03.
- **What settles it.** A scratch plugin whose hook writes a marker; `codex exec` with and without the two flags: whether the marker appears, and whether any settings file changed (by hash).
- **If refuted.** None named; a clean config folder per run, as for Claude Code.
- **Result.** Not yet recorded.

### The host's own memory

#### F-170. Codex's memories are off by default

- **Fact.** Codex's memories are off by default; when on, they add a summary of about 2,500 tokens at a thread's start, take only interactive sessions idle for six hours, never `codex exec` sessions, and do not sync across machines.
- **Needed by.** deliver unlisted (14): DL-D4, V-E1; evaluate unlisted (4.5): the `host` arm on Codex run by hand, the `eva` arm, claim row 3.
- **Settled by.** Published source.
- **What settles it.** Codex's source and documentation at the recorded version; the default checked in the evaluation home.
- **If refuted.** None named.
- **Result.** Not yet recorded.

#### F-171. Codex's memory writer's models and threshold

- **Fact.** Codex's own memory writer uses a named small model for extraction and a larger one for consolidation, and stands down when under 25% of the allowance is left.
- **Needed by.** derive unlisted (8.3, 3.5): the policy table, the quota gate's 25%, DV-03's tiers.
- **Settled by.** Published source.
- **What settles it.** Codex's source at the version in use: the model names and the threshold.
- **If refuted.** None named; DV-03's frontier picks the default anyway.
- **Result.** Not yet recorded.

#### F-172. The instruction chain is built once per session

- **Fact.** Codex builds its instruction-file chain once per session and has no path scoping outside the repository.
- **Needed by.** deliver unlisted (9): DL-24, DL-D1.
- **Settled by.** Published source.
- **What settles it.** Codex's documentation on instruction files at the recorded version.
- **If refuted.** None named.
- **Result.** Not yet recorded.

### Timing

#### F-173. One hook start costs 100 ms or less at the 95th percentile

- **Fact.** One hook start on Codex, through the shell Codex runs it with, the process's start included, costs 100 ms or less at the 95th percentile.
- **Needed by.** plan section 8 threshold and 13.2; ledger plan item: C-H1, LG-20, HO-20; deliver §12 row 16: V-H1, HO-20, DL-14; hosts §14 #7: generation 1's PreToolUse row, HO-30, risk 13; evaluate unlisted (10 row 6): the LAT suite.
- **Settled by.** The Codex sitting. **Mark: by the field report.**
- **What settles it.** At least 200 hook starts, each timed from the moment its shell was created to the recorder's exit, with the Codex version and the shell; the median and the 95th percentile. Every second start also hashes a made-up folder of 2 KB, as a check of a plugin root would (hosts §14 #7's second half), and the two halves are reported apart.
- **If refuted.** Plan: no hook on Codex's shell tool beyond what v1 declares; hosts risk 3: tool time on edits only.
- **Result.** Not yet recorded.

#### F-174. SessionStart `compact` runs before the next request, also mid-turn

- **Fact.** On Codex, SessionStart with source `compact` runs before the next model request, also when the compaction happens mid-turn, and context earlier hooks added is not kept after it.
- **Needed by.** deliver §12 row 9: DL-16, DL-09 (the owed brief, epochs).
- **Settled by.** The Codex sitting. **Mark: later** (the run-book's Part 2, group E).
- **What settles it.** A compaction forced in the middle of a long made-up task: the hook order against the turn's progress; a planted word an earlier hook gave and one the compact hook gave, both asked for.
- **If refuted.** The `owed` retry at the next prompt.
- **Result.** Not yet recorded.

## D. The git server

#### F-175. The server takes a push of machine commits

- **Fact.** The git server accepts a push of commits whose author and committer address is unknown to it, one SSH-signed by a key no account has registered and one unsigned, made from the evaluation home without the real install's credentials.
- **Needed by.** store §16 row 4 and risk 3: ST-00, ST-D4, plan decision 3, ST-06, ST-07 step 7, ST-09; evaluate unlisted (10 row 9): the store's machine identity; evaluate unlisted (4.3): the live checks of the daily commit and push; plan section 8 ("Daily commit outcomes; signing prompts").
- **Settled by.** Sitting 0 (the evaluation home, the machine and the git server). **Mark: core** (the run-book's Part 1).
- **What settles it.** The run-book's G-04, from a Terminal window in the evaluation account: two commits on the scratch repository's default branch, one unsigned and one signed, each pushed by itself to a new branch that is not the default, authenticated by the deploy key made in the evaluation account for the test and passed per call (never a credential of the owner's own account). For each: accepted or refused, and the class of a refusal (never the server's text). Only an acceptance, or a refusal for the committer or the signature, settles the fact; a refusal for a protected branch, a branch-name rule or the key leaves it not settled. Then G-05: how the server labels the signed commit (verified, unverified, no label), and the repository deleted at once. Also whether any prompt appeared. The run-book's first version made this push from the scheduled job in sitting 0; that push now waits in Part 2's group J (S0-13).
- **If refuted.** Plan decision 3: leave machine commits unsigned if only the signature is refused; if the committer is refused, its fallback (squash at each push with your identity and signing, ST-D4's `identity = "user"`).
- **Result.** Not yet recorded.

#### F-176. The real wiki's project has no push rule against them

- **Fact.** The real wiki's project on the git server has no push rule that requires a known committer address or signed commits.
- **Needed by.** store §16 row 4 (the real project's rules) and §18 item 3: ST-D4, plan decision 3.
- **Settled by.** Sitting 0 (the evaluation home, the machine and the git server). **Mark: by the field report.**
- **What settles it.** The owner's answers to the second report's question 4, read from the real project's push-rule and branch-protection settings page in the browser, reading only, each a yes or no, never a setting's name (no command run against the real install).
- **If refuted.** As the row above.
- **Result.** Not yet recorded.

#### F-177. The real wiki's git server is reachable without a private network

- **Fact.** The real wiki's git server is reachable from the Mac without a private network, so a push can succeed whenever the machine is online.
- **Needed by.** store unlisted (§18 item 12, 8.2 `pending`): ST-06's `time_to_synced`, S-H3; plan 13.6 question 3.
- **Settled by.** Sitting 0 (the evaluation home, the machine and the git server). **Mark: by the field report.**
- **What settles it.** The owner's answer.
- **If refuted.** 8.2: `pending` with backoff and, after repeated failure, one plain ask.
- **Result.** Not yet recorded.

#### F-178. A read with prompts off never waits

- **Fact.** Under the person's credential setup, `git ls-remote` with prompts off succeeds silently or fails fast; it never waits on a prompt or an approval dialog.
- **Needed by.** store §16 row 6 and risk 4: ST-00, ST-06, 8.1, 8.2.
- **Settled by.** Sitting 0 (the evaluation home, the machine and the git server). **Mark: by the field report.**
- **What settles it.** `git ls-remote` against the scratch repository with prompts off, batch mode and a 10-second limit, with the deploy key made for the test held in the evaluation account's SSH agent, and then with that agent locked (`ssh-add -x`): the time, the exit class, and any dialog. The real setup from the owner's answer to plan 13.6 question 3; the sitting settles it only for the evaluation account's credential, a key in its agent.
- **If refuted.** Risk 4: the machine key as a deploy key is offered, and attempts are rate-limited after one that needed approval.
- **Result.** Not yet recorded.

#### F-179. An anonymous read of a private repository fails fast

- **Fact.** An anonymous read of a private repository, with no credentials and prompts off, fails fast on the git server.
- **Needed by.** store unlisted (8.1): ST-06, S-H3's `remote_not_private`, the setup must-keeps.
- **Settled by.** Sitting 0 (the evaluation home, the machine and the git server). **Mark: from source.**
- **What settles it.** An anonymous HTTPS `git ls-remote` of the scratch repository with no credential helper: exit class and time.
- **If refuted.** Only the check at attach remains.
- **Result.** Not yet recorded.

#### F-180. A background job knows the server's host key

- **Fact.** The git server's SSH host key is known to the SSH client a background job uses, so a push in batch mode does not stop at an unknown-host check.
- **Needed by.** store unlisted (4.5, 8.2): ST-06, ST-09.
- **Settled by.** Sitting 0 (the evaluation home, the machine and the git server). **Mark: by the field report.**
- **What settles it.** From the scheduled job, an SSH read of the scratch repository in batch mode, once before anyone accepted the server's host key, and once after the owner accepted it in a terminal of the evaluation account: whether each stops at the host-key check, and which known-hosts files the job reads.
- **If refuted.** None named.
- **Result.** Not yet recorded.

#### F-181. Release downloads reach a background job

- **Fact.** The release download addresses on github.com and its asset host are reachable, for new asset names, from a background job on the Mac's network.
- **Needed by.** hosts §14 #13 and field question 16: the upgrade channel (10.2), HO-25 (now HO-13's daily check), `update.unreachable`.
- **Settled by.** Sitting 0 (the evaluation home, the machine and the git server). **Mark: later** (the run-book's Part 2, group J).
- **What settles it.** From the scheduled job, one public eva-md release asset fetched by name: the HTTP status only.
- **If refuted.** `update.unreachable` in status; upgrades by hand.
- **Result.** Not yet recorded.

#### F-182. Write deploy keys exist and can be added

- **Fact.** Deploy keys with write access exist on the git server in use, can be added to one repository, and a push authenticated by one succeeds; the installed `gh` or `glab`, if any, has a command to add one.
- **Needed by.** store §16 row 5 and risk 4: ST-00, ST-06, ST-09, S-H5.
- **Settled by.** Sitting 0 (the evaluation home, the machine and the git server). **Mark: by the field report.**
- **What settles it.** A write deploy key made in the evaluation account, added by the owner to the scratch repository on the server's web page, and Part 1's pushes made with it (G-02, G-04). Whether `gh` or `glab` is installed, and its version, read by the second report with `--version` only; the lead then reads that version's published documents for a deploy-key command with write access. If neither is installed, that half stays not settled.
- **If refuted.** `auth = "user"`: transport as git already does for that person.
- **Result.** Not yet recorded.

#### F-183. The default branch takes a push with a lease

- **Fact.** The scratch repository's default branch accepts a forced push with a lease; the server's default branch protection does not block it.
- **Needed by.** store unlisted (8.7, 8.4, §18 item 3): ST-11, ST-D8 (plan section 12, "Removing something from pushed history").
- **Settled by.** Sitting 0 (the evaluation home, the machine and the git server). **Mark: later** (the run-book's Part 2, group M).
- **What settles it.** Whether `main` is protected by default; if the owner agrees to a further push, a `--force-with-lease` push of a rewritten history with the deploy key, and its outcome; the owner's word on the real project's branch.
- **If refuted.** ST-D8's "if no": forget retires and blanks; removing a pushed secret is a hand procedure; "never force" stays absolute.
- **Result.** Not yet recorded.

#### F-184. `ssh.github.com` is github.com

- **Fact.** `ssh.github.com` is another SSH host name for `github.com`, so remotes on either name are the same repository.
- **Needed by.** ledger unlisted (8.2): remote normalization, LG-22.
- **Settled by.** Published source.
- **What settles it.** The git server's published documents on SSH over port 443.
- **If refuted.** None named.
- **Result.** Not yet recorded.

## E. The operating system

### The Mac's git and tools

#### F-185. The Mac's git does what the store needs

- **Fact.** The Mac's git is 2.36 or newer and signs a commit with an SSH key given only by per-call `-c` settings, accepts `core.fsync` for loose objects and references, leaves the base, ours and theirs versions of each conflicted path readable after `git merge --no-commit -X no-renames`, and checks a committed symlink out as a plain file under `core.symlinks=false`.
- **Needed by.** store §16 row 1 and preface: ST-00, ST-05, ST-06, ST-09, ST-D4, plan decision 3, `time_to_durable`.
- **Settled by.** Sitting 0 (the evaluation home, the machine and the git server). **Mark: by the field report.**
- **What settles it.** The store checks in a scratch folder with made-up files: the version, and pass or fail for each behaviour.
- **If refuted.** Older or missing git: the store is deferred with `git_missing`. Signing that fails: plan decision 3 (unsigned) or ST-D4's `identity = "user"`.
- **Result.** Not yet recorded.

#### F-186. `ssh-keygen` makes and signs with a key, with no prompt

- **Fact.** `ssh-keygen` is present and makes an ed25519 key with no passphrase that signs a git commit, with no prompt.
- **Needed by.** store §16 row 2: ST-00, ST-05, S-H5, ST-D4, plan decision 3.
- **Settled by.** Sitting 0 (the evaluation home, the machine and the git server). **Mark: from source.**
- **What settles it.** Presence, the key type made, and whether signing succeeded with no prompt.
- **If refuted.** Plan decision 3 (unsigned) or ST-D4's fallback.
- **Result.** Not yet recorded.

#### F-187. Signature letters for known and unknown keys

- **Fact.** On the Mac's git, `git log --format=%G?` with a per-call allowed-signers file reports `G` for a commit signed by an enrolled key and `U` for one signed by an unknown key.
- **Needed by.** store unlisted (preface, 8.6): the trust set and quarantine, ST-09, ST-D4, S-V3, plan section 12's sync row.
- **Settled by.** Sitting 0 (the evaluation home, the machine and the git server). **Mark: from source.**
- **What settles it.** Two commits signed by two keys, one listed in an allowed-signers file passed per call: the letter for each.
- **If refuted.** 8.6: without machine signing nothing can be classified, and every commit on the remote is trusted.
- **Result.** Not yet recorded.

#### F-188. Per-call settings never start the person's signing program

- **Fact.** With eva-md's per-call `-c` settings, a store commit never starts the signing program the person's global git config names (`gpg.ssh.program`, for one).
- **Needed by.** store unlisted (4.3, 4.5; plan 6.2 item 5): ST-05, ST-D4, plan decision 3, the bar "a daily signing or sign-in prompt: never".
- **Settled by.** Sitting 0 (the evaluation home, the machine and the git server). **Mark: from source.**
- **What settles it.** The evaluation account's global git config names a stand-in signing script that only records being called; one store-style commit: whether it was called.
- **If refuted.** None named.
- **Result.** Not yet recorded.

#### F-189. The git a background job finds is new enough

- **Fact.** The git a scheduled job and a hook-started job find on their PATH is 2.36 or newer, even where a person's shell uses another git.
- **Needed by.** store unlisted (4.2, §16 row 1): ST-05, ST-06, 4.2's deferred store, S-H2.
- **Settled by.** Sitting 0 (the evaluation home, the machine and the git server). **Mark: by the field report.**
- **What settles it.** `command -v git` and `git --version` from the scheduled job and from a hook's detached job on each host, beside the same from a terminal.
- **If refuted.** 4.2: the store is deferred with `git_missing` until a new enough git is found.
- **Result.** Not yet recorded.

#### F-190. No privacy prompt for the store's folder

- **Fact.** macOS lets a background process read and write under `~/.local/share` without a privacy prompt.
- **Needed by.** store §16 row 9: ST-00, ST-05, S-H1, 4.1.
- **Settled by.** Sitting 0 (the evaluation home, the machine and the git server). **Mark: from source.**
- **What settles it.** The scheduled job and a hook-started job each create, write, read and remove a file under a test folder of that path shape: exit status and any prompt.
- **If refuted.** None named.
- **Result.** Not yet recorded.

#### F-191. The machine key pushes with no agent

- **Fact.** A push authenticated with the machine key passed per call succeeds with no agent socket, and never consults the person's SSH agent or approval app, even when the SSH config names one for that host.
- **Needed by.** store unlisted (4.5, risk 4): ST-06, ST-09, the bar of no sign-in prompt.
- **Settled by.** Sitting 0 (the evaluation home, the machine and the git server). **Mark: from source.**
- **What settles it.** For the job's run, the evaluation account's SSH config names a made-up agent for the scratch server; from the scheduled job, with no agent socket, the deploy key made for the test, standing for the machine key, used per call: whether the read and the push succeed, and whether any dialog appeared.
- **If refuted.** None named.
- **Result.** Not yet recorded.

#### F-192. `core.fsync` and a full flush on macOS

- **Fact.** There is a `core.fsync` value that makes git flush loose objects and references before a commit returns, and on macOS git uses a full flush for it.
- **Needed by.** store §16 row 3: ST-05's durability, `time_to_durable`, storage must-keep 3.
- **Settled by.** Published source.
- **What settles it.** `git help config` for the Mac's git version: the value list and what git documents about the flush on macOS.
- **If refuted.** The claim is limited to what git documents.
- **Result.** Not yet recorded.

#### F-193. Go's file sync is a full flush on macOS

- **Fact.** On macOS, Go's `File.Sync` on a written file and on its folder reaches stable storage as a full flush.
- **Needed by.** store unlisted (7.1 step 5): ST-05's Apply, storage must-keep 3.
- **Settled by.** Published source.
- **What settles it.** Go's source for `(*os.File).Sync` on darwin.
- **If refuted.** None named.
- **Result.** Not yet recorded.

#### F-194. `~/.local/share` is not synced to the cloud

- **Fact.** `~/.local/share` is not a folder macOS syncs to iCloud Drive.
- **Needed by.** store unlisted (4.1): ST-05's `Dirs.Store()`, storage must-keep 16.
- **Settled by.** Published source.
- **What settles it.** Apple's documentation of the folders iCloud Drive syncs.
- **If refuted.** None named.
- **Result.** Not yet recorded.

#### F-195. A push into a branch that is not checked out

- **Fact.** git accepts a push from a clone into a branch that is not checked out (`refs/heads/in/<name>`) of a non-bare repository with default receive settings.
- **Needed by.** store unlisted (4.7): ST-10, 8.6.
- **Settled by.** Published source.
- **What settles it.** git's documentation of `receive.denyCurrentBranch`; the machine checks confirm it on the Mac's git.
- **If refuted.** None named.
- **Result.** Not yet recorded.

#### F-196. `git maintenance run --auto` with `gc.auto=0`

- **Fact.** With `gc.auto=0`, `git maintenance run --auto` still packs the store's objects.
- **Needed by.** store unlisted (4.3, risk 6): ST-05, S-H2, ST-12.
- **Settled by.** Published source.
- **What settles it.** `git help maintenance` for the installed version: which tasks `run --auto` runs and which settings their conditions read; the machine checks try it on a scratch repository.
- **If refuted.** None named.
- **Result.** Not yet recorded.

#### F-197. `ssh-keygen -Y verify`, and no quarantine on a fetched file

- **Fact.** Every supported Mac has `ssh-keygen` with `-Y verify`, and a file fetched by curl or by a Go binary carries no quarantine mark.
- **Needed by.** hosts unlisted (10.2, 10.6): the signed manifest (plan HO-13), HO-D8.
- **Settled by.** Published source.
- **What settles it.** OpenSSH's and Apple's documentation; the machine checks also run `-Y verify` on a made-up signature and read a fetched file's extended attributes.
- **If refuted.** None named.
- **Result.** Not yet recorded.

#### F-198. An atomic rename leaves a running binary on its old file

- **Fact.** Replacing the stable binary by an atomic rename leaves a running hook or MCP server on its old file while new starts get the new one.
- **Needed by.** hosts unlisted (10.3 step 4): HO-25, 10.5.
- **Settled by.** Published source.
- **What settles it.** POSIX rename semantics.
- **If refuted.** None named.
- **Result.** Not yet recorded.

#### F-199. systemd timers catch up, and notify-send posts

- **Fact.** A systemd user timer with `Persistent=true` runs a missed tick after the machine was off, and `notify-send` posts a notification where a desktop session exists.
- **Needed by.** hosts unlisted (6, 11.2, 7.6): HO-40, HO-22 on Linux.
- **Settled by.** Published source.
- **What settles it.** The systemd.timer and notify-send documentation.
- **If refuted.** The fallback of hosts section 6.
- **Result.** Not yet recorded.

### The operating system's scheduler

#### F-200. A launchd user agent installs without an admin and keeps time

- **Fact.** A launchd user agent with `StartInterval` 900 and `RunAtLoad` installs with `launchctl bootstrap gui/<uid>` without an administrator, runs every 15 minutes, and runs again after the Mac wakes from sleep.
- **Needed by.** hosts §14 #11 first part: the tick (6), HO-D2 (plan decision 4), HO-22, fault matrix row 4; derive §11 row 12: the tick (3.8), DV-D1, D-H1, P4's timed steps.
- **Settled by.** Sitting 0 (the evaluation home, the machine and the git server). **Mark: later** (the run-book's Part 2, group J).
- **What settles it.** A made-up agent that appends a time to a file every 900 seconds: whether a password was asked, `launchctl print` for it, and the times across the sittings, a sleep included.
- **If refuted.** The fallback: the worker a hook starts and the MCP server's 10-minute call.
- **Result.** Not yet recorded.

#### F-201. Work the scheduler starts reaches the model and the git server

- **Fact.** Work started by a launchd user agent, with no terminal or login shell, can run each host's CLI on the plan login and reach the git server, with no sign-in, keychain or SSH-agent prompt.
- **Needed by.** plan's HF-00 row; store §16 row 7: ST-00, ST-06, S-H2, plan decision 4; hosts plan item: HO-22, derive's worker, store sync; derive plan item: the tick, DV-D1, P4's `store.Sync`, DV-24; evaluate unlisted (6.9): EV-22, release gate R2.
- **Settled by.** Sitting 0 (the evaluation home, the machine and the git server). **Mark: later** (the run-book's Part 2, group J).
- **What settles it.** The scheduled job's first run: one made-up headless call per host, `git ls-remote`, and group J's pushes (the run-book's S0-13); each step's exit status, whether `SSH_AUTH_SOCK` was set, and the class of any failure.
- **If refuted.** The tick runs local steps only; the model and the push wait for a hook-started worker; time to synced holds only while a chat is open (store 8.2); with no agent socket, `auth = "machine-key"`.
- **Result.** Not yet recorded.

#### F-202. No device management forbids user launch agents

- **Fact.** No device management profile on the Mac forbids user launch agents, so the tick is allowed on the real install's machine.
- **Needed by.** hosts §14 #11 second part and field question 10: the tick on the real install, HO-D2, HO-43, risk 6; derive §11 row 12 and field question 11.
- **Settled by.** Sitting 0 (the evaluation home, the machine and the git server). **Mark: by the field report.**
- **What settles it.** The owner reads System Settings' device management page in their own account and in the evaluation account, and says whether any profile is listed (yes or no only). Nothing of the real install is run.
- **If refuted.** The fallback; the install records `tick: unavailable` and a low-severity condition in status.
- **Result.** Not yet recorded.

#### F-203. Installing an agent shows a background-item notice

- **Fact.** Installing a launchd user agent makes macOS show a one-time "background item added" notice naming the program.
- **Needed by.** hosts unlisted (6): the 5.2 statement's extra line, HO-22, risk 6.
- **Settled by.** Sitting 0 (the evaluation home, the machine and the git server). **Mark: from source.**
- **What settles it.** When the scratch agent is installed: whether a notice appeared and which name it showed.
- **If refuted.** The statement line is dropped.
- **Result.** Not yet recorded.

#### F-204. A notification from a scheduled job needs no permission

- **Fact.** `osascript` run from a launchd user agent posts a notification with no permission prompt, attributed to the system's script runner.
- **Needed by.** hosts §14 #11 third part: the OS channel (7.6), HO-D4, HO-23, plan section 12 (an operating-system notification as a last resort).
- **Settled by.** Sitting 0 (the evaluation home, the machine and the git server). **Mark: later** (the run-book's Part 2, group J).
- **What settles it.** One made-up notification from the scratch agent: whether a prompt appeared, and the name the notification shows. The macOS version stays in the recording; the Result says only whether the fact holds.
- **If refuted.** No OS channel on macOS.
- **Result.** Not yet recorded.

## F. The model

#### F-205. The small model's price against the larger ones

- **Fact.** By the published credit prices, Codex's small model costs a tenth to a twentieth of its larger ones per token; on Claude the gap, and how effort changes output tokens, are measured.
- **Needed by.** derive unlisted (8.3): DV-D2 (plan decision 5), the tokens-per-chat bar, DV-03.
- **Settled by.** DV-02.
- **What settles it.** Codex: the published price table. Claude: tokens in and out per pass at the highest and at a low effort.
- **If refuted.** None named.
- **Result.** Not yet recorded.

#### F-206. A small extraction call takes about 13,000 tokens

- **Fact.** A small extraction call takes about 13,000 tokens.
- **Needed by.** evaluate unlisted (9): the budget rows for small calls, EV-15, the canary budget.
- **Settled by.** DV-02.
- **What settles it.** Tokens per call in the live gates' rows.
- **If refuted.** Section 9 is rewritten from the measurements.
- **Result.** Not yet recorded.

#### F-207. An agent session's size and length

- **Fact.** An agent session on a fixture task takes about 0.3 to 0.7 million prompt tokens, 80 to 90% of them cache reads, and about four minutes.
- **Needed by.** evaluate unlisted (9): the budget, pack sizes, EV-D2 and plan decision 16's ceilings, EV-03's acceptance.
- **Settled by.** EV-03.
- **What settles it.** Tokens (cached apart), events and wall time per session, from the streams.
- **If refuted.** Section 9 is rewritten; suites are cut in the stated order.
- **Result.** Not yet recorded.

## G. Held for later: cloud sessions and remote machines

### Cloud sessions, Cowork and remote machines

#### F-208. Account plugins reach cloud sessions

- **Fact.** Plugins enabled on a person's account sync to Claude Code cloud sessions, and their hooks run there.
- **Needed by.** hosts §14 #14: the cloud read path (11.4), HO-42, HO-44, plan section 12 ("Cloud sessions").
- **Settled by.** None in Stage 0 (plan section 12).
- **What settles it.** HO-42's spike: a scratch plugin on a test account, a cloud session opened, and whether its SessionStart hook wrote a marker.
- **If refuted.** No cloud reach in v2.
- **Result.** Not yet recorded.

#### F-209. Hooks in a cloud session have a root and a data folder

- **Fact.** In a Claude Code cloud session a synced plugin's hooks run with a plugin root and a writable data folder.
- **Needed by.** hosts unlisted (11.4 question 1): HO-42, HO-44.
- **Settled by.** None in Stage 0 (plan section 12).
- **What settles it.** The spike: a marker written in the data folder and the root's path shape.
- **If refuted.** No cloud reach in v2.
- **Result.** Not yet recorded.

#### F-210. A cloud hook's detached child reaches the network

- **Fact.** In a cloud session a hook's detached child keeps running and reaches the network.
- **Needed by.** hosts unlisted (11.4 question 2): HO-42 (the worker's clone).
- **Settled by.** None in Stage 0 (plan section 12).
- **What settles it.** The spike: a detached child fetches a public page and writes the status.
- **If refuted.** No cloud reach in v2.
- **Result.** Not yet recorded.

#### F-211. A cloud session reaches the store's remote

- **Fact.** A cloud session can reach the store's git remote with a credential.
- **Needed by.** hosts unlisted (11.4 question 3 and its limits): HO-42's read path.
- **Settled by.** None in Stage 0 (plan section 12).
- **What settles it.** The spike: a scratch repository cloned from the session.
- **If refuted.** No cloud reach in v2.
- **Result.** Not yet recorded.

#### F-212. A synced script keeps its executable bit

- **Fact.** A file under a synced plugin's `scripts/` keeps its executable bit in a cloud session.
- **Needed by.** hosts unlisted (11.4 question 4): HO-42's portable plugin.
- **Settled by.** None in Stage 0 (plan section 12).
- **What settles it.** The spike: the file's mode listed.
- **If refuted.** No cloud reach in v2.
- **Result.** Not yet recorded.

#### F-213. `claude -p` runs inside a cloud session

- **Fact.** `claude -p` runs inside a cloud session on the session's own login.
- **Needed by.** hosts unlisted (11.4 question 5): HO-42's write path.
- **Settled by.** None in Stage 0 (plan section 12).
- **What settles it.** The spike: one made-up one-line prompt and its exit status.
- **If refuted.** Read path only, no capture.
- **Result.** Not yet recorded.

#### F-214. How long a cloud machine lives after the last turn

- **Fact.** A cloud session's machine lives for a known time after the last turn.
- **Needed by.** hosts unlisted (11.4 question 6): HO-42.
- **Settled by.** None in Stage 0 (plan section 12).
- **What settles it.** The spike: a detached child writes a time every minute; the last time.
- **If refuted.** None named.
- **Result.** Not yet recorded.

#### F-215. What Cowork loads

- **Fact.** Cowork loads a plugin's hooks, loads its local MCP server only when it runs on the person's computer, never reads the CLI's folders, and on consumer plans runs only in the cloud.
- **Needed by.** hosts unlisted (11.5): HO-44.
- **Settled by.** None in Stage 0 (plan section 12).
- **What settles it.** HO-44: the six cloud questions answered for Cowork with a scratch plugin.
- **If refuted.** The floor of 11.3 where local MCP loads; nothing where it does not.
- **Result.** Not yet recorded.

#### F-216. A desktop SSH session copies the plugin to the remote machine

- **Fact.** For the desktop app's SSH sessions, the Claude app copies the plugin to the remote machine, where the binary path the plugin names does not exist.
- **Needed by.** hosts unlisted (4.6): §14 #9's second half, HO-40.
- **Settled by.** None in Stage 0 (plan section 12).
- **What settles it.** A desktop SSH session to a scratch Linux machine, and the plugin folder that appears there.
- **If refuted.** None named.
- **Result.** Not yet recorded.

## Facts stated two ways

Where two papers, or a paper and the plan, state a fact differently. Each is resolved by what the plan says, or listed for the lead.

### Resolved by the plan

1. **What proves a prompt typed on Codex** (F-079, F-121, F-122). The ledger (3.6) bases `typed` on the hook's entrypoint, which only Claude Code gives. The plan (6.1, LG-13, LG-21) says Codex's payload has no such field: the hook files a Codex prompt as unknown and the worker sets its author from the host's record of what started the turn. The sheet follows the plan: the entrypoint fact is Claude Code's, and Codex has its own record facts.
2. **How a program's chat is told apart** (F-018, F-079). Deliver (7.2) detects it by "SessionStart with a program's entrypoint" on both hosts; the plan (6.1) says only Claude Code marks how a chat was started. F-018 asks for any field in the payload or the record, on each host; F-079 is the entrypoint, on Claude Code only.
3. **Whether a skill change costs a Codex review** (F-150, F-151). Deliver and evaluate say a skill file changed alone does not make Codex ask; hosts says it does. Both are right of a different plugin: a skill inside the plugin that holds the hooks changes the hashed bytes (F-150), a skill in a second plugin with no hooks does not (F-151). The plan's HO-20 puts skills and the MCP entry in a second plugin; the sitting tests both.
4. **A hook on every Codex tool** (F-120, F-173). The ledger's C-H1 declares PostToolUse for every tool on Codex. The plan's threshold says that if one Codex hook start costs over 100 ms at the 95th percentile, no hook goes on Codex's shell tool beyond what v1 declares. The threshold governs; F-173 measures it, and F-120 records which events fire.
5. **A pre-tool hook that asks** (F-095). Deliver (section 8, DL-D2) writes the ask tier for both hosts; the plan's section 12 says "Claude Code only: Codex has no way for a hook to ask", and defers it. The fact is stated for Claude Code, with Codex's lack as its second half.
6. **Runs on the real install** (F-173, F-178, F-181, F-049). Hosts §14 #7 and deliver row 16 put 200 timed runs "on the real install"; store row 6 runs `ls-remote` there; deliver row 11 reads "a developer's machine"; hosts §14 #13 runs `upgrade --check` there. The plan touches the real install only with read-only reports. The first run-book moved all of them to the evaluation home, which shares the real install's network. Since the cut, the second report reads F-173 (its row (f)) and F-178 (push outcomes and the owner's answers) read-only on the real install, F-049 comes from source, and F-181 waits with the clock (group J of the run-book's Part 2), unless the lead lets the report run `eva-md upgrade --check`.
7. **Real-install settings no scratch repository shows** (F-176, F-177, F-183, F-202). The store's push rules, protected branch and private-network questions, and hosts' and derive's "launch agents allowed on the real install", need the real project or machine. Since the cut, the owner answers them in the second report (its question 4, and a yes or no on device management), and no sitting reads them. Nothing is run against the real install, and their Results say only which way the design goes (see "How to read it").
8. **The detached child, and `EVA_MD_INNER`** (F-030, F-028). Derive marks both for DV-02; the ledger and hosts mark the detached child for the sitting, and the recorder sees both directly. They are settled in the sittings.
9. **The fallback when the server refuses machine commits** (F-175). Store risk 3 and ST-D4 name an address the person chooses, or `identity = "user"`. The plan's decision 3 adds a middle step: if only the signature is refused, the commits stay unsigned; the squash with the person's identity comes only if the committer is refused too. The plan's order is the sheet's.
10. **Six cloud questions, not five** (F-208 to F-214). Hosts section 2 says the cloud spike confirms five facts; its section 11.4, its HO-42 and the plan's section 12 say six. The sheet lists six, and §14 #14.
11. **A scratch plugin, not eva-md, in the evaluation home.** Evaluate 4.3 has eva-md installed in the evaluation home and its hooks approved; the plan's HF-00 row has "a scratch plugin's hooks" approved. The sittings install only the scratch plugins; eva-md comes to the evaluation home with DV-02's live gates.
12. **The `--bare` question** (F-114). Derive row 3 and evaluate row 2 state it two ways (safe mode keeps the plan login; a flag keeps the non-bare behaviour). They are one fact, settled first by DV-02's recorded runs; evaluate's risk 7 has the canary check it again at every host release.
13. **Where the model that answered is read** (F-060). Store and derive mark it for DV-02, evaluate (row 12) for EV-03. DV-02 records it first, on both hosts; EV-03 reads the same fields again.

### For the lead

1. **The facts held for later** (F-208 to F-216). Cloud sessions, Cowork and the desktop app's SSH sessions wait in the plan's section 12; their spike, HO-42, is a Stage 4 package. None has a Stage 0 route. Keep them numbered and unsettled, or move them out of the sheet.
2. **One push, or more** (F-182, F-183, F-191). The plan asks for one push to the scratch repository. The store also wants a push with a deploy key and a forced push with a lease. The run-book's Part 1 makes it (G-04) from a Terminal window with a deploy key made in the evaluation account, as two pushes of one commit each, to new branches, so that a refusal of one does not hide the other's outcome; it settles F-175 and shows F-182's server half and F-191 with it, and G-05 deletes the repository right after. The scheduled job's push (S0-13) waits in Part 2's group J, and the forced push with a lease (S0-16) in group M, until section 12's removal from pushed history opens.
3. **Evaluate's twelve facts and the shrunk pilot** (F-067 to F-072, F-115 to F-117, F-163, F-169). The plan's EV-03 row no longer lists the host facts the paper's EV-03 did. The sheet keeps them with EV-03, since only its container and pty runs show them. Two of them, the pty session (F-070) and memory written in a pty-driven session (F-116), need the Mac; say whether they move to the sittings.
4. **The spare sessions of deliver row 17** (F-011). The paper does not say which desktop app. v1's design knows Claude's desktop spare folder, so it is most likely Claude's; the sheet checks both apps, at no extra step.
5. **Two doubts the sittings should resolve rather than assume.** The ledger (3.4) has the Claude Code task tool hand over the whole list each time, while the finding it cites speaks of one task's status line (F-078). The ledger (6.2) expects a subagent's rows in the main transcript with a sidechain flag, while Claude Code may write them to a file of their own (F-027). The recorder records which.
6. **The `gc.auto=0` doubt** (F-196). The store sets `gc.auto=0` and relies on `git maintenance run --auto`, whose default task goes through `git gc --auto`, which that setting turns off. A scratch test on git 2.56 on Linux packed nothing that way. The fact is marked for the published source and sitting 0's machine checks try it; the store's builder should not wait for the sitting to find out.
7. **The Claude Code half of the PostCompact fallback** (F-098). The ledger names a fallback only for Codex. The sheet presumes the record's compaction-summary row, which v1 marks and skips; confirm.
8. **What the fixtures may keep** (the card, F-121 and the voice facts). The plan says the fixtures keep the recordings' field names and shapes, with every value made up. Some values are the hosts' own vocabulary and the fixtures are useless without them: event names, row types, source and reason values, booleans. The card keeps those, from a closed list the lead approves, and replaces every other value. Model names, effort levels, plan names, versions and usage figures are never on the list, even when a host writes them on every row. Say whether that list is acceptable. The recording itself, which is never committed, keeps more: the enum values of a named list of record fields (among them `turn_attribution.turn_trigger` and the voice rows' `type`, `role` and `outcome`), and every enum-shaped value on the few row types the core facts read. Approve that field list before sitting 0, or a core fact's value may be recorded only as a hash.
9. **Facts that serve deferred packages.** Tool-time delivery (F-089, F-123, F-048), generated rules files (F-094), the ask tier (F-095), the OS notification (F-204), the MCP-only floor (F-055, F-108) and additive hook plugins (F-159) feed packages the plan has cut or deferred. None is in the run-book's Part 1; those a sitting would settle wait in its Part 2.
10. **Two Codex figures of about 2,500 tokens.** F-125 is the size of a hook message; F-170 is the size of Codex's own memory summary. They are not the same fact.
