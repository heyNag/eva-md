# IF-01c: the store's fake and the retrieval core's fake

**Plan:** section 8, Stage 1 row IF-01, and Stage 2's first paragraph: a need on ST-05 means its Stage 1 interface and fake while building. Rows ST-02 and DL-04 say whose interface each fake stands for. **Design:** `critique-delivery.md` issue 1 (the store's reference model exposed as a store fake, and a fake retrieval core) and issue 14 (DV-24 builds on the retrieval interface and its fake). **Size:** S. **Reviewers:** one. **Depends on:** IF-01a, ST-02 and DL-04, all merged. **Part of:** IF-01, after IF-01a. The runner's fake waits for DV-11.

## Why

ST-05, the store core, is large and on the critical path. Before it lands, DV-23, DV-24, DL-07, DL-08 and HO-21 need a store their tests can write to. DV-24 also shows the model related items from the retrieval core (DL-06) before DL-06 exists. ST-02 leaves an importable reference model and DL-04 leaves the `System` interface. This part puts a fake over each. It changes nothing v1 does and adds nothing to the binary, so there is no switch.

## What to build

1. **The store's fake**, `internal/store/storetest`, in memory, over ST-02's reference model in `internal/store/conformance`. It adds no behaviour of its own: every outcome is the model's.
   - `New(now func() time.Time) (*Fake, error)` opens the model with no fault.
   - `*Fake` is a `store.Store`, with a compile-time assertion.
   - It also offers `Files()`, `History()`, `Items()`, `SessionFile(session)` and `HandEdit(path, Edit)`, on an item's file or a session's, as ST-02's harness defines them.
   - `Reopen(fault store.Fault) error` closes the fake and opens it again on the durable part, as a process's death and restart would. A nil fault opens it with none.
2. **The retrieval core's fake**, `internal/eval/retrieval/retrievaltest`, which reads no set file:
   - `New(answers map[string][]string) *Fake` is a `retrieval.System`, with a compile-time assertion.
   - `Add` returns the item ids given for the event's id, in the order given, and nothing (silence) for any other event. It returns a copy each time.
   - `FailOn(eventID string, err error)` makes `Add` return `err` for that event only.
   - `Asked() []string` gives the event ids asked, in order.
3. **The matrix.** In the interfaces table of `docs/v2/contract-matrix.md`, the store's and the retrieval core's rows name these two folders in place of `waits: IF-01c`. No other line changes.

## Owns and may touch

`internal/store/storetest/**` (new), `internal/eval/retrieval/retrievaltest/**` (new), and those two rows of `docs/v2/contract-matrix.md`.

## Must not touch

Everything else. `internal/store/api.go`, `internal/store/conformance`, the rest of `internal/eval/retrieval`, `internal/cardcheck` and `internal/ledger` are read only. If a model or interface lacks what a fake needs, put the gap in the report.

## Accepted when

- `scripts/linux-check.sh` passes, and `cardcheck.Check` finds no problem with the two rows changed.
- ST-02's five properties (`RunCrashSafety` and the rest) pass on a `Subject` built over `storetest.Fake`, for at least 200 seeds, or fewer under the `race` build tag, as ST-02's own tests do.
- A test in package `storetest_test`, which imports only `store`, `storetest` and the standard library, uses the fake the way a consumer would:
  - a pass creates an item;
  - the same pass again makes no new commit and returns the same outcomes;
  - `Plan` returns `Apply`'s outcomes;
  - an operation on a locked item is refused as `locked`, and the item's bytes do not change;
  - after `Reopen` the item is still there.
- On a small set of the test's own, built with DL-04's types, two runs through DL-04's scorer give the same figures as `oracle`'s and `silent`'s: one with the fake given the `oracle`'s answers, one with the fake given none.
- `FailOn` fails only its event. `Asked` keeps the order. Changing a returned slice does not change the fake.
- A test fails if a non-test file outside either new package imports it. `go list -deps ./cmd/eva-md` lists neither package. Both import only the standard library and eva-md's packages. No test sleeps, reads the wall clock or writes outside `t.TempDir()`.
