"""Three-lane skeptic pass over a delivered reply (lanes 2-3).

Producer!=judge: the discussion lane's self-skepticism instruction is style,
not duty - verification lives here. Lane 2 (worker, Sonnet 5) mechanically
transcribes claim/citation pairs into a forced-tool JSON schema; lane 3
(judgment, Fable 5) issues per-claim refutation verdicts. The judgment lane's
context is mode-selected (config/skeptic.json): excerpt-only, or windowed
(reply + mechanical doc passages).
"""
from __future__ import annotations

import json
import re
from pathlib import Path
from typing import Any

import models

WORKER_MAX_TOKENS = 8000
JUDGMENT_MAX_TOKENS = 4000

# Resolved at import (server.py imports this module), so a config missing a
# lane role fails the server at startup - not mid-request as a skeptic_error.
WORKER_MODEL = models.resolve("worker")
JUDGMENT_MODEL = models.resolve("judgment")

CONFIG_PATH = Path(__file__).resolve().parent / "config" / "skeptic.json"
JUDGMENT_CONTEXTS = ("excerpt", "window")


def load_judgment_context(path: Path | None = None) -> str:
    """Judgment-lane context mode from config/skeptic.json, fail-loud.

    Same posture as models.load_config: a missing/malformed config or an
    unknown mode fails the server at startup, never mid-request."""
    path = path if path is not None else CONFIG_PATH
    if not path.is_file():
        raise FileNotFoundError(f"skeptic config not found: {path}")
    data = json.loads(path.read_text(encoding="utf-8"))
    if not isinstance(data, dict):
        raise ValueError(f"skeptic config must be a JSON object: {path}")
    mode = data.get("judgment_context")
    if mode not in JUDGMENT_CONTEXTS:
        raise ValueError(
            f"judgment_context must be one of {JUDGMENT_CONTEXTS}, got {mode!r}: {path}"
        )
    return mode


# Resolved at import (server.py imports this module), same reason as the
# lane models above: bad config fails startup, not a request.
JUDGMENT_CONTEXT = load_judgment_context()

CLAIM_STATUSES = ("cited", "uncited")
VERDICTS_BY_STATUS = {
    "cited": ("supported", "overreach"),
    "uncited": ("marked-inference", "unmarked-inference"),
}

EXTRACT_TOOL = {
    "name": "record_claims",
    "description": "Record every factual claim in the reply with its citation status.",
    "input_schema": {
        "type": "object",
        "properties": {
            "claims": {
                "type": "array",
                "items": {
                    "type": "object",
                    "properties": {
                        "id": {"type": "string"},
                        "claim_text": {"type": "string"},
                        "status": {"type": "string", "enum": ["cited", "uncited"]},
                        "citation": {
                            "type": ["object", "null"],
                            "properties": {
                                "title": {"type": "string"},
                                "cited_text": {"type": "string"},
                            },
                            "required": ["title", "cited_text"],
                        },
                    },
                    "required": ["id", "claim_text", "status", "citation"],
                },
            },
        },
        "required": ["claims"],
    },
}

VERDICT_TOOL = {
    "name": "record_verdicts",
    "description": "Record a refutation verdict for every claim.",
    "input_schema": {
        "type": "object",
        "properties": {
            "verdicts": {
                "type": "array",
                "items": {
                    "type": "object",
                    "properties": {
                        "id": {"type": "string"},
                        "verdict": {
                            "type": "string",
                            "enum": [
                                "supported",
                                "overreach",
                                "marked-inference",
                                "unmarked-inference",
                            ],
                        },
                    },
                    "required": ["id", "verdict"],
                },
            },
        },
        "required": ["verdicts"],
    },
}

WORKER_PROMPT = """\
Transcribe every factual claim in the reply below into the record_claims
tool. Mechanical transcription only - do not judge whether any claim is
true. A claim is "cited" when a [cites <title>: "<quote>"] annotation
follows it; copy that annotation's title and quote into the citation field
verbatim. Any other claim is "uncited" with citation null. Ids c1, c2, ...
in reading order.

Reply:"""

