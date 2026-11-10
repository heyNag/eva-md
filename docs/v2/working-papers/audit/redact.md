# Redaction audit: the current rules, the frozen baseline and the union

Scope: `internal/redact/redact.go` (1,898 lines), `internal/redact/passwords.go` (941), `internal/redact/union.go` (183), `internal/redact/baseline/baseline.go` (500, frozen by project rule), their tests (1,623 lines), the callers, `docs/design.md` line 89 (Redactor row) and lines 421-488 (The inbox), and the "Redaction" section of `docs/known-limitations.md` (lines 5-16).

Method. I read every non-test line of the four files and the call sites. To check behaviour rather than trust comments, I copied the four non-test files verbatim into a scratch Go module outside the repo (only the baseline import path changed) and ran them on made-up inputs. Nothing in the repo was edited, and no repo test or restricted command was run. Where a claim rests on such a run it says "ran a copy". Timings are from this audit's Linux VM, so read them as ratios and orders of magnitude, not as Mac figures.

---

## 1. How it works

### 1.1 Entry points

| Entry point | What it does | Where |
| --- | --- | --- |
| `redact.Redact(text)` | Cleans, replaces private key blocks, then up to four rounds of "baseline rules ∪ current rules" | `union.go:22-38` |
| `redact.RedactKnown(text)` | Current rules only, without the entropy rule and without the baseline; for identity fields | `redact.go:481` |
| `redact.Identifier(s, limit)` | `RedactKnown` in "names" mode (a provider format that reads as a name is kept), control characters dropped, cut to `limit` runes, and an 8-hex SHA-256 prefix added when anything was replaced | `redact.go:507-552` |
| `baseline.Redact(text)` | The original rule set, version 1, frozen | `baseline/baseline.go:198-210` |

`redact.Version` is 5 (`redact.go:18`), `baseline.Version` is 1 (`baseline.go:18`). Neither constant is read anywhere at run time (grep over non-test Go finds no use).

### 1.2 Data flow inside `Redact`

1. `clean` (`redact.go:599-614`): drops NUL, every Unicode `Cf` character (bidi controls, zero-width characters, tag characters, ZWJ, ZWNJ, soft hyphen) and U+034F, then ANSI escape sequences. Invalid UTF-8 becomes U+FFFD.
2. `privateKeys` (`union.go:53-67`): replaces PEM private key blocks first, with one regex shared by both rule sets. The regex ends at the END line or at the end of the text (`|\z`).
3. `unionRound` (`union.go:40-51`), at most 4 times: run `baseline.Redact` and the current `redact(text, entropy)` on the same text, then `union` their outputs.
4. Neither engine exposes spans, so `union` recovers them by lining each output up with the input: `removals` (`union.go:122-183`) splits an output at its marker runs, finds each kept piece in the base with `strings.Index` and `strings.LastIndex`, and where a piece could sit in more than one place it counts more text as removed, "never less" (`union.go:116-121`). If line-up fails, the fallback redacts the baseline's output again with the current rules (`union.go:46-50`).
5. Each engine loops its rule list until a pass replaces nothing, at most 16 passes (`redact.go:27, 483-495`; `baseline.go:26, 198-210`). So text with no secret costs 1 round x (1 + 1) passes; text with any secret costs at least 2 rounds x (2 + 2) passes.

### 1.3 The current rules (`redact.go:117-415`)

- 21 provider formats and service URLs (`redact.go:123-250`): Anthropic, OpenAI, 1Password, GitHub, GitLab, Slack token and webhook, Telegram, Discord, AWS id and secret, Google `AIza`, Stripe, Hugging Face, npm, PyPI, Vault, DigitalOcean, SendGrid, Shopify, Docker.
- JWT with a decoded-header check (`redact.go:251-256, 1879-1898`), URL userinfo passwords (`257-263`), Authorization headers and bare Bearer (`264-272, 287-297`).
- Generic `key = value`: a key that contains password, passwd, passphrase, secret, private_key, token, api_key, apikey or access_key (`redact.go:97-100`), with quoted values (`273-286`) and unquoted values in two "families" (`298-315`) decided by `secretValue` (`833-866`): at a line start the value runs to the end of the line (`lineValue`, `1195-1227`), inside a line to the end of the token (`inlineEnd`, `1357-1399`); code shapes are kept by about 600 lines of heuristics (`isCode` 1490-1536, `plainCode` 964-1004, `wordLike` 1099-1146, `passphrase` 1275-1290, `typeHead`, `codeHead`, and so on).
- Two "floor" rules (`redact.go:316-334`, `floorValue` 911-945) that re-implement, inside the current rules, what the baseline's generic value rules take.
- Password shapes added in version 5 (`passwords.go`): keys ending in PASS or PWD (`passKey`, 621-652), six command families read by a hand-written shell word reader that reads each command twice with two quote interpretations (`commandMatches` 176-252, `commandWords` 282-361), and "password is X" prose (`prosePassword` 99, `proseValue` 820-845, `passwordLike` 875-900).
- The entropy rule (`redact.go:1699-1724`): a token of 20+ characters with Shannon entropy of 3.5 bits per character or more, within 5 words and 256 bytes after a trigger word (`redact.go:30-35, 431`).

