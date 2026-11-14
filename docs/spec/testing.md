# Testing

- **Component:** The test suite
- **Paths:** `cmd/eva-md`, `internal`
- **Status:** v1 as built, frozen on 2026-11-14

The blocks below are v1's design and verification text, moved here word for word when the two documents were split; [INDEX.md](INDEX.md) lists every spec.

<!-- v1 docs/design.md 035 -->
## Testing

The update's input builder, validator, renderer, runners and job, the offline accuracy suite with its rule and knowledge histories, the update's crash safety, system pages, the code they cite, knowledge pages, prompt pointers and recall, the brief's constraints, the daily commit and push and the setup states have their tests now (see [verification.md](../verification.md)).
The live accuracy runs and the live resume test with the update on are still to run.

- **Unit tests:**
  - remote normalization: 25 or more URL forms, worktrees, submodules, no remote, outside any repo
  - redaction corpus with positives and negatives (SHAs, UUIDs and `op://` must survive; fails closed)
  - fence calculation; caps that never split a character; watermark arithmetic; the escaper
  - branch-to-ID mapping: `index`, `.wip`, `-x`, deep names, unicode
  - the actor string always passes `IsValidActor`
- **Hook contract tests:**
  - recorded payloads for every event on both hosts, from the docs plus scrubbed live captures
  - exit 0 on garbage input
  - correct stdout shape: Codex Stop always `{}`; Claude Stop, async hooks and PostModelSwitch print nothing
  - tolerance of missing fields: Claude model absent; Codex `model` absent on SessionEnd, `turn_id` absent on SessionStart and SessionEnd, `transcript_path` and `last_assistant_message` null
  - subagent payloads (`agent_id`) never stored as user prompts
  - Codex `Exit code: N` parsing when supplied, empty-response `exit unknown` fallback, and apply_patch path parsing
  - the update triggers through an injected spawn: which events spawn for which sessions, at thresholds just below and at 60 KB, 30 minutes, 600 bytes and 2 minutes, the catch-up's limit of 5, a resumed chat's own backlog taken at its start and not again at its next prompt, nothing with
    `updates = false`, and nothing from a program-run Claude chat's own triggers, whose chat start still catches up the project's other idle sessions; and a real spawn from a hook process: the job in its own session, with no stdio of the hook's, outliving the hook, whose output ends at once
- **Inbox concurrency:** 8 writers with 1,000 appends each, and 4 writer processes, produce no interleaving. A line cut off mid-append, as a kill would leave it, reads as a gap marker, and the next append follows it without rewriting the file.
- **Deterministic resume suite** (temporary HOME, a repo with main plus 2 worktrees, scripted payloads):
  - startup, clear, resume, fork and compact, each with no page, a stale page and a fresh page
  - an Esc-interrupted last turn recovered by catch-up; a StopFailure marker
  - a chat with no real work skipped
  - lineage on a new branch
  - the first-prompt fallback for a spare session; the hash-triggered refresh, at most twice, after a compact brief too
  - a long-tenured Now (40 decisions) that keeps every next step and open question; the opening request from the head of a 5 MB session file
  - off-the-record content absent
  - Codex to Claude across hosts and back
  - hostile content (backtick runs, fake labels)
  - size caps
  - no assertion on Claude plan entries
- **okf adapter golden tests** (`generated.at` masked):
  - item accounting; strikethrough supersession; speaker downgrade
  - dates taken from refs, and unsourced dates rejected
  - idempotent re-apply leaves the file untouched
  - compare-and-swap retry; no-clobber create refuses an existing file
  - out-of-order guard; locked, human-verified and damaged pages
  - unknown sections and flat extra keys kept
  - fixed frontmatter order; `sources` as resource mappings
  - your `## Notes` with horizontal rules around a `Status:` line marks the page damaged, without a retry loop
  - okf Validate (Drift on) passes on the full layout with zero errors and zero orphans
  - index files sorted and unchanged when only thread content changes
  - a missing root index.md with a knowledge/ folder present is refused
  - SIGKILL during a save leaves a `.tmp-okf-*` file that is never staged, and okf's lock file, which the next save removes before its own
