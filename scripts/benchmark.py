#!/usr/bin/env python3
"""Offline capture and resume benchmark for a built eva-md binary.

All state goes to an isolated temporary HOME; the real setup is never touched.
--cwd must be an existing folder outside temporary folders (hooks skip TMPDIR
and /tmp). Use a git repository, such as this checkout, since that is the case
the latency targets are for. The transcript-recovery figure is
sensitive to machine load, so compare builds in alternating runs. The
installed binary the hooks spawn for an update or maintain is a stand-in that
only counts its runs, so a benchmark never starts a real update, a model call
or a commit.
"""
import argparse
import json
import os
from pathlib import Path
import subprocess
import tempfile
import time
from datetime import datetime, timedelta, timezone


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
                   EVA_MD_INNER="", CLAUDE_CODE_REMOTE="", CLAUDE_CODE_ENTRYPOINT="", TMPDIR="/tmp")
        env.pop("GIT_DIR", None); env.pop("GIT_WORK_TREE", None)
        # The installed binary the hooks spawn: the stand-in that counts its
        # runs. Its first run is outside the timings, since macOS checks a new
        # executable then.
        standin=Path(home)/".local/share/eva-md/bin/eva-md"
        standin.parent.mkdir(parents=True,mode=0o700)
        standin.write_text('#!/bin/sh\necho "$*" >> "$HOME/spawned"\n');standin.chmod(0o700)
        subprocess.run([str(standin),"warm-up"],env=env,check=True,timeout=10)
        spawned=Path(home)/"spawned"

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
        # Every prompt after a brief checks the thread's Now for a refresh. The
        # wiki holds 200 other projects' now pages and none of this one's, which
        # that check must not read.
        wiki=Path(home)/"wiki"
        for i in range(200):
            folder=wiki/"projects"/("other-%03d"%i)
            folder.mkdir(parents=True,mode=0o700)
            (folder/"now.md").write_text('---\ntype: now\ntitle: Other\nremote: "example.com/other/%03d"\n---\n'%i)
        (wiki/"index.md").write_text('---\nokf_version: "0.2"\n---\n\n# Knowledge Base\n')
        # The wiki as init leaves it, its instance in its schema and in the
        # config, which the hooks need to start the day's maintain.
        schema=wiki/".eva-md/schema.toml"
        schema.parent.mkdir(mode=0o700)
        schema.write_text('schema_version = 1\nokf_spec = "0.2"\ninstance = "bench-0a1b2c3d"\n');schema.chmod(0o600)
        config=Path(home)/".config/eva-md/config.toml"
        config.parent.mkdir(parents=True,mode=0o700)
        config.write_text('wiki_path = %s\ninstance = "bench-0a1b2c3d"\n'%json.dumps(str(wiki)));config.chmod(0o600)
        # With a wiki set up, the start-of-chat notice reads the newest run
        # log and commit records, at most 64 KB of each day's and 16 KB of
        # each month's: here a busy day of updates on each of two days and two
        # months of daily commits, all healthy, so no notice shows. The first
        # of these starts the day's maintain, one open of its marker and a
        # spawn; the others check for a push to retry at most every 30
        # minutes, and otherwise stat that check's marker.
        state=Path(home)/".local/state/eva-md"
        now=datetime.now().astimezone()
        stamp=lambda t: t.isoformat(timespec="seconds")
        def plant(path, rows):
            path.parent.mkdir(parents=True,exist_ok=True,mode=0o700)
            path.write_text("".join(json.dumps(r)+"\n" for r in rows));path.chmod(0o600)
        update=lambda t,i: dict(at=stamp(t),host=("claude-code","codex")[i%2],session="0199a000-%04d"%(i%7),
                                project="github.com__example__project",thread="main",outcome="ok",chunk_bytes=30000,pages=1,
                                duration_ms=40000,tokens_in=20000,tokens_out=1500,model="model-x",effort="high",effort_used="high",
                                rules_version=1,boundary_to_saved_ms=60000,calls=1)
        for day in (now-timedelta(days=1),now):
            plant(state/"updates"/(day.strftime("%Y-%m-%d")+".jsonl"),[update(day-timedelta(minutes=2*(200-i)),i) for i in range(200)])
        commits=[dict(at=stamp(now-timedelta(days=60-i)),outcome="committed",reason="",pages=5,validate_errors=0,unstaged=[],push="ok") for i in range(60)]
        for month in sorted({c["at"][:7] for c in commits}):
            plant(state/"commits"/(month+".jsonl"),[c for c in commits if c["at"].startswith(month)])
        startup_records=[]
        for i in range(10):
            _,elapsed=hook("codex","SessionStart","m1-records-"+str(i),source="startup")
            startup_records.append(elapsed)
        # With a wiki set up, updates are on: each Stop also checks the
        # session's backlog, one stat, which here never reaches 60 KB.
        capture=[]
        for i in range(100):
            _, elapsed=hook("codex", "Stop", "m1-capture", turn_id=str(i), last_assistant_message="Synthetic timing reply.")
            capture.append(elapsed)
        hook("codex","SessionStart","m1-prompts",source="startup")
        prompts=[]
        for i in range(100):
            _,elapsed=hook("codex","UserPromptSubmit","m1-prompts",turn_id=str(i),prompt="Synthetic timing prompt "+str(i))
            prompts.append(elapsed)
        # Pointers: 400 system pages on the map and a full names index, 2,000
        # names near its 128 KB bound. Each chat starts untimed and gets four
        # prompts, each naming three systems it has not been pointed at, by a
        # name of one word, a slug and a name of several words: so every prompt
        # reads and matches the whole index, checks three pages, reads the map
        # and gets three pointers, the 12 a chat may have.
        systems=wiki/"systems"
        systems.mkdir(mode=0o700)
        rows,maplines=[],[]
        for i in range(400):
            slug,title="svc-%04d-ledger"%i,"Ledger Service %04d"%i
            aliases=("LedgerSync%04d"%i,"ledger-api-%04d"%i,"Ledger Gateway Zone %04d"%i)
            (systems/(slug+".md")).write_text('---\ntype: system\ntitle: %s\ndescription: Keeps the books of region %04d\nstatus: stable\naliases: [%s]\nas_of: "2026-10-28"\nstate: active\n---\n\n## Summary\n%s.\n'%(title,i,", ".join(aliases),title))
            maplines.append("- [%s](%s.md): Keeps the books of region %04d · as of 2026-10-28\n"%(title,slug,i))
            rows+=["%s\tsystems/%s\t%s\n"%(name,slug,title) for name in (title,slug)+aliases]
        (systems/"map.md").write_text("---\ntype: map\ntitle: System map\n---\n\n# System map\n\n"+"".join(maplines))
        names="".join(sorted(rows))
        if len(rows)!=2000 or not 120<<10<=len(names.encode())<=128<<10:
            raise RuntimeError("the names index is not near its bounds")
        (wiki/".eva-md/names.tsv").write_text(names)
        pointed=[]
        for c in range(25):
            hook("codex","SessionStart","m1-pointers-%d"%c,source="startup")
            for k in range(4):
                n=c*12+k*3
                prompt="Why does LedgerSync%04d drop rows when svc-%04d-ledger retries, and does the ledger gateway zone %04d see it? Check this morning's logs first."%(n,n+1,n+2)
                output,elapsed=hook("codex","UserPromptSubmit","m1-pointers-%d"%c,turn_id=str(k),prompt=prompt)
                value=json.loads(output)
                context=value.get("hookSpecificOutput",{}).get("additionalContext",value.get("additionalContext",""))
                if context.count(" show systems/svc-")!=3:
                    raise RuntimeError("a prompt naming three systems did not get three pointers")
                pointed.append(elapsed)
        # Spawning: every Stop with 60 KB or more unprocessed, and every
        # SessionEnd, starts the stand-in detached, as the day's first hook
        # event with the wiki set up did for maintain.
        pad=json.dumps(dict(sample,host="codex",session="m1-spawn")).encode()+b"\n"
        (files[0].parent/"codex-m1-spawn.jsonl").write_bytes(pad*((61<<10)//len(pad)+1))
        stop_spawn=[]
        for i in range(30):
            _,elapsed=hook("codex","Stop","m1-spawn",turn_id=str(i),last_assistant_message="Synthetic timing reply.")
            stop_spawn.append(elapsed)
        ends=[]
        for i in range(30):
            _,elapsed=hook("claude-code","SessionEnd","m1-end-"+str(i))
            ends.append(elapsed)
        expected=len(stop_spawn)+len(ends)+1
        runs=lambda: len(spawned.read_text().splitlines())-1
        deadline=time.time()+10
        while runs()<expected and time.time()<deadline:
            time.sleep(0.05)
        if runs()!=expected or spawned.read_text().splitlines().count("maintain")!=1:
            raise RuntimeError("the hooks started the stand-in %d times, not %d with maintain once"%(runs(),expected))
        p95=lambda values: sorted(values)[int((len(values)-1)*.95)]
        report={"offline":True,"isolated_home":True,"cross_host_resume":True,"inbox_bytes":files[0].stat().st_size,
                "capture_samples":len(capture),"capture_p95_ms":round(p95(capture),3),
                "startup_samples":len(startup),"startup_p95_ms":round(p95(startup),3),
                "transcript_capture_samples":len(recovery),"transcript_capture_p95_ms":round(p95(recovery),3),
                "transcript_capture_target_met":p95(recovery)<50,
                "prompt_after_brief_samples":len(prompts),"prompt_after_brief_p95_ms":round(p95(prompts),3),
                "prompt_after_brief_target_met":p95(prompts)<50,
                "prompt_with_pointers_samples":len(pointed),"prompt_with_pointers_p95_ms":round(p95(pointed),3),
                "prompt_with_pointers_target_met":p95(pointed)<50,
                "capture_with_spawn_samples":len(stop_spawn),"capture_with_spawn_p95_ms":round(p95(stop_spawn),3),
                "capture_with_spawn_target_met":p95(stop_spawn)<50,
                "session_end_samples":len(ends),"session_end_p95_ms":round(p95(ends),3),
                "session_end_target_met":p95(ends)<200,"spawns":runs(),
                "capture_target_met":p95(capture)<50,"startup_target_met":p95(startup)<300,
                "startup_with_records_samples":len(startup_records),"startup_with_records_p95_ms":round(p95(startup_records),3),
                "startup_with_records_target_met":p95(startup_records)<300,
                "checked_at":datetime.now(timezone.utc).isoformat()}
        encoded=json.dumps(report,indent=2)+"\n"
        if a.output:
            a.output.parent.mkdir(parents=True,exist_ok=True,mode=0o700)
            a.output.write_text(encoded);a.output.chmod(0o600)
        print(encoded,end="")


if __name__=="__main__":
    main()
