import os
import tempfile
import unittest

import importer


class ImporterTests(unittest.TestCase):
    def test_parse_row(self):
        self.assertEqual(importer.parse_row("bolts,12"), ("bolts", 12))
        self.assertIsNone(importer.parse_row("bolts,twelve"))
        self.assertIsNone(importer.parse_row(",3"))

    def test_good_rows_are_written(self):
        with tempfile.TemporaryDirectory() as d:
            importer.import_rows(["bolts,12", "nuts,3"], d)
            with open(os.path.join(d, "accepted.txt")) as f:
                self.assertEqual(f.read(), "bolts,12\nnuts,3\n")


if __name__ == "__main__":
    unittest.main()
