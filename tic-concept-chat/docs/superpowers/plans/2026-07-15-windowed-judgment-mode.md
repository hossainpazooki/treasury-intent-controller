# Windowed Judgment Mode Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship a `window` judgment mode (judge sees claims + full reply + mechanical doc windows around each quote) and measure it against excerpt-only in the credentialed probe, with a doc-inversion trap the excerpt view provably cannot catch.

**Architecture:** All lane logic stays in `skeptic.py`: a fail-loud config resolver (`config/skeptic.json`), a pure `doc_windows()` locator, and a mode parameter on `judge_claims()`. `server.py` plumbs the resolved mode + reply text + startup-loaded docs into the lane. `scripts/live_smoke.py` gains the trap (c4) and a both-modes comparison with gated/reported checks. README flips only after the live run passes.

**Tech Stack:** Python 3.12+, FastAPI, anthropic SDK, pytest (all API calls mocked offline), httpx/uvicorn in the probe.

**Spec:** `docs/superpowers/specs/2026-07-15-windowed-judgment-mode-design.md` — read it first; it fixes the trap bytes, gating rules, and honesty fallback.

## Global Constraints

- **Never run `git commit`/`git push`** — each task's last step *records* the commit command; all commands are emitted to Hossain at the end (his global git rule).
- Model ids live only in `config/models.json`; the judgment-context mode lives only in `config/skeptic.json`. No literals in code or prose.
- SSE event names/shapes unchanged. Verdict vocabulary unchanged. `VERDICT_TOOL` unchanged. Worker lane untouched. Cached document prefix untouched.
- Excerpt mode must remain **byte-for-byte** prompt-identical to today (`JUDGMENT_PROMPT` unchanged).
- Fail-loud at import for config; fail-loud (`raise` → `skeptic_error`) for unlocatable quotes at runtime.
- Offline tests never hit the API (`FakeClient` pattern in `test_skeptic.py`).
- Windows console is cp1252 — keep all `print()` output ASCII.
- Run tests with: `python -m pytest -q` from the repo root (`C:\Users\hossa\dev\tic-concept-chat`).

---

### Task 1: `config/skeptic.json` + fail-loud mode resolver

**Files:**
- Create: `config/skeptic.json`
- Modify: `skeptic.py` (add loader + import-time constant)
- Test: `test_skeptic.py`

**Interfaces:**
- Produces: `skeptic.load_judgment_context(path: Path | None = None) -> str`; `skeptic.JUDGMENT_CONTEXT: str` (resolved at import); `skeptic.JUDGMENT_CONTEXTS = ("excerpt", "window")`. Tasks 3–5 rely on these exact names.

- [ ] **Step 1: Write the failing tests** — append to the `--- config / roles ---` section of `test_skeptic.py`:

```python
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
```

- [ ] **Step 2: Run to verify failure**

Run: `python -m pytest test_skeptic.py -q -k judgment_context`
Expected: FAIL — `AttributeError: module 'skeptic' has no attribute 'JUDGMENT_CONTEXT'`

- [ ] **Step 3: Create `config/skeptic.json`**

```json
{
  "judgment_context": "window"
}
```

- [ ] **Step 4: Implement the loader** — in `skeptic.py`, add `from pathlib import Path` to the imports, then insert after the `WORKER_MODEL`/`JUDGMENT_MODEL` block:

```python
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
```

- [ ] **Step 5: Run the full suite**

Run: `python -m pytest -q`
Expected: all pass (35 existing + 2 new).

- [ ] **Step 6: Record the commit command (do NOT run git)**

```bash
git add config/skeptic.json skeptic.py test_skeptic.py
git commit -m "feat: judgment-context mode config, fail-loud at import"
```

---

### Task 2: `doc_windows()` — mechanical context locator

**Files:**
- Modify: `skeptic.py`
- Test: `test_skeptic.py`