Each rule is gated by lower-case substring hints or a gate function (`redact.go:616-632`).

### 1.4 The baseline (`baseline/baseline.go`)

A private key rule, 11 provider format rules, JWT, URL credentials, Authorization, quoted and unquoted generic values, and the same entropy rule (`baseline.go:58-177, 345-370`). Its generic value rule takes any run of 8 or more characters after a secret-looking key, up to whitespace, a quote or one of `, ; & < > ( ) { } [ ]` (`baseline.go:49, 161-176`); the only values it spares are a SHA, a UUID, an `op://` reference and a `$VAR`-style reference (`baseline.go:454-469`), or a value followed by `(`.

### 1.5 Caps, budgets and limits

| Limit | Value | Where |
| --- | --- | --- |
| Passes per engine | 16 | `redact.go:27`, `baseline.go:26` |
| Union rounds | 4 | `union.go:30` |
| Shortest generic value | 8 characters | `redact.go:39` |
| Entropy token, bits, window | 20 chars, 3.5 bits, 5 words / 256 bytes | `redact.go:30-35` |
| Code look-ahead, floor scan, rescan allowance | 512 bytes, 64 KiB, text length + 64 KiB | `redact.go:40-41, 719` |
| Marker kind | `[a-z_]{1,32}` | `redact.go:425` |
| Time limit inside the redactor | none; it takes no context and cannot be interrupted | `union.go:22`, `catchup.go:909-911` |

Limits the callers add: the hook budget is 300 ms, or 1.5 s for SessionStart and tool events (`hook.go:229-235`), with payloads up to 2 MiB (`hook.go:30`); the inbox cuts text over 200 KiB from the middle after redaction (`inbox.go:24, 122-130`); transcript recovery redacts in three shrinking head-and-tail window levels, 200, 48 and 16 KiB, and stores a gap when all are outrun (`catchup.go:879-883, 912-931, 967-1016`); the commit scan refuses to judge a line over 1 MiB (`wikigit/scan.go:13-15`).

### 1.6 Triggers and what is written where

- Capture, every hook event: `inbox.Store.Append` redacts `Text` with `Redact` before the entry is marshalled and appended to `~/.local/state/eva-md/inbox/<project>/<host>-<session>.jsonl` (`inbox.go:77, 89`); a redactor panic stores the entry with empty text (`inbox.go:89-92, 247-256`). Host, session and project must pass `RedactKnown` unchanged or the whole entry is refused (`inbox.go:96-101`). Branch goes through `Identifier` (`inbox.go:107`); the other metadata fields through `Redact` (`inbox.go:114-121`).
- Tool lines: the command is redacted before it is cut to its first line and 200 characters (`hook.go:803-808`).
- Transcript recovery: `catchup.redactText = redact.Redact` under the bounded windows (`catchup.go:43, 612`).
- Read time: the brief and the update's input redact stored text again (`freetext.go:66-67`; `resume.go:605`; `update/input.go:654, 947`).
- Model output: every field of an update is redacted again (`update/validate.go:999-1008`, called at 110-111, 524-822; `update/knowledge.go:116, 178, 204`; `update/rules.go:129-171`).
- Wiki gates: `wikigit.SecretLines` / `secretLine` flags a line when `Redact` would replace anything, after x-ing out identifier-shaped words (`wikigit/scan.go:87-137`). It is used by the daily commit's staged-diff scan, which leaves a flagged file out of the commit (`wikigit/commit.go:216-220`), by system page writes (`systempage/page.go:152-187`), by the knowledge validator (`update/knowledge.go:404-409`), by `eva-md show` (`resume.go:622-637`) and by the per-prompt recall and pointer lines (`hook/recall.go:180`, `hook/pointers.go:404`).
- Identity: `identity.plainKey` and `coderef.remoteKey` switch a project to a hashed key when `RedactKnown` would change its readable key (`identity/identity.go:165-168`; `coderef/coderef.go:178-185`).

---

## 2. What must be kept

