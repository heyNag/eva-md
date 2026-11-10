# SEAM-0c: registries for status rows and switches

**Plan:** section 8, Stage 0, row SEAM-0 (registries), and section 9 ("each change behind a switch that defaults to today's behaviour"). **Size:** M. **Reviewers:** one.

## Why

Two shared places grow with almost every package: the rows `eva-md status` and `eva-md doctor` print, and the settings a package is switched on by. Today each is edited in the middle of a shared file. After this package a new row or switch is a new file. (The benchmark's scenarios get the same treatment in EV-16, which owns `scripts/benchmark.py`.)

## What to build

1. **Status rows** (`internal/health`). The rows that `Status` and the doctor produce come from a registry of row producers, each registered from the file that implements it, with an explicit place in the order. The output of `eva-md status` and `eva-md doctor`, with and without their flags, stays byte-identical in every existing test. A producer added by a test, in a test file, shows up at its place.
2. **Switches** (`internal/config`). A package needs a way to be off by default and on by a setting, without a new field in `Config` for each. Add one table to the config file, `[switches]`, whose keys are names that packages register, each with its default and the values it accepts (a boolean, or one of a short list of words). `config.Load` reads it; an unregistered name under `[switches]` is reported as an unknown key exactly as an unknown top-level key is today; a value a switch does not accept makes the config invalid in the way a bad `notice` does today. A config file with no `[switches]` table behaves exactly as today. No switch is registered by this package outside tests.
## Owns and may touch

`internal/health/**` and `internal/config/**`. README's config section only if a sentence about `[switches]` is needed for the unknown-key message to make sense; prefer no README change.

## Must not touch

`cmd/eva-md/**` (SEAM-0a owns the command side), `internal/hook/**`, `scripts/**`, the docs, the workflows.

## Accepted when

- `scripts/linux-check.sh` passes with no existing test changed.
- Golden tests pin today's `status` and `doctor` output on a fixture state, written before the refactor and unchanged after it (commit them first, on their own, so the diff shows it).
- A row producer registered in a test file appears at its place and nowhere else; two producers with the same place fail a test with a clear message.
- `[switches]`: an unregistered name is reported as unknown; a bad value invalidates the config as a bad `notice` does; a registered switch reads its default with no table and its value with one; the hook's config read is not slower (the benchmark's capture and brief figures in alternating runs of the old and the new binary differ by no more than two runs of the old one do; report the numbers).
