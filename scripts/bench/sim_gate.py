"""The latency gate's rule, measured by simulation. Run by hand; it is not a
test, and takes minutes:

    python3 -I scripts/bench/sim_gate.py [--seeds 300] [--load NAME ...] [--change NAME ...]
    python3 -I scripts/bench/sim_gate.py --gates DIR/same-*.json ...
    python3 -I scripts/bench/sim_gate.py --monotonic [--gates ...]
    python3 -I scripts/bench/sim_gate.py --rework GATE.json ...

It feeds core.decide made-up samples, as the gate's runs give them at the
default two rounds: each hook figure with the samples a run it has in the
gate's runs, a long right tail, a few slow outliers, runs that differ a
little on a quiet machine, by as much as the figure's runs differ in the
saved gates, and gates that differ from each other in that too. --gates
takes the base samples from saved gate.json files of an unchanged binary
instead: each seed takes one gate's eight runs and deals them to the four
roles at random, each run's samples of a figure topped up to the gate's
count by drawing from that run's own (a saved gate's chat-start figures
have 10 a run, from before the gate took more). On that base it puts a load
(none, light bursts, heavy bursts, whole runs, a drift, one run of each of
the new binary's roles, or three or all four of the new binary's runs and
none of the old one's) and a change to the new binary (none, every hook
20%, 30%, 50% slower or two or three times as slow, 20 ms added to
capture, or the first chat start alone half as slow again or twice as
slow), and prints, for each pair, the share of seeds whose verdict was
fail, pass and not judged, with the figures that most often failed or
stopped the verdict.

--monotonic takes the same base samples a seed, on a quiet machine, with
every hook 20%, 30%, 50% slower and two and three times as slow, and counts
the seeds in which a smaller rise failed and a larger one did not.

--rework works saved gate.json files out again by the rule as it now is,
from their samples by run (a file written before the samples were kept by
run is split into runs by its runs' sample counts), and prints each
verdict and what decided it.

docs/v2/baselines/README.md holds its tables for the rule as merged.
"""
import argparse
from collections import Counter
import importlib.util
import json
import math
from pathlib import Path
import random
import sys
import time

HERE = Path(__file__).resolve().parent
sys.dont_write_bytecode = True
if "evabench" in sys.modules:  # core.py, loaded already, as by test_gate.py
    core = sys.modules["evabench"]
else:
    spec = importlib.util.spec_from_file_location("evabench", HERE/"core.py")
    core = importlib.util.module_from_spec(spec)
    sys.modules["evabench"] = core
    spec.loader.exec_module(core)

# The figures the gate compares, in the order a run times them, each with
# the samples a run gives it in the gate's runs, its median in ms, how much
# its runs' medians differ on a quiet machine (the standard deviation of
# their logarithm, the median over the saved gates), and its ceiling (twice
# its target), from the 21 gates of an unchanged binary the gate's proof ran
# on Linux. The gate's runs time one chat start untimed before the first
# figure's, so none of its samples is a cold start.
G = core.GATE_RUN_SAMPLES
FIGURES = [
    # name, samples a run, median, run spread, ceiling
    ("startup", G, 63, 0.06, 600),
    ("startup_with_programs", G, 110, 0.045, 600),
    ("transcript_capture", 100, 17, 0.07, 100),
    ("startup_with_records", G, 75, 0.05, 600),
    ("capture", 100, 17, 0.045, 100),
    ("prompt_after_brief", 100, 17, 0.055, 100),
    ("prompt_with_pointers", 100, 22, 0.035, 100),
    ("capture_with_spawn", G, 17, 0.05, 100),
    ("session_end", G, 19, 0.075, 400),
    ("code_brief", G, 48, 0.06, 600),
    ("code_prompt", G, 24, 0.045, 100),
    ("prompt_with_recall", 100, 28, 0.033, 100),
    ("process_start_claude_code", 100, 10, 0.033, None),
    ("process_start_codex", 100, 11.5, 0.033, None),
    ("real_install_startup", G, 89, 0.035, 600),
    ("real_install_prompt", 50, 18, 0.04, 100),
    ("real_install_capture", 50, 17, 0.04, 100),
    ("turn_claude_code_tool", 50, 14, 0.045, 100),
    ("turn_codex_tool", 50, 16, 0.05, 100),
]
COUNTS = {f[0]: f[1] for f in FIGURES}
CEILINGS = {f[0]: f[4] for f in FIGURES}
ORDER = [role for _, role in core.schedule(core.ROUNDS)]  # the runs, in time
RUNS = len(ORDER)
# How much a figure's run spread differs from gate to gate (the standard
# deviation of its logarithm), and how much the share of a run's samples in
# the long tail differs from run to run: the saved gates' runs differ in
# their p95 by about a third more than in their medians.
GATE_SPREAD = 0.4
TAIL_SPREAD = 0.6


