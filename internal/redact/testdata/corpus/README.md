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
  the case is read. Every secret is split at least once, so no file holds a
  whole secret: a test runs the current rules over each raw file and finds no
  finding they are certain of. Split a private key's BEGIN line, a provider
  token's prefix, or anything else a rule would take whole from the raw file.

No mark nests in another, and none may appear in a case but as a mark.

A secret is kept when the redacted text holds any 6 consecutive characters of
it. So a secret is at least 6 characters of printable ASCII and newlines, and
no 6 consecutive characters of it appear anywhere else in its case or in a
marker such as `[REDACTED:github_token]`; that is why a fine-grained GitHub
token's secret starts after its prefix. The loader checks each of these
rules.
