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
| [EV-16b](EV-16b.md) | The latency gate's rule, second round: noise from runs, and a slowdown no longer read as noise | built and fixed twice, and its rule measured by simulation, on `pkg/ev-16b-r2`; merges after the owner has cut the counters release, since the gate's baseline restarts with it |
| [EV-16c](EV-16c.md) | The benchmark's count of background starts allows for a hook that missed its limit | after EV-16b |
| [EV-16d](EV-16d.md) | The latency gate's rule, third round: three rounds, and what a gate that blocks must show | after EV-16b and EV-16c |
| [CT-1](CT-1.md) | Counters release: what the hooks held back and delivered | merged |
| [CT-2](CT-2.md) | Counters release: what the update cost and why work waited | merged |
| [CT-3](CT-3.md) | Counters release: capture checked against the host's own record | merged |
| [CT-3b](CT-3b.md) | Counters release: the capture audit pairs a session by its id too | merged |
| [CT-4](CT-4.md) | Counters release: a content-free report | merged |
| [HF-00](HF-00.md) | The recorder for the host sittings, and fixtures with every value made up | Part 1 building since 2026-11-11: what the core sittings of [host-sitting-runbook.md](../host-sitting-runbook.md) need; Part 2 may come later |
| [HF-00b](HF-00b.md) | The recorder keeps the shape of a voice hand-over, and no word of it | after HF-00's Part 1; merges before the owner's first sitting |
| [HF-01](HF-01.md) | The shapes of the hosts' records, as a read-only command | building since 2026-11-11, on a branch; merges only after the owner has cut the counters release, since it changes the binary |
| [DV-03a](DV-03a.md) | Contract spike (throwaway): its scripts, renderer and two forms of the contract | ready |
| [DV-03b](DV-03b.md) | Contract spike: its checker, repair round and measures | after DV-03a |
| [DV-03c](DV-03c.md) | Contract spike: an arm's passes, recording, replay, fidelity and rows | after DV-03b and EV-14b |
| [DV-03d](DV-03d.md) | Contract spike: `eva-eval spike`, the plan of the run, and the frontier | after DV-03c and DL-04; the live run is the owner's to allow, on a price the lead states first |
| [EV-03a](EV-03a.md) | The pilot (throwaway): ten tasks, their format and their offline check | ready |
| [EV-03b](EV-03b.md) | The pilot's driver | after EV-03a; the live run waits for the owner's leave, a ceiling and decision 28's keys |
| [EV-03c](EV-03c.md) | The pilot's report and its README | after EV-03a; beside EV-03b |
| [DV-02a](DV-02a.md) | The live run's trace: cost, versions and what the host returned, for [the live gates](../live-gates-runbook.md) | ready after EV-14d; merges after the owner has cut the counters release, before DV-13 |
| [DL-04](DL-04.md) | The labelled retrieval set and its scorer, with v1's row | built on `pkg/dl-04`, in review; the baseline is accepted again on `main` after the merge |
| [DV-12a](DV-12a.md) | The worker, its wake marks and its journal | merged |
| [HO-10a](HO-10a.md) | The host adapter and its table | merged |
| [HO-14](HO-14.md) | Fake hosts that fail the way real ones do | building since 2026-11-11 |
| [LG-12](LG-12.md) | The redaction corpus, its rates, and the baseline as an oracle | building since 2026-11-11, on a branch; merges after the owner has cut the counters release, since it changes the binary |
| [LG-13a](LG-13a.md) | The hook payload decoder and the author allowlist, with no caller | ready |
| [LG-13b](LG-13b.md) | The decoder held to the hosts' recorded payloads | after LG-13a and the fixtures of the core sittings (HF-00) |
| [SEAM-0b](SEAM-0b.md) | The hook's handler list | building since 2026-11-11, on a branch; merges after the owner has cut the counters release, since it changes every hook |
| [DV-12b](DV-12b.md) | v1's update and maintain under the worker, behind a switch | after DV-12a, SEAM-0b and CT-1 to CT-3 |
| [HO-10b](HO-10b.md) | Install and setup ask the adapter | building since 2026-11-11, on a branch; merges after the owner has cut the counters release, since it changes the binary |
| [HO-11](HO-11.md) | The install manifest | after HO-10a and HO-10b |
| [HO-12a](HO-12a.md) | Health's host names through the adapter | after HO-10b and the counters release |
| [HO-12b](HO-12b.md) | Each stage's lag beside its bar, and a verdict | after HO-12a, DV-12b and the counters release |
| [HO-12c](HO-12c.md) | One line, prepared outside the hook | after HO-12b and SEAM-0b |
| [DV-13](DV-13.md) | The cost ledger, a token budget, and a low effort by default | after DV-12b and CT-2 |
| [DV-11a](DV-11a.md) | The runner's interface, typed outcomes, the probe and the sealed run, with no caller | ready |
| [DV-11b](DV-11b.md) | The runner's Codex backend | after DV-11a and HO-14; beside DV-11c |
| [DV-11c](DV-11c.md) | The runner's Claude Code backend | after DV-11a; beside DV-11b |
| [HO-13a](HO-13a.md) | The signed release manifest, and its check in Go | not before the Stage 0 readout (decision 9); open then: one approval of a cut or two, and whether the manifest's first format carries the day it was published |
| [HO-13b](HO-13b.md) | Every release carries a signed manifest | after the readout and HO-13a |
| [HO-13c](HO-13c.md) | An upgrade that checks the signature, asks nothing and keeps the previous binary | after the readout, HO-13a and HO-11 |
| [HO-13d](HO-13d.md) | A daily check for a release, and one line when one has waited a week | after HO-13c and HO-12c |
| [EV-11](EV-11.md) | Ownership and size checks | building since 2026-11-11 |
| [IF-01a](IF-01a.md) | The contract matrix and its check | built on `pkg/if-01a`, in review |
| [IF-01b](IF-01b.md) | The reader's fake | building since 2026-11-11 |
| [IF-01c](IF-01c.md) | The store's fake and the retrieval core's fake | after IF-01a, ST-02 and DL-04 |
| [ST-02](ST-02.md) | The storage properties, as tests against a reference model | building since 2026-11-11 |
| [ST-03a](ST-03a.md) | The line-preserving, fail-closed codec | merged |
| [ST-03b](ST-03b.md) | v1's pages read through the codec, as v1 read them | building since 2026-11-11 |
| [ST-01b](ST-01b.md) | The profile checker reads through the codec, so both read a lock alike | building since 2026-11-11 |
| [ST-04](ST-04.md) | The migrator and the minimum reader version | building since 2026-11-11 |
| [EV-14a](EV-14a.md) | The scenario model, with v1's 12 scripts ported | merged |
| [EV-14b](EV-14b.md) | The fidelity scorer, and a reader for v1's pages | building since 2026-11-11 |
| [EV-14c](EV-14c.md) | Six new families, half held out | after EV-14a and EV-14b |
| [EV-14d](EV-14d.md) | v1's fidelity row, from a recording | after EV-14a and EV-14b, before EV-10a |
| [EV-17a](EV-17a.md) | Continuation tasks, their hidden tests, and the rows they give | ready |
| [EV-17b](EV-17b.md) | The continuation driver, with memory off and the ideal record | after EV-17a |
| [EV-17c](EV-17c.md) | The v1 condition: memory built from the earlier chat as v1 builds it, and what it cost | after EV-17b; real runs after EV-17d and decision 28's keys |
| [EV-17d](EV-17d.md) | The evaluation build, which no release can carry | ready; merges after the owner has cut the counters release, since it adds a file to `cmd/eva-md` |
| [EV-17e](EV-17e.md) | The 28 continuation tasks and their three fixture repositories | after EV-17a and the pilot (EV-03) |
| [EV-10a](EV-10a.md) | v1's design split into component specs, and dated decision records | after the counters release |
| [EV-10b](EV-10b.md) | Each spec lists the must-keep items it touches and the tests that hold them | after EV-10a |

