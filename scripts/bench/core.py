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
import collections
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
    when the run is one of the gate's; scenario is the name of the scenario
    running, and sources maps each metric to the scenario that gave it."""

    def __init__(self, binary, cwd, gate=False):
        self.binary, self.cwd, self.gate = binary, cwd, gate
        self.report, self.metrics, self.targets, self.sources = {}, {}, {}, {}
        self.scenario = None

    def loops(self, plain):
        """How many times a scenario times a hook whose figure would have
        too few samples for the gate at plain: plain in the benchmark, and
        in the gate's runs at least GATE_RUN_SAMPLES, so that at the default
        rounds each binary gives the figure WHOLE samples even when one of
        its runs failed the scenario."""
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
        if self.scenario is not None:
            self.sources[name] = self.scenario

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

# The hook's own time limit for each event, in milliseconds, and the margin
# it takes off it for its work. They mirror budget in internal/hook/hook.go
# (300 ms, and 1500 ms for SessionStart, PostToolUse and PostToolUseFailure)
# and the 10*time.Millisecond that Run there takes off the deadline for its
# work's context. A capture whose work passes that delivers nothing, by
# design: no output but Codex Stop's {}, and no background job.
EVENT_LIMIT_MS = {"SessionStart": 1500, "PostToolUse": 1500, "PostToolUseFailure": 1500}
DEFAULT_LIMIT_MS = 300
LIMIT_MARGIN_MS = 10


def past_limit(event, ms):
    """Whether a hook of event, timed at ms milliseconds, process start
    included, reached its limit less its margin. Only such a hook can have
    missed its limit, since the time a hook measures for itself is never
    longer than the benchmark's."""
    return ms >= EVENT_LIMIT_MS.get(event, DEFAULT_LIMIT_MS) - LIMIT_MARGIN_MS


def starts_allowed(expected, seen, times):
    """Whether seen starts of the stand-in are what expected hooks allow,
    and how many of those hooks passed their limit: times holds the event
    and the measured milliseconds of each hook that should have started
    one. A hook past its limit (past_limit) may have delivered nothing, its
    start included, so seen may be short by as many as passed it; with none
    past it the count is exact. More than expected is never allowed."""
    late = sum(1 for event, ms in times if past_limit(event, ms))
    return expected-late <= seen <= expected, late


