"""Tests for the MCP gate (CONTRACT.md section 2.7, "MCP
gate"; claim 17 of the published SDK's section 5.4 table -- claim numbers
differ between the two repos by design; section 2.7 resolves in both).

Skips VISIBLY where fastmcp is absent -- the adapter is optional and a
skipped adapter test is never a green one. Everything else runs against an
in-process stdlib http.server gate double that CAPTURES method, path, and
POST body, so the wire bytes and the consult call order are asserted, never
assumed. There is no pytest-asyncio in this project: async bodies are
driven with asyncio.run(...) inside sync test functions, mirroring how
test_langchain_adapter.py drives its one async test.
"""

from __future__ import annotations

import asyncio
import json
import pathlib

import pytest

pytest.importorskip(
    "fastmcp", reason="fastmcp absent (the adapter is optional; install it to run this lane)"
)

from fastmcp import Client as MCPClient
from fastmcp import FastMCP
from fastmcp.exceptions import ToolError
from pydantic import BaseModel, Field

from client import Client
from declare import (
    ALREADY_RESERVED,
    INDETERMINATE,
    POLICY_DENIED,
    SHADOW_RECORDED,
    UNEVALUABLE,
    UNKNOWN,
    derive_key,
    intent_id,
    marshal_request,
    Request,
)
from gating import ALREADY_ACHIEVED
from mcp_adapter import IntentGateMiddleware, gated_proxy, _tier2_args, _Unkeyable
from _gate_double import _Script, _serve

SPEC_HASH = "ab" * 32
SCOPE = "per-actor"
RUN_ID = "run-mcp-1"

ACHIEVED_BODY = {"terminal": "ACHIEVED", "reason": ""}


class _Counted:
    """A tool body that counts its own executions and records the EFFECTIVE
    arguments it received. Module-level purely for reuse across the lane's
    server factories -- it appears in no annotation, so the Python 3.14
    constraint does not apply to it. (That constraint is about a pydantic
    model used AS a tool parameter's annotation, which must be defined at
    module scope so the annotation can resolve; see _Opts below.)"""

    def __init__(self):
        self.calls = 0
        self.seen = []  # the EFFECTIVE arguments each execution received


def _gate_client(url):
    return Client(url, timeout=5.0)


def _gated_server(url, tools=None):
    """One gated tool, sample_transfer(amount, unit='alpha')."""
    counted = _Counted()
    server = FastMCP("test")

    @server.tool
    def sample_transfer(amount: str, unit: str = "alpha") -> str:
        counted.calls += 1
        return "moved %s %s" % (amount, unit)

    server.add_middleware(
        IntentGateMiddleware(
            _gate_client(url), intent_spec_hash=SPEC_HASH, scope=SCOPE, run_id=RUN_ID, tools=tools
        )
    )
    return server, counted


def _two_tool_server(url, tools):
    gated_counted = _Counted()
    ungated_counted = _Counted()
    server = FastMCP("test")

    @server.tool
    def gated_one(x: str) -> str:
        gated_counted.calls += 1
        return "g " + x

    @server.tool
    def ungated_one(x: str) -> str:
        ungated_counted.calls += 1
        return "u " + x

    server.add_middleware(
        IntentGateMiddleware(
            _gate_client(url), intent_spec_hash=SPEC_HASH, scope=SCOPE, run_id=RUN_ID, tools=tools
        )
    )
    return server, gated_counted, ungated_counted


def _call(server, name, args):
    async def run():
        async with MCPClient(server) as c:
            return await c.call_tool(name, args)

    return asyncio.run(run())


def _expected_key(tool_name, args):
    """The expected key for a call whose arguments are ALL spelled explicitly
    and already in their canonical JSON types. It deliberately does NOT model
    the adapter's normalization (default injection, numeric coercion), so it
    is silently WRONG for a call that elides a default -- use it only for
    fully-explicit calls, and assert key relationships (equal / not equal)
    from the posted bodies everywhere else."""
    payload = json.dumps(args, sort_keys=True, separators=(",", ":")).encode("utf-8")
    return derive_key(SCOPE, RUN_ID, tool_name, payload)


def _posted_key(script, i):
    return json.loads(script.calls[i][2])["idempotency_key"]


def _posted_seed(script, i):
    return json.loads(script.calls[i][2])["episode_seed"]


# --- claim 17: proceed executes, wire bytes exact --------------


