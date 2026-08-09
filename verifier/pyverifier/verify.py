"""The Python twin of the Go verifier (CONTRACT.md section 9.1).

Re-derives the durable feed's commitments from the record bytes alone,
importing nothing from the gate: a genuinely second implementation of the
section 6 hash encoding and the section 2.3 record shape. Both twins must
produce BYTE-IDENTICAL reports on the same feed - same check order, same
closed reason vocabulary, same rendering - or the feed fixtures go red.

Usage: python verify.py <path/to/events.jsonl>
Prints the canonical report to stdout; exits 0 ONLY on VERIFIED.

The one named trap (consumer-research memo section 2b): field lengths in the
hash encoding are UTF-8 BYTE lengths, never len(str) character counts.
"""

from __future__ import annotations

import hashlib
import json
import sys

VERIFIED = "VERIFIED"
REFUTED = "REFUTED"
UNVERIFIABLE = "UNVERIFIABLE"

KNOWN_TYPES = {
    "DECLARED", "RESOLVING", "ACTIVE", "VERIFYING", "SCORED", "RECHECK",
    "UNEVALUABLE", "REVOKED", "IDEMPOTENCY_RESERVED", "ACHIEVED", "FAILED",
    "FAILED_AT_DISPATCH", "SHADOW_RECORDED",
}
TERMINAL_TYPES = {
    "ACHIEVED", "SHADOW_RECORDED", "FAILED", "FAILED_AT_DISPATCH",
    "UNEVALUABLE", "REVOKED",
}
TRACE_TYPES = {"ACHIEVED", "SHADOW_RECORDED"}


def trajectory_hash(recs: list[dict]) -> str:
    """Recompute the section 6 canonical encoding: for each record, fields
    (intent_seq, type, detail) each written as <byte-len>:<bytes>\\n."""
    h = hashlib.sha256()
    def write_field(s: str) -> None:
        b = s.encode("utf-8")
        h.update(str(len(b)).encode("ascii"))
        h.update(b":")
        h.update(b)
        h.update(b"\n")
    for r in recs:
        write_field(str(r["intent_seq"]))
        write_field(r["type"])
        write_field(r.get("detail", ""))
    return h.hexdigest()


_OPTIONAL_STR_FIELDS = (
    "detail", "idempotency_key", "rule_artifact_hash", "intent_spec_hash",
    "trajectory_hash",
)


def _shape_ok(r: object) -> bool:
    return (
        isinstance(r, dict)
        and isinstance(r.get("seq"), int) and not isinstance(r.get("seq"), bool)
        and r["seq"] >= 1
        and isinstance(r.get("intent_seq"), int) and not isinstance(r.get("intent_seq"), bool)
        and r["intent_seq"] >= 0
        and isinstance(r.get("intent_id"), str) and r["intent_id"] != ""
        and isinstance(r.get("type"), str) and r["type"] != ""
        and all(isinstance(r[f], str) for f in _OPTIONAL_STR_FIELDS if f in r)
    )


def parse_feed(raw: bytes) -> tuple[list[dict], list[tuple[str, str, str]]]:
    """Split raw events.jsonl bytes, reproducing the section 2.3 read
    tolerance: blank lines skipped, a torn TRAILING line tolerated, an
    unparseable or shape-invalid line anywhere else is a feed finding."""
    recs: list[dict] = []
    findings: list[tuple[str, str, str]] = []
    lines = raw.decode("utf-8", errors="replace").split("\n")
    for idx, line in enumerate(lines):
        trimmed = line.rstrip("\r\n")
        if trimmed == "":
            continue
        lineno = idx + 1
        torn_tail = idx == len(lines) - 1  # no final newline: torn last write
        try:
            r = json.loads(trimmed)
        except ValueError:
            if torn_tail:
                continue
            findings.append(("feed", UNVERIFIABLE, f"unparseable-line:{lineno}"))
            continue
        if not _shape_ok(r):
            findings.append(("feed", REFUTED, f"bad-record:{lineno}"))
            continue
        recs.append(r)
    return recs, findings


