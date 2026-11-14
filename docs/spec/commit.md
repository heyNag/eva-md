# The daily commit and push

- **Component:** Maintain / committer
- **Paths:** `internal/wikigit`, `internal/lint`, `cmd/eva-md/commit.go`, `cmd/eva-md/maintain.go`
- **Status:** v1 as built, frozen on 2026-11-14

The blocks below are v1's design and verification text, moved here word for word when the two documents were split; [INDEX.md](INDEX.md) lists every spec.

<!-- v1 docs/design.md 012 -->
## Daily commit and push

Built in `internal/wikigit` and run by `eva-md maintain`, or by hand with `eva-md commit`.

1. **Trigger.** With a wiki path and an instance in the config, or with no wiki at all, for maintain's search index step, the first hook event of the local day that passes the guards creates `<state>/commit/attempt-<date>` with O_EXCL once its output is written, and spawns `eva-md maintain` detached
   like the update (see "Background update"), with SSH_AUTH_SOCK, GNUPGHOME, GIT_SSH, GIT_SSH_COMMAND, GIT_CONFIG_GLOBAL, GIT_CONFIG_NOSYSTEM and XDG_CONFIG_HOME kept as well, so git can sign and push.
   A capture that misses its deadline spawns nothing and leaves the day unused.
   A spawn that fails is ignored, and the day's commit waits for the next day or a manual run.
