import io
import os
import tempfile
import unittest

import agenda
import cli


class HiddenTests(unittest.TestCase):
    def test_merge(self):
        first = [(60, "a"), (120, "c")]
        second = [(60, "b"), (90, "x")]
        self.assertEqual(agenda.merge(first, second), [(60, "a"), (60, "b"), (90, "x"), (120, "c")])

    def test_subcommand(self):
        with tempfile.TemporaryDirectory() as d:
            paths = []
            for name, text in (("one.txt", "10:00 talk\n"), ("two.txt", "09:00 coffee\n11:00 walk\n")):
                paths.append(os.path.join(d, name))
                with open(paths[-1], "w") as f:
                    f.write(text)
            out = io.StringIO()
            self.assertEqual(cli.main(["quorvane"] + paths, out), 0)
            self.assertEqual(out.getvalue(), "09:00 coffee\n10:00 talk\n11:00 walk\n")
