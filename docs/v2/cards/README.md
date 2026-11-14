# Cards

A card is the lead's order for one work package of [the v2 plan](../plan.md). A builder starts from its card and reads nothing else it does not point to. The plan governs: where a card and the plan differ, the plan is right and the card is fixed.

How a package runs (plan section 9): the builder works on a branch in its own worktree and commits there; reviewers who were given only the card and the diff try to break it; the builder fixes what they find; the lead runs the quick document and script tests `AGENTS.md` names, merges it into `main` and pushes, and the project's checks and CI run on that commit after it (`AGENTS.md`, the standing leave), with a failure fixed forward at once. Only the lead pushes. Nothing is released by a merge.

Rules every card carries, so they are said once here:

- v2 is built beside v1. A package changes v1's behaviour only behind a switch that defaults to today's behaviour. With every switch off, v1's own tests and benchmark are unchanged.
- A diff stays inside the paths on its card. If the work needs another path, the builder stops and says so in its report.
- Every rule of [AGENTS.md](../../../AGENTS.md) holds. In particular: `internal/redact/baseline` is never edited; tests never touch the real machine and never depend on its speed; a hook never calls a model or the network; only eva's name is shown; no new dependency.
- Before a builder reports, `scripts/linux-check.sh` passes from the root of its worktree.
- No secret, and nothing from a real chat, goes into a fixture. Test values are made up.
- A package that changes a hook, or anything a hook runs, reports the latency gate's comparison with the commit it started from: `scripts/linux-gate.sh` on Linux. `not judged` is not a result: run it again when the first load figure `uptime` gives is below the number `nproc` prints. If the machine has not been that quiet for half an hour, stop waiting and say so in the report, with the load: CI's `latency` job on the merge is the run of record, and the lead reads it.
- CI's macOS job is the run of record and no builder can run macOS, so tests are written for it: a test that compares a path with one that git or the system resolved resolves its own first (`filepath.EvalSymlinks` on `t.TempDir()`), since macOS reaches its temporary folders through an alias, and no test leans on a file system that tells letter case apart.
- `AGENTS.md` and `README.md` are the lead's. A builder changes only the lines its card names in them; for anything else it gives the words in its report and the lead writes them at the merge.
- A card carries, in its "Owns and may touch", a fenced block with the info string `paths`: one entry a line, with no space, backtick or list mark, an exact file, a folder ending in `/**` for everything under it, or a pattern whose `*`, `?` and `[...]` match within one name of a path, such as `docs/v2/cards/*.md`. CI's `guards` job fails a commit that names the card and touches a path outside its block, names a card that does not exist or has no block, or widens its own card's block; a card its first parent did not have counts as one with no paths, so the lead commits each card before a commit names it. The lead writes a card's block; a builder never widens it.
- A card may carry a fenced block with the info string `deletes`: the v1 files, or folders ending in `/**`, that its package schedules for deletion, all of them files v1 had at commit 51b241c. Once the card is merged, `eva-eval size` subtracts their built lines from the Go built into eva-md, which is held to v1's 53,694 lines at CUT-4 and reported at every merge until then ([the plan](../plan.md), section 7, "Where the decisions stand"); once CUT-4 is merged nothing is subtracted, and a scheduled file still present is a fault. A block also freezes what it names, so it is written only on the card whose merged package replaces that code. The lead writes every `deletes` block.
- A commit that lands a package carries `Card: <ID>` on a line of its own straight after its subject, one such line for each card it lands, and holds the package's files only. The ownership check reads such a line wherever it stands in the message. Any other line that starts, in any case or after any space, with `Card:` or `Cards:`, such as `Card: AA-1, AA-2` or an indented line of a squash message, is a fault. A commit with neither is the lead's: CI lists it and does not judge it, and the index, a card's notes and `AGENTS.md` change in such a commit.