def start(argv, env):
    """Starts a hook's launch in a process group of its own, so that stop
    reaches every process in that group: on Codex's path the shell does not
    replace itself with the binary, and the binary is its child. A process
    the hook puts in a session or group of its own on purpose, as eva-md's
    detached spawns and its git calls are, is not in it."""
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
    killed with every process in its process group (start), and
    TimeoutExpired is raised."""
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
        # A Codex Stop prints {}, but one past its limit prints nothing, by
        # design: Run in internal/hook/hook.go writes its output only by the
        # deadline. So an empty output is let through from a Codex Stop timed
        # past its limit (past_limit) alone, for its loop to pass over and
        # count (to_read in benchmark.py); from one in time it is a fault.
        if event == "Stop" and out != ("{}" if host == "codex" else ""):
            if not (host == "codex" and out == "" and past_limit(event, elapsed)):
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


# The gate, by its third rule (RULE). Each round runs each binary twice, as
# two roles: old and old_again, new and new_again, in the order schedule
# gives, so at the default three rounds each binary has six runs. decide
# takes each role's samples by run, and compares the old binary's runs,
# pooled, with the new one's twice: by the lower bound of the bootstrap
# interval (LEVEL, DRAWS draws, a fixed seed) of the ratio of the new p95 to
# the old one, and by that of the ratio of their medians.
#
# The controls are splits of the runs into two sides that each hold half of
# the old binary's runs and half of the new one's: 18 at two rounds, the two
# role splits among them (old and new against old_again and new_again, old
# and new_again against old_again and new); with more rounds, a seeded
# sample of at most SPLITS that always holds the two role splits. Each side
# holds as many runs of each binary, so only what the machine did between
# runs is left between the sides: load on some runs and not others puts as
# much between the sides of some split as between the two binaries. A
# change cancels out too, unless it is large: then each side's p95 comes
# from its runs of the slower binary alone, and the controls would measure
# its runs against each other. So when the binaries' typical runs differ by
# more than THRESHOLD (the ratio of the lower medians of their runs'
# medians, which load on half of a binary's runs or fewer leaves where it
# was), the controls first bring the new binary's samples to THRESHOLD of
# the old one's (aligned). A figure's noise is the NOISE_QUANTILE, by
# nearest rank, of its controls' lower bounds of the p95, both ways, and at
# least 1: the sixth largest of 72 at three rounds, the third of 36 at two.
# Its median's noise is the same of the controls' bounds of the median.
#
# A figure is judged only with at least WHOLE samples on each binary's
# side. One with fewer is listed as having too few samples, and neither
# fails nor passes; when it is short only because a scenario failed in some
# runs (each run, given as many samples as the figure's fullest, would have
# reached WHOLE), the gate is not judged, to be run again. A judged figure
# whose noise is above THRESHOLD has a p95 too noisy to judge; when
# NOISY_FIGURES or more have, the machine was too noisy, and the whole
# verdict stops. So it does when one of a binary's runs was more than QUICK
# times as quick as the median of its other runs, over all the judged
# figures (run_levels): load on all of a binary's runs but one, or on half
# of them, is what no control can tell from a change, since every split has
# loaded runs on each side. So it does when the slower half of a binary's
# runs was more than UNEVEN times as slow as its quicker half, on its own
# and against the other binary's runs beside them (uneven_load): load on
# half of one binary's runs, each run by its own factor, which leaves no
# one run quick, while a round both binaries ran under load cancels out.
# Only the runs themselves call the machine noisy. Otherwise each figure is judged on its p95 when that is not too
# noisy, and on its median when that is not: it fails when a lower bound is
# above its threshold (THRESHOLD for the p95, MEDIAN_THRESHOLD for the
# median) times its noise, or when the lower bound of the interval of the
# new binary's p95 is above its ceiling and the old binary's p95 is not. A
# lower bound above its threshold but not above that times its noise leaves
# the figure unsettled; a figure neither can judge is too noisy to judge.
# Either stops nothing: another figure that failed still fails the gate,
# and without one the gate is not judged. The controls have no ceiling: a
# fixed ceiling says where a binary sits, not how noisy the machine is.
ROLES = ("old", "new", "old_again", "new_again")
BINARY = {"old": "old", "old_again": "old", "new": "new", "new_again": "new"}
ROLES_OF = {"old": ("old", "old_again"), "new": ("new", "new_again")}
RULE = 3  # the rule's number, which gate.json records
ROUNDS = 3  # the gate's rounds unless --rounds says otherwise
THRESHOLD = 1.15
# The median's own threshold. Its controls' bounds are far tighter than the
# p95's, so a rise of one hook alone, which the p95 of 100 to 150 samples a
# side seldom tells from noise, shows in it; a lower one, 1.10, failed an
# unchanged binary on a quiet machine where its runs differed by chance.
MEDIAN_THRESHOLD = 1.15
MEDIAN = 0.5
DRAWS = 2000
SEED = 0x65766120  # fixed; changing it changes every interval
LEVEL = 0.95  # the interval's coverage; its lower bound is the 2.5% quantile
WHOLE = 100  # the samples on each binary's side a figure needs to be judged
# The samples a figure gets in each of the gate's runs at least, so that at
# ROUNDS rounds each binary gives it WHOLE even when one of its runs is
# lost to a failed scenario (Run.loops): 20 at three rounds, where the
# second rule's two rounds took 34.
GATE_RUN_SAMPLES = -(-WHOLE // (2*ROUNDS-1))
SPLITS = 36  # the most controls a figure has
# Of the controls' lower bounds: the sixth largest of 72. The eighth (0.89),
# the second rule's, failed unchanged binaries by their p95 on the saved
# gates under a mild load and on CI's runner.
NOISE_QUANTILE = 0.92
NOISY_FIGURES = 2  # the figures whose p95 is too noisy that call the machine too noisy
# A binary's run more than QUICK times as quick as the lower median of its
# other runs calls the machine too noisy (one_quick_run). It is below
# THRESHOLD: a mild load (1.1 to 1.3) on half the new binary's runs, or on
# all but one, leaves the median of its others on a loaded run, while the
# pooled samples can put a figure past THRESHOLD and no control sees it.
# On a quiet machine the runs of an unchanged binary differ by less; how
# often it trips there, and what 1.08 and 1.09 cost instead, are in
# docs/v2/baselines/README.md.
QUICK = 1.10
# The slower half of a binary's runs more than UNEVEN times as slow as its
# quicker half, on their own and against the other binary's runs beside
# them, calls the machine too noisy (uneven_load). Mild load on three of
# the new binary's six runs, each run by its own factor between 1.1 and
# 1.3, failed an unchanged binary about once in 70 without it; what it
# costs is in docs/v2/baselines/README.md.
UNEVEN = 1.10
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
def ranks(sizes, seed, draws, q=P95):
    """For each size n in sizes, in turn, from one generator seeded with
    seed: the ranks, in n sorted values, of the q quantile (the p95 unless
    said) of draws resamples of them with replacement. The quantile by
    nearest rank of a resample is its R-th smallest value, R = max(ceil(q
    n), 1), and the R-th smallest of n uniform draws is Beta(R, n-R+1), so a
    rank is drawn directly and no resample is built. They depend on the
    sizes alone."""
    rng = random.Random(seed)
    out = []
    for n in sizes:
        r = max(math.ceil(q*n), 1)
        out.append(tuple(min(int(rng.betavariate(r, n-r+1)*n), n-1) for _ in range(draws)))
    return tuple(out)


def ratios(base, cand, seed=SEED, draws=DRAWS, q=P95):
    """The bootstrap draws of p95(cand)/p95(base), sorted, or of the q
    quantile's ratio: each draw takes the quantile of a resample of each, so
    the same samples, in any order, give the same draws."""
    rb, rc = ranks((len(base), len(cand)), seed, draws, q)
    base, cand = sorted(base), sorted(cand)
    b, c = list(map(base.__getitem__, rb)), list(map(cand.__getitem__, rc))
    if base[0] > 0:
        return sorted(map(operator.truediv, c, b))
    return sorted(y/x if x > 0 else (math.inf if y > 0 else 1.0) for x, y in zip(b, c))


@functools.lru_cache(maxsize=1024)
def pairs(sizes, seed, draws, q=P95):
    """The pairs of ranks ranks draws for two sizes, each with how many
    draws gave it: the ranks gather about the quantile's own, so far fewer
    pairs than draws."""
    rb, rc = ranks(sizes, seed, draws, q)
    return tuple(collections.Counter(zip(rb, rc)).items())


def interval(base, cand, seed=SEED, draws=DRAWS, q=P95):
    """The bootstrap interval of p95(cand)/p95(base), or of the q
    quantile's ratio, at LEVEL: the draws of ratios at both ends, found
    by sorting each pair of ranks once, with its count."""
    base, cand = sorted(base), sorted(cand)
    counted = pairs((len(base), len(cand)), seed, draws, q)
    if base[0] > 0:
        r = [cand[j]/base[i] for (i, j), _ in counted]
    else:
        r = [cand[j]/base[i] if base[i] > 0 else (math.inf if cand[j] > 0 else 1.0) for (i, j), _ in counted]
    k = int(math.floor((1-LEVEL)/2*draws))
    ends, seen = [], 0
    for at in sorted(range(len(r)), key=r.__getitem__):
        seen += counted[at][1]
        while len(ends) < 2 and seen > (k, draws-1-k)[len(ends)]:
            ends.append(r[at])
    return ends[0], ends[1]


def ratio_low(base, cand, seed=SEED, draws=DRAWS, q=P95):
    """The lower bound of the bootstrap interval of p95(cand)/p95(base), or
    of the q quantile's ratio."""
    return interval(base, cand, seed, draws, q)[0]


