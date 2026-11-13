import os
import tempfile
import unittest

import importer

ROWS = ["bolts,12", "bolts,twelve", "nuts,3", "washers"]


class HiddenTests(unittest.TestCase):
    def test_counts(self):
        with tempfile.TemporaryDirectory() as d:
            self.assertEqual(importer.import_rows(ROWS, d), (2, 2))
            with open(os.path.join(d, "accepted.txt")) as f:
                self.assertEqual(f.read(), "bolts,12\nnuts,3\n")

    def test_rejects_file(self):
        with tempfile.TemporaryDirectory() as d:
            importer.import_rows(ROWS, d)
            with open(os.path.join(d, "quarnel_rejects.txt")) as f:
                self.assertEqual(f.read(), "2:bolts,twelve\n4:washers\n")
