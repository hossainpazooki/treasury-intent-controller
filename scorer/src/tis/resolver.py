"""Artifact resolver seam (CONTRACT-SCORER §S.4).

The ke-artifact-py reader is INJECTED behind ArtifactResolver and exercised
only where the wheel exists (Linux/CI) — the wheel does not build on Windows
BY DESIGN; never rebuild a binding. `KeArtifactResolver` is the wheel-backed
reader slice (post-Stage-A, ATLAS PR #13); `NullResolver` stays the
Windows-local default, and the app records the skip in basis so
implemented-vs-planned stays visible on the wire.
"""

from __future__ import annotations

import importlib
from pathlib import Path
from typing import Any, Protocol


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


class KeArtifactResolver:
    """Wheel-backed reader: verifies the governing artifacts by content address.

    Holds a directory of `.kew` files — indexed once, lazily, by manifest
    artifact hash; the store is treated as immutable for the service's lifetime.
    A file that does not decode as a canonical artifact is simply absent from
    the index, so requesting its hash fails closed. `verify()` runs the folded
    ke-artifact-py verdict for EVERY hash the gate sent and returns True only if
    each one is present, verdict-"verified" under the configured keydir /
    context / policy / registry evidence at the configured export instant, and
    re-addresses to the exact hash requested.

    Malformed input JSON raises instead of returning False: a config bug is an
    internal error, which the app also fails closed to UNEVALUABLE — but with a
    distinct, diagnosable basis. The export instant is explicit (no wallclock:
    determinism stays testable).
    """

    def __init__(
        self,
        artifact_dir: str | Path,
        keydir_json: str,
        context_json: str,
        policy_json: str,
        registry_json: str,
        exported_at_unix: int,
        binding: Any | None = None,
    ) -> None:
        # Lazy wheel import: constructing the resolver is the moment the wheel
        # becomes required; merely importing this module never is.
        self._binding = (
            binding if binding is not None else importlib.import_module("ke_artifact_py")
        )
        self._artifact_dir = Path(artifact_dir)
        self._keydir_json = keydir_json
        self._context_json = context_json
        self._policy_json = policy_json
        self._registry_json = registry_json
        self._exported_at = int(exported_at_unix)
        self._index: dict[str, bytes] | None = None

    @classmethod
    def from_paths(
        cls,
        artifact_dir: str | Path,
        inputs_dir: str | Path,
        exported_at_unix: int,
        binding: Any | None = None,
    ) -> "KeArtifactResolver":
        """Construct from an inputs directory holding the four ATLAS verify
        inputs by their contract-test names: keydir.json, context.json,
        policy.json, registry.json (see regulatory-rule-engine
        scripts/contract-inputs/)."""
        inputs = Path(inputs_dir)

        def read(name: str) -> str:
            return (inputs / name).read_text(encoding="utf-8")

        return cls(
            artifact_dir,
            read("keydir.json"),
            read("context.json"),
            read("policy.json"),
            read("registry.json"),
            exported_at_unix,
            binding=binding,
        )

    def _store(self) -> dict[str, bytes]:
        if self._index is None:
            index: dict[str, bytes] = {}
            for kew_path in sorted(self._artifact_dir.rglob("*.kew")):
                data = kew_path.read_bytes()
                try:
                    artifact = self._binding.from_bytes(data)
                except ValueError:
                    # Not a canonical artifact: absent from the index, so any
                    # request for its hash fails closed at lookup.
                    continue
                index[artifact.artifact_hash.lower()] = data
            self._index = index
        return self._index

    def _verified(self, requested: str) -> bool:
        want = requested.lower()
        kew = self._store().get(want)
        if kew is None:
            return False
        outcome = self._binding.verify_artifact(
            kew,
            self._keydir_json,
            self._context_json,
            self._policy_json,
            self._registry_json,
            self._exported_at,
        )
        # Belt and braces: the folded verdict already includes the content-hash
        # check, but the resolver's promise is "THIS hash verifies", so compare
        # the re-addressed hash to the one the gate asked about.
        return (
            outcome.get("verdict") == "verified"
            and str(outcome.get("content_hash", "")).lower() == want
        )

    def verify(
        self, rule_artifact_hash: str | None, intent_spec_hash: str | None
    ) -> bool:
        requested = [h for h in (rule_artifact_hash, intent_spec_hash) if h]
        if not requested:
            # The app only calls with a hash present; answering True to a
            # hashless call would be a fail-open edge, so refuse it.
            return False
        return all(self._verified(h) for h in requested)