def p95_low(values, seed=SEED, draws=DRAWS):
    """The lower bound of the bootstrap interval of the values' p95."""
    (rv,) = ranks((len(values),), seed, draws)
    values = sorted(values)
    return sorted(values[i] for i in rv)[int(math.floor((1-LEVEL)/2*draws))]


def splits(rounds, seed=SEED, most=SPLITS):
    """The controls at rounds rounds. A binary's runs are numbered as
    decide orders them, its first role's then its again role's, 0 to
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


def aligned(o_runs, n_runs, threshold=THRESHOLD):
    """What the controls divide the new binary's samples by: as much as
    brings the lower median of its runs' medians to within threshold of the
    old binary's, and 1 when it is within already. Load on half of a
    binary's runs or fewer leaves its lower median on a run the load
    missed. The new binary is brought to threshold, not to the old one, so
    that what sets the two apart by chance, which says how far apart the
    runs can be, still shows in the controls."""
    def typical(runs):
        medians = sorted(nearest_rank(r, 0.5) for r in runs if r)
        return medians[(len(medians)-1)//2]
    if not any(o_runs) or not any(n_runs):
        return 1.0
    k = typical(n_runs)/typical(o_runs)
    return k/threshold if k > threshold else k*threshold if k < 1/threshold else 1.0


def run_levels(figures):
    """How quick each of a binary's runs was, over all its figures: for
    each figure every run of which gave samples, each run's median over
    the lower median of its runs' medians; and for each run, the median of
    those over the figures. figures is a list, for each figure, of the
    binary's runs' samples. None when no figure had samples in every
    run."""
    rel = []
    for runs in figures:
        if runs and all(runs):
            medians = [nearest_rank(r, 0.5) for r in runs]
            typical = sorted(medians)[(len(medians)-1)//2]
            rel.append([m/typical for m in medians])
    if not rel:
        return None
    return [nearest_rank([r[i] for r in rel], 0.5) for i in range(len(rel[0]))]


def one_quick_run(levels, threshold=QUICK):
    """Whether a binary's quickest run, by run_levels, was more than
    threshold times as quick as the median of its other runs (their lower
    median): load on all its other runs, or on all but one or two of them
    at three rounds, which no control can tell from a change, or a run the
    machine sped up. The median of the others, not the next run, so that
    one of them slow by chance does not hide it."""
    if not levels or len(levels) < 3:
        return False
    ordered_levels = sorted(levels)
    return nearest_rank(ordered_levels[1:], 0.5) > threshold*ordered_levels[0]


def uneven_load(levels, other, threshold=UNEVEN):
    """Whether the slower half of a binary's runs, by run_levels, was more
    than threshold times as slow as its quicker half, both on its own and
    against the other binary's runs beside them: each of its runs over the
    other binary's run of the same number, which ran next to it in the same
    round (schedule). A half's run is the lower median of the slower half
    and the upper median of the quicker one, the second slowest and the
    second quickest of six, so one slow run alone does not count. Load on
    half of one binary's runs and not on the other's is what the pooled
    samples can read as a change while no control sees it, and when each
    run took its own share of it, no one run is quick; load on a whole
    round fell on both binaries' runs, and cancels out against the runs
    beside them."""
    if not levels or not other or len(levels) < 3 or len(other) != len(levels):
        return False
    half = len(levels)//2
    order = sorted(range(len(levels)), key=levels.__getitem__)
    paired = [mine/theirs for mine, theirs in zip(levels, other)]

    def gap(values):
        slower, quicker = sorted(values[i] for i in order[-half:]), sorted(values[i] for i in order[:half])
        return slower[(half-1)//2] > threshold*quicker[half//2]
    return gap(levels) and gap(paired)


def control_bounds(o_runs, n_runs, controls, seed=SEED, draws=DRAWS, q=P95):
    """The controls' lower bounds, both ways: for each split, the bound of
    its other side's p95 (or q quantile) over its first side's, and of the
    first over the other. A split with a side that has no samples, as when
    runs were lost, gives none."""
    return controls_of(o_runs, n_runs, controls, seed, draws, (q,))[q]


def controls_of(o_runs, n_runs, controls, seed=SEED, draws=DRAWS, qs=(P95,)):
    """control_bounds for each quantile in qs, by quantile, from the same
    sides: each run is sorted once, so each side is a merge."""
    o_runs, n_runs = [sorted(r) for r in o_runs], [sorted(r) for r in n_runs]
    every = range(len(o_runs))
    bounds = {q: [] for q in qs}
    for a_old, a_new in controls:
        a = sorted(itertools.chain(*[o_runs[i] for i in a_old], *[n_runs[i] for i in a_new]))
        b = sorted(itertools.chain(*[o_runs[i] for i in every if i not in a_old], *[n_runs[i] for i in every if i not in a_new]))
        if a and b:
            for q in qs:
                low, high = interval(a, b, seed, draws, q)
                bounds[q] += [low, 1/high if high > 0 else math.inf]
    return bounds


def judge(low, noise, threshold, what):
    """One statistic of a figure, its ratio's lower bound low against its
    controls' noise: the reasons it fails, and what leaves it unsettled."""
    if low > threshold*noise:
        return ["%s ratio's lower bound %.3f is above %.2f times the controls' %.3f" % (what, low, threshold, noise)], []
    if low > threshold:
        return [], ["%s ratio's lower bound %.3f is above %.2f, but not above %.2f times the controls' %.3f" % (
            what, low, threshold, threshold, noise)]
    return [], []


