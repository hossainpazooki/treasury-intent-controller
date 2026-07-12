"""Resolver seam (CONTRACT-SCORER §S.4).

NullResolver is the Windows-local default: the ke-artifact-py wheel builds on
Linux/CI only, BY DESIGN. The wheel-backed resolver lane below skips visibly
where the wheel is absent — never faked green.
"""

import pytest

from tis.resolver import ArtifactResolver, NullResolver


def test_null_resolver_is_a_marker_not_a_verifier():
    # NullResolver means "no resolver on this host". It must never be usable to
    # CLAIM verification: calling verify is a bug and must raise (the app's
    # catch-all then fails closed to UNEVALUABLE).
    with pytest.raises(Exception):
        NullResolver().verify("rh", "sh")


def test_protocol_accepts_structural_impls():
    class Impl:
        def verify(self, rule_artifact_hash, intent_spec_hash) -> bool:
            return True

    r: ArtifactResolver = Impl()
    assert r.verify("rh", "sh") is True


def test_ke_artifact_wheel_lane():
    # Linux/CI-only: the real reader binds here after ATLAS PR #13 merges.
    # On hosts without the wheel this SKIPS — visibly, never faked green.
    pytest.importorskip(
        "ke_artifact",
        reason="ke-artifact-py wheel absent (Linux/CI-only by design)",
    )
    pytest.fail(
        "wheel present but the KeArtifactResolver lane is not built yet — "
        "this is the post-#13 reader slice"
    )
