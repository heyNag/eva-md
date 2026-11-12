# The contract matrix

Every need one area design has of another, with the plan package that meets it, and the interface and fake each track builds against. The needs are the rows whose direction holds "need" in the contracts section of the six designs in [working-papers/design/](working-papers/design/).

The ids are the plan's (section 8). A design id the plan folded into another row is written as that row's: LG-03 as HF-00, EV-13 as EV-14, HO-25 as HO-13, and ST-13 and DV-26 as CUT-4.

`internal/cardcheck` checks this file against the plan, the designs and the cards; its test runs on every change. A row's columns:

- **Need**: the design's id. Ids repeat across designs, so a need is its id and its design.
- **Consumer**: the plan packages whose work rests on the need, from the design's package rows that cite it; `none` when no plan package does, because the plan cut or deferred what did.
- **Provider**: plan ids, or one word: `v1` (kept v1 code meets it), `later` (an item of section 12), `dropped` (the plan ruled it out) or `open` (the lead must rule). No provider is in a later stage than a consumer; where the plan gives none, or gives a later one, the row is `open`.
- **Where**: the Go name, file or section that meets it; for `open`, the question.

A card fails the check when one of its packages consumes a row whose provider is `open`, `later` or `dropped`. A Stage 2 package that needs another track's Stage 2 package (ST-05, LG-22, LG-23) builds against that track's Stage 1 interface and fake, below, and needs the package itself only to integrate (plan section 8, Stage 2).

## Needs

