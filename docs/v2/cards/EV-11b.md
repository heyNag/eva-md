# EV-11b: the size ledger: what leaves the binary, and when

**Plan:** section 7, "Where the decisions stand" (the ruling of 2026-11-13 on decision 26); section 6 before 6.0; section 8, "What runs in parallel" and the CUT-4 row; section 9's last rule; 10.5. **Follows:** [EV-11](EV-11.md), whose "The size check's first reading" owes it. **Design:** `critique-simplicity.md` issue 1 and section 6 item 4. **Size:** M; adds no line to `./cmd/eva-md`. **Reviewers:** one. **Depends on:** EV-11.

## Why

EV-11's first reading put the binary 9,542 lines over v1's 53,694. The ceiling binds at CUT-4, and the lead brings a ledger of what leaves, and when, to each stage's readout. Its figures are counted, never typed, and a test holds what must always be true of it. Nothing eva-md does changes: no switch.

## What to build

1. **`docs/v2/size-ledger.md`** (new). It states its format at the top. Each table row is one path in backticks, as `guard.ParsePattern(s, false)` accepts. No file is under two rows.
   - **(a) v1 paths the plan deletes.** Columns: Path, Built lines, Deleted by (ST-13, DV-26, or a track's own), Stage (`CUT-4` today), Named (`file` or `loose`), Scheduled on (the merged card whose `deletes` block names this exact path, or `none`). By file: `internal/okfadapter/**`, `internal/wikiinit/**`, `internal/update/**`, `internal/lint/**`, `internal/threadpage/**`, `internal/knowledgepage/**`, `internal/hook/trigger.go`, `internal/hook/pointers.go`, `internal/hook/recall.go`, `internal/config/answers.go`, `internal/install/templates/recall-skill.md`, `cmd/live-accuracy/**`. Loose, as whole files, each reading given in the report: catchup's hook slicing, inbox's readers, wikigit's commit path, `internal/resume` but its search and show, the guided setup, `config.env`, the doctor's rows, and the `cmd/eva-md` files that call only what (a) deletes.
   - **(b) The counters release's instruments.** Columns: Path, Built lines, Instrument, Added by, Fate (`leaves at CUT-4`, or `kept by <plan id>`), Could take it over (such as HO-12b, HO-23, HO-24, DV-13, LG-21, EV-23). The rows: `internal/hook/record.go`, and `held.go`, `delivered.go`, `hookweek.go` in `internal/health` (CT-1, 4341736); `internal/health/updates.go` (CT-2, 6114939); `backlog.go`, `report.go` and each `report_*.go` in `internal/health` (CT-4, 80e6aa2); `internal/catchup/record.go`, `internal/ledger/audit/**`, `cmd/eva-md/audit.go` (CT-3, CT-3b); `internal/ledger/shapes/**`, `cmd/eva-md/shapes.go` (HF-01). CT-2's new files in `internal/update` count in (a). Under the table, with no figure: the v1 files those commits changed that no (a) row holds, among `internal/hook/hook.go`, `resume.go` and `spawn.go`, `internal/health/watchdog.go`, `internal/catchup/catchup.go`, `internal/resume/resume.go`, `cmd/eva-md/commands.go`, `update.go` and `table.go`.
   - **(c) Merged, not yet called.** Columns: Path, Lines, Built by, First caller (a plan id). `internal/store/api.go`, `internal/store/ids.go`, `internal/store/codec/**`, `internal/store/migrate/**`, `internal/store/profile/**`, `internal/work/**`, `internal/derive/modelrun/**`, `internal/host/capture/**`.
   - **The end state**, last: a fenced block, info string `ledger`, lines `key: value`: `counted-at` (12 hex), `built`, `leaves-v1` (the (a) total), `leaves-instruments` (the leaving (b) rows), `leaves`, `not-yet-called` (the (c) total), `end-state`, `ceiling`.
2. **The figures.** (a), (b): `guard.Built`'s lines of the files under the row, v1's or not, 0 for a file not built. (c): newlines of the row's `.go` files that are not `_test.go`, not under `testdata` and not built. `leaves` = `leaves-v1` + `leaves-instruments`; `end-state` = `built` − `leaves` + `not-yet-called`; `ceiling` is `guard.Ceiling`.
3. **`internal/eval/guard/ledger.go`** (new): the check, given built lines and v1's files as `Count` is (a nil v1 skips the v1 faults), reusing `Block`, `Patterns`, `Merged` and `CardFile`. Faults: a path that does not exist; an (a) row naming no file v1 had, or a (b) row naming one; two rows over one file; a merged card's `deletes` entry with no (a) row of that path scheduled on that card; a `Scheduled on` naming a card not merged or whose block lacks the path; a sum or `ceiling` that does not add up; a row scheduled on a merged card (frozen, plan section 9) whose figure is not today's. Differences: every other row or key whose figure is not today's, as `row <path>: says N, counts M`.
4. **`eva-eval size --ledger FILE`**, FILE read under `--repo`: prints `size`'s report, then each row and key with today's figure, the differences and the faults. Exit 1 on either, 0 otherwise, 2 for usage, an unreadable ledger or no v1 history. Synopsis `size [--repo dir] [--ledger file]`. Without the flag, `size` is unchanged.

## Owns and may touch

The ledger (new), `ledger.go` and its test (new), and `cmd/eva-eval/guard.go` and its test, for the flag.

```paths
docs/v2/size-ledger.md
internal/eval/guard/ledger.go
internal/eval/guard/ledger_test.go
cmd/eva-eval/guard.go
cmd/eva-eval/guard_test.go
```

## Must not touch

The guard package's other files (call its test helpers, change none); `Ceiling`; any card or `deletes` block; `.github/workflows/ci.yml`; `cmd/eva-eval/main.go`; `AGENTS.md` and `README.md` (give the lead a line for "Read first").

## Accepted when

- `scripts/linux-check.sh` passes; `TestSizeCommand` is unchanged and passes.
- With cards and a ledger in `t.TempDir()`, built lines and v1's files given by hand, each fault above fails, named, and each can be shown to pass when mended; an unmerged card's entry needs no row. A row one line off is a difference that fails the check, and is a fault when its row is frozen. A `kept by` row adds nothing to `leaves`. (c) counts no built, test or `testdata` file.
- `cmd/eva-eval`'s test gives `--ledger` a missing file and a fixture with no v1 history (exit 2), and an unknown flag.
- `TestTheSizeLedgerHoldsForThisRepository` runs the check on the repository, reached as `TestTheRepositorysCardsAreWellFormed` reaches it, with `Built` under `goEnv` and v1 nil: it fails on a fault, not a difference, needs no git history, and prints the line to write.
- `go list -deps ./cmd/eva-md | grep -c internal/eval` prints 0.
- The report gives `eva-eval size --ledger docs/v2/size-ledger.md` on `main` with its whole history (no difference), each `loose` row's reading, and the cards building or ready whose paths name a file under a row.

## The lead's rulings of 2026-11-13

The card's checker left these to the lead.

- The test on the repository holds the ledger's structure, its sums and the rows a merged `deletes` block has frozen. Any other difference from today's count makes `eva-eval size --ledger` exit 1, and the lead runs it and rewrites the figures at each merge and each readout: a builder of another card cannot own the ledger, so no builder's check may fail on it.
- Each loose reading of what leaves is the builder's, listed in the report, and stands only once the lead has accepted it at the merge.
- Whether CI's `guards` job runs the ledger is the lead's, after the merge.

## As merged

Merged from `pkg/ev-11b-r1` on 2026-11-14, after a review, one fix round and a verification that accepted it. A restart of the machine cut the first verifier off; a second started afresh. The review's major fault: seven rows counted whole files of which the same reading keeps a part, against the ledger's own rule, so what leaves was overstated by about 4,000 lines. Those files are set aside now, each named in its bullet with what is kept.

**The first reading**, by the command at the merge: built 63,269; v1 code that leaves 20,609; instruments that leave 5,871; so 26,480 leave; v2 code nothing calls yet 10,241; end state 47,030 against the ceiling's 53,694. About 6,700 lines are left for what v2 still builds into the binary.

The lead's rulings at the merge:

- **Each loose reading still counted is accepted as a reading**, to be confirmed by the card that deletes the file: `internal/resume`'s `lines.go` and `lineage.go`; the four files of the guided setup, though they come to about three times the plan's estimate, since only `eva-md setup` reaches them; `internal/health`'s `knowledge.go`, `code.go` and `times.go`; and `cmd/eva-md`'s `update.go`, `maintain.go`, `knowledge.go` and `commit.go`.
- **The rule's three exceptions stand**: a time limit tests raise, a part that names a package a row deletes, and one declaration of at most 12 lines keep nothing of a file.
- **`internal/hook`'s `maintain.go` and `spawn.go` stay out** until DV-26's card names what of them goes.
- **CI's `guards` job does not run the ledger.** A merge that moves a count would turn it red until the lead had written the figures again. The lead runs `go run ./cmd/eva-eval size --ledger docs/v2/size-ledger.md` at each merge that changes Go the binary holds, and at each readout, and writes what it prints.

Known and left: a package that deletes a path a merged card froze, before the card of its stage is marked merged, fails the ledger's test of this repository on its own branch. Every stage is CUT-4 today. The card that first deletes such a path ahead of CUT-4 gets the ledger in its paths, and the lead sets the row's stage in the same merge.
