# DV-02a: the live run's trace: cost, versions and what the host returned

**Plan:** section 8 Stage 0 row DV-02, EV-01 ("the output is recorded as fixtures"); Stage 1 rows DV-11 ("the host's version is recorded") and DV-13; section 8's threshold row on seconds and tokens per pass at the highest effort; section 15.4 ("v1's real output from both hosts at two efforts"). **Design:** `docs/v2/working-papers/design/derive.md`, row DV-02 in section 18 ("a recorder that saves envelopes and event streams, scrubbed, under `testdata/<host>/<cli version>/headless/`") and the host facts of section 11; `evaluate.md`, row EV-01 in section 21 ("tokens, seconds, the validator's report, refusals, versions"). **Size:** M. **Reviewers:** two: the contract, and privacy (it keeps a host's raw output). **Depends on:** EV-14d, which edits the same `main.go`. It adds a field to v1's runner that nothing in eva-md sets, so it changes what the binary builds: it merges after the owner has cut the counters release, as a named exception to the freeze of v1's update, as DV-13 is. It is built before DV-13, which edits `runner.go`, `claude.go` and `codex.go` too, and the two are never open at the same time.

## Why

The live gates are run once, by the owner, at a Mac. EV-14d records each pass's answer and nothing else, by design. Four packages need more from the same run, and today the run keeps none of it:

- DV-13 and section 8's row need tokens and seconds for each pass at each effort. `update.Result` has the tokens (`TokensIn`, `TokensOut`), and `cmd/live-accuracy` drops them; the seconds appear only in a line of prose on stderr. `tokens_in` includes cached input, which DV-13 weighs apart and `Result` does not split until DV-13; the raw files hold it (Claude's `cache_read_input_tokens`, Codex's `cached_input_tokens`), and the lead reads it from there.
- DV-11 needs what a real host prints when it fails (auth, a rate limit, a server error, a refused effort, a timeout) and the host CLI's version. The runner reads the envelope or event stream and keeps only a reason; no version is read.
- DV-11 and DV-03 need the model the host says answered (`Result.ModelReported`), the effort the accepted attempt used and the attempts a pass took, which the command counts but does not keep. EV-01's row needs the validator's refusals pass by pass, and eva-md's commit beside every figure.
- A pass that ends in `timeout` stops the whole run (`live.Run` treats every reason but `invalid_output` and `tool_used` as a stop), and EV-14d's replay refuses a stopped run's recording. The real install hit the 600 s limit on 2 of 61 calls at the highest effort, so about half of the 21-pass highest runs would stop, and a host would lose v1's fidelity row at that effort. A timeout is itself a reading for section 8, so the run should keep it and go on.

Without this, the owner would have to copy figures by hand, or sit a second time. This adds two flags and one optional hook on the runner, and changes nothing else.

## What to build

1. **A tap on the runner.** `update.Request` gains `Tap func(stdout, stderr []byte)`. When it is set, `runClaude` and `runCodex` call it once after each CLI attempt of a run, never for `codex features list`, with the bytes the runner already holds: Claude's stdout as the capped buffer has it, Codex's stream as written (a copy capped at `maxStdout`, teed beside `codexStream`), and `child.stderr`. A nil `Tap` changes nothing, byte for byte; a set one never changes a `Result`. No caller in eva-md sets it.
2. **`--trace <dir>` on `cmd/live-accuracy`**, to a folder that is new or empty, refused as a usage error (exit 2) before anything runs otherwise. Written as the run goes, each line synced:
   - `run.json`: `{"v":1,"host":…,"effort":…,"model":<asked, "" for the default>,"cli_version":<the first N.N.N of the version of the CLI the run starts, read as internal/host/adapter reads it, "" when unread>,"eva_md":{"commit":<vcs.revision>,"dirty":<vcs.modified>},"timeout_refused":<bool>,"day":<UTC date>}`, the build's fields from `debug.ReadBuildInfo`, with an empty commit when the build has none. No field names the machine.
   - `passes.jsonl`, one line a pass: `{"script":<1 to 12, the order of accuracy.Scripts()>,"pass":<from 1>,"ms":<the call's wall time, as ask measures it>,"tokens_in","tokens_out","attempts","effort_used","model_reported","model_note","degraded","reason","refused"}`, each from `update.Result`, and `refused` the reason the run counts the answer refused, or `""`: the runner's reason when it is `invalid_output` or `tool_used`, a timeout under item 3, or else the error `update.Validate` gives for the pass's answer, batch and page, which `ask` holds, so `live` is not edited.
   - `raw/<script>-<pass>-<attempt>.out` and `.err`, two digits each: the tap's bytes.
   - `end.json`: `{"outcome":"met"|"missed"|"stopped","passes":n,"refused":n,"timeouts":n,"stopped":<the reason, or "">}`, written last, also when a run stops or is interrupted.
   A run that stops keeps everything written up to and with the pass that stopped it, its raw output included.