JUDGMENT_PROMPT = """\
You are a skeptic judging claims extracted from a reply about the
treasury-intent-controller contracts. Record one verdict per claim via the
record_verdicts tool. Default to refutation. A cited claim is "supported"
only if its cited_text excerpt actually carries the claim as stated -
otherwise "overreach". An uncited claim is "marked-inference" only if its
own text presents itself as inference or opinion - otherwise
"unmarked-inference". Judge only from the JSON below; you have no access to
the source documents.

Claims:"""

JUDGMENT_PROMPT_WINDOW = """\
You are a skeptic judging claims extracted from a reply about the
treasury-intent-controller contracts. Record one verdict per claim via the
record_verdicts tool. Default to refutation. A cited claim is "supported"
only if its cited_text excerpt carries the claim as stated AND the
surrounding document passage (DOC CONTEXT below, keyed by claim id) does not
qualify or negate it - a quote whose neighboring words invert its meaning is
"overreach". An uncited claim is "marked-inference" only if its own text
presents itself as inference or opinion - otherwise "unmarked-inference".
THE REPLY below is the full reply the claims were extracted from; use it to
resolve referents and whether a claim is actually asserted.

Claims:"""


def render_reply(content: list[Any]) -> str:
    """Plain-text rendering of the final message for the worker lane: reply
    text with each citation annotated inline where the API attached it."""
    parts: list[str] = []
    for block in content:
        if getattr(block, "type", None) != "text":
            continue
        parts.append(block.text)
        for c in getattr(block, "citations", None) or []:
            title = getattr(c, "document_title", "") or ""
            cited = getattr(c, "cited_text", "") or ""
            parts.append(f'\n[cites {title}: "{cited}"]\n')
    return "".join(parts)


WINDOW_RADIUS = 400


def _locate(doc: str, quote: str) -> tuple[int, int] | None:
    """Exact substring first; then a whitespace-tolerant fallback (the worker
    lane transcribes quotes and may collapse or trim whitespace). Tokens must
    still match the doc exactly and in order - fabricated or paraphrased text
    finds nothing and the caller fails loud."""
    at = doc.find(quote)
    if at >= 0:
        return at, at + len(quote)
    tokens = [re.escape(t) for t in quote.split()]
    if not tokens:
        return None
    m = re.search(r"\s+".join(tokens), doc)
    return (m.start(), m.end()) if m else None


def doc_windows(
    claims: dict, docs: dict[str, str], radius: int = WINDOW_RADIUS
) -> dict[str, str]:
    """Per cited claim, the doc passage surrounding its quote (window mode).

    Mechanical by design: exact match first; whitespace-tolerant fallback for
    worker-lane transcription drift; first occurrence wins. Then +/- `radius`
    chars snapped outward to line boundaries. A quote that cannot be located
    fails loud - it surfaces as skeptic_error, never as a silently windowless
    judgment."""
    windows: dict[str, str] = {}
    for c in claims["claims"]:
        if c["status"] != "cited":
            continue
        title, quote = c["citation"]["title"], c["citation"]["cited_text"]
        doc = docs.get(title)
        if doc is None:
            raise ValueError(f"claim {c['id']!r} cites unknown document {title!r}")
        span = _locate(doc, quote)
        if span is None:
            raise ValueError(f"claim {c['id']!r}: cited_text not found in {title!r}")
        at, quote_end = span
        start = doc.rfind("\n", 0, max(at - radius, 0)) + 1
        end = doc.find("\n", quote_end + radius)
        windows[c["id"]] = doc[start : end if end >= 0 else len(doc)].strip("\n")
    return windows


def _tool_input(resp: Any, tool_name: str) -> dict:
    for block in resp.content:
        if getattr(block, "type", None) == "tool_use" and block.name == tool_name:
            return block.input
    raise ValueError(f"model response carried no {tool_name!r} tool call")


