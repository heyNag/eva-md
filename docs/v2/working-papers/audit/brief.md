# The resume brief: audit

Scope: what a new chat is given at start, after /clear and after compaction.
Code read in full: `internal/resume/{resume,read,lines,pages,now,rules,knowledge,git,links,projectpage,lineage,systems}.go`, `internal/hook/resume.go`; the parts of `internal/hook/{hook,pointers,trigger,recall}.go`, `internal/install/templates.go`, `internal/identity/identity.go`, `internal/update/wiki.go`, `internal/threadpage/page.go` and `internal/freetext/freetext.go` the brief depends on. Tests read for coverage, not run. Nothing in the repository was changed and no restricted command was run.

Every claim marked **verified** was read in the code at the cited lines. Claims marked **inferred** follow from the code plus a host behaviour or a timing figure I could not observe here.

---

## 1. How it works

### 1.1 Triggers

| Trigger | Variant built | Budget of the whole hook | Where |
| --- | --- | --- | --- |
| `SessionStart` source `startup` or `clear` | `startup` / `clear` | 1.5 s | `internal/hook/hook.go:229-235`, `:556-571` |
| `SessionStart` source `compact` | `compact` | 1.5 s | same |
| `SessionStart` source `resume` or `fork` | `resume` / `fork` | 1.5 s | same |
| `UserPromptSubmit`, no `briefed` marker for the session | `startup` (the first-prompt fallback) | 300 ms | `internal/hook/resume.go:130-135` |
| `UserPromptSubmit`, marker present, fewer than 2 refreshes used | `refresh` | 300 ms | `internal/hook/resume.go:135-139` |
| `UserPromptSubmit`, 2 refreshes used | nothing | 300 ms | `internal/hook/resume.go:135-136` |
| `eva-md brief --as ...` | any but refresh | CLI timeout | `cmd/eva-md/commands.go:56-103` |

The host itself allows the hook 10 s at SessionStart and 5 s at UserPromptSubmit (`internal/install/templates.go:72-73`, `:82-83`); the 1.5 s and 300 ms are eva-md's own limits.

No brief is built at all when: the hook runs inside eva-md's own model run (`EVA_MD_INNER`), in a cloud session (`CLAUDE_CODE_REMOTE=true`, `hook.go:284-286`), in an excluded, temporary, wiki or state folder (`hook.go:308-311`), in a desktop spare folder (`hook.go:335-341`), while the chat is off the record (`hook.go:494-497`, `:510-512`), or while transcript recovery has not read the transcript to its end within 100 ms (`hook.go:432-441`, `:503-505`).

### 1.2 Data flow of a startup or clear brief

1. `hook.capture` resolves the project by git (`identity.Resolve`, up to 800 ms), runs the project's catch-up of idle transcripts (150 ms, `hook.go:343-347`), takes the session lock, runs transcript recovery (100 ms), appends a `session_start: <source>` event, then calls `resumeContext` (`hook.go:571`).
2. `hook.brief` reads the `briefed` marker, picks the variant and calls `resume.Build` (`internal/hook/resume.go:120-146`).
3. `resume.Build` (`internal/resume/resume.go:78-173`):
   - `readPage` (`read.go:520-555`): opens the wiki through a pinned `os.Root`, checks `index.md` and the instance (`read.go:564-586`), finds the project's wiki folder by reading `projects/<slug>/now` and, when that is missing, up to 256 other projects' now pages until one names the project's remote (`read.go:592-635`); reads the thread page's frontmatter and body up to the Now's end marker with `okfadapter.ReadHead` (`read.go:647-674`); reads `general/rules` and `projects/<slug>/rules` (`rules.go:42-76`).
   - `readHistories` (`read.go:110-271`): lists the project's inbox folder, sorts by modification time, and for the 64 newest session files (`maxFiles`, `read.go:80`) decodes the last MiB (`tailBytes`, `read.go:29`), splits it into one history per branch, drops off-the-record ranges, and scans the bytes before the tail for "real work" only when the tail shows none (`prefixWork`, `read.go:442-493`). Files found to hold a chat a program ran do not count toward the 64, up to 64 more files and 2 MiB (`read.go:91-94`, `:185-190`). In the general area a file last changed over 12 hours ago is listed but not read (`read.go:102`, `:157-159`).
   - Selection (`resume.go:106-141`): the previous chat is the newest history on this branch, on either host, that is `useful` (a tool or plan entry, or more than 2 prompts and more than 2 replies, and not program-run; `resume.go:424-444`), leaving out this session. A compact brief takes this session's own history. With no previous chat and no Now, a startup or clear falls back to "lineage": the project's most recently active useful chat on any branch (`resume.go:128-141`), and reads that branch's page instead (`resume.go:160-165`).
   - `brief` (`resume.go:281-409`) gathers: first prompts of the chosen chat and of up to 3 earlier chats from the first 64 KiB of each file (`read.go:304-360`); live git (`git.go:44-143`); the latest plan entry (`lines.go:100-109`); a tool-activity heuristic (`lines.go:175-216`); earlier chats and other branches (`lines.go:113-165`); the system pointers from `systems/map.md` (`systems.go:59-87`); the standing rules (`rules.go:82-121`); up to 3 constraints from `.eva-md/knowledge.tsv` (`knowledge.go:43-66`); the footer (`lines.go:302-321`).
   - Assembly order as shown: rules, constraints, then the free text (header, opening request, live git, Now, plan, tool activity, earlier chats, other branches), then the verbatim exchanges, then the system pointers, then the footer (`resume.go:386-392`). Everything is wrapped under one label line in a backtick fence with the info string `eva-memory` (`resume.go:693-721`).
4. Back in the hook: the catch-up update for idle sessions is queued from the file list the brief returned (`internal/hook/resume.go:150-156`); a health notice may replace or accompany the message (`:173-187`); the `briefed` marker is saved, only with 20 ms to spare (`:49`, `:74-76`, `:193-200`); the per-chat prompt state records the system and knowledge pages the brief named, found by running two regular expressions over the brief's own text (`internal/hook/pointers.go:163-187`, `:95-113`); the output is `hookSpecificOutput.additionalContext`, plus on Claude a `systemMessage` "eva.md: earlier work is available for this chat." unless `notice = "problems"` (`internal/hook/resume.go:95-105`, `:185-186`; the default is `start`, `internal/config/config.go:83`).

