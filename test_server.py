import json

import server


def test_sse_format():
    out = server.sse({"type": "text", "text": "hi"})
    assert out.startswith("data: ")
    assert out.endswith("\n\n")
    assert json.loads(out[len("data: "):]) == {"type": "text", "text": "hi"}


def test_system_prompt_built_at_import():
    assert server.SYSTEM, "system prompt must be built at import (fail-loud startup)"
    assert "cache_control" in server.SYSTEM[-1]
