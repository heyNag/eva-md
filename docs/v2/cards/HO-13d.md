# HO-13d: a daily check for a release, and one line when one has waited a week

**Plan:** section 8, Stage 1 row HO-13; section 6.5, design 8 ("the worker checks daily") and design 5 (without the clock, jobs hooks start carry the steps); section 5.4, row "Upgrade"; decision 9 of section 7, on its recommendation. **Design:** `docs/v2/working-papers/design/hosts.md` sections 10.2 (the channel) and 10.6 (`check`: a record, then one line after 7 days). **Part of:** HO-13, after [HO-13a](HO-13a.md) and [HO-13c](HO-13c.md). **Size:** S. **Reviewers:** one, on the contract and the network rule. **Depends on:** HO-13a; HO-13c (its switch `upgrades`); HO-12c (its conditions), and so DV-12b, which edits `maintain.go` first. **Starts after the Stage 0 readout.**

## Why

Only a person's `eva-md upgrade` looks for a release in v1, so a fix for a host's changed format waits until someone notices. With `upgrades = "check"`, eva-md looks once a day from a job a hook starts detached, never from the hook, and says so in one line once a release has waited a week; HO-13c's upgrade then installs it and asks nothing. The clock (HO-22) and background installs come later.

## What to build

1. **The record**, `internal/release/upgrades/record.go` (new, in HO-13c's package, which has no network code): `<state>/release-check.json`, 0600, written with `paths.WriteFileAtomicAt`, read under a size limit: `{"v":1,"checked":"<RFC 3339>","result":"current|available|unreachable|refused","tag":"<tag or empty>","since":"<RFC 3339, when this tag was first found available>"}`. `since` stays while the same tag is available and moves when a newer one is found. Nothing else: no address, no message, no machine's name.
2. **The check**, `release.CheckDaily(ctx, d paths.Dirs, version string, now time.Time) (upgrades.Record, error)` in `internal/release/check.go` (new), `version` being this binary's. When the record's `checked` is under 20 hours before `now`, it does nothing. Otherwise it finds the latest tag with `release.Latest`; a tag `release.Replaces` does not count as newer is `current`. When `release.TrustedSigners()` returns a key, a newer tag is `available` only once its `release.json` and signature pass `VerifyRelease` and name that tag, else `refused`; with no key it is `available`, as `upgrade --check` would say. A failed request is `unreachable`. Every result is written with `checked`. It adds no time limit of its own: `LookupTimeout` and `DownloadTimeout` bound it. The request carries nothing about the machine: no version, no id, no header of eva-md's own.
3. **Where it runs.** `eva-md maintain`, the daily job the first hook event of a day starts detached, calls it as its last step, with a wiki and without one, only when `upgrades.Switch.Value(c)` is `"check"` for the config maintain loaded. It prints one line, `maintain: release check: <result>`, and never changes maintain's exit code. `--push-only` does not call it. No hook calls it, and nothing under `internal/hook` imports `internal/release`.
4. **The status row**, `internal/health/release.go` (new), a producer registered with `registerRows` at a free place, only when the switch is `"check"`: `PASS` "up to date, checked <day>"; `PASS` "eva-md <tag> is available since <day>; eva-md upgrade installs it and asks nothing"; `UNKNOWN` for `unreachable` and `refused`, with the reason, and "not checked for <n> days" when `checked` is over 3 days old or there is no record. It reads the record through `internal/release/upgrades`, never `internal/release`. Switch off, no row, and doctor's and status's output are today's.
5. **The line.** In HO-12c's `internal/health/conditions.go`, one condition more, `release.waiting`, medium and last, relevant on every host when the switch is `"check"` and the record says `available` with `since` 7 days or more before now. Its line: `eva.md: eva-md <tag> has waited a week; eva-md upgrade installs it and asks nothing.` HO-12c's `Prepare` and the hook show it under their own rules; this part changes no hook code.
6. **Docs**: README's "Privacy" line that eva-md contacts GitHub only when a person runs upgrade or a script, and "Upgrade"'s "the only command that uses the network itself", gain "and, with `upgrades = "check"`, once a day from the daily job, to read the latest release's tag and manifest"; `internal/release/version.go`'s package comment, which says no daily job looks for a release; design.md's account of maintain's steps and of status's rows, or, once EV-10a has merged, the `## Since the split` of the specs `docs/spec/INDEX.md` names for them.

## Owns and may touch

```paths
internal/release/upgrades/record.go
internal/release/upgrades/record_test.go
internal/release/check.go
internal/release/check_test.go
internal/release/version.go
cmd/eva-md/maintain.go
cmd/eva-md/maintain_release_test.go
internal/health/release.go
internal/health/release_test.go
internal/health/conditions.go
internal/health/conditions_test.go
README.md
docs/design.md
```

The `record`, `check`, `release` and `maintain_release` files are new. In `version.go`, the package comment; in `maintain.go`, the one call and its line; in `conditions.go` and its test, the one condition and its golden line; in the docs, item 6's lines.

## Must not touch

`internal/hook`, `upgrade.go`, the switch, `internal/testlimits`, the workflows and scripts, HO-12c's other conditions and its notice file's format, and maintain's other steps.

## Accepted when

- `scripts/linux-check.sh` passes; switch off, no existing test changes and maintain makes no request (a test server that fails the test on any request).
- A test runs `go list -deps ./internal/hook`: it names neither the package `internal/release` nor `net/http`.
- Against a local test server, with an injected clock: a second run within 20 hours makes no request; a newer signed release is `available` and keeps its `since` across days; a newer tag with a bad or missing signature, when a key is trusted, is `refused` and never `available`; a server answering 500 is `unreachable`; maintain's exit code is the same in each case.
- The server's log of each request holds no version, no path of the machine and no header but Go's own.
- With the record `available` since 6 days, HO-12c's `Prepare` prepares no line for it; since 7 days, the line above, under 160 characters; switch off, never.
- Each of these mutations fails a test: the 20-hour gate dropped, an unsigned tag counted as available, the call made with `--push-only`.
