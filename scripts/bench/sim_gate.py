"""The latency gate's rule, measured by simulation. Run by hand; it is not a
test, and takes minutes:

    python3 -I scripts/bench/sim_gate.py [--seeds 300] [--load NAME ...] [--change NAME ...] [--rounds N]
                                         [--common 0.025] [--own-scale 1]
    python3 -I scripts/bench/sim_gate.py --gates DIR/same-*.json ...
    python3 -I scripts/bench/sim_gate.py --monotonic [--gates ...]
    python3 -I scripts/bench/sim_gate.py --rework GATE.json ...

It feeds core.decide made-up samples, as the gate's runs give them at the
default three rounds (or --rounds): each hook figure with the samples a run
it has in the gate's runs, a long right tail, a few slow outliers, runs
that differ a little on a quiet machine, as the saved gates' runs differ
(by a factor each run shares with every figure, and by one of each
figure's own), and gates that differ from each other in that too. --gates
takes the base samples from saved gate.json files of an unchanged binary,
all from one machine, instead: at two rounds each seed takes one gate's
eight runs and deals them to the four roles at random; at three, two
gates, each figure of the second brought to the first's level, and twelve
of their sixteen runs, never one twice (from_gates). The runs keep the
samples they have (a saved gate's chat-start figures may have 10 a run,
from before the gate took more, and are too few to judge there), and a
gate that lost a run is not drawn. On that base it puts a load (none,
light bursts, heavy bursts, whole runs, a drift, one run of each of the new
binary's roles, the four runs of one round, three or four of the new
binary's runs and none of the old one's, or a mild load of 1.1 to 1.3 on
three, all but one or all of them, by one factor or, with -each, by one
each loaded run draws) and a change to the new binary (none,
every hook 20%, 30%, 50% slower or two or three times as slow, 20 ms
added to capture, one hook alone 30% or 50% slower or two or three times
as slow, capture slower by a factor each of the new binary's runs draws,
or every hook 30% slower in all but one of the new binary's runs), and
prints, for each pair, the share of seeds whose verdict was fail, pass and
not judged, with the figures that most often failed or stopped the
verdict. --common and --own-scale widen the made-up base's runs, as CI's
runner's differ: a run's common factor, and each figure's own.

--monotonic takes the same base samples a seed, on a quiet machine, with
every hook 20%, 30%, 50% slower and two and three times as slow, and each
hook alone 30%, 50% slower and two and three times as slow, and counts the
seeds in which a smaller rise failed and a larger one did not.

--rework works saved gate.json files out again by the rule as it now is,
from their samples by run (a file written before the samples were kept by
run is split into runs by its runs' sample counts), and prints each
verdict and what decided it.

docs/v2/baselines/README.md holds its tables for the rule as it now is.
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
# its runs' medians differ on a quiet machine beyond what every figure of a
# run shares (the standard deviation of their logarithm, less the run's
# common factor, the median over the saved gates), and its ceiling (twice
# its target), from the 21 gates of an unchanged binary the gate's proof ran
# on Linux. The gate's runs time one chat start untimed before the first
# figure's, so none of its samples is a cold start.
G = core.GATE_RUN_SAMPLES
FIGURES = [
    # name, samples a run, median, run spread of its own, ceiling
    ("startup", G, 63, 0.049, 600),
    ("startup_with_programs", G, 110, 0.036, 600),
    ("transcript_capture", 100, 17, 0.056, 100),
    ("startup_with_records", G, 75, 0.037, 600),
    ("capture", 100, 17, 0.039, 100),
    ("prompt_after_brief", 100, 17, 0.051, 100),
    ("prompt_with_pointers", 100, 22, 0.027, 100),
    ("capture_with_spawn", G, 17, 0.044, 100),
    ("session_end", G, 19, 0.082, 400),
    ("code_brief", G, 48, 0.062, 600),
    ("code_prompt", G, 24, 0.038, 100),
    ("prompt_with_recall", 100, 28, 0.031, 100),
    ("process_start_claude_code", 100, 10, 0.032, None),
    ("process_start_codex", 100, 11.5, 0.026, None),
    ("real_install_startup", G, 89, 0.030, 600),
    ("real_install_prompt", 50, 18, 0.033, 100),
    ("real_install_capture", 50, 17, 0.035, 100),
    ("turn_claude_code_tool", 50, 14, 0.036, 100),
    ("turn_codex_tool", 50, 16, 0.044, 100),
]
COUNTS = {f[0]: f[1] for f in FIGURES}
CEILINGS = {f[0]: f[4] for f in FIGURES}
ORDER, RUNS, ROUNDS = [], 0, core.ROUNDS


def set_spread(common, own_scale):
    """The made-up base's run spread: its common factor and the scale of
    each figure's own."""
    global COMMON, OWN_SCALE
    COMMON, OWN_SCALE = common, own_scale


