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


def _events(gen):
    return [json.loads(chunk[len("data: "):]) for chunk in gen]


def _claims():
    return {"claims": [
        {"id": "c1", "claim_text": "a", "status": "cited",
         "citation": {"title": "CONTRACT.md", "cited_text": "q"}},
        {"id": "c2", "claim_text": "b", "status": "uncited", "citation": None},
    ]}


def test_skeptic_pass_event_sequence(monkeypatch):
    monkeypatch.setattr(server, "get_client", lambda: object())
    monkeypatch.setattr(server.skeptic, "extract_claims", lambda client, content: _claims())
    monkeypatch.setattr(server.skeptic, "judge_claims", lambda client, claims: [
        {"id": "c1", "verdict": "supported"},
        {"id": "c2", "verdict": "unmarked-inference"},
    ])
    events = _events(server.skeptic_pass([]))
    assert [e["type"] for e in events] == [
        "skeptic_claims", "skeptic_verdict", "skeptic_verdict", "skeptic_done",
    ]
    assert events[0]["claims"] == _claims()["claims"]
    assert events[1] == {"type": "skeptic_verdict", "id": "c1", "verdict": "supported"}
    assert events[-1]["counts"] == {"supported": 1, "unmarked-inference": 1}


def test_skeptic_pass_failure_emits_skeptic_error_only(monkeypatch):
    # The reply is already delivered; a skeptic failure must surface as
    # skeptic_error - never as the producer's "error" event.
    monkeypatch.setattr(server, "get_client", lambda: object())

    def boom(client, content):
        raise RuntimeError("lane down")

    monkeypatch.setattr(server.skeptic, "extract_claims", boom)
    events = _events(server.skeptic_pass([]))
    assert len(events) == 1
    assert events[0]["type"] == "skeptic_error"
    assert "lane down" in events[0]["message"]


def test_skeptic_pass_no_claims(monkeypatch):
    monkeypatch.setattr(server, "get_client", lambda: object())
    monkeypatch.setattr(server.skeptic, "extract_claims",
                        lambda client, content: {"claims": []})
    monkeypatch.setattr(server.skeptic, "judge_claims", lambda client, claims: [])
    events = _events(server.skeptic_pass([]))
    assert [e["type"] for e in events] == ["skeptic_claims", "skeptic_done"]
    assert events[-1]["counts"] == {}
