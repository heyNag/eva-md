"""drovershaw: imports 'name,qty' rows into an output folder."""

import os


def parse_row(line):
    """Return (name, qty) for a good row, or None for a bad one."""
    parts = line.split(",")
    if len(parts) != 2 or not parts[0].strip() or not parts[1].strip().isdigit():
        return None
    return parts[0].strip(), int(parts[1])


def import_rows(lines, out_dir):
    """Write the good rows to accepted.txt in out_dir; return how many there were."""
    accepted = []
    for line in lines:
        row = parse_row(line)
        if row is not None:
            accepted.append("%s,%d\n" % row)
    with open(os.path.join(out_dir, "accepted.txt"), "w") as f:
        f.writelines(accepted)
    return len(accepted)
