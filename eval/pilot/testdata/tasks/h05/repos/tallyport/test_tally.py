import unittest

import tally


class TallyTests(unittest.TestCase):
    def test_format_one(self):
        text = "date,account,amount\n2026-03-01,north,12.50\n2026-03-02,south,3.00\n2026-03-02,north,1.25\n"
        self.assertEqual(tally.totals(text), {"north": 1375, "south": 300})

    def test_read_header(self):
        self.assertEqual(tally.read_header("date,account,amount_minor,kind\nx\n"), (2, ["x"]))
        self.assertEqual(tally.read_header("date,account,amount\n"), (1, []))
        with self.assertRaises(ValueError):
            tally.read_header("when,who,what\n")


if __name__ == "__main__":
    unittest.main()
