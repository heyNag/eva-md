#!/usr/bin/env python3
"""Offline capture and resume benchmark for a built eva-md binary.

All state goes to isolated temporary homes; the real setup is never touched.
--cwd must be an existing folder outside temporary folders (hooks skip TMPDIR
and /tmp). Use a git repository, such as this checkout, since that is the case
the latency targets are for. The transcript-recovery figure is
sensitive to machine load, so compare builds in alternating runs. The
installed binary the hooks spawn for an update or maintain is a stand-in that
only counts its runs, so a benchmark never starts a real update, a model call
or a commit. Two searches over the padded inbox, before and after eva-md
index, are reported as figures only.

The scenarios come from a list: the padded inbox below, and every file in
scripts/bench/ (see core.py there), each in a home of its own. Every hook
figure holds the start of its processes, on the host's launch path.

With --compare OLD NEW, the gate: each round runs every scenario on OLD and
NEW twice each, alternating, and the gate exits 1 when NEW is slower than
OLD on a hook figure whose controls show the machine quiet enough to judge
it, or fails a scenario clearly more often than OLD (see decide in
scripts/bench/core.py), 0 otherwise, and 2 when it could not run.
"""
import argparse
import importlib.util
import json
import os
from pathlib import Path
import platform
import select
import subprocess
import sys
import time
from datetime import datetime, timedelta, timezone

BENCH = Path(__file__).resolve().parent/"bench"


