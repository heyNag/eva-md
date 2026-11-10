"""What the benchmark's scenarios share: the list they add themselves to, the
isolated home each runs in, and the gate's decision.

scripts/benchmark.py loads this file by its path, as the module evabench,
before any scenario file, since python3 -I ignores the script's own folder.
A scenario file in this folder is loaded by its path too, in name order, and
adds itself with the scenario decorator:

    from evabench import Home, scenario

    @scenario(500, "a-new-scenario")
    def a_new_scenario(run):
        with Home(run.binary, run.cwd) as h:
            run.figure("a_new_hook", [h.hook("codex", "Stop", "s1")[1]], target=50)

Scenarios run in the order of their place, each in a home of its own, and
add their figures to the one report. A file whose name starts with test_ is
a test and is never loaded as a scenario.

The decision (decide) takes samples and gives a verdict; it reads no clock
and starts nothing, so its tests feed it numbers.
"""
import json
import math
import os
from pathlib import Path
import random
import subprocess
import tempfile
import time

# The list of scenarios, in the order they were added; runs sort it by place.
SCENARIOS = []


class Scenario:
    def __init__(self, place, name, fn):
        self.place, self.name, self.fn = place, name, fn


def scenario(place, name):
    """Adds the decorated function to the list, at place. Two scenarios may
    share neither a place nor a name."""
    def add(fn):
        for s in SCENARIOS:
            if s.place == place or s.name == name:
                raise RuntimeError("scenario %s at %d: %s at %d already has that %s" %
                                   (name, place, s.name, s.place, "place" if s.place == place else "name"))
        SCENARIOS.append(Scenario(place, name, fn))
        return fn
    return add


def ordered():
    return sorted(SCENARIOS, key=lambda s: s.place)


class Run:
    """One run of every scenario on one binary: the report the scenarios
    fill and the samples of the metrics the gate compares."""

    def __init__(self, binary, cwd):
        self.binary, self.cwd = binary, cwd
        self.report, self.metrics, self.targets = {}, {}, {}

    def put(self, key, value):
        if key in self.report:
            raise RuntimeError("two scenarios report %s" % key)
        self.report[key] = value

    def metric(self, name, samples, target=None):
        """Gives the gate the samples of a hook figure, in milliseconds, and
        its target, from which the gate's ceiling comes; None for none."""
        if name in self.metrics:
            raise RuntimeError("two scenarios give the metric %s" % name)
        self.metrics[name], self.targets[name] = list(samples), target

    def figure(self, name, samples, target=None):
        """Reports a hook figure as the benchmark always has, name_samples,
        name_p95_ms and, with a target, name_target_met, and gates it."""
        self.put(name+"_samples", len(samples))
        self.put(name+"_p95_ms", round(report_p95(samples), 3) if samples else None)
        if target is not None:
            self.put(name+"_target_met", bool(samples) and report_p95(samples) < target)
        self.metric(name, samples, target)


def report_p95(values):
    """The p95 the report has always printed; the gate uses nearest_rank."""
    return sorted(values)[int((len(values)-1)*.95)]


def shell_quote(s):
    """Quotes as the Codex hook declaration quotes the binary's path."""
    return "'" + s.replace("'", "'\"'\"'") + "'"


# How each host starts a hook, as internal/install/templates.go declares it:
# Claude Code runs the binary itself, with its arguments; Codex runs a
# command line through a shell, which /bin/sh stands in for here. Every hook
# figure is timed from before the first process starts to after the last
# exits, so it holds each process's start. The Codex line's fallback, which
# answers {} when the hook fails, also exits 1 here, so a hook that fails
# still fails the benchmark; it runs only then, so it costs a hook that
# works nothing.
def launch(binary, host):
    if host == "codex":
        return ["/bin/sh", "-c", shell_quote(str(binary))+" hook --host codex || { echo '{}'; exit 1; }"]
    return [str(binary), "hook", "--host", host]


LAUNCH_PATHS = {"claude-code": "exec <binary> hook --host claude-code",
                "codex": "/bin/sh -c \"'<binary>' hook --host codex || echo '{}'\""}
# Processes a launch starts before eva-md does anything: the shell, then the
# binary, for Codex; the binary alone for Claude Code.
LAUNCH_PROCESSES = {"claude-code": 1, "codex": 2}