def test_proceed_executes_once_and_wire_bytes_match_marshal():
    script = _Script([(200, ACHIEVED_BODY)])
    gate_server, url = _serve(script)
    try:
        server, counted = _gated_server(url)
        out = _call(server, "sample_transfer", {"amount": "100.00", "unit": "beta"})
    finally:
        gate_server.shutdown()
    assert out.data == "moved 100.00 beta"
    assert counted.calls == 1
    key = _expected_key("sample_transfer", {"amount": "100.00", "unit": "beta"})
    posts = [c for c in script.calls if c[0] == "POST"]
    assert len(posts) == 1 and posts[0][1] == "/v2/intents"
    posted = json.loads(posts[0][2])
    assert posted["idempotency_key"] == key
    seed = posted["episode_seed"]
    # Fresh intent per invocation: the seed carries the key plus a nonce,
    # never the bare key (a bare-key seed redeclares the intent id on a
    # same-args retry -- the live verifier refuted that, 2026-08-18).
    assert seed.startswith(key + "-") and len(seed) > len(key) + 1
    want = marshal_request(
        Request(
            episode_seed=seed,
            idempotency_key=key,
            intent_spec_hash=SPEC_HASH,
            idempotency_scope=SCOPE,
        )
    )
    assert posts[0][2] == want  # byte-exact: the adapter speaks marshal_request's wire


# --- claim 17: every non-Proceed refuses with call count ZERO --


@pytest.mark.parametrize(
    "status,body,want_class,want_retry",
    [
        (200, {"terminal": "FAILED", "reason": "balance"}, POLICY_DENIED, True),
        (200, {"terminal": "FAILED_AT_DISPATCH", "reason": "idempotency-collision"}, ALREADY_RESERVED, False),
        (200, {"terminal": "FAILED", "reason": "unevaluable:balance"}, UNEVALUABLE, True),
        (200, {"terminal": "SHADOW_RECORDED", "reason": ""}, SHADOW_RECORDED, True),
        (500, None, INDETERMINATE, False),  # empty feed for this intent -> INDETERMINATE stands
        (200, {"terminal": "SOMETHING_NEW", "reason": "x"}, UNKNOWN, False),  # out-of-vocab: the fail-closed pin
    ],
)
def test_refusals_never_execute(status, body, want_class, want_retry):
    script = _Script([(status, body if body is not None else {})])
    gate_server, url = _serve(script)
    try:
        server, counted = _gated_server(url)
        with pytest.raises(ToolError) as exc:
            _call(server, "sample_transfer", {"amount": "1", "unit": "beta"})
    finally:
        gate_server.shutdown()
    assert counted.calls == 0  # the whole point: refused means NEVER executed
    msg = str(exc.value)
    assert ("class=%s" % want_class) in msg
    assert ("retry_safe=%s" % ("true" if want_retry else "false")) in msg


# --- key determinism, four legs ----------------------------------------------


def test_key_determinism_four_legs():
    script = _Script([(200, ACHIEVED_BODY)] * 5)
    gate_server, url = _serve(script)
    try:
        server, counted = _gated_server(url)
        _call(server, "sample_transfer", {"amount": "5", "unit": "beta"})       # 0
        _call(server, "sample_transfer", {"amount": "5", "unit": "beta"})       # 1: identical to 0
        _call(server, "sample_transfer", {"amount": "5"})                       # 2: unit elided (default alpha)
        _call(server, "sample_transfer", {"amount": "5", "unit": "alpha"})      # 3: default passed explicitly
        _call(server, "sample_transfer", {"amount": "6", "unit": "beta"})       # 4: changed amount
    finally:
        gate_server.shutdown()
    # (a) identical calls post the same idempotency key.
    assert _posted_key(script, 0) == _posted_key(script, 1)
    # (b) schema-default injection: elided-default and explicit-default post
    # the SAME key. This fails without canonicalization decision 1.
    assert _posted_key(script, 2) == _posted_key(script, 3)
    # (c) a changed argument value posts a different key.
    assert _posted_key(script, 0) != _posted_key(script, 4)
    # (d) identical calls still post DIFFERENT episode seeds / intent ids.
    seed0, seed1 = _posted_seed(script, 0), _posted_seed(script, 1)
    assert seed0 != seed1
    assert intent_id(seed0) != intent_id(seed1)


# --- the fail-open pin: 500-edge consult is scoped to the CALLING intent -----


def test_500_consult_carries_the_calling_invocations_own_intent_id():
    # No feed entries at all: whatever the consult finds, it must be looking
    # under call 2's OWN fresh intent id -- and finding nothing, refuse.
    script = _Script([(200, ACHIEVED_BODY), (500, {})])
    gate_server, url = _serve(script)
    try:
        server, counted = _gated_server(url)
        _call(server, "sample_transfer", {"amount": "1", "unit": "beta"})
        with pytest.raises(ToolError) as exc:
            _call(server, "sample_transfer", {"amount": "2", "unit": "beta"})
    finally:
        gate_server.shutdown()
    assert counted.calls == 1  # call 2 never executed
    assert ("class=%s" % INDETERMINATE) in str(exc.value)
    posts = [c for c in script.calls if c[0] == "POST"]
    seed1, seed2 = json.loads(posts[0][2])["episode_seed"], json.loads(posts[1][2])["episode_seed"]
    gets = [c for c in script.calls if c[0] == "GET"]
    assert len(gets) == 1
    # The consult URL must carry call 2's OWN intent id -- a shared-seed
    # implementation consults call 1's id here and inherits its terminal.
    assert intent_id(seed2) in gets[0][1]
    assert intent_id(seed1) not in gets[0][1]


