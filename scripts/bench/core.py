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


# The gate. Each binary runs twice a round, as two roles: old and old_again,
# new and new_again. The gate compares the old binary's runs, both roles
# pooled, with the new one's, by the lower bound of the bootstrap interval
# of the ratio of the new p95 to the old one.
#
# The controls are the two other ways to split the four roles into two
# pairs: old and new against old_again and new_again, and old and new_again
# against old_again and new. Each side of a control holds one role of each
# binary, so a change cancels out of it and only what the machine did
# between runs is left. With the binaries alike, the three splits are alike
# too: load that falls on the new binary's runs and not on the old one's
# falls unevenly on new and new_again, or on old and old_again, and a
# control sees it. A metric's noise is the largest lower bound of its
# controls' ratios, both ways, and at least 1: how far apart the machine
# alone put two sets of runs. A metric fails when its lower bound is above
# THRESHOLD times its noise, or when the new p95 passes the metric's
# ceiling, which the old one did not, and its noise is at most THRESHOLD.
# The controls have no ceiling: a fixed ceiling says where a binary sits,
# not how noisy the machine is.
ROLES = ("old", "new", "old_again", "new_again")
BINARY = {"old": "old", "old_again": "old", "new": "new", "new_again": "new"}
GATE_SPLIT = (("old", "old_again"), ("new", "new_again"))
CONTROL_SPLITS = ((("old", "new"), ("old_again", "new_again")),
                  (("old", "new_again"), ("old_again", "new")))
THRESHOLD = 1.15
DRAWS = 2000
SEED = 0x65766120  # fixed; changing it changes every interval
LEVEL = 0.95  # the interval's coverage; its lower bound is the 2.5% quantile
WHOLE = 100  # the samples a side from which a metric's control speaks for the machine
# A scenario fails the gate when the new binary failed it in at least this
# many more runs than the old one did.
BROKEN_MARGIN = 2


def schedule(rounds):
    """The order of the gate's runs, as (round, role): each round runs the
    four roles once, old and new alternating, and every other round runs
    them in reverse, so that over two rounds each role's mean place is the
    same and none always runs first."""
    order = []
    for r in range(rounds):
        order += [(r+1, role) for role in (ROLES if r % 2 == 0 else ROLES[::-1])]
    return order


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
    the figures and the reasons it fails, none when it passes. A ceiling of
    None checks the ratio alone."""
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


def pooled(samples, roles, name):
    """One metric's samples from the runs of the given roles, together."""
    return [v for role in roles for v in (samples.get(role, {}).get(name) or [])]


def scenarios(failures, rounds):
    """What the scenarios that failed say. failures maps each role to the
    scenarios that failed in its runs, each with the errors, one a run; each
    role ran rounds times, so each binary 2*rounds times, and a binary's
    failed runs are counted over both its roles.

    broken: the scenarios the new binary failed in at least BROKEN_MARGIN
    more runs than the old one, as a hook that hangs or fails would: the
    worst regression there is, and one that leaves no samples. A scenario
    failing now and then, on either binary, is load, as when a git call the
    brief makes runs past its time limit, and decides nothing; its failed
    runs' samples are left out. A scenario the old binary fails and the new
    one passes, as one added with the change, decides nothing either.

    unrunnable: the scenarios every run failed, on both binaries: the
    benchmark, or the machine it runs on, is broken, and the gate could not
    run."""
    counts = {}
    for role, by in failures.items():
        for name, errors in by.items():
            counts.setdefault(name, {"old": 0, "new": 0})[BINARY[role]] += len(errors)
    runs = 2*rounds
    broken = sorted(n for n, c in counts.items() if c["new"] >= c["old"]+BROKEN_MARGIN and c["old"] < runs)
    unrunnable = sorted(n for n, c in counts.items() if c["old"] >= runs and c["new"] >= runs)
    return {"runs_per_binary": runs, "failed_runs": {n: dict(c) for n, c in sorted(counts.items())},
            "broken": broken, "unrunnable": unrunnable}


