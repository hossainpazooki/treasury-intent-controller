"""The MCP gate (CONTRACT.md section 2.7, "MCP gate"; claim 17 of the
published SDK's section 5.4 table -- claim NUMBERS differ between the two
repos by design, so section 2.7 is the citation that resolves in both).

Two entry points, ONE gate implementation. `IntentGateMiddleware` gates an
MCP server the operator owns directly: attach it with `add_middleware`.
`gated_proxy` gates a server the operator does NOT own by fronting it with
`create_proxy` and attaching the SAME middleware to the proxy, so the
backend never needs to change to be gated.

Each gated call declares an intent to the gate service and executes the
tool body ONLY on a fresh synchronous authorization; every other outcome
raises ToolError and the tool body never runs.

This module and its sibling `langchain_adapter.py` are the TWO exceptions to
pydeclarant's stdlib-only rule (CONTRACT.md section 2.7): this one imports
fastmcp and pydantic, that one imports langchain_core. Both are optional --
pydantic is already fastmcp's own dependency, nothing else in the tree
imports any of them, and each adapter's tests skip VISIBLY where its
framework is absent.
"""

from __future__ import annotations

import asyncio
import inspect
import json
import uuid

import pydantic
from fastmcp.exceptions import ToolError
from fastmcp.server import create_proxy
from fastmcp.server.dependencies import without_injected_parameters
from fastmcp.server.middleware import Middleware

from client import Client
from declare import Request, derive_key
from gating import IntentRefused, require_fresh_proceed


class _Unkeyable(Exception):
    """The arguments cannot be canonicalized honestly, so the call is refused
    BEFORE anything is declared. A private type, not ToolError, so that the
    refusal cannot be swallowed by the schema-read except-block around it."""


# --- canonicalization -------------------------------------------------------
#
# WHY THIS IS AS ELABORATE AS IT IS -- do not "simplify" it back.
#
# A key FORK -- one logical action canonicalizing to two different
# idempotency keys -- is FAIL-OPEN, the worst failure class here. The gate
# deduplicates BY KEY: a second, never-seen key has no reservation to collide
# with, so the gate authorizes it and the tool body EXECUTES A SECOND TIME.
# In the reference deployment that means money moves twice. (A key COLLISION,
# two actions sharing one key, is fail-closed instead -- the second action
# bricks. Forks are the direction to engineer against.)
#
# The previous recipe here canonicalized the raw client arguments plus
# TOP-LEVEL schema defaults, and was shipped with a note claiming the
# un-normalized nested case "errs fail-closed". That was inverted reasoning
# and it was wrong: a fork is precisely the case where no duplicate
# declaration can occur. Measured against a live fastmcp 3.4.7 server, that
# recipe double-executed on four argument shapes -- a nested object left
# empty vs spelled out, a nested object spelled only partly, an omitted
# `Field(default_factory=...)` value (pydantic emits NO "default" key for it,
# so the top-level injection silently skipped it), and `500` vs `500.0` for
# one float parameter. CONTRACT.md section 2.7 records the withdrawal.
#
# There are two tiers because the two deployment shapes expose different
# information about the action. The tier is a property of the TOOL, never a
# configuration flag.


def _tier1_args(fn, raw: dict) -> dict:
    """Tier 1 -- the gated server OWNS the tool, so its callable is reachable.

    Rebuild a pydantic model from the tool function's own signature and
    validate the raw arguments against it. The key then becomes a function of
    the EFFECTIVE action: pydantic applies every default (nested model
    defaults and `default_factory` alike) and coerces declared types, so all
    four fork shapes normalize to one dict. This is deliberately the same
    normalization the LangChain sibling gets for free from its framework
    (`langchain_adapter.canonical_args`) -- the same recipe, reached a
    different way.

    Framework-injected parameters (a `Context`) are stripped first: they are
    supplied by the server, not by the caller, and are not part of the
    action's identity.
    """
    try:
        signature = inspect.signature(without_injected_parameters(fn), eval_str=True)
    except Exception as exc:
        # An annotation we cannot resolve means we cannot rebuild the model,
        # and a half-built model would normalize some parameters and not
        # others -- i.e. it could still fork. Refuse, as with an unreadable
        # schema: nothing declared, nothing executed.
        raise _Unkeyable(
            "cannot read the tool signature to canonicalize arguments: %s" % exc
        ) from exc

    fields = {}
    for name, param in signature.parameters.items():
        if param.kind in (param.VAR_POSITIONAL, param.VAR_KEYWORD):
            continue  # *args/**kwargs carry no schema; MCP never populates them
        annotation = object if param.annotation is inspect.Parameter.empty else param.annotation
        default = ... if param.default is inspect.Parameter.empty else param.default
        fields[name] = (annotation, default)

    try:
        model = pydantic.create_model(
            "_GateArgs",
            # extra="forbid" deliberately, matching fastmcp's own call
            # validation. Pydantic's default would silently DROP an undeclared
            # argument, this validation would pass, the gate would declare and
            # be authorized -- and only then would fastmcp reject the call,
            # leaving a committed ACHIEVED in the feed for an action that never
            # happened. A validator laxer than the one downstream of it emits
            # exactly that false record. (Probed on fastmcp 3.4.7: an
            # undeclared argument is rejected with "Unexpected keyword
            # argument" AFTER middleware runs.)
            __config__=pydantic.ConfigDict(extra="forbid"),
            **fields,
        )
        validated = model.model_validate(raw)
        return validated.model_dump(mode="json")
    except Exception as exc:
        # Arguments that cannot validate would be rejected by fastmcp AFTER
        # the gate had already declared and been authorized -- leaving a
        # committed ACHIEVED record in the feed for an action that never
        # happened. Consumers settle from that feed, so refuse first.
        raise _Unkeyable("arguments are not valid for tool: %s" % exc) from exc


