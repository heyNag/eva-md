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
a test, and one whose name starts with sim_ a simulation run by hand
(sim_gate.py measures the gate's rule): neither is loaded as a scenario.

The decision (decide) takes samples and gives a verdict; it reads no clock
and starts nothing, so its tests feed it numbers.
"""
import functools
import itertools
import json
import math
import operator
import os
from pathlib import Path
import random
import signal
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
    fill and the samples of the metrics the gate compares. gate is true
    when the run is one of the gate's."""

    def __init__(self, binary, cwd, gate=False):
        self.binary, self.cwd, self.gate = binary, cwd, gate
        self.report, self.metrics, self.targets = {}, {}, {}

    def loops(self, plain):
        """How many times a scenario times a hook whose figure would have
        too few samples for the gate at plain: plain in the benchmark, and
        in the gate's runs at least GATE_RUN_SAMPLES, so that at the default
        rounds each binary gives the figure WHOLE samples."""
        return max(plain, GATE_RUN_SAMPLES) if self.gate else plain

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
# The seconds a hook may run before the benchmark stops it.
HOOK_LIMIT = 4


def start(argv, env):
    """Starts a hook's launch in a process group of its own, so that stop
    reaches every process it started: on Codex's path the shell does not
    replace itself with the binary, and the binary is its child."""
    return subprocess.Popen(argv, stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                            text=True, env=env, start_new_session=True)


def stop(p):
    """Kills the process group start gave p, and waits for p."""
    try:
        os.killpg(p.pid, signal.SIGKILL)
    except ProcessLookupError:
        pass
    p.wait()


def run_hook(argv, payload, env, limit=HOOK_LIMIT):
    """Runs one hook's launch with payload on its input; returns its exit
    code, output and diagnostics. One still running after limit seconds is
    killed with every process it started, and TimeoutExpired is raised."""
    with start(argv, env) as p:
        try:
            out, err = p.communicate(payload, timeout=limit)
        except subprocess.TimeoutExpired:
            stop(p)
            raise
    return p.returncode, out, err


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
        began = time.perf_counter()
        code, out, err = run_hook(launch(self.binary, host), json.dumps(payload), env or self.env)
        elapsed = (time.perf_counter()-began)*1000
        if code or err:
            raise RuntimeError("hook failed or wrote diagnostics")
        if event == "Stop" and out != ("{}" if host == "codex" else ""):
            raise RuntimeError("invalid Stop output")
        return out, elapsed

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


# The gate. Each round runs each binary twice, as two roles: old and
# old_again, new and new_again, in the order schedule gives, so at the
# default two rounds each binary has four runs. decide takes each role's
# samples by run, and compares the old binary's runs, pooled, with the new
# one's, by the lower bound of the bootstrap interval (LEVEL, DRAWS draws, a
# fixed seed) of the ratio of the new p95 to the old one.
#
# The controls are splits of the runs into two sides that each hold half of
# the old binary's runs and half of the new one's: 18 at two rounds, the two
# role splits among them (old and new against old_again and new_again, old
# and new_again against old_again and new); with more rounds, a seeded
# sample of at most SPLITS that always holds the two role splits. Each side
# holds as many runs of each binary, so a change cancels out of a control and
# only what the machine did between runs is left: load on some runs and not
# others puts as much between the sides of some split as between the two
# binaries. A figure's noise is the NOISE_QUANTILE, by nearest rank, of its
# controls' lower bounds, both ways, and at least 1.
#
# A figure is judged only with at least WHOLE samples on each binary's
# side; one with fewer is listed as having too few samples, and neither
# fails nor stops the verdict. A judged figure whose noise is above
# THRESHOLD says the machine was too noisy, and stops the whole verdict:
# only the controls call the machine noisy. Otherwise it fails when its
# lower bound is above THRESHOLD times its noise, or when the lower bound of
# the interval of the new binary's p95 is above its ceiling and the old
# binary's p95 is not. One whose lower bound is above THRESHOLD but not above
# THRESHOLD times its noise is unsettled, and stops nothing: another figure
# that failed still fails the gate. The controls have no ceiling: a fixed
# ceiling says where a binary sits, not how noisy the machine is.
ROLES = ("old", "new", "old_again", "new_again")
BINARY = {"old": "old", "old_again": "old", "new": "new", "new_again": "new"}
ROLES_OF = {"old": ("old", "old_again"), "new": ("new", "new_again")}
ROUNDS = 2  # the gate's rounds unless --rounds says otherwise
THRESHOLD = 1.15
DRAWS = 2000
SEED = 0x65766120  # fixed; changing it changes every interval
LEVEL = 0.95  # the interval's coverage; its lower bound is the 2.5% quantile
WHOLE = 100  # the samples on each binary's side a figure needs to be judged
# The samples a figure gets in each of the gate's runs at least, so that at
# ROUNDS rounds each binary gives it WHOLE (Run.loops).
GATE_RUN_SAMPLES = -(-WHOLE // (2*ROUNDS))
SPLITS = 36  # the most controls a figure has
NOISE_QUANTILE = 0.89  # of the controls' lower bounds: the fourth largest of 36
# A scenario the new binary failed in at least BROKEN of its runs more than
# the old one fails the gate; in at least LOST of them, the verdict is "not
# judged" and the gate is to be run again.
BROKEN = 0.75
LOST = 0.5
P95 = 0.95


def schedule(rounds):
    """The order of the gate's runs, as (round, role): each round runs the
    four roles once, old and new alternating, and every other round runs
    them in reverse, so that over two rounds each role's mean place is the
    same and none always runs first."""
    order = []
    for r in range(rounds):
        order += [(r+1, role) for role in (ROLES if r % 2 == 0 else ROLES[::-1])]
    return order


def nearest_rank(values, q=P95):
    """The smallest value at least q of the values are at or below."""
    ordered_values = sorted(values)
    return ordered_values[max(math.ceil(q*len(ordered_values)), 1)-1]


@functools.lru_cache(maxsize=1024)
def ranks(sizes, seed, draws):
    """For each size n in sizes, in turn, from one generator seeded with
    seed: the ranks, in n sorted values, of the p95 of draws resamples of
    them with replacement. The p95 by nearest rank of a resample is its R-th
    smallest value, R = max(ceil(0.95 n), 1), and the R-th smallest of n
    uniform draws is Beta(R, n-R+1), so a rank is drawn directly and no
    resample is built. They depend on the sizes alone."""
    rng = random.Random(seed)
    out = []
    for n in sizes:
        r = max(math.ceil(P95*n), 1)
        out.append(tuple(min(int(rng.betavariate(r, n-r+1)*n), n-1) for _ in range(draws)))
    return tuple(out)


def ratios(base, cand, seed=SEED, draws=DRAWS):
    """The bootstrap draws of p95(cand)/p95(base), sorted: each draw takes
    the p95 of a resample of each, so the same samples, in any order, give
    the same draws."""
    rb, rc = ranks((len(base), len(cand)), seed, draws)
    base, cand = sorted(base), sorted(cand)
    b, c = [base[i] for i in rb], [cand[i] for i in rc]
    if base[0] > 0:
        return sorted(map(operator.truediv, c, b))
    return sorted(y/x if x > 0 else (math.inf if y > 0 else 1.0) for x, y in zip(b, c))


def interval(base, cand, seed=SEED, draws=DRAWS):
    """The bootstrap interval of p95(cand)/p95(base), at LEVEL."""
    r = ratios(base, cand, seed, draws)
    k = int(math.floor((1-LEVEL)/2*draws))
    return r[k], r[draws-1-k]


def ratio_low(base, cand, seed=SEED, draws=DRAWS):
    """The lower bound of the bootstrap interval of p95(cand)/p95(base)."""
    return interval(base, cand, seed, draws)[0]


def p95_low(values, seed=SEED, draws=DRAWS):
    """The lower bound of the bootstrap interval of the values' p95."""
    (rv,) = ranks((len(values),), seed, draws)
    values = sorted(values)
    return sorted(values[i] for i in rv)[int(math.floor((1-LEVEL)/2*draws))]


def splits(rounds, seed=SEED, most=SPLITS):
    """The controls at rounds rounds. A binary's runs are numbered as
    decide orders them, its first role's then its second's, 0 to
    2*rounds-1; a split is the runs of the old binary and of the new one on
    its first side, which holds the old binary's run 0, and the rest are on
    the other. The two role splits come first; with more than most splits,
    the rest are a sample, seeded with seed."""
    first, again = tuple(range(rounds)), tuple(range(rounds, 2*rounds))
    role = [(first, first), (first, again)]
    rest = [(o, n) for o in itertools.combinations(range(2*rounds), rounds) if o[0] == 0
            for n in itertools.combinations(range(2*rounds), rounds) if (o, n) not in role]
    if len(role)+len(rest) > most:
        rest = random.Random(seed).sample(rest, most-len(role))
    return role + rest


def scenarios(failures, rounds):
    """What the scenarios that failed say. failures maps each role to the
    scenarios that failed in its runs, each with the errors, one a run; each
    role ran rounds times, so each binary 2*rounds times, and a binary's
    failed runs are counted over both its roles.

    broken: the scenarios the new binary failed in at least BROKEN of its
    runs more than the old one, 3 of 4 at two rounds, as a hook that hangs or
    fails would: the worst regression there is, and one that leaves no
    samples. lost: those it failed in at least LOST of its runs more, 2 of
    4, and fewer than BROKEN: load can lose a scenario that often, as when
    a git call the brief makes runs past its time limit, so the gate is not
    judged and is to be run again. Fewer decides nothing, and a failed
    run's samples are left out. A scenario the old binary fails and the new
    one passes, as one added with the change, decides nothing either.

    unrunnable: the scenarios every run failed, on both binaries: the
    benchmark, or the machine it runs on, is broken, and the gate could not
    run."""
    counts = {}
    for role, by in failures.items():
        for name, errors in by.items():
            counts.setdefault(name, {"old": 0, "new": 0})[BINARY[role]] += len(errors)
    runs = 2*rounds
    more = {n: c["new"]-c["old"] for n, c in counts.items()}
    broken = sorted(n for n, d in more.items() if d >= BROKEN*runs)
    lost = sorted(n for n, d in more.items() if LOST*runs <= d < BROKEN*runs)
    unrunnable = sorted(n for n, c in counts.items() if c["old"] >= runs and c["new"] >= runs)
    return {"runs_per_binary": runs, "failed_runs": {n: dict(c) for n, c in sorted(counts.items())},
            "broken": broken, "lost": lost, "unrunnable": unrunnable}


def by_run(samples, rounds):
    """Each binary's runs, as decide numbers them: a list of rounds runs a
    role, each a map of figures to samples; a role not given ran and gave
    nothing."""
    out = {}
    for binary, roles in ROLES_OF.items():
        out[binary] = []
        for role in roles:
            runs = samples.get(role, [{}]*rounds)
            if len(runs) != rounds:
                raise ValueError("the role %s has %d runs, not %d" % (role, len(runs), rounds))
            out[binary] += runs
    return out


def decide(samples, ceilings, failures=None, rounds=ROUNDS, threshold=THRESHOLD, seed=SEED, draws=DRAWS,
           whole=WHOLE, quantile=NOISE_QUANTILE):
    """The gate's verdict. samples maps each role (ROLES) to its runs, in
    order, each a map of each figure to its samples from that run; ceilings
    maps each gated figure to its ceiling in milliseconds, or None;
    failures holds the scenarios that failed, as scenarios() takes them. Each
    role ran rounds times. The rule is the one above ROLES.

    A figure the old binary gave no samples, as one added with the change,
    is listed as not compared. One the new binary gave none fails, unless
    the new binary failed a scenario in every one of its runs, which the
    scenarios' rule then judges: it has too few samples.

    The verdict is the first of these that applies: "could not run", exit
    2, when a scenario failed in every run of both binaries; "fail", exit 1,
    when the new binary broke a scenario or gave a figure no samples;
    "not judged", exit 0, when a judged figure's controls called the machine
    too noisy; "fail", exit 1, when a figure failed; "not judged", exit 0,
    when a scenario was lost or a figure unsettled; otherwise "pass", exit
    0."""
    found = scenarios(failures or {}, rounds)
    runs = by_run(samples, rounds)
    controls = splits(rounds, seed)
    all_lost = any(c["new"] >= found["runs_per_binary"] for c in found["failed_runs"].values())
    metrics, unsettled_all, failed, missing, skipped, too_few = {}, [], [], [], [], []
    noisy = False
    for name in sorted(ceilings):
        o_runs = [list(run.get(name) or []) for run in runs["old"]]
        n_runs = [list(run.get(name) or []) for run in runs["new"]]
        o, n = [v for r in o_runs for v in r], [v for r in n_runs for v in r]
        if not o:
            skipped.append(name)
            continue
        if not n:
            if all_lost:
                too_few.append(name)
            else:
                missing.append("%s: the new binary gave no samples, the old one %d" % (name, len(o)))
            continue
        lows = []
        for a_old, a_new in controls:
            a = [v for i in a_old for v in o_runs[i]] + [v for i in a_new for v in n_runs[i]]
            b = [v for i in range(2*rounds) if i not in a_old for v in o_runs[i]] + \
                [v for i in range(2*rounds) if i not in a_new for v in n_runs[i]]
            if a and b:
                low_ab, high_ab = interval(a, b, seed, draws)
                lows += [low_ab, 1/high_ab if high_ab > 0 else math.inf]
        noise = max(1.0, nearest_rank(lows, quantile)) if lows else math.inf
        low = ratio_low(o, n, seed, draws)
        p95_o, p95_n, p95_n_low = nearest_rank(o), nearest_rank(n), p95_low(n, seed, draws)
        ceiling = ceilings[name]
        reasons, unsettled = [], []
        enough = min(len(o), len(n)) >= whole
        if not enough:
            too_few.append(name)
        elif noise > threshold:
            noisy = True
            unsettled.append("the controls' %.3f is above %.2f: the machine was too noisy to judge" % (noise, threshold))
        else:
            if low > threshold*noise:
                reasons.append("p95 ratio's lower bound %.3f is above %.2f times the controls' %.3f" % (low, threshold, noise))
            elif low > threshold:
                unsettled.append("p95 ratio's lower bound %.3f is above %.2f, but not above %.2f times the controls' %.3f" % (
                    low, threshold, threshold, noise))
            if ceiling is not None and p95_n_low > ceiling >= p95_o:
                reasons.append("p95 %.1f ms, at least %.1f ms, passed the ceiling of %g ms" % (p95_n, p95_n_low, ceiling))
        metrics[name] = {
            "p95_base_ms": round(p95_o, 3), "p95_ms": round(p95_n, 3), "p95_low_ms": round(p95_n_low, 3),
            "ratio": round(p95_n/p95_o, 3) if p95_o > 0 else None,
            "ratio_low": round(low, 3) if math.isfinite(low) else None,
            "n_old": len(o), "n_new": len(n), "ceiling_ms": ceiling,
            "n_runs": {role: [len(run.get(name) or []) for run in samples.get(role, [])] for role in ROLES},
            "controls": len(lows), "noise": round(noise, 3) if math.isfinite(noise) else None,
            "too_few_samples": not enough, "reasons": reasons, "unsettled": unsettled,
            "judged": enough and not unsettled}
        unsettled_all += ["%s: %s" % (name, r) for r in unsettled]
        failed += ["%s: %s" % (name, r) for r in reasons]
    hard = ["the new binary failed the scenario %s in %d of %d runs, the old one in %d" % (
        s, found["failed_runs"][s]["new"], found["runs_per_binary"], found["failed_runs"][s]["old"])
        for s in found["broken"]] + missing
    lost = ["the new binary failed the scenario %s in %d of %d runs, the old one in %d: load may have lost it, run the gate again" % (
        s, found["failed_runs"][s]["new"], found["runs_per_binary"], found["failed_runs"][s]["old"])
        for s in found["lost"]]
    if found["unrunnable"]:
        verdict, code = "could not run", 2
    elif hard:
        verdict, code = "fail", 1
    elif noisy:
        verdict, code = "not judged", 0
    elif failed:
        verdict, code = "fail", 1
    elif lost or unsettled_all:
        verdict, code = "not judged", 0
    else:
        verdict, code = "pass", 0
    return {"verdict": verdict, "exit": code, "rounds": rounds, "threshold": threshold, "seed": seed, "draws": draws,
            "whole": whole, "noise_quantile": quantile, "controls": len(controls),
            "machine_too_noisy": noisy, "unsettled": unsettled_all, "lost": lost, "failures": hard+failed,
            "too_few_samples": too_few, "scenarios": found, "not_compared": skipped, "metrics": metrics}


def redecide(gate):
    """The verdict worked out again from a gate.json: its samples by run,
    its ceilings, its scenarios' failures and the rule's parameters it
    recorded."""
    d = gate["decision"]
    return decide(gate["samples"], gate["ceilings"], gate["scenario_failures"], gate["rounds"], d["threshold"],
                  d["seed"], d["draws"], d["whole"], d["noise_quantile"])
