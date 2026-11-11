# EV-14b: the fidelity scorer, and a reader for v1's pages

**Plan:** section 6.6 (the fidelity instrument: "scores each fact by value, with a precision side that counts what no span supports"), section 8 Stage 1 row EV-14 and its gate, the lead's choice in section 7 that no model judges a gated number. **Design:** `docs/v2/working-papers/design/evaluate.md` section 6.1 ("Scoring, black-box") and its row EV-14 in section 21; `audit/eval.md` F3 and F9. **Size:** M. **Reviewers:** one. **Depends on:** EV-14a. **Part of:** EV-14 (a, then b, then c and d at once).

## Why

v1's `Measure` (`internal/update/accuracy/measure.go`) finds a fact by its codename in one section and checks its tag. It passes "Cache entries for 7 minutes, KITE-5" where the user said 5, an item that drops the date of FROST-9, and any number of items nobody said. The plan's gate for EV-14 is to catch exactly those. The scorer sees only what a store holds, never the code that wrote it, so v1 and v2 are judged by one rule.

## What to build

Two new packages that no release contains. Neither non-test file imports `internal/update`, `internal/threadpage` or anything that organizes memory.

1. **`internal/eval/score`.** Its input is `Export`, a list of `Item`: `Kind` (the kinds of EV-14a's `Fact.Kinds`, plus `summary`), `State` (`active`, `retired` or `log`), `Authority` (the tag: `stated`, `accepted`, `observed`, `suggested`, `external`, `echo`, or v1's `inferred`), `Scope` (empty when the store has none), `Text`, `Date` (a dated line's day) and `Sources` (session id or its 8-character prefix, and seq). v2's export will fill the same type.
2. **`Score(scenario.Scenario, Export) Result`.** Items are tied to facts only through their sources, with `scenario.Locate`; codenames are slots like any other value. For each fact it gives one outcome from a closed list: `holds` (every slot, a permitted kind, the right authority, scope and validity), `found` (every slot, something else wrong, named), `missing`, `value_wrong`, `value_dropped`, `date_dropped`, `cond_dropped`, `limit_dropped`, `stale_in_force` or `violation`. Each item gets one too: `supports`, `noise` (it cites only lines that plant nothing), `unsupported` (it cites nothing), or the fact outcome it caused. Slots are matched by class with the scorer's own small normalizers: `3 March 2026` and `2026-03-03` are one date, `5 min` and `5 minutes` one quantity.
3. **Metrics**, in `Result.Metrics`, and `Rows(template results.Row) []results.Row`, unit `ratio` or `count`: `fidelity.fact_recall` and `fidelity.fact_holds` (shares of facts that must be stored), `fidelity.wrong_write` (active items with a wrong or dropped slot, or unsupported), `fidelity.noise`, `fidelity.misattributed` (a user tag on an item whose every source turn is someone else's words), `fidelity.stale_in_force`, `fidelity.dupes`, `fidelity.otr_stored` and `fidelity.violations` (an item that breaks a fact's `Must`). Log items never count as stale or duplicate. Summaries are not scored here.
4. **The seeded set.** `Ideal(scenario.Scenario) Export` builds the store a perfect organizer would write, and `Distort(Export, scenario.Scenario, Distortion)` applies one of: `value_wrong`, `date_dropped`, `cond_dropped`, `invented`, `wrong_voice`, `stale_kept` and `moved` (a correct item filed in another permitted kind). EV-14c's tests use both.
5. **`internal/eval/v1page`.** `ReadThread(body string)` and `ReadRules(body string)` read a v1 thread or rules page, as `threadpage.Render` and `RenderRules` write them, frontmatter tolerated, with their own small parser, into an `Export`: items under "Decisions in force", "Next" and "Open questions" are active items of kind `decision`, `next` and `open`; Done items are retired `next`; Timeline lines are `log` items dated by their group; refs `s:<sid8>#<line>` become sources; a rule is an active `rule`, a retired rule a retired one. A line it cannot read is an error that names its line number, never its text.

## Owns and may touch

`internal/eval/score/**` and `internal/eval/v1page/**` (both new), with their test data.

## Must not touch

Everything else. `internal/eval/scenario` (EV-14a's), `internal/eval/results`, `internal/update/**` and `internal/threadpage/**` are read, never edited. Tests may import `internal/threadpage` and `internal/update/accuracy`; non-test files may not, and `TestOnlyItImportsTheSuite` passes unchanged.

## Accepted when

- `scripts/linux-check.sh` passes, and `go list -deps ./cmd/eva-md | grep -c internal/eval` prints 0.
- A test parses the imports of every non-test file of both packages and finds none of the forbidden ones.
- For each of v1's 12 ported scenarios, a test builds v1's ideal page as a `threadpage.Page`, renders it, reads it with `v1page` and scores it: `fact_holds` is 1 and every count is 0, and v1's `Measure` on the same page is `Perfect()`.
- **The plan's gate**, by test, on v1's port: a page with a wrong value (the correction's or the cache's), one with FROST-9's date dropped, and one with an added item (citing nothing, and again citing the filler) each still give `Perfect()` from v1's `Measure`, and the scorer counts each as one `wrong_write` or `noise` with the named outcome.
- Every distortion of the seeded set is scored right on every port scenario it applies to, and `moved` on a test scenario with a fact of two kinds leaves `fact_holds` at 1. The test logs how many of the seven v1's `Measure` scores right (expected 3) and how many the scorer does (7).
- `Rows` gives one row for each metric, accepted by `results.Store` with `Forbid` set to `scenario.Forbid`, and no row field holds a planted word, by test.
- A property test builds pages of made-up items in every section, with every tag, refs and Timeline groups, renders them with `threadpage.Render` and `RenderRules`, and finds each item back from `v1page` with its kind, state, tag, sources and date. A page with an unreadable line returns an error without its text, by test.