def _defs_of(schema: dict) -> dict:
    defs = {}
    for key in ("$defs", "definitions"):  # 2020-12 and the draft-07 spelling
        section = schema.get(key)
        if isinstance(section, dict):
            defs[key] = section
    return defs


def _resolve(schema, defs: dict):
    """Follow `$ref` chains against the root schema's `$defs`/`definitions`.

    Keys spelled beside a `$ref` (pydantic emits `default` there) override the
    target's, so a referenced nested model keeps its call-site default.
    An unresolvable ref is refused, not guessed at -- the same principle as
    the unreadable-schema rule.
    """
    seen = set()
    while isinstance(schema, dict) and "$ref" in schema:
        ref = schema["$ref"]
        if ref in seen:
            raise _Unkeyable("schema $ref cycle at %r" % ref)
        seen.add(ref)
        parts = str(ref).split("/")
        target = None
        if len(parts) == 3 and parts[0] == "#" and parts[1] in defs:
            target = defs[parts[1]].get(parts[2])
        if not isinstance(target, dict):
            raise _Unkeyable(
                "cannot resolve schema $ref %r; the schema cannot be canonicalized" % ref
            )
        siblings = {k: v for k, v in schema.items() if k != "$ref"}
        schema = dict(target)
        schema.update(siblings)
    return schema if isinstance(schema, dict) else {}


def _tier2_value(value, schema, defs: dict, path: str, strict: bool):
    schema = _resolve(schema, defs)
    if "type" not in schema:
        # Optional[T] renders as anyOf[T, null] with no top-level type. Recurse
        # into the single non-null branch so its defaults and numeric type
        # still apply; a genuinely ambiguous union is left AS SPELLED.
        branches = schema.get("anyOf") or schema.get("oneOf")
        if isinstance(branches, list) and value is not None:
            concrete = [
                b for b in branches
                if isinstance(b, dict) and _resolve(b, defs).get("type") != "null"
            ]
            if len(concrete) == 1:
                return _tier2_value(value, concrete[0], defs, path, strict)
    declared = schema.get("type")
    if isinstance(value, dict) and (declared == "object" or "properties" in schema):
        return _tier2_object(value, schema, defs, path, strict)
    if isinstance(value, list) and isinstance(schema.get("items"), dict):
        return [
            _tier2_value(v, schema["items"], defs, "%s[%d]" % (path, i), strict)
            for i, v in enumerate(value)
        ]
    if isinstance(value, bool):
        return value  # bool is an int subclass; never coerce it to a number
    if declared == "number" and isinstance(value, (int, float)):
        return float(value)  # this is what makes 500 and 500.0 ONE key
    if declared == "integer" and isinstance(value, (int, float)) and float(value).is_integer():
        return int(value)
    return value


