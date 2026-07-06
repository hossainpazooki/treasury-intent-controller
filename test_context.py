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


def test_system_prompt_shape():
    blocks = context.build_system_prompt()
    assert len(blocks) == 1 + len(context.DOC_NAMES)
    assert blocks[0]["text"].startswith("You are a design discussion partner")
    cached = [b for b in blocks if "cache_control" in b]
    assert cached == [blocks[-1]]
    assert blocks[-1]["cache_control"] == {"type": "ephemeral"}