SEAM-0 of the plan is split into four parts so that each is small and they do not touch the same files: a (this wave), b the hook's handler list, c the registries for status rows, config keys and benchmark scenarios, d (this wave). Part c is in the next wave, before the counters release that needs its registries; the benchmark's own list of scenarios is built by EV-16. Part b, the hook's handler list, changes the most time-critical code in the product, so it is built just before the first package that adds a step to the hook (DV-12), and not sooner.

The counters release of the plan (LG-01, LG-02, DV-01, DL-02, HO-02) is built as four parts, CT-1 to CT-4, so that they touch different files. The second report's prompt, and three sentences for v1's documents, are the lead's and are written with CT-4.

Eleven rows of the plan's Stage 1 are built in parts, for the same reason. HO-12: a moves health's host names onto the adapter, b gives each stage its lag beside its bar and a verdict, c prepares the one line a chat start may show, outside the hook. IF-01: a is the matrix of what each track needs from another and the check that every need has a provider; b and c are the fakes, each built when its interface has merged; the runner's fake comes with DV-11, which writes that interface. ST-03: a is the codec and how a lock is read; b reads v1's pages through it and holds that reading equal to v1's. EV-14: a is the scenario model with v1's 12 scripts; b the scorer; then c, the six new families, and d, v1's row from a recording, at once. What a part leaves for a live run, the owner's machine or a host fact not yet recorded is named in its card and built later. DV-12: a is the worker as a package with no caller, which can start at once; b puts v1's two background jobs under it behind the switch `worker`, and waits for SEAM-0b and for the counters release, which change the same files of the hook. HO-10: a is the adapter and its table, with no caller; b moves install and setup onto it; health and the doctor's command file move with HO-12, which rebuilds those files, and not as a part of their own. LG-13: a is the decoder and the allowlist, built from v1's lists and the hosts' published source, with no caller; b holds it to what the core sittings record. DV-11: a writes the runner's interface, its outcomes, the probe and the sealed run; b (Codex) and c (Claude Code) are its backends, side by side; a later part, not yet carded, puts v1's update behind it with a switch, after DV-13 and the live gates. HO-13, which does not start before the Stage 0 readout: a is the signed manifest and its check, b the release that carries one, c the upgrade, d the daily check. EV-17: a is the task format, grader and rows; b the driver with two of its conditions; c the v1 condition; d the evaluation build; e the 28 tasks, after the pilot. EV-10: a splits v1's two large documents into specs and dated records, after the counters release has written its last sentences into them; b lists, in each spec, the must-keep items and the tests that hold them.

Two Stage 0 rows are throwaways built in parts, each ending in a live run that is the owner's to allow: the contract spike (DV-03a to d, deleted when DV-21 lands) and the pilot (EV-03a to c, deleted when EV-17 lands). No builder starts a live run: each command prints what it would call and stops unless it is given the flag the live accuracy run asks for.

`eva-eval`'s commands are added as `eva-md`'s are: an instrument registers its command from its own file (`register` in `cmd/eva-eval/main.go`), so the cards that add an instrument do not edit that file.
