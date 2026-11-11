"""Tests of the latency gate's decision, of its plumbing on stub binaries and
made-up scenarios, and of the scenario list. They feed numbers and read no
clock: python3 -I scripts/bench/test_gate.py"""
import argparse
import contextlib
import importlib.util
import io
import json
from pathlib import Path
import random
import subprocess
import sys
import tempfile
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

    def test_bounds_the_ratio_from_below(self):
        base = samples(1, 300, 20)
        # Both sides are resampled, so the bound sits below the ratio of the
        # p95s, and equals it only when no draw can move either.
        low = core.ratio_low(base, [x*1.5 for x in base])
        self.assertTrue(1.3 < low < 1.5, low)
        self.assertEqual(core.ratio_low([20.0]*50, [30.0]*50), 1.5)
        self.assertLess(core.ratio_low(base, samples(2, 300, 20)), 1.15)
        self.assertGreater(core.ratio_low(base, samples(2, 300, 30)), 1.15)

    def test_a_zero_base(self):
        self.assertEqual(core.ratio_low([0.0]*10, [0.0]*10), 1.0)
        self.assertEqual(core.ratio_low([0.0]*10, [1.0]*10), float("inf"))


def four(old, new, old_again=None, new_again=None, name="capture"):
    """The samples of one metric, a list a role; a role not given repeats
    its binary's other role, shifted so the two are not one list."""
    old_again = old_again if old_again is not None else [x*1.001 for x in old]
    new_again = new_again if new_again is not None else [x*1.001 for x in new]
    return {"old": {name: old}, "new": {name: new}, "old_again": {name: old_again}, "new_again": {name: new_again}}


def merge(*parts):
    out = {role: {} for role in core.ROLES}
    for part in parts:
        for role, by in part.items():
            out[role].update(by)
    return out