- **Accuracy suite, written fresh** (`internal/update`): 12 fact scripts, one per known failure mode (an assistant's suggestion against the user's decision, an event date against the date it was said, a reversal, a correction, a conditional fact, pasted text posing as a decision, a host prompt that
  must never become stated, item accounting across two passes, the two-session acceptance, the replace question and its exit, overlapping sessions processed in the wrong order, and an assistant's suggestion that survives as a proposal), and a 12-week tenure case.
  Each fact has a speaker, time, validity, the fact it replaces, a tag, a branch and a codename no model could infer.
  Code renders the scripts into inbox entries and derives the obligations: enforceable ones, which the validator guarantees whatever the model writes, checked after every output, and content ones, which depend on the model.
  Offline against prepared outputs in CI, where the correct outputs must score perfectly after every pass on the operation-level metrics (recall of planted facts, update accuracy, stale-value rate, speaker-tag and date accuracy, item accounting, duplicates).
  Live and opt-in per host, once updates run, with a target of every enforceable obligation met, zero speaker or date errors, and at most 2 content obligations missed.
  Seven knowledge histories run the same way, each pass saved into a real wiki: a fact superseded later, an assistant's suggestion, pasted text, a restatement, an older chat's supersede and drop, a locked page and a constraint with its path.
- **Recall precision** (`internal/knowledgepage`): 73 labelled prompts, each asked in its project, at indexes of 1, 3, 10, 36 and 500 rows, from the fixture's 36 rows of two projects and neutral rows that hold none of the prompts' words, with the two projects apart and sharing a system page, which
  fail the test below 90% precision or 60% recall at any size in either and log a sweep of floors at each size, the false positives and the misses.
- **Scope suite, written fresh (22 cases; planned):** one project across clones and worktrees; forks kept separate; folder identity; subfolders map to the top level; excludes; the wiki excludes itself; the general area; worktree chats resolved from payload `cwd`; one machine's instance guard refuses another machine's wiki.
- **Live accuracy run** (`cmd/live-accuracy`), opt-in, one host at a time: the accuracy suite's 12 fact scripts, which live in `internal/update/accuracy` so the offline suite and the run share them, run as the update job runs its passes.
  Each pass's batch is built for the page the passes before it made, sent once through the host's real headless update command with the update's rules and the page's schema, in an empty run folder, and the answer is checked by the validator and applied with `threadpage.Apply`; then every enforceable obligation is checked and the page measured.
  An answer the runner refuses as invalid or for a tool call, or the validator refuses, leaves the page and its `as_of` as they were, as a failed pass leaves them in the job, and counts as refused and as a content obligation missed.
  The page is still measured at that chat's time, so each fact the chat says and each value it ends counts as missed, as content whatever its kind, and the script's later passes, whose page is then older than they expect, are no longer held to its out-of-order guard.
  Any other host failure, such as auth, a rate limit or a timeout, stops the run unjudged, and so does an answer from the host's default model in place of a `--model` the host rejected, refused or not.
  It prints each history's metrics and misses, then the counts against the target: enforceable obligations missed, speaker errors (a fact under another tag), date errors (a Timeline line under another date) and content obligations missed (an answer refused, or a fact missing, not logged, still in
  force after it stopped holding, in two Now items, asserted though said off the record or pasted, or without its condition), each planted fact counted once a kind, and how many answers came at each effort the host used.
  It needs `--host` and refuses to run without `--spend-model-quota`, since it makes 21 model calls on the host's subscription, and refuses as a usage error an effort or model the runner would refuse and a `--cli` that names no executable file, which the runner would pass over for the CLI on PATH.
  It works in a temporary home of its own, which holds the run folders and is removed at the end, and reads and writes nothing of eva-md's; the host CLI keeps your usual login.
  `--record <file>`, a file that must not exist yet, writes each pass's answer as the run goes, one JSON line a pass holding the host, the model asked for, the effort the answer came at, the script and pass, the answer's JSON or null, and the runner's reason, and nothing else: no token count, no time and no path.
  `--replay <file>` answers each pass from such a recording: it calls nothing, needs no `--spend-model-quota`, and prints the report the run printed, given the run's `--effort` and `--model`; since the recording keeps the effort each answer came at, not the one asked for, a replay without `--effort` says the effort is `as recorded`.
  Before anything runs it refuses a recording of another host, model or format version, one with a reason the update does not know, a line with a key missing, added, given twice or of another type, null among them, and one that lacks a pass of the suite or holds one twice, as one from a run that stopped part way lacks one.
  `--pages <dir>`, a folder that is new or empty, gets each script's page after its last pass, as `threadpage.Render` writes it, as `v1.NN.md`, which the evaluation harness's `eva-eval fidelity` scores into v1's fidelity rows; a page that cannot be written stops the run before its next call.
  `--trace <dir>`, a folder that is new or empty, keeps what the run cost and what the host printed, written as the run goes: `run.json` with the host, the effort and model asked for, the version of the CLI the run starts, eva-md's commit and whether its tree had changes, whether timeouts count
  refused and the UTC date; `passes.jsonl`, a line a pass with the call's milliseconds, tokens in and out, attempts, the effort used, the model the host reported, the model and degraded notes, the runner's reason and why the run counts the answer refused; `raw/`, what the CLI printed on stdout and
  stderr on each attempt, which the update's runner hands to a tap that only this command sets; and `end.json`, written last, also when the run stops or is interrupted, with the outcome, met, missed or stopped, why it stopped and its counts.
  `end.json`'s reason for a stop is a code: the runner's reason, `model_fallback` for an answer from the host's default model, whose line is still written, `interrupted`, or `error`.
  Its JSON files hold no path and nothing of the machine, but the trace holds model names, host, session and thread ids and raw host output, which can name a plan, a rate limit's reset time, the model's reasoning and paths under the home folder, so its folder and files are private to their owner, the
  folder must lie outside the git work tree the command runs from, and it is handed over privately and never committed as written; a replay, which calls nothing, takes no `--trace`.
  `--timeout-refused` counts a pass that timed out as an answer the runner refused, so the run goes on, and the report adds how many answers timed out; the recording and the trace keep the timeout, and a replay with the flag counts a recorded timeout the same way.
  It is a developer's command: no release or install step builds it, and eva-md never imports the suite.