1. **Redact before any byte reaches disk, and before truncating.** `inbox.go:89` precedes marshal and write; the cut comes after (`inbox.go:122-130`; `hook.go:803-805`). A token that spans a cut can never leak a prefix.
2. **Fail closed.** A redactor panic stores no text (`inbox.go:89-92, 247-256`) or a gap in recovery (`catchup.go:936-942`). Capture never falls back to unredacted text.
3. **The monotonic property for true secrets.** A rule change must never keep a secret an earlier rule set removed (`union.go:17-21`; `floor_test.go:159-198`). The property is worth keeping; the mechanism (a runtime union with an imprecise frozen rule set) is what finding 1 questions.
4. **Idempotence.** Redacting redacted text changes nothing (`union.go:21`; `redact_test.go:219`; `values_test.go:231`; `passwords_test.go:460`). Read-time re-redaction, model-output scrubbing and stable thread keys all rest on it.
5. **Markers.** Only a complete `[REDACTED:<kind>]` counts as earlier redaction (`redact.go:20-22, 425`); no span may start or end inside a marker (`redact.go:675-680, 1451-1460`); a bare marker start left by a cut is not re-redacted (`redact.go:1148-1157`).
6. **Cleaning first.** Invisible characters and escape sequences are removed before matching, so a secret split by zero-width characters is caught and hidden text cannot ride into memory (`redact.go:597-614`; `redact_test.go:236`).
7. **Context-sensitive keeps.** SHAs and UUIDs survive in ordinary text but not as the value of a secret-looking key, a URL password or an Authorization header (`redact.go:845-854, 262, 271, 296`; `redact_test.go:167`). `op://` references always survive (`redact.go:429, 784-789, 1851-1854`).
8. **Identifier treatment.** Identity fields get format-only rules, names that read as words are kept (`nameish`, `redact.go:1811-1826`), and two identifiers that differ only in what was replaced stay distinct (`redact.go:541-550`; `known_test.go:95-158`).
9. **Line discipline.** No replacement spans a newline except a private key block (`passwords_test.go:540`). Recovery's windowed redaction depends on exactly this, with three named exceptions (`catchup.go:946-962`).
10. **Linear cost.** Bounded rescans (`redact.go:708-720`), bounded look-ahead (`redact.go:40-41`), each command read a bounded number of times (`passwords.go:162-175`; `passwords_test.go:590`). Go's regexp engine has no backtracking, so there is no pathological input.
11. **The code-shape corpus in the tests.** Hundreds of exact cases of code that must be kept and passwords that must go (`values_test.go:84-200`, `floor_test.go:110-134`, `passwords_test.go:18-433`). This is the hard-won asset; it should become the acceptance corpus of any new engine.
12. **Counts only in logs.** Callers record replacement counts, never content (`inbox.go:44, 89`).
13. **One judge for writers and the commit gate.** Writers ask the same function the commit scan uses, so a page is refused at write time rather than left out at commit time (`wikigit/scan.go:87-97`).

---

## 3. Findings, ranked by consequence

### F1. Non-secret facts are erased, and the frozen baseline makes that permanent (memory-quality, critical, partly known)

Evidence.
- The baseline's generic value rule takes any 8+ character run after a secret-looking key (`baseline.go:49, 161-176`); its only keeps are SHA, UUID, `op://` and `$VAR` (`baseline.go:454-469`). The entropy rule takes any 20+ character token with enough entropy within five words of a trigger word (`baseline.go:345-370, 404-415`).
- `Redact` keeps every baseline removal by construction (`union.go:40-44`), and the current rules carry two floor rules that re-create the same removals (`redact.go:316-334`). AGENTS.md forbids changing the baseline.
- The current rules add their own class: a key at a line start takes the rest of the line when it reads as a passphrase (`redact.go:1223-1226, 1275-1290`).
- Ran a copy. All of these are replaced, by the baseline and by the current rules alike: `token: optional`, `password: required`, `Decided: access_token=short-lived, refresh_token=rotating`, `token_endpoint: https://auth.example.com/oauth/token`, `TOKEN_ISSUER=https://auth.example.com`, `private_key_path: ~/.ssh/id_ed25519_deploy`, `api_key_header: X-Internal-Api-Key`, `secret_name: payments-stripe-webhook-signing`, `password_policy: zxcvbn-score-3`. By the entropy rule: `the token is validated by AuthenticationMiddlewareFactory`, `token := generateSecureRandomToken(32)`, `expect(response.body.token).toEqual(expectedTokenFromFixture)`, a docker digest after `token-service@sha256:`. By the current rules only: `Token: rotated on Friday`, `Password: stored in 1Password`, `Secret: none needed here`.

What the docs say. known-limitations lines 9 and 12 list the camelCase case, `Password: expired yesterday`, and that "every password-looking key" loses a value of 8 characters or more. They do not say that the loss can never be repaired while the union stands, nor how wide it is: endpoints, file paths, header names, policy names and decisions.

Impact. In any project that touches authentication, the facts a later chat most needs (which endpoint, which header, where the key lives, what was decided) are replaced in the inbox, again in the model's output, and in pages. No change to the current rules can bring them back.

Direction. Judge the value, not only the key name: a value classifier that separates random-looking strings from words, enumerations, URLs without userinfo, paths and identifiers. Keep "never weaker than the baseline" as a tested property over a frozen corpus of true secrets, with the baseline package as the oracle in tests, and release the baseline's false-positive classes explicitly. This needs the owner to amend the frozen-baseline rule; it cannot be done by patching the current rules. Action: rebuild.

### F2. Detection has verified holes that no rule covers (reliability-safety, high, not in the docs)

Evidence.
- The key vocabulary is nine words (`redact.go:97-100`); the entropy triggers add client_secret, credential and bearer, each with word boundaries (`redact.go:431`). The value pattern never crosses a line (`redact.go:87`). Values under 8 characters are kept (`redact.go:39`). The URL rule stops the password at the first `@` or `/` (`redact.go:259`). There are 21 provider formats (`redact.go:123-250`). No rule covers personal data.
- Ran a copy. Kept whole: `ENCRYPTION_KEY=<24 random>`, `RAILS_MASTER_KEY=<32 hex>`, `signing_key: <random>`, `KEY=<random>`, an Azure storage `AccountKey=<base64>`, TOML `[auth]` then `key = "<random>"`, `auth = <random>`, `"auth": "<base64 user:password>"` as in a Docker config, `credentials: <random>` (the trigger is `credential` with a word boundary), `Cookie: sessionid=<random>`, `Set-Cookie: connect.sid=...`, `psk="..."`, a `.netrc` line (`password <value>` with no separator), `echo '<password>' | sudo -S ...`, `--from-literal=pw=<password>`, a value on the line after `password: |` or after `"password":`, and bare tokens with no rule: a Groq-style `gsk_`, a Tailscale `tskey-auth-`, a Google OAuth `ya29.`, an `AGE-SECRET-KEY-1`. Partly leaked: `postgres://app:p@ssW0rd!x9@db.internal/app` becomes `postgres://app:[REDACTED:url_credentials]@ssW0rd!x9@db...`; with a `/` in the password nothing is replaced.

