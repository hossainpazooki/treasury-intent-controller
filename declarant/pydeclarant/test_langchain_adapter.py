"""Tests for the LangChain framework adapter (CONTRACT.md section 2.7,
"Framework adapter"; section 5.4 claim 16).

Skips VISIBLY where langchain_core is absent -- the adapter is optional and
a skipped adapter test is never a green one. Everything else runs against
an in-process stdlib http.server gate double that CAPTURES method, path,
and POST body, so the wire bytes and the consult call order are asserted,
never assumed.
"""

from __future__ import annotations

import asyncio
import json
import threading
from http.server import BaseHTTPRequestHandler, HTTPServer

import pytest

pytest.importorskip(
    "langchain_core",
    reason="langchain-core absent (the adapter is optional; install it to run this lane)",
)

from langchain_core.tools import tool as lc_tool
from pydantic import BaseModel

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
from langchain_adapter import ALREADY_ACHIEVED, IntentRefused, canonical_args, gate_tool

SPEC_HASH = "ab" * 32
SCOPE = "per-actor"
RUN_ID = "run-lc-1"


# --- gate double: scripted responses, full capture ---------------------------


class _Script:
    """Per-test script: declare responses consumed in order; feed responses
    keyed by intent id. Captures every (method, path, body)."""

    def __init__(self, declare_responses, feeds=None):
        self.declare_responses = list(declare_responses)
        self.feeds = dict(feeds or {})
        self.calls = []  # (method, path, body-bytes-or-None)


def _serve(script):
    class Handler(BaseHTTPRequestHandler):
        def do_POST(self):
            body = self.rfile.read(int(self.headers.get("Content-Length", 0)))
            script.calls.append(("POST", self.path, body))
            status, payload = script.declare_responses.pop(0)
            data = json.dumps(payload).encode("utf-8")
            self.send_response(status)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(data)))
            self.end_headers()
            self.wfile.write(data)

        def do_GET(self):
            script.calls.append(("GET", self.path, None))
            iid = self.path.split("/")[3] if self.path.startswith("/v2/intents/") else ""
            events = script.feeds.get(iid, [])
            data = json.dumps({"events": events}).encode("utf-8")
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(data)))
            self.end_headers()
            self.wfile.write(data)

        def log_message(self, *a):  # keep pytest output clean
            pass

    server = HTTPServer(("127.0.0.1", 0), Handler)
    t = threading.Thread(target=server.serve_forever, daemon=True)
    t.start()
    return server, "http://127.0.0.1:%d" % server.server_address[1]


class _Counted:
    """A tool body that counts its own executions."""

    def __init__(self):
        self.calls = 0

    def make(self):
        counter = self

        @lc_tool
        def sample_transfer(amount: str, unit: str = "alpha") -> str:
            """Move an amount of a unit."""
            counter.calls += 1
            return "moved %s %s" % (amount, unit)

        return sample_transfer


def _gated(base_url, tool_obj):
    return gate_tool(
        tool_obj,
        Client(base_url, timeout=5.0),
        intent_spec_hash=SPEC_HASH,
        scope=SCOPE,
        run_id=RUN_ID,
    )


def _expected_key(tool_name, kwargs):
    return derive_key(SCOPE, RUN_ID, tool_name, canonical_args(kwargs))


ACHIEVED_BODY = {"terminal": "ACHIEVED", "reason": ""}


# --- claim 16: proceed executes, wire bytes exact ----------------------------


def test_proceed_executes_once_and_wire_bytes_match_marshal():
    counted = _Counted()
    tool_obj = counted.make()
    script = _Script([(200, ACHIEVED_BODY)])
    server, url = _serve(script)
    try:
        out = _gated(url, tool_obj).invoke({"amount": "100.00", "unit": "beta"})
    finally:
        server.shutdown()
    assert out == "moved 100.00 beta"
    assert counted.calls == 1
    key = _expected_key("sample_transfer", {"amount": "100.00", "unit": "beta"})
    want = marshal_request(
        Request(
            episode_seed=key,
            idempotency_key=key,
            intent_spec_hash=SPEC_HASH,
            idempotency_scope=SCOPE,
        )
    )
    posts = [c for c in script.calls if c[0] == "POST"]
    assert len(posts) == 1 and posts[0][1] == "/v2/intents"
    assert posts[0][2] == want  # byte-exact: the adapter speaks marshal_request's wire


# --- claim 16: every non-Proceed refuses with call count ZERO ----------------


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
    counted = _Counted()
    tool_obj = counted.make()
    script = _Script([(status, body if body is not None else {})])
    server, url = _serve(script)
    try:
        with pytest.raises(IntentRefused) as exc:
            _gated(url, tool_obj).invoke({"amount": "1", "unit": "beta"})
    finally:
        server.shutdown()
    assert counted.calls == 0  # the whole point: refused means NEVER executed
    assert exc.value.class_ == want_class
    assert exc.value.same_key_retry_safe is want_retry


# --- key determinism ---------------------------------------------------------


def _posted_key(script, i):
    return json.loads(script.calls[i][2])["idempotency_key"]


def test_key_determinism_same_args_same_key_changed_args_different_key():
    counted = _Counted()
    tool_obj = counted.make()
    script = _Script([(200, ACHIEVED_BODY)] * 3)
    server, url = _serve(script)
    try:
        g = _gated(url, tool_obj)
        g.invoke({"amount": "5", "unit": "beta"})
        g.invoke({"amount": "5", "unit": "beta"})
        g.invoke({"amount": "6", "unit": "beta"})
    finally:
        server.shutdown()
    assert _posted_key(script, 0) == _posted_key(script, 1)
    assert _posted_key(script, 0) != _posted_key(script, 2)


