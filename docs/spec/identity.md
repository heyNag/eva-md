# Identity and git's environment

- **Component:** Identity resolver
- **Paths:** `internal/identity`
- **Status:** v1 as built, frozen on 2026-11-14

The blocks below are v1's design and verification text, moved here word for word when the two documents were split; [INDEX.md](INDEX.md) lists every spec.

<!-- v1 docs/verification.md 012 -->
- **Identity cache** (`internal/identity`).
  A git wrapper counts its own runs to prove that a cache hit starts no git process.
  The tests cover branch switches, a tag named like the branch, a folder name ending in a space, a detached HEAD, remote changes, the first commit, packed refs, includes in the global config, linked worktrees, deleted and re-created repositories, symlinked paths, damaged cache entries and concurrent hooks.
  A differential test compares cached and live answers across 20 generated repositories with random changes.
  An entry an older eva-md stored with a key the current redaction rules give differently, a readable key the inbox now refuses, a hash where the readable key now passes and a remote's refused key, resolves live and is replaced, though git's files are unchanged (`TestCacheKeyFollowsTheRedactionRules`).
  The config parser is checked against `git config --list`.

<!-- v1 docs/verification.md 033 -->
- **Git's environment, output caps and files started at once** (`internal/identity`, `internal/wikigit`, `internal/resume`, `internal/update`, `internal/install`, `internal/health`, `internal/paths`, `internal/hook`, `cmd/eva-md`, `internal/wikiinit`).
  One planted variable of each kind every git leaves out, another repository, config in the environment, a pathspec setting, a trace and the locale, and some it passes, git's own config files and what signs and signs in: the wiki's runner and the read-only calls drop and pass the same ones, and differ
  only by the read-only calls' two settings (`TestEveryGitDropsTheSameVariables`); the brief's live git and the update's commit subjects run with exactly the read-only environment (`TestLiveGitRunsWithTheReadEnvironment`, `TestCommitsOfRunsWithTheReadEnvironment`); setup's signing line counts no
  signing that only config in the environment or another repository's config turns on (`TestGitSigningReadsWithTheCommitsEnvironment`); with a git trace on, a folder outside any repo is still the general area and no trace file is written (`TestGitTraceChangesNothing`); a fake git that words a refusal in German unless `LC_ALL=C` still gives its read reason (`TestReadFailureInAnyLocale`).
  Each cap holds against a fake command printing just past it, copied as os/exec copies, and the command printing up to it keeps it all: a wiki git command fails, its diagnostics are cut, and the daily commit lists 12,000 staged files, past 1 MiB (`TestRunFailsPastTheOutputLimit`,
  `TestDiagnosticsAreCutAtTheLimit`, `TestStagedFilesPastOneCommandsLimit`); uninstall counts 9,000 changed files, past 1 MiB (`TestUnsavedPastOneCommandsLimit`); setup's and init's check for uncommitted changes tells changes from none whatever the list's length, and a git that fails is never a clean
  wiki (`TestDirtyReadsAListOfAnyLength`), so init refuses a wiki of 1,400 changed files for its changes, not for git's output (`TestAdoptionRefusesAnyNumberOfChanges`); the brief keeps 64 KiB (`TestLiveGitOutputIsBounded`) and still counts 2,000 stashes, whose list would pass it, as git counts them,
  also beside a file named refs/stash (`TestBriefCountsEveryStash`); a host inventory fails and its diagnostics are cut (`TestHostOutputIsCapped`); the doctor's host command is unavailable (`TestHostCommandOutputIsCapped`); gh's output fails, sorted or not (`TestCLIOutputIsCapped`).
  Eight callers that start the same new file at once each get it, never ENOENT: through `paths.OpenOrCreate` (`TestOpenOrCreate`), the commit's lock file and the month's commit records (`TestNewLockAndRecordFilesTakeConcurrentCreators`), the run log and the day's job log
  (`TestNewLogsTakeConcurrentCreators`), the install lock (`TestNewInstallLockTakesConcurrentCreators`) and a chat's prompts marker (`TestNewPromptsMarkerTakesConcurrentCreators`).

<!-- /v1 -->

## Since the split

No change yet.
