# Critique: feasibility and correctness

Critic's lens: will each design work as written, against the code at `the repository`, the hooks' budgets and fail-closed rule, the privacy guarantees, the hosts' behaviour as the research recorded it, and AGENTS.md.

What was read: the brief, the lead's notes, all six designs in full (evaluate's work-package table skimmed), and the parts of the audits, research notes and code that each claim below rests on. Nothing in the repository was changed. Nothing was run but read-only commands.

What is left out: everything that is fine. Most of each design is. The host facts each design lists as "to confirm first" are not repeated here unless a design builds on one without a fallback.

## Verdict

The six designs are careful one by one and mostly agree with the code and the research. Together, as written, they do not yet work. Two faults would break a guarantee outright: an agent's write tool can store text from a chat that is off the record, and the `memory` taint, read as written, makes all agent text in every chat that got a brief count as an echo of memory. The user's-authority tier is weaker than v1's in three places. The cut-over contradicts itself: the upgrade design needs a binary that still runs v1's layout for a day, three designs delete v1's code at the cut-over, and the store's migration is not the pure function its own sync rules need. Two privacy regressions are silent: "off the record" is ignored on a prompt whose author the allowlist cannot prove, and the store's migration commits and pushes files v1's secret gate held back. The brief's newest exchange now depends on a detached job, with no measure of how often it is missing.

None of this needs a different architecture. All of it is cheaper to fix in the plan than in the build. Thirteen issues would change the plan; seven are smaller.

## Issues, most serious first

### 1. Blocker. deliver (with derive, ledger): a write tool can store text from an off-the-record chat