def validate_claims(data: dict) -> dict:
    claims = data.get("claims")
    if not isinstance(claims, list):
        raise ValueError("extraction output missing 'claims' list")
    seen_ids: set[str] = set()
    for c in claims:
        cid, status = c.get("id"), c.get("status")
        if cid in seen_ids:
            raise ValueError(f"duplicate claim id {cid!r}")
        seen_ids.add(cid)
        if status not in CLAIM_STATUSES:
            raise ValueError(f"claim {cid!r}: bad status {status!r}")
        if status == "cited":
            cit = c.get("citation")
            if not cit:
                raise ValueError(f"claim {cid!r}: cited but carries no citation")
            for key in ("title", "cited_text"):
                if not isinstance(cit.get(key), str) or not cit[key]:
                    raise ValueError(f"claim {cid!r}: citation missing {key!r}")
        if status == "uncited" and c.get("citation") is not None:
            raise ValueError(f"claim {cid!r}: uncited but carries a citation")
    return data


def validate_verdicts(data: dict, claims: dict) -> list[dict]:
    verdicts = data.get("verdicts")
    if not isinstance(verdicts, list):
        raise ValueError("judgment output missing 'verdicts' list")
    status_by_id = {c["id"]: c["status"] for c in claims["claims"]}
    seen: dict[str, str] = {}
    for v in verdicts:
        vid, verdict = v.get("id"), v.get("verdict")
        if vid not in status_by_id:
            raise ValueError(f"verdict for unknown claim {vid!r}")
        if vid in seen:
            raise ValueError(f"duplicate verdict for claim {vid!r}")
        if verdict not in VERDICTS_BY_STATUS[status_by_id[vid]]:
            raise ValueError(
                f"claim {vid!r} is {status_by_id[vid]}; verdict {verdict!r} illegal"
            )
        seen[vid] = verdict
    unjudged = set(status_by_id) - set(seen)
    if unjudged:
        raise ValueError(f"claims with no verdict: {sorted(unjudged)}")
    return verdicts


def extract_claims(client: Any, content: list[Any]) -> dict:
    """Lane 2 (worker): forced-tool claim/citation extraction."""
    resp = client.messages.create(
        model=WORKER_MODEL,
        max_tokens=WORKER_MAX_TOKENS,
        tools=[EXTRACT_TOOL],
        tool_choice={"type": "tool", "name": "record_claims"},
        messages=[{
            "role": "user",
            "content": WORKER_PROMPT + "\n\n" + render_reply(content),
        }],
    )
    return validate_claims(_tool_input(resp, "record_claims"))


def judge_claims(
    client: Any,
    claims: dict,
    *,
    mode: str = "excerpt",
    reply_text: str | None = None,
    docs: dict[str, str] | None = None,
) -> list[dict]:
    """Lane 3 (judgment): per-claim refutation verdicts.

    "excerpt" (the original lane): claims JSON only, byte-identical prompt.
    "window": claims JSON + the full rendered reply + a mechanical doc
    window around each quote (doc_windows) - catches context the excerpt
    provably cannot carry, at a small multiple of the excerpt cost."""
    if mode not in JUDGMENT_CONTEXTS:
        raise ValueError(f"unknown judgment context {mode!r}; valid: {JUDGMENT_CONTEXTS}")
    if not claims["claims"]:
        return []
    if mode == "window":
        if reply_text is None or docs is None:
            raise ValueError("window mode requires reply_text and docs")
        content = (
            JUDGMENT_PROMPT_WINDOW
            + "\n\n" + json.dumps(claims["claims"], indent=2)
            + "\n\nDOC CONTEXT (per cited claim id):\n"
            + json.dumps(doc_windows(claims, docs), indent=2)
            + "\n\nTHE REPLY:\n" + reply_text
        )
    else:
        content = JUDGMENT_PROMPT + "\n\n" + json.dumps(claims["claims"], indent=2)
    resp = client.messages.create(
        model=JUDGMENT_MODEL,
        max_tokens=JUDGMENT_MAX_TOKENS,
        tools=[VERDICT_TOOL],
        tool_choice={"type": "tool", "name": "record_verdicts"},
        messages=[{"role": "user", "content": content}],
    )
    return validate_verdicts(_tool_input(resp, "record_verdicts"), claims)
