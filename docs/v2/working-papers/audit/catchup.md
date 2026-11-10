# Transcript recovery (catch-up): audit

Scope: `internal/catchup` (catchup.go 1,509 lines, parser.go 1,228, drift.go 103, prune.go 170, forget.go 60; 2,896 lines of tests), its callers in `internal/hook`, `internal/update`, `internal/health`, `cmd/eva-md`, and the matching parts of docs/design.md (line 91, 489-527, 1227-1248) and docs/known-limitations.md ("Transcript recovery", lines 35-49).

All paths below are under `the repository`. "Verified" means I read the code path; "inferred" means the claim depends on host behaviour or timing I could not observe here.

Git history: 7 commits touch the package in 11 days. The heaviest: 54a6dff (+499, drift, elsewhere, slow rows), 51b241c (+845, prune, forget, symlinked stores, multi-line command cut), 5a8e654 (+302, baseline resets). Every one of them adds a special case to the same hook-sliced loop.

---

## 1. How it works

### Purpose and stance

Package doc: "repairs missing final exchanges from registered host transcripts. It never discovers old chats or imports anything preceding the first hook" (internal/catchup/catchup.go:1-2). Hooks are the primary capture path; the transcript is read only to fill what a hook missed, and only for sessions a hook registered.

### Triggers

