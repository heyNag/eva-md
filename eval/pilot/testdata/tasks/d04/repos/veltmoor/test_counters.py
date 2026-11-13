import unittest

from counters import Counters


class CountersTests(unittest.TestCase):
    def test_incr_and_get(self):
        c = Counters()
        c.incr("alpha")
        c.incr("alpha", 2)
        self.assertEqual(c.get("alpha"), 3)
        self.assertEqual(c.get("beta"), 0)


if __name__ == "__main__":
    unittest.main()