# --- historical Proceed never re-fires the consequence -----------------------


def test_same_key_retry_500_with_own_achieved_refuses_not_reexecutes():
    # Call 1 achieves and executes. The RETRY of the same call 500s, and the
    # consult finds the caller's OWN committed ACHIEVED. That is a historical
    # authorization: the consequence already fired once; re-firing it here
    # would break at-most-once at the adapter layer. Refuse, never re-run.
    own_achieved = {
        "type": "ACHIEVED",
        "detail": "",
        "intent_seq": 7,
        "trajectory_hash": "cc" * 32,
    }
    script = _Script([(200, ACHIEVED_BODY), (500, {})], own_feed=[own_achieved])
    gate_server, url = _serve(script)
    try:
        server, counted = _gated_server(url)
        _call(server, "sample_transfer", {"amount": "9", "unit": "beta"})
        with pytest.raises(ToolError) as exc:
            _call(server, "sample_transfer", {"amount": "9", "unit": "beta"})
    finally:
        gate_server.shutdown()
    assert counted.calls == 1  # the consequence fired exactly once
    # The two calls must share a KEY for this to be the retry the test name
    # claims. Without this the test passes identically when call 2 has
    # different arguments -- i.e. it would be green in exactly the forked
    # condition it exists to guard against.
    assert _posted_key(script, 0) == _posted_key(script, 1)
    msg = str(exc.value)
    assert ("class=%s" % ALREADY_ACHIEVED) in msg
    assert "retry_safe=false" in msg


# --- tools= filter: an unlisted tool passes through UNGATED ------------------


def test_tools_filter_gates_only_listed():
    script = _Script([(200, ACHIEVED_BODY)])
    gate_server, url = _serve(script)
    try:
        server, gated_counted, ungated_counted = _two_tool_server(url, tools={"gated_one"})
        out_ungated = _call(server, "ungated_one", {"x": "a"})
        assert script.calls == []  # unlisted tool: NO POST observed at all
        out_gated = _call(server, "gated_one", {"x": "b"})
    finally:
        gate_server.shutdown()
    assert out_ungated.data == "u a"
    assert ungated_counted.calls == 1
    assert out_gated.data == "g b"
    assert gated_counted.calls == 1
    posts = [c for c in script.calls if c[0] == "POST"]
    assert len(posts) == 1


# --- schema-fetch failure refuses BEFORE declaring ---------------------------


class _StubMessage:
    def __init__(self, name, arguments):
        self.name = name
        self.arguments = arguments


class _StubFastmcpContext:
    def __init__(self, fastmcp):
        self.fastmcp = fastmcp


class _StubContext:
    """Minimal stand-in for fastmcp's MiddlewareContext: only the attributes
    IntentGateMiddleware.on_call_tool actually reads."""

    def __init__(self, name, arguments, fastmcp):
        self.message = _StubMessage(name, arguments)
        self.fastmcp_context = _StubFastmcpContext(fastmcp)


def test_unreadable_schema_refuses_before_declaration():
    script = _Script([])
    gate_server, url = _serve(script)
    try:
        empty_server = FastMCP("test")  # no tools registered: get_tool returns None
        mw = IntentGateMiddleware(
            _gate_client(url), intent_spec_hash=SPEC_HASH, scope=SCOPE, run_id=RUN_ID
        )
        stub_ctx = _StubContext("missing_tool", {"amount": "1"}, empty_server)

        async def call_next(ctx):
            raise AssertionError("call_next must never run when the schema cannot be read")

        with pytest.raises(ToolError) as exc:
            asyncio.run(mw.on_call_tool(stub_ctx, call_next))
    finally:
        gate_server.shutdown()
    assert script.calls == []  # nothing declared, nothing executed
    # Assert the REASON, not just the exception type: every refusal in this
    # module raises ToolError, so the type alone would pass on an unrelated
    # failure and still look like the schema rule working.
    msg = str(exc.value)
    assert "cannot read schema" in msg
    assert "missing_tool" in msg


# --- gated proxy: fronts a server the operator does not own ------------------


