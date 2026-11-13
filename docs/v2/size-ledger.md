# The size ledger: what leaves the binary, and when

v1's macOS binary is built from 53,694 lines of this repository's Go, and v2.0 may be built from no more: the ceiling binds at CUT-4, and until then the net figure is reported at every merge and fails nothing ([the plan](plan.md), section 7, "Where the decisions stand", the ruling of 2026-11-13 on decision 26). This ledger says, file by file and package by package, what leaves the binary and when, what the counters release's instruments weigh and what becomes of them, and the merged v2 code that nothing calls yet. The lead brings it to each stage's readout. Its figures are counted, never typed: `go run ./cmd/eva-eval size --ledger docs/v2/size-ledger.md` prints each of them as it counts today, with the line to write for each that is not today's.

## Its format

The ledger has three tables, each known by its header, and a last fenced block. `internal/eval/guard`'s `CheckLedger` reads them; no other table may stand in the ledger, and a path in backticks outside a table's rows, as in the prose below, is not a row.

- Each row of a table names one path in backticks: an exact file, or a folder ending in `/**` for everything under it, as a card's `deletes` block names them. The path exists in the repository, and no file is under two rows of the ledger.
- **(a) v1 paths the plan deletes.** Columns: Path; Built lines; Deleted by, a plan id or a track's own (such as `hosts' own`); Stage, the package that removes the path (CUT-4 today); Named, `file` when the plan names the path, `loose` when the row is a reading of the plan's looser words; Scheduled on, the merged card whose `deletes` block names this exact path, or `none`. Every row names at least one file v1 had at commit 51b241c. Every entry of a merged card's `deletes` block has a row of that path scheduled on that card, and a row scheduled on a card names a merged card whose block names its path. A deletes block freezes what it names (plan section 9), so a scheduled row's figure may not move.
- **(b) The counters release's instruments.** Columns: Path; Built lines; Instrument; Added by, the card and, where there is one, the commit; Fate, `leaves at CUT-4` or `kept by <plan id>`; Could take it over, the v2 packages that might take the instrument's work over inside the budget. No row names a file v1 had.
- **(c) Merged, not yet called.** Columns: Path; Lines; Built by; First caller, the plan id of the package expected to build the code into eva-md.
- **The end state**, last: a fenced block whose info string is `ledger`, one `key: value` a line: `counted-at`, the first 12 hexadecimal digits of the commit the figures were counted at; `built`; `leaves-v1`, the sum of (a); `leaves-instruments`, the sum of the (b) rows that leave; `leaves`, the two together; `not-yet-called`, the sum of (c); `end-state`, built less leaves plus not-yet-called; and `ceiling`.

How a figure is counted. In (a) and (b): the lines, counted as newlines, of the files under the row that are built into eva-md for macOS, as `eva-eval size` counts them, v1's files or not; a file not built counts 0. In (c): the newlines of the row's `.go` files that are not `_test.go`, not under a `testdata` folder and not built. A row that is a loose reading counts whole files: the part of a file that a reading would keep is not split off, and a file of which a reading keeps a part is not counted.

What fails. `go test ./internal/eval/guard -run TestTheSizeLedgerHoldsForThisRepository` fails on the ledger's structure, a sum that does not add up, a ceiling that is not the ceiling, a frozen row whose figure moved, and, among the faults above, every one that needs no git history but a path gone from a row no deletes block has frozen, which a package may move; `-v` prints the lines to write. `eva-eval size --ledger` fails on those, on the faults that need v1's files, and on every figure that is not today's: other packages move the count at every merge, so a figure that is merely out of date fails no builder's check, and the lead writes the figures again at each merge and each readout.

## (a) v1 paths the plan deletes

| Path | Built lines | Deleted by | Stage | Named | Scheduled on |
| --- | ---: | --- | --- | --- | --- |
| `internal/okfadapter/**` | 1038 | ST-13 | CUT-4 | file | ST-03a |
| `internal/wikiinit/**` | 572 | ST-13 | CUT-4 | file | none |
| `internal/update/**` | 8977 | DV-26 | CUT-4 | file | none |
| `internal/lint/**` | 401 | DV-26 | CUT-4 | file | none |
| `internal/threadpage/**` | 1881 | DV-26 | CUT-4 | file | none |
| `internal/knowledgepage/**` | 2244 | DV-26 | CUT-4 | file | none |
| `internal/hook/trigger.go` | 235 | DV-26 | CUT-4 | file | none |
| `internal/hook/pointers.go` | 594 | recall's own | CUT-4 | file | none |
| `internal/hook/recall.go` | 184 | recall's own | CUT-4 | file | none |
| `internal/config/answers.go` | 363 | hosts' own | CUT-4 | file | none |
| `internal/install/templates/recall-skill.md` | 0 | recall's own | CUT-4 | file | none |
| `cmd/live-accuracy/**` | 0 | proof's own | CUT-4 | file | none |
| `internal/catchup/catchup.go` | 1541 | record's own | CUT-4 | loose | none |
| `internal/catchup/drift.go` | 103 | record's own | CUT-4 | loose | none |
| `internal/wikigit/commit.go` | 610 | ST-13 | CUT-4 | loose | none |
| `internal/resume/resume.go` | 836 | recall's own | CUT-4 | loose | none |
| `internal/resume/lines.go` | 328 | recall's own | CUT-4 | loose | none |
| `internal/resume/now.go` | 162 | recall's own | CUT-4 | loose | none |
| `internal/resume/git.go` | 184 | recall's own | CUT-4 | loose | none |
| `internal/resume/rules.go` | 137 | recall's own | CUT-4 | loose | none |
| `internal/resume/lineage.go` | 30 | recall's own | CUT-4 | loose | none |
| `cmd/eva-md/setup.go` | 1336 | hosts' own | CUT-4 | loose | none |
| `cmd/eva-md/setup_checks.go` | 190 | hosts' own | CUT-4 | loose | none |
| `cmd/eva-md/setup_refusals.go` | 443 | hosts' own | CUT-4 | loose | none |
| `cmd/eva-md/setup_repo.go` | 974 | hosts' own | CUT-4 | loose | none |
| `internal/health/knowledge.go` | 108 | hosts' own | CUT-4 | loose | none |
| `internal/health/code.go` | 174 | hosts' own | CUT-4 | loose | none |
| `internal/health/watchdog.go` | 482 | hosts' own | CUT-4 | loose | none |
| `internal/health/times.go` | 51 | hosts' own | CUT-4 | loose | none |
| `cmd/eva-md/update.go` | 170 | DV-26 | CUT-4 | loose | none |
| `cmd/eva-md/maintain.go` | 176 | DV-26 | CUT-4 | loose | none |
| `cmd/eva-md/knowledge.go` | 48 | DV-26 | CUT-4 | loose | none |
| `cmd/eva-md/commit.go` | 92 | ST-13 | CUT-4 | loose | none |

The rows named by file are the plan's: sections 6.0, 8 ("What runs in parallel") and the CUT-4 row. CT-2's new files in `internal/update` count under its row. The loose rows, by the plan's words:

- **`internal/catchup`'s hook slicing** (section 8, the record track): `catchup.go`, the registration of a transcript and its recovery after a reply and at a chat's start, and `drift.go`, the format-drift signal that recovery records. `parser.go`, the parsers, is the record track's to keep; `forget.go` and `prune.go` keep the recovery records within bounds and stay while the worker might keep such records; `record.go` is CT-3's, in (b).
- **`internal/inbox`'s readers** (section 8): no row. The readers, `ReadFrom` and the watermarks, share `inbox.go` with the writer the record track keeps, and `walk.go` is search's walker.
- **`internal/wikigit`'s commit path** (section 8, ST-13): `commit.go`, the daily commit and the push that follows it. `git.go`, the runner, and `scan.go`, the secret scan, stay.
- **`internal/resume` but its search and show** (section 8, the recall track): the files of the brief, `resume.go`, `lines.go`, `now.go`, `git.go` and `rules.go`, and `lineage.go`, which only the update calls. `search.go`, `search_index.go` and `show.go` stay, and so do the files they and the MCP server read: `pages.go`, `read.go`, `knowledge.go`, `systems.go`, `projectpage.go` and `links.go`. Whole files are counted, so `resume.go`'s helpers that search uses count as leaving.
- **The guided setup** (sections 6.0 and 8, HO-21): `cmd/eva-md/setup.go` and the three files that serve it alone.
- **`config.env`** (section 8): no row of its own. Its reader and template are `internal/config/answers.go`, a row by file.
- **The doctor's rows** (section 8, the hosts track; health is rebuilt small, HO-23): the files of `internal/health` that hold only rows, `knowledge.go` (the knowledge pages), `code.go` (the code status of system pages), `watchdog.go` (the hooks' heartbeats, freshness, failures and drift) and `times.go` (the hooks' times). `health.go`, `status.go`, `background.go`, `hosts.go` and `mcp.go` hold what the hook, the MCP server or a command calls as well, and stay; `rows.go` is SEAM-0c's registry.
- **The `cmd/eva-md` files that call only what (a) deletes**: `update.go` (the update; DV-26 keeps `eva-md update` as an alias of the new pass), `maintain.go` (the daily maintain, which DV-26 deletes), `knowledge.go` (maintain's knowledge step) and `commit.go` (the commit path).

## (b) The counters release's instruments

| Path | Built lines | Instrument | Added by | Fate | Could take it over |
| --- | ---: | --- | --- | --- | --- |
| `internal/hook/record.go` | 105 | the hook counters: what each hook held back and delivered | CT-1, 4341736 | leaves at CUT-4 | HO-12b, HO-23 |
| `internal/health/held.go` | 48 | the hook counters: what each hook held back and delivered | CT-1, 4341736 | leaves at CUT-4 | HO-12b, HO-23 |
| `internal/health/delivered.go` | 72 | the hook counters: what each hook held back and delivered | CT-1, 4341736 | leaves at CUT-4 | HO-12b, HO-23 |
| `internal/health/hookweek.go` | 297 | the hook counters: a week of counts, behind the two doctor rows | CT-1, 4341736 | leaves at CUT-4 | HO-12b, HO-23 |
| `internal/health/updates.go` | 327 | the update counters behind `status --updates` | CT-2, 6114939 | leaves at CUT-4 | DV-13, HO-24 |
| `internal/health/backlog.go` | 242 | `doctor --report` | CT-4, 80e6aa2 | leaves at CUT-4 | EV-23 |
| `internal/health/report.go` | 424 | `doctor --report` | CT-4, 80e6aa2 | leaves at CUT-4 | EV-23 |
| `internal/health/report_hooks.go` | 123 | `doctor --report` | CT-4, 80e6aa2 | leaves at CUT-4 | EV-23 |
| `internal/health/report_record.go` | 255 | `doctor --report` | CT-4, 80e6aa2 | leaves at CUT-4 | EV-23 |
| `internal/health/report_updates.go` | 101 | `doctor --report` | CT-4, 80e6aa2 | leaves at CUT-4 | EV-23 |
| `internal/health/report_wiki.go` | 133 | `doctor --report` | CT-4, 80e6aa2 | leaves at CUT-4 | EV-23 |
| `internal/catchup/record.go` | 101 | `audit capture` | CT-3, CT-3b | leaves at CUT-4 | LG-21, EV-23 |
| `internal/ledger/audit/**` | 1643 | `audit capture` | CT-3, CT-3b | leaves at CUT-4 | LG-21, EV-23 |
| `cmd/eva-md/audit.go` | 75 | `audit capture`, and the `audit` command that `audit shapes` runs under | CT-3, CT-3b | leaves at CUT-4 | LG-21, EV-23 |
| `internal/ledger/shapes/**` | 1868 | `audit shapes` | HF-01 | leaves at CUT-4 | none |
| `cmd/eva-md/shapes.go` | 57 | `audit shapes` | HF-01 | leaves at CUT-4 | none |

The v1 files those commits changed that no (a) row holds, whose changes stay in the binary while the files do: `internal/hook/hook.go` (CT-1, CT-2), `internal/hook/resume.go` (CT-1), `internal/hook/spawn.go` (CT-2) and `cmd/eva-md/commands.go` (CT-2, CT-4). HF-01 changed `cmd/eva-md/table.go` too, which is SEAM-0a's and not v1's. Those commits also changed `internal/health/watchdog.go` (CT-1), `internal/resume/resume.go` (CT-1), `internal/catchup/catchup.go` (CT-3) and `cmd/eva-md/update.go` (CT-2), which loose rows of (a) hold, and `internal/hook/pointers.go` (CT-1), `internal/hook/trigger.go` and `internal/update` (CT-2), which rows by file hold.

## (c) Merged, not yet called

| Path | Lines | Built by | First caller |
| --- | ---: | --- | --- |
| `internal/store/api.go` | 163 | ST-02 | ST-05 |
| `internal/store/ids.go` | 77 | ST-02 | ST-05 |
| `internal/store/codec/**` | 2168 | ST-03a, ST-03b | ST-05 |
| `internal/store/migrate/**` | 1805 | ST-04 | ST-05 |
| `internal/store/profile/**` | 1682 | ST-01, ST-01b | ST-05 |
| `internal/work/**` | 1089 | DV-12a | DV-12b |
| `internal/derive/modelrun/**` | 2535 | DV-11a, DV-11b, DV-11c | DV-11 |
| `internal/host/capture/**` | 722 | LG-13a | LG-20 |

## The end state

```ledger
counted-at: deaf73409923
built: 63269
leaves-v1: 24664
leaves-instruments: 5871
leaves: 30535
not-yet-called: 10241
end-state: 42975
ceiling: 53694
```