**Interfaces:**
- Consumes: the claims dict shape validated by `validate_claims` (`{"claims": [{"id", "claim_text", "status", "citation": {"title", "cited_text"} | None}]}`).
- Produces: `skeptic.doc_windows(claims: dict, docs: dict[str, str], radius: int = WINDOW_RADIUS) -> dict[str, str]` (claim id → surrounding passage; cited claims only) and `skeptic.WINDOW_RADIUS = 400`. Task 3 calls it exactly like this.

- [ ] **Step 1: Write the failing tests** — new section in `test_skeptic.py`:

```python
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
```

- [ ] **Step 2: Run to verify failure**

Run: `python -m pytest test_skeptic.py -q -k doc_windows`
Expected: FAIL — `AttributeError: module 'skeptic' has no attribute 'doc_windows'`

- [ ] **Step 3: Implement** — in `skeptic.py`, after `render_reply`:

```python
WINDOW_RADIUS = 400


def doc_windows(
    claims: dict, docs: dict[str, str], radius: int = WINDOW_RADIUS
) -> dict[str, str]:
    """Per cited claim, the doc passage surrounding its quote (window mode).

    Mechanical by design: exact substring search (the live smoke proved every
    real citation is a literal substring; first occurrence wins), then +/-
    `radius` chars snapped outward to line boundaries. A quote that cannot be
    located fails loud - it surfaces as skeptic_error, never as a silently
    windowless judgment."""
    windows: dict[str, str] = {}
    for c in claims["claims"]:
        if c["status"] != "cited":
            continue
        title, quote = c["citation"]["title"], c["citation"]["cited_text"]
        doc = docs.get(title)
        if doc is None:
            raise ValueError(f"claim {c['id']!r} cites unknown document {title!r}")
        at = doc.find(quote)
        if at < 0:
            raise ValueError(f"claim {c['id']!r}: cited_text not found in {title!r}")
        start = doc.rfind("\n", 0, max(at - radius, 0)) + 1
        end = doc.find("\n", at + len(quote) + radius)
        windows[c["id"]] = doc[start : end if end >= 0 else len(doc)].strip("\n")
    return windows
```

- [ ] **Step 4: Run the full suite**

Run: `python -m pytest -q`
Expected: all pass.

- [ ] **Step 5: Record the commit command (do NOT run git)**

```bash
git add skeptic.py test_skeptic.py
git commit -m "feat: doc_windows - mechanical doc context around each quote"
```

---

### Task 3: window mode in `judge_claims`

**Files:**
- Modify: `skeptic.py`
- Test: `test_skeptic.py`

**Interfaces:**
- Consumes: `doc_windows` (Task 2), `JUDGMENT_CONTEXTS` (Task 1).
- Produces: `skeptic.judge_claims(client, claims, *, mode: str = "excerpt", reply_text: str | None = None, docs: dict[str, str] | None = None) -> list[dict]` and `skeptic.JUDGMENT_PROMPT_WINDOW`. Tasks 4–5 call it with these exact keywords. Default `"excerpt"` keeps every existing caller byte-identical.

- [ ] **Step 1: Write the failing tests** — append to the `--- lane calls ---` section:

```python
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
```

- [ ] **Step 2: Run to verify failure**

Run: `python -m pytest test_skeptic.py -q -k "window_mode or unknown_mode or prompt_unchanged"`
Expected: FAIL — `TypeError: judge_claims() got an unexpected keyword argument 'mode'`

- [ ] **Step 3: Implement** — in `skeptic.py`, add the window prompt after `JUDGMENT_PROMPT` (leave `JUDGMENT_PROMPT` untouched):

```python
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
```

then replace the whole `judge_claims` function with:

```python
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
```

Also update the module docstring's last two sentences to say the judgment lane's context is mode-selected (`config/skeptic.json`): excerpt-only, or windowed (reply + mechanical doc passages).

- [ ] **Step 4: Run the full suite**

