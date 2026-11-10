# Per-prompt recall and pointers: audit

Scope: `internal/hook/recall.go`, `internal/hook/pointers.go`, `internal/knowledgepage/*`, `internal/resume/systems.go`, `internal/resume/knowledge.go`, `internal/install/templates/recall-skill.md`, with the design in `docs/design.md` (Per prompt 1019-1033, Knowledge page 358-403, Fencing 1089-1095) and `docs/known-limitations.md` 133-172.

Everything below was read in the code unless it says "inferred". No test was run and no file in the repository was changed. Numbers marked "arithmetic" were computed from the constants in the code.

Paths are relative to `the repository`.

---

## 1. How it works

### 1.1 Trigger and place in the hook

- The only injection points are `SessionStart` and `UserPromptSubmit`. The plugins register no `PreToolUse` and no `SubagentStart` hook: `internal/install/templates.go:71-87`.
- `hook.Run` gives a prompt hook 300 ms in all (`internal/hook/hook.go:229-235`), less 10 ms kept for writing the output (`hook.go:200`). Before recall runs, the same budget pays for identity resolution (`hook.go:320`), the session lock (`hook.go:361`), transcript recovery of up to 100 ms (`hook.go:433`), the inbox append (`hook.go:578`) and the brief or refresh (`internal/hook/resume.go:64`).
- Recall and pointers run last, inside `resumeContext` -> `nextState` -> `promptLines` (`internal/hook/resume.go:68`, `internal/hook/pointers.go:95-113`, `pointers.go:194-210`).
- Gates that give a prompt nothing at all:
  - eva-md's own inner run and Claude Code cloud sessions: `hook.go:281-286` (`EVA_MD_INNER`, `CLAUDE_CODE_REMOTE`).
  - excluded folders, the wiki, the state folder, temporary folders: `hook.go:308-315`.
  - off the record, or transcript recovery not caught up: `hook.go:494-512`.
  - empty prompt or built-in command: `hook.go:515-517`, list at `hook.go:740-750`.
  - a prompt the user did not type (any `agent_id`, scheduled/loop/subagent sources, `<task-notification>` and similar prefixes) and headless chats (`sdk-cli`, `sdk-ts`, `sdk-py`): `hook.go:518-521`, `hook.go:679-694`, `hook.go:732-738`, and again `internal/hook/resume.go:60`.
  - `recall = false` (recall only), no wiki, wiki of another instance: `internal/hook/recall.go:93`, `recall.go:102`, `pointers.go:243`, `pointers.go:255`, `pointers.go:312-318`.

### 1.2 What one prompt can get

`promptLines` (`pointers.go:194-210`) builds, in order:

1. **System pointers** (`pointers.go:241-281`): the prompt is matched against `.eva-md/names.tsv` (titles, slugs, aliases; at most 2,000 names or 128 KB, `internal/systempage/names.go:30-33`). At most 3 a prompt and 12 a chat (`pointers.go:37-38`), each page once a chat, never a page the brief's `System:` lines named (`pointers.go:166`, `pointers.go:267`), never a page whose file is gone (`pointers.go:322-325`). A pointer is one line of at most 160 characters: `eva.md: <title>: <description> (<code note>): <bin> show systems/<slug>` (`pointers.go:380-409`). The description comes from `systems/map.md` and the code note from `<state>/code-status.json`, both read only when a pointer goes out (`pointers.go:270-272`, `pointers.go:330-356`). No git runs.
2. **Recalled knowledge** (`internal/hook/recall.go:91-117`): reads `.eva-md/knowledge.tsv` whole, scores every row in memory, fences the best rows. At most 3 rows (`internal/knowledgepage/recall.go:16`) in at most 1,000 characters with label and fence (`recall.go:31`, `recall.go:138-163`). Each page once a chat and never one the brief listed as a `Constraint:` line (`recall.go:107-109`, `pointers.go:170`).
3. **Reminder**, only when neither of the above went out and the prompt carries no brief: on prompt 1 and then every `reminder_every` prompts, default 5 (`pointers.go:204-208`, `pointers.go:214`, `internal/config/config.go:38`). One line of at most 100 characters such as `eva.md: earlier chats: <bin> search <terms>; systems: show systems/index` (`pointers.go:221-234`).

These lines are appended after any brief or refresh, two newlines apart (`internal/hook/resume.go:80-86`), and go out as `hookSpecificOutput.additionalContext` (`internal/hook/resume.go:95-105`).

### 1.3 Pointer matching

- `scanned`: the whole prompt, or the first and last 32 KB of a long one (`pointers.go:438-443`).
- `words`: letters and digits, joined by `-`, `_`, `.` when a letter or digit follows (`pointers.go:451-476`).
- `named`/`matches` (`pointers.go:502-547`): a several-word name matches whole words ignoring case with the same text between them. A one-word name needs 5 or more characters (`minWord`, `pointers.go:42`), must not be on a stoplist of about 105 words (`pointers.go:480-493`), and matches in any case only when "distinctive" (inner capital, digit, `-`, `.`, `_`; `pointers.go:552-559`), otherwise only in the exact case the index lists.

### 1.4 Recall scoring (`internal/knowledgepage/recall.go`)