class Leg(BaseModel):
    # module-level: py3.14 deferred annotations cannot resolve a
    # function-local class when the tool schema is built from annotations
    venue: str
    qty: int


def test_key_determinism_default_elision_and_nested_models():
    calls = []

    @lc_tool
    def route_order(leg: Leg, mode: str = "safe") -> str:
        """Route one order leg."""
        calls.append(1)
        return "ok"

    script = _Script([(200, ACHIEVED_BODY)] * 3)
    server, url = _serve(script)
    try:
        g = _gated(url, route_order)
        g.invoke({"leg": {"venue": "v1", "qty": 3}})                      # default elided
        g.invoke({"leg": {"venue": "v1", "qty": 3}, "mode": "safe"})      # default explicit
        g.invoke({"leg": Leg(venue="v1", qty=3), "mode": "safe"})         # model instance
    finally:
        server.shutdown()
    assert len(calls) == 3
    assert _posted_key(script, 0) == _posted_key(script, 1) == _posted_key(script, 2)


# --- async delegates to the gated sync path ----------------------------------


def test_async_delegates_through_the_gate():
    counted = _Counted()
    tool_obj = counted.make()
    script = _Script([(200, ACHIEVED_BODY)])
    server, url = _serve(script)
    try:
        out = asyncio.run(_gated(url, tool_obj).ainvoke({"amount": "2", "unit": "beta"}))
    finally:
        server.shutdown()
    assert out == "moved 2 beta"
    assert counted.calls == 1
    assert [c[0] for c in script.calls] == ["POST"]  # the declare was consulted


# --- the fail-open pin: 500-edge consult is scoped to the CALLING intent -----


def test_500_consult_carries_the_calling_invocations_own_intent_id():
    counted = _Counted()
    tool_obj = counted.make()
    key1 = _expected_key("sample_transfer", {"amount": "1", "unit": "beta"})
    achieved_record = {
        "type": "ACHIEVED",
        "detail": "",
        "intent_seq": 4,
        "trajectory_hash": "aa" * 32,
        "idempotency_key": key1,
    }
    # call 1's intent has an ACHIEVED record; call 2's feed is EMPTY.
    script = _Script(
        [(200, ACHIEVED_BODY), (500, {})],
        feeds={intent_id(key1): [achieved_record]},
    )
    server, url = _serve(script)
    try:
        g = _gated(url, tool_obj)
        g.invoke({"amount": "1", "unit": "beta"})
        with pytest.raises(IntentRefused) as exc:
            g.invoke({"amount": "2", "unit": "beta"})
    finally:
        server.shutdown()
    assert counted.calls == 1  # call 2 never executed
    assert exc.value.class_ == INDETERMINATE
    key2 = _expected_key("sample_transfer", {"amount": "2", "unit": "beta"})
    gets = [c for c in script.calls if c[0] == "GET"]
    assert len(gets) == 1
    # The consult URL must carry call 2's OWN intent id -- a wrap-time-seed
    # implementation consults call 1's id here and inherits its terminal.
    assert intent_id(key2) in gets[0][1]
    assert intent_id(key1) not in gets[0][1]


# --- historical Proceed never re-fires the consequence -----------------------


def test_same_key_retry_500_with_own_achieved_refuses_not_reexecutes():
    # Call 1 achieves and executes. The RETRY of the same call 500s, and the
    # consult finds the caller's OWN committed ACHIEVED. That is a historical
    # authorization: the consequence already fired once; re-firing it here
    # would break at-most-once at the adapter layer. Refuse, never re-run.
    counted = _Counted()
    tool_obj = counted.make()
    key = _expected_key("sample_transfer", {"amount": "9", "unit": "beta"})
    own_achieved = {
        "type": "ACHIEVED",
        "detail": "",
        "intent_seq": 7,
        "trajectory_hash": "cc" * 32,
        "idempotency_key": key,
    }
    script = _Script(
        [(200, ACHIEVED_BODY), (500, {})],
        feeds={intent_id(key): [own_achieved]},
    )
    server, url = _serve(script)
    try:
        g = _gated(url, tool_obj)
        g.invoke({"amount": "9", "unit": "beta"})
        with pytest.raises(IntentRefused) as exc:
            g.invoke({"amount": "9", "unit": "beta"})
    finally:
        server.shutdown()
    assert counted.calls == 1  # the consequence fired exactly once
    assert exc.value.class_ == ALREADY_ACHIEVED
    assert exc.value.same_key_retry_safe is False


# --- string input refused before any declaration -----------------------------


def test_string_input_refused_before_any_declaration():
    counted = _Counted()
    tool_obj = counted.make()
    script = _Script([])
    server, url = _serve(script)
    try:
        with pytest.raises(TypeError):
            _gated(url, tool_obj).invoke("100.00")
    finally:
        server.shutdown()
    assert counted.calls == 0
    assert script.calls == []  # nothing declared, nothing consulted


# --- import graph ------------------------------------------------------------


def test_adapter_import_graph_is_tree_local():
    import pathlib

    src = (pathlib.Path(__file__).parent / "langchain_adapter.py").read_text(encoding="utf-8")
    allowed = {"__future__", "json", "langchain_core", "client", "declare"}
    for line in src.splitlines():
        s = line.strip()
        if s.startswith("import ") or s.startswith("from "):
            root = s.split()[1].split(".")[0]
            assert root in allowed, "unexpected import: %s" % s