Run: `python -m pytest -q`
Expected: all pass — including the untouched `test_judge_claims_forces_judgment_tool_and_gets_excerpts_only` (defaults preserve the excerpt path).

- [ ] **Step 5: Record the commit command (do NOT run git)**

```bash
git add skeptic.py test_skeptic.py
git commit -m "feat: windowed judgment mode - reply + doc windows to the judge"
```

---

### Task 4: server wiring

**Files:**
- Modify: `server.py`
- Test: `test_server.py`

**Interfaces:**
- Consumes: `skeptic.JUDGMENT_CONTEXT` (Task 1), `judge_claims` keywords (Task 3), `context.load_docs()` (exists).
- Produces: `server.DOCS: dict[str, str]` (loaded at import); `skeptic_pass` passes `mode`/`reply_text`/`docs`. The probe (Task 5) relies on the server lane running the configured mode.

- [ ] **Step 1: Update the monkeypatched fakes and write the failing test** — in `test_server.py`, change the two `judge_claims` lambdas (in `test_skeptic_pass_event_sequence` and `test_skeptic_pass_no_claims`) from `lambda client, claims:` to `lambda client, claims, **kw:` (the server now passes keywords; positional-only fakes would break). Then append:

```python
def test_skeptic_pass_plumbs_mode_reply_and_docs(monkeypatch):
    # The lane must run the CONFIGURED mode with the rendered reply and the
    # startup-loaded docs - a server that silently judges excerpt-only while
    # config says window would be a fail-open verification path.
    monkeypatch.setattr(server, "get_client", lambda: object())
    monkeypatch.setattr(server.skeptic, "extract_claims", lambda client, content: _claims())
    seen = {}

    def fake_judge(client, claims, **kw):
        seen.update(kw)
        return [{"id": "c1", "verdict": "supported"},
                {"id": "c2", "verdict": "unmarked-inference"}]

    monkeypatch.setattr(server.skeptic, "judge_claims", fake_judge)
    content = [__import__("types").SimpleNamespace(type="text", text="the reply", citations=None)]
    _events(server.skeptic_pass(content))
    assert seen["mode"] == server.skeptic.JUDGMENT_CONTEXT
    assert seen["reply_text"] == server.skeptic.render_reply(content)
    assert seen["docs"] == server.DOCS


def test_docs_loaded_at_import():
    assert set(server.DOCS) == set(context.DOC_NAMES)
```

- [ ] **Step 2: Run to verify failure**

Run: `python -m pytest test_server.py -q`
Expected: FAIL — `AttributeError: module 'server' has no attribute 'DOCS'`

- [ ] **Step 3: Implement** — in `server.py`: below `DOCUMENTS = context.build_document_blocks()` add:

```python
DOCS = context.load_docs()  # raw doc text for the window-mode judge
```

and in `skeptic_pass`, replace the `judge_claims` call with:

```python
        verdicts = skeptic.judge_claims(
            get_client(),
            claims,
            mode=skeptic.JUDGMENT_CONTEXT,
            reply_text=skeptic.render_reply(final_content),
            docs=DOCS,
        )
```

- [ ] **Step 4: Run the full suite**

Run: `python -m pytest -q`
Expected: all pass.

- [ ] **Step 5: Record the commit command (do NOT run git)**

```bash
git add server.py test_server.py
git commit -m "feat: skeptic pass runs the configured judgment context"
```

---

### Task 5: the trap + both-modes measurement in the probe

**Files:**
- Modify: `scripts/live_smoke.py`
- Test: `test_context.py` (offline trap tripwire)

**Interfaces:**
- Consumes: `judge_claims` keywords (Task 3), `skeptic.JUDGMENT_CONTEXT` (Task 1), `context.load_docs()`.
- Produces: the extended probe. Gated checks added: trap-passage precondition, server-mode-is-window, window verdicts on c1–c4. Reported (never gated): excerpt's c4 verdict, per-mode organic verdict counts, per-mode judgment token usage.

