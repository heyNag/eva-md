# Background update: the offline checks

- **Component:** Updater and validator: their offline checks
- **Paths:** `internal/update`, `internal/update/accuracy`
- **Status:** v1 as built, frozen on 2026-11-14

The blocks below are v1's design and verification text, moved here word for word when the two documents were split; [INDEX.md](INDEX.md) lists every spec.

<!-- v1 docs/verification.md 015 -->
- **Update input and validator** (`internal/update`).
  The input builder: physical line numbers and end offsets past a watermark (`Prefix` counts the lines before it and finds whether the chat was off the record there; `Decode` keeps a malformed line as a numbered gap and leaves an unfinished last line); off-the-record ranges dropped, one open at the
  watermark included; groups under one exchange or 600 characters left for later, while the rest of a group a pass stopped in, such as a closed chat's last reply, is built; speakers from the role, with pasted headers quoted so only the input's own headers start a line; tool runs compressed and ending
  at a command, plans as checklists, host summaries labelled; a reply taken as a report of a result only when it is the first after a tool entry, with no prompt, host prompt or other reply between; every tool entry and report of a turn whose tools fetched or searched the web marked, from one prompt,
  typed or a host's, to the next, before the fetch too and for a subagent's search, and none of a turn of commands, reads, edits or MCP tools only, and the rest of such a turn marked in a pass that starts inside it, with a reply that opens a pass, which may report a tool an earlier pass held, marked
  when its turn read the web and never counted as a result (`TestBuildMarksWebTurns`); a fact that cites a web turn's report and a later turn's command refused as observed knowledge in one pass and when a pass opens with the report (`TestReportOpeningAPassKeepsItsWebMark`); whether the turn open at
  the watermark used the web, read from the lines before it and from the entries a pass covers alike: a prompt, one over 64 KB too, or a host prompt, the first or a later one, opens a new turn, which a web tool's line marks, while a command, a reply or a prompt that only names a web tool marks nothing
  and nothing off the record changes the state, a host prompt or a web tool's line there included, though capture writes neither there (`TestTheOpenTurnsWebState`); the same state when a pass that starts off the record, as after a run that ended so, comes back on it before a web fetch and the
  watermark moves past all its entries at once, wherever it stops (`TestWebStateAfterComingBackOnTheRecord`); a turn whose web fetch one pass covers and whose report the next does, as when 60 KB stops inside it, giving no observed knowledge within one run, in a later one, and in a run whose first pass
  starts off the record, where the run before ended, and comes back on it before the turn (`TestWebTurnCarriesAcrossPasses`); and a pass whose 60 KB stops inside a turn before its web fetch, which it can't see, so a fact a command before the fetch showed is observed knowledge there, while one pass
  over the whole turn refuses it (`TestAPassCantSeeAFetchPast60KB`); every kind trimmed to 6 or 8 KB keeping its head and tail; oldest-first chunks of at most 60 KB, checked at exactly 60 KB and one byte over, with the next pass starting where the batch's end offset says; the batch's last time taken
  from the session's newest work, never an event and not only the chunk's; and the page context (Now items with ids, kinds, tags and refs, the last 5 Timeline lines with their dates, a new thread's parent Now, 5 commit subjects, and no summary or last chat), the subjects and head read with
  `log.showSignature` on and no signature checked (`TestCommitsOfWithShowSignature`), with `GIT_NO_LAZY_FETCH=1` and never `--no-lazy-fetch` (`TestCommitsOfFetchesNothing`).
  The schema: every object strict, with the design's keys, enums and bounds and no keyword Codex's strict mode refuses, and an items bound of one for each of the page's ids and 40 more, so a page of 61 ids takes an action for each and an add; the parser rejects a missing or extra key, a null, a wrong
  type or enum value, a ref that isn't an integer and an array out of bounds, naming the place but never the text.
  The validator, rule by rule: refs resolved, with the full session id when asked, refs outside the batch removed, and a line kept to its first 5 refs, its tag checked on those; each speaker tag against its citations, a host prompt never counting as the user's, accepted only with a cited proposal
  before the prompt, the two-session acceptance (change 4), an accepted claim without a proposal kept stated when it shares a word with the prompt, and a stated or accepted claim that rests on none of the cited words, as one injected behind a bare "yes", filed as an unattributed question
  (`TestValidateSpeakerTags`); an item written by hand, with no tag, dropped, edited or replaced only on the user's word (`TestValidateProtectsHandWrittenItems`); a zero-tag-change fixture past the decision cap, with a replace question and a paraphrased code token; dates in every form, lowercase month
  words such as "option 2 may be simpler" that are not dates, the 3-drop limit and the Timeline date in the entry's own offset; item accounting with carried, refused and ignored operations, the more-than-half rejection, and decisions that are never done or answered; freshness with equal instants
  accepted, restatements refused, the replace question and both its exits, each on its own, its text never edited, superseded or done, and edits that never move an item's time back; duplicates against the Now and the Timeline, a return to a superseded or dropped value kept, recurring done, change and
  problem lines, and the assistant-only overlap and code-token arms, each on its own; grounding misses by class with no tag changed; the out-of-order guard, on the session's newest entry rather than its first chunk's, with Timeline lines alone accepted and filed under their own entries' date;
  redaction, caps and escaping through the renderer; and the same output validated twice giving the same update.
  The cleaning the brief and the input share (`internal/freetext`) has its own test as well.
  Knowledge: the input lists the recall index's rows that share words with the batch, most first, newest first on ties, `(read-only)` marked, rows sharing none left out, at most 12 lines and 2,400 characters, a line that doesn't fit skipped while shorter ones after it go in
  (`TestBuildBoundsTheKnowledgeOnFile`), and the pinned context gains the section and its empty form (`TestBuildShowsThePageContext`); adds kept when stated or observed, refused when suggested, inferred, the assistant's alone, accepted on the user's question alone, an observed constraint, without a
  title, a duplicate or near copy of a row shown or not, or with an ungrounded path, a path fragment dropped, and no near copy of a short row it merely holds or of a path that shares only its file name, the kept operation exact, 7 adds keeping 5 and counting 2 over the cap, and the out-of-order guard
  ignoring all (`TestValidateKnowledgeAdds`); scope all only on one prompt that both holds the statement and names all projects, never on another cited prompt's, or outside a project (`TestKnowledgeScope`); a supersede of an entry for all projects keeping all on the user's words for it, as for all
  projects or not, and the project's, moved to the chat's project, on words that narrow it, "just in this repo" or "not in all my projects", while outside a project it stays all (`TestKnowledgeScopeOfASupersede`); another project's own row shown `(read-only)`, its supersede and drop counted read-only
  with no Timeline line, and its statement no duplicate here (`TestKnowledgeOfAnotherProject`); a fact or procedure observed on a command's result or an MCP tool's kept, and refused on a tool entry or report of a turn that read the web, cited alone or beside another turn's, a supersede too, while the
  user's own words in that turn still settle a fact (`TestValidateKnowledgeOffTheWeb`); supersedes and drops with their exact Timeline lines and refs, stale on older words or no user prompt, refused as a restatement, untagged or without a reason, refused on the assistant's words beside the user's
  newer prompt and on older words beside a newer prompt about something else, a drop refused on a prompt that doesn't name the entry and stale on older words that do, a drop's reason with a date no entry writes dropped, counted when read-only, not shown, unknown or a second change, a supersede freeing
  the old statement, one to another entry's statement a duplicate, and one for all projects without the user's words kept the project's (`TestValidateKnowledgeChanges`); the same output over 200 rows validated twice to the same result (`TestValidateKnowledgeIsDeterministic`); the secret scan passing
  plain statements, identifiers, refs and paths (`TestKnowledgeSecretScanPassesPlainText`); a change refused when the commit's scan flags a line the save or the thread page would write though each text alone passes, the recall index row, the folder index line, the History line and the Timeline line,
  and every file the save writes for kept changes, and the thread page, passing the scan (`TestKnowledgeSecretScan`).
  The schema's 6 objects, the knowledge object's nullable fields, bounds and enums (`TestSchemaIsStrict`); a version-3 answer parsed, a version-2 answer read as no knowledge changes, and knowledge without rules, a bad enum, missing refs or paths, a missing or extra key and 11 changes refused
  (`TestParseOutputIsStrict`); the rules at version 4, 3,300 to 3,900 characters, with the knowledge paragraph, the rule that a turn that used a web fetch or web search settles no observed knowledge, and the rule never to copy a credential (`TestRulesSayWhatTheEntriesAre`); the dry run pinning version
  4, the knowledge paragraph, the web and credential rules, the empty knowledge section and the schema's knowledge array (`TestDryRunIsByteIdenticalAndShowsNoValues`).
  The canned outputs of the chat tests carry an empty knowledge list.

<!-- v1 docs/verification.md 016 -->
- **Accuracy suite** (`internal/update`, offline).
  12 fact scripts, one per known failure mode: an assistant's suggestion against the user's decision, a weakened event date, a reversal, a correction, a conditional fact, pasted text posing as a decision, a host prompt that must never become stated, item accounting across two passes, the two-session
  acceptance, the replace question and its exit, two overlapping same-day sessions processed in the wrong order (then a chat of the day before, whose finding goes under its own date while the Now stays), and an assistant's suggestion that survives as a proposal.
  Each fact has a speaker, time, validity, the fact it replaces, a tag, a branch and a codename no model could infer; code renders the facts into inbox entries, derives the obligations and runs canned outputs through the builder, the validator and `threadpage.Apply`.
  Enforceable obligations are checked after every output: nothing said off the record reaches the batch, every fact said on the record reaches it word for word on its branch, only the batch's own headers start a line, no assistant's, tool's or host's fact becomes a decision unless the user accepted
  it, a host prompt never becomes the user's, a newer value on the page stays, a session older than the page leaves the Now as it was, every user-tagged operation cites a user prompt, an item the user owns is dropped only on the user's word, done cites a tool entry or a report of a result, a decision is never done or answered, every date written is in an entry, and no id leaves the page.
  Each adversarial output (a suggestion tagged stated, an invented date, a missing id, a supersession citing an older prompt, a restatement as a supersession, a paraphrased code token, the user's decision dropped on the assistant's word, a step done on the user's word, a decision marked done, an
  acceptance with no proposal, an acceptance older than the stored proposal, and others) must be rejected or mended as expected; those the validator can't catch by design, such as a reversal kept beside the old value, a dropped condition or a pasted decision, must show up in the content metrics instead.
  The correct outputs must score perfectly after every pass on the operation-level metrics, measured at that pass's time: a fact counts once its chat is processed and stops holding when its validity ends, and a value that stopped holding counts as stale in force under any tag.
  The metrics are recall of planted facts, update accuracy, stale-value rate, speaker-tag and date accuracy, item accounting and duplicates.
  A tenure case runs 12 weekly passes to 40 decisions, 6 next steps and 3 open questions, giving every id an action, without a decision dropped or moved, and keeps every weekly done line.
  Content obligations are graded on real model output only once wiki updates run.
  Seven knowledge histories, each pass saved into a real wiki, with the enforceable knowledge obligations checked after every canned output: a fact stated, then superseded in a later session, its old statement in the History and a Timeline change line; an assistant's suggestion, in five adversarial
  outputs, two accepted on the chat's closing thanks, never a decision or constraint, nor a supersede on the assistant's words; pasted text never stated; a restatement refused, exact, near and as a paraphrase of its code tokens; an older chat's supersede and drop refused as stale, and refused when a
  newer thanks or a newer prompt about something else is cited beside the older words; a locked page never written, with a stale index and after a rebuild; and a constraint with its path, bad paths dropped, listed for the brief.
  Weakening the kind and tag rule, the stale rule, the user's-words rule or the path check makes it fail (`TestAccuracyKnowledgeHistories`).

<!-- /v1 -->

## Since the split

No change yet.
