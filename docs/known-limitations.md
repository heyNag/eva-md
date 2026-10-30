# Known limitations

These are open problems in capture and resume as the code stands. Wiki updates and support for work machines on self-hosted git are planned and not built yet; they are described in [design.md](design.md), not here. What has been verified is in [verification.md](verification.md).

## Redaction

- About 1% of a deliberately hard set of generated passwords still keep a piece after redaction. The shapes are ambiguous by nature: a value containing `)` inside a call, `&k=` or `;k=` inside a URL query or connection-string value, and values that start with an unclosed bracket.
- A UUID or a 40-character hex string is kept even when it is the value of an explicit secret key, as in `password=<uuid>`. Secrets in those shapes are stored in the inbox.
- The entropy rule erases ordinary camelCase identifiers that follow words such as token, secret or password, as in `token = maxCompletionTokensPerRequest`. Prose is over-redacted too: `Password: expired yesterday` loses its value.
- When `redact.Identifier` replaces part of a branch name or project key, it puts an unsalted 8-hex hash of the original in front. The hash can be checked against guesses, and cleaning an identifier that was already cut to its length limit can change it again.
- Redaction takes about twice as long as the baseline rules alone, because `redact.Redact` runs both rule sets. Very long recovered replies are therefore more often cut to head and tail windows.

## Capture, identity and config

- Identity uses `git rev-parse --abbrev-ref HEAD`. When a tag or another ref shares the branch's name, git answers `heads/<name>`, so that branch's chats go to a separate thread.
- `origin` is read with `git config` and not `git remote get-url`, so a `url.<base>.insteadOf` alias gives one repo two project keys and can get past a remote exclusion.
- A top-level folder whose name ends in whitespace cannot be resolved, because git output is trimmed. Chats in that repo are not captured.
- A remote exclusion such as `remote: github.com/acme/*` (with a space) or one written as a URL, scp form or with `.git` passes validation but never matches.
- Path exclusions compare bytes, so the NFC and NFD spellings of the same non-ASCII folder name do not match each other.
- Unknown or misspelled config keys are ignored without a warning, so a mistyped exclusion is silently absent. An explicit `daily_cap = 0` becomes 40.
- A Codex memory-writing run is skipped only for the events whose prompt or folder shows it. Other events of the same run are captured as an ordinary chat.

## Transcript recovery

- Start-of-chat catch-up reads only the first 256 recovery records per host, in directory order, and old records are never pruned. After weeks of use it can miss this project's abandoned chats, including the newest one.
- Duplicate suppression uses the turn id or, without one, the text and its timestamp. A recovered final reply over 48 KiB that has no turn id can be stored twice.
- Going off the record moves the recovery cursor to the end of the transcript first. On-record replies that had not been recovered yet are lost.
- Recovered main-thread rows take the agent id of the last hook event that registered the chat, which may be a subagent's.
- While a recovery pass is still behind, a hook stores only its event's name, as off-the-record mode does, because the unread rows may hold an off-the-record request. A tool line, a host compaction summary or a compact or first-prompt brief arriving then is dropped without a gap marker.
- Recovered Claude Code prompts are recognised by their `promptId` field. If a host version stopped writing it, typed prompts would no longer be recovered from transcripts; the hook would still capture them.

## Resume brief

- The first-prompt fallback brief has the prompt hook's 300 ms budget. If live git is slow, the brief is discarded and rebuilt on every prompt.
- The briefed marker is saved before the brief is written out. If the deadline falls in between, that brief is lost and the first-prompt fallback does not run.
- Reading the Now page loads the whole wiki through okf. One stray or invalid file anywhere in the wiki removes the Now section from every brief, and the brief says no page exists.
- Stale-Now detection counts every record, including the `session_start` the hook writes just before building, so a compact brief can call a fresh Now stale.
- A prompt or reply whose text starts with `stop_failure` is shown as a failed reply.
- "Tool activity" is the last tool record, not the last test or build command and the files touched.
- The space reserved for the brief does not count growth from cleaning and fencing, so label-like lines can still cut the newest reply or the `eva-md show` footer.
- If a git lookup for an in-progress operation fails, the brief says nothing about it instead of saying it is unknown.
- Refresh, resume and fork briefs repeat everything instead of only what changed, so a dirty tree or an old stash makes every resume brief non-empty.
- Source refs such as `s:<8 characters>#<line>` use the first 8 characters of the session id. Codex ids are time-ordered and can share them, and `eva-md show` then reports the ref as ambiguous and does not accept a full session id.

## Install and uninstall

- If the installed binary is missing, install refuses to restore it, although doctor tells you to run install.
- If a failed upgrade removed the old plugin, rollback can stop before its step that re-adds it. Install also has no Ctrl-C or SIGTERM handling, so an interrupt skips the rollback.
- `uninstall --apply` does nothing, and reports no error, when the install folder or its marker is missing, even if the plugins are still registered with the hosts.
- A stray file such as `.DS_Store` in a managed plugin folder blocks install, including binary-only updates, and blocks uninstall. The error does not name the file.
- Install fails when it is run through a symlinked executable.
- Uninstall leaves empty folders in `~/.local/share/eva-md`.
- Codex keeps its hook approvals after uninstall, so a reinstall with identical hooks may not ask for approval again.
- Install and uninstall print the same action list for a preview, a real run and a run that failed and rolled back.

## Health check

- Doctor reports UNKNOWN, not FAIL, when Codex loads no eva-md hooks for an installed and enabled plugin, or when the plugin inventory shows a conflicting registration.
- Chats in excluded, temporary or wiki folders write no heartbeat but do create host transcripts. The capture-freshness check can then report a false FAIL and a false "no matching capture heartbeat" notice.
- The capture-freshness scan gives up after 4096 entries or 512 folders in a host's chat store. Once a store is that large, the check stays UNKNOWN and no longer flags a trust lapse.
- The first trusted Codex chat after install can still show the notice to review hooks in `/hooks`.
- Background updates and daily commits are reported as UNKNOWN until wiki updates are built.

## Setup and wiki

- Adopting an existing wiki is refused for any validation finding, such as a warning, an orphan page or a broken link, not only for errors, although `eva-md validate` passes the same wiki (`internal/wikiinit/init.go`).
- An adopted wiki stores the raw hostname as the machine label, while a fresh wiki stores a sanitized one.
- The scaffold's `.gitignore` entry `inbox/` is not anchored, so it ignores any folder named `inbox` in the wiki. This will matter once wiki updates commit pages.
- Re-running `eva-md init` or setup after a failed signed commit fails on files git ignores, such as `.DS_Store`, and the error does not name the file.

## Tests

- Deadline-based hook tests can fail on a busy machine. A rerun normally passes.
- `TestGitDoesNotRunFSMonitor` cannot fail, because none of the git commands identity runs would start fsmonitor even without the hardening flag.