- [ ] **Step 1: Write the offline trap tripwire (failing only if the doc drifts)** — append to `test_context.py`:

```python
def test_trap_passage_still_present_in_contract_durability():
    # scripts/live_smoke.py plants a claim quoting this exact substring; its
    # known-correct verdict (overreach) depends on the inverting "no longer "
    # sitting immediately BEFORE the quote - outside any excerpt. If tic's
    # contract rewords this line, fix the probe's TRAP_QUOTE (backup trap in
    # the spec: the GlobalSeq/TrajectoryHash "never" sentence).
    docs = context.load_docs()
    quote = "calls `adapter.OnAchieved` in-process"
    doc = docs["CONTRACT-DURABILITY.md"]
    at = doc.find(quote)
    assert at > 0, "trap quote vanished from CONTRACT-DURABILITY.md"
    assert doc[at - len("no longer "):at] == "no longer "
```

- [ ] **Step 2: Run it**

Run: `python -m pytest test_context.py -q`
Expected: PASS (it guards reality; it fails only when the doc drifts).

- [ ] **Step 3: Extend the probe.** In `scripts/live_smoke.py`:

**(3a)** Add to the imports: `from types import SimpleNamespace`. After the `check()` definition add:

```python
def report(name: str, evidence: str) -> None:
    print(f"[REPORT] {name}\n        {evidence}\n", flush=True)


class RecordingClient:
    """Pass-through client that captures usage per call (cost measurement)."""

    def __init__(self, inner):
        self._inner = inner
        self.last_usage = None
        self.messages = SimpleNamespace(create=self._create)

    def _create(self, **kw):
        resp = self._inner.messages.create(**kw)
        self.last_usage = resp.usage
        return resp
```

**(3b)** In `main()`, extend the models line and gate the shipped mode — after the existing `print(f"models: ...")` add:

```python
    print(f"judgment_context={skeptic.JUDGMENT_CONTEXT}\n")
    check("server ships window judgment mode", skeptic.JUDGMENT_CONTEXT == "window",
          f"config/skeptic.json -> {skeptic.JUDGMENT_CONTEXT}")
```

**(3c)** Replace the whole non-vacuity section — everything from the `# ---- NON-VACUITY` comment through the last of its three `check(...)` calls, including the old `planted` dict and `real_quote` loop (the block below redefines both) — with:

