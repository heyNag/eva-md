import io
import unittest

import todo


def run(items, *argv):
    out = io.StringIO()
    code = todo.main(list(argv), items, out)
    return code, out.getvalue()


class TodoTests(unittest.TestCase):
    def test_add_and_list(self):
        items = []
        run(items, "add", "fence", "5")
        run(items, "add", "gutter", "3")
        self.assertEqual(run(items, "list"), (0, "3 gutter\n5 fence\n"))

    def test_done(self):
        items = []
        run(items, "add", "fence", "5")
        run(items, "add", "gutter", "3")
        run(items, "done", "1")
        self.assertEqual(run(items, "list"), (0, "5 fence\n"))

    def test_unknown(self):
        self.assertEqual(run([], "dance")[0], 2)


if __name__ == "__main__":
    unittest.main()
