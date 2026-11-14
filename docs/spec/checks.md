# How v1 is checked, and what it guarantees

- **Component:** The test suite and CI
- **Paths:** `.github/workflows/ci.yml`, `internal/testgitenv`, `internal/testlimits`
- **Status:** v1 as built, frozen on 2026-11-14

The blocks below are v1's design and verification text, moved here word for word when the two documents were split; [INDEX.md](INDEX.md) lists every spec.

<!-- v1 docs/design.md 034 -->
## Reliability guarantees

| Guarantee | How |
| --- | --- |
| A hook never blocks or breaks a chat | Always exit 0, never 2, never a block decision. Panics recovered. Internal deadlines well under the host timeouts. No model or network in hooks. Slow work only in setsid-detached children with all stdio redirected. Codex Stop always gets `{}`. Claude Stop and async hooks print nothing. |
| Every finished exchange is saved | Documented payload fields, appended under flock with fsync. PostToolUseFailure for failed commands. StopFailure markers. Limited transcript catch-up. Gaps that can't be recovered are written as markers, never skipped silently. A config that exists but can't be read stops capture until it is fixed, and each chat start says so in one line. |
| "Where did we stop?" works even if no model ever runs | The brief comes from the raw inbox tail, plan entries and live git. The Now section is optional and flagged as stale when it is behind. The resume suite runs with the update switched off. |
| Every chat gets its brief once, within host caps | SessionStart with the first-prompt fallback, keyed by a per-session marker. Resume and fork inject only changes. Hard caps of 8,000 characters on Claude and 6,000 on Codex, whose hooks also set a 3000-token limit, with the footer and the newest exchange reserved first. |
| Compaction never drops the current state | PreCompact catches up and starts an update. SessionStart(compact) re-injects Now plus the last exchanges verbatim. |
| A failed or killed update loses nothing and rarely doubles anything | The watermark advances only after the output validates, and after the page's folder fsync when the page changed. Compare-and-swap. Unchanged saves are skipped. Leftover temp files are ignored and never staged. Catch-up at every chat start, and the day's sweep of idle backlogs with a person's first chat start of the day. A kill or a failed watermark write after a save can repeat Next items and done, change or problem lines on the next pass. |
| Pages never silently lose items or your text | Accountable item ids, an append-only Timeline, no-clobber create. A knowledge page keeps every statement it held in its History, and a retired page stays on the map. Locked, human-verified and damaged pages are respected. Your sections are kept and never rewritten. Out-of-order guard. |
| Dates and speakers come from the data | Refs to entries. Code-assigned dates. Any date in the text must appear in a cited entry. Tags are checked against the speakers of cited entries, and prompts you didn't type never count as yours. |
| Secrets stay out of git | Redaction at capture, on model output and on the staged diff. The inbox lives outside the wiki. Explicit-path staging that refuses `.jsonl`, inbox and `.tmp-okf-*` files. Quarantine for text you wrote. Codex updates that used a tool are rejected. |
| One wiki's memory never mixes with another's | One config per machine. A remote guard and an instance id checked on every write, commit and push. No API calls and no syncing between machines. |
| Commits never block a hook or nag | A detached daily job, one signing attempt per day, push retries with timeouts, never a force push. |
| A stuck or orphaned MCP server never holds the host or the hooks | The server writes nothing and takes no lock. Each call has a deadline; one still running a second past it, or a second past the host's cancel, is left running, and 8 such calls end the server. It exits when its stdin ends, a write fails or its parent process changes. |
| No recursion | `EVA_MD_INNER=1` makes the hook exit at once, and this holds even when managed-policy hooks run. Claude `--safe-mode`; Codex `--disable hooks --disable plugins`. An empty run folder and no session persistence. |
| A missing, stale or damaged search index never hides a file or shows text the inbox doesn't hold | The inbox is the truth and the index a cache of it. The scan covers what the index does not; every line the index proposes is read again from its file and checked before it is shown; the full scan runs when the index finds nothing; a damaged folder is scanned, and built again by the next catch-up. |
| Unknowns are stated as unknown | The brief says "unknown", "stale", "no reply recorded" or "no reply (stop_failure)". The doctor uses UNKNOWN for anything it can only infer, such as Codex trust. The author is recorded as `<host>/unknown` when the model is unknown. |

---

<!-- v1 docs/verification.md 001 -->
## Verification

This page says what has been checked, and how.
Everything built is covered by the automated tests below.
The live checks cover capture, resume, the Codex feature list and plugin inventory, read without a model call, the releases on GitHub, cut from a Mac and from GitHub, and the install and uninstall scripts from one: no update has run live yet, and the live checks still owed, among them wiki updates,
system pages and the code they cite, standing rules, pointers, knowledge pages and prompt recall, the digest and recall skills, the MCP server, setup with real gh, and with glab beyond one self-hosted GitLab, `eva-md upgrade` and a whole uninstall from a release, an upgrade over an installed plugin and a day's run of a wiki on a self-hosted GitLab, are listed under "Not verified yet".
Open problems are listed in [known-limitations.md](../known-limitations.md).

## Automated tests

Run them from the repo root:

```sh
gofmt -l cmd internal     # prints nothing
go vet ./...
go test ./...
go test -race ./...
```

