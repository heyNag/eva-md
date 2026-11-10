# Critique: delivery by a team of coding agents

Lens: can a team of coding agents, led by one orchestrator, build the six designs in the order given, with the one real install working at every step?

Read: the design brief, the lead's notes, the six designs (ledger, store, derive, deliver, hosts, evaluate), their work-package tables, contracts, migrations, host-fact tables and owner decisions. Checked against the code at `the repository`: `.github/workflows/ci.yml`, `scripts/benchmark.py`, `internal/install/templates.go` and its skill templates, `internal/hook/hook.go`, `cmd/eva-md/docs_test.go`, `cmd/eva-md/setup.go`, `cmd/eva-md/timelimits_raised.go`, `cmd/eva-md/upgrade.go`, `internal/install/record.go`, the install tests, and the Linux check log of this session.

Totals taken from the designs: 128 work packages (ledger 19, store 14, derive 22, deliver 25, hosts 24, evaluate 24): 13 L, 71 M, 44 S. 51 owner decisions (8, 8, 8, 9, 8, 10). 81 host facts to confirm on real hosts (14, 9, 12, 20, 14, 12).

---

## Verdict

Not in the order given. Each design is buildable by itself: the cards are specific, most acceptance checks are deterministic and offline, and the evaluate design's protocol (card, spec step, adversarial review, integrate, prove) is the right method. The six do not compose into a schedule yet. Five things would stop a team inside the first two stages:

1. Stage 2 is serial, not parallel. The "depends on" columns point at other areas' implementations, not at contracts, so derive and deliver wait for the ledger's two L packages and the store's one.
2. Eight or more cards delete or replace live v1 code before the cut-over, against the trunk-with-flags method and against each area's own shadow plan.
3. The cut-over is the riskiest step for the one real install and no package owns it. Its gate (a shadow week) sits in Stage 5 packages, and a rollback loses the chats captured after it.
4. The files most packages edit (`internal/hook`, `cmd/eva-md`, `internal/config`) have no owner, and Stage 0 sends five areas into the same lines before the seams exist.
5. Many needs that later designs put on earlier ones have no work package in the provider's table.

Behind those sit the person gates. Nearly every first package of every area waits on an owner decision, a logged-in Mac or model quota, and the plan does not rank or batch them.

With the reordering below the plan is buildable. The calendar is then set by the waits on real use (about five weeks that no number of agents shortens) and by evidence windows, not by agent effort.

---

## Issues, most serious first

### 1. Stage 2 is serial as written; "parallel tracks behind contracts" has no packages (cross-cutting, blocker)

Evidence. Dependency columns: DV-20 on LG-10 and LG-21; DV-21 on DV-10, DV-20, LG-13; DV-23 on ST-05 and DV-21; DV-24 on DV-20 to DV-23 and ST-05; DL-07 on LG-22 and ST-05; DL-08 on ST-05; DL-09 on LG-20 and LG-23; DL-10 on LG-21; DL-11 on LG-13, LG-20, DL-09, DL-10; DL-13 on LG-21; HO-21 on ST-05; HO-23 on LG-20. The longest chain is LG-03 (a person) → LG-13 (M) → LG-20 (L) → LG-21 (L) → DV-20 (L) → DV-21 (L) → DV-23 (M) → DV-24 (M) → DV-25 (M). At the designs' own sizes (M a week, L two) that is about 12 weeks before the migration rehearsal and HO-27, however many agents there are. The evaluate design requires "each area's interface package (types and signatures only) lands in Stage 1" (12.3 rule 5, E-X2), but only LG-10 does that. ST-01 says "Out: any code", DV-10 and DL-03 are documents, and the store's reference model (ST-02) is "a test-only package nothing imports".

Change. Add one Stage 1 package per area: the interface package plus a fake that other areas' tests can import (`ledgertest` over fixture events, the store's reference model exposed as a store fake, a fake runner, a fake retrieval core). Rewrite the Stage 2 dependency columns to name the interface and fake. Add three explicit integration packages (ledger to derive, store to derive and deliver, the whole pipeline on fixtures, which is EV-20). The longest chain then drops to about six weeks plus integration.

### 2. Cards delete or replace live v1 code before the cut-over (cross-cutting, blocker)

