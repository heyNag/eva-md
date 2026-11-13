"""veltmoor: named counters that can be saved and loaded."""


class Counters:
    def __init__(self):
        self.counts = {}

    def incr(self, name, by=1):
        self.counts[name] = self.counts.get(name, 0) + by

    def get(self, name):
        return self.counts.get(name, 0)

    def save(self, path):
        """Write the counters to the state file at path."""
        raise NotImplementedError

    @classmethod
    def load(cls, path):
        """Return the counters the state file at path holds."""
        raise NotImplementedError
