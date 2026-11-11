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
| [EV-16b](EV-16b.md) | The latency gate's rule, second round: noise from runs, and a slowdown no longer read as noise | built, reviewed and fixed on `pkg/ev-16b-r1`; the fix changed the rule, so it is verified by simulation before it merges; that is paused while the counters are fixed |
| [EV-16c](EV-16c.md) | The benchmark's count of background starts allows for a hook that missed its limit | after EV-16b |
| [CT-1](CT-1.md) | Counters release: what the hooks held back and delivered | built; in review |
| [CT-2](CT-2.md) | Counters release: what the update cost and why work waited | built and reviewed; its fix is being made |
| [CT-3](CT-3.md) | Counters release: capture checked against the host's own record | built; in review |
| [CT-4](CT-4.md) | Counters release: a content-free report | building since 2026-11-11, on the three other parts as built; reviewed once they are merged |
| [DL-04](DL-04.md) | The labelled retrieval set and its scorer, with v1's row | started 2026-11-11; paused while the counters are fixed |
| [DV-12a](DV-12a.md) | The worker, its wake marks and its journal | merged |
| [HO-10a](HO-10a.md) | The host adapter and its table | merged |
| [HO-14](HO-14.md) | Fake hosts that fail the way real ones do | started 2026-11-11; paused while the counters are fixed |
| [LG-12](LG-12.md) | The redaction corpus, its rates, and the baseline as an oracle | ready |
| [SEAM-0b](SEAM-0b.md) | The hook's handler list | after CT-1 and CT-2 |
| [DV-12b](DV-12b.md) | v1's update and maintain under the worker, behind a switch | after DV-12a, SEAM-0b and CT-1 to CT-3 |
| [HO-10b](HO-10b.md) | Install and setup ask the adapter | ready |
| [HO-11](HO-11.md) | The install manifest | after HO-10a and HO-10b |
| [HO-12a](HO-12a.md) | Health's host names through the adapter | after HO-10b and the counters release |
| [HO-12b](HO-12b.md) | Each stage's lag beside its bar, and a verdict | after HO-12a, DV-12b and the counters release |
| [HO-12c](HO-12c.md) | One line, prepared outside the hook | after HO-12b and SEAM-0b |
| [DV-13](DV-13.md) | The cost ledger, a token budget, and a low effort by default | after DV-12b and CT-2 |
| [EV-11](EV-11.md) | Ownership and size checks | started 2026-11-11; paused while the counters are fixed |
| [IF-01a](IF-01a.md) | The contract matrix and its check | started 2026-11-11; paused while the counters are fixed |
| [IF-01b](IF-01b.md) | The reader's fake | started 2026-11-11; paused while the counters are fixed |
| [IF-01c](IF-01c.md) | The store's fake and the retrieval core's fake | after IF-01a, ST-02 and DL-04 |
| [ST-02](ST-02.md) | The storage properties, as tests against a reference model | started 2026-11-11; paused while the counters are fixed |
| [ST-03a](ST-03a.md) | The line-preserving, fail-closed codec | merged |
| [ST-03b](ST-03b.md) | v1's pages read through the codec, as v1 read them | started 2026-11-11; paused while the counters are fixed |
| [ST-01b](ST-01b.md) | The profile checker reads through the codec, so both read a lock alike | started 2026-11-11; paused while the counters are fixed |
| [ST-04](ST-04.md) | The migrator and the minimum reader version | started 2026-11-11; paused while the counters are fixed |
| [EV-14a](EV-14a.md) | The scenario model, with v1's 12 scripts ported | merged |
| [EV-14b](EV-14b.md) | The fidelity scorer, and a reader for v1's pages | ready |
| [EV-14c](EV-14c.md) | Six new families, half held out | after EV-14a and EV-14b |
| [EV-14d](EV-14d.md) | v1's fidelity row, from a recording | after EV-14a and EV-14b, before EV-10a |
| [EV-10a](EV-10a.md) | v1's design split into component specs, and dated decision records | after the counters release |
| [EV-10b](EV-10b.md) | Each spec lists the must-keep items it touches and the tests that hold them | after EV-10a |

SEAM-0 of the plan is split into four parts so that each is small and they do not touch the same files: a (this wave), b the hook's handler list, c the registries for status rows, config keys and benchmark scenarios, d (this wave). Part c is in the next wave, before the counters release that needs its registries; the benchmark's own list of scenarios is built by EV-16. Part b, the hook's handler list, changes the most time-critical code in the product, so it is built just before the first package that adds a step to the hook (DV-12), and not sooner.

The counters release of the plan (LG-01, LG-02, DV-01, DL-02, HO-02) is built as four parts, CT-1 to CT-4, so that they touch different files. The second report's prompt, and three sentences for v1's documents, are the lead's and are written with CT-4.

Seven rows of the plan's Stage 1 are built in parts, for the same reason. HO-12: a moves health's host names onto the adapter, b gives each stage its lag beside its bar and a verdict, c prepares the one line a chat start may show, outside the hook. IF-01: a is the matrix of what each track needs from another and the check that every need has a provider; b and c are the fakes, each built when its interface has merged; the runner's fake comes with DV-11, which writes that interface. ST-03: a is the codec and how a lock is read; b reads v1's pages through it and holds that reading equal to v1's. EV-14: a is the scenario model with v1's 12 scripts; b the scorer; then c, the six new families, and d, v1's row from a recording, at once. What a part leaves for a live run, the owner's machine or a host fact not yet recorded is named in its card and built later. DV-12: a is the worker as a package with no caller, which can start at once; b puts v1's two background jobs under it behind the switch `worker`, and waits for SEAM-0b and for the counters release, which change the same files of the hook. HO-10: a is the adapter and its table, with no caller; b moves install and setup onto it; health and the doctor's command file move with HO-12, which rebuilds those files, and not as a part of their own. EV-10: a splits v1's two large documents into specs and dated records, after the counters release has written its last sentences into them; b lists, in each spec, the must-keep items and the tests that hold them.

`eva-eval`'s commands are added as `eva-md`'s are: an instrument registers its command from its own file (`register` in `cmd/eva-eval/main.go`), so the cards that add an instrument do not edit that file.
