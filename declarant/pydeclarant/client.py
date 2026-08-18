"""The Python twin of declarant/client.go (CONTRACT.md section 2.7).

The 500-edge consults the per-intent feed BEFORE deciding; an unreachable
feed decides nothing (INDETERMINATE). Stdlib urllib only.
"""

from __future__ import annotations

import json
import urllib.error
import urllib.parse
import urllib.request
from dataclasses import dataclass, field

from declare import (
    INDETERMINATE,
    SDK_BUG,
    UNKNOWN,
    Request,
    classify,
    classify_feed_record,
    intent_id,
    marshal_request,
)


@dataclass
class Result:
    """Mirror of Go client.Result."""

    http_status: int = 0
    terminal: dict | None = None  # decoded synchronous terminal; None when none arrived (400/500)
    class_: str = ""              # a class const from declare.py  (trailing _: 'class' is a py keyword)
    same_key_retry_safe: bool = False
    feed_consulted: bool = False  # True iff the 500-edge feed consult actually RAN and returned records
    feed_records: list = field(default_factory=list)


# DEFAULT_TIMEOUT bounds one declarant HTTP call (section 2.7 client-timeout
# rule, mirrored by Go's DefaultTimeout): a hung gate hangs the one call,
# never the declarant forever. An unbounded client is an explicit caller
# opt-in (timeout=None), never the default.
DEFAULT_TIMEOUT = 30.0


class Client:
    """Mirror of Go client.Client. declare() drops the Go ctx param - Python
    has no context.Context; the honest equivalent is the per-call timeout on
    the Client (see __init__)."""

    def __init__(self, base_url: str, timeout: float | None = DEFAULT_TIMEOUT):
        self.base_url = base_url        # gate origin, e.g. "http://127.0.0.1:8080"; no trailing slash
        self.timeout = timeout          # bounded by default; None = unbounded, explicit opt-in only

    def declare(self, r: Request) -> Result:
        """Mirror Go Client.Declare. No ctx param - Python has no
        context.Context; the honest equivalent is the per-call timeout
        configured on this Client (see __init__).

        Order pin (the httptest mirror in test_client.py proves this): the
        POST to /v2/intents happens first; on the 500 edge, the GET to
        /v2/intents/{id}/events happens SECOND, and classification is
        decided only after that GET returns. The consult precedes the
        decision.
        """
        body = marshal_request(r)  # may raise -> propagates as the "marshal error" analogue

        req = urllib.request.Request(
            self.base_url + "/v2/intents",
            data=body,
            method="POST",
            headers={"Content-Type": "application/json"},
        )

        try:
            resp = urllib.request.urlopen(req, timeout=self.timeout)
        except urllib.error.HTTPError as err:
            status = err.code
            err.read()  # discard body, mirror io.Copy(io.Discard, ...)
            err.close()
        except urllib.error.URLError:
            # Transport error: re-raise, mirror Go's "return Result{}, err" -
            # the caller knows nothing and must consult the feed before retry.
            raise
        else:
            status = resp.status
            if status == 200:
                raw = resp.read()
                resp.close()
                try:
                    terminal = json.loads(raw)
                except (json.JSONDecodeError, UnicodeDecodeError):
                    return Result(http_status=200, class_=UNKNOWN)
                cls, safe = classify(terminal.get("terminal", ""), terminal.get("reason", ""))
                return Result(http_status=200, terminal=terminal, class_=cls, same_key_retry_safe=safe)
            resp.read()  # discard body, mirror io.Copy(io.Discard, ...)
            resp.close()

        if status == 400:
            return Result(http_status=400, class_=SDK_BUG, same_key_retry_safe=True)

        # THE 500 EDGE (and any other unexpected status): consult the feed
        # BEFORE deciding. An unreachable feed decides nothing.
        res = Result(http_status=status, class_=INDETERMINATE)
        try:
            recs = self.intent_events(intent_id(r.episode_seed))
        except (urllib.error.URLError, urllib.error.HTTPError, OSError, RuntimeError):
            return res  # feed_consulted stays False; INDETERMINATE stands

        res.feed_consulted = True
        res.feed_records = recs
        term, ok = terminal_position(recs)
        if ok:
            res.class_, res.same_key_retry_safe = classify_feed_record(term)
        return res

    def intent_events(self, intent_id_str: str) -> list:
        """Mirror Go Client.IntentEvents."""
        url = self.base_url + "/v2/intents/" + urllib.parse.quote(intent_id_str, safe="") + "/events"
        out = self._get_json(url)
        return out.get("events", [])

    def poll_achieved(self, since: int) -> tuple[list, int]:
        """Mirror Go Client.PollAchieved.

        Settlement discipline (CONTRACT.md S2.7): apply side effects from
        the returned records FIRST, persist next_since AFTER. On error this
        mirrors Go's (nil, since, err): let the underlying request error
        propagate.
        """
        url = self.base_url + "/v2/events?since=" + str(since) + "&type=ACHIEVED"
        out = self._get_json(url)
        return (out.get("events", []), out.get("next_since", since))

    def _get_json(self, url: str) -> dict:
        """Mirror Go client.getJSON."""
        req = urllib.request.Request(url, method="GET")
        try:
            resp = urllib.request.urlopen(req, timeout=self.timeout)
        except urllib.error.HTTPError as err:
            status = err.code
            err.read()
            err.close()
            raise RuntimeError("status %d" % status) from err
        with resp:
            status = resp.status
            body = resp.read()
        if status != 200:
            raise RuntimeError("status %d" % status)
        return json.loads(body)


def terminal_position(recs: list) -> tuple[dict, bool]:
    """Mirror Go terminalPosition. Picks the record with the max
    intent_seq; only a committed final record (non-empty trajectory_hash)
    decides."""
    if not recs:
        return ({}, False)
    best = recs[0]
    for rec in recs:
        if rec.get("intent_seq", float("-inf")) > best.get("intent_seq", float("-inf")):
            best = rec
    if not best.get("trajectory_hash", ""):
        return ({}, False)
    return (best, True)
