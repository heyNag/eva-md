"""A whole turn of 50 tool calls, for each host's hook declaration.

A turn is the prompt, 50 tool calls and the reply's end, and every event the
host declares a hook for starts the hook (internal/install/templates.go):
UserPromptSubmit, PostToolUse for each call and Stop, on both hosts. Every
call here is one the host's matcher takes. The turn runs twice in the same
home, with a wiki set up so updates are on: once timed, giving the
milliseconds summed over its hooks and over the ones the host waits for
(the declaration makes PostToolUse async on both), and once with git
counted, giving the processes it started: each hook's launch, the git runs
eva-md made, and the jobs it started in the background.
"""
import shutil

from evabench import LAUNCH_PROCESSES, Home, scenario

CALLS = 50
# Tool calls as each host reports them, all of them ones its PostToolUse
# matcher takes.
TOOLS = {
    "claude-code": [
        ("Bash", {"command": "go test ./internal/inbox/ -run TestWalk"}, {"stdout": "ok  \tinbox\t0.412s", "stderr": "", "interrupted": False}),
        ("Read", {"file_path": "/src/example/internal/inbox/walk.go"}, {"type": "text", "file": {"numLines": 120}}),
        ("Edit", {"file_path": "/src/example/internal/inbox/walk.go", "old_string": "limit := 64", "new_string": "limit := 96"}, {"filePath": "/src/example/internal/inbox/walk.go"}),
        ("Grep", {"pattern": "func Walk", "path": "/src/example/internal"}, {"numFiles": 2}),
        ("Glob", {"pattern": "**/*_test.go", "path": "/src/example"}, {"numFiles": 31}),
        ("Write", {"file_path": "/src/example/docs/notes.md", "content": "Walk reads the newest files first.\n"}, {"type": "create"}),
        ("TodoWrite", {"todos": [{"content": "Raise the walk limit", "status": "completed"}, {"content": "Run the race tests", "status": "in_progress"}]}, {}),
    ],
    "codex": [
        ("Bash", {"command": "go test ./internal/inbox/ -run TestWalk"}, "ok  \tinbox\t0.412s"),
        ("apply_patch", {"input": "*** Begin Patch\n*** Update File: internal/inbox/walk.go\n@@\n-limit := 64\n+limit := 96\n*** End Patch\n"}, "Success. Updated the following files:\nM internal/inbox/walk.go"),
        ("update_plan", {"plan": [{"step": "Raise the walk limit", "status": "completed"}, {"step": "Run the race tests", "status": "in_progress"}]}, "Plan updated"),
    ],
}
SYNC = {"UserPromptSubmit", "Stop"}


@scenario(400, "turn-of-50-tool-calls")
def turn(run):
    run.put("turn_tool_calls", CALLS)
    for host in ("claude-code", "codex"):
        key = "turn_" + host.replace("-", "_")
        with Home(run.binary, run.cwd) as h:
            h.wiki()
            h.hook(host, "SessionStart", "turn-warm-up", source="startup")
            h.take_sweep()
            h.hook(host, "SessionStart", "turn-timed", source="startup")
            times, total, blocking = one_turn(h, host, "turn-timed")[:3]
            shim = h.path/"counting"/"git"
            shim.parent.mkdir(mode=0o700)
            real = shutil.which("git", path=h.env["PATH"])
            if not real:
                raise RuntimeError("git is not on PATH")
            shim.write_text('#!/bin/sh\necho >> "$HOME/git-runs"\nexec %s "$@"\n' % real); shim.chmod(0o700)
            counted = dict(h.env, PATH=str(shim.parent)+":"+h.env["PATH"])
            h.hook(host, "SessionStart", "turn-counted", source="startup")
            jobs = h.runs()
            hooks = one_turn(h, host, "turn-counted", counted)[3]
            gits = h.path/"git-runs"
            gits = len(gits.read_text().splitlines()) if gits.exists() else 0
            run.put(key+"_hooks", hooks)
            run.put(key+"_processes", hooks*LAUNCH_PROCESSES[host] + gits + h.runs()-jobs)
            run.put(key+"_ms_summed", round(total, 3))
            run.put(key+"_blocking_ms_summed", round(blocking, 3))
            run.figure(key+"_tool", times, target=50)


def one_turn(h, host, sid, env=None):
    """Runs one turn in a chat that has started; returns the tool hooks'
    milliseconds, the sum over every hook, the sum over the hooks the host
    waits for, and the hooks run."""
    ids = {"turn_id": "1"} if host == "codex" else {"prompt_id": "1"}
    sums = {True: 0.0, False: 0.0}
    hooks = 0

    def timed(event, **kwargs):
        nonlocal hooks
        _, elapsed = h.hook(host, event, sid, env=env, **ids, **kwargs)
        sums[event in SYNC] += elapsed
        hooks += 1
        return elapsed
    timed("UserPromptSubmit", prompt="Raise the inbox walk limit to 96 and run the tests that cover it.")
    tools = TOOLS[host]
    times = [timed("PostToolUse", tool_name=tools[i % len(tools)][0], tool_input=tools[i % len(tools)][1],
                   tool_response=tools[i % len(tools)][2]) for i in range(CALLS)]
    timed("Stop", last_assistant_message="The walk limit is 96 now and the inbox tests pass.")
    return times, sums[True]+sums[False], sums[True], hooks
