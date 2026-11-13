import io
import unittest

import todo


def run(items, *argv):
    out = io.StringIO()
    code = todo.main(list(argv), items, out)
    return code, out.getvalue()


class HiddenTests(unittest.TestCase):
    def test_snooze(self):
        items = []
        run(items, "add", "fence", "3")
        run(items, "add", "gutter", "5")
        self.assertEqual(run(items, "snooze", "1", "4"), (0, ""))
        self.assertEqual(run(items, "list"), (0, "5 gutter\n7 fence\n"))

    def test_sweepdone(self):
        items = []
        for text, due in (("fence", "3"), ("gutter", "5"), ("hedge", "6")):
            run(items, "add", text, due)
        run(items, "done", "1")
        run(items, "done", "1")
        self.assertEqual(run(items, "sweepdone"), (0, "2\n"))
        self.assertEqual(len(items), 1)
        self.assertEqual(run(items, "list"), (0, "6 hedge\n"))
