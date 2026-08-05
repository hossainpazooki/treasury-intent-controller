"""Builds the Opus context from treasury-intent-controller's authoritative docs.

The framing lives in the system prompt; the four docs are attached as
`document` content blocks with citations enabled, so every claim Opus makes can
be anchored to the exact passage it rests on (and an uncited claim is visibly
its own inference, not contract text).
"""
from __future__ import annotations

import os
from pathlib import Path

DOC_NAMES = ("README.md", "CONTRACT.md", "CONTRACT-DURABILITY.md", "CONTRACT-SCORER.md")

FRAMING = """\
You are a design discussion partner for the concepts underlying
treasury-intent-controller ("tic"), the authorization plane of the ATLAS
Treasury intent-gated action loop. The four authoritative documents are
attached (README, CONTRACT, CONTRACT-DURABILITY, CONTRACT-SCORER; where the
base and durability contracts disagree on a symbol, the durability contract
wins).

Discuss the underlying concepts rigorously: the intent lifecycle state
machine, tri-state fail-closed scoring, idempotency by construction at the
dispatch edge, stable vs volatile criteria, and deterministic replay from a
logical-clock event log. Be direct and skeptical: surface disagreements and
trade-offs explicitly rather than agreeing politely, and say "this is wrong
because..." when it is.

Ground each claim in the attached documents and cite the specific passage it
rests on. When the documents do not settle a question, say so explicitly and
mark the answer as your inference rather than presenting it as contract text."""


def tic_dir() -> Path:
    override = os.environ.get("TIC_DIR")
    if override:
        return Path(override)
    return Path(__file__).resolve().parent.parent / "treasury-intent-controller"


def load_docs(base: Path | None = None) -> dict[str, str]:
    base = base if base is not None else tic_dir()
    docs: dict[str, str] = {}
    for name in DOC_NAMES:
        path = base / name
        if not path.is_file():
            raise FileNotFoundError(f"required doc not found: {path}")
        docs[name] = path.read_text(encoding="utf-8")
    return docs


def build_document_blocks(base: Path | None = None) -> list[dict]:
    """The four docs as citation-enabled `document` blocks.

    The cache breakpoint sits on the last block, so the framing + all four
    docs are cached as one prefix (they precede it in render order).
    """
    docs = load_docs(base)
    blocks: list[dict] = []
    for name, text in docs.items():
        blocks.append(
            {
                "type": "document",
                "source": {"type": "text", "media_type": "text/plain", "data": text},
                "title": name,
                "citations": {"enabled": True},
            }
        )
    blocks[-1]["cache_control"] = {"type": "ephemeral"}
    return blocks


def inject_documents(
    messages: list[dict], blocks: list[dict] | None = None, base: Path | None = None
) -> list[dict]:
    """Return a copy of `messages` with the document blocks prepended to the
    first (user) turn's content. The browser never carries the ~18K-token docs;
    the server injects them here so the cached prefix stays byte-identical.
    """
    if not messages:
        return []
    if blocks is None:
        blocks = build_document_blocks(base)
    out = [dict(m) for m in messages]
    content = out[0]["content"]
    if isinstance(content, str):
        content = [{"type": "text", "text": content}]
    else:
        content = list(content)
    out[0]["content"] = blocks + content
    return out
