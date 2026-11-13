# IF-01b: the reader's fake

**Plan:** section 8, Stage 1 row IF-01 ("a fake of each that other tracks' tests can import"; "LG-10 ... for the types"), Stage 2's first paragraph, and row LG-10. **Design:** `critique-delivery.md` issue 1 (`ledgertest` over fixture events) and item 8 of its "The first ten work packages"; `internal/ledger/SPEC.md` sections 1, 7, 8 and 10. **Size:** S. **Reviewers:** one. **Depends on:** LG-10 (merged). **Part of:** IF-01. It runs beside IF-01a, which writes the matrix; it shares no file with it.

## Why

DV-20, DL-13 and EV-14 read the record through `ledger.Reader`. They need sessions of any shape their tests can build, such as a voice hand-over, a copy or a withheld line, without writing a v1 inbox. One check must hold the fake and every backend to the same contract, so the fake cannot drift from the real reader. Nothing v1 does changes, so there is no switch.

## What to build

The package `internal/ledger/ledgertest`, built on eva-md's ledger packages and the standard library only. Its package comment documents it. `internal/ledger/SPEC.md` is at its size limit and is not edited.

1. **Sessions.** `type Session struct { Key ledger.Key; Class event.Class; Events []event.Event }`.
2. **`New(ss ...Session) (*Fake, error)`** refuses:
   - an event that `event.Event.Validate` refuses;
   - seqs that do not rise;
   - a copy whose key or turn is not its first's;
   - a key given twice;
   - a class that is not valid.
3. **The fake is a `ledger.Reader`**, with a compile-time assertion.
   - `Sessions` lists the keys in the order given.
   - `Read` returns the session record and copies of the events, so a caller cannot change the fake.
   - The record's host, id and folder come from the key. `Events`, `Copies`, `Withheld` and `Wrappers` are counted from the events, as SPEC.md section 7 defines them.
   - An unknown key is an error.
4. **Fixtures from files.** `ReadSession(fsys fs.FS, name string, k ledger.Key, class event.Class) (Session, error)` reads a file of a header line and event lines in the codec's form (`event.DecodeHeader`, `event.Decode`). The header must name the key's host and session.
5. **Faults.** `(*Fake).FailSessions(err)` and `(*Fake).FailRead(k, err)` make those calls fail. A cancelled context ends either call with its error.
6. **The contract check**, `Check(ctx context.Context, r ledger.Reader) error`, holds any backend to the contract of SPEC.md and returns the first fault, naming the session and seq:
   - every listed key reads, with the key's host and session and a valid class;
   - the record's counts equal its events;
   - seqs rise from 1 or more, and every event validates;
   - a copy names an earlier event with its key and turn that is not itself a copy, and no two events that are not copies share a key;
   - `ledger.Resolve` of `e:<session>#<seq>` gives that event. It may give `ledger.ErrAmbiguous` only when the listing holds that session in more than one folder.

## Owns and may touch

`internal/ledger/ledgertest/**` (new) and `internal/ledger/inboxv1/conform_test.go` (new).

```paths
internal/ledger/ledgertest/**
internal/ledger/inboxv1/conform_test.go
```

## Must not touch

Everything else. The rest of `internal/ledger`, `internal/inbox` and `docs/v2/contract-matrix.md` are read only. The lead changes the reader's row of the matrix from `waits: IF-01b` to the package at the merge.

## Accepted when

- `scripts/linux-check.sh` passes.
- `inboxv1/conform_test.go` runs `ledgertest.Check` on LG-10's fixture inbox (`fixtureInbox`) and on 200 generated inboxes (`generate`), with the helpers of `inboxv1`'s own tests, and both pass. `Check` also passes on a fake read from a fixture file through `ReadSession`.
- For each clause of `Check`, a reader that wraps the fake and breaks that one clause makes `Check` fail and name it.
- `New` refuses each input of item 2, by test. A header that names another session makes `ReadSession` fail.
- Changing a returned event does not change the next `Read`. `FailRead`, `FailSessions` and a cancelled context each return their error.
- A test fails if a non-test file outside the package imports it, and `go list -deps ./cmd/eva-md` does not list it.
- `ledger.TestOnlyEvasNameAndNoNewModule` passes over the new files.
