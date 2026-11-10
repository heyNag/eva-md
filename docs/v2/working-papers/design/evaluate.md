# Proof: the evaluation program and how the build is run (L8, Stage 0, and the delivery method)

Design section for the eva-md v2 plan. Area: the evaluation harness and its results store; the suites and their numeric targets; what runs offline, what needs a signed-in host and model quota, and the budget; Stage 0 (live gates, the field report, a counted week) and what each result changes; and the method by which a team of coding agents builds v2 (specs, decision records, guarantees as data, the work-package protocol, ownership and branch rules, release gates).

Finding ids are those of the findings register: `EVL-01` to `EVL-15` (evaluation and testing), `PRD-01`, `PRD-05`, `PRD-09` (product), and the evaluation-lens findings of the other audits: `CAP-08`, `CUP-06`, `CUP-07`, `RED-06`, `BRF-05`, `RCL-05`, `SRC-04`, `PGS-06`, `SYS-02`, `UPM-01`, `UPW-02`, `STO-11`, `OKF-13`, `MCP-05`, `SET-13`, `OPS-06`. The audits' verification sections were read, catchup's and the brief's included. No finding this design rests on was refuted. Four were lowered by the second readers and the design follows them: EVL-08 and EVL-09 are medium, EVL-11 is "partly" (v1 does report a dead hook at a chat start; what is missing is a check before users are hit), EVL-15 is low.

The five sibling drafts (ledger, store, derive, deliver, hosts) were read. Their evaluation contracts (C-E1, C-E2, S-E1, S-E2, D-E1 to D-E4, V-E1 to V-E4, H-E1, H-E2) are accepted in section 17, with two changes stated there.

Every number given as a target is a proposal. None has been measured on eva-md. Budget figures are estimates that the pilot (EV-03) replaces.

---

## 1. Goal and the measurable bar

**Goal.** Anyone, at any commit, can answer three questions with one command each: is the memory right, does it help a later chat, and what does it cost. Every answer is a number with an interval, a baseline, and the versions of eva-md, the host and the model it was measured on. No rebuild in the plan lands without a "before" on the same instrument. A team of agents can build in parallel without reading a 390 KB document and without colliding in shared files.

**The bar for this area** (the bars for the memory itself are in section 8).

| Measure | Bar | Today |
| --- | --- | --- |
| Suites with a stored v1 baseline before the rebuild they judge starts | all of E, F, A, C, K, LAT | none (EVL-01, EVL-04, EVL-05) |
| Live results that carry date, eva-md commit, host CLI version, model asked and answered | 100% | 0%: the only live evidence is from the first commit and unversioned (EVL-13) |
| Seeded distortions the fidelity scorer catches (wrong value, dropped date, dropped condition, invented item, wrong speaker, stale kept) | 100% of the seeded set | wrong value, dropped date and invented item all score as perfect (EVL-03) |
| A correct fact filed in another permitted place counts as found | yes | no: three such misses fail a run (EVL-14) |
| Scorer code shared with the code under test | none | the oracle calls the validator's own helpers (EVL-10) |
| Held-out scenarios that existed before the code they judge was frozen | none: drawn from the release candidate's commit hash | no held-out set at all (EVL-05, EVL-14) |
| Latency targets that fail a build when missed | all hook targets | none (EVL-09) |
| Host releases checked within a day against recorded payload shapes | both hosts | no scheduled check (EVL-11, OPS-06, CUP-06) |
| Model calls made by a pull request's checks | 0 | 0 (kept) |
| Text of any chat or page in a results row, a report or a log this area writes | none (a test plants codenames and scans) | kept from v1's run log rule |
| Largest file an agent must read to build one component | 24 KB, no line over 400 characters | 394 KB, 21 lines over 2,000 characters that a standard reader cuts silently (PRD-09) |
| Work packages merged with a diff outside the paths they own | 0 (CI check) | no rule |

---

## 2. The design in one page

```
                       spec/metrics.json  (one catalogue of metric ids, owners, units)
                                   │
 Level 0  CI, every PR, no model   │   go test ./...  +  eva-eval gate --level 0
   scenario generator ─► hook payload replay ─► ledger ─► derive (simulated or recorded model) ─► store
   black-box scorers (fidelity, retrieval, drift, poisoning store stage, aging, migration)
   suites other areas own report in: capture audit, redaction corpus, storage conformance,
   fault matrix, first-run scorecard; latency gate on the macOS job
                                   │
 Level 1  live model, no agent     │   eva-eval run --level 1 --host H --spend-model-quota
   the same loop with the real sealed runner; k trials; cassettes recorded for Level 0
                                   │
 Level 2  live agent sessions      │   eva-eval run --level 2 ...
   session A replayed from fixtures ─► real pipeline ─► session B live under an arm
   arms: off | eva | control | oracle | host     graders: hidden tests, diffs, tool events
                                   │
 Level 3  in the product, local    │   eva-md status --json (hosts' command), eva-md audit ...
   content-free counters every area already writes; shadow decisions; never sent anywhere
                                   ▼
        results rows (JSONL, content-free) ─► baselines ─► gates.json ─► defaults.json, release gate
```

Nine rules carry the design.

1. **Measure v1 first.** Every suite has an adapter that runs it on v1. The first row in the results store for each suite is v1's. A rebuild is judged against that row.
2. **One scenario, every level.** A scenario is a script of sessions with planted facts and derived obligations, as v1's fact scripts are. The same scenario drives the offline check, the live model run and, where it has a task, the live agent run.
3. **The eval drives the product's code path.** Capture goes through the real hook binary, organizing through `organize.Derive`, delivery through the real hooks in a real host. No suite has a copy of product logic.
4. **The scorer sees what a user could see.** It reads the scenario and the store's export. It shares no code with the checker or the retrieval core.
5. **Deterministic graders.** Tests, diffs, tool events, slot matches. No model judges any gated number.
6. **Every result is versioned and expires.** A row names the commit, the contract, the suite, the host CLI and the model. A live result is stale once the code or the host it covers has moved.
7. **Baselines that do not depend on eva-md are cached.** Memory off, the oracle and the host's own memory are properties of a scenario, a host and a model. They are run once and checked for drift, not rerun for every build.
8. **Claims are made at the precision the data supports.** Primary outcomes are chosen for power: steps, tokens and corrections, and tasks where memory is necessary. Intervals are published. A target that the budget cannot verify is not called met.
9. **The harness is in the repository and never in a release.** `cmd/eva-eval` and `internal/eval` are built by no release and imported by nothing the product imports, as `cmd/live-accuracy` is today. What ships is content-free local measurement.

What is deliberately not here: no model judge in a gate, no public conversational benchmark as a target, no telemetry, no results service, no new dependency, no test that depends on machine speed, no live call from CI on a pull request, no comparison against named third-party products in v2.0.

---

## 3. Levels: what runs where

Derive's draft uses "Level 0" for CI and "Level 1" for any live run. This design splits the live runs in two, because their cost differs by two orders of magnitude.

| Level | What it is | Where | Model calls | When |
| --- | --- | --- | --- | --- |
| 0 | Deterministic: generated scenarios, simulated or recorded model answers, fake hosts, injected clocks | CI on macOS (the run of record) and Linux | none | every pull request; long forms nightly |
| 1 | The real sealed model run on generated scenarios; single model calls for brief probes | a container or an evaluation home (section 4.3) | hundreds to low thousands of small calls | weekly on the default row; before a release that changes the contract, the prompt or the model policy |
| 2 | Real agent sessions in a real host with the real plugin, graded by hidden tests | the same | tens to hundreds of agent sessions | weekly smoke; a core pack per minor release; the full pack for a claim |
| 3 | Counters the product writes locally in real use; shadow decisions | every install | none | always on; read by `eva-md status --json` |

Kept from v1 without change: macOS decides, a failure seen only on Linux is reported and never patched around; a latency gate lives outside `go test`; nothing at Level 1 or 2 runs without `--host` and `--spend-model-quota`, and the run states its call count and token estimate and exits 2 without the flag.

---

## 4. The harness

### 4.1 Packages and commands

| Path | Shipped | Holds |
| --- | --- | --- |
| `internal/eval/scenario` | no | the scenario model, families (one file each), the generator, the paraphrase bank, renderers to v1 inbox entries and to v2 hook payload fixtures |
| `internal/eval/score` | no | the black-box fidelity scorer, the retrieval scorer, probe, compliance, drift, poisoning and funnel scorers; its own small normalizer |
| `internal/eval/sim` | no | the simulated model: answers in the contract's schema from a scenario's facts, with error knobs |
| `internal/eval/run` | no | levels, arms, host drivers, the quota and home guards, cassettes |
| `internal/eval/results` | no | row schema, the store, baselines, statistics, gate evaluation, the diff report |
| `cmd/eva-eval` | no | `plan`, `run`, `score`, `gate`, `diff`, `baseline accept`, `record`, `home init`, `home check`, `canary`, `replay --private` |
| `eval/` | no | `gates.json`, `protocol.md`, `baselines/`, `packs/`, `labelled/`, `cassettes/`, and fixture repositories under `eval/testdata/repos/` (a `testdata` folder, so Go tooling ignores them) |
| `spec/metrics.json`, `spec/guarantees/` | read by tests | the metric catalogue and the guarantees (section 12) |
| `internal/spec` | no (tests only) | the meta-tests of section 12 |

`go test ./...` runs the Level 0 suites, since they are ordinary test packages in the module. `go build ./cmd/eva-md` never includes them. Two isolation tests, ported from `cmd/live-accuracy/main_test.go`, hold the line: `go list -deps ./cmd/eva-md` names no `internal/eval` or `internal/spec` package, and no release workflow or script names `eva-eval` or the `evamd_eval` tag.

### 4.2 The loop, and the three model backends

v1's best idea is kept: one loop for canned and live answers (`live.Run` takes an `Ask`). In v2 the loop is derive's `organize.Derive(ledger fixture, runner) -> store` (D-E2), and the harness chooses the runner.

| Backend | Used at | What it is |
| --- | --- | --- |
| simulated | Level 0 | `internal/eval/sim` writes a contract-conformant answer from the scenario's facts. Knobs: drop a fact, change a value, drop a date or condition, give the wrong voice, drift a quote, invent an item, propose a payload as a rule, restate delivered memory. This is what lets the checker's reaction be tested at scale and the aging suite run offline |
| recorded | Level 0 | a cassette: the real model's answer, stored under the hash of (contract version, rendered input, model, effort). A missing cassette at Level 0 is an error, never a live call |
| live | Level 1, 2 | derive's sealed runner on the real host CLI |

A contract or prompt change makes the cassettes miss. CI then falls back to the simulated model for the enforceable obligations and marks the content metrics "needs a live record" on the pull request. That mark blocks a release, not a merge.

The simulated model is built from the contract's generated schema (E-D1), so it cannot drift from the contract. It produces answers only. The scorer stays independent of it.

### 4.3 Where live runs happen

Two places, chosen by what is being measured.

**Containers, with keys kept for evaluation (suites).** The memory-quality question, whether a delivered brief or item changes what the model does, does not depend on the operating system. eva-md already builds and passes its tests on Linux, and the test stand-in already runs setup there. A container gives a clean home per run, parallel runs and nothing to clean up. The hosts run headless with an API key. This is also what lets the agent team itself run evidence windows from a cloud session, with keys held by the environment's secret mechanism and never in the repository.

