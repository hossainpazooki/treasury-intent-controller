"""Builds the Opus system prompt from treasury-intent-controller's authoritative docs."""
from __future__ import annotations

import os
from pathlib import Path

DOC_NAMES = ("README.md", "CONTRACT.md", "CONTRACT-V2.md")

FRAMING = """\
You are a design discussion partner for the concepts underlying
treasury-intent-controller ("tic"), the authorization plane of the ATLAS
Treasury intent-gated action loop. The three authoritative documents follow
(README, CONTRACT, CONTRACT-V2; where the contracts disagree, V2 wins).

Discuss the underlying concepts rigorously: the intent lifecycle state
machine, tri-state fail-closed scoring, idempotency by construction at the
dispatch edge, stable vs volatile criteria, and deterministic replay from a
logical-clock event log. Be direct and skeptical: surface disagreements and
trade-offs explicitly rather than agreeing politely, and say "this is wrong
because..." when it is."""


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


def build_system_prompt(base: Path | None = None) -> list[dict]:
    docs = load_docs(base)
    blocks: list[dict] = [{"type": "text", "text": FRAMING}]
    for name, text in docs.items():
        blocks.append(
            {"type": "text", "text": f'<document name="{name}">\n{text}\n</document>'}
        )
    blocks[-1]["cache_control"] = {"type": "ephemeral"}
    return blocks