- **Terms**: the prompt's distinct runs of letters and digits, lowercased, 3 or more characters, not on a stoplist of about 260 words (`recall.go:175-196`, `recall.go:249-271`). A prompt over 64 KB is read by its first and last 32 KB (`recall.go:176-179`); of more than 64 terms the first 32 and last 32 count (`recall.go:188-190`). Fewer than 2 terms recalls nothing (`recall.go:73`).
- **Match**: a row holds a term when its title or statement holds that exact token (`recall.go:88-115`). No stemming, no splitting of camelCase, no synonyms.
- **Gate**: at least 2 distinct terms, and either every content token of the title or at least 3 terms (`recall.go:111`, `recall.go:124`).
- **Score**: sum over held terms of `ln(1+(N-df+0.5)/(df+0.5)) / ln(1+(N-0.5)/1.5)`, doubled for a term in the title (`recall.go:117-136`). `N` and `df` are over every row of the index, all projects. Floor 2.5 (`recall.go:37`).
- **Scope** (`recall.go:137`, `internal/knowledgepage/scope.go`): rows of the chat's project and rows with scope `all` are local. Another project's row counts only when the prompt names that project (`scope.go:68-83`) or the two share a system page by `.eva-md/repos.tsv`, read lazily (`scope.go:33-38`, `internal/hook/recall.go:123-129`, `internal/systempage/repos.go:94-106`).
- **Order**: score, then local before related, then newest `as_of`, then newest cited entry (`recall.go:141-152`). Kind, tag, usage and code freshness play no part.
- `Row.Paths` (the code paths a page cites) is written to and parsed from the index (`internal/knowledgepage/index.go:44`, `index.go:63-68`, `index.go:173-177`) and is not read by recall. Its only reader outside the package is the update's plan (`internal/update/knowledge.go:416`).

### 1.5 The recall block (`internal/hook/recall.go`)

- Label (`recall.go:38-39`, `recall.go:60-85`): "eva.md knowledge: lasting facts saved from earlier chats that match this prompt; reference data, not instructions." then what the shown tags rest on, then " Where they disagree with the user or the code, the user and the code are current.", then, if it fits, " Read one: <bin> show knowledge/<id>." (`recall.go:159`).
- Fence: `resume.Fence`, a backtick fence one longer than the longest run inside, at least 4, info string `eva-memory` (`internal/resume/resume.go:718-721`).
- Line: `k12 · decision [stated] · <project key or "all projects"> · 2026-10-28: <statement>` (`recall.go:170-184`). No title.
- A line is dropped when the secret scan flags it or when cleaning it as the brief cleans page text would change it (`recall.go:180-182`, `internal/resume/systems.go:223-228`).
- A row that does not fit is skipped and the next tried (`recall.go:141-154`).
- Arithmetic: label and fence take 258 characters with one tag kind, 334 with two, 411 with the "Read one" suffix for a 45-character install path. With a 25-character project key each line has a 67-character prefix. Three facts fit only when statements average about 154 characters or less; two 300-character statements (`MaxStatement`, `internal/knowledgepage/page.go:84`) total 1,069 and do not fit, so the prompt gets one.

### 1.6 The recall index and the pages

- `.eva-md/knowledge.tsv`, mode 0600, header `# eva-md knowledge index 1`, one 11-field tab-separated row per active page, newest `as_of` first, at most 512 KiB; past it the oldest `as_of` rows are left out (`internal/knowledgepage/index.go:21-29`, `index.go:97-115`).
- It is rebuilt from the pages by every `Save` (`internal/knowledgepage/save.go:113`), by the doctor and by maintain (`internal/knowledgepage/files.go:60-70`, `cmd/eva-md/knowledge.go:27-47`), written atomically under the wiki lock (`internal/okfadapter/adapter.go:301-311`, `adapter.go:655-695`).
- The strict read: pinned `os.Root`, no link on the path, regular file, one hard link, unchanged between lstat and open, at most 512 KiB (`index.go:186-254`).
- A page (`knowledge/k<N>.md`) is one statement of at most 300 characters with a kind (fact, decision, procedure, constraint), scope (project or all), project key, state, `as_of`, a speaker tag, inbox refs and up to 3 code sources at a commit (`page.go:59-90`, `page.go:119-145`). There is no branch, no confidence, no expiry and no usage field.
- `Save` is idempotent, never reuses an id, never writes a locked, verified, damaged or other-type page, keeps a person's sections, and refuses an add whose normalized statement an applying active page already holds (`save.go:86-119`, `save.go:163-186`, `save.go:295-304`).

### 1.7 The brief's share

- `constraintsBlock`: at most 3 constraints that hold for the project (its own and scope `all`), newest `as_of` first, 600 characters together, from the recall index alone (`internal/resume/knowledge.go:25-27`, `knowledge.go:43-66`, `internal/knowledgepage/recall.go:159-168`).
- Constraints are not in a compact brief (`internal/resume/resume.go:321-324`).
- `systemPointers`: up to 3 systems whose repos include the project, those citing its code first, then a line for the system index, 600 characters together, with a live count of commits since the cited commit within 150 ms (`internal/resume/systems.go:27-48`, `systems.go:59-108`, `systems.go:119-146`).

### 1.8 State written

- One file per chat: the session's `prompts` marker, JSON `{prompts, pointers, recalled, pages}` (`pointers.go:54-59`), written in place without fsync under the session lock, refusing a symlink or a second hard link (`pointers.go:120-161`).
- Order: the briefed marker is saved first, then this state, and the lines are dropped if either save fails (`internal/hook/resume.go:74-79`).
- A startup or clear brief resets the state; compact, resume and fork keep it (`pointers.go:105-107`, `internal/hook/resume.go:178`).
- `Recalled` and `Pointers` are counters nothing else reads: the only reads are `empty()` and the per-chat pointer cap (`pointers.go:63`, `pointers.go:246`; grep over `internal` and `cmd` finds no other reader).

