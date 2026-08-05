from pathlib import Path

import pytest

import context


def test_docs_found():
    docs = context.load_docs()
    assert set(docs) == set(context.DOC_NAMES)
    assert all(docs.values()), "no doc may be empty"


def test_missing_doc_fails_loudly(tmp_path):
    with pytest.raises(FileNotFoundError) as exc:
        context.load_docs(tmp_path)
    assert "README.md" in str(exc.value)


def test_framing_is_text():
    assert isinstance(context.FRAMING, str)
    assert context.FRAMING.startswith("You are a design discussion partner")


def test_document_blocks_shape():
    blocks = context.build_document_blocks()
    assert len(blocks) == len(context.DOC_NAMES)
    for b in blocks:
        assert b["type"] == "document"
        assert b["source"]["type"] == "text"
        assert b["source"]["media_type"] == "text/plain"
        assert b["source"]["data"], "document data must be non-empty"
        assert b["citations"] == {"enabled": True}
    # Cache breakpoint on the last block only.
    cached = [b for b in blocks if "cache_control" in b]
    assert cached == [blocks[-1]]
    assert blocks[-1]["cache_control"] == {"type": "ephemeral"}
    assert blocks[-1]["title"] == "CONTRACT.md"


def test_inject_documents_prepends_and_preserves():
    msgs = [{"role": "user", "content": "hi"}]
    out = context.inject_documents(msgs)

    # Original is untouched.
    assert msgs[0]["content"] == "hi"

    content = out[0]["content"]
    assert isinstance(content, list)
    assert len(content) == len(context.DOC_NAMES) + 1
    assert all(b["type"] == "document" for b in content[:-1])
    assert content[-2]["title"] == "CONTRACT.md"  # last doc, then the question
    assert content[-1] == {"type": "text", "text": "hi"}


def test_inject_documents_empty_guard():
    assert context.inject_documents([]) == []


def test_trap_passage_still_present_in_contract():
    # scripts/live_smoke.py plants a claim quoting this exact substring; its
    # known-correct verdict (overreach) depends on the inverting "never "
    # sitting immediately BEFORE the quote - outside any excerpt. (This is the
    # spec's backup trap; the original adapter.OnAchieved sentence did not
    # survive the 2026-08 contract consolidation verbatim.) If the contract
    # rewords this line, fix the probe's TRAP_QUOTE.
    docs = context.load_docs()
    quote = "enters the per-intent `TrajectoryHash`"
    doc = docs["CONTRACT.md"]
    at = doc.find(quote)
    assert at > 0, "trap quote vanished from CONTRACT.md"
    assert doc[at - len("never "):at] == "never "
