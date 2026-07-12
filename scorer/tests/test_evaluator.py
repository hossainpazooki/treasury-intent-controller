"""Evaluator unit behavior (CONTRACT-SCORER §S.4).

fact >= threshold => PASS; fact < threshold => FAIL; fact None => UNEVALUABLE.
basis is deterministic fixed-precision text (no wallclock, no randomness).
"""

from tis.evaluator import evaluate
from tis.models import EvalRequest


def req(criterion: str = "balance", threshold: float = 100.0) -> EvalRequest:
    return EvalRequest(
        intent_id="itx-test-0001",
        criterion=criterion,
        threshold=threshold,
        phase="declaration",
        volatility="stable",
    )


def test_fact_at_or_above_threshold_passes():
    resp = evaluate(req(), 250.0)
    assert resp.result == "PASS"
    assert resp.basis == "balance=250.00 >= 100.00"


def test_fact_equal_to_threshold_passes():
    # >= is the contract: the boundary is a PASS, not a FAIL.
    resp = evaluate(req(), 100.0)
    assert resp.result == "PASS"
    assert resp.basis == "balance=100.00 >= 100.00"


def test_fact_below_threshold_fails():
    resp = evaluate(req(), 50.0)
    assert resp.result == "FAIL"
    assert resp.basis == "balance=50.00 < 100.00"


def test_missing_fact_is_unevaluable():
    resp = evaluate(req(criterion="nonexistent", threshold=10.0), None)
    assert resp.result == "UNEVALUABLE"
    assert resp.basis == "unknown criterion: nonexistent"


def test_deterministic_given_same_fact():
    a = evaluate(req(), 123.456)
    b = evaluate(req(), 123.456)
    assert a == b
    assert a.basis == "balance=123.46 >= 100.00"  # fixed 2-decimal rendering
