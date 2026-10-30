# Known limitations

These are open problems in capture and resume as the code stands. Wiki updates and support for work machines on self-hosted git are planned and not built yet; they are described in [design.md](design.md), not here. What has been verified is in [verification.md](verification.md).

## Redaction

- About 1% of a deliberately hard set of generated passwords still keep a piece after redaction. The shapes are ambiguous by nature: a value containing `)` inside a call, `&k=` or `;k=` inside a URL query or connection-string value, and values that start with an unclosed bracket.
- A UUID or a 40-character hex string after the word Bearer, without an Authorization header, is kept as in ordinary text.
- The entropy rule erases ordinary camelCase identifiers that follow words such as token, secret or password, as in `token = maxCompletionTokensPerRequest`. Prose is over-redacted too: `Password: expired yesterday` loses its value.
- When `redact.Identifier` replaces part of a branch name or project key, it puts an unsalted 8-hex hash of the original in front. The hash can be checked against guesses.
- Redaction takes about twice as long as the baseline rules alone, because `redact.Redact` runs both rule sets. Very long recovered replies are therefore more often cut to head and tail windows.

## Capture, identity and config

- `origin` is read from `remote.origin.url` as configured, without `url.<base>.insteadOf` rewrites, so a clone through an alias and a clone through the full URL get two project keys. Write a remote exclusion in the form origin is configured with; for a repo cloned both ways, exclude both forms.
- A config that fails to load stops capture and briefs on both hosts, with no notice in chat, until it is fixed. `eva-md doctor` and `eva-md status` report it. A remote exclusion written as a URL that can't be normalized, such as a `file://` URL or one with a query string, makes the config fail to load.
- Path exclusions compare bytes, so the NFC and NFD spellings of the same non-ASCII folder name do not match each other.
- A Codex memory-writing run is skipped only for the events whose prompt or folder shows it. Other events of the same run are captured as an ordinary chat.
- Under very heavy CPU load, git's error output can be cut short. A chat in a folder outside any git repo then fails identity, and that event is not captured.

## Transcript recovery

- Start-of-chat catch-up looks at the 256 most recently updated of at most 4,096 recovery records per host, and records are never pruned. Past 4,096 records it can miss this project's abandoned chats, including the newest one.
- Duplicate suppression uses the turn id or, without one, the text and its timestamp. A recovered final reply over 48 KiB that has no turn id can be stored twice.
- Going off the record moves the recovery cursor to the end of the transcript first. On-record replies that had not been recovered yet are lost.
- While a recovery pass is still behind, a hook stores only its event's name, as off-the-record mode does, because the unread rows may hold an off-the-record request. A tool line, a host compaction summary or a compact or first-prompt brief arriving then is dropped without a gap marker.
- Recovered Claude Code prompts are recognised by their `promptId` field. If a host version stopped writing it, typed prompts would no longer be recovered from transcripts; the hook would still capture them.

## Resume brief

- The briefed marker is saved only with 20 ms to spare before the hook's deadline. If its rename and folder sync take longer than that, as they can when the disk is very busy, the brief is lost and not retried.
- Reading the Now page loads the whole wiki through okf. One stray or invalid file anywhere in the wiki removes the Now section from every brief, and the brief says no page exists.
- "Tool activity" is the last tool record, not the last test or build command and the files touched.
- The space reserved for the brief does not count growth from cleaning and fencing, so label-like lines can still cut the newest reply or the `eva-md show` footer.
- Refresh, resume and fork briefs repeat everything instead of only what changed, so a dirty tree or an old stash makes every resume brief non-empty.
- Source refs such as `s:<8 characters>#<line>` use the first 8 characters of the session id. Codex ids are time-ordered and can share them. `eva-md show` then reports the ref as ambiguous; the full session id in place of the 8 characters resolves it.

## Install and uninstall

- Codex keeps its hook approvals after uninstall, so a reinstall with identical hooks may not ask for approval again.

## Health check

- Chats in excluded, temporary or wiki folders write no heartbeat but do create host transcripts. The capture-freshness check can then report a false FAIL and a false "no matching capture heartbeat" notice.
- The capture-freshness scan gives up after 4096 entries or 512 folders in a host's chat store. Once a store is that large, the check stays UNKNOWN and no longer flags a trust lapse.
- Background updates and daily commits are reported as UNKNOWN until wiki updates are built.
