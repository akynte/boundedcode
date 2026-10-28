"""Scoring."""

THRESHOLD = 0.5


def score(values):
    return sum(values) / len(values) if values else 0.0


class Model:
    def predict(self, values):
        return score(values) >= THRESHOLD
