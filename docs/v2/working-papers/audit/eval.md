# eva-md audit: evaluation and testing

Part: the accuracy suite, the live accuracy run, the latency benchmark, CI and the test strategy.

Scope read in full: `internal/update/accuracy/{scripts.go,measure.go}`, `internal/update/accuracy/live/live.go`, `cmd/live-accuracy/main.go`, `scripts/benchmark.py`, `.github/workflows/ci.yml`, `scripts/linux-check.sh`. Tests read enough to know their reach: `internal/update/accuracy/accuracy_test.go`, `internal/update/accuracy/live/live_test.go`, `internal/update/accuracy_test.go`, `internal/update/accuracy_live_test.go`, `internal/update/knowledge_accuracy_test.go` (head and outline), `internal/update/export_test.go`, `internal/update/guarantees.go`, `cmd/live-accuracy/main_test.go` (outline and the real-runner test), `internal/knowledgepage/recall_test.go` (the precision fixture and test). Docs: `docs/verification.md` (lines 3, 16, 18, 36-38, 44-45, 57-59, 62-160), `docs/design.md` 1227-1324, `docs/known-limitations.md` opening and lines 145-208.

All paths below are relative to `the repository`. Nothing was run but read-only commands (grep, wc, awk, git log).

The question behind this part: **how would anyone know whether eva-md's memory is good, getting better or getting worse?** Short answer: today nobody can. The repository proves, very thoroughly, that the *code around the model* does what its design says. It has no instrument that measures the *memory* (what a later chat gets, whether it is right, whether it helps, what it costs), and the one instrument that touches a real model has never been run and would not record its result if it were.

---

## 1. How it works

### 1.1 The accuracy suite (offline)

**Data model** (`internal/update/accuracy/scripts.go`):

- A `Fact` is a planted fact: codename, speaker, chat, time, `Until` (validity end), `Replaces`, the tag a correct page gives it, the section it belongs in (`decisions`, `next`, `open`, `timeline`, or empty for "no page may assert"), and flags `Off` (off the record), `Pasted`, `Cond` (scripts.go:28-43).
- A `Script` is one history: turns by chat, facts, and `Passes` (the order the job processes chats, with `Kept` for the out-of-order guard) (scripts.go:51-65).
- There are 12 scripts, 29 planted facts, 38 `Say` turns and 21 passes (scripts.go:69-74; counts by grep; `TestScripts` pins 12/21 at accuracy_test.go:47).
- `Script.Chat(i, host)` renders a chat into `inbox.Entry` rows as a host's hooks would store them and appends a fixed two-turn filler so the group passes the 600-character minimum (scripts.go:107-136). Every row has project `github.com__you__eva-md`, cwd `projects/eva-md`, origin `hook` (scripts.go:138-141).
- `Say` sets every fact's branch to `"main"` (scripts.go:99). The "branch" attribute the docs list for each fact never varies.

The 12 failure modes: assistant's suggestion vs user's decision (with an off-the-record fact); weakened event date; reversal; correction; conditional fact; pasted text posing as a decision; host prompt that must never become stated; item accounting across two passes; two-session acceptance; the replace question and its exit; overlapping same-day sessions processed out of order; an assistant's suggestion surviving as a proposal (scripts.go:169-286).

**Scoring** (`internal/update/accuracy/measure.go`):

- A fact is "found" when an item in its section contains its codename, case-insensitively (`HoldsCode`, measure.go:37-39; `present`, measure.go:42-49). It "holds" when that item also has the expected tag (measure.go:52-55).
- `Measure` (measure.go:114-193) computes per-history `Metrics`: Recall, Updates, Stale, Tags, Dates, Carried, Duplicates, a `Content` list, and a `Misses` list with kinds speaker / date / content (measure.go:62-93).
  - Off-record or pasted fact: a miss if any Now item contains its codename (measure.go:122-126).
  - Timeline fact: must appear in a Timeline group whose date equals the date it was *said*, with `[tag]:` in the line (measure.go:127-149).
  - A fact whose validity ended must be gone from its section (measure.go:150-158).
  - A conditional fact must contain the literal `"if "` (measure.go:167).
- `Enforce` (measure.go:206-230) checks three script-level obligations "whatever the model wrote": a non-user fact is never a decision unless accepted; a host's fact never carries a user tag; a newer value is not replaced by an older one.
- `CheckInput` (measure.go:241-257) checks the *batch*: no off-record codename reaches it, each on-record fact reaches it verbatim on its branch, and only real entry headers start a line.
- `Tally` (measure.go:281-346) counts a run against the live target: `Meets()` = zero enforceable misses, zero speaker errors, zero date errors, and at most `MaxContentMissed = 2` content misses (measure.go:263, 344-346).

**Offline run** (`internal/update/accuracy_test.go`): for each pass it builds the batch as the job does, runs every *adversarial* canned output through `Validate` and `threadpage.Apply` and checks the validator's expected reaction, then applies the *correct* canned output, which must score perfectly (accuracy_test.go:136-188). The canned outputs are Go literals written next to the validator (accuracy_test.go:192-576). Beyond the 12 fact scripts there are 5 rule histories (accuracy_test.go:932-937), a 12-week tenure case to 40 decisions (accuracy_test.go:963-1036), and 7 knowledge histories saved into a real temporary wiki (knowledge_accuracy_test.go:94-149, 558-561).

`update.Guarantees` (`internal/update/guarantees.go:24-101`) is the generic oracle used after every validated pass: out-of-order sessions leave the Now alone, user tags cite user prompts, user-owned items drop only on the user's word, done cites a result, decisions are never done/answered, every date written is in an entry, no id leaves the page.

### 1.2 The live accuracy run

