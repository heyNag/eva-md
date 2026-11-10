# Storage audit: the okf adapter, the wiki's git runner, wiki init, the daily commit and push, and maintain

Scope read in full (non-test): `internal/okfadapter/{adapter,head}.go`, `internal/wikigit/{git,commit,scan}.go`, `internal/wikiinit/{init,git}.go`, `cmd/eva-md/commit.go`, `cmd/eva-md/maintain.go`, `internal/hook/maintain.go`, plus `internal/hook/spawn.go` (how maintain is spawned) and the parts of `internal/health/background.go`, `cmd/eva-md/setup.go`, `internal/update/wiki.go`, `internal/knowledgepage/save.go` and `internal/lint/lint.go` that this part's behaviour depends on. Tests read for coverage: all test names in the three packages, and `TestDivergedRemoteIsLeftAlone`, `TestPendingPushThenPushOnly`, `TestMetadataGoldenRoundTrip`, `TestFullFixtureWiki` in full. The okf library was read at the pin, `$GOMODCACHE/github.com/okf-memory/okf-agent-memory@v0.8.0/pkg/okf/{lock,mutate}.go`. Docs: `docs/design.md` 110-198 and 612-640, `docs/known-limitations.md` 221-235, `docs/decisions.md` 20-94.

Nothing was run except read-only shell commands (grep, sed, wc, go list, git log).

Everything below is "verified in code" unless a sentence says "inferred".

---

## 1. How it works

### 1.1 The page store (internal/okfadapter)

