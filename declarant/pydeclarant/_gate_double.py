"""Shared scripted in-process HTTP gate double, used as test support by the
pydeclarant adapter test lanes. Not part of the shipped SDK surface.
"""

from __future__ import annotations

import json
import threading
from http.server import BaseHTTPRequestHandler, HTTPServer

from declare import intent_id


class _Script:
    """Per-test script: declare responses consumed in order; feed responses
    keyed by intent id. Captures every (method, path, body). own_feed, when
    set, is served for the intent id of the LAST POSTed episode_seed --
    "the calling invocation's own intent" without knowing the fresh nonce
    up front.

    key_aware (default False, OPT-IN) makes the double behave like the real
    gate's reservation: the FIRST sight of an idempotency key gets the next
    scripted response, and any REPEAT of that same key is refused with
    FAILED_AT_DISPATCH / idempotency-collision without consuming a scripted
    response.

    It is opt-in because the default double is KEY-BLIND -- it pops responses
    in order and never reads idempotency_key at all. That is the structural
    reason a 45-test lane stayed green over four key FORKS: a forked key drew
    exactly the same scripted response as a shared one, so no test in the lane
    could observe the difference between deduplicating and not. Key-blindness
    is still the right default for tests scripting a specific sequence of
    terminals; a test asserting AT-MOST-ONCE must set key_aware=True, or it is
    asserting key equality and calling it non-duplication.
    """

    def __init__(self, declare_responses, feeds=None, own_feed=None, key_aware=False):
        self.declare_responses = list(declare_responses)
        self.feeds = dict(feeds or {})
        self.own_feed = own_feed
        self.key_aware = key_aware
        self.reserved = []  # idempotency keys seen, in order (key_aware only)
        self.calls = []  # (method, path, body-bytes-or-None)


def _serve(script):
    class Handler(BaseHTTPRequestHandler):
        def do_POST(self):
            body = self.rfile.read(int(self.headers.get("Content-Length", 0)))
            script.calls.append(("POST", self.path, body))
            if script.key_aware:
                key = json.loads(body).get("idempotency_key")
                if key in script.reserved:
                    # The reservation already exists: this is a duplicate
                    # declaration, which is what at-most-once looks like from
                    # the gate's side. It consumes no scripted response, so a
                    # test scripts only the terminals it expects to be FRESH.
                    status, payload = 200, {
                        "terminal": "FAILED_AT_DISPATCH",
                        "reason": "idempotency-collision",
                    }
                    data = json.dumps(payload).encode("utf-8")
                    self.send_response(status)
                    self.send_header("Content-Type", "application/json")
                    self.send_header("Content-Length", str(len(data)))
                    self.end_headers()
                    self.wfile.write(data)
                    return
                script.reserved.append(key)
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
            if script.own_feed is not None:
                posts = [c for c in script.calls if c[0] == "POST"]
                own = intent_id(json.loads(posts[-1][2])["episode_seed"]) if posts else ""
                if iid == own:
                    events = script.own_feed
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
