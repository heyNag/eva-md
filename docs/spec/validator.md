# Validation

- **Component:** Validator
- **Paths:** `internal/update/validate.go`
- **Status:** v1 as built, frozen on 2026-11-14

The blocks below are v1's design and verification text, moved here word for word when the two documents were split; [INDEX.md](INDEX.md) lists every spec.

<!-- v1 docs/design.md 024 -->
## Validation

All of this happens in code (`Validate` in `internal/update`, pure and deterministic) before writing.
Its report counts every change for the run log, as counts only, never text: refs removed, tags changed, decisions filed as proposals, lines dropped for dates, ids carried, operations ignored or refused, stale supersessions, restatements, replace questions, duplicates, grounding misses by class, unknowns, the rule and knowledge changes and the out-of-order guard.

1. **Strict parse.** The run is rejected if a Codex run used a tool, if Claude returned no `structured_output`, or if the output doesn't fit the schema exactly: a missing or extra key, a null where none is allowed, a value of another type or outside its enum, a ref that isn't an integer, or an array longer or shorter than the schema allows.
2. **Refs.** Every ref must be the number of an entry in this batch; any other is removed and counted.
   A line keeps its first 5 distinct refs, which its tag is checked against; the rest are removed and counted too.
   Each resolves to `s:<id8>#<line>` with the entry's physical line (a run of tool lines names its last), or to the full session id when another session file in the inbox starts with the same 8 characters, as `eva-md search` writes refs.
3. **Speaker tags.** A tag must be one the cited entries support:
   - **stated** needs a cited user prompt whose own words share a content word with the claim, the same word or one the start of the other from 4 letters ("Postgres" and "PostgreSQL" match; "log" and "login" don't).
     A negative contraction such as "don't" or "isn't", or its fragment, is no content word.
     A prompt stored as an event, such as a host prompt, never counts.
     So citing a bare "ok, go ahead" can't make an assistant's text, or text injected into it, the user's.
   - **accepted** needs a cited user prompt and an earlier proposal: a cited assistant entry before it, or (change 4) the stored item an edit or answer acts on, when that item is tagged suggested or inferred and the cited prompt is at or after its time.
     An add of a stored proposal's text that the same output accepts may carry accepted too.
     The claim must share a content word with what was accepted: a cited assistant entry before the prompt, the stored item, or the user's own words.
     A claim of accepted that has its user prompt but no proposal is stated when it shares a word with the prompt's own words, so the user's words never go under the assistant's label; otherwise, and for an edit or answer on a stored proposal that its prompt doesn't come after, it falls to inferred.
   - **suggested** needs an assistant entry.
   - **observed** needs a tool entry or an assistant's report of a result: the first reply after a tool entry, with no prompt, host prompt or other reply between them.
     The input marks every tool entry and report of a turn, from one prompt, typed or a host's, to the next, in which a tool line names WebFetch, WebSearch or Codex's web_search, before the fetch too, and in a later pass that starts inside the turn, where an earlier pass's 60 KB stopped after the fetch,
     a reply that opens the pass among them, since it may report a tool the earlier pass held, though it counts as no result: the job carries the turn's state from pass to pass, and from run to run through the lines before the watermark, as it carries the off-the-record state; such a citation still makes a Now item or a Timeline or Done line observed, but never knowledge (rule 13).
     A pass whose 60 KB stops inside such a turn before its fetch can't see the fetch and marks none of the turn, so a command it holds can still make knowledge observed; nothing the fetch read can reach that pass.
   - Anything else becomes **inferred**, and the change is counted.

   Only stated and accepted items are decisions: an add or Timeline line of kind decision with any other tag is filed as a question, which the page shows under Open questions as `Proposed (assistant): ...` when suggested and `Unattributed: ...` when inferred. A stated or accepted claim that cites no user prompt, or rests on none of the cited words, is counted as `unattributed`.

   **Pasted text** is the user's only as a quote.
   On Claude Code, a span opened by the paste wrapper's tag runs to the closing tag that repeats the opening tag's id, so pasted text can't end its own span, or to the end of the prompt; the model sees each pasted line marked `[pasted] `.
   A stated or accepted item or Timeline line whose content words are found in cited pasted text and not in the prompt's own words becomes inferred and is counted as `pasted`, and a prompt that is all paste isn't a user prompt.
   Codex prompts are never parsed for pastes.
   Done lines get their tag from their citations: observed for a tool entry or a report, stated for a user prompt, inferred otherwise.

   The invariant (S1): no rule changes a speaker tag except this one, and no rule shows user speech under an assistant label.
   Lines eva-md writes itself carry a code and keep their stored tag: a decision kept past the cap (`eva-md: decision over cap, kept: ...`) and a replace question (`eva-md: replace question: does "X" replace "Y"?`), which takes Y's tag.
   A decision, or an item the user owns (stated or accepted, or written by hand, as an item with no tag is, since the update tags every item it writes), takes no edit or replacement that would leave it without the user's tag; that operation is refused and the item stays.
4. **Dates.** A date written in any text, as YYYY-MM-DD, D Month YYYY, D Month, Month D or Month D YYYY, must appear in the text of a cited entry, in any of those forms.
   In what the model writes, a month name in lowercase counts only with a year, so "option 2 may be simpler" holds no date; an entry's text is read in any case.
   An entry's own date, in its header, is not in its text and doesn't count: an event the entry says happened yesterday didn't happen on that date.
   The summary and last chat cite nothing, so they are checked against every entry of the batch.
   A line with an uncited date is dropped (a summary or last chat is left out and the page keeps its own) and counted, and more than 3 such drops reject the run.
   Timeline dates come from the local date, in the entry's own offset, of the newest entry the update cites (only its Timeline lines' entries when the out-of-order guard holds), or else of the batch's newest entry.
