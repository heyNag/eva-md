# LG-22a: the project registry's types, the project id as a function of a key, and its fake

**Plan:** section 8, Stage 2 row LG-22 and Stage 2's first paragraph; section 6.1, design 6; decision 20, taken at its recommendation. [Ruling 11](../contract-matrix-rulings.md) and the matrix row "LG-22, project identity". **Design:** `ledger.md` section 8.2 (the plan overrides its random id); `docs/spec/store-format.md` sections 3 and 5 (the project identity record). **Size:** S. **Reviewers:** one. **Depends on:** LG-10 and ST-01, both merged. **Part of:** LG-22, its first part, in Stage 1, with no caller.

## Why

ST-07 and DL-07 build against LG-22's interface and fake, but no Stage 1 row writes them. This part does: the types, the id as a pure function of a key, and a fake with one project per key. It changes nothing v1 does, so there is no switch, and it adds no line to `./cmd/eva-md`.

Left for the rest of LG-22: joins on two kinds of evidence and a fork never joined, writing and following `merged_into`, folder projects and their keys, the resolver's changes in `internal/identity`, the real registry and its file, `eva-md project`, and the scripted gate. Left for ST-07: which key is first for a v1 project that had two, and where it comes from.

## What to build

New packages `internal/ledger/registry` and `internal/ledger/registry/registrytest`, each with a package comment. Their non-test files import the standard library only (the fake also imports `registry`).

1. **`func ProjectID(key string) (string, bool)`**: `p_` and the first 60 bits of SHA-256 of the key's bytes, high first, as 12 characters of `0123456789abcdefghjkmnpqrstvwxyz` (store-format.md section 3). The key is hashed as given: never trimmed, lower-cased or normalized. An empty key gives `""` and false. The key of a project v1 knew is its key as v1's resolver writes it (`identity.Project.Key`, which v1's inbox entries carry as `project`); the general area's is `general` (`identity.GeneralKey`).
2. **`Kind`**: `KindRepo` `"repo"`, `KindLocal` `"local"` and `KindGeneral` `"general"`, the strings of `identity.Kind`, and `KindFolder` `"folder"`, a folder outside git as its own project (decision 20).
3. **`Project`**: `ID`, `Kind`, `Title`, `Keys` (every key that has named it, first first; for a project v1 knew, its v1 keys, the record's `legacy_keys`), `Remotes`, `RootCommits`, `Roots` (local folders, never written to the store), `MergedInto` (`""` until LG-22 joins it; a joined project keeps its id), `Related`. Its doc says `ID` is `ProjectID(Keys[0])`.
4. **`Registry`**, an interface: `Projects(ctx) ([]Project, error)` lists every project, joined ones too, sorted by id; `ByKey(ctx, key) (Project, bool, error)` gives the project a key named, following no join, and false with no error for a key none named.
5. **The fake**, `registrytest.New(es ...Entry) (*Fake, error)`, with `Entry{Key, Kind, Title, Remotes, Roots}`: one project per entry, `Keys` holding its key alone, joins none. It refuses an empty key, a key given twice, an unknown kind, and `general` without `KindGeneral` or the other way round. It is a `registry.Registry` (a compile-time assertion), returns copies, is safe for concurrent use, and has `FailProjects(err)` and `FailByKey(err)`. A cancelled context ends a call with its error.
6. **`registrytest.Check(ctx, r registry.Registry) error`** names the first fault of any backend: the listing is sorted with no id twice; every id is `ProjectID(Keys[0])` and every kind is known; no key names two projects; `ByKey` of every key gives its project; `MergedInto` and `Related` name listed projects, never the project itself.
7. **ST-01's fixture.** Its `p_6pbe1q7fxb5h` is `ProjectID("local-scratch-9f2c1a")`, its legacy key. `p_00gb3367a19r` is not: it hashes the remote `github.com/acme/orders`, and its key `github.com__acme__orders` gives `p_c0476xchw35b`. It is not made again: that would rename its folder and its two branch workstreams, whose ids hash the project id, and change tests of ST-01, ST-03 and ST-04. A new `internal/store/testdata/README.md`, beside the store so the profile never reads it, says the fixture's ids are made up and lists, one a line in backticks, each project id that does not follow the rule: today `p_00gb3367a19r`. No file of `fixture-store` changes.

## Owns and may touch

`internal/ledger/registry/**` (new) and `internal/store/testdata/README.md` (new). At the merge the lead changes the matrix row's fake from `waits: LG-22` to `internal/ledger/registry/registrytest`; the builder's report gives words for `AGENTS.md`'s layout list.

```paths
internal/ledger/registry/**
internal/store/testdata/README.md
```

## Must not touch

`internal/identity`; every other file of `internal/store` and `internal/ledger`, `internal/ledger/SPEC.md` included; `docs/spec/store-format.md`; `docs/v2/contract-matrix.md`; `AGENTS.md`, `README.md` and the cards.

## Accepted when

- `scripts/linux-check.sh` passes, and `go list -deps ./cmd/eva-md` names neither package.
- Vectors: `github.com__acme__orders` gives `p_c0476xchw35b`, `local-scratch-9f2c1a` `p_6pbe1q7fxb5h`, `general` `p_1zne2van6sdc`, the remote form `github.com/acme/orders` `p_00gb3367a19r`, and `GitHub.com__acme__orders` `p_btxwm93qrxpc`; the empty key gives false. Over a few hundred generated keys each id matches `^p_[0-9a-hjkmnp-tv-z]{12}$` and is the same on a second call.
- A test holds the three shared kinds equal to `identity`'s, and `ProjectID(identity.GeneralKey)` to the vector.
- A test reads every `project.md` of the fixture store through `internal/store/codec`, at least two: each id is `ProjectID` of its first legacy key, but those the README lists. A listed id that follows the rule, or is in no record, fails too.
- `New` refuses each input of item 5, by test. Changing a returned project does not change the next call; each fault and a cancelled context return their error.
- `Check` passes on the fake; for each clause of item 6, a registry wrapping the fake that breaks it makes `Check` fail and name it.
- A test fails if a non-test file outside `registrytest` imports it, as `TestOnlyTestsImportIt` in `internal/ledger/ledgertest` does, with a planted importer showing it can fail. `ledger.TestOnlyEvasNameAndNoNewModule` passes over the new files.
- A reviewer's own mutations (a normalized key, 64 bits or hex, the README's list dropped, a fake that joins) are each caught by a test.

## The lead's rulings of 2026-11-13

The card's checker left these to the lead.

- The names stand: `registry.Registry` with `Projects` and `ByKey`, `Project`'s fields as carded, and `ProjectID` giving a string and a bool.
- `registrytest.Check` stays: it holds the fake and the later real registry to one contract.
- The four kinds stay, `general` among them, so a v1 project's kind converts directly; ST-07's card decides whether v1's general area is split.
- The fixture's ids are said to be made up in a README beside it; the fixture is not made again.

## As merged

Merged on 2026-11-14 (47a8460, from `pkg/lg-22a-r1`, after one review, one round of fixes and a verification that accepted it).

- **Stricter than the card names.** `Check` also refuses a project with no key, an empty key, and a key repeated within one project; the fake refuses two keys that give one id. Each follows from "every id is ProjectID(Keys[0])", "an empty key has no id" and "no key names two projects". If LG-22 rules that one project may repeat a key, the repeat clause skips it.
- `Kind` has a small `Known()` method the card did not name.
- AGENTS.md's list of v2's packages does not name `internal/ledger/registry` or its fake `registrytest`: the file is 14 bytes under its limit, so the words wait for a change that condenses it.