def test_gated_proxy_stops_calls_before_the_backend_sees_them():
    inner_counted = _Counted()
    inner = FastMCP("inner")

    @inner.tool
    def do_thing(x: str) -> str:
        inner_counted.calls += 1
        return "did " + x

    script = _Script([(200, ACHIEVED_BODY), (200, {"terminal": "FAILED", "reason": "balance"})])
    gate_server, url = _serve(script)
    try:
        proxy = gated_proxy(
            inner, _gate_client(url), intent_spec_hash=SPEC_HASH, scope=SCOPE, run_id=RUN_ID
        )

        async def run():
            async with MCPClient(proxy) as c:
                tools = await c.list_tools()
                out = await c.call_tool("do_thing", {"x": "hi"})
                refused = False
                try:
                    await c.call_tool("do_thing", {"x": "bye"})
                except ToolError:
                    refused = True
                return [t.name for t in tools], out, refused

        names, out, refused = asyncio.run(run())
    finally:
        gate_server.shutdown()
    assert names == ["do_thing"]  # proxy mirrors the inner server's tools
    assert out.data == "did hi"
    # The inner counter is the load-bearing observable: it proves the gate
    # stops the call before the server the operator does not own ever sees
    # it, both on the Proceed pass-through and on the refusal.
    assert inner_counted.calls == 1
    assert refused
    assert inner_counted.calls == 1  # unchanged by the refused second call


# --- the KEY-FORK regression battery -----------------------------------------
#
# A key FORK is FAIL-OPEN: the gate deduplicates BY KEY, so one logical action
# that derives two keys has no reservation to collide with on its second
# spelling -- the gate authorizes it and the tool body executes AGAIN. Until
# 2026-08-20 the adapter canonicalized raw arguments plus TOP-LEVEL schema
# defaults, and the four pairs below each derived two keys against a live
# fastmcp 3.4.7 server. The old key test passed throughout because it probed
# only a top-level SCALAR default -- the one shape that recipe got right.
# These are the tests that would have caught it. Each pair is its OWN named
# case so a regression names the shape it broke.


class _Opts(BaseModel):
    """A nested parameter model with its own defaults. MODULE level, never
    function-local: under Python 3.14 anything a tool signature's annotation
    names must resolve at module scope."""

    mode: str = "fast"
    depth: int = 3


def _fork_server(url):
    """One gated tool exercising all four fork shapes at once: a nested model
    with its own defaults, a `default_factory` parameter (for which pydantic
    emits NO "default" key in the JSON schema), and a float parameter."""
    counted = _Counted()
    server = FastMCP("fork")

    @server.tool
    def fork_transfer(amount: float, opts: _Opts = _Opts(),
                      tags: list[str] = Field(default_factory=list),
                      note: str | None = None) -> str:
        counted.calls += 1
        counted.seen.append((amount, opts.model_dump(), list(tags), note))
        return "moved"

    server.add_middleware(
        IntentGateMiddleware(
            _gate_client(url), intent_spec_hash=SPEC_HASH, scope=SCOPE, run_id=RUN_ID
        )
    )
    return server, counted


def _keys_for_pair(factory, name, args_a, args_b, key_aware=False):
    """Run one action twice, spelled two ways.

    With key_aware=True the gate double REFUSES any repeat of a key it has
    already reserved, exactly as the real gate does -- so a pair that shares a
    key executes ONCE and the second call raises, while a pair that forks
    executes TWICE. That is the property this module exists to provide, and
    only the key-aware double can observe it: the default double is key-blind
    and returns the same scripted PROCEED either way.
    """
    script = _Script([(200, ACHIEVED_BODY)] * 2, key_aware=key_aware)
    gate_server, url = _serve(script)
    try:
        server, counted = factory(url)
        _call(server, name, args_a)
        refused = None
        try:
            _call(server, name, args_b)
        except ToolError as exc:
            refused = str(exc)
    finally:
        gate_server.shutdown()
    return _posted_key(script, 0), _posted_key(script, 1), counted, refused


def _fork_pair(args_a, args_b, key_aware=False):
    return _keys_for_pair(_fork_server, "fork_transfer", args_a, args_b, key_aware)


def _assert_deduplicated(key_a, key_b, counted, refused):
    """One logical action, spelled two ways, against a key-aware gate: ONE
    key, the repeat REFUSED as a collision, and the consequence fired exactly
    ONCE. Asserting only key_a == key_b would leave the actual at-most-once
    property untested."""
    assert key_a == key_b
    assert refused is not None, "the repeat was authorized -- the key FORKED"
    assert ("class=%s" % ALREADY_RESERVED) in refused
    assert counted.calls == 1  # the consequence fired exactly once


def _assert_not_deduplicated(key_a, key_b, counted, refused):
    """Two genuinely different actions: different keys, no collision, both
    execute. Without this the battery could be satisfied by collapsing every
    call to one key -- a COLLISION, which bricks the second action
    permanently and is worse than the bug being fixed."""
    assert key_a != key_b
    assert refused is None
    assert counted.calls == 2