GitHub Actions runs four jobs for every push to main and every pull request ([ci.yml](../../.github/workflows/ci.yml)).
The macOS job, the test run of record, runs the same four checks and the build.
A second macOS job runs the latency gate, apart from the tests, whose load would leave it nothing to judge: it builds the binary of the commit before the change, runs `scripts/benchmark.py --compare` on it and the change's own binary, prints the report and saves it as the run's `latency` artifact.
The gate is advisory for now: it reports and never fails the job, until an unchanged binary has passed it twenty times there with no fail among the runs that count and fewer than one in four of them not judged ([docs/v2/baselines/README.md](../v2/baselines/README.md) holds its rule, the count and the stored baseline).
The Linux job runs `scripts/linux-check.sh`, which builds eva-md and runs gofmt, vet, the tests, the race detector included, and the benchmark, with the `TMPDIR` outside `/tmp` the tests need there (in CI's one-commit checkout the benchmark doesn't time the code briefs, which count commits since
`HEAD~1`); it keeps eva-md building and its tests passing on Linux, where cloud sessions work on it, though eva-md itself runs only on macOS.
A fourth job, `guards`, on Linux with the whole history, runs `eva-eval paths` over the commits the change brings, on their first parents.
It fails the job when a commit with a `Card:` line touches a path outside its cards' `paths` blocks, names a card that does not exist or has no such block, widens its own card's block, or carries a line that sets out to name a card in another form, and it lists a commit with no such line as the lead's.
It then runs `eva-eval size`, which prints the Go lines built into eva-md for macOS, the lines merged cards schedule for deletion, the net, the ceiling of 53,694 and the headroom, in a step that fails nothing: the ceiling binds at the cut-over ([docs/v2/plan.md](../v2/plan.md), section 7, "Where the decisions stand").
The checks that need macOS itself run only in the macOS job: the release script's own tests (`internal/release/release_darwin_test.go`), which give it a pseudo-terminal through macOS's own calls, and the checks of a folder typed in another letter case, which need a file system that ignores case.

The evaluation build is eva-md built with the `evamd_eval` tag, for the evaluation's continuation sessions, which run scripted chats in Linux containers: its setup gets past the macOS check, and a hook run with `EVA_MD_EVAL_ACTOR=person` stores a Claude Code chat's prompts as a person's even when a program started the chat.
The effect stays in the hook: the jobs a hook starts get an environment without either variable.
No release carries it.
`evalbuild_test.go` in `cmd/eva-md` fails if `evalbuild.go`'s build constraint is anything but the tag alone; if another file of the command names the tag or the variable; if a build without the tag compiles the file, for this machine or for any target the release workflow builds, in that workflow's
environment; if a script or workflow names the tag, passes `-tags` to anything but git, go test or go vet, or sets `GOFLAGS` to `-tags` or to a value from a variable; or if the release's own build, for each of its targets, records a build tag or holds the evaluation build's code.

Every test runs against a temporary HOME with `GIT_CONFIG_GLOBAL` and `GIT_CONFIG_NOSYSTEM=1` set, so no test reads or changes the real home folder, the hosts' settings or the user's git config.
The one exception is Go's own installation and its build and module caches, which `go test` itself uses: the release stand-in's build shares them, and `TestReleaseLicenses` reads the licenses and notices it checks there.
A temporary HOME, `GIT_CONFIG_GLOBAL` and `GIT_CONFIG_NOSYSTEM` don't shut out git configuration carried by the environment, `GIT_CONFIG_COUNT` with `GIT_CONFIG_KEY_<n>` and `GIT_CONFIG_VALUE_<n>`, `GIT_CONFIG_PARAMETERS` and `GIT_CONFIG`, which cloud workspaces set, as to rewrite `git@github.com:`
addresses to `https://github.com/`, nor a commit identity carried by the environment, `GIT_AUTHOR_NAME`, `GIT_AUTHOR_EMAIL`, `GIT_COMMITTER_NAME` and `GIT_COMMITTER_EMAIL`, which a cloud environment may set to give its commits its owner's name, so every package whose tests run git clears those
variables first in its `TestMain`, with `internal/testgitenv`, and the suite behaves the same with or without them.
Fixtures that look like secrets are joined from fragments at run time.

A fake that runs as a copy of the test binary, such as the host CLIs of `internal/host/hostfake`, leaves through `syscall.Exit`, not `os.Exit`: under the race detector, which the copy is built with too, `os.Exit` waits a second before the process ends, so a package that starts its fakes hundreds of times would take minutes more.
A fake gives up only what the tests never read: a race in its own process no longer sets its exit code, and it writes no coverage data.

What the suite covers:

<!-- /v1 -->

## Since the split

**2026-11-14, EV-17f: the release workflow reads each binary it built.**
The release workflow's build job reads each binary it built, in the step right before the upload, with `go version -m` and by its bytes, and fails on one that is not eva-md's command, records a build tag or holds either mark of the evaluation build's code.
`internal/release/release_tagcheck_test.go` holds the step in place and runs it, with the runner's variables set, on the release's own build, the same build with the tag, and made-up binaries.
The step runs for the first time at the next release.
