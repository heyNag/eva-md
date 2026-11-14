# The pilot

The pilot (EV-03) runs ten continuation tasks on both hosts under three conditions, memory off (`off`), v1's memory (`eva`) and the ideal record (`oracle`), to measure how much a session's figures vary, what a session costs in tokens and minutes, and whether the tasks can show a difference at all. It runs on the continuation instrument's driver; its report is the instrument's own.

The tasks in `testdata/tasks`, their checker (`eva-eval pilot check`) and their export (`eva-eval pilot export`) are throwaway: they are deleted when the instrument's own tasks land (EV-17e). The driver, the report and this folder's figures stay until then.

A run starts real chats and spends real model quota. It needs the owner's leave and a ceiling the owner names for the pilot. Nothing below is run without both. The run-book, [docs/v2/pilot-runbook.md](../../docs/v2/pilot-runbook.md), splits the ceiling over the runs and says where and in what order they run; this file holds the commands and the rules they keep.

## Where it runs

A Linux container with git 2.44 or newer, Go, python3 and both hosts' CLIs, whose network is open to the model provider. Each host's CLI runs as a second user: `--cli` names a wrapper that starts the real CLI as that user, so a chat cannot read the tasks, the repositories or the results by their paths. The chat signs in with an evaluation key, passed to it by name with `--pass-env`. How each host's CLI is called is in `internal/eval/continuation/drive/hosts.go`.

## The commands, in order

Run them from the root of an eva-md checkout, whose commit the rows record.

1. Check the tasks:

   ```sh
   go run ./cmd/eva-eval pilot check
   ```

2. Export them to the instrument's format, into a folder that does not exist yet. It prints a line a task and then `session-timeout <n>m`, which every run below takes:

   ```sh
   go run ./cmd/eva-eval pilot export --out EXPORT
   ```

3. Check the export as the driver reads it:

   ```sh
   go run ./cmd/eva-eval continuation check --tasks EXPORT/tasks --repos EXPORT/repos
   ```

4. Build the evaluation build of eva-md, which the `eva` condition loads through `--eva-md`. No release carries it:

   ```sh
   go build -tags evamd_eval -o EVAL-BIN ./cmd/eva-md
   ```

5. A smoke run: one task and one trial on each host under each condition, from an export of that task alone (`pilot export --task ID --out SMOKE-EXPORT`), each run with `--trials 1`, an empty `--work` and a results folder of its own that no later step reads.

6. The six runs, two hosts by three conditions, each with `--trials 3`, the export's `--session-timeout`, a fresh `--out` of its own outside every `--work`, an empty `--work`, and one `--model`, the same for a host's three conditions:

   ```sh
   go run ./cmd/eva-eval continuation run --tasks EXPORT/tasks --repos EXPORT/repos \
     --host HOST --cli WRAPPER --condition CONDITION --trials 3 \
     --session-timeout TIMEOUT --work WORK --out OUT --model MODEL \
     --max-sessions N --max-tokens N --pass-env KEY-NAME [--eva-md EVAL-BIN] \
     --spend-model-quota
   ```

   Without `--spend-model-quota` a run prints its plan and starts nothing. Each run appends a record to its `--out`, however it ends, unless the process is killed.

7. The report, over the six results folders and any rerun's, with the full export's tasks. It prints ids and figures only. `--rates` gives money per million tokens, uncached prompt, cached and output, as the provider prices them on the day; no rate is written in this repository:

   ```sh
   go run ./cmd/eva-eval continuation report --tasks EXPORT/tasks [--rates IN,CACHED,OUT] OUT...
   ```

## Rules the report keeps

- Each trial is counted once. A results folder given twice, a copy of one, or the smoke run's folder beside the full runs' is refused: the smoke run's trial is the full run's first again.
- A folder's rows must be exactly those its records count. A killed run writes no record, so the report refuses its folder: leave that folder out, and run that host and condition again in a fresh `--out`, from an export of the tasks it was to run (`pilot export --task ID ...`).
- A task with no trial judged on a host under a condition reruns alone: `pilot export --task ID` into a fresh export folder, then a run of it alone, on that host and condition, in a fresh `--out`. The report takes that folder beside the others.
- A trial left unjudged writes no row and moves no figure; the records count it, by its code.
- The report exits 0, or 2 with no figure when a flag is wrong or it refuses what it reads, and names a results folder by its place among the arguments, never by its path.

## What it settles

Of the host facts in [docs/v2/host-facts.md](../../docs/v2/host-facts.md), the pilot's figures settle these:

- F-067, a headless run with only an API key: the judged trials.
- F-068, tool events in the stream: `agent_events`.
- F-069, a turn bound and resumed runs: the corrections.
- F-115 and F-163, hooks that fire in a headless run on Claude Code and on Codex: the trials left unjudged `memory_off`.
- F-207, a session's size and length: the tokens and minutes per session.

It does not settle F-070 to F-072, F-116, F-117 or F-169.