3. **`--timeout-refused`.** With it, the `ask` that `main.go` gives `live.Run` returns a pass whose reason is `timeout` with the reason `invalid_output`, so the run counts it refused and goes on, as `live` does for an invalid answer; it prints the existing "timeout after … s" line, and the summary adds `answers timed out: n` after `answers refused`. The recording and the trace keep `timeout` for that pass. `--replay` with the flag maps a recorded `timeout` the same way, so such a recording replays to the same report; without the flag, a run or a replay stops on a timeout as today.
4. **Files.** `cmd/live-accuracy/trace.go` holds the format and the writer and imports neither `internal/update/accuracy` nor its `live` package, so `TestOnlyItImportsTheSuite` passes unchanged; what needs the suite stays in `main.go`.
5. **Docs.** The "Live accuracy run" text in `docs/verification.md` and `docs/design.md` (or the spec that holds it, if EV-10a has merged) gains both flags, and says that a trace holds model names, host ids and raw host output, which can name a plan and a rate limit's reset time, so it is handed over privately and never committed as written.

## Owns and may touch

`cmd/live-accuracy/main.go`, `cmd/live-accuracy/trace.go` and `trace_test.go` (new); `internal/update/runner.go` (the field), `claude.go` and `codex.go` (the call), `internal/update/tap_test.go` (new); the docs text named in item 5.

```paths
cmd/live-accuracy/main.go
cmd/live-accuracy/trace.go
cmd/live-accuracy/trace_test.go
internal/update/runner.go
internal/update/claude.go
internal/update/codex.go
internal/update/tap_test.go
docs/verification.md
docs/design.md
```

## Must not touch

Everything else. `cmd/live-accuracy/main_test.go`, `cmd/live-accuracy/record.go`, `internal/update/accuracy/**`, `internal/host/adapter/**` and every existing test in `internal/update` are read, never edited. No hook, and nothing a hook runs, changes: `go list -deps ./internal/hook` is the same before and after.

## Accepted when

- `scripts/linux-check.sh` passes, with every existing test unchanged.
- On the fake `claude` and `codex` executables the runner's tests use: the tap receives each attempt's stdout and stderr byte for byte, the attempts an effort refusal steps down from and a run that times out included; with a tap set, every `Result` equals the one without, by test.
- A live run on a fake runner with `--trace` writes `run.json`, 21 lines, the raw files of every attempt and `end.json` with `met` or `missed`. A run that stops at pass 5 on `rate_limit` writes 5 lines, the fifth with its reason, its raw files, and `end.json` with `stopped`; an interrupted run writes `end.json` too. An answer the validator refuses has its error in `refused` and an empty `reason`. By test.
- A fake runner that times out at pass 5: with `--timeout-refused`, the run goes on to 21 passes, the fifth line has `reason` `timeout` and a `refused` that is not empty, `end.json` counts one timeout, and the recording keeps `timeout`; `--replay` of that recording with the flag prints the same report and writes the same pages; without the flag, the run stops at pass 5 as today. By test.
- No line of `run.json`, `passes.jsonl` or `end.json` holds a path, the run's temporary home, a time of day or the machine's system or architecture, by test; a fake CLI that prints `2.1.400 (Claude Code)` gives `"cli_version":"2.1.400"`.
- `--trace` to a folder that holds a file exits 2 and calls nothing.
- Without the new flags the command prints and does exactly what it did; with `--record`, `--pages` and `--trace` together, each writes what it writes alone.
- No new module; only eva's name in any output.
