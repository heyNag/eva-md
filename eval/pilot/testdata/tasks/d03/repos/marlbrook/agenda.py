"""marlbrook: schedules as lists of (minute of the day, title) pairs."""


def parse(text):
    """Return the schedule text holds: one 'HH:MM title' a line."""
    entries = []
    for line in text.splitlines():
        line = line.strip()
        if not line:
            continue
        clock, title = line.split(" ", 1)
        hours, minutes = clock.split(":")
        entries.append((int(hours) * 60 + int(minutes), title))
    return entries


def render(entries):
    """Return entries as text, one 'HH:MM title' a line."""
    return "".join("%02d:%02d %s\n" % (m // 60, m % 60, t) for m, t in entries)


def merge(first, second):
    """Return both schedules' entries in one, by time.

    On a tie, first's entry comes first.
    """
    raise NotImplementedError