def body(rng, tail):
    """One sample over its run's median: a lognormal body with a longer
    right tail, a share tail of the samples in it, and now and then an
    outlier, as the proof's samples have (over their run's median: q75
    1.07, q90 1.16, q95 1.23, q99 1.42, and about 1 in 700 more than twice
    it)."""
    v = math.exp(rng.gauss(0, 0.085))
    if rng.random() < tail:
        v *= math.exp(rng.expovariate(1/0.1))
    if rng.random() < 0.0015:
        v *= rng.uniform(2, 8)
    return v


def made_up(rng):
    """The samples of one gate of an unchanged binary, by run in time
    order: for each run, each figure's samples."""
    spreads = {name: spread*math.exp(rng.gauss(0, GATE_SPREAD)) for name, _, _, spread, _ in FIGURES}
    runs = []
    for _ in range(RUNS):
        run = {}
        for name, n, median, _, _ in FIGURES:
            level = median*math.exp(rng.gauss(0, spreads[name]))
            tail = min(0.3*math.exp(rng.gauss(0, TAIL_SPREAD)), 0.9)
            run[name] = [level*body(rng, tail) for _ in range(n)]
        runs.append(run)
    return runs


def runs_of(gate):
    """A saved gate's samples by run, in its runs' order; a gate saved
    before they were kept by run is split by its runs' sample counts."""
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
    return gate.get("ceilings") or {name: m["ceiling_ms"] for name, m in gate["decision"]["metrics"].items()}


def from_gates(gates):
    """A base drawn from saved gates: one gate's eight runs dealt to the
    roles at random, with the samples they have. A gate one of whose runs
    lost a scenario is not drawn: its figures would be short by a run,
    which the gate's runs now take enough samples to stand."""
    pools = []
    for gate in gates:
        by_role = runs_of(gate)
        runs = [run for role in core.ROLES for run in by_role[role]]
        if all(set(run) == set(runs[0]) for run in runs):
            pools.append(runs)

    def base(rng):
        runs = [dict(run) for run in rng.choice(pools)]
        rng.shuffle(runs)
        return runs
    return base


# Where each sample falls in the gate's time, in runs from its start: a run
# times its figures in turn, each for a share of the run as long as its
# samples take.
def positions(runs):
    out = []
    for k, run in enumerate(runs):
        total = sum(sum(v) for v in run.values()) or 1
        at, by = 0.0, {}
        for name in [f[0] for f in FIGURES if f[0] in run] + [n for n in run if n not in CEILINGS]:
            v = run[name]
            width = sum(v)/total
            by[name] = [k + at + width*(j+0.5)/len(v) for j in range(len(v))]
            at += width
        out.append(by)
    return out


def bursts(rate, length, low, high):
    """Bursts of load: about rate a run, each lasting about length of a run,
    each slowing what it covers by a factor between low and high."""
    def make(rng):
        spans = []
        for _ in range(poisson(rng, rate*RUNS)):
            s = rng.uniform(0, RUNS)
            spans.append((s, s+rng.expovariate(1/length), rng.uniform(low, high)))
        return lambda t: max([f for a, b, f in spans if a <= t < b] or [1.0])
    return make


def poisson(rng, mean):
    k, p, limit = 0, 1.0, math.exp(-mean)
    while True:
        p *= rng.random()
        if p < limit:
            return k
        k += 1


def whole_runs(rng):
    """Each run loaded, or not, on its own: a quarter of them, each by a
    factor between 1.2 and 1.8."""
    factors = [rng.uniform(1.2, 1.8) if rng.random() < 0.25 else 1.0 for _ in range(RUNS)]
    return lambda t: factors[min(int(t), RUNS-1)]


