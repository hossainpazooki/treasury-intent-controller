"""Wire models for POST /ml/evaluate (CONTRACT-SCORER §S.1).

The Go gate marshals EvalRequest; unknown fields are IGNORED for forward
compatibility. The tri-state result is the closed string set PASS | FAIL |
UNEVALUABLE. basis is observability only — free text that must never reach the
gate's audit log, durable feed, or any hash.
"""

from typing import Literal

from pydantic import BaseModel, ConfigDict


class EvalRequest(BaseModel):
    model_config = ConfigDict(extra="ignore")

    intent_id: str
    criterion: str
    threshold: float
    phase: str
    volatility: str
    # Opaque passthroughs (§S.1): present once Stage A lands; verification
    # failure with a configured resolver => UNEVALUABLE.
    rule_artifact_hash: str | None = None
    intent_spec_hash: str | None = None


class EvalResponse(BaseModel):
    result: Literal["PASS", "FAIL", "UNEVALUABLE"]
    basis: str | None = None