def _tier2_object(value: dict, schema: dict, defs: dict, path: str, strict: bool) -> dict:
    properties = schema.get("properties")
    properties = properties if isinstance(properties, dict) else {}
    required = schema.get("required")
    required = set(required) if isinstance(required, list) else set()
    out = {}
    for name, prop in properties.items():
        prop = prop if isinstance(prop, dict) else {}
        here = "%s.%s" % (path, name) if path else name
        if name in value:
            out[name] = _tier2_value(value[name], prop, defs, here, strict)
        elif "default" in prop:
            # Recurse into the default too: a default object may itself elide
            # nested defaults, and the spelled-out call would then not match.
            out[name] = _tier2_value(prop["default"], prop, defs, here, strict)
        elif "default" in _resolve(prop, defs):
            out[name] = _tier2_value(_resolve(prop, defs)["default"], prop, defs, here, strict)
        elif name in required:
            # Refuse, and note this is NOT subject to strict_args: an absent
            # REQUIRED argument is an invalid call, not an ambiguous one.
            # Leaving it absent let the call be keyed from partial arguments,
            # declared, and AUTHORIZED -- and only then rejected by fastmcp,
            # committing an ACHIEVED for a consequence that never fired.
            # Measured on the proxy path before this fix: 1 POST, 0 executions.
            # Tier 1 gets this for free by validating; Tier 2 has to check.
            raise _Unkeyable(
                "required argument %r is missing, so this call cannot be keyed or "
                "executed; nothing was declared" % here
            )
        elif strict:
            # The remote's `default_factory` case: JSON Schema declares no
            # default, so the effective value is UNKNOWABLE from here. Keying
            # it as spelled would fork against the caller who spells it out.
            raise _Unkeyable(
                "argument %r was omitted and the tool's schema declares no default for it, "
                "so its effective value cannot be determined and two spellings of this call "
                "would derive different idempotency keys (fail-OPEN: the action could execute "
                "twice). Pass %r explicitly, or construct the gate with strict_args=False to "
                "accept keying calls exactly as spelled." % (here, here)
            )
    for name, extra in value.items():
        if name not in properties:
            out[name] = extra  # undeclared argument: keyed exactly as spelled
    return out


def _tier2_args(raw: dict, schema, strict: bool) -> dict:
    """Tier 2 -- the tool is reached only through a proxy, so the operator does
    NOT own it and there is no callable to rebuild a model from (`ProxyTool`
    has no `fn`; confirmed on fastmcp 3.4.7, which is the headline proxy
    path). All that is exposed is a JSON Schema, so defaults are injected
    RECURSIVELY through `$defs`/`$ref` and numeric values are coerced to their
    declared type. What a schema cannot express -- a remote `default_factory`
    -- is refused rather than guessed at, unless the operator has explicitly
    opted out with strict_args=False.
    """
    schema = schema if isinstance(schema, dict) else {}
    defs = _defs_of(schema)
    return _tier2_object(raw, _resolve(schema, defs), defs, "", strict)


def _canonical_args(arguments: dict | None, tool, strict_args: bool) -> bytes:
    """Canonicalize one call's arguments to the bytes the idempotency key is
    derived from: Tier 1 where the tool's callable is reachable, Tier 2 from
    the JSON Schema otherwise, then sorted-key compact JSON either way.

    MCP delivers the RAW client-sent arguments to middleware -- no defaults
    are applied and no types are coerced before the hook runs (probed on
    fastmcp 3.4.7). Every normalization here exists because without it one
    logical action derives two keys; see the block comment above for why that
    is fail-OPEN.
    """
    raw = dict(arguments) if arguments else {}
    fn = getattr(tool, "fn", None)
    if fn is not None:
        args = _tier1_args(fn, raw)
    else:
        args = _tier2_args(raw, getattr(tool, "parameters", None), strict_args)
    return json.dumps(args, sort_keys=True, separators=(",", ":")).encode("utf-8")


def _refusal_message(refused: IntentRefused) -> str:
    """Section 2.7: this string is the caller-facing contract
    (the MCP client sees it verbatim via ToolError). class=<CLASS> and
    retry_safe=<true|false> must appear as those literal substrings, with
    booleans lowercased -- gating.IntentRefused's own message renders the
    Python bool capitalized, so it cannot be reused verbatim here."""
    return "intent refused: class=%s terminal=%s reason=%s retry_safe=%s -- %s" % (
        refused.class_,
        refused.terminal,
        refused.reason,
        "true" if refused.same_key_retry_safe else "false",
        refused.retry_guidance,
    )


