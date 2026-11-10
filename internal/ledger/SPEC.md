# The record's event model and the one reader

This is the contract every later part of the record, of organizing and of recall builds on (plan section 6.1, designs 1 and 2; ledger design sections 3 and 14). Everything that reads captured chats reads them through `ledger.Reader`, as typed events. Nothing calls it yet. The first backend, `inboxv1`, reads v1's inbox files as they are and never writes.

- `internal/ledger/event`: the model, its codec, ids and references.
- `internal/ledger`: the `Reader` interface and `Resolve`.
- `internal/ledger/inboxv1`: the read-only backend over v1's inbox.

## 1. The event

An event is one thing that happened in one session. Its id is the session's host, the session's id and its `seq`. Fields, as the codec writes them:

| Field | Meaning |
| --- | --- |
| `seq` | the event's place in its session, from 1, never reused |
| `t` | the source's own time, RFC 3339 |
| `turn` | the turn's ordinal, from 1; 0 before the first turn |
| `tid` | the host's turn id, when given |
| `k` | the kind (section 2) |
| `pv` | the author: whose words the text is (section 3) |
| `text` | redacted text |
| `red` | where redaction markers stand in `text`: `[start,end,kind,tier]` or `[start,end,kind,tier,handle]`, byte positions, ordered and apart |
| `rv` | the redaction rule version that produced `text`; 0 when not known |
| `cut` | every truncation (section 6) |
| `agent` | the subagent's id |
| `taint` | the exposure marks (section 5) |
| `src` | `h`: a hook supplied it; `r`: the host's row id; `key`: the sameness key (section 8) |
| `op` | a control's operation: `off`, `on`, `forget` |
| `why` | a gap's reason |
| `mark` | marks a consumer must heed (section 4) |
| `copy` | the `seq` of the event this one is a second copy of |
| `ctx` | the context: project, branch, folder relative to home, head commit, model |

In a stored session only `ctx` events carry `ctx`, and the reader hands every event out with it filled; the v1 backend fills it from each line it returns (a withheld or repaired line has none). A session file starts with a header, `{"v":2,"k":"header","host":…,"session":…}`, and a reader trusts the file only when the header's host and session are its folder's.

The codec is strict: `Decode` refuses a field it does not know, trailing data, and any event that breaks an invariant of section 10, and `Encode` refuses to write one. `Decode` and `DecodeHeader` accept only the form the codec writes: a line must encode back to itself byte for byte, so a key in another letter case or given twice, white space, a null or an empty list is refused, and no other JSON reader can read a stored line as something else. Kind-specific fields of the ledger design's table 3.4 that are not above (`paste`, `end`, `tool`, `cls`, `target`, `decision`, `items` and the rest) are added by the packages that first write them.

## 2. Kinds

