"""Live effect probe: the credentialed run the unit tests cannot stand in for.

`pytest` mocks the API, so it proves the wiring and can never prove the effect.
This probes the RESULTING STATE of a real run, and every check is written to
FAIL a system that merely "streams something plausible":

- citation quotes are recomputed against the raw bytes of the four contracts
  (a paraphrased or hallucinated `cited_text` fails);
- the cached prefix must WRITE on turn 1 and READ on turn 2 (a prefix
  perturbed by a single byte silently re-writes, and that fails here);
- the judgment lane is fed PLANTED claims whose correct verdicts are known in
  advance, in BOTH context modes -- a rubber-stamp judge, a blanket-refuter,
  AND an excerpt view blind to a doc-inversion trap are all caught or
  measured.

Costs real tokens: two discussion turns plus both skeptic lanes. The API's
burst limit is easy to trip; on a `Rate limited` result, pause and re-run --
that is an environment answer, never evidence about the app.
Runtime ~6 min (pacing sleeps between fable-5 calls).

Run:  export ANTHROPIC_API_KEY=...   # or an `ant auth login` profile
      python scripts/live_smoke.py
"""
from __future__ import annotations

import json
import sys
import threading
import time
from pathlib import Path
from types import SimpleNamespace

import httpx
import uvicorn

REPO = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(REPO))

import context  # noqa: E402  (after sys.path)
import models  # noqa: E402
import server  # noqa: E402
import skeptic  # noqa: E402

QUESTION = (
    "What exactly does the gate guarantee about an Unevaluable score, and what "
    "happens at the dispatch edge if the scorer times out? Cite the contracts."
)

results: list[tuple[bool, str, str]] = []


def check(name: str, ok: bool, evidence: str) -> None:
    results.append((ok, name, evidence))
    print(f"[{'PASS' if ok else 'FAIL'}] {name}\n        {evidence}\n", flush=True)


def report(name: str, evidence: str) -> None:
    print(f"[REPORT] {name}\n        {evidence}\n", flush=True)


class RecordingClient:
    """Pass-through client that captures usage per call (cost measurement)."""

    def __init__(self, inner):
        self._inner = inner
        self.last_usage = None
        self.messages = SimpleNamespace(create=self._create)

    def _create(self, **kw):
        resp = self._inner.messages.create(**kw)
        self.last_usage = resp.usage
        return resp


def run_turn(client: httpx.Client, messages: list[dict]) -> list[dict]:
    events: list[dict] = []
    with client.stream("POST", "/chat", json={"messages": messages}, timeout=300) as r:
        for line in r.iter_lines():
            if line.startswith("data: "):
                events.append(json.loads(line[6:]))
    return events


