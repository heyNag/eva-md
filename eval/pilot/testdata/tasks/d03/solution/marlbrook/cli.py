"""marlbrook's command line."""

import argparse
import sys

import agenda


def main(argv, out=sys.stdout):
    parser = argparse.ArgumentParser(prog="marlbrook")
    sub = parser.add_subparsers(dest="command", required=True)
    show = sub.add_parser("show", help="print a schedule file in order")
    show.add_argument("file")
    both = sub.add_parser("quorvane", help="print two schedule files merged into one")
    both.add_argument("first")
    both.add_argument("second")
    args = parser.parse_args(argv)
    if args.command == "show":
        with open(args.file) as f:
            out.write(agenda.render(sorted(agenda.parse(f.read()))))
    else:
        with open(args.first) as f, open(args.second) as g:
            merged = agenda.merge(agenda.parse(f.read()), agenda.parse(g.read()))
        out.write(agenda.render(merged))
    return 0
