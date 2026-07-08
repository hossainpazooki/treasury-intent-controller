# Model Tiers + Producer≠Judge Skeptic Pass — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Port rigor `f5d9783`'s substance into tic-concept-chat: config-centralized role→model choice (Stage A, Tasks 1–2), then a three-lane producer/worker/judge skeptic pass over each delivered reply (Stage B, Tasks 3–6).

**Architecture:** Stage A adds `config/models.json` + `models.py` (flat role→model, fail-loud at import) and repoints `server.py`'s model literal through it, guarded by a check-tier-sync analog test. Stage B adds `skeptic.py` — a Sonnet 5 worker lane that mechanically extracts claim/citation pairs into forced-tool JSON, and a Fable 5 judgment lane that issues per-claim refutation verdicts from excerpts only — streamed as new SSE events on the same response after `done`, rendered as verdict chips in the UI.

**Tech Stack:** Python 3.14, FastAPI, anthropic SDK (already in `requirements.txt` — no new dependencies), pytest, vanilla-JS single-file UI.

**Spec of record:** `docs/superpowers/specs/2026-07-07-model-tiers-skeptic-pass-design.md` — the plan implements it; where they disagree, the spec wins.

## Global Constraints

- **Never run `git commit` / `git push`** — output the commit command for Hossain and continue (global git rule; overrides this template's commit steps).
- **Tests are offline/mocked only** — no `ANTHROPIC_API_KEY` exists on this machine. Live effect stays **unverified**; every status line written anywhere says "built, tests green, effect unverified", never "working".
- **Cached prefix stays byte-identical** — nothing in Stage B may touch the producer call's document blocks, system prompt, or `inject_documents`.
- **Producer≠judge** — `context.py` FRAMING's self-skepticism stays as style; verification duty moves to lanes 2–3. Do not merge lanes.
- **No orphan roles** — the role-sync test (Task 2) must stay green through every task; config roles and `models.resolve("...")` call sites land in the same change.
- **Model ids in exactly one place** — `config/models.json`. No model literal in code; Stage B removes the one in `static/index.html` prose.
- **README updated in the same change as each stage** (docs-honesty rule).
- **Windows console is cp1252** — keep any `print()`/stdout ASCII (HTML/JS files are UTF-8 and may use `✓✗○⚠`).
- Exact model ids (copied from spec): `discussion: claude-opus-4-8`, `worker: claude-sonnet-5`, `judgment: claude-fable-5`.

---

## File Structure

```
tic-concept-chat/
├── config/
│   └── models.json        # CREATE Task 1 (discussion only) · MODIFY Task 3 (+worker, +judgment)
├── models.py              # CREATE Task 1 — load_config(), resolve()
├── test_models.py         # CREATE Task 1 · EXTEND Task 2 (role-sync test)
├── skeptic.py             # CREATE Task 3 — tools, prompts, render/validate, extract/judge
├── test_skeptic.py        # CREATE Task 3
├── server.py              # MODIFY Task 2 (resolve("discussion")) · Task 4 (skeptic_pass)
├── test_server.py         # EXTEND Task 4
├── static/index.html      # MODIFY Task 5 (verdict panel, header prose)
├── context.py             # MODIFY Task 6 (stale "three docs" docstrings only)
└── README.md              # MODIFY Task 2 (Stage A note) · Task 6 (Stage B section)
```

---

### Task 1: Config substrate — `config/models.json` + `models.py`

**Files:**
- Create: `config/models.json`
- Create: `models.py`
- Test: `test_models.py`

**Interfaces:**
- Consumes: nothing (leaf module — stdlib only).
- Produces: `models.load_config(path: Path | None = None) -> dict[str, str]` and `models.resolve(role: str, path: Path | None = None) -> str`. Task 2 calls `models.resolve("discussion")` in `server.py` and `models.load_config()` in the sync test; Task 3 calls `models.resolve("worker")` / `models.resolve("judgment")` in `skeptic.py`.

- [ ] **Step 1: Write the failing tests**

Create `test_models.py`:

```python
import json

import pytest

import models


def test_resolve_discussion():
    assert models.resolve("discussion") == "claude-opus-4-8"


def test_unknown_role_fails_loudly():
    with pytest.raises(KeyError) as exc:
        models.resolve("nonexistent-role")
    assert "nonexistent-role" in str(exc.value)


def test_missing_config_fails_loudly(tmp_path):
    with pytest.raises(FileNotFoundError):
        models.resolve("discussion", tmp_path / "absent.json")


def test_malformed_config_fails_loudly(tmp_path):
    bad = tmp_path / "models.json"
    bad.write_text(json.dumps({"discussion": 7}), encoding="utf-8")
    with pytest.raises(ValueError):
        models.resolve("discussion", bad)


def test_config_is_flat_role_to_model():
    config = models.load_config()
    assert all(isinstance(v, str) and v for v in config.values())
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `python -m pytest test_models.py -v`
Expected: collection error — `ModuleNotFoundError: No module named 'models'`

- [ ] **Step 3: Write the implementation**

Create `config/models.json` — **`discussion` only**; `worker`/`judgment` arrive with the code that dispatches them (Task 3), so the Task-2 sync test never sees an orphan:

```json
{
  "discussion": "claude-opus-4-8"
}
```

Create `models.py`:

```python
"""Role -> model resolution from config/models.json.

Model choice lives in exactly one place (the config); code never carries a
model literal. The mapping is deliberately flat (role -> model): three roles
in one service do not earn rigor's tier->model indirection. Fail-loud
posture matches context.py's doc loading: a missing/malformed config or an
unknown role raises immediately.
"""
from __future__ import annotations

import json
from pathlib import Path

CONFIG_PATH = Path(__file__).resolve().parent / "config" / "models.json"


def load_config(path: Path | None = None) -> dict[str, str]:
    path = path if path is not None else CONFIG_PATH
    if not path.is_file():
        raise FileNotFoundError(f"model config not found: {path}")
    data = json.loads(path.read_text(encoding="utf-8"))
    if not isinstance(data, dict) or not data:
        raise ValueError(f"model config must be a non-empty JSON object: {path}")
    for role, model in data.items():
        if not isinstance(model, str) or not model:
            raise ValueError(f"role {role!r} must map to a model id string: {path}")
    return data


def resolve(role: str, path: Path | None = None) -> str:
    config = load_config(path)
    if role not in config:
        raise KeyError(f"unknown model role {role!r}; config has {sorted(config)}")
    return config[role]
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `python -m pytest test_models.py -v`
Expected: 5 passed

- [ ] **Step 5: Run the full suite (regression)**

Run: `python -m pytest -q`
Expected: 14 passed (9 existing + 5 new)

- [ ] **Step 6: Output the commit command for Hossain (do not run)**

```bash
cd ~/dev/tic-concept-chat
git add config/models.json models.py test_models.py
git commit -m "feat: config-centralized role->model resolution (Stage A substrate)"
```

---

### Task 2: Repoint `server.py` through the config + role-sync test + README Stage A note

**Files:**
- Modify: `server.py:21` (the `MODEL` literal) and its import block
- Modify: `README.md` (model-choice paragraph)
- Test: `test_models.py` (append the sync test)

**Interfaces:**
- Consumes: `models.resolve(role)` and `models.load_config()` from Task 1.
- Produces: the role-sync invariant every later task must keep green — every `models.resolve("<role>")` literal in non-test code exists in the config, and every config role is dispatched somewhere. Task 3 relies on this shape: it must add config entries and `resolve` calls **in the same change**.

- [ ] **Step 1: Write the failing test**

Append to `test_models.py`:

```python
import re
from pathlib import Path

ROOT = Path(__file__).resolve().parent
RESOLVE_RE = re.compile(r'models\.resolve\(\s*"([a-z_]+)"')


def roles_referenced_in_code() -> set[str]:
    found: set[str] = set()
    for py in ROOT.glob("*.py"):
        if py.name.startswith("test_") or py.name == "models.py":
            continue
        found |= set(RESOLVE_RE.findall(py.read_text(encoding="utf-8")))
    return found


def test_role_sync():
    # check-tier-sync analog: bidirectional, no exceptions list.
    code_roles = roles_referenced_in_code()
    config_roles = set(models.load_config())
    assert code_roles, "no models.resolve(...) call found in code"
    missing = code_roles - config_roles
    orphans = config_roles - code_roles
    assert not missing, f"roles used in code but absent from config: {sorted(missing)}"
    assert not orphans, f"orphan roles in config no code dispatches: {sorted(orphans)}"
```

- [ ] **Step 2: Run test to verify it fails**

Run: `python -m pytest test_models.py::test_role_sync -v`
Expected: FAIL — `assert code_roles` (no `models.resolve` call exists in code yet)

- [ ] **Step 3: Modify `server.py`**

In the import block (after `import context`), add:

```python
import models
```

Replace line 21:

```python
MODEL = "claude-opus-4-8"
```

with:

```python
# Resolved at import so a missing/malformed config or unknown role fails the
# server at startup, loudly - same posture as the doc loading below.
MODEL = models.resolve("discussion")
```

No other change; behavior is identical (Stage A promise).

- [ ] **Step 4: Run tests to verify they pass**

Run: `python -m pytest -q`
Expected: 15 passed

- [ ] **Step 5: Update README (same-change docs rule)**

In `README.md`, in the intro paragraph, change

```
with Claude
Opus 4.8, primed on the repo's README + CONTRACT.md + CONTRACT-DURABILITY.md +
CONTRACT-SCORER.md.
```

to

```
with Claude
(models per `config/models.json`), primed on the repo's README + CONTRACT.md +
CONTRACT-DURABILITY.md + CONTRACT-SCORER.md.
```

and add to the **Notes** section:

```markdown
- Model choice is config-centralized (rigor's tier discipline, flattened to
  role -> model): `config/models.json` is the only place a model id lives, and
  a sync test fails if a role is referenced in code but missing from config,
  or sits in config with no code path dispatching it.
```

- [ ] **Step 6: Output the commit command for Hossain (do not run)**

```bash
cd ~/dev/tic-concept-chat
git add server.py test_models.py README.md
git commit -m "feat: server model via resolve('discussion'); role-sync test (Stage A done)"
```

---

### Task 3: `skeptic.py` — lanes 2–3 (extraction + refutation) with config roles

**Files:**
- Create: `skeptic.py`
- Modify: `config/models.json` (add `worker`, `judgment` — same change as the `resolve` calls, keeping role-sync green)
- Test: `test_skeptic.py`

**Interfaces:**
- Consumes: `models.resolve(role)` from Task 1.
- Produces (Task 4 calls exactly these):
  - `skeptic.extract_claims(client, content: list) -> dict` — returns validated `{"claims": [{"id", "claim_text", "status", "citation"}]}`; `content` is the final message's SDK content blocks (objects with `.type`, `.text`, `.citations` attributes).
  - `skeptic.judge_claims(client, claims: dict) -> list[dict]` — returns `[{"id": str, "verdict": str}]`; empty list when there are no claims (no API call).
  - Both raise (`ValueError` or SDK errors) on any failure; the caller maps that to `skeptic_error`.

- [ ] **Step 1: Write the failing tests (pure functions first)**

Create `test_skeptic.py`:

```python
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `python -m pytest test_skeptic.py -v`
Expected: collection error — `ModuleNotFoundError: No module named 'skeptic'`

- [ ] **Step 3: Write the implementation**

Modify `config/models.json` (same change as the `resolve` calls below — role-sync):

```json
{
  "discussion": "claude-opus-4-8",
  "worker": "claude-sonnet-5",
  "judgment": "claude-fable-5"
}
```

Create `skeptic.py`:

```python
"""Three-lane skeptic pass over a delivered reply (lanes 2-3).

Producer!=judge: the discussion lane's self-skepticism instruction is style,
not duty - verification lives here. Lane 2 (worker, Sonnet 5) mechanically
transcribes claim/citation pairs into a forced-tool JSON schema; lane 3
(judgment, Fable 5) issues per-claim refutation verdicts from the claims JSON
alone - it never re-reads the ~18K-token docs or the full reply. The smallest
context goes to the most expensive model; that economy is the point.
"""
from __future__ import annotations

import json
from typing import Any

import models

WORKER_MAX_TOKENS = 8000
JUDGMENT_MAX_TOKENS = 4000

CLAIM_STATUSES = ("cited", "uncited")
VERDICTS_BY_STATUS = {
    "cited": ("supported", "overreach"),
    "uncited": ("marked-inference", "unmarked-inference"),
}

EXTRACT_TOOL = {
    "name": "record_claims",
    "description": "Record every factual claim in the reply with its citation status.",
    "input_schema": {
        "type": "object",
        "properties": {
            "claims": {
                "type": "array",
                "items": {
                    "type": "object",
                    "properties": {
                        "id": {"type": "string"},
                        "claim_text": {"type": "string"},
                        "status": {"type": "string", "enum": ["cited", "uncited"]},
                        "citation": {
                            "type": ["object", "null"],
                            "properties": {
                                "title": {"type": "string"},
                                "cited_text": {"type": "string"},
                            },
                            "required": ["title", "cited_text"],
                        },
                    },
                    "required": ["id", "claim_text", "status", "citation"],
                },
            },
        },
        "required": ["claims"],
    },
}

VERDICT_TOOL = {
    "name": "record_verdicts",
    "description": "Record a refutation verdict for every claim.",
    "input_schema": {
        "type": "object",
        "properties": {
            "verdicts": {
                "type": "array",
                "items": {
                    "type": "object",
                    "properties": {
                        "id": {"type": "string"},
                        "verdict": {
                            "type": "string",
                            "enum": [
                                "supported",
                                "overreach",
                                "marked-inference",
                                "unmarked-inference",
                            ],
                        },
                    },
                    "required": ["id", "verdict"],
                },
            },
        },
        "required": ["verdicts"],
    },
}

WORKER_PROMPT = """\
Transcribe every factual claim in the reply below into the record_claims
tool. Mechanical transcription only - do not judge whether any claim is
true. A claim is "cited" when a [cites <title>: "<quote>"] annotation
follows it; copy that annotation's title and quote into the citation field
verbatim. Any other claim is "uncited" with citation null. Ids c1, c2, ...
in reading order.

Reply:"""

JUDGMENT_PROMPT = """\
You are a skeptic judging claims extracted from a reply about the
treasury-intent-controller contracts. Record one verdict per claim via the
record_verdicts tool. Default to refutation. A cited claim is "supported"
only if its cited_text excerpt actually carries the claim as stated -
otherwise "overreach". An uncited claim is "marked-inference" only if its
own text presents itself as inference or opinion - otherwise
"unmarked-inference". Judge only from the JSON below; you have no access to
the source documents.

Claims:"""


def render_reply(content: list[Any]) -> str:
    """Plain-text rendering of the final message for the worker lane: reply
    text with each citation annotated inline where the API attached it."""
    parts: list[str] = []
    for block in content:
        if getattr(block, "type", None) != "text":
            continue
        parts.append(block.text)
        for c in getattr(block, "citations", None) or []:
            title = getattr(c, "document_title", "") or ""
            cited = getattr(c, "cited_text", "") or ""
            parts.append(f'\n[cites {title}: "{cited}"]\n')
    return "".join(parts)


def _tool_input(resp: Any, tool_name: str) -> dict:
    for block in resp.content:
        if getattr(block, "type", None) == "tool_use" and block.name == tool_name:
            return block.input
    raise ValueError(f"model response carried no {tool_name!r} tool call")


def validate_claims(data: dict) -> dict:
    claims = data.get("claims")
    if not isinstance(claims, list):
        raise ValueError("extraction output missing 'claims' list")
    for c in claims:
        cid, status = c.get("id"), c.get("status")
        if status not in CLAIM_STATUSES:
            raise ValueError(f"claim {cid!r}: bad status {status!r}")
        if status == "cited" and not c.get("citation"):
            raise ValueError(f"claim {cid!r}: cited but carries no citation")
        if status == "uncited" and c.get("citation") is not None:
            raise ValueError(f"claim {cid!r}: uncited but carries a citation")
    return data


def validate_verdicts(data: dict, claims: dict) -> list[dict]:
    verdicts = data.get("verdicts")
    if not isinstance(verdicts, list):
        raise ValueError("judgment output missing 'verdicts' list")
    status_by_id = {c["id"]: c["status"] for c in claims["claims"]}
    seen: dict[str, str] = {}
    for v in verdicts:
        vid, verdict = v.get("id"), v.get("verdict")
        if vid not in status_by_id:
            raise ValueError(f"verdict for unknown claim {vid!r}")
        if vid in seen:
            raise ValueError(f"duplicate verdict for claim {vid!r}")
        if verdict not in VERDICTS_BY_STATUS[status_by_id[vid]]:
            raise ValueError(
                f"claim {vid!r} is {status_by_id[vid]}; verdict {verdict!r} illegal"
            )
        seen[vid] = verdict
    unjudged = set(status_by_id) - set(seen)
    if unjudged:
        raise ValueError(f"claims with no verdict: {sorted(unjudged)}")
    return verdicts


def extract_claims(client: Any, content: list[Any]) -> dict:
    """Lane 2 (worker): forced-tool claim/citation extraction."""
    resp = client.messages.create(
        model=models.resolve("worker"),
        max_tokens=WORKER_MAX_TOKENS,
        tools=[EXTRACT_TOOL],
        tool_choice={"type": "tool", "name": "record_claims"},
        messages=[{
            "role": "user",
            "content": WORKER_PROMPT + "\n\n" + render_reply(content),
        }],
    )
    return validate_claims(_tool_input(resp, "record_claims"))


def judge_claims(client: Any, claims: dict) -> list[dict]:
    """Lane 3 (judgment): per-claim refutation verdicts from excerpts only."""
    if not claims["claims"]:
        return []
    resp = client.messages.create(
        model=models.resolve("judgment"),
        max_tokens=JUDGMENT_MAX_TOKENS,
        tools=[VERDICT_TOOL],
        tool_choice={"type": "tool", "name": "record_verdicts"},
        messages=[{
            "role": "user",
            "content": JUDGMENT_PROMPT + "\n\n" + json.dumps(claims["claims"], indent=2),
        }],
    )
    return validate_verdicts(_tool_input(resp, "record_verdicts"), claims)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `python -m pytest test_skeptic.py -v`
Expected: 13 passed (1 role test + 2 render + 5 validate_claims + 1 validate_verdicts + 4 lane tests)

- [ ] **Step 5: Run the full suite — role-sync must still be green**

Run: `python -m pytest -q`
Expected: 28 passed. `test_role_sync` in particular: `skeptic.py` now dispatches `worker` and `judgment`, so the two new config entries are not orphans.

- [ ] **Step 6: Output the commit command for Hossain (do not run)**

```bash
cd ~/dev/tic-concept-chat
git add config/models.json skeptic.py test_skeptic.py
git commit -m "feat: worker+judgment skeptic lanes (extraction and refutation, forced-tool JSON)"
```

---

### Task 4: Wire the skeptic pass into the SSE stream (`server.py`)

**Files:**
- Modify: `server.py` — `stream_reply` (lines 46–89) restructured; new `skeptic_pass` generator
- Test: `test_server.py` (append)

**Interfaces:**
- Consumes: `skeptic.extract_claims(client, content)` and `skeptic.judge_claims(client, claims)` from Task 3; existing `sse()` and `get_client()`.
- Produces: SSE events Task 5's UI consumes, exactly these payload shapes:
  - `{"type": "skeptic_claims", "claims": [<claim objects>]}` — once, after extraction
  - `{"type": "skeptic_verdict", "id": "<claim id>", "verdict": "<verdict>"}` — one per claim
  - `{"type": "skeptic_done", "counts": {"<verdict>": <int>, ...}}` — once, at end
  - `{"type": "skeptic_error", "message": "<str>"}` — on any skeptic-lane failure, then end-of-stream

- [ ] **Step 1: Write the failing tests**

Append to `test_server.py` (also add `from types import SimpleNamespace` to its imports):

```python
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `python -m pytest test_server.py -v`
Expected: 3 new tests FAIL with `AttributeError: module 'server' has no attribute 'skeptic_pass'` (and the existing 3 still pass)

- [ ] **Step 3: Modify `server.py`**

Add to the import block (after `import models` from Task 2):

```python
import skeptic
```

Restructure `stream_reply` so `final` escapes the producer's try-block, and append the skeptic pass **after** the producer's error handling — a producer failure yields no `final`, so the skeptic never runs on a failed reply; a skeptic failure can never re-enter the producer's except-chain:

```python
def stream_reply(messages: list[dict]) -> Iterator[str]:
    full = context.inject_documents(messages, blocks=DOCUMENTS)
    final = None
    try:
        with get_client().messages.stream(
            model=MODEL,
            max_tokens=MAX_TOKENS,
            system=SYSTEM,
            thinking={"type": "adaptive", "display": "summarized"},
            messages=full,
        ) as stream:
            for event in stream:
                if event.type == "content_block_delta":
                    delta = event.delta
                    if delta.type == "thinking_delta":
                        yield sse({"type": "thinking", "text": delta.thinking})
                    elif delta.type == "text_delta":
                        yield sse({"type": "text", "text": delta.text})
                    elif delta.type == "citations_delta":
                        c = delta.citation
                        yield sse({
                            "type": "citation",
                            "title": getattr(c, "document_title", "") or "",
                            "text": getattr(c, "cited_text", "") or "",
                        })
            final = stream.get_final_message()
            yield sse({
                "type": "done",
                "usage": {
                    "input_tokens": final.usage.input_tokens,
                    "output_tokens": final.usage.output_tokens,
                    "cache_creation_input_tokens": final.usage.cache_creation_input_tokens,
                    "cache_read_input_tokens": final.usage.cache_read_input_tokens,
                },
            })
    except anthropic.RateLimitError:
        yield sse({"type": "error", "message": "Rate limited - wait a moment and retry."})
    except anthropic.AuthenticationError:
        yield sse({"type": "error", "message": "Auth failed - set ANTHROPIC_API_KEY or run `ant auth login`."})
    except anthropic.APIStatusError as e:
        yield sse({"type": "error", "message": f"API error {e.status_code}: {e.message}"})
    except anthropic.APIConnectionError:
        yield sse({"type": "error", "message": "Network error reaching the API."})
    except Exception as e:  # e.g. SDK TypeError when no credentials resolve
        yield sse({"type": "error", "message": f"{type(e).__name__}: {e}"})
    if final is not None:
        yield from skeptic_pass(final.content)


def skeptic_pass(final_content: list) -> Iterator[str]:
    """Lanes 2-3 on the same stream, after the reply is fully delivered.
    Failure here never taints the delivered reply: skeptic_error, then
    end-of-stream - no retroactive mutation."""
    try:
        claims = skeptic.extract_claims(get_client(), final_content)
        yield sse({"type": "skeptic_claims", "claims": claims["claims"]})
        verdicts = skeptic.judge_claims(get_client(), claims)
        counts: dict[str, int] = {}
        for v in verdicts:
            counts[v["verdict"]] = counts.get(v["verdict"], 0) + 1
            yield sse({"type": "skeptic_verdict", "id": v["id"], "verdict": v["verdict"]})
        yield sse({"type": "skeptic_done", "counts": counts})
    except Exception as e:
        yield sse({"type": "skeptic_error", "message": f"{type(e).__name__}: {e}"})
```

Note: `monkeypatch.setattr(server, "get_client", ...)` in the tests replaces the module attribute, which is what `skeptic_pass` looks up — the `lru_cache` on the real function is untouched.

- [ ] **Step 4: Run the full suite**

Run: `python -m pytest -q`
Expected: 31 passed (includes the `inject_documents` regression tests — cached-prefix invariant untouched, and `test_role_sync` still green: `server.py` gained no new roles)

- [ ] **Step 5: Output the commit command for Hossain (do not run)**

```bash
cd ~/dev/tic-concept-chat
git add server.py test_server.py
git commit -m "feat: stream skeptic pass after done - skeptic_claims/verdict/done/error SSE events"
```

---

### Task 5: UI verdict panel (`static/index.html`)

**Files:**
- Modify: `static/index.html` — header line 50, CSS block, SSE handler in `send()`

**Interfaces:**
- Consumes: the four `skeptic_*` SSE event shapes from Task 4, verbatim.
- Produces: nothing downstream. No automated test exists for the HTML (repo has no JS test rig — do not add one); verification is by eye against the event shapes, and live behavior stays unverified with everything else.

- [ ] **Step 1: Remove the model literal from the header prose**

Replace line 50:

```html
    <small>claude-opus-4-8 · README + CONTRACT + DURABILITY + SCORER · cited</small></h1>
```

with:

```html
    <small>README + CONTRACT + DURABILITY + SCORER · cited · skeptic-checked</small></h1>
```

(model ids now live only in `config/models.json` — the "exactly one place" rule).

- [ ] **Step 2: Add skeptic CSS**

Insert after the `.usage` rule (line 39):

```css
  .skeptic { margin-top:10px; padding-top:8px; border-top:1px solid #2b3138; }
  .skeptic-h { color:var(--accent); font-size:12px; text-transform:uppercase;
               letter-spacing:.04em; margin-bottom:4px; }
  .skeptic-claim { font-size:13px; margin-bottom:4px; }
  .chip { display:inline-block; min-width:88px; margin-right:6px; padding:0 6px;
          border-radius:10px; font-size:12px; border:1px solid #3a4149; color:var(--dim); }
  .chip.supported { color:#3fb950; border-color:#3fb950; }
  .chip.overreach { color:#f85149; border-color:#f85149; }
  .chip.marked-inference { color:var(--dim); }
  .chip.unmarked-inference { color:var(--accent); border-color:var(--accent); }
  .skeptic-summary { color:var(--dim); font-size:12px; margin-top:4px; }
```

- [ ] **Step 3: Create the panel and handle the events**

In `send()`, after `bubble.append(think, body, sourcesEl);` (line 92), add:

```js
  const skepticEl = document.createElement('div');
  skepticEl.className = 'skeptic';
  bubble.appendChild(skepticEl);
  const chipByClaim = {};

  const CHIP = {
    'supported':          ['✓ supported',  'supported'],
    'overreach':          ['✗ overreach',  'overreach'],
    'marked-inference':   ['○ inference',  'marked-inference'],
    'unmarked-inference': ['⚠ unmarked',   'unmarked-inference'],
  };

  function renderClaims(claims) {
    skepticEl.innerHTML = '';
    const h = document.createElement('div');
    h.className = 'skeptic-h';
    h.textContent = 'Skeptic';
    skepticEl.appendChild(h);
    claims.forEach(c => {
      const row = document.createElement('div');
      row.className = 'skeptic-claim';
      const chip = document.createElement('span');
      chip.className = 'chip';
      chip.textContent = '… judging';
      const t = document.createElement('span');
      t.textContent = c.claim_text;
      row.append(chip, t);
      skepticEl.appendChild(row);
      chipByClaim[c.id] = chip;
    });
    if (!claims.length) {
      const p = document.createElement('div');
      p.className = 'skeptic-summary';
      p.textContent = 'no claims extracted';
      skepticEl.appendChild(p);
    }
  }
```

In the SSE `for (const part of parts)` dispatch chain, add after the `done` branch (line 164):

```js
        } else if (ev.type === 'skeptic_claims') {
          renderClaims(ev.claims);
        } else if (ev.type === 'skeptic_verdict') {
          const chip = chipByClaim[ev.id];
          if (chip) {
            const [label, cls] = CHIP[ev.verdict] || [ev.verdict, ''];
            chip.textContent = label;
            if (cls) chip.classList.add(cls);
          }
        } else if (ev.type === 'skeptic_done') {
          const p = document.createElement('div');
          p.className = 'skeptic-summary';
          p.textContent = Object.entries(ev.counts)
            .map(([k, v]) => `${v} ${k}`).join(' · ') || 'nothing to judge';
          skepticEl.appendChild(p);
        } else if (ev.type === 'skeptic_error') {
          const p = document.createElement('div');
          p.className = 'skeptic-summary error';
          p.textContent = 'skeptic pass failed: ' + ev.message;
          skepticEl.appendChild(p);
        }
```

Finally, after the stream loop ends, hide the panel if the skeptic never spoke (producer error path). After `if (!thinking) think.remove();` (line 174), add:

```js
  if (!skepticEl.childNodes.length) skepticEl.remove();
```

- [ ] **Step 4: Sanity-check the suite and the page**

Run: `python -m pytest -q`
Expected: 31 passed (no Python touched — regression only).
Then eyeball: `python -c "from pathlib import Path; t = Path('static/index.html').read_text(encoding='utf-8'); assert 'skeptic_claims' in t and 'claude-opus-4-8' not in t; print('ok')"`
Expected: `ok`

- [ ] **Step 5: Output the commit command for Hossain (do not run)**

```bash
cd ~/dev/tic-concept-chat
git add static/index.html
git commit -m "feat: skeptic verdict panel - per-claim chips; drop model literal from header"
```

---

### Task 6: README Stage B section + docstring hygiene + final gate

**Files:**
- Modify: `README.md` (three-lane flow, limitation, status tags)
- Modify: `context.py` (stale "three docs" docstrings — hygiene, separate commit)

**Interfaces:**
- Consumes: everything built in Tasks 1–5 (documents it; changes no behavior).
- Produces: the repo's honest public status. Status wording is fixed: **"built, tests green, effect unverified"** — never "working".

- [ ] **Step 1: Update README (same-change docs rule for Stage B)**

Add after the "Contract-anchored citations" section:

```markdown
## Producer≠judge skeptic pass

Each delivered reply is checked by two further lanes on the same SSE stream,
after `done` (the reply is fully delivered first, so skeptic latency or
failure never costs you the answer):

1. **discussion** (producer) — streams the reply, as before.
2. **worker** — mechanically extracts every claim with its citation status
   into a forced-tool JSON schema. No judgment.
3. **judgment** — issues a refutation verdict per claim from the claims JSON
   alone: cited → `supported`/`overreach`, uncited →
   `marked-inference`/`unmarked-inference`. Default is refutation.

The judgment lane sees **excerpts only** (claim text + `cited_text`), never
the full docs or the full reply — the smallest context goes to the most
expensive model. Known limitation, accepted by design: excerpt-only judging
can miss context-dependent overreach.

The producer's framing still asks it to be skeptical — kept as style, no
longer trusted as duty. Verification is mechanical, in lanes 2–3
(`skeptic.py`), so the discussion model is never its own citation-police.

> Status: built and unit-tested offline (mocked API); the live three-lane
> effect is **unverified** — no credentialed run has exercised it.
```

Update the mermaid diagram's model node and add the skeptic leg — replace the `O` node line and add edges:

```
    S -->|"prepends the 4 contracts as<br/>citation-enabled document blocks<br/>(first user turn, prompt-cached)"| O["discussion model<br/>(config/models.json)<br/>streaming"]
    O -->|"SSE deltas: thinking · text · citation · done"| B
    S -.->|"after done: worker extracts claims,<br/>judgment issues verdicts"| K["skeptic lanes<br/>worker + judgment"]
    K -.->|"SSE: skeptic_claims · skeptic_verdict<br/>· skeptic_done · skeptic_error"| B
```

Update the existing citations status blockquote's test count (`pytest` 9/9 → the current green count from Task 4, 31/31).

- [ ] **Step 2: Fix stale docstrings in `context.py` (found at pick-up; docs-honesty)**

Line 3–4: change `the three docs are attached` → `the four docs are attached`.
Line 54 (`build_document_blocks` docstring): change `The three docs as citation-enabled` → `The four docs as citation-enabled`.
Line 56–57: change `the framing + all three docs` → `the framing + all four docs`.

- [ ] **Step 3: Run the full suite (final gate)**

Run: `python -m pytest -q`
Expected: 31 passed — this is the plan's exit gate; do not report done on anything less.

- [ ] **Step 4: Output the commit commands for Hossain (do not run)**

```bash
cd ~/dev/tic-concept-chat
# Stage B docs land with the feature they describe (docs-honesty rule).
git add README.md
git commit -m "docs: three-lane skeptic pass - flow, excerpt-only limitation, status tags"
# Hygiene, separate concern: docstrings said three docs; DOC_NAMES has four.
git add context.py
git commit -m "docs: context.py docstrings - four docs, not three"
git push
```

---

## Self-Review (performed at write time)

- **Spec coverage:** config/models.json + flat mapping → Task 1; models.py resolve + import-time fail-loud → Tasks 1–2; server MODEL change → Task 2; check-tier-sync analog → Task 2; discussion-only-then-add rule (spec's "preferred" option) → Tasks 1/3; server-side same-stream placement after done → Task 4; lane 2 schema → Task 3 (EXTRACT_TOOL matches the spec's JSON shape, `citation: null` when uncited enforced by `validate_claims`); lane 3 excerpts-only + default-refuted + verdict enums → Task 3; four SSE events → Task 4; UI chips → Task 5; error handling (skeptic_error, never taints reply) → Task 4; testing boundary (offline, schema validation, event shapes, role-sync, inject_documents regression) → Tasks 1–4; README same-change obligations → Tasks 2 and 6; non-goals — nothing in any task adds a dispatcher, stakes rubric, or cache change.
- **Placeholder scan:** no TBDs; every code step carries the code.
- **Type consistency:** `extract_claims(client, content) -> dict` and `judge_claims(client, claims) -> list[dict]` identical in Task 3 (producer) and Task 4 (consumer); SSE payload keys in Task 4's tests match Task 5's JS handlers (`ev.claims`, `ev.id`, `ev.verdict`, `ev.counts`, `ev.message`); verdict enum strings identical across VERDICT_TOOL, validate_verdicts, server counts, and the UI CHIP map.
