# The redaction corpus

Labelled texts for the redaction tests of `internal/redact`: `TestRates`, which
writes `../rates.txt`, and `TestOracle`, which holds the current rules to the
frozen baseline. Every value in it is made up. Redaction is for credentials
only (decision 32 of the plan), so a positive here is a credential, never a
personal detail.

## Files

Every `*.txt` file in this folder is read; this README is not. A file's lines
before its first case are comments.

| File | What it holds |
| --- | --- |
| `providers.txt` | each provider token format the rules know, as `secret` |
| `structured.txt` | private key blocks, JWTs, URL passwords, Authorization values, and a secret key's value in each shape: shell, YAML, JSON, TOML, URL query, connection string |
| `commands.txt` | command-line passwords, a password in prose, random tokens near a trigger word |
| `misses.txt` | the shapes the audit found kept (F2, redact-m2), as `secret`: recorded as misses, not fixed |
| `oracle_gaps.txt` | the shapes `TestOracle` found where the current rules alone keep a secret the baseline rules remove, as `secret` of a class whose name starts with `oracle_gap_`: recorded, not fixed (see below) |
| `negatives.txt` | code, prose, docs, chat-like lines written by hand, SHAs, UUIDs, `op://` and `$VAR` references |
| `facts.txt` | facts about authentication written as `key: value`, the T3 row of the ledger design's section 10.3 and more like them |

## A case

A case starts with a header line and runs to the next header or the end of
the file:

```
=== <label> <class> <name>
<text, one or more lines>
```

- `label` is `secret`, `negative` or `fact`.
- `class` is one word. The rates give recall for each class of `secret` cases
  and, for each class of `negative` cases, the share of cases a rule set
  changed. `fact` cases give `fact_loss_rate`.
- `name` is the rest of the line, and says what the case is.

The text is the lines up to the next header, joined with a newline; blank
lines at its end are dropped.

## Marks in a case's text

- `'⟦` and `⟧'` hold a secret: a `secret` case has at least one, and the
  others none. The quotes are part of the mark, not of the text: they stop a
  rule from reading the file itself as an Authorization value or a URL's
  password.
- `⟪` and `⟫` hold a fact's value: a `fact` case has at least one, and the
  others none. A fact is lost when the redacted text no longer holds its value
  whole.
- `¦` splits a text where it stands, anywhere in a case, and is removed when
  the case is read. Every secret is split at least once inside it, with part
  of the secret on each side of the split (a split at its first or last
  character does not count), so no file holds a whole secret: the loader
  refuses a secret that is not, and a test runs the current rules over each
  raw file and finds no finding they are certain of. Split a private key's BEGIN line, a provider
  token's prefix, or anything else a rule would take whole from the raw file.

No mark nests in another, and none may appear in a case but as a mark.

A secret is kept when the redacted text holds any 6 consecutive characters of
it. So a secret is at least 6 characters of printable ASCII and newlines, and
no 6 consecutive characters of it appear anywhere else in its case or in a
marker such as `[REDACTED:github_token]`; that is why a fine-grained GitHub
token's secret starts after its prefix. The loader checks each of these
rules.

## Gap classes

`TestOracle` holds the current rules alone to the frozen baseline: a secret
the baseline rules remove must not be kept. A class whose name starts with
`oracle_gap_`, in `oracle_gaps.txt`, or `gen_oracle_gap_` from a generator,
records a shape in which the oracle found the current rules alone keeping
such secrets. No rule is changed for it here, and the measure is not
loosened: its violations are counted apart, as `oracle_gap_violations` in
`../rates.txt`, every other class is still held to none, and `TestOracle`
fails when a gap class has no violation left, so the rule change that closes
it (LG-26) moves its cases to the class of their shape. While a gap class
exists, the switch `redaction_current_rules_only` keeps secrets that v1
removes, and stays off.