### 1.9 The pull path

- The recall skill (`internal/install/templates/recall-skill.md:10-20`) tells the agent to run `<bin> search <terms>` and `<bin> show <ref>` before re-deciding, before changing code (searching the file's path), and when the user refers to earlier work.
- The MCP server offers `eva_search`, `eva_show` and `eva_status` (`internal/mcp/tools.go:83-104`). It has no ranked recall tool: `knowledgepage.Recall` is called only from the hook (`internal/hook/recall.go:105`).

---

## 2. What must be kept

1. **Hooks stay offline, bounded and fail closed.** Recall and pointers read local files only, each with a size bound, run no git and no model, and return nothing on any error (`internal/hook/recall.go:91-104`, `pointers.go:241-256`, `pointers.go:294-308`). Measured cost about 3 ms on a full index (`docs/verification.md:110`); the benchmark holds a recalling prompt under 50 ms p95 (`scripts/benchmark.py:373-374`).
2. **The strict read of anything injected.** Pinned root, no link on the path, regular file, single hard link, size bound, unchanged between lstat and open (`internal/knowledgepage/index.go:216-254`), and a rebuild that replaces an index the readers would refuse (`internal/knowledgepage/files.go:327-355`).
3. **Atomic index writes under the wiki lock, and the index as a rebuildable cache** (`internal/okfadapter/adapter.go:655-695`, `files.go:60-70`).
4. **One fence, one label style.** A backtick fence longer than any run inside; a label worded as plain facts, not commands; "the user and the code are current" (`internal/resume/resume.go:718-721`, `internal/hook/recall.go:38-39`, `docs/design.md:1091-1093`).
5. **A line that cannot be shown as it is, is dropped, never altered.** Secret scan plus the brief's own cleaning check on every recall and pointer line (`recall.go:180-182`, `pointers.go:403-407`, `internal/resume/systems.go:223-228`).
6. **Provenance reaches the reader.** Each line carries its tag, and the label says what the tags shown rest on (`recall.go:45-85`). Decisions and constraints are promoted only from the user's words; a suggestion or an inference is never promoted (`internal/update/knowledge.go:72-80`).
7. **Scope is closed by default.** Own project and scope `all` only; scope `all` only on the user's own words (`internal/update/knowledge.go:323-335`); another owner's project only by its whole owner path; a key that hides its name cannot be named (`internal/knowledgepage/scope.go:68-104`).
8. **Precision first.** A weak match adds nothing (`recall.go:37`, `recall.go:124`, `recall.go:137`); the score does not grow with the size of the index (`recall.go:116-118`).
9. **No repeats.** A page once a chat, never one the brief already listed, reset by a startup or clear brief (`recall.go:107-109`, `pointers.go:105-108`, `pointers.go:166-187`).
10. **State before delivery, on the soft path.** The state is saved before the lines go, and the lines are dropped when the save fails, so nothing repeats without bound; the state file is never written through a link (`internal/hook/resume.go:74-79`, `pointers.go:141-154`).
11. **Gates for privacy.** Nothing off the record or while transcript recovery is behind; nothing from a wiki of another instance (`hook.go:494-512`, `pointers.go:312-318`).
12. **Hard caps on what a prompt can cost.** 3 facts in 1,000 characters, 3 pointers of 160, a 100-character reminder; 64 terms and 64 KB of prompt read (`recall.go:31`, `pointers.go:36-45`, `internal/knowledgepage/recall.go:14-25`).
13. **Page safety in `knowledgepage.Save`.** Idempotent, ids never reused, locked/verified/damaged pages never written but still listed read-only, a person's sections and keys kept, free text escaped so it cannot forge a heading, fence, tag or frontmatter key (`save.go:86-119`, `page.go:171-195`, `page.go:572-581`).
14. **A pointer never names a page that is gone, and a code note shows only when it was measured at the commits the map line cites** (`pointers.go:322-325`, `pointers.go:364-370`).

---

## 3. Where it falls short

Ranked by consequence for the goal.

### F1. Recall is keyed to the words of a typed prompt and nothing else (critical; recall-quality; verified)

- The only inputs are the prompt's text and the project key: `promptLines(ctx, env, project, p.Prompt, ...)` (`pointers.go:112`, `pointers.go:194-198`), `promptTerms(prompt)` (`internal/knowledgepage/recall.go:72`).
- A prompt with fewer than 2 content terms recalls nothing (`recall.go:73`): "yes", "continue", "do it", "fix that".
- There is no hook before a tool call (`internal/install/templates.go:71-87`), so nothing surfaces when the agent is about to edit a file or run a command that a constraint or decision covers.
- The code paths a fact cites are in the index and unused by recall (`index.go:44`; only `internal/update/knowledge.go:416` reads `Row.Paths`). A prompt that names a file recalls a fact about that file only when the path's words happen to be in the fact's title or statement.
- The brief's thread, the branch, the files the chat has touched and the previous turns are not inputs.
- The recall skill asks the agent to search the file's path before changing code (`recall-skill.md:13`). That is a manual tool call the model must choose to make for every edit, and nothing measures whether it does.

Cost: in agentic work one prompt starts many tool calls. The moment a constraint matters is the moment of action, and nothing reaches the agent then. This is the largest gap between "the agent remembers" and what is built.

Direction: a single retrieval step driven by events, not by prompt words alone. Its query is the prompt plus the paths and commands in play, the branch and the thread. It runs at the prompt and before file edits and commands, and uses the cited paths as a first-class match. Rebuild.

### F2. Matching is exact-word overlap with thresholds tuned on a fixture (high; recall-quality; known in part; verified)

- Exact tokens only (`recall.go:88-104`, `recall.go:213-243`): "dependency" does not match "dependencies"; `LedgerSync` is one token; a paraphrase matches nothing. Listed at `docs/known-limitations.md:155`.
- The gate needs the whole title or 3 terms (`recall.go:124`).
- The weights depend on the mix of the whole wiki. Arithmetic from `recall.go:117-136`: in 200 rows, a term 5 rows hold weighs 0.735, so three such terms in a statement score 2.2 and miss the 2.5 floor; a term 40 rows hold weighs 0.33. As a wiki grows inside one domain, that domain's words lose weight and the same prompt stops recalling the same fact. In a wiki of one row every term weighs 1.
- `df` and `N` are counted over every project's rows (`recall.go:86-118`), so another project's vocabulary shifts this project's scores.
- There is no length normalization: a pasted log or file gives 64 terms (`recall.go:188-190`) and any row that shares 3 with enough weight is recalled. Inferred: false positives rise with pasted content in a large single-domain index; the fixture has three long prompts, none of code from the same domain (`recall_test.go:186-188`).

Direction: keep the hook lexical and offline, but match over fields written for retrieval. At promotion the update model writes each fact's cues (other words for it, the question it answers, when it applies, paths, identifiers); the scorer normalizes tokens (stems, identifier parts) and weights fields. If an on-device embedding index is acceptable, add it as a second signal built in the background. Rebuild the scorer; evolve the index format.

### F3. A constraint is shown once at chat start or by word overlap, then suppressed (high; recall-quality; parts known; verified)

- The brief lists the 3 newest constraints (`internal/resume/knowledge.go:25`, `knowledge.go:48-53`). Older ones depend on prompt overlap (`docs/known-limitations.md:163`).
- A constraint the brief listed is recorded in the chat's pages (`pointers.go:170`, `pointers.go:181-185`) and recall skips it for the rest of the chat (`internal/hook/recall.go:107-109`).
- A compact brief carries no constraints (`internal/resume/resume.go:321-324`), and a compact brief does not reset the state (`pointers.go:105-107`; `fresh` is startup or clear only, `internal/hook/resume.go:178`).
- So after a compaction a constraint is not in the new brief and cannot be recalled, and neither can any fact recalled before the compaction (`docs/known-limitations.md:161` lists the once-a-chat part).

Cost: in a long chat, the rules that must hold are the first thing the context loses and the one thing the system refuses to send again.

Direction: track what is in the context now, not what was ever sent. Reset that on compaction. Choose constraints by what the chat is doing (paths, commands, topic), not the newest three, and re-send a relevant one when it has left the context. Rebuild the state model.

### F4. Recalled knowledge has no validity signal (high; memory-quality; verified)

- A recalled line shows `as_of` and nothing else about freshness (`internal/hook/recall.go:179`).
- System pointers say how far the cited code moved (`pointers.go:364-370`). Knowledge pages cite code at a commit (`page.go:133-135`, `save.go:427-444`), but `internal/coderef` and `cmd/eva-md/code.go` never measure them (grep finds no knowledge handling there), and recall does not read the paths.
- A page has a project but no branch (`page.go:119-145`, `index.go:38-45`). A decision made in a chat on a branch that was later abandoned is a project-wide fact, recalled on every branch.
- A fact changes only when a later batch's user words supersede or drop it (`internal/update/knowledge.go:151-157`). Nothing ages, re-checks or retires an `[observed]` fact about code that has since changed.
- Two active facts that disagree are both recalled; a project's own fact does not outrank a scope-`all` one (`recall.go:137`, `recall.go:141-152`).

Direction: give each fact a validity record: where it applies (project, branch, paths), what it rests on, when it was last confirmed, how far its cited code has moved. Measure it in the daily job, use it in ranking, show it in a few words, and propose retirement when it fails. Add.

### F5. Recall quality has never been measured on real knowledge (high; evaluation; verified)

- The floor was "tuned on fixtures, not on real use" (`recall.go:36`).
- The fixture is 36 hand-written rows and 73 hand-written prompts in one file, the prompts written against the rows and sharing their words (`recall_test.go:15-61`, `recall_test.go:123-203`). The same prompts set the floor and report its precision and recall; there is no held-out set.
- The 500-row case is the 36 rows plus 464 rows of made-up words no prompt holds (`recall_test.go:225-246`). That raises `N` without raising `df`, which makes every real term weigh more (0.74 at 36 rows becomes 0.85 at 500 for a term 3 rows hold, arithmetic). It does not model a large wiki of one domain.
- The test fails only below 90% precision or 60% recall (`recall_test.go:320`).
- `BenchmarkRecall` repeats the same 36 rows (`recall_test.go:646-661`).
- No update has run live, and prompt recall, pointers and the recall skill are on the "not verified yet" list (`docs/verification.md:3`). So recall has never scored a statement a model wrote.
- At run time nothing records a recall decision. `Recalled` is written and never read (section 1.8). There is no count of prompts scored, rows over the floor, rows dropped for size or cleaning, or runs lost to the deadline.
- Nothing measures the outcome that matters: whether the agent, given the fact, acted on it.

Direction: an evaluation harness. Offline: replay real prompts and tool events from the inbox against a snapshot of the wiki, with relevance judged by a model and spot-checked by a person, split into tuning and held-out sets. Online: a local log of each decision (ids, scores, reasons, never text). End to end: scripted tasks where a stored constraint must change what the agent does. Add.

### F6. Whole classes of chats get no recall (high; reach; known in part; verified)

- Subagents (any `agent_id`), scheduled and loop prompts, and headless or SDK runs: `hook.go:518-521`, `hook.go:679-694`, `hook.go:732-738`. `docs/known-limitations.md:141` lists pointers and the reminder; recall is gated the same way (`docs/design.md:1030`).
- Claude Code cloud sessions: `hook.go:284-286`.
- Hosts and surfaces without hooks have only the MCP tools, which are exact-term search and page reads (`internal/mcp/tools.go:83-104`). The ranked, scoped recall is not offered there.
- The privacy reason to not capture a program's chat does not apply to reading memory into it.

Cost: the autonomous workers that most need the constraints (subagents doing the edits, SDK-driven agents) work without them.

Direction: split reading from capturing. Offer the same retrieval as an MCP tool and resource, inject at subagent start where the host allows, and decide per kind of chat whether read-only recall is on. Add.

### F7. What is injected carries a lot that is not memory (medium; invisibility; verified, the permission cost inferred)

- Label and fence take 258 to 411 of the 1,000 characters on every recalling prompt (section 1.5). With long statements a prompt gets one fact, not three.
- Each line spends about 25 characters on a project key such as `github.com__acme__billing` (`recall.go:171-179`; listed at `docs/known-limitations.md:162`).
- The reminder rides on a chat's first prompt, right after the SessionStart brief whose footer says the same, and then every 5 prompts (`pointers.go:204-214`; `internal/hook/pointers_test.go:296`). A due reminder that a pointer displaces is not deferred, it is skipped.
- Pointers and the reminder give the agent a shell command at an absolute path to run (`pointers.go:222-226`, `pointers.go:385`). Following one costs a tool call and its output, and, inferred, a permission prompt in a default-permission chat; eva-md never writes host settings to pre-approve it (`docs/known-limitations.md:311`).
- Only `System:` and `Constraint:` lines of the brief are deduplicated (`pointers.go:166-187`). A decision that is both in the thread page's Now and a knowledge page is sent twice.

Direction: one planner that owns the budget of every injection in a chat. Say the legend once (in the brief), send only content per prompt, name pages by a short handle, and deliver the needed text itself or fetch through a pre-approved tool rather than a pasted command. Remove the periodic reminder. Rebuild the presentation.

### F8. Scope can widen by heuristics and by pages the agent writes (medium; reliability-safety; partly known; mechanism verified, exposure inferred)

- Siblings come from `.eva-md/repos.tsv` (`internal/systempage/repos.go:94-106`), built from each system page's `repos` frontmatter (`internal/systempage/write.go:250`, `write.go:532`). A system page is written through `eva-md system write`, by the agent in a chat. One broad page that lists repos of two clients makes each a sibling of the other (`docs/known-limitations.md:157`), and then a lexical match alone moves one client's fact into the other's chat.
- A bare word that is a project's name, such as `api` or `web`, names that project in any prompt of the same owner (`scope.go:82`; `docs/known-limitations.md:158`).
- Recall ranks `[observed]` facts and procedures, which can rest on a file's or an MCP tool's output (`internal/update/knowledge.go:76-77`; `docs/known-limitations.md:150`), level with the user's own words (`recall.go:141-152`). Text that entered through a tool result can come back in later chats as "knowledge". The label does say what it rests on.
- The hook trusts the index rows as they stand; it never checks a row against its page (`internal/hook/recall.go:101-109`). The index is committed to the wiki repository (`index.go:16-20`), so whatever writes that repository writes what prompts receive, until the next rebuild.

Direction: an explicit boundary model. Workspaces (owner, client or a set the user names) fixed at setup or by the user's words, never widened by a page the agent wrote without the user's say. Trust tiers in ranking: user-grounded first, observed lower and marked, imperative kinds only from the user. Evolve.

### F9. Three matchers, three file readers, five side files (medium; architecture; verified)

- Three hand-rolled matchers, each with its own tokenizer, stoplist and case rules, each tuned on its own fixture:
  - system names: `pointers.go:445-583`;
  - knowledge terms: `internal/knowledgepage/recall.go:170-271`;
  - project names: `internal/knowledgepage/scope.go:54-159`.
  Two rules for a long prompt (`pointers.go:438-443`, `recall.go:176-179`).
- Three "read a wiki file safely" implementations with different checks: `pointers.go:294-308` (no hard-link check), `index.go:216-254` (parent, hard-link and same-file checks), `resume.regular` (used at `internal/resume/systems.go:307`).
- A prompt can read `names.tsv`, `knowledge.tsv`, `repos.tsv`, `systems/map.md`, `code-status.json` and `schema.toml`, each a format of its own, and parses the whole 512 KiB TSV every time (`index.go:186-200`).
- `internal/knowledgepage` holds the page model, the save, four derived files and the retrieval in one package. Part of the page model exists for okf: the statement repeated as the description so okf's drift check passes (`page.go:36-37`, `page.go:276`), a map link on every page so okf counts none as orphaned (`page.go:92-93`), the folder index and root index lines (`files.go:279-298`, `files.go:369-391`), and reading a page as damaged when okf's save would refuse it (`page.go:186-193`).
- Derived caches (`.eva-md/*.tsv`) are committed with the wiki (`index.go:16-20`, `internal/systempage/names.go:17-25`). Inferred: two machines that both rebuild them will conflict.
- The history shows the pattern: recall arrived on 2026-11-02 in one commit of 4,208 added lines across these files (9e4f609), was touched the same day in "Fix 22 bugs" (5a8e654), and gained scope as a second layer of about 700 lines five days later (51b241c).

Direction: one retrieval module with one tokenizer, one index over systems, knowledge, rules and threads, one safe reader, and one planner that decides what each injection holds. The page store keeps the pages; the index is local state built from them, not a file in the repository. Rebuild.

### F10. Nothing learns from use (medium; memory-quality; partly known; verified)

- No record of which facts were recalled, read with `show`, contradicted, or superseded soon after (section 1.8).
- Ranking has no prior for kind, tag, recency or past usefulness (`recall.go:141-152`).
- Past 512 KiB the index drops the oldest `as_of` first (`index.go:104-113`; `docs/known-limitations.md:160`). The oldest facts are often the founding ones.
- The duplicate check at save is exact normalized text (`save.go:295-304`); near-duplicates rely on the update's own check over what the index lists.

Direction: record recall events and their outcomes locally, use them to rank, to choose what the bound keeps, and to propose merges and retirements in the daily job. Add.

### F11. System pointers match names only and hand the agent a command (medium; recall-quality; known; verified)

- A one-word name under 5 characters never matches (`pointers.go:42`, `pointers.go:536`): Auth, API, CLI, DB, UI. Listed at `docs/known-limitations.md:135-136`.
- 12 pointers a chat, then none, even for a system not yet named (`pointers.go:246`; `docs/known-limitations.md:143`). Once a chat, kept through compaction (`docs/known-limitations.md:140`).
- The pointer is a title, a cut description and a command, not the part of the page the prompt needs (`pointers.go:380-409`).

Direction: fold systems into the same retrieval and planner as knowledge: match by description and cited paths as well as names, and deliver the relevant section within the shared budget. Rebuild with F1 and F9.

### F12. Recall can be lost without a trace (medium; reliability-safety; mechanism verified, the Codex cut inferred)

- Recall runs last in a 300 ms budget shared with capture, transcript recovery and the brief (section 1.1). When time runs out it returns nothing (`internal/hook/recall.go:93`, `recall.go:102`) or the state save fails and the lines are dropped (`internal/hook/resume.go:77-79`). A run that ends past its deadline is counted as a failed hook run (`hook.go:217-219`, `internal/hook/resume.go:338-351`), but nothing says a recall was lost with it, and a run that ends just inside the deadline with recall skipped counts as a success.
- A row whose line the secret scan flags, or that cleaning would change, is never recalled, and nothing reports which rows those are (`recall.go:180-182`; the doctor's knowledge row has no such count, `internal/health/knowledge.go:53-77`).
- The state is saved before the output is written (`internal/hook/resume.go:77-87`, `hook.go:140`). If the output is then lost, the fact is marked as sent and never comes again in that chat.
- Inferred: on Codex the hook context is limited to 3,000 tokens (`internal/install/templates.go:95`); recall is last in the context (`internal/hook/resume.go:81-86`), so it is what a cut would remove.

Direction: count every decision and every drop in the local log of F5 and show them in the doctor; base re-sending on what is in the context (F3) so a lost delivery heals itself. Evolve.

---

## 4. Essential and accidental complexity

Essential: deciding what a chat needs at a given moment, within a small budget, without a network call, and without letting stored text steer the agent or cross a privacy boundary. The strict reads, the fence, the provenance tags, the closed scope and the caps are all that problem.

Accidental:

- Three lexical matchers and their stoplists, each patched by fixture (F9).
- A scoring rule whose gate and floor encode one fixture's near misses (`recall.go:27-37`).
- Project-name detection in prompt text (`scope.go`, 159 lines) standing in for a scope model.
- Five side files and three safe readers.
- A label that restates the legend on every prompt.
- A "sent once" list standing in for "is in the context".
- Page fields and files kept for okf's drift and orphan checks.

---

## 5. Overall call

Keep the safety envelope (section 2) and `knowledgepage`'s page and save model, which is careful and well tested. Evolve the page with validity and retrieval fields. Rebuild retrieval: one module, one index, one planner, driven by prompt and tool events, with paths as a first-class signal and a context-presence model in place of the once-a-chat list. Add evaluation and a decision log before tuning anything, since no number in the current scorer has met real data.

---

## 6. Open questions

1. Which hosts let a hook add context before a tool call and at subagent start, and with what limits? The answer sets how far F1 and F6 can go on Claude Code and on Codex.
2. Is an on-device embedding index acceptable under "two dependencies only" and "hooks make no network calls", or must retrieval stay lexical over cues the update model writes?
3. Should program-run chats and subagents read memory when they are not captured?
4. What is the boundary the user means by "project": a repo, an owner, a client? Should a workspace be declared at setup?
5. How does Codex cut hook context past `additionalContextLimit`: from the end, silently?
6. Should derived indexes stay in the wiki repository, given several machines?
7. What do model-written titles and statements look like? No live update has run, so the wording recall must match is unknown.
8. May eva-md pre-approve its own read-only tools at setup, with the user's consent, so that following a pointer never asks?

---

## Verification

A second reader tried to refute each finding against the code. Nothing in the repository was changed and no test or restricted command was run. Paths are relative to `the repository`. Arithmetic was recomputed from the constants in the code.

Result: 10 confirmed, 2 partly (reach-1, invis-1), 0 refuted. Two severities changed: recall-1 from critical to high and pointers-1 from medium to low. The rest are kept.

### Verdicts

| Id | Verdict | Severity | Evidence and corrections |
| --- | --- | --- | --- |
| recall-1 | confirmed | high (was critical) | `promptLines` hands `pointers` and `recall` the prompt text and the project key only (`internal/hook/pointers.go:194-198`). `recall` returns nil under 2 terms (`internal/knowledgepage/recall.go:72-75`). The hook lists hold no `PreToolUse` or `SubagentStart` on either host (`internal/install/templates.go:71-87`), and `PostToolUse` is async, so it cannot inject. `Row.Paths` has no reader in recall: the only readers are the index's own write and parse and `internal/update/knowledge.go:416`. Lowered to high because the startup brief still delivers the Now, the standing rules, up to 3 constraints and the project's system pointers, and because nothing in the repository shows what a host allows a hook to add before a tool call (open question 1). |
| recall-2 | confirmed | high | Whole lowercased tokens only (`recall.go:91-104`, `211-243`); gate at `recall.go:124`; floor 2.5 (`recall.go:37`). Recomputed: N=200, df=5 gives 0.735 a term, 2.20 for three statement terms, under the floor; with one of the three in the title it is 2.94 and passes. `df` is counted over every row before the scope test (`recall.go:88-110`, scope at `137`). Listed in part at `docs/known-limitations.md:155-156`. |
| recall-3 | confirmed | high | `maxConstraints = 3`, newest first (`internal/resume/knowledge.go:25`, `48-53`); skip by the chat's pages (`internal/hook/recall.go:107-109`); `cons` is built only when `!compact` (`internal/resume/resume.go:321-324`); the state resets only when `b.fresh`, startup or clear (`pointers.go:105-107`, `internal/hook/resume.go:178`). `docs/design.md:1011` and `1033` say the same. One correction to the impact: the standing rules page is a separate block and a compact brief does carry it (`resume.go:321`, `rulesBlock` is outside the `!compact` test), so what is lost is knowledge constraints and recalled facts, not standing rules. |
| recall-4 | confirmed | high | The line shows id, kind, tag, project and `as_of` (`internal/hook/recall.go:179`). `Entry.Code` holds code sources (`internal/knowledgepage/page.go:133-135`); `internal/coderef`, `cmd/eva-md/code.go` and `internal/lint` hold no knowledge handling (grep). `Entry` and `Row` have no branch field. A supersede or drop needs a user prompt at or after the entry's time (`internal/update/knowledge.go:151-157`, `212`), so a later observation alone never changes an `[observed]` fact. "Both shown" for two facts that disagree follows from the absence of any contradiction check; it was not seen in a run. |
| eval-1 | confirmed | high | `Floor` "was tuned on fixtures, not on real use" (`recall.go:36`). The 500-row case adds rows of `zq…` words no prompt holds (`recall_test.go:225-246`). `Recalled` is read only by `empty()` (`pointers.go:63`); neither `recall.go` nor `pointers.go` logs anything. `docs/verification.md:3` and `:149` say no live update has promoted a page. Correction to `known`: the "never measured on real use" half is listed (`docs/known-limitations.md:156`, `docs/verification.md:149`); the missing decision log is not. |
| reach-1 | partly | high | Right: a subagent gets nothing (`hook.go:680`, no `SubagentStart` hook); a prompt from a program or a headless chat gets no per-prompt lines (`hook.go:518-521`); the MCP server has `eva_search`, `eva_show`, `eva_status` only (`internal/mcp/tools.go:81-109`); `knowledgepage.Recall` has one caller (`internal/hook/recall.go:105`). Off: a headless or SDK chat still gets the SessionStart brief with the standing rules and up to 3 constraints, since `resumeContext` gates only `UserPromptSubmit` (`internal/hook/resume.go:60`, `hook.go:571`; `docs/known-limitations.md:29`), so "run without them" overstates it for those. The cloud gate (`hook.go:284-286`) is not the cause there: eva-md runs only on macOS and the wiki is on the Mac. |
| invis-1 | partly | medium | Right: label and fence are 258 characters with one tag kind and 334 with two (recomputed); the reminder goes on prompt 1 and every 5th when nothing else rides (`pointers.go:204-214`); pointers and the reminder end in a shell command at an absolute path (`pointers.go:223-226`, `385`); only `System:` and `Constraint:` lines are deduplicated (`pointers.go:166-187`). Off: two 300-character statements do fit with one tag kind, at 991 characters for a 25-character key and a 3-character id; they miss only with two tag kinds (1,067) or a longer key. The 411 figure includes the "Read one" suffix, which is added only when the block still fits (`recall.go:150` against `159`), so it never displaces a fact. The permission prompt is inferred, as the report says. |
| safety-1 | confirmed | medium | Siblings are every other key of a page that lists the project (`internal/systempage/repos.go:94-106`); a page's repos are its frontmatter list plus the repos of its code sources (`internal/systempage/write.go:250`, `364`), and no write restricts them. Bare name within the owner at `scope.go:82`. `promotable` allows `observed` facts and procedures (`internal/update/knowledge.go:76-77`); the order ignores the tag (`recall.go:141-152`). "Never checks a row against its page" is true and adds little: whoever can write the index can write the page. |
| arch-1 | confirmed | medium | Three matchers as cited. Two stoplists, not three: `scope.go` uses recall's through `content()`. `knowledgepage.openIndex` (`index.go:216-254`) and `resume.regular` (`internal/resume/read.go:33-68`) are near copies with the same checks; only `hook.readFile` (`pointers.go:294-308`) checks less. Six files a prompt can read, as listed. The inferred conflict across machines does not arise today: a wiki belongs to one machine's instance and eva-md never pulls (`docs/known-limitations.md:223`, `docs/design.md:1295`). It becomes real only if several machines are to share a wiki. |
| mem-1 | confirmed | medium | `Recalled` is write-only. `Pointers` is read, for the 12-a-chat cap (`pointers.go:246`, `263`). The order is score, local, newest (`recall.go:141-152`). The bound drops the oldest `as_of` first (`index.go:104-113`), but it is reached at about 1,678 rows (`docs/verification.md:110`), so that part is far off. |
| pointers-1 | confirmed | low (was medium) | `minWord = 5` (`pointers.go:42`, `536`), 12 a chat (`pointers.go:246`), title, cut description and command (`pointers.go:380-409`). All listed at `docs/known-limitations.md:135-143`. Lowered because the brief already lists the project's systems and the index, a name of several words or an alias matches in any case, and the pointer does carry a description. It is fixed by recall-1 and arch-1, not on its own. |
| rel-1 | confirmed | low | Unshowable rows dropped without a count (`internal/hook/recall.go:180-182`); the doctor's knowledge row counts pages and the rows the bound leaves out, nothing else (`internal/health/knowledge.go:53-77`). The state is saved in `resumeContext` (`internal/hook/resume.go:77-79`) and the output written later in `Run`'s deferred call, and dropped when the work deadline passed first (`hook.go:140`, `214-219`). The Codex cut stays inferred. |

### Missed

1. **scope-key-1: a project's memory hangs on an exact key string, and common events change the key** (high; reliability-safety; verified in code; not listed).
   - The key is the normalized `remote.origin.url`, or for a repo without a remote a hash of its folder path (`internal/identity/identity.go:142-153`, `internal/identity/key.go:13-36`).
   - Recall's local test, the brief's constraints and the brief's system pointers all compare it for equality (`internal/knowledgepage/recall.go:137`, `162`; `internal/resume/systems.go:100`). No code maps an old key to a new one, and no doctor row reports knowledge under a key no project has.
   - So a first push of a local repo, a renamed or transferred repo, a moved folder or a clone through a URL alias gives a new key, and every constraint, fact and system pointer of the project leaves the chat's scope without a word. `docs/known-limitations.md:21` lists the alias clone, for exclusions only.
   - Direction: a project identity kept in the wiki, one id with every key that has named it. Resolve a chat's key to the id, add a key when the same folder shows a new remote, and let the user join two.

2. **sent-1: the sent-once list records page ids, not what text reached the chat** (medium; recall-quality; verified in code; not listed).
   - With 3 or more constraints in scope each line gets about 199 characters, of which the command takes 66 for a 45-character install path, so a statement of up to 300 characters is cut to about 119 less the title's length, or left out whole (`internal/resume/knowledge.go:59-60`, `72-87`; `knowledge_test.go:182-183` shows both forms).
   - That page is then on the chat's list (`pointers.go:170`, `181-185`) and recall never sends its full statement (`internal/hook/recall.go:107-109`).
   - A supersede rewrites the statement under the same id (`internal/knowledgepage/save.go:359-372`), and a refresh is triggered by the Now and rules hashes only (`internal/resume/resume.go:92`), so a chat that was sent the old statement never gets the corrected one, nor word that a fact was retired, until a startup or clear.
   - Direction: record what was delivered, by page, version and whether whole, and treat a changed or retired fact the chat holds as due for a correction.

3. **corpus-1: per-prompt recall reads knowledge pages only** (high; recall-quality; mechanism verified, the share of memory outside it inferred; listed in part at `docs/known-limitations.md:147-148`).
   - A prompt reads `knowledge.tsv` and `names.tsv` and nothing else of the wiki (`internal/hook/recall.go:101`, `pointers.go:254`). Thread pages reach a chat only as the current branch's Now in a brief; other branches are named with a day (`internal/resume/lines.go:147-149`).
   - A pass keeps at most 5 knowledge changes (`internal/update/knowledge.go:17`) and refuses any statement the validator cannot ground in the user's words (`knowledge.go:128`).
   - So a decision that lives in another branch's Now or in a Timeline cannot be recalled by any prompt; the agent must choose to search.
   - Direction: put decisions in force, Timeline entries and standing rules of every thread in the same retrieval index, with their branch and state as scope and validity signals, and keep promotion to knowledge for what must outlive its thread.
