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


def test_judgment_context_resolved_at_import_and_pinned():
    # Changing the mode is a deliberate two-file act (config + this pin),
    # same discipline as the model pins above.
    assert skeptic.JUDGMENT_CONTEXT == "window"
    assert skeptic.JUDGMENT_CONTEXT in skeptic.JUDGMENT_CONTEXTS


def test_load_judgment_context_fail_loud(tmp_path):
    with pytest.raises(FileNotFoundError):
        skeptic.load_judgment_context(tmp_path / "absent.json")

    bad_shape = tmp_path / "bad_shape.json"
    bad_shape.write_text("[1, 2]", encoding="utf-8")
    with pytest.raises(ValueError):
        skeptic.load_judgment_context(bad_shape)

    bad_value = tmp_path / "bad_value.json"
    bad_value.write_text('{"judgment_context": "full-docs"}', encoding="utf-8")
    with pytest.raises(ValueError):
        skeptic.load_judgment_context(bad_value)

    missing_key = tmp_path / "missing_key.json"
    missing_key.write_text('{"other": "x"}', encoding="utf-8")
    with pytest.raises(ValueError):
        skeptic.load_judgment_context(missing_key)

    good = tmp_path / "good.json"
    good.write_text('{"judgment_context": "excerpt"}', encoding="utf-8")
    assert skeptic.load_judgment_context(good) == "excerpt"


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


def _window_fixture():
    claims = {"claims": [
        claim("c1", "the gate calls OnAchieved in-process", "cited",
              {"title": "D.md", "cited_text": "calls OnAchieved in-process"}),
        claim("c2", "my inference: this eases audit", "uncited"),
    ]}
    docs = {"D.md": "the gate no longer calls OnAchieved in-process.\n"}
    verdicts = {"verdicts": [
        {"id": "c1", "verdict": "overreach"},
        {"id": "c2", "verdict": "marked-inference"},
    ]}
    return claims, docs, verdicts


def test_judge_claims_window_mode_carries_windows_and_reply():
    claims, docs, verdicts = _window_fixture()
    fake = FakeClient("record_verdicts", verdicts)
    out = skeptic.judge_claims(
        fake, claims, mode="window", reply_text="THE FULL REPLY TEXT", docs=docs,
    )
    assert out == verdicts["verdicts"]
    (call,) = fake.calls
    prompt = call["messages"][0]["content"]
    assert "no longer calls OnAchieved in-process" in prompt   # the doc window
    assert "THE FULL REPLY TEXT" in prompt                     # the reply
    assert '"cited_text": "calls OnAchieved in-process"' in prompt
    assert call["model"] == models.resolve("judgment")
    assert call["tool_choice"] == {"type": "tool", "name": "record_verdicts"}


def test_judge_claims_excerpt_mode_prompt_unchanged():
    # Excerpt mode stays byte-identical to the pre-window lane: same prompt
    # constant, no reply, no windows.
    claims = {"claims": [claim("c1", "a", "cited", {"title": "t", "cited_text": "q"})]}
    fake = FakeClient("record_verdicts", {"verdicts": [{"id": "c1", "verdict": "supported"}]})
    skeptic.judge_claims(fake, claims, mode="excerpt", reply_text="IGNORED", docs={"t": "q"})
    (call,) = fake.calls
    prompt = call["messages"][0]["content"]
    assert prompt.startswith(skeptic.JUDGMENT_PROMPT)
    assert "IGNORED" not in prompt


def test_judge_claims_window_mode_requires_reply_and_docs():
    claims, docs, _ = _window_fixture()
    fake = FakeClient("record_verdicts", {"verdicts": []})
    with pytest.raises(ValueError):
        skeptic.judge_claims(fake, claims, mode="window", docs=docs)
    with pytest.raises(ValueError):
        skeptic.judge_claims(fake, claims, mode="window", reply_text="r")
    assert fake.calls == []


def test_judge_claims_rejects_unknown_mode():
    fake = FakeClient("record_verdicts", {"verdicts": []})
    with pytest.raises(ValueError):
        skeptic.judge_claims(fake, {"claims": [claim("c1", "a", "uncited")]}, mode="full-docs")
    assert fake.calls == []