Evidence. LG-21: "Deletes the hook-sliced recovery", while the ledger's own shadow step says "the hook runs v1 capture unchanged"; v1 capture is that recovery (`internal/hook/hook.go` 276 to 576 calls `catchup.Recover`, `RecoverProject`, `Register`, `Contains`). DV-20: "Deletes the prefix scan and markers". DV-21: "Deletes the word-overlap code". Both are in `internal/update`, which runs live until DV-24. HO-21: "remove `config.env` and the dialogue", although every release reaches the real install through v1's `eva-md upgrade`, which runs the new binary's `setup` (`cmd/eva-md/upgrade.go` 156), and the v1 move (HO-27) lands later. HO-11 removes the `install-record.json` and `cli-paths.json` readers. DL-05 deletes the three v1 readers and moves `resume` and `searchindex` to a new tokenizer. DL-12 removes `eva_status` and the recall skill. ST-13 and DV-26 delete the v1 paths in Stage 2, the same stage as the cut-over, while the store promises that `migrate --abandon` leaves a working v1 install for 14 days. The evaluate design says the opposite: v1 packages frozen, v2 "in new packages beside the old ones, behind flags that default to v1's behaviour", `main` always releasable.

Change. One rule in the plan: before the cut-over no package deletes or changes a v1 live path; every v2 path is additive behind one mode key. Strike the "Deletes" clauses from LG-21, DV-20, DV-21, HO-21, HO-11, DL-05 and DL-12. Move every deletion into packages that start only when the abandon window has closed on the real install (ST-13, DV-26, and new ones for the ledger, deliver and hosts). Add a "flags off equals v1" gate: the v1 test suite and benchmark unchanged on every merge.

### 3. The cut-over has no owner, no Stage 2 gate, and a rollback that loses chats (cross-cutting, blocker)

