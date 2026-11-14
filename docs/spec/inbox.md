# The inbox

- **Component:** Inbox store
- **Paths:** `internal/inbox`, `cmd/eva-md/inbox.go`
- **Status:** v1 as built, frozen on 2026-11-14

The blocks below are v1's design and verification text, moved here word for word when the two documents were split; [INDEX.md](INDEX.md) lists every spec.

<!-- v1 docs/design.md 008 -->
## The inbox

**Location.** `~/.local/state/eva-md/inbox/<project-key>/<host>-<session-id>.jsonl` on each machine.
- The project key is the normalized remote (`github.com__you__eva-md`), `local-<folder>-<hash6>`, or `general`. When a name would read as a provider token, the key is `repo-<hash12>` or `local-<hash12>` instead, so the redactor never rewrites it. Which names read so depends on the current rules, so a rule change can move a project to the other form; its later chats then go to the new key's folder.
- Watermarks live in `inbox/<key>/.state/`.
  Session markers live in `~/.local/state/eva-md/sessions/`, heartbeats in `heartbeats/` (each event's last 20 runs and a week of its daily counts, and each host's `uncaptured.json`, the time a hook last ran in a folder eva-md doesn't capture) and transcript cursors and drift records in `catchup/`
  under the same folder; so do the background update's run log, its busy exits (`busy-<YYYY-MM-DD>.jsonl`) and the day it last took the day's sweep (`sweep`), in `updates/`, its detached jobs' logs, in `logs/`, the daily commit's records, in `commits/`, and its trigger markers, in `commit/`, the lint's `lint.json` and install's `cli-paths.json`.
  The identity cache lives in `~/.local/state/eva-md/cache/` and can be deleted at any time.
- The search index lives in `~/.local/state/eva-md/cache/search/`, a folder per inbox project folder. It is built from the inbox, never committed, and can be deleted at any time (see "Search index").
- Folders are 0700 and files 0600 (umask 077). The inbox is kept outside the wiki, every repo, and Documents, Desktop and iCloud. Capture works before the wiki exists.

**Format.** JSONL, one object per line, never rewritten. Fields:

| Field | Meaning |
| --- | --- |
| `v` | format version, 1 |
| `ts` | RFC 3339 with local offset |
| `host` | `claude-code` or `codex` |
| `session` | host session id |
| `turn` | Claude `prompt_id` or Codex `turn_id`. Absent on Codex SessionStart and SessionEnd, and before the first prompt on Claude |
| `role` | `user`, `assistant`, `tool`, `plan`, `host` or `event` |
| `kind` | see below |
| `agent` | subagent id when the payload carries `agent_id` |
| `text` | redacted text |
| `project`, `branch`, `cwd` | identity. `cwd` is relative to the home folder |
| `model` | when the payload has it (Codex: every event except SessionEnd) |
| `origin` | `hook` or `catchup` |
| `red` | redaction count |
| `trunc` | bytes cut from the middle of anything over 200 KB |

Kinds:
- **prompt:** your words, verbatim.
- **reply:** the final assistant message, verbatim. On Claude this is `last_assistant_message`, which holds only the final reply text. Codex's value may be null, which counts as a missing reply.
- **tool:** tool name, plus what it acted on, plus the exit code, never the output: the file path for Read, Edit, Write and NotebookEdit, the redacted head of a command, the quoted pattern and the path for Grep and Glob, the URL for WebFetch without its user part, its fragment or any query parameter
  whose name says it carries a credential (such as `sig`, `token`, `key` or `code`), the file names of an `apply_patch`, at most 64, and for an `mcp__*` tool its string and number arguments as `key=value`, shortest first, at most 16, leaving out a value over 1 KiB and any argument whose name says it
  carries a credential (such as `auth`, `cookie`, `pass`, `password`, `session` or `apiKey`), and cleaning an argument that is an absolute URL as WebFetch's URL is, when the argument is named as an address (url, uri, href, link or address, even with a space in the path) or the URL is the whole argument (text that only starts with an address is kept as it is).
  A WebSearch is kept by its name alone, never its query or results, so the update can tell a turn that read the web.
  Each part is redacted; a command head, a pattern and the MCP arguments are cut to 200 characters, a path or URL to 1024.
  The Claude Code plugin's tool matcher includes Read, Grep, Glob, WebFetch, WebSearch and `mcp__` tools, and the Codex plugin's includes `mcp__` tools, so these lines come from the hooks themselves.
  - Claude: success (PostToolUse) means exit 0. Failures come from PostToolUseFailure, whose error starts with "Exit code N".
  - Codex: parse `Exit code: N` when present and discard the response text. On Codex 0.164.0, a unified-shell command with no output, such as `exit 7`, returns an empty response and no other exit-status field, so record `exit unknown` when it is missing. Never infer success from turn completion or the requested command. File paths are parsed from apply_patch's `*** Add/Update/Delete File:` lines.
  - Subagent tool lines carry `agent` and are labelled "(subagent)".
- **plan:** a checklist from Claude's TaskCreate, TaskUpdate or TodoWrite tool calls when present, or from Codex `update_plan`. On current Claude default models these tools are off unless you opt in, so Claude plan entries are optional.
- **host_summary:** Claude's PostCompact `compact_summary`, labelled as written by the host.
- **event:** session_start, compact, session_end, off_record, on_record, model_switch, interrupt, stop_failure, host_prompt, gap.

Prompts you didn't type are stored as events, never as your words.
On Claude these are scheduled tasks, /loop runs, background subagent reports and messages from other sessions.
Codex prompts that carry `agent_id` are also stored as events.
So are the prompts of a Claude chat a program runs, told by `CLAUDE_CODE_ENTRYPOINT` being `sdk-cli` (`claude -p`), `sdk-ts` or `sdk-py`; its replies and tool lines are kept, a record control in its prompts is still honoured, and transcript recovery never stores its prompts either.
Any other value, such as `cli`, `claude-desktop`, `claude-desktop-3p`, `local-agent` or `claude-vscode`, or none, counts as a person's chat.
Each heartbeat records the cleaned entrypoint (32 characters) and whether it was a program's chat.
Codex `codex exec` runs are not told apart: nothing known marks them.
Built-in slash commands are not stored.

A chat a program runs starts no update of its own: its SessionEnd, PreCompact, Stop backlog and prompt triggers queue nothing, and the chat-start catch-up never takes its own file, though it still takes the project's other idle sessions.
The update job passes over a session whose inbox file holds no prompt the user typed and whose catch-up record marks it a program's chat, or that, once its window reaches the end of the file, holds a host prompt: it records `skipped (headless)` and moves the watermark past the window with no model
call, to the end of the file unless a marked chat's backlog runs on past the window (see "A program's chat" under "Job control").
A person's chat that holds host prompts, from scheduled tasks, /loop runs or subagent reports, or a typed prompt anywhere, is updated as before.
Codex `codex exec` runs can't be told apart and stay as they are.

An entry is referred to as `s:<sid8>#<line>`, and `eva-md show` resolves those refs. It also accepts the full session id in place of the first 8 characters, which settles a ref that two sessions share. Because entries are JSON-escaped, pasted text can never forge a speaker or an entry boundary.

**Redaction.** Applied in the hook before any byte reaches disk, again on model output, and on the staged diff before each commit. Rules:
- private key blocks
- AWS AKIA, ASIA, ABIA and ACCA ids and secret keys
- GitHub `gh*_` and `github_pat_` tokens; GitLab `glpat-`, `gldt-`, `glrt-`, `glptt-` and `gloas-` tokens
- Slack `xox*-` and `xapp-` tokens and webhook URLs, Discord webhook URLs and Telegram bot tokens in a Bot API address
- OpenAI `sk-`, `sk-proj-`, `sk-svcacct-` and `sk-admin-`, Anthropic `sk-ant-`, Google `AIza`, and Stripe `sk_` and `rk_` keys; 1Password `ops_` service tokens
- Hugging Face `hf_` and npm `npm_` tokens that mix capitals, small letters and digits; PyPI `pypi-AgE…`, HashiCorp Vault `hvs.` and `hvb.`, DigitalOcean `dop_v1_`, `doo_v1_` and `dor_v1_`, SendGrid `SG.`, Shopify `shpat_`, `shpca_`, `shppa_` and `shpss_`, and Docker `dckr_pat_` tokens
- JWTs; Authorization Bearer and Basic values; `user:password@` inside URLs
- the value in `key=value` or `key: value` when the key looks like password, secret, token, api_key, access_key or client_secret and the value is 8 characters or more
- the same for a key whose last part is `PASS` or `PWD` after one or more separators or a change of case, such as `DB_PASS`, `MYSQL_PWD`, `SMTP__PASS`, `Db__Pwd` or `userPwd`, with a quoted value taken whole, spaces too, unless it starts or ends with a blank, as code between two strings does; and
  for `pass` or `pwd` alone after `?`, `&` or `;`, as in a URL query or a connection string's `Pwd=`, or as `--pass=`.
  A key ending in `pass` or `Pass`, as in `render_pass`, `onPass` or `Smtp__Pass`, and `pass` or `pwd` alone anywhere else, as in YAML or an object literal, lose a value only when it looks like a password (below).
  A path after a `pwd` key is kept, since `PWD` and its kin hold a folder, and so is `PASS:` in capitals, a test result
- a MySQL or MariaDB client's `-p<password>` (`mysql`, `mysqldump`, `mariadb-dump`, `mariabackup` and the like), the password in curl's `-u`, `--user`, `-U` or `--proxy-user` `user:password` (a `-u` or `-U` alone or after short options that take no value, as in `-sSu`), the one sshpass takes after
  `-p` among its own options, the one `docker login` or `podman login` takes after `-p` or `--password` (never `--password-stdin`, nor another command's `-p`, a port), the one `redis-cli` takes after `-a` or `--pass`, and openssl's `-pass`, `-passin` or `-passout` `pass:<password>` (never its `env:`, `file:`, `fd:` or `stdin` forms), when the command's name starts a command word.
  Every such option of the command is read, and only the command's own: it ends at a line end, an unquoted `;`, `|`, `&&`, `||`, a `&` that sends it to the background (never a redirection's, as in `2>&1`, nor one between two characters of a word, as in an unquoted URL query's `?a=1&b=2`, nor any of
  these inside a bracket the command opened, as in `$(cd x && cat y)`), a backtick or a `)` it didn't open; a command written over several lines with backslashes is read across them, and a quoted string is one word, whatever it holds, though a command inside it is read on its own, within the quotes, as in `sh -c 'curl -u …'`, up to that string's closing quote when the name follows the quote.
  A command is read twice.
  The first reading pairs each quote with the next of its kind on the line, so a `;` in a quoted header or SQL statement ends nothing, and `\'` inside bash's `$'…'` is escaped; the second pairs a quote only when its string is a word of its own, and also takes a backtick substitution that is a word of
  its own, as in ``mysql -h `cat host` -p…``, as part of the command, where the first ends at the backtick as inline code in Markdown does.
  Whatever either reading takes is replaced, so a quote the first pairs wrongly, as the apostrophe in `o'brien` or a quote inside `"$(…)"`, can't hide a password, and the second reads an option's value as the first does, so both take a password the same way.
  An option where another option's value was due, as the second `-u` of `-u -u admin:…` or the `-p` of `sshpass -d -p …`, is read as the option.
  An sshpass that a later word of another starts, as in `sshpass -e ssh jump sshpass -p …` or `$(sshpass -p …)`, has its own options read, which end as the first's do or at a `)` that closes a bracket opened before it.
  An option's value is attached to it or the next word after blanks, quoted in whole or in part, as in `admin:'S3cret pw'`, and a `:` inside the marker of a token that names curl's user is not the one before its password.
  A variable, a command substitution or a placeholder there is kept, except inside single quotes, where the shell expands nothing
- a password in prose, after password, passwd, pass or pw and is or was ("my password is Hunter2!x"), when the word looks like a password: 6 characters or more with letters mixed with digits or symbols, or 12 or more letters that mix cases at random.
  An ordinary word, words joined by `-`, `_`, `.`, `/` or `'`, digits alone, a technical term such as `base64`, `sha256` or `utf-8`, a measurement on its own, such as `16.6ms` or `1.25GB`, where a whole number takes a unit of two letters or more, as in `120ms`, a path, a placeholder, code and a variable are kept
- high-entropy strings next to such words

Matches become `[REDACTED:<kind>]`. Git SHAs and UUIDs are kept in ordinary text, but not as the value of a secret-looking key, a URL password or an Authorization header, where they may be the secret. `op://` references are kept. If redaction fails, nothing is stored (fail closed).

A `key=value` or `key: value` on a line of its own (dotenv, YAML, INI, properties) takes the value to the end of the line, across `& ; , ( ) < > { } [ ]`, while code shapes such as calls and `$VAR` references are kept.
The text removed is the union of what the baseline rules (`internal/redact/baseline`) and the current rules remove, judged on the same cleaned text, so a rule change can only remove more.
Identity fields get only the format rules, not the entropy rule, so names like `token-refresh-worker` stay intact.

**Retention.** No time limit and no automatic deletion. Uninstall keeps the inbox; `uninstall --apply --all` removes it with the rest of the local data (see "Install and uninstall"). The doctor reports its size, roughly 0.1 to 0.3 GB a year.

**Removal.** `eva-md inbox clear` removes captured sessions whole, never a line of one, since the JSONL is never rewritten: those `--session` names (repeatable; a full id, or the first 8 characters a ref keeps, or a ref `s:<id>#<line>`), those of the project `--project` names by its key, its inbox
folder's name, and those last written before the local day `--before` names, by the file's modification time (the watermark's, for a file already gone); given together, all must match.
A filter given an empty value, as a script's unset variable gives it, is a usage error, never taken for one not given, which would widen the removal, and a day `--before` is given always filters, 0001-01-01 too, which in UTC is the zero time: it chooses nothing.
8 characters two sessions share, with neither's whole id given, choose nothing, and the error names both.
A session whose file is gone while its watermark stays, as a file deleted by hand leaves it, counts too.
Without `--apply` it previews: each session with its host, id, project, last write and size (20, then a count), how many refs in the wiki's thread pages name them, and that the wiki and its history keep what updates wrote; it changes nothing.
`--apply` takes the update's lock without waiting, so no update writes a watermark again for a file removed, and the search index lock of each project concerned, waiting up to 5 s for a search's top-up; while either is held it removes nothing and says to try again.
It then takes each session under its session lock, waiting up to 10 s for a hook, and stopping there, saying to run it again, while one still holds it: its transcript recovery record goes when it would append to a project folder whose capture goes, or can't be read, so a cursor behind the hooks can
never bring removed rows back, but a blocked one, as a chat off the record has, stays: it may be all that keeps the chat off the record, when the markers couldn't be written, and it brings nothing back, since a blocked record recovers no row and leaving the block starts again at the transcript's
end; then it appends a line for the session to `inbox/.removed` (JSON: v, at, host, session; fsynced, under a flock), just before its files go, whose removal Ctrl-C no longer stops, so a removal that stops at a session a hook holds, on Ctrl-C or on a failure records no session it keeps, short of a
file it can't remove or a kill in between; then each session file, then its watermark, so a removal cut short leaves at most a watermark, which the next removal takes.
Last it deletes the search index folder of each project it removed from, so the removed words leave the disk at once rather than at the catch-ups after (see "Search index"); `eva-md index` or the daily maintain builds it again, and search scans those files meanwhile.
A project folder left empty goes too.
The session markers stay, since they hold no chat text and `off_record` and `was_off` keep a chat still open off the record, and so does the session's empty lock file, which a hook may already hold open.
A chat still open starts a new file at its next turn.
The wiki, its history and the hosts' own transcripts are never touched.
The daily lint reads `inbox/.removed` (see "Health check"), and so do `eva-md show` and `eva_show` when a ref finds no line, to say that its chat was removed with `eva-md inbox clear` rather than that the source wasn't found.
`inbox list` and `inbox show` are planned; `eva-md search --all` shows each inbox hit's project key and ref.

---

<!-- /v1 -->

## Must keep

The must-keep items of [findings.md](../v2/findings.md) this component touches, numbered from K01 in each list, and the tests that hold them.

| Id | Item | Tests |
| --- | --- | --- |
| CAP-K06 | Redaction before disk fails closed: a redactor panic stores no text, a secret in an identity field rejects the entry, other metadata is redacted | internal/inbox:TestRedactionTruncationAndFailClosed, internal/inbox:TestIdentitySecretsStillRejected, internal/inbox:TestMetadataRedactionAndInvalidLines, internal/hook:TestModelAndMetadataSafety |
| CAP-K12 | Append-only JSONL under a file lock; a killed writer's partial line is ended and marked; a malformed line reads as a gap | internal/inbox:TestConcurrentAppend, internal/inbox:TestSeparateProcessAppend, internal/inbox:TestInterruptedAppendRecovery, internal/inbox:TestPartialLinesAndWatermark, internal/inbox:TestMetadataRedactionAndInvalidLines |
| CAP-K13 | Escaping and quoting keep pasted text from forging an entry, a speaker or the brief's notes | internal/freetext:TestShown, internal/resume:TestFreeTextCannotForgeSpeakerHeaders |
| CAP-K14 | State is confined: opened through a root with no linked ancestor, safe names, links and swapped files refused, private modes | internal/paths:TestRootedWritesRejectEscapeAndCancellation, internal/paths:TestSafeName, internal/inbox:TestTraversalAndSymlinks, internal/inbox:TestOpenSessionRefusesLinks, internal/inbox:TestPermissionsAndRefs, internal/session:TestSymlinkConfinement, internal/session:TestHostileNamesStayInsideRoot, internal/session:TestPermissions |
| CAP-K15 | An entry is trusted only in the file its host, session and project name, and nothing said off the record reaches search | internal/inbox:TestWalkYieldsWhatSearchCanMatch, internal/inbox:TestWalkResumesFromAPosition |
| CAP-K17 | Removal previews first, an empty filter is a usage error, and lock, record, file and watermark go in that order | cmd/eva-md:TestInboxClearPreviewsThenRemoves, cmd/eva-md:TestInboxClearArguments, cmd/eva-md:TestInboxClearKeepsTheRecordOfAChatOffTheRecord, internal/inbox:TestRemoveTakesTheFileThenItsWatermark, internal/inbox:TestRecordOfRemovals |
| CUP-K15 | Removing a capture removes the record that could bring it back, under the session lock, only if unchanged; a blocked record stays | internal/catchup:TestForgetKeepsRecoveryFromBringingBackARemovedCapture, internal/catchup:TestForgetKeepsARecordThatKeepsTheChatOffTheRecord, internal/catchup:TestPruneKeepsARecordSavedAgainAfterTheListing, internal/catchup:TestPruneKeepsARecordSavedBeforeItLocks, internal/catchup:TestPruneLeavesARecordThatAloneKeepsAChatOffTheRecord |
| RED-K01 | Text is redacted before any byte reaches disk and before it is cut, so a token across a cut leaks no prefix | internal/inbox:TestRedactionTruncationAndFailClosed, internal/hook:TestToolLines, internal/catchup:TestBoundedRedactMatchesFullRedaction |
| RED-K02 | A redactor panic stores no text, or a gap in recovery, never unredacted text | internal/inbox:TestRedactionTruncationAndFailClosed, internal/catchup:TestRedactorPanicLeavesGap |
| RED-K12 | Logs and entries record how many replacements, never what was replaced | internal/inbox:TestAppendRedactedKeepsTextAndChecksMetadata, internal/redact:TestCount, internal/hook:TestRecordsHoldNoContent |
| SRC-K03 | An entry must belong to the file it is read from | internal/resume:TestSearchTrustsOnlyEntriesThatMatchTheirFile, internal/inbox:TestWalkYieldsWhatSearchCanMatch, internal/searchindex:TestOffRecordAndUntrustedNeverIndexed |
| SRC-K11 | Clearing chats holds the index folders and drops each cleared project's index | internal/searchindex:TestHoldDropsAFoldersIndexWhole, internal/searchindex:TestHoldIsBusyWhileAWriterHoldsAFolder |

## Since the split

No change yet.
