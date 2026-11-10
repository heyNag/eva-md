# Background update, second half: validation, page writes, job control, triggers, budget, run log

Audit of `the repository` at the checked-out commit. Read in full: `internal/update/{validate,wiki,pass,job,runlog,knowledge,rules,backlog,dryrun,guarantees}.go`, `cmd/eva-md/update.go`, `internal/hook/trigger.go`. Read in part, to follow the flow: `internal/update/{schema,tokens,runner,input,baseline,context,claude}.go`, `internal/hook/{hook,resume,spawn,maintain}.go`, `internal/health/{background,status}.go`, `internal/config/config.go`, the tests' names and the cases named below, `docs/design.md` 799-840 and 938-993, `docs/known-limitations.md` "Background update", "Standing rules and pasted text", "Knowledge pages and recall".

Every claim about code carries file:line. Where I estimate what a real model would do, I say "inferred": no live update has run yet (`docs/known-limitations.md:176`), so nothing in the repository measures it.

---

## 1. How it works

### 1.1 Data flow

1. **A hook asks.** A hook event appends to the inbox, writes its output, then `launch` starts each due command detached and never waits (`internal/hook/spawn.go:19-36`, `47-69`: setsid, stdin `/dev/null`, output to the day's log, allowlisted environment). "An update always is" due (`internal/hook/maintain.go:49-64`).
2. **One job at a time.** `Job.run` returns `paused` when `updates = false` (`internal/update/job.go:369-372`), then takes `<locks>/update.lock` without waiting; a busy lock makes the job exit 0 with `busy` (`job.go:373-377`, `486-495`). Nothing re-queues that trigger.
3. **Start.** The job sets aside pre-update chats (`job.go:382`), opens the wiki after checking schema version and instance (`internal/update/wiki.go:35-55`), reads the run log of yesterday and today (`internal/update/runlog.go:114-124`), lists every session file of the whole inbox (`job.go:514-558`), lets the caller pick (named sessions, plus the day's sweep, or all), and keeps the files whose watermark shows a backlog or can't be used (`job.go:409-438`).
4. **Drain.** For each file, oldest change first: skip it unopened when its host is stopped or its session is quarantined today (`internal/update/pass.go:55-71`); otherwise open it at the watermark (`internal/update/backlog.go:49-74`, which re-counts the lines before the watermark from the start of the file) and run passes until nothing is pending, the rest waits, a pass fails or a stop applies (`pass.go:82-91`).
5. **Plan a pass** (`pass.go:138-203`): the group is the run of entries from the watermark on one branch (`backlog.go:134-148`); a last prompt without a reply waits (`backlog.go:182-195`); a program's chat is skipped with no call (`pass.go:168-171`, `backlog.go:161-173`); a group under one exchange or 600 characters waits, or is skipped when a branch switch closed it (`pass.go:172-178`); the thread page and both rules pages are read (`pass.go:182-187`); `Build` cuts up to 60 KB of trimmed input (`internal/update/input.go:222-232`); a locked, verified or damaged thread page makes the pass a skip with no call (`pass.go:196-199`).
6. **Gate** (`pass.go:309-353`): the daily cap, then (unless manual) a hold, a backoff and the session's quarantine.
7. **Call** (`pass.go:379-454`): save the watermark as it stands (`380-383`); run the host CLI in an empty temp folder with the chat's own model (`pass.go:201`, `internal/update/context.go:138-150`) at the configured effort, default the host's highest (`internal/config/config.go:185-197`, `internal/update/runner.go:327-330`), with a timeout of 180 to 600 s by effort (`runner.go:62-65`, `337-345`); validate (`pass.go:400`); save knowledge pages, then rules pages, then the thread page (`410-435`); rebuild the area's overview and index files (`444`, `wiki.go:605-641`); move the watermark to the end of the batch (`449`); append the run-log record (`452`).
8. **Record.** One JSON line per pass or skip in `<state>/updates/<day>.jsonl`, fsynced (`runlog.go:300-326`); a line per pass and stop, counts and reasons only, in `<state>/logs/update-<day>.log` (`job.go:480-484`, `runlog.go:437-461`).

### 1.2 Triggers (all are hook events; there is no timer and no queue)

| Trigger | Condition | Code |
|---|---|---|
| SessionEnd, PreCompact | not a program's chat | `internal/hook/hook.go:568-570` |
| Stop | 60 KB or more unprocessed in the session file | `hook.go:582-584`, `internal/hook/trigger.go:19`, `98-104` |
| Prompt after a gap | file unchanged 30 min, 600 bytes or more unprocessed | `hook.go:522-524`, `trigger.go:20-21`, `111-116` |
| SessionStart catch-up | own file idle 2 min with a backlog, then the project's other idle sessions the brief listed; at most 5; one job | `internal/hook/resume.go:150-156`, `trigger.go:22-23`, `151-168` |
| The day's sweep | asked by a person's chat start until a job claims the day; 5 sessions idle 60 min, any project, plus up to 16 "left" chats recorded with no call | `trigger.go:62-81`, `job.go:138-142`, `150-181`, `199-233`, `328-347` |
| Manual | `eva-md update --session`/`--pending`/`--sweep`, `--foreground` makes it a person's run | `cmd/eva-md/update.go:63-148` |

No trigger fires while a chat is off the record or its transcript recovery is behind (`hook.go:495-512` return before the triggers).

### 1.3 Caps, budgets, time limits

| What | Value | Code |
|---|---|---|
| Input per pass | 60 KB trimmed; user entry 6 KB, other 8 KB | `input.go:223-225` |
| Smallest group | one exchange and 600 characters (exchange only when the chat ended) | `input.go:216-218`, `226`, `244-248` |
| Window read | 4 MiB, doubling to 64 MiB | `backlog.go:16-19` |
| Daily cap | 40 passes with a call, per host, per day; counts records, not CLI attempts | `config.go:34`, `runlog.go:328-337`, `pass.go:322-324` |
| Hold | `auth`, `mcp_failed`: the host until tomorrow or a successful manual run | `runlog.go:57`, `339-353` |
| Backoff | `rate_limit`, `host_error`, `wiki_error`, `cli_missing`: 5 min doubling to 60 | `runlog.go:58`, `63-64`, `355-383` |
| Quarantine | 3 session failures in a row (`invalid_output`, `tool_used`, `page_refused`, `timeout`, `model_rejected`); one retry on a later day; a failed retry moves the watermark past the batch | `runlog.go:56`, `62`, `385-403`, `pass.go:340-353`, `601-608` |
| State window | run log of yesterday and today only | `runlog.go:107-124` |
| Call timeout | 180 s (lowest effort) to 600 s (highest), shared by the attempts | `runner.go:62-65`, `164-168`, `208-211` |
| Save retries | 3 saves on a compare-and-swap conflict | `wiki.go:28-30`, `370-379` |
| Output arrays | done 8, timeline 20, adds 40 plus one per page id, rules 10, knowledge 10 (5 kept), refs 1 to 5, paths 3 | `internal/update/schema.go:167-176`, `internal/update/knowledge.go:15-17` |
| Text caps | summary 600, last chat 300, done 200, other 300 | `validate.go:64-70`, `internal/threadpage/page.go:100-106` |
| Now caps | next 10, open 10, decisions 40 | `threadpage/page.go:104-106` |
| Rules per page | 40 | `internal/threadpage/rules.go:34`, `internal/update/rules.go:141` |
| Sources per thread page | last 20 sessions | `wiki.go:25-26`, `328` |
| Job logs kept | 14 days; the run log is never pruned | `runlog.go:65`, `463-481` |

### 1.4 What it writes where

- **Wiki** (through `okfadapter`, compare-and-swap by page hash): `projects/<slug>/threads/<branch>.md` or `general/threads/default.md` (`wiki.go:76-82`, `344-381`); `projects/<slug>/rules.md` and `general/rules.md` (`wiki.go:383-389`, `480-537`); knowledge pages and their derived files through `knowledgepage.Save` (`pass.go:410-417`, `467-473`); `projects/<slug>/now.md` (claimed with the first page under one wiki lock, `wiki.go:362-367`, `564-578`; rewritten after each write, `682-727`); `threads/index.md`, the area's `index.md`, `projects/index.md` and one line of the root `index.md` (`wiki.go:605-641`, `743-755`).
- **Frontmatter of a thread page**: `as_of` (forward only), `head`, `branch`, `model`, `model_note`, `project`, `state: active` (always), `writer`, `parent`, `locked`, the last 20 sources (`wiki.go:294-329`).
- **Inbox**: only the watermark file, `offset [more] [wait <size>]` (`backlog.go:197-246`).
- **State**: `<state>/updates/<day>.jsonl`, `<state>/updates/sweep`, `<state>/logs/update-<day>.log`.
- **Temp**: one empty `eva-md-run-*` folder per call, removed after (`pass.go:546-550`).

---

## 2. What must be kept

These are the invariants and hard-won fixes I would carry into any redesign.

1. **Hooks never wait and never share a lock with the job.** Spawn after the output, detached, ignore a failed spawn (`spawn.go:15-36`); the job never takes the hooks' session lock (`job.go:486-488`). Trigger checks cost one stat, a watermark read only past a threshold (`trigger.go:94-116`).
2. **The inbox watermark is the truth about pending work**, not a side queue (`job.go:428-436`). If a queue is added, it must be a hint that the watermark can always overrule.
3. **The watermark rule**: it moves to exactly the end of the batch, only after the output validated and the pages were saved, and it is saved as it stands before the call so an unwritable watermark costs no call (`pass.go:368-378`, `380-383`, `449-451`; `backlog.go:197-204`). A watermark that fails after a call still counts the call (`pass.go:625-634`).
4. **Save order and idempotency**: knowledge, then rules, then the thread page, so a rerun after a failed thread save finds its own changes on file (`pass.go:368-378`); a pass that changes nothing writes nothing but may stamp `as_of` and `sources` (`wiki.go:209-214`, `346-351`); compare-and-swap, read again, apply again (`wiki.go:344-381`).
5. **A folder is proved before it is used**: `now.md` names the remote and is created under the same wiki lock as the first page (`wiki.go:84-134`, `564-578`).
6. **People's pages are never overwritten**: locked, verified or unparsable pages are not written (`wiki.go:216-254`, `433-465`); an untagged item is taken as hand-written and owned by the user (`validate.go:447-450`).
7. **Speaker invariant (S1)**: only `stated` and `accepted` make a decision; a claim of the user's words needs a cited user prompt with words of the user's own; pasted text never counts; an event or a host prompt is never the user (`validate.go:245-286`, `307-350`, `452-460`). Rules come only from the user's own words (`rules.go:98-153`). What a web fetch read never becomes observed knowledge (`knowledge.go:66-80`).
8. **Freshness (S4)**: a supersession, a rule edit or drop and a knowledge change need the user's prompt at or after the item's stored time (`validate.go:619-621`, `rules.go:185-186`, `knowledge.go:176`, `212-214`, `246-259`).
9. **No id is lost and nothing is silently merged**: missing ids are carried, retired ids stay on a Timeline line, an unrelated replacement becomes a "does X replace Y?" question (`validate.go:650-680`, `915-920`; `internal/update/guarantees.go:82-99`).
10. **Output is treated as hostile text**: redacted again and capped (`validate.go:997-1008`), structure escaped by the renderer, knowledge lines pre-checked against the commit's secret scan (`knowledge.go:398-409`).
11. **Logs hold counts, names and reasons, never text** (`runlog.go:69-70`, `validate.go:17-18`, `pass.go:662-663`).
12. **Failure classes are told apart**: host-level failures hold or back off and never count against a session; a missing CLI backs off; a person's run overrides and, when it succeeds, clears holds and quarantines (`runlog.go:48-59`, `pass.go:316-335`, `341-343`).
13. **Program-run chats cost no call** (`pass.go:168-171`), excluded projects are left alone (`pass.go:258-260`), and pre-update chats are not worked through without the user choosing it (`job.go:382-399`).
14. **The dry run is side-effect free and byte-stable** (`dryrun.go:24-30`).
15. **The wiki's schema and instance are checked before any write** (`wiki.go:35-55`).
16. **A prompt waits for its reply, and a cut group is continued**, so a tail of replies is never judged as a group of its own (`backlog.go:175-195`, `220-246`; `pass.go:141`).
17. **`Validate` is pure and deterministic, and `Guarantees` can check any validated pass** (`validate.go:76-91`, `guarantees.go:13-23`). These are what make the update testable offline.
18. **The sweep's day is the hook's day**, so a time-zone difference between hook and job can't make the sweep run twice or never (`job.go:283-303`, `320-347`).

---

## 3. The validator weighed against a real model's output

### 3.1 What rejects a whole answer

Three things reject the whole pass as `invalid_output` (`pass.go:400-403`):

1. **A strict parse failure** (`schema.go:253-307`): a missing or extra key, a wrong type, an enum outside its list, an array outside its bounds, including a Timeline, Done, rule or knowledge line with no ref or more than 5 (`schema.go:50`, `84`, `101`, `121`, `324`). The same schema goes to the host (`claude.go:41`), so this should be rare when the host enforces array bounds. Inferred, not measured.
2. **More than 3 lines dropped for a date no cited entry holds** (`validate.go:65`, `922-923`). A date is any `YYYY-MM-DD`, `D Month` or `Month D` (`tokens.go:231-279`), and the entry's own header date doesn't count (`validate.go:865-884`). A project whose text is full of dated file names (eva-md's own run logs are `<YYYY-MM-DD>.jsonl`) is exposed whenever the model cites a different entry than the one that names the file. Inferred.
3. **More than half of the page's ids with no action** (`validate.go:924-925`). The model must return one operation, with all eight fields, for every Now id, up to 60 and more (`schema.go:62-71`, `threadpage/page.go:104-106`). On a page with one id, one omission rejects the answer: `2*1 > 1`. Carrying the item forward is what the code does for a missing id anyway (`validate.go:915-920`), so the rejection throws away a pass's Timeline, Done, rules and knowledge over bookkeeping.

A rejected answer is retried blind: the next trigger sends the same batch with the same rules and schema (`pass.go:541-565`), and the reason for the rejection is neither logged nor recorded: `checkOutput`'s error is dropped at `pass.go:401-403`, and `failed` clears the validation report (`pass.go:602`).

My estimate, inferred: a few percent of honest answers rejected whole, more on small pages, on long pages at lower effort, and in projects with dated names. The honest answer is that nobody knows.

### 3.2 What is dropped line by line, with the pass still succeeding

This is where most useful output would be lost, and the loss is final, because the watermark moves on (`pass.go:449`).

- **Standing rules.** One cited prompt's own words must hold every content word of the rule as a substring (`rules.go:268-274`, `283-299`: `strings.Contains(hay, w)`), in the same polarity (`rules.go:309-329`). "Always run the tests with -race before pushing" fails against "before you push" because "pushing" is not in the prompt; "race detector" fails on "detector". Only a near-verbatim copy passes. A refusal is counted in `rules.refused` (`rules.go:137-138`) and the rule is never offered again. Listed in `known-limitations.md:214`.
- **Decision and constraint knowledge.** It must be tagged stated or accepted (`knowledge.go:72-80`) and at least half its distinctive words must be in the user's own cited words, never the accepted proposal's (`knowledge.go:128`, `261-267`). In agentic coding most decisions are the assistant's proposal and the user's "yes, do that"; those are all refused as knowledge. Listed in `known-limitations.md:149`.
- **Any knowledge** with a code-like token or a number with a unit that the batch doesn't hold verbatim (`knowledge.go:134`, `306-315`), any past the fifth (`knowledge.go:112-115`), any in a batch older than the page (`knowledge.go:92-95`).
- **Thread-page lines**: mostly mended, not dropped. Tags fall to inferred, decisions without the user's tag become questions (`validate.go:452-460`), duplicates and date lines are dropped. This part is lenient: `stated` needs one shared content word (`validate.go:393-407`).

My estimate, inferred: thread-page lines lose little; rule adds lose a large minority to a majority whenever the model tidies the user's wording; decision and constraint knowledge is refused in most real cases. The run log counts each of these (`validate.go:19-38`), and nothing reads the counts (section 4, finding 2).

### 3.3 What happens to a chat whose update keeps failing

- Each failure leaves the watermark (`pass.go:597-608`) and the drain of that file stops.
- Three session failures in a row quarantine the session for the day (`runlog.go:385-403`); each of its triggers then spawns a job that exits (`pass.go:67-71`).
- On a later day it gets one retry. If that fails for a session reason, the watermark moves past the batch as `skipped (quarantined)` (`pass.go:604-606`). That batch never reaches a page. The skip resets the row (`runlog.go:398-399`), so the next batch starts a fresh count.
- If the run log's two-day window has passed, the count starts fresh instead (`runlog.go:114-124`).
- The user sees a line at chat start telling them to run `eva-md doctor` (`internal/health/background.go:417-431`).
- A failure whose cause is the host, not the batch, but which is classed as a session failure (`timeout`, `tool_used`, `invalid_output` from a changed CLI envelope) walks every session through this: one batch discarded per session per day while the cause lasts.

### 3.4 How long after a chat ends its memory is available

- **A chat that ends cleanly (SessionEnd)**: spawn in milliseconds, then one pass per 60 KB. A pass is one headless run of the chat's own model at the highest effort with a 600 s limit. Inferred: one to three minutes for a short chat, up to ten per pass.
- **The next chat started straight away** (or `/clear`): its brief is built before the job ends, from the old page and the raw tail; the new Now arrives as a refresh on a later prompt (`internal/hook/resume.go:130-139`). Rules and knowledge from the previous chat are not on file for the first turns.
- **A chat that never ends** (a desktop chat, a terminal left open): nothing fires until 60 KB is unprocessed, a prompt follows a 30-minute gap, a compaction, or another chat starts in that project and finds it idle 2 minutes (`trigger.go:18-24`). A standing rule said in chat A is not on file for a parallel chat B until one of those.
- **A trigger that found the lock busy**: dropped (`job.go:373-377`). The chat waits for the next chat start in its project or the day's sweep, which runs once a day, takes 5 sessions and is claimed before any work is done (`job.go:166`).
- **A short chat with no SessionEnd** (under one exchange or 600 characters): waits for ever (`pass.go:176-178`, `backlog.go:206-218`).

---

## 4. Findings, ranked by consequence

### F1. Refusals, skips and quarantines are final: the watermark moves past content that never reached memory (critical; reliability-safety; rebuild; verified in code; not listed)

- A validated pass moves the watermark past the whole batch whatever the validator refused inside it (`pass.go:449`): refused rules (`rules.go:137-138`), refused or over-cap knowledge (`knowledge.go:112-115`, `128-135`), date-dropped lines (`validate.go:749-751`).
- Skips move it with no call: `locked`, `verified`, `damaged` (`pass.go:196-199`, `612-623`), `quarantined` (`pass.go:604-606`), `small` (`pass.go:172-175`).
- The doctor reports failures, quarantines and bad watermarks (`background.go:89-138`); it has no row for content skipped this way.
- A host-level cause classed as a session failure discards one batch per session per day (section 3.3). `known-limitations.md:185` lists the misclassification, not the loss.
- **Cost**: silent, permanent holes in memory, exactly when something is wrong.
- **Direction**: a per-session ledger of ranges with an outcome each (done, refused items, dead-lettered with reason and rules version). Nothing is dropped; dead-lettered ranges are retried on a new rules version, binary or model, and a failure seen across several sessions trips a host-level breaker instead of advancing any session.

### F2. The validator's cost has never been measured, and the job throws away the evidence (critical; evaluation; add; verified in code; "no live update yet" is listed, the rest is not)

- No live update has run (`known-limitations.md:176`); the live accuracy run covers 12 thread-page scripts, never rules or knowledge (`known-limitations.md:177`).
- The validation report is written to the run log (`pass.go:404`, `runlog.go:93`) and read by nothing: no reference outside `internal/update` (grep for `.Validation`).
- The reason an answer was rejected is dropped (`pass.go:401-403`, `602`).
- `boundary_to_saved_ms` is written (`pass.go:445-447`) and read nowhere.
- A trigger that found the lock busy leaves one log line with no session (`job.go:375`).
- The doctor's backlog row passes whatever the backlog's size or age (`internal/health/status.go:116-128`).
- **Cost**: the central trade-off, strictness against memory kept, is tuned blind; the team can't tell a working system from one that keeps 20% of what users say.
- **Direction**: build the measurement first. Record the rejection rule and place (never text); aggregate per-rule loss, time from last entry to saved page, and dropped triggers into a local report; replay recorded real batches against validator versions; set targets and gate changes on them.

### F3. Whether memory is the user's is decided by word overlap, which refuses most natural phrasing (high; memory-quality; rebuild; verified in code; listed)

- Rule: every content word in one prompt as a substring, same polarity (`rules.go:268-329`). Knowledge under the user's tag: half the words, proposals excluded (`knowledge.go:261-267`). Scope "all projects" from a fixed phrase list (`rules.go:37-41`), narrowing from another (`knowledge.go:337-342`).
- About 1,860 lines (`validate.go`, `rules.go`, `knowledge.go`) and `tokens.go` exist to test, after the fact, whether text the model wrote is close enough to text the user wrote.
- **Cost**: "from now on never do X" and "yes, use that design" are the two things a user most expects a memory to keep, and both are the likeliest to be refused.
- **Direction**: make provenance structural. The model returns spans (entry number and offsets) and code copies the user's words; an accepted proposal is a first-class record holding the proposal span and the assent span. Overlap heuristics then disappear, and the same check works in any language.

### F4. Scheduling is spawn-and-forget under one global lock, with no queue and no timer (high; architecture; rebuild; verified in code; mostly listed)

- A busy lock ends the job and loses the trigger (`job.go:373-377`); the running job never looks again for work that arrived (`job.go:400-406`).
- All triggers are hook events (section 1.2). The day's sweep (`job.go:132-347`, added 2026-11-07, +206 lines) is a patch for what they miss: once a day, 5 sessions, needs a person's chat start, and is claimed before any work is done (`job.go:166`), so a job stopped by the budget or a backoff spends the day's sweep.
- One lock serves both hosts and every project while a pass can take 10 minutes (`runner.go:62-65`).
- **Cost**: with several chats open, the last trigger of a chat (its end) is often the one dropped; that chat's memory waits for the next chat start in its project or a later day.
- **Direction**: triggers become wake-ups. One worker drains until no ripe session is left (ended, idle, or large backlog), re-listing before it releases the lock; a timer (a launchd agent or a self-rescheduling job) wakes it without a chat; per-project fairness and the budget are applied by the worker, not by which hook fired.

### F5. Memory is late and costly by construction: one slow path for everything (high; invisibility; evolve; code facts verified, durations inferred; partly listed)

- Every pass is the chat's own model at the highest effort (`pass.go:201`, `config.go:185-197`) with a 600 s limit, up to 40 a day per host (`config.go:34`), on the user's subscription; the sweep shares the first chat's rate limits (`known-limitations.md:180`).
- Nothing lands before a trigger (section 3.4), and the update is the only writer of rules and knowledge (`pass.go:472`, `wiki.go:506`; the MCP server is read-only).
- **Cost**: the next chat regularly starts before the last one is remembered; a rule said now is not in force in a parallel chat; the user pays top-tier quota for bookkeeping.
- **Direction**: two tiers. A cheap, fast pass shortly after a turn settles captures explicit instructions and decisions; a slower consolidation pass does summaries and knowledge. Add a direct, attributed write path for "remember this". Choose model and effort by task, and adapt on timeouts.

### F6. One all-or-nothing verdict and a full-state protocol (high; memory-quality; rebuild; verified in code; not listed)

- More than half the ids unacted, or more than 3 date drops, rejects everything (`validate.go:915-928`), including valid rules and knowledge checked just before (`validate.go:107-115`).
- Every pass must restate every id (`schema.go:21-31` rule "items", `62-71`), though code carries a missing id forward (`validate.go:916-920`).
- Retries are blind and identical (`pass.go:541-565`); a timeout doesn't lower the effort or shrink the batch (`runlog.go:405-435` lowers it only on a refusal).
- **Cost**: output tokens spent on "keep" lines; passes lost to bookkeeping; a deterministic disagreement between model and validator repeats until the batch is discarded (F1).
- **Direction**: a delta protocol (only changes; silence means keep), acceptance per section, and one repair round that gives the model the validator's findings by rule and place.

### F7. Rules and knowledge depend on the thread page's state (high; memory-quality; rebuild; verified in code; the guard is listed, the lock and damage cases are not)

- A session older than the page changes no rule and no knowledge, even an add that conflicts with nothing (`rules.go:109-113`, `knowledge.go:91-95`, `validate.go:101-104`; listed at `known-limitations.md:172` and `217`). With two chats on one branch this is the normal case for the one processed second.
- A locked, verified or damaged thread page skips the pass before any call, so no rule or knowledge is learned from any chat on that branch (`pass.go:196-199`), and the watermark moves on.
- Rules and knowledge already carry per-item times and freshness checks (`rules.go:185-186`, `knowledge.go:176`).
- **Cost**: an explicit instruction is lost because of which chat's update ran first, or because a page was pinned.
- **Direction**: separate the three products. The Now is ordered state and keeps its guard; the Timeline is an append-only log; rules and knowledge are judged item by item against their own times, in their own step with its own outcome.

### F8. Thread pages never end, and nothing consolidates (medium; memory-quality; add; verified in code; only the Timeline's growth is listed)

- Every save stamps `state: active` (`wiki.go:313`); there is no code for a merged or deleted branch (no hit for a merge check in `internal/update`, `internal/resume`, `internal/threadpage`).
- The Timeline is never trimmed (`known-limitations.md:207`); each pass parses the whole page and `refreshArea` reads and parses every thread page of the area (`wiki.go:605-609`, `653-680`).
- Promotion happens only from the batch in hand (`known-limitations.md:148`); the daily job rebuilds derived files only (`cmd/eva-md/knowledge.go:17-27`).
- **Cost**: decisions made on a feature branch stay on that branch's page after the merge; per-pass work grows with the wiki's age.
- **Direction**: a lifecycle (active, merged, archived) that folds a merged branch's decisions into its parent, and a periodic consolidation pass that promotes recurring Timeline facts, merges duplicates and compacts old Timeline groups.

### F9. State is spread over five places with no transaction between them (medium; architecture; rebuild; verified in code; consequences listed one by one)

- The watermark file (`backlog.go:197-246`), the run log scanned for budget, holds, backoff, quarantine and effort (`runlog.go:107-111`), the sweep file (`job.go:279-347`), the baseline record, and page frontmatter (`wiki.go:294-329`).
- A page saved before a crash or a failed watermark write is processed again by a new model call, repeating Next, done, change and problem lines (`known-limitations.md:202`); the overview and index writes sit between the save and the watermark (`pass.go:444-449`).
- A quarantine is forgotten after two days (`known-limitations.md:196`); a job killed in a call records nothing, so the call is not in the budget (`pass.go:385-390`).
- **Direction**: one small transactional job store (the ledger of F1), and the covered range written in the page's frontmatter with the save, so a rerun can tell that the page already holds the batch and only mend the watermark.

### F10. When something fails, the user is asked to operate the system (medium; invisibility; evolve; verified in code; listed in parts)

- Chat-start notices end "run eva-md doctor" (`background.go:417-431`); the doctor's fixes are commands and hand edits: `eva-md update --pending --foreground`, deleting a watermark file, trimming a Timeline (`background.go:113-131`, `status.go:125`, `known-limitations.md:207`).
- Every Stop past 60 KB spawns a process while a stop lasts (`known-limitations.md:187`).
- **Cost**: the opposite of "it just works".
- **Direction**: the worker heals what it can (retry with a smaller batch or lower effort, rebuild a watermark from the ledger, compact a page) and speaks only when the user alone can act, such as signing in again.

### F11. Reach: English wording, one machine, the hosts' CLIs (medium; reach; evolve; verified in code; not listed)

- Stopwords, negations, contractions, the phrases for all projects and month names are English lists (`tokens.go:34-47`, `75-77`, `231`; `rules.go:40-41`, `303-304`; `knowledge.go:339-342`). A user who writes another language, or whose update is written in English from prompts in another, shares no word with the claim, so `stated` falls to inferred (`validate.go:393-407`) and decisions become "Unattributed" questions; a rule can never be for all projects. Text without spaces between words is one token (`tokens.go:55-72`). Behaviour inferred from the code.
- The wiki must name this machine's instance (`wiki.go:49-52`): a user's second machine can't write the same memory.
- The update needs each host's headless CLI and names failures by matching its error text (`known-limitations.md:185`).
- **Direction**: F3's structural provenance removes the language dependence; a multi-writer wiki needs a merge model (append-only logs and per-item pages merge well; a rewritten Now doesn't).

### F12. The prose the next chat reads first has no provenance (medium; reliability-safety; evolve; verified in code; not listed)

- Summary and last chat are free text checked only for dates, with grounding misses counted and never acted on (`validate.go:110-111`, `849-863`, `886-888`).
- The brief shows the summary. A claim the validator demoted in the items ("Unattributed") can still stand as fact in the summary, and text an assistant reply repeated from a tool or a web page can reach it.
- **Direction**: derive the summary from validated items, or require it to cite and hold it to the same tag rules; show it as the assistant's account.

---

## 5. Where the complexity is essential and where it is accidental

**Essential**: untrusted model output written into files that later steer an agent; attribution of who said what; ordering between chats; crash safety around a slow external call; never blocking a hook; never overwriting a person's edit.

**Accidental**:
- Lexical-overlap provenance and its English special cases (`rules.go:37-96`, `301-362`; `knowledge.go:261-355`; `tokens.go`): it exists because the model writes free text that code must then match to the user's words.
- The trigger zoo and the sweep (`trigger.go`, `job.go:132-347`): they exist because there is no queue and no timer.
- State derived by scanning a two-day log (`runlog.go:328-435`).
- One call, one schema, one verdict for three stores (`schema.go:38-133`, `validate.go:95-118`).
- The full-state "keep" protocol (`validate.go:915-928`).
- Index and overview upkeep in the pass, made necessary by the library counting unlinked pages as orphans (`wiki.go:598-680`, comment at `650-652`).

## 6. Keep, evolve or rebuild

Keep the watermark rule, the save order, compare-and-swap, the page protections, the speaker and freshness invariants, the failure taxonomy, the dry run and the purity of `Validate`. Rebuild the control plane (a worker with a ledger and a timer in place of spawn-and-forget, the sweep and log scans) and the extraction contract (span-based provenance, a delta protocol, per-product acceptance, a repair round). The current code is careful and well tested offline, but it was shaped by tests with canned answers; its two weakest points, lost triggers and lost content, both come from structure, not from bugs.

## 7. Open questions

1. What do real answers from each host lose to each validator rule? Nothing can be tuned before this is measured.
2. Is a timer-woken worker (a launchd agent) acceptable, given that `docs/decisions.md` rules out "resident servers"?
3. May an assistant's proposal the user accepted in a few words become decision knowledge, with both spans recorded?
4. May a cheaper model or lower effort be the default for a fast first pass? Decision 17 sets the highest effort on the chat's model.
5. Which app surfaces fire SessionEnd and Stop? The never-ending-chat path decides how much rests on a timer.
6. Should dead-lettered ranges be reprocessed automatically after an upgrade, within the budget?
7. Is one memory across a user's machines in scope?
8. Do both hosts enforce array bounds of the schema, so that strict-parse rejections are rare?

## 8. What the tests cover and don't

Covered offline, with a fake runner and canned answers: the page write, overview and indexes (`job_test.go:202`), no-change passes (`263`, `293`), schema and instance (`349`), conflicts (`376`), save failures (`411`), watermark failures (`449`), a kill mid-save (`545`), the second job's exit (`637`), protected pages (`698`); the budget, holds, backoff and quarantine (`budget_test.go:16-297`); the sweep (`sweep_test.go:48-417`); the triggers (`internal/hook/trigger_test.go:117-533`); each validator rule (`validate_test.go:43-832`); rules and knowledge (`rules_test.go`, `knowledge_test.go`).

Not covered anywhere: a real model's answer; the rate at which each rule refuses honest output; time from a chat's end to memory on file; a trigger lost to a busy lock and how long its session then waits; a host-level failure classed as a session failure across many sessions; languages other than English; pages with dozens of ids answered by a model at low effort.

## Verification

A second reader tried to refute each finding against the code at the checked-out commit. Nothing was run but read-only commands. Verdicts: 9 confirmed, 3 partly, 0 refuted. No cited line was wrong; the corrections are to scope and severity.

### Verdicts

| Finding | Verdict | Severity (theirs → mine) |
|---|---|---|
| 1. Refused, skipped and quarantined content is lost for good | partly | critical → high |
| 2. Nothing measures what the validator costs | partly | critical → high |
| 3. Attribution by word overlap | confirmed | high |
| 4. Spawn-and-forget triggers | confirmed | high |
| 5. One slow, costly path | confirmed | high |
| 6. All-or-nothing verdict, full-state protocol, blind retries | confirmed | high → medium |
| 7. Rules and knowledge depend on the thread page | confirmed | high |
| 8. Thread pages never end | confirmed | medium |
| 9. Job state in five places | confirmed | medium |
| 10. Failures hand the system to the user | confirmed | medium |
| 11. English word lists, one machine per wiki | partly | medium |
| 12. Summary and last chat carry no provenance | confirmed | medium |

**1, partly.** The mechanics are as claimed: `pass.go:449` advances whatever the validator refused; `pass.go:196-199`, `604-606` and `612-623` skip; `runlog.go:56` counts `timeout`, `tool_used` and `invalid_output` against the session; `hostStop` (`pass.go:319-335`) has no breaker for those reasons; `updateRow` (`health/background.go:89-138`) shows only the newest record's outcome. Two corrections. The content is not lost for good: the inbox keeps the raw chat with no time limit (`docs/design.md:483`, decision 4) and search reads it, so the loss is from pages, rules and knowledge, and the material for a dead-letter retry already exists. It is not fully silent either: each validated pass's record holds the refusal counts (`pass.go:404`, `runlog.go:93`). There is no reprocess path short of deleting a watermark, which reads the session again from its start. The loss per day is bounded by the cap: each skipped batch costs four calls over two days. Parts are listed (`known-limitations.md:171`, `190`; the quarantine skip is `design.md:982`).

**2, partly.** Confirmed: `pass.go:401-403` drops `checkOutput`'s error and the report it returns with a rejection; nothing outside `internal/update` reads `.Validation` or `BoundaryToSavedMS` (grep); `job.go:375` logs no session. Overstated: the job discards only the cause of a whole-answer rejection. Per-rule counts are written for every validated pass, `boundary_to_saved_ms` for each that saves, and nothing prunes the run log (`known-limitations.md:201`), so an aggregate can be built from what is on disk today. A developer's harness also exists: `cmd/live-accuracy` runs real answers through `Validate` and records each refusal with its reason and place (`internal/update/accuracy/live/live.go:105-120`). It has never been run, covers 12 scripted thread-page histories, and sees no rule or knowledge change (`known-limitations.md:177`).

**3, confirmed.** `found` (`rules.go:286-299`) needs every content word of the rule as a substring of one prompt. Worked example: the prompt "from now on, don't ever push directly to main" refuses the rule "Never push directly to main", since "never" is a content token the prompt doesn't hold. `usersWords` (`knowledge.go:267`) reads only the user's own words. Scope note for the redesign: an accepted proposal does become a decision in force on the thread page, through `proposed` and `restsOnProposal` (`validate.go:320-329`, `368-391`, `409-426`); what is refused is its promotion to a knowledge page. That path is a working precedent for the proposal-and-assent record the direction asks for. The rules prompt already tells the model to "keep the user's words" (`schema.go:27`). "1,860 lines" is the whole of three files, which also hold dates, duplicates, id accounting and scrubbing.

**4, confirmed.** `job.go:373-377`, `400-405`; `claimSweep` runs inside `pick` (`job.go:166`), before any drain, so a host the budget or a hold stops spends the day's sweep. No timer exists: `hook/spawn.go:19-36` and `hook/maintain.go:49-64` start jobs only from hook events.

**5, confirmed.** `pass.go:201`, `config.go:187-196`, `runner.go:62-65`, `config.go:34`; `knowledgepage.Save` and `threadpage.ApplyRules` have no caller but the update. Durations are inferred, as the report says. What covers part of the wait: the brief's raw tail of the previous chat and its pending line (`resume/lines.go:15`), and up to two refreshes a chat, which carry changed rules as well as a changed Now (`hook/resume.go:36`, `resume/resume.go:93-96`). The direction reopens decision 5 (the chat's own model), decision 17 (highest effort by default) and sits close to the ruled-out "model call on every turn".

**6, confirmed, medium.** `validate.go:922-925`, `schema.go:71`, and `runModel` (`pass.go:561-563`) sends the same rules, input and schema each time. One more all-or-nothing edge: a Done, Timeline, rule or knowledge line with no ref or with six, a 21st Timeline line or a 9th Done line rejects the whole answer at parse (`schema.go:310`, `324`, `373`, `394`), though `cite` (`validate.go:245-258`) already knows how to drop surplus refs. I rate it medium until measured: how often a real model trips these is unknown, and a retry draws a new answer, so the usual cost is one more call and a delay; it turns into loss only through finding 1.

**7, confirmed.** `rules.go:110-113`, `knowledge.go:92-95`, `validate.go:103`, `pass.go:196-199`. The guard fires when the batch's newest entry is older than the page's `as_of`, so it hits late catch-ups, which are the sessions finding 4 delays, not every chat processed second. `design.md:605` lists the locked-page skip; nothing says rules and knowledge go with it.

**8, confirmed.** `wiki.go:313` writes `state: active` and no code reads a thread's state. `refreshArea` runs only on a pass that wrote a thread or rules page (`pass.go:443-444`, `489-506`), and its cost is small beside a model call. Knowledge pages are project-wide, so a promoted decision survives a merge; Now items don't.

**9, confirmed.** As listed. A cancelled call records nothing (`pass.go:385-390`). Related and not said: a validated answer is never kept, so any failure after the call (a save, the wiki lock, the watermark) pays for a new call and gets a different answer (`known-limitations.md:197-198`).

**10, confirmed.** `background.go:419-429`, `113-131`. Correction: the backlog row fails on a damaged watermark (`status.go:123-126`); it passes whatever the backlog's size or age. The job heals some failures itself: backoff, the next-day retry, effort stepping down after a refusal.

**11, partly.** The lists are English, and unspaced text is one token, so `stated` fails for CJK and for a page written in English from prompts in another language. In a spaced language where the model writes the user's language, the user's tag still holds, and more loosely, since no stopword is filtered; there the polarity check sees no negation, so it protects nothing, and the all-projects wording never matches (a rule from a chat outside a repo still goes to all projects). One wiki per machine is decision 1, checked on purpose at `wiki.go:49-52`; it is a product question, not a defect.

**12, confirmed.** `free` (`validate.go:852-863`) checks dates and counts grounding; no tag or citation applies. The brief shows the text under "Summary" with no tag (`resume/now.go:40-44`). Bounds: 600 characters, redacted, escaped, inside the brief's data fence.

### The three questions

- **Share of honest output rejected.** Unmeasured, and I could not measure it either. From the code: thread-page lines are mostly mended, not dropped; a rule survives only with the user's content words kept; knowledge from an accepted proposal is refused by construction; whole-answer rejection depends on whether the hosts enforce the schema's array bounds, which nothing in the repository shows.
- **A chat whose update keeps failing.** Three session failures quarantine the whole session file for the day, new entries included (`pass.go:67-71`); a later day gives one retry, and its failure moves the watermark past the batch (`pass.go:604-606`). A gap of two days or more starts a fresh count (`runlog.go:114-124`), so slow triggers can cost one to three calls a day without ever reaching the skip.
- **Time to memory.** Minutes after a clean end when the lock is free and the budget allows; otherwise the next chat start in that project once the file has been idle 2 minutes, or the day's sweep after 60 minutes idle.

### Missed

**M1. Backlogs drain oldest first to exhaustion: no priority in the budget or the lock** (architecture; medium; rebuild; verified in code; not listed). Named sessions are drained in listing order, oldest change first (`job.go:161-165`, `551-556`); `drain` loops a file until it is empty (`pass.go:82-91`); the only bound is the host's 40 calls (`pass.go:322-324`); the lock is held for the whole job (`job.go:373-381`). One long idle session a chat start names is drained before newer chats, can spend the day's calls and hold the lock for hours, and fresh chats then get no update that day. Direction: the worker ranks work, the chat just ended and the current project first, newest first, with a share of the budget kept for fresh chats.

**M2. A date the page already holds is treated as invented** (memory-quality; medium; evolve; verified in code; not listed). `dated` reads dates only from the batch's cited entries (`validate.go:869-884`). `edit` (`538-541`) never consults the stored item and `free` (`852-863`) never the stored summary; each drop counts toward the three-drop rejection (`922-923`). A dated item, such as a deadline, can't be edited in a later chat without losing its date; a summary that restates one is dropped and the old one stays. Direction: keep dates as fields with their source ref, carried by code, and ground a written date against the item changed and the page's validated text as well as the batch.

**M3. Every chat outside a git repo shares one thread, and its rules bind all projects** (memory-quality; medium; rebuild; verified in code; not listed). `threadID` gives `general/threads/default` whatever the branch (`wiki.go:77-82`); any folder outside a git repo is general (`identity/identity.go:33`); every rule of such a chat is filed for all projects (`rules.go:132`, `wiki.go:419`). Unrelated work shares one Now of 10 next steps and one summary, the out-of-order guard hits any older chat there, and "never do X here" said in a scratch folder is shown in every project. Direction: give work outside a repo an identity of its own, by folder or by a topic the update assigns, and require the user's words for all-projects scope there too.
