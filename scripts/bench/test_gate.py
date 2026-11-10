"""Tests of the latency gate's decision and of the scenario list. They feed
numbers and read no clock: python3 -I scripts/bench/test_gate.py"""
import importlib.util
from pathlib import Path
import random
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


class Decide(unittest.TestCase):
    def run_gate(self, old, new, control, ceilings=None):
        return core.decide({"capture": old}, {"capture": new}, {"capture": control}, ceilings or {"capture": 100})

    def test_unchanged_passes(self):
        d = self.run_gate(samples(1, 300, 20), samples(2, 300, 20), samples(3, 300, 20))
        self.assertEqual((d["verdict"], d["exit"], d["failures"], d["control_failures"]), ("pass", 0, [], []))
        m = d["metrics"]["capture"]
        self.assertEqual((m["n_old"], m["n_new"], m["n_control"], m["ceiling_ms"]), (300, 300, 300, 100))

    def test_a_20_ms_delay_fails(self):
        old = samples(1, 300, 20)
        new, control = [x+20 for x in samples(2, 300, 20)], [x+20 for x in samples(3, 300, 20)]
        d = self.run_gate(old, new, control)
        self.assertEqual((d["verdict"], d["exit"]), ("fail", 1))
        self.assertEqual(len(d["failures"]), 1)
        self.assertIn("capture: p95 ratio's lower bound", d["failures"][0])

    def test_a_noisy_control_is_not_judged(self):
        old, new = samples(1, 300, 20), samples(2, 300, 20)
        noisy = samples(3, 300, 40)
        for control in (noisy, [x/2 for x in new]):
            d = self.run_gate(old, new, control)
            self.assertEqual((d["verdict"], d["exit"]), ("not judged", 0))
            self.assertTrue(d["control_failures"])

    def test_noise_never_fails_a_build(self):
        # New is slower than old, but the control is as far from new: the
        # gate can't tell the change from the machine, and does not fail.
        old, new = samples(1, 300, 20), samples(2, 300, 40)
        d = self.run_gate(old, new, samples(3, 300, 20))
        self.assertEqual((d["verdict"], d["exit"]), ("not judged", 0))
        self.assertTrue(d["failures"])

    def test_one_noisy_metric_of_many_stops_the_verdict(self):
        old = {"capture": samples(1, 300, 20), "startup": samples(4, 300, 100)}
        new = {"capture": [x+20 for x in samples(2, 300, 20)], "startup": samples(5, 300, 100)}
        control = {"capture": [x+20 for x in samples(3, 300, 20)], "startup": samples(6, 300, 200)}
        d = core.decide(old, new, control, {"capture": 100, "startup": 600})
        self.assertEqual((d["verdict"], d["exit"]), ("not judged", 0))
        self.assertEqual([f.split(":")[0] for f in d["failures"]], ["capture"])
        self.assertTrue(all(f.startswith("startup: ") for f in d["control_failures"]))

    def test_the_threshold_is_strict(self):
        old = [10.0]*100
        self.assertEqual(self.run_gate(old, [11.5]*100, [11.5]*100)["verdict"], "pass")
        self.assertEqual(self.run_gate(old, [11.6]*100, [11.6]*100)["verdict"], "fail")

    def test_passing_the_ceiling_fails(self):
        # A rise too small for the ratio, that passes the ceiling.
        old, new = [95.0]*100, [102.0]*100
        d = self.run_gate(old, new, new[:])
        self.assertEqual((d["verdict"], d["exit"]), ("fail", 1))
        self.assertEqual(d["failures"], ["capture: p95 102.0 ms passed the ceiling of 100 ms"])
        self.assertEqual(self.run_gate(old, new, new[:], {"capture": None})["verdict"], "pass")
        self.assertEqual(self.run_gate(old, [100.0]*100, [100.0]*100)["verdict"], "pass")

    def test_a_ceiling_the_old_binary_was_over_already(self):
        d = self.run_gate([105.0]*100, [110.0]*100, [110.0]*100)
        self.assertEqual((d["verdict"], d["failures"]), ("pass", []))

    def test_a_control_on_both_sides_of_the_ceiling_is_not_judged(self):
        d = self.run_gate([95.0]*100, [99.0]*100, [101.0]*100)
        self.assertEqual((d["verdict"], d["exit"]), ("not judged", 0))
        self.assertEqual(d["control_failures"], ["capture: p95 101.0 ms passed the ceiling of 100 ms"])

    def test_a_metric_without_samples_is_not_compared(self):
        d = core.decide({"capture": samples(1, 50, 20), "code_brief": []}, {"capture": samples(2, 50, 20), "code_brief": [5.0]},
                        {"capture": samples(3, 50, 20)}, {"capture": 100, "code_brief": 600})
        self.assertEqual((d["verdict"], d["not_compared"], list(d["metrics"])), ("pass", ["code_brief"], ["capture"]))

    def test_the_same_samples_give_the_same_verdict(self):
        old, new, control = samples(1, 120, 20), samples(2, 120, 23), samples(3, 120, 23)
        self.assertEqual(self.run_gate(old, new, control), self.run_gate(old, new, control))


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
                         ["/bin/sh", "-c", "'/opt/it'\"'\"'s/eva-md' hook --host codex || echo '{}'"])
        self.assertEqual(core.launch(Path("/opt/eva-md"), "claude-code"), ["/opt/eva-md", "hook", "--host", "claude-code"])


if __name__ == "__main__":
    unittest.main()