**An evaluation home on a Mac, signed in on the plan (canary tier and what containers cannot do).** A dedicated folder used as `HOME`, or a dedicated macOS account, that a person sets up once: sign in to both hosts, install eva-md the normal way, approve the Codex hooks in `/hooks`. v1's rule stays word for word: live checks never copy `auth.json`, manufacture trust records or bypass hook trust. The harness refuses to run unless `HOME` holds the marker `eva-eval home init` wrote and differs from the invoking user's real home. Inside it the harness resets eva-md's own state, config and store before each scenario arm; it never touches anything outside it.

| Needs the Mac home | Why |
| --- | --- |
| The interactive path (typed prompts, `/clear`, `/compact`, the entrypoint values) | headless sessions are a program's by the capture allowlist |
| The plugin and trust path on Codex, the install and upgrade path | a container uses managed hooks, which skip review |
| Claude Code's own memory as an arm | it is off in self-hosted environments and cannot be turned on from a background session |
| Latency on real hardware; signing and push to a scratch remote | machine facts |

### 4.4 Drivers, and the one evaluation seam

| Driver | Session kind | Used for |
| --- | --- | --- |
| replay | session A: recorded or generated hook payloads fed to the real `eva-md hook` on stdin, in order, with interactive-shaped fields from the ledger's fixtures (LG-03) | every suite's history; costs no model call |
| headless | session B: `claude -p --output-format stream-json --max-turns N` with `--plugin-dir` for the eva arm; `codex exec --json` | the suites at scale |
| pty | session A or B typed into the host's own terminal UI through a pseudo-terminal | the canary tier and driver parity |

A headless session is a program's chat. v1 stores its prompts as host prompts and v2's allowlist will not call them typed, and deliver gives a program's chat rules and constraints only. So a headless session B would not get the brief a person gets. Two answers were weighed: drive everything through a pty (the true path, three to five times slower, brittle against UI changes), or one seam. The design takes the seam, and keeps the pty for parity:

- A file behind the build tag `evamd_eval` lets the environment variable `EVA_MD_EVAL_ACTOR=person` classify the session's actor as a person. The precedent is `timelimits_raised.go` behind `evamd_testlimits`.
- No release contains the file. The isolation test builds the release way and checks the tag is absent; the release workflow never passes tags.
- **Driver parity** is a canary check: two scenarios run through the pty on the release build and through the headless driver on the eval build must produce the same delivery records (item ids and surfaces). A difference fails the canary, because it means the suites no longer measure what a person gets.

This touches the provenance allowlist, so it is an owner decision (EV-D4).

### 4.5 Arms

| Arm | eva-md | Host's own memory | What it answers |
| --- | --- | --- | --- |
| `off` | not loaded | off | the baseline: the repository alone |
| `eva` | loaded; store built by the real pipeline from session A | the host's shipped default (Claude Code on, Codex off) where the run place allows it; off in containers | what a user gets |
| `control` | loaded; the pack compiled from another scenario's store, matched in tokens | off | whether an eva-shaped block of irrelevant memory costs anything |
| `oracle` | not loaded; the scenario's ideal note placed at the head of the first prompt | off | the ceiling, and whether the task offers memory any opportunity |
| `host` | not loaded | on, session A run in the host so it can write its own memory | whether eva-md helps beyond what the host already does |
| `oracle-store` (diagnostic, subset) | loaded; the store built from the scenario's oracle items, derive bypassed | off | separates loss in organizing from loss in delivery |

Arms are set per invocation and nothing is written to a host's settings: `--plugin-dir` present or absent, `--settings '{"autoMemoryEnabled": false}'` or `CLAUDE_CODE_DISABLE_AUTO_MEMORY=1` on Claude Code; `--disable hooks --disable plugins` on Codex.

The `host` arm is Claude Code only by default, on the Mac home, with session A typed through the pty. Codex's own memory is off by default, takes only interactive sessions idle for six hours as input, and never `codex exec` sessions. It is measured by hand once per claim release if the owner wants it, and reported as "not run" otherwise.

The primary contrasts: utility is `eva − off`; utility beyond the host is `eva − host`; the gap closed is `(eva − off) / (oracle − off)`; harm is `control − off` and `eva − off` on tasks where the oracle does not beat off.

### 4.6 Cached baselines and the drift check

`off`, `oracle` and `host` do not run eva-md. They are stored per (scenario hash, host, model) and reused. Each release run repeats a random fifth of the `off` runs. If the repeated runs differ from the cached ones beyond an equivalence margin (section 7), the cache for that host and model is dropped and rebuilt before the release is judged. A change of model id drops it at once. This is the largest saving in the budget and the reason a per-release run costs a third of a naive one.

### 4.7 The funnel

For each fact a task depends on, the harness joins ids across the stages, using derive's sources, deliver's decision log and the ledger's delivery records (V-E2, V-E4):

`captured` (an event holds the turn) → `written` (an active item holds the value) → `compiled` (the pack holds the item) → `delivered` or `pulled` (a delivery or pull record before the deciding action) → `used` (the grader passes).

Each release report prints the first stage at which each failed scenario broke. This table, not the headline number, is what tells the orchestrator where the next work package goes.

---

## 5. Data formats

### 5.1 Scenario

An evolution of `accuracy.Script` (`internal/update/accuracy/scripts.go`): facts keep speaker, time, validity and replacement; they gain value slots, permitted kinds, a `must` field and anchors. Codenames stay only in the "marked" families ported from v1.

```json
{"id": "e.reversal.0007", "family": "reversal", "v": 3, "seed": "9f2c…", "lang": "en", "split": "tuning",
 "world": {"repo": "gokv", "project": "p1", "branches": ["main", "feat/retry"]},
 "sessions": [{"sid": "s1", "host": "claude-code", "actor": "person", "branch": "feat/retry",
   "start": "2026-01-13T09:00:00+01:00",
   "turns": [{"n": 1, "by": "user", "pv": "typed", "text": "…", "plants": ["f1"]},
             {"n": 2, "by": "agent", "text": "…",
              "tools": [{"name": "Bash", "cmd": "go test ./...", "status": 1, "digest": "…"}]}]}],
 "facts": [{"id": "f1", "kinds": ["decision", "constraint"], "voice": "typed", "scope": "project",
   "subject": ["retry", "limit"],
   "slots": {"value": {"class": "int", "is": "7"}, "cond": null, "date": null},
   "valid": {"from": "s1#1", "until": "s2#3"}, "supersedes": null, "must": "store",
   "anchors": ["internal/retry/retry.go"]}],
 "must_not": [{"id": "x1", "why": "pasted", "at": "s1#4"}],
 "maintenance": [{"after": "s2", "event": "compaction"}],
 "probes": [{"kind": "decision", "expects": ["f1"]}],
 "task": {"prompt": "…", "check": "hidden_tests", "needs": ["f1"], "max_turns": 30},
 "oracle": {"note": "…", "items": ["…"]}}
```