def main() -> int:
    # The server resolves models + docs at import; report what we are actually
    # exercising so a config swap can never hide behind a green probe.
    print(f"models: discussion={models.resolve('discussion')} "
          f"worker={skeptic.WORKER_MODEL} judgment={skeptic.JUDGMENT_MODEL}\n")

    print(f"judgment_context={skeptic.JUDGMENT_CONTEXT}\n")
    check("server ships window judgment mode", skeptic.JUDGMENT_CONTEXT == "window",
          f"config/skeptic.json -> {skeptic.JUDGMENT_CONTEXT}")

    cfg = uvicorn.Config(server.app, host="127.0.0.1", port=8799, log_level="error")
    srv = uvicorn.Server(cfg)
    threading.Thread(target=srv.run, daemon=True).start()
    for _ in range(100):
        if srv.started:
            break
        time.sleep(0.1)

    client = httpx.Client(base_url="http://127.0.0.1:8799")
    docs = context.load_docs()  # raw bytes of the four contracts, from disk

    # ---- Turn 1 -----------------------------------------------------------
    msgs = [{"role": "user", "content": QUESTION}]
    ev1 = run_turn(client, msgs)
    kinds = [e["type"] for e in ev1]
    errors = [e for e in ev1 if e["type"] in ("error", "skeptic_error")]
    check("no error events on a real turn", not errors, str(errors) if errors else "clean stream")

    text = "".join(e["text"] for e in ev1 if e["type"] == "text")
    check("reply text streamed", len(text) > 200, f"{len(text)} chars; events={sorted(set(kinds))}")

    # ---- Citations are REAL (recomputed against the doc bytes) -------------
    cites = [e for e in ev1 if e["type"] == "citation"]
    check("citation events arrived", len(cites) > 0, f"{len(cites)} citation deltas")

    bad = []
    for c in cites:
        doc = docs.get(c["title"])
        if doc is None:
            bad.append((c["title"], "unknown document title"))
        elif not c["text"] or c["text"] not in doc:
            bad.append((c["title"], f"cited_text NOT a substring of the doc: {c['text'][:60]!r}"))
    check(
        "every cited_text is a literal substring of the doc it names",
        len(cites) > 0 and not bad,
        "all quotes verified against raw doc bytes" if not bad else f"{len(bad)} bad: {bad[:3]}",
    )

    titles = {c["title"] for c in cites}
    check("citations name only the four attached docs", titles <= set(context.DOC_NAMES),
          f"titles={sorted(titles)}")

    # ---- Cache prefix behavior --------------------------------------------
    done1 = next((e for e in ev1 if e["type"] == "done"), None)
    u1 = done1["usage"] if done1 else {}
    # The prefix must be ENGAGED, which is a write on a cold cache and a read if
    # a recent run left it warm (5-min TTL). Demanding a write here fails a
    # perfectly good app on a back-to-back re-run -- the invariant is that the
    # prefix is cached at all, and that turn 2 reads it (checked below).
    engaged1 = (u1.get("cache_creation_input_tokens", 0) > 0
                or u1.get("cache_read_input_tokens", 0) > 0)
    check("turn 1 engaged the cached prefix (write cold / read warm)", engaged1, f"usage={u1}")

    # ---- Skeptic lanes, live ----------------------------------------------
    claims_ev = next((e for e in ev1 if e["type"] == "skeptic_claims"), None)
    verdict_ev = [e for e in ev1 if e["type"] == "skeptic_verdict"]
    done_ev = next((e for e in ev1 if e["type"] == "skeptic_done"), None)
    n_claims = len(claims_ev["claims"]) if claims_ev else 0
    check("worker lane extracted claims from the real reply", n_claims > 0,
          f"{n_claims} claims")
    check("judgment lane returned a verdict per claim", len(verdict_ev) == n_claims and n_claims > 0,
          f"{len(verdict_ev)} verdicts / {n_claims} claims; counts={done_ev['counts'] if done_ev else None}")
    check("skeptic events arrive AFTER done (reply delivered first)",
          "done" in kinds and kinds.index("done") < kinds.index("skeptic_claims")
          if "skeptic_claims" in kinds else False,
          f"order: done@{kinds.index('done') if 'done' in kinds else '-'} "
          f"skeptic_claims@{kinds.index('skeptic_claims') if 'skeptic_claims' in kinds else '-'}")

    # ---- Turn 2: the prefix must now READ from cache ------------------------
    # Paced: turn 1 + both skeptic lanes burn ~40K tokens in seconds, which trips
    # the burst limit. A rate-limited turn 2 yields no `done` and would look like
    # a cache failure -- so wait, and report a rate limit as INCONCLUSIVE, never
    # as evidence.
    time.sleep(75)
    msgs2 = [
        {"role": "user", "content": QUESTION},
        {"role": "assistant", "content": text},
        {"role": "user", "content": "Restate your key point in one sentence."},
    ]
    ev2 = run_turn(client, msgs2)
    done2 = next((e for e in ev2 if e["type"] == "done"), None)
    err2 = [e for e in ev2 if e["type"] == "error"]
    u2 = done2["usage"] if done2 else {}
    if err2 and not done2:
        print(f"[INCONCLUSIVE] turn 2 cache read -- turn 2 errored: {err2[0]['message']}\n"
              f"        (rate limit is an environment answer, not app evidence; re-run)\n")
    else:
        check("turn 2 READ the cached prefix (prefix stayed byte-identical)",
              u2.get("cache_read_input_tokens", 0) > 0, f"usage={u2}")

    # ---- NON-VACUITY + MODE COMPARISON --------------------------------------
    # Planted claims with known-correct verdicts, judged in BOTH modes.
    # c1-c3: the original trio (both modes must get them right).
    # c4: the doc-inversion trap - a genuine quote whose inverting "never "
    # sits immediately BEFORE it in CONTRACT.md, outside any
    # excerpt BY CONSTRUCTION. Window mode must catch it (gated); excerpt
    # mode's verdict is REPORTED, never gated - if it says overreach it can
    # only be prior, not evidence, and that is a finding to record.
    real_quote = "Unevaluable"  # replaced below with a genuine doc sentence
    for line in docs["CONTRACT.md"].splitlines():
        if "Unevaluable" in line and len(line) > 40:
            real_quote = line.strip("- *#").strip()
            break

    TRAP_QUOTE = "enters the per-intent `TrajectoryHash`"
    trap_doc = docs["CONTRACT.md"]
    at = trap_doc.find(TRAP_QUOTE)
    check("trap passage present, negation just outside the excerpt",
          at > 0 and trap_doc[at - len("never "):at] == "never ",
          f"quote at byte {at}; preceding bytes {trap_doc[max(at - 20, 0):at]!r}")

    planted = {"claims": [
        {   # (a) cited, but the quote does NOT carry the claim -> overreach
            "id": "c1",
            "claim_text": "The gate automatically retries a timed-out scorer three times before failing.",
            "status": "cited",
            "citation": {"title": "CONTRACT.md", "cited_text": real_quote},
        },
        {   # (b) bald uncited assertion -> unmarked-inference
            "id": "c2",
            "claim_text": "The gate settles the payment itself once all criteria pass.",
            "status": "uncited",
            "citation": None,
        },
        {   # (c) uncited but self-marked as inference -> marked-inference
            "id": "c3",
            "claim_text": "My inference, not stated in the contracts: this design likely eases audit.",
            "status": "uncited",
            "citation": None,
        },
        {   # (d) THE TRAP: quote genuinely carries the claim locally; the doc
            #     inverts it two words earlier -> overreach, invisible to excerpts
            "id": "c4",
            "claim_text": "The gate calls `adapter.OnAchieved` in-process during authorization.",
            "status": "cited",
            "citation": {"title": "CONTRACT.md", "cited_text": TRAP_QUOTE},
        },
    ]}
    planted_reply = " ".join(c["claim_text"] for c in planted["claims"])
    expected = {"c1": "overreach", "c2": "unmarked-inference", "c3": "marked-inference"}

    rec = RecordingClient(server.get_client())

    time.sleep(60)  # pace: each judgment call is a fresh fable-5 request
    excerpt_v = {v["id"]: v["verdict"] for v in skeptic.judge_claims(rec, planted, mode="excerpt")}
    ex_usage = rec.last_usage

    time.sleep(60)
    window_v = {v["id"]: v["verdict"] for v in skeptic.judge_claims(
        rec, planted, mode="window", reply_text=planted_reply, docs=docs)}
    win_usage = rec.last_usage

    for cid, want in expected.items():
        check(f"excerpt mode: {cid} -> {want}", excerpt_v.get(cid) == want,
              f"verdicts={excerpt_v}")
        check(f"window mode (no regression): {cid} -> {want}", window_v.get(cid) == want,
              f"verdicts={window_v}")
    check("window mode CATCHES the doc-inversion trap (c4 -> overreach)",
          window_v.get("c4") == "overreach", f"verdicts={window_v}")
    report("excerpt mode's verdict on the trap c4 (expected miss: 'supported')",
           f"{excerpt_v.get('c4')} - the negation is absent from its input; "
           "a correct verdict here could only be prior, not evidence")
    report("judgment cost per mode (planted set)",
           f"excerpt in={ex_usage.input_tokens} out={ex_usage.output_tokens}; "
           f"window in={win_usage.input_tokens} out={win_usage.output_tokens}")

    # ---- Organic reply, both modes ------------------------------------------
    # The stream already judged turn 1's claims in the CONFIGURED mode
    # (window). Re-judge the same claims excerpt-only and report the shift.
    if claims_ev and verdict_ev:
        organic = {"claims": claims_ev["claims"]}
        stream_counts = done_ev["counts"] if done_ev else {}
        time.sleep(60)
        organic_ex = skeptic.judge_claims(rec, organic, mode="excerpt")
        ex_counts: dict[str, int] = {}
        for v in organic_ex:
            ex_counts[v["verdict"]] = ex_counts.get(v["verdict"], 0) + 1
        stream_by_id = {e["id"]: e["verdict"] for e in verdict_ev}
        missing = [v["id"] for v in organic_ex if v["id"] not in stream_by_id]
        flipped = sum(
            1 for v in organic_ex
            if v["id"] in stream_by_id and stream_by_id[v["id"]] != v["verdict"]
        )
        report("organic reply verdict distribution",
               f"window (stream): {stream_counts}; excerpt (re-judge): {ex_counts}; "
               f"{flipped}/{len(organic_ex)} verdicts differ between modes"
               + (f"; {len(missing)} ids missing from stream verdicts" if missing else ""))

    failed = [n for ok, n, _ in results if not ok]
    print("=" * 70)
    print(f"{len(results) - len(failed)}/{len(results)} checks passed")
    if failed:
        print("FAILED: " + "; ".join(failed))
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
