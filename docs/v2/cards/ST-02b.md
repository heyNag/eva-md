# ST-02b: the store's properties catch a store that never crashed, and a pass that loses a session's notes

**Plan:** section 6.2 (design 3, one write path; design 5, a codec that keeps every line a person wrote), the Stage 1 rows ST-02 and ST-05 (whose gate is these properties on the real store); decision 21 (one writer, one machine). **Follows:** [ST-02](ST-02.md) and [IF-01c](IF-01c.md), both merged; it closes what their "As merged" sections owe. **Design:** `docs/v2/working-papers/design/store.md` 5.5 (`episode.append` never rewrites earlier lines), 5.7 (a person's lines kept byte for byte), 10 (every property killed by a broken variant). **Size:** S. **Reviewers:** one. **Depends on:** ST-02, IF-01c.

## Why

Two holes in `internal/store/conformance`. First, when an armed crash never fires, `runner.apply` in `harness.go` goes on (`case !r.fired: return res, nil`), and so does `CheckCrashSafety`'s own loop (`if !a.fired`). A store that drops its fault, or an adapter that never passes it on, passes CrashSafety, ExactlyOnce, Restore and Replay without one crash. Second, no property checks that a pass keeps what a session file holds; only `TestAPassKeepsTheLinesOfASessionFileItDoesNotOwn` does, on the model alone. This lands before ST-05 is held to the harness: ST-05's card will depend on it. Nothing in the binary imports the package: it adds no line to `./cmd/eva-md`, changes nothing v1 does, and needs no switch.

## What to build

1. **An armed crash must fire.** Every `Apply` of a well-formed pass on an open store reaches `recover` first, and `plan` when the pass is not in its history (`api.go`: the fault is called at each step, in order). So a crash armed at `recover`, or at `plan` on a pass not in `History()` before that `Apply`, that does not fire fails at that step, with a `Failure` whose detail says `a crash armed at <step> did not fire`. This holds in `runner.apply` and in `CheckCrashSafety`'s loop. A crash armed at a later step may still not fire, since a pass can write fewer files or nothing.
2. **Over a run, at least one.** `RunCrashSafety`, `RunExactlyOnce`, `RunRestore` and `RunReplay` fail when no armed crash fired over their seeds, with a `Failure` naming the property, the last seed as `Seed`, and the detail `no armed crash fired in <n> seeds`. The count lives in an unexported function that runs a property over a list of seeds; `RunX` gives it seeds 1 to n, and item 2's test its own list. The exported `CheckX` keep their signatures. `RunProtection` and item 4's property arm no crash and keep no count. With n of 200 or more, each of the four also fails when some step of `store.Steps` had no armed crash fire over the run (`no armed crash fired at <step> in <n> seeds`), so a store that passes its fault on at `recover` and `plan` alone is caught; the report gives, for each of the four, the first seed by which the model and the fake have fired every step, and if either needs more than 200 the builder says so and takes the least round number that holds.
3. **A model that ignores its fault**: a variant `faultIgnored` (an unexported field of `Model`, beside the others), whose `handle.step` never calls the fault. It goes in `variants` in `properties_test.go` for each of the four crash properties, on a fixed seed, with `did not fire` as its words. A second variant, `faultEarlyOnly`, calls the fault at `recover` and `plan` alone: it passes items 1 and 2's first rule and fails each of the four at n of 200.
4. **A sixth property, SessionKept**, in `properties.go` beside `CheckProtection`: `PropSessionKept = "SessionKept"`, `CheckSessionKept(newSubject, seed)` and `RunSessionKept(t, newSubject, n)` on a `Subject`. It plays the schedule of `options{edits: true}` with every pass uncrashed, as Protection does. For each session file it keeps a list of expected texts: the `text` of each `session.append` whose outcome was `applied`, and the `Text` of each `EditChange` made by hand on that file. At an `EditChange` the list becomes the new text plus the earlier texts the file still holds just after the edit, since a person may rewrite what a pass wrote. After every event, each expected text is still in its file, byte for byte. The schedule's texts are unique (`s<n> …`), so a plain search is enough. It joins `TestTheModelKeepsEveryProperty` and the `check` switch in `properties_test.go`.
5. **Two variants kill it**: `sessionRewritten`, which writes a session file anew on each pass, with its coverage and only that pass's notes; and `handTextDropped`, which keeps the notes the store wrote and drops every other line of the log. Each fails SessionKept on a fixed seed.
6. **`PROPERTIES.md`**, still at most 12 KB (10,668 bytes today; tighten other prose if needed, dropping no source or variant): six properties, in its opening too; a row and a section for SessionKept, its sources (P6.2 design 5, S5.5, S5.7, S10 durability-1) and its variants; `faultIgnored` in the four crash rows; a sentence in "Subjects and schedules" on the rules of items 1 and 2. `TestThePropertiesTable` wants the six rows in the table's order.
7. **The fake keeps all six.** `TestTheFakeKeepsEveryProperty` in `internal/store/storetest/properties_test.go` gains SessionKept (it crashes nothing), and its comments no longer say the harness lets an unfired fault pass. Its own count of faults stays.

## Owns and may touch

The conformance package, and the one test file of the fake that lists the properties.

```paths
internal/store/conformance/**
internal/store/storetest/properties_test.go
```

## Must not touch

`internal/store/api.go`, `ids.go`, and every other file of `internal/store/storetest`. What `newSchedule` gives for a seed: the variants' fixed seeds rest on it. What the model does with no variant set. The plan, the design papers, the cards and `docs/spec/store-format.md`: a gap goes in the report.

## Accepted when

- `scripts/linux-check.sh` passes.
- Each `RunX`, `RunSessionKept` included, passes 1,000 seeds on the model and 200 on the fake, 40 under the `race` tag, as today.
- `TestEachVariantFailsItsProperty` shows `faultIgnored` failing CrashSafety, ExactlyOnce, Restore and Replay, each on a seed the model passes; every variant there today still fails as it does now.
- A test shows item 2's rule alone: on seeds where no crash is armed at `recover` or `plan`, `faultIgnored` passes each check, and the run over those seeds fails with `no armed crash fired`, for at least one crash property, while the model passes the same run.
- `sessionRewritten` and `handTextDropped` each fail SessionKept on a fixed seed, and `TestThePropertiesTable` passes with six rows.
- A reviewer's own mutations are each noticed by a test: `case !r.fired: return res, nil` put back, `CheckCrashSafety`'s `!a.fired` branch put back, item 2's count dropped, SessionKept checking a note only after the pass that wrote it, a hand-written text never put on its list, and a refused note put on it.
- `TestNoCheckTouchesTheMachine`, `TestOnlyEvasNameShows` and `TestNothingUnderCmdReachesTheSuite` pass.

## The lead's rulings of 2026-11-13

The card's checker left these to the lead.

- The stronger rule is in: over a run of 200 seeds or more, an armed crash fires at every step (item 2), with a variant that fails it (item 3).
- The names stand: `faultIgnored`, `faultEarlyOnly`, `sessionRewritten`, `handTextDropped` and `SessionKept`.
- ST-05's card, when it is written, depends on this one.
