# ST-04b: four tests and a figure that merged packages owe (ST-04, ST-03, EV-14, DL-04, DV-02)

**Plan:** follows [ST-04](ST-04.md), [ST-03b](ST-03b.md), [EV-14d](EV-14d.md) and [DL-04](DL-04.md), and what their notes and the index left owed; AGENTS.md's rule that a test must be able to fail; DL-04's reading of section 5.4's bar on wrong context, at least 160 negatives in each half, since zero in 160 is under 1.9%. **Size:** S. **Reviewers:** one. **Depends on:** ST-04, ST-03b, EV-14d and DL-04, all merged.

## Why

Each package left one gap: a guard no test catches alone, a branch with no fixture, an exit no test can see, a bound no figure is large enough to give. Nothing v1 does changes, so there is no switch, and nothing here is built into `./cmd/eva-md`: it adds no built line.

## What to build

1. **The manifest's dotted-key guard (ST-04).** `internal/store/migrate/manifest.go:83` refuses a dotted key. `TestCheck`'s rows "min_reader dotted" and "min_reader spaced and dotted" (`reader_test.go:38`, `:50`) follow `manifest("2", "2")`, which holds `min_reader = 2`, so with `k.dotted` dropped the repeat check (line 87) still refuses them. Add two rows whose file holds `schema = 2`, the store id and no plain `min_reader`: one with `min_reader.x = 1`, one with `min_reader .x = 1`, each wanting `ErrNoManifest` at known 2.
2. **A plain value that ends in a colon, then a comment (ST-03).** `colonComment` (`internal/store/codec/v1.go:362`) refuses a value ending in `:` (line 365); the four `colon-*-comment.md` difference files hold their colon inside the value. Add `testdata/v1/differences/colon-end-comment.md`, a copy of `colon-value-comment.md` whose writer line is `writer: Tide: # x`, and its row in `TestV1Differences` (`v1_test.go:84`): v1 read `the writer "Tide: # x"`; the codec gives `refused(ErrNotWritten, Protection{})` with an error text holding "a comment after a plain value that holds a colon". `TestDifferencesAreDifferences` (`v1oracle/oracle_test.go:228`) reads the file by glob and must pass unchanged; if v1's library cannot read it, stop and report. `SPEC.md` and `protectsLess` stay as they are.
3. **The exit when the last page cannot be written (EV-14).** `eva-eval fidelity` with `v1.12.md` missing is tested (`TestFidelityMissingPages`, `cmd/eva-eval/fidelity_test.go:247`): nothing is owed there. What is owed: `cmd/live-accuracy/main.go:264-268` sets the exit to 1 when the last script's page failed, but the case "the last" of `TestAPageThatCannotBeWritten` (`record_test.go:608`) misses the target anyway, so it exits 1 without those lines too. No fake answers meet the target on all twelve scripts, so `main.go` gains one seam: line 104 reads its scripts from a package variable `suiteScripts`, which defaults to `accuracy.Scripts`. A new test in `record_test.go` sets it, restoring it with `t.Cleanup`, to two copies of `accuracy.Conditional()` renamed apart, answered with the right answer of `internal/update/accuracy/live/live_test.go:105`: with `--pages` the run exits 0 and writes `v1.01.md` and `v1.02.md`; with a folder made where `v1.02.md` goes, it prints the report on stdout and "live-accuracy: could not write the page v1.02.md: " on stderr, and exits 1. Nothing else in `main.go` changes.
4. **False events over all negatives of a half (DL-04).** The per-class `false_events` (`internal/eval/retrieval/score.go:191-192`) count fewer than 160 events: on the committed set the halves hold 241 and 234 negatives, but the largest class, `near_miss`, 128 and 117. Add `retrieval.<half>.negative.false_events`: of a half's events whose class is not `positive`, those given any addition. `measure` computes it beside the class figures, left out with no events like every ratio; `Metrics()` gives it after the three classes of each half; the group's name is an exported constant. `eval/retrieval/metrics.json` gains its two metrics in that order, direction `lower`, with the four paths every metric has; `gates.json` is not changed. Then accept `baseline.json` again: commit the code and catalogue; on that clean commit, with DIR a folder outside the checkout, run `go run ./cmd/eva-eval retrieval --system v1-recall --out DIR` and `go run ./cmd/eva-eval baseline accept --catalogue eval/retrieval/metrics.json --out eval/retrieval/baseline.json DIR/results.jsonl`; commit the baseline. The lead accepts it again on `main` after the merge.
5. **A run that cannot start leaves nothing (DV-02a).** When the pages folder cannot be made, the run removes the recording it made, as it does when the trace cannot be made (`main.go`, the `MkdirAll` of `--pages`); with no `--record` nothing changes. The two pages cases of `TestATraceThatCannotBeMade` (`trace_test.go`) gain `--record` and want the recording gone, and a case without `--trace` wants the same. The report gives the sentence for `docs/verification.md`, which says today that the recording is left, for the lead.