5. **Item accounting and freshness.** Every existing id must be kept, edited or retired.
   Only an id on the page counts: an operation on any other id, or a second one on an id, is ignored.
   A missing id is carried forward and counted, and more than half missing rejects the run, unless the out-of-order guard keeps the Now. done needs a tool entry or an assistant's report among its citations; answered needs an answer and a cited user prompt, and the answer carries its own tag; superseded
   needs a replacement; dropped needs a reason, and a stated or accepted item, or one written by hand, is never dropped without a cited user prompt.
   A decision is neither a task nor a question: it is never done or answered, and leaves the Now only superseded or dropped, through the user.
   A replace question is eva-md's own line, whose text its exit reads, so it is only kept, answered or dropped.
   A refused operation leaves its item as it was.

   Freshness (change 5, S4): a supersession applies only when the replacement's newest cited user prompt is at or after the item's stored time, compared as instants (equal accepts).
   A replacement that normalizes to the old text (lowercased, letters and digits only) is refused as a restatement.
   A replacement that shares no content token with the old item (a lowercased word of 3 or more letters outside a short stopword list, or a code-like token) becomes an add of the replacement, X, plus a line eva-md writes under Open questions, `does "X" replace "Y"?`, and Y stays.
   The exit (S2): once the page holds that line for Y, a supersession of Y whose replacement normalizes to X, or any supersession of Y citing a user prompt after the line's time, applies without the token test.
   An assistant's item never supersedes the user's.
   An edit refreshes an item's time only when it cites a user prompt newer than that time, and never moves it back.
6. **Duplicates and idempotency (change 6, S5).** Adds and Timeline lines of kind decision, finding or question are dropped and counted, never merged, when their normalized text is already on the page (in a Now item that stays this pass, or in a Timeline line, each side of an arrow counted on its
   own, but never struck-through text, so a return to a superseded or dropped value is kept) or earlier in the same output. next, done, change and problem lines are never deduplicated, so recurring work keeps its own lines.
   An add cited only by assistant entries is also dropped when one existing Now item holds 0.6 or more of its content tokens or, when it has a code-like token, all of them; a suggestion with neither survives as a proposal.
   `threadpage.Apply` then leaves out adds and Timeline lines whose text and refs are already on the page, so applying the same pass twice changes nothing, and the job detects a pass that changes nothing by comparing the rendered page with the body read back, and doesn't rewrite the body: it only stamps the page's `as_of` and `sources` when the pass moves them.
