# SEAM-0a: the command table, and document checks that no longer read prose

**Plan:** section 8, Stage 0, row SEAM-0 (first sentence), and decision 15. **Size:** S to M. **Reviewers:** one.

## Why

Today a new command means an edit to the `switch` in `cmd/eva-md/main.go`, to the hard-coded usage line, to one sentence of `docs/design.md` and to one line of `AGENTS.md`, because `cmd/eva-md/docs_test.go` ties them together. Every later package that adds a command or a file would collide on those four places. `docs/design.md` is frozen by decision 15, so tests must stop reading its prose.

## What to build

1. **A command table.** A new file `cmd/eva-md/table.go` holds the type and the registry. Each command registers itself from the file that implements it, with: its name or names, the file that holds it, its place in the usage line, its synopsis lines as README's Commands list shows them, and its handler. `run` in `main.go` dispatches through the table and prints the usage line from it. Adding a command then means adding a file, and no edit to `main.go` or `table.go`.
2. **Byte-identical behaviour.** The usage line, every usage error, the unknown-command message, exit codes, and the special paths of `hook`, `version` and `setup` (which calls `recordSetupWrites`) stay exactly as they are. Existing tests are the judge; none of them is weakened.
3. **Document checks against the table.** In `cmd/eva-md/docs_test.go`:
   - `TestTheDesignNamesTheBuiltCommands` no longer reads `docs/design.md`. It checks that the usage line lists exactly the table's commands, and keeps the behaviour half of the old test (what a planned or unknown command prints, with exit 2), with the command names held in the test and not parsed from prose.
   - `TestTheDesignShowsEveryFlagTheReadmeShows` no longer reads `docs/design.md`. It checks README's Commands list against the table: every command README shows is in the table, and every flag README shows for it is in that command's synopsis in the table.
   - `TestAgentsNamesEveryFileOfTheCLI` no longer needs every file named in `AGENTS.md`. It checks that every non-test file of `cmd/eva-md` is named by a table entry or by a short list of support files that `table.go` holds with one phrase each.
   - `TestAgentsPreviewsWhatTakesApply` and the two verification tests stay as they are.
4. **One edit to `AGENTS.md`.** The Layout line that starts ``- `cmd/eva-md`: `` is rewritten once so that it no longer lists each file: it says that `table.go` lists every command with the file that holds it and the files that support them, and keeps the two facts agents need most (that `timelimits_raised.go` is the tests' release stand-in and sets `hostOS`, and why `rosetta_other.go` exists). Nothing else in `AGENTS.md` changes.

## Owns and may touch

`cmd/eva-md/main.go`, `cmd/eva-md/table.go` (new), `cmd/eva-md/docs_test.go`, each other non-test file of `cmd/eva-md` only to add its registration, and the one Layout line of `AGENTS.md`.

## Must not touch

`docs/design.md`, `docs/verification.md`, `README.md`, any file under `internal/`, any workflow or script.

## Accepted when

- `go test ./cmd/eva-md` and `scripts/linux-check.sh` pass, with no existing test changed except the three named above.
- A test shows the guard works: a file with no entry, and a README flag missing from the table, each make the check fail (drive the check with a made-up list inside the test; do not add a broken file).
- `eva-md` with no arguments prints the same bytes as before, shown by a test that holds the line.
- The benchmark is not slower: registration costs nothing a hook can feel. `eva-md hook` must not walk the table before it runs.