def drift(rng):
    """The machine slows steadily, by a half over the gate."""
    return lambda t: 1 + 0.5*t/RUNS


def new_pair(rng):
    """One run of new and one of new_again loaded, by a factor between 1.2
    and 2: the load a control of whole roles cannot see."""
    new = [k for k, role in enumerate(ORDER) if role == "new"]
    again = [k for k, role in enumerate(ORDER) if role == "new_again"]
    loaded, f = {rng.choice(new), rng.choice(again)}, rng.uniform(1.2, 2.0)
    return lambda t: f if int(t) in loaded else 1.0


def new_runs(count):
    """count of the new binary's runs loaded and none of the old one's, by
    a factor between 1.2 and 2: with three or four, no control can tell the
    load from a change."""
    def make(rng):
        loaded = set(rng.sample([k for k, role in enumerate(ORDER) if core.BINARY[role] == "new"], count))
        f = rng.uniform(1.2, 2.0)
        return lambda t: f if int(t) in loaded else 1.0
    return make


LOADS = {
    "none": lambda rng: None,
    "light": bursts(1.0, 0.05, 1.2, 1.6),
    "heavy": bursts(3.0, 0.10, 1.5, 3.0),
    "runs": whole_runs,
    "drift": drift,
    "new-pair": new_pair,
    "new-3": new_runs(3),
    "new-4": new_runs(4),
}
CHANGES = {
    "unchanged": lambda name, v: v,
    "all-x1.2": lambda name, v: v*1.2,
    "all-x1.3": lambda name, v: v*1.3,
    "all-x1.5": lambda name, v: v*1.5,
    "all-x2": lambda name, v: v*2,
    "all-x3": lambda name, v: v*3,
    "capture+20ms": lambda name, v: v+20 if name == "capture" else v,
    "startup-x1.5": lambda name, v: v*1.5 if name == "startup" else v,
    "startup-x2": lambda name, v: v*2 if name == "startup" else v,
}
DEFAULT_LOADS = ["none", "light", "heavy", "runs", "drift", "new-pair"]
DEFAULT_CHANGES = ["unchanged", "all-x1.2", "all-x1.3", "capture+20ms"]
RISES = ["all-x1.2", "all-x1.3", "all-x1.5", "all-x2", "all-x3"]


def one(runs, load, change, rng):
    """One gate's samples by role: the base runs, the change on the new
    binary's, and the load over them all; a loaded sample is slowed by a
    share of the load that differs from sample to sample."""
    where = positions(runs)
    factor = load(rng)
    samples = {role: [] for role in core.ROLES}
    for k, (role, run) in enumerate(zip(ORDER, runs)):
        out = {}
        for name, values in run.items():
            new = core.BINARY[role] == "new"
            vals = []
            for v, t in zip(values, where[k][name]):
                v = change(name, v) if new else v
                if factor is not None:
                    f = factor(t)
                    if f != 1.0:
                        v *= 1 + (f-1)*rng.uniform(0.5, 1.5)
                vals.append(v)
            out[name] = vals
        samples[role].append(out)
    return samples


def seeded(s):
    return random.Random(1_000_003*s + 7)


def tally(d, why):
    if d["verdict"] == "not judged":
        for name, m in d["metrics"].items():
            if m["unsettled"]:
                why["stopped by "+name if "too noisy" in m["unsettled"][0] else "unsettled "+name] += 1
        if d["lost"]:
            why["lost scenario"] += 1
    for f in d["failures"]:
        why["failed "+f.split(":")[0]] += 1


def measure(base, ceilings, load, change, seeds, first=0, quantile=core.NOISE_QUANTILE):
    verdicts, why = Counter(), Counter()
    for s in range(first, first+seeds):
        rng = seeded(s)
        d = core.decide(one(base(rng), LOADS[load], CHANGES[change], rng), ceilings, quantile=quantile)
        verdicts[d["verdict"]] += 1
        tally(d, why)
    return verdicts, why