def set_rounds(rounds):
    """The runs, in time, at rounds rounds: the made-up base and the loads
    read them."""
    global ORDER, RUNS, ROUNDS
    ORDER = [role for _, role in core.schedule(rounds)]
    RUNS, ROUNDS = len(ORDER), rounds


set_rounds(core.ROUNDS)
# What every figure of a run shares, the run's common factor: the standard
# deviation of its logarithm, the median over the saved gates (0.025), and
# how much it differs from gate to gate. How much a figure's own run spread
# differs from gate to gate (the standard deviation of its logarithm), and
# how much the share of a run's samples in the long tail differs from run
# to run: the saved gates' runs differ in their p95 by about a third more
# than in their medians.
COMMON = 0.025
# How much more than that a figure's own run spread is (--own-scale), and
# the common factor (--common), for a machine whose runs differ more, as
# CI's runner's do: there the median over its saved gates, the first
# round's load left out, is about 0.06 and 0.09.
OWN_SCALE = 1.0
COMMON_SPREAD = 0.3
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
    spreads = {name: OWN_SCALE*spread*math.exp(rng.gauss(0, GATE_SPREAD)) for name, _, _, spread, _ in FIGURES}
    common = COMMON*math.exp(rng.gauss(0, COMMON_SPREAD))
    runs = []
    for _ in range(RUNS):
        run, shared = {}, rng.gauss(0, common)
        for name, n, median, _, _ in FIGURES:
            level = median*math.exp(shared + rng.gauss(0, spreads[name]))
            tail = min(0.3*math.exp(rng.gauss(0, TAIL_SPREAD)), 0.9)
            run[name] = [level*body(rng, tail) for _ in range(n)]
        runs.append(run)
    return runs


# A saved gate's samples by run, in its runs' order (a gate saved before
# they were kept by run is split by its runs' sample counts), and its
# ceilings: as redecide reads them.
runs_of, ceilings_of = core.samples_by_run, core.ceilings_of


