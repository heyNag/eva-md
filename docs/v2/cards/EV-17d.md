# EV-17d: the evaluation build, which no release can carry

**Plan:** section 8, the Stage 1 row EV-17 ("the evaluation build lifts the macOS-only gate first"); section 7, the lead's choice that an evaluation-only build tag lets a scripted session count as a person's chat, "with tests that no release contains it"; decision 23 (which systems v2.0 runs on; until then a release runs on macOS only). **Design:** `docs/v2/working-papers/design/evaluate.md` section 4.4 (the seam behind `evamd_eval`, and its precedent `timelimits_raised.go`) and EV-D4; `critique-delivery.md` section 11. **Size:** S. **Reviewers:** two (it changes, in the evaluation build only, which chats a hook stores as a person's): the contract, and a reviewer who tries to get the tag or its effect into a release. **Merges only after the owner has cut the counters release:** it adds a file to `cmd/eva-md` and a line to `table.go`.

## Why

The continuation sessions run in Linux containers, and a release build of eva-md refuses setup on anything but macOS (`cmd/eva-md/setup.go`, the check on `env.goos`). It also takes a chat that Claude Code's print mode starts for a program's (`headless` in `internal/hook/hook.go`): its prompts are stored as host prompts, it gets no recall at a prompt, and its hooks never queue v1's update. So a headless session would measure the program's path, not a person's. The tests' release stand-in already gets past the first check with a build tag (`timelimits_raised.go` sets `hostOS`); the evaluation build does the same with a tag of its own, and adds the one seam. A release is unchanged.

## What to build

1. **`cmd/eva-md/evalbuild.go`**, built only with the tag `evamd_eval` (`//go:build evamd_eval`). Its `init` sets `hostOS` to `darwin`, so setup gets past its macOS check, as the stand-in's does.
2. **The seam.** In the evaluation build, when the command is `hook` and the variable `EVA_MD_EVAL_ACTOR` is `person`, the hook takes the chat for a person's whatever `CLAUDE_CODE_ENTRYPOINT` says, for example by giving the process the entrypoint `cli` before the hook reads it. Any other value of the variable, or none, changes nothing. A Codex chat is unaffected. Neither `main.go` nor anything under `internal/` is edited: the seam lives in the tagged file.
3. **`table.go`** gains one line in `supportFiles` for the new file, as `timelimits_raised.go` has.
4. **Words for the lead.** The report gives a sentence for `AGENTS.md`'s Layout line on `cmd/eva-md` and one for `docs/verification.md`, saying what the evaluation build is and that no release carries it. The builder edits neither.

## Owns and may touch

The tagged file and its test (both new), and one line of `table.go`.

```paths
cmd/eva-md/evalbuild.go
cmd/eva-md/evalbuild_test.go
cmd/eva-md/table.go
```

## Must not touch

Everything else: `cmd/eva-md/main.go`, `setup.go`, `timelimits_raised.go`, `release_test.go`, `internal/hook`, `scripts/`, `.github/workflows/`, `AGENTS.md`, `README.md` and `docs/`. `release_test.go`'s helpers (`standInEnv`, `goTool`) are called, never changed.

## Accepted when

- `scripts/linux-check.sh` passes, and the latency gate's comparison with the commit it started from (`scripts/linux-gate.sh`) is in the report, though no hook a release builds has changed.
- **No release can carry it**, by tests in `evalbuild_test.go`:
  - `go list -f '{{.GoFiles}}' ./cmd/eva-md` with no tags does not list `evalbuild.go`, and with `-tags evamd_eval` does;
  - no file under `.github/workflows/` or `scripts/` names `evamd_eval`, and no `go build`, `go install` or `go run` line there passes `-tags`;
  - no `GOFLAGS` set there holds `-tags`;
  - a binary built with the flags the test reads from the `go build` line of `.github/workflows/release.yml` (its `\` continuations joined; today `-trimpath -mod=readonly` and `-ldflags`) has no `-tags` setting in its build information (`debug/buildinfo`), and one built with the tag has `evamd_eval` there, which is what EV-17c reads.
- **The seam works only in the evaluation build**, by tests that build eva-md with `standInEnv`, once with `evamd_testlimits,evamd_eval` and once with `evamd_testlimits` alone, and run `eva-md hook --host claude-code` on a made-up prompt in a home of their own, the chat's folder outside `TMPDIR` as `project` in `main_test.go` makes it, with `CLAUDE_CODE_ENTRYPOINT=sdk-cli`:
  - the evaluation build with `EVA_MD_EVAL_ACTOR=person` stores the prompt in the inbox as the user's (`"role":"user","kind":"prompt"`, with its text);
  - the evaluation build without it, and the stand-in with it, store only the event `host_prompt`, as a program's chat.
- **The gate is lifted**: eva-md built with `evamd_eval` alone (not the stand-in's tag, which sets `hostOS` too) runs `eva-md setup` in a home of its own and passes the line `macOS` of "Checking this Mac" on Linux; the test reads only that line. On macOS it passes anyway, and the release-side tests above carry the proof there.
- A reviewer's mutations (the build line dropped from `evalbuild.go`, `hostOS` left unset, the seam made to ignore the variable's value, a `-tags evamd_eval` added to a workflow's `go build`) are each caught by a test.
- With the tag absent, `go build ./cmd/eva-md` compiles the same files as before.

## As merged

Merged from `pkg/ev-17d-r1` on 2026-11-13, after two reviews, a fix round and a verification that accepted it. Both reviewers found the same fault, and the fix answers it: the test that no release carries the file had asked only about the machine it ran on, while a release is built for macOS on both architectures without cgo. It now reads the release workflow's build step, lists and builds for each of its targets in that step's environment, and looks in the built binaries for the tag and for the file's code. Three widened build constraints that had passed are caught.

- The seam is keyed on the command `hook` alone, as the card says, not on the host. A Codex hook ignores the entrypoint it sets, and the jobs a hook starts are given an environment that holds neither variable, so the effect stays in the hook.
- Owed, outside this card: a step in the release workflow that reads each published binary's build information and fails on a build tag, so that the published file itself is checked. It needs a card that owns `release.yml`.

