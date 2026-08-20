"""The LangChain framework adapter (CONTRACT.md section 2.7, "Framework
adapter"; claim 16 of the published SDK's section 5.4 table -- claim numbers
differ between the two repos by design; section 2.7 resolves in both).

`gate_tool` wraps any LangChain tool so invocation becomes declare -> await
terminal -> proceed-or-refuse: the wrapped tool executes ONLY on PROCEED;
every other class raises IntentRefused and the tool function is never
called. The worst case is an action that wrongly waits, never one that
wrongly executes.

This module and its sibling `mcp_adapter.py` are the TWO exceptions to
pydeclarant's stdlib-only rule (CONTRACT.md section 2.7): this one imports
langchain_core, that one imports fastmcp. Both are optional -- nothing else
in the tree imports either, and each one's tests skip visibly where its
dependency is absent.
"""

from __future__ import annotations

import json
import uuid

from langchain_core.tools import StructuredTool

from client import Client
from declare import Request, derive_key
from gating import ALREADY_ACHIEVED, IntentRefused, RETRY_GUIDANCE, require_fresh_proceed


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
        require_fresh_proceed(res)
        return tool.invoke(kwargs)

    return StructuredTool.from_function(
        func=gated,
        name=tool.name,
        description=tool.description or ("gated: %s" % tool.name),
        args_schema=tool.args_schema,
    )
