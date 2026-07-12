"""Criterion evaluation (CONTRACT-SCORER §S.4).

fact >= threshold => PASS; fact < threshold => FAIL; fact None => UNEVALUABLE.
Deterministic given the same fact: fixed 2-decimal basis rendering, no
wallclock, no randomness.
"""

from .models import EvalRequest, EvalResponse


def evaluate(req: EvalRequest, fact: float | None) -> EvalResponse:
    if fact is None:
        return EvalResponse(
            result="UNEVALUABLE", basis=f"unknown criterion: {req.criterion}"
        )
    if fact >= req.threshold:
        return EvalResponse(
            result="PASS", basis=f"{req.criterion}={fact:.2f} >= {req.threshold:.2f}"
        )
    return EvalResponse(
        result="FAIL", basis=f"{req.criterion}={fact:.2f} < {req.threshold:.2f}"
    )
