# Redaction

- **Component:** Redactor
- **Paths:** `internal/redact`
- **Status:** v1 as built, frozen on 2026-11-14

The blocks below are v1's design and verification text, moved here word for word when the two documents were split; [INDEX.md](INDEX.md) lists every spec.

<!-- v1 docs/verification.md 002 -->
- **Redaction** (`internal/redact`).
  Each secret format, key=value lines in dotenv, YAML, INI and properties files, values with symbols, several secrets in one URL query, form body or connection string, and code shapes that must be kept.
  `redact.Redact` removes the union of what the baseline rules (`internal/redact/baseline`, frozen) and the current rules remove.
  Generated-corpus tests check that no generated password the baseline rules remove survives `Redact`, in 16 line shapes and thousands of random passwords.
  Other tests check that redaction is idempotent on random input, that a SHA or UUID is taken as the value of a secret-looking key but kept in ordinary text, and that identifiers such as branch names and project keys are cleaned without merging two of them, and stay the same when cleaned again.
  The shapes `redact.Version` 5 added: keys ending in `PASS` or `PWD` in env, JSON, YAML, dotted and camelCase forms, after a double underscore and with a quoted value that holds spaces, `pass` and `pwd` alone in a URL query, a connection string, a long option and an object literal, a MySQL or MariaDB
  client's `-p<password>` and curl's `user:password` in quotes, clusters, containers, substitutions, after `pymysql` or `libcurl` and over lines continued with a backslash, `sshpass -p` after its own options, `docker login` and `podman login`'s `-p` and `--password`, `redis-cli`'s `-a` and `--pass`
  and openssl's `pass:` form, quoted too, every password option of a command, the first too, after quoted strings that hold `;`, `|` or `&`, after a redirection such as `2>&1` or an unquoted URL query's `&`, and in inline code, a password quoted after curl's user, a password in prose, and a value of
  digits and one letter, or one that joins a word to a number and a unit's letters, as `Hunter_42ms`, in prose and after a `pass` key, a command inside another of the same name, in quotes, in quotes inside quotes, in `$(…)`, past an sshpass's options or after a `&` inside a word, a quoted command
  followed by another whose password is quoted, with the text after a quoted command's password kept, quotes inside `"$(…)"`, a bearer token a command fetches, `&&`, `;` and `|` inside a `$(…)` before the option, an apostrophe, bash's `$'…'`, a backtick substitution, the option given twice or after an
  option that takes a value, and a quoted `PASS` value that runs on into the next key, each replaced exactly (`TestPasswordShapes`); 12 provider tokens with a fixed prefix in text, by `RedactKnown` and in identifiers (`TestTokenPrefixes`); 137 lines kept as they are, among them `mkdir -p`,
  `git add -p`, `cp -p`, `ssh -p 22`, `mysql -p` alone, `docker run -p 8080:80`, `docker login --password-stdin`, an `ssh -p` that `sshpass -f` runs, `redis-cli -p 6379`, openssl's `env:` and `file:` forms, another command's options after `&&`, `&`, `||`, a pipe or a closing `)`, with or without
  blanks, SQL in quotes, a value after an operator, as `-a <password>`, code between two strings, variables, "the password is in the vault", "a pass is required", technical terms such as `base64-encoded`, measurements after a render pass such as `16.6ms` or `100MB/s`, test results such as
  `--- PASS: TestParseV2`, `PWD=` paths and code (`TestPasswordShapesKept`); branch names and project keys with these words left whole (`TestPasswordShapesKeepIdentifiers`); idempotence, stable identifiers and whole markers on random mixes of their words (`TestPasswordShapesIdempotentOnRandomInput`);
  a token that names curl's user replaced under its own kind, with no current rule cutting a marker in two (`TestPasswordShapesKeepMarkersWhole`); the command rules handing their patterns, and reading as commands, at most seven times the text's length in their two readings, however many command names
  one long line holds, as a minified API spec does, and however commands of one name sit inside each other, inside two kinds of quotes and inside backticks (`TestPasswordCommandsReadEachCommandOnce`); the two readings' overlapping spans joined into one (`TestMergeOverlapsJoinsSpans`); each gate
  holding wherever its rule's pattern matches (`TestPasswordGatesCoverTheirPatterns`); and no replacement taking a newline (`TestPasswordShapesKeepLines`).