### 1.3 Other variants

- **Compact** (`resume.go:115-117`, `:282`, `:501-512`): this session's own history on this branch, its last 4 quotable records, no constraints, no earlier chats or other branches, tool activity reserved before the Now.
- **Resume or fork** (`changes`, `resume.go:232-269`): only the rules when their hash changed, the Now in 700 characters when its hash changed and its `as_of` is after this chat's last work, one line for each other chat on the branch that worked since, and live git when anything is dirty. Nothing when nothing changed.
- **Refresh** (`refresh`, `resume.go:206-228`): on a prompt, compares the Now's hash and the rules' hash with the marker's; shows the changed rules and the whole Now again. At most 2 a chat, the count running through compactions (`internal/hook/resume.go:36`, `:157-172`).
- **First chat of a project** with no chat and no Now: the footer alone, unfenced, plus rules, constraints and system pointers when there are any (`resume.go:142-158`).

### 1.4 Caps, budgets and time limits

| What | Value | Where |
| --- | --- | --- |
| Startup or clear cap | 8,000 characters on Claude, 6,000 on Codex | `resume.go:194-197` |
| Compact cap | 6,000 / 4,500 | `resume.go:187-192` |
| Resume, fork cap | 1,500 | `resume.go:178` |
| Refresh cap | 2,500, at most 2 a chat | `resume.go:179`, `hook/resume.go:36` |
| Now budget | 3,200 / 2,400; 1,714 / 1,285 when stale or missing; compact 2,100 / 1,575 | `resume.go:186-202` |
| Exchange budget | 3,085 / 2,314; 4,571 / 3,428 when stale; compact 2,250 / 1,687 | same |
| Newest message | half the exchange budget; the one before it at least 300 | `resume.go:517-527` |
| Label and fence overhead | 494-character label, 515 with the fence (measured) | `resume.go:52`, `:740-742` |
| Rules, constraints, system pointers | 600 each; 3 constraints; 3 pointers of 200 and an index line | `rules.go:19`, `knowledge.go:25-27`, `systems.go:27-35` |
| Footer | 450 | `lines.go:295` |
| Opening request, plan, tool line | 200, 600, 150 and 5 files | `lines.go:94-97`, `resume.go:307`, `lines.go:175-216` |
| Live git | 900 characters, 15 paths, 3 subjects, 64 KiB of output a call | `git.go:63`, `:102`, `:141`, `:169` |
| Session files read | 64, plus 64 program-run files within 2 MiB | `read.go:80`, `:91-94` |
| Tail and head reads | 1 MiB, 64 KiB | `read.go:29`, `:304` |
| Wiki page, system map | 2 MiB, 256 KB | `read.go:618`, `systems.go:33` |
| Live git time | 800 ms, never past the deadline less 50 ms | `git.go:33-55` |
| Code counts for pointers | need 200 ms left, 60 ms a call, 150 ms in all | `systems.go:44-48` |
| Marker reserve | 20 ms | `hook/resume.go:49` |

Budgets are in characters (runes), not tokens, and were scaled from a 7,000-character brief (`resume.go:182-185`).

### 1.5 What it writes

- The inbox: one `session_start` event line (`hook.go:556-560`).
- `<state>/sessions/<host>/<id>/briefed`: JSON `{now_hash, variant, refreshes, slug}`; `now_hash` is the Now's hash and the rules' hash joined by `+` (`hook/resume.go:22-32`, `rules.go:126-137`).
- `<state>/sessions/<host>/<id>/prompts`: the prompt state, with the pages the brief named (`hook/pointers.go:47-59`, `:120-161`).
- A heartbeat (`hook/resume.go:228-257`).
- After the output: a detached `eva-md update` for idle backlogs (`hook/resume.go:150-156`).

The brief itself writes nothing to the wiki and makes no model or network call.

---

## 2. What must be kept

1. **No model, no network, deterministic, fail closed.** `Build` reads local files and local git only; any error yields no context and the hook still exits 0 (`hook.go:115-145`, `hook/resume.go:146-149`).
2. **The fence and the forgery defence.** One label outside the fence; a fence one backtick longer than any run inside (`resume.go:718-736`); speaker headers come only from entry fields and stand at column 0 while every quoted line is indented (`resume.go:565-599`); a middle cut can never leave a quoted line at column 0 (`resume.go:758-775`); free text that reads as a speaker header or as the label is escaped (`internal/freetext/freetext.go:15-87`); control, format and ANSI characters are removed. Covered by `internal/resume/forge_test.go` and `fence_test.go`.
3. **Redaction at display, as the union with the frozen baseline** (`resume.go:605`, `freetext.go:65`).
4. **Off the record is honoured**: ranges are dropped from histories (`read.go:191-206`), an `off_record` before the first prompt hides the opening request (`read.go:347-349`), and no brief is built while a chat is off the record or its transcript is not read to the end (`hook.go:494-512`).
5. **Pinned, bounded reads of the wiki**: `os.Root`, no symlink, hard link or swapped inode (`read.go:33-68`); the instance check before any page (`read.go:564-586`); pages read by path, never the wiki whole, so a stray file elsewhere costs nothing (`TestStrayWikiFileKeepsTheNow`); `ReadHead`, so a Timeline of any length never hides the Now (`read.go:647-649`, `long_page_test.go`).
6. **Bounded cost however long chats grow**: tail and head reads, the cap on files, the context checked between files and lines.
7. **Branch scoping**: another branch's Now never stands in for this branch's except as labelled lineage on a startup or clear (`resume.go:124-141`, `lines.go:72-90`); a resumed or forked chat never takes lineage; a compact brief never borrows another session (`TestCompactDoesNotBorrowAnotherSession`).
8. **An event never makes a chat the newest or the Now stale**: order and staleness use the newest prompt, reply, tool or plan entry (`resume.go:472-484`, `:795-805`; `TestEventsDoNotMakeAnIdleChatNewest`, `TestSessionStartDoesNotMakeNowStale`).
9. **Program-run chats are never the previous chat and cannot crowd out a person's chats** (`resume.go:424-427`, `read.go:185-190`).
10. **Reserved first, never cut**: the newest exchange with both its sides, the footer, the rules, the constraints, the system pointers (`resume.go:325-343`, `:394-407`).
11. **Honest freshness**: the Now is labelled with its `as_of`, as stale when it predates the captured exchanges, and as another branch's when borrowed (`lines.go:72-90`); a pending update is announced (`lines.go:12-15`, `:57-68`); the label says the user and the code are current.
12. **Provenance**: every quoted message carries its speaker, time and a `s:<id8>#<line>` ref with the physical line number, kept through recovery's reordering (`resume.go:567-573`, `read.go:263-265`); every long cut names its ref (`resume.go:578-587`).
13. **Live git that cannot harm or stall**: no hooks, fsmonitor, optional locks, lazy fetch or signature check, a scrubbed environment, a process group killed at the deadline, capped output (`git.go:145-184`).
14. **A brief not delivered is retried**: the marker is saved only with reserve, and the first prompt without a marker builds the brief (`hook/resume.go:74-76`, `:130-135`; `TestBriefTooCloseToDeadlineIsRetried`).
15. **Hidden comments never trigger a refresh**: the hash is taken after item ids and dates are removed (`now.go:13-26`, `read.go:660-673`).
16. **An empty scaffold is not memory**: the undated placeholder Now gives no brief (`read.go:666-671`, `empty_page_test.go`).
17. **Recall never repeats what the brief listed** (`hook/pointers.go:172-187`); keep the behaviour, not the mechanism.

