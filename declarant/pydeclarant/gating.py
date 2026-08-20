"""Framework-neutral refusal core shared by the pydeclarant framework
adapters (CONTRACT.md section 2.7, "Framework adapter" and "MCP gate").
It is shared by section 5.4 claims 16 (the LangChain adapter) and 17 (the
MCP gate), so a change here moves BOTH claims. Stdlib-only: no adapter's
framework dependency (langchain_core, fastmcp, ...) may ever appear here.
"""

from __future__ import annotations

from declare import PROCEED


# Adapter-level refusal (CONTRACT.md section 2.7, framework-adapter
# paragraph): a Proceed read back from the 500-edge feed consult is a
# HISTORICAL ACHIEVED -- the consequence already fired once. Layered on top
# of declare.py's closed class table, not part of it.
ALREADY_ACHIEVED = "ALREADY_ACHIEVED"


class IntentRefused(Exception):
    """The gate did not authorize this invocation. Carries the classified
    outcome and the same-key retry position (section 2.7 table) so callers
    can distinguish "retry permitted" from "never with intent to execute"
    mechanically, not by parsing prose."""

    def __init__(self, class_: str, terminal: str, reason: str,
                 same_key_retry_safe: bool, retry_guidance: str):
        self.class_ = class_
        self.terminal = terminal
        self.reason = reason
        self.same_key_retry_safe = same_key_retry_safe
        self.retry_guidance = retry_guidance
        super().__init__(
            "intent refused: class=%s terminal=%s reason=%s retry_safe=%s -- %s"
            % (class_, terminal, reason, same_key_retry_safe, retry_guidance)
        )


# Caller guidance per class (section 2.7 retry column, prose layered on top
# of the same_key_retry_safe position the exception also carries).
RETRY_GUIDANCE = {
    "SHADOW_RECORDED": "recorded, NOT authorized; promotion to enforce is a new attestation",
    "POLICY_DENIED": "criteria bound and failed; retry permitted after facts change",
    "SDK_BUG": "declaration defect; fix the caller, never auto-retry",
    "SPEC_UNATTESTED": "retry after the spec is attested",
    "SPEC_DEFECT": "route to the spec owner; never retry unchanged",
    "HUMAN_JUDGMENT": "a human must resolve the entry via a new attestation",
    "UNEVALUABLE": "backoff retry; never treat as pass",
    "REVOKED": "only after a fresh attestation",
    "FACT_DRIFT": "retry permitted; the key was not reserved",
    "ALREADY_RESERVED": "NEVER retry with intent to execute; reconcile from the feed by key",
    ALREADY_ACHIEVED: "already achieved on a prior attempt; the consequence fired once -- recover the outcome from the feed, never re-execute",
    "INDETERMINATE": "no terminal observed and the feed decided nothing; investigate before retry",
    "UNKNOWN": "outside the closed vocabulary; surface, no auto-retry",
}


def require_fresh_proceed(res) -> None:
    """Return normally iff `res` is a FRESH SYNCHRONOUS authorization to
    execute; otherwise raise IntentRefused. Execution requires a Proceed
    that arrived on THIS call's own synchronous response (HTTP 200 with a
    decoded terminal). A Proceed read back from the 500-edge feed consult
    is a HISTORICAL ACHIEVED -- the consequence already fired once, and
    re-firing it would break at-most-once at the adapter layer."""
    if res.class_ == PROCEED:
        if res.http_status == 200 and res.terminal is not None:
            # A fresh synchronous authorization: execute, once.
            return
        # A Proceed read back from the 500-edge feed consult is a
        # HISTORICAL ACHIEVED: the consequence already fired once.
        # Re-firing it here would break at-most-once at this layer.
        raise IntentRefused(
            ALREADY_ACHIEVED, "ACHIEVED", "",
            False, RETRY_GUIDANCE[ALREADY_ACHIEVED],
        )
    terminal = res.terminal or {}
    raise IntentRefused(
        res.class_,
        terminal.get("terminal", ""),
        terminal.get("reason", ""),
        res.same_key_retry_safe,
        RETRY_GUIDANCE.get(res.class_, RETRY_GUIDANCE["UNKNOWN"]),
    )
