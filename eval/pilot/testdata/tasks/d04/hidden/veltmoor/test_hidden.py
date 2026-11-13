import os
import tempfile
import unittest

from counters import Counters


def filled():
    c = Counters()
    c.incr("beta")
    c.incr("alpha", 3)
    return c


class HiddenTests(unittest.TestCase):
    def test_round_trip(self):
        with tempfile.TemporaryDirectory() as d:
            path = os.path.join(d, "state")
            filled().save(path)
            c = Counters.load(path)
            self.assertEqual((c.get("alpha"), c.get("beta"), c.get("gamma")), (3, 1, 0))

    def test_state_file_format(self):
        with tempfile.TemporaryDirectory() as d:
            path = os.path.join(d, "state")
            filled().save(path)
            with open(path) as f:
                self.assertEqual(f.read(), "#brisolde-state 2\nalpha=3\nbeta=1\n")
