"""Process start, counted: what a hook costs before eva-md does anything.

Every hook figure in the report is timed from before its first process
starts to after its last exits, on the host's launch path: the binary
itself for Claude Code, a shell that runs it for Codex. This scenario says
so in the report, and times a hook that reads no payload and exits at once
on each launch path, which is the floor under every other hook figure.
"""
import subprocess
import time

from evabench import LAUNCH_PATHS, LAUNCH_PROCESSES, Home, launch, scenario

SAMPLES = 100


@scenario(200, "process-start")
def process_start(run):
    run.put("hook_figures_include_process_start", True)
    for host in ("claude-code", "codex"):
        key = host.replace("-", "_")
        run.put("hook_launch_"+key, LAUNCH_PATHS[host])
        run.put("hook_launch_"+key+"_processes", LAUNCH_PROCESSES[host])
    with Home(run.binary, run.cwd) as h:
        for host in ("claude-code", "codex"):
            argv = launch(run.binary, host)
            times = []
            for _ in range(SAMPLES):
                start = time.perf_counter()
                r = subprocess.run(argv, input="", text=True, capture_output=True, env=h.env, timeout=4)
                times.append((time.perf_counter()-start)*1000)
                # An empty payload is no event: the hook writes nothing and
                # adds nothing to a chat, on either path.
                if r.returncode or r.stderr or r.stdout.strip() not in ("", "{}"):
                    raise RuntimeError("a hook with no payload failed or wrote output")
            if h.state.exists() and any(h.state.rglob("*")):
                raise RuntimeError("a hook with no payload wrote state")
            run.figure("process_start_"+host.replace("-", "_"), times)