---

## 3. Findings, ranked

### F1. The brief is chosen by recency and by branch, blind to what the new chat is for (recall-quality, high, verified)

- `Options` carries no prompt (`resume.go:30-37`), also on the first-prompt fallback where the hook holds the prompt (`hook/resume.go:146`).
- The previous chat is the newest useful history on the same branch (`resume.go:109-123`); a thread is a branch (`resume.go:807-812`), so a project worked on `main` has one Now for every topic, capped at 40 decisions, 10 next steps and 10 questions (`internal/threadpage/page.go:100-106`).
- Up to 3,085 characters (4,571 when the Now is stale) are verbatim text of whichever chat was last (`resume.go:186-202`), whatever the new chat is about.
- Within the Now the order is fixed: decisions newest first, next, questions, and the summary, last chat and done only in what is left (`now.go:40-44`, `:99-113`).
- Lineage is the most recently active branch, not the branch git says this one was made from (`resume.go:128-141`, `lineage.go:15-30`).
- The prompt-time recall reads only knowledge pages and system names (`hook/recall.go:91-117`, `hook/pointers.go:241-281`), so thread state, Timeline lines and earlier chats that match the prompt reach the agent only if it decides to search.

Cost: the agent starts with the last conversation, which is the right one only when the user continues it. Any other task gets about 2,000 tokens of unrelated text and none of the memory that matches.

Direction: two-stage delivery. At start, a small state part that holds for any task (rules, constraints, where the project stands). With the first prompt, a part retrieved by that prompt over thread items, Timeline, knowledge and chats. Threads become workstreams that a branch can hold several of. Selection and assembly are a rebuild; the safety shell (section 2) is kept.

### F2. The Now is behind exactly when the brief is needed (recall-quality and memory-quality, high, verified for triggers, inferred for timing)

- The update for a chat starts at `SessionEnd` and `PreCompact` (`hook.go:568-570`), at a Stop only once 60 KB are unprocessed, and at a prompt only after a 30-minute gap (`hook/trigger.go:18-24`, `:98-116`). It is a headless model run with a timeout of 180 to 600 s (design.md:605).
- So at /clear the next chat's `SessionStart` runs seconds after the update was spawned, and at compaction the same. `stale` is then true (`resume.go:283`), the Now's budget is halved (`resume.go:198-200`), the heading says it predates the exchanges (`lines.go:84-88`), and the line "An update for the previous chat is pending; its Now arrives with a later prompt." is shown (`lines.go:15`).
- The real state then arrives one or more prompts later, as the whole Now again (F6).
- Staleness is judged against the chosen chat only: `as_of` is one time for the whole thread, the newest entry any pass covered (`internal/update/wiki.go:294-305`), and `updatePending` reads only the chosen chat's watermark (`lines.go:57-68`). An older chat on the branch that was never summarized is not flagged.

Cost: after /clear or a compaction, the commonest hand-over, the first answer is built from a raw tail of at most a few thousand characters and an old summary.

