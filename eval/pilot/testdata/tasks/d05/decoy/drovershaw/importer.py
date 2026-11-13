"""drovershaw: imports 'name,qty' rows into an output folder."""

import os


def parse_row(line):
    """Return (name, qty) for a good row, or None for a bad one."""
    parts = line.split(",")
    if len(parts) != 2 or not parts[0].strip() or not parts[1].strip().isdigit():
        return None
    return parts[0].strip(), int(parts[1])


def import_rows(lines, out_dir):
    """Write the good rows to accepted.txt in out_dir and the bad ones,
    each after its line number and a colon, to rejected.txt.

    Return how many rows were accepted and how many rejected.
    """
    accepted, rejected = [], []
    for n, line in enumerate(lines, 1):
        row = parse_row(line)
        if row is None:
            rejected.append("%d:%s\n" % (n, line.rstrip("\n")))
        else:
            accepted.append("%s,%d\n" % row)
    with open(os.path.join(out_dir, "accepted.txt"), "w") as f:
        f.writelines(accepted)
    with open(os.path.join(out_dir, "rejected.txt"), "w") as f:
        f.writelines(rejected)
    return len(accepted), len(rejected)
