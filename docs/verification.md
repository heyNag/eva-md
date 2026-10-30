# Verification

This page says what has been checked in capture and resume, and how. Wiki updates are planned and not built yet, so nothing here covers them. Open problems are listed in [known-limitations.md](known-limitations.md).

## Automated tests

Run them from the repo root:

```sh
gofmt -l cmd internal     # prints nothing
go vet ./...
go test ./...
go test -race ./...
```

Every test runs against a temporary HOME with `GIT_CONFIG_GLOBAL` and `GIT_CONFIG_NOSYSTEM=1` set, so no test reads or changes the real home folder, the hosts' settings or the user's git config. Fixtures that look like secrets are joined from fragments at run time.

What the suite covers:

- **Redaction** (`internal/redact`). Each secret format, key=value lines in dotenv, YAML, INI and properties files, values with symbols, several secrets in one URL query, form body or connection string, and code shapes that must be kept. `redact.Redact` removes the union of what the baseline rules (`internal/redact/baseline`, frozen) and the current rules remove. Generated-corpus tests check that no generated password the baseline rules remove survives `Redact`, in 16 line shapes and thousands of random passwords. Other tests check that redaction is idempotent on random input and that identifiers such as branch names and project keys are cleaned without merging two of them.
- **Transcript recovery** (`internal/catchup`). The 1 MiB read window, a row longer than the window scanned in bounded chunks, a row of 256 KiB or more that records an attempt before it is decoded, the cursor saved after every row, slow rows that step down to tighter head-and-tail bounds and then to a gap event, a redactor panic that becomes a gap, duplicate suppression, host-generated rows that are never stored as the user's prompts, and record controls found in missed rows, also beside an attachment. A fuzz test checks the row scanner against arbitrary input.
- **Hooks** (`internal/hook`). Every event on both hosts, the folder and remote guards, off-the-record and back on, input, work and output deadlines, malformed input, panics, writes that arrive after a deadline, parallel hooks, and the rule that a pending recovery pass stores only event names.
- **Resume briefs** (`internal/resume`). Every brief variant, lineage only for startup and clear, empty wiki scaffolds, the brief's size cap, and text that tries to forge a speaker header.
- **Install** (`internal/install`). Install, dry run, uninstall and rollback, with fake `claude` and `codex` commands standing in for the host CLIs. This covers partial failures, uncertain native writes and ownership checks. A binary-only update leaves both plugin trees byte-identical.
- **Identity cache** (`internal/identity`). A git wrapper counts its own runs to prove that a cache hit starts no git process. The tests cover branch switches, a detached HEAD, remote changes, the first commit, packed refs, includes in the global config, linked worktrees, deleted and re-created repositories, symlinked paths, damaged cache entries and concurrent hooks. A differential test compares cached and live answers across 20 generated repositories with random changes. The config parser is checked against `git config --list`.
- **Health, setup and CLI** (`internal/health`, `internal/wikiinit`, `cmd/eva-md`). Doctor checks and hook-trust parsing, wiki creation and adoption, and the command-line surface.

Some hook tests depend on deadlines and can fail on a heavily loaded machine. See [known-limitations.md](known-limitations.md).

## Live checks

These were run by hand in the real host apps with synthetic test chats. Each planted a few made-up facts in one chat, then asked for them in another with tools and the host's own memory turned off, so the answer had to come from eva-md's brief.

Passed:

- **Claude Code CLI.** Recall of all planted facts in a new chat, after forced compaction (`/compact`), and after `/clear`. The answer after `/clear` was slow because of the model's reasoning effort; the hook had already delivered the facts.
- **Codex CLI and cross-host.** Facts planted in Claude Code were recalled in a new Codex chat, in a Codex chat started by a clear, and again in that chat after forced compaction.
- **Claude Code desktop app.** Recall at startup and after `/compact`. After compaction the context also held the host's own compaction summary, so this check does not prove eva-md was the only source.
- **Install through the native CLIs.** Claude Code's strict plugin validation and Codex's plugin validator passed. After the user approved the hooks in Codex `/hooks`, all seven eva-md hooks showed as trusted and enabled, and `eva-md doctor` reported no FAIL checks.
- **Transcript recovery on real hosts.** Replies missed by a hook were recovered from the known transcript without duplicates. Typed prompts in real Claude Code transcripts carry the `promptId` field that recovery relies on.

## Performance

Targets, as p95 including process start:

| Measure | Target | Measured |
| --- | --- | --- |
| Capture (one Stop hook) | 50 ms | about 25-30 ms, in a git repo and in a plain folder |
| Startup brief (SessionStart) | 300 ms | about 105-160 ms |
| Capture with transcript recovery | 50 ms | under 50 ms |

Capture in a git repo meets its target because hooks cache each worktree's identity (see [design.md](design.md)). Without the cache, every hook started three or four git processes.

To measure on your machine, build the binary and run the benchmark:

```sh
go build -o bin/eva-md ./cmd/eva-md
python3 -I scripts/benchmark.py --binary bin/eva-md --cwd <a git repo outside temp folders>
```

The script uses its own temporary HOME and never touches your setup. Pass a folder outside temporary folders, because hooks skip those. It plants three exchanges, pads the inbox to about 50 MiB, then times 10 startup briefs, 100 Codex Stop hooks and 100 Claude Stop hooks that each recover a new transcript row. It prints the p95 of each and whether it met its target. `--output <file>` also saves the report. The transcript figure is sensitive to machine load, so compare two builds in alternating runs.

## Not verified yet

- **Codex desktop app.** Recall in a new chat and after compaction have not been checked in the Codex desktop app.
- **Real use.** A stretch of everyday use with zero hook errors and capture p95 under 50 ms, measured on an installed build.