7. **Out-of-order guard.** If this session's newest prompt, reply, tool, plan or host summary is older than the page's `as_of`, the update keeps the Now and only appends its Timeline lines; the page's ids then need no action. The newest is taken over all the entries `Build` is given, not only the chunk a pass covers, so the first chunk of a newer session isn't taken for an older one.
8. **Escaping, caps and redaction.** Every string is redacted again (`redact.Redact`) and cut to its cap: the summary 600 characters, the last chat 300, Done lines 200, everything else 300.
   The renderer (`internal/threadpage`) then escapes all free text in one pass: control and format characters are removed; HTML comment delimiters are broken; runs of 3 or more backticks or tildes are broken; bracketed speaker labels and tags such as `[user]` or `[stated]` become parenthesized; `](` is
   broken, so no link forms; a line that is exactly `---` is written as `\---`; heading lines are escaped; a line starting with a frontmatter key okf checks (`verified`, `governance`, `generated`, `type`, `status`, `code_refs` or `stale_after`, quoted or not, before a colon) or with eva-md's own labels
   (`eva-md:`, `Proposed (assistant):`, `Unattributed:`) gets that key in backticks; and every text but the summary is collapsed to one line.
9. **Grounding count (change 1, S8).** Code-like tokens (paths, URLs, SHAs of 7 to 40 hex digits, identifiers with dots, underscores or `::`) in the text the update writes are checked, after output redaction, against every raw redacted entry of the batch, the page's item texts and the commit subjects, ignoring case; a path's basename matches, and so does a SHA's 7-character prefix.
   Numbers with units (`300 ms`, `40 calls`, `64 KiB`, `v1.2`) are a class of their own, found when the number and unit appear in the batch, the page's Now items or the commit subjects.
   Misses are counted by class.
   This rule never changes a tag or drops a line.
10. **Locked, human-verified or damaged pages** are not written. The watermark advances and the run log records "skipped (locked)" and so on.
11. **Refused page.** If okf still refuses the save because of text you wrote, the pass fails with `page_refused`, which counts toward the session's quarantine, so it is never retried in a loop, and your sections are never rewritten; the doctor's updates row reports it.
12. **Standing rules.** An add must be tagged stated, and one cited user prompt's own words, outside any paste, must hold every content word of the rule in the same sense: a negation word (never, not, no, nor, stop, avoid, without, instead, cannot, any contraction ending in n't, and the common ones
    written without the apostrophe, such as dont, isnt or wouldnt) turns around the rest of its clause, the rule must negate only what the prompt negates, and it must not assert a word the prompt only negates.
    A rule already on file is a duplicate, and one past 40 is refused.
    An edit passes the same test; a drop needs a reason and one cited prompt holding at least half the rule's words.
    An edit or drop needs a cited user prompt at or after the rule's time, and every kept one adds a Timeline line, such as `change [stated]: rule r1: ~~old~~ → new` or `rule r1: ~~old~~ (dropped: reason)`.
    A rule goes to all projects only when the prompt that states it says so ("all my projects", "every repo", "everywhere" and a few more) in words no negation of scope turns around: not, cannot or a word ending in n't, before them in their clause with no word of the rule between ("this isn't a rule for
    all my projects", "don't add this to every repo" and "not in all projects" don't count; in "don't mock the database in all my projects" the negation is the rule's own, and "never in all my projects mock the database" counts too), or when the chat is outside any project.
    Changing such a rule needs the words named in any sense, since they then name the rule's scope ("rule g1 shouldn't apply to all projects anymore").
    The out-of-order guard refuses every rule change.
    Rules pages are saved after the knowledge pages and before the thread page, with compare-and-swap; a locked, verified or damaged rules page is left alone, and a project's first rules page claims its folder as a first thread page does.
    A rule add, edit or drop on such a page is passed over: it leaves no Timeline line on the thread page and comes out of the added, edited and dropped counts, counted `read_only` for a locked or verified page and `refused` for a damaged one, and a pass whose only change was such a rule change ends `no_change`.