# --- doc_windows ---------------------------------------------------------------

DOC = (
    "line one is padding\n"
    "the gate STOPS at appending and no longer calls adapter.OnAchieved in-process.\n"
    "line three is padding\n"
)


def test_doc_windows_locates_quote_with_surrounding_context():
    claims = {"claims": [
        claim("c1", "gate calls OnAchieved in-process", "cited",
              {"title": "D.md", "cited_text": "calls adapter.OnAchieved in-process"}),
    ]}
    windows = skeptic.doc_windows(claims, {"D.md": DOC})
    assert set(windows) == {"c1"}
    assert "calls adapter.OnAchieved in-process" in windows["c1"]
    assert "no longer" in windows["c1"]          # the inverting context is IN


def test_doc_windows_snaps_to_line_boundaries_within_radius():
    # radius=1: barely past the quote, but snapping extends to whole lines.
    claims = {"claims": [
        claim("c1", "x", "cited", {"title": "D.md", "cited_text": "no longer calls"}),
    ]}
    (w,) = skeptic.doc_windows(claims, {"D.md": DOC}, radius=1).values()
    assert w.startswith("the gate STOPS")
    assert w.endswith("in-process.")


def test_doc_windows_quote_at_doc_start():
    claims = {"claims": [
        claim("c1", "x", "cited", {"title": "D.md", "cited_text": "line one"}),
    ]}
    (w,) = skeptic.doc_windows(claims, {"D.md": DOC}, radius=5).values()
    assert w.startswith("line one is padding")


def test_doc_windows_first_occurrence_wins():
    doc = "alpha beta\nalpha gamma\n"
    claims = {"claims": [
        claim("c1", "x", "cited", {"title": "D.md", "cited_text": "alpha"}),
    ]}
    (w,) = skeptic.doc_windows(claims, {"D.md": doc}, radius=2).values()
    assert "beta" in w


def test_doc_windows_skips_uncited_claims():
    claims = {"claims": [claim("c1", "x", "uncited")]}
    assert skeptic.doc_windows(claims, {"D.md": DOC}) == {}


def test_doc_windows_fail_loud():
    unknown_title = {"claims": [
        claim("c1", "x", "cited", {"title": "NOPE.md", "cited_text": "line one"}),
    ]}
    with pytest.raises(ValueError):
        skeptic.doc_windows(unknown_title, {"D.md": DOC})

    absent_quote = {"claims": [
        claim("c1", "x", "cited", {"title": "D.md", "cited_text": "not in the doc"}),
    ]}
    with pytest.raises(ValueError):
        skeptic.doc_windows(absent_quote, {"D.md": DOC})


def test_doc_windows_tolerates_whitespace_drift_in_quote():
    # The worker lane transcribes quotes and sometimes collapses whitespace;
    # a whitespace-drifted quote must still locate its doc span.
    doc = "alpha beta\n  gamma delta epsilon\nzeta\n"
    claims = {"claims": [
        claim("c1", "x", "cited", {"title": "D.md", "cited_text": "beta gamma delta"}),
    ]}
    (w,) = skeptic.doc_windows(claims, {"D.md": doc}, radius=3).values()
    assert "beta" in w and "delta" in w


def test_doc_windows_tolerates_trimmed_quote_edges():
    claims = {"claims": [
        claim("c1", "x", "cited", {"title": "D.md", "cited_text": "  line one is padding  "}),
    ]}
    (w,) = skeptic.doc_windows(claims, {"D.md": DOC}, radius=5).values()
    assert w.startswith("line one is padding")


def test_doc_windows_still_fails_loud_on_fabricated_text():
    # Whitespace tolerance must never become fuzzy matching: text that is not
    # in the doc (even case-drifted) still raises.
    for bad in ("entirely fabricated words", "LINE ONE IS PADDING"):
        claims = {"claims": [
            claim("c1", "x", "cited", {"title": "D.md", "cited_text": bad}),
        ]}
        with pytest.raises(ValueError):
            skeptic.doc_windows(claims, {"D.md": DOC})
