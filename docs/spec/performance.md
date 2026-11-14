# Performance

- **Component:** Latency targets and the benchmark
- **Paths:** `scripts/benchmark.py`, `scripts/bench`
- **Status:** v1 as built, frozen on 2026-11-14

The blocks below are v1's design and verification text, moved here word for word when the two documents were split; [INDEX.md](INDEX.md) lists every spec.

<!-- v1 docs/verification.md 049 -->
## Performance

Targets, as p95 including process start:

| Measure | Target | Measured |
| --- | --- | --- |
| Capture (one Stop hook) | 50 ms | about 25-30 ms, in a git repo and in a plain folder |
| Startup brief (SessionStart) | 300 ms | about 100-150 ms |
| Startup brief with 64 newer chats a program ran, 2 MiB of short tool lines and replies | 300 ms | about 170 ms in one run with other jobs running (load average 4 to 12), against 168 ms for the same run's brief without them |
| Capture with transcript recovery | 50 ms | under 50 ms |
| Prompt after a brief, checking for a refresh, with 200 other projects in the wiki | 50 ms | about 25-27 ms |
| Prompt naming 3 systems, with a full names index (2,000 names), 3 pointers sent | 50 ms | about 30 ms |
| Stop that spawns an update (60 KB or more unprocessed) | 50 ms | about 27-31 ms |
| SessionEnd, spawning an update | 200 ms | about 45 ms |
| Startup brief with two days of run log and two months of commit records to check | 300 ms | about 120-130 ms |
| Startup brief pointing at 3 systems that cite the project's code at HEAD~1, counting it (3 git calls) | 300 ms | about 105 ms; the counts add about 25-30 ms |
| Prompt naming 3 systems, each pointer with its code note from a code status of 400 pages | 50 ms | about 35-37 ms, about 1 ms over the same prompt without notes |
| Prompt recalling 3 facts from a full recall index (1,678 rows, 512 KiB), beside the full names index | 50 ms | about 31 ms; recall's own read and scoring take about 3 ms |
| MCP server: a cold start to its `initialize` answer | 100 ms | about 14 ms |
| `eva_search` over the 50 MiB inbox, a wiki of 200 projects and 400 system pages | 5 s | about 0.11 s |
| `eva_show` of a source ref the search found | 300 ms | about 6 ms |
| `eva_status` with the run log and commit records above | 300 ms | about 70 ms |
| MCP server's exit once its stdin closes | 1 s (the slowest of 10) | about 1 ms |

Capture in a git repo meets its target because hooks cache each worktree's identity (see [design.md](../design.md)). Without the cache, every hook started three or four git processes.

The startup brief's cost doesn't grow with the wiki or with the length of a chat. Measured with `eva-md brief` on a Mac, median of runs:

| Layout | Brief |
| --- | --- |
| Wiki of 10,000 pages (44 MB) | about 38 ms, the same as an empty wiki |
| Wiki of 10,000 pages with a stray symlink | about 39 ms, Now section still shown |
| One 200 MB session file | about 130 ms |
| 64 session files of 5 MB each | about 325 ms |

The last row is the brief's remaining cost: it decodes the last MiB of each of the 64 newest session files, so it passes 300 ms only when about 64 recent chats each exceed 1 MiB.

To measure on your machine, build the binary and run the benchmark:

```sh
go build -mod=readonly -o bin/eva-md ./cmd/eva-md
python3 -I scripts/benchmark.py --binary bin/eva-md --cwd <a git repo outside temp folders>
```

The script uses its own temporary HOME, under /tmp so the binary's path that every pointer names is near an installed one's length, and never touches your setup.
Pass a folder outside temporary folders, because hooks skip those.
It clears `CLAUDE_CODE_ENTRYPOINT`, so a run from inside an agent's chat isn't taken for a program's chat.
It plants three exchanges, the first event spawning the day's maintain, as on an install without a wiki, pads the inbox to about 50 MiB, then times 10 startup briefs, 10 more with 64 newer chats a program ran beside the person's, each 32 KiB of short tool lines and replies with quotes and line
breaks, 2 MiB in all, the most the brief reads beyond its 64 files, which must not push the person's chat out of the brief and which the script then removes, and 100 Claude Stop hooks that each recover a new transcript row.
Then, with a wiki of 200 other projects' now pages, its schema and an instance in the config, so that updates and the chat start's push-retry check are on, and two days of run log and 60 days of commit records, it times 10 startup briefs that check them, the first of which also asks for the day's
sweep, giving the hook's day, which the script then records as the day it was taken, as the job would, 100 Codex Stop hooks and 100 prompts after a brief.
With 400 system pages, a map and a full names index planted, it times 100 prompts that each name 3 systems and must get 3 pointers.
Then come 30 Codex Stop hooks that each spawn an update and 30 SessionEnd hooks that each spawn one.
The installed binary in its temporary HOME is a stand-in that only counts its runs, so it never starts a real update, commit or model call, and the script checks for 62 spawns, maintain and the sweep exactly once each.
A hook that misses its time limit delivers nothing by design, neither its spawn nor its output, so the script lets a Stop's or a SessionEnd's spawn be missing, and passes over the empty output of a prompt or a Codex Stop while no more than one hook in twenty of its loop passed the limit, only for a
hook it timed itself at that limit less the 10 ms the hook keeps for its output, and prints beside its report how many hooks of each timed loop passed it.
Then it times the code notes: 10 startup briefs that point at 3 systems citing the `--cwd` repo at HEAD~1, so each brief counts the commits since, and every pointer must say the code moved (a repo with a single commit skips these); and 25 prompts that each name 3 of the 400 systems, whose map lines
cite a made-up repo's code and whose entries in a planted code status of all 400 pages say it moved, and every pointer must say so.
Then it fills the recall index to just under its 512 KiB bound, 1,678 facts, and times 25 chats of 4 prompts, each asking about 3 facts it has not had by their numbers, and every prompt must get the label, the fence and the 3 fact lines in at most 1,000 characters, the facts being another project's that a planted repos index makes a sibling of the checkout's, so every prompt reads that index too.
It prints the p95 of each and whether it met its target.
It also reports, as figures only, the time of `eva-md search --all --days 0` over the padded inbox before and after `eva-md index`, the first including the search's own top-up of the index, and the time `eva-md index` took: about 120 to 240 ms before, about 120 to 160 ms for `eva-md index` and about 30 ms after, process start included.
The padding is events, which are never indexed, so the figures say little about a real inbox.
`--output <file>` also saves the report.
The transcript and capture figures are sensitive to machine load, so compare two builds in alternating runs, or best of all interleaved sample by sample.