2. **maintain** takes `<locks>/maintain.lock` without waiting (a busy run exits 0), builds the files that list the knowledge pages again when they are out of date (see "Knowledge step" below), runs the commit and push unless `commits = false`, then the lint (`internal/lint`, which records thread
   pages over 512 KB and refs the inbox no longer resolves in `<state>/lint.json`), then takes the code status of the system pages, as `eva-md system status` does (a wiki without system pages has none to take, and only one that can't be read fails the step), then removes attempt markers older than 14 days.
   It prints at most one line a step, counts and reasons only; the knowledge step prints one only when it rebuilt something or found the wiki busy.
3. Take the commit lock without waiting (a busy run is recorded as `busy`), then the wiki lock, waiting up to 60 s for running writes.
4. **Guards**, in this order, each a refusal with its reason: no wiki; an origin or push URL other than the configured remote (`origin`); an `instance` in `.eva-md/schema.toml` other than the config's; a `schema_version` other than 1, as a newer eva-md writes (`schema`); a `.git/index.lock`
   (`index_lock`, recorded with its age and never deleted); a rebase, merge, cherry-pick or revert in progress (`rebase`, `merge`, `cherry_pick`, `revert`); unmerged paths in the index, which `git ls-files --unmerged` lists, as a conflicted `git stash pop`, `git checkout -m` or `git apply --3way`
   leaves them with no state file (`conflicts`), since `git add` would mark them resolved, conflict markers and all; a detached HEAD (`detached`).
5. `git add` the explicit paths that exist: projects, general, systems, knowledge, index.md, log.md, README.md and .eva-md, which holds the names index, the repos index and the recall index.
   A path gone from disk that git still tracks (`git ls-files` lists it), such as a knowledge folder removed whole, is added too, so its removal is committed; one git doesn't know is left out, as `git add` would fail on it.
6. Read the staged names and leave out, recording each path in the run's `unstaged` list: paths outside those, symlinks and submodules, `.jsonl` files, inbox paths, `.tmp-okf-*` and `.tmp-eva-*` files, and the secret hits of step 8. A left-out path still staged when the index is read again fails the run (`unstage`).
7. When anything is left to scan, run okf Validate (Drift on, Strict off) and record its error count. Errors never stop the commit: saving memory matters more than a clean report.
8. **Secret scan** of the staged diff, line by line (see the Redactor row): a file with a flagged added line, a binary file or an added line over 1 MiB is left out.
   Nothing is redacted in place. git runs without the variables that change how it reads a pathspec (`GIT_LITERAL_PATHSPECS`, `GIT_GLOB_PATHSPECS`, `GIT_NOGLOB_PATHSPECS` and `GIT_ICASE_PATHSPECS`), as it runs without those that would point it at another repository, so the literal paths steps 5, 6 and 8 name mean what they say whatever your shell exports.
9. Release the wiki lock. With nothing staged, the run's outcome is `nothing`, and it pushes only when HEAD is ahead of its upstream.
10. `git commit -m "eva-md daily 2026-01-15: 12 pages"` ("1 page" for one; knowledge pages and the knowledge map count, index files and the root log.md don't) with your signing as configured, and a 300 s timeout, since an approval can take a while.
    A failed commit is `failed` with a reason that git's diagnostics sort, without keeping their text: `commit_signer_missing` when git can't start the signing program, as one a desktop app's short PATH doesn't reach; `commit_signature` when the signature failed or was declined; `commit_timeout`; or `commit` for any other failure.
    It is not retried until the next day or a manual `eva-md commit`.
11. `git push --porcelain origin HEAD` right after, with a 60 s timeout, never forced. The result is `ok`, `up_to_date`, `diverged` for a non-fast-forward, which waits for a person, `failed`, or `pending` when the host is out of reach, the push timed out or wasn't approved, or a local remote is missing.
12. Append the run to `<state>/commits/<YYYY-MM>.jsonl` (0600): `at`, `outcome` (committed, nothing, refused, failed or busy), `reason`, `pages`, `validate_errors`, `unstaged`, `push` (ok, up_to_date, pending, diverged, failed or skipped) and `lock_age_s`. Counts, paths and reasons only.

**Push retry.** At a chat start with commits on, unless this hook just started the day's maintain, the hook checks for a pending push at most every 30 minutes: between checks it costs one stat of `<state>/commit/push-retry`; a check reads up to 16 KB of each of the last two months' commit records and sets that marker.
The newest record that is neither busy nor a run with nothing to commit that skipped its push decides: only `pending` is retried, by spawning `eva-md maintain --push-only`, which pushes only while that is still so.
Refused runs, failed commits and `diverged`, `failed`, `ok` or `up_to_date` pushes are never retried.
A branch without an upstream, such as a wiki set up with `--no-push`, never pushes on a day with nothing to commit; only this retry pushes it.

`commits = false` keeps the commit, the push and the retry out of maintain, which still runs the lint, the code status and the search index; `eva-md commit` by hand ignores it. The lines page saves add to log.md go out with the day's commit.

**Knowledge step.** First, unless `--push-only`, when `knowledge/` exists, or a recall index or root index line left from it does (`knowledgepage.Report.Exists`), and eva-md may write the wiki, its schema being schema_version 1 and naming the config's instance (`health.WritableWiki`, the doctor's
gate), maintain compares the knowledge map, `knowledge/index.md`, the recall index and the root index's line with what the pages give, and builds those out of date again under the wiki lock (`knowledgepage.Refresh`), so the same day's commit holds them.
It prints `maintain: knowledge: rebuilt the index (N pages)`, N counting every knowledge page, only when it wrote something, and nothing otherwise.
A wiki lock that stays busy prints `maintain: knowledge: the wiki stayed busy; the next run builds the index again` and fails nothing; any other error goes to stderr as `eva-md: knowledge: <error>` and makes maintain exit 1.

**Recovery records.** After the attempt markers, maintain removes the transcript-recovery records, `<state>/catchup/<host>/<session>.json`, not saved for 30 days, and those past the newest 2,048 of their host, newest by when each was last saved, so the chat-start catch-up, which lists at most 4,096 a host, still finds the newest; it never removes the drift records beside those folders.
The hooks save a record only under its session's lock, so maintain removes one only while it holds that lock, taken without waiting and by the session the record names, and only when the record is still the one it listed, which it checks once it holds that lock: a record whose session a hook holds, or that a hook saved again meanwhile, even after maintain read it, stays for the next run.
A record it can't read, which no hook saves again, goes as listed.
A chat whose record is gone starts again at its transcript's end at its next event, as a new chat does: nothing is read twice, rows its hooks missed before then are never recovered, and whether it is off the record still comes from the session's marker, so the record of a chat off the record stays
while that marker isn't set, as when it couldn't be written, until recovery writes it at the chat's next pass.
It prints `maintain: transcript recovery: removed N records` only when it removed some; a record it can't remove waits for the next run.
The hooks never prune.

**Search index.** Maintain brings the search index up to date (see "Search index"), last when a wiki is set up, and prints `maintain: index: ...`, such as `2 files (3.1 MB) indexed in 1 project, 12.0 MB in all` or `up to date (12.0 MB)`, or `busy`; a failure, such as a disk too full to write a segment, exits 1.
Without a wiki, maintain runs only this step, then the pruning of attempt markers and recovery records, so a capture-only install gets a daily index too; the hooks start it there once a day, but never a push retry, and a wiki path without the instance init sets still starts nothing.
`--push-only` without a wiki is refused, as before.

Plain git only. A wiki on a self-hosted server that is out of reach just leaves its push pending.

<!-- v1 docs/verification.md 029 -->
- **Daily commit, push and lint** (`internal/wikigit`, `internal/lint`, `cmd/eva-md`, `internal/hook`).
  A commit of the commit paths with your signing settings untouched; refusals for another origin or push URL, another instance, an `index.lock`, a rebase, merge, cherry-pick or revert, unmerged paths left by a real stash pop that conflicts, with no commit and the edit kept (`TestGuardsRefuse`), and a
  detached HEAD; left-out JSONL, inbox and temp files, paths outside the commit paths, symlinks and secret hits; a busy wiki lock; a push on a run with nothing to commit; a missing remote giving `pending`; a busy push-only run; the lint's oversize pages and bad refs.
  `maintain`: commit and push, the lint, pruning, a busy lock, `commits = false`, and a push-only run turning pending into ok.
  The daily trigger spawns maintain once a day, also with no wiki at all (for the index step), never for a wiki path without the instance init sets or in the headless child, and not after a capture that missed its deadline (`TestLateCaptureStartsNoMaintain`); the push retry's spacing, never retrying a diverged, refused, declined or pushed run, passing over busy and no-op runs.
  A GitLab set with a fake ssh and an unreachable host: all three remote forms give stable project keys and pass init's check, init fails cleanly and works when rerun, and a pending push is retried to ok.
  Knowledge pages, the knowledge map and index and the recall index are staged and committed, the pages and the map counting as pages (`TestCommitsKnowledgePages`), and a knowledge folder removed whole is committed as removed with the next day's changes, leaving nothing uncommitted (`TestCommitsARemovedKnowledgeFolder`).
  Maintain prints nothing for knowledge files that are current, never builds them on `--push-only`, builds nothing in a wiki whose schema names another instance or a newer schema version, and builds a missing recall index before the commit, which then holds it
  (`TestMaintainRebuildsTheKnowledgeIndex`). git's pathspec settings in the environment never reach it, so with `GIT_LITERAL_PATHSPECS` set the secret scan still leaves out a page holding a token and what is never committed is still left out (`TestPathspecSettingsNeverReachGit`).
  The secret scan flags page lines that carry `DB_PASS=`, `MYSQL_PWD=`, a password in prose, a `mysql -p<password>`, a curl `user:password` and `hf_` and `npm_` tokens, and passes lines about passwords that hold none (`TestSecretLineCatchesPasswordShapes`).

<!-- /v1 -->

## Since the split

No change yet.
