# HO-12a: health's host names through the adapter

**Plan:** section 6.5 (design 1), section 8 Stage 1 rows HO-10 and HO-12. **Design:** `docs/v2/working-papers/design/hosts.md` sections 3.1 and 3.4, and its rows HO-10 and HO-12 in section 21. **Part of:** HO-12, built in three parts that run in order: a (this), b (each stage's lag and a verdict), c (one line, prepared outside the hook). The cards' README gives the move of health and the doctor's command file to HO-12. **Size:** S to M. **Reviewers:** one. **Depends on:** HO-10a and HO-10b merged; the counters release (CT-1 to CT-4) merged, since CT-1, CT-2 and CT-4 add files to `internal/health` and CT-4 edits `cmd/eva-md/commands.go`, and their host names move too.

## Why

`internal/health` and the doctor's command spell a host three ways (`claude`, `claude-code`, `codex`) in about 30 lines: the event list of each host (`hostEvents` in `watchdog.go`), its command (`cli` in `background.go`), its chat folder and the variable that moves it (`store` and `moves` in `watchdog.go`), its app's name (`Absent` in `hosts.go`), and loops over `[]string{"claude-code", "codex"}` in `health.go` and `watchdog.go`. Parts b and c add a stage and a condition for each host. They are written against one place that knows a host, so this part moves the names there first and changes nothing a person sees.

## What to build

1. **No literal left.** In the non-test Go files of `internal/health` and in `cmd/eva-md/commands.go`, each comparison with, or literal of, a host's spelling becomes a read of `internal/host/adapter`: its `ID`, its `Names` (`App`, `CLI`, `InstallName`), `All()` for every loop over hosts, and the host's event list for `hostEvents`. What one host does differently and the adapter lacks (the chat folder `store` names, `~/.claude/projects` or `~/.codex/sessions`; the variable `moves` names, `CLAUDE_CONFIG_DIR` or `CODEX_HOME`; whether the chat folder holds a folder per project or a folder per day, which picks `newestClaudeChat` or `newestCodexChat`) becomes a field of the adapter, documented there and set for both hosts. `update.HostClaude` and `update.HostCodex` stay where they are, since `internal/update` is not touched; a test holds them equal to the adapter's ids.
2. **The doctor's features rule.** The reading of the `hooks` row of `codex features list` in `inspectHosts` (`hosts.go`) goes through the adapter function HO-10b made. The doctor keeps its own rule: PASS only on a last column of `true`, FAIL on any other value, UNKNOWN with no `hooks` row.
3. **The count.** In `internal/host/adapter/testdata/host-literals.txt`, set `internal/health` to 0 and lower `cmd/eva-md` by the lines this part removes.

What stays exactly as it is: every row's name, status, detail and fix, in the same order, with and without `--json`; the hosts' order (Claude Code, then Codex); the events' order inside each host's rows; every path read under the state folder (`heartbeats/<host>/<event>.json`, `uncaptured.json`, `catchup/drift-<host>.json`, `marketplace/<name>/.eva-md-managed`); `QuickNotice`'s lines. This part changes nothing v1 does, so it needs no switch.

## Owns and may touch

The non-test Go files of `internal/health`, changed only where a host is spelt; new test files in `internal/health`; in `cmd/eva-md/commands.go`, only the lines that spell a host; `internal/host/adapter/**` (the added fields and the count).

```paths
internal/health/**
cmd/eva-md/commands.go
internal/host/adapter/**
```

## Must not touch

Existing test files of `internal/health` and `cmd/eva-md`, which pass unchanged. `internal/hook/**`, `internal/update/**`, `internal/mcp/**`, `internal/install/**`, `internal/catchup/**`, and every other file of `cmd/eva-md`.

## Accepted when

- `scripts/linux-check.sh` passes, and no existing test has changed. SEAM-0c's golden output of `status` and `doctor` passes as it is.
- The literal count reads 0 for `internal/health` and the lowered figure for `cmd/eva-md`.
- A test holds each host's event list from the adapter, in order, equal to today's: Claude Code's ten (`SessionStart`, `UserPromptSubmit`, `PostToolUse`, `Stop`, `PreCompact`, `SessionEnd`, `PostToolUseFailure`, `StopFailure`, `PostCompact`, `PostModelSwitch`) and Codex's seven (the first six, then `Interrupt`).
- A test holds `update.HostClaude` and `update.HostCodex` equal to `adapter.ClaudeCode` and `adapter.Codex`.
- The doctor's features rule is pinned on the inputs HO-10b found where install's rule and the doctor's differ, and gives the doctor's answer on each.
- The diff adds no import of `internal/hook`, `internal/update` or `internal/install` to a file that lacked it.
