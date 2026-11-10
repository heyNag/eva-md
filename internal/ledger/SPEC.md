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
| `turn` | the turn's ordinal, from 1; 0 before the first prompt |
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

The codec is strict: `Decode` refuses a field it does not know, trailing data, and any event that breaks an invariant of section 10, and `Encode` refuses to write one. A line `Encode` wrote decodes and encodes back byte for byte. Kind-specific fields of the ledger design's table 3.4 that are not above (`paste`, `end`, `tool`, `cls`, `target`, `decision`, `items` and the rest) are added by the packages that first write them.

## 2. Kinds

`prompt`, `reply` (the final assistant message of a turn), `segment` (assistant text between tool calls), `tool`, `dialog`, `plan_decision`, `tasks`, `sub_task`, `sub_report`, `summary` (the host's compaction summary), `delivery` (what eva-md delivered into the chat: ids and hashes, never text), `control`, `ctx`, `life` (a host lifecycle event), `gap` (something is missing here) and `gone` (a tombstone left by forgetting), as the ledger design's table 3.4 defines them.

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
- `withheld`: the reader returns nothing of the event but its place. It is a bare `gap` with a `why`: no text, time, turn id, subagent or context.
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

From v1's files the class is set only from what they show, in this order: every returned line carries an agent id (`subagent`); the first prompt line with no agent id is a `host_prompt` event and no prompt was filed as the user's (`program`); an end event, no start and no turn (`unrun`); no turn at all (`housekeeping`); otherwise `unknown`. A turn is shown by a turn id, a prompt, reply, tool, plan or summary line, a `host_prompt`, a record control, an interrupt, a failed stop, or a missing reply's gap. Only lines the reader returns count, so nothing off the record decides a class; a pause is itself a turn. A v1 session whose prompts are voice hand-overs is `unknown`, never `program` or `housekeeping`: telling it apart as a person's chat needs the wrapper decoded.

## 8. The sameness key

`src.key` is the key that makes two events one. It is separate from the id: two events with the same key are one event stored twice, and the later is marked with `copy`, the `seq` of the first, and counted for its session. The reader drops nothing.

The key is, in this order: the host's row id where there is one (with the index of the event within the row); otherwise the turn, the kind and the tool call's id or the line's position. It is never the text alone.

v1's files hold no row id and no tool call id, and a v1 line's place in its file cannot be the position, since the same entry stored twice sits at two places. So the v1 backend keys:

- a prompt or a reply that has a turn id, and is not a fixed name, by its turn id, its kind, its subagent and its text, hashed: the hook and v1's recovery each store the same prompt or reply under that turn. The same words in another turn, as another kind or from another subagent are not a copy;
- every other line by its position, `v1:line:<seq>`, so it is never a copy: the same command run twice in one turn is two events, two `host_prompt` names in one turn are two prompts, and a reply v1 stored with no turn id is never matched to another (v1's recovery often leaves one without; the copy is then kept and not counted).

Two different replies in one turn differ in text and are two events. A second prompt of one turn is never taken for the first unless it says the same words, which v1's recovery does (plan 14.3).

## 9. The v1 backend

`inboxv1.Backend{Root}` reads the inbox at `Root`. `Sessions` lists every session file with v1's own listing (`inbox.Store.Captures`), and its session id as v1 reads it (`Store.SessionID`). A session v1 wrote into two project folders is two sessions with one id, each with its folder; joining them is the migration's (LG-25). A folder that cannot be read fails the listing.

`Read` decodes the file with v1's own reader (`inbox.Store.ReadFrom`), runs v1's walker (`inbox.Walk`) over the same bytes, and checks every line with v1's trust check (`inbox.Trusted`). It writes nothing, and refuses a location that leaves the inbox, a symlink and a multiply linked file, as v1 does.

**One event for each line.** Each complete physical line is one event, in order, and its `seq` is the line's number, gap lines and repaired partial lines included. So every reference v1 wrote, `s:<first 8>#<line>`, names the event of that line, and resolves to v1's text for it. An unfinished last line is left unread, as v1's readers leave it, and counted in `tail`.

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
| `event` `gap: …` | `gap`, author `eva`, marked `name` |
| `event` lifecycle names (`session_start`, `session_end`, `compact`, `model_switch`, `stop_failure`, `interrupt`) | `life`, author `host`, marked `name` |
| `event` with another text | `life`, author `unknown` |
| a prompt, reply, tool or plan kind with a role v1 never writes with it | that kind, author `unknown`, as v1's walker yields it |
| any other role and kind | withheld, `why: unknown_kind` |

A returned event's text is v1's text for its line, byte for byte; its time, turn id, subagent and context are the line's; `src.h` is set for a hook's line and `recovered` for recovery's; `rv` is 0, since v1 does not record the rule version of a line; `turn` counts the prompts the reader returns, copies not included.

**Never typed.** v1 has no evidence that a prompt was typed on Codex, and it files voice hand-overs and program prompts as the user's, so no event of the v1 backend has the author `typed`.

**Trust and the record.** A line is returned only when it belongs to its file by v1's trust check. The off-the-record state changes at each pause and resume marker, in file order. v1's readers disagree on which markers count: its walker (search and the brief) honours only a marker that belongs to the file, and its update honours any marker line. The backend withholds a line when either would, so a marker forged into a file can pause the record but never resume it. The markers themselves are returned, as fixed names. A prompt, reply, tool or plan line is returned only when v1's walker yields it with the entry v1's reader decoded for the same line; otherwise it is withheld (`why: unconfirmed`). The walker reads the file only as far as the reader did, and a file whose lines change in number or length between the two fails the read.

**Where v1's readers disagree.** A line exactly 2 MiB long is decoded by v1's walker but replaced by a gap by its reader and its update. The backend sides with the reader, so the line's text is never returned (`TestLineAtV1sLimit`).

## 10. Invariants and the tests that hold them

| Invariant | Tests |
| --- | --- |
| The codec round-trips: golden events and headers decode and encode back byte for byte, and the golden file holds every kind, author, mark, exposure, cut reason and operation | `event.TestGoldenEventsRoundTrip`, `TestGoldenEventsCoverTheModel`, `TestGoldenHeadersRoundTrip` (`testdata/codec/`) |
| An event that breaks an invariant is never read or written: seq at least 1, known kind and author, a key, a copy of an earlier event only, an operation on controls only, a reason on gaps only, spans ordered, apart and inside the text, cuts with bytes lost, a known reason and a place, exposures of a known kind brought by an earlier event, marks known and once, a withheld event a bare gap, an undecoded wrapper a prompt neither typed nor spoken | `event.TestInvalidEventsRefused` (`testdata/codec/invalid.jsonl`), `TestEncodeRefusesInvalidEvent` |
| The short form names a session as v1's references name it; ids and references parse and print back | `event.TestPrefixMatchesV1References`, `TestIDForms`, `TestRefForms`, `TestRefNamesASession` |
| A v1 inbox yields one event for each line, seq equal to the line number, text equal to what v1's reader, its update and its walker return for that line | `inboxv1.TestFixtureInboxReadsAsV1Does`, `TestGeneratedInboxes` (through `checkV1`) |
| Every v1 reference resolves to the same text; a short reference two sessions share is refused | `inboxv1.TestV1ReferencesResolve`, `TestAmbiguousReferences`, `ledger.TestResolve` |
| No event of the v1 backend has the author typed | `checkV1`, so every test of the backend; `TestGeneratedInboxes` |
| Nothing off the record, forged into a file, or unfinished is returned, in any field, under generated placements of pauses and resumes, forged markers included | `TestFixtureInboxReadsAsV1Does`, `TestGeneratedInboxes`, `TestForgedMarkers`, `TestUnconfirmedContentIsWithheld` |
| A second copy is marked and counted and nothing is dropped: the same entry twice is a copy; two replies in one turn, the same command twice in one turn, the same words in another turn or kind, or with no turn id, are not | `TestSecondCopies`, `TestGeneratedInboxes`, `TestFixtureInboxReadsAsV1Does` |
| A session's class comes only from what its file shows, and the v1 backend never classes a session as a person's chat | `TestSessionClass`, `TestFixtureInboxReadsAsV1Does`, `TestGeneratedInboxes` |
| The packages import no new module, and only eva's name appears in their text | `ledger.TestOnlyEvasNameAndNoNewModule` |

## 11. Decisions taken here

1. **Two authors beyond the plan's list.** The plan lists who authors a text; the ledger design adds `tool` for tool records and `eva` for records eva-md writes. Both are kept, since a tool record and a gap are no one's words and `unknown` would hide what is known. A third, `v1_user`, is the card's: a prompt v1 filed as the user's.
2. **The v1 sameness key uses the text, with the turn.** The plan's key is the turn, the kind and the tool call's id or the line's position. v1 keeps neither a call id nor a position in the host's record, and its double entries are a prompt or a reply stored by both the hook and recovery under one turn, so the text stands in for the position for those two kinds only, never alone (section 8).
3. **The order of the v1 class evidence.** The card lists four kinds of evidence for five classes. An end with no start and no turn is a thread opened and never run, as Codex's source shows (plan 13.5); a session with no turn at all and not that is the host's housekeeping. The host sitting (HF-00) may move the line between the two.
4. **Off the record is the union of v1's readers.** Where v1's walker and update disagree on a forged marker, the backend withholds what either withholds (section 9).
5. **What is not here.** Decoding a voice hand-over (LG-13), authors from live payloads (LG-13, LG-21), exposure marks (LG-21, LG-23), the stored ledger and its header (LG-20), joining a session kept in two folders and the `legacy` map (LG-25).