def test_fork_shape_a_nested_object_default_elided_or_spelled_executes_once():
    _assert_deduplicated(*_fork_pair(
        {"amount": 500, "opts": {}, "tags": []},
        {"amount": 500, "opts": {"mode": "fast", "depth": 3}, "tags": []},
        key_aware=True,
    ))


def test_fork_shape_b_top_level_default_factory_omitted_or_spelled_executes_once():
    _assert_deduplicated(*_fork_pair(
        {"amount": 500},
        {"amount": 500, "tags": []},
        key_aware=True,
    ))


def test_fork_shape_c_equivalent_json_numeric_forms_executes_once():
    _assert_deduplicated(*_fork_pair(
        {"amount": 500, "tags": []},
        {"amount": 500.0, "tags": []},
        key_aware=True,
    ))


def test_fork_shape_d_partially_spelled_nested_object_executes_once():
    _assert_deduplicated(*_fork_pair(
        {"amount": 500, "opts": {"mode": "fast"}, "tags": []},
        {"amount": 500, "opts": {"mode": "fast", "depth": 3}, "tags": []},
        key_aware=True,
    ))


def test_fork_shape_e_optional_none_omitted_or_explicit_null_executes_once():
    # This shape was already correct before the fix (a top-level scalar
    # default, the one shape the old recipe handled). Pinned so it stays so.
    _assert_deduplicated(*_fork_pair(
        {"amount": 500, "tags": []},
        {"amount": 500, "tags": [], "note": None},
        key_aware=True,
    ))


@pytest.mark.parametrize(
    "shape,args_a,args_b",
    [
        ("a-nested-default-elided",
         {"amount": 500, "opts": {}, "tags": []},
         {"amount": 500, "opts": {"mode": "fast", "depth": 3}, "tags": []}),
        ("b-default-factory-omitted",
         {"amount": 500},
         {"amount": 500, "tags": []}),
        ("c-numeric-form",
         {"amount": 500, "tags": []},
         {"amount": 500.0, "tags": []}),
        ("d-nested-partially-spelled",
         {"amount": 500, "opts": {"mode": "fast"}, "tags": []},
         {"amount": 500, "opts": {"mode": "fast", "depth": 3}, "tags": []}),
        ("e-optional-none",
         {"amount": 500, "tags": []},
         {"amount": 500, "tags": [], "note": None}),
    ],
)
def test_each_fork_pair_really_is_one_logical_action(shape, args_a, args_b):
    """The premise the battery rests on: each pair above is ONE action spelled
    two ways. Run key-BLIND so both calls execute, and compare what the tool
    body actually received. If the effective arguments differed, two keys
    would be correct and the battery would be pinning a bug."""
    _, _, counted, refused = _fork_pair(args_a, args_b)
    assert refused is None
    assert counted.seen[0] == counted.seen[1]


# --- negative controls: normalization must not flatten REAL differences ------


def test_negative_control_changed_scalar_still_executes_twice():
    _assert_not_deduplicated(*_fork_pair(
        {"amount": 500, "tags": []},
        {"amount": 501, "tags": []},
        key_aware=True,
    ))


def test_negative_control_changed_value_inside_nested_object_executes_twice():
    _assert_not_deduplicated(*_fork_pair(
        {"amount": 500, "opts": {"mode": "fast"}, "tags": []},
        {"amount": 500, "opts": {"mode": "slow"}, "tags": []},
        key_aware=True,
    ))


def test_negative_control_changed_default_factory_value_executes_twice():
    _assert_not_deduplicated(*_fork_pair(
        {"amount": 500, "tags": []},
        {"amount": 500, "tags": ["x"]},
        key_aware=True,
    ))


# --- at-most-once, stated directly -------------------------------------------


def test_byte_identical_repeat_is_refused_and_executes_exactly_once():
    # The property the whole module exists to provide, asserted against a gate
    # double that actually reserves keys. Before key_aware existed, NO test in
    # this lane asserted it: the key-blind double returned the same scripted
    # PROCEED whether the key was shared or forked.
    _assert_deduplicated(*_fork_pair(
        {"amount": 500, "opts": {"mode": "fast"}, "tags": ["a"]},
        {"amount": 500, "opts": {"mode": "fast"}, "tags": ["a"]},
        key_aware=True,
    ))


# --- tier 1: arguments that cannot validate refuse BEFORE declaring ----------