class Home:
    """An isolated home under /tmp for one scenario: no real setup is read or
    touched. The installed binary the hooks spawn for an update or maintain
    is a stand-in that only counts its runs, so nothing here starts a real
    update, a model call or a commit. A home under /tmp keeps the binary's
    path, which every pointer names, near the length of an installed one's."""

    def __init__(self, binary, cwd, prefix="eva-md-bench-"):
        self.binary, self.cwd, self.prefix = binary, cwd, prefix

    def __enter__(self):
        self._dir = tempfile.TemporaryDirectory(prefix=self.prefix, dir="/tmp")
        self.path = Path(self._dir.name)
        env = os.environ.copy()
        env.update(HOME=str(self.path), GIT_CONFIG_GLOBAL=str(self.path/"gitconfig"), GIT_CONFIG_NOSYSTEM="1",
                   EVA_MD_INNER="", CLAUDE_CODE_REMOTE="", CLAUDE_CODE_ENTRYPOINT="", TMPDIR="/tmp")
        env.pop("GIT_DIR", None); env.pop("GIT_WORK_TREE", None)
        self.env = env
        # The stand-in's first run is outside the timings, since macOS
        # checks a new executable then.
        standin = self.path/".local/share/eva-md/bin/eva-md"
        standin.parent.mkdir(parents=True, mode=0o700)
        standin.write_text('#!/bin/sh\necho "$*" >> "$HOME/spawned"\n'); standin.chmod(0o700)
        subprocess.run([str(standin), "warm-up"], env=env, check=True, timeout=10)
        self.spawned = self.path/"spawned"
        self.state = self.path/".local/state/eva-md"
        return self

    def __exit__(self, *exc):
        self._dir.cleanup()

    def hook(self, host, event, sid, env=None, **kwargs):
        """Runs one hook on the host's launch path; returns its output and
        its milliseconds, the start of every process included."""
        payload = dict(hook_event_name=event, session_id=sid, cwd=str(self.cwd), **kwargs)
        start = time.perf_counter()
        r = subprocess.run(launch(self.binary, host), input=json.dumps(payload),
                           text=True, capture_output=True, env=env or self.env, timeout=4)
        elapsed = (time.perf_counter()-start)*1000
        if r.returncode or r.stderr:
            raise RuntimeError("hook failed or wrote diagnostics")
        if event == "Stop" and r.stdout != ("{}" if host == "codex" else ""):
            raise RuntimeError("invalid Stop output")
        return r.stdout, elapsed

    def runs(self):
        """How many times hooks started the stand-in."""
        return len(self.spawned.read_text().splitlines())-1

    def wiki(self, instance="bench-0a1b2c3d"):
        """Sets a wiki up as init leaves it, its instance in its schema and
        in the config, so updates are on; returns its folder."""
        wiki = self.path/"wiki"
        schema = wiki/".eva-md/schema.toml"
        schema.parent.mkdir(parents=True, mode=0o700)
        schema.write_text('schema_version = 1\ninstance = "%s"\n' % instance); schema.chmod(0o600)
        config = self.path/".config/eva-md/config.toml"
        config.parent.mkdir(parents=True, mode=0o700)
        config.write_text('wiki_path = %s\ninstance = "%s"\n' % (json.dumps(str(wiki)), instance)); config.chmod(0o600)
        return wiki

    def take_sweep(self):
        """The day's first chat start with updates on asks the stand-in for
        the day's sweep. Records it as taken, as the job would, so later
        chat starts ask for none."""
        deadline = time.time()+10
        sweeps = []
        while not sweeps and time.time() < deadline:
            sweeps = [a for line in self.spawned.read_text().splitlines() for a in line.split() if a.startswith("--sweep=")]
            time.sleep(0.05)
        if len(sweeps) != 1:
            raise RuntimeError("the day's first chat start did not ask for the day's sweep once")
        sweep = self.state/"updates"/"sweep"
        sweep.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
        sweep.write_text(sweeps[0][len("--sweep="):])


def context(output):
    """The text a hook's output adds to the chat."""
    value = json.loads(output)
    return value.get("hookSpecificOutput", {}).get("additionalContext", value.get("additionalContext", ""))