- Loop: `live.Run` (`internal/update/accuracy/live/live.go:67-77`) walks the scripts; `script` (live.go:80-128) builds each pass's batch for the page the earlier passes produced (live.go:158-165), calls an `Ask`, validates with `update.Validate` (live.go:105), applies with `threadpage.Apply` (live.go:110), checks `broken` (out-of-order guard, `Enforce`, `Guarantees`; live.go:147-154), then `Measure`s and tallies (live.go:122-124).
- A runner refusal (`invalid_output`, `tool_used`) or a validator rejection leaves the page and `as_of` unchanged and counts as one content miss plus whatever the chat's facts then miss (live.go:101-121, 130-140). Any other failure reason (auth, rate limit, timeout...) stops the run unjudged with `*Stopped` (live.go:113-114).
- The offline tests drive the same loop with the canned outputs as the "model" (`internal/update/accuracy_live_test.go:22-73`, via `CannedAnswer` in `internal/update/export_test.go:21-39`).
- `cmd/live-accuracy/main.go` is the only real-model entry: flags `--host` (required), `--spend-model-quota` (required to call anything), `--effort` (default `highest`), `--model`, `--cli`, `--codex-ignore-user-config` (main.go:57-62). Without `--spend-model-quota` it prints the call count and exits 2 (main.go:86-89). It makes a temporary home, one empty run folder per call, and removes them (main.go:90-100, 141-145). Each call goes through the production `update.Runner` with the production `update.Rules` and `update.Schema(page)` (main.go:148-149). A host that fell back from the requested model stops the run (main.go:154-172).
- Cost: 21 model calls per run, each allowed up to 10 minutes (main.go:87; the runner's timeout scales 180-600 s by effort, `internal/update/runner.go:63-64, 337-344`).
- Output: one line per history with the seven metrics, misses and the final page when anything was missed, then a summary with counts against the target and the efforts used; exit 0/1/2 (main.go:180-222). Everything goes to stdout/stderr. **Nothing is written to disk.**
- Isolation is tested: no release or script builds it, only it imports the suite (`cmd/live-accuracy/main_test.go:462, 511`).

### 1.3 The latency benchmark (`scripts/benchmark.py`)

- Runs a built binary against an isolated temporary HOME under `/tmp`, with git config isolated and host env cleared (benchmark.py:120-124). The "installed" binary the hooks spawn is a shell stand-in that only appends its arguments to a file, so no update, model call or commit ever starts (benchmark.py:128-132).
- Sequence: plant 3 exchanges; pad the session file to about 50 MiB with synthetic event lines (benchmark.py:149-166); 10 cross-host startup briefs that must contain the planted codename and stay under 7,000 chars (168-174); 10 briefs with 64 program-run chats (175-201); search before/after `eva-md index` (206-220); 100 Claude Stop hooks with transcript recovery (221-233); a wiki of 200 other projects, config, two days of run log and 60 days of commit records, then 10 briefs (237-289); 100 Codex Stop (292-295); 100 prompts after a brief (296-300); 400 system pages + a 2,000-name index, 100 prompts each expecting exactly 3 pointers (307-332); 30 Stop hooks that spawn and 30 SessionEnd (336-354, checking exactly 62 spawns with maintain and the sweep once each); MCP cold starts and tool calls (72-107, 357); code-note scenarios (404-455); a recall scenario over a recall index filled to just under 512 KiB (458-501).
- p95 is `sorted(values)[int((len(values)-1)*.95)]` (benchmark.py:358). With 10 samples that is the 9th of 10 (the maximum is excluded); with 100 it is the 95th.
- It prints a JSON report with `*_target_met` booleans (benchmark.py:361-401). **It never exits non-zero for a missed target.** It raises only on functional failures (a missing codename, a wrong spawn count, a wrong pointer count...).
- Targets: capture p95 < 50 ms, startup brief < 300 ms, prompt < 50 ms, SessionEnd < 200 ms, MCP initialize < 100 ms, eva_search < 5 s, eva_show/eva_status < 300 ms, MCP exit < 1 s (benchmark.py:365-392).

### 1.4 CI and the Linux check

- `.github/workflows/ci.yml`: triggers are push to main and pull_request only, no schedule (ci.yml:3-6); `permissions: contents: read` (8-9); actions pinned by commit (16, 19). The macOS job, "the test run of record", runs gofmt, vet, `go test ./...`, `go test -race ./...`, and the build (ci.yml:22-29). It does **not** run the benchmark. The Linux job runs `scripts/linux-check.sh` (ci.yml:38-48).
- `scripts/linux-check.sh`: gives the tests a TMPDIR outside `/tmp` (hooks skip temp folders) and, when root, an unprivileged user `eva-md-check` on a committed snapshot of the working tree with a shared read-only module cache (linux-check.sh:72-124); refuses a TMPDIR under a temp folder (126-132); runs gofmt, vet, test, race test, build, benchmark, every step even when one fails, and exits non-zero if any did (134-187). A full run "takes 6 to 10 minutes" (linux-check.sh:79-81). The benchmark step fails only if the script raises (168-176).
- In CI's Linux job the checkout has one commit, so the code-brief timings are skipped and `code_brief_target_met` is `false` with no samples (benchmark.py:413-430, 394-396; docs/verification.md:16).

### 1.5 The test strategy as a whole

- 1,266 `Test` functions, 4 `Benchmark`, 4 `Fuzz` (grep). Test code is about 58k lines against about 58k lines of source; `internal/update` alone has 10,869 test lines for 8,669 source lines (wc).
- Rules, all enforced by convention and partly by tests: tests use a temporary HOME, isolated git config, `testgitenv.Clear()`; tests never depend on machine speed (limits raised once in `TestMain`, timings logged rather than bounded, hang guards of 30 s or more) (AGENTS.md "Rules"; docs/verification.md:18, 62).
- Hosts are fakes: fake `claude` and `codex` shell scripts for runners, a fake GitHub, fake gh/glab, a release stand-in (docs/verification.md:37, 55, 60).
- Tests also gate documentation: design.md must name built commands, AGENTS.md must name every CLI source file, and every verification.md bullet must cite tests that exist (`TestTheDesignNamesTheBuiltCommands`, `TestAgentsNamesEveryFileOfTheCLI`, `TestVerificationBulletsNameTheirPackages`; docs/verification.md:58).

---

## 2. What is measured today, and against what

| Thing | Instrument | Against a real model or host? | Gate? |
| --- | --- | --- | --- |
| Validator guarantees (speaker tags, dates from entries, ids never lost, out-of-order guard) | `update.Guarantees`, `Script.Enforce`, adversarial canned outputs | No, canned | Yes, CI |
| Thread page content: planted facts in the right section with the right tag, replaced values gone | `Script.Measure` | No, canned; live tool exists, never run | CI on canned only |
| Rules pages (5 histories), knowledge pages (7 histories), tenure (12 weeks) | `internal/update/accuracy_test.go`, `knowledge_accuracy_test.go` | No. Offline only, by design (docs/verification.md:91; docs/known-limitations.md:177) | CI on canned only |
| Recall precision/recall | `TestRecallPrecision`: 73 labelled prompts, 36 fixture rows, sizes 1/3/10/36/500 | No model involved; fixture only | Yes: fails under 90% precision or 60% recall (recall_test.go:320) |
| Resume brief content | Deterministic resume suite (planted text present, caps, fences) | No model | Yes, CI |
| "Does a new chat answer with the planted facts" | Manual live checks, update off, three surfaces (docs/verification.md:66-72) | Yes, by hand, once | No |
| Hook latency | `scripts/benchmark.py` | Synthetic inbox, real binary | No: prints, never fails on a miss |
| Real-use hook latency | Doctor "hook times" row from heartbeats (`internal/health/times.go:14-41`) | Yes, but nobody has read it after a week (docs/verification.md:144) | No |
| Update cost | `tokens_in`, `tokens_out`, `duration_ms`, `calls` per pass in the run log (`internal/update/runlog.go:71-94`) | Would be real, once updates run | No; never summed or shown |
| Validator mends per pass | `Report` in each run-log record (`internal/update/validate.go:19-38`; written at `internal/update/pass.go:404`) | Would be real | No; nothing reads it back |

### What has only ever run against canned model output

Everything downstream of the model call: the 12 fact scripts, the 5 rule histories, the 7 knowledge histories, the tenure case, the headless runners (fake CLIs), the update job, `doctor --live`, knowledge promotion, recall over a real index, system-page digests. The docs say so plainly: "No update has run live yet" (docs/verification.md:145; docs/known-limitations.md:176), the live accuracy run "has not run yet" (docs/verification.md:84), and "the update rules at version 4 have not been run by a real model" (docs/verification.md:149). `RulesVersion = 4` (`internal/update/schema.go:16`).

### What docs/verification.md lists as not verified yet (lines 141-160)

Codex desktop app recall; a stretch of real use with zero hook errors and p95 under 50 ms; live updates, the live resume test with the update on, the live accuracy runs, `doctor --live`; the daily signed commit from a detached job; a day's run on a self-hosted GitLab; the digest and recall skills on real hosts; knowledge pages and recall in real use (floor precision on a real index, "whether recalled facts help a chat", whether real system pages make the right siblings); code refs in real use; host signals (entrypoints, paste wrapper, `codex exec`); `inbox clear` on a real inbox; setup with real gh; a deleted install folder on Claude Code; install/upgrade/uninstall from a release beyond one Mac, and no check ever runs the amd64 binary; release edge paths; an upgrade over an installed plugin; a burst of parallel tool calls; Codex `mcp__` tool names; the MCP server on real hosts.

### What a memory system needs measured that nothing here measures

1. **Whether a resumed chat really continues correctly.** No harness. The "Live resume test (the headline)" is five bullets in docs/design.md:1297-1302; grep finds no code, script, build tag or environment switch for it.
2. **Whether recall brings the right knowledge and stays silent otherwise, on real prompts.** Only the self-authored fixture. No production signal either: the per-chat marker counts recalled facts (`internal/hook/pointers.go:54-59`, `internal/hook/recall.go:115`) but nothing aggregates it or relates it to use.
3. **Whether knowledge goes stale or contradicts itself.** The daily lint checks only page size and dangling source refs (`internal/lint/lint.go:1-4`). A recalled line shows `as_of` and nothing about staleness (`internal/hook/recall.go:165-183`).
4. **What it costs in tokens and quota.** Per-pass counts are logged and never totalled; the doctor shows "N of 40 model calls today" and nothing else (`internal/health/background.go:111`). The default is the host's default model at `highest` effort (`internal/config/config.go:196`) for up to 40 calls per host per day (`internal/config/config.go:34`). No measurement supports that default. Injected context per chat (brief up to 8,000/6,000 chars, recall up to 1,000, pointers, the MCP tool list at about 500 tokens) is capped but never measured in use.

---

## 3. What must be kept

1. **Script-first fact model with derived obligations.** Facts carry speaker, time, validity, replacement and expected tag; code derives what must hold. The split between *enforceable* obligations (hold whatever the model writes) and *content* obligations (depend on the model) is the right frame and should survive any rebuild (scripts.go:8-16; measure.go:201-230).
2. **One loop for canned and live answers.** `live.Run` takes an `Ask`; offline tests and the real runner go through the same batch, validate, apply, measure path (live.go:21-25, 67-128; accuracy_live_test.go:22-73). Any new eval should keep "the eval drives the production code path, not a copy".
3. **Adversarial outputs with an expected validator reaction, per failure mode**, including those the validator cannot catch by design, which the metrics must flag (accuracy_test.go:55-61, 152-171).
4. **Honest accounting of refusals and host failures.** A refused answer costs; auth, rate-limit and timeout failures stop the run unjudged rather than polluting the score; a model fallback stops the run so answers are never attributed to the wrong model (live.go:101-121; main.go:154-172).
5. **Quota safety of the live tool.** `--host` and `--spend-model-quota` required, the call count stated before anything runs, a temporary home, nothing of eva-md's read or written, never built by a release, never imported by the product (main.go:66-100; main_test.go:462, 511).
6. **Test isolation.** Temporary HOME, isolated git config, `testgitenv.Clear()`, no reads of the real machine.
7. **Tests never depend on machine speed.** Timings are logged, not asserted; limits are raised once; hang guards only (docs/verification.md:62). A latency gate, when added, must live outside `go test`.
8. **The benchmark's isolation and its functional assertions.** Stand-in binary that only counts spawns; exact spawn count with maintain and the sweep once each; cross-host brief must contain the planted codename; 3 pointers; recall block fenced within 1,000 chars (benchmark.py:128-132, 172, 330, 353, 497).
9. **The latency targets themselves** (capture 50 ms, brief 300 ms, prompt 50 ms, SessionEnd 200 ms).
10. **Recall's precision-first floor as a CI gate**, and the score's independence from index size (`internal/knowledgepage/recall_test.go:255-344`).
11. **The run log holds counts and names only, never chat text** (`internal/update/runlog.go:69-70`). Any new telemetry must keep this.
12. **macOS is the run of record; Linux failures are reported, never patched around** (AGENTS.md; ci.yml:31-37).
13. **CI hygiene**: actions pinned by commit, read-only token (ci.yml:8-9, 16, 19).
14. **Fuzzers on the parsers** (`internal/catchup/scan_test.go:223`, `internal/searchindex/segment_test.go:255`, `internal/threadpage/scan_test.go:187`, `internal/threadpage/property_test.go:17`).

---

## 4. Where it falls short

Ranked by consequence for the goal.

### F1. No end-to-end measure of the product's purpose: does a resumed chat continue correctly? (critical; evaluation; add)

- Evidence: docs/design.md:1297-1302 describes the "Live resume test (the headline)". No code implements it: `grep -rn -i "live resume"` over `*.go`, `*.py`, `*.sh`, `*.yml` returns nothing, and there is no build tag or env switch for live tests. The manual checks that did run planted "a few made-up facts" and asked for them back, with the update off (docs/verification.md:66-72); the desktop-app check "does not prove eva-md was the only source" (docs/verification.md:72).
- Why it matters: a memory system's value is that the *next* chat behaves as if it remembered: it picks up the next step, respects decisions, does not re-ask settled questions, does not act on stale state. "The brief contains the string" is a necessary condition, not the outcome. With no outcome measure there is no way to tell whether a redesign of capture, summarization or recall made the product better.
- Direction: build an agent-in-the-loop continuation harness as a first-class subsystem. Scripted multi-session scenarios on real host CLIs run headlessly in an isolated HOME with eva-md installed; session A does work, session B (new chat, /clear, compaction, other host) is given a task whose correct execution depends on A. Grade with deterministic checks (files touched, commands run, a decision obeyed, a question not re-asked) plus a rubric-graded judge for the rest. Always run three arms: no memory, the host's native memory, eva-md. Report deltas, not absolutes.
- Basis: verified in code (absence) and docs.

### F2. The whole model-in-the-loop path has never met a real model (critical; evaluation; evolve; known)

- Evidence: docs/verification.md:84 ("It has not run yet"), :145, :149; docs/known-limitations.md:176-177. Every "accuracy" result in the repo comes from Go literals written by the validator's authors (accuracy_test.go:192-576). "The correct outputs must score perfectly" checks that the fixtures and the scorer agree with each other.
- Why it matters: the update rules prompt (about 4 KB, version 4), the JSON schema, the 60 KB chunking and the validator's mend-or-refuse behaviour are all untested hypotheses about how Claude and Codex answer. The first live run is as likely to reveal a systematic refusal (`invalid_output` on most passes) as a pass. Until then nobody knows whether thread, rules and knowledge pages get written at all.
- Direction: treat the first live runs as the next engineering milestone, then make them routine: a scheduled, budgeted job on a Mac runner with logged-in hosts, per host and model and effort, N trials, results stored.
- Basis: docs state it; the canned nature is verified in code.

### F3. The accuracy score is keyed on codenames and cannot see wrong values, dropped detail, or invented items (high; evaluation and memory-quality; rebuild the scorer)

- Evidence:
  - A fact is "found" if its codename appears in an item in the right section (measure.go:37-49). "Store pages as XML, OSPREY-J" scores the same as the truth.
  - The "weakened event date" script plants "The freeze is on 3 March 2026, FROST-9" (scripts.go:182-183); `Measure` checks only that FROST-9 is in `decisions` with tag `stated` (measure.go:159-175). An item that drops the date entirely is perfect. The offline adversarial case covers an *invented* date (accuracy_test.go:221-230), which the validator catches; a *dropped* one is invisible to both.
  - The conditional check is the substring `"if "` (measure.go:167).
  - There is no precision measure. `Metrics` has no count of items the script did not plant (measure.go:62-78). A model that adds ten plausible but unsupported Now items scores perfectly unless one carries an off-record or pasted codename.
  - Summary, last_chat, Done lines and unknowns are never scored.
  - Codenames are themselves an aid no real chat has: every fact is flagged by a unique all-caps token.
- Scale and realism: 29 facts in 38 turns; each chat is about 1 KB, against a production chunk of up to 60 KB (`internal/update/input.go:223`); the only distractor is a fixed filler that says "nothing in them changes the plan" (scripts.go:109-110); one project, one branch (scripts.go:99, 140), English only, almost no tool output.
- Why it matters: the failure modes that hurt in real use (a decision recorded with the wrong value, a date or condition lost, noise items crowding the Now, a 50 KB tool-heavy session where the one decision is on line 700) are the ones the score cannot see.
- Direction: keep the script-first model and the 12 failure modes as regression classes; rebuild scoring as fact-level grading with value slots (each fact has required attributes: value, date, condition, owner) checked by normalised match where possible and a rubric judge where not; add a precision side (every page item must be attributable to a planted fact or be counted as noise); generate scenarios at realistic size with distractors, tool output, branches and long gaps; drop the dependence on codenames for anything but locating.
- Basis: verified in code.

### F4. The live run cannot show a trend: one sample, pass/fail, nothing stored, the most useful signals thrown away (high; evaluation; evolve)

- Evidence:
  - One run is 21 calls, each asked once; the verdict is a boolean with a tolerance of 2 content misses out of 29 facts (measure.go:263, 344-346). The docs concede "One run is one sample of a model's answers" (docs/verification.md:91).
  - Results go to stdout only (main.go:180-222). No output file, no history, no comparison with a previous run.
  - The validator's `Report` for each live answer (how many tags it downgraded, operations it refused, dates it removed, ids it carried) is used only for `KeptNow` and `Carried` (live.go:111-112, 149-150) and never printed. That is the direct measure of how far the real model is from the rules.
  - Token counts are dropped: `caller.ask` logs seconds and effort and nothing of `res.TokensIn`/`TokensOut` (main.go:148-174).
  - The live loop builds batches with no rules and no knowledge on file (`update.Options{Branch, AsOf}`, live.go:164) and calls `update.Validate`, which discards knowledge operations (`internal/update/validate.go:88-91`). Whatever a real model promotes to a rule or to knowledge is neither applied nor judged. Rule and knowledge histories "run offline only" (docs/verification.md:91).
- Why it matters: "getting better or worse" needs a number with a variance, a history and a cost beside it. A flaky boolean invites rerunning until green.
- Direction: an eval runner that takes a matrix (host × model × effort × rules version), runs k trials, records per-fact outcomes, validator mend counts, tokens, latency and refusals as structured rows in a results store kept in the repo or an artifact, and reports rates with confidence intervals and a diff against the last accepted baseline. Rules-prompt changes then become A/B-able.
- Basis: verified in code. The "one sample" part is in known-limitations.md:177; the rest is not.

### F5. Recall quality is measured only on the fixture that tuned it, and not at all in use (high; recall-quality; rebuild the eval; known)

- Evidence: the floor "was chosen on the labelled prompts in recall_test.go" (`internal/knowledgepage/recall.go:27-37`); the same prompts are the test (recall_test.go:123-203, 255-325). There is no held-out set. The 500-row case pads with nonsense words built to share no token with any prompt (recall_test.go:225-246), so it tests arithmetic stability, not a large real index with overlapping vocabulary. Thresholds: 90% precision, 60% recall (recall_test.go:320). Limits already conceded: no stemming, no paraphrase, "three rare words in common can recall an unrelated fact" (docs/known-limitations.md:155-156). "Whether recalled facts help a chat" is unmeasured (docs/verification.md:149).
- In production: the hook records how many facts a chat was sent (`internal/hook/pointers.go:54-59`) and nothing else. No silence rate, no record of whether the agent then read the page or contradicted the fact.
- Why it matters: recall is where memory becomes visible. A wrong recalled fact is noise in the agent's context with the authority of "the user's words"; a missed one is the product not working. Lexical matching with a floor tuned on 36 rows is a guess about both.
- Direction: a retrieval eval that is independent of the algorithm: labelled (prompt, corpus, relevant ids) sets with paraphrases and hard negatives, split into tuning and held-out; metrics precision@k, recall@k, silence rate on irrelevant prompts, injected characters per prompt. Use it to compare the current lexical scorer with candidates (stemming, hybrid lexical + local embedding, model rerank off the hot path). Add local, text-free use signals (a recalled id later opened, superseded or contradicted in the same chat) for the in-product loop.
- Basis: verified in code; listed in known-limitations.

### F6. Nothing measures the memory corpus itself: staleness, contradiction, duplication, promotion (high; memory-quality; add)

- Evidence: the daily lint checks page size over 512 KB and source refs the inbox no longer resolves (`internal/lint/lint.go:1-4, 29-31`). Knowledge supersession is tested only for an entry the pass's own input shows (knowledge_accuracy_test.go:164-180). No check compares active knowledge pages with each other, with rules, or with the code they cite. The promotion bar ("would still matter in six months") is "the model's judgment" (docs/known-limitations.md:147), a pass keeps at most 5 knowledge changes and the rest "are counted over the cap and lost" (docs/known-limitations.md:171). The validator `Report` and `KnowledgeCounts` land in the run log (`internal/update/pass.go:404`) and no reader exists (`grep -rn "\.Validation"` shows only the two writers).
- Why it matters: a memory that only grows degrades. Without corpus-level measures nobody will notice knowledge contradicting itself, facts about deleted code being recalled as current, or a promotion rate of zero.
- Direction: a corpus audit as part of the daily job and of the eval: contradiction and near-duplicate clusters among active pages, age and code-drift of every statement, promotion funnel (candidates seen, kept, refused by reason, lost to caps), page growth; surfaced as a small set of health numbers and trended.
- Basis: verified in code for lint scope and unread reports; "no corpus audit" inferred from the absence of any such code in the paths read.

### F7. Cost is logged but never measured, and the default is the most expensive setting (high; invisibility; add)

- Evidence: default effort `highest` for every host (`internal/config/config.go:196`); 40 calls per host per day (`internal/config/config.go:34`); each call up to 60 KB of input plus rules, schema and the whole thread page, with a timeout of up to 600 s (`internal/update/runner.go:63-64`). `tokens_in`/`tokens_out` are written per pass (`internal/update/runlog.go:82-83`; `internal/update/pass.go:393`) and appear only in the job's own log line (`internal/update/pass.go:655-656`). The doctor reports calls, not tokens (`internal/health/background.go:111`). The live accuracy tool has `--effort` and `--model` but reports no tokens (main.go:153-174), so even a manual sweep would not give a cost/quality curve.
- Why it matters: "invisible" includes not eating the user's subscription. The updates share rate limits with the user's own chats (docs/known-limitations.md:180). Nobody knows whether `low` effort on a small model gives the same pages at a tenth of the quota.
- Direction: a cost ledger: tokens in/out, calls and wall time per pass, per day and per project; injected context characters per chat by source (brief, recall, pointers, tools). Show it in `status`. Put cost beside quality in every eval report and pick defaults from the measured frontier.
- Basis: verified in code.

### F8. The latency targets are not gated anywhere (high; evaluation; evolve)

- Evidence: `benchmark.py` computes `*_target_met` and prints it; no code path exits non-zero on a miss (benchmark.py:358-401). The macOS job, the run of record, does not run the benchmark (ci.yml:22-29). The Linux job does, on an OS the product does not ship for, and fails only on a functional error (linux-check.sh:168-183). The startup-brief p95 is the 9th of 10 samples (benchmark.py:168, 358). The figures in docs/verification.md:97-115 are hand-copied ("about 25-30 ms"). design.md says so: "run by hand before a change is finished, not a test" (docs/design.md:1316). The padding is event lines "which are never indexed, so the figures say little about a real inbox" (docs/verification.md:137). Real-use p95 lives in a doctor row nobody has reviewed (docs/verification.md:144), and that row leaves out process start (`internal/health/times.go:14-17`).
- Why it matters: AGENTS.md makes "hooks stay fast" a rule. A hook that drifts from 30 to 80 ms adds latency to every prompt of every chat, and today that lands silently.
- Direction: keep the benchmark's scenarios; make it a regression gate on macOS runners with enough samples, interleaved old/new binaries, relative thresholds against a stored baseline, and a hard ceiling at the targets; store each run's JSON. In the product, record full-process hook time histograms locally so real-use p95 is a number in `status`.
- Basis: verified in code.

### F9. The oracle is not independent of the system it judges, and there are two scoring systems that disagree (medium; evaluation and architecture; rebuild the scorer)

- Evidence: `update.Guarantees` lives in the update package and is built from the validator's own helpers: `(&validator{b: b}).ref(...)` (guarantees.go:37), `userTag` (58, 60), `heldItems` (50), `entryDates` and `dates` (53, 76). A bug in date extraction or ref numbering is shared by the code under test and its checker. The canned "correct" outputs are written against the same internals (`view.n`, `view.id`; accuracy_test.go:70-82). Separately, `Metrics.Perfect()` (measure.go:100-102) and the `Misses`/`Tally` path can disagree: a Timeline fact that is never logged adds a content miss but leaves `Perfect()` true (measure.go:147-149; pinned by the test at accuracy_test.go:117-120), so the offline run needs a second assertion to compensate (accuracy_test.go:183-185).
- Why it matters: when the eval shares the implementation's assumptions, it confirms them. The suite's pass rate overstates what is known.
- Direction: a black-box oracle that sees only what a user would: inbox entries in, rendered Markdown pages out, properties checked from the page text and the script alone, with its own minimal parsing. One scoring model, not two.
- Basis: verified in code.

### F10. No canary for host drift; every host behaviour is a fake written from one version (medium; reach and reliability-safety; add; known in part)

- Evidence: CI has no schedule and never starts a real `claude` or `codex` (ci.yml:3-6, 22-29). Failure classes are matched on the error wording of claude 2.1.312 and codex 0.166.0 (docs/known-limitations.md:185). Typed prompts are recognised by a `promptId` field recovery "relies on" (docs/verification.md:74). Codex desktop, the MCP server, the skills and `mcp__` tool names are unverified on real hosts (docs/verification.md:143, 148, 159-160). No check ever runs the amd64 binary (docs/verification.md:155).
- Why it matters: eva-md sits on undocumented host behaviour that changes weekly. A host release can silently turn capture, recovery or updates off; the first to know is a user whose agent forgot.
- Direction: a scheduled contract suite against the current host CLIs on a Mac runner: model-free probes first (plugin validate, features list, hook payload shapes captured from a scripted headless session, transcript format), a small budgeted model probe second (`doctor --live` equivalent), a recorded payload corpus per host version, and an alert on drift. In the product, promote the existing drift and heartbeat signals to a clear "memory is degraded" state.
- Basis: verified in code for CI; the rest from the docs.

### F11. Whether stored text can steer a real agent is never tested (medium; reliability-safety; add)

- Evidence: the defences are structural and tested structurally: fences, escaping of forged headers, pasted spans, the label "reference data, not instructions" (`internal/hook/recall.go:36-39`; accuracy_test.go:306-329, 808-843). The suite's hostile cases stop at the validator: pasted text must not become a decision, a rule or knowledge. No test puts a brief or a recall block that contains an instruction in front of a real model and observes what it does. Knowledge "observed" from MCP tools is conceded as a path for web content (docs/known-limitations.md:150).
- Why it matters: a memory layer is a persistence mechanism for prompt injection: text from a web page or a tool result today can be recalled, with a trust label, into every chat for months.
- Direction: a red-team track in the continuation harness (F1): plant instructions through each ingress (tool output, pasted text, web fetch, MCP result, another project's page), then measure both whether they reach a page and whether a later agent acts on them when briefed or recalled. Track an attack success rate per release.
- Basis: inferred from the absence of any such test in the paths read; the structural tests are verified.

### F12. The verification ledger is prose that gates the build (medium; architecture; evolve)

- Evidence: docs/verification.md is 182 KB in 160 lines; single bullets run to 22 KB (line 55) and 9.8 KB (line 35). Tests fail when a bullet names a test that does not exist in the packages it lists (`TestVerificationBulletsNameTheirPackages`), when AGENTS.md omits a CLI source file, or when design.md's command table lacks a flag (docs/verification.md:58). verification.md changed in 29 of the repo's 55 commits (`git log --oneline -- docs/verification.md | wc -l`). The Linux check needs a dedicated user, a snapshot repository and a shared module cache because about 20 tests fail as root and hooks skip `/tmp` (linux-check.sh:11-27; "about 20 tests" at line 15).
- Why it matters: every behaviour change costs a prose edit that a machine then polices, while the question "which guarantee has no live evidence" still has to be answered by reading 180 KB. A team of engineers working in parallel will collide in these files on every change.
- Direction: keep the guarantees and the tests; replace the prose ledger with data: a list of guarantee ids (the table at docs/design.md:1229-1245 is the seed), each test tagged with the ids it covers, each id carrying its evidence level (unit, offline end-to-end, live on host X at version Y, in real use). Generate the human page from it. That same table is where eval results from F1-F8 attach.
- Basis: verified in code and git history.

---

## 5. Essential and accidental complexity

**Essential.** Grading a non-deterministic model against a stateful, multi-pass document is hard; the enforceable/content split, the refusal accounting and the same-loop design are real answers to that. Isolating tests from a developer's machine and from machine speed is essential for a tool that writes to `~/.claude`, `~/.codex` and a git remote. Quota-spending tools need explicit consent.

**Accidental.** Codename-substring scoring (chosen to make a deterministic check possible, now the ceiling on what can be measured). Two overlapping scoring structures (`Metrics` fields and `Misses`/`Tally`). An oracle built from validator internals. A benchmark that computes verdicts and discards them. A 182 KB prose ledger policed by tests. The Linux snapshot-user machinery, which exists because tests assume a non-root macOS-like environment.

---

## 6. Overall call: keep the frame, rebuild the instrument, add the missing layers

- **Keep**: the script-first fact model, the 12 failure modes, the same-loop offline/live design, refusal accounting, quota guards, test isolation rules, the benchmark's scenarios and isolation, the recall floor gate, CI hygiene.
- **Rebuild**: the scorer (fact-level with value slots and a precision side, black-box against rendered pages) and the scenario corpus (realistic size, distractors, branches, tools).
- **Evolve**: the live runner into a matrix runner with trials, stored results, mend counts and cost; the benchmark into a macOS regression gate; the verification ledger into data.
- **Add**: the continuation harness with baselines (the headline measure), the retrieval eval, the corpus audit, the cost ledger, the host-drift canary, the injection red-team track.

The reason: what exists is a strong *correctness* suite for the deterministic code and a well-designed but unused and under-powered probe for the model. None of it answers the owner's question. An evaluation system is the precondition for the redesign: without it, "rebuild rather than patch" has no way to show that a rebuild was better.

A proposed shape, as a direction and not a spec:

1. **Level 0, deterministic** (today's `go test`): unchanged, plus the black-box oracle.
2. **Level 1, model-in-the-loop, offline corpus**: matrix runner over generated scenarios; per-fact precision/recall/value fidelity, validator mend rates, tokens, latency; k trials; stored results; run on demand and nightly within a budget.
3. **Level 2, agent-in-the-loop continuation**: scripted multi-session tasks on real hosts, three arms, deterministic plus rubric grading, injection track.
4. **Level 3, in-product, local, text-free signals**: cost ledger, hook latency histograms, recall use signals, corpus health, degraded-state detection; shown in `status`, never sent anywhere.
5. **One results store and one guarantee table** that all four levels write to, so "better or worse than last release" is a query.

---

## 7. Open questions

1. Has any live update or live accuracy run happened since the docs were written, with results kept outside the repo?
2. What may evaluation spend, per night and per release, and on whose account: the maintainer's subscriptions, or API keys kept for evaluation (the product's runs deliberately drop API keys)?
3. Is there, or can there be, a Mac runner with logged-in Claude Code and Codex for scheduled live runs and host canaries?
4. May real chats, redacted and opt-in, seed an evaluation corpus, or must every scenario be synthetic?
5. Which baselines count: no memory, each host's native memory, other memory systems, public long-term-memory benchmarks?
6. Is a model-graded judge acceptable for the parts deterministic checks cannot reach, and which model judges?
7. Is "local only, counts and names, never text, no network" the hard limit for in-product quality signals?
8. Is the `highest` default effort a decision or a placeholder until cost is measured?
9. Should rule and knowledge histories join the live run before or after the scorer is rebuilt?
10. Who owns the eval's results store and the accepted baseline when a team works in parallel?

---

## Verification

A second reader tried to refute each finding against the code. Read in full: `internal/update/accuracy/{scripts.go,measure.go,accuracy_test.go}`, `internal/update/accuracy/live/live.go`, `cmd/live-accuracy/main.go`, `scripts/benchmark.py`, `.github/workflows/ci.yml`, `scripts/linux-check.sh`, `internal/update/guarantees.go`. Read in part: `internal/update/{validate.go,schema.go,runlog.go,pass.go,accuracy_test.go,accuracy_live_test.go}`, `internal/knowledgepage/{recall.go,recall_test.go}`, `internal/hook/{pointers.go,recall.go,resume.go}`, `internal/health/{health.go,times.go,background.go,knowledge.go}`, `internal/lint/lint.go`, `cmd/eva-md/docs_test.go`, `docs/verification.md` 62-96 and 138-160, `docs/design.md` 1225-1325, `docs/known-limitations.md` 143-190, and `git log` and `git blame`. Nothing was run but read-only commands.

No finding is refuted. Eleven are confirmed, four of them at a lower severity than given, and one is partly right.

| Id | Verdict | Severity given | Severity after checking |
| --- | --- | --- | --- |
| eval-1 | confirmed | critical | critical |
| eval-2 | confirmed | critical | critical |
| eval-3 | confirmed, and it errs the other way too | high | high |
| eval-4 | confirmed, one wording corrected | high | high |
| eval-5 | confirmed, one detail corrected | high | high |
| eval-6 | confirmed, with what does exist named | high | high |
| eval-7 | confirmed | high | medium |
| eval-8 | confirmed | high | medium |
| eval-9 | confirmed | medium | medium |
| eval-10 | partly | medium | medium |
| eval-11 | confirmed | medium | medium |
| eval-12 | confirmed | medium | low |

### eval-1: confirmed, critical

- `docs/design.md:1297-1302` is the only form the live resume test has. The repository's build tags are `evamd_testlimits`, `race`, `!race`, `!darwin` and `!linux`; none switches a live test on, and no environment variable does (`grep -rn 'os.Getenv("EVA'` finds only `EVA_MD_INNER` and test helpers).
- The manual checks at `docs/verification.md:70-72` did run, with the update off. `git blame` puts those three bullets in the first commit, `af3e066` of 2026-10-30, unchanged since (see missed finding 2). They strengthen the finding: the only live evidence is older than most of the brief.

### eval-2: confirmed, critical

- `docs/verification.md:84` ("It has not run yet"), `:145`, `:149` and `docs/known-limitations.md:176` say it. `internal/update/accuracy_live_test.go:34` feeds `live.Run` with `update.CannedAnswer`.
- One addition: the rules prompt is pinned only by its length and its first line (`internal/update/input_test.go:838, 861`). No offline test depends on its wording, so a change to the prompt is invisible to CI. Until live runs exist, the prompt has no test at all.

### eval-3: confirmed, high

- `measure.go:37-49` and `:159-175` are as cited. `Guarantees` checks every date an update writes against the entries (`guarantees.go:75-81`), never that a date in an entry was kept, so "Freeze releases, FROST-9" scores as perfect. `Metrics` (`measure.go:62-78`) has no count of items the script didn't plant.
- The off-record and pasted check reads Now items only (`measure.go:122-126`), so a pasted "decision" written to the Timeline, the summary or a Done line is not counted.
- The score errs in the other direction too, which the finding doesn't say. `present` looks only in the one section the script names and only for the codename (`measure.go:42-49`). A model that files "We use Go 1.26, MARTIN-4" as a Timeline finding and not a decision, or drops a trailing codename, gets "is missing". Three such misses in 29 facts fail the run (`measure.go:263`). See missed finding 3.

### eval-4: confirmed, high

- `cmd/live-accuracy/main.go` has no output flag; `caller.ask` (`:140-175`) never reads `res.TokensIn` or `res.TokensOut`, which `update.Result` carries (`internal/update/runner.go:87`). `live.go:111-112` uses the `Report` for `KeptNow` and `Carried` only. `update.Validate` drops the knowledge operations `checkOutput` keeps (`validate.go:88-91`), and `live.go:164` gives `Build` no rules or knowledge on file.
- Correction to "pass or fail": the tool prints seven metrics per history and the counts of each kind of miss (`main.go:180-222`). They are text on stdout, with no stored baseline, so the trend claim stands.
- The severity holds because of what is never judged: rules reach every later chat and knowledge is recalled into prompts, and neither has a live measure even once the tool runs.

### eval-5: confirmed, high

- `recall.go:27-37` says the floor was chosen on the prompts in `recall_test.go`, and `TestRecallPrecision` measures on the same prompts. `neutralRows` builds the 500-row index from made-up words no prompt holds (`recall_test.go:225-246`).
- Correction: the hook keeps more than a count. `promptState.Pages` holds the refs of the pages recalled or pointed at in the chat (`internal/hook/pointers.go:58`, `recall.go:114`). It is there to avoid repeats, lives in a per-chat marker, and nothing aggregates it, so the conclusion holds. It also means a text-free use signal has its raw data already.
- The fixture does hold near misses and reworded prompts (`recall_test.go:153-183`); what it lacks is a held-out part. The gate (90% precision, 60% recall) sits well under the 100% and 88-93% the comment reports, so a real regression can pass.

### eval-6: confirmed, high

- `internal/lint/lint.go` reads thread pages only. `rec.Validation` has two writers (`internal/update/pass.go:404, 602`) and no reader. `recallLine` shows the row's `as_of` and nothing else about age (`internal/hook/recall.go:165-183`).
- What does exist: the doctor counts knowledge pages by kind, retired pages and pages the recall index left out (`internal/health/knowledge.go:53-72`), and system pages get a "code moved N commits" status (`internal/coderef`, `internal/health/code.go`). Knowledge pages store the code paths they cite (`internal/knowledgepage/save.go:424-437`), but nothing compares them with the code or shows drift at recall. None of this measures contradiction, duplication or promotion rate.
- The corpus is empty today, since no live update has promoted a page, so the risk is ahead, not present.

### eval-7: confirmed, medium

- `Config.EffortFor` returns `highest` when nothing is set (`internal/config/config.go:186-196`); `DefaultDailyCap` is 40. Tokens appear only in the job's log line (`internal/update/pass.go:655-656`). The doctor row shows "N of 40 model calls today" (`internal/health/background.go:111`); no file under `internal/health`, `internal/mcp` or `cmd/eva-md` reads `tokens_in`.
- Lower severity, for two reasons. The daily cap bounds the spend and is shown. And the default is a recorded decision (`docs/decisions.md:107`), which AGENTS.md says not to reopen without a strong reason: the action is to measure first, then reopen it with the numbers.

### eval-8: confirmed, medium

- `benchmark.py` raises only on a functional failure; every `*_target_met` is printed (`:361-401`). `p95` with 10 samples is index 8, the 9th value (`:358`). The macOS job has no benchmark step (`ci.yml:22-29`). The doctor's hook-times row is "for information only and never FAIL" and keeps at most the last 20 runs of an event (`internal/health/times.go:13-20`).
- Lower severity: hooks have hard internal deadlines, so drift is bounded; the benchmark is in AGENTS.md's checklist before a change is finished, so a miss is printed, only not enforced; and a gate must live outside `go test` by the project's own rule. The benchmark and the doctor row also use different percentile rules and count different things (one includes the process start), so the two numbers can't be compared as they are.

### eval-9: confirmed, medium

- `guarantees.go:37` calls `(&validator{b: b}).ref`, `:50` `heldItems`, `:53` `entryDates`, `:76` `dates`. `accuracy/accuracy_test.go` pins a case where `Perfect()` is true beside a "is not logged" miss, and the offline run asserts both (`internal/update/accuracy_test.go:183-187`).
- Scope: only `Guarantees` shares code. `Script.Enforce` and `Measure` import `threadpage` alone. The shared helpers have tests of their own with hand-written results (`internal/update/tokens_test.go:71`, `validate_test.go:43, 288`), so a shared bug is less likely than the finding suggests.
- The stronger reason to rebuild it: an oracle made of the validator's internals can't judge a rebuilt validator. A redesign needs the black-box oracle first.

### eval-10: partly, medium

- The CI facts hold: `ci.yml:3-6` has push and pull_request only, `cut-release.yml` is `workflow_dispatch`, `release.yml` runs on tags; no job starts a real host. `docs/known-limitations.md:185` and `docs/verification.md:155` are as cited.
- The impact is overstated. Failure is not silent in the product: the chat-start notice reports a host whose recent activity has no capture heartbeat, failing hook runs, transcripts that recovery no longer recognizes, and failing updates (`internal/health/health.go:98-117`, `internal/catchup/drift.go`). The full report says so; the finding's impact line doesn't.
- What is missing is a check before users are affected, and drift the watchdogs can't see, such as a reworded error turning a rate limit into `invalid_output`.

### eval-11: confirmed, medium

- It holds by construction: no test in the repository starts a real model. The cited tests check fences, quoted headers and paste marks.
- One canned case is worth knowing: a pasted "decision" cited to the user's own prompt, pasted without the host's paste wrapper, passes the validator as `stated` (`internal/update/accuracy_test.go:321-329`, marked `content: true`). The suite leaves that case to the model and the metrics, which is exactly the path no real model has been shown.

### eval-12: confirmed, low

- `docs/verification.md` is 182,435 bytes in 160 lines; the longest, line 55, is 22,608 characters. 29 of 55 commits touch it. `TestVerificationBulletsNameTheirPackages` is at `cmd/eva-md/docs_test.go:232`; line 255 is inside it, not a fourth test (the tests are at 66, 106, 128, 170, 204 and 232).
- Lower severity: it costs the team's throughput, not memory quality. The cost is real for parallel work, since git merges by line and one bullet is one line.

### Missed findings

**1. Nothing measures how fresh the memory is when the next chat starts (evaluation, high, add).**
- Evidence: each pass writes `boundary_to_saved_ms`, the time from the chunk's newest entry to the saved page (`internal/update/pass.go:446`, `runlog.go:88`). The only other mention in the repository is the benchmark's planted record (`scripts/benchmark.py:267`). A hook heartbeat run holds `at`, `failed` and `duration_ms` (`internal/health/health.go:138-142`). The brief works out whether the Now is stale or an update is pending (`internal/resume/resume.go:283`, `lines.go:15`) and records neither; the brief marker keeps a hash, a variant and a refresh count for one chat (`internal/hook/resume.go:157-172`).
- Impact: a pass may take minutes at the highest effort, and people start the next chat at once. If most briefs carry a stale or pending Now, the model-written memory rarely arrives when it is needed, and all its cost buys little. Nobody would see it.
- Direction: record for each brief, as counts only, whether the Now was fresh, stale, pending or absent, and report the lag's p50 and p95 and the share of fresh briefs in `status` and in every eval run. Use it to choose triggers, model and effort.

**2. The only live evidence for the headline dates from the first commit and carries no version (evaluation, medium, evolve).**
- Evidence: `git blame` puts `docs/verification.md:70-72` in `af3e066` (2026-10-30), unchanged since. They name no eva-md version, host version or date. `internal/resume/resume.go` changed in 7 later commits, which added the Now, rules, pointers, knowledge and recall, and the handling of program-run chats.
- Impact: "Passed" reads as current. Those checks ran before most of what a brief now holds existed, so a regression in what a real model does with today's brief would contradict nothing on record.
- Direction: give every live result a date, an eva-md version and the host and model versions, and expire it when the code it covers changes. The continuation harness replaces these hand checks; until then, mark them as of 2026-10-30.

**3. The update prompt would be tuned on the same 12 scripts that judge it, and the target fails on defensible answers (evaluation, medium, rebuild).**
- Evidence: `accuracy.Scripts()` is the only scenario set (`scripts.go:69-74`), with no held-out part. A fact counts only in the one section its script names and only by its codename (`measure.go:42-49`), and the target allows 2 content misses in 29 facts (`measure.go:263`). The rules prompt is version 4 with no live run behind it.
- Impact: the finding on recall (eval-5) names this problem for the floor; it applies to the prompt as well. Gating prompt changes on this run, as eval-2 proposes, would push the rules toward echoing codenames and the scripts' filing choices. A model that files a fact elsewhere for a good reason fails the target.
- Direction: split scenarios into a tuning set and a held-out set, generated from templates so new ones are cheap; accept any correct filing of a fact; judge a prompt change on held-out results over several trials. This belongs in the scorer rebuild of eval-3.
- Basis: the mechanism is verified in code; that a real model would trip it is a prediction, since none has run.