- **Live resume test** (the headline), opt-in:
  - On each CLI, chat A plants a codename, a decision, an open question and a next step.
  - Then a new chat, /clear and forced compaction each ask "where did we stop?", and the answer must contain all four.
  - Run with the update off and on, and with chat B started while the update is still running.
  - Uses bounded synthetic fixture chats with the normal authenticated host and normally approved plugin hooks. State-only automated tests use an isolated HOME. Live tests never copy `auth.json`, manufacture trust records, or bypass hook trust. A fresh-home authentication route is a separate opt-in test, not a prerequisite for the normal installed route.
  - Both desktop apps are checked by hand for each release.
- **Git** (against a bare remote and a stub signer):
  - one commit per day; a declined signature not retried the same day; a pending push retried
  - a non-fast-forward recorded as `diverged`, never rebased or forced
  - remote and instance mismatch refused
  - inbox and `.tmp-okf-*` never staged
  - your signing config untouched
- **Packaging:**
  - `claude plugin validate --strict --json` on the marketplace root and the plugin folder
  - hooks JSON lint (integer timeouts, no code path exits 2)
  - **both plugin folders byte-identical across binary-only releases**
  - install, uninstall and rollback round trip under a temporary HOME
  - the install record, the link on PATH, and uninstall's preview, `--apply`, `--all` and the update's and the daily job's locks
  - the install and uninstall scripts and `eva-md upgrade` end to end against a release stand-in built in the test, served by a fake curl or a local test server, with the wiki and its bare remote unchanged by an upgrade, an uninstall and the install command run again on a Mac that is set up
