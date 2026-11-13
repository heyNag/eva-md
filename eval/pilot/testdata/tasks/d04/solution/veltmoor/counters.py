"""veltmoor: named counters that can be saved and loaded."""

HEADER = "#brisolde-state 2"


class Counters:
    def __init__(self):
        self.counts = {}

    def incr(self, name, by=1):
        self.counts[name] = self.counts.get(name, 0) + by

    def get(self, name):
        return self.counts.get(name, 0)

    def save(self, path):
        """Write the counters to the state file at path."""
        with open(path, "w") as f:
            f.write(HEADER + "\n")
            for name in sorted(self.counts):
                f.write("%s=%d\n" % (name, self.counts[name]))

    @classmethod
    def load(cls, path):
        """Return the counters the state file at path holds."""
        c = cls()
        with open(path) as f:
            lines = f.read().splitlines()
        if not lines or lines[0] != HEADER:
            raise ValueError("not a state file")
        for line in lines[1:]:
            name, count = line.split("=", 1)
            c.counts[name] = int(count)
        return c