Impact. What gets through is stored in an inbox with no time limit, shown to the update model, and can reach a wiki that is pushed to a git host. The misses are ordinary shapes, not adversarial ones.

Direction. Value-first detection that does not depend on a key vocabulary: structured readers for dotenv, YAML and JSON (including next-line values), HTTP headers and cookies, and URL userinfo split at the last `@`; a provider table held as data so it can grow without code; and exact-match masking of secrets the machine already knows (environment, `.env` files, credential files), held as keyed hashes, which has no false positives. Action: rebuild.

### F3. A miss is permanent, and all three gates share one detector (reliability-safety, high, partly known)

Evidence.
- `redact.Version` is never read at run time (`redact.go:18`; grep). An inbox entry stores a count, not the rule version (`inbox.go:44`; design.md lines 431-446). The JSONL is never rewritten, and `inbox clear` removes whole sessions only (design.md line 429; known-limitations line 100).
- Read paths do redact again with the current rules (`freetext.go:66-67`; `update/input.go:654`), so a newer rule protects the brief and the update's input. The stores do not change: the inbox file, the search index built from it, and wiki pages already written. The commit scan judges only lines a staged diff adds (`wikigit/scan.go:30-37, 82-84`); nothing scans the pages already committed (grep finds no `SecretLines` use in `internal/lint`).
- The commit scan is `redact.Redact` again (`wikigit/scan.go:115, 136`). Capture, model-output scrubbing and the last gate before a push therefore fail on the same inputs.

Impact. A secret that slips past once is on disk for good, can be copied into a page by the model, and will pass the scan before the push, because it is the same rule set a third time. Improved rules never reach what is already stored.

Direction. Stamp the rule version on entries and pages. Add a maintenance step that re-scans the inbox and the whole wiki when the rules change and repairs what it finds (which means inbox lines must become replaceable by a tombstoned rewrite), and a "forget this value" purge across inbox, index and wiki. Make the pre-push gate different in kind, not a rerun: exact-match known secrets plus a context-free entropy check on added lines. Action: add.

### F4. The costliest code sits on the hook's hot path, uninterruptible and unmeasured (invisibility, high, partly known)

Evidence.
- `Append` calls `Redact` synchronously (`inbox.go:77, 89`) inside a 300 ms hook budget (`hook.go:229-235`), on payloads up to 2 MiB (`hook.go:30`). A capture that misses the deadline "delivers nothing" (`hook.go:202-219`). The redactor takes no context (`union.go:22`); recovery has to abandon a goroutine to bound it (`catchup.go:909-931`).
- Both engines run on every call (`union.go:41-42`), and any replacement forces more rounds and passes (section 1.2).
- The hints barely gate: the baseline's JWT rule runs on any text with a `.`, GitHub on `gh`, GitLab on `gl` (`baseline.go:82, 87, 123`; `redact.go:144, 150, 254`), and the generic rules on the substring `key` or `token` (`redact.go:277, 311`).
- Ran a copy (Linux VM). Reply-like Markdown with code that mentions tokens: 8 KiB 10 ms, 16 KiB 20 ms, 32 KiB 38 ms, 64 KiB 62-78 ms, 200 KiB 184 ms; the baseline alone is about half. A log-like 200 KiB paste: 290 ms. Secret-dense 48 KiB: 283 ms; 200 KiB: 1.3 s. Plain English, 64 KiB: 4 ms; the same size with the word "key" in each sentence: 67 ms.
- The latency benchmark measures capture with a 23-byte reply (`scripts/benchmark.py:152, 294`), so the 50 ms capture target says nothing about redaction.

What the docs say. known-limitations line 16 lists the doubled cost and its effect on recovered replies. It does not list the exposure of the hook itself or the blind benchmark.

Impact. Redaction is the dominant cost of capturing a realistic reply. Long prompts and replies can miss the hook, and are then recovered late and in head-and-tail windows, or become a gap. The targets that are meant to keep the product invisible are checked on inputs where this code does no work.

Direction. One linear scan: a keyword automaton to find candidates, per-line structure to judge them, spans as output. Make it cancellable and incremental, so a deadline yields a redacted prefix and an honest gap instead of nothing. Move baseline equivalence into tests so only one engine runs at run time. Benchmark p95 at 2 to 64 KiB of realistic text. Action: rebuild.

### F5. Three mechanisms guard one property, and every consumer works around the engine (architecture, high, not in the docs)

