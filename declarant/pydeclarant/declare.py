"""The Python twin of declarant/declarant.go (CONTRACT.md section 2.7).

Stdlib-only: imports nothing from this module outside declarant/pydeclarant/.
force_scores exists in NO type in this package, by construction (mirrors the
Go twin's guarantee that the guarded test affordance is unreachable through
the SDK).

"""

from __future__ import annotations

import hashlib
import json
from dataclasses import dataclass

# --- Class-name constants (mirror Go `Class` consts, EXACT string values) ---

PROCEED = "PROCEED"                    # Go Proceed          - ACHIEVED, authorized, one durable record
SHADOW_RECORDED = "SHADOW_RECORDED"    # Go ShadowRecorded   - scored, recorded, NOT authorized
POLICY_DENIED = "POLICY_DENIED"        # Go PolicyDenied     - criteria bound and failed
SDK_BUG = "SDK_BUG"                    # Go SDKBug           - key/marshal defect; fix caller
SPEC_UNATTESTED = "SPEC_UNATTESTED"    # Go SpecUnattested
SPEC_DEFECT = "SPEC_DEFECT"            # Go SpecDefect
HUMAN_JUDGMENT = "HUMAN_JUDGMENT"      # Go HumanJudgment
UNEVALUABLE = "UNEVALUABLE"            # Go Unevaluable      - scorer outage/missing fact; backoff, never pass
REVOKED = "REVOKED"                    # Go Revoked
FACT_DRIFT = "FACT_DRIFT"              # Go FactDrift
ALREADY_RESERVED = "ALREADY_RESERVED"  # Go AlreadyReserved
INDETERMINATE = "INDETERMINATE"        # Go Indeterminate    - no terminal observed; consult feed, decide nothing
UNKNOWN = "UNKNOWN"                    # Go Unknown          - outside closed vocab; surface, no auto-retry


@dataclass
class Request:
    """Mirror of Go declarant.Request (declarant.go)."""

    episode_seed: str = ""
    idempotency_key: str = ""
    rule_artifact_hash: str = ""      # optional artifact-plane passthrough; OMITTED from wire when ""
    intent_spec_hash: str = ""        # content address of the attested payload
    idempotency_scope: str = ""       # carried data; folded into the KEY, NOT a keyspace partition
    spec_envelope: object | None = None   # optional S2.6 hybrid path; mirrors Go json.RawMessage; None => omitted


def marshal_request(r: Request) -> bytes:
    """Mirror Go MarshalRequest: produce the exact wire bytes.

    Field order is load-bearing and mirrors Go wireRequest exactly:
    episode_seed, idempotency_key, [rule_artifact_hash], intent_spec_hash,
    spec (always present; inner idempotency_scope always present, even
    when ""), [spec_envelope] (always last, inlined raw JSON). Optional
    fields are OMITTED from the wire (Go `omitempty`), never emitted blank.
    Compact separators, no sorting, no indent -- matches Go json.Marshal.
    """
    ordered: dict[str, object] = {}
    ordered["episode_seed"] = r.episode_seed
    ordered["idempotency_key"] = r.idempotency_key
    if r.rule_artifact_hash != "":
        ordered["rule_artifact_hash"] = r.rule_artifact_hash
    ordered["intent_spec_hash"] = r.intent_spec_hash
    ordered["spec"] = {"idempotency_scope": r.idempotency_scope}
    if r.spec_envelope is not None:
        envelope = r.spec_envelope
        if isinstance(envelope, (bytes, bytearray)):
            envelope = json.loads(envelope.decode("utf-8"))
        elif isinstance(envelope, str):
            envelope = json.loads(envelope)
        ordered["spec_envelope"] = envelope
    return json.dumps(ordered, separators=(",", ":")).encode("utf-8")


def derive_key(scope: str, agent_run_id: str, tool_name: str, canonical_args: bytes) -> str:
    """Mirror Go DeriveKey exactly. canonical_args is bytes; the caller's
    duty to canonicalize -- the SDK hashes the bytes it is given."""
    digest = hashlib.sha256(canonical_args).hexdigest()
    return scope + ":" + agent_run_id + ":" + tool_name + ":" + digest


def intent_id(episode_seed: str) -> str:
    """Mirror Go IntentID: sha256(episode_seed)[:16 hex chars]."""
    return hashlib.sha256(episode_seed.encode("utf-8")).hexdigest()[:16]


def classify(terminal: str, reason: str) -> tuple[str, bool]:
    """Mirror Go Classify branch-for-branch. TOTAL over the closed S3.2/S3.3
    vocabulary; fails closed to UNKNOWN outside it. Specific `unevaluable:`
    prefixes are checked before the generic one -- order is load-bearing.
    """
    if terminal == "ACHIEVED":
        return (PROCEED, False)
    if terminal == "SHADOW_RECORDED":
        return (SHADOW_RECORDED, True)
    if terminal == "FAILED":
        if reason == "unevaluable:absent-key":
            return (SDK_BUG, True)
        if reason == "unevaluable:unattested-spec":
            return (SPEC_UNATTESTED, True)
        if reason in ("unevaluable:invalid-posture", "unevaluable:empty-criteria") or reason.startswith(
            "unevaluable:invalid-volatility:"
        ):
            return (SPEC_DEFECT, True)
        if reason.startswith("unevaluable:human-judgment:"):
            return (HUMAN_JUDGMENT, True)
        if reason.startswith("revoked:"):
            return (REVOKED, True)
        if reason.startswith("unevaluable:"):
            return (UNEVALUABLE, True)
        if reason != "" and ":" not in reason:
            return (POLICY_DENIED, True)
        return (UNKNOWN, False)
    if terminal == "FAILED_AT_DISPATCH":
        if reason.startswith("volatile-recheck:"):
            return (FACT_DRIFT, True)
        if reason == "idempotency-collision":
            return (ALREADY_RESERVED, False)
        if reason.startswith("revoked:"):
            return (REVOKED, True)
        return (UNKNOWN, False)
    return (UNKNOWN, False)


def classify_feed_record(rec: dict) -> tuple[str, bool]:
    """Mirror Go classifyFeedRecord: re-map step-1 refusal shapes back onto
    the synchronous vocabulary, then delegate to classify()."""
    t = rec.get("type", "")
    d = rec.get("detail", "")
    if t in ("ACHIEVED", "SHADOW_RECORDED", "FAILED", "FAILED_AT_DISPATCH"):
        return classify(t, d)
    if t == "UNEVALUABLE":
        if d == "absent-key":
            return classify("FAILED", "unevaluable:absent-key")
        if d.startswith("unattested-spec:"):
            return classify("FAILED", "unevaluable:unattested-spec")
        if d.startswith("invalid-posture:"):
            return classify("FAILED", "unevaluable:invalid-posture")
        if d.startswith("empty-criteria:"):
            return classify("FAILED", "unevaluable:empty-criteria")
        return classify("FAILED", "unevaluable:" + d)
    if t == "REVOKED":
        return classify("FAILED", "revoked:" + d)
    return (UNKNOWN, False)


def golden_request() -> Request:
    """Mirror the Go test helper goldenRequest(); shared by declare and
    client tests. Reproduces the frozen golden bytes exactly."""
    return Request(
        episode_seed="golden-episode",
        idempotency_key=derive_key(
            "per-actor", "run-7", "sample.transfer", b'{"amount":"100.00","unit":"alpha"}'
        ),
        intent_spec_hash="11" * 32,
        idempotency_scope="per-actor",
    )
