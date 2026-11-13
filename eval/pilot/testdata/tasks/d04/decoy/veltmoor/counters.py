"""veltmoor: named counters that can be saved and loaded."""

import json


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
            json.dump(self.counts, f)

    @classmethod
    def load(cls, path):
        """Return the counters the state file at path holds."""
        c = cls()
        with open(path) as f:
            c.counts = json.load(f)
        return c
