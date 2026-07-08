from types import SimpleNamespace

import pytest

import models
import skeptic


# --- fakes -----------------------------------------------------------------

def text_block(text, citations=None):
    return SimpleNamespace(type="text", text=text, citations=citations)


def citation(title, cited_text):
    return SimpleNamespace(document_title=title, cited_text=cited_text)


class FakeClient:
    """Stands in for anthropic.Anthropic: returns a canned tool_use response."""

    def __init__(self, tool_name, tool_input):
        self._resp = SimpleNamespace(content=[
            SimpleNamespace(type="text", text="preamble"),
            SimpleNamespace(type="tool_use", name=tool_name, input=tool_input),
        ])
        self.calls = []
        self.messages = SimpleNamespace(create=self._create)

    def _create(self, **kwargs):
        self.calls.append(kwargs)
        return self._resp


def claim(id, text, status, cit=None):
    return {"id": id, "claim_text": text, "status": status, "citation": cit}


# --- config / roles ----------------------------------------------------------

def test_worker_and_judgment_roles_resolve():
    assert models.resolve("worker") == "claude-sonnet-5"
    assert models.resolve("judgment") == "claude-fable-5"


def test_lane_models_resolved_at_import():
    # Fail-loud posture extends to Stage B: skeptic.py is imported by
    # server.py, so a config missing worker/judgment fails at startup,
    # not mid-request in a skeptic_error.
    assert skeptic.WORKER_MODEL == models.resolve("worker")
    assert skeptic.JUDGMENT_MODEL == models.resolve("judgment")


# --- render_reply ------------------------------------------------------------

def test_render_reply_annotates_citations_inline():
    content = [
        text_block("The gate is fail-closed.", [citation("CONTRACT.md", "scoring is tri-state")]),
        text_block(" Probably it also sings."),
    ]
    out = skeptic.render_reply(content)
    assert "The gate is fail-closed." in out
    assert '[cites CONTRACT.md: "scoring is tri-state"]' in out
    assert out.index("fail-closed") < out.index("[cites") < out.index("sings")


def test_render_reply_skips_non_text_blocks():
    content = [SimpleNamespace(type="tool_use", name="x", input={}), text_block("hi")]
    assert skeptic.render_reply(content) == "hi"


# --- validation --------------------------------------------------------------

def test_validate_claims_accepts_good_shape():
    data = {"claims": [
        claim("c1", "a", "cited", {"title": "CONTRACT.md", "cited_text": "q"}),
        claim("c2", "b", "uncited"),
    ]}
    assert skeptic.validate_claims(data) == data


@pytest.mark.parametrize("bad", [
    {},                                                        # no claims key
    {"claims": [claim("c1", "a", "maybe")]},                   # bad status
    {"claims": [claim("c1", "a", "cited")]},                   # cited, no citation
    {"claims": [claim("c1", "a", "uncited", {"title": "t", "cited_text": "q"})]},
    {"claims": [claim("c1", "a", "uncited"),                   # duplicate id
                claim("c1", "b", "uncited")]},
    {"claims": [claim("c1", "a", "cited", {"title": "t"})]},   # citation missing cited_text
    {"claims": [claim("c1", "a", "cited", {"title": "", "cited_text": "q"})]},
])
def test_validate_claims_rejects_bad_shapes(bad):
    with pytest.raises(ValueError):
        skeptic.validate_claims(bad)


def test_validate_verdicts_requires_one_verdict_per_claim():
    claims = {"claims": [
        claim("c1", "a", "cited", {"title": "t", "cited_text": "q"}),
        claim("c2", "b", "uncited"),
    ]}
    good = {"verdicts": [
        {"id": "c1", "verdict": "supported"},
        {"id": "c2", "verdict": "unmarked-inference"},
    ]}
    assert skeptic.validate_verdicts(good, claims) == good["verdicts"]

    with pytest.raises(ValueError):  # missing verdict for c2
        skeptic.validate_verdicts({"verdicts": [{"id": "c1", "verdict": "supported"}]}, claims)
    with pytest.raises(ValueError):  # verdict illegal for a cited claim
        skeptic.validate_verdicts({"verdicts": [
            {"id": "c1", "verdict": "marked-inference"},
            {"id": "c2", "verdict": "unmarked-inference"},
        ]}, claims)
    with pytest.raises(ValueError):  # verdict illegal for an uncited claim
        skeptic.validate_verdicts({"verdicts": [
            {"id": "c1", "verdict": "supported"},
            {"id": "c2", "verdict": "overreach"},
        ]}, claims)


# --- lane calls (mocked client) ----------------------------------------------

def test_extract_claims_forces_worker_tool():
    data = {"claims": [claim("c1", "a", "uncited")]}
    fake = FakeClient("record_claims", data)
    out = skeptic.extract_claims(fake, [text_block("a")])
    assert out == data
    (call,) = fake.calls
    assert call["model"] == models.resolve("worker")
    assert call["tool_choice"] == {"type": "tool", "name": "record_claims"}
    assert call["tools"] == [skeptic.EXTRACT_TOOL]


def test_judge_claims_forces_judgment_tool_and_gets_excerpts_only():
    claims = {"claims": [claim("c1", "a", "cited", {"title": "t", "cited_text": "q"})]}
    verdicts = {"verdicts": [{"id": "c1", "verdict": "overreach"}]}
    fake = FakeClient("record_verdicts", verdicts)
    out = skeptic.judge_claims(fake, claims)
    assert out == verdicts["verdicts"]
    (call,) = fake.calls
    assert call["model"] == models.resolve("judgment")
    assert call["tool_choice"] == {"type": "tool", "name": "record_verdicts"}
    # Economy invariant: the judgment lane sees the claims JSON, nothing else.
    prompt = call["messages"][0]["content"]
    assert '"cited_text": "q"' in prompt


def test_judge_claims_skips_api_when_no_claims():
    fake = FakeClient("record_verdicts", {"verdicts": []})
    assert skeptic.judge_claims(fake, {"claims": []}) == []
    assert fake.calls == []


def test_missing_tool_use_block_raises():
    fake = FakeClient("wrong_tool", {})
    with pytest.raises(ValueError):
        skeptic.extract_claims(fake, [text_block("a")])