## Owns and may touch

The tests and fixture of items 1 and 2; the seam of item 3 and its test; the scorer, its tests, the catalogue and the baseline. Every commit carries `Card: ST-04b`.

```paths
internal/store/migrate/reader_test.go
internal/store/codec/v1_test.go
internal/store/codec/testdata/v1/differences/colon-end-comment.md
cmd/live-accuracy/main.go
cmd/live-accuracy/record_test.go
cmd/live-accuracy/trace_test.go
internal/eval/retrieval/score.go
internal/eval/retrieval/score_test.go
eval/retrieval/metrics.json
eval/retrieval/baseline.json
```

## Must not touch

Every other file: `manifest.go`, `v1.go`, the codec's `SPEC.md` and `v1oracle/`, `cmd/live-accuracy/main_test.go`, `record.go` and `trace.go`, `internal/update/**`, `eval/retrieval/set/**`, `gates.json`, `retrievaltest/**` and `cmd/eva-eval/**` are read only. `AGENTS.md`, `README.md` and the cards are the lead's.

## Accepted when

- `scripts/linux-check.sh` passes; `go list -deps ./cmd/eva-md | grep -c 'internal/eval\|internal/store/'` prints 0.
- The report shows each mutation failing a test: `k.dotted` dropped from `manifest.go:83`; `|| strings.HasSuffix(v, ":")` dropped from `v1.go:365`; `code = 1` dropped from `main.go:267`; item 5's removal of the recording dropped; the new figure counting a positive event, or left out.
- `TestDifferencesAreDifferences` passes, `protectsLess` unchanged.
- Every existing test of `cmd/live-accuracy` passes with the seam at its default, `main_test.go` unedited.
- On the five-event set the new held-out figure is 1 of 2, and 2 of 2 with the sibling item added. In every run a test makes, it equals `all.silence_neg`'s events less its count, over the same events. The `silent` arm on the committed set gives 0 over at least 160 in each half, and the table prints a bound under 1.9% on that line.
- `TestTheGateFiles` and `TestTheBaselineIsV1sRun` pass; the baseline keeps every earlier row's metric and value, and holds two rows more.

## The lead's rulings of 2026-11-13

The card's checker left these to the lead.

- DV-02a's notes owed two tests to the package that next changes `cmd/live-accuracy`, and this is it. The lead rules the behaviour: a run that cannot make its pages folder takes its recording away too (item 5). The other, a test of `createTrace` removing what it made when it fails part way, needs a seam in `trace.go` and stays owed to a card that changes that file.
- The group's name `negative` stands.
- The codec's `SPEC.md` is not widened here: it has four bytes to spare, and ST-03c makes the room.
- The lead accepts the retrieval baseline again on `main` after the merge.

## As merged

Merged on 2026-11-14 (4891b1a, from `pkg/st-04b-r1`, after one review, one round of fixes and a verification that accepted it). The lead accepted the retrieval baseline again on `main` at that commit, in a scratch home: its 42 rows hold the values the branch's did, the 40 earlier ones unchanged, and two more (false events over all negatives, tuning 108 of 234 and held-out 120 of 241).

- ST-03c had merged the colon-end fixture first; this card made it a copy of its neighbour with the writer line alone changed, as the card words it, and holds the refusal's words.
- The card's line numbers moved: the `code = 1` it names at `main.go:267` is at line 277.
- **Owed: `docs/verification.md`**, three changes, which wait for EV-10a's merge since EV-10a holds that file while it builds. (1) In the paragraph on the trace, "A pages folder that cannot be made after it takes the trace away again, but leaves the recording, an empty file: remove it before the next run, which wants a file that does not exist yet." becomes "A pages folder that cannot be made takes the trace and the recording away too, so the next run can make the recording again." (2) In the live accuracy run's entry, "and a pages folder that cannot be made after it takes the trace away again (`TestATraceThatCannotBeMade`)" becomes "and a pages folder that cannot be made takes both away, with a trace or without one (`TestATraceThatCannotBeMade`)". (3) In the same entry, after "(`TestAPageThatCannotBeWritten`)": "; a run whose answers meet the target exits 0 with every page written, and 1 when only the last script's page cannot be written, its report printed and the page named on stderr (`TestTheLastPageOfARunThatMeetsTheTarget`)".