def verify_intent(intent_id: str, recs: list[dict]) -> tuple[str, str, str]:
    """Per-intent checks in the twins' FIXED order; first finding wins."""
    srt = sorted(recs, key=lambda r: r["intent_seq"])

    # 1. intent_seq contiguity: gap-free 0..n, no duplicates.
    for i, r in enumerate(srt):
        if r["intent_seq"] != i:
            if i > 0 and r["intent_seq"] == srt[i - 1]["intent_seq"]:
                return (intent_id, REFUTED, f"intent-seq-dup:{r['intent_seq']}")
            return (intent_id, REFUTED, f"intent-seq-gap:{r['intent_seq']}:{i}")
    # 2. DECLARED first, detail = intent id.
    if srt[0]["type"] != "DECLARED":
        return (intent_id, REFUTED, "missing-declared")
    if srt[0].get("detail", "") != intent_id:
        return (intent_id, REFUTED, "declared-detail-mismatch")
    # 3. Closed event-type set.
    for r in srt:
        if r["type"] not in KNOWN_TYPES:
            return (intent_id, UNVERIFIABLE, f"unknown-event-type:{r['type']}")
    # 4. Hash placement: only the terminal-position record may carry one.
    last = srt[-1]
    for r in srt[:-1]:
        if r.get("trajectory_hash", ""):
            return (intent_id, REFUTED, f"hash-on-nonterminal:{r['intent_seq']}")
    # 5. Terminal hash present; abstain (never pass) when absent.
    if not last.get("trajectory_hash", ""):
        return (intent_id, UNVERIFIABLE, "no-terminal-hash")
    if last["type"] not in TERMINAL_TYPES:
        return (intent_id, REFUTED, f"terminal-type:{last['type']}")
    # 6. ACHIEVED discipline + provenance fields.
    achieved = sum(1 for r in srt if r["type"] == "ACHIEVED")
    if achieved > 1:
        return (intent_id, REFUTED, "duplicate-achieved")
    if achieved == 1 and last["type"] != "ACHIEVED":
        return (intent_id, REFUTED, "achieved-not-terminal")
    if last["type"] in TRACE_TYPES:
        # Only the structurally guaranteed fields; rule_artifact_hash is an
        # optional artifact-plane passthrough (see the Go twin's comment).
        for field in ("idempotency_key", "intent_spec_hash"):
            if not last.get(field, ""):
                return (intent_id, REFUTED, f"trace-field-missing:{last['type']}:{field}")
    # 7. The commitment itself.
    if trajectory_hash(srt) != last["trajectory_hash"]:
        return (intent_id, REFUTED, "hash-mismatch")
    return (intent_id, VERIFIED, "")


def verify(raw: bytes) -> dict:
    recs, feed_findings = parse_feed(raw)
    feed = list(feed_findings)

    if not recs:
        feed.append(("feed", UNVERIFIABLE, "empty"))

    # Global seq: strictly ascending, gap-free from 1, in file order.
    for i, r in enumerate(recs):
        if r["seq"] != i + 1:
            feed.append(("feed", REFUTED, f"seq-gap:{r['seq']}:{i + 1}"))
            break  # one report; everything after the first gap is suspect
    # Exactly-one-ACHIEVED per idempotency key, feed-wide.
    seen_keys: set[str] = set()
    for r in recs:
        if r["type"] != "ACHIEVED" or not r.get("idempotency_key", ""):
            continue
        key = r["idempotency_key"]
        if key in seen_keys:
            feed.append(("feed", REFUTED, f"duplicate-achieved-key:{key}"))
        seen_keys.add(key)

    # Per-intent checks, first-appearance order.
    by_intent: dict[str, list[dict]] = {}
    for r in recs:
        by_intent.setdefault(r["intent_id"], []).append(r)
    intents = [verify_intent(i, rs) for i, rs in by_intent.items()]

    overall = VERIFIED
    for _, verdict, _ in feed + intents:
        if verdict == REFUTED:
            overall = REFUTED
        elif verdict == UNVERIFIABLE and overall != REFUTED:
            overall = UNVERIFIABLE
    return {"feed": feed, "intents": intents, "overall": overall}


def render(rep: dict) -> str:
    """The canonical report - the byte surface the twins are held to."""
    out: list[str] = []
    for scope, verdict, reason in rep["feed"]:
        out.append(f"{scope} {verdict} {reason}".rstrip())
    for intent_id, verdict, reason in rep["intents"]:
        out.append(f"intent {intent_id} {verdict} {reason}".rstrip())
    v = sum(1 for _, x, _ in rep["intents"] if x == VERIFIED)
    r = sum(1 for _, x, _ in rep["intents"] if x == REFUTED)
    u = sum(1 for _, x, _ in rep["intents"] if x == UNVERIFIABLE)
    out.append(
        f"RESULT: {rep['overall']} intents={len(rep['intents'])} "
        f"verified={v} refuted={r} unverifiable={u}"
    )
    return "\n".join(out) + "\n"


def main(argv: list[str]) -> int:
    if len(argv) != 2:
        print("usage: verify.py <events.jsonl>", file=sys.stderr)
        return 2
    try:
        with open(argv[1], "rb") as f:
            raw = f.read()
    except OSError as exc:
        print(f"verify.py: {exc}", file=sys.stderr)
        return 2
    rep = verify(raw)
    # Bytes, not text: Windows text-mode stdout would CRLF the report and
    # break the twins' byte-identity.
    sys.stdout.buffer.write(render(rep).encode("utf-8"))
    return 0 if rep["overall"] == VERIFIED else 1


if __name__ == "__main__":
    raise SystemExit(main(sys.argv))