Evidence.
- The same guarantee is implemented three times: floor rules inside the current rules (`redact.go:316-334`, `floorValue` 911-945, `plainCode` 964-1004), the run-time union (`union.go:40-51`), and string re-alignment to recover spans neither engine returns (`union.go:122-183`), with a fallback for when alignment fails (`union.go:46-50`). A known artefact is a marker cut in two, `[REDACTED:[REDACTED:url_credentials]` (known-limitations line 14; reproduced by running a copy).
- Recovery copies the cleaning function and the private key regex because it needs positions (`catchup.go:1039-1072`), and encodes three exceptions to "rules match within a line" in a comment (`catchup.go:946-962`).
- Consumers each built a shim around the entropy rule's reading of identifiers: three entry points with modes (`redact.go:473-481, 507`), the commit scan's x-ing out of identifier-shaped words (`wikigit/scan.go:102-137`), `cleanPage` for `eva-md show` (`resume.go:613-637`), and hashed project keys (`identity/identity.go:156-168`).
- The engine is about 3,500 lines of regexes plus hand-written heuristics for what is code; the latest change added 941 lines for six commands (commit `51b241c`).

Impact. Each new shape costs a patch in several places, the layers interact (markers cut, wider removal on ambiguous line-up), and the callers' shims have their own failure modes (known-limitations lines 20 and 87).

Direction. One engine with one contract: `Scan(text, policy)` returns spans with start, end, kind, rule and confidence; `Apply` renders them. Union, windowing, identifier handling and line judging become operations on spans and policies inside the package. Action: rebuild.

### F6. Nothing measures precision, recall, cost at size, or what redaction does in real use (evaluation, high, not in the docs)

Evidence.
- The tests are exact examples (`redact_test.go:37-166`; `values_test.go`; `passwords_test.go:18-433`) plus random-input checks of idempotence and no panic. The one generated-corpus test checks only that nothing the baseline removes survives (`floor_test.go:159-198`). There is no labelled corpus, no testdata folder and no fuzz target; the "1% of a hard set" and the "Go, Python and Ruby sources the rules were measured against" (`floor_test.go:107-109`; known-limitations line 7) refer to material that is not in the repo.
- The per-entry count `red` is written (`inbox.go:89, 112, 120`) and read by nothing (grep). `internal/health` has no redaction row (grep). An entry whose text was dropped by a redactor panic leaves no event (`inbox.go:89-92`).
- No benchmark covers redaction (section F4).

Impact. Neither the leak rate nor the rate of destroyed facts is known, so a rule change cannot be judged, and a user cannot learn that redaction is eating their memory or failing.

Direction. A labelled corpus in the repo, positives by format and shape and negatives drawn from real code, docs and chat lines, with per-rule precision and recall thresholds in CI. A latency curve by size. Local, counts-only telemetry by kind and rule, shown by the doctor. A shadow mode that runs a candidate rule set over the user's own inbox and reports only the deltas. Action: add.

### F7. A false positive becomes a refused write, a dropped fact or a manual repair (invisibility, medium, not in the docs)

Evidence.
- System page writes refuse every flagged line (`systempage/page.go:152-154, 170-187`).
- The daily commit leaves a flagged file out (`wikigit/commit.go:216-220`); the doctor then reports FAIL with the fix "remove what must not be committed from the files left out, then run eva-md commit" (`health/background.go:298-305`).
- A knowledge change whose written lines are flagged is counted as refused and dropped (`update/knowledge.go:140-143, 192-195, 232-235, 404-409`). The mechanism is verified; how often real model output trips it is not measured.
- Ran a copy of `secretLine` (`wikigit/scan.go:114-137`). Flagged: `token_endpoint: https://auth.example.com/oauth/token`, `private_key_path: ~/.ssh/id_ed25519_deploy`, `- password: required, at least 12 characters`, `The token is validated by AuthenticationMiddlewareFactory before routing`, `The token is checked in validateAccessTokenAndLoadSession() first`, `title: Refresh token: rotation-policy-for-mobile-clients`.

Impact. An agent documenting an authentication system is refused line by line and spends turns rewording. A hand-edited page with one such line stops syncing until a person edits it. These are the manual steps and surprises the goal rules out.

Direction. Give findings a confidence tier. Certain secrets block. Suspects are rewritten to a typed placeholder or quarantined, and the write or commit proceeds. Depends on F1 for the precision that makes the tiers meaningful. Action: evolve.

### F8. A marker is an anonymous hole, so memory keeps hollow facts (recall-quality, medium, not in the docs)

Evidence.
- A marker carries only a kind (`redact.go:791-806`; `union.go:104-108`). Only `Identifier` adds a distinguishing hash, and only for identifiers (`redact.go:541-550`).
- Model output is redacted field by field and the statement is kept with the marker in it (`update/validate.go:999-1008`; `update/knowledge.go:116`; `update/validate_test.go:807` asserts the marker stays). System pages refuse a marker (`systempage/page.go:198-199`); the update's validator has no such check (grep).

Impact. A later chat reads "the staging token is [REDACTED:secret_value]" and cannot tell which secret it was, whether it is the one mentioned last week, or where to get it. When the marker replaced a non-secret (F1), recall serves a statement with its fact removed.

