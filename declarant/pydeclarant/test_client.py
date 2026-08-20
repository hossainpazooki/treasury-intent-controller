"""Conformance tests for the Python declarant twin's client.py module
(CONTRACT.md section 2.7).

An in-process stdlib http.server (the mirror of Go's httptest) stands in for
the gate. The 500-edge pin is the load-bearing one: it proves the client
consults GET /v2/intents/{id}/events BEFORE deciding on a 500 response, by
recording server-side call order and asserting on it from the test thread
(never inside the handler itself, which runs on a background thread whose
assertion failures would not fail the test).
"""

from __future__ import annotations

import json
import threading
from contextlib import contextmanager
from http.server import BaseHTTPRequestHandler, HTTPServer

from client import DEFAULT_TIMEOUT, Client
from client import terminal_position  # noqa: F401  (imported per contract; exercised directly below)
from declare import INDETERMINATE, PROCEED, SDK_BUG, golden_request, intent_id


class _Handler(BaseHTTPRequestHandler):
    """Records every request into self.calls, then answers from self.routes.

    routes maps a route key to (status, body_bytes):
      "post_intents" -> POST /v2/intents
      "get_events"   -> GET  /v2/intents/{id}/events
      "poll_events"  -> GET  /v2/events
    A missing key answers 404 so an unexpected call is visible in the result
    rather than hanging the client.
    """

    routes: dict = {}
    calls: list = []

    def log_message(self, format, *args):  # noqa: A002 - stdlib signature
        pass  # keep the test console clean/ASCII; no per-request logging

    def _reply(self, status: int, body: bytes, headers: dict | None = None) -> None:
        # headers is optional so a route can answer a 3xx with a Location
        # (the redirect test); every other route passes a 2-tuple as before.
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        for name, value in (headers or {}).items():
            self.send_header(name, value)
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_POST(self):
        length = int(self.headers.get("Content-Length", 0) or 0)
        self.rfile.read(length)  # discard the marshaled request body
        if self.path == "/v2/intents":
            self.calls.append("POST /v2/intents")
            self._reply(*self.routes.get("post_intents", (404, b"{}")))
        else:
            self._reply(404, b"{}")

    def do_GET(self):
        if self.path.startswith("/v2/intents/") and self.path.endswith("/events"):
            self.calls.append("GET " + self.path)
            self._reply(*self.routes.get("get_events", (404, b"{}")))
        elif self.path.startswith("/v2/events"):
            self.calls.append("GET " + self.path)
            self._reply(*self.routes.get("poll_events", (404, b"{}")))
        else:
            self._reply(404, b"{}")


_ACHIEVED_200 = json.dumps(
    {"terminal": "ACHIEVED", "reason": "", "trajectory_hash": "aa", "achieved_seq": 7}
).encode("utf-8")


class _OtherOrigin(BaseHTTPRequestHandler):
    """The redirect target: answers ANY request with an ACHIEVED-shaped 200
    and records what it actually received (method, path, body length)."""

    seen: list = []

    def log_message(self, format, *args):  # noqa: A002 - stdlib signature
        pass

    def _record_and_reply(self):
        length = int(self.headers.get("Content-Length", 0) or 0)
        body = self.rfile.read(length) if length else b""
        self.seen.append("%s %s body=%d" % (self.command, self.path, len(body)))
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(_ACHIEVED_200)))
        self.end_headers()
        self.wfile.write(_ACHIEVED_200)

    do_GET = _record_and_reply
    do_POST = _record_and_reply


@contextmanager
def _running_other_origin():
    """Second origin on its own ephemeral port; yields (base_url, seen)."""
    seen: list = []
    handler_cls = type("_RecordingOtherOrigin", (_OtherOrigin,), {"seen": seen})
    server = HTTPServer(("127.0.0.1", 0), handler_cls)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    try:
        yield ("http://127.0.0.1:%d" % server.server_address[1], seen)
    finally:
        server.shutdown()
        server.server_close()
        thread.join(timeout=5)


@contextmanager
def _running_server(routes: dict):
    """Starts a real HTTPServer on an ephemeral 127.0.0.1 port in a daemon
    thread; yields (client, calls); shuts the server down on exit."""
    calls: list = []
    handler_cls = type("_RoutedHandler", (_Handler,), {"routes": routes, "calls": calls})
    server = HTTPServer(("127.0.0.1", 0), handler_cls)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    try:
        port = server.server_address[1]
        client = Client(base_url="http://127.0.0.1:%d" % port)
        yield client, calls
    finally:
        server.shutdown()
        server.server_close()
        thread.join(timeout=5)


def test_declare_achieved():
    body = json.dumps(
        {"terminal": "ACHIEVED", "reason": "", "trajectory_hash": "aa", "achieved_seq": 7}
    ).encode("utf-8")
    routes = {"post_intents": (200, body)}
    with _running_server(routes) as (client, _calls):
        res = client.declare(golden_request())
    assert res.class_ == PROCEED
    assert res.same_key_retry_safe is False
    assert res.terminal is not None
    assert res.terminal["achieved_seq"] == 7


def test_declare_400_is_sdk_bug():
    routes = {"post_intents": (400, b"{}")}
    with _running_server(routes) as (client, _calls):
        res = client.declare(golden_request())
    assert res.class_ == SDK_BUG
    assert res.terminal is None