def test_invalid_arguments_refuse_before_any_declaration():
    # Declaring first and letting fastmcp's own validation reject the call
    # afterwards leaves a committed ACHIEVED in the feed for an action that
    # never happened -- and consumers settle from that feed.
    script = _Script([(200, ACHIEVED_BODY)])
    gate_server, url = _serve(script)
    try:
        server, counted = _fork_server(url)
        with pytest.raises(ToolError) as exc:
            _call(server, "fork_transfer", {"amount": "not-a-number", "tags": []})
    finally:
        gate_server.shutdown()
    assert script.calls == []  # nothing declared: not one POST
    assert counted.calls == 0
    assert "intent gate:" in str(exc.value)  # OUR refusal, not fastmcp's later one


def test_undeclared_argument_refuses_before_any_declaration():
    # fastmcp rejects an undeclared argument, but only AFTER middleware has
    # run (probed, 3.4.7). A validator laxer than the one downstream of it
    # therefore declares, gets authorized, and commits an ACHIEVED for a call
    # that then never executes. The rebuilt model forbids extras for exactly
    # this reason -- pydantic's DEFAULT would drop the argument silently and
    # this test would go red.
    script = _Script([(200, ACHIEVED_BODY)])
    gate_server, url = _serve(script)
    try:
        server, counted = _fork_server(url)
        with pytest.raises(ToolError) as exc:
            _call(server, "fork_transfer", {"amount": 500, "tags": [], "bogus": 1})
    finally:
        gate_server.shutdown()
    assert script.calls == []
    assert counted.calls == 0
    assert "intent gate:" in str(exc.value)


# --- tier 2: the same shapes through a proxy, where no callable exists -------


def _proxy_backend():
    counted = _Counted()
    inner = FastMCP("inner-fork")

    @inner.tool
    def proxied_transfer(amount: float, opts: _Opts = _Opts()) -> str:
        counted.calls += 1
        counted.seen.append((amount, opts.model_dump()))
        return "moved"

    return inner, counted


def _proxy_factory(url):
    """gated_proxy over a backend the operator does not own: its ProxyTool
    exposes a JSON Schema and NO callable, so this is the Tier 2 path."""
    inner, counted = _proxy_backend()
    return gated_proxy(
        inner, _gate_client(url), intent_spec_hash=SPEC_HASH, scope=SCOPE, run_id=RUN_ID
    ), counted


def _direct_factory_same_tool(url):
    """The same backend gated DIRECTLY -- its FunctionTool has a callable, so
    this is Tier 1 over an identically-named, identically-shaped tool."""
    inner, counted = _proxy_backend()
    inner.add_middleware(
        IntentGateMiddleware(
            _gate_client(url), intent_spec_hash=SPEC_HASH, scope=SCOPE, run_id=RUN_ID
        )
    )
    return inner, counted


def test_tier2_proxy_nested_default_elision_executes_once():
    _assert_deduplicated(*_keys_for_pair(
        _proxy_factory, "proxied_transfer",
        {"amount": 500, "opts": {}},
        {"amount": 500, "opts": {"mode": "fast", "depth": 3}},
        key_aware=True,
    ))


def test_tier2_proxy_numeric_form_executes_once():
    _assert_deduplicated(*_keys_for_pair(
        _proxy_factory, "proxied_transfer",
        {"amount": 500},
        {"amount": 500.0},
        key_aware=True,
    ))


def test_tier2_proxy_negative_control_nested_value_executes_twice():
    _assert_not_deduplicated(*_keys_for_pair(
        _proxy_factory, "proxied_transfer",
        {"amount": 500, "opts": {"mode": "fast"}},
        {"amount": 500, "opts": {"mode": "slow"}},
        key_aware=True,
    ))


def test_tier1_and_tier2_derive_the_same_key_for_one_action():
    # A deployment may reach one action either way (a proxy in front of a
    # server the operator also owns; a retry that lands on the other route).
    # If the two tiers disagreed, that would be a fork ACROSS routes -- the
    # same fail-open double execution, one layer up.
    direct_a, direct_b, _, _ = _keys_for_pair(
        _direct_factory_same_tool, "proxied_transfer",
        {"amount": 500, "opts": {}},
        {"amount": 500, "opts": {"mode": "fast", "depth": 3}},
    )
    proxy_a, proxy_b, _, _ = _keys_for_pair(
        _proxy_factory, "proxied_transfer",
        {"amount": 500, "opts": {}},
        {"amount": 500, "opts": {"mode": "fast", "depth": 3}},
    )
    assert direct_a == direct_b == proxy_a == proxy_b


# --- a missing REQUIRED argument is refused before declaring, BOTH tiers -----


def _two_arg_backend():
    counted = _Counted()
    inner = FastMCP("inner-required")

    @inner.tool
    def needs_dest(amount: float, dest: str) -> str:
        counted.calls += 1
        counted.seen.append((amount, dest))
        return "moved"

    return inner, counted