Direction. Typed secret references: a stable handle per distinct secret from a per-machine keyed hash, so two mentions can be tied together without revealing the value, and capture of where the secret lives (the variable name, vault path or file) as the fact worth remembering. The validator should drop a statement whose only content was redacted. Action: add.

### F9. Mentioning a PEM header deletes the rest of the entry (memory-quality, medium, not in the docs)

Evidence. The private key pattern ends at the END line "or the end of the text" (`union.go:53`; `redact.go:120`; `baseline.go:61`; copied in `catchup.go:1060`). Ran a copy: a 3,100-byte reply that quotes the BEGIN line once, with no END line, comes back as 54 bytes ending in `[REDACTED:private_key]`.

Impact. A chat about key formats, a grep pattern, or a truncated paste silently loses everything after the header in that prompt or reply. The inbox records one replacement and no truncation.

Direction. Bound a key block by its body: the header, then lines of base64 or armor headers, ending at the first line that is neither. An unterminated header with no body is text. The pattern is in the baseline too, so this falls under the same rule decision as F1. Action: evolve.

### F10. Command-line passwords are handled by a partial shell parser that grows per command (architecture, medium, symptoms known)

Evidence. Six command families are hard-coded with their options (`passwords.go:73-94, 116-128`). A hand-written word reader reads each command twice with two quote interpretations and merges the results (`passwords.go:140-252, 282-361`), with per-command scopes (`passwords.go:483-539`). known-limitations line 11 lists more than ten shapes it gets wrong in both directions (a `-p2222` port taken as a password, `openssl enc -k` missed, a word after an option in prose taken).

Impact. Every further tool (psql, mongosh, kubectl, az, gh, ansible) needs more parser code and more exceptions, and the known errors both leak passwords and erase words.

Direction. If per-command knowledge stays, hold it as a data table (command, flag, value form) over one shared shell tokenizer, the same one tool-line capture should use. Better, make it unnecessary: exact-match known secrets (F2) and value classification (F1) catch a password wherever it appears. Action: rebuild.

### F11. No policy surface, English only, and cleaning alters legitimate text (reach, medium, not in the docs)

Evidence.
- `internal/config` has no redaction setting (grep): no custom pattern, no allowlist, no optional class. Personal data is untouched; ran a copy: an email address, a US social security number and a card number pass unchanged.
- Key words and the prose rule are English (`redact.go:97-100, 431`; `passwords.go:99`); ran a copy: `contraseña: <password>` is kept.
- `clean` drops every `Cf` character (`redact.go:599-608`); ran a copy: an emoji ZWJ sequence is split into two emoji, a Persian word loses its ZWNJ, a soft hyphen disappears.

Impact. A team cannot add its own token format, silence a recurring false positive, or meet a rule about customer data in a wiki shared across an organisation. Text in other languages is both less protected and altered.

Direction. A redaction policy read from the config and from the wiki's schema, so a team shares it: custom patterns, an allowlist, optional personal-data classes. Match on a normalised shadow of the text and map spans back, so stored text keeps legitimate joiners while bidi and tag controls are still removed. Action: add.

### F12. Project and thread identity depend on the current rule set (architecture, medium, known)

Evidence. A project key is hashed when `RedactKnown` would change it (`identity/identity.go:156-168`; `coderef/coderef.go:174-185`), and the inbox refuses any entry whose host, session or project changes under `RedactKnown` (`inbox.go:96-101`). `Identifier` prefixes an unsalted 8-hex SHA-256 of the original (`redact.go:541-550`). design.md line 424 and known-limitations lines 15 and 20 state the consequences.

Impact. A rules upgrade can move a project to a new inbox folder, so its brief and search see only the chats after it. The unsalted hash can be tested against guesses.

Direction. Mint a stable opaque id per project and per thread once, store the mapping, and redact display names separately. Use a keyed hash where a hash must be shown. Action: evolve.

---

## 4. Where the complexity is essential and where it is accidental

Essential: telling a secret from code or prose is a judgement, and the corpus of cases in the tests encodes real knowledge; cleaning before matching; marker integrity; linear cost; fail-closed handling.

Accidental:
- Two engines at run time and string alignment between them, because neither returns spans (`union.go`).
- Floor rules that duplicate the baseline inside the current rules (`redact.go:316-334, 911-1004`).
- A multi-pass loop over 40 or so regexes with weak hints, each rescanning the text.
- Three entry points and shims in four other packages, all to keep identifiers out of the entropy rule's reach.
- A shell parser written for six commands.
- Recovery's copy of the cleaning and key patterns.

## 5. Overall call

Rebuild the engine; keep the invariants and the test cases. The package is careful work, but it is built on a contract (strings in, strings out, a frozen imprecise floor unioned at run time) that fixes its two worst properties in place: it destroys ordinary facts about authentication for good, and it doubles the cost on the hook's hot path. A span-based, single-pass, tiered engine with the baseline kept as a test oracle over true secrets, a labelled corpus, exact-match masking of known secrets and a way to repair what is already stored addresses F1 to F8 together. The existing tests carry over as the acceptance corpus. The rebuild needs one decision from the owner first: whether the frozen-baseline rule protects true secrets or every removal the baseline ever made.

## 6. Open questions

