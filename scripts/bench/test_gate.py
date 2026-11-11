"""Tests of the latency gate's decision, of its plumbing on stub binaries and
made-up scenarios, of how a hook is stopped, and of the scenario list. The
decision's tests feed numbers and read no clock; the hook tests wait for a
process only up to a hang guard: python3 -I scripts/bench/test_gate.py"""
import argparse
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
        base, cand = samples(1, 120, 20), samples(2, 80, 22)
        draws = core.ratios(base, cand)
        self.assertEqual(len(draws), core.DRAWS)
        self.assertEqual(draws, sorted(draws))
        self.assertEqual(core.interval(base, cand), (draws[50], draws[core.DRAWS-51]))

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
                self.assertEqual((d["verdict"], d["machine_too_noisy"]), ("not judged", True), (runs, factor))
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
        self.assertFalse(d["machine_too_noisy"])
        self.assertEqual((d["verdict"], d["exit"]), ("fail", 1))
        self.assertEqual([f.split(":")[0] for f in d["failures"]], ["capture"])
        # Alone, the unsettled figure leaves the gate not judged.
        d = core.decide(prompt, {"prompt": 100})
        self.assertEqual((d["verdict"], d["exit"], d["failures"], d["machine_too_noisy"]), ("not judged", 0, [], False))

    def test_a_noisy_figure_stops_the_verdict(self):
        # Load slowed one run of each binary's startup: the gate's
        # comparison is untouched, but some controls put the two runs on
        # one side, so the machine was too noisy to judge capture's rise.
        capture = runs_of("capture", add=20)
        startup = runs_of("startup", {"old": (1, 1, 1, 1.5), "new": (1.5, 1, 1, 1)}, n=25, centre=100, seed=50)
        d = core.decide(merge(capture, startup), {"capture": 100, "startup": 600})
        self.assertGreater(d["metrics"]["startup"]["noise"], 1.15)
        self.assertEqual((d["verdict"], d["exit"], d["machine_too_noisy"]), ("not judged", 0, True))
        self.assertEqual([f.split(":")[0] for f in d["failures"]], ["capture"])
        self.assertEqual([u.split(":")[0] for u in d["unsettled"]], ["startup"])
        self.assertIn("the machine was too noisy", d["unsettled"][0])

    def test_a_figure_with_too_few_samples_neither_fails_nor_stops(self):
        # 99 samples on the new binary's side: startup doubled, and its
        # runs differ by half, but it is only listed.
        few = {"old": (25,)*4, "new": (25, 25, 25, 24)}
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
        noisy = merge(parts, runs_of("startup", {"old": (1, 1, 1, 1.5), "new": (1.5, 1, 1, 1)}, n=25, centre=100, seed=50))
        d = core.decide(noisy, {"capture": 100, "code_brief": 600, "startup": 600})
        self.assertTrue(d["machine_too_noisy"])
        self.assertEqual((d["verdict"], d["exit"]), ("fail", 1))
        # Unless the new binary lost a scenario in every run: the scenarios'
        # rule judges that, and the figure has too few samples.
        lost = {role: ({"code": ["RuntimeError: a brief did not say it"]*2} if core.BINARY[role] == "new" else {})
                for role in core.ROLES}
        d = core.decide(parts, {"capture": 100, "code_brief": 600}, lost)
        self.assertEqual(d["too_few_samples"], ["code_brief"])
        self.assertEqual(d["failures"], ["the new binary failed the scenario code in 4 of 4 runs, the old one in 0"])

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
        self.assertNotEqual(s, core.splits(3, seed=core.SEED+1))
        self.assertEqual(core.splits(3, seed=core.SEED+1)[:2], s[:2])

    def test_three_rounds_decide(self):
        parts = {role: [{"capture": samples(10*k+i, 50, 20)} for i in range(3)] for k, role in enumerate(core.ROLES)}
        d = core.decide(parts, {"capture": 100}, rounds=3)
        self.assertEqual((d["verdict"], d["metrics"]["capture"]["controls"], d["metrics"]["capture"]["n_new"]), ("pass", 72, 300))

    def test_the_gate_takes_enough_samples_at_the_default_rounds(self):
        self.assertGreaterEqual(core.GATE_RUN_SAMPLES*2*core.ROUNDS, core.WHOLE)
        self.assertEqual(core.Run(None, None).loops(10), 10)
        self.assertEqual(core.Run(None, None, gate=True).loops(10), 25)
        self.assertEqual(core.Run(None, None, gate=True).loops(30), 30)


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
        noisy = runs_of("capture", {"old": (1, 1, 1, 1.5), "new": (1.5, 1, 1, 1)})
        d = core.decide(noisy, {"capture": 100}, self.failures(new=2, new_again=1), 2)
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
        code, out, report = self.gate()
        again = core.redecide(report)
        self.assertEqual(json.loads(json.dumps(again)), report["decision"])
        self.assertEqual((again["verdict"], again["exit"]), (report["verdict"], code))
        self.assertIn("latency gate: %s" % again["verdict"], out.splitlines()[-1])
        # Every sample is kept as it was taken: new_again's second run is the
        # new binary's third, in which capture was the fifth scenario it ran.
        self.assertEqual(report["samples"]["new_again"][1]["capture"], figures("new", 5))
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
        self.assertEqual(seen, [(True, 25)]*8)
        self.assertEqual(report["decision"]["metrics"]["startup"]["n_new"], 100)
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
        # One run of each binary slowed by half: the controls see it.
        self.scenario(lambda which, k: samples(k, 50, 20*(1.5 if (which, k) in (("old", 4), ("new", 1)) else 1)))
        code, out, report = self.gate()
        self.assertEqual((code, report["verdict"], report["decision"]["machine_too_noisy"]), (0, "not judged", True), out)
        self.assertTrue(out.endswith("latency gate: not judged (runs of both binaries that a change cancels out of differed "
                                     "by more than 1.15: the machine was too noisy to judge)\n"), out)

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

    GUARD = 60  # seconds: a hang guard, never what decides a test

    def setUp(self):
        self.dir = tempfile.TemporaryDirectory()
        self.path = Path(self.dir.name)
        self.pids = self.path/"pids"
        self.stub = self.path/"eva-md"
        self.stub.write_text('#!/bin/sh\nsleep 600 &\necho "$$ $!" > "$PIDS.tmp"\nmv "$PIDS.tmp" "$PIDS"\nwait\n')
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
        samples_by_role = sim.one(sim.made_up, sim.LOADS["none"], sim.CHANGES["unchanged"], rng)
        for name, n, *_ in sim.FIGURES:
            self.assertEqual([len(run[name]) for role in core.ROLES for run in samples_by_role[role]], [n]*8, name)
            self.assertGreaterEqual(4*n, core.WHOLE, name)
        self.assertEqual(sorted(sim.CHANGES), ["all-x1.2", "all-x1.3", "capture+20ms", "unchanged"])
        self.assertEqual(sorted(sim.LOADS), ["drift", "heavy", "light", "new-pair", "none", "runs"])


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