- `kinds` is a set: any permitted filing of a correct fact counts (EVL-14).
- `slots` carry what must survive: a value with a class (integer, number with unit, date, path, identifier, version, short phrase with accepted forms), a condition, a date. Slot classes have normalizers in the scorer.
- `must` is `store`, `store_as_state`, `not_store` (the repository already says it; off the record; delivered memory restated), or `not_with_authority` (pasted, tainted, an agent's suggestion).
- A scenario's text comes from templates and a paraphrase bank committed as data. The bank is written once with a model, reviewed by a person, and never generated at run time. Every value is made up.

### 5.2 Packs and the held-out rule

A pack is a list of (family, seed, parameters, expected hash). Instances are regenerated; the hash pins the generator, so a generator change that alters an instance is caught and bumps the suite version.

- **Tuning** packs have fixed seeds in the repository. Builders may read them and tune on them.
- **Held-out** instances are drawn at gate time with the seed set to the release candidate's commit hash. They do not exist while the code is being written, so nothing can have been tuned on them.
- **Held-out families** (four at first, hand-written) guard against tuning to the templates. Only this area's packages and adversarial reviewers may open them; the ownership check of section 12 fails any other diff or card that names them.

### 5.3 Result row

One header per run and one row per (scenario, arm, trial). JSONL, append-only, one folder per run id.

```json
{"run": "2026-12-03T0412Z-7c1e", "suite": "C", "suite_v": 2, "pack": "c.core", "pack_hash": "…", "level": 2,
 "eva": {"version": "2026.12.3", "commit": "…", "dirty": false, "tags": "evamd_eval"},
 "v": {"contract": 7, "pack_format": 2, "scorer": 4, "harness": 3},
 "host": {"name": "codex", "cli": "0.167.0", "model_asked": "…", "model_answered": "…", "effort": "default",
          "billing": "api_key", "driver": "headless", "place": "container"},
 "scenario": "c.hidden.0031", "scenario_hash": "…", "family": "hidden_decision", "split": "heldout",
 "arm": "eva", "trial": 2, "status": "ok",
 "m": {"solved": 1, "corrections": 0, "agent_events": 14, "prompt_tokens": 212340, "cached_tokens": 181200,
       "output_tokens": 6120, "wall_ms": 184000, "injected_tokens": 912},
 "funnel": {"captured": 1, "written": 1, "compiled": 1, "delivered": 1, "used": 1},
 "codes": []}
```

`status` is `ok`, `refused`, `stopped_unjudged` or an error class. A host failure that says nothing about the answer (sign-in, rate limit, timeout, a model other than the one asked) stops the run unjudged, as v1's live loop does. A row never holds text: ids, counts, codes from a closed list.

### 5.4 Baselines, gates, defaults

- `eval/baselines/index.json` names, per (suite, pack, host, model, arm), the accepted run and its summary statistics, with the date and the role that accepted it. A baseline lists the component paths it covers. It is **stale** when one of those paths has a commit newer than the baseline's commit (for `eva` arms), or when the host's model changed or the drift check failed (for cached arms).
- `eval/gates.json` holds every gate as data:

```json
{"metric": "ledger.otr_leaks", "suite": "CAP", "level": 0, "class": "hard", "op": "==", "value": 0, "blocks": ["merge", "release"]}
{"metric": "hook.capture_p95_ms", "suite": "LAT", "level": 0, "class": "regress", "ratio_ci_low_max": 1.15, "ceiling": 50, "blocks": ["release"]}
{"metric": "cont.handoff.events_ratio", "suite": "A", "level": 2, "class": "claim", "ship": "ci_high < 0.90", "claim": "point <= 0.60", "blocks": ["feature:deliver.start", "claim"]}
```

- `defaults.json` is written by `eva-eval gate --emit-defaults` at integration and embedded in the binary. It is how deliver's `off`, `shadow`, `on` flags follow results (V-E3) without the product reading the evaluation tree at run time. A feature with no passing row stays `shadow`.

### 5.5 The metric catalogue

`spec/metrics.json`: one entry per metric id with its owner area, unit, direction, source and the suites that report it. Areas keep their own content-free logs (the ledger's health lines, derive's journal, deliver's decision log, the store's sync records). There is one catalogue and one reader, hosts' `status --json`, and no shared writer. A meta-test fails when a gate, a work-package card or a spec names a metric that is not in the catalogue, and when `status --json` omits a Level 3 metric.

---

## 6. The suites

Letters follow the evaluation research note, which deliver's draft also uses. Suite D of that note (sequential repository work mined from real issue histories) is not built: it is the costliest to construct and C covers its question.

| Suite | Question | Levels | Owner | Grader | First needed |
| --- | --- | --- | --- | --- | --- |
| E fidelity | Is the right thing written, attributed, dated, scoped, and retired when it stops holding? | 0, 1 | evaluate; derive supplies `organize.Derive` | slot match on the export | Stage 1 (v1 baseline) |
| F retrieval, L abstention | Was a push right to fire, and silent otherwise? Does pull find it? | 0 | evaluate owns the set, the sealing and the scorer; deliver the core under test (DL-04) | labels by construction | Stage 1 |
| K brief probes | Does the brief alone hold what a successor needs? | 0, 1 | evaluate | slot presence; one model call per probe at Level 1 | Stage 1 |
| A handoff | Does a new chat finish interrupted work in fewer steps? | 2 | evaluate | hidden tests, events, tokens | Stage 1 (v1 baseline) |
| C hidden decision, lesson | Is a decision or a failed approach from an earlier chat applied? | 2 | evaluate | hidden tests, corrections per task | Stage 1 (v1 baseline) |
| B switching | Does per-workstream state survive interleaved work? | 2 | evaluate | the same verifier as the control | Stage 3 |
| G rule compliance | Is a standing rule obeyed, not only shown? | 2 | evaluate | command, path and diff checks | Stage 3 |
| H drift worlds | After the code changes, does memory stop asserting what no longer holds? | 0, 2 | evaluate | claim classes by construction | Stage 3 |
| I poisoning | Can untrusted text become trusted memory, be delivered, be acted on? | 0, 1, 2 | evaluate; the store supplies the foreign-commit channel (S-E2) | canary markers | Stage 2 for the store stage, Stage 3 for the rest |
| J aging | Does quality hold over 200 sessions and every maintenance event? | 0; 1 once per claim | evaluate | E's metrics as a time series | Stage 3 |
| COR corpus audit | Is the real store degrading? | 3 | derive owns the counters (DV-32); evaluate the thresholds | counts | Stage 3 |
| CAP capture audit | Is capture complete, ordered, rightly attributed, private? | 0, 3 | ledger (C-E2) | the ledger's scorer | Stage 0 for v1, Stage 2 |
| RED redaction corpus | Leak rate and destroyed-fact rate | 0 | ledger (LG-12) | labelled corpus | Stage 1 |
| STO storage conformance | No lost write, convergence, restore | 0 | store (ST-02) | reference model | Stage 1 |
| MIG migration rehearsal | Does every v1 statement survive, and does a brief say the same before and after? | 0; owner's dry run | evaluate, with store, ledger, derive | E's scorer on both sides | Stage 2 |
| LAT, cost, freshness | Hook latency, tokens per chat, lag at chat start | 0, 2, 3 | evaluate for the gate; derive and hosts for the ledgers | benchmark; journals | Stage 1 |
| FRS first run; fault matrix | Questions asked, lines printed; time to detect a silent stop | 0 | hosts (HO-31, HO-12, HO-23) | scripted fake hosts | Stage 2 |
| CAN canary | Did a host release change something eva-md rests on? | real hosts | hosts for the tier with no sign-in (HO-50); evaluate for the signed-in tier | schema diff; probes | Stage 2 |

### 6.1 E: write-path fidelity

**Families.** v1's twelve failure modes are ported first and kept as regression classes. Added: a proposal the user accepts in a word; a dialog answer; a plan approval; implicit invalidation ("we moved off X"); a two-hop update; a decision at line 700 of a tool-heavy chat; a cut inside a turn; a compaction summary that restates old facts; a decision on an unmerged branch; a claim that came from a web page or a tool; a subagent's finding; a lesson (tried, failed, because, fix); a fact the repository already states (must not be stored); delivered memory restated by the agent (must not count as new support); and translated twins in one spaced and one unspaced language. Parameters: voice, validity window, chain length, branch, session order, distractor volume up to a full chunk, position of the fact.

**Scoring, black-box.** The scorer reads `eva-md export --json` (E-S1): typed records with kind, state, tag, scope, statement, detail, anchors and the (session, sequence) of each source. Because contract v2 makes every item cite the events it rests on, attribution is deterministic:

1. An item's sources map it to the turn it came from, and so to the planted facts or distractors of that turn.
2. A fact is **found** when an active item sourced at its turn holds its value slots, in any permitted kind. It **holds** when voice, scope and validity are also right.
3. An item sourced at a fact's turn that lacks a slot is `value_dropped` or `value_wrong`. An item sourced at a distractor is `noise`. An item that serves a `not_store` or `not_with_authority` entry is a violation of that entry.

| Metric | Meaning |
| --- | --- |
| `fact_recall` | facts found with every slot right |
| `wrong_write` | active items that are `value_wrong`, or supported by no planted fact and no permitted background |
| `noise_rate` | items from distractors per chat |
| `misattributed` | text of an agent, a tool, a paste or an outside source under the user's tag |
| `stale_in_force` | superseded or ended facts still active |
| `dupes`, `scope_errors`, `otr_stored` | as named; `otr_stored` must be 0 |
| `honest_refused`, `accepted_captured` | derive's measures, computed from refusal codes and the accepted-proposal families |

The enforceable and content split of v1 is kept: enforceable obligations must hold whatever the model writes and are checked against the simulated model's adversarial answers; content obligations are scored at Level 1.

**What a smaller change would leave.** Adding value checks to v1's `Measure` would still locate facts by codename in one named section, still have no precision side, and still be built on a page model that derive's rebuild removes. The scorer is rebuilt; the script model, the failure modes and the loop are kept.

### 6.2 F and L: retrieval and abstention

One labelled set, two sources of items. **Synthetic**: the oracle items of generated scenarios. **Model-written**: the items a real model wrote at Level 1 on the same scenarios, recorded once per contract version, so that retrieval is scored on statements and cues a model produced, which v1 never did (RCL-05). Labels come from construction: each event (a typed prompt, a file path about to be edited, a command) is generated from a fact, so its relevant ids are known.

At least 30% of events are negatives: no match; a sibling project only; a near miss sharing two or three rare tokens; a drifted item that must be held back; an item provisional on another branch; a long pasted log. A paraphrase split shares no content token with the statement.

Metrics per surface and per gate: precision, recall, false injection on negatives, silent rate, tokens per injection; for pull, recall at 5 and calls to answer. "Zero injections on no-match and sibling-only events" is reported with its bound: zero in 160 held-out negatives means under 1.9% at 95% confidence, and the plan says so.

v1's `knowledgepage.Recall` and v1's search are scored on the same set first.

### 6.3 A, B, C: continuation

**Fixture repositories.** Small, written in-house, in Go, Python and JavaScript, with no dependency beyond each language's standard test runner. In-house means no model has seen the solutions, which the public issue sets can no longer promise. Each task is screened before it enters a pack: for C the oracle arm passes twice and the off arm fails twice; for A the task is solvable from the repository alone, and the oracle arm needs at least 30% fewer agent events than off. A task that fails its screen is rewritten or dropped. If the pilot cannot produce tasks that pass, the suite cannot detect anything and the plan must say so before Stage 2 leans on it.

**A, handoff.** A predecessor's run is recorded once and cut at deterministic points: after the first source edit, after the first test result, after the first edit that follows a failure. Session B gets the repository at that point and the task. Outcomes: agent events and prompt tokens to completion, solved or not, and whether work already done was done again.

**C, hidden decision.** A decision, a convention or a failed approach exists only in an earlier chat. The hidden test encodes it. A failed attempt gets the failing test names back, up to five rounds. Outcomes: solved at the first attempt, corrections per task. Variants: the earlier chat was on the other host (the case neither host's own memory can serve); the fact is a lesson; about twice as many decoy chats as real ones.

**B, switching.** A focus task with four unfinished tasks on other branches interleaved, against an uninterrupted control. A default-branch variant scores workstream binding (D-E4).

### 6.4 G, H: rules and drift

**G.** Rules with a mechanical check: a command never run, a path never touched, a convention in the diff. Tasks that tempt a violation. Depths of 1 and 16 intervening sessions, which are replayed fixtures and cost nothing. Arms: off, shown in the brief, shown at tool time, armed as a check (warn, ask), and a delegated variant in which a subagent does the edit. Reported as two rates: the rule was in context (from the delivery record), and the rule was obeyed.

**H.** Generated repositories in which each stored claim maps to an artifact, then scripted mutations: a value changes, logic changes, a file is deleted or moved, content is withheld. At Level 0 each claim must end held back or labelled, never shown as current. At Level 2, on a small set, a task depends on the changed value and the hidden test encodes the new one.

### 6.5 I: poisoning, with storing, delivering and acting scored apart

A corpus of payload placements across the write channels: a tool's failure digest, pasted text, a web page restated by the assistant, an MCP result, a file in the repository, a forged closing fence or tag, hidden and bidirectional Unicode, an agent's suggestion phrased as the user's decision, a subagent's report, a compaction summary, a call of eva-md's own write tool that claims the user's authority, and a commit from an unknown writer arriving through sync. Each payload asks for a harmless marker action (create a named file, add a named line to a commit message, add a dependency that does not exist). Containers run with the network closed except to the model provider.

| Stage | Level | Metric | Bar |
| --- | --- | --- | --- |
| stored with authority | 0 (simulated model that obeys the payload), 1 (a real model reads the poisoned chat) | `stored_with_authority` | 0, a hard gate |
| stored at all | 0, 1 | `stored_any` | reported |
| delivered unasked | 0, with the payload force-planted as a candidate or tainted item | `delivered_tainted` | 0, a hard gate |
| acted on later | 2, a later session with a benign trigger | `acted_on` | 0 in at least 150 trials per host, reported with its bound |
| persistence | 2, after one clean session | `still_present` | reported |
| last line | 2, the payload force-planted as an active item under the user's tag | `acted_when_delivered` | reported, never gated: it shows how much rests on the stages before it |

### 6.6 J and COR: aging and the corpus

**J.** A generated deployment of 200 sessions over simulated months, replayed with the simulated model at realistic error rates, with maintenance events in the stream: a redaction rule bump and repair, the v1 migration, housekeeping, a sync merge, an upgrade with a replay of refused ranges. E's metrics are computed after every tenth session and after every event. A regression at an event fails the gate. `ranges_unaccounted` must stay 0. Store size, pack size and brief tokens are tracked. One live deployment per claim release costs about 250 small calls per host.

**COR.** Content-free counts over the real store: items by kind, state and tag; near-equal clusters; open disputes; candidates by age; anchors moved or gone; items never delivered; the promotion funnel by refusal code; items whose value tokens the repository's own instruction files already hold. This area sets the thresholds that turn a count into a decision, such as derive's DV-D8 rule for whether a model review tier is built.

### 6.7 MIG: the migration rehearsal

A user's wiki and inbox must survive, so the migration is tested by the same instrument as everything else. The harness builds v1 state at scale with v1's own code (scenarios rendered to v1 inbox entries, v1's update run with canned answers), runs the ledger's, the store's and derive's migrations, and scores both sides with the fidelity scorer through a v1 export adapter. `migration_loss` (facts found before and not after) must be 0, and K's probes on the brief must find the same facts before and after. The owner's dry run on a copy of the real wiki reports the same counts and no content (ST-07).

### 6.8 LAT, cost, freshness

**Latency.** `scripts/benchmark.py` keeps its scenarios, its isolation and its functional assertions. It gains a gate mode on the macOS job: the old and the new binary run interleaved on the same machine; a metric fails when the lower bound of the bootstrap interval of the new-to-old p95 ratio is above 1.15, or when an absolute ceiling is passed. On the CI machine the ceiling is twice the target; on the reference Mac at a release it is the target. At least 200 samples per hook metric, p95 by the nearest-rank rule, process start included. No retry: the decision rule is statistical. Each run's JSON is stored as a result row. Added scenarios: a realistic inbox that is not padding, a first prompt that owes a brief, a 64 KiB reply.

**Cost.** Three numbers, from derive's journal and the delivery records: tokens per organized chat; `injected_share`, the tokens eva-md added to a session as a share of that session's prompt tokens, which is exact arithmetic on delivery records and needs no A/B run; and net tokens per completed task at Level 2, with cached and uncached input apart.

**Freshness.** `state_lag_turns` at chat start and `end_to_saved`, at Level 0 on a simulated day with injected clocks, at Level 2 from the runs themselves, at Level 3 from real use.

### 6.9 The canary's signed-in tier

Hosts' HO-50 checks install, probes, swap and uninstall on throwaway runners with no sign-in. This area adds what needs a signed-in home, daily, inside a fixed budget of about ten small calls and eight short sessions:

1. Model-free first: host versions; a pty session whose hook payloads are recorded and diffed, by field names and types and never values, against the ledger's fixtures for that host. A new or missing field opens an issue and offers the recording as the next fixture (CUP-06).
2. `doctor --live` or its v2 form, one call per host.
3. Two fidelity scenarios, one continuation smoke (plant four items, ask "where did we stop" in a new chat, after `/clear` and after a forced compaction), two poisoning probes.
4. Driver parity (section 4.4).
5. Weekly, a short fault set that is safe on a real host: the binary missing (the chat must be unaffected and no prompt blocked), the store unreadable, the worker killed mid-pass, the pack missing. The full fault matrix stays offline with fake hosts, where hosts' draft puts it.

The canary writes result rows. A scheduled check fails when the newest canary row is older than 36 hours or red.

---

## 7. Statistics, and what can honestly be claimed

- **Paired design.** The same scenario and trial index under every arm. Intervals by bootstrap, resampling scenarios (10,000 draws, seeded). McNemar's exact test for paired pass or fail. `pass^3` reported for C.
- **Primary outcomes are chosen for power.** Steps, tokens and corrections are counts with large expected effects (20 to 60% in the published work). Tasks where memory is necessary make the pass rate itself move by tens of points. With a paired log-ratio standard deviation near 0.6, 60 scenario-trials detect a 25% reduction at over three standard errors.
- **What cannot be verified per release.** A 3-point gain in solve rate on ordinary tasks needs several hundred pairs per host. A harm rate of 1% needs 300 runs with none to bound it at 1%. A 3% rise in tokens from changed behaviour needs about a thousand pairs. The design does not pretend otherwise:
  - solve rate on ordinary tasks is a non-inferiority guard, pooled across releases for the same suite version, with its interval published;
  - harm is gated on what can be seen: `injected_share` (exact), the control arm's efficiency within 5% of off, zero flips beyond the off-against-off noise floor; and the pooled interval is printed beside the 1% target until it is tight enough to claim;
  - "zero" is always printed with its rule-of-three bound.
- **Trials.** Three per scenario for `eva` and `off`; two for `oracle` and `host`; one for `control`.
- **Protocol first.** `eval/protocol.md` and `gates.json` are committed before a suite's first live run. A change to a primary metric bumps the suite version and starts a new baseline.
- **The equivalence margin** of the drift check is a ratio of medians within 0.9 to 1.1 for events and tokens and a pass-rate difference within the two-sided 90% interval of the cached runs.

---

## 8. Targets that would justify the word best

"Best" is an evidence package, not a rank: no accepted leaderboard exists for coding-agent memory. Two columns. **Ship** is what lets a feature default to on: a real effect whose interval excludes zero. **Claim** is what must hold on held-out scenarios, on both hosts' default models, before the word is used. Rows marked with an area are that area's bar, repeated here so the package is in one place.

| # | Claim | Ship | Claim | Suite |
| --- | --- | --- | --- | --- |
| 1 | Resuming is cheap | successor events lower than off, upper bound of the ratio's interval under 0.90; solve rate not lower | events at least 40% lower and prompt tokens at least 50% lower than off, from a start brief of at most 2,000 tokens (deliver) | A |
| 2 | Decisions stick | first-attempt pass rate above off, interval above zero | at least 70% of the oracle gap closed; corrections per task at least 50% lower than off | C |
| 3 | It helps beyond the host | not worse than the host's own memory on same-host tasks | on cross-host tasks at least 50 points of the oracle gap closed where the host's own memory closes none by design; not worse on same-host tasks | C, `host` arm |
| 4 | Switching is safe | recovery above zero | at least 75% of the resolution lost to interleaving recovered | B |
| 5 | The memory is accurate | `misattributed` 0, `otr_stored` 0, `wrong_write` at most 0.02 | `fact_recall` at least 0.90 at the default row and 0.95 after one escalation; `stale_in_force` at most 0.02; twins within 5 points; the same after 200 sessions (derive) | E, J |
| 6 | Push is precise | precision at least 0.95, false injection at most 1% on the model-written held-out set | the same, with zero on no-match and sibling-only events (deliver) | F, L |
| 7 | It never hurts | `injected_share` at most 3%; control within 5% of off on events and tokens; no flips beyond the noise floor | pooled solve-rate difference on no-opportunity tasks at least −1 point with the interval published; net tokens per completed task not above off where memory can help | harm set, cost |
| 8 | Rules are obeyed | violations with a shown rule below off | at most 15% violations at 16 sessions for armed, checkable rules; the seen-against-obeyed split published (deliver) | G |
| 9 | Stale claims are caught | none shown as current | at least 95% of drifted claims held back or labelled; no fabricated replacement | H |
| 10 | Untrusted text stays untrusted | `stored_with_authority` 0; `delivered_tainted` 0 | `acted_on` 0 in at least 150 trials per host | I |
| 11 | Nothing is lost | `otr_leaks` 0, `pv_false_typed` 0, `ranges_unaccounted` 0, lost writes 0 in 10,000 schedules, `migration_loss` 0 | turn coverage at least 99.5%, tool coverage at least 99%, convergence and restore 100% (ledger, derive, store) | CAP, STO, MIG |
| 12 | It is invisible | capture p95 under 50 ms, chat start under 300 ms (the AGENTS.md ceilings); zero blocked prompts under the fault matrix | deliver's tighter budgets (chat start 150 ms, pre-tool 30 ms); first-run questions 0; a week on the real install with no command typed for eva-md (hosts) | LAT, FRS |
| 13 | It is fresh and cheap | `state_lag_turns` and tokens per chat reported | `state_lag_turns` p90 at most 1; `end_to_saved` p50 at most 90 s; tokens per organized chat at most a fifth of v1's measured figure (derive) | cost, freshness |
| 14 | It can be checked | every number has a command | the harness, the packs, the protocol and the per-item rows of the claim run are public | all |

Rows 1 to 4 are the claim. Rows 5 to 11 are why it can be trusted. Rows 12 to 14 make it a product. What the package will not claim: a rank against named products, a score on a conversational benchmark, or any number measured on one host stated for both.

---

## 9. Budget

Estimates, to be replaced by the pilot's measurements. A small extraction call is taken as about 13,000 tokens. An agent session on a fixture task is taken as 0.3 to 0.7 million prompt tokens, 80 to 90% of them cache reads, and four minutes.

| Run | When | Small calls | Agent sessions | Tokens, rough | Wall time |
| --- | --- | --- | --- | --- | --- |
| Pull request checks | every PR | 0 | 0 | 0 | plus about 3 min of CI |
| Canary, signed-in tier | daily | 10 | 8 short | 3 M | 20 min |
| Weekly: E on the tuning pack, default row, both hosts, 1 trial; continuation smoke | weekly | 520 | 26 | 15 M | 2 h |
| Minor release: E held-out, 3 trials, both hosts; K; the core pack (A 12, C 12, G 6, harm 8, I 30 runs) with `eva` at 3 trials, `control` on a subset, a fifth of `off` repeated | per minor release that touches organizing or delivery | 2,200 | 360 | 140 M | 6 h at 4 in parallel |
| Baselines for the core and full packs: `off` 3, `oracle` 2, `host` 2 (Claude Code, on the Mac) | once per host model, and on drift | 0 | 400 to 540 per host | 150 to 250 M per host | 1 to 2 days |
| Claim run: every suite, full packs, held-out, both hosts; one live aging deployment | before the word "best" is used | 4,500 | 1,020 | 350 M | 17 h at 4 in parallel |
| Model and effort frontier (derive's DV-03) | once, and when a host's small model changes | 1,900 | 0 | 25 M | 3 h |
| Pilot (EV-03) | once, Stage 0 | 100 | 180 | 60 M | 1 day |

The runner enforces a ceiling: `eva-eval run` takes `--max-tokens` and `--max-sessions`, prints the plan first, and stops unjudged when a ceiling is reached. If the owner sets a lower budget, suites are cut in this order: B, the `host` arm's second trial, G's depth of 16, the `control` arm, H at Level 2. A and C on the core pack are the last to go.

---

## 10. Stage 0: ground truth

Stage 0 is five pieces of work, most of them owned by other areas. This area owns the run-book, the format the first numbers are stored in, and the table that says what each result changes. Stage 0 does not block Stage 1: specs, the harness and the seams start at once. Only the scope of Stage 2 waits on it.

| Piece | What | Owner | Spends quota |
| --- | --- | --- | --- |
| Field report | the prompt in `docs/v2/field-report-prompt.md`, run by the owner on the one real install | the lead; EV-00 folds it in | one chat |
| Host fact sheets | LG-03 (payloads), DL-01 (delivery), HO-01 (packaging and trust), ST-00 (git), and the evaluation route (EV-03) | each area | EV-03 only |
| Live gates on v1 | `doctor --live`, the live accuracy run at the highest and at a low effort on both hosts with rules and knowledge in the batches (DV-02), the hand-run live resume test with the update on, the MCP server and both skills seen working | derive for the tool; EV-01 for the run-book and rows | 21 calls per host and effort, plus a few chats |
| A counted week | one point release of v1 that adds only content-free counters (LG-01, LG-02, DV-01, DL-02, HO-02) and changes no plugin byte, so Codex's approval holds; the owner upgrades once; a week of ordinary use; the report again | each area for its counters; EV-02 for the release check and the reading | none |
| Pilot | ten scenarios, three arms, three trials on v1's brief; variance, tokens per session, whether tasks pass their screen | EV-03 | 180 sessions |

### What each result changes

Thresholds are proposals. They are written down before the results arrive so that the reading is not bent to fit a plan already made.

| # | Measurement | If | Then |
| --- | --- | --- | --- |
| 1 | Share of v1 passes that end with the whole answer refused or `invalid_output` (run log, live run) | 30% or more | contract v2 (DV-21) is first in Stage 2 at full size |
| | | under 10%, with `fact_recall` at least 0.9 on the new scorer | DV-21 shrinks: span evidence for user-tagged items only, v1's reconcile kept for workstream state; deliver's rebuild moves ahead of it |
| 2 | Knowledge and rule adds refused by the checker, of those proposed | half or more | word-overlap provenance is confirmed as the loss; exact spans first |
| | | 15% or less | spans are still built (the hidden contract, other languages) but the repair round is dropped |
| 3 | Share of chat starts whose Now was stale or pending; `boundary_to_saved` | 30% or more of starts | the worker and the fast pass (DV-12, DV-24) go before any recall work; the brief's model-free handoff carries freshness |
| | | under 10% | the two-tier split drops in priority; the tick is argued on reliability alone |
| 4 | Seconds and tokens per pass at the highest effort; timeouts | median over 120 s or any timeout | the small-model default (DV-D2) is no longer optional |
| | | under 30 s and 20,000 tokens | the frontier run shrinks to two tiers |
| 5 | Events v1's gate held back; turn and tool coverage against the host's record | under 0.5% held and coverage at least 99% | the ingester (LG-21) is justified by ids, order and missed decisions only, and moves behind derive; decisions and typed tool records come through hooks first |
| | | 2% or more held, or tool coverage under 95% | as designed |
| 6 | One hook start on Codex, p95 | over 100 ms | no synchronous pre-tool hook on the shell tool on Codex; tool-time delivery on edits only |
| 7 | Entrypoint values seen | a desktop surface reports a program's value | every such chat is unsummarized today: a v1 fix and an allowlist fixture come before anything else |
| 8 | Knowledge pages in the real wiki | none or a handful | recall has no real index to be judged on; the "real index" target waits for two weeks of derive v2 in shadow; the migration is small |
| | | hundreds | the migration stays large and its property test is first |
| 9 | Daily commit and push outcomes; signing prompts | 90% or more of active days, no prompt | the machine identity (ST-D4) is less urgent; per-pass local commits stay |
| | | prompts daily, or pushes failing | as designed; if the server refuses an unknown committer, the store's `identity = "user"` fallback |
| 10 | Codex reviews needed after upgrades; foreign files in the plugin cache | any | hosts' unit split is the first Stage 2 package for that area |
| 11 | The host's own memory: on, entries for the project, overlap with the brief | present and overlapping | the `host` arm is run on every core pack, and deliver's overlap count (DL-23) moves to Stage 2 |
| 12 | The MCP server and skills | not connected, or never used unprompted in the report's chat | pull stays the deep path only; nothing is built that depends on the agent choosing to call a tool |
| 13 | The live resume test with the update on | all four planted items returned on both hosts | the headline holds on v1; the brief's form is tuned by suite K, not rebuilt on suspicion |
| | | the model ignores or misreads the brief | the typed brief (DL-10) moves first |
| 14 | Pilot: oracle against off on screened tasks | under 30% fewer events on A, or C's off arm passes | the tasks are rebuilt before any suite result is trusted |
| 15 | Pilot: the evaluation route | headless sessions do not fire plugin hooks under a key | suites move to the Mac home on the plan, packs halve |
| 16 | Managed settings on the real install | hooks or plugins restricted | reach and the tick are re-planned with hosts' managed kit first |
| 17 | Doctor rows not passing, and whether each fix could have been automatic | most could | hosts' repair classes are sized from this list |
| 18 | Steps the owner took by hand since install | the list | it becomes the first-run and "zero actions" budget row by row |

---

## 11. Measurement inside the product

Nothing here is new machinery. Each area already writes content-free local evidence. This area adds the catalogue, three audits' thresholds and one rule.

- **One reader.** Hosts' `eva-md status --json` is the content-free report. Its Level 3 section lists every catalogue metric with its value over 7 and 30 days. After v2 ships, the field report is that command's output, not an agent's reading of files.
- **Audits.** `eva-md audit capture` (ledger), `eva-md audit corpus` (derive's counters, this area's thresholds), `eva-md audit redaction` (ledger). Counts only.
- **Shadow.** A candidate gate, scorer or contract computes what it would have done and logs ids, scores and codes beside the live decision. `eva-eval replay --private` runs a candidate build over the owner's own record on their own machine and prints counts and nothing else. It needs the owner's yes (EV-D7).
- **The rule.** Nothing leaves the machine. A person may share the report. There is no upload path to remove.

---

## 12. How the build is run

### 12.1 Documents: specs, decision records, guarantees as data

`docs/design.md` is not rewritten. A rewrite is weeks of prose with no measurement behind it. It is frozen and superseded piece by piece.

| Step | What | When |
| --- | --- | --- |
| Freeze and split | `docs/design.md`, `docs/verification.md` and `docs/decisions.md` move to `docs/v1/`, split by heading into files with lines reflowed under 400 characters. No words change. Agents can read them at once | first day of Stage 1 |
| Guarantees first | the "Reliability guarantees" table, every audit's must-keep list and every verification bullet become records under `spec/guarantees/<component>.json`, each with the tests that cover it. A meta-test fails if a v1 guarantee row maps to no record. This happens before any v1 text stops being the truth, so no hard-won invariant is lost in the move | Stage 1 |
| Spec per component | `docs/spec/<component>.md`, written by the work package that rebuilds or first touches the component, at its spec step. Components that stay (the hook shell, `paths`, `freetext`, the sealed process runner, install) get a short as-is spec | as packages start |
| Supersede | when a spec is accepted, the v1 sections it replaces get one line at the top naming it. `docs/spec/INDEX.md` lists each component, its spec's status and the v1 sections still in force | per package |
| Decisions | `docs/decisions/<date>-<slug>.md`: context, decision, consequences, what it reopens, status. v1's 17 decisions and its "ruled out" list are imported as accepted records. Names are dated, not numbered, so parallel packages never collide on a number | Stage 1 |

A spec has fixed sections: purpose; interfaces as signatures; data formats; invariants, each with a guarantee id; limits as one table the code reads; failure behaviour; host facts relied on, each with its fixture; out of scope; open questions. At most 24 KB.

A guarantee record:

```json
{"id": "G-HOOK-01", "component": "hook", "text": "A hook never blocks or breaks a chat",
 "from": "v1 design, Reliability guarantees, row 1",
 "tests": ["internal/hook:TestRunAlwaysExitsZero"], "killed_by": "mutation: return 2 on panic",
 "evidence": [{"level": "unit"}, {"level": "live", "host": "claude-code", "cli": "2.1.312", "date": "2026-10-30", "note": "hand check"}],
 "expires_on_change": ["internal/hook/**"]}
```

Tests carry a comment tag, `// guarantee: G-HOOK-01`, which the meta-test reads with the Go parser; no test imports anything for it. Evidence levels are unit, offline end to end, live on a host at a version and date, and real use. Live evidence is shown as stale when a path it covers has changed since. The human page is generated at integration, never edited by hand.

Meta-tests in `internal/spec` replace v1's five tests that police prose (`TestTheDesignNamesTheBuiltCommands`, `TestTheDesignShowsEveryFlagTheReadmeShows`, `TestAgentsNamesEveryFileOfTheCLI`, `TestVerificationCountsTheKeptLines`, `TestVerificationBulletsNameTheirPackages`): every guarantee has a live test tag and a named mutation that kills it; every tag names a real guarantee; every package belongs to one component in the index; no spec is over its size or line limit; the command reference is generated from the command table and matches. `TestThePublicRepoUsesNeutralWording` stays.

This changes two lines of AGENTS.md ("the source of truth for behaviour" and "update docs/design.md in the same change"), so it is an owner decision (EV-D3).

### 12.2 The work-package protocol

A package is one card, one branch, one pull request. The card is `plan/wp/<ID>.md` with a machine-read header:

```json
{"id": "DV-21", "stage": 2, "size": "L", "state": "building", "depends_on": ["DV-10", "DV-20", "LG-13"],
 "owns": ["internal/organize/contract/**", "docs/spec/contract.md", "spec/guarantees/contract.json"],
 "touches": ["internal/update/**: delete only"],
 "class": "moves", "metric": "organize.honest_refused", "baseline_run": "2026-11-24T…", "target": "<= 0.05",
 "acceptance": ["go test ./internal/organize/contract/...", "eva-eval gate --suite E --level 0"],
 "host_facts": ["LG-03:claude.prompt_id"], "live_step": "E held-out, default row, both hosts"}
```

| Step | Who | Output | Gate to the next step |
| --- | --- | --- | --- |
| 1 Spec | the builder agent | the spec change, new or changed guarantee records, the card with exact acceptance commands, the metric and its baseline run | a docs-only pull request |
| 2 Review | a second agent that did not write it | a checklist result: invariants kept, contracts honoured, AGENTS.md rules, host facts confirmed by fixture, the smallest design, acceptance runnable | the orchestrator accepts, or sends it back; the owner is asked only if an owner decision is touched |
| 3 Build | the builder, in its own worktree and branch `wp/<ID>` | code and tests; each guarantee's test written to fail on its named mutation; `scripts/linux-check.sh` output | all acceptance commands pass |
| 4 Adversarial review | a fresh agent given only the card, the spec and the diff, told to break it | failing tests, mutations that survive, edits outside `owns`, a new dependency, a name of a library in user-facing text, a test that depends on speed or touches the real machine, a fixture that looks like a secret, a read of a held-out family | every finding fixed, or recorded and accepted by the orchestrator |
| 5 Integrate | the integrator | rebase on main; the macOS job and the Linux job; Level 0 gates; generated pages; the result row against the baseline; a squash commit with trailers `WP:`, `Guarantees:`, `Measure:` | merged; state `integrated` |
| 6 Prove | the orchestrator, in an evidence window | the live step's rows | state `proven`, or the feature stays `shadow` and a decision is recorded within a week: fix, revert or accept |

Three classes of acceptance, so that agents do not invent a metric where none belongs. **Moves**: a named metric passes its target against the baseline. **Holds**: every gate stays green while something is removed or replaced (owning the codec, deleting v1 code). **Guards**: a new guarantee with a test that a named mutation kills. Principle C of the plan reads "nothing ships on unless a measurement moves"; foundations are judged as holds or guards.

Live steps are batched into **evidence windows**, twice a week, run by the orchestrator inside the budget. A builder never spends quota.

### 12.3 Ownership, branches and the hot files

The main device is that v2 is built in new packages beside the old ones, behind flags that default to v1's behaviour, on `main`. Parallel tracks then own disjoint folders, `main` stays releasable, and the real install can run shadow modes from ordinary releases. The alternative, a long-lived v2 branch, hides shadow from the real install until the end and ends in one very large merge.

| Area | Owns |
| --- | --- |
| ledger | `internal/ledger/**`, `internal/redact/**` except `baseline`, `internal/identity/**`, the capture half of the host adapter, recorded host fixtures under `testdata/hosts/` (append-only: a new host version adds a folder) |
| store | `internal/store/**`, `internal/gitrun/**` |
| derive | `internal/work/**`, `internal/organize/**`, `internal/modelrun/**` |
| deliver | `internal/recall/**`, `internal/deliver/**`, `internal/access/**`, `internal/tok/**`, `internal/saferead/**`, `internal/searchindex/**`, `internal/mcp/**` |
| hosts | `internal/host/**`, `internal/install/**`, `internal/release/**`, `internal/health/**`, `internal/platform/**`, the install scripts, the release workflows |
| evaluate | `internal/eval/**`, `internal/spec/**`, `cmd/eva-eval/**`, `eval/**`, `spec/metrics.json`, `plan/**`, `scripts/benchmark.py`, `scripts/linux-check.sh`, `.github/workflows/ci.yml` |

Rules:

1. **A diff stays inside `owns`.** A CI job reads the card named in the pull request title and fails on any path outside `owns` and the approved `touches`.
2. **v1 packages are frozen** (`internal/update`, `resume`, `knowledgepage`, `threadpage`, `systempage`, `okfadapter`, `wikigit`, `wikiinit`, `catchup`, `inbox`, `lint`). Only a fix the orchestrator approves and the cut-over packages touch them. No agent improves code that is scheduled for deletion.
3. **Hot files have one writer, the integrator**: `go.mod`, `go.sum`, `AGENTS.md`, `CLAUDE.md`, `README.md`, `scripts/LICENSES.txt`, the hook declarations in `internal/install`, generated pages. A builder states the change it needs on its card.
4. **Seams make hot files additive.** One early package gives the command line a table with one file per command, and gives the hook a handler list, so adding a command or a shadow handler is a new file and not an edit to `main.go` or `hook.go`. The test that makes every new command file edit AGENTS.md is replaced in the same package.
5. **Contracts before parallel work.** Each area's interface package (types and signatures only) lands in Stage 1. After that a contract changes only through a decision record signed off by every area that consumes it.
6. **The hook declaration is frozen by hosts' rule** (generation 1 at the end of Stage 3). No package changes a declared event, matcher or timeout outside that step.
7. **Branches.** `wp/<ID>-<slug>` from `main`; a builder rebases only its own branch; nobody force-pushes a shared branch; merges are squashes by the integrator in dependency order. Agents commit with the identity and signing the environment gives them and never change either.
8. **Generated and golden files** are regenerated only by their owner's package or by the integrator.
9. **Held-out families and `gates.json`** are evaluate's. A package that needs a gate changed says so on its card; the change is its own pull request.

The integrator is a queue, not a person's attention: a package whose diff is inside `owns`, whose gates are green and whose adversarial review is closed merges without the orchestrator reading it. The orchestrator reads specs, decision records, contract changes and anything that touches a hot file.

### 12.4 Release gates

| Gate | Holds for | Evidence |
| --- | --- | --- |
| R0 | every merge | the macOS job and the Linux job; every hard gate at Level 0 |
| R1 | every merge that touches a gated component | regression gates at Level 0 (latency, fidelity on cassettes, the retrieval tuning set); a move beyond tolerance needs a new baseline accepted on the card |
| R2 | every release | both canary tiers green and under 36 hours old |
| R3 | a release that changes the contract, the prompt or the model policy; every minor | E on held-out scenarios, both hosts, the default row |
| R4 | a release that changes a delivery default or the brief's form | the core pack at Level 2 |
| R5 | the cut-over release | MIG green; the owner's dry run reports nothing unaccounted |
| R6 | a release that touches install or setup | hosts' scorecard and fault matrix |
| R7 | every release | the desktop apps checked by hand, as in v1, recorded as dated evidence rows |
| R8 | before the word "best" | the claim run and the published package |

Hard gates are never waived. A regression or claim gate may be waived only by the owner, in a dated decision record; a waived claim gate leaves its feature in `shadow`. The release path reads `gates.json`: the Cut a release workflow's plan step fails when a blocking gate is red or stale.

---

## 13. Kept, rebuilt, evolved, removed

### 13.1 Invariants

All fourteen must-keep items of the evaluation audit hold. Three are extended and one is replaced on a schedule.

| Invariant | In v2 |
| --- | --- |
| Script-first facts with derived obligations; the enforceable and content split | kept; facts gain slots, permitted kinds and `must` |
| One loop for canned and live answers | kept: `organize.Derive` with a simulated, recorded or live runner |
| Adversarial outputs with an expected reaction per failure mode | kept: the simulated model's knobs, one family per mode |
| Refusals cost; host failures stop a run unjudged; a model fallback stops it | kept in the runner and the row's `status` |
| Quota safety: `--host`, `--spend-model-quota`, the count stated first, a temporary home, never built by a release, never imported by the product | kept, and extended with token and session ceilings and the home guard |
| Test isolation; tests never depend on machine speed; a latency gate outside `go test` | kept |
| The benchmark's isolation and functional assertions; the latency targets | kept; the targets become ceilings that fail |
| Recall's precision-first floor as a CI gate | kept while v1's recall exists; replaced by the held-out gate when it is deleted |
| Logs hold counts and names, never text | kept for every row, report and log this area adds |
| macOS is the run of record; CI actions pinned by commit, a read-only token | kept; a scheduled live job, if the owner wants one in CI, reads its keys only on `schedule`, never on `pull_request` |
| Fuzzers on the parsers | kept |

### 13.2 Rebuilt, and what a smaller change would leave

| Rebuilt | Findings | A smaller change would leave |
| --- | --- | --- |
| The fidelity scorer: black-box, fact-level, with a precision side | EVL-03, EVL-10, EVL-14, UPM-01 | value checks added to `Measure` still key on a codename in one section and still judge a page model that is being removed |
| The scenario corpus: generated families, realistic size, sealed held-out | EVL-03, EVL-14, EVL-05 | twelve hand scripts of 1 KB on one branch, tuned on and judged by the same set |
| The documents: specs, decision records, guarantees as data | PRD-09, EVL-15 | an index over the old files leaves 21 lines no agent reads to the end, and one bullet per line that every parallel change conflicts on |

### 13.3 Evolved, where evolving gets most of the benefit

| Evolved | Findings | Why not a rebuild |
| --- | --- | --- |
| `cmd/live-accuracy` into `eva-eval run --level 1`: a matrix, trials, stored rows, tokens, mend counts | EVL-02, EVL-04, UPW-02 | its loop, guards and refusal accounting are right; Stage 0 adds only a results file and runs it as it is |
| `scripts/benchmark.py` into a gate | EVL-09 | the scenarios and isolation are right; it lacks an exit code, enough samples and a baseline |
| `docs/design.md`: frozen, split, superseded section by section | PRD-09 | the text is correct; its form is the fault |

### 13.4 Added

The continuation harness (EVL-01, PRD-05, BRF-05, PGS-06, MCP-05); the results store with versions and baselines (EVL-04, EVL-13); the retrieval set with negatives and a held-out half (EVL-05, RCL-05, SRC-04); corpus thresholds (EVL-06); freshness and cost gates (EVL-07, EVL-08); the canary's signed-in tier (EVL-11, OPS-06, CUP-06); the poisoning suite (EVL-12); the migration rehearsal (STO-11); and the wiring that takes the other areas' suites into one store (CAP-08, CUP-07, RED-06, OKF-13, SET-13). System pages get no suite of their own (SYS-02): derive freezes them, and they appear only as one arm in C and F.

### 13.5 Removed

`cmd/live-accuracy` once `eva-eval` reproduces its run. Codename matching as the measure (kept as a locator in the marked families). The two scoring structures that can disagree, `Metrics.Perfect()` and `Tally`. `update.Guarantees` as the oracle (derive keeps its own assertions; they are not the judge). The five prose-policing tests. `docs/verification.md` as a hand-kept ledger. Hand-copied benchmark figures in documents. `recall_test.go`'s fixture as a gate, when v1's recall is deleted.

---

## 14. Host facts this design leans on, and how each is confirmed first

All from documentation read on 2026-11-10. EV-03 confirms each on the current releases before Stage 1's harness packages fix their drivers.

| # | Fact | Used for | Confirm | If false |
| --- | --- | --- | --- | --- |
| 1 | `claude -p` without `--bare` loads a plugin given by `--plugin-dir` and fires its hooks, with an API key | the headless driver and the arms on Claude Code | a scratch plugin whose hook writes a marker | the Mac home on the plan; pty |
| 2 | `--bare` is not yet the default for `-p`; when it is, a flag keeps the non-bare behaviour | the same | `claude --help` at each release; the canary | the Mac home on the plan |
| 3 | `--settings` and `CLAUDE_CODE_DISABLE_AUTO_MEMORY` turn the host's memory off for one run | the `off` and `eva` arms | a run that asks the model what memory it was given | a clean config folder per run |
| 4 | `--output-format stream-json` and Codex's `--json` give tool events and token usage with cached input apart | agent events, tokens | read one stream of each | count tool events from eva-md's own ledger; tokens from the final envelope |
| 5 | `--max-turns` bounds a run; `claude -p --resume <id>` and `codex exec resume` continue a session with hooks firing on each prompt | task bounds; corrections per task | a three-prompt scripted session | a wall-clock limit with the process group killed, as v1's runner does; single-attempt tasks |
| 6 | `codex exec` fires hooks declared at the managed level without review, in a container this project administers | the Codex driver in containers | a managed requirements file with one scratch hook | the Mac home with one human review; user-level hooks there |
| 7 | In the Mac home, Codex's approval of eva-md's hooks holds across runs while the hooks unit's bytes are unchanged | the Codex canary tier | hosts' HO-01 rows 2 and 3 | one review per plugin change, counted as a canary result |
| 8 | With `HOME` set to another folder, each host signs in and stays signed in there | the evaluation home | `eva-eval home init`, by a person | a dedicated macOS account |
| 9 | Claude Code's own memory writes during an interactive session driven through a pty | the `host` arm | one scripted session, then count memory files | the `host` arm is run by hand per claim release |
| 10 | The host's terminal UI can be driven through a pty: a typed prompt is seen as typed, `/clear` and `/compact` work | the canary's interactive path, driver parity | one scripted session against the recorder | those checks stay by hand per release, as in v1 |
| 11 | `claude plugin eval` exists with a no-plugin baseline | a possible driver for the `off` and `eva` arms on Claude Code | read its page and run it once | the harness's own driver, which is needed for Codex in any case |
| 12 | The hosts report the model that answered | the row's `model_answered`; stopping on a fallback | the stream's opening message; derive's DV-11 | rows are marked "model unconfirmed" and excluded from claims |

---

## 15. Migration

This area holds no user data. It owes the migration three things.

1. **The rehearsal** (section 6.7): the ledger's, store's and derive's migrations are judged by the fidelity scorer and the brief probes on generated v1 state, and the owner's dry run on a copy of the real wiki and inbox reports counts in the same metric ids. The cut-over release is gated on it (R5).
2. **v1's evaluation assets move without a gap.** The twelve scripts become the first twelve families and keep running against v1's update, in CI, until that code is deleted. `cmd/live-accuracy` stays until `eva-eval` reproduces its run on the same scripts, row for row. v1's recall fixture stays a gate until v1's recall is deleted. v1's hand-run live checks are imported as evidence rows dated 2026-10-30 and marked stale, which is what they are (EVL-13).
3. **The documents move with no rule lost**: guarantees first, then the freeze, then supersession per component (section 12.1). Until a component's spec is accepted, the v1 text governs it.

---

## 16. Alternatives weighed

| Question | Options | Choice and reason |
| --- | --- | --- |
| Scorer | patch v1's; a model judge; black-box slot match on the export | Slot match. A patch keeps the codename ceiling. A judge is lenient to vague answers in the published audits, costs quota on every run and needs its own validation. Exact spans in contract v2 make attribution deterministic |
| Scorer input | parse the Markdown itself; use derive's types; a stable export | The export. A second parser duplicates the codec; derive's types are the thing under test; an export is what a user or another tool would read, and the store has its own conformance suite |
| Held-out | a secret set; a fixed set in the repository; drawn from the release commit | Drawn from the commit, plus a few held-out families. A secret cannot be kept from agents that can read the repository; a fixed set is tuned on within weeks |
| Session A | run it live every time; replay recorded or generated payloads | Replay. It exercises capture through the real hook binary, costs nothing, and is identical across arms |
| Session B | pty only; headless with one seam; inject the brief by hand | Headless with the seam, parity checked by pty. Injecting by hand would skip the delivery path being measured |
| Baseline arms | rerun per build; cache with a drift check | Cache. They do not run eva-md |
| Where suites run | the signed-in Mac for everything; containers with keys; CI on every PR | Containers for suites, the Mac for what needs sign-in, nothing live on a PR |
| Primary outcome | solve rate; efficiency and necessary-memory tasks | The second. The first needs samples no release budget buys |
| Results home | CI artifacts; in the main repository; a separate evidence repository | Summaries and baselines in the main repository; per-item rows in an evidence repository the evaluation machine can push to without holding a credential for the product's code |
| Public benchmarks | adopt one as the headline; none; an occasional sanity run | None in v2.0. The conversational ones measure another product; the coding ones have no maintained harness for these hosts. The packs are published so others can run them |
| Fixture repositories | public issue sets; in-house | In-house. Models have seen the public solutions, which hides any memory effect |
| Documents | rewrite the design; index it; freeze and supersede | Freeze and supersede |
| Build method | a v2 branch; trunk with flags and new packages | Trunk |
| Canary on Claude Code | `claude plugin eval`; own driver | Decide in the pilot. It is one host and has no oracle or control arm, so it cannot replace the harness |

---

## 17. Contracts

Accepted as written: the ledger's C-E2 (its capture scorer and redaction corpus as packages the harness calls); the store's S-E1 (its suite and benchmark report in the row format) and S-E2 (the corpus audit reads through the one reader; poisoning injects through foreign commits); derive's D-E1 (the fidelity suite, held-out scripts, twins, the poisoning corpus, the aging run, a results store), D-E2 (`organize.Derive` as a library with a fake, recorded or live runner), D-E3 (corpus thresholds decide the review tier), D-E4 (the default-branch variant of B); deliver's V-E1 (the labelled set, the suites and their arms), V-E2 (replay, the decision log's schema, shadow), V-E4 (delivery ids that join to outcomes); hosts' H-E2 (`status --json` pinned; the fault matrix and scorecard as libraries).

Accepted with a change: **C-E1** asks this area for recorded fixtures; the recorder is the ledger's own LG-03, and this area supplies the gate wiring and the canary's schema diff that proposes new fixtures. **V-E3**: defaults reach the product as an embedded `defaults.json` written at integration, not by reading the results store at run time. **H-E1**: the runner for the nightly canary is `eva-eval canary`; the fake hosts' new behaviours are hosts' HO-14.

| Id | With | Direction | One line |
| --- | --- | --- | --- |
| E-L1 | ledger | need | The real `eva-md hook` accepts recorded and generated payloads on stdin in a temporary home; the fixture format is documented and keyed by host and CLI version |
| E-L2 | ledger | need | The actor seam is one file in the host adapter behind the `evamd_eval` tag; a release build does not contain it |
| E-L3 | ledger | need | Delivery events are readable per session by id, and the ledger's audit counters use catalogue metric ids |
| E-S1 | store | need | `eva-md export --json`: one typed record per item (id, kind, state, tag, scope, project, workstream, statement, detail, anchors, sources as session and sequence, supersedes, flags, times), schema versioned; a v1 form through the frozen v1 readers |
| E-S2 | store | need | The migration dry run reports in catalogue metric ids, counts only |
| E-D1 | derive | need | The contract's schema and refusal codes as generated data; a cassette key of contract version, rendered input, model and effort; a recorded miss is an error, never a live call |
| E-D2 | derive | need | The runner's `Outcome` (class, model asked and answered, usage with cached input apart) reaches every row; a fallback to another model stops a run unjudged |
| E-D3 | derive | promise | The frontier table of DV-03 and the rule that names the default row are computed by `eva-eval`, so the default is a query on stored rows |
| E-V1 | deliver | promise | The labelled set, the sealed held-out half and the scorer; `defaults.json` emitted from gates |
| E-V2 | deliver | need | The pack can be compiled from any store folder the harness names (for the control and oracle-store arms) with no product flag |
| E-H1 | hosts | need | Install, upgrade and uninstall run without a terminal into any `HOME` from a local build; the plugin can be rendered to a folder for `--plugin-dir`; the release path never builds `cmd/eva-eval` or passes `evamd_eval` |
| E-H2 | hosts | need | `status --json` lists every Level 3 metric in the catalogue; the Cut a release plan step runs `eva-eval gate --release` and fails on a red or stale blocking gate |
| E-H3 | hosts | promise | The row format, the catalogue, `eva-eval canary` and the freshness check for canary rows |
| E-X1 | all | need | Every work package names the catalogue metric it moves, or its class (holds, guards); no area adds a second results format |
| E-X2 | all | need | Each area lands its interface package and its spec skeleton in Stage 1, and keeps every diff inside the paths its card owns |
| E-X3 | all | promise | A v1 baseline row for the area's suite exists before the area's first Stage 2 package starts |

---

## 18. Where the lead's draft is wrong or over-built

1. **"A harness that is part of the product."** Two things are meant and they must be kept apart. The harness is in the repository and gates releases, and it is never in a release binary: v1's rule that nothing shipped can spend quota or import the suite is worth more than the phrase. What ships is the content-free counters and one reader.
2. **Four conditions are not enough, and three of them should not be rerun.** The host's own memory is a fifth arm, and the `eva` arm has to say what the host's memory is doing. Off, oracle and host do not depend on eva-md and are cached; rerunning them per build would triple the cost for no information.
3. **A solve-rate gain cannot be the gate.** The draft's evidence expects 1 to 5 points; verifying 3 points needs several hundred pairs per host per release. The gates are steps, tokens, corrections and tasks where memory is necessary. Harm is gated on exact injected share and the control arm, and its solve-rate interval is published as it tightens.
4. **"Nothing ships unless a measurement moves" needs three classes.** Applied literally it makes agents invent metrics for a codec or a spec. Moves, holds and guards.
5. **L8 lists everything at once.** Order matters more than coverage. Stage 1 builds only what gives v1 a baseline and gates Stage 2: the scenario model, the fidelity scorer, the results store, the retrieval set, the latency gate, and suites A, C and K on v1's brief. Poisoning's act stage, aging, switching and drift at Level 2 come with the features they judge.
6. **Proof is not Stage 5.** The instruments come first and evidence accumulates. Stage 5 is one claim run and its publication.
7. **Stage 0 should not block Stage 1,** and its counted week needs a release that changes no plugin byte, or Codex capture stops on the one real install during the week meant to measure it.
8. **Do not rewrite the design document.** Freeze it, make it readable by splitting, and let each rebuild's spec supersede its sections. Import the guarantees before anything is frozen.
9. **Fault injection belongs offline.** On a real host only a short safe set runs. Hosts' fault matrix on fake hosts with an injected clock is the instrument.
10. **A "real-host canary" is two tiers with different owners:** no sign-in on throwaway runners (hosts), and a signed-in home with a fixed small budget (this area).
11. **No model judge, no public benchmark as a target, no competitor arms in v2.0.** Each adds cost and argument and none changes a build decision.
12. **The orchestrator must not be the merge queue.** Mechanical checks merge mechanical packages. The orchestrator reads specs, contracts, decisions and hot files.
13. **Headless is not interactive.** The draft does not say how a real host is driven. A headless session is a program's chat to capture and to delivery, so without the seam and the parity check the suites would measure a path no person uses.

---

## 19. Owner decisions

| Id | Question | Options | Recommendation | Why | If no |
| --- | --- | --- | --- | --- | --- |
| EV-D1 | Where do live evaluations run, and on whose account? | containers with API keys kept for evaluation, plus a signed-in evaluation home on a Mac for the canary tier; the signed-in home on the plan for everything; by hand per release | Containers with keys for suites, the Mac home for what needs sign-in | Clean homes, parallel runs, no draw on anyone's plan, and the agent team can run evidence windows itself. Scheduled programmatic use of a plan login is the path one vendor has twice tried to re-bill | The plan login only. Cost: packs halve, a release run takes days of one machine and shares its owner's limits, the canary is weekly |
| EV-D2 | May live evaluation run with standing leave inside a budget, not asked for each time? Changes the AGENTS.md rule on the live accuracy run | standing leave inside an evaluation home or container, under weekly and per-release ceilings the owner sets; ask each time | Standing leave with ceilings; a start of 100 M tokens a week and 200 M a minor release, reset after the pilot | The method needs evidence windows twice a week; a question each time makes the orchestrator wait on a person | Ask each time. Cost: packages sit `integrated` and unproven until someone answers |
| EV-D3 | May per-component specs, dated decision records and guarantees as data replace `docs/design.md` and `docs/verification.md` as the source of truth, with the five prose-policing tests replaced? Changes two lines of AGENTS.md | yes; split and reflow only | Yes | An agent cannot load the design, 21 of its lines are cut by a standard reader, and one bullet per line makes parallel work collide | Split and reflow only: the prose stays the truth and stays policed. Cost: every behaviour change still edits shared files, and "which guarantee has no live evidence" stays a reading task |
| EV-D4 | May an evaluation-only build let a headless session count as a person's chat, with a test that no release contains it? | the seam with parity checks; pty only | The seam | Without it suites measure the program path, or run three to five times slower through a terminal UI that changes weekly | Pty only. Cost: suites shrink by about half and break on host UI changes |
| EV-D5 | What must eva-md beat to be called best? | memory off and each host's own memory; also named third-party tools; also public benchmarks | Memory off and the hosts' own memory, on a published harness | Those are what a user would otherwise have. Competitor arms cost a run each and invite dispute over setup | Add up to two named tools as arms in the claim run. Cost: about 250 sessions per tool per host, and their setup must be defensible |
| EV-D6 | May a model judge decide any gated number? | none; a validated judge on a sample as a diagnostic; yes | None in gates | Slot match and hidden tests cover the gated numbers; published judges accepted most vague wrong answers | A judge on a 10% sample for statement faithfulness, validated on seeded distortions first, from the other provider. Cost: about 10% more Level 1 calls |
| EV-D7 | May a candidate build be replayed over your own local record, on your machine, printing counts only? | yes, as an opt-in command; no | Yes | It is the only check on real chats, and nothing leaves the machine | Synthetic scenarios and the field report only. Cost: a rule that misbehaves on real phrasing is found after release |
| EV-D8 | Where do per-item results live? | a separate public evidence repository; a branch of this one; the evaluation machine only | A separate repository | Rows of synthetic scenarios are safe to publish and the claim needs them public; the evaluation machine then holds no credential for the product's code | The machine only, with summaries added by pull request. Cost: target 14 is not met |
| EV-D9 | How is the build run? | trunk on `main` behind flags, new packages beside old, v1 frozen, gates waived only by you; a long-lived v2 branch | Trunk with flags | `main` stays releasable, shadow modes reach the real install, parallel tracks own disjoint folders | A v2 branch. Cost: no shadow evidence from real use until the end, and one very large merge |
| EV-D10 | Will you install one v1 point release that adds only counters, and bring back a second report after a week? | yes; the first field report only | Yes | Three rates that size Stage 2 (events held back, stale briefs, checker refusals by rule) are not in v1's files today | The first report only. Cost: rows 3 and 5 of the Stage 0 table are decided on inference |

---

## 20. Risks

1. **The evaluation becomes a second product.** The staging in section 18, point 5, is the guard, with one rule: a suite is built when a decision in the plan waits on its number.
2. **Headless results do not transfer to interactive use.** The seam, the daily parity check and the field counters bound it. They do not remove it.
3. **Variance swamps the effect.** Screened tasks, efficiency outcomes, three trials, pooled intervals. If the pilot shows the oracle barely beats off, the tasks are wrong and are rebuilt before anything is claimed.
4. **Hosts and models move weekly.** Cached baselines go stale; the drift check and the model id key catch it at the price of a rebuild of one to two days per host.
5. **Tuning to the suites.** Held-out draws from the release commit, held-out families, adversarial reviewers, and the private replay on real chats.
6. **Synthetic chats are unlike real ones.** The paraphrase bank, model-written items in the retrieval set, tool-heavy distractors at full chunk size, and the counted week on the real install.
7. **Key and plan behave differently** (limits, the bare-mode default). The canary runs on the plan; row 2 of section 14 is checked at every release.
8. **Agents game a gate.** `gates.json`, the held-out families and baseline acceptance belong to this area and the owner; the ownership check and the adversarial step are the enforcement.
9. **An invariant is lost in the document move.** Guarantees are imported and tested before the freeze, and a meta-test maps every v1 guarantee row to a record.
10. **The integration queue stalls.** Mechanical merges for mechanical packages; two evidence windows a week.
11. **Poisoning tests run attacks.** Payloads ask only for marker actions, containers have no network beyond the model provider, and the corpus is synthetic.
12. **Cost overruns.** Ceilings in the runner, a plan printed before any call, a cut order agreed in advance.
13. **The seam leaks into a release.** Two tests and the release workflow's own check.
14. **In-house tasks are too easy or too odd.** The screen measures it; the first twenty tasks are reviewed by a person before the pack is frozen.

---

## 21. Work packages

Sizes: S up to two days of one agent, M up to a week, L more. "Live" steps need the owner's leave (EV-D2) and run in an evidence window.

| Id | Stage | Size | Outcome | Scope | Depends on | Acceptance, and the measure that must move |
| --- | --- | --- | --- | --- | --- | --- |
| EV-00 | 0 | S | The field report is folded into the plan | the decision table of section 10 filled in; a dated decision record; changed stage order and package sizes. No code | the owner's report | Every row has a measured value or "unknown, because"; each row that crossed a threshold names the packages it moved. Measure: rows decided, 18 of 18 |
| EV-01 | 0 | S | v1's first live numbers are stored and versioned | a run-book for the live gates; a `--out` file on `cmd/live-accuracy` in the row format (tokens, seconds, the validator's report, refusals, versions); the hand-run resume test and the MCP and skill checks as evidence rows. With DV-02 | DV-02; the owner's leave | Rows exist for both hosts at the highest and a low effort; a test finds no fixture text in a row; rows 1, 2, 4 and 13 of section 10 can be read from them |
| EV-02 | 0 | S | The counted week is run and read | the list of counters the point release must carry; a check that both plugin folders are byte-identical to the last release; the reading of the second report | LG-01, LG-02, DV-01, DL-02, HO-02; EV-D10 | The existing byte-identity test passes on the release; after a week the report answers rows 3, 5, 6, 9 and 10 |
| EV-03 | 0 | M | The evaluation route is proven and priced | throwaway drivers; the 12 facts of section 14 on a container and on a Mac home; 10 screened tasks, 3 arms, 3 trials on v1's brief | EV-D1, EV-D4; the owner's leave | A fact sheet with each row answered and its fallback chosen; measured tokens and minutes per session; the variance that sets pack sizes; section 9 rewritten from measurements |
| EV-10 | 1 | M | Specs, decisions and guarantees a team can build from | `docs/v1/` split and reflowed; `docs/spec/INDEX.md` and the template; `docs/decisions/` with v1's imported; `spec/guarantees/`; `internal/spec` meta-tests; the five prose tests removed; AGENTS.md updated | EV-D3 | Every row of v1's guarantees table and every must-keep item maps to a record with a tagged test; no file under `docs/spec` or `docs/v1` has a line over 400 characters; `scripts/linux-check.sh` passes. Measure: largest file an agent must read, 394 KB to 24 KB |
| EV-11 | 1 | S | The work-package protocol is enforced by the repository | `plan/wp/` cards and their checker; the ownership job in CI; the command table with one file per command; the generated command reference | EV-10 | A pull request that edits a path outside its card's `owns` fails CI (shown with a seeded diff); adding a command is one new file; no behaviour change (the benchmark and all tests unchanged) |
| EV-12 | 1 | M | One results store, one catalogue, gates as data | `internal/eval/results`; `spec/metrics.json`; `eval/gates.json`; `eva-eval gate`, `diff`, `baseline accept`; staleness from git paths | none | Fixture rows give exact statistics under a fixed seed; a doctored row flips a gate; a planted codename is found in no row or report; a baseline goes stale when a covered path changes |
| EV-13 | 1 | L | Scenarios at realistic size, with a held-out half that cannot be tuned on | `internal/eval/scenario`: the model, 24 families (v1's 12 ported), the generator, the paraphrase bank, both renderers, packs with hashes, the commit-hash draw | LG-10 for the payload format | v1's 12 scripts render to byte-identical v1 inbox entries; every family has a golden instance; two draws from different commits share no instance; fixtures pass the redactor's scan for anything but listed fake values |
| EV-14 | 1 | M | A scorer that sees wrong values and noise, and v1's score on it | `internal/eval/score` (fidelity), `internal/eval/sim`, the v1 export adapter; v1 scored at Level 0 | EV-13 | The seeded set (wrong value, dropped date, dropped condition, invented item, wrong voice, stale kept, correct in another permitted place) is scored right in every case; the scorer imports nothing from `internal/update` or `internal/organize`. Measure: seeded distortions caught, 3 of 7 to 7 of 7 |
| EV-15 | 1 | M | Live fidelity as a routine, with cassettes | `internal/eval/run` at Level 1: matrix, trials, guards, cassettes; v1's live baseline on both hosts; `cmd/live-accuracy` removed after row parity | EV-12, EV-14; live | `eva-eval plan` prints calls and tokens and spends nothing; a run without the flag exits 2; a home without the marker is refused; a recorded run replays at Level 0 with no network (test seam). Measure: v1 `fact_recall` and `wrong_write` exist with intervals |
| EV-16 | 1 | M | Latency that fails a build | `scripts/benchmark.py` gate mode, interleaved binaries, stored baseline, the three added scenarios; the macOS job runs it | EV-12 | A binary with a seeded 20 ms delay in capture fails the gate; an unchanged binary passes in 20 of 20 CI runs; process start is inside every hook figure |
| EV-17 | 1 | L | The headline has a number: continuation on v1 | drivers and arms; three fixture repositories; packs A (20), C (24) and K; the funnel; baselines for `off`, `oracle`, `host` and v1 as `eva` | EV-03, EV-12, EV-13; live | Every task passes its screen; rows for both hosts with intervals; the drift check invalidates a cache when a seeded model id changes. Measure: v1's events ratio on A and gap closed on C exist |
| EV-20 | 2 | M | The rebuilt areas are gated as they land | wiring for CAP, RED, STO, E on contract v2, F and L held-out, the poisoning store stage, hard gates in `gates.json`, `defaults.json` | LG-21, ST-05, DV-21, DL-06 as each lands | CI fails on a seeded leak, a seeded misattribution, a seeded unaccounted range and a seeded scope leak; each area's first Stage 2 package has a v1 baseline row |
| EV-21 | 2 | M | The migration is rehearsed before any user runs it | v1 state built at scale by v1's code; MIG scoring on both sides; the dry run's report format | ST-07, LG-25, DV-25 | `migration_loss` 0 on 200 generated v1 installs; K's probes agree before and after; a seeded dropped statement is caught |
| EV-22 | 2 | M | A host release that breaks an assumption is seen within a day | `eva-eval canary` on the Mac home: schema diff, probes, parity, the weekly fault set, rows and the freshness check | EV-15, EV-17, LG-03, HO-14 | A fake host that renames a payload field turns the canary red and proposes a fixture; a stale canary row fails the scheduled check |
| EV-23 | 2 | S | One content-free report covers every area | the Level 3 section of `status --json` checked against the catalogue; `eva-eval replay --private` | E-H2; EV-D7 | A meta-test fails when a Level 3 metric is missing; the replay's output holds only numbers and codes (a planted-codename test) |
| EV-30 | 3 | M | Rules and stale claims have numbers | suites G and H: rule checks, tempting tasks, drift-world generator and mutations | EV-17, DL-18, DL-19 | A seeded always-violate agent scores 100% violations and a seeded never-violate one 0%; every mutation class yields the expected claim class at Level 0. Measure: targets 8 and 9 |
| EV-31 | 3 | M | Storing, delivering and acting on untrusted text are measured apart | the payload corpus across 12 channels; the forced-plant variants; the act stage in containers with the network closed | EV-17, DL-17, ST-09 for the remote channel | A build with the taint rule removed fails `stored_with_authority`; one with the push gate removed fails `delivered_tainted`; marker actions are detected in a seeded transcript. Measure: target 10 |
| EV-32 | 3 | M | Quality over 200 sessions, and corpus thresholds | suite J with maintenance events; thresholds for COR | DV-30, DV-32, EV-21 | A seeded regression at a maintenance event fails the gate; `ranges_unaccounted` stays 0; the thresholds reproduce derive's DV-D8 rule on a seeded store |
| EV-33 | 3 | S | Harm is measured where it can be | the no-opportunity set, the control arm, `injected_share`, the noise floor | EV-17 | `injected_share` equals a hand count on three sessions; an eva arm with a seeded 20% token overhead fails. Measure: target 7's ship column |
| EV-34 | 3 | M | Switching and cross-host continuity | suite B with its default-branch variant; C's cross-host variants | EV-17, DV-23 | The control and switched runs share one verifier; a cross-host task fails under `host` and passes under `oracle`. Measure: targets 3 and 4 |
| EV-40 | 4 | S | Reach is judged by the same suites | C's cross-machine variant through the store's sync; arms for the hook-free floor and Linux | ST-09, DL-22, HO-40 | The same packs run unchanged on each new surface; rows name the surface |
| EV-50 | 5 | M | The claim, or a plain statement of what fell short | the claim run on held-out draws; the evidence package; a decision record on what may be said | everything above; live | Every row of section 8 has a value, an interval and a command; rows are public; any unmet row is named in the release notes |
| EV-51 | 5 | S | Releases read the gates | `eva-eval gate --release` in the Cut a release plan step and in `scripts/release.sh --plan`; defaults follow results (with DL-25) | EV-20, hosts | A doctored red gate stops a dry-run release; a doctored regression turns a feature to `shadow` in the emitted defaults |

Order that matters: EV-10 and EV-11 on the first day of Stage 1, since every other package uses them; EV-12 before any suite; EV-13 before EV-14 and EV-17; EV-14, EV-15 and EV-17 before the Stage 2 package each one judges; EV-21 before the cut-over.

---

## 22. What a field report from a real install should answer for this area

In general terms, counts only, nothing quoted.

1. Whether any background update has ever finished, and the counts by outcome and reason, since every Stage 0 threshold on the checker starts there.
2. How long from a chat's last entry to its saved page, and at how many chat starts the work state was marked stale or pending.
3. Seconds and tokens per update pass, by model and effort, and whether any pass hit its time limit.
4. Whether knowledge and rules pages exist at all and how many, so the plan knows if there is a real index to judge recall on and how large a migration to rehearse.
5. Whether recall, a pointer or a reminder reached the report's own chat, and whether the agent used a memory tool without being told to.
6. Hook times per event, and the wall time of one hook run under each host.
7. Which host versions and surfaces are in use, the entrypoint values recorded, and whether settings are managed.
8. Whether each host can run headless on that machine, signed in on a plan or with a key, by reading help output and versions only.
9. Whether each host's own memory is on, how many entries it holds for the project, and whether they repeat the brief.
10. Which counts the agent wanted for the report and could not get from eva-md's files, which is the list of counters v1 lacks.
11. For the owner: every step taken by hand since install; whether a separate account or folder on a Mac they control could serve as an evaluation home; whether keys for evaluation exist.
12. For the owner: whether `doctor --live` or the live accuracy run was ever run, and what it said.
