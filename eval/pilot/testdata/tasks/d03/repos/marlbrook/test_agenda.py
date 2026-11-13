import io
import os
import tempfile
import unittest

import agenda
import cli


class AgendaTests(unittest.TestCase):
    def test_parse_and_render(self):
        entries = agenda.parse("09:30 standup\n\n13:05 lunch\n")
        self.assertEqual(entries, [(570, "standup"), (785, "lunch")])
        self.assertEqual(agenda.render(entries), "09:30 standup\n13:05 lunch\n")

    def test_show(self):
        with tempfile.TemporaryDirectory() as d:
            path = os.path.join(d, "day.txt")
            with open(path, "w") as f:
                f.write("13:05 lunch\n09:30 standup\n")
            out = io.StringIO()
            self.assertEqual(cli.main(["show", path], out), 0)
            self.assertEqual(out.getvalue(), "09:30 standup\n13:05 lunch\n")


if __name__ == "__main__":
    unittest.main()
