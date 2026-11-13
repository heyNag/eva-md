import unittest

import tally

HEADER = "date,account,amount_minor,kind\n"


class HiddenTests(unittest.TestCase):
    def test_cents(self):
        text = HEADER + "2026-03-01,north,1250,sale\n2026-03-02,north,250,sale\n2026-03-02,south,99,sale\n"
        self.assertEqual(tally.totals(text), {"north": 1500, "south": 99})

    def test_reversal(self):
        text = HEADER + "2026-03-01,north,1250,sale\n2026-03-03,north,500,reversal\n"
        self.assertEqual(tally.totals(text), {"north": 750})
        self.assertEqual(tally.totals("date,account,amount\n2026-03-01,north,2.00\n"), {"north": 200})