def decide(samples, ceilings, failures=None, rounds=ROUNDS, threshold=THRESHOLD, seed=SEED, draws=DRAWS,
           whole=WHOLE, quantile=NOISE_QUANTILE, noisy_figures=NOISY_FIGURES, sources=None, quick=QUICK,
           median_threshold=MEDIAN_THRESHOLD, uneven=UNEVEN):
    """The gate's verdict. samples maps each role (ROLES) to its runs, in
    order, each a map of each figure to its samples from that run; ceilings
    maps each gated figure to its ceiling in milliseconds, or None;
    failures holds the scenarios that failed, as scenarios() takes them;
    sources maps a figure to the scenario that gives it, when known; quick
    is the gap between a binary's quickest run and the median of its others
    that calls the machine too noisy, and uneven that between the slower
    and the quicker half of its runs (uneven_load); median_threshold is the
    median's own bound. Each role ran rounds times. The rule is the one above ROLES.

    A figure the old binary gave no samples, as one added with the change,
    is listed as not compared. One the new binary gave none fails, unless
    the new binary failed the scenario that gives it in every one of its
    runs (with no sources, any scenario), which the scenarios' rule then
    judges: it is short by those runs.

    The verdict is the first of these that applies: "could not run", exit
    2, when a scenario failed in every run of both binaries; "fail", exit 1,
    when the new binary broke a scenario or gave a figure no samples; "not
    judged", exit 0, when noisy_figures or more figures were too noisy to
    judge, one of a binary's runs was more than quick times as quick as the
    median of its others, or the slower half of its runs was more than
    uneven times as slow as the quicker half, on its own and against the
    other binary's runs; "fail", exit 1, when a figure failed; "not
    judged", exit 0, when a figure was too noisy to judge or unsettled, a
    scenario was lost, or a figure was short because a scenario failed in
    some runs; otherwise "pass", exit 0."""
    found = scenarios(failures or {}, rounds)
    runs = by_run(samples, rounds)
    controls = splits(rounds, seed)
    every = found["runs_per_binary"]
    lost_everywhere = {s for s, c in found["failed_runs"].items() if c["new"] >= every}
    metrics, unsettled_all, failed, missing, skipped, too_few, short, noisy, unjudged = {}, [], [], [], [], [], [], [], []
    judged_runs = {"old": [], "new": []}
    for name in sorted(ceilings):
        o_runs = [list(run.get(name) or []) for run in runs["old"]]
        n_runs = [list(run.get(name) or []) for run in runs["new"]]
        o, n = [v for r in o_runs for v in r], [v for r in n_runs for v in r]
        if not o:
            skipped.append(name)
            continue
        if not n:
            source = (sources or {}).get(name)
            if (source in lost_everywhere) if source is not None else lost_everywhere:
                too_few.append(name)
                short.append(name)
            else:
                missing.append("%s: the new binary gave no samples, the old one %d" % (name, len(o)))
            continue
        k = aligned(o_runs, n_runs, threshold)
        n_aligned = [[v/k for v in r] for r in n_runs]
        bounds = controls_of(o_runs, n_aligned, controls, seed, draws, (P95, MEDIAN))
        lows, m_lows = bounds[P95], bounds[MEDIAN]
        noise = max(1.0, nearest_rank(lows, quantile)) if lows else math.inf
        m_noise = max(1.0, nearest_rank(m_lows, quantile)) if m_lows else math.inf
        low, m_low = ratio_low(o, n, seed, draws), ratio_low(o, n, seed, draws, MEDIAN)
        p95_o, p95_n, p95_n_low = nearest_rank(o), nearest_rank(n), p95_low(n, seed, draws)
        med_o, med_n = nearest_rank(o, MEDIAN), nearest_rank(n, MEDIAN)
        ceiling = ceilings[name]
        reasons, unsettled, judged_on = [], [], []
        enough = min(len(o), len(n)) >= whole
        if enough:
            judged_runs["old"].append(o_runs)
            judged_runs["new"].append(n_runs)
        if not enough:
            too_few.append(name)
            # Short by failed runs: the scenario that gives it (with no
            # sources, any scenario) failed in some run, and each run, given
            # as many samples as the figure's fullest, would have made whole.
            source = (sources or {}).get(name)
            failed_some = (source in found["failed_runs"]) if source is not None else bool(found["failed_runs"])
            if failed_some and max(len(r) for r in o_runs+n_runs)*2*rounds >= whole:
                short.append(name)
        else:
            if noise <= threshold:
                judged_on.append("p95")
                r, u = judge(low, noise, threshold, "p95")
                reasons += r
                unsettled += u
                if ceiling is not None and p95_n_low > ceiling >= p95_o:
                    reasons.append("p95 %.1f ms, at least %.1f ms, passed the ceiling of %g ms" % (p95_n, p95_n_low, ceiling))
            else:
                noisy.append(name)
            if m_noise <= threshold:
                judged_on.append("median")
                r, u = judge(m_low, m_noise, median_threshold, "median")
                reasons += r
                unsettled += u
            if not judged_on:
                unjudged.append(name)
                unsettled.append("the controls' %.3f, and the median's %.3f, are above %.2f: too noisy to judge" % (
                    noise, m_noise, threshold))
        metrics[name] = {
            "p95_base_ms": round(p95_o, 3), "p95_ms": round(p95_n, 3), "p95_low_ms": round(p95_n_low, 3),
            "ratio": round(p95_n/p95_o, 3) if p95_o > 0 else None,
            "ratio_low": round(low, 3) if math.isfinite(low) else None,
            "median_base_ms": round(med_o, 3), "median_ms": round(med_n, 3),
            "median_ratio_low": round(m_low, 3) if math.isfinite(m_low) else None,
            "n_old": len(o), "n_new": len(n), "ceiling_ms": ceiling,
            "n_runs": {role: [len(run.get(name) or []) for run in samples.get(role, [])] for role in ROLES},
            "controls": len(lows), "aligned": round(k, 3), "noise": round(noise, 3) if math.isfinite(noise) else None,
            "median_noise": round(m_noise, 3) if math.isfinite(m_noise) else None, "judged_on": judged_on,
            "too_few_samples": not enough, "reasons": reasons, "unsettled": unsettled,
            "judged": enough and not unsettled}
        unsettled_all += ["%s: %s" % (name, r) for r in unsettled]
        failed += ["%s: %s" % (name, r) for r in reasons]
    hard = ["the new binary failed the scenario %s in %d of %d runs, the old one in %d" % (
        s, found["failed_runs"][s]["new"], every, found["failed_runs"][s]["old"])
        for s in found["broken"]] + missing
    lost = ["the new binary failed the scenario %s in %d of %d runs, the old one in %d: load may have lost it, run the gate again" % (
        s, found["failed_runs"][s]["new"], every, found["failed_runs"][s]["old"])
        for s in found["lost"]]
    lost += ["%s has too few samples because a scenario failed in some runs: run the gate again" % name for name in short]
    levels = {b: run_levels(f) for b, f in judged_runs.items()}
    quick_runs = [b for b in ("old", "new") if one_quick_run(levels[b], quick)]
    uneven_runs = [b for b in ("old", "new") if uneven_load(levels[b], levels["new" if b == "old" else "old"], uneven)]
    machine = len(noisy) >= noisy_figures or bool(quick_runs) or bool(uneven_runs)
    if found["unrunnable"]:
        verdict, code = "could not run", 2
    elif hard:
        verdict, code = "fail", 1
    elif machine:
        verdict, code = "not judged", 0
    elif failed:
        verdict, code = "fail", 1
    elif lost or unsettled_all:
        verdict, code = "not judged", 0
    else:
        verdict, code = "pass", 0
    return {"verdict": verdict, "exit": code, "rule": RULE, "rounds": rounds, "threshold": threshold,
            "median_threshold": median_threshold, "seed": seed, "draws": draws,
            "whole": whole, "noise_quantile": quantile, "noisy_figures": noisy_figures, "quick": quick, "uneven": uneven,
            "controls": len(controls), "machine_too_noisy": machine, "too_noisy": noisy, "too_noisy_to_judge": unjudged,
            "one_quick_run": quick_runs, "uneven_load": uneven_runs,
            "run_levels": {b: [round(x, 3) for x in v] if v else None for b, v in levels.items()}, "unsettled": unsettled_all, "lost": lost,
            "failures": hard+failed, "too_few_samples": too_few, "short_by_failures": short, "scenarios": found,
            "not_compared": skipped, "metrics": metrics}


