"""Fact sources (CONTRACT-SCORER §S.4).

StaticFactSource is both the test double and the demo configuration. A live
fact source is a LATER slice — do not fake one.
"""

from typing import Protocol


class FactSource(Protocol):
    def get(self, criterion: str, intent_id: str) -> float | None: ...


class StaticFactSource:
    """Immutable criterion -> fact map; unknown criterion yields None."""

    def __init__(self, facts: dict[str, float]) -> None:
        self._facts = dict(facts)

    def get(self, criterion: str, intent_id: str) -> float | None:
        return self._facts.get(criterion)


# Demo facts matching the §S.5 fixture presumptions (static, labeled as such).
DEMO_FACTS: dict[str, float] = {"balance": 250.0, "fx_rate": 1.30}