After the spawn hooks, and before the code notes and recall, the benchmark starts the MCP server in the `--cwd` folder ten times, each timed to its `initialize` answer and to its exit once stdin closes, then times 20 `eva_search` calls for the planted word over the padded inbox, 20 `eva_show` calls of the ref it found and 20 `eva_status` calls on one server.
It checks each answer, and that the server writes nothing to stderr.

The benchmark's scenarios run from a list: each is a file in `scripts/bench/` that adds itself, and `scripts/benchmark.py` loads them by path.
Beside the padded inbox above there are three more: the start of the process on each host's launch path, which every hook figure includes (Codex's hooks run through `/bin/sh -c`, as its declaration does); the shape of the one real install, an inbox of 600 session files and a wiki of 54 pages, with a
chat start, a prompt, a capture and the brief built by command; and one turn of 50 tool calls on each host's hook declaration, with the processes it starts and the milliseconds it sums.

`python3 -I scripts/benchmark.py --compare OLD NEW --cwd <repo>` is the latency gate.
It runs both binaries in alternating runs on one machine, each twice a round, three rounds by default, and exits 1 when the new one is slower than the machine's own noise allows, by a figure's p95 or its median, or fails a scenario the old one passes, 0 with `not judged` when the machine was too noisy to tell, and 2 when it could not run.
Its rule, and why each part of it is there, are in [docs/v2/baselines/README.md](../v2/baselines/README.md); its decision is a pure function of the samples, tested with made-up numbers by `python3 -I scripts/bench/test_gate.py`, which the gate's job runs first.
On the Linux machine it was built on, an unchanged binary passed 19 gates of 20 by its first rule and the other was not judged, while the machine's disk was busy with other work; a binary with 20 ms added to capture failed on 14 figures.
Worked out again by the second round's rule, the same 20 gates pass 16 times and are not judged 4 times, and the one with 20 ms added fails on 12 figures.
By the third round's rule, the same 20 gates pass 17 times and are not judged 3 times, and the one with 20 ms added fails on 12 figures.

<!-- /v1 -->

## Must keep

The must-keep items of [findings.md](../v2/findings.md) this component touches, numbered from K01 in each list, and the tests that hold them.
An item that tests hold only in part ends with what of it no test holds, as `(untested: ...)`.

| Id | Item | Tests |
| --- | --- | --- |
| CAP-K18 | Capture p95 under 50 ms and the brief under 300 ms, checked by the benchmark | check:scripts/benchmark.py, internal/spec:TestTheBenchmarkHoldsTheSpecsTargets |
| EVL-K09 | The benchmark is isolated and checks what the hooks do as well as how fast | check:scripts/benchmark.py, internal/spec:TestTheBenchmarkHoldsTheSpecsTargets |
| EVL-K10 | Capture p95 under 50 ms, the startup brief under 300 ms, a prompt under 50 ms and SessionEnd under 200 ms | check:scripts/benchmark.py, check:scripts/bench/test_gate.py, internal/spec:TestTheBenchmarkHoldsTheSpecsTargets |

## Since the split

**2026-11-14, EV-16e: a brief whose git calls ran out of time.**
In the offline benchmark's code briefs every pointer must say the code moved, unless git's trace of that brief shows that the brief's clock cut its counts short.
The script turns the trace on for these briefs alone, through the home's global git config.
A brief is cut short when the notes it lacks are the last ones, the counts before them exited 0, at most one count followed them, killed or exited 0, and either that count was killed or the brief took 60 ms or more, the most one count may take (`CodeCall`).
Such a brief is passed over, keeps its time among the samples and is counted beside the report, and more than one brief in two passed over fails.
A repo with a single commit skips these briefs, as before.
