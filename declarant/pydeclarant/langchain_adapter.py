"""The LangChain framework adapter (CONTRACT.md section 2.7, "Framework
adapter"; section 5.4 claim 16).

`gate_tool` wraps any LangChain tool so invocation becomes declare -> await
terminal -> proceed-or-refuse: the wrapped tool executes ONLY on PROCEED;
every other class raises IntentRefused and the tool function is never
called. The worst case is an action that wrongly waits, never one that
wrongly executes.

This is the ONE pydeclarant module that is not stdlib-only: it imports
langchain_core (optional -- nothing else in the tree imports it, and its
tests skip visibly where it is absent).
"""

from __future__ import annotations

import json
import uuid

from langchain_core.tools import StructuredTool

from client import Client
from declare import PROCEED, Request, derive_key


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
_RETRY_GUIDANCE = {
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


def _jsonable(value):
    """Recursively convert schema-validated argument values to plain JSON
    data. Pydantic model instances (nested args arrive as models after
    validation) are converted via model_dump(mode="json") -- duck-typed so
    this module does not import pydantic directly."""
    if hasattr(value, "model_dump"):
        return value.model_dump(mode="json")
    if isinstance(value, dict):
        return {k: _jsonable(v) for k, v in value.items()}
    if isinstance(value, (list, tuple)):
        return [_jsonable(v) for v in value]
    return value


def canonical_args(kwargs: dict) -> bytes:
    """The adapter's fixed canonicalization recipe (section 2.7): plain-JSON
    conversion of the schema-validated keyword args, then sorted-key compact
    JSON. Schema validation has already injected defaults, so omit-default
    and explicit-default calls canonicalize identically."""
    return json.dumps(_jsonable(dict(kwargs)), sort_keys=True,
                      separators=(",", ":")).encode("utf-8")


def gate_tool(tool, client: Client, *, intent_spec_hash: str,
              scope: str, run_id: str) -> StructuredTool:
    """Wrap a LangChain tool so every invocation is gated.

    Each invocation declares under its OWN, FRESH intent: the episode seed
    is the derived idempotency key plus a fresh per-invocation nonce.
    Same-key retries are same-key/fresh-episode (section 2.7) -- a REUSED
    seed would redeclare one intent id, replay intent_seq 0 under it, and
    make the verifier refute the whole feed on sequence contiguity (found
    live, 2026-08-18). The fresh seed also scopes the client's 500-edge
    feed consult to this invocation's own records, never another call's.
    """

    def gated(*args, **kwargs):
        if args:
            # String/positional input reaches the func unvalidated in
            # langchain-core and would give the same logical call a second
            # idempotency key. Refuse BEFORE any declaration: nothing
            # declared, nothing executed.
            raise TypeError(
                "gated tool %r requires keyword (dict) input; positional or "
                "string input would fork the idempotency key -- pass a dict"
                % tool.name
            )
        key = derive_key(scope, run_id, tool.name, canonical_args(kwargs))
        res = client.declare(Request(
            episode_seed=key + "-" + uuid.uuid4().hex,  # fresh intent per invocation, never a reused id
            idempotency_key=key,
            intent_spec_hash=intent_spec_hash,
            idempotency_scope=scope,
        ))
        if res.class_ == PROCEED:
            if res.http_status == 200 and res.terminal is not None:
                # A fresh synchronous authorization: execute, once.
                return tool.invoke(kwargs)
            # A Proceed read back from the 500-edge feed consult is a
            # HISTORICAL ACHIEVED: the consequence already fired once.
            # Re-firing it here would break at-most-once at this layer.
            raise IntentRefused(
                ALREADY_ACHIEVED, "ACHIEVED", "",
                False, _RETRY_GUIDANCE[ALREADY_ACHIEVED],
            )
        terminal = res.terminal or {}
        raise IntentRefused(
            res.class_,
            terminal.get("terminal", ""),
            terminal.get("reason", ""),
            res.same_key_retry_safe,
            _RETRY_GUIDANCE.get(res.class_, _RETRY_GUIDANCE["UNKNOWN"]),
        )

    return StructuredTool.from_function(
        func=gated,
        name=tool.name,
        description=tool.description or ("gated: %s" % tool.name),
        args_schema=tool.args_schema,
    )