# The gate. A metric fails when the lower bound of the bootstrap interval of
# the ratio of the candidate's p95 to the base's is above THRESHOLD, or when
# the candidate passes the metric's ceiling, which the base did not.
THRESHOLD = 1.15
DRAWS = 2000
SEED = 0x65766120  # fixed; changing it changes every interval
LEVEL = 0.95  # the interval's coverage; its lower bound is the 2.5% quantile


def nearest_rank(values, q=0.95):
    """The smallest value at least q of the values are at or below."""
    ordered_values = sorted(values)
    return ordered_values[max(math.ceil(q*len(ordered_values)), 1)-1]


def ratio_low(base, cand, seed=SEED, draws=DRAWS):
    """The lower bound of the bootstrap interval of p95(cand)/p95(base):
    each draw resamples both with replacement, from a generator seeded with
    seed, so the same samples, in any order, give the same bound."""
    base, cand = sorted(base), sorted(cand)
    rng = random.Random(seed)
    ratios = []
    for _ in range(draws):
        b = nearest_rank(rng.choices(base, k=len(base)))
        c = nearest_rank(rng.choices(cand, k=len(cand)))
        ratios.append(c/b if b > 0 else (math.inf if c > 0 else 1.0))
    ratios.sort()
    return ratios[int(math.floor((1-LEVEL)/2*draws))]


def judge(base, cand, ceiling, threshold=THRESHOLD, seed=SEED, draws=DRAWS):
    """Compares one metric's candidate samples with its base samples. Returns
    the figures and the reasons it fails, none when it passes."""
    low = ratio_low(base, cand, seed, draws)
    p95_base, p95_cand = nearest_rank(base), nearest_rank(cand)
    reasons = []
    if low > threshold:
        reasons.append("p95 ratio's lower bound %.3f is above %.2f" % (low, threshold))
    if ceiling is not None and p95_cand > ceiling >= p95_base:
        reasons.append("p95 %.1f ms passed the ceiling of %g ms" % (p95_cand, ceiling))
    return {"p95_base_ms": round(p95_base, 3), "p95_ms": round(p95_cand, 3),
            "ratio": round(p95_cand/p95_base, 3) if p95_base > 0 else None,
            "ratio_low": round(low, 3) if math.isfinite(low) else None, "reasons": reasons}


def decide(old, new, control, ceilings, threshold=THRESHOLD, seed=SEED, draws=DRAWS):
    """The gate's verdict on samples. old, new and control map each metric
    to its samples from the old binary's runs, the new one's, and the new
    one's again, run as a control; ceilings maps each gated metric to its
    ceiling in milliseconds, or None.

    The control is judged first, by the same rule, both ways: new against
    the control and the control against new. If either fails, two runs of
    one binary differed as much as a regression would, so the machine was
    too noisy to judge: the verdict is "not judged" and the exit 0, whatever
    the old binary did. Otherwise a metric of new that fails against old
    fails the gate, exit 1. A metric without samples on every side is listed
    as not compared and decides nothing."""
    metrics, control_reasons, failed, skipped = {}, [], [], []
    for name in sorted(ceilings):
        o, n, c = old.get(name) or [], new.get(name) or [], control.get(name) or []
        if not (o and n and c):
            skipped.append(name)
            continue
        ceiling = ceilings[name]
        verdict = judge(o, n, ceiling, threshold, seed, draws)
        both = [judge(n, c, ceiling, threshold, seed, draws), judge(c, n, ceiling, threshold, seed, draws)]
        verdict.update(n_old=len(o), n_new=len(n), n_control=len(c), ceiling_ms=ceiling,
                       control_ratio_low=max((j["ratio_low"] for j in both if j["ratio_low"] is not None), default=None),
                       control_reasons=[r for j in both for r in j["reasons"]])
        metrics[name] = verdict
        control_reasons += ["%s: %s" % (name, r) for r in verdict["control_reasons"]]
        failed += ["%s: %s" % (name, r) for r in verdict["reasons"]]
    if control_reasons:
        verdict, code = "not judged", 0
    elif failed:
        verdict, code = "fail", 1
    else:
        verdict, code = "pass", 0
    return {"verdict": verdict, "exit": code, "threshold": threshold, "seed": seed, "draws": draws,
            "control_failures": control_reasons, "failures": failed, "not_compared": skipped, "metrics": metrics}