Evidence. Four areas switch at once: the ledger's `capture.v2 = "on"`, the store's migration, derive's worker and deliver's flags ("one flag for store, derive and deliver together" in the store, derive and deliver designs; the ledger's cut-over needs "the consumers that read the ledger" ready, so it is the same moment). The parts are spread over LG-25, ST-07, DV-25, DL-11, HO-27, ST-13 and DV-26, and no package names the one flag, its states or its test. The gate each area names, a week of shadow on real use (ledger 14.1, derive 12.7, deliver 18.7), is owned by no Stage 2 package: LG-50 and DV-50 carry "a week on the real install" in Stage 5. After the cut-over "an older binary keeps working on the v1 inbox and does not see chats captured after cut-over" (ledger 14.4), and the store's abandon restores the old wiki as it was. So abandoning on day 10 loses ten days of memory from the v1 view, and needs a v1-capable binary that issue 2's deletions would have removed.

Change. Add a cut-over set with one owner. CUT-1: the single mode key (`v1`, `shadow`, `v2`) and a state table for every area. CUT-2: the shadow release and the shadow week on the real install, with its readout as the gate (a person installs, uses it for a week, runs the report). CUT-3: the cut-over release, which keeps the v1 paths in the binary and keeps writing the v1 inbox as the shadow for the 14-day abandon window, with a test that abandons after simulated v2 use and finds every chat readable by v1. CUT-4: the deletions, one release later. EV-21 (rehearsal) and the owner's dry run stay as the gate into CUT-3.

### 4. The hottest files have no owner, and Stage 0 collides in them (evaluate and cross-cutting, blocker)

Evidence. The ownership table (evaluate 12.3) assigns no one `internal/hook/**`, `cmd/eva-md/**`, `internal/config/**`, `internal/coderef/**`, `internal/paths/**` or `docs/**`. `internal/hook/hook.go` is 1,075 lines with a 320-line `capture` function and no handler list. It is edited by LG-01, DL-02, DV-12, HO-12, LG-20, LG-24, DL-11, DV-24, HO-23 and DV-26, from five areas. `internal/config` is edited by LG-20, LG-23, LG-32, DL-11, HO-21, DV-13, ST-05, ST-06 and HO-25. `scripts/benchmark.py` is evaluate's, but LG-20, DL-08, DL-10, DL-14, ST-05, ST-12, HO-12 and HO-23 each add a scenario to it. `spec/metrics.json` and `docs/spec/INDEX.md` are single files every package edits. In Stage 0, LG-01, LG-02, DV-01, DL-02 and HO-02 each add a doctor row, a command or a flag before the command-table seam (EV-11, Stage 1) exists. Each must then edit the same sentence in `docs/design.md`, the same table, the README and the one `cmd/eva-md` line of AGENTS.md, because `cmd/eva-md/docs_test.go` enforces all four (`TestTheDesignNamesTheBuiltCommands`, `TestTheDesignShowsEveryFlagTheReadmeShows`, `TestAgentsNamesEveryFileOfTheCLI`). With hot files given to one integrator, that integrator is on the path of most packages.

Change. Make the seams the first package, before any counter: a command table with one file per command, a hook handler list, a registry for doctor and status rows, a registry for config keys (one file per area), a registry for benchmark scenarios, the metric catalogue split into one file per area. It needs no owner decision. Build the Stage 0 counters as one package by one builder. Assign owners: the hook shell and `internal/config` core to the ledger, `cmd/eva-md` to hosts, `internal/coderef` to deliver.

### 5. Needs placed on an earlier design have no work package (cross-cutting, major)

Evidence. The ledger was written first and its table covers none of: deliver's V-L1 (`deliver.json` slot), V-L2 (`note` event), V-L3 (receipt token), V-L4 (catalog fields), V-L5 (`ledger.Read`); derive's D-L1 (branch, captured head, fork parent in context) and D-L2; evaluate's E-L1 and E-L2 (the actor seam); hosts' H-L2 (`ledger.Health`) and H-L3 (`--event`). The store's table has no package for C-S3 (`ItemsCiting`, a `forget` transaction, which LG-30 depends on), V-S2 (v1 refs through a migration map), V-S3 (`shares_with`, `private`), D-S5 (workstream ids from a key), E-S1 (`export --json`) or H-S2 (`store.Health`). Derive's table has no package for V-D1's `commands` field.

Change. Before Stage 1 ends, one reconciliation package produces a matrix of every "need" row against a named provider package, and the card checker fails a card whose contract has no provider. The lead rules on the rows the designers left open (issue 15, and H-V2).

### 6. "Stage 0 does not block Stage 1" is contradicted by the dependency columns, and 51 decisions are unranked (evaluate and cross-cutting, major)

Evidence. Stage 1 packages that wait on Stage 0 or on the owner: DV-10 on DV-03, which is on DV-02 (live, the owner's leave); DV-11 on DV-02; LG-13 on LG-03 (a person on real hosts); EV-17 on EV-03 (live); ST-01 on "ST-D1 to ST-D7 answered"; DL-04 on ST-01; EV-10 on EV-D3; EV-11 on EV-10; HO-13 on HO-D3. LG-10 lists no dependency but its kinds rest on LG-D1 and LG-D8. So the spec or contract package of four areas (store, derive, evaluate, and deliver through DL-04) waits on a person, and the ledger's adapters wait on recorded fixtures. Only LG-11, DV-12, DL-03, HO-10, HO-14 and EV-12 can start with nobody asked.

Change. Three decision rounds, stated in the plan. Round A, before any code (about 14): EV-D1, EV-D2 with ceilings, EV-D3, EV-D4, EV-D9 with merge authority (issue 17), EV-D10, which machine is the evaluation home, ST-D1 to ST-D3, ST-D6, ST-D7, LG-D1, LG-D8. Round B, at the Stage 0 readout, sizing Stage 2: LG-D2, LG-D4, LG-D7, ST-D4, DV-D1 to DV-D6, DL-D6 to DL-D8, HO-D1 to HO-D3, HO-D7. Round C, when a Stage 3 or 4 package is about to start: the rest. Every undecided question defaults to its "if no" column so that no builder waits.

### 7. Codex stays on generation 0 until the end of Stage 3, so two stages of Codex work have no real-use evidence (hosts, ledger, deliver, major)

Evidence. Generation 0 declares seven Codex events with a tool matcher `^(Bash|apply_patch|update_plan|mcp__.*)$` (`internal/install/templates.go` 80 to 86): no SubagentStop, PostCompact, SubagentStart or PreToolUse. Hosts fixes generation 1 "at the end of Stage 3" (HO-30), and HO-30 depends on "DL-14's gate result", which is itself a live suite on both hosts that needs PreToolUse declared on Codex. Until then LG-23 (subagent reports, the Codex summary, every tool), the starred real-install bars of the ledger, DL-11, DL-14 and DL-15 can be exercised on Codex only in an evaluation home. There the evaluate design keeps v1's rule that hook trust is never bypassed, so each byte change of a development hooks unit needs a person in `/hooks`; the container route rests on an unconfirmed fact (evaluate's fact 6). The ledger still says the events are "declared together, once", and deliver says the matcher is "fixed after Stage 0".

Change. Freeze generation 1 at the cut-over with the capture events, which are on by default then by hosts' own rule, and offer it in the same sitting as the cut-over's other asks. Ship SubagentStart and PreToolUse as one additive unit when their gates open. That costs one more review and buys Stage 3 real-use data on Codex. State per stage which acceptance is "Codex in the evaluation home only".

### 8. The frozen generation 0 skill text names commands and pages that v2 removes (deliver, hosts, store, major)

Evidence. The Codex generation 0 root holds the skills and the MCP file beside the hooks (`templates.go` renders `skills/eva-md-digest`, `skills/eva-md-recall` and `.mcp.json` under one plugin root), and hosts will not touch that folder. The recall skill tells the agent to run `search`, `show <ref>`, `show knowledge/map` and `show systems/index`; the digest skill `show systems/map`, `system status` and `system write` (`recall-skill.md` 18 to 19, `digest-skill.md` 10 to 12). The store removes both maps and every index from the tree. Deliver keeps `search` and `show` "for one release" and removes the recall skill in DL-12, and says tool text "lives in the content plugin", which exists only from generation 1. Hosts says generation 0 may linger for good (risk 8).

Change. A compatibility test that parses the frozen generation 0 skill text and runs every command and ref it names against a v2 store; it stays until no supported machine is on generation 0. Deliver's "one release" becomes "while generation 0 is supported". DL-12 removes the skill only from generation 1's tools unit.

### 9. Stage 0 asks one person to run six instruments for 81 facts, partly on the real install (cross-cutting, major)

Evidence. LG-03 (a recorder hook on real hosts), DL-01 (20 rows, 200 timed Codex hook runs "on the real install"), HO-01 (a scratch plugin and marketplace, `/hooks` approvals), ST-00 (a push of a machine commit to the real install's git server), DV-02 and DV-03 (live runs on both hosts), EV-03 (a container and a Mac home). Several rows are the same fact in three designs (a detached child survives: ledger, derive, hosts; Codex hook start cost: deliver 16, hosts 7; the `--bare` default: derive, evaluate). Recorded payloads are to be committed to a public repository. The real install is the only source of the counted week, and scratch plugins, recorder hooks and test pushes there put it at risk.

Change. One host-facts kit, one package, one owner: one script and run-book per host, one output sheet with the 81 rows merged and numbered once, run in an evaluation home with a made-up user and a scratch project. The real install is touched only by read-only, content-free reports and by the one push test the owner agrees to. Budget the person's time in the plan: about one sitting per host, plus the two field reports.

### 10. Acceptance cells mix offline checks, live results and wall-clock numbers (cross-cutting, major)

Evidence. Live results inside the acceptance of a build package: DV-21 ("live, at the default row: `fact_recall` at least 0.90"), DV-24 (`end_to_saved` live), DL-10 ("suite A's first run"), DL-12 ("suite C's pull arm"), DL-14 to DL-16 and DL-19 (suites C, G, K), ST-07 (the owner's dry run), ST-09 (a live check on two git hosts), HO-30 (the real install), LG-50 and DV-50. Wall-clock numbers that the test rules forbid inside `go test`: LG-11 ("at least twice as fast"), LG-20 (three p95 keys), ST-05 (`time_to_durable` under 2 s), DL-08 (compile under 200 ms), DL-10 and DL-13 (a 1 GB ledger under 150 and 300 ms), HO-12 (under 1 ms), HO-23 (under 100 ms), ST-03 ("10 minutes of fuzzing"). The macOS job, the run of record, does not run the benchmark today (`.github/workflows/ci.yml` runs gofmt, vet, both test runs and the build; only the Linux job runs the benchmark), so no macOS latency baseline exists. Deliver's targets (pre-tool under 30 ms, prompt under 50 ms) sit close to this session's Linux figures (capture with spawn 25.8 ms, prompt with recall 37.9 ms p95).

Change. Every card splits acceptance into a merge gate (deterministic, offline, runnable by a reviewer with `scripts/linux-check.sh`) and a prove step (live or timed, run in an evidence window), as the evaluate card format already allows. Latency merges are judged by EV-16's interleaved old-against-new comparison; absolute ceilings are judged on the evaluation Mac. EV-16 moves to the first week so a macOS baseline exists before any hook change.

### 11. The continuation harness needs eva-md to install on Linux, and it is the long pole of Stage 1 (evaluate, major)

Evidence. EV-03 and EV-17 run agent sessions in containers. A release build refuses setup on anything but macOS (`cmd/eva-md/setup.go` 99); only the test stand-in sets `hostOS` to darwin (`timelimits_raised.go` 11). Linux support is HO-40, Stage 4, and no evaluate package depends on it. EV-17 is one L package holding drivers, arms, three fixture repositories, packs A (20 tasks), C (24) and K, the funnel and four baselines, with a person reviewing the first twenty tasks. By the evaluate budget the pilot, the frontier and the baselines come to roughly 385 to 585 million tokens before Stage 2 starts, on keys the owner must provide. Deliver's first Stage 2 packages are judged against EV-17's rows (E-X3).

Change. Pull the platform gate (`platform.Supported`) forward as a Stage 1 package for the evaluation build only. Split EV-17 into drivers and arms on fake hosts, the fixture repositories and tasks with the person's review, and the baseline runs. Start the first two on day one. Put the token total in round A.

### 12. The counted week can be contaminated, and no test pins the Codex plugin folder across releases (evaluate, hosts, major)

Evidence. Stage 1 packages change v1 behaviour on the real install: DV-12 ("runs v1's update and maintain as registered jobs", "can ship on v1 alone"), HO-12 (new notices), LG-11 (a new redaction driver under `Redact`), HO-11. The counted week measures exactly those behaviours (busy exits, boundary to saved, notices). EV-02 relies on "the existing byte-identity test"; the repository pins only `hooks.json` and `.mcp.json` by golden files, not the skills or the whole root, and the install tests assert that a changed folder needs trust again. A counter package that touches skill text would stop Codex capture during the week. HO-27's upgrade test from published releases lands late in Stage 2, yet every release from Stage 0 on is an upgrade on the real install; each needs the owner to cut it (the AGENTS.md release rule) and to upgrade by hand until HO-25.

Change. A release train in the plan. Release 1 carries the counters only. No behaviour-changing package is released until the second report is read. Pull two tests into the first package: a whole-root bundle hash for generation 0 (from HO-20) and "upgrade from the last published release keeps that hash and both hosts capturing" (from HO-27), with a fixture added for every published release.

### 13. The tick, its fallback and the MCP server have three owners and conflicting tests (hosts, derive, deliver, major)

Evidence. DV-12's scope includes "the no-tick fallback" and derive names `eva-md work --tick`; HO-22 builds `cmd/eva-md/tick.go`, `eva-md tick` and "the MCP server's 10-minute detached start"; `internal/mcp` is deliver's. DL-12's acceptance is "a test hashes the home folder and finds writes only under `<state>/deliver`", while HO-D7 and HO-23 have the server write health lines and run the evaluator, and HO-22 has it start the worker. Hosts' H-V2 puts a health line in a pull result; deliver's V-H4 says health text never enters its context. Hosts asks the lead to rule.

Change. Rule before DL-12's spec step. Hosts owns the `tick` command and the platform scheduler; derive owns `work.Tick()` as a library; deliver owns the server and exposes one registration point for start and interval callbacks. DL-12's write test names the allowed state folders as a list.

### 14. Missing, inverted and misnamed dependencies (derive, deliver, ledger, evaluate, major)

Evidence. DV-24 shows the model "related items (deliver's retrieval core)" but does not depend on DL-06; derive calls it `retrieve.Related`, deliver `recall.Related`. DL-17 calls `organize.Check` and does not depend on DV-21. EV-31 (Stage 3) depends on ST-09 (Stage 4). Drift of item anchors is built twice: DV-30 ("drift for item anchors through `internal/coderef`") and DL-18 ("`coderef` on item anchors; drift in the pack"). The capture adapter is `internal/hostadapter` in LG-13 and `internal/host` in HO-10, and the ownership table gives the ledger "the capture half" of a folder hosts owns, which a path check cannot express. DL-05 edits `internal/resume`, which rule 2 freezes. ST-05's secret gate uses `redact.Scan(text, Gate)`, which arrives in LG-11 and LG-26, and lists neither; if it starts on the kept `SecretLines` scan, the card should say so.

Change. Fix each in the card: DV-24 on the retrieval interface and fake; DL-17 on DV-21; the remote channel of EV-31 moves to Stage 4; drift is computed once (deliver) and read by derive; one package path with a sub-folder per half (`internal/host/capture`, owned by the ledger); list the allowed exceptions to the v1 freeze.

### 15. The store's first package cannot meet its acceptance until the lead settles the memory model (store, derive, major)

Evidence. ST-01's measure is "fields without a stated rule, 0", and DL-04 depends on ST-01. The designers left open: a kind `rule` (store) against `constraint` and `preference` (derive, deliver); workstream states started, active, merged, archived against active, paused, merged, abandoned, archived; a workstream body as a maintained paragraph against one cited status line; an episode `Summary` against a code-built trail; random ids with `merged_into` against workstream ids minted from project and branch (D-S5). Derive owns the kind and state tables (S-D4) but its spec, DV-10, waits on DV-03, a live run.

Change. The lead rules on these five in the plan itself. Derive's kind, tag and state tables are split out of DV-10 into a small package with no live dependency, ahead of ST-01.

### 16. Shadow organizing on the real install either doubles the quota spend or measures nothing, and the scratch store's fate is undefined (derive, deliver, major)

Evidence. Derive 12.7: the new worker runs in shadow "on the real ledger with canned or live answers". Live answers run beside v1's update, which may still spend up to "40 calls a host a day at the highest effort". Canned answers produce no model-written items, and deliver's shadow week needs them ("no model-written item has ever been scored"). At the cut-over the store is built from the v1 wiki with v1 coverage, so the scratch store's span-verified items for the same ranges are neither adopted nor mentioned; replay is opt-in (DV-D7).

Change. Decide in the plan: during the shadow week v1's update is paused for a sampled share of sessions, or the shadow pass runs under its own small budget with the owner's leave. State what happens to the scratch store (adopt as support for migrated items, or discard and say so). Make the choice part of the cut-over set's gate.

### 17. Merge authority and protected files are not decided (evaluate, major)

Evidence. The protocol has agents commit, push branches and open pull requests, and an integrator merge to `main` "without the orchestrator reading it". AGENTS.md says "Don't commit or push unless asked". EV-D9 asks only trunk or branch. The release path will read `eval/gates.json`, which agents edit, and the release rules require a recorded approval. Nothing says who may change `gates.json`, the workflows, `scripts/release.sh`, the held-out families or `internal/redact/baseline`.

Change. Add to round A: standing leave for agents to commit, push `wp/` branches and open pull requests; who merges; and a protected list that only the owner merges (workflows, release scripts, `gates.json` and baseline acceptance, the frozen redaction baseline, the frozen unit goldens, AGENTS.md).

### 18. EV-10 is under-sized and stands in front of every package (evaluate, major)

Evidence. EV-10 (M) must split 394 KB and 182 KB documents, import 17 decisions, turn "every row of v1's guarantees table and every must-keep item" (17 audits, roughly 14 to 24 items each) into records with a tagged test and "a named mutation that kills it", replace five tests and change AGENTS.md. EV-11 depends on it and "every other package uses them". The sixth prose test, `TestAgentsPreviewsWhatTakesApply`, is not in the list of five. `docs/known-limitations.md` (81 KB) and the README (57 KB) stay single files that every behaviour change must edit.

Change. Split it. First, the split and reflow, the decision import and the AGENTS.md change (S), which unblocks everyone. Second, the guarantees import with test tags (M to L), in parallel, with the mutation required only for new guarantees. Say what happens to the sixth test, and give `known-limitations.md` the same per-component treatment.

### 19. CI capacity and three scheduled systems are assumed, not planned (evaluate, store, hosts, minor)

Evidence. The macOS job has a 20-minute limit and runs both test runs. The plan adds the conformance suite at 500 schedules with real git under the race detector, the Level 0 suites, and the latency gate; evaluate estimates "about 3 min". It also adds a nightly 10,000-schedule job (ST-02), a nightly canary on throwaway macOS and Linux runners that installs both host CLIs (HO-50), and a daily signed-in canary on a Mac that must stay on (EV-22). Some packages can only be verified on macOS (the release script tests, launchd in HO-22, HO-25), so a builder in a Linux session cannot self-check them.

Change. Measure the macOS job time now and set a per-area CI budget. One scheduled workflow owned by evaluate runs the long forms. Mark the macOS-only packages so their review runs on the macOS job, and name the hardware for the signed-in canary in round A.

### 20. Sizes are in agent-days; the plan needs a calendar and a limit on work in progress (cross-cutting, minor)

Evidence. At the designs' scale the 128 packages are about 115 agent-weeks, yet v1's 120,000 lines took 11 days, so effort is not the limit. The waits on real use are fixed: the counted week, the shadow week, 24 hours of probation, 14 days before deletions, and the proof week, about five weeks in sequence. Evidence windows are twice a week. Each package is two pull requests through one integrator.

Change. Schedule by gates, not sizes. Cap work in progress by integrator and review capacity (about six packages building at once). Agree Stages 0 to 2 and the cut-over as the first commitment; list Stages 3 to 5 (46 packages) as candidates whose order the Stage 2 numbers decide.

---

## Contradictions between designs

1. When the Codex declaration is fixed: once with capture's (ledger C-H1), after Stage 0 (deliver V-H1), at the end of Stage 3 (hosts HO-30, evaluate rule 6).
2. Deletions: evaluate freezes v1 packages until the cut-over; LG-21, DV-20, DV-21, HO-21, HO-11, DL-05 and DL-12 delete or replace v1 code before it.
3. Where the shadow week sits: before the cut-over in Stage 2 (ledger 14, derive 12, deliver 18) against Stage 5 packages (LG-50, DV-50).
4. Stage 0 "does not block Stage 1" (evaluate) against DV-10, DV-11, LG-13, EV-17 and ST-01 depending on Stage 0 or on decisions.
5. Aliases for `search` and `show`: one release (deliver) against as long as generation 0 is in the field (hosts).
6. Tool text "lives in the content plugin" (deliver) against generation 0, where it lives beside the hooks until the person accepts generation 1 (hosts).
7. The MCP server: writes only under the deliver state folder and starts nothing (deliver DL-12, V-D10) against writes health lines, runs the evaluator, starts the worker every 10 minutes (hosts HO-D7, HO-22, derive D-H1).
8. Health text in a pull result (hosts H-V2) against never in deliver's context (deliver V-H4).
9. The tick command and fallback: `eva-md work --tick` and the fallback in DV-12 against `eva-md tick` and the fallback in HO-22.
10. Kind `rule` (store) against `constraint` and `preference` (derive, deliver).
11. Workstream states, body and the episode summary (store against derive).
12. Workstream ids: random with a redirect (store) against minted from project and branch (derive D-S5).
13. The retrieval call: `retrieve.Related` (derive) against `recall.Related` (deliver).
14. The adapter package: `internal/hostadapter` (ledger) against `internal/host` (hosts).
15. Drift of cited code: computed in derive's housekeeping (DV-30) and in deliver (DL-18).
16. The v1 freeze: `internal/update`, `resume` and `catchup` are frozen (evaluate rule 2) against DV-01, DV-02, DV-11 and DV-12 changing `internal/update` and its callers, and DL-05 changing `internal/resume`, in Stages 0 and 1.
17. Live levels: two in derive, three in evaluate; cards quote both.
18. Rollback after migration: a working v1 install for 14 days (store) against v1 code deleted at the cut-over (ST-13, DV-26) and the previous slot dropped if it cannot read the result (hosts 10.5).

---

## Gaps no design covers

1. A package and owner for the cut-over itself: the one mode key, the shadow release, the readout, the release that switches, the rollback test.
2. A contract matrix that maps every need to a provider package, and interface packages with fakes for the store, derive and deliver.
3. Seams for the hook, the config keys, the status rows and the benchmark scenarios; owners for `internal/hook`, `cmd/eva-md`, `internal/config`, `internal/coderef`.
4. A release train: which packages each release carries, who cuts it, and what must not ship during a measurement week.
5. A whole-root hash test for the Codex generation 0 folder, and an upgrade fixture per published release, before the first new release.
6. A compatibility set for generation 0: every command and ref its frozen skills name.
7. The fate of the shadow scratch store, and the quota of shadow organizing beside v1's update.
8. Merge authority, protected files, and standing leave to commit and push.
9. A person-time and hardware budget: which Mac is the evaluation home, whether a second machine exists for the sync check (ST-09), the keys and token total for containers, who watches the first real migration.
10. A macOS latency baseline and a CI time budget.
11. A calendar: the fixed waits on real use and how packages overlap them.
12. How recorded real payloads are scrubbed for a public repository (user name, paths, session ids, model text), beyond a redactor scan.
13. What a builder in a Linux cloud session does with a package whose acceptance runs only on macOS.

---

## Where the plan needs a person

| Need | Packages | When |
| --- | --- | --- |
| Decisions, round A, B, C | all | before code; at the Stage 0 readout; before each later package |
| The field report, twice | EV-00, EV-02 | now; after the counted week |
| Cutting each release and approving it; upgrading the real install until HO-25 | every release | throughout |
| One sitting per host in an evaluation home: sign-in, `/hooks` approval, typed prompts, dialogs, plan approvals, forks, compactions, timed hook runs | LG-03, DL-01, HO-01, EV-03 | Stage 0 |
| Leave and quota for live runs; keys for containers | DV-02, DV-03, EV-01, EV-03, EV-15, EV-17, every evidence window | Stage 0 on |
| A push test and a deploy key on the real install's git server | ST-00 | Stage 0 |
| Review of the first twenty tasks | EV-17 | Stage 1 |
| A signing key made and stored; an offline key | HO-13, HO-D3 | Stage 1 |
| A week of use on the shadow release, then the report | the cut-over set | end of Stage 2 |
| The migration dry run on a copy; watching the first real run; the scope review after it | ST-07, EV-21, deliver 18.4 | the cut-over |
| One `/hooks` visit for generation 1 | HO-30 | the cut-over (as proposed) or the end of Stage 3 |
| A re-approval in the evaluation home whenever a development hooks unit changes | DL-11, DL-14, DL-15, LG-23 | Stages 2 and 3 |
| The desktop apps checked by hand | gate R7 | every release |
| A second machine and a second git host for the sync check | ST-09 | Stage 4 |
| Seeing one OS notification before agreeing to it | HO-D4 | Stage 2 |

---

## What must be measured before a stage starts

- Before any code: the macOS job's time and a macOS benchmark baseline.
- Before Stage 2: both field reports read into the 18-row table (EV-00); the host-facts sheet with every row answered or its fallback chosen; v1 baselines on fidelity at Level 0 and live (EV-14, EV-15), on retrieval (DL-04), on capture (LG-02), on continuation (EV-17) and on latency (EV-16); the model and effort frontier (DV-03); the pilot's variance and price (EV-03); the contract matrix closed.
- Before the shadow release: the hard gates green on fixtures (EV-20); flags off equals v1; the Codex bundle hash unchanged; the upgrade fixtures green.
- Before the cut-over: the shadow week's coverage, leak, misattribution, refusal and precision counts; the migration rehearsal (EV-21); the owner's dry run at 0 unaccounted.
- Before deletions: the abandon window closed on the real install.
- Before a Stage 3 feature defaults on: its own suite row; until then it stays in shadow.
- Before Stage 4 sync: the conformance suite at 10,000 schedules, and the trust rules of the store's 8.6.

---

## The stage plan I would run

**Stage 0, ground truth and guard rails.** Round A. The seams and guards. One counters release and the counted week. The host-facts kit. The live gates on v1, the frontier and the pilot in evidence windows. Nothing user-visible changes but counters.

**Stage 1, foundations, all "holds" or "guards".** Documents and cards; the results store; interface packages and fakes for all five areas; scenarios, scorers and v1 baselines; the latency gate; the redaction driver; the codec, the migrator framework and the conformance model; the adapter and fake hosts; the labelled retrieval set. No behaviour-changing release until the second report is read. Gate: the "before Stage 2" list, then round B.

**Stage 2, rebuilds in five tracks against fakes, additive behind one mode key.** Record (LG-20, LG-21, LG-22 to LG-24). Store (ST-05, ST-06, ST-08). Organize (DV-20 to DV-24). Deliver (DL-06 to DL-13). Hosts (HO-11 to HO-13, HO-20 to HO-26). Three integration packages. Then the migrations (LG-25, ST-07, DV-25), the rehearsal (EV-21) and the v1 move (HO-27).

**Stage 2C, the cut-over set.** The shadow release and week; the readout; the cut-over release with v1 kept and written as shadow; generation 1 for capture offered once; 14 days; then the deletions.

**Stage 3, memory quality.** Each feature behind its own flag, in shadow until its row holds; order chosen by the funnel table. Codex tool-time and subagent events as one additive unit when their gates open.

**Stage 4, reach and sync.** Linux, the hook-free floor, sync, the rest, each gated as the designs say.

**Stage 5.** The claim run and what may be said.

### The first ten work packages, in order

| # | Package | From | Why here |
| --- | --- | --- | --- |
| 1 | Seams and guards: command table, hook handler list, registries for status rows, config keys and benchmark scenarios; the generation 0 whole-root hash; the upgrade-from-last-release test; flags off equals v1 | EV-11 (seam half), HO-20 and HO-27 (the two tests) | every later package edits these files; needs no decision |
| 2 | The v1 counters, as one package and one release | LG-01, DV-01, DL-02, HO-02, EV-01's output file, EV-02's check | starts the counted week, the longest fixed wait |
| 3 | The host-facts kit and recorder, one run-book per host | LG-03, DL-01, HO-01, ST-00, the recorders of DV-02 and EV-03 | one sitting per host answers 81 rows and yields the fixtures Stage 1 needs |
| 4 | The latency gate and a macOS baseline | EV-16 | a baseline must exist before any hook change |
| 5 | Documents split, decisions imported, AGENTS.md changed; the guarantees import runs beside it | EV-10, split | agents can read the design; nobody waits on the import |
| 6 | Cards, the ownership check and the contract matrix | EV-11 (card half), new | makes parallel work safe and exposes needs with no provider |
| 7 | The results store, the metric catalogue per area, gates as data | EV-12 | every baseline lands in it |
| 8 | The event model, the reader with its v1 backend and a test fake, signed by every consumer | LG-10, widened | derive, deliver and evaluate build against it |
| 9 | The kind, tag and state tables; then the store's format spec, interface and reference model as an importable fake | part of DV-10, ST-01, ST-02 | settles the model differences; unblocks DL-04 and the store track |
| 10 | Scenarios, the black-box scorer and v1's Level 0 score; continuation drivers and arms on fake hosts with the Linux evaluation build | EV-13, EV-14, EV-17 (first part) | the instrument that judges Stage 2 |

Beside these, with no file in common: LG-11, HO-14, ST-03, ST-04, DL-04, and in the evidence windows DV-02, DV-03, EV-03 and the baseline runs.