`prompt`, `reply` (the final assistant message of a turn), `segment` (assistant text between tool calls), `tool`, `dialog`, `plan_decision`, `tasks`, `sub_task`, `sub_report`, `summary` (the host's compaction summary), `delivery` (what eva-md delivered into the chat: ids and hashes, never text), `control`, `ctx`, `life` (a host lifecycle event), `gap` (something is missing here) and `gone` (a tombstone left by forgetting), as the ledger design's table 3.4 defines them. A `gone` holds nothing of what was forgotten: author `eva`, its place as its key, and no text, rule version, cut, subagent, exposure, copy, hook or row id. Its own fields (the kind it replaced, `at`) are the forgetting package's.

## 3. Authors

An author is set only on positive evidence. Anything not positively identified is `unknown`, and unknown text is still kept. The evidence for a live host is named for each host by the adapters (LG-13) and the worker (LG-21); this table says what each value means and what can never set it.

| `pv` | Meaning | Evidence |
| --- | --- | --- |
| `typed` | typed by the user | a prompt whose source the host marks as the person's typing, by the evidence the adapter names for that host. Never a voice hand-over, a program's or a subagent's prompt, and never set from v1's files |
| `pasted` | wholly a paste | the host marks the whole prompt as pasted (Claude Code only) |
| `chosen` | chosen by the user from options the agent wrote | a dialog answer picked from its options, or a plan approval |
| `agent` | written by the agent | assistant replies and segments, plan text, dialog questions, a subagent's task |
| `subagent` | written by a subagent | an event that carries a subagent's id |
| `host` | written by the host | compaction summaries, scheduled and loop prompts, notices, banners, lifecycle events |
| `program` | written by a program that runs the chat | prompts of a chat a program runs, messages from another session |
| `spoken` | spoken by the user in a voice call | a line of a decoded voice hand-over that the host attributes to the person. Never typed |
| `front_model` | the voice front model's | a hand-over's request and the front model's spoken lines. It never carries the person's authority |
| `tool` | a tool record | tool calls and their failure digests |
| `eva` | a record eva-md wrote | controls, gaps, deliveries, context |
| `v1_user` | filed by v1 as the user's prompt; not proven typed | set only by the v1 reader (section 9) |
| `unknown` | none of the above is shown | everything else |

## 4. Marks

- `repaired`: the line could not be decoded, and the text is the fixed gap v1's readers put in its place.
- `withheld`: the reader returns nothing of the event but its place. It is a bare `gap` by `eva`, with a `why` of `off_record`, `untrusted`, `unknown_kind` or `unconfirmed`, no other mark, and its place as its key, `place:<seq>`: no text, rule version, cut, time, turn id, subagent, exposure, copy, hook, row id or context. Nothing in it tells what it stands for, not even a key derived from its text.
- `wrapper`: the prompt begins with a voice hand-over wrapper, which this reader does not decode (decoding is LG-13's). Its text is not one speaker's words, and its author is never `typed` or `spoken`.
- `name`: the text is a fixed name eva-md wrote (`session_end`, `host_prompt`, `gap: …`), not anyone's words.
- `recovered`: v1 recovered the event from the host's own record rather than from a hook.

## 5. Exposure marks

`taint` is not authorship. It marks that outside text entered the turn before the event, with `at`, the `seq` of the event that brought it in, so text written before a fetch is told from text written after: `web` (a fetch or a search), `net` (a shell command that reaches the network), `mcp` (a tool of a server other than eva-md's), `memory` (something eva-md delivered or looked up), `subagent` (a subagent's report). The v1 backend sets none: v1's files hold no tool call ids, and the marks are set by the worker from typed tool records (LG-21, LG-23).

## 6. Cuts

A cut says where text was taken out, how much and why: `{at, lost, why}`, with `why` one of `size`, `deadline`, `policy` or `legacy`. `at` is a byte position in `text`, or -1 for `legacy`: v1 recorded only how many bytes it took out (`trunc`), not where, and it cut in two places, the inbox's middle cut at 200 KiB and its recovery's windowed redaction. A cut is never silent: every v1 line with `trunc` above 0 yields one `legacy` cut.

## 7. Sessions and classes

`event.Session` is a session with its class and what reading it found: `events`, `copies` (events that are a second copy, section 8), `withheld`, `wrappers` (prompts that are an undecoded voice hand-over, counted apart) and `tail` (bytes of an unfinished last line left unread). A class is set from positive evidence, as an author is.

| Class | Meaning | Evidence |
| --- | --- | --- |
| `chat` | a person's chat | a prompt proven typed, or a voice hand-over: a thread whose prompts arrive through a voice hand-over is a person's chat. Never set from v1's files |
| `subagent` | a subagent's thread | every event of the session carries a subagent's id |
| `program` | a program's run | the session's first prompt is a program's, and none is filed as the user's |
| `unrun` | a thread that was opened and never run | an end event with no start and no turn (Codex fires an end and no start for a thread loaded and never run) |
| `housekeeping` | the host's own housekeeping | events, but no turn at all, and not `unrun` |
| `unknown` | none is shown | everything else, and a session with no event the reader returns |

From v1's files the class is set only from what they show, in this order: every returned line carries an agent id (`subagent`); the first returned prompt line with no agent id is a `host_prompt` event and no prompt that belongs to the file, withheld or not, was filed as the user's with no agent id (`program`); an end event, no start and no turn (`unrun`); no turn at all (`housekeeping`); otherwise `unknown`. A turn is shown by a turn id, a prompt, reply, tool, plan or summary line, a `host_prompt`, a record control, an interrupt, a failed stop, or a missing reply's gap. Only lines the reader returns set a class, so nothing off the record does; a pause is itself a turn. A prompt filed as the user's that was withheld still denies `program`: a person's chat with its prompts said off the record is not a program's. It tells no more than `withheld` does. A v1 session whose prompts are voice hand-overs is `unknown`, never `program` or `housekeeping`: telling it apart as a person's chat needs the wrapper decoded.

## 8. The sameness key

`src.key` is the key that makes two events one. It is separate from the id: two events with the same key are one event stored twice, and the later is marked with `copy`, the `seq` of the first, and counted for its session. The reader drops nothing.

The key is, in this order: the host's row id where there is one (with the index of the event within the row); otherwise the turn, the kind and the tool call's id or the line's position. It is never the text alone.

v1's files hold no row id and no tool call id, and a v1 line's place in its file cannot be the position, since the same entry stored twice sits at two places. So the v1 backend keys:

- a prompt or a reply that has a turn id, and is not a fixed name, by its turn id, its kind, its subagent and its text, hashed, and an ordinal: v1's recovery stores again under that turn a prompt or reply the hook stored. The same words in another turn, as another kind or from another subagent are not a copy. The hook stores each prompt it is given once, so two hook lines are two events even when they say the same words in one turn, as a prompt sent twice into a running turn does (plan 14.3): the n-th hook line with that hash has ordinal n, and every other line 0. A recovered line is so a copy of the first line with its hash, and a hook line only of a recovered one;
- every other line by its position, `v1:line:<seq>`, so it is never a copy: the same command run twice in one turn is two events, two `host_prompt` names in one turn are two prompts, and a reply v1 stored with no turn id is never matched to another (v1's recovery often leaves one without; the copy is then kept and not counted).

Two different replies in one turn differ in text and are two events. A second prompt of one turn is never taken for the first unless it says the same words and one of the two is recovered (plan 14.3). A withheld event and a tombstone are keyed by their place, `place:<seq>`.

## 9. The v1 backend

`inboxv1.Backend{Root}` reads the inbox at `Root`. `Sessions` lists every session file with v1's own listing (`inbox.Store.Captures`), and its session id as v1 reads it (`Store.SessionID`); a watermark whose file is gone is not a session. A session v1 wrote into two project folders is two sessions with one id, each with its folder; joining them is the migration's (LG-25). Until then an event id, `<host>:<session>#<seq>`, is unique only with its folder. A folder that cannot be read in full fails the listing.

`Read` decodes the file with v1's own reader (`inbox.Store.ReadFrom`), runs v1's walker (`inbox.Walk`) over the same bytes, and checks every line with v1's trust check (`inbox.Trusted`). It writes nothing, and refuses a location that leaves the inbox, a symlink and a multiply linked file, as v1 does, and a key whose host and session are not those `Sessions` names for its file.

**One event for each line.** Each complete physical line is one event, in order, and its `seq` is the line's number, gap lines and repaired partial lines included. So every reference v1 wrote, `s:<first 8>#<line>`, names the event of that line, and resolves to v1's text for it. An unfinished last line is left unread, as v1's readers leave it, and counted in `tail`.

`ledger.Resolve` reads every session a reference names and keeps those that hold its event, as v1's `eva-md show` keeps the files that hold the line. Where more than one does, it keeps those whose line decodes and belongs to its file (not `repaired`, not withheld as `untrusted`), as v1 does, and the reference is ambiguous unless exactly one is left. So two sessions that share their first 8 characters, or one kept in two folders, resolve a line only one of them holds. A session it cannot read fails the reference.

| v1 line | Event |
| --- | --- |
| does not decode, or is too long | `gap`, author `eva`, text `gap: malformed inbox record`, marked `repaired` and `name` |
| belongs to another session, folder or host | withheld, `why: untrusted` |
| said off the record | withheld, `why: off_record` |
| `user`/`prompt` | `prompt`, author `v1_user`, or `subagent` with an agent id; `wrapper` when its text begins with `<realtime_delegation>` after white space |
| `assistant`/`reply` | `reply`, `agent` or `subagent` |
| `tool`/`tool` | `tool`, `tool` or `subagent` |
| `plan`/`plan` (v1's task list tools) | `tasks`, `agent` or `subagent` |
| `host`/`host_summary` | `summary`, `host` |
| `event` `host_prompt` | `prompt`, author `program` (`subagent` with an agent id), marked `name` |
| `event` `off_record`, `on_record` | `control`, `op` `off` or `on`, author `eva`, marked `name` |
| `event` whose text begins `gap: ` | `gap`, author `eva`, marked `name` |
| `event` lifecycle names (`session_start`, `session_end`, `compact`, `model_switch`, `stop_failure`, `interrupt`) | `life`, author `host`, marked `name` |
| `event` with another text | `life`, author `unknown` |
| a prompt, reply, tool or plan kind with a role v1 never writes with it | that kind, author `unknown`, as v1's walker yields it |
| any other role and kind | withheld, `why: unknown_kind` |

A returned event's text is v1's text for its line, byte for byte; its time, turn id, subagent and context are the line's; `src.h` is set for a hook's line and `recovered` for recovery's; `rv` is 0, since v1 does not record the rule version of a line. `turn` comes from returned lines only, so nothing withheld tells how many turns it held: a turn id not yet seen opens a new turn, and one seen before is in the turn it opened; a prompt with no turn id opens a new turn; a copy is in its first's turn; any other line, and a withheld one, is in the current turn. So a reply after a pause and a resume, which store no prompt, is in the resume's turn, not the last prompt's.

**Never typed.** v1 has no evidence that a prompt was typed on Codex, and it files voice hand-overs and program prompts as the user's, so no event of the v1 backend has the author `typed`.

**Trust and the record.** A line is returned only when it belongs to its file by v1's trust check. The off-the-record state changes at each pause and resume marker, in file order. v1's readers disagree on which markers count: its walker (search and the brief) honours only a marker that belongs to the file, and its update honours any marker line. The backend withholds a line when either would, so a marker forged into a file can pause the record but never resume it. The markers themselves are returned, as fixed names. A prompt or a reply said off the record and stored again on the record, with the same turn id, kind, subagent and words, as v1's recovery may store it, is withheld as `off_record` too; v1's walker would return it. The keys this takes are kept inside the read and never returned. A prompt, reply, tool or plan line is returned only when v1's walker yields it with the entry v1's reader decoded for the same line; otherwise it is withheld (`why: unconfirmed`). The walker reads the file only as far as the reader did, and a file whose lines change in number or length between the two fails the read.

**Where v1's readers disagree.** A line exactly 2 MiB long is decoded by v1's walker but replaced by a gap by its reader and its update. The backend sides with the reader, so the line's text is never returned (`TestLineAtV1sLimit`). When that line is a pause, the walker pauses and the reader and the update do not. The walker tells its state only where it stops, so when the reader repaired a line the backend runs the walker one line at a time and pauses where the walker paused at a repaired line, and never resumes there (`TestPauseAtV1sLimit`). With that, the walker passes over no prompt, reply, tool or plan line the backend would return, unless the file changes between the two reads, which the `unconfirmed` check is kept for.

## 10. Invariants and the tests that hold them

| Invariant | Tests |
| --- | --- |
| The codec round-trips: golden events and headers decode and encode back byte for byte, and the golden file holds every kind, author, mark, exposure, cut reason and operation | `event.TestGoldenEventsRoundTrip`, `TestGoldenEventsCoverTheModel`, `TestGoldenHeadersRoundTrip` (`testdata/codec/`) |
| An event that breaks an invariant is never read or written: seq at least 1, known kind and author, a key, a copy of an earlier event only, an operation on controls only, a reason on gaps only, spans ordered, apart and inside the text, cuts with bytes lost, a known reason and a place, exposures of a known kind brought by an earlier event, marks known and once, a withheld event a bare gap that holds nothing but its place, a tombstone nothing of what was forgotten, an undecoded wrapper a prompt neither typed nor spoken | `event.TestInvalidEventsRefused` (`testdata/codec/invalid.jsonl`), `TestEncodeRefusesInvalidEvent` |
| The short form names a session as v1's references name it; ids and references parse and print back | `event.TestPrefixMatchesV1References`, `TestIDForms`, `TestRefForms`, `TestRefNamesASession` |
| A v1 inbox yields one event for each line, seq equal to the line number, text equal to what v1's reader, its update and its walker return for that line | `inboxv1.TestFixtureInboxReadsAsV1Does`, `TestGeneratedInboxes` (through `checkV1`) |
| Every v1 reference resolves to the same text; a reference is ambiguous only where more than one session it names holds its line as one that decodes and belongs | `inboxv1.TestV1ReferencesResolve`, `TestAmbiguousReferences`, `ledger.TestResolve` |
| The codec reads only its own form | `event.TestDecodeRefusesAnotherForm`, the `form:` lines of `invalid.jsonl`, `TestGoldenHeadersRoundTrip` |
| No event of the v1 backend has the author typed | `checkV1`, so every test of the backend; `TestGeneratedInboxes` |
| Nothing off the record, forged into a file, or unfinished is returned, in any field, under generated placements of pauses and resumes, forged markers, pauses at v1's limit and lines said off the record stored again included; no line is returned where v1's walker or its update is off the record | `checkV1`, `TestFixtureInboxReadsAsV1Does`, `TestGeneratedInboxes`, `TestForgedMarkers`, `TestPauseAtV1sLimit`, `TestOffRecordLineStoredAgain`, `TestUnconfirmedContentIsWithheld` |
| A read is labelled only with its file's host and session | `TestReadRefusesAnotherSessionsKey` |
| Turns follow turn ids and prompts of returned lines | `TestTurnsFollowTurnIDs`, `TestFixtureInboxReadsAsV1Does` |
| A second copy is marked and counted and nothing is dropped: the same entry stored by the hook and recovery is a copy; two replies in one turn, the same command twice in one turn, two hook lines, the same words in another turn or kind, or with no turn id, are not | `TestSecondCopies`, `TestGeneratedInboxes`, `TestFixtureInboxReadsAsV1Does` |
| A session's class comes only from what its file shows, the v1 backend never classes a session as a person's chat, and never one with a prompt filed as the user's as a program's | `TestSessionClass`, `TestFixtureInboxReadsAsV1Does`, `TestGeneratedInboxes` |
| Each v1 event name and the white space before a wrapper are read as the table says; a watermark alone is not a session, and a folder that cannot be read fails the listing | `TestV1EventNames`, `TestWrapperAfterWhiteSpace`, `TestSessionsListing` |
| The packages import no new module, and only eva's name appears in their text | `ledger.TestOnlyEvasNameAndNoNewModule` |

## 11. Decisions taken here

1. **Two authors beyond the plan's list.** The plan lists who authors a text; the ledger design adds `tool` for tool records and `eva` for records eva-md writes. Both are kept, since a tool record and a gap are no one's words and `unknown` would hide what is known. A third, `v1_user`, is the card's: a prompt v1 filed as the user's.
2. **The v1 sameness key uses the text, with the turn.** The plan's key is the turn, the kind and the tool call's id or the line's position. v1 keeps neither a call id nor a position in the host's record, and its double entries are a prompt or a reply stored by both the hook and recovery under one turn, so the text stands in for the position for those two kinds only, never alone (section 8).
3. **The order of the v1 class evidence.** The card lists four kinds of evidence for five classes. An end with no start and no turn is a thread opened and never run, as Codex's source shows (plan 13.5); a session with no turn at all and not that is the host's housekeeping. The host sitting (HF-00) may move the line between the two.
4. **Off the record is the union of v1's readers.** Where v1's walker and update disagree on a forged marker or a marker at v1's limit, the backend withholds what either withholds (section 9), and it withholds a line said off the record when it is stored again on the record, which neither does.
5. **Two hook lines are never one event.** v1's recovery takes a second prompt under one turn id for a copy (plan 14.3). The hook stores each prompt it is given, so the backend counts a copy only where recovery stored a line again. Whether a prompt sent into a running turn carries that turn's id is for the host sitting to confirm.
6. **A turn is told by its turn id.** A pause, a resume or a withheld prompt stores no prompt line, so a turn counted from prompts alone files a reply under another prompt's turn. Turn ids, where v1 has them, decide.
7. **What is not here.** Decoding a voice hand-over (LG-13), authors from live payloads (LG-13, LG-21), exposure marks (LG-21, LG-23), the stored ledger and its header (LG-20), joining a session kept in two folders and the `legacy` map (LG-25).