1. Will the owner amend the rule "never change the baseline; Redact removes the union" so that the baseline's false-positive classes (plain words, URLs without userinfo, paths and identifiers after a secret-looking key) can be released, with the baseline kept unchanged as a test oracle for true secrets?
2. Where is the trust boundary? Recovery reads the hosts' own transcripts (`catchup.go:1258-1264`), which hold the same text unredacted on the same disk for as long as the host keeps them. Is at-rest redaction of the inbox meant to protect long-lived local storage, or mainly what leaves the machine (the model call and the wiki push)? The answer decides whether suspects may be held locally for later restoration.
3. May eva-md read local secret sources (the environment, `.env` files, credential files) to build an exact-match mask set, and where may that set live?
4. Is personal and customer data in scope for wikis shared across a team or an organisation?
5. May inbox lines become replaceable (a tombstoned rewrite), which remediation needs, given that the design says the JSONL is never rewritten?
6. What does redaction cost on the target Macs at 16 to 200 KiB? The figures here come from a Linux VM.
7. How often do the F1 and F7 false positives occur in real inboxes and wikis? Nothing measures it today (F6); a counts-only shadow run would answer it.

---

## Verification

A second reader tried to refute each finding. Method: read the four files and every cited call site; copied `redact.go`, `passwords.go`, `union.go`, `baseline/baseline.go` and the line judge of `wikigit/scan.go` verbatim into a scratch module outside the repo (`working-papers/verify-redact`, only import paths changed) and ran made-up inputs. No repo file was edited, no repo test and no restricted command was run. Timings are from a Linux VM; read them as ratios.

One check in the code's favour: 150,000 random inputs built from secret-shaped fragments, markers, quotes and brackets gave no case where `Redact(Redact(x)) != Redact(x)` and none where `baseline.Redact` still replaced anything in `Redact(x)`. The union's two promises (idempotent, never weaker than the baseline) hold as implemented; 14 repeated shapes from 16 to 256 KiB all scaled linearly.

