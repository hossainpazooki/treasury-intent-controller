"""Service fail-closed matrix (CONTRACT-SCORER §S.1) via TestClient — no network.

Every row for a well-formed request answers 200 with UNEVALUABLE; non-2xx is
reserved for malformed requests. An outage or bug may only ever make the gate
refuse — never grant.
"""

import pytest
from fastapi.testclient import TestClient

from tis.app import create_app
from tis.facts import StaticFactSource
from tis.resolver import NullResolver


def body(**over) -> dict:
    b = {
        "intent_id": "itx-test-0001",
        "criterion": "balance",
        "threshold": 100.0,
        "phase": "declaration",
        "volatility": "stable",
    }
    b.update(over)
    return b


def client(facts=None, resolver=None) -> TestClient:
    src = StaticFactSource({"balance": 250.0}) if facts is None else facts
    return TestClient(create_app(facts=src, resolver=resolver))


class RaisingFacts:
    """FactSource whose get() raises — the 'evaluator raises' matrix row."""

    def get(self, criterion: str, intent_id: str):
        raise RuntimeError("boom")


class NoneFacts:
    """FactSource that knows nothing — the 'no fact' matrix row."""

    def get(self, criterion: str, intent_id: str):
        return None


class FalseResolver:
    """ArtifactResolver whose verify always fails — the 'verify fails' row."""

    def verify(self, rule_artifact_hash, intent_spec_hash) -> bool:
        return False


class TrueResolver:
    """ArtifactResolver whose verify succeeds — scoring then proceeds."""

    def verify(self, rule_artifact_hash, intent_spec_hash) -> bool:
        return True


def test_healthz():
    r = client().get("/healthz")
    assert r.status_code == 200
    assert r.text == "ok"


def test_happy_path_pass():
    r = client().post("/ml/evaluate", json=body())
    assert r.status_code == 200
    assert r.json() == {"result": "PASS", "basis": "balance=250.00 >= 100.00"}


def test_unknown_criterion_is_200_unevaluable():
    r = client().post("/ml/evaluate", json=body(criterion="nonexistent"))
    assert r.status_code == 200
    assert r.json()["result"] == "UNEVALUABLE"


def test_fact_source_without_fact_is_200_unevaluable():
    r = client(facts=NoneFacts()).post("/ml/evaluate", json=body())
    assert r.status_code == 200
    assert r.json()["result"] == "UNEVALUABLE"


def test_evaluator_exception_is_200_unevaluable():
    r = client(facts=RaisingFacts()).post("/ml/evaluate", json=body())
    assert r.status_code == 200
    assert r.json()["result"] == "UNEVALUABLE"


def test_resolver_verify_failure_is_200_unevaluable():
    r = client(resolver=FalseResolver()).post(
        "/ml/evaluate",
        json=body(rule_artifact_hash="rh-1", intent_spec_hash="sh-1"),
    )
    assert r.status_code == 200
    assert r.json()["result"] == "UNEVALUABLE"


def test_resolver_verify_success_scores_from_facts():
    r = client(resolver=TrueResolver()).post(
        "/ml/evaluate",
        json=body(rule_artifact_hash="rh-1", intent_spec_hash="sh-1"),
    )
    assert r.status_code == 200
    assert r.json()["result"] == "PASS"


def test_null_resolver_skips_with_visible_basis_note():
    # Hashes present but no real resolver on this host: skip verification,
    # RECORD the skip (implemented-vs-planned stays visible in basis).
    r = client(resolver=NullResolver()).post(
        "/ml/evaluate",
        json=body(rule_artifact_hash="rh-1", intent_spec_hash="sh-1"),
    )
    assert r.status_code == 200
    got = r.json()
    assert got["result"] == "PASS"
    assert got["basis"].startswith("resolver=null: verification skipped; ")


def test_hashes_absent_means_no_resolver_involvement():
    r = client(resolver=FalseResolver()).post("/ml/evaluate", json=body())
    assert r.status_code == 200
    assert r.json()["result"] == "PASS"  # FalseResolver never consulted


def test_malformed_request_is_4xx():
    r = client().post("/ml/evaluate", json={"criterion": "balance"})
    assert r.status_code in (400, 422)


def test_non_json_body_is_4xx():
    r = client().post(
        "/ml/evaluate",
        content=b"not json",
        headers={"Content-Type": "application/json"},
    )
    assert r.status_code in (400, 422)


def test_unknown_request_fields_are_ignored():
    # Forward compatibility (§S.1): unknown fields must not fail the request.
    r = client().post("/ml/evaluate", json=body(some_future_field="x"))
    assert r.status_code == 200
    assert r.json()["result"] == "PASS"


def test_basis_absent_key_when_none():
    # If basis is ever None it must be OMITTED, not serialized as null.
    r = client().post("/ml/evaluate", json=body())
    assert "basis" not in r.json() or r.json()["basis"] is not None