def load_file(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    sys.modules[name] = module
    spec.loader.exec_module(module)
    return module


def load(folder=BENCH):
    """Loads core.py, as evabench, then every scenario file in folder, by
    their paths, since python3 -I ignores the script's own folder."""
    sys.dont_write_bytecode = True
    core = load_file("evabench", folder/"core.py")
    for path in sorted(folder.glob("*.py")):
        if path.name != "core.py" and not path.name.startswith("test_"):
            load_file("evabench_"+path.stem, path)
    return core


class MCP:
    """One eva-md mcp server over pipes, as a host starts it."""

    def __init__(self, binary, env, cwd):
        self.p = subprocess.Popen([str(binary), "mcp", "--host", "codex"], stdin=subprocess.PIPE,
                                  stdout=subprocess.PIPE, stderr=subprocess.PIPE, env=env, cwd=str(cwd))
        self.buf, self.next_id = b"", 0

    def request(self, method, params, limit=10):
        """Sends one request and returns its answer and the milliseconds it took."""
        self.next_id += 1
        line = json.dumps({"jsonrpc": "2.0", "id": self.next_id, "method": method, "params": params}).encode()+b"\n"
        start = time.perf_counter()
        self.p.stdin.write(line); self.p.stdin.flush()
        while True:
            reply = json.loads(self.readline(start+limit))
            if reply.get("id") == self.next_id:
                return reply, (time.perf_counter()-start)*1000

    def call(self, name, arguments):
        reply, elapsed = self.request("tools/call", {"name": name, "arguments": arguments})
        result = reply.get("result") or {}
        if result.get("isError") or len(result.get("content", [])) != 1:
            raise RuntimeError("eva-md mcp: %s failed" % name)
        return result["content"][0]["text"], elapsed

    def readline(self, deadline):
        while b"\n" not in self.buf:
            ready, _, _ = select.select([self.p.stdout], [], [], max(deadline-time.perf_counter(), 0))
            chunk = os.read(self.p.stdout.fileno(), 1 << 16) if ready else b""
            if not chunk:
                raise RuntimeError("eva-md mcp: no answer")
            self.buf += chunk
        line, self.buf = self.buf.split(b"\n", 1)
        return line

    def close(self):
        """Closes stdin and returns the milliseconds until the server exited."""
        start = time.perf_counter()
        self.p.stdin.close()
        self.p.wait(timeout=5)
        elapsed = (time.perf_counter()-start)*1000
        if self.p.returncode or self.p.stderr.read():
            raise RuntimeError("eva-md mcp failed or wrote diagnostics")
        return elapsed


def mcp_timings(binary, env, cwd):
    """Cold starts timed to the initialize answer and to the exit after
    stdin closes, then eva_search over the padded inbox, eva_show of a ref
    it found and eva_status, each timed over one server."""
    initialize = {"protocolVersion": "2025-06-18", "capabilities": {}, "clientInfo": {"name": "benchmark", "version": "1"}}
    starts, exits = [], []
    for _ in range(10):
        start = time.perf_counter()
        server = MCP(binary, env, cwd)
        reply, _ = server.request("initialize", initialize)
        starts.append((time.perf_counter()-start)*1000)
        if reply["result"]["serverInfo"]["name"] != "eva-md":
            raise RuntimeError("eva-md mcp: not eva-md's server")
        exits.append(server.close())
    server = MCP(binary, env, cwd)
    server.request("initialize", initialize)
    searches, shows, statuses = [], [], []
    ref = None
    for _ in range(20):
        text, elapsed = server.call("eva_search", {"terms": ["MARIGOLD"]})
        searches.append(elapsed)
        ref = ref or next((part for line in text.splitlines() for part in line.split(" · ") if part.startswith("s:")), None)
    if not ref:
        raise RuntimeError("eva_search found no planted exchange")
    for _ in range(20):
        text, elapsed = server.call("eva_show", {"ref": ref})
        if "MARIGOLD" not in text:
            raise RuntimeError("eva_show did not open the ref eva_search found")
        shows.append(elapsed)
    for _ in range(20):
        text, elapsed = server.call("eva_status", {})
        if not text.startswith("scope: "):
            raise RuntimeError("eva_status gave no status")
        statuses.append(elapsed)
    server.close()
    return starts, exits, searches, shows, statuses


def padded_inbox(run, evabench):
    """The benchmark's first scenario: a 50 MB inbox, a wiki of 400 system
    pages and a full recall index, with the records of busy days."""
    binary, cwd = run.binary, run.cwd
    with evabench.Home(binary, cwd, prefix="eva-md-m1-") as h:
        home, env, spawned, hook = str(h.path), h.env, h.spawned, h.hook
        # With no config, a capture-only install, the day's first event
        # starts maintain, for its search index step: the stand-in's one
        # maintain run.
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
        # Chats a program ran, newer than the person's: 64 files of 32 KiB of
        # short tool lines and replies with quotes and line breaks, as an
        # agent loop leaves them, 2 MiB in all, the most the brief reads
        # beyond its 64 files. Each starts with the host_prompt event and has
        # no typed prompt. Timed, then removed, so nothing later sees them.
        programs=[]
        def entry(sid,role,kind,text):
            return json.dumps(dict(sample,session=sid,role=role,kind=kind,text=text)).encode()+b"\n"
        for i in range(64):
            sid="m1-program-%02d"%i
            body=entry(sid,"event","event","session_start: startup")+entry(sid,"event","event","host_prompt")
            pair=entry(sid,"tool","tool",'Bash go test ./internal/... -run "TestReplay" (exit 0)')+entry(sid,"assistant","reply",'The worker\'s "report": the step passed.\n\tDetail: /src/replay/step.go:12 returned {"ok": true}.\n'*3)
            while len(body)+len(pair)<=32<<10:
                body+=pair
            path=files[0].parent/("claude-code-"+sid+".jsonl")
            path.write_bytes(body);path.chmod(0o600)
            programs.append(path)
        startup_programs=[]
        for i in range(10):
            output,elapsed=hook("codex","SessionStart","m1-programs-"+str(i),source="startup")
            value=json.loads(output)
            context=value.get("hookSpecificOutput",{}).get("additionalContext",value.get("additionalContext",""))
            if "MARIGOLD" not in context:
                raise RuntimeError("the chats a program ran pushed the person's chat out of the brief")
            startup_programs.append(elapsed)
        for path in programs:
            path.unlink()
        # Search with no window over every project: first as a search finds
        # an inbox with no index, which also indexes up to 16 MiB of it for
        # the next search, then after eva-md index. Figures only: process
        # start dominates at 50 MB.
        def search():
            start=time.perf_counter()
            r=subprocess.run([str(binary),"search","--all","--days","0","--cwd",str(cwd),"MARIGOLD"],text=True,capture_output=True,env=env,timeout=30)
            elapsed=(time.perf_counter()-start)*1000
            if r.returncode or "MARIGOLD" not in r.stdout:
                raise RuntimeError("search failed")
            return r.stdout,elapsed
        _,search_before=search()
        start=time.perf_counter()
        if subprocess.run([str(binary),"index"],capture_output=True,env=env,timeout=120).returncode:
            raise RuntimeError("eva-md index failed")
        index_ms=(time.perf_counter()-start)*1000
        found,search_after=search()
        if " through the index of " not in found:
            raise RuntimeError("the search after eva-md index did not use it")
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
        (wiki/"index.md").write_text('# Knowledge Base\n')
        # The wiki as init leaves it, its instance in its schema and in the
        # config, which the hooks need to start the day's maintain.
        schema=wiki/".eva-md/schema.toml"
        schema.parent.mkdir(mode=0o700)
        schema.write_text('schema_version = 1\ninstance = "bench-0a1b2c3d"\n');schema.chmod(0o600)
        config=Path(home)/".config/eva-md/config.toml"
        config.parent.mkdir(parents=True,mode=0o700)
        config.write_text('wiki_path = %s\ninstance = "bench-0a1b2c3d"\n'%json.dumps(str(wiki)));config.chmod(0o600)
        # With a wiki set up, the start-of-chat notice reads the newest run
        # log and commit records, at most 64 KB of each day's and 16 KB of
        # each month's: here a busy day of updates on each of two days and two
        # months of daily commits, all healthy, so no notice shows. The day's
        # maintain started already, so each finds its marker; they check for
        # a push to retry at most every 30 minutes, and otherwise stat that
        # check's marker.
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
            if i==0:
                # The day's first chat start with updates on asked for the
                # day's sweep, giving the job the hook's day. The stand-in
                # can't take it, so record it as taken on that day, as the
                # job would, and later chat starts ask for none.
                deadline=time.time()+10
                sweeps=[]
                while not sweeps and time.time()<deadline:
                    sweeps=[a for line in spawned.read_text().splitlines() for a in line.split() if a.startswith("--sweep=")]
                    time.sleep(0.05)
                if len(sweeps)!=1:
                    raise RuntimeError("the day's first chat start did not ask for the day's sweep once")
                (state/"updates"/"sweep").write_text(sweeps[0][len("--sweep="):])
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
        # event did for maintain.
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
        # The first chat start's sweep came alone, a job of its own.
        expected=len(stop_spawn)+len(ends)+2
        runs=lambda: len(spawned.read_text().splitlines())-1
        deadline=time.time()+10
        while runs()<expected and time.time()<deadline:
            time.sleep(0.05)
        started=spawned.read_text().splitlines()
        if runs()!=expected or started.count("maintain")!=1 or sum(line.startswith("update --sweep=") for line in started)!=1:
            raise RuntimeError("the hooks started the stand-in %d times, not %d with maintain and the sweep once each"%(runs(),expected))
        # The MCP server, started in the project as Claude Code starts it,
        # with the padded inbox, the wiki and the records above in place.
        mcp_starts,mcp_exits,mcp_searches,mcp_shows,mcp_statuses=mcp_timings(binary,env,cwd)
        p95=lambda values: sorted(values)[int((len(values)-1)*.95)]
        code=code_scenarios(hook,cwd,wiki,state,sample["project"])
        recalled=recall_scenario(hook,wiki,sample["project"])
        report={"offline":True,"isolated_home":True,"cross_host_resume":True,"inbox_bytes":files[0].stat().st_size,
                "capture_samples":len(capture),"capture_p95_ms":round(p95(capture),3),
                "startup_samples":len(startup),"startup_p95_ms":round(p95(startup),3),
                "startup_with_programs_samples":len(startup_programs),"startup_with_programs_p95_ms":round(p95(startup_programs),3),
                "startup_with_programs_target_met":p95(startup_programs)<300,
                "transcript_capture_samples":len(recovery),"transcript_capture_p95_ms":round(p95(recovery),3),
                "transcript_capture_target_met":p95(recovery)<50,
                "prompt_after_brief_samples":len(prompts),"prompt_after_brief_p95_ms":round(p95(prompts),3),
                "prompt_after_brief_target_met":p95(prompts)<50,
                "prompt_with_pointers_samples":len(pointed),"prompt_with_pointers_p95_ms":round(p95(pointed),3),
                "prompt_with_pointers_target_met":p95(pointed)<50,
                "recall_index_rows":recalled["rows"],"recall_index_bytes":recalled["bytes"],
                "prompt_with_recall_samples":len(recalled["prompts"]),"prompt_with_recall_p95_ms":round(p95(recalled["prompts"]),3),
                "prompt_with_recall_target_met":p95(recalled["prompts"])<50,
                "capture_with_spawn_samples":len(stop_spawn),"capture_with_spawn_p95_ms":round(p95(stop_spawn),3),
                "capture_with_spawn_target_met":p95(stop_spawn)<50,
                "session_end_samples":len(ends),"session_end_p95_ms":round(p95(ends),3),
                "session_end_target_met":p95(ends)<200,"spawns":runs(),
                "capture_target_met":p95(capture)<50,"startup_target_met":p95(startup)<300,
                "startup_with_records_samples":len(startup_records),"startup_with_records_p95_ms":round(p95(startup_records),3),
                "startup_with_records_target_met":p95(startup_records)<300,
                "search_before_index_ms":round(search_before,3),"index_ms":round(index_ms,3),
                "search_after_index_ms":round(search_after,3),
                "mcp_initialize_samples":len(mcp_starts),"mcp_initialize_p95_ms":round(p95(mcp_starts),3),
                "mcp_initialize_target_met":p95(mcp_starts)<100,
                "mcp_exit_max_ms":round(max(mcp_exits),3),"mcp_exit_target_met":max(mcp_exits)<1000,
                "mcp_search_samples":len(mcp_searches),"mcp_search_p95_ms":round(p95(mcp_searches),3),
                "mcp_search_target_met":p95(mcp_searches)<5000,
                "mcp_show_samples":len(mcp_shows),"mcp_show_p95_ms":round(p95(mcp_shows),3),
                "mcp_show_target_met":p95(mcp_shows)<300,
                "mcp_status_samples":len(mcp_statuses),"mcp_status_p95_ms":round(p95(mcp_statuses),3),
                "mcp_status_target_met":p95(mcp_statuses)<300,
                "checked_at":datetime.now(timezone.utc).isoformat()}
        for name,values,target in (("code_brief",code["brief"],300),("code_prompt",code["prompts"],50)):
            report.update({name+"_samples":len(values),name+"_p95_ms":round(p95(values),3) if values else None,
                           name+"_target_met":bool(values) and p95(values)<target})
        for key,value in report.items():
            run.put(key,value)
        # The hook figures the gate compares, with their targets. The MCP
        # server's and search's figures are not a hook's, and are reported
        # only.
        for name,values,target in (("capture",capture,50),("startup",startup,300),("startup_with_programs",startup_programs,300),
                                   ("transcript_capture",recovery,50),("prompt_after_brief",prompts,50),
                                   ("prompt_with_pointers",pointed,50),("prompt_with_recall",recalled["prompts"],50),
                                   ("capture_with_spawn",stop_spawn,50),("session_end",ends,200),
                                   ("startup_with_records",startup_records,300),("code_brief",code["brief"],300),
                                   ("code_prompt",code["prompts"],50)):
            run.metric(name,values,target)


def run_once(core, binary, cwd, failures=None):
    """Runs every scenario, in the order of their places, on binary; returns
    the run with its report and the gate's samples. A scenario that fails
    stops the run, unless failures is given, as by the gate: then any error
    it raises is added there under the scenario's name, the samples it gave
    are dropped, and the next scenario runs. The gate counts the failure
    against the binary that had it."""
    run = core.Run(binary, cwd)
    for s in core.ordered():
        if failures is None:
            s.fn(run)
            continue
        before = set(run.metrics)
        try:
            s.fn(run)
        except Exception as e:
            failures.setdefault(s.name, []).append("%s: %s" % (type(e).__name__, e))
            for name in set(run.metrics) - before:
                del run.metrics[name]
    return run


def write(path, data):
    path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    path.write_text(data); path.chmod(0o600)


ROLE_NAMES = {"old": "old", "new": "new", "old_again": "old (again)", "new_again": "new (again)"}


def compare(core, a):
    """The gate: each round runs OLD and NEW twice each, as the roles old,
    new, old_again and new_again, in the order core.schedule gives. Prints
    the verdict and returns its exit code."""
    old, new = a.compare[0].resolve(strict=True), a.compare[1].resolve(strict=True)
    cwd = a.cwd.resolve(strict=True)
    load = os.getloadavg()[0]
    with core.Home(new, cwd) as h:
        for binary in (old, new):
            # Each binary's first start is outside the timings, since macOS
            # checks a new executable then.
            subprocess.run([str(binary), "version"], capture_output=True, env=h.env, timeout=30)
    binaries = {role: (old if core.BINARY[role] == "old" else new) for role in core.ROLES}
    samples = {role: {} for role in core.ROLES}
    # A scenario that fails in a run leaves that run's figures of it out,
    # and counts against the binary that failed it (core.scenarios).
    targets, runs, failures = {}, [], {role: {} for role in core.ROLES}
    for rnd, role in core.schedule(a.rounds):
        print("benchmark: round %d of %d, %s" % (rnd, a.rounds, ROLE_NAMES[role]), file=sys.stderr, flush=True)
        run = run_once(core, binaries[role], cwd, failures[role])
        for name, values in run.metrics.items():
            samples[role].setdefault(name, []).extend(values)
        targets.update(run.targets)
        runs.append({"round": rnd, "binary": role, "report": run.report})
    # A ceiling factor of 0 turns the ceilings off.
    ceilings = {name: (target*a.ceiling_factor if target is not None and a.ceiling_factor else None)
                for name, target in targets.items()}
    verdict = core.decide(samples, ceilings, failures, a.rounds)
    gate = {"verdict": verdict["verdict"], "exit": verdict["exit"], "old": str(old), "new": str(new),
            "rounds": a.rounds, "schedule": [role for _, role in core.schedule(a.rounds)],
            "ceiling_factor": a.ceiling_factor, "launch_paths": core.LAUNCH_PATHS,
            "load_average_at_start": round(load, 2), "decision": verdict, "scenario_failures": failures, "runs": runs,
            # Every sample, so the verdict can be worked out again.
            "samples": {role: {name: [round(v, 3) for v in values] for name, values in sorted(by.items())}
                        for role, by in samples.items()},
            "checked_at": datetime.now(timezone.utc).isoformat()}
    if a.output:
        write(a.output, json.dumps(gate, indent=2)+"\n")
    if a.baseline:
        write(a.baseline, json.dumps(baseline(core, a, samples, targets, runs, load), indent=2)+"\n")
    for name, m in verdict["metrics"].items():
        print("%-28s old %8.1f ms  new %8.1f ms  ratio %6s  lower bound %6s  noise %6s  %s" % (
            name, m["p95_base_ms"], m["p95_ms"], m["ratio"], m["ratio_low"], m["noise"],
            "; ".join(m["reasons"] + m["unsettled"]) or "ok"))
    for name in verdict["not_compared"]:
        print("%-28s not compared: the old binary gave it no samples" % name)
    for role, by in failures.items():
        for name, errors in sorted(by.items()):
            print("the %s binary failed the scenario %s in %d of %d runs: %s" % (
                ROLE_NAMES[role], name, len(errors), a.rounds, errors[0]))
    for failure in verdict["failures"]:
        print("fails: %s" % failure)
    print("latency gate: %s" % verdict["verdict"] + {
        "not judged": " (a figure rose no more than the machine put between two sets of runs that a change cancels out of, so it was too noisy to judge)",
        "could not run": " (a scenario failed in every run of both binaries)",
        "fail": "", "pass": ""}[verdict["verdict"]])
    return verdict["exit"]


def gate(prepare, a):
    """Runs the gate on the scenarios prepare loads; anything that stops it,
    of any kind, is exit 2, never the 1 of a regression."""
    try:
        return compare(prepare(), a)
    except Exception as e:
        print("latency gate: could not run: %s: %s" % (type(e).__name__, e), file=sys.stderr)
        print("latency gate: could not run")
        return 2


def baseline(core, a, samples, targets, runs, load):
    """The baseline file: the new binary's figures over its runs, both its
    roles, as docs/v2/baselines/README.md describes."""
    counts, per_run = {}, {}
    for run in runs:
        if core.BINARY[run["binary"]] == "new":
            for key, value in run["report"].items():
                if key.endswith("_processes") or key.endswith("_hooks"):
                    counts.setdefault(key, value)
                elif key.endswith("_ms_summed") or (key.startswith("real_install_brief_command_") and key.endswith("_ms")):
                    per_run.setdefault(key, []).append(value)
    new = {name: core.pooled(samples, core.GATE_SPLIT[1], name) for name in sorted(targets)}
    return {"v": 1, "measured_on": platform.system().lower(), "machine": "%s, %d CPUs" % (platform.machine(), os.cpu_count() or 0),
            "user": "root" if os.geteuid() == 0 else "not root",
            "load_average_at_start": round(load, 2), "commit": a.commit or "",
            "runs": sum(1 for run in runs if core.BINARY[run["binary"]] == "new"),
            "p95": "nearest rank, process start included", "taken_at": datetime.now(timezone.utc).isoformat(timespec="seconds"),
            "metrics": {name: {"n": len(v), "p95_ms": round(core.nearest_rank(v), 3), "median_ms": round(core.nearest_rank(v, 0.5), 3),
                               "target_ms": targets.get(name)} for name, v in new.items() if v},
            "counts": counts, "per_run": per_run}


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("--binary", type=Path)
    p.add_argument("--compare", type=Path, nargs=2, metavar=("OLD", "NEW"))
    p.add_argument("--cwd", type=Path, required=True)
    p.add_argument("--output", type=Path)
    p.add_argument("--rounds", type=int, default=2, help="gate: rounds, each of which runs each binary twice")
    p.add_argument("--ceiling-factor", type=float, default=2.0, help="gate: a hook's ceiling is its target times this; 0 for none")
    p.add_argument("--baseline", type=Path, help="gate: write the new binary's figures as a baseline file here")
    p.add_argument("--commit", help="gate: the new binary's commit, for the baseline file")
    a = p.parse_args()
    if (a.binary is None) == (a.compare is None):
        p.error("give --binary, or --compare OLD NEW")
    if a.binary and (a.baseline or a.commit):
        p.error("--baseline and --commit are for --compare")
    if a.rounds < 1 or a.ceiling_factor < 0:
        p.error("--rounds must be 1 or more, and --ceiling-factor 0 or more")

    def prepare():
        core = load()
        core.scenario(100, "padded-inbox")(lambda run: padded_inbox(run, core))
        return core
    if a.compare:
        return gate(prepare, a)
    core = prepare()
    run = run_once(core, a.binary.resolve(strict=True), a.cwd.resolve(strict=True))
    encoded = json.dumps(run.report, indent=2)+"\n"
    if a.output:
        write(a.output, encoded)
    print(encoded, end="")
    return 0


def code_scenarios(hook, cwd, wiki, state, key):
    """Times the code notes. Startup briefs in --cwd, whose project key the
    inbox recorded, point at 3 systems that cite its repo at HEAD~1, so each
    brief counts the commits since, 3 git calls, and every pointer must say
    the code moved. Then prompts each name 3 of the 400 systems, whose map
    lines cite a made-up repo's code and whose entries in a planted code
    status, one for each of the 400 pages, say it moved 14 commits: every
    pointer must say so, read from the status with no git. A --cwd repo with
    a single commit skips the briefs."""
    head1=subprocess.run(["git","-C",str(cwd),"rev-parse","HEAD~1"],capture_output=True,text=True)
    systems=wiki/"systems"
    mapfile=systems/"map.md"
    text=mapfile.read_text()
    briefs=[]
    if head1.returncode==0 and " " not in key:
        for letter in "abc":
            (systems/("code-"+letter+".md")).write_text('---\ntype: system\ntitle: Code %s\nas_of: "2026-10-30"\n---\n\n## Summary\nCode.\n'%letter.upper())
            text+="- [Code %s](code-%s.md): Code of this repo · repos: %s · code: %s@%s · as of 2026-10-30\n"%(letter.upper(),letter,key,key,head1.stdout.strip())
        mapfile.write_text(text)
        for i in range(10):
            out,elapsed=hook("codex","SessionStart","m1-code-"+str(i),source="startup")
            value=json.loads(out)
            context=value.get("hookSpecificOutput",{}).get("additionalContext",value.get("additionalContext",""))
            lines=[l for l in context.split("\n") if l.startswith("System: Code ") and " (code moved " in l]
            if len(lines)!=3:
                raise RuntimeError("a startup brief did not say how far the code of 3 systems moved")
            briefs.append(elapsed)
    commit="4e2bf4c0a1b2"
    pages=[]
    for i in range(400):
        slug="svc-%04d-ledger"%i
        old="- [Ledger Service %04d](%s.md): Keeps the books of region %04d · as of 2026-10-28\n"%(i,slug,i)
        new="- [Ledger Service %04d](%s.md): Keeps the books of region %04d · repos: github.com__example__ledger · code: github.com__example__ledger@%s:internal/region%04d · as of 2026-10-28\n"%(i,slug,i,commit,i)
        text=text.replace(old,new)
        pages.append(dict(slug=slug,as_of="2026-10-28",repos=[dict(key="github.com__example__ledger",commit=commit,paths=["internal/region%04d"%i],
                                                                  state="moved",moved=14,clone="/Users/example/code/ledger",branch="main",head="0"*40)]))
    mapfile.write_text(text)
    status=state/"code-status.json"
    status.write_text(json.dumps(dict(v=1,at=datetime.now().astimezone().isoformat(timespec="seconds"),duration_ms=1800,pages=pages))+"\n")
    status.chmod(0o600)
    prompts=[]
    for c in range(25):
        hook("codex","SessionStart","m1-code-prompts-%d"%c,source="startup")
        n=300+c*3
        prompt="Why does LedgerSync%04d drop rows when svc-%04d-ledger retries, and does the ledger gateway zone %04d see it?"%(n,n+1,n+2)
        output,elapsed=hook("codex","UserPromptSubmit","m1-code-prompts-%d"%c,turn_id="0",prompt=prompt)
        value=json.loads(output)
        context=value.get("hookSpecificOutput",{}).get("additionalContext",value.get("additionalContext",""))
        if context.count(" (code moved 14 commits")!=3:
            raise RuntimeError("a prompt naming three systems did not get three pointers saying their code moved")
        prompts.append(elapsed)
    return {"brief":briefs,"prompts":prompts}


def recall_scenario(hook, wiki, key):
    """Times prompts that recall knowledge. The recall index is full: active
    facts near its 512 KiB bound, each titled by its own number, desk and
    route. They are another project's, which a system page shares with the
    project in --cwd, whose key is key, as the repos index says. Each chat
    starts untimed and gets four prompts, each asking about three facts it
    has not had by their numbers, desks and routes, so every prompt reads and
    scores the whole index, beside the full names index above, then reads
    the repos index for the facts' project, and must get the three facts in
    one fenced block."""
    repos=wiki/".eva-md/repos.tsv"
    repos.write_text("# eva-md repos index 1\nsystems/quartz-desks\t%s\tgithub.com__example__ledger\n"%key);repos.chmod(0o600)
    lines=["# eva-md knowledge index 1\n"]
    size,n=len(lines[0]),0
    while True:
        statement=("Settlement batches for quartz %04d close at noon UTC; late entries roll to the next business day and are "
                   "flagged for review by the ledger team."%(n+1))
        line="\t".join(["k%d"%(n+1),"fact","observed","project","github.com__example__ledger","2026-10-28",
                        "2026-10-28T10:00:00+07:00","","Quartz %04d settlement cutoff, desk D%04d, route R%04d"%((n+1,)*3),statement,
                        "internal/quartz%04d"%(n+1)])+"\n"
        if size+len(line.encode())>512<<10:
            break
        lines.append(line)
        size,n=size+len(line.encode()),n+1
    text="".join(lines)
    if not 500<<10<=len(text.encode())<=512<<10:
        raise RuntimeError("the recall index is not near its bound")
    index=wiki/".eva-md/knowledge.tsv"
    index.write_text(text);index.chmod(0o600)
    prompts=[]
    for c in range(25):
        hook("codex","SessionStart","m1-recall-%d"%c,source="startup")
        for k in range(4):
            numbers=[c*12+k*3+m+1 for m in range(3)]
            prompt=("When do the quartz %04d, %04d and %04d settlement batches close at desks D%04d, D%04d and D%04d on routes "
                    "R%04d, R%04d and R%04d, and where do late entries go?")%tuple(numbers*3)
            output,elapsed=hook("codex","UserPromptSubmit","m1-recall-%d"%c,turn_id=str(k),prompt=prompt)
            value=json.loads(output)
            context=value.get("hookSpecificOutput",{}).get("additionalContext",value.get("additionalContext",""))
            if not context.startswith("eva.md knowledge: ") or "\n````eva-memory\n" not in context or len(context)>1000 or \
               any("\nk%d · fact [observed] · github.com__example__ledger · 2026-10-28: "%m not in context for m in numbers):
                raise RuntimeError("a prompt about three facts did not get them fenced")
            prompts.append(elapsed)
    return {"prompts":prompts,"rows":n,"bytes":len(text.encode())}


if __name__=="__main__":
    sys.exit(main())
