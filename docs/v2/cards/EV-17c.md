# EV-17c: the v1 condition: memory built from the earlier chat, as v1 builds it

**Plan:** section 6.6; section 8, rows EV-17 ("v1's row recorded") and EV-03, whose v1 arm this is (the lead's ruling of 2026-11-13); section 7's evaluation build tag. **Design:** `working-papers/design/evaluate.md` 4.4, 4.5; [host-facts.md](../host-facts.md) F-115, F-163 (recorded by the pilot, which runs this arm), F-018. **Size:** M, as before. **Reviewers:** one, as before (made-up chats). **Depends on:** [EV-17b](EV-17b.md), [EV-17d](EV-17d.md), [EV-17g](EV-17g.md) (`drive.CodeMemoryOff`, `--keep-trials`).

## Why

The third condition is what a person gets: eva-md set up, the earlier chat captured by its hooks, v1's update, the new chat with its plugin. A trial whose hooks never fired is unjudged, not read as v1. eva-md is unchanged: no switch, no line added to `./cmd/eva-md`. Elsewhere: task export (EV-03b), live-run guards (EV-17g), the report and the pilot's README (EV-03c); the run-book is the lead's.

## What to build

`internal/eval/continuation/v1arm` registers `task.Eva` with `drive.Register`; `cmd/eva-eval/continuation_eva.go` imports it for effect. eva-md runs as `<Bin>/eva-md`, in `Trial.Env`.

1. **Check** refuses no `--eva-md`, one whose `debug/buildinfo` `-tags` lacks `evamd_eval` (EV-17d), and a `--pass-env` naming `CLAUDE_CODE_ENTRYPOINT` or `EVA_MD_EVAL_ACTOR`. A test-only variable skips the tag check.
2. **Setup:** `git init --bare <Root>/remote.git`; `eva-md setup --dir <Home>/wiki --remote <Root>/remote.git --no-push --yes --host <InstallName>`, before any hook (v1's `Baseline`); then `drive.PluginReady` (item 10). Failure: `setup_failed`.
3. **The earlier chat**, fed to `eva-md hook --host <id>` in the new chat's folder, one `payload` (`internal/hook/hook.go`) a call: `SessionStart` (`source` `startup`), `UserPromptSubmit` (`prompt`) per user turn, `Stop` (`last_assistant_message`) per agent turn, `SessionEnd`; `cwd` that folder, no transcript, no `CLAUDE_CODE_ENTRYPOINT`. Its session id, from the trial alone, passes `safeID`.
4. **The update.** `eva-md update --session <id>:<session> --foreground`, again while its line (`Summary.Line`) says `stopped: busy`. Then `update.Days` must hold a record of that host and session with outcome `ok` or `no_change`, a detached job's too (a `backoff` stop exits 0). Else, or on exit other than 0: `update_failed`.
5. **Detached jobs.** The hooks start maintain, a catch-up with the sweep, and at `SessionEnd` an update, each in a session of its own (`internal/hook/spawn.go`). Before the chat and before `After` sums, the arm waits while `update.lock` or `maintain.lock` in `<Home>/.local/state/eva-md/locks` is held.
6. **The new chat:** `Launch.Plugin` true, `Launch.Env` `EVA_MD_EVAL_ACTOR=person`.
7. **Memory on.** `After` lists `<Home>/.local/state/eva-md/inbox` with `inboxv1.Backend` and reads every session but the replayed one (same host and id). `drive.CodeMemoryOff` when none holds an event; on `adapter.ClaudeCode`, whose hook reads an entrypoint, also when none holds one of `event.KindPrompt` by `event.AuthorV1User`: `host_prompt` alone is the seam failing. A file over 64 MiB is not read; an unreadable inbox shows nothing.
8. **Cost.** `After` sums `TokensIn` + `TokensOut` and `DurationMS` of `update.Days` on `<Home>/.local/state/eva-md` (today, yesterday), returned with `memory_off` too, for the ceiling.
9. **Bounded.** Each command runs in its own process group, killed past a limit tests raise: `setup_failed` for setup and hooks, `update_failed` for the update and item 5. At most 64 KiB of a stdout is kept.
10. **`drive.PluginReady(home string, h adapter.Host) error`**, new in `drive/plugin.go`: the host table's `Plugin` entry on `home`, writing nothing.

## Owns and may touch

All new. `v1arm`'s `TestMain` calls `testgitenv.Clear()`, `fakehost.Main()`, `hostfake.Main()`, and raises the limits, the driver's (`versionTimeout`, `waitDelay`) too, or stops and reports.

```paths
internal/eval/continuation/v1arm/**
internal/eval/continuation/drive/plugin.go
internal/eval/continuation/drive/plugin_test.go
cmd/eva-eval/continuation_eva.go
cmd/eva-eval/continuation_eva_test.go
```

## Must not touch

Everything else: eva-md's code, `task`, the driver's other files, `fakehost`, `hostfake`. A seam missing: stop and report.

## Accepted when

- `scripts/linux-check.sh` passes; `go list -deps ./cmd/eva-md` names no `internal/eval`; `TestHostLiteralCount` unchanged.
- Per host, a fake eva-md (the test binary, recording what it gets) and the driver's fake host: bare repository, setup, hooks, update, chat, in order; setup's arguments exact; payloads equal a golden file; hooks lack `CLAUDE_CODE_ENTRYPOINT`, the chat has `EVA_MD_EVAL_ACTOR=person`; no `asked_again` row.
- No chat starts while the fake says `stopped: busy` or a process it left holds either lock; released, it does. A failing setup or update, exit 0 with no `ok` record, a command stalled past a limit the test gives: its code, no row. A detached job's `ok`, then `nothing pending`: judged.
- `continuation run --condition eva --spend-model-quota` refuses an untagged eva-md and each refused `--pass-env`; nothing runs.
- Two hand-written run-log records give `memory_tokens` and `memory_ms` as their sums, counted toward `--max-tokens`, under `memory_off` too.
- Planted inboxes: the replayed session alone, `memory_off`; another with a person's prompt, judged; on Claude Code, only `host_prompt`, `memory_off`; on Codex, only `session_start`, judged.
- End to end, per host: eva-md built from the checkout with `-tags evamd_eval,evamd_testlimits` (as `standInEnv`, `cmd/eva-md/release_test.go`: own home, `GOENV=off`, telemetry `off`), HO-14's `(*hostfake.Fake).WriteCLIs` in `bin/`: setup runs the plugin commands (`Calls`); `PluginReady` passes; `eva-md brief --cwd <folder> --host <id>` prints a word of the history; item 4 gives `update_failed` (no model); the test ends once both locks are free. It needs a `TMPDIR` outside `/tmp`.
- A word planted in every turn is in no row, output or error.
- Mutations (no `PluginReady`, the replayed session counted, `host_prompt` as a person's, no lock wait) each fail a test.

## For the smoke run

- Claude Code: is the plugin loaded twice (setup's registration, `--plugin-dir`)? In a trial kept by `--keep-trials`, each prompt is stored once.
- Codex: are managed hooks read from `<home>/.codex/requirements.toml` (F-163)? If not, every Codex trial is `memory_off`; the lead rules before Codex's v1 row.

## The lead's rulings of 2026-11-13

The card's checker left these to the lead.

- **A job that has started and not yet taken its lock** is not seen by item 5's wait. The wait stands as carded; what closes the gap at a trial's end is EV-17g's removal, which tries again before it stops a run.
- **The lock probe may make a job of the trial leave as busy**: accepted for `maintain.lock` as for `update.lock`. A day's commit skipped in a trial's wiki changes no measure.
- **Item 4's rule stands**: the update counts when the run log holds a record of the replayed session with outcome `ok` or `no_change`; a pass v1 marks skipped is `update_failed`.
- **On Codex any event of another session counts as memory on**, a chat start alone included, until the smoke run shows which hooks a headless Codex chat fires.
- **`memory_off` stops the run** (EV-17g's rulings).
- The end-to-end test needs a `TMPDIR` outside `/tmp`, as eva-md's hook tests do, and the checks' script gives it one.
- EV-17g exports the driver's two limits for this package's tests (its item 9).
- If the card proves too large for one run, the end-to-end test becomes a follow-up of its own, and the builder says so.

## Added by the lead on 2026-11-14, before the build

The pilot's run-book was checked against the code and found what this card had missed: **v1's update cannot sign in with a key.** `internal/update/process.go` gives the host's CLI a cleared environment and drops every key, by design, so that the update runs on the person's own sign-in. A trial's home is new and holds no sign-in, so in a container that has only keys every trial of this condition would end `update_failed`.

The ruling: eva-md is not changed, and the driver and this arm still hold, read and write no key. The command gains **`--sign-in PATH`**, for this condition alone: a program the run-book gives, run once in each trial before setup, in the trial's environment (so with the variables `--pass-env` names) and with the trial's home as its folder. It signs the host's CLI in within the trial's home by the host's own means, so that a CLI started later with a cleared environment finds its sign-in there. The arm does not read what it prints (at most 64 KiB is kept, for an error's length alone), bounds it like item 9's commands, and counts a failure as `setup_failed`. Without the flag nothing runs and item 4 behaves as carded. Tests: the fake program records its environment and folder; it runs before setup, once a trial; its failure or its stall is `setup_failed` with no row; a word it prints is in no row, output or error. The trial's folder is removed when its rows are written, so what the program wrote goes with it.

Three more points from the same check:

- **Where each command runs.** The run-book's wrapper, given as `--cli`, takes a call of the host's CLI made in the trial's workspace for a chat and gives the trial to the chat's user. So setup and the update run with the trial's home as their folder, never the workspace; only the replayed hooks name the workspace, in their payload's `cwd`, and they run eva-md, not the host's CLI. A test holds the folder of each.
- **A run does not go on through trials that all fail the same way.** `setup_failed` and `update_failed` stop the run when the first two trials of a run both end in one of them: each further trial would end the same. A later one is counted and the run goes on.
- **Jobs that outlive the chat can call the model.** With a sign-in in the trial's home, eva-md's own jobs started by the new chat's last hooks can spend after item 5's wait. The removal of the trial's folder takes their sign-in away; what they spend before that is uncounted, and the run-book's limit at the provider is what bounds it. The card says so in its package comment.

## As merged

Merged from `pkg/ev-17c-r1` on 2026-11-14, after a review that found only small faults, a fix round for them and a verification that accepted it. The end-to-end test is in: eva-md built from the checkout with the evaluation tag, a real setup against a fake of the host's plugin commands, and a brief that prints the replayed chat. It showed one thing no fake had: v1's brief resumes a chat only when it has three prompts and three replies, so the trial replays three exchanges.

The lead's rulings at the merge:

- **The stop rule fires on any two failures**: when a run's first two trials end in `setup_failed` or `update_failed`, in any mix, the run stops.
- **`--sign-in` stays as built for the pilot**: the command's wrapper passes it to the arm. A field of the driver's options would be cleaner and is owed with the points below.

Owed on the driver's files, in one short round before the first full run, none of which changes a figure of a run that goes well:

- A code of its own for a run the stop rule ended: as built the third trial's `Prepare` returns a plain error, the run's record says `error`, and that trial is counted neither unjudged nor not run, so the record's counts do not add up to what was planned.
- `--sign-in` as a field of the driver's options and a flag of the command.
- What a failed update or the sign-in program spent: `Prepare` returns no cost, so only what `After` sums reaches the ceiling.

Known and left: a job of eva-md's that the new chat's last hooks started may still be running, signed in, when the trial's folder is removed; what it spends is not counted, and the limit at the provider bounds it. Test as root, one case fails by design (an inbox no one may read), as about twenty of eva-md's do.