Direction: keep the hand-over state current during the chat, not after it: an incremental checkpoint that is at most one turn old at any boundary (structured facts taken at capture, small frequent passes, or the host's own compaction summary as an input), with per-session coverage recorded on the page so the brief can name every chat the Now does not yet cover. This is shared with the update part; on the brief's side it is an evolve.

### F3. The first-prompt fallback has a fifth of the time, is never measured, and fails silently (reliability-safety, high, verified path, inferred frequency)

- A chat with no `briefed` marker gets the full `startup` build inside `UserPromptSubmit`'s 300 ms (`hook/resume.go:130-135`, `hook.go:229-235`), after up to 100 ms of transcript recovery (`hook.go:432-441`). The host would allow 5 s (`install/templates.go:73`).
- This is the normal path for desktop spare sessions (`hook.go:335-341`; design.md:580) and the retry path for a SessionStart brief that missed its 1.5 s.
- The docs' own figures put a startup build at 325 ms to 0.7 s for 64 files of 1 MiB (known-limitations.md:54). When the build runs out of time it returns nothing, no marker is saved, and the next prompt builds the whole brief again (`hook/resume.go:146-149`), so such a chat pays the wait on every prompt and never gets a brief. Nothing tells the user.
- The fallback always builds `startup`, whatever the chat's real start was (`hook/resume.go:133-134`). A resumed chat that reaches it is shown another chat as its "Previous chat".
- `scripts/benchmark.py` times SessionStart briefs and prompts after a brief (lines 169, 194, 296-299); no sample is a prompt without a marker.
- Whether a claimed spare session fires SessionStart again is listed as unchecked (design.md:1488).

Cost: the desktop app, the surface the owner names first, depends on the weakest path.

Direction: delivery becomes a read of something prepared off the hot path (see F4), with an explicit delivery state per chat (delivered, pending, failed and why) that the doctor and the status tool show, and a budget for the fallback set from what the host allows.

### F4. No session catalog: every chat start re-derives its inputs from raw JSONL (architecture, high, verified)

- `readHistories` lists the folder, sorts by time and decodes up to 64 MiB, plus 2 MiB, to learn five things: which chat was last on this branch, whether it did real work, whether a program ran it, what its first prompt was, and which other branches are active (`read.go:110-271`).
- Each bound exists to cap that scan and each leaves a documented hole: the 64-file cap and the program-file allowance (`read.go:80-94`; known-limitations.md:54), the prefix scan for work before the tail (`read.go:442-493`; known-limitations.md:55), the 64 KiB head read for first prompts (`read.go:304-360`; known-limitations.md:58), the tail-only test for a program's chat, which the update judges by a different rule (`read.go:175-190`; design.md:833).
- The project's wiki folder is found by reading up to 256 other projects' now pages when its own is missing, at every start of a project with no page yet (`read.go:592-635`).
- `read.go` is the most rewritten file of this part: 448 changed lines in 54a6dff, 204 added in 5a8e654 ("Fix 22 bugs"), more in bf296c9 and 51b241c.
- Three per-session stores already exist beside the inbox: the session markers, the catch-up record and the update's watermark.

Cost: the latency risk of F3, and facts the brief cannot know (a program's chat over 1 MiB, a first prompt past 64 KiB).

Direction: a per-session manifest written at capture, under the session lock the hook already holds: branches, first prompt ref, last work time, counts, program flag, ending, files edited, last check run, coverage by the update. Plus a project registry mapping a key to its wiki folder. The brief then reads manifests and one chat's tail. Rebuild the read path; the pinned-open and off-the-record rules carry over.

### F5. Nothing measures whether a brief helps (evaluation, high, verified)

- The tests of this part check caps, variants, bounds, forgery, branch scoping and git safety (`internal/resume/*_test.go`, `internal/hook/resume*_test.go`, `refresh_test.go`). None checks an outcome.
- The one outcome test, the "live resume test" (plant a codename, a decision, a question and a next step; ask "where did we stop?"; design.md:1297-1302), is manual, opt-in and has not run with the update on (design.md:1355, verification.md:145).
- Not measured anywhere: how much of a brief the next chat uses; relevance when the next task differs; how often the Now is stale at /clear; how often the fallback or a refresh is used or times out; whether each host delivers 8,000 characters untruncated; tokens added per chat.
- The hook keeps a heartbeat with duration and failure (`hook/resume.go:206-257`) but records nothing about the brief: variant, size, stale, found, delivered.

Cost: every budget and priority in the brief (about 25 constants) is a guess, and a redesign could not show it is better.

Direction: add a resume evaluation: scripted multi-chat scenarios replayed through the real hooks, with graded questions for continuation, adherence to a decision, a superseded decision, an unrelated next task and a distractor; and local counters per brief, shown by the doctor. Add.

### F6. The refresh repeats the whole Now, cannot tell whose update changed it, and stops after two (recall-quality and invisibility, medium, verified; partly known)

- A refresh fires when the Now's or the rules' hash differs from the marker's (`resume.go:93-99`, `:206-228`). It shows the whole Now in up to 2,500 characters and every rule again. known-limitations.md:60 and :68 say so.
- Not said: the hash cannot tell which session's update changed the page. A long chat's own update, started by its 60 KB Stop trigger or a 30-minute gap (`hook/trigger.go:18-24`), changes the Now, and the next prompt spends one of the chat's two refreshes on a summary of what is already in its context.
- After 2, nothing more arrives in that chat: no later decision of a parallel chat, no changed rule (`hook/resume.go:135-136`).
- Each prompt until then re-reads the now page, the thread page's head and both rules pages (`read.go:520-555`).

Direction: deliver by item, not by page. The Now's items already carry ids (`<!-- i:n3 -->`, `internal/threadpage/page.go:17-30`) and source refs that name their session. The marker records the item ids the chat has seen; a prompt gets only items new to it that another session produced. No fixed count is needed. Rebuild the refresh on the structured model of F9.

### F7. The brief reaches only hook-driven chats on one Mac (reach, high, verified for code paths, inferred for surfaces)

- The only delivery is `additionalContext` from `SessionStart` and `UserPromptSubmit` hooks of Claude Code and Codex (`hook/resume.go:95-105`).
- Cloud sessions return before anything (`hook.go:284-286`; design.md:1133 limits coverage to the CLI and the desktop Code tab, local or SSH).
- The MCP server offers `eva_search`, `eva_show` and `eva_status` (`internal/mcp/tools.go:83-104`); it has no brief, so a client that connects by MCP alone, such as a chat surface without hooks, gets no state unless the agent asks.
- The exchanges come from the local inbox, and no code pulls the wiki (`internal/wikigit` runs no fetch, pull or merge), so a second machine has at best an old Now and no chats.
- The general area holds every folder outside a git repository as one project with one thread (`internal/identity/identity.go:72-108`; `read.go:134`, `:157`, `:260`). A chat in one such folder is shown the newest chat of any other such folder from the last 12 hours, verbatim. This is not in known-limitations.md.

Direction: one context builder with thin adapters: hooks, an MCP brief (tool, resource and server instructions), and a file a host imports where neither exists; identity for folders outside git (a space per folder or per topic; entries already record `cwd`, `hook.go:422`); wiki sync for several machines. Add, and rebuild the general area's identity.

### F8. The brief does not know what the host already put in the chat (invisibility and recall-quality, medium, verified in code, inferred for host behaviour)

- A compact brief keeps the last 4 quotable records, and a host summary is quotable (`resume.go:501-512`). Claude's `PostCompact` stores the compaction summary as such a record (`hook.go:538-550`). If it is stored before `SessionStart(compact)`, the order design.md:590-593 gives, the "newest message" that takes half the exchange budget (`resume.go:334-343`, `:517-523`) is a middle-cut copy of the summary the host has just put in the context, and the real last exchange gets what is left. No test builds a Claude compact brief with a stored summary; `TestCompactKeepsCurrentBranchAndLastTwoExchanges` uses Codex.
- Live git, up to 900 characters and 7 to 10 git processes (`git.go:56-140`), repeats the git snapshot Claude Code gives a chat itself.
- A chat a program runs (`claude -p`, the Agent SDK) gets the full startup brief: `resumeContext` has no test of `p.headless` (`hook/resume.go:56-88`, `:155`). A script's one-off run receives up to 8,000 characters about the person's last chat.

Direction: define each variant against the host's own context: after a compaction, complement the host's summary (decisions, constraints, next steps, refs) and never replay it; omit what the host injects; decide by policy what a program's chat gets. Evolve.

### F9. The assembler is a character packer with fix-up loops, and its output is parsed back by regular expression (architecture, medium, verified)

- `brief` (`resume.go:281-409`) reserves some parts, fits others "whole or left out", then loops cutting text until the cleaned, fenced whole fits (`resume.go:398-407`); `message` and `wrap` have the same measure-clean-cut loops (`resume.go:578-587`, `:697-709`). Redaction runs over the same text several times on the hot path.
- The budgets are ratios of a 7,000-character brief nobody builds any more (`resume.go:186-202`), in characters, which the docs say can pass Codex's token limit (known-limitations.md:59).
- `Result` is text only (`resume.go:46-50`), so the hook learns which pages the brief named by matching `^System: .* show (systems/...)$` and `^Constraint: .* show (knowledge/k...)$` in it (`hook/pointers.go:163-187`).
- The marker's hash is two hashes joined by `+`, with a legacy `refreshed` flag (`rules.go:126-137`, `hook/resume.go:26-28`, `:39-44`).
- Wiki text is redacted again at display. A Now line passes `shown` (`resume.go:227`, `:252`, `:357`), so an identifier after a word such as token is shown as `[REDACTED:...]` though the page holds it, while `eva-md show` keeps it (`resume.go:613-637`); a constraint or system line that cleaning would change is dropped whole (`systems.go:223-228`, `knowledge.go:60`).
- In this hot path okf serves only as a frontmatter parser (`ReadMax`, `ReadHead`); the bounded reads and the root-index check exist in the adapter because okf has neither (`read.go:560-563`, `internal/okfadapter/head.go:15-37`).

Direction: a typed brief: sections with a kind, priority, floor, ceiling, provenance and the ids of what they show; one renderer per host and variant with token-based budgets; the result carries its metadata, which F6's delta and recall's "never repeat" both need. Text is cleaned once, where it is stored. Rebuild.

### F10. The work-state lines are reparsed strings, and the plan is wrong on Claude Code (memory-quality, medium, verified)

- The hook stores a tool call as one formatted line, `<tool> <detail> (exit <code>)` (`hook.go:781-881`), and the brief parses it back (`lines.go:221-230`).
- With Claude Code's task tools each `TaskCreate` or `TaskUpdate` is its own plan entry holding one task, and an update without a subject holds only `task <id>` (`hook.go:1017-1033`). `latestPlan` takes the last plan entry (`lines.go:100-109`), so "Latest plan:" is one task's status line, not the plan. The tests use only Codex's `update_plan` form (`brief_test.go:280`, `resume_test.go:396`).
- The test or build line matches 10 command prefixes after an optional `cd` (`lines.go:168`, `:232-279`); known-limitations.md:57 lists what it misses. Files edited come only from the last MiB.

Cost: "what was I in the middle of" is the part of a hand-over a model cannot rebuild from a summary, and it is the weakest part.

Direction: capture tool and plan events as typed fields; keep the task list's state by id in the session manifest (F4), with files touched and the last check's result; the brief renders open tasks from it. Rebuild the capture shape of tool and plan entries and this section.

### F11. Pulling more costs the user a step, and the brief is visible by default (invisibility, medium, verified text, inferred prompts)

- The footer, the rules' "N more" line, each constraint and each system pointer tell the agent to run the binary by its absolute path through the shell (`lines.go:302-328`, `rules.go:97-115`, `knowledge.go:72-87`, `systems.go:236-264`). In a host's default permission mode a shell command asks the user; a surface without a shell cannot run it. The brief never names the MCP tools, which the docs say also ask on first use (known-limitations.md:311).
- Claude shows "eva.md: earlier work is available for this chat." at every start by default (`hook/resume.go:185-186`, `internal/config/config.go:83`).
- Every brief and refresh spends 494 characters on the label (`resume.go:52`).
- A health problem puts "run eva-md doctor" in the chat and the model's context at each start (`hook/resume.go:173-184`).

Direction: one read-only tool surface the agent can use without a prompt (MCP where connected, allowed at install with the user's consent), named in the brief by host; silent by default; a short label with the tag legend sent only when a tagged line is shown. Evolve.

### F12. A wiki file is trusted as "the user's own words" (reliability-safety, medium for a shared wiki, inferred)

- The rules block is headed "Standing rules, the user's own words" and placed first, outside the final cleaning's cuts (`rules.go:28`, `resume.go:391`). Its lines are whatever `general/rules.md` and `projects/<slug>/rules.md` hold under `## Rules` (`rules.go:58-76`). The Now's tags, which the label says rest on the user's words, are read from the page as text.
- The brief checks the path, the link count and the wiki's instance (`read.go:33-68`, `:564-586`), not who wrote a line. For one person on one Mac that is sound. Once a wiki is shared or pulled (F7), anyone who can push to it writes text that every chat receives under that heading.

Direction: provenance per item (author, source ref, how it was verified) checked when a wiki is pulled or read, and labels that say whose words a line is. Add, before any multi-user or multi-machine reach.

### Lesser points

- A chat of two exchanges with no tool call is not `useful` (`resume.go:443`), so a short decision chat is never the previous chat; under 600 characters the update does not summarize it either (`internal/update/input.go:226`, `:391`). Verified.
- The current session is always left out of a startup or clear pick (`resume.go:111-118`). If a host keeps a session's id across /clear, which `hook/pointers.go:91-93` allows for, the conversation just cleared is the one the brief skips. Inferred; no test starts a `clear` with the id of the chat cleared.
- The marker is saved before the output is written (`hook/resume.go:74`, `hook.go:140`), so an output the host drops is recorded as delivered. Verified; narrow.
- Live git starts 5 processes to look for 5 files (`git.go:114-130`). Verified; cost only.

---

## 4. Where the complexity is essential and where it is accidental

Essential: showing stored text to a model without letting it pose as the user, the system or the brief (the fence, the speaker rule, the indent, the escaping); never following a link out of the wiki or the inbox; honouring off the record; never stalling the host; honest freshness and provenance.

Accidental: bounding a scan of raw JSONL that a catalog would remove (most of `read.go`); packing characters with fix-up loops and ratios of an old size; parsing the brief's own text back; reparsing formatted tool lines; a hash and a counter standing in for item-level delivery; cleaning the same text several times and redacting wiki pages again at display; probing 256 folders for a slug.

## 5. Overall call

Keep the safety shell and the no-model, no-network property. Rebuild the selection, the read path and the assembly on three things the code lacks: a session catalog, a typed brief with item ids, and the first prompt as an input. Evolve the Now's rendering and the git summary into sections of that model. Add an MCP brief, an evaluation of outcomes and per-brief counters. The current design answers "what was the last chat here?" well and safely; the goal needs "what does this chat need?", which recency on a branch cannot answer.

## 6. Open questions

1. On Claude Code, does `PostCompact` run before or after `SessionStart(compact)`? It decides whether a compact brief quotes the host's own summary (F8).
2. Does either host keep a session's id across /clear?
3. Does a claimed desktop spare session fire `SessionStart` again with the real folder, or is the first-prompt fallback the only path there?
4. Should a chat a program runs receive a brief at all, and if so which parts?
5. Is the 300 ms limit on a prompt still wanted for the fallback, given that the host allows 5 s?
6. What does each host inject on its own (git state, project instruction files, the compaction summary), per version, so the brief can complement it?
7. Is a wiki meant to be shared by several people or machines? That decides how much provenance checking a rule or a Now line needs.
8. What is the owner's budget for memory in a chat's context, in tokens, at start and per prompt?

---

## Verification

A second reader tried to refute each finding against the code at commit 51b241c. Read in full: `internal/resume/{resume,read,lines,now,rules,knowledge,git,links,projectpage,lineage,pages}.go`, `internal/hook/{resume,trigger,recall}.go`, `internal/hook/hook.go:90-1075`, `internal/hook/pointers.go:30-300`; in part: `internal/install/templates.go`, `internal/mcp/tools.go`, `internal/identity/{identity,cache}.go`, `internal/health/{health,watchdog}.go`, `internal/update/wiki.go`, `internal/freetext/freetext.go`, `scripts/benchmark.py`, the docs. No test, benchmark or restricted command was run; nothing in the repository was changed.

Result: 7 confirmed, 5 partly, 0 refuted. No finding rests on code that is not there. The corrections below are to scope, to three wrong supporting claims and to severity.

| Finding | Verdict | My severity | Theirs |
| --- | --- | --- | --- |
| F1 relevance | confirmed | high | high |
| F2 stale at boundary | confirmed | high | high |
| F3 fallback | partly | medium | high |
| F4 no catalog | partly | medium | high |
| F5 evaluation | partly | high | high |
| F6 refresh | confirmed | medium | medium |
| F7 reach | confirmed | high | high |
| F8 host-blind | confirmed | medium | medium |
| F9 assembler | confirmed | medium | medium |
| F10 work state | confirmed | medium | medium |
| F11 pull path | partly | medium | medium |
| F12 wiki trust | partly | low | medium |

### F1: confirmed, high

- `Options` has no prompt field (`resume.go:30-37`) and the fallback passes none (`hook/resume.go:146`). The pick is the first useful history on the branch in an order by last work (`resume.go:109-123`, `:795-805`). `threadID` is one page a branch (`resume.go:807-812`). Lineage is the first useful chat of any branch (`resume.go:128-141`). Prompt-time retrieval is the recall index (`hook/recall.go:101-109`) and system names (`hook/pointers.go:241-281`); neither reads thread items, the Timeline or the inbox.
- Correction to the impact: "about 2,000 tokens of unrelated text and none of the thread state" overstates it. The whole brief is capped at 8,000 characters, and the rules, constraints, live git and the branch's Now are project or branch state, not text of the last chat. The part that can be unrelated is the verbatim exchanges, 3,085 characters (4,571 when the Now is stale), with the opening request, plan and tool lines: about 800 to 1,200 tokens. The Now is shown whatever the task, so thread state does arrive; what is missing is any selection by the task.
- Nothing elsewhere handles it: the only help is the footer and the first prompt's reminder (`hook/pointers.go:204-208`), which leave the search to the agent.

### F2: confirmed, high

- `queueUpdate` at SessionEnd and PreCompact only adds to `spawns` (`hook.go:568-570`), which `launch` starts after the output (`hook.go:140-141`). Mid-chat triggers are 60 KB at a Stop and 600 bytes after a 30-minute gap (`hook/trigger.go:18-24`, `:98-116`). `stale` halves the Now's budget to 1,714 of 3,200 (`resume.go:283`, `:198-201`). `updatePending` reads the chosen chat's watermark alone and only for startup or clear (`lines.go:57-68`); a compact brief never shows the pending line.
- `as_of` is one time a thread and only moves forward (`internal/update/wiki.go:296-305`), so a chat older than the chosen one with a backlog is not flagged. The start's catch-up does queue it (`hook/trigger.go:151-168`); the brief just does not say so.
- Not in known-limitations.md as a limit; design.md describes the stale label and the pending line as the intended answer.

### F3: partly, medium

- Right: a prompt with no marker builds the full `startup` variant (`hook/resume.go:130-135`) inside the 300 ms of `budget` (`hook.go:229-235`), the plugin allows 5 s (`install/templates.go:73`), spare folders return before any brief (`hook.go:335-341`), a failed build saves no marker so every prompt builds again (`hook/resume.go:147-149`, `:74-76`), and `scripts/benchmark.py` times no prompt without a marker (every timed prompt follows a SessionStart of the same session: lines 149-151, 296-299, 323-327, 446-449, 489-494).
- Wrong, "silent on failure": a run that passes its deadline is recorded as a failed UserPromptSubmit run by `lateBeat` (`hook.go:217-219`, `:142-144`; `hook/resume.go:338-351`), the doctor fails the host's hook-failures row at half of the last 20 runs (`internal/health/watchdog.go:30-38`, `:67-74`), and the next chat start that does build says so (`internal/health/health.go:108-112`). What is missing is narrower: failed prompts are diluted by the other events' good runs, a build that fails for a reason other than time is recorded as a good run (`hook.go:350-357`), and no state says which chat is still owed a brief.
- Wrong, "never measured": each run's time is kept in the event's heartbeat (`hook/resume.go:210-214`) and the doctor has a hook-times row (`internal/health/times.go:22`). The benchmark gap stands.
- Scope: known-limitations.md:54's 325 ms to 0.7 s is for 64 recent chats of over 1 MiB each. verification.md:100 measures the usual startup brief at 100 to 150 ms with process start, so the fallback fits in the common case. Hence medium.
- Added: when the fallback's build fails, the prompt's pointers and recall are dropped with it, since `resumeContext` returns before `nextState` (`hook/resume.go:64-68`).
- "A resumed chat gets a startup brief" holds only for a resumed chat with no marker at all; with a marker the prompt takes the refresh path and there is no retry of any kind (see M1).

### F4: partly, medium

- Right: `readHistories` lists, sorts and decodes the last MiB of up to 64 files plus 64 program-run files within 2 MiB (`read.go:80-94`, `:110-271`); it also streams every byte before the tail to count lines (`read.go:374-390`); first prompts come from a 64 KiB head read (`read.go:304-360`); `prefixWork` scans the prefix (`read.go:442-493`); `slugCandidates` and `projectSlug` read up to 256 other projects' now pages when the project's own is missing (`read.go:592-635`). The brief tells a program's chat by its tail holding line 1 (`read.go:177-190`), the update by the catch-up record or a host prompt before the watermark (design.md:833).
- Wrong: "the part's most rewritten file". `git log --follow` gives resume.go 8 commits and 281 deleted lines, read.go 5 commits and 202.
- Overstated: "start latency grows with the inbox". It grows to a bound: verification.md:119-128 measures 38 ms on an empty inbox, 130 ms for one 200 MB session file and 325 ms for 64 files of 5 MB, against a 1.5 s SessionStart deadline.
- Each hole is already in known-limitations.md (:54, :55, :58), so `known` is closer to true for the holes; the missing catalog itself is not listed. An architecture cost, not a present failure: medium.

### F5: partly, high

- Right: no test in `internal/resume` or `internal/hook` grades an outcome; the live resume test is opt-in and by hand (design.md:1297-1302), an automated one is "planned" (design.md:1353), and none has run with the update on (verification.md:145). A heartbeat keeps the time, the failure flag, the entrypoint and the transcript flags, nothing of a brief (`hook/resume.go:210-214`, `:245-255`).
- Wrong: "delivery on real hosts is unconfirmed". verification.md:66-72 records hand-run checks that passed: planted facts recalled in a new chat, after /clear and after forced compaction on the Claude Code CLI, the Codex CLI across hosts, and the Claude desktop app at startup and after /compact, with tools and the host's memory off. Still open: the Codex desktop app, any run with the update on, and whether 8,000 characters arrive whole.
- The severity stands for a redesign: nothing repeatable can show one brief is better than another.

### F6: confirmed, medium

- A refresh fires on any difference between the page's hash and the marker's (`resume.go:93-99`) and shows the whole Now (`resume.go:206-228`). Only the hook writes the marker (`session.Briefed` is used at `hook/resume.go:74` and `:122`, nowhere else), so the chat's own update, from its Stop or gap trigger, reads as a change. After two, the prompt path returns at once (`hook/resume.go:135-136`), a changed rule included.
- known-limitations.md:60 and :68 list the repeat and the cap; the self-echo is not listed. `known: true` is right for half of it.
- A second cause the report does not name: the marker holds no branch, so a checkout mid-chat also reads as a changed Now (see M2).

### F7: confirmed, high

- `reply.encode` is the only output of a brief from the hooks (`hook/resume.go:95-105`); `CLAUDE_CODE_REMOTE=true` returns before anything (`hook.go:284-286`); the MCP server has `eva_search`, `eva_show` and `eva_status` (`internal/mcp/tools.go:81-111`), and its instructions name them but carry no state (`internal/mcp/instructions.go:18`); the only git commands that bring a wiki in are the two clones at setup (`internal/wikiinit/init.go:90`, `cmd/eva-md/setup.go:1080`); `identity.General()` is one key, one slug and one branch for every folder outside git (`internal/identity/identity.go:72-76`, `resume.go:807-810`), bounded only by the 12-hour window (`read.go:102`, `:157-159`, `:260`).
- Precision: "the only delivery" leaves out `eva-md brief --as` and `eva_show <thread>#Now`, which are pull paths, as the impact says.
- Context for the team: cloud sessions and the chat tabs are out of v1 by decisions.md:35, and the general area is decisions.md:65. The finding reopens agreed decisions; the facts are as stated. The general-area lumping is not in known-limitations.md.

### F8: confirmed, medium (inferred, as marked)

- `quotable` takes `host_summary` records and a compact brief keeps the last 4 (`resume.go:501-512`); PostCompact stores the summary as one (`hook.go:538-550`); the newest record gets half the exchange budget, 1,125 of 2,250 characters on Claude (`resume.go:517-523`, `:192`). design.md:590-593 itself puts PostCompact before SessionStart(compact), so by the design's own order the newest "message" of a Claude compact brief is the host's summary. No resume test builds a compact brief with a `host_summary` record (the kind appears only in forge, hook, inbox and search-index tests).
- A headless chat's SessionStart reaches `resumeContext` (`hook.go:554-571`); `p.headless` is read only for the catch-up's sweep (`hook/resume.go:155`). Its prompts get nothing (`hook.go:518-521`), so the claim holds for the start only. `TestProgramRunChatAsksForNoUpdateOfItsOwn` runs that start through `brief`.
- Live git is 7 to 10 processes: status, up to 3 for a moved HEAD, 5 `rev-parse --git-path`, 1 stash count (`git.go:56-140`).
- verification.md:70 supports it: after /compact in the desktop app "the context also held the host's own compaction summary".

### F9: confirmed, medium

- Reserve, fit and the cut loop: `resume.go:325-343`, `:359-374`, `:398-407`; ratios of 7,000: `resume.go:186-202`; `Result` is text, a hash, a slug and files (`resume.go:46-50`); the hook recovers the pages named by two regular expressions over the text (`hook/pointers.go:166`, `:170`, `:174-187`); a Now passes `shown`, which redacts (`resume.go:357`, `internal/freetext/freetext.go:60-66`), while `eva-md show` uses `cleanPage`, which keeps what the commit scan reads as identifiers (`resume.go:622-637`, `show.go:100`, `:111`).
- One more cost of parsing the text back: the Now's summary and last-chat lines are free lines at column 0 (`now.go:67-70`), so a line shaped `Constraint: ... show knowledge/k12` there is recorded as shown and recall then skips that page for the chat. Suppression only, and it needs model-written text of that exact shape.

### F10: confirmed, medium

- `planText` gives one task a call, and `task <id>` when an update names no subject (`hook.go:1017-1033`); `hook_test.go:463` pins the stored line as `TaskUpdate [x] task 12 (exit 0)`. `latestPlan` returns the last plan entry (`lines.go:100-109`), so the brief prints `Latest plan:` over that one line. No resume test uses a task-tool entry (`TaskCreate`, `TaskUpdate` and `TodoWrite` appear only in `hook_test.go` and `update/input_test.go`).
- `activity` reparses `<tool> <detail> (exit N)` (`lines.go:175-230`) against 10 prefixes (`lines.go:168`).
- Caveat on frequency: design.md:455 says these tools are off on current default Claude models unless turned on, so Claude plan entries are optional. Where they are on, the line is wrong every time.

### F11: partly, medium

- Right: the footer, the rules' "N more" line, each constraint and each pointer name `<Share>/bin/eva-md` by absolute path (`lines.go:303-306`, `rules.go:97`, `:111`, `knowledge.go:49`, `:74`, `systems.go:246`); no file of `internal/resume` names an MCP tool; the plugin grants no permission (no `permissions` or `allowed-tools` in `internal/install` or its templates); the label is 494 characters (`resume.go:52`, counted).
- Off: the start line is not an oversight but decisions.md:100, "one line at the start of a chat, plus a notice only when something is wrong". And a health notice enters the model's context only when there is no brief (`hook/resume.go:180-182`); with a brief it is the `systemMessage` alone (`:183-184`).
- The permission prompt is inferred from host behaviour, as marked; known-limitations.md:311 lists the same prompt for the MCP tools.

### F12: partly, low

- Right: `ruleLines` takes every list line under `## Rules` (`rules.go:58-76`), the block is headed "the user's own words" (`rules.go:28`) and reserved first (`resume.go:321`, `:391`); the read checks are the path, the links and the instance (`read.go:33-68`, `:564-586`).
- Scope: no code path today brings another writer's lines into a wiki in use. Nothing fetches, pulls or merges (the only clones are at setup), and a wiki of another instance is refused (`read.go:578-584`). Rules the update writes are checked against the user's words (known-limitations.md:214). So the impact is a precondition for a later multi-writer design, which the finding says itself; as a problem in this code it is low. The one present path is a wiki cloned at setup, whose rules pages are then shown under that heading.

### Missed

**M1. A compact or resume brief that SessionStart fails to deliver is never retried** (reliability-safety, medium, verified).
The fallback fires only when the session has no briefed marker (`hook/resume.go:133-134`). A compacted or resumed chat already has one, so its prompts go to the refresh path, which sends something only on a hash change (`:135-139`, `:157-162`). Every way a start can lose its brief leaves that marker in place: recovery behind (`hook.go:503-505`), the lock (`hook.go:361-364`), the 1.5 s deadline (`hook.go:217-219`), a failed build (`hook/resume.go:147-149`), a marker save past its reserve (`:74-76`). The marker's `Variant` is written and never read. So after a compaction, one of the three moments the product names, a lost brief stays lost: the last exchanges, the plan and the tool line never arrive. known-limitations.md:42 and :56 cover two causes for a chat start, not the missing retry.

**M2. A branch switch mid-chat reads as a changed Now and uses up a refresh** (recall-quality, medium, verified).
The marker holds one hash and no branch (`hook/resume.go:22-32`). The hook takes the branch from HEAD on each event (`hook.go:394-409`; the identity cache's fingerprint covers HEAD), and a refresh reads the page of that branch (`resume.go:91-99`, `read.go:549`). After a checkout to a branch that has a thread page, the next prompt gets that branch's whole Now under "changed since it was last shown in this chat" (`resume.go:217-221`), with no branch named, and one of two refreshes is spent; switching back spends the other. `refresh_test.go` has no branch switch, and neither doc mentions it.

**M3. After a compaction, constraints and facts already sent are not restated and stay suppressed** (recall-quality, medium, verified; known).
A compact brief lists no constraints (`resume.go:321-324`). The per-chat "already shown" state is reset only by a startup or clear brief (`hook/resume.go:178`, `hook/pointers.go:104-107`), and recall and the prompt pointers skip every page in it (`hook/recall.go:107-109`, `hook/pointers.go:267`). So once the host has compacted away the constraints and facts the chat was sent, eva-md neither restates the constraints nor lets recall or a pointer send those pages again for the rest of the chat. known-limitations.md:140 and :161 list the once-a-chat rule through a compaction; design.md:1011 lists the omission. The report's item 17 in "What must be kept" keeps this dedupe without noting that a compaction breaks its premise.