def test_tier2_missing_required_argument_refuses_before_any_declaration():
    # Measured before this fix: the proxy path derived a key from the PARTIAL
    # arguments, POSTed it, was authorized ACHIEVED, and only then did fastmcp
    # reject the call -- 1 POST, 0 executions. A durable ACHIEVED for a
    # consequence that never fired, indistinguishable in the feed from a real
    # one, and this repo's consumers settle from that feed.
    script = _Script([(200, ACHIEVED_BODY)])
    gate_server, url = _serve(script)
    try:
        inner, counted = _two_arg_backend()
        proxy = gated_proxy(
            inner, _gate_client(url), intent_spec_hash=SPEC_HASH, scope=SCOPE, run_id=RUN_ID
        )
        with pytest.raises(ToolError) as exc:
            _call(proxy, "needs_dest", {"amount": 500})
    finally:
        gate_server.shutdown()
    assert script.calls == []  # nothing declared: no POST, no false ACHIEVED
    assert counted.calls == 0
    assert "dest" in str(exc.value)


def test_tier1_missing_required_argument_refuses_before_any_declaration():
    # Tier 1 gets this from validating the rebuilt model; pinned so the two
    # tiers cannot drift apart on it.
    script = _Script([(200, ACHIEVED_BODY)])
    gate_server, url = _serve(script)
    try:
        inner, counted = _two_arg_backend()
        inner.add_middleware(
            IntentGateMiddleware(
                _gate_client(url), intent_spec_hash=SPEC_HASH, scope=SCOPE, run_id=RUN_ID
            )
        )
        with pytest.raises(ToolError) as exc:
            _call(inner, "needs_dest", {"amount": 500})
    finally:
        gate_server.shutdown()
    assert script.calls == []
    assert counted.calls == 0
    assert "intent gate:" in str(exc.value)


# --- tools= must not accept a bare string ------------------------------------


def test_tools_as_a_bare_string_is_rejected_at_construction():
    # A str IS a collection -- of characters. tools="wire_transfer" built
    # {'w','i','r','e',...}, no name ever matched, and every call took the
    # UNGATED pass-through: measured at 0 gate POSTs and 1 tool execution,
    # with no error anywhere. An operator gating one tool writes exactly that,
    # and would silently disable the gate on the tool they meant to protect.
    with pytest.raises(TypeError) as exc:
        IntentGateMiddleware(
            _gate_client("http://127.0.0.1:1"), intent_spec_hash=SPEC_HASH,
            scope=SCOPE, run_id=RUN_ID, tools="wire_transfer",
        )
    msg = str(exc.value)
    assert "wire_transfer" in msg
    assert "UNGATED" in msg  # the message says what goes wrong, not just "no"
    assert "{'wire_transfer'}" in msg  # ...and what to write instead


def test_gated_proxy_also_rejects_a_bare_string_tools():
    inner, _ = _two_arg_backend()
    with pytest.raises(TypeError):
        gated_proxy(
            inner, _gate_client("http://127.0.0.1:1"), intent_spec_hash=SPEC_HASH,
            scope=SCOPE, run_id=RUN_ID, tools="needs_dest",
        )


def test_tools_as_a_set_of_one_name_still_gates_that_tool():
    # The correct spelling keeps working -- the guard rejects the mistake, not
    # the single-tool case.
    script = _Script([(200, ACHIEVED_BODY)])
    gate_server, url = _serve(script)
    try:
        server, gated_counted, ungated_counted = _two_tool_server(url, tools={"gated_one"})
        _call(server, "gated_one", {"x": "b"})
    finally:
        gate_server.shutdown()
    assert gated_counted.calls == 1
    assert len([c for c in script.calls if c[0] == "POST"]) == 1


# --- tier 2 strict_args: an unknowable default is refused, not guessed -------


def _tagged_proxy_factory(url, strict_args=True):
    counted = _Counted()
    inner = FastMCP("inner-tagged")

    @inner.tool
    def proxied_tagged(amount: float, tags: list[str] = Field(default_factory=list)) -> str:
        counted.calls += 1
        counted.seen.append((amount, list(tags)))
        return "tagged"

    return gated_proxy(
        inner, _gate_client(url), intent_spec_hash=SPEC_HASH, scope=SCOPE, run_id=RUN_ID,
        strict_args=strict_args,
    ), counted


def test_tier2_strict_refuses_an_omitted_property_with_no_schema_default():
    # A remote `default_factory` is INVISIBLE in JSON Schema, so Tier 2
    # cannot know the effective value. Keying the call as spelled would fork
    # it against the caller who spells the value out -- fail-open. Refuse.
    script = _Script([(200, ACHIEVED_BODY)])
    gate_server, url = _serve(script)
    try:
        proxy, counted = _tagged_proxy_factory(url)
        with pytest.raises(ToolError) as exc:
            _call(proxy, "proxied_tagged", {"amount": 500})
    finally:
        gate_server.shutdown()
    assert script.calls == []  # refused BEFORE any declaration
    assert counted.calls == 0
    assert "tags" in str(exc.value)  # the message names the property to pass
    assert "strict_args=False" in str(exc.value)