| Trigger | Call | Budget | Where |
| --- | --- | --- | --- |
| Every captured hook event (all events, both hosts) | `Register`, then one `Recover` pass | 100 ms + Slack for the pass, inside the hook's 300 ms or 1,500 ms | internal/hook/hook.go:424, 432-441, 503; budgets hook.go:229-235 |
| Stop that carries a reply | `RecoverAfterReply` (same pass, counts toward drift) | same | hook.go:275-279; catchup.go:268-270 |
| SessionStart | `RecoverProject`: other idle sessions of the same project | 150 ms + Slack in total | hook.go:342-347; catchup.go:1080-1155 |
| UserPromptSubmit with a record control | `SetOffRecord` (moves cursor to end, blocks or unblocks) | hook budget | hook.go:461-493; catchup.go:200-240 |
| Daily maintain | `Prune` only (no recovery) | - | cmd/eva-md/maintain.go:147; prune.go:42-66 |
| `eva-md inbox clear --apply` | `Forget` | - | cmd/eva-md/inbox.go:346; forget.go:26-60 |
| Doctor, chat-start notice | `ReadDrift` | - | internal/health/watchdog.go:89-104; health.go:113-116 |
| Update job | `Headless` only (reads the record's flag; never recovers) | - | internal/update/pass.go:168; job.go:224 |

There is no background or scheduled recovery. The update job and maintain never read a transcript.

### Data flow of one pass

1. `Register` (catchup.go:123-194) loads the session's record, keeps the old cursor, and starts a new baseline at the transcript's current size when the record is new, or the path, project, cwd or branch changed, or the chat is or was blocked, or the file's device/inode changed, or the file shrank (line 186-190). An unchanged registration within 10 s is not saved (158-163). An event with no transcript path keeps the old one (134-136); a transcript that cannot be opened marks the session `Missing` (164-176).
2. `recoverPass` (272-331): returns at once when blocked, missing or off the record (290-299); refuses a replaced or truncated file (316-318); for Codex reads the first 64 KiB and requires a `session_meta` row whose id and cwd match (319-325; parser.go:372-388).
3. `run` (359-425): at most one 1 MiB window of whole rows per pass (`maxTail`, line 29; `windowed`, 386-397). A row that does not end within the window is streamed past in 1 MiB chunks, at most 64 MiB a pass (354-357, 727-767), by `rowScan`, a hand-written JSON state machine whose whole state is saved in the cursor record so a later hook invocation resumes it (parser.go:405-1089; `Source.Skip`, catchup.go:76-78).
4. `rows` (487-549) decodes each row with `parser.read` (parser.go:45-228). Kept: a Claude `user` row with a `promptId` and no host wrapper (parser.go:93-121), a Claude `assistant` row whose `stop_reason` is `end_turn` or `stop_sequence` and that is not synthetic or an API error (121-140), a Codex `user_message`, final `agent_message`, `task_complete`, or final assistant `response_item` (156-195). Everything else is read and dropped: meta, sidechain, agent, non-human origin, compaction summaries (78-109), tool results (userText, 271-272), mid-turn assistant rows (131-134).
5. A record control in any user row is checked on the raw text before anything else (parser.go:204-217); "off the record" blocks the record first, then writes the markers (catchup.go:650-663). "Back on the record" is never acted on by recovery (parser.go:214-216; catchup.go:777-778).
6. `store` (592-625): skips a turn+role already in the inbox before redacting (602-604); otherwise redacts within one of three bounds (879-883: whole up to 200 KiB else 96+96 KiB; 48 / 22+22; 16 / 6+6), racing the deadline in a goroutine (912-932). A row that outruns the pass is marked slow and retried, three tries a level unless it began with most of the budget left (631-645); past the last level it becomes a gap event (609, 620-622).
7. `add` (681-709) appends through `inbox.Store.AppendRedacted` with `origin=catchup`, deduplicated against the last 1 MiB of the session's inbox file (1391-1431) by `turn:<id>:<role>` or a hash of role + text + timestamp to the second (1382-1390).
8. The cursor moves past each handled row and is saved on every way out (544-545, 818-840), with a 50 ms grace past the deadline (848-855). A pass with more to read returns `ErrPending`.
9. Back in the hook: any result other than `ErrPending`, `ErrPrivacyBoundary` or a timed-out context counts as caught up (hook.go:437-440). Not caught up means the hook stores only the event's fixed name, and no brief (hook.go:503-505). When the pass recovered rows, the hook checks `Contains` before appending its own prompt or reply (hook.go:574-580; catchup.go:1479-1498).

### What it writes where

- `<state>/catchup/<host>/<session>.json`: the `Source` record (catchup.go:61-92, 1216-1234), mode 0600, atomic write with file and directory fsync (internal/paths/paths.go:166, 178). It holds the cursor, device/inode, the registered home, path, cwd, project, branch and model, `Blocked`/`Reason`, `Missing`, `Headless`, `Elsewhere`, the oversized-row scanner state, the slow-row state and the drift counter.
- `<state>/catchup/drift-<host>.json` (drift.go:27-88).
- Session markers `OffRecord` (value `1` when recovery found the control, `asked` when the user asked through the hook) and `WasOff` (catchup.go:45-52, 245-250, 650-663).
- Inbox entries with `origin=catchup`: prompts, final replies, and the gap events `gap: transcript record too large` and `gap: transcript record too long` (catchup.go:54-58, 666-676).

### Caps, budgets and limits

| Limit | Value | Where |
| --- | --- | --- |
| Rows read per pass | one 1 MiB window | catchup.go:29, 386-397 |
| Pass time in a hook | 100 ms (+Slack) | hook.go:433 |
| Project catch-up at chat start | 150 ms, 5 sessions, idle 2 min or more, from the 256 newest of at most 4,096 records a host | hook.go:344; catchup.go:1099, 1115-1117, 1124, 1134 |
| Attempt recorded before decoding | rows of 256 KiB or more | catchup.go:117, 556-576 |
| Oversized-row scan | 1 MiB chunks, 64 MiB a pass | catchup.go:354-357 |
| Scanner | depth 32, key 64 bytes, fields 256 / 4,096 bytes | parser.go:489-502 |
| Redaction bounds | 200 KiB whole or 96+96; 48 or 22+22; 16 or 6+6 KiB | catchup.go:879-883 |
| Redaction without a goroutine | 8 KiB | catchup.go:892 |
| Tail-window context | 4 KiB | catchup.go:898 |
| `Contains` text-key bound | 48 KiB | catchup.go:888, 1489 |
| Dedupe window | last 1 MiB of the session's inbox file | catchup.go:1411 |
| Codex identity header | first 64 KiB | catchup.go:320 |
| Grace past a deadline | 50 ms | catchup.go:848 |
| Unchanged registration not saved | within 10 s | catchup.go:158-163 |
| Drift signal | 3 reply-ending turns with no recognized row | drift.go:19; catchup.go:466-474 |
| Record retention | 30 days, newest 2,048 a host | prune.go:22-25 |
| Streamed control check | texts over 16 KiB | parser.go:352 |

---

## 2. What must be kept

These are the invariants and hard-won fixes a redesign must carry over, whatever shape the code takes.

Trust in what is read
- Only a transcript a hook named is ever opened; host stores are never swept (catchup.go:1-2, 1077-1079).
- The path must be absolute, hold nothing the redactor would change, be named for the session (`<id>.jsonl` on Claude, `...-<id>.jsonl` on Codex), lie inside the host's store where its symlinks lead, and be a regular, singly linked file opened without following a link (catchup.go:1258-1287, 1235-1257, 1310-1334). Tests: store_test.go:20, 82; catchup_test.go:349.
- Codex rows carry no session id, so the opening `session_meta` must match the session and folder (parser.go:370-388).
- Device, inode and size are checked every pass; a replaced or shrunken file is never read at a stale offset (catchup.go:316-318, 183-190).

Privacy boundary
- A record control is read from the raw text, before redaction, in every user row: beside an attachment (parser.go:250-282; regression_test.go:448), indented or punctuated (catchup_test.go:365), and inside a row too large to decode (parser.go:1051-1089; regression_test.go:240).
- The block is saved before the markers are written and asserted again on later passes (catchup.go:278-289, 650-663).
- Recovery never turns capture back on (parser.go:214-216; catchup.go:777-778).
- Going off or back on the record moves the cursor to the transcript's end before the block is lifted, so nothing said off the record can be recovered later (catchup.go:200-240; catchup_test.go:220).
- `WasOff` keeps compaction summaries out once a chat has been off the record (catchup.go:242-250).
- A size limit never takes a chat off the record; it leaves a gap event, written before the cursor moves (catchup.go:258-259, 772-803; scan_test.go:320).
- A pruned or forgotten record never re-enables capture: a blocked record stays while the marker is missing (prune.go:157-161; forget.go:37-38).

Authorship
- A Claude user row is the user's typed prompt only by allowlist: it carries its own `promptId` and starts with no host wrapper (parser.go:116-120, 320-328). Meta, sidechain, agent, non-human origin, compaction summary and synthetic rows never become the user's words (parser.go:78-109, 283-291; catchup_test.go:146; regression_test.go:409).
- Synthetic model placeholders and API-error rows are not replies (parser.go:125-130; catchup_test.go:193).
- A program-run chat's prompts are never stored as the user's, though a control in one is honoured (parser.go:218-222; drift_test.go:19).
- Recovered rows are the main thread's and carry no agent id (catchup.go:293-295; catchup_test.go:447).
- A row is never filed under a project or branch it was not written in (baseline reset, catchup.go:186-190; baseline_test.go:163).

Robustness
- No single row can stop recovery for good: bounded tries, then a gap event (catchup.go:587-645; wedge_test.go, scan_test.go:560).
- Progress is saved per row; a pass cut short resumes and repeats no work (catchup.go:544-545, 816-840; scan_test.go:680).
- A redactor panic becomes a gap, not a stuck cursor (catchup.go:934-943; scan_test.go:632).
- Redaction is always `redact.Redact`, the union of the baseline and current rules (catchup.go:43).
- Removing a capture also removes the record that could bring it back (forget.go:13-25).
- Records are removed only under the session's lock and only if unchanged since listed (prune.go:139-170).
- Format drift is detected and reported rather than silently accepted (drift.go; watchdog.go:89-104).
- Tests never depend on machine speed (catchup_test.go:26-29; regression_test.go:25-74).

---

## 3. Where it falls short

Ranked by consequence for the goal.

### F1. The transcript reader throws away most of what the transcript holds (memory-quality, high)

Verified. The transcript is the only complete record of a chat, yet the reader extracts exactly what a hook would have given: typed prompts and final replies.

- Assistant text written between tool calls is dropped: any `stop_reason` other than `end_turn` or `stop_sequence` returns nothing (parser.go:131-134). The test names it "DO NOT SAVE NARRATION" (catchup_test.go:113-116).
- Tool calls and tool results are never read for content (parser.go:271-272; assistant content with any non-text item returns the empty string, parser.go:239-245).
- Sidechain and subagent rows are skipped whole (parser.go:78-83); subagent transcript files are never opened.
- A turn the user interrupted has no `end_turn` row, so recovery adds nothing for it; the interrupt notice itself is a host wrapper (parser.go:322). design.md:525 says "Stop does not fire after an Esc interrupt, so catch-up covers it": by the parser's own rules it cannot, unless the host writes an `end_turn` row for a turn that was cut short (inferred for the host side).
- The row's own `uuid`, `parentUuid`, `gitBranch` and `version` fields are never read (parser.go reads only the fields at 58-96).

This is a recorded decision (design.md:1391: "Assistant narration between tool calls and all tool output are not saved ... Transcript formats are not a stable interface"), not listed in known-limitations.md. For the goal it is the largest fidelity loss in the system: after a 40-tool-call turn the memory holds the prompt, one 200-character line per tool and "Done", while the reasons, the rejected options and what a subagent found are in a file eva-md already has open.

Direction: make the transcript a first-class, enriching source. One normalized event stream per session (prompt, assistant segment, tool call with structured fields from its input, subagent report, interruption), produced by a versioned adapter per host, with hook payloads as the guaranteed floor when the format is not recognized. Tool output stays out by default. Action: rebuild.

### F2. Capture is gated on recovery: while a pass is behind, the hook discards what it holds (reliability-safety, high, known)

Verified; listed in known-limitations.md:41.

- A pass reads one 1 MiB window and stops (catchup.go:386-397), or stops at a slow row (612-615). The hook then keeps only the event's fixed name: no prompt, reply, tool line, compaction summary or brief (hook.go:503-505, 454-459). PostToolUse and Stop have no event name (hook.go:657-675), so nothing at all is stored for them.
- Prompts and replies can be recovered later; tool lines and host summaries cannot, since recovery never produces them.
- A Stop whose transcript copy of a long reply outruns the pass loses the hook's own copy; the next pass may keep 22 + 22 KiB where the hook would have kept 200 KiB (catchup.go:631-640, 879-883; inbox.go:24, 122-130).
- More than 1 MiB between two events is ordinary in heavy sessions: one large file read or a batch of parallel reads (inferred frequency; nothing measures it).

The guarantee this buys is uneven. An error is treated as caught up (see F7), so the system pays in lost memory when recovery is slow and stores freely when recovery is broken.

Direction: take recovery out of the capture decision. Hooks append what they hold at once; an ingest that runs off the hot path verifies each session up to a "verified-through" point; readers that can leak (brief of another chat, search, update) stop at that point; a control found late removes what was captured after it. Action: rebuild.

### F3. Recovery runs only inside hooks, so a missed tail arrives late or never (recall-quality, high)

Verified, except where marked.

- No background recovery exists. Update and maintain never recover (update/pass.go:168 and job.go:224 call only `Headless`; maintain.go:147 only `Prune`). An update can summarize a chat whose last reply is still only in the transcript.
- A chat that ended without a further hook (crash, force quit, lid closed) is recovered only by a later SessionStart in the same project on the same machine (hook.go:343-347).
- That catch-up skips a session registered less than 2 minutes ago (catchup.go:1124). Restarting straight after a crash, the case where continuity matters most, gets a brief without the lost tail, and nothing in the new chat looks again.
- It takes the 5 most recently registered sessions of the project whether or not they have unread bytes (catchup.go:1130-1136). Caught-up, blocked and missing sessions use the places, so an older chat with a real tail can be starved until it is pruned.
- Listing costs up to 4,096 stats and 256 record reads per host on every chat start, inside 150 ms, because there is no index by project (catchup.go:1090-1129).
- Rows recovered late carry the transcript's timestamp (parser.go:58). The update's out-of-order guard then keeps the Now and ignores every knowledge change when another session has updated the page since (update/validate.go:103; update/input.go:277-281; design.md:961, 967). The conclusion of an abandoned chat can reach the Timeline but never the Now or a knowledge page (inferred from the two verified pieces).

Direction: a work queue of sessions with unread transcript bytes, drained by a detached, single-flight ingest that any hook can start, that runs before every update pass and in the daily job, and that is indexed by project. Action: rebuild the trigger model.

### F4. About 1,200 of 3,070 lines exist to fit an unbounded job into 100 ms slices of a hook (architecture, high)

Verified.

- `rowScan`, a resumable streaming JSON parser with surrogate, escape and UTF-8 handling, whose state is serialized into the cursor record between hook invocations (parser.go:405-1089; catchup.go:76-78).
- Slow-row levels, tries, the `fresh` judgement and the decode-attempt counter saved before a big row is decoded (catchup.go:101-117, 506-520, 551-645).
- Bounded redaction with head and tail windows, which relies on the redactor's rules matching within a line "with three exceptions" (catchup.go:945-966) and copies the redactor's cleaning and two of its patterns (catchup.go:1039-1072). A new multi-line rule in `internal/redact` would silently break the windowing; known-limitations.md:16 already records one such case.
- Redaction raced in an abandoned goroutine, and a grace period past the deadline (catchup.go:909-932, 842-855).
- Three pairs of implementations that must agree, held together by parity tests: `parser.read` and `rowScan.verdict` (scan_test.go:105), `session.RecordControl` and `ctlText` (scan_test.go:45), `redact`'s cleaning and `cleanLikeRedact` (scan_test.go:512). About 1,400 of the 2,900 test lines test this machinery (scan_test.go, wedge_test.go, regression_test.go).
- The record conflates five things: cursor, session registry (path, cwd, project, branch, model), the program-run flag the update reads (catchup.go:1168-1174), a backup privacy flag (prune.go:132-135), and scanner and drift state. Pruning the cursor therefore changes how the update treats a chat (known-limitations.md:38).

The essential problem is small: read JSONL forward from a cursor, classify rows, redact, append idempotently, honour a control. Outside a hook it needs none of the above: `encoding/json`'s decoder, full redaction, no per-row deadline.

Direction: a detached ingest worker (eva-md already starts detached jobs from hooks), a session registry separate from a disposable cursor, and deletion of the scanner, the slow-row ladder, the redaction windows and the replicas. Action: rebuild.

### F5. Memory starts empty, and chats without a hook are invisible for good (reach, high)

Verified. A new registration starts at the transcript's current end (catchup.go:186-187; hook.go:423). History backfill is "Ruled out" (docs/decisions.md:116; design.md:1403). A session whose hooks never fired (Codex before hook trust, hooks disabled, a host surface that fires none) is never read. Not in known-limitations.md.

For a product meant to feel built in, the first week is the test: months of the user's own chats sit in `~/.claude/projects` and `~/.codex/sessions` and the memory knows none of it.

Direction: once ingest is a standalone pipeline, an opt-in, bounded import (per project, recent first, on-record only, same redaction, with the model cost stated and capped) is the same code run from offset 0. This reopens a recorded decision and needs the owner's call. Action: add.

### F6. Correctness rests on undocumented host fields, with no real-host fixtures and no version awareness (evaluation, high, partly known)

Verified for the code; the host side is inferred.

- Recovery of prompts needs `promptId`; of replies, `stop_reason`; of authorship, `isMeta`, `isSidechain`, `turnOrigin`, `origin.kind`, `isCompactSummary`; on Codex, `phase == "final"` and a `session_meta` row inside the first 64 KiB (parser.go:66-140, 150-195; catchup.go:320).
- Every test row is a hand-built map (catchup_test.go:85-87; baseline_test.go:16-19). The repo holds no captured transcript from any host version. The one real-host check was manual (docs/verification.md:74).
- The drift signal fires only when no row at all is recognized for three turns (drift.go:15-19; catchup.go:466-474). A change that breaks only prompts or only replies, or Codex identity, is silent (known-limitations.md:42-43).
- The fix for drift is a new release the user must install by hand; the chat-start notice asks them to run the doctor (health.go:113-116; watchdog.go:101).
- Authorship is decided by prefix denylists kept in two places that differ: the hook's `synthetic` (hook.go:679-694) and the parser's `syntheticText` and `hostWrapper` (parser.go:283-291, 320-328). A new host wrapper is stored as the user's words until someone adds a prefix.

Direction: a golden corpus of redacted real transcripts per host version, run in CI; adapters selected by the transcript's own version field; and reconciliation as the drift detector: hooks and transcript report the same turns independently, so a per-turn comparison (counts only) flags partial drift at once. Action: add.

### F7. Failures and degradations of recovery leave no trace (evaluation, medium)

Verified.

- Every error other than pending or privacy counts as caught up, with no log, counter or failed heartbeat (hook.go:432-441). That covers a replaced or truncated transcript (catchup.go:316-318), a Codex identity mismatch (319-325), an unreadable transcript, and an unreadable record.
- `Register`'s error is discarded (hook.go:424). A record that fails to load (damaged, or any version but 1: catchup.go:1207) blocks registration (127-130) and stays until it is 30 days old or past the newest 2,048 (prune.go:104-106). A future schema change would switch recovery off for every existing session, silently.
- Nothing counts rows recovered, passes that ended pending, events reduced to their name, gap events written, or duplicates. The heartbeat holds only time, failure and duration (hook/resume.go:245-255).
- The benchmark measures one small assistant row per Stop (scripts/benchmark.py:221-233); no measure covers a large row, a backlog or a slow redaction.

Direction: content-free counters per session and host (bytes behind, rows recovered, events deferred, errors by reason) surfaced in status and the doctor, and a recovery-rate measure in the evaluation suite: replay a real transcript with a chosen fraction of hooks dropped and score what the inbox ends with. Action: add.

### F8. The privacy rule is strict when recovery is slow and absent when it resets or fails (reliability-safety, medium)

Verified. Rows lost at a reset are known (known-limitations.md:46); the inconsistency is not.

- Capture is dropped while unread rows "may hold an off-the-record control" (hook.go:498-505).
- A new baseline jumps the cursor over unread rows without scanning them, on a change of folder, branch or project, a replaced file or a shrunken one (catchup.go:186-190). `Register` runs before the pass (hook.go:424 before 503). A plain `cd` inside the same repo is enough.
- A pass that errors reads nothing and the hook stores normally (hook.go:437-440). So does a session with no transcript (catchup.go:290-292).
- Recovery never consults exclusions: a path excluded after a session registered can still have its tail recovered by another chat's start (no config import in catchup.go:23-26; catchup.go:1118-1127).

Direction: one explicit state per session (verified through offset N, behind, unreadable) and one policy applied to all of them; attribute each row by its own folder and branch fields off the hot path so the cursor never needs to jump. Action: rebuild.

### F9. Deduplication has no stable identity for a row (memory-quality, medium, partly known)

Verified in code; how often it bites depends on host fields.

- Keys are `turn:<id>:<role>` or a hash of role, text and timestamp to the second (catchup.go:1382-1390). The comment concedes that text keys "rarely match across sources" (885-887).
- The parser's current turn is not saved between passes (parser.go:34; `Source` has no such field). A reply read in a later pass than its prompt gets a turn id only if the row carries one, else the weak key.
- The hook checks for a duplicate only when its own pass recovered something (hook.go:575). If another invocation recovered the reply first, such as a late asynchronous PostToolUse, the Stop hook appends a second copy (inferred timing).
- One reply per turn id: a second final reply under the same `promptId` is skipped (catchup.go:602-604), and the hook's own copy is dropped too whenever its pass recovered anything (catchup.go:1484-1486). Whether a host reuses the id is an open host question (design.md:1487).
- Only the last 1 MiB of the session's inbox file is checked (catchup.go:1411).
- Spurious entries: a row over 1 MiB that would never have been stored, such as a tool result holding a PDF, leaves `gap: transcript record too large` (catchup.go:785-793; regression_test.go:211-235), and the update input shows events to the model (update/input.go:495).

Direction: key every entry on the host's own row id, or on session plus byte offset, so appends are idempotent by construction; make the transcript the one writer of prompts and replies where it is readable so there is nothing to reconcile; mark a hole only where storable text was lost. Action: rebuild.

### F10. Recovery can take a chat off the record without the user asking or knowing (invisibility, medium)

Verified in code; the trigger rows are as the tests model them.

- The control check runs on every user row that is not empty and not one of 13 synthetic prefixes, before the typed-prompt test (parser.go:204-217). Bash-mode output and command banners are host wrappers but not synthetic prefixes (compare parser.go:285 with 322), so a `<bash-stdout>` row with a line that reads "off the record" after punctuation is stripped (internal/session/control.go:13-34), such as the heading at docs/design.md:789, takes the chat off the record.
- Nothing tells the user: the hook prints a confirmation only for a control it saw itself (hook.go:461-475), recovery appends no `off_record` event (catchup.go:650-663), and no reader distinguishes the marker value `1` from `asked` (written at catchup.go:285, 659; never compared anywhere).
- From then on the chat stores only event names, and recovery never turns capture back on.

Direction: only a prompt the user typed can be a control; a control found late is surfaced once in the chat and in status, with the way back. Action: evolve.

### F11. Two hosts and one operating system are wired through every layer (reach, medium, partly known)

Verified.

- Host names are literal in `stateFile`, `ReadDrift`, `Prune`, `RecoverProject`, `openTranscript` and `storePath` (catchup.go:1090, 1157-1160, 1265-1276, 1315-1322; drift.go:41; prune.go:58); the parser is one function with an `if claude else codex` (parser.go:65, 144); the scanner has per-host field tables and verdicts (parser.go:504-507, 1061-1088). A third host touches all of them.
- Store roots are fixed to `~/.claude/projects` and `~/.codex/sessions`; a host moved by `CLAUDE_CONFIG_DIR` or `CODEX_HOME` gets no recovery (known-limitations.md:49).
- `syscall.Stat_t` and `O_NOFOLLOW` are used without build tags (catchup.go:19, 1243, 1252, 1342), so the package builds on macOS and Linux only.
- Records hold absolute local paths; nothing here serves a chat on another machine, a cloud session (hook.go:284-286) or an app surface without a local JSONL transcript.

Direction: a transcript-adapter interface (locate, identify, decode a row to normalized events, version) with one implementation per host and the store root taken from the host's own environment at registration. Action: rebuild.

### F12. Every hook event pays for recovery in the user's time (invisibility, low to medium)

Verified in code; the cost in milliseconds is inferred.

- Per event: the record loaded twice (catchup.go:127, 273), the transcript opened twice, each time running the full redactor over its path and resolving symlinks (1262, 1324-1328), and on Codex 64 KiB read and parsed for identity every pass (319-325).
- Up to three durable saves per event, each a file fsync, a rename and a directory fsync (paths.go:166, 175, 178): the registration (catchup.go:193), the attempt marker for a big row (568), and the cursor (828). The cursor is a cache and does not need durability once appends are idempotent.
- UserPromptSubmit, Stop, SessionStart and SessionEnd are synchronous for the user; recovery may take 100 ms of each, 250 ms of a chat start.

Direction: hooks register and return; the cursor is written without fsync; ingest runs detached. Action: rebuild (falls out of F4).

---

## 4. Complexity: essential and accidental

Essential: trusting which file to read; knowing which rows are the user's own words and which are final replies, per host and version; honouring a privacy control found late; redacting before anything is stored; idempotent appends; noticing when a host changes its format.

Accidental: running all of it synchronously in 100 ms slices of a hook process. That one choice produces the resumable scanner, the slow-row ladder, the windowed redaction and its replicas of redactor internals, the grace deadlines, the durable per-row cursor, the one-window rule and, through it, the "behind means discard" rule in the hook. A second accidental source is the lack of row identity, which produces the turn and text-hash keys, the 1 MiB dedupe window and the baseline resets.

---

## 5. Overall call: rebuild the execution model, keep the rules

Rebuild. The package's knowledge is sound and expensive to rediscover: which rows are typed prompts, which are replies, how a control hides in an attachment or an oversized row, how to open a transcript safely. Carry those over as per-host adapters with their tests. Replace what surrounds them: a detached, single-flight ingest per session; a session registry apart from a disposable cursor; entries keyed by row identity; a verified-through point that readers respect in place of dropping captures; reconciliation between hook and transcript as the health signal. More patching of the hook-sliced loop adds states to a machine that already has seven commits of special cases in eleven days.

---

## 6. Open questions

1. Do real Claude Code assistant and tool-result rows carry `promptId`, and is `stop_reason` set on the final text row in every current version? The dedupe and reply paths depend on it (F6, F9).
2. What does an interrupted Claude turn look like in the transcript? If no `end_turn` row is written, design.md:525's claim does not hold (F1).
3. On Codex, is the hook payload's `cwd` the session's or the turn's? If the turn's, a turn in another folder fails the identity check and the control in it is not honoured, against known-limitations.md:48 (catchup.go:319-325).
4. How large is Codex's `session_meta` row with long instructions? Past 64 KiB the identity check fails for the whole session (catchup.go:320; parser.go:372-388).
5. Can an asynchronous PostToolUse hook run after the final assistant row is written? If so, replies are stored twice (F9).
6. How often is a pass behind in real sessions? Nothing records it (F2, F7).
7. Is the owner willing to reopen two recorded decisions: history backfill (decisions.md:116) and saving assistant text between tool calls (design.md:1391)?
8. Is a detached ingest worker started from hooks acceptable under "no daemons or sleeping jobs" (design.md:1360)? It would be short-lived, like the update job.

---

## Verification

A second reader tried to refute each finding above. Read in full: internal/catchup (catchup.go, parser.go, drift.go, prune.go, forget.go). Read in part: internal/hook/hook.go:79-694 and 781-841, trigger.go:95-175, resume.go:225-258; internal/update/validate.go:88-105, input.go:255-300, knowledge.go:92, wiki.go:285-303; internal/inbox/inbox.go and redacted.go; internal/session/control.go; internal/redact (the entropy rule); scripts/benchmark.py:215-233; the cited lines of docs/design.md, decisions.md, known-limitations.md and verification.md. No test and no eva-md command was run. What real transcripts and hook payloads carry could not be observed here, so every claim that rests on a host field stays inferred.

### Verdicts

| Finding | Verdict | My severity | Theirs |
| --- | --- | --- | --- |
| catchup-1 | partly | medium | high |
| catchup-2 | confirmed | high | high |
| catchup-3 | confirmed | medium | high |
| catchup-4 | confirmed | medium | high |
| catchup-5 | confirmed | medium | high |
| catchup-6 | confirmed | high | high |
| catchup-7 | confirmed | medium | medium |
| catchup-8 | confirmed | medium | medium |
| catchup-9 | confirmed | medium | medium |
| catchup-10 | confirmed | low | medium |
| catchup-11 | partly | low | medium |
| catchup-12 | partly | low | low |

None is refuted. Every file and line cited shows what the finding says it shows. The corrections are to scope and severity.

**catchup-1, partly, medium.** The code is as cited: parser.go:131-134 drops assistant rows without `end_turn` or `stop_sequence`, 78-83 skips sidechain and agent rows, 239-246 returns no text when any item is not text, 271-272 returns nothing for a tool result; catchup_test.go:113-116 pins it; design.md:1391 records the decision and known-limitations.md has no line for it. The impact is overstated in two places. Tool inputs do reach memory, as one line per tool through the PostToolUse hook (hook.go:781-841), though not through recovery and not beyond 200 characters. An interrupted turn keeps its prompt and its tool lines from the hooks; what it loses is the partial reply. What never reaches memory is assistant text between tool calls, subagent rows and partial replies. design.md:525 says catch-up covers an Esc interrupt; no code does that. Medium, because the final reply and the tool lines are kept and the loss is a recorded decision.

**catchup-2, confirmed, high.** hook.go:432-441, 454-459, 503-505 and 657-675; catchup.go:386-397, 612-615, 631-645, 879-883; inbox.go:24; known-limitations.md:41. Three additions. UserPromptSubmit has no event name either, so a prompt that arrives while behind leaves nothing and gets no recall pointers, since `markersOnly` returns before `resumeContext`. A pass is also behind when its 100 ms runs out (catchup.go:818-823; hook.go:440), which needs no 1 MiB backlog, only a slow disk. A brand-new chat is behind at its SessionStart only through that timeout, because a new registration starts at the transcript's end (catchup.go:186-187). One caution on the direction: the inbox is append-only and removal takes whole sessions (design.md, "Removal"), so "a control found late removes what was captured after it" needs a new mechanism and cannot reach what an update already wrote.

**catchup-3, confirmed, medium.** The only callers of `Recover`, `RecoverAfterReply` and `RecoverProject` are in internal/hook (hook.go:276-278, 345); update calls `Headless` (pass.go:168, job.go:224) and maintain calls `Prune` (maintain.go:147). catchup.go:1124 skips a session registered under 2 minutes ago, and the filter runs before the cut at 1130-1136, so the five are the five most recently registered idle sessions, whatever they hold. The out-of-order claim holds only when another session's pass has moved the page's `as_of` past the tail's time (validate.go:103; input.go:277-281; knowledge.go:92; rules.go:110); in the common path the SessionStart that recovers a tail queues that session's update in the same hook (trigger.go:151-170). Medium, because a turn cut short by a crash has no `end_turn` row for any pass to recover (catchup-1); the cases that matter are replies the host wrote and a failed or behind hook dropped.

**catchup-4, confirmed, medium.** The count holds: parser.go:405-1228 is 824 lines, and the slow-row state (catchup.go:101-117, 506-520, 551-645), the scan (352-357, 365-385, 725-814), bounded redaction (864-1072) and the grace period (842-862) add about 470, so about 1,290 of 3,070. The three parity tests are at scan_test.go:45, 105 and 512. The leak risk is narrower than stated: windows apply only to text over a level's `full` bound, 200 KiB at the first level (catchup.go:879-883, 968-971), and scan_test.go:380 compares windowed with whole redaction for today's rules. Medium on its own stated impact, which is maintainability; it is the root cause of catchup-2, 3 and 12, and earns its place in a redesign through them.

**catchup-5, confirmed, medium.** catchup.go:1-2 and 186-187; hook.go:423; decisions.md:116; design.md:1403. A feature ruled out by a recorded decision, not a defect.

**catchup-6, confirmed, high.** parser.go:66-195; internal/catchup has no testdata folder and the repo no transcript file; drift.go:15-19. parser.go:74-77 sets `recognized` before the `promptId` and `stop_reason` tests, so a row that is recognized and never stored counts as healthy. The two prefix lists differ (hook.go:688; parser.go:285, 322), in part by design: the hook sees only submitted prompts, while the transcript also holds bash-mode and banner rows. This finding weighs more than "the fallback may stop": see catchup-m1 below.

**catchup-7, confirmed, medium.** hook.go:424 and 437-440; catchup.go:127-130 and 1207; prune.go:104-106, where an unreadable record stays until it is 30 days old; resume.go:245-255. Some traces do exist: the drift record, for total failure only, and the heartbeat's `transcript_available` and `transcript_elsewhere` (health.go:185; watchdog.go:101-104, 427). The record's `Reason` notes (catchup.go:166, 390, 410, 642) are read by nothing outside the package. One more consequence of an unreadable record: "back on the record" fails without a word, since `SetOffRecord` returns the load error (catchup.go:201-204) and the hook then stops before it clears the marker (hook.go:484-486). The schema-change scenario is hypothetical.

**catchup-8, confirmed, medium.** catchup.go:186-190; hook.go:424 before 503; hook.go:437-440; catchup.go:290-292; catchup.go:23-27 imports no config and 1118-1127 checks none. The exclusion case needs a second folder with the same project key that is not excluded: another clone or worktree, or any folder outside a repo, since all of those share the project "general". The excluded chat's hooks then stop at hook.go:308, its record stays, and another chat's start recovers the prompts and replies it wrote after the exclusion. A further inconsistency: a Claude row whose folder differs from the registered one returns before the control check (parser.go:78-80), while Codex honours a control there (parser.go:150-153). The reset loss itself is known (known-limitations.md:46).

**catchup-9, confirmed, medium, partly known.** catchup.go:1382-1390, 602-604, 1411 and 1484-1486; parser.go:34; hook.go:575; catchup.go:785-793, which regression_test.go:211-235 asserts for an oversized tool result; update/input.go:495 shows events to the model. known-limitations.md:38 lists one duplicate case, so `known` should read partly. How often a reply is stored twice rests on host fields nobody captured: every test row carries `promptId` on assistant rows (catchup_test.go:85-87), and verification.md:74 reports a live check without duplicates.

**catchup-10, confirmed, low.** parser.go:204-217 checks the control before `typed` is consulted at 220; `<bash-stdout>`, `<bash-stderr>`, `<bash-input>`, `<command-message>`, `<command-args>`, `<local-command-stderr>` and the interrupt notice are in `hostWrapper` (322) and not in `syntheticText` (285); session/control.go:27-31 matches any line that is only the phrase; catchup.go:650-663 writes no event; hook.go:461-475 confirms only a control the hook saw. Low, because the trigger is narrow: bash-mode output with such a line. Whether those rows lack `isMeta` on a real host is as the repo's own test models them (regression_test.go:409-425).

**catchup-11, partly, low.** The code facts are right. But macOS only is the product's stated scope (AGENTS.md; known-limitations.md:265, 289), host literals and `syscall` are used across the codebase (35 `"claude-code"` literals outside this package), and the fixed store roots are already listed (known-limitations.md:49). This is a property of eva-md, not a defect of this package, and a third host needs plugin and hook work first.

**catchup-12, partly, low.** The code facts are right: the record is loaded at catchup.go:127 and 273, the transcript opened at 169 and 300 with the redactor on its path at 1262; saves at 193 (skipped within 10 s when nothing changed, 158-163), 568 (rows of 256 KiB or more only) and 828 (only when the cursor moved); the listing at 1099-1119. "Not measured" is wrong for the common case: scripts/benchmark.py:221-233 and 366-367 time 100 Stops that each recover one row, against a 50 ms p95 target. Unmeasured are a backlog, a long row and the chat-start listing. One cost not listed: every pass that meets a prompt or reply row decodes up to the last 1 MiB of the session's inbox file (catchup.go:593, 1391-1431), and the Stop hook decodes it again (1480).

### Missed

**catchup-m1. On an ordinary Stop the transcript's copy of the reply is stored and the hook's own is dropped (reliability-safety, medium, verified in code).** The hook runs the recovery pass before it appends anything (hook.go:503). When the final assistant row is already in the transcript, the pass stores it (catchup.go:592-625). The hook then asks `Contains`, which answers by turn id and compares no text (hook.go:574-577; catchup.go:1484-1486), and drops the host's `last_assistant_message`. So the reply path of an ordinary turn rests on the transcript parser, which design.md:1391 calls an unstable interface, and not on the documented payload. A row that parsed to part of a reply would replace the whole one. A long reply must be redacted inside the pass's 100 ms, or the hook discards its full copy and a later pass keeps 22 + 22 KiB (catchup.go:612-615, 631-640, 879-883): this is why the cut in catchup-2 is systematic. Which copy is stored turns on whether the host flushed the row before the hook ran. Direction: at Stop the hook's payload is authoritative; store it first and let recovery fill only turns for which no hook delivered a reply. A redesign that makes the transcript the one writer needs the fixtures of catchup-6 first.

**catchup-m2. Text typed beside an image or document is never recovered (memory-quality, low, verified in code).** `userText` returns no stored text when a content item is neither text nor a tool result (parser.go:266-280), and parser.go:224 then drops the row; an assistant row with any non-text item is dropped the same way (239-246). The hook discards a prompt while recovery is behind on the stated ground that a later pass recovers it (hook.go:498-505). A prompt sent with a screenshot at such a moment, or whose hook failed, is lost whole, with no gap marker, and the row still counts as recognized (parser.go:77), so no drift shows. known-limitations.md does not list it. Direction: store the text items of a mixed row with a marker for each attachment left out, and write a gap event where a row cannot be stored.

### Open points this pass could not settle

- Whether Claude Code's assistant rows carry `promptId`, and whether `last_assistant_message` and the final row always hold the same text. Catchup-9 and catchup-m1 turn on both.
- Whether a Codex `session_meta` row can pass 64 KiB. If it can, the identity check fails for the whole chat (catchup.go:319-325; parser.go:372-388) and the hook takes the chat as caught up.
