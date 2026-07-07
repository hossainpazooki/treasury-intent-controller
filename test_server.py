import json

import context
import server


def test_sse_format():
    out = server.sse({"type": "text", "text": "hi"})
    assert out.startswith("data: ")
    assert out.endswith("\n\n")
    assert json.loads(out[len("data: "):]) == {"type": "text", "text": "hi"}


def test_system_is_framing():
    # SYSTEM is the framing string; the docs moved to citation-enabled
    # document blocks in the first user turn.
    assert server.SYSTEM == context.FRAMING
    assert server.SYSTEM


def test_documents_built_at_import():
    # Fail-loud startup: a missing doc raises at import, not on first request.
    assert server.DOCUMENTS
    assert len(server.DOCUMENTS) == len(context.DOC_NAMES)
    assert server.DOCUMENTS[-1]["citations"] == {"enabled": True}
    assert "cache_control" in server.DOCUMENTS[-1]