def monotonic(base, ceilings, seeds, first=0, quantile=core.NOISE_QUANTILE):
    """The verdicts of each rise in RISES on the same base samples a seed,
    on a quiet machine: the share of seeds each failed, and the seeds in
    which a smaller rise failed and a larger one did not."""
    fails, broken = Counter(), []
    for s in range(first, first+seeds):
        runs = base(seeded(s))
        verdicts = []
        for change in RISES:
            d = core.decide(one(runs, LOADS["none"], CHANGES[change], seeded(s)), ceilings, quantile=quantile)
            verdicts.append(d["verdict"])
            fails[change] += d["verdict"] == "fail"
        if any(verdicts[i] == "fail" and verdicts[j] != "fail" for i in range(len(RISES)) for j in range(i+1, len(RISES))):
            broken.append((s, verdicts))
    return fails, broken


def rework(paths, quantile=core.NOISE_QUANTILE):
    for path in paths:
        gate = json.loads(Path(path).read_text())
        by_role = runs_of(gate)
        d = core.decide(by_role, ceilings_of(gate), gate.get("scenario_failures"), gate["rounds"], quantile=quantile,
                        sources=gate.get("figure_scenarios"))
        noisy = [n for n, m in d["metrics"].items() if m["unsettled"] and "too noisy" in m["unsettled"][0]]
        print("%-12s %-10s was %-10s failed: %s  noisy: %s  unsettled: %s  too few: %d  lost: %d" % (
            Path(path).stem, d["verdict"], gate.get("verdict"), sorted({f.split(":")[0] for f in d["failures"]}) or "-",
            noisy or "-", [u.split(":")[0] for u in d["unsettled"] if "too noisy" not in u] or "-",
            len(d["too_few_samples"]), len(d["lost"])))


def main():
    p = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    p.add_argument("--seeds", type=int, default=300)
    p.add_argument("--first", type=int, default=0, help="the first seed")
    p.add_argument("--quantile", type=float, default=core.NOISE_QUANTILE,
                   help="the quantile of the controls' bounds a figure's noise is, to try another")
    p.add_argument("--load", nargs="+", choices=sorted(LOADS), default=DEFAULT_LOADS)
    p.add_argument("--change", nargs="+", choices=sorted(CHANGES), default=DEFAULT_CHANGES)
    p.add_argument("--gates", nargs="+", type=Path, help="take the base samples from these gate.json files")
    p.add_argument("--monotonic", action="store_true", help="count the seeds in which a larger rise did not fail")
    p.add_argument("--rework", nargs="+", type=Path, help="work these gate.json files out again, and stop")
    a = p.parse_args()
    if a.rework:
        rework(a.rework, a.quantile)
        return 0
    if a.gates:
        gates = [json.loads(path.read_text()) for path in a.gates]
        base, ceilings = from_gates(gates), ceilings_of(gates[0])
    else:
        base, ceilings = made_up, CEILINGS
    print("rule: noise is the %g quantile of %d controls' bounds; threshold %.2f; %d samples a side to judge; %s base; %d seeds a case" % (
        a.quantile, 4*len(core.splits(core.ROUNDS)), core.THRESHOLD, core.WHOLE,
        "saved gates'" if a.gates else "made-up", a.seeds), flush=True)
    if a.monotonic:
        began = time.time()
        fails, broken = monotonic(base, ceilings, a.seeds, a.first, a.quantile)
        print("quiet machine, the same samples a seed: %s; a larger rise not failing where a smaller one failed: %d of %d seeds%s (%.0f s)" % (
            ", ".join("%s fails %.1f%%" % (c, 100*fails[c]/a.seeds) for c in RISES), len(broken), a.seeds,
            "".join("\n  seed %d: %s" % (s, " ".join(v)) for s, v in broken[:10]), time.time()-began), flush=True)
        return 0
    print("%-10s %-14s %6s %6s %10s  %s" % ("load", "change", "fail", "pass", "not judged", "most often"), flush=True)
    for load in a.load:
        for change in a.change:
            began = time.time()
            verdicts, why = measure(base, ceilings, load, change, a.seeds, a.first, a.quantile)
            share = {v: 100*verdicts[v]/a.seeds for v in ("fail", "pass", "not judged")}
            print("%-10s %-14s %5.1f%% %5.1f%% %9.1f%%  %s  (%.0f s)" % (
                load, change, share["fail"], share["pass"], share["not judged"],
                ", ".join("%s %d" % kv for kv in why.most_common(3)) or "-", time.time()-began), flush=True)
    return 0


if __name__ == "__main__":
    sys.exit(main())