class Decide(unittest.TestCase):
    def run_gate(self, old, new, old_again=None, new_again=None, ceilings=None):
        return core.decide(four(old, new, old_again, new_again), ceilings or {"capture": 100})

    def test_unchanged_passes(self):
        d = self.run_gate(samples(1, 150, 20), samples(2, 150, 20), samples(3, 150, 20), samples(4, 150, 20))
        self.assertEqual((d["verdict"], d["exit"], d["failures"], d["unsettled"]), ("pass", 0, [], []))
        m = d["metrics"]["capture"]
        self.assertEqual((m["n_old"], m["n_new"], m["ceiling_ms"]), (300, 300, 100))
        self.assertEqual(m["n_roles"], {"old": 150, "new": 150, "old_again": 150, "new_again": 150})

    def test_a_20_ms_delay_fails(self):
        d = self.run_gate(samples(1, 150, 20), [x+20 for x in samples(2, 150, 20)],
                          samples(3, 150, 20), [x+20 for x in samples(4, 150, 20)])
        self.assertEqual((d["verdict"], d["exit"]), ("fail", 1))
        self.assertEqual(len(d["failures"]), 1)
        self.assertIn("capture: p95 ratio's lower bound", d["failures"][0])
        # The change cancels out of the controls: each side holds one set of
        # runs of each binary.
        self.assertLess(d["metrics"]["capture"]["noise"], 1.15)

    def test_a_noisy_control_is_not_judged(self):
        # One set of the new binary's runs was slowed by load: the gate's
        # comparison rises, and so does a control, as far.
        quiet = [samples(k, 150, 20) for k in range(1, 5)]
        for slow in ("new", "new_again"):
            parts = dict(zip(core.ROLES, quiet))
            parts[slow] = [x*2 for x in parts[slow]]
            d = self.run_gate(parts["old"], parts["new"], parts["old_again"], parts["new_again"])
            self.assertEqual((d["verdict"], d["exit"], d["failures"]), ("not judged", 0, []), slow)
            m = d["metrics"]["capture"]
            self.assertTrue(m["unsettled"])
            self.assertFalse(m["judged"])
            self.assertGreater(m["ratio_low"], 1.15)
            self.assertGreaterEqual(1.15*m["noise"], m["ratio_low"])
        # Slowed runs of the old binary, or quicker runs of either, can only
        # make the new binary look faster: nothing fails.
        for slow in core.ROLES:
            for factor in (2, 0.5):
                if factor == 2 and slow.startswith("new"):
                    continue
                parts = dict(zip(core.ROLES, quiet))
                parts[slow] = [x*factor for x in parts[slow]]
                d = self.run_gate(parts["old"], parts["new"], parts["old_again"], parts["new_again"])
                self.assertEqual((d["verdict"], d["failures"]), ("pass", []), (slow, factor))

    def test_a_regression_beyond_the_noise_fails(self):
        # The new binary's two sets of runs differ by a fifth, which the
        # controls show; it is half as slow again as the old binary, which is
        # well past 1.15 times that.
        d = self.run_gate(samples(1, 150, 20), samples(2, 150, 30), samples(3, 150, 20), samples(4, 150, 36))
        m = d["metrics"]["capture"]
        self.assertGreater(m["noise"], 1.05)
        self.assertEqual((d["verdict"], d["exit"]), ("fail", 1))
        self.assertIn("times the controls'", d["failures"][0])

    def test_load_on_the_new_runs_alone_is_seen(self):
        # The load of the reviewed gate's second same-binary run fell on runs
        # 2 to 5 of nine, the new binary's and its control's and none of the
        # old one's, and no control saw it. Every stretch of runs the
        # schedule has loads new and new_again, or old and old_again,
        # unevenly when it loads new more than old, so a control sees it.
        order = [role for _, role in core.schedule(2)]
        for i in range(len(order)):
            for j in range(i+1, len(order)+1):
                loaded = order[i:j]
                parts = {role: [] for role in core.ROLES}
                for k, role in enumerate(order):
                    parts[role] += [x*(1.6 if i <= k < j else 1.0) for x in samples(10+k, 75, 20)]
                d = self.run_gate(parts["old"], parts["new"], parts["old_again"], parts["new_again"])
                self.assertNotEqual(d["verdict"], "fail", loaded)

    def test_the_second_control_sees_what_the_first_cannot(self):
        # Load that sends an eighth of a run's samples to three times their
        # time, on both runs of new, one of old_again and one of new_again,
        # of the two runs each role has: the new binary's p95 rises, the
        # first control's sides hold two loaded runs each, and only the
        # second control sees the load.
        def run(seed, loaded):
            values = samples(seed, 100, 20)
            return [x*3 if loaded and k % 8 == 0 else x for k, x in enumerate(values)]
        parts = {"old": run(1, False)+run(2, False), "new": run(3, True)+run(4, True),
                 "old_again": run(5, True)+run(6, False), "new_again": run(7, True)+run(8, False)}
        d = self.run_gate(parts["old"], parts["new"], parts["old_again"], parts["new_again"])
        self.assertGreater(d["metrics"]["capture"]["ratio_low"], 1.15)
        self.assertEqual((d["verdict"], d["exit"], d["failures"]), ("not judged", 0, []))
        first = core.ratio_low(parts["old"]+parts["new"], parts["old_again"]+parts["new_again"])
        second = core.ratio_low(parts["old"]+parts["new_again"], parts["old_again"]+parts["new"])
        self.assertLess(first, 1.15)
        self.assertGreater(second, 1.15)

    def test_noise_never_fails_a_build(self):
        # New is slower than old, but one of its runs alone: the gate can't
        # tell the change from the machine, and does not fail.
        d = self.run_gate(samples(1, 150, 20), samples(2, 150, 40), samples(3, 150, 20), samples(4, 150, 20))
        self.assertEqual((d["verdict"], d["exit"], d["failures"]), ("not judged", 0, []))
        self.assertFalse(d["metrics"]["capture"]["judged"])
        self.assertTrue(d["metrics"]["capture"]["unsettled"])

    def test_a_noisy_control_on_a_well_sampled_metric_stops_the_verdict(self):
        # Load moved the startup figure between two runs of one binary: the
        # capture figure's regression may be the machine's too.
        cap = four(samples(1, 150, 20), [x+20 for x in samples(2, 150, 20)],
                   samples(3, 150, 20), [x+20 for x in samples(4, 150, 20)])
        start = four(samples(5, 50, 100), samples(6, 50, 100), samples(7, 50, 100), samples(8, 50, 200), "startup")
        d = core.decide(merge(cap, start), {"capture": 100, "startup": 600})
        self.assertEqual((d["verdict"], d["exit"], d["machine_too_noisy"]), ("not judged", 0, True))
        self.assertEqual([f.split(":")[0] for f in d["failures"]], ["capture"])
        self.assertTrue(d["unsettled"])
        self.assertTrue(all(f.startswith("startup: ") for f in d["unsettled"]))

    def test_a_noisy_control_on_a_few_samples_leaves_that_metric_alone(self):
        # With 99 samples on a control's side, a noisy control leaves
        # startup unjudged, and capture's regression, whose controls agreed,
        # still fails.
        cap = four(samples(1, 150, 20), [x+20 for x in samples(2, 150, 20)],
                   samples(3, 150, 20), [x+20 for x in samples(4, 150, 20)])
        start = four(samples(5, 50, 100), samples(6, 50, 100), samples(7, 50, 100), samples(8, 49, 200), "startup")
        d = core.decide(merge(cap, start), {"capture": 100, "startup": 600})
        self.assertEqual((d["verdict"], d["exit"], d["machine_too_noisy"]), ("fail", 1, False))
        self.assertEqual([f.split(":")[0] for f in d["failures"]], ["capture"])
        self.assertEqual((d["metrics"]["capture"]["judged"], d["metrics"]["startup"]["judged"]), (True, False))
        # Without the regression, the noisy control alone is not judged.
        cap = four(samples(1, 150, 20), samples(2, 150, 20), samples(3, 150, 20), samples(4, 150, 20))
        d = core.decide(merge(cap, start), {"capture": 100, "startup": 600})
        self.assertEqual((d["verdict"], d["exit"], d["failures"]), ("not judged", 0, []))

    def test_the_threshold_is_strict(self):
        old = [10.0]*100
        self.assertEqual(self.run_gate(old, [11.5]*100, old[:], [11.5]*100)["verdict"], "pass")
        self.assertEqual(self.run_gate(old, [11.6]*100, old[:], [11.6]*100)["verdict"], "fail")

    def test_passing_the_ceiling_fails(self):
        # A rise too small for the ratio, that passes the ceiling.
        old, new = [95.0]*100, [102.0]*100
        d = self.run_gate(old, new, old[:], new[:])
        self.assertEqual((d["verdict"], d["exit"]), ("fail", 1))
        self.assertEqual(d["failures"], ["capture: p95 102.0 ms passed the ceiling of 100 ms"])
        self.assertEqual(self.run_gate(old, new, old[:], new[:], {"capture": None})["verdict"], "pass")
        self.assertEqual(self.run_gate(old, [100.0]*100, old[:], [100.0]*100)["verdict"], "pass")

    def test_a_ceiling_passed_amid_noise_is_unsettled(self):
        # One set of the new binary's runs passed the ceiling, the other did
        # not: the machine put more than 1.15 between them.
        d = self.run_gate([95.0]*100, [96.0]*100, [95.0]*100, [115.0]*100)
        m = d["metrics"]["capture"]
        self.assertGreater(m["noise"], 1.15)
        self.assertEqual((d["verdict"], d["exit"], d["failures"], m["reasons"]), ("not judged", 0, [], []))
        self.assertEqual(len(m["unsettled"]), 2)
        self.assertIn("passed the ceiling of 100 ms, but the controls'", m["unsettled"][1])

    def test_a_ceiling_the_old_binary_was_over_already(self):
        d = self.run_gate([105.0]*100, [110.0]*100, [105.0]*100, [110.0]*100)
        self.assertEqual((d["verdict"], d["failures"]), ("pass", []))

    def test_the_controls_have_no_ceiling(self):
        # Two runs of the new binary on either side of the ceiling show where
        # the binary sits, not that the machine is noisy: the gate fails.
        d = self.run_gate([95.0]*100, [99.0]*100, [95.0]*100, [101.0]*100)
        self.assertEqual((d["verdict"], d["exit"], d["unsettled"]), ("fail", 1, []))
        self.assertEqual(d["failures"], ["capture: p95 101.0 ms passed the ceiling of 100 ms"])

    def test_a_large_rise_to_the_ceiling_fails(self):
        # Capture from about 22 ms to about 100, and prompt doubled: both
        # fail, whatever seed made the samples.
        for seed in range(0, 40, 4):
            cap = four(samples(seed, 150, 20), samples(seed+1, 150, 92), samples(seed+2, 150, 20), samples(seed+3, 150, 92))
            prompt = four(samples(seed+4, 150, 10), samples(seed+5, 150, 20), samples(seed+6, 150, 10),
                          samples(seed+7, 150, 20), "prompt")
            d = core.decide(merge(cap, prompt), {"capture": 100, "prompt": 100})
            self.assertEqual((d["verdict"], d["exit"], d["unsettled"]), ("fail", 1, []), seed)
            self.assertEqual(sorted({f.split(":")[0] for f in d["failures"]}), ["capture", "prompt"])

    def test_a_metric_the_old_binary_gave_no_samples_is_not_compared(self):
        parts = merge(four(samples(1, 50, 20), samples(2, 50, 20), samples(3, 50, 20), samples(4, 50, 20)),
                      {"new": {"code_brief": [5.0]}, "new_again": {"code_brief": [5.0]}})
        d = core.decide(parts, {"capture": 100, "code_brief": 600})
        self.assertEqual((d["verdict"], d["not_compared"], list(d["metrics"])), ("pass", ["code_brief"], ["capture"]))

    def test_a_metric_the_new_binary_gave_no_samples_fails(self):
        parts = merge(four(samples(1, 50, 20), samples(2, 50, 20), samples(3, 50, 20), samples(4, 50, 20)),
                      {"old": {"code_brief": [5.0]*10}, "old_again": {"code_brief": [5.0]*10}})
        d = core.decide(parts, {"capture": 100, "code_brief": 600})
        self.assertEqual((d["verdict"], d["exit"]), ("fail", 1))
        self.assertEqual(d["failures"], ["code_brief: the new binary gave no samples, the old one 20"])
        # Even when the machine was too noisy to judge a ratio.
        parts["new_again"]["capture"] = [x*2 for x in parts["new_again"]["capture"]]
        parts["old"]["capture"] = parts["old"]["capture"]*3
        parts["new"]["capture"] = parts["new"]["capture"]*3
        parts["old_again"]["capture"] = parts["old_again"]["capture"]*3
        parts["new_again"]["capture"] = parts["new_again"]["capture"]*3
        d = core.decide(parts, {"capture": 100, "code_brief": 600})
        self.assertTrue(d["machine_too_noisy"])
        self.assertEqual((d["verdict"], d["exit"]), ("fail", 1))

    def test_a_control_with_an_empty_side_leaves_the_metric_unjudged(self):
        # The old binary's and the new one's first runs both failed.
        d = self.run_gate([], [], samples(3, 50, 20), [x*2 for x in samples(4, 50, 20)])
        self.assertEqual((d["verdict"], d["exit"], d["failures"]), ("not judged", 0, []))
        self.assertEqual(d["unsettled"], ["capture: p95 ratio's lower bound %.3f is above 1.15, and a control has a side with no samples"
                                          % d["metrics"]["capture"]["ratio_low"]])
        self.assertEqual(d["metrics"]["capture"]["controls_without_samples"], ["old+new against old_again+new_again"])

    def test_the_same_samples_give_the_same_verdict(self):
        args = (samples(1, 60, 20), samples(2, 60, 23), samples(3, 60, 20), samples(4, 60, 23))
        self.assertEqual(self.run_gate(*args), self.run_gate(*args))


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

    def test_a_scenario_the_new_binary_fails_clearly_more_often_is_broken(self):
        for counts in ({"new": 2, "new_again": 2}, {"new": 1, "new_again": 1}, {"new": 2},
                       {"old": 1, "new": 2, "new_again": 1}):
            found = core.scenarios(self.failures(**counts), 2)
            self.assertEqual((found["broken"], found["unrunnable"]), (["recall"], []), counts)

    def test_load_now_and_then_decides_nothing(self):
        for counts in ({"new": 1}, {"new_again": 1}, {"old": 1, "new": 2}, {"old": 2, "new_again": 3},
                       {"old": 1}, {"old": 2, "old_again": 2}):
            found = core.scenarios(self.failures(**counts), 2)
            self.assertEqual((found["broken"], found["unrunnable"]), ([], []), counts)
        self.assertEqual(core.scenarios({}, 2), {"runs_per_binary": 4, "failed_runs": {}, "broken": [], "unrunnable": []})

    def test_a_scenario_every_run_failed_could_not_run(self):
        found = core.scenarios(self.failures(old=2, new=2, old_again=2, new_again=2), 2)
        self.assertEqual((found["broken"], found["unrunnable"]), ([], ["recall"]))
        d = core.decide({}, {}, self.failures(old=1, new=1, old_again=1, new_again=1), 1)
        self.assertEqual((d["verdict"], d["exit"]), ("could not run", 2))

    def test_a_broken_scenario_fails_the_gate_whatever_the_noise(self):
        noisy = four(samples(1, 150, 20), samples(2, 150, 20), samples(3, 150, 20), samples(4, 150, 40))
        d = core.decide(noisy, {"capture": 100}, self.failures(new=2, new_again=1), 2)
        self.assertTrue(d["machine_too_noisy"])
        self.assertEqual((d["verdict"], d["exit"]), ("fail", 1))
        self.assertEqual(d["failures"][0], "the new binary failed the scenario recall in 3 of 4 runs, the old one in 0")
        self.assertEqual(core.decide(noisy, {"capture": 100}, self.failures(new=1), 2)["verdict"], "not judged")


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
        self.assertEqual({role: len(by["capture"]) for role, by in report["samples"].items()},
                         {role: 200 for role in core.ROLES})

    def test_a_scenario_the_new_binary_always_fails(self):
        self.scenario(lambda which, k: RuntimeError("hook failed or wrote diagnostics") if which == "new" else samples(k, 100, 20))
        code, out, report = self.gate()
        self.assertEqual((code, report["verdict"]), (1, "fail"), out)
        self.assertIn("the new binary failed the scenario fake in 4 of 4 runs, the old one in 0", report["decision"]["failures"])
        self.assertIn("capture: the new binary gave no samples, the old one 400", report["decision"]["failures"])

    def test_a_hook_that_hangs_in_two_runs_of_four(self):
        # As fast as the old binary when it answers, but past the hook's
        # time limit in two of its four runs: the worst regression there is.
        def figures(which, k):
            if which == "new" and k in (1, 3):
                return subprocess.TimeoutExpired(["eva-md", "hook"], 4)
            return samples(k, 100, 20)
        self.scenario(figures)
        code, out, report = self.gate()
        self.assertEqual((code, report["verdict"]), (1, "fail"), out)
        self.assertEqual(report["decision"]["scenarios"]["failed_runs"], {"fake": {"old": 0, "new": 2}})
        self.assertIn("fails: the new binary failed the scenario fake in 2 of 4 runs, the old one in 0", out)
        # Once in four is load, and decides nothing.
        self.calls.clear()
        core.SCENARIOS.clear()
        self.scenario(lambda which, k: subprocess.TimeoutExpired(["eva-md"], 4) if which == "new" and k == 2 else samples(k, 100, 20))
        code, out, report = self.gate()
        self.assertEqual((code, report["verdict"]), (0, "pass"), out)

    def test_a_failed_runs_samples_are_left_out(self):
        # The new binary's first run gives its figure, then fails.
        def figures(which, k):
            return samples(k, 10, 20)
        @core.scenario(100, "fake")
        def fake(run):
            which = self.binary(run)
            self.calls.append(which)
            run.figure("capture", figures(which, self.runs_of(which)), target=50)
            if which == "new" and self.runs_of("new") == 1:
                raise RuntimeError("invalid Stop output")
        code, out, report = self.gate()
        self.assertEqual(code, 0, out)
        self.assertEqual({role: len(by.get("capture", [])) for role, by in report["samples"].items()},
                         {"old": 20, "new": 10, "old_again": 20, "new_again": 20})
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
        self.scenario(lambda which, k: [95.0]*20 if which == "old" else [102.0]*20)
        code, out, report = self.gate()
        self.assertEqual((code, report["verdict"]), (1, "fail"), out)
        self.assertEqual(report["decision"]["metrics"]["capture"]["ceiling_ms"], 100)
        self.assertEqual(report["decision"]["failures"], ["capture: p95 102.0 ms passed the ceiling of 100 ms"])
        code, out, report = self.gate(ceiling_factor=0)
        self.assertEqual((code, report["verdict"]), (0, "pass"), out)
        self.assertIsNone(report["decision"]["metrics"]["capture"]["ceiling_ms"])

    def test_the_order_turns_each_round(self):
        self.scenario(lambda which, k: samples(k, 100, 20))
        code, out, report = self.gate(rounds=3)
        order = [role for _, role in core.schedule(3)]
        self.assertEqual(report["schedule"], order)
        self.assertEqual([(r["round"], r["binary"]) for r in report["runs"]], core.schedule(3))
        self.assertEqual(self.calls, [core.BINARY[role] for role in order])
        self.assertEqual(self.calls[:8], ["old", "new", "old", "new", "new", "old", "new", "old"])

    def test_the_baseline_holds_the_new_binarys_runs(self):
        self.scenario(lambda which, k: samples(k, 100, 20 if which == "old" else 30))
        code, out, report = self.gate()
        base = json.loads((self.path/"base.json").read_text())
        self.assertEqual((base["runs"], base["commit"], base["metrics"]["capture"]["n"]), (4, "0123abc", 400))
        self.assertGreater(base["metrics"]["capture"]["median_ms"], 27)


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