def decide(samples, ceilings, failures=None, rounds=1, threshold=THRESHOLD, seed=SEED, draws=DRAWS, whole=WHOLE):
    """The gate's verdict. samples maps each role (ROLES) to each metric's
    samples from that role's runs; ceilings maps each gated metric to its
    ceiling in milliseconds, or None; failures holds the scenarios that
    failed, as scenarios() takes them, over rounds rounds.

    A metric the old binary gave samples and the new one gave none fails;
    one the old binary gave none, as one added with the change, is listed as
    not compared and decides nothing.

    Each other metric is compared, old against new. Its ratio's lower bound
    above THRESHOLD fails it when it is also above THRESHOLD times the
    metric's noise; otherwise the rise is no more than the machine put
    between two sets of runs a change cancels out of, and the metric is
    unsettled. Passing the ceiling fails it when the noise is at most
    THRESHOLD, and otherwise unsettles it; so does a control with a side
    that has no samples. An unsettled metric with at least whole samples on
    every side means the machine was too noisy to judge any metric: load in
    some runs and not others moves every figure, and a regression's seeming
    is then the machine's. On a metric with fewer, whose p95 is one of its
    few largest samples, it says more about the samples than about the
    machine: that metric alone is not judged.

    The verdict is the first of these that applies: "could not run", exit
    2, when a scenario failed in every run of both binaries; "fail", exit 1,
    when the new binary broke a scenario or gave a metric no samples; "not
    judged", exit 0, when the machine was too noisy; "fail", exit 1, when a
    metric failed; "not judged", exit 0, when a metric was unsettled;
    otherwise "pass", exit 0."""
    found = scenarios(failures or {}, rounds)
    metrics, unsettled_all, failed, missing, skipped = {}, [], [], [], []
    noisy = False
    for name in sorted(ceilings):
        o, n = pooled(samples, GATE_SPLIT[0], name), pooled(samples, GATE_SPLIT[1], name)
        if not o:
            skipped.append(name)
            continue
        if not n:
            missing.append("%s: the new binary gave no samples, the old one %d" % (name, len(o)))
            continue
        ceiling = ceilings[name]
        lows, sizes, empty = [], [len(o), len(n)], []
        for a_roles, b_roles in CONTROL_SPLITS:
            a, b = pooled(samples, a_roles, name), pooled(samples, b_roles, name)
            if not (a and b):
                empty.append("%s against %s" % ("+".join(a_roles), "+".join(b_roles)))
                continue
            sizes += [len(a), len(b)]
            lows += [ratio_low(a, b, seed, draws), ratio_low(b, a, seed, draws)]
        # The noise: the most two sets of runs that no change separates
        # differed by, and never less than none.
        noise = math.inf if empty else max([1.0] + lows)
        low = ratio_low(o, n, seed, draws)
        reasons, unsettled = [], []
        if low > threshold:
            if low > threshold*noise:
                reasons.append("p95 ratio's lower bound %.3f is above %.2f times the controls' %.3f" % (low, threshold, noise))
            elif empty:
                unsettled.append("p95 ratio's lower bound %.3f is above %.2f, and a control has a side with no samples" % (low, threshold))
            else:
                unsettled.append("p95 ratio's lower bound %.3f is above %.2f, but not above %.2f times the controls' %.3f" % (
                    low, threshold, threshold, noise))
        p95_o, p95_n = nearest_rank(o), nearest_rank(n)
        if ceiling is not None and p95_n > ceiling >= p95_o:
            if noise <= threshold or reasons:
                reasons.append("p95 %.1f ms passed the ceiling of %g ms" % (p95_n, ceiling))
            else:
                unsettled.append("p95 %.1f ms passed the ceiling of %g ms, but the controls' %.3f is above %.2f" % (
                    p95_n, ceiling, noise, threshold))
        verdict = {"p95_base_ms": round(p95_o, 3), "p95_ms": round(p95_n, 3),
                   "ratio": round(p95_n/p95_o, 3) if p95_o > 0 else None,
                   "ratio_low": round(low, 3) if math.isfinite(low) else None}
        verdict.update(n_old=len(o), n_new=len(n), ceiling_ms=ceiling,
                       n_roles={role: len(samples.get(role, {}).get(name) or []) for role in ROLES},
                       noise=round(noise, 3) if math.isfinite(noise) else None, controls_without_samples=empty,
                       reasons=reasons, unsettled=unsettled, judged=not unsettled)
        metrics[name] = verdict
        unsettled_all += ["%s: %s" % (name, r) for r in unsettled]
        noisy = noisy or (bool(unsettled) and min(sizes) >= whole)
        failed += ["%s: %s" % (name, r) for r in reasons]
    hard = ["the new binary failed the scenario %s in %d of %d runs, the old one in %d" % (
        s, found["failed_runs"][s]["new"], found["runs_per_binary"], found["failed_runs"][s]["old"])
        for s in found["broken"]] + missing
    if found["unrunnable"]:
        verdict, code = "could not run", 2
    elif hard:
        verdict, code = "fail", 1
    elif noisy:
        verdict, code = "not judged", 0
    elif failed:
        verdict, code = "fail", 1
    elif unsettled_all:
        verdict, code = "not judged", 0
    else:
        verdict, code = "pass", 0
    return {"verdict": verdict, "exit": code, "threshold": threshold, "seed": seed, "draws": draws, "whole": whole,
            "machine_too_noisy": noisy, "unsettled": unsettled_all, "failures": hard+failed,
            "scenarios": found, "not_compared": skipped, "metrics": metrics}