- **Benchmark** (`scripts/benchmark.py`, run by hand before a change is finished, not a test, and by a macOS job of CI as a comparison of the change's binary with the one before it, `--compare`, which reports and does not yet fail the job; see [verification.md](../verification.md)): p95 under 50 ms
  for capture and under 300 ms for the brief on a 50 MB inbox, under 200 ms for SessionEnd with its spawn, under 50 ms for a prompt that recalls 3 facts from a recall index near its 512 KiB bound, and the MCP server's initialize, search, show and status figures.
  The tests themselves never depend on machine speed.
- **Releases** (`internal/release`): on macOS, `scripts/release.sh` run in a temporary repo with a bare origin, a fake gh and a fake signing program, on a new pseudo-terminal or none: each failed check with its fix, and in a workflow as an annotation, the tag's name, a dry run, a plan with no
  signing setup or terminal and what it writes for the workflow, a declined question, a published release and a wrong one, with no git environment from outside reaching it; on any system, every action in every workflow pinned to a commit, the same files summed, attested and published by the release
  workflow as the script and Cut a release check, Cut a release's run name, jobs, conditions, environment, permissions, time limits and one forceless tag push, no job in either workflow named with an expression, the tag job going on without a push only for origin's annotated tag on the planned commit
  with the planned notes, the release workflow's two triggers and the run's link in a called run's notes, one provenance signer everywhere with each way's source ref, and one tag pattern everywhere, agreeing with `ValidTag`; and `scripts/LICENSES.txt` holding the licenses of eva-md, of each module
  built into it and no other, and of Go, and each notice in Go's own code built into the binaries that asks to go with them (`cmd/eva-md`).
- **okf upgrade gate:** the golden corpus round trip, plus a re-save of a copy of the live wiki with a clean `git diff`.
- **Crash safety:** killing the update mid-run leaves the page and watermark unchanged, and the next run succeeds.
- **Search index:** the segment format read back exactly, across dictionary and location blocks; every single byte changed in a segment caught, and a fuzz test of the reader; refreshes, budgets, generations, merges equal to a fresh build, pruning and catch-up; nothing off the record or forged ever
  indexed; scores equal whether a line is scored from the index or from its text; every search test but the two cut short by a deadline run with and without the index, with the same hits; the fallback scan, tails, stale and damaged files, the top-up's thresholds and a fault on the index's path; 8 writers appending to shared session files with a refresh after each step; and 1,000 session files.
- **MCP server:** the protocol and its errors over pipes, the deadlines, cancellation and the abandoned-call limit, the off states, the fixed tool list, the instructions and their warnings, the three tools against a planted home, inbox and wiki, a real child process found by a test, and a hash of
  the home folder that 30 calls leave unchanged, with more unindexed chat in scope than `eva-md search` would index.

---

<!-- v1 docs/verification.md 046 -->
The tests check what eva-md does, never how fast the machine is, so a slow or busy machine, a slow disk or the race detector can't fail them.
The time limits that protect a host are variables the tests raise once, before any test runs: git's time and the wait for its output, for an identity, a brief's live git, commit subjects, the wiki's commits, setup's reads of a folder or a repo, and the checks of a system page's code (a pass, show's
and a write's check and a brief's counts); gh's and glab's time and the wait for their output; a headless model run's wait for its output; the read of `codex features list` before a Codex run; the time of a whole brief, show or search, an install or uninstall, setup's install and its health check,
and the doctor's and status's checks; the 50 ms a recovery pass has to save its cursor past its deadline; host commands; the wiki lock's wait; the doctor's scans and its probe of the MCP server; upgrade's lookup of the latest release, each of its downloads and its run of the downloaded binary's `eva-md version`.
`internal/testlimits` raises every exported one for the packages that can import it, and the release stand-in the end-to-end tests build raises `cmd/eva-md`'s own limits and the exported ones as it starts (`raiseTimeLimits`).
Hook tests lengthen every hook limit with `Slack`, and the command-line tests through `hookSlack`; the MCP server's tests and the command-line tests lengthen its call deadlines, grace and judgement time with `mcp.Slack`.
A test of a limit itself makes the slow side block until the test releases it or the limit's context ends, or ends the limit's time at an exact point, such as once a fake has logged the call it hangs in, and bounds wall-clock time from above only with a hang guard of 30 s or more.
A test that needs a short limit gives it to one value only, such as a second wiki handle whose lock wait is short, never by changing a package variable.
No sleep or time window decides an outcome, or what a test can catch, and timings are logged rather than bounded.
The end-to-end tests' git config turns off the maintenance git starts in the background after a commit or a push, whose lock stays in the objects folder after the command returns, so the walks that show a wiki and its remote unchanged never see a file come and go (`TestFakeMacStartsNoMaintenance`).
To try a slow machine, put a `git` first on PATH that runs `/bin/sleep 0.3` before the real git: it reproduces the failures the GitHub runner showed before the limits were raised in tests.

<!-- /v1 -->

## Must keep

The must-keep items of [findings.md](../v2/findings.md) this component touches, numbered from K01 in each list, and the tests that hold them.
An item that tests hold only in part ends with what of it no test holds, as `(untested: ...)`.

| Id | Item | Tests |
| --- | --- | --- |
| EVL-K07 | Tests run in a temporary home with the environment's git configuration cleared, never on the real machine (untested: the temporary home) | internal/testgitenv:TestClearUnsetsOnlyGitConfigAndIdentityFromTheEnvironment |
| EVL-K08 | Tests never depend on machine speed, and a latency gate lives outside go test | none yet |
| EVL-K15 | The parsers have fuzzers: the row scan, the segment reader, the page parse and scan | internal/catchup:FuzzRowScan, internal/searchindex:FuzzSegmentReader, internal/threadpage:FuzzParse, internal/threadpage:FuzzScanner |

## Since the split

No change yet.
