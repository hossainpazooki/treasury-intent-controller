"""Conformance tests for the Python declarant twin's declare.py module
(CONTRACT.md section 2.7). Mirrors declarant/declarant_test.go pin-for-pin
where a Python stdlib twin can honestly mirror it; two pins (the reflection
pin over struct field names/tags, and json.RawMessage passthrough) have no
literal Go equivalent and are covered by their honest stdlib substitutes,
noted inline below.
"""

import hashlib
from dataclasses import fields
from pathlib import Path

import pytest

from declare import (
    ALREADY_RESERVED,
    FACT_DRIFT,
    HUMAN_JUDGMENT,
    POLICY_DENIED,
    PROCEED,
    REVOKED,
    SDK_BUG,
    SHADOW_RECORDED,
    SPEC_DEFECT,
    SPEC_UNATTESTED,
    UNEVALUABLE,
    UNKNOWN,
    Request,
    classify,
    derive_key,
    golden_request,
    intent_id,
    marshal_request,
)

GOLDEN_PATH = Path(__file__).resolve().parent.parent / "testdata" / "request-golden.json"
FEED_PATH = (
    Path(__file__).resolve().parent.parent.parent / "core" / "contract" / "feed" / "events-good.jsonl"
)


def _golden_bytes() -> bytes:
    if not GOLDEN_PATH.exists():
        pytest.skip(f"declarant golden fixture absent: {GOLDEN_PATH}")
    return GOLDEN_PATH.read_bytes()


def _feed_first_line() -> str:
    if not FEED_PATH.exists():
        pytest.skip(f"feed fixture absent: {FEED_PATH}")
    with FEED_PATH.open("r", encoding="utf-8") as f:
        line = f.readline()
    if not line:
        pytest.skip(f"feed fixture empty: {FEED_PATH}")
    return line


def test_golden_request_bytes():
    got = marshal_request(golden_request())
    want = _golden_bytes()
    assert got == want
    # Empty optional is OMITTED from the wire, not emitted blank.
    assert b"rule_artifact_hash" not in got


def test_marshal_omits_empty_optionals_and_orders_fields():
    r = Request(
        episode_seed="s",
        idempotency_key="k",
        rule_artifact_hash="",
        intent_spec_hash="h",
        idempotency_scope="",
        spec_envelope=None,
    )
    got = marshal_request(r)

    assert b"rule_artifact_hash" not in got
    assert b"spec_envelope" not in got

    # spec.idempotency_scope is ALWAYS present, even when set to "".
    assert b'"spec":{"idempotency_scope":""}' in got

    idx_episode = got.index(b"episode_seed")
    idx_key = got.index(b"idempotency_key")
    idx_intent_hash = got.index(b"intent_spec_hash")
    idx_spec = got.index(b'"spec"')
    assert idx_episode < idx_key < idx_intent_hash < idx_spec

    # A non-empty rule_artifact_hash lands BETWEEN idempotency_key and intent_spec_hash.
    r2 = Request(
        episode_seed="s",
        idempotency_key="k",
        rule_artifact_hash="rah",
        intent_spec_hash="h",
        idempotency_scope="",
    )
    got2 = marshal_request(r2)
    idx_key2 = got2.index(b"idempotency_key")
    idx_rah2 = got2.index(b"rule_artifact_hash")
    idx_hash2 = got2.index(b"intent_spec_hash")
    assert idx_key2 < idx_rah2 < idx_hash2


def test_derive_key_shape():
    args = b'{"a":1}'
    want = "scope-x:run-1:tool.name:" + hashlib.sha256(args).hexdigest()
    got1 = derive_key("scope-x", "run-1", "tool.name", args)
    got2 = derive_key("scope-x", "run-1", "tool.name", args)
    assert got1 == want
    assert got2 == want  # deterministic, never random


def test_intent_id_matches_gate_derivation():
    line1 = _feed_first_line()
    import json

    first = json.loads(line1)
    assert intent_id("fixture-achieved") == first["intent_id"]
    assert first["detail"] == first["intent_id"]


def test_classification_total():
    table = [
        (("ACHIEVED", ""), (PROCEED, False)),
        (("SHADOW_RECORDED", ""), (SHADOW_RECORDED, True)),
        (("FAILED", "unevaluable:absent-key"), (SDK_BUG, True)),
        (("FAILED", "unevaluable:unattested-spec"), (SPEC_UNATTESTED, True)),
        (("FAILED", "unevaluable:invalid-posture"), (SPEC_DEFECT, True)),
        (("FAILED", "unevaluable:empty-criteria"), (SPEC_DEFECT, True)),
        (("FAILED", "unevaluable:invalid-volatility:alpha"), (SPEC_DEFECT, True)),
        (("FAILED", "unevaluable:human-judgment:beta"), (HUMAN_JUDGMENT, True)),
        (("FAILED", "unevaluable:alpha"), (UNEVALUABLE, True)),
        (("FAILED", "revoked:pull-1"), (REVOKED, True)),
        (("FAILED", "alpha,beta"), (POLICY_DENIED, True)),
        (("FAILED_AT_DISPATCH", "volatile-recheck:gamma"), (FACT_DRIFT, True)),
        (("FAILED_AT_DISPATCH", "idempotency-collision"), (ALREADY_RESERVED, False)),
        (("FAILED_AT_DISPATCH", "revoked:pull-1"), (REVOKED, True)),
        (("FAILED", "wat:is-this"), (UNKNOWN, False)),
        (("FAILED", ""), (UNKNOWN, False)),
        (("FAILED_AT_DISPATCH", "novel-cause"), (UNKNOWN, False)),
        (("EXPLODED", "boom"), (UNKNOWN, False)),
    ]
    for (terminal, reason), want in table:
        got = classify(terminal, reason)
        assert got == want, f"classify({terminal!r}, {reason!r}) == {got!r}, want {want!r}"


def test_policy_denied_colon_edge():
    # A criterion name containing ':' fails closed to UNKNOWN, NOT PolicyDenied.
    assert classify("FAILED", "a:b") == (UNKNOWN, False)
    assert classify("FAILED", "plain-criterion") == (POLICY_DENIED, True)


def test_out_of_vocabulary_reason_fails_closed():
    assert classify("FAILED", "zzz:qqq:rrr") == (UNKNOWN, False)
    assert classify("WEIRD_TERMINAL", "anything") == (UNKNOWN, False)


def test_no_force_field_anywhere():
    # Honest twin of Go's TestNoForceScoresInAnyType. Go inspects struct
    # field names and json tags via reflection; Python has no wire struct
    # with tags, so the honest equivalent is: (a) no Request dataclass
    # field name contains "force", and (b) marshal_request never emits a
    # "force" key on the wire, even for a fully-populated Request.
    assert all("force" not in f.name.lower() for f in fields(Request))

    r = Request(
        episode_seed="s",
        idempotency_key="k",
        rule_artifact_hash="rah",
        intent_spec_hash="h",
        idempotency_scope="scope",
        spec_envelope={"extra": "field"},
    )
    got = marshal_request(r)
    assert b"force" not in got

    import json

    assert "force" not in json.dumps(json.loads(got))
