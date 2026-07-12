"""Cross-language golden fixtures (CONTRACT-SCORER §S.5).

The fixture bytes in contract/scorer are the ONLY surface shared with the Go
gate. Each request fixture must parse; the service's serialized response must
equal the response fixture byte-for-byte (surrounding whitespace trimmed).
Fixtures live in THIS repo (contract/scorer, two levels up); TIC_CONTRACT_DIR
overrides for unusual layouts. If absent these tests SKIP VISIBLY — a skipped
fixture test is NOT a green one.
"""

import json
import os
from pathlib import Path

import pytest
from fastapi.testclient import TestClient

from tis.app import create_app
from tis.facts import StaticFactSource

# scorer/tests/ -> repo root -> contract/scorer (same repo since the one-repo
# intent-layer consolidation, 2026-07-08).
DEFAULT_CONTRACT_DIR = Path(__file__).resolve().parents[2] / "contract" / "scorer"


def contract_dir() -> Path:
    d = Path(os.environ.get("TIC_CONTRACT_DIR", DEFAULT_CONTRACT_DIR))
    if not d.is_dir():
        pytest.skip(
            f"contract fixtures not found at {d} — set TIC_CONTRACT_DIR or pair "
            "the repos in CI; this skip is NOT a pass"
        )
    return d


# Per-case fact configuration: the facts each fixture's response presumes.
CASES = [
    ("pass", {"balance": 250.0}),
    ("fail", {"balance": 50.0}),
    ("unevaluable-unknown-criterion", {}),
    ("volatile-dispatch", {"fx_rate": 1.30}),
    ("hashes-present", {"balance": 250.0}),
]


@pytest.mark.parametrize("case,facts", CASES, ids=[c for c, _ in CASES])
def test_fixture_pair_round_trips(case: str, facts: dict):
    d = contract_dir()
    request_bytes = (d / f"request-{case}.json").read_bytes().strip()
    response_bytes = (d / f"response-{case}.json").read_bytes().strip()

    # The request fixture must be exactly what the service accepts.
    client = TestClient(create_app(facts=StaticFactSource(facts)))
    r = client.post(
        "/ml/evaluate",
        content=request_bytes,
        headers={"Content-Type": "application/json"},
    )
    assert r.status_code == 200, r.text

    # Byte-identical response: the serialized body IS the contract.
    assert r.content == response_bytes, (
        f"response drifted for {case}:\n got: {r.content!r}\nwant: {response_bytes!r}"
    )


def test_request_fixtures_parse_as_eval_request():
    from tis.models import EvalRequest

    d = contract_dir()
    names = sorted(p.name for p in d.glob("request-*.json"))
    assert len(names) == 5, f"expected the 5 §S.5 request fixtures, found {names}"
    for name in names:
        raw = json.loads((d / name).read_text())
        req = EvalRequest.model_validate(raw)
        want = {"intent_id", "criterion", "threshold", "phase", "volatility"}
        if "hashes" in name:
            want |= {"rule_artifact_hash", "intent_spec_hash"}
        assert set(req.model_dump(exclude_none=True)) == want, name
