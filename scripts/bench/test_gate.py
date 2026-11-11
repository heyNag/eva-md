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
        d = core.decide(runs_of("capture"), {"capture": 100})
        self.assertEqual((d["verdict"], d["exit"], d["failures"], d["unsettled"], d["too_few_samples"]), ("pass", 0, [], [], []))
        m = d["metrics"]["capture"]
        self.assertEqual((m["n_old"], m["n_new"], m["ceiling_ms"], m["controls"], m["judged"]), (400, 400, 100, 36, True))
        self.assertEqual(m["n_runs"], {role: [100, 100] for role in core.ROLES})
        self.assertLessEqual(m["noise"], 1.15)

    def test_a_20_ms_delay_fails(self):
        d = core.decide(runs_of("capture", add=20), {"capture": 100})
        self.assertEqual((d["verdict"], d["exit"]), ("fail", 1))
        self.assertEqual(len(d["failures"]), 1)
        self.assertIn("capture: p95 ratio's lower bound", d["failures"][0])
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
                d = core.decide(parts, {"capture": 100})
                m = d["metrics"]["capture"]
                self.assertGreater(m["ratio_low"], 1.15, (runs, factor))
                self.assertNotEqual(d["verdict"], "fail", (runs, factor, m))
                self.assertEqual((d["verdict"], d["too_noisy"], d["machine_too_noisy"]), ("not judged", ["capture"], False), (runs, factor))
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
                d = core.decide(runs_of("capture", loaded(range(i, j+1), 1.6)), {"capture": 100})
                self.assertNotEqual(d["verdict"], "fail", (i, j))

    def test_slower_old_runs_or_quicker_runs_fail_nothing(self):
        for k in range(1, 9):
            for factor in (2, 0.5):
                if factor == 2 and PLACES[k-1][0] == "new":
                    continue
                d = core.decide(runs_of("capture", loaded([k], factor)), {"capture": 100})
                self.assertNotEqual(d["verdict"], "fail", (k, factor))
                self.assertEqual(d["failures"], [])

    def test_a_30_percent_rise_with_a_little_noise_fails_though_another_figure_is_unsettled(self):
        # Capture rose 30%, and its new runs differ by 4%, which the
        # controls show; prompt rose 25%, but half its new runs only 12%,
        # so the controls put 1.116 between them: it is unsettled, and
        # stops nothing.
        capture = runs_of("capture", {"new": (1.3*1.04, 1.3*1.04, 1.3, 1.3)}, spread=0.02)
        prompt = runs_of("prompt", {"new": (1.25, 1.25, 1.12, 1.12)}, spread=0.02, seed=50)
        d = core.decide(merge(capture, prompt), {"capture": 100, "prompt": 100})
        c, p = d["metrics"]["capture"], d["metrics"]["prompt"]
        self.assertTrue(1.02 < c["noise"] < 1.06, c["noise"])
        self.assertTrue(1.05 < p["noise"] <= 1.15 and 1.15 < p["ratio_low"] <= 1.15*p["noise"], p)
        self.assertEqual(p["reasons"], [])
        self.assertTrue(p["unsettled"])
        self.assertEqual((d["machine_too_noisy"], d["too_noisy"]), (False, []))
        self.assertEqual((d["verdict"], d["exit"]), ("fail", 1))
        self.assertEqual([f.split(":")[0] for f in d["failures"]], ["capture"])
        # Alone, the unsettled figure leaves the gate not judged.
        d = core.decide(prompt, {"prompt": 100})
        self.assertEqual((d["verdict"], d["exit"], d["failures"], d["machine_too_noisy"]), ("not judged", 0, [], False))

    def test_one_figure_too_noisy_stops_nothing_and_two_stop_the_verdict(self):
        # Load slowed one run of each binary's startup: the gate's
        # comparison is untouched, but some controls put the two runs on
        # one side, so startup is too noisy to judge. It cannot fail, and
        # stops nothing: capture's rise of 20 ms fails the gate.
        capture = runs_of("capture", add=20)
        startup = runs_of("startup", {"old": (1, 1, 1, 1.5), "new": (1.5, 1, 1, 1)}, n=34, centre=100, seed=50)
        d = core.decide(merge(capture, startup), {"capture": 100, "startup": 600})
        self.assertGreater(d["metrics"]["startup"]["noise"], 1.15)
        self.assertEqual((d["verdict"], d["exit"], d["machine_too_noisy"], d["too_noisy"]), ("fail", 1, False, ["startup"]))
        self.assertEqual([f.split(":")[0] for f in d["failures"]], ["capture"])
        self.assertEqual([u.split(":")[0] for u in d["unsettled"]], ["startup"])
        self.assertIn("too noisy to judge", d["unsettled"][0])
        # Alone, it leaves the gate not judged.
        d = core.decide(merge(runs_of("capture"), startup), {"capture": 100, "startup": 600})
        self.assertEqual((d["verdict"], d["exit"], d["machine_too_noisy"]), ("not judged", 0, False))
        # Two figures too noisy to judge say the machine was too noisy, and
        # stop the verdict, capture's rise and all.
        prompt = runs_of("prompt", {"old": (1.5, 1, 1, 1), "new": (1, 1, 1.5, 1)}, centre=30, seed=90)
        d = core.decide(merge(capture, startup, prompt), {"capture": 100, "startup": 600, "prompt": 100})
        self.assertEqual((d["verdict"], d["exit"], d["machine_too_noisy"], d["too_noisy"]), ("not judged", 0, True, ["prompt", "startup"]))
        self.assertEqual([f.split(":")[0] for f in d["failures"]], ["capture"])
        self.assertEqual(core.NOISY_FIGURES, 2)

    def test_a_figure_with_too_few_samples_neither_fails_nor_stops(self):
        # 99 samples on the new binary's side: startup doubled, and its
        # runs differ by half, but it is only listed.
        few = {"old": (25,)*4, "new": (25, 25, 25, 24)}  # no scenario failed
        startup = runs_of("startup", {"old": (1, 1, 1, 1.5), "new": (2, 3, 2, 2)}, n=few, centre=100, seed=50)
        for capture, verdict in ((runs_of("capture"), "pass"), (runs_of("capture", add=20), "fail")):
            d = core.decide(merge(capture, startup), {"capture": 100, "startup": 120})
            m = d["metrics"]["startup"]
            self.assertEqual((d["verdict"], d["machine_too_noisy"], d["too_few_samples"]), (verdict, False, ["startup"]))
            self.assertEqual((m["n_new"], m["too_few_samples"], m["judged"], m["reasons"], m["unsettled"]), (99, True, False, [], []))
            self.assertGreater(m["noise"], 1.15)
            self.assertGreater(m["p95_low_ms"], 120)
            self.assertFalse(any(f.startswith("startup") for f in d["failures"]))
        # With 100 it is judged, and fails.
        startup = runs_of("startup", {"new": (2, 2, 2, 2)}, n=25, centre=100, seed=50)
        d = core.decide(startup, {"startup": 600})
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
            m = core.decide(parts, {"capture": 100})["metrics"]["capture"]
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
        self.assertEqual(core.decide(parts, {"capture": 100})["metrics"]["capture"]["noise"], round(bounds[-4], 3))
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
        d = core.decide(parts(11.5), {"capture": 100})
        self.assertEqual((d["metrics"]["capture"]["noise"], d["verdict"], d["too_noisy"]), (1.15, "pass", []))
        d = core.decide(parts(11.6), {"capture": 100})
        self.assertEqual((d["verdict"], d["too_noisy"]), ("not judged", ["capture"]))

    def test_the_threshold_is_strict(self):
        old = {"old": [{"capture": [10.0]*100}]*2, "old_again": [{"capture": [10.0]*100}]*2}
        for new, verdict in ((11.5, "pass"), (11.6, "fail")):
            parts = dict(old, new=[{"capture": [new]*100}]*2, new_again=[{"capture": [new]*100}]*2)
            self.assertEqual(core.decide(parts, {"capture": 100})["verdict"], verdict, new)

    def constant(self, old, new, name="capture"):
        """Each binary's runs, the samples of each run all one value."""
        out = {}
        for binary, values in (("old", old), ("new", new)):
            for i, role in enumerate(core.ROLES_OF[binary]):
                out[role] = [{name: [float(v)]*100} for v in values[2*i:2*i+2]]
        return out

    def test_passing_the_ceiling_fails(self):
        # A rise too small for the ratio, past the ceiling.
        d = core.decide(self.constant([95]*4, [102]*4), {"capture": 100})
        self.assertEqual((d["verdict"], d["exit"]), ("fail", 1))
        self.assertEqual(d["failures"], ["capture: p95 102.0 ms, at least 102.0 ms, passed the ceiling of 100 ms"])
        self.assertEqual(core.decide(self.constant([95]*4, [102]*4), {"capture": None})["verdict"], "pass")
        self.assertEqual(core.decide(self.constant([95]*4, [100]*4), {"capture": 100})["verdict"], "pass")

    def test_a_figure_at_its_ceiling_by_sampling_alone_does_not_fail(self):
        # Both binaries' samples from one law whose p95 is the ceiling: the
        # new binary's p95 lands above it, and the old one's below, in about
        # a quarter of the seeds, but its interval reaches below it.
        over = 0
        for seed in range(0, 240, 8):
            parts = runs_of("capture", centre=100/1.045, spread=0.05, seed=seed)
            d = core.decide(parts, {"capture": 100})
            m = d["metrics"]["capture"]
            if m["p95_ms"] > 100 >= m["p95_base_ms"]:
                over += 1
                self.assertLess(m["p95_low_ms"], 100)
            self.assertEqual(d["verdict"], "pass", (seed, m))
        self.assertGreaterEqual(over, 3)

    def test_a_ceiling_the_old_binary_was_over_already(self):
        d = core.decide(self.constant([105]*4, [110]*4), {"capture": 100})
        self.assertEqual((d["verdict"], d["failures"]), ("pass", []))

    def test_the_controls_have_no_ceiling(self):
        # Runs of the new binary on either side of the ceiling show where
        # the binary sits, not that the machine is noisy: the gate fails.
        d = core.decide(self.constant([95]*4, [99, 101, 99, 101]), {"capture": 100})
        self.assertEqual((d["verdict"], d["exit"], d["unsettled"]), ("fail", 1, []))
        self.assertEqual(d["failures"], ["capture: p95 101.0 ms, at least 101.0 ms, passed the ceiling of 100 ms"])

    def test_a_large_rise_fails(self):
        # Capture from about 20 ms to about 90, and prompt doubled: both
        # fail, whatever seed made the samples.
        for seed in range(0, 160, 16):
            capture = runs_of("capture", {"new": (4.5,)*4}, seed=seed)
            prompt = runs_of("prompt", {"new": (2,)*4}, centre=10, seed=seed+8)
            d = core.decide(merge(capture, prompt), {"capture": 100, "prompt": 100})
            self.assertEqual((d["verdict"], d["exit"], d["unsettled"]), ("fail", 1, []), seed)
            self.assertEqual(sorted({f.split(":")[0] for f in d["failures"]}), ["capture", "prompt"])

    def test_a_figure_the_old_binary_gave_no_samples_is_not_compared(self):
        parts = runs_of("capture")
        for run in parts["new"] + parts["new_again"]:
            run["code_brief"] = [5.0]
        d = core.decide(parts, {"capture": 100, "code_brief": 600})
        self.assertEqual((d["verdict"], d["not_compared"], list(d["metrics"])), ("pass", ["code_brief"], ["capture"]))

    def test_a_figure_the_new_binary_gave_no_samples_fails(self):
        parts = runs_of("capture")
        for run in parts["old"] + parts["old_again"]:
            run["code_brief"] = [5.0]*10
        d = core.decide(parts, {"capture": 100, "code_brief": 600})
        self.assertEqual((d["verdict"], d["exit"]), ("fail", 1))
        self.assertEqual(d["failures"], ["code_brief: the new binary gave no samples, the old one 40"])
        # Even when the machine was too noisy to judge a ratio.
        noisy = merge(parts, runs_of("startup", {"old": (1, 1, 1, 1.5), "new": (1.5, 1, 1, 1)}, n=25, centre=100, seed=50),
                      runs_of("prompt", {"old": (1.5, 1, 1, 1), "new": (1, 1, 1.5, 1)}, centre=30, seed=90))
        d = core.decide(noisy, {"capture": 100, "code_brief": 600, "startup": 600, "prompt": 100})
        self.assertTrue(d["machine_too_noisy"])
        self.assertEqual((d["verdict"], d["exit"]), ("fail", 1))
        # Unless the new binary lost a scenario in every run: the scenarios'
        # rule judges that, and the figure has too few samples.
        lost = {role: ({"code": ["RuntimeError: a brief did not say it"]*2} if core.BINARY[role] == "new" else {})
                for role in core.ROLES}
        d = core.decide(parts, {"capture": 100, "code_brief": 600}, lost)
        self.assertEqual(d["too_few_samples"], ["code_brief"])
        self.assertEqual(d["failures"], ["the new binary failed the scenario code in 4 of 4 runs, the old one in 0"])
        # A scenario lost in some runs alone explains nothing.
        lost = {role: ({"code": ["RuntimeError: a brief did not say it"]} if role == "new" else {}) for role in core.ROLES}
        d = core.decide(parts, {"capture": 100, "code_brief": 600}, lost)
        self.assertEqual((d["verdict"], d["too_few_samples"]), ("fail", []))
        self.assertEqual(d["failures"], ["code_brief: the new binary gave no samples, the old one 40"])

    def test_the_same_samples_give_the_same_verdict(self):
        parts = merge(runs_of("capture", {"new": (1.1, 1.2, 1.1, 1.2)}), runs_of("startup", n=25, centre=100, seed=40))
        shuffled = json.loads(json.dumps(parts))
        for runs in shuffled.values():
            for run in runs:
                for values in run.values():
                    random.Random(len(values)).shuffle(values)
        self.assertEqual(core.decide(parts, {"capture": 100, "startup": 600}),
                         core.decide(shuffled, {"capture": 100, "startup": 600}))

    def test_a_role_must_have_a_run_a_round(self):
        parts = runs_of("capture")
        parts["new"] = parts["new"][:1]
        with self.assertRaisesRegex(ValueError, "the role new has 1 runs, not 2"):
            core.decide(parts, {"capture": 100})


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
        d1, d3 = core.decide(at, {"capture": None}), core.decide(tripled, {"capture": None})
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

    def test_the_runs_levels_and_one_quick_run(self):
        # Two figures; the new binary's run 2 is quick in both, by 1.25.
        a = [[10.0]*5, [12.5]*5, [12.5]*5, [12.5]*5]
        b = [[40.0]*5, [50.0]*5, [51.0]*5, [49.0]*5]
        self.assertEqual(core.run_levels([a, b]), [0.8, 1.0, 1.0, 1.0])
        self.assertTrue(core.one_quick_run(core.run_levels([a, b])))
        # In one figure only, the median over figures does not see it.
        c = [[30.0]*5, [30.0]*5, [31.0]*5, [29.0]*5]
        self.assertFalse(core.one_quick_run(core.run_levels([a, c, c])))
        # 1.15 is not more than 1.15.
        self.assertFalse(core.one_quick_run([1.0, 1.15, 1.2, 1.3]))
        self.assertTrue(core.one_quick_run([1.0, 1.151, 1.2, 1.3]))
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
            d = core.decide(parts, {"capture": 100, "prompt": 100})
            self.assertEqual((d["verdict"], d["one_quick_run"], d["machine_too_noisy"]), (verdict, quick, verdict == "not judged"), levels)
            self.assertEqual(d["too_noisy"], [])
            self.assertEqual(sorted({f.split(":")[0] for f in d["failures"]}), ["capture", "prompt"])
        # Figures with too few samples to judge count for no run: two of
        # them with a quick run of the new binary leave the gate a pass.
        few = [runs_of(name, {"new": (0.7, 1, 1, 1)}, n=10, spread=0.05, centre=50, seed=60+k) for k, name in enumerate(("a", "b"))]
        parts = merge(runs_of("capture", spread=0.05), runs_of("prompt", spread=0.05, centre=30, seed=40), *few)
        d = core.decide(parts, {"capture": 100, "prompt": 100, "a": None, "b": None})
        self.assertEqual((d["verdict"], d["one_quick_run"], d["too_few_samples"]), ("pass", [], ["a", "b"]))
        # Three of the old binary's runs loaded: as noisy.
        parts = runs_of("capture", {"old": (1.4, 1.4, 1, 1.4)}, spread=0.05)
        d = core.decide(parts, {"capture": 100})
        self.assertEqual((d["verdict"], d["one_quick_run"]), ("not judged", ["old"]))


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
        # Chat start doubled, and one run of the new binary lost: 3 runs of
        # 34 still judge it, and it fails.
        for lose in (("new", 0), ("new_again", 1), ("old", 1), ("old_again", 0)):
            parts, failures = self.startup(core.GATE_RUN_SAMPLES, {"new": (2,)*4}, lose)
            d = core.decide(parts, {"startup": 600}, failures, sources={"startup": "padded-inbox"})
            self.assertEqual((d["verdict"], d["too_few_samples"], d["short_by_failures"]), ("fail", [], []), lose)
            self.assertEqual(min(d["metrics"]["startup"]["n_old"], d["metrics"]["startup"]["n_new"]), 3*34)

    def test_a_figure_short_only_by_lost_runs_is_not_judged(self):
        # At 25 a run, as the gate took before, one lost run leaves 75: the
        # figure is listed as too few, and the gate is not judged, not a
        # pass, whichever binary lost it.
        for lose in (("new", 0), ("old_again", 1)):
            parts, failures = self.startup(25, {"new": (2,)*4}, lose)
            d = core.decide(parts, {"startup": 600}, failures, sources={"startup": "padded-inbox"})
            self.assertEqual((d["verdict"], d["exit"]), ("not judged", 0), lose)
            self.assertEqual((d["too_few_samples"], d["short_by_failures"]), (["startup"], ["startup"]))
            self.assertEqual(d["failures"], [])
            self.assertIn("startup has too few samples because a scenario failed in some runs: run the gate again", d["lost"])
        # With nothing lost, 25 a run is 100 a side, and it fails.
        parts, failures = self.startup(25, {"new": (2,)*4})
        self.assertEqual(core.decide(parts, {"startup": 600}, failures)["verdict"], "fail")

    def test_a_figure_short_by_design_is_only_listed(self):
        # 20 a run can never make 100 a side at two rounds: listed, and the
        # gate passes on the rest.
        parts = merge(runs_of("capture"), runs_of("startup", {"new": (2,)*4}, n=20, centre=100, seed=70))
        d = core.decide(parts, {"capture": 100, "startup": 600})
        self.assertEqual((d["verdict"], d["too_few_samples"], d["short_by_failures"]), ("pass", ["startup"], []))

    def test_the_old_binarys_side_needs_enough_too(self):
        # 99 on the old binary's side and 100 on the new one's.
        parts = runs_of("startup", {"new": (2,)*4}, n={"old": (25, 25, 25, 24), "new": (25,)*4}, centre=100, seed=70)
        d = core.decide(parts, {"startup": 600})
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
        d = core.decide(parts, {"capture": 100, "startup": 600}, failures, sources=sources)
        self.assertEqual((d["verdict"], d["exit"]), ("fail", 1))
        self.assertEqual(d["failures"], ["startup: the new binary gave no samples, the old one 136"])
        # When startup's own scenario is the one lost in every new run, the
        # scenarios' rule judges it: here it decides nothing, and the
        # figure is short only by the lost runs, so the gate is not judged.
        sources["startup"] = "recall"
        d = core.decide(parts, {"capture": 100, "startup": 600}, failures, sources=sources)
        self.assertEqual((d["verdict"], d["failures"], d["too_few_samples"], d["short_by_failures"]),
                         ("not judged", [], ["startup"], ["startup"]))
        # A gate.json from before the scenarios were recorded: any scenario
        # lost in every new run excuses the missing figure.
        d = core.decide(parts, {"capture": 100, "startup": 600}, failures)
        self.assertEqual((d["verdict"], d["failures"]), ("not judged", []))

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
        d = core.decide(self.constant([100]*4, [110]*4), {"capture": 100})
        self.assertEqual(d["failures"], ["capture: p95 110.0 ms, at least 110.0 ms, passed the ceiling of 100 ms"])
        d = core.decide(self.constant([100.001]*4, [110]*4), {"capture": 100})
        self.assertEqual((d["verdict"], d["failures"]), ("pass", []))

    def test_a_ceiling_is_passed_whatever_the_noise_below_the_threshold(self):
        # The new binary's runs differ by 10%, so its noise is about 1.1:
        # the rise is too small for the ratio, but every run is past the
        # ceiling.
        d = core.decide(self.constant([95]*4, [101, 111, 101, 111]), {"capture": 100})
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
        self.assertEqual(core.Run(None, None, gate=True).loops(10), 34)
        self.assertEqual(core.Run(None, None, gate=True).loops(30), 34)
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

    def gate(self, rounds=2, ceiling_factor=2.0):
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
                         {role: [100, 100] for role in core.ROLES})
        self.assertEqual(report["ceilings"], {"capture": 100})

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
        self.assertIn("capture                      old %8.1f ms  new %8.1f ms  ratio %6s  lower bound %6s  noise %6s" % (
            m["p95_base_ms"], m["p95_ms"], m["ratio"], m["ratio_low"], m["noise"]), out)

    def test_the_gates_runs_take_more_samples_of_a_few_hooks(self):
        seen = []

        @core.scenario(100, "chat-start")
        def chat_start(run):
            seen.append((run.gate, run.loops(10)))
            run.figure("startup", samples(len(seen), run.loops(10), 100), target=300)
        code, out, report = self.gate()
        self.assertEqual(seen, [(True, 34)]*8)
        self.assertEqual(report["decision"]["metrics"]["startup"]["n_new"], 136)
        self.assertEqual(report["figure_scenarios"], {"startup": "chat-start"})
        self.assertEqual(report["decision"]["too_few_samples"], [])
        self.assertEqual(bench.run_once(core, Path("eva-md-old"), self.path).report["startup_samples"], 10)
        self.assertEqual(seen[-1], (False, 10))

    def test_a_scenario_the_new_binary_always_fails(self):
        self.scenario(lambda which, k: RuntimeError("hook failed or wrote diagnostics") if which == "new" else samples(k, 100, 20))
        code, out, report = self.gate()
        self.assertEqual((code, report["verdict"]), (1, "fail"), out)
        self.assertEqual(report["decision"]["failures"], ["the new binary failed the scenario fake in 4 of 4 runs, the old one in 0"])
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
            got, out, report = self.gate()
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
                         {"old": [30, 30], "new": [0, 30], "old_again": [30, 30], "new_again": [30, 30]})
        self.assertEqual(report["decision"]["metrics"]["capture"]["n_runs"]["new"], [0, 30])
        self.assertEqual(report["scenario_failures"]["new"], {"fake": ["RuntimeError: invalid Stop output"]})

    def test_any_error_in_a_scenario_is_a_failed_run(self):
        # A scenario that reads what only the new binary writes raises a
        # KeyError on the old one: the old binary failed it, and the gate
        # still runs and decides.
        self.scenario(lambda which, k: KeyError("project") if which == "old" else samples(k, 100, 20))
        self.scenario(lambda which, k: samples(k, 100, 20), place=200, name="other")
        code, out, report = self.gate()
        self.assertEqual((code, report["verdict"], report["decision"]["not_compared"]), (0, "pass", ["capture"]), out)
        self.assertEqual(report["scenario_failures"]["old"], {"fake": ["KeyError: 'project'"]*2})

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
        self.scenario(lambda which, k: samples(k, 20, 20 if which == "old" else 40))
        code, out, report = self.gate()
        self.assertEqual((code, report["verdict"], report["decision"]["too_few_samples"]), (0, "pass", ["capture"]), out)
        self.assertIn("too few samples to judge, fewer than 100 a binary", out)

    def test_a_noisy_machine_is_not_judged(self):
        # One run of each binary slowed by half: the controls see it, in
        # one figure and then in two.
        slow = lambda which, k, at: samples(k, 50, 20*(1.5 if (which, k) in at else 1))
        self.scenario(lambda which, k: slow(which, k, (("old", 4), ("new", 1))))
        code, out, report = self.gate()
        self.assertEqual((code, report["verdict"], report["decision"]["machine_too_noisy"], report["decision"]["too_noisy"]),
                         (0, "not judged", False, ["capture"]), out)
        self.assertTrue(out.endswith("latency gate: not judged (a figure's runs differed by more than 1.15, too much to judge it: "
                                     "run the gate again)\n"), out)

    def test_two_noisy_figures_say_the_machine_was_too_noisy(self):
        # Each binary's run index is k counted over both scenarios.
        at = (("old", 4), ("new", 1))
        slow = lambda which, k: samples(k, 50, 20*(1.5 if (which, (k+1)//2) in at else 1))
        self.scenario(slow)
        self.scenario(slow, place=200, name="startup")
        code, out, report = self.gate()
        self.assertEqual((code, report["verdict"], report["decision"]["machine_too_noisy"]), (0, "not judged", True), out)
        self.assertTrue(out.endswith("latency gate: not judged (runs differed by more than 1.15 in 2 figures or more, or one run "
                                     "was that much quicker than the rest: the machine was too noisy to judge)\n"), out)

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
                         {"old": [10.0, 40.0], "new": [11.0, 41.0], "old_again": [20.0, 30.0], "new_again": [21.0, 31.0]})

    def test_the_baseline_holds_the_new_binarys_runs(self):
        self.scenario(lambda which, k: samples(k, 100, 20 if which == "old" else 30))
        code, out, report = self.gate()
        base = json.loads((self.path/"base.json").read_text())
        self.assertEqual((base["runs"], base["commit"], base["metrics"]["capture"]["n"]), (4, "0123abc", 400))
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

    def test_stop_kills_every_process_a_hook_started(self):
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
            self.assertEqual([len(run[name]) for role in core.ROLES for run in samples_by_role[role]], [n]*8, name)
            self.assertGreaterEqual(3*n, core.WHOLE, name)
        self.assertEqual(sorted(sim.CHANGES), ["all-x1.2", "all-x1.3", "all-x1.5", "all-x2", "all-x3", "capture+20ms",
                                               "startup-x1.5", "startup-x2", "unchanged"])
        self.assertEqual(sorted(sim.LOADS), ["drift", "heavy", "light", "new-3", "new-4", "new-pair", "none", "runs"])
        # A change falls on the new binary's runs alone.
        base = sim.made_up(random.Random(2))
        x = sim.one(base, sim.LOADS["none"], sim.CHANGES["all-x1.5"], random.Random(3))
        y = sim.one(base, sim.LOADS["none"], sim.CHANGES["unchanged"], random.Random(3))
        self.assertEqual(x["old"], y["old"])
        self.assertEqual(x["new"][0]["capture"], [v*1.5 for v in y["new"][0]["capture"]])

    def test_a_saved_gate_that_lost_a_run_is_not_drawn(self):
        whole = {role: [{"capture": [1.0]}, {"capture": [2.0]}] for role in core.ROLES}
        lost = {role: [{"capture": [1.0]}, {}] for role in core.ROLES}
        base = sim.from_gates([{"samples": whole}, {"samples": lost}])
        for seed in range(10):
            self.assertEqual(sorted(run["capture"][0] for run in base(random.Random(seed))), [1.0]*4+[2.0]*4)


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
        self.assertEqual(core.GATE_RUN_SAMPLES, 34)

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
        try:
            names = [s.name for s in bench.load().ordered()]
        finally:
            sys.modules["evabench"] = core
        self.assertEqual(names, ["process-start", "real-install", "turn-of-50-tool-calls"])

    def test_launch_paths(self):
        self.assertEqual(core.launch(Path("/opt/it's/eva-md"), "codex"),
                         ["/bin/sh", "-c", "'/opt/it'\"'\"'s/eva-md' hook --host codex || { echo '{}'; exit 1; }"])
        self.assertEqual(core.launch(Path("/opt/eva-md"), "claude-code"), ["/opt/eva-md", "hook", "--host", "claude-code"])


if __name__ == "__main__":
    unittest.main()
