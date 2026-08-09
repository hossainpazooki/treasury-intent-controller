"""Conformance tests for the Python verifier twin (CONTRACT.md section 9.1).

The fixture tests byte-compare this lane's report against the SAME frozen
files the Go lane is held to. If the fixture directory is absent they skip
VISIBLY - a skipped fixture test is never a green one (the hard witness
against deletion is the Go generator test, which fails on absence).
"""

import hashlib
from pathlib import Path

import pytest

from verify import (
    REFUTED,
    UNVERIFIABLE,
    VERIFIED,
    render,
    trajectory_hash,
    verify,
)

FIXTURE_DIR = Path(__file__).resolve().parent.parent.parent / "core" / "contract" / "feed"


def _fixture(name: str) -> bytes:
    p = FIXTURE_DIR / name
    if not p.exists():
        pytest.skip(f"feed fixture {name} absent - a skipped fixture test is never a green one")
    return p.read_bytes()


def test_fixture_good_verifies():
    rep = verify(_fixture("events-good.jsonl"))
    assert rep["overall"] == VERIFIED
    assert render(rep).encode("utf-8") == _fixture("report-good.txt")


def test_fixture_tampered_refutes():
    rep = verify(_fixture("events-tampered.jsonl"))
    assert rep["overall"] == REFUTED
    assert render(rep).encode("utf-8") == _fixture("report-tampered.txt")


def test_hash_uses_byte_lengths_not_char_counts():
    # The named encoding trap (consumer-research memo section 2b): a detail
    # carrying non-ASCII has len(str) != len(bytes). The expected value is
    # built here by explicit byte concatenation, so a len(str) implementation
    # cannot pass.
    detail = "montant:€100"  # euro sign: 1 char, 3 UTF-8 bytes
    recs = [{"intent_seq": 0, "type": "DECLARED", "detail": detail}]
    encoded = b""
    for field in ("0", "DECLARED", detail):
        b = field.encode("utf-8")
        encoded += str(len(b)).encode("ascii") + b":" + b + b"\n"
    assert trajectory_hash(recs) == hashlib.sha256(encoded).hexdigest()


def test_torn_trailing_line_tolerated():
    raw = _fixture("events-good.jsonl") + b'{"seq":99,"intent_'
    assert verify(raw)["overall"] == VERIFIED


def test_unparseable_midfile_abstains():
    raw = _fixture("events-good.jsonl") + b'{"seq":99,"intent_\n'
    rep = verify(raw)
    assert rep["overall"] == UNVERIFIABLE
    assert any(r.startswith("unparseable-line:") for _, _, r in rep["feed"])


def test_empty_feed_unverifiable():
    rep = verify(b"")
    assert rep["overall"] == UNVERIFIABLE
    assert rep["feed"] == [("feed", UNVERIFIABLE, "empty")]