| Finding | Verdict | Severity (mine) | Evidence and correction |
| --- | --- | --- | --- |
| redact-1 | partly | high (was critical) | Every listed input reproduced: `token: optional`, `password: required`, `token_endpoint: https://...`, `TOKEN_ISSUER=...`, `private_key_path: ...`, `api_key_header: ...`, `secret_name: ...`, `password_policy: ...` are replaced, and `http.StatusUnauthorized` after "missing token" goes to the entropy rule. Two corrections. Scope: the same facts in prose survive ("The token endpoint is https://auth.example.com/oauth/token and the header is X-Internal-Api-Key", "We store the private key at ~/.ssh/id_ed25519_deploy" came back unchanged), so the loss is lines shaped `key: value` or `key=value` and identifiers within five words of a trigger word, not every auth fact. Cause: the current rules alone replace every one of the listed inputs too, so amending the frozen-baseline rule restores nothing until the current rules also change. The trade-off is partly accepted in known-limitations lines 9 and 12, and its rate is unmeasured. |
| redact-2 | confirmed | high | Reproduced kept whole: `ENCRYPTION_KEY=`, `RAILS_MASTER_KEY=`, `signing_key:`, `KEY=`, Azure `AccountKey=`, TOML `[auth]` `key =`, `auth =`, `"auth":`, `credentials:`, `Cookie:`, `Set-Cookie:`, `psk=`, the `.netrc` line, `sudo -S`, `--from-literal=pw=`, `gsk_`, `tskey-auth-`, `ya29.`, `AGE-SECRET-KEY-1`; `app:p@ssW0rd!x9@db` leaks its tail and a `/` in the password stops the rule. One correction: a next-line value is kept only when it is under 20 characters or low in entropy; a 24-character random value after `password: \|` or `"password":` is taken by the entropy rule, which crosses lines. One addition in the same class: "API key" written with a space is neither a key nor a trigger, so `API key: <24 random>` and "Here is my API key <24 random>" are kept. |
| redact-3 | confirmed | high | `redact.Version` appears only in docs and a test comment; `Entry` has `v` (schema) and `red`, no rule version (`inbox.go:28-45`); `leaks` reads `git diff --cached -U0` and judges `+` lines only (`scan.go:32, 82-84`); `internal/lint` and `maintain.go` contain no secret scan; model output is scrubbed with `redact.Redact` (`validate.go:999-1008`). The gate is in fact weaker than "the same detector" (see redact-m1). design.md line 1437 lists the repeat runs as the mitigation for a missed format, which this finding undercuts. |
| redact-4 | confirmed | high | `Append` redacts inside the hook's budget (`inbox.go:89`, `hook.go:223-235`), `Redact` takes no context, both engines run. My timings are higher than the report's once a text holds any replacement, since a replacement forces a second union round: reply-like Markdown with one false positive per 0.5 KiB took 33 ms at 8 KiB, 67 ms at 16 KiB, 288 ms at 64 KiB; minified JSON with secret-looking keys 191 ms at 16 KiB; plain prose 64 KiB 9 ms, with "key" in each sentence 66 ms. Citation slip: the timed 23-byte reply is `scripts/benchmark.py:294`; line 152 seeds a 38-byte one. Additions: a Bash tool line redacts the whole command, up to the 2 MiB payload, to keep its first line and 200 characters (`hook.go:803-806`), and a Stop can redact the same reply again in recovery and in `catchup.Contains` (`catchup.go:612, 1492`; `hook.go:576`). Whether a real Mac misses the budget is unmeasured, as the report says. |
| redact-5 | partly | medium (was high) | The three mechanisms, recovery's copies (`catchup.go:1039-1072`) and the callers' shims are as cited; 3,522 non-test lines. Two corrections. The floor rules are not a pure duplicate: `RedactKnown` and `Identifier` never run the baseline (`redact.go:481, 531`), so the floor is what holds identity fields to the baseline's key=value level. "Each new secret shape needs patches in several packages" is not shown: a provider format is one entry in `rules` (`redact.go:123-250`); only shapes that cross lines touch recovery. |
| redact-6 | confirmed | high | No `Fuzz` function or `testdata` under `internal/redact`; `Red` is assigned in `inbox.go` and `catchup.go` and read nowhere; `internal/health` has no mention of redaction; no benchmark over 38 bytes of reply. "No labelled corpus" is strong: the tests hold several hundred labelled keep and remove cases (`TestPositives`, `TestNegatives`, `TestCodeLinesKept`, `TestPasswordShapes`, `TestPasswordShapesKept`), which do judge a rule change case by case; what is missing is a rate. The hex measurement in redact-m2 took minutes and shows what a rate check finds. |
| redact-7 | confirmed | medium | All four sites are as cited. Running the copied `SecretLines`: `token_endpoint: https://...` and `- password: required, at least 12 characters` are flagged. Narrowing: for knowledge, text is scrubbed before `flagged` runs, so that refusal needs the rendered line to trip anew; the common outcomes are a refused system page line and a file left out after a hand edit. |
| redact-8 | confirmed | medium | A marker is `[REDACTED:<kind>]` and nothing else; `scrub` keeps the statement with the marker; no file under `internal/update`, `internal/knowledgepage` or `internal/hook` outside tests mentions the marker, so nothing drops or flags a hollow statement, and recall shows it (`hook/recall.go:180` passes a line whose only hit is an existing marker). |
| redact-9 | confirmed | medium | Reproduced: a 3,113-byte text with one `-----BEGIN OPENSSH PRIVATE KEY-----` and no END came back as 54 bytes, n=1; with a matching END only the pair went. It needs the full dashed header: `grep -l 'BEGIN RSA PRIVATE KEY'` is untouched. |
| redact-10 | partly | low (was medium) | Six command families and the two readings are as cited, and known-limitations line 11 lists the misreadings. But the shape the direction asks for is already there: commands are `command` values named by rule entries over one shared reader (`commandMatches`, `commandWords`); per-command code is two scope functions and three value pickers. The 941 lines are the whole of `passwords.go`, which also holds the pass and pwd keys and the prose rule, not six commands. |
| redact-11 | confirmed | medium | `internal/config` has project and remote exclusions only; an email, a US social security number and a card number pass; `contraseña:`, `mot de passe:` and `Passwort:` keep a 12-character value; a ZWJ emoji sequence, a Persian ZWNJ and a soft hyphen are altered by `clean`. |
| redact-12 | partly | low (was medium) | Code as cited, and `hook.go:394` keys threads with `redact.Identifier`. Scope: `RedactKnown` has no entropy rule, so only a new or changed format or key=value rule that matches a folder, repo or branch name moves a key; documented at known-limitations lines 15 and 20. |

### Missed

**redact-m1. The last gate before a push detects less than capture does (reliability-safety, medium).** `SecretLines` splits the text and judges each line alone (`wikigit/scan.go:90-97`), and x-es out identifier-shaped words before judging (`scan.go:114-137`). Running the copied judge next to `Redact` on the whole text: "The deploy token is:" followed by a fenced 24-character value, `password: |` with the value on the next line, `"client_secret":` with the value on the next line, "The staging token is prod-9f8a7b6c5d4e3f2a1b0c-east" and "Use the secret deploy_9f8a..._key" are all replaced by `Redact` and none is flagged by the gate. System pages (`systempage/page.go:152`) and hand edits never pass capture, so this check is their only one, yet it still blocks the non-secret `key: value` lines of redact-7. design.md line 89 states the line-alone reading as design; known-limitations line 87 gives its effect on search only. Direction: judge the added hunk or the whole page with the detector's own context and map spans back to lines; make identifier exemptions a span kind inside the engine so the gate is a superset of capture.

**redact-m2. The entropy rule keeps a fifth of random 32-hex keys and every digit-only one (reliability-safety, medium).** Both rule sets test a token's own symbol frequencies against 3.5 bits (`redact.go:30-35, 1774-1785, 1829-1843`; `baseline.go:404-415, 436-450`); hex tops out at 4.0 and a short sample falls below. 20,000 random tokens each after "the staging token is": kept whole 78% at 20 hex characters, 54% at 24, 19% at 32, 0.9% at 48, 100% of 24 digits; base62 at 20 characters 0.1%. This rule is the only cover for a secret in prose, on the next line or beside an unlisted key. Adding detection is allowed under the frozen-baseline rule. Direction: replace per-string entropy with a test by character class and length (hex, base32, base64, digits), with thresholds set and gated on a generated corpus.
