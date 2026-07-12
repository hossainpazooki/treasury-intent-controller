"""treasury-intent-scorer: the /ml/evaluate seam's Python side (CONTRACT-SCORER §S.4).

One service = resolver (artifact verify, injected) + scorer (fact vs threshold).
Tri-state closed set PASS | FAIL | UNEVALUABLE; every failure of the network,
the service, or the facts is UNEVALUABLE (spec invariant 2, fail-closed).
"""
