"""marlbrook's command line."""

import argparse
import sys

import agenda


def main(argv, out=sys.stdout):
    parser = argparse.ArgumentParser(prog="marlbrook")
    sub = parser.add_subparsers(dest="command", required=True)
    show = sub.add_parser("show", help="print a schedule file in order")
    show.add_argument("file")
    args = parser.parse_args(argv)
    if args.command == "show":
        with open(args.file) as f:
            out.write(agenda.render(sorted(agenda.parse(f.read()))))
    return 0
