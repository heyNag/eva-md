# IF-01a: the contract matrix and its check

**Plan:** section 8, Stage 1 row IF-01, Stage 0's line "IF-01 waits for SEAM-0", and Stage 2's first paragraph (a need on another track's Stage 2 package means its Stage 1 interface and fake); section 9 ("each track's interface lands in Stage 1"); section 15.2. **Design:** `critique-delivery.md` issues 1, 5 and 14 and "Gaps no design covers" 2; the contract section of each of the six designs (`ledger.md` 16, `store.md` 12, `derive.md` 14, `deliver.md` 19, `hosts.md` 17, `evaluate.md` 17). **Size:** M. **Reviewers:** one. **Depends on:** SEAM-0a, c and d, LG-10 and ST-01 (all merged). **Part of:** IF-01, in three parts: this one and IF-01b (the reader's fake) at once, then IF-01c (the store's and the retrieval core's fakes). The runner's fake waits for DV-11.

## Why

The six designs list 83 needs one area has of another. The delivery critique found many with no package to provide them. Stage 2's tracks can build at once only if every need has a provider and every interface has a fake. A missing provider must fail a check, not surface in a build. Nothing v1 does changes, so there is no switch.

## What to build

1. **The matrix**, `docs/v2/contract-matrix.md`, at most 24 KB, no line over 400 characters, with two tables.
   - **Needs.** One row for each row whose direction contains `need` in a design's section headed `## <n>. Contracts`.
     - Columns: `Need` (the design's id), `Design` (its file name), `Consumer` (plan ids), `Provider` and `Where`.
     - A provider is one or more plan ids, or one word: `v1` (kept v1 code meets it), `later` (a section 12 item), `dropped` (the plan ruled it out) or `open` (the lead must rule).
     - `Where` gives the Go name or file that meets the need, the plan section for `later` or `dropped`, or the question for `open`.
     - Consumers come from the design's package rows that cite the need.
     - A design id that the plan folded into another row is written as that row's id: LG-03 as HF-00, EV-13 as EV-14, HO-25 as HO-13, and ST-13 and DV-26 as CUT-4.
     - A need that the plan leaves with no provider, or whose provider comes in a later stage than its consumer, is written `open`, with its question. The matrix then passes, and only a card that consumes the need fails.
     - The one-line text is not copied.
   - **Interfaces and fakes.** Columns: interface, Go name, provider and fake.
     - The reader: `ledger.Reader`, LG-10, `waits: IF-01b`.
     - The store: `store.Store` in `internal/store/api.go`, ST-01 and ST-02, `waits: IF-01c`.
     - The retrieval core: `retrieval.System`, DL-04, `waits: IF-01c`.
     - The runner: DV-11, `waits: DV-11`.
     - ST-05, LG-22 and LG-23, which Stage 2's paragraph names: the fake that stands in for each, or `open`.
2. **The check**, package `internal/cardcheck`: `Check(fsys fs.FS) []Problem` over the repository root. Each `Problem` names the file, the row or card, and the rule. Nothing imports the package. Its test runs `Check` on the repository.
   - **Plan ids** come from the first cell of each table row in section 8, from `### Stage 0` to `### What runs in parallel`. A cell may list several ids. An id's stage is the heading it sits under; the cut-over counts as Stage 2.
   - **A card's packages** are its file name's id, less a trailing lowercase part, if the plan lists it, plus every plan id in brackets in its title line. So `DV-12a` is DV-12, and `CT-1` is LG-01 and DL-02. Every Markdown file in `docs/v2/cards/` except `README.md` is a card; a hidden file is skipped, and any other file fails rule d.
   - **Rules:**
     - a. every design need row is in the matrix exactly once, and every matrix row has its design row;
     - b. every Consumer and Provider id is a plan id, and a provider is ids or one of the four words;
     - c. no provider is in a later stage than any of its consumers;
     - d. every card names at least one plan package;
     - e. every id in a card's `**Depends on:**` label is a plan id or a card;
     - f. a card fails when one of its packages is a consumer of a row whose provider is `open`, `later` or `dropped`, and the problem names the need;
     - g. every fake is an existing folder, or reads `waits:` and a plan id or a card; the fake of ST-05, LG-22 or LG-23 may be `open`, and every interface listed above has its row.

## Owns and may touch

`docs/v2/contract-matrix.md` (new) and `internal/cardcheck/**` (new).

```paths
docs/v2/contract-matrix.md
internal/cardcheck/**
```

## Must not touch

Everything else. The plan, the designs, the cards and their README are the lead's. If a merged card fails the check, report the card and the row, and do not edit the card.

## Accepted when

- `scripts/linux-check.sh` passes, and `Check` finds no problem on the repository.
- For each rule a to g, a test runs `Check` on an `fstest.MapFS` copy that breaks that rule once. `Check` names the rule and the row or card.
- The parsers find at least 80 need rows, 85 plan ids and every card file, so a parser that reads nothing fails.
- The matrix's size and line limits hold, by test.
- A test fails if a non-test file outside `internal/cardcheck` imports it, and `go list -deps ./cmd/eva-md` does not list it.
- The report lists every `open` row with its question.

## As merged

Merged from `pkg/if-01a-r1` on 2026-11-12, after one review, a fix round and a verification that accepted it. Beyond the list above, the check as merged reads no table, title or label that stands inside a code block or a quote; reads a design's contracts table only under the header Id, With, Direction, and the matrix's two tables only under their exact headers; reports a need a design lists twice; and takes a `Depends on` label to the next bold label or the end of its paragraph. Its rule tests no longer lean on the words of any merged card, design or matrix row, except the matrix's two headers and three rows they find by id, so the lead can write a ruling into an open row and need change nothing else.

The matrix merged with eleven needs open, each with its question, and with LG-22's fake open. The lead's rulings on all twelve are written into the matrix in a commit of their own after this one, with a record of each beside it, `docs/v2/contract-matrix-rulings.md`; the plan takes their words at the Stage 0 readout.

Three corner cases of Markdown are left as they are, since no card or design writes them today: a closing fence indented four spaces or more, an indented code block straight after a heading, and a code span of two backticks that holds one.