```python
    # ---- NON-VACUITY + MODE COMPARISON --------------------------------------
    # Planted claims with known-correct verdicts, judged in BOTH modes.
    # c1-c3: the original trio (both modes must get them right).
    # c4: the doc-inversion trap - a genuine quote whose inverting "no longer "
    # sits immediately BEFORE it in CONTRACT-DURABILITY.md, outside any
    # excerpt BY CONSTRUCTION. Window mode must catch it (gated); excerpt
    # mode's verdict is REPORTED, never gated - if it says overreach it can
    # only be prior, not evidence, and that is a finding to record.
    real_quote = "Unevaluable"  # replaced below with a genuine doc sentence
    for line in docs["CONTRACT.md"].splitlines():
        if "Unevaluable" in line and len(line) > 40:
            real_quote = line.strip("- *#").strip()
            break

    TRAP_QUOTE = "calls `adapter.OnAchieved` in-process"
    trap_doc = docs["CONTRACT-DURABILITY.md"]
    at = trap_doc.find(TRAP_QUOTE)
    check("trap passage present, negation just outside the excerpt",
          at > 0 and trap_doc[at - len("no longer "):at] == "no longer ",
          f"quote at byte {at}; preceding bytes {trap_doc[max(at - 20, 0):at]!r}")

    planted = {"claims": [
        {   # (a) cited, but the quote does NOT carry the claim -> overreach
            "id": "c1",
            "claim_text": "The gate automatically retries a timed-out scorer three times before failing.",
            "status": "cited",
            "citation": {"title": "CONTRACT.md", "cited_text": real_quote},
        },
        {   # (b) bald uncited assertion -> unmarked-inference
            "id": "c2",
            "claim_text": "The gate settles the payment itself once all criteria pass.",
            "status": "uncited",
            "citation": None,
        },
        {   # (c) uncited but self-marked as inference -> marked-inference
            "id": "c3",
            "claim_text": "My inference, not stated in the contracts: this design likely eases audit.",
            "status": "uncited",
            "citation": None,
        },
        {   # (d) THE TRAP: quote genuinely carries the claim locally; the doc
            #     inverts it two words earlier -> overreach, invisible to excerpts
            "id": "c4",
            "claim_text": "The gate calls `adapter.OnAchieved` in-process during authorization.",
            "status": "cited",
            "citation": {"title": "CONTRACT-DURABILITY.md", "cited_text": TRAP_QUOTE},
        },
    ]}
    planted_reply = " ".join(c["claim_text"] for c in planted["claims"])
    expected = {"c1": "overreach", "c2": "unmarked-inference", "c3": "marked-inference"}

    rec = RecordingClient(server.get_client())

    time.sleep(60)  # pace: each judgment call is a fresh fable-5 request
    excerpt_v = {v["id"]: v["verdict"] for v in skeptic.judge_claims(rec, planted, mode="excerpt")}
    ex_usage = rec.last_usage

    time.sleep(60)
    window_v = {v["id"]: v["verdict"] for v in skeptic.judge_claims(
        rec, planted, mode="window", reply_text=planted_reply, docs=docs)}
    win_usage = rec.last_usage

    for cid, want in expected.items():
        check(f"excerpt mode: {cid} -> {want}", excerpt_v.get(cid) == want,
              f"verdicts={excerpt_v}")
        check(f"window mode (no regression): {cid} -> {want}", window_v.get(cid) == want,
              f"verdicts={window_v}")
    check("window mode CATCHES the doc-inversion trap (c4 -> overreach)",
          window_v.get("c4") == "overreach", f"verdicts={window_v}")
    report("excerpt mode's verdict on the trap c4 (expected miss: 'supported')",
           f"{excerpt_v.get('c4')} - the negation is absent from its input; "
           "a correct verdict here could only be prior, not evidence")
    report("judgment cost per mode (planted set)",
           f"excerpt in={ex_usage.input_tokens} out={ex_usage.output_tokens}; "
           f"window in={win_usage.input_tokens} out={win_usage.output_tokens}")

    # ---- Organic reply, both modes ------------------------------------------
    # The stream already judged turn 1's claims in the CONFIGURED mode
    # (window). Re-judge the same claims excerpt-only and report the shift.
    if claims_ev and verdict_ev:
        organic = {"claims": claims_ev["claims"]}
        stream_counts = done_ev["counts"] if done_ev else {}
        time.sleep(60)
        organic_ex = skeptic.judge_claims(rec, organic, mode="excerpt")
        ex_counts: dict[str, int] = {}
        for v in organic_ex:
            ex_counts[v["verdict"]] = ex_counts.get(v["verdict"], 0) + 1
        flipped = sum(
            1 for v in organic_ex
            for s in [next(e for e in verdict_ev if e["id"] == v["id"])]
            if s["verdict"] != v["verdict"]
        )
        report("organic reply verdict distribution",
               f"window (stream): {stream_counts}; excerpt (re-judge): {ex_counts}; "
               f"{flipped}/{len(organic_ex)} verdicts differ between modes")
```

**(3d)** Update the probe's module docstring: third bullet becomes "the judgment lane is fed PLANTED claims whose correct verdicts are known in advance, in BOTH context modes — a rubber-stamp judge, a blanket-refuter, AND an excerpt view blind to a doc-inversion trap are all caught or measured." Add a line: "Runtime ~6 min (pacing sleeps between fable-5 calls)."

- [ ] **Step 4: Sanity-run the probe's offline surface**