- **What is wrong.** `eva_note` is "checked and applied at once in the server's process" through `store.Apply` (deliver 11; derive 3.4, job P1). The MCP server "still knows only its folder" and "does not know which chat it serves" (deliver 10.3), so `Principal.Private` cannot be set for it. Nothing in deliver 11 or derive refuses a write while a chat is off the record. On a host with no hooks there is no off-the-record state at all, and the note "is recorded in the store's commit".
- **Why it matters.** The ledger's guarantee is "nothing a user says after an off-the-record control is stored, by any path" (ledger 7.1). An agent that calls `eva_note(remember, text=…)` in such a chat puts that text into durable memory, into a commit, and with sync onto the remote. v1 has no such path because its server only reads.
- **Change.** The server never applies a write. It queues the call's arguments in program state under the receipt token it already mints. The write is applied by the ingester when the PostToolUse event for that call is released for a session that is on the record; a session that is off stores only the event's fixed name, and the queued write is dropped. On a host with no hooks the write tools are not offered. This also removes the timing race the design accepts (the turn's prompt not yet released) and gives the write a ledger event, which the store's S-L3 needs.

### 2. Blocker. derive and ledger: the `memory` taint turns every briefed chat into echo

- **What is wrong.** The ledger sets taint `memory` on a turn when there is "a delivery record in or before the turn, or an eva-md lookup" (ledger 3.7). Derive's tag table gives agent text with taint `memory` the tag `echo`: "adds no support and creates nothing" (derive 5.5). Every chat gets a start brief, so every turn of every chat has a delivery record before it. The `accepted` row requires taint "none on the proposal's turn", and its second row lists only `web`, `net`, `mcp`, `subagent`; a proposal in a `memory` turn has no row.
- **Why it matters.** Read as written, no agent text is ever `suggested`, so no candidate fact, no lesson (needs `suggested` or better), no procedure, and no accepted decision can come from any chat eva-md briefed. The intent, stated two lines later ("a restatement of what eva-md put into the chat is echo") and in checker step 9, is per item; the rule is per turn.
- **The same fault, wider.** Taint is set for the whole turn from its tool records. In agentic work one turn is dozens of tool calls. One `gh pr view`, one MCP call to another server or one subagent makes the final reply `external`, and the user's "yes" to it becomes scope `here`, flagged. The share of real turns this hits is unknown and may be most of them.
- **Change.** Drop `memory` from turn taint. Echo is decided per operation: an add that is near-equal to an item whose id is in the session's delivery or pull records is dropped. Record the other taints with the sequence number of the tool event that caused them, so derive can tell text written before a fetch from text written after. Add to Stage 0: the share of turns with each taint class, counted on the real install, before the tag table is fixed. Add fixture families in suite E where a true fact sits in a tainted turn.

### 3. Major. Cross-cutting: the cut-over order contradicts itself

- **What is wrong.** Hosts 10.5 and 15: data migrations run only after a clean probation, and "during probation the new binary works on the old layout". So the binary that carries the migration must still capture into the v1 inbox, run v1's update and build v1's brief for at least a day. Store ST-13 deletes `okfadapter`, `wikigit`'s commit path and the live use of the v1 page packages at the cut-over. Derive DV-26 deletes the v1 control plane in Stage 2. Ledger LG-21 "deletes the hook-sliced recovery" while the ledger's own shadow mode says "the hook runs v1 capture unchanged" until cut-over. Store, derive and deliver "cut over together behind one flag".
- **A second break.** A v1 install reaches v2 through v1's own `eva-md upgrade`, which takes the latest release and runs its `setup` (`cmd/eva-md/upgrade.go:116-156`). v1 knows nothing of `from_min`. So whatever release is newest must still be able to take a v1 install through probation, shadow and both migrations, for as long as any v1 install exists.
- **Change.** The plan names one bridge release: both stacks in one binary, v1 the truth until the migrations have run. v1's writers are deleted only in a later release, and that release's `setup` refuses a v1 layout with one line naming the bridge. State in the plan which work package holds each deletion, and move the deletions out of LG-21, DV-26 and ST-13 into it. For the one real install, say plainly whether the first migration is automatic or run with the owner watching (store risk 2 asks for the second).

### 4. Major. deliver, store, hosts: the user's authority can be forged three ways

- **The tool.** Authority is granted when the call's `quote` is "an exact span of a typed prompt that the ledger has released in this project in the last ten minutes" (deliver 11). Nothing ties `text` to `quote`. The user's prompt is in the agent's context, so an agent steered by a web page can quote any few words of it and attach any rule. Derive's grounding check covers only paths, URLs, identifiers and numbers with units. "S-V5 then holds by construction" (deliver 15.7) is not true.
- **The command line.** "A person typing a command is the user's authority" (deliver 5 and 11). Every coding agent has a shell tool, and hosts 5.3 wants agents to run eva-md commands. `eva-md remember`, `confirm`, `correct`, `rule arm --ask`, `scope review`, and `recall` with `scope: all` are all open to it. The closed cross-project default (DL-D6) is one shell call from open.
- **The file.** A stray edit in the store folder is "committed as kind human", "trusted as the person's", and "takes human authority" (store 7.3, 8.6, 5.7). An agent with a file-write tool can add an item under the user's tag at a path every install shares.
- **Change.** One rule for all three, stated once in the plan: only a typed prompt in the ledger gives the user's authority at once. A tool write is always a candidate, raised only by the fast pass over its own turn under the normal contract. A command gives authority only when it runs on a terminal and no host marker is in its environment; otherwise it is the agent's and makes a candidate or a dispute. An adopted file edit and an `in/` branch are held like a foreign commit (`origin: unverified`, not pushed as "you") until the person acknowledges them. Suite I gains a forged-quote case, a shell case and a file case, each a hard gate.

### 5. Major. derive (and evaluate): a quote proves a span exists, not that it supports the statement

- **What is wrong.** The tag comes from where the cited span sits (derive 5.5). The statement is the model's free sentence. A model that cites the user's "fix the login bug" under a decision the assistant made alone gets tag `stated`. v1 refused this by word overlap; v2 deletes that test and keeps the claim "decided by `pv` instead of by word overlap" as if the invariant were unchanged (derive 10.2). It is weaker: from "the user's words support it" to "some of the user's words were cited".
- **The gates cannot hold as written.** `misattributed` 0 and `stored_with_authority` 0 are hard gates at Level 0, where the simulated model "obeys the payload" (evaluate 4.2, 6.5). A simulated model that cites a real typed span for a payload passes the checker, so either the gate fails by design or the simulation is too weak to test anything. `Guarantees` v2 ("no user-tagged entry was created without a typed span") is true and does not protect.
- **`accepted` rests on the model.** Assent is "the user's first prompt after the agent event holding the proposal". With no polarity test, "no, don't" quoted as assent makes a durable decision. This is the one tag whose authority a model asserts, against rule 5 of the design.
- **Change.** For the user's tier, add a containment rule that needs no word list: the value-bearing tokens of the statement (numbers, quoted strings, identifiers, capitalized and long tokens, by pattern) must appear in the cited typed span, or for `accepted` in the proposal span. For `constraint` and `preference`, store the user's quote as the statement and the model's sentence as the gloss. Limit typed assent to candidates unless the turn also holds a dialog choice or a plan approval, until suite E's rejected-proposal family shows a rate. Say in the plan what code guarantees whatever the model writes, and what is only measured. DV-03 must include the adversarial-citation case.

### 6. Major. store: the migration commits and pushes what v1's secret gate held back

- **What is wrong.** Step 3 of store 9.2: "copy the old working tree's page files over the clone and commit them as `v1 final`", then step 7 pushes. v1's daily commit leaves a file out when an added line trips the scan (`internal/wikigit/commit.go:77`, `Unstaged`; storage audit, line 58). Those files sit changed in the working tree by design. Step 3 names no scan, and the commit is not made through `Apply`.
- **Why it matters.** A line v1 kept off the remote for looking like a secret is pushed by the upgrade. It stays in history even if step 1→2 rewrites the file.
- **Change.** `v1 final` goes through the same gate. A file that trips it keeps its last committed bytes in the commit; the held version goes to the local held reference of store 7.3 and raises the same condition. Add the case to ST-07's acceptance and to the conformance property "no planted secret is reachable from any pushed reference".

### 7. Major. ledger: "off the record" is ignored when the author of a prompt cannot be proven

- **What is wrong.** The Off control fires only on "a typed prompt (`pv` typed)" (ledger 7.2, 5.1 step 5). `typed` needs an entrypoint on the adapter's list and more. Anything else is `unknown`, and "`unknown` text is still stored" (3.6). On a host surface the list does not know yet, a person who types "off the record" is not heard, and the text is kept. v1 fails the other way (capture-4: the deny-list defaults to typed), so the control is honoured.
- **Why it matters.** The design's own argument is that a false Off is cheap and visible and a false On is not. It applies it to On and not to Off.
- **Change.** Off is honoured on `typed` and on `unknown`. On stays `typed` only. Pasted parts and host-written rows stay excluded, which keeps the fix for catchup-10. Add the case to the hard gate `otr_leaks`.

### 8. Major. ledger and deliver: the brief's newest exchange now races a detached job, and nothing measures it

- **What is wrong.** A hook writes only the spool; nothing in a spool is readable (R2). With a readable transcript, an event is released when the scan has passed its counterpart row (R3), and the docs say the transcript "is written asynchronously and may lag" (claude-surface.md:110). Ingest is started by Stop, SessionEnd, a prompt and four other events, not by SessionStart, and a dirty session is swept only after two minutes. So a reply whose row was not yet on disk when Stop's ingest ran stays held until the next signal of that chat.
- **Why it matters.** Deliver says the newest exchange "keeps both sides in short form always" and that the handoff "never waits". Derive calls the manifest and tail "enough for a chat that starts the second the last one ended". Neither holds when the last turn is still in the spool. Worse, `lag = catalog.last_seq − covered` is computed from released data only, so the brief says "0 turns behind" while a turn is missing. That breaks brief must-keeps 10 and 11 without saying so. v1 writes the reply in the Stop hook and withholds the brief when recovery is behind.
- **Change.** The chat-start hook reads the dirty mark of the project's newest sessions. When one is dirty it waits for the in-flight ingest up to a bound (v1 spends 150 ms here today), then, if events are still held, says so in the handoff line. Add `ledger_lag_at_start` (held turns of the previous session at a chat start) to the ledger's bars and the freshness ledger. Add to LG-03: how often the reply's row is absent when Stop fires.

### 9. Major. derive: a squash or rebase merge reads as an abandoned branch, and its knowledge is retired

- **What is wrong.** A workstream is merged "when the branch's head is an ancestor of the default branch, or the branch is gone and its last captured head is". Otherwise, branch gone: "abandoned: its provisional decisions and facts are retired" (derive 6.5, DV-23). After a squash or rebase merge the head is never an ancestor, and the branch is usually deleted.
- **Why it matters.** Squash merging is the default in many teams. Every such branch's decisions and facts would be retired at the moment they became true for the project. A second machine that never had the branch sees "gone" at once.
- **Change.** Absence proves nothing: a gone, unmerged branch pauses the workstream and keeps its items provisional and labelled. Merged is also detected by patch equivalence against the default branch (`git cherry`) and by an empty diff between the captured head's tree and the default branch for the files the workstream edited. Retire only on the user's words. Count the share of squash merges in the field report.

### 10. Major. ledger: the hook's speed bars and the deadline rest on owner decision LG-D2, and one claim is wrong without it

- **What is wrong.** Redaction today costs 20 to 67 ms at 16 KiB of reply-like text and "the baseline alone is about half" (redact audit, lines 132 and 262). The bars are capture p95 under 50 ms at 16 KiB and 120 ms at 64 KiB. LG-11 (Stage 1, depends on nothing) must be "at least twice as fast at 16 KiB" while `Redact` stays the run-time union. That needs the frozen baseline engine not to run, which is LG-D2, an amendment of an AGENTS.md rule that LG-11 does not list.
- **The wrong claim.** "If the owner says no … the deadline … still lands" (ledger 10.6). The baseline takes a string and returns a string, cannot be changed, and cannot be stopped part way. Running it on pieces changes what it removes. The only way to bound it is the abandoned goroutine the design deletes.
- **Also unproven.** LG-11 asks one pass to equal a loop of up to 64 passes on 150,000 inputs. There is no fallback if it cannot.
- **Change.** Put LG-D2 before LG-11 and make LG-11's bar conditional. With LG-D2 refused, state the real consequence: both engines run, a large reply is cut at the hook and filled from the record, and on Codex without a record the cut stands. If one pass cannot equal the loop, keep a bounded loop over spans and gate on the differential test, not on the pass count.

### 11. Major. store and ledger: the v1 migration is not the pure function the store's own sync needs

- **What is wrong.** "A step is a pure function of the tree: no clock, no random ids, nothing of the machine" (store 9.1), and 8.5 leans on it ("files neither side changed migrate to the same bytes on both sides"). Step 1→2 takes project ids from the ledger's registry, where an id is "minted once, random and opaque" (ledger 8.2), and paths are `projects/<pid>-<slug>/…`. It writes `.eva-md/coverage-v1/<writer>.tsv` from local watermarks.
- **Why it matters.** Two machines that migrate one v1 tree get different paths for every file. ST-04's acceptance ("the same tree migrated on two simulated machines gives one tree id") cannot pass for the one real step. Workstream ids keyed on (project, branch) inherit the difference.
- **Change.** A v1 project's id is a function of its v1 key (a hash), and the ledger accepts it. The coverage table is a separate commit after the step, not part of it. ST-07's property test runs the step on two machines.

### 12. Major. store: the merge rules are applied only where git reports a conflict

- **What is wrong.** Sync runs `git merge --no-commit -X no-renames` and resolves "each unmerged path by class" (store 8.2). Git merges cleanly when two machines changed different lines of one item. The field rules say the content group moves as one register, "the whole group from the side with the greater (authority, asserted_at, writer)". A clean text merge can take the title from one side and the statement from the other, or a state from one and a body from the other.
- **Two more.** The add-of-one-path rule ("two different things: keep both") collides with workstream ids minted from a shared key, where two machines are meant to agree. Housekeeping runs on every machine: a machine without the project's clone, or with a stale one, makes different lifecycle calls on the same items every tick.
- **Change.** The engine merges every path both sides changed from the three blobs with its own resolver, never git's text merge, for every class but notes; mark those classes `-merge` in the store's `.gitattributes`. A keyed id that is added twice is merged, not split. Housekeeping steps that need a clone run only where the clone is confirmed (the `coderef` check), and say so in the commit.

### 13. Major. hosts, deliver, ledger, derive: generation 0 on Codex clashes with four other statements

- **What is wrong.** Hosts keeps the approved v1 Codex plugin untouched until the person opts in, after Stage 3 (hosts 4.3, 15). That folder holds v1's two skills and the MCP file with the hooks (`internal/install/templates.go:59-65, 115`). So on Codex, for as long as generation 0 lasts: the recall skill deliver removes is still loaded and still names `eva_search` and `eva-md search`; the digest skill still calls `eva-md system write`, which hosts removes and derive keeps; the content plugin that deliver's migration assumes ("the plugin text that names tools lives in the content plugin") does not exist, and installing it beside generation 0 would declare the MCP server twice.
- **Why it matters.** The first v2 experience on Codex is an agent told by a frozen skill to call tools that answer "use another tool".
- **Change.** Decide once: aliases and `system write` live as long as generation 0 is supported, the v2 server answers the old tool names with real results, and the tools unit is registered only with generation 1. Deliver's capability table is keyed on the installed generation (hosts H-V3 offers it). Or make the move to generation 1 part of the bridge release with one review, and say so to the owner as the price.

### 14. Minor. store: the person's git config still reaches the store, and two format details break rules

- **Signing.** The runner keeps the person's global config on purpose (`internal/identity/gitenv.go:14-31`). The design passes `gpg.format`, `user.signingkey` and `commit.gpgsign` per call but not `gpg.ssh.program`. A machine whose global config names a signing app there will have that app started for every pass, which is the prompt the design promises to end. Pin `gpg.ssh.program=ssh-keygen` and `gpg.ssh.allowedSignersFile` per call.
- **Bytes.** `core.autocrlf`, `core.eol` and a global attributes file can change what is checked out and committed. Pin them, and set `* -text` in the store's own `.gitattributes`.
- **The name.** `index.md` with `okf_version "0.2"` puts the library's name in a file a new wiki gets, which AGENTS.md forbids and v1 avoids on purpose (`docs/design.md:247`). The spec makes the key optional; drop it.
- **Conformance.** `legacy/v1/*.md` holds pages that could not be parsed, written by eva-md. OKF's rule 1 covers every `.md` file in the tree, so the bundle fails the profile the design tests. Give those files another extension.

### 15. Minor. deliver and hosts: a pack is refused unless the same build made it

- **What is wrong.** A hook refuses a pack whose `bin` "is not its own build" (deliver 4.3). Hosts' rule is that releases N and N-1 read each other's state, because a long chat keeps an old MCP server while hooks run the new binary. After every background upgrade the old server's packs are rebuilt by the new binary, and after a rollback the reverse.
- **Change.** Key the check on the pack format, the redaction version and the tokenizer version. Drop build identity.

### 16. Minor. deliver: Codex cannot ask

- **What is wrong.** The `ask` action returns "the host's own permission question" (deliver 8, DL-19, DL-D2). On Codex a PreToolUse hook can deny or rewrite the input, and a PermissionRequest hook can allow or deny (codex-surface.md 1.5). There is no ask. Claude Code has one (claude-surface.md:86).
- **Change.** Say that `ask` is Claude Code only, and that Codex gets `warn`. Suite G's 15% target is then per host.

### 17. Minor. derive: the contract's own bounds refuse its main new case

- **What is wrong.** A quote is "8 to 240 characters" (derive 5.3). The assent in the design's own example is "yes". `accepted_captured` at 80% cannot be met while the assent span cannot be cited.
- **Also.** The renderer quotes a line that looks like an entry and indents continuation lines (5.2). White-space folding covers the indent and not the quote mark, so a quote that crosses such a line misses.
- **Change.** An assent span has no lower bound and is checked as the whole prompt after trimming. `Verify` strips the renderer's own marks before matching.

### 18. Minor. ledger: three races between hooks and the ingester need a rule

- **The spool.** Hooks append under the spool's flock; the ingester "truncates the consumed part". Held events stay, so the file must be rewritten. A hook that opened the old file and waits on its lock then appends to a file that is gone. Lock a separate file, or reopen and compare after taking the lock, or never rewrite and keep a consumed offset.
- **The session lock.** The ingester holds it for a whole pass with no deadline. A hook that must write `privacy` under that lock has 300 ms. The control still reaches the spool, but hooks read privacy from `state.json` without a lock, so for the rest of the pass delivery is not silent. Keep a lock-free off marker, or hold the lock only to append and save.
- **Shadow.** "Shadow costs no user-visible time", yet the hook appends to the v2 spool after writing its output, in the same process, and the host waits for the process. Redaction runs twice. Do the shadow append in the detached child.

### 19. Minor. evaluate: one release gate can block the fix it exists to prompt, and two rules do not fit together

- **R2.** "Every release: both canary tiers green and under 36 hours old", and the release workflow fails when a blocking gate is red or stale. A host release that breaks eva-md turns the canary red. The fix cannot ship until the canary is green, and the canary tests what is published. State that the canary runs on the release candidate, and that a stale signed-in tier (a Mac that was asleep) can be waived by the owner for a host-break fix.
- **Held-out and caching.** Held-out scenarios are drawn from the release candidate's commit hash, so they have no cached `off` or `oracle` rows and no screening run. The budget's largest saving does not apply to the runs that decide a claim. Price the claim run with fresh baselines, or seal a held-out pack by a hash chosen before the code freeze.
- **Ownership.** `internal/hook`, `cmd/eva-md`, `internal/config`, `internal/paths`, `internal/session`, `internal/freetext` and `internal/coderef` are in no area's `owns`, and CI fails a diff outside `owns`. Four areas must edit `internal/hook`. Assign them before Stage 1.

### 20. Minor. store and derive: every session leaves a file and a commit, and two derive rules pull against their bars

- **Coverage commits.** "A pass that changes only coverage still commits", and a program-run or tiny session is covered as `skipped`. Every `claude -p` script and every short chat then adds an episode file and a signed commit to the synced repository. A skip costs no model call to recompute. Keep skipped coverage in local state and commit coverage only for ranges that cost a call.
- **The idle gate.** P3 work waits "while any session of that host has an open turn or an event in the last 2 minutes". A person who works in one chat while another sits idle never gets the idle one organized, which fails the bar "at most the tick interval plus 5 min" for a chat that never ends. Gate on the rate limit, not on activity, for one episode at a time.
- **Writes with no session.** S-L3 and D-L2 need every remember and forget to be a ledger event. The ledger stores events only under a host session. A command typed in a terminal has none, and deliver says a hook-free note lives "in the store's commit and the worker's journal". A rebuild from the record loses both. Give the ledger one operator log per machine.

## Contradictions between designs

1. Hosts 10.5 runs the new binary on the old layout through probation. Store ST-13, derive DV-26 and ledger LG-21 delete v1's code at or before the cut-over.
2. Ledger LG-21 deletes the hook-sliced recovery. Ledger 14 step 1 runs v1 capture unchanged in shadow until cut-over.
3. Ledger 8.2 mints random project ids. Store 9.1 and 8.5 need the v1 migration to be a pure function of the tree.
4. Deliver 18.5 puts tool text in a content plugin at cut-over and removes the recall skill. Hosts 15.5 leaves the v1 Codex plugin, skills and MCP file untouched.
5. Deliver 10.1 keeps `search` and `show` as aliases for one release. Hosts 15.5 keeps them while generation 0 is in the field.
6. Derive 6.8 says `eva-md system write` keeps working. Hosts 8 removes it.
7. Ledger 7.2 offers an MCP tool for off and on the record. Deliver 10.3 says the server cannot tell which chat it serves, and lists no such tool.
8. Store S-L3 and derive D-L2 make every write a ledger event. Deliver 11 records a hook-free note in the store's commit and the journal only.
9. Ledger 3.7 and derive 5.5 set `memory` taint per turn. Derive 5.7 step 9 decides echo per item.
10. Derive 7 sends evidence excerpts only to the host whose chat they came from. Deliver's `eva_open` and derive's own D-R2 show excerpts and the user's quote on any host.
11. Hosts 5.3: the program never rewrites `config.toml`. Store 9.2 step 6 moves `wiki_path` in it.
12. Deliver 4.3 refuses a pack another build made. Hosts 10.5 requires N and N-1 to read each other's state.
13. Ledger: ingest never runs in a hook and delivery never waits for it. Deliver 7.1: the newest exchange is always in the brief.
14. Ledger R4 reduces to its name any event of a turn whose prompt no hook saw. A start brief's delivery record is in turn 0. Derive and deliver need its item ids. The ledger lists five delivery surfaces; deliver uses seven.
15. Evaluate's Stage 0 table, row 1, would keep "v1's reconcile for workstream state". The store has no v1 page formats on the live path.
16. Store's workstream states, "kind rule", "state paragraph" and episode `Summary` differ from derive's. The designers flagged this; it is listed so the lead rules on it once.

## What no design covers

1. **The bridge release.** Which release holds both stacks, when v1's code is deleted, and what a v1 install is told when it meets a release that can no longer migrate it.
2. **Person or agent at the command line.** Every "the person's own command" (remember, confirm, correct, forget, rule arm, scope review, record on, remote set, hosts remove) is open to an agent's shell. No design tests for a terminal or a host marker.
3. **Privacy on the write path and on hook-free hosts.** No rule refuses a write while a chat is off the record, and a host with no hooks has no way to say "off the record" at all.
4. **Ledger lag at a chat start.** No bar, counter or brief line for events still held when the next chat begins.
5. **The tick's environment.** Derive's host facts cover a job a hook starts. A model run and a push from launchd (PATH, the login keychain, the SSH agent a signing app provides) are in no fact sheet. HO-01 checks that the agent installs, not that the work runs.
6. **Recorded host fixtures in a public repository.** Claude Code transcripts now hold request records: system prompts, tool lists, account and machine details. A scrubber that redacts is not enough. Fixtures should be regenerated from the recording's field names and shapes with made-up values.
7. **Two ids for one project.** After a join the store holds two project ids linked by `merged_into`. Deliver's packs, scope table and `Principal.Project` are keyed on one id and never mention the link.
8. **Trust for migrated history.** v1's commits carry the person's identity, not a writer key. A second machine that joins sees the whole v1 history as foreign. One ask then covers every migrated item; no design says so.
9. **`eva-md export --json`.** Evaluate's scorer reads it (E-S1). No store work package builds it.
10. **Stage 0 counts the designs now need.** Taint classes per turn; how often the transcript lags at Stop; whether a message typed while the agent works fires a prompt hook; the share of squash merges; whether the machine's git config names a signing program.

## Order of work, where this lens has a view

1. **Before any spec is written**, the lead rules on eight cross-design contracts: the authority rule (issue 4) and writes while off the record (1); taint and echo (2); the evidence rule for the user's tier (5); deterministic ids for v1 projects (11); the bridge release and where deletions sit (3); the Codex generation policy against deliver's content (13); the pack's compatibility key (15); Off on `unknown` (7).
2. **Stage 0** as designed, plus the counts of gap 10, the adversarial-citation and rejected-proposal cases in DV-03, and LG-D2 answered before LG-11 is sized.
3. **Stage 1.** Guarantees as data first, with the weakened ones (user tier, brief's newest exchange) written as they will be, not as v1 had them. The migrator framework, the codec and the conformance model, with the model merging every path both sides changed. The worker on v1 (DV-12) can ship alone and early. The redaction driver after LG-D2.
4. **Stage 2.** Ledger hook floor and ingester in shadow with v1's recovery still in the hook; store core; contract v2; the retrieval core and packs; all against a scratch store on the real install. Then the bridge release, the two migrations with a dry run and the owner watching, and only in the next release the deletions.
5. **Stage 3.** Write tools last, after suite I holds the forged-quote, shell, file and off-the-record cases as hard gates. Housekeeping without retire-by-absence. Tool-time delivery behind its measure.
6. **Stage 4.** Sync only on the full conformance suite, with adopted edits held like foreign commits. Generation 1 frozen once, with the tools unit.
7. **Stage 5** as designed, with the canary run on the release candidate.