13. **Knowledge.** Checked after the rules, from the current batch only: refs can name only its entries, so only what the batch states can be promoted.
    Under the out-of-order guard every knowledge change is ignored.
    Supersedes and drops come first, the first change for an id only, and only for an id the input showed; one on a read-only row, or on another project's own row (scope `project` with another project key, which the input marks `(read-only)` too), is counted `read_only`.
    Then the adds.
    At most 5 are kept in all; the rest are counted `over_cap`.
   - **An add** needs a kind, a title and a statement that are not empty once scrubbed (redacted again, collapsed, the title cut to 80 characters and the statement to 300 with `…`).
     Every written date must be in a cited entry, as in rule 4.
     The tag goes through rule 3, then the kind decides what it may be: a decision or a constraint only stated or accepted, a fact or a procedure stated, accepted or observed, and suggested or inferred never; pasted text is never stated or accepted.
     Observed knowledge rests on a command, a file read or edit or an MCP tool: one whose citations include a tool entry or report the input marks as part of a turn that used a web fetch or web search (rule 3), even beside another turn's, is refused, so what a web page or search said stays a suggestion, while the user's own words in that turn still settle a fact.
     A pass whose 60 KB stops inside such a turn before its fetch marks none of it, so a command it holds can still make knowledge observed, though nothing the fetch read reaches that pass.
     A statement tagged stated or accepted, of any kind, must also hold at least half its distinctive words in the cited prompts' own words, outside pastes; a proposal's words never count, so neither citing the user's question nor a closing "thanks" after the assistant's proposal makes the assistant's answer the user's.
     Any grounding miss (rule 9) in the title or statement refuses it, since knowledge outlives the chat.
     A statement equal, normalized, to the statement of an active row that applies to the project (one for all projects, or the project's own) or of another add of the output is a duplicate.
     One is a near duplicate when such a row or a kept add holds 0.6 or more of its content tokens, or when it holds every code-like token of a row or add that has some, each as a whole token (a path never by its file name alone), and shares a content word with it beside those tokens.
     Both checks run against every such row of the recall index, not only those the input showed; another project's own rows never count.
   - **A supersede** names an active row the input showed, not read-only, and gives a new statement that is not, normalized, the old one (refused otherwise).
     It needs a cited user prompt, in its own words, at or after the entry's stored time, or it is stale.
     The add's tag, kind and user's-words rules apply to the new text with the change's kind, the user's words read only from cited prompts at or after the entry's time, so an older chat's words never carry a supersede while a newer prompt about something else lends it the time; so do the duplicate check against the other rows, grounding and the secret scan.
     A kept supersede leaves the Timeline line `change [stated]: knowledge k12: ~~<old>~~ → <new>`, each side at most 120 characters, with the change's refs.
   - **A drop** names an active row the input showed, not read-only, with a reason (cut to 300).
     As for a rule's drop, a cited prompt's own words must name the entry, holding at least half its distinctive words (refused otherwise), and one such prompt must be at or after the entry's stored time (stale otherwise).
     It is tagged stated and leaves `change [stated]: knowledge k12: ~~<old>~~ (dropped: <reason>)`.
   - **Scope** is all only when one cited prompt's own words both hold at least half the statement's distinctive words and name all projects with no negation of scope, as for a rule, or the chat is outside a project; otherwise it is the project's.
     A supersede keeps all for an entry that already holds for all projects, unless, in a chat in a project, the own words of a cited prompt at or after the entry's time narrow it: they name all projects under a negation of scope ("not in all my projects"), or say "only this project", "just this repo", "this repository only" or the like.
     The entry then becomes the project's, and its page moves to the chat's project, so that project's brief still lists it.
   - **Paths** are trimmed, lose a leading `./` and a trailing `/`, and are kept only when relative (no leading `/`, no `.` or `..` part, so `a/./b` is dropped, no space or control character, at most 200 bytes), not repeated, at most 3, and equal to a path or identifier a cited entry's text holds,
     trimmed the same way, or to a whole folder of one, ending at a `/` (`internal/inbox` for `internal/inbox/store.go`, never `internal/in` or `inbox/store.go`); the rest are counted `paths_dropped`.
   - **Secret scan.** Every line the save would write for the change (`knowledgepage.Written`: the page as saved apart from its generated line, its History line among them, its line in the folder index, on the map and in the recall index, and a new page's log.md line), and its Timeline line on the
     thread page, go through the daily commit's scan (`wikigit.SecretLines`); a change it flags is refused, so the daily commit never leaves a knowledge page, the files that list it or the thread page out.
   - **Saving.** Knowledge pages are saved first in a pass, through one `knowledgepage.Save` under one hold of the wiki lock, and only when the pass kept a change; then the rules pages, then the thread page.
     A save error fails the pass as a rules page's does (`conflict`, `wiki_error` or `page_refused`) and keeps the watermark.
     A change the save passes over, such as one on a page a person locked after the recall index was built, is moved in the counts (`read_only` for a locked or verified page, `duplicates` for one the pages already hold, `refused` otherwise), leaves a log line such as
     `<label>: knowledge op 0 on k1: locked; left as it is` (`op <n> (a new page)` for an add) and no Timeline line, and the pass still succeeds.
     A rerun after a failed thread save writes no second page: the add is then a duplicate of the index's row.

<!-- /v1 -->

## Since the split

No change yet.