Run: `python -m pytest -q` and `python -c "import ast; ast.parse(open('scripts/live_smoke.py').read())"`
Expected: suite passes; the script parses. (The probe itself runs in Task 6 — it needs a key and real tokens.)

- [ ] **Step 5: Record the commit command (do NOT run git)**

```bash
git add scripts/live_smoke.py test_context.py
git commit -m "test: doc-inversion trap + both-modes measurement in the live probe"
```

---

### Task 6: run the gates (operator-assisted)

**Files:** none created — this task produces evidence.

- [ ] **Step 1: Full offline suite**

Run: `python -m pytest -q`
Expected: 50 passed (35 original + 15 new: 2 config, 6 doc_windows, 4 window-judge, 2 server, 1 trap tripwire), 0 failed.

- [ ] **Step 2: Credentialed live run.** Requires the key. If `~/.anthropic-key` still exists: run in Git Bash. If Hossain deleted it, ask him to re-supply it the same way (written by his own terminal, never pasted into the transcript).

Run (Git Bash):
```bash
cd ~/dev/tic-concept-chat
export ANTHROPIC_API_KEY="$(cat ~/.anthropic-key)"
python scripts/live_smoke.py
```
Expected: all gated checks PASS (original 13, reshaped where the planted section changed, + the new mode/trap gates); `[REPORT]` lines show excerpt's c4 verdict, per-mode organic distribution, and token costs. A `Rate limited` result is INCONCLUSIVE — wait and re-run; never read it as app evidence.

- [ ] **Step 3: Decision per the spec's gating rule**
  - **All gates green** → default stays `window` (already shipped in Task 1); proceed to Task 7's success branch.
  - **Any window gate red** → flip `config/skeptic.json` to `"excerpt"` AND update the pin in `test_skeptic.py::test_judgment_context_resolved_at_import_and_pinned` to `"excerpt"` (the deliberate two-file act), re-run `python -m pytest -q`, and take Task 7's fallback branch. Record the actual verdicts either way — a measured "window did not win" is a publishable result.

---

### Task 7: README honesty update

**Files:**
- Modify: `README.md`

- [ ] **Step 1 (success branch): rewrite the limitation + status.** In the "Producer≠judge skeptic pass" section:
  - Replace the excerpt-only paragraph ("The judgment lane sees **excerpts only** … can miss context-dependent overreach.") with: the judgment context is config-selected (`config/skeptic.json`); default `window` gives the judge the claims, the full reply, and a mechanical doc window around each quote (server-side substring location — never the full ~18K-token docs, and never judge-chosen). Residual limitation, accepted by design: negation or qualification **outside the window radius**, and context split **across documents**, remain uncatchable; excerpt-only mode is still available in config.
  - Extend the section's `> Status:` block with the measured evidence (fill in the ACTUAL numbers from Task 6): date, both modes on the planted set, window catching the c4 doc-inversion trap, excerpt's c4 verdict, and the measured token-cost multiple.
  - In the `## Test` section, update the live-smoke paragraph: planted claims are judged in both context modes, including a doc-inversion trap quoting real `CONTRACT-DURABILITY.md` bytes whose inverting "no longer" sits outside any excerpt; note the ~6-min runtime from pacing.
- [ ] **Step 1-alt (fallback branch, only if a window gate failed):** leave the limitation paragraph, add one sentence: a windowed mode exists behind `config/skeptic.json` but measured below the bar on <date> (state which gate failed with the verdicts); default remains excerpt-only.
- [ ] **Step 2: Verify docs against reality** — every number in the README edit must come from the Task 6 output, none from this plan.
- [ ] **Step 3: Record the commit command (do NOT run git)**

```bash
git add README.md
git commit -m "docs: windowed judgment mode - measured status + residual limitation"
```

---

## Final step: emit all recorded commit commands

Output every recorded commit command from Tasks 1–7 as one Git Bash block (`cd ~/dev/tic-concept-chat` first), plus the two doc commits if not yet run (spec + this plan). Claude never runs them.
