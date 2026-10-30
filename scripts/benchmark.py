#!/usr/bin/env python3
"""Offline capture and resume benchmark for a built eva-md binary.

All state goes to an isolated temporary HOME; the real setup is never touched.
--cwd must be an existing folder outside temporary folders (hooks skip TMPDIR
and /tmp). Use a git repository, such as this checkout, since that is the case
the latency targets are for. The transcript-recovery figure is
sensitive to machine load, so compare builds in alternating runs.
"""
import argparse
import json
import os
from pathlib import Path
import subprocess
import tempfile
import time
from datetime import datetime, timezone


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("--binary", type=Path, required=True)
    p.add_argument("--cwd", type=Path, required=True)
    p.add_argument("--output", type=Path)
    a = p.parse_args()
    binary = a.binary.resolve(strict=True)
    cwd = a.cwd.resolve(strict=True)
    with tempfile.TemporaryDirectory(prefix="eva-md-m1-") as home:
        env = os.environ.copy()
        env.update(HOME=home, GIT_CONFIG_GLOBAL=str(Path(home)/"gitconfig"), GIT_CONFIG_NOSYSTEM="1",
                   EVA_MD_INNER="", CLAUDE_CODE_REMOTE="", TMPDIR="/tmp")
        env.pop("GIT_DIR", None); env.pop("GIT_WORK_TREE", None)

        def hook(host, event, sid, **kwargs):
            payload = dict(hook_event_name=event, session_id=sid, cwd=str(cwd), **kwargs)
            start = time.perf_counter()
            r = subprocess.run([str(binary), "hook", "--host", host], input=json.dumps(payload),
                               text=True, capture_output=True, env=env, timeout=4)
            elapsed = (time.perf_counter()-start)*1000
            if r.returncode or r.stderr:
                raise RuntimeError("hook failed or wrote diagnostics")
            if event == "Stop" and r.stdout != ("{}" if host == "codex" else ""):
                raise RuntimeError("invalid Stop output")
            return r.stdout, elapsed

        hook("claude-code", "SessionStart", "m1-previous", source="startup")
        for i in range(3):
            hook("claude-code", "UserPromptSubmit", "m1-previous", prompt_id=str(i), prompt="Codename MARIGOLD; decision: keep JSONL; open question: retention; next step: resume tests.")
            hook("claude-code", "Stop", "m1-previous", prompt_id=str(i), last_assistant_message="I suggest verifying resume tests next.")
        files = list((Path(home)/".local/state/eva-md/inbox").glob("*/claude-code-m1-previous.jsonl"))
        if len(files) != 1:
            raise RuntimeError("cwd is a temporary folder, which hooks skip, or capture failed")
        original = files[0].read_bytes()
        sample = json.loads(original.splitlines()[-1])
        sample.update(role="event", kind="event", text="benchmark padding " + "x"*4900)
        line = json.dumps(sample).encode()+b"\n"
        # Synthetic older padding in this isolated fixture only. Preserve the
        # final three exchanges and their exact source line addressing.
        target = 50 << 20
        with files[0].open("wb") as out:
            for _ in range(target//len(line)):
                out.write(line)
            out.write(original)
        startup=[]
        for i in range(10):
            output, elapsed=hook("codex", "SessionStart", "m1-new-"+str(i), source="startup")
            value=json.loads(output)
            context=value.get("hookSpecificOutput",{}).get("additionalContext",value.get("additionalContext",""))
            if "MARIGOLD" not in context or "eva-memory" not in context or len(context)>7000:
                raise RuntimeError("cross-host startup failed")
            startup.append(elapsed)
        capture=[]
        for i in range(100):
            _, elapsed=hook("codex", "Stop", "m1-capture", turn_id=str(i), last_assistant_message="Synthetic timing reply.")
            capture.append(elapsed)
        transcript=Path(home)/".claude/projects/m1-fixture/m1-transcript.jsonl"
        transcript.parent.mkdir(parents=True,mode=0o700)
        transcript.touch(mode=0o600)
        hook("claude-code","SessionStart","m1-transcript",source="startup",transcript_path=str(transcript))
        recovery=[]
        for i in range(100):
            message="Synthetic transcript timing reply "+str(i)
            row={"type":"assistant","sessionId":"m1-transcript","cwd":str(cwd),
                 "timestamp":datetime.now(timezone.utc).isoformat(),
                 "message":{"role":"assistant","stop_reason":"end_turn","content":[{"type":"text","text":message}]}}
            with transcript.open("a") as f:f.write(json.dumps(row)+"\n")
            _,elapsed=hook("claude-code","Stop","m1-transcript",transcript_path=str(transcript),last_assistant_message=message)
            recovery.append(elapsed)
        p95=lambda values: sorted(values)[int((len(values)-1)*.95)]
        report={"offline":True,"isolated_home":True,"cross_host_resume":True,"inbox_bytes":files[0].stat().st_size,
                "capture_samples":len(capture),"capture_p95_ms":round(p95(capture),3),
                "startup_samples":len(startup),"startup_p95_ms":round(p95(startup),3),
                "transcript_capture_samples":len(recovery),"transcript_capture_p95_ms":round(p95(recovery),3),
                "transcript_capture_target_met":p95(recovery)<50,
                "capture_target_met":p95(capture)<50,"startup_target_met":p95(startup)<300,
                "checked_at":datetime.now(timezone.utc).isoformat()}
        encoded=json.dumps(report,indent=2)+"\n"
        if a.output:
            a.output.parent.mkdir(parents=True,exist_ok=True,mode=0o700)
            a.output.write_text(encoded);a.output.chmod(0o600)
        print(encoded,end="")


if __name__=="__main__":
    main()
