"""Artifact resolver seam (CONTRACT-SCORER §S.4).

The ke-artifact-py reader is INJECTED behind ArtifactResolver and exercised
only where the wheel exists (Linux/CI) — the wheel does not build on Windows
BY DESIGN; never rebuild a binding. The wheel-backed implementation is the
post-Stage-A reader slice (activates after ATLAS PR #13 merges) and is NOT
built yet — NullResolver is the honest placeholder, and the app records the
skip in basis so implemented-vs-planned stays visible on the wire.
"""

from typing import Protocol


class ArtifactResolver(Protocol):
    def verify(
        self, rule_artifact_hash: str | None, intent_spec_hash: str | None
    ) -> bool: ...


class NullResolver:
    """Marker: NO resolver on this host. The app must skip verification (with a
    visible basis note), never call verify — calling it is a bug and raises,
    which the app's catch-all fails closed to UNEVALUABLE."""

    def verify(
        self, rule_artifact_hash: str | None, intent_spec_hash: str | None
    ) -> bool:
        raise RuntimeError(
            "NullResolver cannot verify artifacts; the app must skip instead"
        )
