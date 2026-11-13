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
| `oracle_near.txt` | for each gap class, secrets just outside its shape, which the current rules remove, as `secret` of a class whose name starts with `oracle_near_`: held to the baseline (see below) |
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
records a shape in which the current rules alone keep such secrets: a shape
of code that the current rules keep on purpose (the exceptions `plainCode`
in `redact.go` lists, and the comparison `floorValue` leaves alone) and the
baseline rules remove. A secret can take any of those shapes, so each is a
gap. The classes are `regex_value`, `yaml_alias`, `name_index` and
`type_annotation`; `oracleGapShapes` in `oracle_test.go` names them, with an
example of each exception, and `TestOracleGapShapesAreWhatPlainCodeKeeps`
checks that the current rules alone keep each example and the baseline rules
remove it. No rule is changed for them here, and the measure is not
loosened: their violations are counted apart, as `oracle_gap_violations` in
`../rates.txt`, every other class is still held to none, and `TestOracle`
fails when a gap class has no violation left, so the rule change that closes
one (LG-26) moves its cases to the class of their shape.

A gap class excuses its whole shape, so each has a near class, held to the
baseline like any class that is no gap: `oracle_near_<shape>` in
`oracle_near.txt` and `gen_oracle_near_<shape>` from a generator. Its
secrets stand just past the bounds of the exceptions that make the shape,
where the current rules remove them today. A bound is one condition of
`floorValue` or `plainCode` in `redact.go`, or of a predicate they call,
that limits an exception: a character class, a length, a count or a
refusal. `oracleBounds` in `oracle_bounds_test.go` lists each one found, one
line each, with an example, a generator of near cases on its far side and a
change of `redact.go` that widens the exception past it alone; its comment
lists the conditions that are no bound a secret can cross, and why. Each
case of `oracle_near.txt` is a labelled near case of one bound, named by the
bound's id and a colon. `TestOracleBoundsHaveNearCases` fails when a bound
has no near case, labelled or generated, or a near case names no bound;
`TestOracleBoundsWidenRedactGo` fails when a widening no longer applies to
`redact.go`; and `TestOracleHoldsEachBound`, run by hand with
`go test ./internal/redact -run '^TestOracleHoldsEachBound$' -args -widen-bounds`,
widens each bound in turn with `go test -overlay` and checks that
`TestOracle` fails on that bound's own near cases, labelled and generated.
Run it after any change to an exception for code. `TestOracle` fails when a
gap shape has no near class or the baseline rules keep a near secret, which
the oracle would then not judge. A near class does not use the spaced `==`
separator: after a comparison the floor takes nothing, so every value there
is in the gap shape.

The oracle holds the bounds that `oracleBounds` lists, against the
widenings it names and others like them, on the near cases it draws. Each
near case's key is written as code and configuration write one: bare, in
double or single quotes or with a tab before its separator, never before
`==`, at the start of a line, indented, or after `{` or `, ` inside one. A far side is sampled,
not exhausted: where it is a set of characters or operators, the generator
draws each mark that can stand there in turn, at the start, inside or at
the end of a name where that matters, and an operator of a type with a
blank on both sides, on one side and on neither, but with a few
continuations and values only. Where it is a set of names, as a variable's
prefix, a scope after `$` or a reference's scheme, it draws each word of a
vocabulary of the names code and configuration give scopes and stores
(`codeWords`) in turn, and random names. Each bound has 200 generated near
cases at least (`TestOracleBoundsHaveNearCases` checks the count, so a
shape that gains bounds needs a larger count in `genCounts`), and a bound
whose far side is a vocabulary takes more turns of its generator (its
weight). A widening of an exception in a way no near case stands for, as in
a context the generator does not write, is held only once it is listed
there with near cases of its own. Each review of the oracle has found such
widenings, and each is recorded as a widening of its bound, with a labelled
near case it keeps.

A widening to one more name that is on no list, a prefix, a scope or a
scheme, is not required to fail `TestOracle`: random names cannot be relied
on to draw one exact name, and a name that shares 6 characters with a key
or a marker, such as `secrets`, can stand in no case (see "Marks in a
case's text"). Its guard is the tripwire, `TestOracleBoundsWidenRedactGo`:
it holds a fingerprint of every top-level declaration of every Go file of
`internal/redact` that is not a test file, each function, method, var,
const, type and init, with its file, and of each file's package clause,
imports and `//go:` directives (`exceptionSource` in
`oracle_tripwire_test.go`). A declaration changed in any token, added,
removed or moved to another file fails it, and so does a new file, until a
person writes the new fingerprint there; comments and layout do not count.
It does not hold `internal/redact/baseline`, another package, which is
frozen by rule, nor the code outside the package that reads the switch. It
reads the files on disk, so a file changed with `go test -overlay` does not
reach it. `TestOracleTripwireHoldsEveryDeclaration` checks that it reads
every file of the folder but the tests and has a line for every
declaration in them, and `TestOracleTripwireSeesEveryWidening` applies
each widening `oracleBounds` lists, named ones too, and checks that the
fingerprint changes. Whoever writes a new fingerprint first adds, for what
the change lets through, a near case labelled here and near cases its
bound's generator draws, and the change as a widening of its bound; a new
condition is a new bound. The switch stays off until the oracle holds the
new shape.

The switch `redaction_current_rules_only` keeps every secret in such a
shape, which v1 removes. It stays off until a rule change has closed each of
those shapes and the oracle holds the rules to each: not merely until the
recorded gap classes are gone, since a new exception of the current rules
for code is a new gap, which the oracle sees only once its shape is in the
corpus and the generators.
