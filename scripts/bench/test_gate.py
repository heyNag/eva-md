"""Tests of the latency gate's decision, of its plumbing on stub binaries and
made-up scenarios, of how a hook is stopped, and of the scenario list. The
decision's tests feed numbers and read no clock; the hook tests wait for a
process only up to a hang guard: python3 -I scripts/bench/test_gate.py"""
import argparse
import ast
import contextlib
import importlib.util
import io
import json
import math
import os
from pathlib import Path
import random
import re
import signal
import subprocess
import sys
import tempfile
import time
import unittest

HERE = Path(__file__).resolve().parent
sys.dont_write_bytecode = True


def load(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    sys.modules[name] = module
    spec.loader.exec_module(module)
    return module


core = load("evabench", HERE/"core.py")
bench = load("benchmark_under_test", HERE.parent/"benchmark.py")
sim = load("sim_gate_under_test", HERE/"sim_gate.py")


def samples(seed, n, centre, spread=0.1):
    """n made-up figures in milliseconds around centre, spread a share of it."""
    rng = random.Random(seed)
    return [centre*(1+rng.uniform(-spread, spread)) for _ in range(n)]


def decide(samples, ceilings, *args, **kwargs):
    """decide at two rounds: the cases written for the second rule give each
    binary four runs (runs_of), and decide takes any rounds from 1. The
    cases at the default rounds, three, are in ThreeRounds."""
    return core.decide(samples, ceilings, *args, rounds=2, **kwargs)


class NearestRank(unittest.TestCase):
    def test_values(self):
        self.assertEqual(core.nearest_rank(list(range(1, 101))), 95)
        self.assertEqual(core.nearest_rank(list(range(1, 21))), 19)
        self.assertEqual(core.nearest_rank(list(range(1, 11))), 10)
        self.assertEqual(core.nearest_rank([7]), 7)
        self.assertEqual(core.nearest_rank([5, 1, 3]), 5)
        self.assertEqual(core.nearest_rank([4, 1, 3, 2], 0.5), 2)

    def test_the_report_keeps_its_own_rule(self):
        # The report's p95 is the one it always printed, which is not the
        # nearest rank for 10 samples.
        self.assertEqual(core.report_p95(list(range(1, 11))), 9)
        self.assertEqual(core.report_p95(list(range(1, 101))), 95)


class RatioLow(unittest.TestCase):
    def test_seeded_and_order_free(self):
        base, cand = samples(1, 200, 20), samples(2, 200, 25)
        low = core.ratio_low(base, cand)
        self.assertEqual(low, core.ratio_low(list(reversed(base)), sorted(cand)))
        shuffled = cand[:]
        random.Random(3).shuffle(shuffled)
        self.assertEqual(low, core.ratio_low(base, shuffled))
        self.assertNotEqual(low, core.ratio_low(base, cand, seed=core.SEED+1))
        self.assertEqual(core.p95_low(cand), core.p95_low(shuffled))

    def test_bounds_the_ratio_from_below(self):
        base = samples(1, 300, 20)
        # Both sides are resampled, so the bound sits below the ratio of the
        # p95s, and equals it only when no draw can move either.
        low = core.ratio_low(base, [x*1.5 for x in base])
        self.assertTrue(1.3 < low < 1.5, low)
        self.assertEqual(core.ratio_low([20.0]*50, [30.0]*50), 1.5)
        self.assertLess(core.ratio_low(base, samples(2, 300, 20)), 1.15)
        self.assertGreater(core.ratio_low(base, samples(2, 300, 30)), 1.15)
        self.assertTrue(core.p95_low(base) < core.nearest_rank(base) < core.interval(base, base)[1]*core.nearest_rank(base))

    def test_the_interval_is_the_draws_at_both_ends(self):
        base, cand = samples(1, 400, 20), samples(2, 400, 22)
        draws = core.ratios(base, cand)
        self.assertEqual(len(draws), core.DRAWS)
        self.assertEqual(draws, sorted(draws))
        # 2.5% of the draws on each side of the interval, and these samples
        # have no tie at either end.
        self.assertEqual(core.interval(base, cand), (draws[50], draws[core.DRAWS-51]))
        self.assertTrue(draws[49] < draws[50] < draws[51] and draws[core.DRAWS-52] <= draws[core.DRAWS-51] < draws[core.DRAWS-50])

    def test_a_zero_base(self):
        self.assertEqual(core.ratio_low([0.0]*10, [0.0]*10), 1.0)
        self.assertEqual(core.ratio_low([0.0]*10, [1.0]*10), float("inf"))

    def test_a_rank_falls_as_a_resamples_p95_does(self):
        # The p95 of a resample of n values is at or below the j-th smallest
        # exactly when at least R of its n draws are, R = ceil(0.95 n): the
        # ranks drawn directly follow that law.
        draws = 20000
        for n in (1, 3, 10, 40, 100, 400):
            (ranks,) = core.ranks((n,), 7, draws)
            r = max(math.ceil(0.95*n), 1)
            counts = [0]*n
            for i in ranks:
                counts[i] += 1
            below = 0
            for j in range(n):
                below += counts[j]
                p = (j+1)/n
                exact = sum(math.comb(n, k)*p**k*(1-p)**(n-k) for k in range(r, n+1))
                self.assertAlmostEqual(below/draws, exact, delta=0.012, msg=(n, j))

    def test_a_resample_built_gives_the_same_bound_near_enough(self):
        # The bound by resamples built, as the gate first took it, and by
        # ranks drawn: two draws of one law, so they differ by little.
        base, cand = samples(4, 150, 20, 0.3), samples(5, 150, 23, 0.3)
        rng = random.Random(9)
        built = sorted(core.nearest_rank(rng.choices(cand, k=150))/core.nearest_rank(rng.choices(base, k=150))
                       for _ in range(core.DRAWS))[50]
        self.assertAlmostEqual(core.ratio_low(base, cand), built, delta=0.02)


def runs_of(name, levels=None, n=100, centre=20, spread=0.1, seed=0, add=0.0):
    """A figure's made-up samples by role and run. levels maps a binary to
    its four runs' levels, numbered as decide numbers them: the first role's
    two runs, then its again role's. n is a count a run, or a map of a
    binary to its four runs' counts; add is added to the new binary's
    samples."""
    out, k = {}, seed
    for binary, roles in core.ROLES_OF.items():
        lv = (levels or {}).get(binary, (1, 1, 1, 1))
        counts = n.get(binary, (100,)*4) if isinstance(n, dict) else (n,)*4
        for i, role in enumerate(roles):
            out[role] = []
            for level, count in zip(lv[2*i:2*i+2], counts[2*i:2*i+2]):
                values = [x*level + (add if binary == "new" else 0) for x in samples(k, count, centre, spread)]
                out[role].append({name: values})
                k += 1
    return out


def merge(*parts):
    """The samples of several figures, run by run."""
    out = {}
    for part in parts:
        for role, runs in part.items():
            mine = out.setdefault(role, [{} for _ in runs])
            for run, more in zip(mine, runs):
                run.update(more)
    return out


# Where each of the gate's runs falls among a binary's runs, as decide
# numbers them, in the order the gate makes them at two rounds.
PLACES = []
for _role in [role for _, role in core.schedule(2)]:
    _binary = core.BINARY[_role]
    _first = _role == core.ROLES_OF[_binary][0]
    _seen = sum(1 for b, i in PLACES if b == _binary and (i < 2) == _first)
    PLACES.append((_binary, (0 if _first else 2) + _seen))


def loaded(runs, factor):
    """Levels with the given runs of the gate, counted from 1 in the order
    it makes them, slowed by factor."""
    levels = {"old": [1.0]*4, "new": [1.0]*4}
    for k in runs:
        binary, i = PLACES[k-1]
        levels[binary][i] = factor
    return levels


class Decide(unittest.TestCase):
    def test_the_places_of_the_runs(self):
        self.assertEqual(PLACES, [("old", 0), ("new", 0), ("old", 2), ("new", 2), ("new", 3), ("old", 3), ("new", 1), ("old", 1)])

    def test_unchanged_passes(self):
        d = decide(runs_of("capture"), {"capture": 100})
        self.assertEqual((d["verdict"], d["exit"], d["failures"], d["unsettled"], d["too_few_samples"]), ("pass", 0, [], [], []))
        m = d["metrics"]["capture"]
        self.assertEqual((m["n_old"], m["n_new"], m["ceiling_ms"], m["controls"], m["judged"]), (400, 400, 100, 36, True))
        self.assertEqual(m["n_runs"], {role: [100, 100] for role in core.ROLES})
        self.assertLessEqual(m["noise"], 1.15)

    def test_a_20_ms_delay_fails(self):
        d = decide(runs_of("capture", add=20), {"capture": 100})
        self.assertEqual((d["verdict"], d["exit"]), ("fail", 1))
        # By its p95 and by its median.
        self.assertEqual(len(d["failures"]), 2)
        self.assertIn("capture: p95 ratio's lower bound", d["failures"][0])
        self.assertIn("capture: median ratio's lower bound", d["failures"][1])
        # The change cancels out of the controls: each side holds as many
        # runs of each binary.
        self.assertLess(d["metrics"]["capture"]["noise"], 1.05)

    def test_load_on_runs_2_and_4_does_not_fail(self):
        # Runs 2 and 4 are a run of new and one of new_again: each control
        # of whole roles has one on each side and cannot see the load, but
        # a split that puts both on one side can, as far as the gate's
        # comparison rises. So can one for any run of new with any of
        # new_again.
        for runs in ((2, 4), (2, 5), (7, 4), (7, 5)):
            for factor in (1.25, 1.5, 2.0):
                parts = runs_of("capture", loaded(runs, factor))
                d = decide(parts, {"capture": 100})
                m = d["metrics"]["capture"]
                self.assertGreater(m["ratio_low"], 1.15, (runs, factor))
                self.assertNotEqual(d["verdict"], "fail", (runs, factor, m))
                # Half the new binary's runs loaded: its quickest is more
                # than QUICK times as quick as the median of its others.
                self.assertEqual((d["verdict"], d["too_noisy"], d["one_quick_run"]), ("not judged", ["capture"], ["new"]), (runs, factor))
                if runs == (2, 4):
                    pool = lambda roles: [v for role in roles for run in parts[role] for v in run["capture"]]
                    for a, b in ((("old", "new"), ("old_again", "new_again")), (("old", "new_again"), ("old_again", "new"))):
                        low, high = core.interval(pool(a), pool(b))
                        self.assertTrue(low < 1.15 and 1/high < 1.15, (factor, a, low, 1/high))

    def test_no_stretch_of_loaded_runs_fails(self):
        # Load over any stretch of the gate's runs, in the order it makes
        # them, on an unchanged binary.
        for i in range(1, 9):
            for j in range(i, 9):
                d = decide(runs_of("capture", loaded(range(i, j+1), 1.6)), {"capture": 100})
                self.assertNotEqual(d["verdict"], "fail", (i, j))

    def test_slower_old_runs_or_quicker_runs_fail_nothing(self):
        for k in range(1, 9):
            for factor in (2, 0.5):
                if factor == 2 and PLACES[k-1][0] == "new":
                    continue
                d = decide(runs_of("capture", loaded([k], factor)), {"capture": 100})
                self.assertNotEqual(d["verdict"], "fail", (k, factor))
                self.assertEqual(d["failures"], [])

    def test_a_30_percent_rise_with_a_little_noise_fails_though_another_figure_is_unsettled(self):
        # Capture rose 30%, and its new runs differ by 4%, which the
        # controls show; prompt rose 25%, but half its new runs only 12%,
        # so the controls put 1.116 between them: it is unsettled, and
        # stops nothing.
        capture = runs_of("capture", {"new": (1.3*1.04, 1.3*1.04, 1.3, 1.3)}, spread=0.02)
        prompt = runs_of("prompt", {"new": (1.25, 1.25, 1.12, 1.12)}, spread=0.02, seed=50)
        d = decide(merge(capture, prompt), {"capture": 100, "prompt": 100})
        c, p = d["metrics"]["capture"], d["metrics"]["prompt"]
        self.assertTrue(1.02 < c["noise"] < 1.06, c["noise"])
        self.assertTrue(1.05 < p["noise"] <= 1.15 and 1.15 < p["ratio_low"] <= 1.15*p["noise"], p)
        self.assertEqual(p["reasons"], [])
        self.assertTrue(p["unsettled"])
        self.assertEqual((d["machine_too_noisy"], d["too_noisy"]), (False, []))
        self.assertEqual((d["verdict"], d["exit"]), ("fail", 1))
        self.assertEqual(sorted({f.split(":")[0] for f in d["failures"]}), ["capture"])
        # Alone, the unsettled figure leaves the gate not judged. (Alone,
        # its quickest runs are 1.116 times as quick as the median of its
        # others, past QUICK, so a gap above that isolates the figure.)
        d = decide(prompt, {"prompt": 100}, quick=1.2)
        self.assertEqual((d["verdict"], d["exit"], d["failures"], d["machine_too_noisy"]), ("not judged", 0, [], False))
        self.assertEqual([u.split(":")[0] for u in d["unsettled"]], ["prompt"])
        self.assertEqual(decide(prompt, {"prompt": 100})["one_quick_run"], ["new"])

    def test_one_figure_too_noisy_stops_nothing_and_two_stop_the_verdict(self):
        # Load slowed one run of each binary's startup: the gate's
        # comparison is untouched, but some controls put the two runs on
        # one side, so startup's p95 is too noisy to judge. It stops
        # nothing: capture's rise of 20 ms fails the gate. Its median is
        # steady, and judges it: it passes.
        capture = runs_of("capture", add=20)
        startup = runs_of("startup", {"old": (1, 1, 1, 1.5), "new": (1.5, 1, 1, 1)}, n=34, centre=100, seed=50)
        d = decide(merge(capture, startup), {"capture": 100, "startup": 600})
        self.assertGreater(d["metrics"]["startup"]["noise"], 1.15)
        self.assertEqual((d["verdict"], d["exit"], d["machine_too_noisy"], d["too_noisy"]), ("fail", 1, False, ["startup"]))
        self.assertEqual(sorted({f.split(":")[0] for f in d["failures"]}), ["capture"])
        self.assertEqual((d["metrics"]["startup"]["judged_on"], d["unsettled"]), (["median"], []))
        # Alone, it passes on its median, where the second rule left the
        # gate not judged.
        d = decide(merge(runs_of("capture"), startup), {"capture": 100, "startup": 600})
        self.assertEqual((d["verdict"], d["exit"], d["machine_too_noisy"]), ("pass", 0, False))
        # Two figures whose p95 is too noisy to judge say the machine was
        # too noisy, and stop the verdict, capture's rise and all.
        prompt = runs_of("prompt", {"old": (1.5, 1, 1, 1), "new": (1, 1, 1.5, 1)}, centre=30, seed=90)
        d = decide(merge(capture, startup, prompt), {"capture": 100, "startup": 600, "prompt": 100})
        self.assertEqual((d["verdict"], d["exit"], d["machine_too_noisy"], d["too_noisy"]), ("not judged", 0, True, ["prompt", "startup"]))
        self.assertEqual(sorted({f.split(":")[0] for f in d["failures"]}), ["capture"])
        self.assertEqual(core.NOISY_FIGURES, 2)

    def test_a_figure_with_too_few_samples_neither_fails_nor_stops(self):
        # 99 samples on the new binary's side: startup doubled, and its
        # runs differ by half, but it is only listed.
        few = {"old": (25,)*4, "new": (25, 25, 25, 24)}  # no scenario failed
        startup = runs_of("startup", {"old": (1, 1, 1, 1.5), "new": (2, 3, 2, 2)}, n=few, centre=100, seed=50)
        for capture, verdict in ((runs_of("capture"), "pass"), (runs_of("capture", add=20), "fail")):
            d = decide(merge(capture, startup), {"capture": 100, "startup": 120})
            m = d["metrics"]["startup"]
            self.assertEqual((d["verdict"], d["machine_too_noisy"], d["too_few_samples"]), (verdict, False, ["startup"]))
            self.assertEqual((m["n_new"], m["too_few_samples"], m["judged"], m["reasons"], m["unsettled"]), (99, True, False, [], []))
            self.assertGreater(m["noise"], 1.15)
            self.assertGreater(m["p95_low_ms"], 120)
            self.assertFalse(any(f.startswith("startup") for f in d["failures"]))
        # With 100 it is judged, and fails.
        startup = runs_of("startup", {"new": (2, 2, 2, 2)}, n=25, centre=100, seed=50)
        d = decide(startup, {"startup": 600})
        self.assertEqual((d["verdict"], d["too_few_samples"]), ("fail", []))

    def test_the_noise_is_the_fourth_largest_bound_and_at_least_1(self):
        cases = ((runs_of("capture", {"old": (1, 1, 1, 1.3), "new": (1.3, 1.05, 1, 1)}), 1.15),
                 (runs_of("capture", {"new": (1.195,)*4}, n=25, spread=0.3), 1.0))
        for parts, at_least in cases:
            old = [run["capture"] for role in core.ROLES_OF["old"] for run in parts[role]]
            new = [run["capture"] for role in core.ROLES_OF["new"] for run in parts[role]]
            bounds = []
            for a_old, a_new in core.splits(2):
                a = [v for i in a_old for v in old[i]] + [v for i in a_new for v in new[i]]
                b = [v for i in range(4) if i not in a_old for v in old[i]] + [v for i in range(4) if i not in a_new for v in new[i]]
                low, high = core.interval(a, b)
                bounds += [low, 1/high]
            bounds.sort()
            m = decide(parts, {"capture": 100})["metrics"]["capture"]
            self.assertEqual(m["noise"], round(max(1.0, bounds[-4]), 3))
            if at_least == 1.0:
                self.assertLess(bounds[-4], 1.0)
                # A rise whose bound is above 1.15 times the
                # fourth largest bound, but not above 1.15.
                self.assertTrue(1.15*bounds[-4] < m["ratio_low"] <= 1.15, (bounds[-4], m["ratio_low"]))
                self.assertEqual(m["reasons"], [])
            else:
                # The largest bound is not the noise.
                self.assertGreater(bounds[-4], at_least)
                self.assertGreater(bounds[-1], bounds[-4]+0.01, bounds[-6:])

    def test_the_noise_is_the_fourth_largest_of_untied_bounds(self):
        # Bounds whose third, fourth and fifth largest all differ.
        parts = runs_of("capture", {"old": (1, 1.06, 1.02, 1.2), "new": (1.16, 1.09, 1.03, 1)}, spread=0.3, seed=16)
        old = [run["capture"] for role in core.ROLES_OF["old"] for run in parts[role]]
        new = [run["capture"] for role in core.ROLES_OF["new"] for run in parts[role]]
        bounds = sorted(core.control_bounds(old, new, core.splits(2)))
        self.assertEqual(len(bounds), 36)
        self.assertTrue(bounds[-5] < bounds[-4] < bounds[-3] and bounds[-4] > 1, bounds[-6:])
        self.assertEqual(decide(parts, {"capture": 100})["metrics"]["capture"]["noise"], round(bounds[-4], 3))
        self.assertEqual(core.NOISE_QUANTILE, 0.89)

    def test_a_noise_of_1_15_is_not_too_noisy(self):
        # One run of each binary is 1.15 times as slow as the rest, all
        # samples alike: the controls that put both on one side say 1.15,
        # which is not above it.
        def parts(slow):
            out = {}
            for binary, levels in (("old", (10, 10, 10, slow)), ("new", (slow, 10, 10, 10))):
                for i, role in enumerate(core.ROLES_OF[binary]):
                    out[role] = [{"capture": [float(v)]*100} for v in levels[2*i:2*i+2]]
            return out
        d = decide(parts(11.5), {"capture": 100})
        self.assertEqual((d["metrics"]["capture"]["noise"], d["verdict"], d["too_noisy"]), (1.15, "pass", []))
        d = decide(parts(11.6), {"capture": 100})
        self.assertEqual((d["too_noisy"], d["metrics"]["capture"]["noise"]), (["capture"], 1.16))
        # Its median, half of each side's samples at 10, does not move: the
        # figure is judged on it, and passes.
        self.assertEqual((d["verdict"], d["metrics"]["capture"]["judged_on"]), ("pass", ["median"]))

    def test_the_threshold_is_strict(self):
        old = {"old": [{"capture": [10.0]*100}]*2, "old_again": [{"capture": [10.0]*100}]*2}
        for new, verdict in ((11.5, "pass"), (11.6, "fail")):
            parts = dict(old, new=[{"capture": [new]*100}]*2, new_again=[{"capture": [new]*100}]*2)
            self.assertEqual(decide(parts, {"capture": 100})["verdict"], verdict, new)

    def constant(self, old, new, name="capture"):
        """Each binary's runs, the samples of each run all one value."""
        out = {}
        for binary, values in (("old", old), ("new", new)):
            for i, role in enumerate(core.ROLES_OF[binary]):
                out[role] = [{name: [float(v)]*100} for v in values[2*i:2*i+2]]
        return out

    def test_passing_the_ceiling_fails(self):
        # A rise too small for the ratio, past the ceiling.
        d = decide(self.constant([95]*4, [102]*4), {"capture": 100})
        self.assertEqual((d["verdict"], d["exit"]), ("fail", 1))
        self.assertEqual(d["failures"], ["capture: p95 102.0 ms, at least 102.0 ms, passed the ceiling of 100 ms"])
        self.assertEqual(decide(self.constant([95]*4, [102]*4), {"capture": None})["verdict"], "pass")
        self.assertEqual(decide(self.constant([95]*4, [100]*4), {"capture": 100})["verdict"], "pass")

    def test_a_figure_at_its_ceiling_by_sampling_alone_does_not_fail(self):
        # Both binaries' samples from one law whose p95 is the ceiling: the
        # new binary's p95 lands above it, and the old one's below, in about
        # a quarter of the seeds, but its interval reaches below it.
        over = 0
        for seed in range(0, 240, 8):
            parts = runs_of("capture", centre=100/1.045, spread=0.05, seed=seed)
            d = decide(parts, {"capture": 100})
            m = d["metrics"]["capture"]
            if m["p95_ms"] > 100 >= m["p95_base_ms"]:
                over += 1
                self.assertLess(m["p95_low_ms"], 100)
            self.assertEqual(d["verdict"], "pass", (seed, m))
        self.assertGreaterEqual(over, 3)

    def test_a_ceiling_the_old_binary_was_over_already(self):
        d = decide(self.constant([105]*4, [110]*4), {"capture": 100})
        self.assertEqual((d["verdict"], d["failures"]), ("pass", []))

    def test_the_controls_have_no_ceiling(self):
        # Runs of the new binary on either side of the ceiling show where
        # the binary sits, not that the machine is noisy: the gate fails.
        d = decide(self.constant([95]*4, [99, 101, 99, 101]), {"capture": 100})
        self.assertEqual((d["verdict"], d["exit"], d["unsettled"]), ("fail", 1, []))
        self.assertEqual(d["failures"], ["capture: p95 101.0 ms, at least 101.0 ms, passed the ceiling of 100 ms"])

    def test_a_large_rise_fails(self):
        # Capture from about 20 ms to about 90, and prompt doubled: both
        # fail, whatever seed made the samples.
        for seed in range(0, 160, 16):
            capture = runs_of("capture", {"new": (4.5,)*4}, seed=seed)
            prompt = runs_of("prompt", {"new": (2,)*4}, centre=10, seed=seed+8)
            d = decide(merge(capture, prompt), {"capture": 100, "prompt": 100})
            self.assertEqual((d["verdict"], d["exit"], d["unsettled"]), ("fail", 1, []), seed)
            self.assertEqual(sorted({f.split(":")[0] for f in d["failures"]}), ["capture", "prompt"])

    def test_a_figure_the_old_binary_gave_no_samples_is_not_compared(self):
        parts = runs_of("capture")
        for run in parts["new"] + parts["new_again"]:
            run["code_brief"] = [5.0]
        d = decide(parts, {"capture": 100, "code_brief": 600})
        self.assertEqual((d["verdict"], d["not_compared"], list(d["metrics"])), ("pass", ["code_brief"], ["capture"]))

    def test_a_figure_the_new_binary_gave_no_samples_fails(self):
        parts = runs_of("capture")
        for run in parts["old"] + parts["old_again"]:
            run["code_brief"] = [5.0]*10
        d = decide(parts, {"capture": 100, "code_brief": 600})
        self.assertEqual((d["verdict"], d["exit"]), ("fail", 1))
        self.assertEqual(d["failures"], ["code_brief: the new binary gave no samples, the old one 40"])
        # Even when the machine was too noisy to judge a ratio.
        noisy = merge(parts, runs_of("startup", {"old": (1, 1, 1, 1.5), "new": (1.5, 1, 1, 1)}, n=25, centre=100, seed=50),
                      runs_of("prompt", {"old": (1.5, 1, 1, 1), "new": (1, 1, 1.5, 1)}, centre=30, seed=90))
        d = decide(noisy, {"capture": 100, "code_brief": 600, "startup": 600, "prompt": 100})
        self.assertTrue(d["machine_too_noisy"])
        self.assertEqual((d["verdict"], d["exit"]), ("fail", 1))
        # Unless the new binary lost a scenario in every run: the scenarios'
        # rule judges that, and the figure has too few samples.
        lost = {role: ({"code": ["RuntimeError: a brief did not say it"]*2} if core.BINARY[role] == "new" else {})
                for role in core.ROLES}
        d = decide(parts, {"capture": 100, "code_brief": 600}, lost)
        self.assertEqual(d["too_few_samples"], ["code_brief"])
        self.assertEqual(d["failures"], ["the new binary failed the scenario code in 4 of 4 runs, the old one in 0"])
        # A scenario lost in some runs alone explains nothing.
        lost = {role: ({"code": ["RuntimeError: a brief did not say it"]} if role == "new" else {}) for role in core.ROLES}
        d = decide(parts, {"capture": 100, "code_brief": 600}, lost)
        self.assertEqual((d["verdict"], d["too_few_samples"]), ("fail", []))
        self.assertEqual(d["failures"], ["code_brief: the new binary gave no samples, the old one 40"])

    def test_the_same_samples_give_the_same_verdict(self):
        parts = merge(runs_of("capture", {"new": (1.1, 1.2, 1.1, 1.2)}), runs_of("startup", n=25, centre=100, seed=40))
        shuffled = json.loads(json.dumps(parts))
        for runs in shuffled.values():
            for run in runs:
                for values in run.values():
                    random.Random(len(values)).shuffle(values)
        self.assertEqual(decide(parts, {"capture": 100, "startup": 600}),
                         decide(shuffled, {"capture": 100, "startup": 600}))

    def test_a_role_must_have_a_run_a_round(self):
        parts = runs_of("capture")
        parts["new"] = parts["new"][:1]
        with self.assertRaisesRegex(ValueError, "the role new has 1 runs, not 2"):
            decide(parts, {"capture": 100})


def runs3(name, levels=None, n=100, centre=20, spread=0.1, seed=0, add=0.0):
    """A figure's made-up samples by role and run at three rounds. levels
    maps a binary to its six runs' levels, numbered as decide numbers them:
    its first role's three runs, then its again role's, so that round r's
    runs of a binary are r and 3+r."""
    out, k = {}, seed
    for binary, roles in core.ROLES_OF.items():
        lv = (levels or {}).get(binary, (1,)*6)
        for i, role in enumerate(roles):
            out[role] = []
            for level in lv[3*i:3*i+3]:
                out[role].append({name: [x*level + (add if binary == "new" else 0) for x in samples(k, n, centre, spread)]})
                k += 1
    return out


def figures3(levels=None, add=None, only=None, seed=0):
    """Eight made-up figures at three rounds, each with the counts the gate
    gives it (GATE_RUN_SAMPLES a run for a chat start, 100 for the rest):
    levels (a binary's six runs, as runs3 numbers them) on every figure, or
    only on the figure only."""
    parts = []
    for k, (name, n, centre) in enumerate((("capture", 100, 17), ("prompt", 100, 22), ("startup", core.GATE_RUN_SAMPLES, 63),
                                            ("session_end", core.GATE_RUN_SAMPLES, 19), ("code_brief", core.GATE_RUN_SAMPLES, 48),
                                            ("recall", 100, 28), ("tool", 50, 14), ("process", 100, 10))):
        lv = levels if only in (None, name) else None
        parts.append(runs3(name, lv, n=n, centre=centre, spread=0.15, seed=seed+20*k))
    return merge(*parts)


CEILINGS3 = {"capture": 100, "prompt": 100, "startup": 600, "session_end": 400, "code_brief": 600, "recall": 100, "tool": 100,
             "process": None}


class ThreeRounds(unittest.TestCase):
    """The rule at the default rounds, three: each binary runs six times."""

    def test_the_default_is_three_rounds(self):
        self.assertEqual((core.ROUNDS, core.RULE), (3, 3))
        d = core.decide(runs3("capture"), {"capture": 100})
        self.assertEqual((d["rounds"], d["rule"], d["verdict"]), (3, 3, "pass"))
        self.assertEqual(d["metrics"]["capture"]["n_runs"], {role: [100]*3 for role in core.ROLES})
        with self.assertRaisesRegex(ValueError, "the role old has 2 runs, not 3"):
            core.decide(runs_of("capture"), {"capture": 100})

    def test_rounds_as_parsed_default_to_the_gates_own(self):
        a = bench.arguments(["--compare", "a", "b", "--cwd", "."], core.ROUNDS)
        self.assertEqual(a.rounds, 3)
        self.assertEqual(bench.arguments(["--compare", "a", "b", "--cwd", ".", "--rounds", "2"], core.ROUNDS).rounds, 2)
        # main takes the default from the core it loads, not a number of
        # its own: its help names it.
        out = io.StringIO()
        saved = sys.modules.get("evabench")
        try:
            with contextlib.redirect_stdout(out), self.assertRaises(SystemExit):
                bench.main(["--help"])
        finally:
            sys.modules["evabench"] = saved
            for name in [n for n in sys.modules if n.startswith("evabench_")]:
                del sys.modules[name]
        self.assertIn("each of which runs each binary twice (default: %d)" % core.ROUNDS, " ".join(out.getvalue().split()))

    def test_load_on_one_round_of_the_new_binary_does_not_fail(self):
        # Every figure 1.3 times slower in both new runs of one round, as
        # load on that round's two runs of the new binary would make it:
        # neither fails nor passes as a change, whichever round.
        for r in range(3):
            levels = [1.0]*6
            levels[r] = levels[3+r] = 1.3
            d = core.decide(figures3({"new": levels}), CEILINGS3)
            self.assertNotEqual(d["verdict"], "fail", (r, d["failures"]))
        # In every one of its runs, it is a change, and fails.
        d = core.decide(figures3({"new": (1.3,)*6}), CEILINGS3)
        self.assertEqual(d["verdict"], "fail")
        self.assertEqual(sorted({f.split(":")[0] for f in d["failures"]}), sorted(CEILINGS3))

    def test_load_on_three_runs_of_the_new_binary_does_not_fail(self):
        # Load of 1.2 on both new runs of one round and one of another:
        # half the new binary's runs.
        for r, other in ((0, 4), (1, 0), (2, 1), (2, 5)):
            levels = [1.0]*6
            levels[r] = levels[3+r] = levels[other] = 1.2
            d = core.decide(figures3({"new": levels}), CEILINGS3)
            self.assertNotEqual(d["verdict"], "fail", (r, other, d["failures"]))

    def test_one_figure_alone_30_percent_slower_fails(self):
        # Every other figure level: each figure alone fails, judged on its
        # median, whatever its count of samples.
        for name in CEILINGS3:
            d = core.decide(figures3({"new": (1.3,)*6}, only=name), CEILINGS3)
            self.assertEqual((d["verdict"], sorted({f.split(":")[0] for f in d["failures"]})), ("fail", [name]), name)
            self.assertTrue(any(f.startswith(name+": median") for f in d["failures"]), d["failures"])
            self.assertEqual(d["metrics"][name]["judged_on"], ["p95", "median"])

    def test_the_median_judges_a_figure_whose_p95_is_too_noisy(self):
        # capture's p95 differs between runs (one run of each binary has a
        # long tail: 40 of its 100 samples, more than the 5% of the 600 a
        # side holds), so it is too noisy to judge; its median is steady,
        # so it is judged on that: unchanged it passes, where the second
        # rule left the gate not judged, and with every sample 30% slower
        # it fails.
        def capture(rise):
            out = runs3("capture", {"new": (rise,)*6}, spread=0.05)
            for role, k in (("old", 1), ("new_again", 2)):
                run = out[role][k]["capture"]
                run[:40] = [v*2.5 for v in run[:40]]
            return out
        for rise, verdict in ((1.0, "pass"), (1.3, "fail")):
            d = core.decide(capture(rise), {"capture": 100})
            m = d["metrics"]["capture"]
            self.assertGreater(m["noise"], core.THRESHOLD)
            self.assertLessEqual(m["median_noise"], core.THRESHOLD)
            self.assertEqual((d["verdict"], m["judged_on"], d["too_noisy"], d["machine_too_noisy"]),
                             (verdict, ["median"], ["capture"], False), rise)
        # Two figures whose p95 is too noisy still call the machine too
        # noisy, though their medians could be judged.
        two = merge(capture(1.0), {role: [{"prompt": run["capture"]} for run in runs] for role, runs in capture(1.0).items()})
        d = core.decide(two, {"capture": 100, "prompt": 100})
        self.assertEqual((d["verdict"], d["too_noisy"], d["machine_too_noisy"]), ("not judged", ["capture", "prompt"], True))

    def test_a_figure_too_noisy_on_both_is_not_judged(self):
        # Three runs of each binary 1.6 times as slow, in every sample: the
        # gate's own comparison does not see it. Every split but the role
        # split has a slow run on each side, so the p95's controls do not
        # see it either, while the median's do: it is judged on its p95.
        s = runs3("capture", {"old": (1, 1, 1, 1.6, 1.6, 1.6), "new": (1.6, 1.6, 1.6, 1, 1, 1)})
        m = core.decide(s, {"capture": 100})["metrics"]["capture"]
        self.assertEqual(m["judged_on"], ["p95"])
        self.assertGreater(m["median_noise"], core.THRESHOLD)
        # With the noise the largest bound, the role split's, both are too
        # noisy: the figure is not judged, and fails nothing.
        d = core.decide(s, {"capture": 100}, quantile=1)
        m = d["metrics"]["capture"]
        self.assertGreater(min(m["noise"], m["median_noise"]), core.THRESHOLD)
        self.assertEqual((d["verdict"], m["judged_on"], m["judged"], d["failures"], d["too_noisy"]), ("not judged", [], False, [], ["capture"]))
        self.assertIn("too noisy to judge", m["unsettled"][0])

    def test_the_median_has_a_bound_of_its_own(self):
        # Every sample one value: the controls say 1, and the median's
        # ratio is the rise itself.
        def constant(rise):
            return {role: [{"capture": [10.0*(rise if core.BINARY[role] == "new" else 1)]*100} for _ in range(3)] for role in core.ROLES}
        d = core.decide(constant(core.MEDIAN_THRESHOLD+0.001), {"capture": None})
        self.assertEqual(d["verdict"], "fail")
        self.assertTrue(any(f.startswith("capture: median ratio's lower bound") for f in d["failures"]), d["failures"])
        d = core.decide(constant(core.MEDIAN_THRESHOLD), {"capture": None})
        self.assertEqual((d["verdict"], d["failures"]), ("pass", []))
        # Its own: a higher one, given, lets the same rise pass the median.
        d = core.decide(constant(core.MEDIAN_THRESHOLD+0.001), {"capture": None}, median_threshold=core.MEDIAN_THRESHOLD+0.01)
        self.assertFalse(any("median" in f for f in d["failures"]), d["failures"])
        self.assertEqual(d["median_threshold"], core.MEDIAN_THRESHOLD+0.01)

    def test_the_record_keeps_the_bounds_the_verdict_read(self):
        d = core.decide(figures3({"new": (1.3,)*6}, only="capture"), CEILINGS3)
        m = d["metrics"]["capture"]
        for key in ("ratio_low", "noise", "median_ratio_low", "median_noise", "median_base_ms", "median_ms", "judged_on"):
            self.assertIn(key, m)
        self.assertGreater(m["median_ratio_low"], core.MEDIAN_THRESHOLD*m["median_noise"])
        self.assertTrue(1.2 < m["median_ms"]/m["median_base_ms"] < 1.4, m)
        self.assertEqual(d["rule"], 3)

    def test_an_earlier_rules_gate_is_worked_out_by_this_rule(self):
        # A gate.json the second rule wrote has no "rule", and its quick
        # gap meant the next run: redecide takes this rule's parameters.
        s = runs_of("capture", {"new": (1.08, 1.2, 1.2, 1.2)}, spread=0.02)
        old = {"samples": s, "ceilings": {"capture": None}, "scenario_failures": {}, "rounds": 2,
               "decision": {"verdict": "not judged", "threshold": 1.15, "seed": core.SEED, "draws": 40, "whole": 300,
                            "noise_quantile": 0.5, "noisy_figures": 9, "quick": 1.5}}
        self.assertEqual(core.redecide(old), core.decide(s, {"capture": None}, {}, 2))
        # One this rule wrote is worked out by what it recorded.
        d = core.decide(s, {"capture": None}, {}, 2, quick=1.5, median_threshold=1.05)
        again = core.redecide(dict(old, decision=d))
        self.assertEqual(again, d)
        self.assertEqual((again["verdict"], core.decide(s, {"capture": None}, {}, 2)["verdict"]), ("fail", "not judged"))


class Controls(unittest.TestCase):
    """The controls with a change in the new binary, and the runs' levels."""

    def same_runs(self, new_levels, name="capture", seed=4, spread=0.3):
        """Each run's samples one list, times the run's level: the old
        binary's at 1, the new one's at new_levels."""
        rng = random.Random(seed)
        s = [20*(1+rng.uniform(-spread, spread)) for _ in range(100)]
        out = {}
        for binary, levels in (("old", (1, 1, 1, 1)), ("new", new_levels)):
            for i, role in enumerate(core.ROLES_OF[binary]):
                out[role] = [{name: [v*level for v in s]} for level in levels[2*i:2*i+2]]
        return out

    def test_a_large_change_is_brought_to_the_threshold_for_the_controls(self):
        # One run of the new binary 20% slower than its others. Three times
        # as slow, the controls divide the new binary's samples by 3/1.15
        # first, and say what they say of a change of 15%: the gate fails.
        # Without that, each side's p95 would come from its runs of the new
        # binary alone.
        at = self.same_runs((1.2*1.15, 1.15, 1.15, 1.15))
        tripled = self.same_runs((3.6, 3, 3, 3))
        d1, d3 = decide(at, {"capture": None}), decide(tripled, {"capture": None})
        m1, m3 = d1["metrics"]["capture"], d3["metrics"]["capture"]
        self.assertEqual(m1["aligned"], 1.0)
        self.assertAlmostEqual(m3["aligned"], round(3/1.15, 3))
        self.assertEqual(m1["noise"], m3["noise"])
        self.assertTrue(1.05 < m3["noise"] <= 1.15, m3["noise"])
        self.assertEqual(d3["verdict"], "fail")

    def test_the_controls_align_only_past_the_threshold(self):
        o = [[10.0]*10, [11.0]*10, [10.0]*10, [12.0]*10]
        # Lower medians 10 and 11.5: 1.15, not past it.
        self.assertEqual(core.aligned(o, [[11.5]*10, [12.0]*10, [11.0]*10, [20.0]*10]), 1.0)
        # Past it, brought to it.
        self.assertAlmostEqual(core.aligned(o, [[11.6]*10, [12.0]*10, [11.0]*10, [20.0]*10]), 1.16/1.15)
        self.assertAlmostEqual(core.aligned(o, [[30.0]*10, [32.0]*10, [31.0]*10, [40.0]*10]), 3.1/1.15)
        # Quicker by as much: brought up to it.
        self.assertAlmostEqual(core.aligned(o, [[8.6]*10, [9.0]*10, [8.6]*10, [9.5]*10]), 0.86*1.15)
        self.assertEqual(core.aligned(o, [[8.7]*10, [9.0]*10, [8.8]*10, [9.5]*10]), 1.0)
        # Load on half of the new binary's runs leaves its lower median on
        # a run it missed, so the controls see the load as it is.
        self.assertEqual(core.aligned(o, [[10.0]*10, [16.0]*10, [11.0]*10, [16.0]*10]), 1.0)
        # A binary that lost runs: the lower median of the rest.
        self.assertAlmostEqual(core.aligned(o, [[], [16.0]*10, [12.0]*10, []]), 1.2/1.15)
        self.assertEqual(core.aligned(o, [[], [], [], []]), 1.0)
        # A run's typical value is its median, not its tail: medians 12
        # against 10, though the old runs' tails are longer.
        tailed = [[10.0]*9+[50.0] for _ in range(4)]
        self.assertAlmostEqual(core.aligned(tailed, [[12.0]*9+[13.0] for _ in range(4)]), 1.2/1.15)

    def test_the_runs_levels_and_one_quick_run(self):
        # Two figures; the new binary's run 2 is quick in both, by 1.25.
        a = [[10.0]*5, [12.5]*5, [12.5]*5, [12.5]*5]
        b = [[40.0]*5, [50.0]*5, [51.0]*5, [49.0]*5]
        self.assertEqual(core.run_levels([a, b]), [0.8, 1.0, 1.0, 1.0])
        self.assertTrue(core.one_quick_run(core.run_levels([a, b])))
        # In one figure only, the median over figures does not see it.
        c = [[30.0]*5, [30.0]*5, [31.0]*5, [29.0]*5]
        self.assertFalse(core.one_quick_run(core.run_levels([a, c, c])))
        # The gap that calls a run quick is QUICK, 1.10, below the
        # threshold, between the quickest run and the lower median of the
        # others, and 1.10 is not more than 1.10.
        self.assertEqual((core.QUICK, core.THRESHOLD), (1.10, 1.15))
        self.assertFalse(core.one_quick_run([1.0, 1.0, 1.1, 1.1]))
        self.assertTrue(core.one_quick_run([1.0, 1.0, 1.101, 1.2]))
        self.assertFalse(core.one_quick_run([1.0, 1.2, 1.0, 1.0]))
        # Not the next run: one of the others quick by chance does not
        # hide the load on the rest.
        self.assertTrue(core.one_quick_run([1.0, 1.06, 1.2, 1.2]))
        self.assertTrue(core.one_quick_run([1.0, 1.0, 1.0, 1.12, 1.12, 1.12]))
        self.assertFalse(core.one_quick_run([1.0, 1.0, 1.0, 1.0, 1.12, 1.12]))
        self.assertFalse(core.one_quick_run([1.0, 1.2, 1.2, 1.2], 1.2))
        self.assertTrue(core.one_quick_run([1.0, 1.2, 1.2, 1.2], 1.19))
        self.assertFalse(core.one_quick_run([1.0, 2.0]))
        # A figure a run of which gave nothing counts for no run.
        self.assertEqual(core.run_levels([[[10.0], [], [10.0], [10.0]]]), None)

    def test_load_on_three_runs_of_the_new_binary_is_not_judged(self):
        # Three of the new binary's runs loaded by 1.4, and none of the old
        # one's: no control sees it, but its one quick run, in every
        # figure, says the machine was too noisy. A rise of 40% in all four
        # runs fails.
        for levels, verdict, quick in (((1, 1.4, 1.4, 1.4), "not judged", ["new"]), ((1.4,)*4, "fail", [])):
            parts = merge(runs_of("capture", {"new": levels}, spread=0.05),
                          runs_of("prompt", {"new": levels}, spread=0.05, centre=30, seed=40))
            d = decide(parts, {"capture": 100, "prompt": 100})
            self.assertEqual((d["verdict"], d["one_quick_run"], d["machine_too_noisy"]), (verdict, quick, verdict == "not judged"), levels)
            self.assertEqual(d["too_noisy"], [])
            self.assertEqual(sorted({f.split(":")[0] for f in d["failures"]}), ["capture", "prompt"])
        # Figures with too few samples to judge count for no run: two of
        # them with a quick run of the new binary leave the gate a pass.
        few = [runs_of(name, {"new": (0.7, 1, 1, 1)}, n=10, spread=0.05, centre=50, seed=60+k) for k, name in enumerate(("a", "b"))]
        parts = merge(runs_of("capture", spread=0.05), runs_of("prompt", spread=0.05, centre=30, seed=40), *few)
        d = decide(parts, {"capture": 100, "prompt": 100, "a": None, "b": None})
        self.assertEqual((d["verdict"], d["one_quick_run"], d["too_few_samples"]), ("pass", [], ["a", "b"]))
        # Three of the old binary's runs loaded: as noisy.
        parts = runs_of("capture", {"old": (1.4, 1.4, 1, 1.4)}, spread=0.05)
        d = decide(parts, {"capture": 100})
        self.assertEqual((d["verdict"], d["one_quick_run"]), ("not judged", ["old"]))

    def figures(self, *figures, spread=0.3):
        """Each figure (name, the old binary's levels, the new one's, its
        centre): one list of samples, times each run's level."""
        out = {role: [{}, {}] for role in core.ROLES}
        for k, (name, old, new, centre) in enumerate(figures):
            rng = random.Random(4+k)
            s = [centre*(1+rng.uniform(-spread, spread)) for _ in range(100)]
            for binary, levels in (("old", old), ("new", new)):
                for i, role in enumerate(core.ROLES_OF[binary]):
                    for j in range(2):
                        out[role][j][name] = [v*levels[2*i+j] for v in s]
        return out

    def test_mild_load_on_three_runs_with_the_fourth_slow_by_chance_is_not_judged(self):
        # An unchanged binary, three of the new binary's runs loaded by 1.2
        # and its fourth 8% slow by chance: that run is only 1.11 times as
        # quick as the others, under the threshold, while both figures'
        # pooled samples are past it and no control sees the load. A quick
        # run is one more than QUICK, 1.10, times as quick as the median of
        # the others, so the gate is not judged; were it the threshold, the
        # gate would fail.
        levels = (1.08, 1.2, 1.2, 1.2)
        s = self.figures(("capture", (1,)*4, levels, 20), ("prompt", (1,)*4, levels, 30))
        d = decide(s, {"capture": None, "prompt": None})
        self.assertEqual((d["verdict"], d["one_quick_run"], d["machine_too_noisy"], d["quick"]), ("not judged", ["new"], True, 1.10))
        self.assertEqual(d["run_levels"]["new"], [0.9, 1.0, 1.0, 1.0])
        self.assertEqual(sorted({f.split(":")[0] for f in d["failures"]}), ["capture", "prompt"])
        self.assertEqual(decide(s, {"capture": None, "prompt": None}, quick=core.THRESHOLD)["verdict"], "fail")
        # redecide takes the gap a gate.json of this rule recorded; one of
        # an earlier rule's is worked out by this rule's own.
        gate = {"samples": s, "ceilings": {"capture": None, "prompt": None}, "scenario_failures": {}, "rounds": 2, "decision": d}
        self.assertEqual(core.redecide(gate)["verdict"], "not judged")
        gate["decision"] = dict(d, quick=1.15)
        self.assertEqual(core.redecide(gate)["verdict"], "fail")
        gate["decision"] = {k: v for k, v in gate["decision"].items() if k != "rule"}
        self.assertEqual(core.redecide(gate)["verdict"], "not judged")

    def test_a_figure_too_noisy_to_judge_counts_in_the_runs_levels(self):
        # a and c have the new binary's first run quicker by 1.15, b and d
        # do not, and d is 50% slower in every run, which fails. c is too
        # noisy to judge, but its runs still say how quick each run was:
        # with it the new binary's first run is quick, and the gate is not
        # judged; without it, the run levels would see no quick run, and
        # the gate would fail on load that may have hidden in c.
        one, mild = (1, 1, 1, 1), (1, 1.15, 1.15, 1.15)
        s = self.figures(("a", one, mild, 20), ("b", one, one, 30), ("c", (1, 1.6, 1, 1.6), mild, 40), ("d", one, (1.5,)*4, 50))
        d = decide(s, dict.fromkeys("abcd"))
        self.assertEqual((d["too_noisy"], d["one_quick_run"], d["verdict"]), (["c"], ["new"], "not judged"))
        self.assertEqual(d["run_levels"]["new"], [0.87, 1.0, 1.0, 1.0])
        self.assertEqual(sorted({f.split(":")[0] for f in d["failures"]}), ["d"])
        # c's p95 is too noisy; its median judges it.
        self.assertEqual(d["metrics"]["c"]["judged_on"], ["median"])


class LostRuns(unittest.TestCase):
    """A figure's samples when a scenario failed in some runs, and the
    scenario each figure comes from."""

    e = ["RuntimeError: invalid Stop output"]

    def startup(self, per_run, levels=None, lose=None):
        """startup, from the scenario padded-inbox, at per_run a run; lose
        is (role, run) whose samples a failed scenario dropped."""
        parts = runs_of("startup", levels, n=per_run, centre=100, spread=0.05, seed=70)
        failures = {role: {} for role in core.ROLES}
        if lose:
            role, k = lose
            parts[role][k] = {}
            failures[role] = {"padded-inbox": self.e}
        return parts, failures

    def test_one_lost_run_leaves_enough_at_the_gates_count(self):
        # Chat start doubled, and one run of either binary lost, at the
        # default rounds: 5 runs of GATE_RUN_SAMPLES still judge it, and it
        # fails.
        for lose in (("new", 0), ("new_again", 2), ("old", 1), ("old_again", 0)):
            parts = runs3("startup", {"new": (2,)*6}, n=core.GATE_RUN_SAMPLES, centre=100, spread=0.05, seed=70)
            failures = {role: {} for role in core.ROLES}
            parts[lose[0]][lose[1]] = {}
            failures[lose[0]] = {"padded-inbox": self.e}
            d = core.decide(parts, {"startup": 600}, failures, sources={"startup": "padded-inbox"})
            self.assertEqual((d["verdict"], d["too_few_samples"], d["short_by_failures"]), ("fail", [], []), lose)
            self.assertEqual(min(d["metrics"]["startup"]["n_old"], d["metrics"]["startup"]["n_new"]), 5*core.GATE_RUN_SAMPLES)
            self.assertGreaterEqual(5*core.GATE_RUN_SAMPLES, core.WHOLE)

    def test_a_figure_short_only_by_lost_runs_is_not_judged(self):
        # At 25 a run, as the gate took before, one lost run leaves 75: the
        # figure is listed as too few, and the gate is not judged, not a
        # pass, whichever binary lost it.
        for lose in (("new", 0), ("old_again", 1)):
            parts, failures = self.startup(25, {"new": (2,)*4}, lose)
            d = decide(parts, {"startup": 600}, failures, sources={"startup": "padded-inbox"})
            self.assertEqual((d["verdict"], d["exit"]), ("not judged", 0), lose)
            self.assertEqual((d["too_few_samples"], d["short_by_failures"]), (["startup"], ["startup"]))
            self.assertEqual(d["failures"], [])
            self.assertIn("startup has too few samples because a scenario failed in some runs: run the gate again", d["lost"])
        # With nothing lost, 25 a run is 100 a side, and it fails.
        parts, failures = self.startup(25, {"new": (2,)*4})
        self.assertEqual(decide(parts, {"startup": 600}, failures)["verdict"], "fail")

    def test_a_figure_short_by_design_is_only_listed(self):
        # 20 a run can never make 100 a side at two rounds: listed, and the
        # gate passes on the rest.
        parts = merge(runs_of("capture"), runs_of("startup", {"new": (2,)*4}, n=20, centre=100, seed=70))
        d = decide(parts, {"capture": 100, "startup": 600})
        self.assertEqual((d["verdict"], d["too_few_samples"], d["short_by_failures"]), ("pass", ["startup"], []))

    def test_the_old_binarys_side_needs_enough_too(self):
        # 99 on the old binary's side and 100 on the new one's.
        parts = runs_of("startup", {"new": (2,)*4}, n={"old": (25, 25, 25, 24), "new": (25,)*4}, centre=100, seed=70)
        d = decide(parts, {"startup": 600})
        m = d["metrics"]["startup"]
        self.assertEqual((m["n_old"], m["n_new"], m["too_few_samples"], d["too_few_samples"]), (99, 100, True, ["startup"]))
        self.assertEqual(d["failures"], [])

    def test_a_figure_missing_from_every_new_run_fails_unless_its_own_scenario_was_lost(self):
        # The new binary lost the scenario recall in every run, and the old
        # one in three of four: recall decides nothing by itself. startup,
        # which another scenario gives, is missing from every run of the new
        # binary: that is a regression, not a lost scenario.
        parts = merge(runs_of("capture"), runs_of("startup", n=34, centre=100, seed=70))
        for role in core.ROLES_OF["new"]:
            for run in parts[role]:
                del run["startup"]
        failures = {"old": {"recall": self.e*2}, "old_again": {"recall": self.e}, "new": {"recall": self.e*2},
                    "new_again": {"recall": self.e*2}}
        sources = {"capture": "padded-inbox", "startup": "padded-inbox", "prompt_with_recall": "recall"}
        d = decide(parts, {"capture": 100, "startup": 600}, failures, sources=sources)
        self.assertEqual((d["verdict"], d["exit"]), ("fail", 1))
        self.assertEqual(d["failures"], ["startup: the new binary gave no samples, the old one 136"])
        # When startup's own scenario is the one lost in every new run, the
        # scenarios' rule judges it: here it decides nothing, and the
        # figure is short only by the lost runs, so the gate is not judged.
        sources["startup"] = "recall"
        d = decide(parts, {"capture": 100, "startup": 600}, failures, sources=sources)
        self.assertEqual((d["verdict"], d["failures"], d["too_few_samples"], d["short_by_failures"]),
                         ("not judged", [], ["startup"], ["startup"]))
        # A gate.json from before the scenarios were recorded: any scenario
        # lost in every new run excuses the missing figure.
        d = decide(parts, {"capture": 100, "startup": 600}, failures)
        self.assertEqual((d["verdict"], d["failures"]), ("not judged", []))

    def test_a_figure_missing_from_every_new_run_when_its_scenario_was_lost_in_only_three(self):
        # recall gives startup, and the new binary lost recall in three of
        # its four runs, the old one in one: two more, so recall itself is
        # only "lost". But the fourth run, which recall did not fail, gave
        # no startup either: that is a regression, and fails.
        parts = merge(runs_of("capture"), runs_of("startup", n=34, centre=100, seed=70))
        for role in core.ROLES_OF["new"]:
            for run in parts[role]:
                del run["startup"]
        failures = {"old": {"recall": self.e}, "old_again": {}, "new": {"recall": self.e*2}, "new_again": {"recall": self.e}}
        sources = {"capture": "padded-inbox", "startup": "recall"}
        d = decide(parts, {"capture": 100, "startup": 600}, failures, sources=sources)
        self.assertEqual(d["scenarios"]["lost"], ["recall"])
        self.assertEqual((d["verdict"], d["failures"]), ("fail", ["startup: the new binary gave no samples, the old one 136"]))

    def test_a_figure_short_though_its_own_scenario_never_failed_is_only_listed(self):
        # startup gave 30, 30, 30 and 5 a binary without its scenario
        # failing, while another scenario failed in one run: startup is
        # short, but not by a failed run, so it is only listed.
        parts = merge(runs_of("capture"),
                      runs_of("startup", n={"old": (30, 30, 30, 5), "new": (30, 30, 30, 5)}, centre=100, seed=70))
        failures = {"old": {}, "old_again": {}, "new": {"recall": self.e}, "new_again": {}}
        sources = {"capture": "padded-inbox", "startup": "padded-inbox"}
        d = decide(parts, {"capture": 100, "startup": 600}, failures, sources=sources)
        self.assertEqual((d["verdict"], d["too_few_samples"], d["short_by_failures"], d["lost"]), ("pass", ["startup"], [], []))
        self.assertEqual(d["metrics"]["startup"]["judged"], False)
        # Had startup's own scenario failed in that run, it would be short
        # by it, and the gate not judged.
        failures["new"] = {"padded-inbox": self.e}
        d = decide(parts, {"capture": 100, "startup": 600}, failures, sources=sources)
        self.assertEqual((d["verdict"], d["short_by_failures"]), ("not judged", ["startup"]))

    def test_the_gate_records_the_scenario_of_each_figure(self):
        run = core.Run(None, None, gate=True)
        run.scenario = "padded-inbox"
        run.figure("startup", [1.0], target=300)
        run.scenario = "turn"
        run.metric("turn_codex_tool", [2.0], 50)
        self.assertEqual(run.sources, {"startup": "padded-inbox", "turn_codex_tool": "turn"})


class Ceilings(unittest.TestCase):
    def constant(self, old, new):
        out = {}
        for binary, values in (("old", old), ("new", new)):
            for i, role in enumerate(core.ROLES_OF[binary]):
                out[role] = [{"capture": [float(v)]*100} for v in values[2*i:2*i+2]]
        return out

    def test_the_old_binary_at_the_ceiling_is_not_over_it(self):
        d = decide(self.constant([100]*4, [110]*4), {"capture": 100})
        self.assertEqual(d["failures"], ["capture: p95 110.0 ms, at least 110.0 ms, passed the ceiling of 100 ms"])
        d = decide(self.constant([100.001]*4, [110]*4), {"capture": 100})
        self.assertEqual((d["verdict"], d["failures"]), ("pass", []))

    def test_a_ceiling_is_passed_whatever_the_noise_below_the_threshold(self):
        # The new binary's runs differ by 10%, so its noise is about 1.1:
        # the rise is too small for the ratio, but every run is past the
        # ceiling.
        d = decide(self.constant([95]*4, [101, 111, 101, 111]), {"capture": 100})
        m = d["metrics"]["capture"]
        self.assertTrue(1.05 < m["noise"] <= 1.15, m["noise"])
        self.assertEqual(m["reasons"], ["p95 111.0 ms, at least 111.0 ms, passed the ceiling of 100 ms"])
        self.assertEqual(d["verdict"], "fail")


class Bootstrap(unittest.TestCase):
    def test_the_ranks_are_beta_draws_of_the_95th_rank(self):
        # One generator for the sizes in turn; for 150 values the 95th
        # percentile is the 143rd, ceil(142.5).
        rng = random.Random(5)
        want = []
        for n in (150, 20):
            r = math.ceil(0.95*n)
            want.append(tuple(min(int(rng.betavariate(r, n-r+1)*n), n-1) for _ in range(40)))
        self.assertEqual(core.ranks((150, 20), 5, 40), tuple(want))
        self.assertEqual(math.ceil(0.95*150), 143)

    def test_p95_low_is_the_quantile_of_its_draws(self):
        values = samples(3, 120, 20, 0.4)
        (rv,) = core.ranks((120,), core.SEED, core.DRAWS)
        draws = sorted(sorted(values)[i] for i in rv)
        self.assertEqual(core.p95_low(values), draws[50])
        self.assertNotEqual(draws[50], draws[100])
        # Real ranks tie, so pin the quantile on draws that do not: ranks
        # 1999 down to 0 of 2,000 distinct values, whose draws, sorted, are
        # the values themselves. The 2.5% of 2,000 is the 51st, 50.
        values = list(range(core.DRAWS))
        random.Random(8).shuffle(values)
        saved = core.ranks
        core.ranks = lambda sizes, seed, draws: (tuple(reversed(range(sizes[0]))),)
        try:
            self.assertEqual(core.p95_low(values), 50)
        finally:
            core.ranks = saved


class Splits(unittest.TestCase):
    def test_two_rounds_have_every_split(self):
        s = core.splits(2)
        self.assertEqual(s[:2], [((0, 1), (0, 1)), ((0, 1), (2, 3))])
        # Three ways to put a second run of the old binary with its first,
        # times six ways to choose two of the new binary's four.
        self.assertEqual(len(s), 18)
        self.assertEqual(len(set(s)), 18)
        self.assertTrue(all(len(o) == 2 and len(n) == 2 and o[0] == 0 for o, n in s))
        self.assertEqual(core.SPLITS, 36)

    def test_one_round_has_the_role_splits_alone(self):
        self.assertEqual(core.splits(1), [((0,), (0,)), ((0,), (1,))])

    def test_more_rounds_take_a_seeded_sample(self):
        s = core.splits(3)
        self.assertEqual(len(s), 36)
        self.assertEqual(len(set(s)), 36)
        self.assertEqual(s[:2], [((0, 1, 2), (0, 1, 2)), ((0, 1, 2), (3, 4, 5))])
        self.assertTrue(all(len(o) == 3 and len(n) == 3 and o[0] == 0 for o, n in s))
        self.assertEqual(s, core.splits(3))
        self.assertEqual(s[2:6], [((0, 1, 2), (1, 2, 5)), ((0, 3, 4), (2, 3, 5)), ((0, 1, 5), (3, 4, 5)), ((0, 3, 4), (2, 4, 5))])
        self.assertNotEqual(s, core.splits(3, seed=core.SEED+1))
        self.assertEqual(core.splits(3, seed=core.SEED+1)[:2], s[:2])

    def test_three_rounds_decide(self):
        parts = {role: [{"capture": samples(10*k+i, 50, 20)} for i in range(3)] for k, role in enumerate(core.ROLES)}
        d = core.decide(parts, {"capture": 100}, rounds=3)
        self.assertEqual((d["verdict"], d["metrics"]["capture"]["controls"], d["metrics"]["capture"]["n_new"]), ("pass", 72, 300))

    def test_the_gate_takes_enough_samples_at_the_default_rounds(self):
        self.assertGreaterEqual(core.GATE_RUN_SAMPLES*2*core.ROUNDS, core.WHOLE)
        self.assertEqual(core.Run(None, None).loops(10), 10)
        self.assertGreaterEqual(core.GATE_RUN_SAMPLES*(2*core.ROUNDS-1), core.WHOLE)
        # 20 a run at three rounds: five runs make 100.
        self.assertEqual((core.WHOLE, core.GATE_RUN_SAMPLES), (100, 20))
        self.assertEqual(core.Run(None, None, gate=True).loops(10), 20)
        self.assertEqual(core.Run(None, None, gate=True).loops(30), 30)
        self.assertEqual(core.Run(None, None, gate=True).loops(50), 50)


class Schedule(unittest.TestCase):
    def test_the_order_turns(self):
        self.assertEqual([r for r, _ in core.schedule(2)], [1]*4+[2]*4)
        self.assertEqual([role for _, role in core.schedule(2)],
                         ["old", "new", "old_again", "new_again", "new_again", "old_again", "new", "old"])
        self.assertEqual([role for _, role in core.schedule(3)][8:], list(core.ROLES))

    def test_every_role_has_the_same_mean_place(self):
        for rounds in (2, 4, 6):
            order = [role for _, role in core.schedule(rounds)]
            places = {role: sum(k for k, r in enumerate(order) if r == role) for role in core.ROLES}
            self.assertEqual(len(set(places.values())), 1, places)

    def test_no_stretch_of_runs_hides_from_the_controls(self):
        # A stretch of loaded runs that loads new and new_again alike, and
        # old and old_again alike, but the new binary more than the old one,
        # looks to every control like a change. The schedule has none.
        for rounds in range(1, 7):
            order = [role for _, role in core.schedule(rounds)]
            for i in range(len(order)):
                for j in range(i+1, len(order)+1):
                    n = {role: order[i:j].count(role) for role in core.ROLES}
                    hidden = n["new"] == n["new_again"] and n["old"] == n["old_again"] and n["new"] != n["old"]
                    self.assertFalse(hidden, (rounds, order[i:j]))


class ScenarioFailures(unittest.TestCase):
    e = ["RuntimeError: a brief did not say it"]

    def failures(self, **counts):
        return {role: ({"recall": self.e*counts[role]} if counts.get(role) else {}) for role in core.ROLES}

    def test_three_more_failed_runs_of_four_fail(self):
        for counts in ({"new": 2, "new_again": 1}, {"new": 2, "new_again": 2}, {"old": 1, "new": 2, "new_again": 2}):
            found = core.scenarios(self.failures(**counts), 2)
            self.assertEqual((found["broken"], found["lost"], found["unrunnable"]), (["recall"], [], []), counts)
            d = core.decide({}, {}, self.failures(**counts), 2)
            self.assertEqual((d["verdict"], d["exit"], d["lost"]), ("fail", 1, []), counts)

    def test_two_more_failed_runs_of_four_are_not_judged(self):
        for counts in ({"new": 1, "new_again": 1}, {"new": 2}, {"old": 1, "new": 2, "new_again": 1}, {"old_again": 2, "new": 2, "new_again": 2}):
            found = core.scenarios(self.failures(**counts), 2)
            self.assertEqual((found["broken"], found["lost"], found["unrunnable"]), ([], ["recall"], []), counts)
            d = core.decide({}, {}, self.failures(**counts), 2)
            self.assertEqual((d["verdict"], d["exit"], d["failures"]), ("not judged", 0, []), counts)
            self.assertEqual(len(d["lost"]), 1)
            self.assertIn("run the gate again", d["lost"][0])

    def test_fewer_decide_nothing(self):
        for counts in ({"new": 1}, {"new_again": 1}, {"old": 1, "new": 2}, {"old": 2, "new_again": 3},
                       {"old": 1}, {"old": 2, "old_again": 2}):
            found = core.scenarios(self.failures(**counts), 2)
            self.assertEqual((found["broken"], found["lost"], found["unrunnable"]), ([], [], []), counts)
            self.assertEqual(core.decide({}, {}, self.failures(**counts), 2)["verdict"], "pass", counts)
        self.assertEqual(core.scenarios({}, 2), {"runs_per_binary": 4, "failed_runs": {}, "broken": [], "lost": [], "unrunnable": []})

    def test_the_shares_hold_at_other_rounds(self):
        # Three quarters and a half of the new binary's runs.
        for rounds, broken, lost in ((1, 2, 1), (3, 5, 3), (4, 6, 4)):
            found = core.scenarios(self.failures(new=broken), rounds)
            self.assertEqual((found["broken"], found["lost"]), (["recall"], []), rounds)
            found = core.scenarios(self.failures(new=broken-1), rounds)
            self.assertEqual((found["broken"], found["lost"]), ([], ["recall"]), rounds)
            found = core.scenarios(self.failures(new=lost-1), rounds)
            self.assertEqual((found["broken"], found["lost"]), ([], []), rounds)

    def test_a_scenario_every_run_failed_could_not_run(self):
        found = core.scenarios(self.failures(old=2, new=2, old_again=2, new_again=2), 2)
        self.assertEqual((found["broken"], found["lost"], found["unrunnable"]), ([], [], ["recall"]))
        d = core.decide({}, {}, self.failures(old=1, new=1, old_again=1, new_again=1), 1)
        self.assertEqual((d["verdict"], d["exit"]), ("could not run", 2))

    def test_a_broken_scenario_fails_the_gate_whatever_the_noise(self):
        noisy = merge(runs_of("capture", {"old": (1, 1, 1, 1.5), "new": (1.5, 1, 1, 1)}),
                      runs_of("prompt", {"old": (1.5, 1, 1, 1), "new": (1, 1, 1.5, 1)}, centre=30, seed=90))
        d = core.decide(noisy, {"capture": 100, "prompt": 100}, self.failures(new=2, new_again=1), 2)
        self.assertTrue(d["machine_too_noisy"])
        self.assertEqual((d["verdict"], d["exit"]), ("fail", 1))
        self.assertEqual(d["failures"][0], "the new binary failed the scenario recall in 3 of 4 runs, the old one in 0")

    def test_a_lost_scenario_hides_no_failed_figure(self):
        d = core.decide(runs_of("capture", add=20), {"capture": 100}, self.failures(new=2), 2)
        self.assertEqual((d["verdict"], d["exit"]), ("fail", 1))
        self.assertEqual(len(d["lost"]), 1)


class Compare(unittest.TestCase):
    """The gate's plumbing, benchmark.compare and benchmark.gate, on stub
    binaries and made-up scenarios that time nothing."""

    def setUp(self):
        self.saved = core.SCENARIOS[:]
        core.SCENARIOS.clear()
        self.dir = tempfile.TemporaryDirectory()
        self.path = Path(self.dir.name)
        for name in ("old", "new"):
            stub = self.path/("eva-md-"+name)
            stub.write_text("#!/bin/sh\nexit 0\n"); stub.chmod(0o700)
        self.calls = []

    def tearDown(self):
        core.SCENARIOS[:] = self.saved
        self.dir.cleanup()

    def binary(self, run):
        return Path(run.binary).name[len("eva-md-"):]

    def runs_of(self, which):
        return sum(1 for b in self.calls if b == which)

    def scenario(self, figures, place=100, name="fake"):
        """Adds a scenario that gives figures(binary, its run of that
        binary) as the metric capture, with a target of 50 ms."""
        @core.scenario(place, name)
        def fake(run):
            which = self.binary(run)
            self.calls.append(which)
            values = figures(which, self.runs_of(which))
            if isinstance(values, BaseException):
                raise values
            run.figure(name if name != "fake" else "capture", values, target=50)

    def gate(self, rounds=None, ceiling_factor=2.0):
        """The gate at rounds, by default the gate's own."""
        rounds = core.ROUNDS if rounds is None else rounds
        a = argparse.Namespace(compare=[self.path/"eva-md-old", self.path/"eva-md-new"], cwd=self.path, rounds=rounds,
                               ceiling_factor=ceiling_factor, output=self.path/"gate.json", baseline=self.path/"base.json",
                               commit="0123abc")
        out, err = io.StringIO(), io.StringIO()
        with contextlib.redirect_stdout(out), contextlib.redirect_stderr(err):
            code = bench.gate(lambda: core, a)
        report = json.loads((self.path/"gate.json").read_text()) if (self.path/"gate.json").exists() else None
        return code, out.getvalue(), report

    def test_an_unchanged_binary_passes(self):
        self.scenario(lambda which, k: samples(k, 100, 20))
        code, out, report = self.gate()
        self.assertEqual((code, report["verdict"]), (0, "pass"), out)
        self.assertTrue(out.endswith("latency gate: pass\n"))
        self.assertEqual({role: [len(run["capture"]) for run in runs] for role, runs in report["samples"].items()},
                         {role: [100]*core.ROUNDS for role in core.ROLES})
        self.assertEqual(report["ceilings"], {"capture": 100})
        self.assertEqual((report["rounds"], report["decision"]["rule"]), (3, 3))

    def test_the_verdict_worked_out_again_from_gate_json_is_the_one_printed(self):
        # Samples no rounding keeps, near the line between pass and fail.
        def figures(which, k):
            rng = random.Random(100*k + (which == "new"))
            return [math.pi*(1+rng.random()/7)*(1.14 if which == "new" else 1) for _ in range(30)]
        self.scenario(figures)
        self.scenario(lambda which, k: samples(k, 25, 100, 0.0137), place=200, name="startup")
        self.scenario(lambda which, k: RuntimeError("load") if (which, k) == ("new", 6) else samples(k, 25, 30), place=300, name="recall")
        code, out, report = self.gate()
        self.assertEqual(report["scenario_failures"]["new_again"], {"recall": ["RuntimeError: load"]})
        again = core.redecide(report)
        self.assertEqual(json.loads(json.dumps(again)), report["decision"])
        self.assertEqual((again["verdict"], again["exit"]), (report["verdict"], code))
        self.assertIn("latency gate: %s" % again["verdict"], out.splitlines()[-1])
        # Every sample is kept as it was taken: new_again's second run is the
        # new binary's third, in which capture was the seventh scenario it ran.
        self.assertEqual(report["samples"]["new_again"][1]["capture"], figures("new", 7))
        self.assertEqual(report["samples"]["old"][0]["capture"], figures("old", 1))
        # The figures it prints come from the decision it kept.
        m = report["decision"]["metrics"]["capture"]
        self.assertIn("capture                      old %8.1f ms  new %8.1f ms  ratio %6s  lower bound %6s  noise %6s  "
                      "median old %8.1f ms  new %8.1f ms  lower bound %6s  noise %6s  judged on %-14s" % (
                          m["p95_base_ms"], m["p95_ms"], m["ratio"], m["ratio_low"], m["noise"], m["median_base_ms"], m["median_ms"],
                          m["median_ratio_low"], m["median_noise"], " and ".join(m["judged_on"])), out)
        # At the default rounds: six runs a binary, and this rule's number.
        self.assertEqual({role: len(runs) for role, runs in report["samples"].items()}, {role: 3 for role in core.ROLES})
        self.assertEqual((report["rounds"], report["decision"]["rule"], len(report["runs"])), (3, 3, 12))

    def test_the_gates_runs_take_more_samples_of_a_few_hooks(self):
        seen = []

        @core.scenario(100, "chat-start")
        def chat_start(run):
            seen.append((run.gate, run.loops(10)))
            run.figure("startup", samples(len(seen), run.loops(10), 100), target=300)
        code, out, report = self.gate()
        self.assertEqual(seen, [(True, core.GATE_RUN_SAMPLES)]*4*core.ROUNDS)
        self.assertEqual(report["decision"]["metrics"]["startup"]["n_new"], 2*core.ROUNDS*core.GATE_RUN_SAMPLES)
        self.assertEqual(report["figure_scenarios"], {"startup": "chat-start"})
        self.assertEqual(report["decision"]["too_few_samples"], [])
        self.assertEqual(bench.run_once(core, Path("eva-md-old"), self.path).report["startup_samples"], 10)
        self.assertEqual(seen[-1], (False, 10))

    def test_a_scenario_the_new_binary_always_fails(self):
        self.scenario(lambda which, k: RuntimeError("hook failed or wrote diagnostics") if which == "new" else samples(k, 100, 20))
        code, out, report = self.gate()
        self.assertEqual((code, report["verdict"]), (1, "fail"), out)
        self.assertEqual(report["decision"]["failures"], ["the new binary failed the scenario fake in 6 of 6 runs, the old one in 0"])
        self.assertEqual(report["decision"]["too_few_samples"], ["capture"])

    def test_a_hook_that_hangs_in_runs_of_the_new_binary(self):
        # As fast as the old binary when it answers, but past the hook's
        # time limit in three of its four runs: the worst regression there
        # is. In two, it may be load: not judged, and run again. In one,
        # it is load, and decides nothing.
        for hung, code, verdict in (((1, 2, 4), 1, "fail"), ((1, 3), 0, "not judged"), ((2,), 0, "pass")):
            self.calls.clear()
            core.SCENARIOS.clear()
            self.scenario(lambda which, k: subprocess.TimeoutExpired(["eva-md", "hook"], 4) if which == "new" and k in hung
                          else samples(k, 100, 20))
            got, out, report = self.gate(rounds=2)
            self.assertEqual((got, report["verdict"]), (code, verdict), out)
            self.assertEqual(report["decision"]["scenarios"]["failed_runs"], {"fake": {"old": 0, "new": len(hung)}})
            self.assertEqual("fails: the new binary failed the scenario fake in 3 of 4 runs, the old one in 0" in out, len(hung) == 3)
            self.assertEqual("not judged: the new binary failed the scenario fake in 2 of 4 runs, the old one in 0: load may have lost it, "
                             "run the gate again" in out, len(hung) == 2)
            if verdict == "not judged":
                self.assertTrue(out.endswith("latency gate: not judged (a scenario may have been lost to load: run the gate again)\n"), out)

    def test_a_failed_runs_samples_are_left_out(self):
        # The new binary's first run gives its figure, then fails.
        @core.scenario(100, "fake")
        def fake(run):
            which = self.binary(run)
            self.calls.append(which)
            run.figure("capture", samples(self.runs_of(which), 30, 20), target=50)
            if which == "new" and self.runs_of("new") == 1:
                raise RuntimeError("invalid Stop output")
        code, out, report = self.gate()
        self.assertEqual(code, 0, out)
        self.assertEqual({role: [len(run.get("capture", [])) for run in runs] for role, runs in report["samples"].items()},
                         {"old": [30]*3, "new": [0, 30, 30], "old_again": [30]*3, "new_again": [30]*3})
        self.assertEqual(report["decision"]["metrics"]["capture"]["n_runs"]["new"], [0, 30, 30])
        self.assertEqual(report["scenario_failures"]["new"], {"fake": ["RuntimeError: invalid Stop output"]})

    def test_any_error_in_a_scenario_is_a_failed_run(self):
        # A scenario that reads what only the new binary writes raises a
        # KeyError on the old one: the old binary failed it, and the gate
        # still runs and decides.
        self.scenario(lambda which, k: KeyError("project") if which == "old" else samples(k, 100, 20))
        self.scenario(lambda which, k: samples(k, 100, 20), place=200, name="other")
        code, out, report = self.gate()
        self.assertEqual((code, report["verdict"], report["decision"]["not_compared"]), (0, "pass", ["capture"]), out)
        self.assertEqual(report["scenario_failures"]["old"], {"fake": ["KeyError: 'project'"]*core.ROUNDS})

    def test_a_scenario_every_run_failed_could_not_run(self):
        self.scenario(lambda which, k: IndexError("list index out of range"))
        code, out, report = self.gate()
        self.assertEqual((code, report["verdict"]), (2, "could not run"), out)

    def test_an_error_outside_the_scenarios_could_not_run(self):
        def prepare():
            raise TypeError("an error in the benchmark itself")
        a = argparse.Namespace()
        out, err = io.StringIO(), io.StringIO()
        with contextlib.redirect_stdout(out), contextlib.redirect_stderr(err):
            self.assertEqual(bench.gate(prepare, a), 2)
        self.assertEqual(out.getvalue(), "latency gate: could not run\n")
        self.assertIn("TypeError: an error in the benchmark itself", err.getvalue())

    def test_the_ceiling_comes_from_the_target(self):
        # 102 ms against 95: a rise too small for the ratio, past twice the
        # target of 50 ms.
        self.scenario(lambda which, k: [95.0]*25 if which == "old" else [102.0]*25)
        code, out, report = self.gate()
        self.assertEqual((code, report["verdict"]), (1, "fail"), out)
        self.assertEqual(report["decision"]["metrics"]["capture"]["ceiling_ms"], 100)
        self.assertEqual(report["decision"]["failures"], ["capture: p95 102.0 ms, at least 102.0 ms, passed the ceiling of 100 ms"])
        code, out, report = self.gate(ceiling_factor=0)
        self.assertEqual((code, report["verdict"]), (0, "pass"), out)
        self.assertIsNone(report["decision"]["metrics"]["capture"]["ceiling_ms"])

    def test_too_few_samples_are_listed(self):
        # 15 a run: 90 a binary at three rounds.
        self.scenario(lambda which, k: samples(k, 15, 20 if which == "old" else 40))
        code, out, report = self.gate()
        self.assertEqual((code, report["verdict"], report["decision"]["too_few_samples"]), (0, "pass", ["capture"]), out)
        self.assertIn("too few samples to judge, fewer than 100 a binary", out)

    def test_a_noisy_machine_is_not_judged(self):
        # One run of each binary slowed by half, at two rounds: the p95's
        # controls see it, the median's do not, and the figure is judged on
        # its median.
        slow = lambda which, k, at, f, spread: samples(k, 50, 20*(f if (which, k) in at else 1), spread)
        self.scenario(lambda which, k: slow(which, k, (("old", 4), ("new", 1)), 1.5, 0.1))
        code, out, report = self.gate(rounds=2)
        d = report["decision"]
        self.assertEqual((code, report["verdict"], d["too_noisy"], d["metrics"]["capture"]["judged_on"]),
                         (0, "pass", ["capture"], ["median"]), out)
        # Twice as slow, among samples that differ more: the median's
        # controls see it too, and the figure is too noisy to judge.
        core.SCENARIOS.clear()
        self.calls.clear()
        self.scenario(lambda which, k: slow(which, k, (("old", 4), ("new", 1)), 2, 0.3))
        code, out, report = self.gate(rounds=2)
        d = report["decision"]
        self.assertEqual((code, report["verdict"], d["machine_too_noisy"], d["too_noisy_to_judge"]),
                         (0, "not judged", False, ["capture"]), out)
        self.assertTrue(out.endswith("latency gate: not judged (a figure's runs differed by more than 1.15 in its p95 and in its median, "
                                     "too much to judge it: run the gate again)\n"), out)

    def test_two_noisy_figures_say_the_machine_was_too_noisy(self):
        # Each binary's run index is k counted over both scenarios.
        at = (("old", 4), ("new", 1))
        slow = lambda which, k: samples(k, 50, 20*(1.5 if (which, (k+1)//2) in at else 1))
        self.scenario(slow)
        self.scenario(slow, place=200, name="startup")
        code, out, report = self.gate(rounds=2)
        self.assertEqual((code, report["verdict"], report["decision"]["machine_too_noisy"]), (0, "not judged", True), out)
        self.assertTrue(out.endswith("latency gate: not judged (runs differed by more than 1.15 in the p95 of 2 figures or more, or one "
                                     "run was more than 1.10 times as quick as the median of the binary's other runs: the machine was "
                                     "too noisy to judge)\n"), out)

    def test_one_quick_run_is_printed_with_its_gap(self):
        # The new binary's runs 2 to 6 are 20% slower than its first, in
        # both figures, and no control can see it.
        slow = lambda which, k: samples(k % 2, 100, 20*(1.2 if which == "new" and k > 2 else 1), 0.05)
        self.scenario(slow)
        self.scenario(slow, place=200, name="startup")
        code, out, report = self.gate()
        self.assertEqual((code, report["verdict"], report["decision"]["one_quick_run"]), (0, "not judged", ["new"]), out)
        self.assertIn("not judged: one run of the new binary was more than 1.10 times as quick as the median of its other runs, over "
                      "the figures judged: load on its other runs looks like a change\n", out)

    def test_the_figures_scenarios_decide_and_are_worked_out_again(self):
        # capture, from the scenario fake, is missing from every run of the
        # new binary, though fake never failed; recall failed in every run
        # of the new binary and three of the old one's. Only the scenario
        # each figure comes from says capture is a regression: the gate
        # fails, and works out the same verdict again from gate.json.
        self.scenario(lambda which, k: [] if which == "new" else samples(k, 100, 20))
        self.scenario(lambda which, k: RuntimeError("load") if which == "new" or k < 9 else samples(k, 100, 30),
                      place=200, name="recall")
        self.scenario(lambda which, k: samples(k, 100, 40), place=300, name="startup")
        code, out, report = self.gate(rounds=2)
        self.assertEqual(report["figure_scenarios"], {"capture": "fake", "recall": "recall", "startup": "startup"})
        self.assertEqual((code, report["verdict"]), (1, "fail"), out)
        self.assertEqual(report["decision"]["failures"], ["capture: the new binary gave no samples, the old one 400"])
        self.assertEqual(report["decision"]["scenarios"]["failed_runs"], {"recall": {"old": 3, "new": 4}})
        self.assertIn("fails: capture: the new binary gave no samples, the old one 400\n", out)
        self.assertTrue(out.endswith("latency gate: fail\n"), out)
        again = core.redecide(report)
        self.assertEqual(json.loads(json.dumps(again)), report["decision"])
        # Without them, recall lost in every run of the new binary would
        # excuse capture.
        del report["figure_scenarios"]
        self.assertEqual(core.redecide(report)["verdict"], "not judged")

    def test_a_figure_short_by_a_lost_run_says_so(self):
        # 18 a run, and one run of the new binary lost: 90, where six would
        # have made 108.
        self.scenario(lambda which, k: RuntimeError("load") if (which, k) == ("new", 1) else samples(k, 18, 20))
        code, out, report = self.gate()
        self.assertEqual((code, report["verdict"], report["decision"]["short_by_failures"]), (0, "not judged", ["capture"]), out)
        self.assertIn("too few samples to judge, fewer than 100 a binary, as a scenario failed in some runs\n", out)
        self.assertIn("not judged: capture has too few samples because a scenario failed in some runs: run the gate again\n", out)

    def test_gate_json_names_no_path(self):
        # The binaries lie under a home folder wherever the gate runs:
        # gate.json keeps neither path.
        self.scenario(lambda which, k: samples(k, 100, 20))
        code, out, report = self.gate()
        self.assertNotIn("old", report)
        self.assertNotIn("new", report)
        self.assertNotIn(self.dir.name, (self.path/"gate.json").read_text())
        self.assertNotIn(self.dir.name, (self.path/"base.json").read_text())

    def test_the_order_turns_each_round(self):
        self.scenario(lambda which, k: samples(k, 100, 20))
        code, out, report = self.gate(rounds=3)
        order = [role for _, role in core.schedule(3)]
        self.assertEqual(report["schedule"], order)
        self.assertEqual([(r["round"], r["binary"]) for r in report["runs"]], core.schedule(3))
        self.assertEqual(self.calls, [core.BINARY[role] for role in order])
        self.assertEqual(self.calls[:8], ["old", "new", "old", "new", "new", "old", "new", "old"])
        self.assertEqual(report["decision"]["controls"], 36)

    def test_the_samples_of_a_role_are_its_runs_in_order(self):
        self.scenario(lambda which, k: [float(10*k + (which == "new"))]*30)
        code, out, report = self.gate()
        self.assertEqual({role: [run["capture"][0] for run in runs] for role, runs in report["samples"].items()},
                         {"old": [10.0, 40.0, 50.0], "new": [11.0, 41.0, 51.0], "old_again": [20.0, 30.0, 60.0],
                          "new_again": [21.0, 31.0, 61.0]})

    def test_the_baseline_holds_the_new_binarys_runs(self):
        self.scenario(lambda which, k: samples(k, 100, 20 if which == "old" else 30))
        code, out, report = self.gate()
        base = json.loads((self.path/"base.json").read_text())
        self.assertEqual((base["runs"], base["commit"], base["metrics"]["capture"]["n"]), (6, "0123abc", 600))
        self.assertGreater(base["metrics"]["capture"]["median_ms"], 27)


class Hooks(unittest.TestCase):
    """How the benchmark runs a hook, and stops one past its limit, on stub
    binaries that start a child and wait for it."""

    GUARD = 30  # seconds: a hang guard, never what decides a test

    def setUp(self):
        self.dir = tempfile.TemporaryDirectory()
        self.path = Path(self.dir.name)
        self.pids = self.path/"pids"
        self.stub = self.path/"eva-md"
        self.stub.write_text('#!/bin/sh\nsleep 60 &\necho "$$ $!" > "$PIDS.tmp"\nmv "$PIDS.tmp" "$PIDS"\nwait\n')
        self.stub.chmod(0o700)
        self.env = dict(os.environ, PIDS=str(self.pids))
        self.started = []

    def tearDown(self):
        for pid in self.started:
            try:
                os.kill(pid, signal.SIGKILL)
            except (ProcessLookupError, PermissionError):
                pass
        self.dir.cleanup()

    def wait_for_pids(self):
        end = time.monotonic() + self.GUARD
        while not self.pids.exists():
            self.assertLess(time.monotonic(), end, "the stub never started its child")
            time.sleep(0.01)
        pids = [int(p) for p in self.pids.read_text().split()]
        self.started += pids
        return pids

    def gone(self, pid):
        """Whether pid has exited, waiting up to the guard: a process killed
        is gone at once, but its parent may take a moment to reap it."""
        end = time.monotonic() + self.GUARD
        while True:
            state = subprocess.run(["ps", "-o", "stat=", "-p", str(pid)], capture_output=True, text=True).stdout.strip()
            if not state or state.startswith("Z"):
                return True
            if time.monotonic() > end:
                return False
            time.sleep(0.01)

    def test_stop_kills_every_process_in_the_hooks_group(self):
        for host in ("claude-code", "codex"):
            self.pids.unlink(missing_ok=True)
            p = core.start(core.launch(self.stub, host), self.env)
            self.started.append(p.pid)
            stub, child = self.wait_for_pids()
            # On Codex's path the shell runs the stub as its child.
            self.assertEqual(stub == p.pid, host == "claude-code")
            core.stop(p)
            self.assertEqual(p.returncode, -signal.SIGKILL)
            for pid in (p.pid, stub, child):
                self.assertTrue(self.gone(pid), (host, pid))
            p.stdout.close(); p.stderr.close(); p.stdin.close()

    def test_a_hook_past_its_limit_is_stopped_with_its_group(self):
        stopped = []
        real = core.stop

        def recording(p):
            stopped.append(p)
            real(p)
        core.stop = recording
        try:
            with self.assertRaises(subprocess.TimeoutExpired):
                core.run_hook(core.launch(self.stub, "codex"), "{}", self.env, limit=0.2)
        finally:
            core.stop = real
        self.assertEqual(len(stopped), 1)
        self.assertEqual(stopped[0].args, core.launch(self.stub, "codex"))
        self.assertEqual(stopped[0].returncode, -signal.SIGKILL)
        self.started.append(stopped[0].pid)
        if self.pids.exists():
            for pid in self.wait_for_pids():
                self.assertTrue(self.gone(pid), pid)

    def test_a_hook_that_answers(self):
        argv = ["/bin/sh", "-c", "cat; echo note >&2; exit 3"]
        self.assertEqual(core.run_hook(argv, '{"a": 1}', self.env), (3, '{"a": 1}', "note\n"))

    def test_every_hook_has_the_same_limit_of_4_seconds(self):
        # The README's 4 seconds, and no scenario gives a hook longer.
        self.assertEqual(core.HOOK_LIMIT, 4)
        import inspect
        self.assertEqual(inspect.signature(core.run_hook).parameters["limit"].default, core.HOOK_LIMIT)
        calls = 0
        for path in [HERE.parent/"benchmark.py"] + sorted(HERE.glob("*.py")):
            for n in ast.walk(ast.parse(path.read_text())):
                if isinstance(n, ast.Call) and getattr(n.func, "id", getattr(n.func, "attr", None)) == "run_hook":
                    if path.name != "test_gate.py":
                        calls += 1
                        self.assertEqual((len(n.args), [k.arg for k in n.keywords]), (3, []), (path.name, n.lineno))
        self.assertGreaterEqual(calls, 2)


class Simulation(unittest.TestCase):
    """The simulation's own pieces; the simulation itself is run by hand."""

    def test_a_saved_gate_is_split_into_runs(self):
        gate = {"samples": {role: {"capture": [1.0, 2.0, 3.0, 4.0, 5.0], "startup": [9.0]} for role in core.ROLES},
                "runs": [{"binary": role, "report": {"capture_samples": 2 if k == 0 else 3, "startup_samples": 1 if k else None}}
                         for k in range(2) for role in core.ROLES]}
        runs = sim.runs_of(gate)
        self.assertEqual(runs["old"], [{"capture": [1.0, 2.0]}, {"capture": [3.0, 4.0, 5.0], "startup": [9.0]}])
        gate["runs"][0]["report"]["capture_samples"] = 1
        with self.assertRaisesRegex(ValueError, "do not add up"):
            sim.runs_of(gate)
        kept = {"samples": {role: [{"capture": [1.0]}, {"capture": [2.0]}] for role in core.ROLES}}
        self.assertEqual(sim.runs_of(kept)["new"], [{"capture": [1.0]}, {"capture": [2.0]}])

    def test_a_made_up_gate_has_the_gates_counts(self):
        rng = random.Random(1)
        samples_by_role = sim.one(sim.made_up(rng), sim.LOADS["none"], sim.CHANGES["unchanged"], rng)
        for name, n, *_ in sim.FIGURES:
            self.assertEqual([len(run[name]) for role in core.ROLES for run in samples_by_role[role]], [n]*4*core.ROUNDS, name)
            self.assertGreaterEqual((2*core.ROUNDS-1)*n, core.WHOLE, name)
        # (startup-x1.5 and startup-x2, the chat start alone, are among the
        # rises of one hook alone.)
        self.assertEqual(sorted(sim.CHANGES), sorted(["all-x1.2", "all-x1.3", "all-x1.5", "all-x2", "all-x3", "capture+20ms",
                                                      "unchanged", "capture-varies", "all-x1.3-in-3"] +
                                                     ["%s-x%s" % (name, x) for name, *_ in sim.FIGURES for x in ("1.3", "1.5", "2", "3")]))
        self.assertEqual(sorted(sim.LOADS), ["drift", "heavy", "light", "new-3", "new-3-mild", "new-4", "new-all-mild", "new-but-1-mild",
                                             "new-pair", "none", "round", "runs"])
        # A change falls on the new binary's runs alone.
        base = sim.made_up(random.Random(2))
        x = sim.one(base, sim.LOADS["none"], sim.CHANGES["all-x1.5"], random.Random(3))
        y = sim.one(base, sim.LOADS["none"], sim.CHANGES["unchanged"], random.Random(3))
        self.assertEqual(x["old"], y["old"])
        self.assertEqual(x["new"][0]["capture"], [v*1.5 for v in y["new"][0]["capture"]])
        # One hook alone, by each rise; a factor each new run draws; all
        # the new binary's runs but one.
        for rise in (1.3, 1.5, 2, 3):
            z = sim.one(base, sim.LOADS["none"], sim.CHANGES["session_end-x%g" % rise], random.Random(3))
            self.assertEqual((z["new"][1]["capture"], z["new"][1]["session_end"]),
                             (y["new"][1]["capture"], [v*rise for v in y["new"][1]["session_end"]]))
        v = sim.one(base, sim.LOADS["none"], sim.CHANGES["capture-varies"], random.Random(3))
        f = [v[role][i]["capture"][0]/y[role][i]["capture"][0] for role in core.ROLES_OF["new"] for i in range(core.ROUNDS)]
        self.assertTrue(all(1.2 <= x <= 2.0 for x in f) and len(set(f)) == 2*core.ROUNDS, f)
        w = sim.one(base, sim.LOADS["none"], sim.CHANGES["all-x1.3-in-3"], random.Random(3))
        f = sorted(round(w[role][i]["startup"][0]/y[role][i]["startup"][0], 9) for role in core.ROLES_OF["new"] for i in range(core.ROUNDS))
        self.assertEqual(f, [1.0] + [1.3]*(2*core.ROUNDS-1))
        # Each run shares a factor with every figure.
        self.assertGreater(sim.COMMON, 0)

    def loaded_runs(self, load, seed):
        """The runs, by their place in the gate's order, that load slowed,
        on a base whose samples are all 1."""
        flat = [{"capture": [1.0]*4} for _ in range(sim.RUNS)]
        s = sim.one(flat, sim.LOADS[load], sim.CHANGES["unchanged"], random.Random(seed))
        nth, out = {role: 0 for role in core.ROLES}, []
        for k, role in enumerate(sim.ORDER):
            run = s[role][nth[role]]
            nth[role] += 1
            if max(run["capture"]) > 1.0:
                out.append((k, role))
        return out

    def test_the_loads_of_the_third_round(self):
        for seed in range(20):
            # One round: the four runs of one round.
            runs = self.loaded_runs("round", seed)
            self.assertEqual(len(runs), 4, runs)
            self.assertEqual(len({k // 4 for k, _ in runs}), 1, runs)
            self.assertEqual(sorted(role for _, role in runs), sorted(core.ROLES))
            # The new binary's runs: three, all but one, all.
            for load, count in (("new-3-mild", 3), ("new-but-1-mild", 2*core.ROUNDS-1), ("new-all-mild", 2*core.ROUNDS)):
                runs = self.loaded_runs(load, seed)
                self.assertEqual(len(runs), count, (load, runs))
                self.assertTrue(all(core.BINARY[role] == "new" for _, role in runs), (load, runs))

    def test_more_rounds(self):
        # The made-up base at two rounds, as the second round measured it,
        # and at the default three.
        try:
            sim.set_rounds(2)
            runs = sim.made_up(random.Random(1))
            self.assertEqual((len(runs), len(sim.ORDER)), (8, 8))
            s = sim.one(runs, sim.LOADS["new-pair"], sim.CHANGES["unchanged"], random.Random(2))
            self.assertEqual([len(s[role]) for role in core.ROLES], [2]*4)
            self.assertIn(core.decide(s, sim.CEILINGS, rounds=2)["verdict"], ("pass", "not judged", "fail"))
        finally:
            sim.set_rounds(core.ROUNDS)
        self.assertEqual((sim.ROUNDS, len(sim.ORDER), len(sim.made_up(random.Random(1)))), (3, 12, 12))

    def test_a_saved_gate_that_lost_a_run_is_not_drawn(self):
        # At two rounds a base is one gate's eight runs.
        whole = {role: [{"capture": [1.0]}, {"capture": [2.0]}] for role in core.ROLES}
        lost = {role: [{"capture": [1.0]}, {}] for role in core.ROLES}
        try:
            sim.set_rounds(2)
            base = sim.from_gates([{"samples": whole}, {"samples": lost}])
            for seed in range(10):
                self.assertEqual(sorted(run["capture"][0] for run in base(random.Random(seed))), [1.0]*4+[2.0]*4)
        finally:
            sim.set_rounds(core.ROUNDS)
        # At three, two gates are needed, and one that lost a run is not one.
        with self.assertRaisesRegex(ValueError, "1 saved gates that lost no run cannot give 12 runs"):
            sim.from_gates([{"samples": whole}, {"samples": lost}])

    def test_three_rounds_take_twelve_runs_of_two_gates_at_one_level(self):
        # Three gates of an unchanged binary, at levels 10, 20 and 40, each
        # run marked by its count of samples: ten times its gate, and its
        # place there. A base takes twelve runs of two gates, never one run
        # twice, the second gate brought to the first's level, figure by
        # figure.
        def gate(g, level):
            return {"samples": {role: [{"capture": [level*(1+0.001*i)]*(10*g+2*k+i+1), "startup": [3*level]*3}
                                       for i in range(2)] for k, role in enumerate(core.ROLES)}}
        gates = [gate(1, 10.0), gate(2, 20.0), gate(3, 40.0)]
        base = sim.from_gates(gates)
        seen = set()
        for seed in range(30):
            runs = base(random.Random(seed))
            self.assertEqual(len(runs), 12)
            marks = [len(run["capture"]) for run in runs]
            self.assertEqual(len(set(marks)), 12, marks)
            pair = {m // 10 for m in marks}
            self.assertEqual(len(pair), 2, marks)
            seen.add(tuple(sorted(pair)))
            # One level: each figure's typical run is the same in every run
            # but by the 0.1% a run's place puts between them.
            for name, at in (("capture", 1.0), ("startup", 3.0)):
                levels = {round(run[name][0]/at, 1) for run in runs}
                self.assertEqual(len(levels), 1, (name, levels))
        self.assertEqual(seen, {(1, 2), (1, 3), (2, 3)})


class MissedLimits(unittest.TestCase):
    """What the padded inbox allows for a hook it timed past its limit,
    which delivers nothing by design: a start of the stand-in missing, or an
    empty output, from that hook alone. On made-up numbers, and on a
    stand-in that drops one of its starts."""

    GUARD = 30  # seconds: a hang guard, never what decides a test

    def times(self, slow_stops=0, stops=30, ends=30):
        """The (event, ms) of the padded inbox's Stop and SessionEnd hooks,
        the first slow_stops of the Stops timed just past their limit."""
        return [("Stop", 290.0 if i < slow_stops else 40.0) for i in range(stops)] + [("SessionEnd", 60.0)]*ends

    def test_none_slow_and_one_missing_fails(self):
        self.assertEqual(core.starts_allowed(60, 59, self.times()), (False, 0))
        self.assertEqual(core.starts_allowed(60, 60, self.times()), (True, 0))

    def test_one_slow_and_one_missing_passes(self):
        self.assertEqual(core.starts_allowed(60, 59, self.times(1)), (True, 1))
        self.assertEqual(core.starts_allowed(60, 60, self.times(1)), (True, 1))

    def test_one_slow_and_two_missing_fails(self):
        self.assertEqual(core.starts_allowed(60, 58, self.times(1)), (False, 1))
        self.assertEqual(core.starts_allowed(60, 58, self.times(2)), (True, 2))

    def test_more_seen_than_expected_fails(self):
        self.assertEqual(core.starts_allowed(60, 61, self.times()), (False, 0))
        self.assertEqual(core.starts_allowed(60, 61, self.times(1)), (False, 1))
        self.assertEqual(core.starts_allowed(1, 2, []), (False, 0))

    def test_a_hook_is_past_its_limit_from_its_limit_less_the_margin(self):
        for event in ("Stop", "SessionEnd", "UserPromptSubmit"):
            self.assertFalse(core.past_limit(event, 289.999), event)
            self.assertTrue(core.past_limit(event, 290), event)
        for event in ("SessionStart", "PostToolUse", "PostToolUseFailure"):
            self.assertFalse(core.past_limit(event, 1489.999), event)
            self.assertTrue(core.past_limit(event, 1490), event)
        self.assertEqual(core.starts_allowed(30, 29, [("Stop", 289.999)]*30), (False, 0))
        self.assertEqual(core.starts_allowed(30, 29, [("SessionStart", 1000.0)]*30), (False, 0))

    def test_the_figures_mirror_the_hooks_go_code(self):
        # budget in internal/hook/hook.go gives each event its limit, and
        # Run there ends its work's context that much before the deadline.
        src = (HERE.parent.parent/"internal/hook/hook.go").read_text()
        body = re.search(r"\nfunc budget\(event string\) time\.Duration \{\n(.*?)\n\}\n", src, re.S).group(1)
        cases = re.findall(r"case ([^:]+):\s*return (\d+) \* time\.Millisecond", body)
        limits = {event.strip('" '): int(ms) for events, ms in cases for event in events.split(",")}
        self.assertEqual(limits, core.EVENT_LIMIT_MS)
        self.assertEqual([int(ms) for ms in re.findall(r"\n\treturn (\d+) \* time\.Millisecond", body)], [core.DEFAULT_LIMIT_MS])
        margins = re.findall(r"workCtx, cancel := env\.withDeadline\(ctx, deadline\.Add\(-(\d+)\*time\.Millisecond\)\)", src)
        self.assertEqual([int(ms) for ms in margins], [core.LIMIT_MARGIN_MS])
        # Before it knows its event, every hook reads its input under
        # budget(""), less the same margin: the limit no_sweep names as the
        # one in which a hook reads its input, past_limit("", ms).
        self.assertEqual(len(re.findall(r"\n\tdeadline := invocationDeadline\(ctx, started\.Add\(budget\(\"\"\)\+env\.Slack\)\)\n", src)), 1)
        reads = re.findall(r"readCtx, cancelRead := env\.withDeadline\(ctx, deadline\.Add\(-(\d+)\*time\.Millisecond\)\)", src)
        self.assertEqual([int(ms) for ms in reads], [core.LIMIT_MARGIN_MS])
        self.assertNotIn("", core.EVENT_LIMIT_MS)
        # A Codex Stop's {} is written only by the deadline, so one past it
        # prints nothing (Home.hook lets that through).
        self.assertIn('\t\tif host == "codex" && event == "Stop" {\n\t\t\toutput = "{}"\n\t\t}\n'
                      '\t\twriteOutput(out, output, deadline, env.withDeadline)\n', src)

    def read(self, results, name="prompt_with_pointers"):
        err = io.StringIO()
        with contextlib.redirect_stderr(err):
            indexes = bench.to_read(name, "UserPromptSubmit", results)
        return indexes, err.getvalue()

    def test_an_empty_output_in_time_fails(self):
        results = [('{"a": 1}', 40.0)]*99 + [("", 289.0)]
        with self.assertRaisesRegex(RuntimeError, "prompt_with_pointers: 1 of 100 hooks gave no output inside their limit"):
            self.read(results)
        # Even beside one that passed its limit.
        with self.assertRaisesRegex(RuntimeError, "1 of 100 hooks gave no output inside their limit"):
            self.read([("", 400.0)] + results[1:])

    def test_an_empty_output_past_the_limit_is_passed_over(self):
        results = [('{"a": 1}', 40.0)]*99 + [("", 290.0)]
        indexes, err = self.read(results)
        self.assertEqual(indexes, list(range(99)))
        self.assertEqual(err, "benchmark: prompt_with_pointers: 1 of 100 hooks passed their limit, 300 ms less 10\n")

    def test_more_than_one_in_twenty_past_the_limit_fails(self):
        # Five of 100 past the limit is one in twenty, and passes; six is
        # more, and fails, when an empty output was passed over.
        five = [('{"a": 1}', 300.0)]*4 + [("", 300.0)] + [('{"a": 1}', 40.0)]*95
        indexes, err = self.read(five)
        self.assertEqual(indexes, [i for i in range(100) if i != 4])
        self.assertIn("5 of 100 hooks passed their limit", err)
        six = five[:5] + [('{"a": 1}', 300.0)] + five[6:]
        with self.assertRaisesRegex(RuntimeError, "6 of 100 hooks passed their limit, more than one in twenty, and 1 gave no output"):
            self.read(six)
        # With no output to pass over there is nothing to excuse: hooks
        # past their limit that answered are the p95's to judge.
        indexes, err = self.read([('{"a": 1}', 300.0)]*10 + [('{"a": 1}', 40.0)]*90)
        self.assertEqual((indexes, err), (list(range(100)), "benchmark: prompt_with_pointers: 10 of 100 hooks passed their limit, 300 ms less 10\n"))

    def test_hooks_in_time_print_nothing(self):
        self.assertEqual(self.read([('{"a": 1}', 289.0)]*25), (list(range(25)), ""))

    # A stand-in that writes its arguments, as the benchmark's does, but
    # drops its DROP-th call, the warm-up being the first. The calls are
    # those the padded inbox's hooks make of it.
    DROPPING = ('#!/bin/sh\nn=$(cat "$HOME/calls" 2>/dev/null || echo 0)\nn=$((n+1))\necho "$n" > "$HOME/calls"\n'
                '[ "$n" = "$DROP" ] || echo "$*" >> "$HOME/spawned"\n')

    def started(self, drop):
        """The lines the padded inbox's starts leave, in its order, with the
        drop-th call of the stand-in dropped; the warm-up's line is left out,
        as the scenario leaves it out."""
        with tempfile.TemporaryDirectory() as d:
            standin = Path(d)/"eva-md"
            standin.write_text(self.DROPPING); standin.chmod(0o700)
            env = dict(os.environ, HOME=d, DROP=str(drop))
            calls = [["warm-up"], ["maintain"], ["update", "--sweep=2026-11-13"]]
            calls += [["update", "--session", "codex:m1-spawn"]]*30
            calls += [["update", "--session", "claude-code:m1-end-%d" % i] for i in range(30)]
            for args in calls:
                subprocess.run([str(standin)] + args, env=env, check=True, timeout=self.GUARD)
            return (Path(d)/"spawned").read_text().splitlines()[1:]

    def test_a_start_lost_with_no_slow_hook_fails_and_names_its_kind(self):
        # The 10th call is a Stop's start, the 40th a SessionEnd's.
        lines = self.started(10)
        self.assertEqual(len(lines), 61)
        with self.assertRaises(RuntimeError) as e:
            bench.check_starts(lines, bench.start_groups([40.0]*30, [60.0]*30))
        message = str(e.exception)
        self.assertEqual(message, "the hooks started the stand-in 61 times, not 62, with 0 of the hooks that start it past their limit: "
                         "Stop 29 of 30 (1 short), SessionEnd 30 of 30, maintain 1 of 1, the sweep 1 of 1; seen: maintain: 1, "
                         "update --session claude-code:m#-end-#: 30, update --session codex:m#-spawn: 29, update --sweep=#-#-#: 1")
        with self.assertRaisesRegex(RuntimeError, r"Stop 30 of 30, SessionEnd 29 of 30 \(1 short\)"):
            bench.check_starts(self.started(40), bench.start_groups([40.0]*30, [60.0]*30))

    def test_a_start_lost_beside_a_slow_hook_of_its_kind_passes(self):
        slow_stop = [290.0] + [40.0]*29
        self.assertEqual(bench.check_starts(self.started(10), bench.start_groups(slow_stop, [60.0]*30)), 1)
        self.assertEqual(bench.check_starts(self.started(0), bench.start_groups(slow_stop, [60.0]*30)), 1)
        self.assertEqual(bench.check_starts(self.started(0), bench.start_groups([40.0]*30, [60.0]*30)), 0)
        # A slow Stop excuses no SessionEnd's start, and nothing excuses
        # maintain's or the sweep's.
        for drop in (40, 2, 3):
            with self.assertRaisesRegex(RuntimeError, r"with 1 of the hooks that start it past their limit: .*\(1 short\)"):
                bench.check_starts(self.started(drop), bench.start_groups(slow_stop, [60.0]*30))

    def test_a_start_of_no_kind_asked_for_fails(self):
        lines = self.started(0)
        for extra, why in (("maintain", r"maintain 2 of 1 \(1 too many\)"), ("maintain --push-only", "1 of no kind asked for"),
                           ("update --session codex:m1-spawn", r"Stop 31 of 30 \(1 too many\)")):
            with self.assertRaisesRegex(RuntimeError, why):
                bench.check_starts(lines + [extra], bench.start_groups([290.0]*30, [60.0]*30))

    def test_a_kind_replaces_digits(self):
        self.assertEqual(bench.kind("update --session claude-code:m1-end-17"), "update --session claude-code:m#-end-#")
        self.assertEqual(bench.kind("update --sweep=2026-11-13"), "update --sweep=#-#-#")

    def test_the_sweeps_message_gives_the_chat_starts_time_and_the_limit_it_passed(self):
        start = "the day's first chat start did not ask for the day's sweep once; that chat start took "
        self.assertEqual(bench.no_sweep(120.4), start+"120 ms, inside its limits")
        self.assertEqual(bench.no_sweep(289.9), start+"289 ms, inside its limits")
        # Every hook reads its input in budget("") less the margin, before
        # it knows it is a chat start.
        self.assertEqual(bench.no_sweep(290.0), start+"290 ms, past the limit in which a hook reads its input")
        self.assertEqual(bench.no_sweep(1489.9), start+"1489 ms, past the limit in which a hook reads its input")
        self.assertEqual(bench.no_sweep(1490.0), start+"1490 ms, past its limit")

    def stop_output(self, host, out, ms):
        """Home.hook on a hook that printed out in ms milliseconds, made up:
        no process starts and no clock is read."""
        real_run, real_time = core.run_hook, core.time
        clock = iter([1.0, 1.0+ms/1000])
        core.run_hook = lambda argv, payload, env: (0, out, "")
        core.time = type("Clock", (), {"perf_counter": staticmethod(lambda: next(clock))})
        try:
            h = core.Home(Path("/nonexistent/eva-md"), Path("/nonexistent"))
            h.env = {}
            return h.hook(host, "Stop", "s1")
        finally:
            core.run_hook, core.time = real_run, real_time

    def test_an_empty_codex_stop_is_let_through_only_past_its_limit(self):
        out, ms = self.stop_output("codex", "", 300)
        self.assertEqual(out, "")
        self.assertAlmostEqual(ms, 300)
        self.assertEqual(self.stop_output("codex", "{}", 40)[0], "{}")
        self.assertEqual(self.stop_output("codex", "{}", 400)[0], "{}")
        self.assertEqual(self.stop_output("claude-code", "", 40)[0], "")
        for host, out, ms in (("codex", "", 289), ("codex", '{"a": 1}', 400), ("claude-code", "{}", 400)):
            with self.assertRaisesRegex(RuntimeError, "^invalid Stop output$"):
                self.stop_output(host, out, ms)

    # The padded inbox's loops whose outputs it reads, or keeps to count the
    # empty ones: each reads them only through to_read, under this name.
    READ = {"padded_inbox": [("capture", "Stop"), ("prompt_with_pointers", "UserPromptSubmit"), ("capture_with_spawn", "Stop")],
            "code_scenarios": [("code_prompt", "UserPromptSubmit")],
            "recall_scenario": [("prompt_with_recall", "UserPromptSubmit")]}

    def scenario(self, name):
        tree = ast.parse((HERE.parent/"benchmark.py").read_text())
        fn = next(n for n in ast.walk(tree) if isinstance(n, ast.FunctionDef) and n.name == name)
        parents = {child: node for node in ast.walk(fn) for child in ast.iter_child_nodes(node)}
        return fn, parents

    def within(self, node, parents, kind):
        """The innermost node of kind that holds node, or None."""
        while node in parents:
            node = parents[node]
            if isinstance(node, kind):
                return node
        return None

    def timings(self, fn):
        """Each timed hook call in fn, in the order of their lines: its
        assignment, its event and its output's name (None for _)."""
        out = []
        for st in ast.walk(fn):
            if isinstance(st, ast.Assign) and isinstance(st.value, ast.Call) and getattr(st.value.func, "id", None) == "hook" \
                    and isinstance(st.targets[0], ast.Tuple) and [getattr(e, "id", None) for e in st.targets[0].elts][1:] == ["elapsed"]:
                name = st.targets[0].elts[0].id
                out.append((st, ast.literal_eval(st.value.args[1]), None if name == "_" else name))
        return sorted(out, key=lambda t: t[0].lineno)

    def calls(self, fn, name):
        return [n for n in ast.walk(fn) if isinstance(n, ast.Call) and getattr(n.func, "id", None) == name]

    def test_prompts_and_codex_stops_are_read_only_through_to_read(self):
        for scenario, expected in self.READ.items():
            fn, parents = self.scenario(scenario)
            found = []
            for st, event, output in self.timings(fn):
                if event not in ("UserPromptSubmit", "Stop") or output is None:
                    continue
                # In its loop the output is only kept, in one list.
                loop = self.within(st, parents, ast.For)
                uses = [n for n in ast.walk(loop) if isinstance(n, ast.Name) and n.id == output and isinstance(n.ctx, ast.Load)]
                kept = {ast.unparse(self.within(n, parents, ast.Call).func) for n in uses}
                self.assertEqual(len(kept), 1, (scenario, event, kept))
                (append,) = kept
                self.assertTrue(append.endswith(".append"), (scenario, append))
                kept = append[:-len(".append")]
                # The list is read by one to_read, of the hook's event.
                (read,) = [c for c in self.calls(fn, "to_read") if kept in {n.id for n in ast.walk(c.args[2]) if isinstance(n, ast.Name)}]
                self.assertEqual(ast.literal_eval(read.args[1]), event, scenario)
                found.append((ast.literal_eval(read.args[0]), event))
                # And nowhere else, but by the indexes to_read returns.
                loop = parents[read] if isinstance(parents[read], ast.For) and parents[read].iter is read else None
                for n in ast.walk(fn):
                    if not (isinstance(n, ast.Name) and n.id == kept and isinstance(n.ctx, ast.Load)):
                        continue
                    if ast.unparse(parents[n]) == kept+".append" or self.within(n, parents, ast.Call) is read:
                        continue
                    self.assertIsNotNone(loop, (scenario, kept, "read outside to_read's indexes", n.lineno))
                    self.assertIs(self.within(n, parents, ast.For), loop, (scenario, kept, n.lineno))
                    self.assertEqual(ast.unparse(parents[n]), "%s[%s]" % (kept, loop.target.id), (scenario, n.lineno))
            self.assertEqual(found, expected, scenario)

    def test_every_timed_loop_says_how_many_passed_their_limit(self):
        for scenario in self.READ:
            fn, parents = self.scenario(scenario)
            said = {(ast.literal_eval(c.args[1]), n.id) for name in ("say_late", "to_read") for c in self.calls(fn, name)
                    for n in ast.walk(c.args[2]) if isinstance(n, ast.Name)}
            for st, event, output in self.timings(fn):
                loop = self.within(st, parents, ast.For)
                lists = {ast.unparse(c.func.value) for c in ast.walk(loop) if isinstance(c, ast.Call) and getattr(c.func, "attr", None) == "append"
                         and any(isinstance(n, ast.Name) and n.id == "elapsed" for n in ast.walk(c))}
                self.assertTrue(any((event, kept) in said for kept in lists), (scenario, st.lineno, event, lists))

    def test_the_padded_inbox_checks_its_starts_by_the_hooks_it_timed(self):
        fn, parents = self.scenario("padded_inbox")
        self.assertEqual([ast.unparse(c) for c in self.calls(fn, "check_starts")], ["check_starts(started, start_groups(stop_spawn, ends))"])
        # The stand-in's first line is its warm-up's, no hook's start.
        self.assertEqual([ast.unparse(st.value) for st in ast.walk(fn) if isinstance(st, ast.Assign) and ast.unparse(st.targets[0]) == "started"],
                         ["spawned.read_text().splitlines()[1:]"])
        timed = {}
        for st, event, _ in self.timings(fn):
            loop = self.within(st, parents, ast.For)
            for c in ast.walk(loop):
                if isinstance(c, ast.Call) and ast.unparse(c) in ("stop_spawn.append(elapsed)", "ends.append(elapsed)"):
                    timed[ast.unparse(c.func.value)] = (event, ast.unparse(st.value.args[2]))
        self.assertEqual(timed, {"stop_spawn": ("Stop", "'m1-spawn'"), "ends": ("SessionEnd", "'m1-end-' + str(i)")})
        # And the day's first chat start that asks for no sweep fails with
        # its time: the chat start of m1-records whose elapsed it gives.
        (raised,) = [n for n in ast.walk(fn) if isinstance(n, ast.Raise) and "no_sweep" in ast.unparse(n)]
        self.assertEqual(ast.unparse(raised), "raise RuntimeError(no_sweep(elapsed))")
        (timing,) = [st for st, _, _ in self.timings(fn) if st in set(ast.walk(self.within(raised, parents, ast.For)))]
        self.assertEqual((ast.literal_eval(timing.value.args[1]), ast.unparse(timing.value.args[2])), ("SessionStart", "'m1-records-' + str(i)"))
        self.assertLess(timing.lineno, raised.lineno)


class GateLoops(unittest.TestCase):
    """How many times the real scenarios time each hook, read from their
    code: in the gate's runs each timed hook gives at least GATE_RUN_SAMPLES
    a run, and in the plain benchmark as many as it always did."""

    # The hooks the plain benchmark times a run, by the line that times
    # them: its report's sample counts.
    PLAIN = {
        ("benchmark.py", "padded_inbox"): [10, 10, 100, 10, 100, 100, 100, 30, 30],
        ("benchmark.py", "code_scenarios"): [10, 25],
        ("benchmark.py", "recall_scenario"): [100],
        ("real_install.py", "real_install"): [20, 50, 50],
    }

    def tree(self, file):
        path = HERE.parent/file if file == "benchmark.py" else HERE/file
        return ast.parse(path.read_text())

    def function(self, tree, name):
        return next(n for n in ast.walk(tree) if isinstance(n, ast.FunctionDef) and n.name == name)

    def timed(self, fn, env):
        """Each timed hook call in fn, in order: the line it is on and how
        many times a run makes it, the product of its loops' ranges."""
        out = []

        def is_hook(call):
            return isinstance(call, ast.Call) and (getattr(call.func, "id", None) == "hook" or getattr(call.func, "attr", None) == "hook")

        def names(target):
            return {n.id for n in ast.walk(target) if isinstance(n, ast.Name)}

        def timing(st):
            return isinstance(st, ast.Assign) and is_hook(st.value) and "elapsed" in names(st.targets[0])

        def visit(statements, times):
            for st in statements:
                if isinstance(st, (ast.FunctionDef, ast.Lambda)):
                    continue
                if isinstance(st, ast.For):
                    if not any(timing(s) for s in ast.walk(st)):
                        continue
                    self.assertTrue(isinstance(st.iter, ast.Call) and getattr(st.iter.func, "id", None) == "range", ast.dump(st.iter))
                    n = eval(compile(ast.Expression(st.iter.args[0]), "<loop>", "eval"), dict(env))
                    visit(st.body, times*n)
                elif timing(st):
                    out.append((st.lineno, times))
                else:
                    for field in ("body", "orelse", "finalbody"):
                        visit(getattr(st, field, []) or [], times)
                    for h in getattr(st, "handlers", []):
                        visit(h.body, times)
        visit(fn.body, 1)
        return out

    def env(self, gate):
        """What the loops' ranges read: the run, and code_scenarios'
        counts as padded_inbox passes them."""
        run = core.Run(None, None, gate=gate)
        caller = self.function(self.tree("benchmark.py"), "padded_inbox")
        call = next(n for n in ast.walk(caller) if isinstance(n, ast.Call) and getattr(n.func, "id", None) == "code_scenarios")
        callee = self.function(self.tree("benchmark.py"), "code_scenarios")
        params = [a.arg for a in callee.args.args]
        defaults = dict(zip(params[len(params)-len(callee.args.defaults):], callee.args.defaults))
        env = {"run": run, "range": range}
        for i, name in enumerate(params):
            if name in defaults:
                arg = call.args[i] if i < len(call.args) else defaults[name]
                env[name] = eval(compile(ast.Expression(arg), "<arg>", "eval"), {"run": run})
        return env

    def counts(self, gate):
        env = self.env(gate)
        return {key: [n for _, n in self.timed(self.function(self.tree(key[0]), key[1]), env)] for key in self.PLAIN}

    def test_the_plain_benchmark_times_as_many_as_it_did(self):
        self.assertEqual(self.counts(False), self.PLAIN)

    def test_the_gates_runs_time_every_hook_enough(self):
        # Enough that the gate's runs of a binary, at the default rounds,
        # give WHOLE samples even with one of them lost.
        counts = self.counts(True)
        for key, plain in self.PLAIN.items():
            self.assertEqual(counts[key], [max(n, core.GATE_RUN_SAMPLES) for n in plain], key)
            for n in counts[key]:
                self.assertGreaterEqual(n*(2*core.ROUNDS-1), core.WHOLE, key)
        self.assertEqual(core.GATE_RUN_SAMPLES, 20)

    def test_the_gates_runs_take_a_chat_start_untimed_first(self):
        # A run's first chat start is a cold one: in the gate's runs an
        # untimed one comes before the first timed, and in the plain
        # benchmark none does.
        fn = self.function(self.tree("benchmark.py"), "padded_inbox")
        first = min(line for line, _ in self.timed(fn, self.env(True)))
        warm = [n for n in ast.walk(fn) if isinstance(n, ast.If) and ast.unparse(n.test) == "run.gate" and n.lineno < first
                and any(isinstance(c, ast.Call) and getattr(c.func, "id", None) == "hook" and
                        any(isinstance(k, ast.Constant) and k.value == "SessionStart" for k in c.args) for c in ast.walk(n))]
        self.assertEqual(len(warm), 1)
        # And between the chat starts and those with chats a program ran,
        # they remove their extra chat starts' files, lest those push the
        # person's chat out of the 64 a brief reads.
        lines = sorted(line for line, _ in self.timed(fn, self.env(True)))
        removes = [n for n in ast.walk(fn) if isinstance(n, ast.If) and ast.unparse(n.test) == "run.gate" and lines[0] < n.lineno < lines[1]
                   and any(isinstance(c, ast.Call) and getattr(c.func, "attr", None) == "unlink" for c in ast.walk(n))]
        self.assertEqual(len(removes), 1)

    def test_the_gates_runs_remove_exactly_their_extra_chat_starts_files(self):
        # The removal, run on a folder with the files the chat starts before
        # it leave, each named as the inbox names a chat's file: the host,
        # then the session. It leaves the person's chat and the files of
        # the plain benchmark's 10 chat starts, and nothing else of theirs.
        fn = self.function(self.tree("benchmark.py"), "padded_inbox")
        lines = sorted(line for line, _ in self.timed(fn, self.env(True)))
        gated = [n for n in ast.walk(fn) if isinstance(n, ast.If) and ast.unparse(n.test) == "run.gate"]
        warm = next(n for n in gated if n.lineno < lines[0])
        removal = next(n for n in gated if lines[0] < n.lineno < lines[1])
        (call,) = [c for c in ast.walk(warm) if isinstance(c, ast.Call) and getattr(c.func, "id", None) == "hook"]
        timed = next(st for st in ast.walk(fn) if isinstance(st, ast.For) and st.lineno < lines[0] <= st.end_lineno)
        (chat,) = [c for c in ast.walk(timed) if isinstance(c, ast.Call) and getattr(c.func, "id", None) == "hook"]
        self.assertEqual((ast.literal_eval(call.args[0]), ast.literal_eval(chat.args[0])), ("codex", "codex"))
        warm_sid = ast.literal_eval(call.args[2])
        self.assertEqual((warm_sid, ast.unparse(chat.args[2])), ("m1-warm", "'m1-new-' + str(i)"))
        n = self.env(True)["run"].loops(10)
        with tempfile.TemporaryDirectory() as d:
            inbox = Path(d)
            person = inbox/"claude-code-m1-previous.jsonl"
            for sid in [warm_sid] + ["m1-new-%d" % i for i in range(n)]:
                (inbox/("codex-%s.jsonl" % sid)).write_text("{}\n")
            person.write_text("{}\n")
            exec(compile(ast.Module(body=removal.body, type_ignores=[]), "<removal>", "exec"),
                 {"files": [person], "startup": [0.0]*n})
            left = sorted(p.name for p in inbox.iterdir())
        self.assertEqual(left, sorted(["claude-code-m1-previous.jsonl"] + ["codex-m1-new-%d.jsonl" % i for i in range(10)]))

    def test_the_gates_code_prompts_name_systems_the_wiki_has(self):
        # The wiki has systems 0 to 399; each code prompt names three in a
        # row from its n, which must stay among them however many chats a
        # run takes.
        fn = self.function(self.tree("benchmark.py"), "code_scenarios")
        (assign,) = [st for st in ast.walk(fn) if isinstance(st, ast.Assign) and ast.unparse(st.targets[0]) == "n"]
        for gate in (False, True):
            chats = self.env(gate)["prompts_timed"]
            names = [eval(compile(ast.Expression(assign.value), "<n>", "eval"), {"c": c}) for c in range(chats)]
            self.assertTrue(all(300 <= n and n+2 <= 399 for n in names), (gate, names))
        self.assertEqual(chats, max(25, core.GATE_RUN_SAMPLES))


class Scenarios(unittest.TestCase):
    def setUp(self):
        self.saved = core.SCENARIOS[:]
        core.SCENARIOS.clear()

    def tearDown(self):
        core.SCENARIOS[:] = self.saved

    def test_a_scenario_file_adds_itself(self):
        with tempfile.TemporaryDirectory() as d:
            path = Path(d)/"late.py"
            path.write_text("from evabench import scenario\n\n@scenario(900, 'late')\ndef late(run):\n    run.put('late', 1)\n\n"
                            "@scenario(50, 'early')\ndef early(run):\n    run.put('early', 2)\n")
            load("evabench_late", path)
        self.assertEqual([s.name for s in core.ordered()], ["early", "late"])
        run = core.Run(None, None)
        for s in core.ordered():
            s.fn(run)
        self.assertEqual(list(run.report.items()), [("early", 2), ("late", 1)])

    def test_two_scenarios_cannot_share_a_place_or_a_name(self):
        core.scenario(10, "one")(lambda run: None)
        with self.assertRaisesRegex(RuntimeError, "already has that place"):
            core.scenario(10, "two")(lambda run: None)
        with self.assertRaisesRegex(RuntimeError, "already has that name"):
            core.scenario(11, "one")(lambda run: None)

    def test_two_scenarios_cannot_report_one_key(self):
        run = core.Run(None, None)
        run.figure("capture", [3.0, 1.0, 2.0], target=50)
        self.assertEqual(run.report, {"capture_samples": 3, "capture_p95_ms": 2.0, "capture_target_met": True})
        self.assertEqual((run.metrics, run.targets), ({"capture": [3.0, 1.0, 2.0]}, {"capture": 50}))
        with self.assertRaisesRegex(RuntimeError, "two scenarios report capture_samples"):
            run.put("capture_samples", 1)
        with self.assertRaisesRegex(RuntimeError, "two scenarios give the metric capture"):
            run.metric("capture", [1.0])

    def test_the_benchmark_loads_every_scenario_file(self):
        # The files in this folder, loaded as the benchmark loads them.
        bench = load("benchmark_under_test", HERE.parent/"benchmark.py")
        for name in [n for n in sys.modules if n.startswith("evabench_")]:
            del sys.modules[name]
        try:
            names = [s.name for s in bench.load().ordered()]
        finally:
            sys.modules["evabench"] = core
        self.assertEqual(names, ["process-start", "real-install", "turn-of-50-tool-calls"])
        # Tests and simulations in the folder are not loaded as scenarios.
        self.assertEqual(sorted(n for n in sys.modules if n.startswith("evabench_")),
                         ["evabench_process_start", "evabench_real_install", "evabench_turn"])

    def test_launch_paths(self):
        self.assertEqual(core.launch(Path("/opt/it's/eva-md"), "codex"),
                         ["/bin/sh", "-c", "'/opt/it'\"'\"'s/eva-md' hook --host codex || { echo '{}'; exit 1; }"])
        self.assertEqual(core.launch(Path("/opt/eva-md"), "claude-code"), ["/opt/eva-md", "hook", "--host", "claude-code"])


if __name__ == "__main__":
    unittest.main()