<!-- /v1 -->

## Must keep

The must-keep items of [findings.md](../v2/findings.md) this component touches, numbered from K01 in each list, and the tests that hold them.

| Id | Item | Tests |
| --- | --- | --- |
| CUP-K14 | Recovery redacts with the union of the baseline and current rules | internal/catchup:TestBoundedRedactMatchesFullRedaction, internal/catchup:TestRedactorCleaningAndKeyMarkerReplicas |
| RED-K01 | Text is redacted before any byte reaches disk and before it is cut, so a token across a cut leaks no prefix | internal/inbox:TestRedactionTruncationAndFailClosed, internal/hook:TestToolLines, internal/catchup:TestBoundedRedactMatchesFullRedaction |
| RED-K02 | A redactor panic stores no text, or a gap in recovery, never unredacted text | internal/inbox:TestRedactionTruncationAndFailClosed, internal/catchup:TestRedactorPanicLeavesGap |
| RED-K03 | A rule change never keeps a secret an earlier rule set removed | internal/redact:TestNoSecretVersion1Removed, internal/redact:TestFloorKeepsVersion1Redactions, internal/redact:TestScanMatchesRedactV1, internal/redact:TestOracle |
| RED-K04 | Redacting redacted text changes nothing | internal/redact:TestIdempotent, internal/redact:TestValuesIdempotentOnRandomInput, internal/redact:TestPasswordShapesIdempotentOnRandomInput |
| RED-K05 | Only a whole marker counts as earlier redaction, and no span starts or ends inside one | internal/redact:TestValueMarkersAreOpaque, internal/redact:TestPasswordShapesKeepMarkersWhole, internal/redact:TestSpanSearchMatchesScan, internal/redact:TestUntracedKeepsRulesText |
| RED-K06 | Invisible and control characters are cleaned before matching, so a split secret is caught | internal/redact:TestCleanAndSplitSecrets, internal/catchup:TestRedactorCleaningAndKeyMarkerReplicas |
| RED-K07 | SHAs and UUIDs are kept in plain text but not as a secret's value; op:// references are kept | internal/redact:TestSecretKeyKeepsNoSHAOrUUID, internal/redact:TestNegatives, internal/redact:TestPositives |
| RED-K08 | Identity fields take format-only rules, keep word-like names, and stay distinct and stable | internal/redact:TestIdentifier, internal/redact:TestIdentifierKeepsNamesAndSeparatesSecrets, internal/redact:TestIdentifierStableAfterHashCut, internal/redact:TestRedactKnownKeepsIdentifiers, internal/inbox:TestBranchNamesStayDistinct |
| RED-K09 | No replacement spans a newline but a private key block | internal/redact:TestPasswordShapesKeepLines, internal/catchup:TestBoundedRedactMatchesFullRedaction |
| RED-K10 | Redaction's cost grows linearly with the text | internal/redact:TestPasswordCommandsReadEachCommandOnce |
| RED-K11 | The cases of code to keep and passwords to remove are kept as the acceptance corpus | internal/redact:TestValueCodeShapesKept, internal/redact:TestFloorKeepsCode, internal/redact:TestPasswordShapes, internal/redact:TestPasswordShapesKept, internal/redact:TestCorpusReads |
| RED-K12 | Logs and entries record how many replacements, never what was replaced | internal/inbox:TestAppendRedactedKeepsTextAndChecksMetadata, internal/redact:TestCount, internal/hook:TestRecordsHoldNoContent |
| RED-K13 | Page writers and the commit's scan share one line judge | internal/wikigit:TestSecretLines, internal/wikigit:TestSecretLine, internal/update:TestKnowledgeSecretScan, internal/systempage:TestIdentifiersPassSecretsDoNot |

## Since the split

No change yet.