def level(runs):
    """Each figure's typical run in a gate's runs: the lower median of its
    runs' medians."""
    out = {}
    for name in runs[0]:
        medians = sorted(core.nearest_rank(run[name], 0.5) for run in runs)
        out[name] = medians[(len(medians)-1)//2]
    return out


def from_gates(gates):
    """A base drawn from saved gates of an unchanged binary, all from one
    machine. At RUNS runs or fewer (two rounds): one gate's runs dealt to
    the roles at random, with the samples they have. At more: two gates
    drawn, each figure of the second brought to the level of the first's
    (by its typical run, level), so that the gap between the gates is not
    read as load, and RUNS of their runs drawn and dealt at random, never
    one run twice. A gate one of whose runs lost a scenario is not drawn:
    its figures would be short by a run, which the gate's runs now take
    enough samples to stand."""
    pools = []
    for gate in gates:
        by_role = runs_of(gate)
        runs = [run for role in core.ROLES for run in by_role[role]]
        if all(set(run) == set(runs[0]) for run in runs):
            pools.append(runs)
    if not pools or RUNS > len(pools[0])*min(len(pools), 2):
        raise ValueError("%d saved gates that lost no run cannot give %d runs" % (len(pools), RUNS))

    def base(rng):
        first = rng.randrange(len(pools))
        runs = [dict(run) for run in pools[first]]
        if RUNS > len(runs):
            second = rng.choice([k for k in range(len(pools)) if k != first])
            at, there = level(pools[first]), level(pools[second])
            runs += [{name: [v*at[name]/there[name] for v in values] for name, values in run.items() if name in at}
                     for run in pools[second]]
        rng.shuffle(runs)
        return runs[:RUNS]
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


def new_runs(count, low=1.2, high=2.0, each=False):
    """count of the new binary's runs loaded and none of the old one's, by
    one factor between low and high, or with each, by a factor each loaded
    run draws on its own, as load that comes and goes would; count below 0
    is all the new binary's runs but -count of them. With all of them, no
    control can tell the load from a change."""
    def make(rng):
        mine = [k for k, role in enumerate(ORDER) if core.BINARY[role] == "new"]
        loaded = rng.sample(mine, count if count > 0 else len(mine)+count)
        f = rng.uniform(low, high)
        factors = {k: rng.uniform(low, high) if each else f for k in loaded}
        return lambda t: factors.get(int(t), 1.0)
    return make


def one_round(rng):
    """The four runs of one round loaded, a round drawn at random, by a
    factor between 1.2 and 2: as CI's runner, still busy from the builds
    before the gate, loads its first round."""
    r, f = rng.randrange(ROUNDS), rng.uniform(1.2, 2.0)
    return lambda t: f if int(t)//4 == r else 1.0


LOADS = {
    "none": lambda rng: None,
    "light": bursts(1.0, 0.05, 1.2, 1.6),
    "heavy": bursts(3.0, 0.10, 1.5, 3.0),
    "runs": whole_runs,
    "drift": drift,
    "new-pair": new_pair,
    "round": one_round,
    "new-3": new_runs(3),
    "new-3-mild": new_runs(3, 1.1, 1.3),
    "new-4": new_runs(4),
    "new-but-1-mild": new_runs(-1, 1.1, 1.3),
    "new-all-mild": new_runs(0, 1.1, 1.3),
    "new-3-mild-each": new_runs(3, 1.1, 1.3, each=True),
    "new-but-1-mild-each": new_runs(-1, 1.1, 1.3, each=True),
    "new-all-mild-each": new_runs(0, 1.1, 1.3, each=True),
}


def every(f):
    """A change to every sample of the new binary, the same in every run."""
    return lambda rng: lambda name, v, run: f(name, v)


def capture_varies(rng):
    """capture slower by a factor between 1.2 and 2 that each of the new
    binary's runs draws: a regression whose cost differs from run to run."""
    factors = {}
    return lambda name, v, run: v*factors.setdefault(run, rng.uniform(1.2, 2.0)) if name == "capture" else v


def in_three_runs(rng):
    """Every hook 30% slower in all the new binary's runs but one (three of
    four at two rounds), and unchanged in the other: a regression that
    shows in only some runs."""
    spared = rng.randrange(2*ROUNDS)
    return lambda name, v, run: v if run == spared else v*1.3


CHANGES = {
    "unchanged": every(lambda name, v: v),
    "all-x1.2": every(lambda name, v: v*1.2),
    "all-x1.3": every(lambda name, v: v*1.3),
    "all-x1.5": every(lambda name, v: v*1.5),
    "all-x2": every(lambda name, v: v*2),
    "all-x3": every(lambda name, v: v*3),
    "capture+20ms": every(lambda name, v: v+20 if name == "capture" else v),
    "capture-varies": capture_varies,
    "all-x1.3-in-3": in_three_runs,
}
ALONE = (1.3, 1.5, 2, 3)  # the rises of one hook alone, as startup-x1.5
for _name in [f[0] for f in FIGURES]:
    for _x in ALONE:
        CHANGES["%s-x%g" % (_name, _x)] = every(lambda name, v, _f=_name, _x=_x: v*_x if name == _f else v)
DEFAULT_LOADS = ["none", "light", "heavy", "runs", "drift", "new-pair", "round", "new-3", "new-3-mild"]
DEFAULT_CHANGES = ["unchanged", "all-x1.2", "all-x1.3", "capture+20ms"]
RISES = ["all-x1.2", "all-x1.3", "all-x1.5", "all-x2", "all-x3"]


def one(runs, load, change, rng):
    """One gate's samples by role: the base runs, the change on the new
    binary's, and the load over them all; a loaded sample is slowed by a
    share of the load that differs from sample to sample. A change draws
    from a generator of its own, so that every change of a seed meets the
    same load."""
    where = positions(runs)
    factor = load(rng)
    change = change(random.Random(rng.getrandbits(32)))
    samples = {role: [] for role in core.ROLES}
    nth = {}
    for k, (role, run) in enumerate(zip(ORDER, runs)):
        out = {}
        new = core.BINARY[role] == "new"
        if new:
            nth[k] = len(nth)
        for name, values in run.items():
            vals = []
            for v, t in zip(values, where[k][name]):
                v = change(name, v, nth[k]) if new else v
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
        if d["machine_too_noisy"]:
            why["quick run" if d["one_quick_run"] else "uneven runs" if d["uneven_load"] else "two noisy p95s"] += 1
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
        d = core.decide(one(base(rng), LOADS[load], CHANGES[change], rng), ceilings, rounds=ROUNDS, quantile=quantile)
        verdicts[d["verdict"]] += 1
        tally(d, why)
    return verdicts, why


def monotonic(base, ceilings, seeds, first=0, quantile=core.NOISE_QUANTILE, groups=None):
    """The verdicts of each rise of each group (by default every hook's
    RISES, and each hook's rises alone) on the same base samples a seed, on
    a quiet machine: the share of seeds each rise failed, and, by group,
    the seeds in which a smaller rise failed and a larger one did not."""
    if groups is None:
        groups = {"every hook": RISES}
        groups.update({name: ["%s-x%g" % (name, x) for x in ALONE] for name in ceilings if name in COUNTS})
    fails, broken = Counter(), {g: [] for g in groups}
    for s in range(first, first+seeds):
        runs = base(seeded(s))
        for g, rises in groups.items():
            verdicts = []
            for change in rises:
                d = core.decide(one(runs, LOADS["none"], CHANGES[change], seeded(s)), ceilings, rounds=ROUNDS, quantile=quantile)
                verdicts.append(d["verdict"])
                fails[change] += d["verdict"] == "fail"
            if any(verdicts[i] == "fail" and verdicts[j] != "fail" for i in range(len(rises)) for j in range(i+1, len(rises))):
                broken[g].append((s, verdicts))
    return fails, broken


def rework(paths, quantile=core.NOISE_QUANTILE):
    for path in paths:
        gate = json.loads(Path(path).read_text())
        d = core.decide(runs_of(gate), ceilings_of(gate), gate.get("scenario_failures"), gate["rounds"], quantile=quantile,
                        sources=gate.get("figure_scenarios"))
        print("%-12s %-10s was %-10s failed: %s  p95 too noisy: %s  on the median alone: %d  unsettled: %s  quick run: %s  "
              "uneven: %s  too few: %d  lost: %d" % (
                  Path(path).stem, d["verdict"], gate.get("verdict"), sorted({f.split(":")[0] for f in d["failures"]}) or "-",
                  d["too_noisy"] or "-", sum(m["judged_on"] == ["median"] for m in d["metrics"].values()),
                  [u.split(":")[0] for u in d["unsettled"]] or "-", d["one_quick_run"] or "-", d["uneven_load"] or "-",
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
    p.add_argument("--rounds", type=int, default=core.ROUNDS, help="the gate's rounds")
    p.add_argument("--common", type=float, default=COMMON,
                   help="the made-up base's common run factor, the standard deviation of its logarithm")
    p.add_argument("--own-scale", type=float, default=OWN_SCALE,
                   help="how many times its own run spread each figure of the made-up base has")
    a = p.parse_args()
    if a.rework:
        rework(a.rework, a.quantile)
        return 0
    set_rounds(a.rounds)
    set_spread(a.common, a.own_scale)
    if a.gates:
        gates = [json.loads(path.read_text()) for path in a.gates]
        base, ceilings = from_gates(gates), ceilings_of(gates[0])
    else:
        base, ceilings = made_up, CEILINGS
    print("rule %d: noise is the %g quantile of %d controls' bounds; threshold %.2f, the median's %.2f; quick run %.2f; "
          "uneven halves %.2f; %d samples a side to judge; %d rounds; %s base; %d seeds a case" % (
              core.RULE, a.quantile, 2*len(core.splits(a.rounds)), core.THRESHOLD, core.MEDIAN_THRESHOLD, core.QUICK, core.UNEVEN,
              core.WHOLE, a.rounds, "saved gates'" if a.gates else "made-up", a.seeds), flush=True)
    if a.monotonic:
        began = time.time()
        fails, broken = monotonic(base, ceilings, a.seeds, a.first, a.quantile)
        print("quiet machine, the same samples a seed: %s (%.0f s)" % (
            ", ".join("%s fails %.1f%%" % (c, 100*fails[c]/a.seeds) for c in RISES), time.time()-began))
        for g, seeds in broken.items():
            print("%-26s a larger rise not failing where a smaller one failed: %d of %d seeds%s" % (
                g, len(seeds), a.seeds, "".join("\n  seed %d: %s" % (s, " ".join(v)) for s, v in seeds[:10])), flush=True)
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