def test_tier2_strict_args_false_proceeds_and_keys_as_spelled():
    # The documented opt-out: the operator accepts, explicitly, that the two
    # spellings below are keyed differently and so could both execute.
    script = _Script([(200, ACHIEVED_BODY)] * 2)
    gate_server, url = _serve(script)
    try:
        proxy, counted = _tagged_proxy_factory(url, strict_args=False)
        _call(proxy, "proxied_tagged", {"amount": 500})
        _call(proxy, "proxied_tagged", {"amount": 500, "tags": []})
    finally:
        gate_server.shutdown()
    assert counted.calls == 2  # it proceeds: no refusal
    posts = [c for c in script.calls if c[0] == "POST"]
    assert len(posts) == 2
    assert _posted_key(script, 0) != _posted_key(script, 1)  # keyed AS SPELLED


def test_tier2_strict_refusal_lifts_once_the_property_is_passed():
    # The refusal is about the OMISSION, not the tool: spelling the value out
    # makes the call keyable and it proceeds under the default strict_args.
    script = _Script([(200, ACHIEVED_BODY)])
    gate_server, url = _serve(script)
    try:
        proxy, counted = _tagged_proxy_factory(url)
        _call(proxy, "proxied_tagged", {"amount": 500, "tags": []})
    finally:
        gate_server.shutdown()
    assert counted.calls == 1
    assert len([c for c in script.calls if c[0] == "POST"]) == 1


# --- tier 2 $ref resolution ---------------------------------------------
#
# These two drive the canonicalizer directly rather than through a server,
# because fastmcp 3.4.7 INLINES $defs when it mirrors a backend's schema onto
# a ProxyTool -- so no $ref survives to the proxy path here, and a
# server-driven test could not reach this code at all. A remote MCP server
# the operator does not own is under no such obligation: it may serve $ref,
# and it may serve one that does not resolve.


def test_tier2_resolves_ref_defaults_so_a_nested_elision_is_one_key():
    schema = {
        "type": "object",
        "$defs": {"Opts": {"type": "object", "properties": {
            "mode": {"type": "string", "default": "fast"},
            "depth": {"type": "integer", "default": 3},
        }}},
        "properties": {
            "amount": {"type": "number"},
            "opts": {"$ref": "#/$defs/Opts", "default": {"mode": "fast", "depth": 3}},
        },
        "required": ["amount"],
    }
    elided = _tier2_args({"amount": 500, "opts": {}}, schema, True)
    spelled = _tier2_args({"amount": 500.0, "opts": {"mode": "fast", "depth": 3}}, schema, True)
    omitted = _tier2_args({"amount": 500}, schema, True)
    assert elided == spelled == omitted
    changed = _tier2_args({"amount": 500, "opts": {"mode": "slow"}}, schema, True)
    assert changed != spelled  # negative control: a real difference survives


def test_tier2_unresolvable_ref_is_refused():
    schema = {"type": "object", "properties": {"opts": {"$ref": "#/$defs/Missing"}}}
    with pytest.raises(_Unkeyable):
        _tier2_args({"opts": {}}, schema, True)


# --- import graph --------------------------------------------------------


def test_adapter_import_graph_is_tree_local():
    src = (pathlib.Path(__file__).parent / "mcp_adapter.py").read_text(encoding="utf-8")
    # pydantic and inspect entered with the two-tier canonicalization
    # (2026-08-20): Tier 1 rebuilds a model from the tool's own signature.
    # pydantic is fastmcp's own dependency, so this adds no new install and
    # the module stays optional.
    allowed = {"__future__", "asyncio", "inspect", "json", "uuid", "pydantic",
               "fastmcp", "client", "declare", "gating"}
    for line in src.splitlines():
        s = line.strip()
        if s.startswith("import ") or s.startswith("from "):
            root = s.split()[1].split(".")[0]
            assert root in allowed, "unexpected import: %s" % s


# --- two independent middleware instances (the stateless case) --------------


def test_two_independent_middleware_instances_same_key_different_seed():
    script = _Script([(200, ACHIEVED_BODY), (200, ACHIEVED_BODY)])
    gate_server, url = _serve(script)
    try:
        server1, counted1 = _gated_server(url)
        server2, counted2 = _gated_server(url)
        _call(server1, "sample_transfer", {"amount": "5", "unit": "beta"})
        _call(server2, "sample_transfer", {"amount": "5", "unit": "beta"})
    finally:
        gate_server.shutdown()
    assert _posted_key(script, 0) == _posted_key(script, 1)
    assert _posted_seed(script, 0) != _posted_seed(script, 1)