| Need | Design | Consumer | Provider | Where |
| --- | --- | --- | --- | --- |
| C-D1 | ledger.md | LG-21 | DV-12 | The worker in `internal/work` (DV-12a): a wake that finds it busy loses nothing; LG-21 registers the transcript's reading as its job |
| C-D3 | ledger.md | LG-21 | ST-05, DV-25 | Progress is the store's coverage, `covers` in a session file (store-format.md section 10), written by ST-05; DV-25 carries v1's watermarks over as coverage |
| C-D5 | ledger.md | LG-50 | DV-21 | DV-21's checker takes authority from the cited span's `pv` (plan 6.3 design 3); text between tool calls stays out (section 12) |
| C-S1 | ledger.md | LG-22 | ST-01, ST-05 | The project identity record, `projects/<pid>-<slug>/project.md` (store-format.md section 5), written by ST-05 |
| C-S3 | ledger.md | LG-30 | open | Which package gives `store.ItemsCiting` and the forget transaction that retracts an item left with no evidence: ST-05, ST-08 or LG-30 itself? |
| C-L2 | ledger.md | LG-23 | DL-11 | DL-11 records each delivery as it injects it, as the `delivery` kind of `internal/ledger/event` (ids and hashes), which LG-23 writes |
| C-L4 | ledger.md | LG-24 | DL-11 | DL-11's delivery reads the pause state and is silent while a chat is off the record; there is no spool (section 12) |
| C-H1 | ledger.md | LG-23 | HO-20 | HO-20 freezes the Codex hook declaration once, capture's events with delivery's |
| C-H3 | ledger.md | LG-24 | HO-21, ST-05 | The first run's statement of what is captured and sent (HO-21, decision 32); the machine id is the store's writer id (store-format.md section 3); the record keeps v1's files, so it has no reader version of its own |
| C-E1 | ledger.md | LG-21, LG-50 | HF-00, EV-20 | HF-00's recorded fixtures (the design's LG-03); EV-20 wires the hard gates into CI |
| C-S2 | store.md | ST-05 | open | Which package gives `ledger.Excerpt`, gate-clean with the event id and a hash, for the words decision 33 commits with a rule or a constraint: LG-21, DV-21 or ST-05? |
| C-S4 | store.md | ST-05 | LG-11, LG-26 | `redact.Scan` (LG-11) with LG-26's gate policy on every line `Apply` adds; a rescan when the rule version rises is section 12's repair at rest |
| S-L1 | store.md | ST-07 | LG-10 | Event ids and `ledger.Resolve` in `internal/ledger`: a v1 `s:` reference resolves to the event whose seq is its line |
| S-L2 | store.md | ST-07 | LG-10, LG-22 | Project ids for v1 keys from LG-22's identity records; a v1 watermark is a line, which LG-10 makes the seq (`coverage-v1`, store-format.md section 5); no record migration runs first (CUT-3) |
| S-L3 | store.md | ST-08 | open | Are remember, correct and confirm a person's commands in v2.0, and which package writes them, and forget, as record events a replay applies? Forget is LG-30's, a stage after ST-08 |
| S-D2 | store.md | ST-05 | DV-21, DV-23 | DV-21's checker and DV-23's operations reach the store only through `store.Apply`, and never name an id, a date or a path (plan 6.3 design 2) |
| S-D4 | store.md | ST-05 | ST-01 | The plan ruled the kinds, states, tags and working items in section 6.3, and ST-01 wrote them into docs/spec/store-format.md sections 5 and 10 |
| S-V3 | store.md | ST-05 | DL-08, DL-10 | DL-08's pack leaves out a row that fails its gate; DL-10 shows a contested or locked item as such |
| S-V4 | store.md | ST-05 | DL-08 | Hooks read only DL-08's compiled pack and the record (plan 6.4 design 1) |
| S-V5 | store.md | ST-05 | DL-07 | `shares_with` changes only through DL-07's review, on the person's authority (decision 13); write tools are section 12's |
| S-H1 | store.md | ST-05, ST-07 | HO-21, HO-27 | The first run creates the store with no question (HO-21); the upgrade to v2 runs the migration in an order a crash can resume (HO-27) |
| S-H2 | store.md | ST-06, ST-08 | HO-22 | HO-22's clock wakes the worker, which pushes and runs maintenance; hook wakes are the fallback (plan 6.5 design 5) |
| S-H4 | store.md | ST-05, ST-07 | open | The writer id is the store's (store-format.md section 3). Which package makes an upgrade refuse a binary below the store's `min_reader`: HO-13, HO-27 or CUT-3? |
| S-E2 | store.md | none | dropped | Plan 6.6: the corpus audit and the poisoning suite wait for a decision that needs their number |
| C-D2 | derive.md | DV-24 | LG-10, LG-21 | `event.Session` and its class from `ledger.Reader` (LG-10); LG-21 exposes a session only up to its checked point |
| C-D4 | derive.md | DV-20, DV-21 | LG-10, LG-21 | Released events through `ledger.Reader`; DV-20 groups them into episodes and DV-21 checks quotes against them; an excerpt is C-S2's question |
| D-L1 | derive.md | DV-23 | LG-10, LG-23, LG-24 | The `ctx` field (branch, head commit) of LG-10's events; LG-23's tool events carry the head commit and failure digests; a fork inherits its parent (LG-24) |
| D-L2 | derive.md | DV-23 | open | Which package writes remember, correct, confirm and forget as typed record events, so a replay applies them again (the store's S-L3)? |
| D-S1 | derive.md | DV-21, DV-23, DV-24 | ST-05 | `store.Apply` with an outcome per operation and coverage in the tree (ST-05), behind `store.Store` in `internal/store/api.go`; `store.Plan` for a review is section 12's |
| D-S4 | derive.md | DV-23 | ST-05 | ST-05 refuses an operation aimed at a locked, verified or hand-edited file and records it; a merge report waits for sync (section 12) |
| D-S5 | derive.md | DV-23 | ST-01, ST-05 | A branch's workstream id is from SHA-256 of the project and the branch (store-format.md section 3) |
| D-R1 | derive.md | DV-24 | DL-06 | DL-06's core serves the items a pass is shown, behind `retrieval.System` (DL-04) |
| D-R3 | derive.md | none | later | Section 12, "A richer lifecycle": archive by disuse |
| D-R6 | derive.md | DV-35 | DL-10 | DL-10's brief carries no system pointer (decision 12) |
| D-H1 | derive.md | DV-24 | HO-22 | HO-22's clock; hook wakes are the fallback, and the MCP server starts no tick (plan 6.5 design 5) |
| D-H2 | derive.md | DV-22, DV-24 | HO-10, HO-13, HO-20 | HO-20 declares the wake events with capture's; HO-10's adapter knows each host's CLI path and version; HO-13's upgrade marks the binary's change |
| D-H5 | derive.md | DV-24 | HO-21 | HO-21's first-run statement says what a model is sent and how to stop it |
| D-E1 | derive.md | DV-21 | EV-12, EV-14 | EV-14's fidelity scorer with its held-out half; EV-12's results store and baselines; twins, poisoning and aging wait (plan 6.6) |
| D-E3 | derive.md | none | DV-32 | DV-32's corpus counters; the reviewing pass they decide on is section 12's |
| D-E4 | derive.md | none | dropped | Plan 6.6: the task-switching suite waits for a decision that needs its number |
| V-L1 | deliver.md | DL-09 | open | Where does DL-09 keep a chat's state, the design's `deliver.json` slot, while the record keeps v1's layout and the session-keyed one is section 12's? |
| V-L2 | deliver.md | none | later | Section 12, "Tools that let an agent write to memory" |
| V-L3 | deliver.md | DL-20 | open | Does a read tool's receipt token reach the record in LG-23's typed tool events, so DL-20's receipts cover pulls, or do receipts cover only what was pushed? |
| V-L4 | deliver.md | DL-10 | open | Which package builds the record's session catalog that hooks and DL-10's handoff read (plan 6.4 design 1): LG-21, or DL-08 with the pack? |
| V-L5 | deliver.md | DL-10, DL-12 | LG-10, LG-21 | `Reader.Read` (LG-10) gives a session's events with their context; LG-21 exposes only the checked part |
| V-S1 | deliver.md | DL-08 | ST-01, ST-05 | The item fields of store-format.md section 10, read through ST-05's readers behind `store.Store` |
| V-S2 | deliver.md | DL-12 | ST-05, ST-07 | ST-05's readers resolve a handle; ST-07 maps v1's references as it migrates a wiki |
| V-S3 | deliver.md | DL-07 | ST-01, ST-05 | `shares_with` on the project record (store-format.md section 10); no `private` key, since reading across projects is closed by default (decision 13) |
| V-D1 | deliver.md | DL-08 | open | Which package gives items the command heads the pack's exact keys need? store-format.md has `cues` and `anchors` and no `commands`; who fills cues on migrated items? |
| V-D2 | deliver.md | DL-10 | DV-24 | DV-24's fast pass puts a chat's memory on file within minutes of its end |
| V-D3 | deliver.md | DL-10 | ST-01, DV-23 | Workstream files as plan 6.3 ruled them (a status line, next steps, open questions, decisions in force); DV-23 binds items to workstreams |
| V-D4 | deliver.md | none | DV-21 | The checker is DV-21's; the write tools that would call it are section 12's |
| V-D5 | deliver.md | DL-08 | DV-21, DV-23 | An echo is decided per statement against the delivery record (plan 6.1 design 2) in DV-21; DV-23 keeps a disagreement beside its item |
| V-D7 | deliver.md | DL-08 | DV-21 | Admission is DV-21's (plan 6.3 design 5); armed rule checks are section 12's |
| V-D9 | deliver.md | DL-08, DL-10 | ST-05, DV-24 | `store.Coverage` (ST-05) and DV-24's freshness |
| V-D10 | deliver.md | none | dropped | Plan 6.5 design 5 and HO-22: hook wakes are the fallback, and the MCP server is no tick source |
| V-H1 | deliver.md | DL-11, DL-15 | HO-20 | HO-20 declares SessionStart's sources and SubagentStart once; delivery at tool time is section 12's |
| V-H2 | deliver.md | DL-11 | HO-10, LG-13 | The capability table in `internal/host/adapter` (HO-10a); typed-prompt evidence from LG-13's capture adapters |
| V-H3 | deliver.md | DL-12 | HO-20 | The MCP entry and the skills in HO-20's second plugin |
| V-H4 | deliver.md | DL-08, DL-10 | open | Which package makes install and upgrade end with a pack rebuild: HO-21 and HO-27, or the worker? Health text reaches a chat only as HO-12's prepared line |
| V-E1 | deliver.md | DL-06, DL-10 | DL-04, EV-17 | DL-04's labelled set and scorer, behind `retrieval.System`; EV-17's continuation tasks; the other suites wait (plan 6.6) |
| V-E3 | deliver.md | DL-11 | EV-20, CUT-1 | CUT-1's mode key keeps a part in shadow until EV-20 judges it against v1's row; no defaults file is read at run time |
| H-L1 | hosts.md | HO-12 | LG-01 | v1's run records of each event, and CT-1's week of durations for each event, never pooled |
| H-L2 | hosts.md | HO-23 | LG-21 | LG-21 reports the age of the oldest event not yet checked; there is no spool (section 12) |
| H-D2 | hosts.md | HO-23 | DV-12, DV-22 | `work.Status` (DV-12a), read without the worker lock; DV-22's pause after host failures; probation is section 12's background install |
| H-S2 | hosts.md | HO-23 | ST-05, ST-06 | ST-06 reports the age of the oldest change not yet off the machine |
| H-V1 | hosts.md | HO-21, HO-23 | DL-10 | DL-10's brief carries the prepared line and the first-run statement; until then HO-12c shows the line in v1's brief |
| H-V2 | hosts.md | HO-23 | DL-12 | A read tool's result carries one health line (plan row HO-23: three channels) |
| H-V5 | hosts.md | HO-20 | LG-23, DL-11 | HO-20 freezes the declaration once, in place of the design's HO-30, with LG-23's capture events and DL-11's delivery events |
| H-X1 | hosts.md | HO-27 | HO-11, HO-20 | HO-11's manifest lists each rendered file and HO-20 renders the plugins; decision 25 places nothing else outside eva-md's folders |
| H-E1 | hosts.md | HO-20 | EV-12, HO-14 | EV-12's results store; HO-14's fake hosts with hash-bound trust, versions and policy blocks; the nightly canary has no row (plan 6.6) |
| E-L1 | evaluate.md | EV-14, EV-17, EV-20 | HF-00 | v1's `eva-md hook` reads a payload on stdin in any HOME; HF-00's fixtures are kept by host and version |
| E-L2 | evaluate.md | EV-17 | LG-13 | The evaluation-only build tag of the lead's choices in section 7, in LG-13's capture adapter |
| E-L3 | evaluate.md | EV-20 | LG-10, LG-23 | `delivery` events (LG-10's kinds) written by LG-23, read for each session through `ledger.Reader`; metric ids from EV-12's catalogue |
| E-S1 | evaluate.md | EV-20 | open | Which package gives the scorer a typed export of a v2 store, the design's `eva-md export --json`? EV-14b reads v1's pages itself |
| E-S2 | evaluate.md | EV-21 | ST-07 | ST-07's dry run prints counts only |
| E-D1 | evaluate.md | EV-20 | DV-11, DV-21 | DV-21's schema and refusal codes; recorded runs through DV-11's runner |
| E-D2 | evaluate.md | EV-20 | DV-11 | DV-11's typed outcomes, with the host's version and the model that answered |
| E-V2 | evaluate.md | EV-20 | DL-08 | DL-08 compiles a pack from the store folder it is given |
| E-H1 | evaluate.md | EV-17 | v1 | v1's `setup --dir --remote` and `install` run with no terminal in any HOME, as the release stand-in tests run them; a release builds only `./cmd/eva-md` |
| E-H2 | evaluate.md | EV-23, EV-51 | HO-26 | `status` (row HO-24, HO-26) carries the report EV-23 checks; EV-51 puts the gate into Cut a release |
| E-X1 | evaluate.md | EV-20 | EV-12 | Plan section 9: every card says moves, holds or guards; EV-12 keeps the one row format |
| E-X2 | evaluate.md | EV-20 | IF-01, EV-11 | Row IF-01 and the table below: each track's interface lands in Stage 1 with a fake; EV-11 checks that a diff stays inside its card's paths |

## Interfaces and fakes

Each track's interface is written by its first package, and IF-01 adds a fake of it that other tracks' tests import. A fake is a folder, or `waits:` and the package or card that will build it; for ST-05, LG-22 and LG-23 alone, it may be `open`.

| Interface | Go name | Provider | Fake |
| --- | --- | --- | --- |
| The reader | `ledger.Reader` | LG-10 | `waits: IF-01b` |
| The store | `store.Store` in `internal/store/api.go` | ST-01, ST-02 | `waits: IF-01c` |
| The retrieval core | `retrieval.System` | DL-04 | `waits: IF-01c` |
| The runner | named by DV-11 | DV-11 | `waits: DV-11` |
| ST-05, the store core | `store.Store` | ST-05 | `waits: IF-01c` |
| LG-22, project identity | none in Stage 1 | LG-22 | `open` |
| LG-23, decisions and delegated work | `ledger.Reader`, whose kinds already hold `dialog`, `plan_decision`, `sub_task`, `sub_report` and `delivery` | LG-23 | `waits: IF-01b` |