def test_declare_500_consults_feed_before_deciding():
    # THE ORDER PIN: the 500-edge must GET the per-intent feed before it
    # decides, and the decision must reflect the feed's committed terminal.
    iid = intent_id("golden-episode")
    feed_body = json.dumps(
        {
            "intent_id": iid,
            "events": [
                {"seq": 1, "intent_seq": 0, "intent_id": iid, "type": "DECLARED", "detail": iid},
                {
                    "seq": 2,
                    "intent_seq": 1,
                    "intent_id": iid,
                    "type": "ACHIEVED",
                    "detail": iid,
                    "idempotency_key": "k",
                    "intent_spec_hash": "h",
                    "trajectory_hash": "th",
                },
            ],
        }
    ).encode("utf-8")
    routes = {"post_intents": (500, b"{}"), "get_events": (200, feed_body)}
    with _running_server(routes) as (client, calls):
        res = client.declare(golden_request())
    assert res.feed_consulted is True
    assert res.class_ == PROCEED
    assert res.same_key_retry_safe is False
    assert calls == ["POST /v2/intents", "GET /v2/intents/" + iid + "/events"]


def test_declare_500_in_flight_is_indeterminate():
    # Feed has records but no committed terminal position (only DECLARED) ->
    # the consult ran, but there is nothing terminal to classify from.
    iid = intent_id("golden-episode")
    feed_body = json.dumps(
        {
            "intent_id": iid,
            "events": [
                {"seq": 1, "intent_seq": 0, "intent_id": iid, "type": "DECLARED", "detail": iid}
            ],
        }
    ).encode("utf-8")
    routes = {"post_intents": (500, b"{}"), "get_events": (200, feed_body)}
    with _running_server(routes) as (client, _calls):
        res = client.declare(golden_request())
    assert res.class_ == INDETERMINATE
    assert res.same_key_retry_safe is False
    assert res.feed_consulted is True


def test_declare_500_feed_unreachable_is_indeterminate():
    # The feed consult itself errors (server-side 500 on the events GET) ->
    # unreachable feed decides NOTHING: Indeterminate stands, feed_consulted
    # stays False (distinguishing "consulted, in-flight" from "could not
    # consult at all").
    routes = {"post_intents": (500, b"{}"), "get_events": (500, b"{}")}
    with _running_server(routes) as (client, _calls):
        res = client.declare(golden_request())
    assert res.class_ == INDETERMINATE
    assert res.feed_consulted is False


def test_declare_redirect_is_never_followed():
    # Section 2.7 redirect rule (2026-08-20), mirror of Go
    # TestDeclareRedirectIsNeverFollowed. Following a 301/302/303 answer to
    # the declaration downgrades the POST to a body-less GET: the redirect
    # target never receives the declaration, yet its ACHIEVED-shaped 200
    # would be read back as a FRESH synchronous authorization - the action
    # fires having never been declared. 307/308 preserve the body and so
    # were never the hole, but every 3xx is declined uniformly: a redirect
    # is not an authorization, and there is no special case to get wrong.
    iid = intent_id("golden-episode")
    feed_body = json.dumps({"intent_id": iid, "events": []}).encode("utf-8")
    for code in (301, 302, 303, 307, 308):
        with _running_other_origin() as (other_url, seen):
            routes = {
                "post_intents": (code, b"", {"Location": other_url + "/elsewhere"}),
                # The gate's own feed is reachable but holds nothing that
                # decides: the consult runs, and still nothing authorizes.
                "get_events": (200, feed_body),
            }
            with _running_server(routes) as (client, calls):
                res = client.declare(golden_request())
        assert res.class_ != PROCEED, "a %d classified PROCEED - an undeclared action would fire" % code
        assert res.class_ == INDETERMINATE, "a %d must fail closed: %r" % (code, res.class_)
        assert res.same_key_retry_safe is False
        assert res.terminal is None
        assert res.http_status == code, "the redirect was followed (status %r)" % res.http_status
        assert res.feed_consulted is True, "a 3xx is an unexpected status: consult precedes the decision"
        assert seen == [], (
            "the redirect target was contacted (%r) - it never received the "
            "declaration, so its 200 is not an authorization" % seen
        )
        assert calls == ["POST /v2/intents", "GET /v2/intents/" + iid + "/events"]


def test_poll_achieved():
    body = json.dumps(
        {
            "events": [
                {
                    "seq": 9,
                    "intent_seq": 6,
                    "intent_id": "aa",
                    "type": "ACHIEVED",
                    "idempotency_key": "k",
                    "trajectory_hash": "th",
                }
            ],
            "next_since": 9,
        }
    ).encode("utf-8")
    routes = {"poll_events": (200, body)}
    with _running_server(routes) as (client, calls):
        recs, next_ = client.poll_achieved(4)
    assert len(recs) == 1
    assert recs[0]["idempotency_key"] == "k"
    assert next_ == 9
    # server-side proof the query carried since=4&type=ACHIEVED (recorded
    # from the request thread, asserted here in the test thread)
    assert calls == ["GET /v2/events?since=4&type=ACHIEVED"]


def test_terminal_position():
    assert terminal_position([]) == ({}, False)

    in_flight = [
        {"seq": 1, "intent_seq": 0, "type": "DECLARED"},
        {"seq": 2, "intent_seq": 1, "type": "SOME_STEP"},  # no trajectory_hash
    ]
    assert terminal_position(in_flight) == ({}, False)

    committed = [
        {"seq": 1, "intent_seq": 0, "type": "DECLARED"},
        {"seq": 2, "intent_seq": 1, "type": "ACHIEVED", "trajectory_hash": "th"},
    ]
    rec, ok = terminal_position(committed)
    assert ok is True
    assert rec == committed[1]


def test_default_client_is_bounded():
    # Section 2.7 client-timeout rule (mirrors Go TestDefaultClientIsBounded):
    # the default client is bounded; unbounded is an explicit opt-in.
    assert Client("http://127.0.0.1:1").timeout == DEFAULT_TIMEOUT == 30.0
    assert Client("http://127.0.0.1:1", timeout=None).timeout is None
