# Cards

A card is the lead's order for one work package of [the v2 plan](../plan.md). A builder starts from its card and reads nothing else it does not point to. The plan governs: where a card and the plan differ, the plan is right and the card is fixed.

How a package runs (plan section 9): the builder works on a branch in its own worktree and commits there; reviewers who were given only the card and the diff try to break it; the builder fixes what they find; the lead runs the project's checks on the result, merges it into `main` and pushes. Nothing is released by a merge.

Rules every card carries, so they are said once here:

- v2 is built beside v1. A package changes v1's behaviour only behind a switch that defaults to today's behaviour. With every switch off, v1's own tests and benchmark are unchanged.
- A diff stays inside the paths on its card. If the work needs another path, the builder stops and says so in its report.
- Every rule of [AGENTS.md](../../../AGENTS.md) holds. In particular: `internal/redact/baseline` is never edited; tests never touch the real machine and never depend on its speed; a hook never calls a model or the network; only eva's name is shown; no new dependency.
- Before a builder reports, `scripts/linux-check.sh` passes from the root of its worktree.
- No secret, and nothing from a real chat, goes into a fixture. Test values are made up.

| Card | Package | State |
| --- | --- | --- |
| [SEAM-0a](SEAM-0a.md) | The command table, and document checks that no longer read prose | merged |
| [SEAM-0d](SEAM-0d.md) | Guards on the plugin's bytes and on the upgrade from 2026.11.5 | building |
| [LG-10](LG-10.md) | The event model and the one reader | building |
| [LG-11](LG-11.md) | The redaction driver with spans, same behaviour | building |
| [SEAM-0c](SEAM-0c.md) | Registries for status rows and switches | next |
| [EV-12](EV-12.md) | The results store and gates | next |
| [ST-01](ST-01.md) | The store's written format and its profile checker | next |
| [EV-16](EV-16.md) | Latency that can fail a build, and a baseline before any hook changes | next |
| [CT-1](CT-1.md) | Counters release: what the hooks held back and delivered | after EV-16 |
| [CT-2](CT-2.md) | Counters release: what the update cost and why work waited | after EV-16 |
| [CT-3](CT-3.md) | Counters release: capture checked against the host's own record | after EV-16 and LG-10 |
| [CT-4](CT-4.md) | Counters release: a content-free report | after CT-1 to CT-3 |

SEAM-0 of the plan is split into four parts so that each is small and they do not touch the same files: a (this wave), b the hook's handler list, c the registries for status rows, config keys and benchmark scenarios, d (this wave). Part c is in the next wave, before the counters release that needs its registries; the benchmark's own list of scenarios is built by EV-16. Part b, the hook's handler list, changes the most time-critical code in the product, so it is built just before the first package that adds a step to the hook (DV-12), and not sooner.

The counters release of the plan (LG-01, LG-02, DV-01, DL-02, HO-02) is built as four parts, CT-1 to CT-4, so that they touch different files. The second report's prompt, and three sentences for v1's documents, are the lead's and are written with CT-4.