- **One adapter.** `internal/okfadapter` is the only non-test package that imports okf (`grep` finds the import only in `adapter.go`, `head.go` and tests). It uses exactly these okf identifiers: `ParseConcept`, `SerializeConcept`, `SaveConcept`, `SaveOptions`, `IsValidActor`, `ValidateConceptID`, `InitBundle`, `LoadBundle`, `Validate`, `ValidateOptions`, `ExtractFrontmatter`, and the types `Concept`, `Generated`, `Verified`, `Source`, `ValidationResult`, `Bundle`. okf's non-test code in `pkg/okf` is 5,679 lines, all compiled into the binary (`go list -deps ./cmd/eva-md` lists the package).
- **Open** resolves symlinks, requires a regular root `index.md`, and reads no pages (`adapter.go:90-100`, `540-563`). Cost is constant in wiki size, which the hooks depend on.
- **Read** is `open(O_NOFOLLOW|O_NONBLOCK)` + `okf.ParseConcept`, with a SHA-256 of the raw bytes kept for compare-and-swap (`adapter.go:128-142`, `701-731`). `ReadMax` refuses a page over a byte limit without reading it; `ReadHead` reads the frontmatter and the body up to the first line holding a marker, in 64 KiB pieces, never past `max` (`head.go:23-99`). Both exist so a hook reads bounded data.
- **Create** (`adapter.go:148-188`): take the wiki lock; validate the id and refuse symlinks in the path (`locate`, `578-592`); `Lstat` must say the file is missing (`ErrExists` otherwise); check the actor; `MkdirAll` the folder at 0700 because okf would use 0755; remove a leftover `.okf-write.lock`; `okf.SaveConcept(IsNew, AutoLog:false, AutoIndex:false)`; fsync the folder and every parent up to the root; append one line to `log.md`.
- **Update** (`adapter.go:195-244`): take the wiki lock; re-read; `ErrConflict` when the hash differs from the caller's; parse a fresh copy; run the caller's `render`; refuse a changed id or path; skip the save when the result equals the file apart from the `generated` line (`229-231`, with `serializeAsSaved`/`withoutGenerated` at `823-856` mirroring okf's own trimming); clear a leftover okf lock; `okf.SaveConcept`; fsync the folder; append to `log.md`.
- **Locked** (`adapter.go:108-117`) hands the callback a copy of the `Wiki` with `held = true`, so several writes share one hold of the lock (a system page with its map and index, a pass's knowledge pages with map, folder index, recall index and root index line).
- **The wiki lock** is `flock(LOCK_EX|LOCK_NB)` on `<state>/locks/wiki.lock`, polled every 10 ms for up to `LockTimeout` = 60 s, then `ErrLockBusy` (`adapter.go:62-65`, `861-909`). The kernel releases it when the process dies.
- **Non-page files** (`index.md` files, `log.md`, `.eva-md/*.tsv`, `.gitignore`, `.gitattributes`) go through `WriteFile`/`RemoveFile`: path checks, refusal of `.git` and of any other `.md`, temp file `.tmp-eva-*`, fsync, rename, fsync of folders up to the root (`adapter.go:301-337`, `628-699`). Markdown is 0644, anything else 0600.
- **log.md** is rewritten whole on every page save: read up to 16 MiB, append `* **Creation**: ...` or `* **Update**: Updated concept ...` to the local date's group at the end, once a day per identical line; a missing, linked or oversize log silently gets nothing (`adapter.go:247-294`). Nothing in eva-md reads `log.md` (grep: only the adapter, the commit path list and init touch it).
- **Init** (`adapter.go:381-427`) writes eva-md's own `log.md` and root `index.md` first so that `okf.InitBundle`, which is still called, writes nothing (its own index would name okf).
- **Validate** (`adapter.go:358-371`) is the only call that loads the whole wiki (`okf.LoadBundle` + `okf.Validate{Drift:true, Strict:false}`).
- **Human protections**: `IsLocked` (`739-750`), `IsVerified`/`VerifiedIn` (`757-796`, a hand-rolled frontmatter scan because okf reads `verified` only in two YAML shapes and drops the key when it writes), `SmugglesFrontmatter` (`800-819`, a copy of okf's body check so writers can test text before a save refuses it).

### 1.2 The git runner (internal/wikigit/git.go)

- Every git call is `git -c core.fsmonitor=false -c core.hooksPath=/dev/null --no-optional-locks -C <dir> ...` with `identity.GitEnviron` (no `GIT_DIR`, no config or pathspec variables, prompts off, English messages), a per-call timeout, and `WaitDelay` 200 ms (`git.go:95-112`, `218-226`). `exec.CommandContext` with no `Cancel` set means a timeout kills git with SIGKILL (`git.go:99-101`).
- stdout is capped at 1 MiB (64 MiB for the staged-file list) and stderr is only classified, never stored or shown (`git.go:228-261`). Classification is by English substrings: `signer_missing`, `signature`, `timeout` for commits (`117-126`), and eight "couldn't read the repo" reasons for setup (`132-216`).
- `RemoteIdentity` refuses remote helpers, option lookalikes, passwords, queries and fragments and never echoes the URL (`git.go:268-301`).

### 1.3 Wiki init (internal/wikiinit)

`Run` (`init.go:46-266`):
1. A folder is required and has no default (`53-55`); a remote is required and must pass `RemoteIdentity` (`60-63`). The folder may not be the home folder, Documents, Desktop, Downloads, iCloud Drive, `~/Library/CloudStorage`, or eva-md's own folders (`443-482`).
2. Take `<locks>/init.lock` without waiting (`67-71`, `532-541`). Refuse when the config already names another wiki (`72-78`).
3. `git clone --no-local` into an empty or missing folder, never deleting a failed clone (`79-98`). Require a standalone clone whose top level is the folder and whose origin and every push URL are the requested remote (`99-134`).
4. No schema: the repo must hold only `.git` and no HEAD; make `instance = <machine label>-<8 hex>` and scaffold through the adapter (`152-176`, `268-309`). The scaffold writes `.gitattributes` = `log.md merge=union` + `*.md text eol=lf` and `.gitignore` = `*.jsonl`, `/inbox/`, `.eva-md/tmp/`, `.tmp-*`, `.DS_Store` (`300-301`).
5. Schema present (adoption): require `schema_version == 1` and, when the config has an instance, the same instance (`177-184`).
6. Run okf Validate. A new scaffold must have no findings; an adopted wiki is refused on any error, orphan, broken link, or an actor or parent-index warning (`185-197`).
7. With a HEAD: clear a leftover okf lock, refuse any uncommitted change (`198-213`). Without one: check that only scaffold files exist, `git add` them, check the staged names, `git commit -m "Initialize eva.md wiki"` with the user's signing (`214-234`).
8. Unless `NoPush`: `git push --set-upstream origin HEAD:refs/heads/<branch>`, after asking through `BeforePush` when the remote has never had the branch (`235-255`). A failed push fails init.
9. Save the per-machine config: wiki path, expected remote identity, instance, machine label (`256-265`).

Every git call has a 120 s limit (`wikiinit/git.go:12`).

### 1.4 The daily commit and push (internal/wikigit/commit.go, scan.go)

`Commit` (`commit.go:93-169`):
1. Take `<locks>/commit.lock` without waiting; a held lock is outcome `busy` (`115-133`, `520-530`).
2. Take the wiki lock, waiting up to 60 s (`136-143`, `535-561`). wikigit opens the same file the adapter flocks, by path convention; `TestWikiLockIsTheAdapterLock` pins that.
3. **Guards** (`249-293`), each a refusal with a reason: not an absolute path with a `.git` folder (`wiki`); origin or any push URL not the configured identity (`origin`); schema instance not the config's (`instance`); `schema_version != 1` (`schema`); `.git/index.lock` present, recorded with its age and never deleted (`index_lock`); `rebase-merge`, `rebase-apply`, `MERGE_HEAD`, `CHERRY_PICK_HEAD`, `REVERT_HEAD` (`rebase`, `merge`, `cherry_pick`, `revert`); unmerged index entries (`conflicts`); detached HEAD (`detached`).
4. `git add -- projects general systems knowledge index.md log.md README.md .eva-md`, each only when it exists or is still tracked (`58`, `180-201`).
5. Read the staged list (`git diff --cached --raw --no-renames -z`). Leave out: paths outside the commit paths, symlinks, submodules, `*.jsonl`, `inbox/`, `.tmp-okf-*`, `.tmp-eva-*` (`361-367`).
6. Run okf Validate over the whole wiki and record only `len(Errors)`; errors never stop the commit (`214-216`, `399-409`).
7. **Secret scan**: one `git diff --cached -U0 -- :(literal)<file>` per staged file, streamed; a file is left out when an added line trips `redact.Redact` (with eva-md's own identifiers neutralised first), when the diff is binary, when a line is over 1 MiB, or when git fails (`scan.go:15-138`).
8. `git reset --quiet -- <left-out paths>` in batches of 100, then read the index again and fail (`unstage`) when a left-out path is still staged (`222-235`, `371-384`).
9. Release the wiki lock (`147`). Nothing staged: outcome `nothing`, and push only when `@{upstream}..HEAD` is not empty (`152-158`, `472-475`).
10. `git commit --quiet -m "eva-md daily <date>: N pages"` with a 300 s limit and the user's own signing settings (`159-166`). Failure reasons: `commit_signer_missing`, `commit_signature`, `commit_timeout`, `commit`.
11. `git push --porcelain origin HEAD` with a 60 s limit, never forced (`479-483`). `pushStatus` (`488-516`): `! [rejected]` is `diverged`; any other `!` is `failed`; a non-zero exit with no ref line is `pending`; ` ` or `*` is `ok`; `=` is `up_to_date`.
12. Append one JSON line to `<state>/commits/<YYYY-MM>.jsonl` (0600): time, outcome, reason, page count, validation error count, left-out paths, push result, lock age (`71-80`, `589-610`).

`Push` (`103-110`) is the same guards without the commit-only ones, then the push; its record has reason `push_only`.

Time limits: `GitTimeout` 120 s, `CommitTimeout` 300 s, `PushTimeout` 60 s, `wikiLockWait` 60 s (`48-55`). Output limits: 1 MiB per git call, 64 MiB for the staged list, 1 MiB per diff line.

### 1.5 Triggers and maintain

- Every captured hook event queues `maintain`; a `SessionStart` with commits on and a wiki also queues `maintain --push-only` (`hook/hook.go:360`, `hook/maintain.go:39-47`).
- After the hook's output is written, `launch` decides what is due (`hook/spawn.go:19-36`). The daily job is due only for the event that creates `<state>/commit/attempt-<YYYY-MM-DD>` with `O_EXCL` (`hook/maintain.go:70-81`). The push retry is due when this hook did not start the daily job, the `push-retry` marker is 30 minutes old or older, and the newest meaningful commit record's push is `pending` (`88-105`, `health/background.go:391-401`).
- `detach` starts `<share>/bin/eva-md maintain` in a new session with stdin from `/dev/null`, output appended to the day's update log, the update's allow-listed environment plus `SSH_AUTH_SOCK`, `GNUPGHOME`, `GIT_SSH`, `GIT_SSH_COMMAND`, `GIT_CONFIG_GLOBAL`, `GIT_CONFIG_NOSYSTEM`, `XDG_CONFIG_HOME` (`hook/spawn.go:47-90`). A failed spawn is ignored.
- `runMaintain` (`cmd/eva-md/maintain.go:45-109`): take `maintain.lock` without waiting (busy exits 0); rebuild the knowledge map, index and recall index when stale; commit and push unless `commits = false`; lint; code status of system pages; prune attempt markers older than 14 days and old transcript-recovery records; refresh the search index. `--push-only` runs only the push, and only while a push is pending. Without a wiki it runs the index step and the two prunes.
- `eva-md commit [--push-only]` runs the same `wikigit.Commit`/`Push` by hand and prints one line (`cmd/eva-md/commit.go:19-84`).

### 1.6 What is written where

| What | Where | Written by |
| --- | --- | --- |
| Pages (`*.md` with frontmatter) | the wiki's working tree | `Create`/`Update` through `okf.SaveConcept`, 0644 |
| Index files, `log.md`, `.eva-md/*.tsv`, `.gitignore`, `.gitattributes` | the wiki's working tree | `WriteFile`, atomic |
| `.eva-md/schema.toml` | the wiki | `config.SaveSchema` at init |
| `.okf-write.lock`, `.tmp-okf-*` | the wiki's root and page folders | okf, transient; the adapter removes a leftover lock |
| `wiki.lock`, `commit.lock`, `maintain.lock`, `init.lock` | `<state>/locks/` | flock files |
| `attempt-<date>`, `push-retry` | `<state>/commit/` | the hooks |
| `<YYYY-MM>.jsonl` | `<state>/commits/` | every commit or push run |
| Commits | the wiki's `.git`, then `origin` | once a day |

---

## 2. How memory moves between two machines that share one wiki repository

**Nothing ever pulls, fetches, merges or rebases.** The only git subcommands the wiki's code runs after setup are `config --get`, `remote get-url`, `ls-files`, `add`, `diff --cached`, `reset -- <paths>`, `symbolic-ref`, `rev-list`, `rev-parse`, `status`, `commit` and `push` (`grep` over non-test Go: no `fetch`, `pull`, `merge` or `rebase` argument exists outside setup's one-off `clone` and `ls-remote`). `TestDivergedRemoteIsLeftAlone` asserts that a diverged run did not pull (`wikigit/commit_test.go:494-518`). `docs/design.md:1240` states the intent: "No API calls and no syncing between machines."

What the product does about a second Mac:

1. **It discourages it.** Setup reads the repo's schema from a shallow copy, and when the instance's machine label is not this Mac's it asks "This wiki was set up on X. One wiki per machine: use it here only if X no longer pushes to it. Use it? [y/N]" with No as the default (`cmd/eva-md/setup.go:752-757`). Without a terminal it adopts without the warning (`docs/known-limitations.md:254`).
2. **If the user says yes**, init clones the repo once and saves the same `instance` into this Mac's config (`wikiinit/init.go:79-98`, `177-184`, `256-265`). The "instance guard" therefore passes on both Macs: it identifies the wiki, not the machine.
3. **The second Mac sees a snapshot.** Its hooks, brief, recall and update read the working tree as cloned. No later commit from the first Mac ever reaches it.
4. **Whoever pushes second loses the remote.** The first daily push after the other Mac's push is a non-fast-forward: `! [rejected]` becomes `diverged` (`commit.go:497-498`). `RetryPush` never retries it (`health/background.go:391-401`). The Mac keeps making local commits every day that go nowhere.
5. **What the user is told**: the doctor's push row FAILs with "merge them by hand in the wiki, never with a force push, then run eva-md commit --push-only" (`health/background.go:354-355`), and every chat start on that Mac carries the notice "eva.md: the wiki's remote has commits this clone lacks, so pushes stopped; run eva-md doctor." (`health/background.go:439-440`).
6. **A hand merge conflicts almost everywhere**, because the data model assumes one writer:
   - Knowledge pages are numbered sequentially from the local folder's highest id (`knowledgepage/save.go:249-250`, `s.next++`), so both Macs create `knowledge/k17.md` for different facts: an add/add conflict on every page made since the clone.
   - There is one thread page per project and branch (`projects/<slug>/threads/<branch>.md`, `docs/design.md:214`), so two Macs working on `main` both rewrite the same Now section and Timeline.
   - Every save rewrites the `generated` frontmatter line with a new time (okf `mutate.go:508-511`, `saveConceptLocked`), so any page both sides touched conflicts on that line even when the bodies merge.
   - The derived files are committed: every `index.md`, `knowledge/map.md`, `systems/map.md`, `.eva-md/knowledge.tsv`, `.eva-md/names.tsv`, `.eva-md/repos.tsv` (`commit.go:58`). Both sides regenerate them, sorted, so adjacent-line conflicts are the norm.
   - Only `log.md` has a merge rule (`merge=union`, `wikiinit/init.go:300`).
7. **While the merge is unresolved**, the commit refuses (`merge` or `conflicts`, `commit.go:276-287`), but the adapter has no knowledge of git state, so the update job keeps writing pages into the conflicted tree and hooks keep reading pages that may hold conflict markers (inferred from the absence of any git check in `adapter.go`).
8. **Evidence does not travel.** Pages cite `s:<session>#<line>` refs that resolve only through the capturing Mac's inbox, which is never committed (`.gitignore` `*.jsonl` and `/inbox/` at `wikiinit/init.go:301`; `leftOut` at `commit.go:361-367`). On the other Mac every such ref is an "elsewhere" ref the lint can only count (`lint/lint.go:43-51`, `234-250`).
9. **No page records which machine wrote it.** The machine label lives only in the local config (`config.go:54`; its only readers are init and setup). `generated.by` is `<host>/<model>`.

Net: git is a write-only, once-a-day backup of one machine's memory. A second Mac, a cloud or remote session, a container and a teammate cannot share memory through it.

---

## 3. What must be kept

1. **One boundary for page I/O.** Every page write goes through `Create`/`Update` with no-clobber create, hash compare-and-swap, and a no-op skip when only `generated` would change (`adapter.go:148-244`). The no-op skip is what keeps daily diffs meaningful.
2. **A lock a dead process cannot leave behind**: flock in the state folder, not a file in the wiki (`adapter.go:861-909`). The okf v0.8.0 upgrade showed what the alternative costs (two fix commits, `29bf92f` and `6bf7b5f`).
3. **Durability order**: temp file, fsync, rename, fsync of the folder (and of new parent folders) before a caller advances a watermark (`adapter.go:183`, `239`, `653-699`, `911-939`).
4. **Path and link safety**: `O_NOFOLLOW` reads, a walk that refuses any symlink in a page's path, refusal of `.git`, control characters and `..`, a root that must still be the same real directory (`adapter.go:578-651`, `701-726`, `358-365`).
5. **Bounded reads for hooks**: `Open` reads no pages; `ReadMax` and `ReadHead` never read past a limit (`adapter.go:85-100`, `128-142`; `head.go`).
6. **Groups of writes land together** under `Locked`, and the commit stages under the same lock, so a commit never captures half a group (`adapter.go:108-117`; `commit.go:136-147`).
7. **Human protections**: locked and verified pages are detectable in every YAML shape a person might type (`adapter.go:737-796`), and a body that would be read as frontmatter is refused before the save (`798-819`).
8. **The hardened git runner**: hooks and fsmonitor off, no prompts, an environment that cannot redirect git to another repository or change pathspec meaning, capped output, diagnostics classified but never stored or shown (`git.go:95-112`, `218-261`).
9. **Memory can only be pushed to the configured remote**: origin and every push URL are compared by normalized identity on every commit and push; credential-bearing URLs and remote helpers are refused; never a force push (`commit.go:249-266`, `297-330`, `479-483`; `git.go:268-301`).
10. **Commit gates**: an explicit list of paths; symlinks, submodules, JSONL, inbox and temp files never committed; a per-line secret scan of what the commit adds, where binary or unreadable counts as a secret; the index read again after unstaging; literal pathspecs (`commit.go:174-240`, `361-387`; `scan.go`).
11. **The writers and the commit share one secret test** (`SecretLines`, `scan.go:87-98`), so a writer can refuse a line rather than have the commit drop the page.
12. **Never commit over a state it did not make**: an in-progress merge, rebase, cherry-pick or revert, unmerged paths, a detached HEAD (`commit.go:267-292`).
13. **Records hold counts, reasons and wiki-relative paths only** (`commit.go:68-80`).
14. **Hooks do no git and no network**: one `stat`, one `O_EXCL` create, a detached spawn after the output is written (`hook/maintain.go`, `hook/spawn.go`).
15. **The user's signing and sign-in settings are never changed or bypassed**, and a failed signature is never retried in a loop (`wikiinit/init.go:43-45`; `commit.go:159-166`).
16. **The wiki folder is kept out of folders macOS protects from background processes and out of cloud-sync folders** (`wikiinit/init.go:443-482`).
17. **A commit the remote has never had is shown before the first push** (`wikiinit/init.go:235-254`).
18. **The page format stays plain Markdown with small frontmatter that round-trips byte for byte**; `TestMetadataGoldenRoundTrip` and the fixture wiki test are the conformance tests a rebuilt codec must pass (`adapter_test.go:484-553`).
19. **Saving memory beats a clean report**: validation errors are counted, never a reason to skip a commit (`commit.go:214-216`).

---

## 4. Findings, ranked by consequence for the goal

### sync-1. Git is a write-only backup: nothing pulls, so memory cannot follow the user to a second machine, a cloud session or a team (critical; reach; known)

- **Evidence.** No fetch, pull, merge or rebase exists in the wiki's code (section 2). `commit.go:497-498` turns a non-fast-forward into `diverged`; `health/background.go:391-401` never retries it; `354-355` and `439-440` tell the user to merge by hand and put a notice into every chat start. Setup's own prompt says "One wiki per machine" (`setup.go:756`). `docs/known-limitations.md:223` lists "eva-md never pulls, rebases or forces".
- **Impact.** The owner's targets include Claude Desktop, Codex, and by extension every place an agent runs. A user with a laptop and a desktop gets two unrelated memories or one permanently broken push. Cloud sessions, devcontainers, SSH hosts and teammates get nothing. A "best memory system" whose memory stays on one disk is not one.
- **Direction.** Treat the wiki as a replicated store. Fetch and integrate before every push and in the background at chat start (never in a hook); push with a bounded fetch-merge-retry loop; give every machine a writer id recorded in pages and commits; let a fresh environment bootstrap by cloning read-mostly. This needs model-1 first, or the merges conflict.
- **Action.** Rebuild (`wikigit.Commit`/`Push` become a sync engine). Basis: verified.

### model-1. The wiki's data model is single-writer, so any merge conflicts almost everywhere (critical; architecture; not in the docs)

- **Evidence.** Sequential knowledge ids from the local maximum (`knowledgepage/save.go:249-250`); one thread page per project and branch; `generated.at` rewritten on every save by okf (`mutate.go:508-511`); committed derived files (`commit.go:58` includes `.eva-md`, every `index.md`, the maps); `merge=union` only for `log.md` (`wikiinit/init.go:300`); one machine-named `instance` in the shared schema (`init.go:172`); duplicate detection only against the local tree (`save.go:243-246`).
- **Impact.** Adding `git pull` to today's layout would produce conflicts on nearly every file both machines touched, and silent duplicates where it did not. This is the real blocker behind sync-1, and it also blocks team memory and offline work on two machines.
- **Direction.** Make every write commute: collision-free ids (ULID or content hash) for facts; thread state stored as per-writer, append-only operation files or merged by item id with a merge driver built into the binary (the update already re-applies operations by item id after `ErrConflict`); no volatile timestamps in merged files; derived indexes, maps and TSVs regenerated locally and kept out of git, or regenerated after every merge.
- **Action.** Rebuild (the on-disk layout and id scheme, with a one-time migration). Basis: verified.

### onboard-1. Memory cannot start without a user-chosen folder and a git remote (high; invisibility; not listed as a limitation)

- **Evidence.** `wikiinit/init.go:53-55` refuses an empty folder choice ("there is no default location"); `60-63` refuses a missing remote; `commit.go:298-300` makes a wiki with no configured remote refuse every commit (`origin`). The folder may not be in Documents, Desktop, Downloads, iCloud or cloud storage (`init.go:458-466`). Setup then asks to approve a signed first commit and a push (`setup.go:504-524`).
- **Impact.** Between "install" and "the agent remembers" the user must have or create a private repo on a git host (with gh or glab signed in, or a bare repo made by hand), pick a folder, answer several questions and approve a signature and a push. Until then eva-md is capture-only: no thread pages, no knowledge, no recall. That is the opposite of "built in".
- **Direction.** Local-first by default: at install, create the store silently in eva-md's own data folder as a local git repository with no remote, so the first chat is remembered. Make "connect a remote" (backup and sync) an optional, later, one-step action that can also be done by the agent on request. Keep a user-facing checkout as an option, not a precondition.
- **Action.** Rebuild (init's flow and the commit's remote guard, which must allow "no remote yet"). Basis: verified.

### durability-1. Memory leaves the machine at most once per active day, with no same-day retry (high; reliability-safety; partly known)

- **Evidence.** The daily job is due only for the event that creates the day's marker (`hook/maintain.go:70-81`); the marker is created before the spawn, and a failed spawn is ignored (`hook/spawn.go:26-35`). A busy `maintain.lock` exits 0 (`cmd/eva-md/maintain.go:65-69`). A failed commit "is not retried until the next day" (`docs/design.md:625`). Only a `pending` push is retried, at chat starts 30 minutes apart (`hook/maintain.go:23`, `88-105`). Pages written after the day's first event wait for the next active day's first event.
- **Impact.** Friday's work is committed and pushed on Monday morning, if a chat starts. A lost or wiped Mac loses every page since the last successful push, and a spawn failure, a busy lock, an unseen signing prompt or a missed deadline each burn a whole day. The user sees nothing unless they run the doctor.
- **Direction.** Decouple "record" from "publish". Commit locally after every update (cheap, no prompt, see signing-1) and push opportunistically with backoff from a small durable job queue, not from "first event of the day" markers. Track and expose one number: the age of the oldest change that is not yet off the machine.
- **Action.** Rebuild (the trigger and retry scheme in `hook/maintain.go` and `cmd/eva-md/maintain.go`). Basis: verified. Known: `docs/known-limitations.md:225-226` lists the missed spawn and the 30-minute retry, not the exposure.

### signing-1. The daily commit puts a human in the loop: the user's own signing and sign-in prompts, and a human fix when they fail (high; invisibility; partly known)

- **Evidence.** `git commit` runs with the user's signing settings and a 300 s limit "since an approval can take a while" (`commit.go:45-52`, `159-166`); the job is detached with no terminal and the agent's socket passed through (`hook/spawn.go:60-75`). Failures: `commit_signer_missing` (fix: set `gpg.program` to a full path), `commit_signature`, `commit_timeout` (fix: run `eva-md commit` and approve) (`health/background.go:329-334`). A push that was not approved is `pending` and tried again at later chat starts (`commit.go:36`, `hook/maintain.go:88-105`).
- **Impact.** A user who signs commits with a password manager or a hardware key gets an unexplained approval prompt at the first chat of every day, and possibly again at each push retry. Miss it and that day has no commit; after 2 days a notice enters every chat. A user whose signer is not on the desktop app's PATH gets no commits at all until they edit git config.
- **Direction.** Memory commits should be machine-made records, not statements by the user. Give the store its own identity: local commits by an "eva-md on <machine>" author with a machine key generated at setup (or unsigned when the user allows), and a dedicated deploy key or token for the remote. The user's personal signing is asked for at most once, at setup, to vouch for the machine key.
- **Action.** Rebuild (commit identity and sign-in), with decision 7 reopened on purpose. Basis: verified for the code paths; the frequency of prompts depends on the user's git setup (inferred). Known: `docs/known-limitations.md:224`.

### okf-1. Most of the adapter exists to neutralise the library it wraps; the design's own exit condition is met (high; architecture; not listed as a limitation)

- **Evidence.** Of okf's 5,679 lines, eva-md calls 8 functions. The adapter (1,038 lines, 1,397 of tests) contains: its own lock because okf's is a non-reentrant file in the wiki (`861-909`); removal of okf's leftover lock before every call (`436-452`); rewriting of okf's errors so its file names never show (`454-479`); its own no-clobber and compare-and-swap (`162-166`, `209-215`); a mirror of okf's serializer trimming to detect no-op saves (`821-856`); a copy of okf's body check (`798-819`); a frontmatter scan because okf drops `verified` forms it does not read (`752-796`); an init that pre-writes files so `InitBundle` writes nothing (`373-427`); its own log and index writers. `docs/design.md:129` lists nine okf entry points that must never be called, `155-171` thirteen gaps, `185` "7 of 20 releases changed behaviour in functions the adapter calls", and `187-190` the exit: "eva-md then writes its own pages. That takes a few hundred lines."
- **Impact.** The library sets limits the product does not want: 0644 pages, a timestamp rewritten on every save (a merge hazard, see model-1), bodies refused for `---` blocks, titles refused for `---`, unknown frontmatter not round-tripped, a lock file inside the user's repo. Each upgrade is a risk with no benefit taken, and the merge-safe layout of model-1 cannot be built on a serializer eva-md does not control.
- **Direction.** Own the page codec and store: a parser and serializer for eva-md's handful of page types, atomic write, lock and compare-and-swap in one small package. Keep the on-disk format readable as the same Markdown with frontmatter, and keep the golden round-trip tests. Use okf only in tests, as an optional conformance validator, or drop it.
- **Action.** Rebuild (decision 15 reopened, as the design's own clause allows). Basis: verified.

### ownership-1. The wiki is both eva-md's live store and the user's ordinary clone, so every unusual git state is terminal and needs a person (high; reliability-safety; partly known)

- **Evidence.** Fifteen refusal or failure reasons (eleven guard refusals, four commit failures), each with a hand fix in the doctor (`health/background.go:311-339`): `index_lock` is "never deleted" (`commit.go:268-275`), yet eva-md's own timeouts kill git with SIGKILL (`git.go:96-101`), which is how git leaves that file (inferred from git's behaviour); merges, rebases, conflicts, a detached HEAD and a changed origin all stop commits until fixed; a file the scan leaves out is listed in every run "until handled by hand" (`docs/known-limitations.md:227`); the adapter writes pages whatever state git is in.
- **Impact.** For a user who never opens the wiki folder, one stale lock or one stray binary file dropped into `projects/` silently ends backups, and the only signals are a doctor row and, after days, a line in the agent's context. For a user who does edit the wiki, their git operations race the update job with no coordination.
- **Direction.** Make eva-md the only git actor on its store: a repository it owns under its data folder, where it can safely clear its own stale locks, reset its index, quarantine a refused file and re-clone. Offer people a separate, optional checkout (or `eva-md edit`) whose changes arrive as ordinary commits and are merged like any other writer's (sync-1).
- **Action.** Rebuild (the guard ladder shrinks to "is this my store"). Basis: verified, except the SIGKILL-leaves-lock link (inferred).

### history-1. One lump commit a day gives no undo, no attribution and no audit of what a model changed (medium; memory-quality; not in the docs)

- **Evidence.** The only commit message is `eva-md daily <date>: N pages` (`commit.go:159`). Pages are overwritten in the working tree by every update between commits, so intermediate states never reach git. `log.md` records only that a page changed, once a day (`adapter.go:256-269`), and nothing reads it. The record file holds counts (`commit.go:71-80`).
- **Impact.** When a background model run distorts a thread page or supersedes a correct fact, there is no way to see which chat and model did it, or to revert just that change: the nearest restore point is yesterday's whole wiki, and same-day damage on top of same-day good writes cannot be separated. Trust in memory depends on being able to answer "why does it think that" and "undo that".
- **Direction.** One local commit per update pass, with trailers for session, host, model and the operations applied. That history is then the audit log, the undo mechanism (`eva-md undo <pass>` or a revert by the agent on request), and the operation stream sync needs. `log.md` becomes redundant.
- **Action.** Rebuild (commit granularity), remove `log.md` writing. Basis: verified.

### restore-1. The restore path is stricter than the save path: a wiki the daily commit pushes can be refused when cloned again (medium; reliability-safety; not in the docs)

- **Evidence.** The commit counts okf's errors and commits anyway, and does not even record orphans or broken links (`commit.go:214-216`, `399-409`). Adoption, which is also the "clone the missing wiki again" path (`setup.go:524`), refuses on any error, orphan, broken link or blocking warning (`wikiinit/init.go:185-197`). A thread page is created under one lock hold and its index lines are written in later holds (`update/wiki.go:360-367`, `604-641`), so a commit taken between them, or a killed update, pushes an orphan page.
- **Impact.** The one moment the remote copy matters, a new or wiped Mac, setup can answer "wiki validation has findings; run eva-md validate to inspect" and leave the user to repair Markdown by hand before memory works again. How often a pushed HEAD is in that state is inferred, not measured.
- **Direction.** A restore must always succeed: adopt first, then repair. Make derived files rebuildable from pages (they already are for knowledge) and rebuild them on adoption; treat validation as a report plus automatic repair, never as a gate on reading one's own memory.
- **Action.** Evolve (`wikiinit.Run`'s adoption gate), add a rebuild-all step. Basis: verified for the asymmetry; frequency inferred.

### evidence-1. The sources behind a page stay on the machine that captured them (medium; recall-quality; partly known)

- **Evidence.** Pages cite `s:<session>#<line>`; the inbox is never committed (`wikiinit/init.go:301`, `commit.go:361-367`); the lint has a whole class for refs "this Mac never captured, as another Mac's ... or chats from before this inbox" (`lint/lint.go:43-51`).
- **Impact.** After a reinstall, on a second machine, or after `inbox clear`, a later chat that needs the detail behind a fact (the brief tells the agent to run `eva-md show <ref>`) gets nothing, and a person cannot check where a claim came from. Memory degrades to unverifiable summaries exactly when it has travelled.
- **Direction.** Decide what evidence is allowed to travel and store it with the memory: short redacted excerpts (the cited lines) saved content-addressed beside the fact at promotion time, under the same secret gate, with the raw inbox staying local. Refs then resolve anywhere, to the excerpt at least.
- **Action.** Add. Basis: verified. Known: `docs/known-limitations.md:108` describes the lint's handling, not the loss.

### eval-1. Nothing measures whether memory is safe, current or convergent (medium; evaluation; not in the docs)

- **Evidence.** The storage tests cover one clone against a local bare remote. No test has two writers; the only multi-clone test asserts that nothing is pulled (`commit_test.go:494-518`). No record or doctor row gives the age of unpushed memory. Nothing times staging, scanning or validation as a wiki grows. `AGENTS.md` says "a day's run against that wiki" and the live checks on real hosts "are still pending", so the daily commit has not yet run for a day on a real remote.
- **Impact.** The properties the goal depends on (no loss, bounded delay to off-machine, convergence across machines, a restore that works) are unmeasured, so a redesign cannot show it is better and a regression would not be seen.
- **Direction.** Add a storage conformance suite before the rebuild: N simulated machines against a fake remote with random interleavings, kills at every step, and assertions of convergence, no lost write and a successful cold restore; a scale benchmark at 1k, 10k and 50k pages; and two runtime figures in every record, time-to-durable and time-to-synced.
- **Action.** Add. Basis: verified (by the test list and the docs).

### scale-1. Work done under the wiki lock grows with the wiki, and `log.md` is write-only bookkeeping (low to medium; architecture; partly known)

- **Evidence.** Under the wiki lock, the commit loads and validates the whole wiki (`commit.go:214-216`, `399-409`) and starts one `git diff` process per staged file (`commit.go:217-221`, `scan.go:30-37`); page writers wait at most 60 s for that lock and then fail (`adapter.go:62-65`, `899-902`). Every page save reads and rewrites the whole of `log.md` and fsyncs the root (`adapter.go:256-269`); past 16 MiB it silently stops. The wiki lock is implemented twice, joined only by a path convention and a test (`adapter.go:861-909`, `commit.go:535-561`); flock helpers exist in six packages.
- **Impact.** Fine at today's size; at thousands of pages the first commit after a break can hold the lock long enough to fail concurrent updates, and each save pays for a log nothing reads.
- **Direction.** Scan the staged diff in one streamed git call; validate outside the lock against the staged snapshot; drop `log.md` in favour of commit history (history-1); one lock helper used by every writer.
- **Action.** Evolve (scan and validate), remove (`log.md` writer). Basis: verified. Known: `docs/known-limitations.md:234` notes the log's limits only.

---

## 5. Where the complexity is essential and where it is accidental

**Essential, keep:** the path and link safety around a folder people can edit; atomic write with fsync before a watermark moves; a single lock that dies with its holder; compare-and-swap and no-op skipping; bounded reads for hooks; the hardened git runner; the remote guard; the secret gate on what leaves the machine; refusing to commit over someone else's half-finished git operation.

**Accidental, caused by okf:** the lock-file clearing, error rewording, pre-written init files, serializer mirroring, body-check copy, `verified` scan, own log and index writers, the list of calls that must never be made, the weekly review and upgrade ritual. Roughly half of `adapter.go` by line count.

**Accidental, caused by the "user's working clone is the store" choice:** the eleven-refusal guard ladder, never-delete rules, the left-out-forever list, the folder location bans, the first-push question, most of the doctor's commit and push fixes.

**Accidental, caused by "one signed commit a day, triggered by a hook":** the attempt markers, the push-retry mtime marker, the 300 s signing wait, the stderr word-matching to tell a missing signer from a declined one, the notices after 2 and 3 days, four lock files and their ordering.

**Missing, and essential for the goal:** any inbound path (fetch, merge), a mergeable layout, writer identity, portable evidence, and measurements.

---

## 6. Overall call for this part

**Rebuild the storage core; keep its safety properties and its tests as the specification.**

The pieces are individually careful and well tested, and the invariants in section 3 are hard-won. But the three structural choices under them, a wrapped library that fights the product, a single-writer layout pushed once a day, and a store that is also the user's hand-edited clone, each rule out something the goal requires (sync across machines and sessions, zero-step onboarding, no human in the commit loop, self-healing). Patching `git pull` onto today's layout would trade a clean "diverged" for daily merge conflicts.

Suggested shape: an eva-md-owned local repository created at install; eva-md's own page codec; commute-by-construction pages (collision-free ids, per-writer or item-merged thread state, derived files outside git); one local commit per update with provenance trailers; a background sync step (fetch, deterministic merge, push with retry) under a machine identity; an optional remote and an optional human checkout. Order of work: eval-1's conformance suite first, then okf-1 and model-1 together (one migration of the on-disk layout), then history-1 and durability-1, then sync-1 and signing-1, then onboard-1 and ownership-1 as the user-visible result.

---

## 7. Open questions for the owner

1. Is "one wiki per machine" (decision 1, `design.md:1240`) open for change? Everything in sync-1 and model-1 depends on the answer.
2. Should memory commits carry the user's own signature at all (decision 7), or may the store sign with a machine identity the user vouches for once?
3. May eva-md own its repository under its data folder, with the user-facing wiki folder becoming an optional checkout? That removes most refusals but changes "the wiki is a folder you chose".
4. May the on-disk layout change with a one-time migration (new knowledge ids, thread state split by writer or operation, derived files out of git)? Existing wikis are few (releases are days old; the live day's run is still pending).
5. Is okf compatibility a product promise or only a way out? If only a way out, a documented, stable Markdown format owned by eva-md satisfies it.
6. Is a remote required for the product to be "on", or is local-only memory with an optional remote acceptable as the default?
7. How much evidence may leave the machine? Redacted excerpts of cited lines beside each fact would make sources portable; the current rule is that no captured text is ever committed.
8. Is team or shared project memory in scope (a second, shared store with a publish step), or strictly personal memory across one person's machines and sessions?
9. Is a background agent started by the system (a launchd job run on a timer or on demand, not a server) acceptable? Sync and retries without one can only happen when a chat fires a hook.
10. Which non-macOS environments must the store serve first: Linux remote hosts and containers, Claude Code cloud sessions, Windows? The flock, `O_NOFOLLOW` and folder rules are POSIX and macOS-shaped today.

---

## Verification

A second reader tried to refute each finding against the code. Nothing was run but read-only commands (grep, sed, wc, git log); no test, and no command AGENTS.md restricts. Result: 9 confirmed, 3 partly, 0 refuted. Five severities are lower than the audit's.

### The two-machine question, checked

- **Nothing pulls or merges.** A grep of all non-test Go finds no `fetch`, `pull`, `merge` or `rebase` git argument. The only inbound git calls are `clone` at init (`wikiinit/init.go:90`), setup's shallow peek (`cmd/eva-md/setup.go:1080`) and an `ls-remote` visibility probe (`cmd/eva-md/setup_repo.go:865`).
- **A second Mac sees one snapshot.** Init clones once and saves the wiki's own `instance` into that Mac's config (`init.go:256`), so the instance guard (`commit.go:259-262`) passes on both Macs. Readers check the instance only (`resume/read.go:576-582`, `hook/pointers.go:312-318`).
- **A conflict never happens inside eva-md; a rejected push does.** The Mac that pushes second gets `! [rejected]`, recorded as `diverged` (`commit.go:496-499`). `RetryPush` retries only `pending` (`health/background.go:398`). That Mac keeps committing locally each day, its push fails each day, the doctor says to merge by hand (`background.go:355`) and each chat start carries a notice (`background.go:439-440`).
- **During a hand merge** the commit refuses (`commit.go:276-287`), and no writer looks at git state (no `MERGE_HEAD` check outside `wikigit` and `resume/git.go`, which reads the project's repo, not the wiki's).

### Verdicts

| Finding | Verdict | Audit's severity | Mine | Evidence and corrections |
| --- | --- | --- | --- | --- |
| sync-1 | confirmed | critical | critical | As above. It is a deliberate decision (`docs/decisions.md:26-28`, `docs/design.md:1240`). Nuance: a second Mac does get one snapshot at clone. |
| model-1 | confirmed | critical | high | Every citation holds: `knowledgepage/save.go:150` and `249-250`, `update/wiki.go:81`, okf `mutate.go:508-511`, `commit.go:58`, `wikiinit/init.go:300`. Scope: conflicts land in the shared files (knowledge pages, maps, indexes, TSVs, the general thread, a branch both Macs work on), not in projects only one Mac touches. `generated.at` changes only when the page does (`adapter.go:229-231`). It harms nobody until something merges, so high. It misses two blockers; see merge-guard-1 below. |
| onboard-1 | confirmed | high | high | `init.go:53-63` and `458-466` and `commit.go:298-300` hold. Worse than stated: setup "stopped before installing anything" when init fails (`setup.go:534`), so on the documented path even capture waits for a wiki. Softer: gh or glab can create the repo, and a bare repo on the Mac counts as the remote. Deliberate: `docs/decisions.md:104`. |
| durability-1 | confirmed | high | medium | `hook/maintain.go:70-81`, `hook/spawn.go:34`, `cmd/eva-md/maintain.go:65-69` hold. A failed spawn also leaves no record, so no notice follows. The loss is bounded to the last active day's pages, the raw inbox never leaves the Mac anyway, and `eva-md commit` runs it by hand. It becomes high once other machines read the remote. |
| signing-1 | confirmed | high | medium | `commit.go:159-166`, `hook/spawn.go:75`, `background.go:329-334` hold. Scope: only git setups that sign through an approver, an agent that asks at each push, or a signer off the app's PATH. An unsigned setup with a silent credential helper sees nothing. One approval a day is decision 7. |
| okf-1 | partly | high | medium | okf's limits are real (0644, `generated.at`, the body check, the lock file). But about 220 of `adapter.go`'s 939 lines work around okf (`246-294`, `429-479`, `752-856`, parts of `373-427`). The flock, compare-and-swap, no-clobber, path checks, atomic writes and bounded reads are needed by any store, and the audit's own keep list names them. The adapter calls 9 okf functions, not 8. `design.md:187`'s exit test is "stops round-tripping" or "keeps growing": the first is not shown, the second is a judgement. "Cannot be built on a serializer eva-md does not control" is too strong: the `generated` line is the serializer's only merge hazard. |
| ownership-1 | partly | high | medium | Eleven refusals (`commit.go:249-293`), fifteen fixes (`background.go:311-339`) and the never-cleared `index.lock` hold. Wrong: a stray binary file does not end commits. `commit.go:217-235` leaves the file out and commits the rest, as `TestLeavesOutWhatIsNeverCommitted` asserts; only a doctor row fails (`background.go:304-305`), and no chat-start notice follows (`background.go:438-445`). The SIGKILL-to-stale-lock link stays inferred. Missed silent state: the guard pins no branch (`commit.go:289` refuses only a detached HEAD; `481` pushes `HEAD`), so another checked-out branch is committed and pushed, and a restore clones the default branch. |
| history-1 | partly | medium | low | `commit.go:159` is the only daily message, and no code reads `log.md`. But "nobody can see which chat and model did it" is wrong: item lines carry `s:<session>#<line>` refs and tags, the Timeline and a knowledge page's History only append (`threadpage/page.go:1-80`), Last chat names host and model, and `update/runlog.go:71-94` records session, host and model for each pass. What is missing is a per-pass revert in git. |
| restore-1 | confirmed | medium | medium | `commit.go:399-409` counts only `Errors`, and nothing reads `validate_errors` (grep finds the field written at `commit.go:215` and read nowhere). `init.go:195` refuses errors, orphans, broken links and a page not listed in its parent index, on the re-clone path too (`setup.go:524`). `update/wiki.go:360-365` and `605-641` take separate lock holds. A repair also needs a hand commit, because `init.go:207-213` refuses uncommitted changes. How often a pushed commit has findings is unmeasured. |
| evidence-1 | confirmed | medium | medium | The inbox sits outside the wiki, and `init.go:301` and `commit.go:365` keep it out; `lint/lint.go:43-46` only counts refs from elsewhere. It is a deliberate privacy rule (the scaffold's README text, `init.go:281`). The statement itself travels; only its proof stays behind. |
| eval-1 | confirmed | medium | medium | Two tests use a second clone (`commit_test.go:499`, `588`); both assert nothing is fetched. No test merges, restores a pushed wiki that has findings, or times a commit at size. Safety itself is tested (guards, secret scan, locks), so the gap is convergence, delay and restore. |
| scale-1 | confirmed | low | low | `commit.go:136-147` holds the wiki lock across validation and one `git diff` per file (`scan.go:32`); `adapter.go:899-902` fails writers after 60 s. Correction: `log.md` is read on every save but rewritten only on a page's first save of the day (`adapter.go:266`, `282-287`). |

### Problems the audit missed

**merge-guard-1. Merged thread pages would break the update's own rules (high; architecture; verified in code).**
- Evidence: items get ids `n<N>` from the page's own counter (`threadpage/apply.go:192`, `224`). The out-of-order guard sets `KeepNow` when a batch's newest entry is before the page's `as_of` (`update/validate.go:101-104`); `update/knowledge.go:92-95` and `update/rules.go:110-113` then ignore every change. `as_of` only moves forward (`update/wiki.go:296-303`).
- Impact: even with conflict-free files, a page merged from another machine brings its `as_of` and ids. This machine's chats that are older then lose their Now, rule and knowledge changes, counted only as ignored, and two machines give one id to different items. model-1's "merge by item id" does not work as the ids stand.
- Direction: collision-free item ids, and `as_of` kept per writer or per session so the guard compares a chat only with its own writer's progress. Put both in model-1's migration and in eval-1's suite, as two writers on one branch.
- Known: the single-machine form of the guard is listed (`docs/known-limitations.md:172`, `217`); the multi-writer effect and the id collision are not.

**replay-1. A wiki cloned again loses what was summarized since the last push, though the chats are still in the inbox (medium; reliability-safety; verified in code).**
- Evidence: a watermark is a byte offset in `<inbox>/<project>/.state/<file>.wm` (`inbox/inbox.go:429-434`), moved only by the update (`update/backlog.go:197-227`). Setup's re-clone runs `wikiinit.Run` alone (`setup.go:524`), which imports no inbox code. `eva-md update` has no flag to process a chat again (`cmd/eva-md/update.go:68-72`).
- Impact: after the wiki folder is lost and cloned again, or a diverged clone is replaced by a fresh one (the easy way out of "merge by hand"), every page change since the last good push is gone for good, because the watermarks say those chats are done. With a push stuck for weeks, that is weeks.
- Direction: tie progress to what the store holds. Record with each commit the offset it covers for each session, and after a restore or any rollback set the watermarks back to the store's values and replay from the inbox.

**migrate-1. Nothing can change a wiki's layout: a schema mismatch only refuses, and the scaffold's git files are never updated (medium; architecture; verified in code).**
- Evidence: `schema_version` is only compared with 1 (`commit.go:263-266`, `update/wiki.go:47-48`, `systempage/write.go:182-183`, `health/health.go:66`); a grep for "migrat" in non-test Go finds nothing. `.gitattributes` and `.gitignore` are written by the scaffold alone (`wikiinit/init.go:300-308`) and are outside `commitPaths` (`commit.go:58`).
- Impact: model-1 and okf-1 both need one layout migration. Today the first machine to write schema 2 turns wiki writes off on every older binary, whose hooks keep reading the new layout with the old parser (readers check the instance only), and a new merge rule cannot reach an existing wiki.
- Direction: build the migration path before the new layout: an idempotent, resumable migrator run under the wiki lock and committed as one change, a minimum reader version that old hooks honour, and git files the store owns, rewrites and commits.

Not verified here: how often a pushed commit carries validation findings, how often a signing or push prompt appears, and whether a killed git leaves `index.lock` in practice.