async def _gate_call(context, call_next, *, client: Client, intent_spec_hash: str,
                      scope: str, run_id: str, tools, strict_args: bool):
    name = context.message.name
    if tools is not None and name not in tools:
        return await call_next(context)  # not in the gated set: pass through UNGATED

    try:
        tool = await context.fastmcp_context.fastmcp.get_tool(name)
        if tool is None:
            raise LookupError("no such tool: %r" % name)
        tool.parameters  # read it here so an unreadable schema refuses below
    except Exception as exc:
        # Section 2.7: a schema we cannot read means we cannot
        # canonicalize honestly. Refuse BEFORE declaring -- nothing
        # declared, nothing executed -- rather than guess at a
        # canonicalization and risk a forked idempotency key.
        raise ToolError(
            "intent gate: cannot read schema for tool %r: %s" % (name, exc)
        ) from exc

    try:
        payload = _canonical_args(context.message.arguments, tool, strict_args)
    except _Unkeyable as exc:
        # Also BEFORE any declaration. Declaring first and failing later would
        # commit an ACHIEVED record for an action that never happened, and
        # consumers settle from that feed.
        raise ToolError("intent gate: tool %r: %s" % (name, exc)) from exc

    key = derive_key(scope, run_id, name, payload)
    req = Request(
        # Fresh intent per invocation: the episode seed is the derived key
        # plus a per-call nonce, never the bare key. A seed derived from
        # the key alone redeclares one intent id on same-args retries and
        # makes the verifier refute the whole event feed on sequence
        # contiguity -- that happened live on 2026-08-18 and cost a design
        # fix. Never simplify this away, and never let it depend on
        # middleware instance state (two independent middleware instances
        # with identical scope/run_id/spec_hash must still mint distinct
        # seeds for the same logical call).
        episode_seed=key + "-" + uuid.uuid4().hex,
        idempotency_key=key,
        intent_spec_hash=intent_spec_hash,
        idempotency_scope=scope,
    )
    # The gate client is BLOCKING (urllib-based); the middleware hook is
    # async. Run it off the event loop rather than blocking the loop for
    # every other in-flight call.
    res = await asyncio.to_thread(client.declare, req)
    try:
        require_fresh_proceed(res)
    except IntentRefused as refused:
        raise ToolError(_refusal_message(refused)) from refused
    return await call_next(context)


class IntentGateMiddleware(Middleware):
    """Gates every `on_call_tool` invocation on a server the operator owns.

    `tools=None` (default) gates every tool; a collection of names gates
    only those, and an unlisted tool passes through UNGATED (no POST is
    ever made for it).

    `strict_args` affects TIER 2 ONLY (a tool reached through a proxy, whose
    callable is out of reach). Left at its default True, a call that omits a
    property the schema declares no default for is REFUSED before declaring,
    because its effective value is unknowable and keying it as spelled would
    fork. Setting it False is the operator accepting, explicitly, that two
    spellings of one action may key differently and so may execute twice.
    Tier 1 knows the effective value, so it never refuses for this reason.
    """

    def __init__(self, client: Client, *, intent_spec_hash: str, scope: str,
                 run_id: str, tools=None, strict_args: bool = True):
        if isinstance(tools, (str, bytes)):
            # A str IS a collection -- of CHARACTERS. tools="wire_transfer"
            # would build {'w','i','r','e',...}, no tool name would ever match,
            # and every call would take the UNGATED pass-through: measured at 0
            # gate POSTs and 1 tool execution, with no error at construction or
            # at call time. That silently disables the gate on precisely the
            # tool the operator meant to protect, so it must be impossible to
            # write rather than merely documented.
            raise TypeError(
                "tools must be a collection of tool NAMES, not a string: pass "
                "tools={%r} (a set) rather than tools=%r, which would be read as "
                "a set of single characters and would leave every tool UNGATED"
                % (tools, tools)
            )
        self._client = client
        self._intent_spec_hash = intent_spec_hash
        self._scope = scope
        self._run_id = run_id
        self._tools = set(tools) if tools is not None else None
        self._strict_args = strict_args

    async def on_call_tool(self, context, call_next):
        return await _gate_call(
            context, call_next,
            client=self._client, intent_spec_hash=self._intent_spec_hash,
            scope=self._scope, run_id=self._run_id, tools=self._tools,
            strict_args=self._strict_args,
        )


def gated_proxy(backend, client: Client, *, intent_spec_hash: str, scope: str,
                run_id: str, tools=None, strict_args: bool = True):
    """create_proxy(backend) with the SAME middleware attached, so a server
    the operator does not own is gated without changing it. The backend
    never sees a call the gate refuses.

    This is the Tier 2 path by construction -- a ProxyTool exposes a JSON
    Schema and no callable -- so `strict_args` is live here; see
    IntentGateMiddleware."""
    proxy = create_proxy(backend)
    proxy.add_middleware(
        IntentGateMiddleware(
            client, intent_spec_hash=intent_spec_hash, scope=scope, run_id=run_id,
            tools=tools, strict_args=strict_args,
        )
    )
    return proxy