def samples_by_run(gate):
    """A gate.json's samples, each role's by run, in its runs' order. The
    first rule's gate.json kept each role's samples pooled: they are split
    into its runs by the sample counts each run's report gives
    ("<figure>_samples")."""
    by_role = {}
    for role, value in gate["samples"].items():
        if isinstance(value, list):
            by_role[role] = [dict(run) for run in value]
            continue
        cut, runs = {name: 0 for name in value}, []
        for entry in gate["runs"]:
            if entry["binary"] != role:
                continue
            run = {}
            for name, values in value.items():
                n = entry["report"].get(name+"_samples") or 0
                if n:
                    run[name] = values[cut[name]:cut[name]+n]
                cut[name] += n
            runs.append(run)
        if any(cut[name] != len(values) for name, values in value.items()):
            raise ValueError("the samples of %s do not add up to its runs' counts" % role)
        by_role[role] = runs
    return by_role


def ceilings_of(gate):
    """A gate.json's ceilings: the first rule's kept them only in its
    decision's metrics."""
    if "ceilings" in gate:
        return gate["ceilings"]
    return {name: m["ceiling_ms"] for name, m in gate["decision"]["metrics"].items()}


def redecide(gate):
    """The verdict worked out again by this rule from a gate.json: its
    samples by run, its ceilings, its scenarios' failures and the scenario
    that gave each figure. One this rule wrote (its decision's "rule" is
    RULE) is worked out by the parameters it recorded, so exactly to the
    verdict it printed; an earlier rule's, by this rule's own, the first
    rule's too, whose samples samples_by_run splits into runs."""
    d = gate["decision"]
    if d.get("rule") != RULE:
        return decide(samples_by_run(gate), ceilings_of(gate), gate.get("scenario_failures"), gate["rounds"],
                      sources=gate.get("figure_scenarios"))
    return decide(gate["samples"], gate["ceilings"], gate["scenario_failures"], gate["rounds"], d["threshold"],
                  d["seed"], d["draws"], d["whole"], d["noise_quantile"], d["noisy_figures"], gate.get("figure_scenarios"),
                  d["quick"], d["median_threshold"], d["uneven"])


