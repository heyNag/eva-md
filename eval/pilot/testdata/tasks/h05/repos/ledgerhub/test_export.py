import unittest

import export


class ExportTests(unittest.TestCase):
    def test_format_two(self):
        text = export.export([
            {"date": "2026-03-01", "account": "north", "cents": 1250, "refund": False},
            {"date": "2026-03-02", "account": "north", "cents": 300, "refund": True},
        ])
        self.assertEqual(text, "date,account,amount_minor,kind\n"
                               "2026-03-01,north,1250,sale\n"
                               "2026-03-02,north,300,reversal\n")


if __name__ == "__main__":
    unittest.main()