| Card | Package | State |
| --- | --- | --- |
| [SEAM-0a](SEAM-0a.md) | The command table, and document checks that no longer read prose | merged |
| [SEAM-0d](SEAM-0d.md) | Guards on the plugin's bytes and on the upgrade from 2026.11.5 | merged |
| [LG-10](LG-10.md) | The event model and the one reader | merged |
| [LG-11](LG-11.md) | The redaction driver with spans, same behaviour | merged |
| [SEAM-0c](SEAM-0c.md) | Registries for status rows and switches | merged |
| [EV-12](EV-12.md) | The results store and gates | merged |
| [ST-01](ST-01.md) | The store's written format and its profile checker | merged |
| [EV-16](EV-16.md) | Latency that can fail a build, and a baseline before any hook changes | merged; advisory in CI until 20 passes with no fail on macOS |
| [EV-16b](EV-16b.md) | The latency gate's rule, second round: noise from runs, and a slowdown no longer read as noise | merged; the gate stays advisory, since this rule cannot yet block a merge (EV-16d) |
| [EV-16c](EV-16c.md) | The benchmark's count of background starts allows for a hook that missed its limit | merged |
| [EV-16d](EV-16d.md) | The latency gate's rule, third round: three rounds, and what a gate that blocks must show | merged; on CI's runner the gate still seldom judges, and its count starts again at 0 |
| [EV-16e](EV-16e.md) | The benchmark's code-brief check allows for a brief whose git calls ran out of time | building since 2026-11-14 |
| [CT-1](CT-1.md) | Counters release: what the hooks held back and delivered | merged |
| [CT-2](CT-2.md) | Counters release: what the update cost and why work waited | merged |
| [CT-3](CT-3.md) | Counters release: capture checked against the host's own record | merged |
| [CT-3b](CT-3b.md) | Counters release: the capture audit pairs a session by its id too | merged |
| [CT-4](CT-4.md) | Counters release: a content-free report | merged |
| [HF-00](HF-00.md) | The recorder for the host sittings, and fixtures with every value made up | Part 1 merged: what the core sittings of [host-sitting-runbook.md](../host-sitting-runbook.md) need; its allowlist and vocabulary are approved; Part 2 may come later |
| [HF-00b](HF-00b.md) | The recorder keeps the shape of a voice hand-over, and no word of it | merged |
| [HF-01](HF-01.md) | The shapes of the hosts' records, as a read-only command | merged |
| [DV-03a](DV-03a.md) | Contract spike (throwaway): its scripts, renderer and two forms of the contract | merged |
| [DV-03b](DV-03b.md) | Contract spike: its checker, repair round and measures | merged, with a second round for four rulings of the lead's; three known limits of its checker are named for DV-21 |
| [DV-03c](DV-03c.md) | Contract spike: an arm's passes, recording, replay, fidelity and rows | merged |
| [DV-03d](DV-03d.md) | Contract spike: `eva-eval spike`, the plan of the run, and the frontier | merged, with a second round for two rulings of the lead's; the live run is the owner's to allow |
| [EV-03a](EV-03a.md) | The pilot (throwaway): ten tasks, their format and their offline check | merged |
| [EV-03b](EV-03b.md) | The pilot's ten tasks, exported to the continuation format | merged; the continuation's check accepts all ten |
| [EV-03c](EV-03c.md) | The pilot's report, over the continuation's rows, and the pilot's README | merged |
| [DV-02a](DV-02a.md) | The live run's trace: cost, versions and what the host returned, for [the live gates](../live-gates-runbook.md) | merged, ahead of DV-13; ST-04b carries one of the two small things its notes owe, and the other waits for a card that changes `trace.go` |
| [DL-04](DL-04.md) | The labelled retrieval set and its scorer, with v1's row | merged; ST-04b adds the figure for false additions over all negatives of a half |
| [DV-12a](DV-12a.md) | The worker, its wake marks and its journal | merged |
| [HO-10a](HO-10a.md) | The host adapter and its table | merged |
| [HO-14](HO-14.md) | Fake hosts that fail the way real ones do | merged; open for the host sittings: whether Codex keeps a plugin's approvals when it is removed |
| [LG-12](LG-12.md) | The redaction corpus, its rates, and the baseline as an oracle | merged; its switch stays off until LG-26 closes the four shapes of code the oracle records; SEAM-0f mends the hook test that depends on speed |
| [LG-13a](LG-13a.md) | The hook payload decoder and the author allowlist, with no caller | merged; owed before LG-20: the hand-over decoder counts line breaks once, not once for each line |
| [LG-13b](LG-13b.md) | The decoder held to the hosts' recorded payloads | after LG-13a and the fixtures of the core sittings (HF-00) |
| [LG-22a](LG-22a.md) | The project registry's types, the project id as a function of a key, and its fake | building since 2026-11-14; LG-22's first part, with no caller |
| [SEAM-0b](SEAM-0b.md) | The hook's handler list | merged |
| [SEAM-0e](SEAM-0e.md) | A git child that a signal ended says which signal: what one failed run of CI could not tell | merged |
| [SEAM-0f](SEAM-0f.md) | Hook tests that a slow machine cannot fail | merged |
| [DV-12b](DV-12b.md) | v1's update and maintain under the worker, behind a switch | building since 2026-11-14 |
| [HO-10b](HO-10b.md) | Install and setup ask the adapter | merged |
| [HO-11](HO-11.md) | The install manifest | building since 2026-11-14 |
| [HO-12a](HO-12a.md) | Health's host names through the adapter | ready; after DV-12b, which adds a file to the same package |
| [HO-12b](HO-12b.md) | Each stage's lag beside its bar, and a verdict | after HO-12a and DV-12b |
| [HO-12c](HO-12c.md) | One line, prepared outside the hook | after HO-12b |
| [DV-13](DV-13.md) | The cost ledger, a token budget, and a low effort by default | after DV-12b and CT-2 |
| [DV-11a](DV-11a.md) | The runner's interface, typed outcomes, the probe and the sealed run, with no caller | merged |
| [DV-11b](DV-11b.md) | The runner's Codex backend | merged, with a second round for the lead's ruling that a run with no known event is never `ok` |
| [DV-11c](DV-11c.md) | The runner's Claude Code backend | merged, with a second round for what its reviews left |
| [HO-13a](HO-13a.md) | The signed release manifest, and its check in Go | not before the Stage 0 readout (decision 9); open then: one approval of a cut or two, and whether the manifest's first format carries the day it was published |
| [HO-13b](HO-13b.md) | Every release carries a signed manifest | after the readout and HO-13a |
| [HO-13c](HO-13c.md) | An upgrade that checks the signature, asks nothing and keeps the previous binary | after the readout, HO-13a and HO-11 |
| [HO-13d](HO-13d.md) | A daily check for a release, and one line when one has waited a week | after HO-13c and HO-12c |
| [EV-11](EV-11.md) | Ownership and size checks | merged; owed: DV-13's `paths` block, once DV-12b is merged; EV-11b is the ledger of what leaves the binary and when |
| [EV-11b](EV-11b.md) | The size ledger: what leaves the binary, and when | merged; by its first reading the end state is 47,030 lines against the ceiling's 53,694 |
| [IF-01a](IF-01a.md) | The contract matrix and its check | merged; no need is open: the lead's rulings of 2026-11-12 are in [the matrix](../contract-matrix.md) and [its rulings record](../contract-matrix-rulings.md) |
| [IF-01b](IF-01b.md) | The reader's fake | merged |
| [IF-01c](IF-01c.md) | The store's fake and the retrieval core's fake | merged; owed: the fake gains `ItemsCiting`, and keeps words and `Evidence`, once ST-05 puts them in the store's interface and the model |
| [ST-02](ST-02.md) | The storage properties, as tests against a reference model | merged; ST-02b carries what its notes and IF-01c's owe |
| [ST-02b](ST-02b.md) | The store's properties catch a store that never crashed, and a pass that loses a session's notes | building since 2026-11-14; before ST-05 is held to the harness |
| [ST-03a](ST-03a.md) | The line-preserving, fail-closed codec | merged |
| [ST-03b](ST-03b.md) | v1's pages read through the codec, as v1 read them | merged; ST-03c mends ST-03a's reading of a lock or a verified mark behind Unicode white space |
| [ST-03c](ST-03c.md) | The codec's protection reads a lock as v1 read it, behind white space, quotes and escapes | merged; the format's specification owes two sentences on it (the card's last section) |
| [ST-01b](ST-01b.md) | The profile checker reads through the codec, so both read a lock alike | merged |
| [ST-04](ST-04.md) | The migrator and the minimum reader version | merged; ST-07's card settles the files v1's `.gitignore` hid |
| [ST-04b](ST-04b.md) | Four tests and a figure that merged packages owe | building since 2026-11-14 |
| [EV-14a](EV-14a.md) | The scenario model, with v1's 12 scripts ported | merged |
| [EV-14b](EV-14b.md) | The fidelity scorer, and a reader for v1's pages | merged |
| [EV-14c](EV-14c.md) | Six new families, half held out | merged; EV-14e mends the scorer before these figures are put in a gate |
| [EV-14d](EV-14d.md) | v1's fidelity row, from a recording | merged |
| [EV-14e](EV-14e.md) | The scorer tells an agreement from a statement, and asks a slot of some kinds only | building since 2026-11-14; two reviewers; before the families' figures are put in a gate |
| [EV-17a](EV-17a.md) | Continuation tasks, their hidden tests, and the rows they give | merged |
| [EV-17b](EV-17b.md) | The continuation driver, with memory off and the ideal record | merged, with a second round for two rulings of the lead's; the pilot runs on it, and its host table waits for the pilot's recordings |
| [EV-17c](EV-17c.md) | The v1 condition: memory built from the earlier chat as v1 builds it, and what it cost; the pilot's v1 arm | merged; real runs after decision 28's keys |
| [EV-17g](EV-17g.md) | The continuation driver's guards before a live run | merged, with later rounds for a trace, a diagnosis, the points the v1 condition owed, and every chat's web tools off; nothing in the driver is owed before the pilot |
| [EV-17d](EV-17d.md) | The evaluation build, which no release can carry | merged; EV-17f adds the check of each published binary for a build tag |
| [EV-17e](EV-17e.md) | The 28 continuation tasks and their three fixture repositories | after EV-17a and the pilot (EV-03); it also settles grading as another user and the build cache grading uses (EV-17g checks a chat's reach for hidden files) |
| [EV-17f](EV-17f.md) | The release workflow reads each binary it built for a build tag | building since 2026-11-14; two reviewers; its first run on a real release is the owner's next cut |
| [EV-10a](EV-10a.md) | v1's design split into component specs, and dated decision records | ready, since the counters release is cut (2026.11.13); after DV-12b, which changes two of its files |
| [EV-10b](EV-10b.md) | Each spec lists the must-keep items it touches and the tests that hold them | after EV-10a |

SEAM-0 of the plan is split into four parts so that each is small and they do not touch the same files: a (this wave), b the hook's handler list, c the registries for status rows, config keys and benchmark scenarios, d (this wave). Part c is in the next wave, before the counters release that needs its registries; the benchmark's own list of scenarios is built by EV-16. Part b, the hook's handler list, changes the most time-critical code in the product, so it is built just before the first package that adds a step to the hook (DV-12), and not sooner. A fifth part, e, was added after one run of CI failed in a way its log could not explain: it makes the death of a git child by a signal say which signal.

The counters release of the plan (LG-01, LG-02, DV-01, DL-02, HO-02) is built as four parts, CT-1 to CT-4, so that they touch different files. The second report's prompt, and three sentences for v1's documents, are the lead's and are written with CT-4.

On 2026-11-12 the owner said the next release takes what is still coming, built on `main` with no pull request. So no package waits for a release to be cut any more: where a card says it merges after the owner has cut the counters release, it merges when its reviewers and a verifier have passed it.

Eleven rows of the plan's Stage 1 are built in parts, for the same reason. HO-12: a moves health's host names onto the adapter, b gives each stage its lag beside its bar and a verdict, c prepares the one line a chat start may show, outside the hook. IF-01: a is the matrix of what each track needs from another and the check that every need has a provider; b and c are the fakes, each built when its interface has merged; the runner's fake comes with DV-11, which writes that interface. ST-03: a is the codec and how a lock is read; b reads v1's pages through it and holds that reading equal to v1's. EV-14: a is the scenario model with v1's 12 scripts; b the scorer; then c, the six new families, and d, v1's row from a recording, at once. What a part leaves for a live run, the owner's machine or a host fact not yet recorded is named in its card and built later. DV-12: a is the worker as a package with no caller, which can start at once; b puts v1's two background jobs under it behind the switch `worker`, and waits for SEAM-0b and for the counters release, which change the same files of the hook. HO-10: a is the adapter and its table, with no caller; b moves install and setup onto it; health and the doctor's command file move with HO-12, which rebuilds those files, and not as a part of their own. LG-13: a is the decoder and the allowlist, built from v1's lists and the hosts' published source, with no caller; b holds it to what the core sittings record. DV-11: a writes the runner's interface, its outcomes, the probe and the sealed run; b (Codex) and c (Claude Code) are its backends, side by side; a later part, not yet carded, puts v1's update behind it with a switch, after DV-13 and the live gates. HO-13, which does not start before the Stage 0 readout: a is the signed manifest and its check, b the release that carries one, c the upgrade, d the daily check. EV-17: a is the task format, grader and rows; b the driver with two of its conditions; c the v1 condition; d the evaluation build; e the 28 tasks, after the pilot. EV-10: a splits v1's two large documents into specs and dated records, after the counters release has written its last sentences into them; b lists, in each spec, the must-keep items and the tests that hold them.

Two Stage 0 rows are throwaways built in parts, each ending in a live run that is the owner's to allow: the contract spike (DV-03a to d, deleted when DV-21 lands) and the pilot (EV-03a to c, deleted when EV-17 lands). No builder starts a live run: each command prints what it would call and stops unless it is given the flag the live accuracy run asks for.

**The pilot runs on EV-17's driver** (the lead's ruling of 2026-11-13). The pilot was to come before the continuation instrument and have a throwaway driver of its own. The instrument's task format, its driver and the evaluation build were built first, since the pilot's live run waits for the owner's leave, so EV-03b as carded would have built a second driver that does what EV-17b and EV-17c do, and the pilot would have proved a host table that nothing keeps. Three agents read both sides of the code for the lead and all reached the same answer. So: EV-03b becomes the export of the pilot's ten tasks into the continuation format; a new card, EV-17g, gives the driver its guards before a live run (a session that reports no usage, what a chat can reach, one record a run, grading's leftovers); EV-17c, amended, is the pilot's v1 arm; and EV-03c reads the continuation's rows and is kept for EV-17's own screens. The pilot's tasks, their checker and the export are what is thrown away when EV-17's own tasks land. Nothing changes in what the owner is asked for: leave, a ceiling for the pilot and decision 28's keys. The run-book for the live run is [pilot-runbook.md](../pilot-runbook.md), the lead's.

`eva-eval`'s commands are added as `eva-md`'s are: an instrument registers its command from its own file (`register` in `cmd/eva-eval/main.go`), so the cards that add an instrument do not edit that file.
