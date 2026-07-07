# tic-concept-chat Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A local web chat UI (FastAPI + single HTML page) for discussing treasury-intent-controller's design concepts with Claude Opus 4.8, primed on the repo's README and contracts.

**Architecture:** Stateless FastAPI server — the browser holds the full conversation and POSTs it each turn; the server prepends a prompt-cached system prompt built from `README.md` + `CONTRACT.md` + `CONTRACT-DURABILITY.md` and streams the Opus reply back as SSE. Session-only persistence with a client-side markdown export.

**Tech Stack:** Python 3.11+, FastAPI, uvicorn, `anthropic` SDK, vanilla JS + marked.js (CDN) for the page.

**Spec:** `treasury-intent-controller/docs/2026-07-05-tic-concept-chat-design.md`

## Global Constraints

- Tool lives at `C:\Users\hossa\dev\tic-concept-chat\` — **nothing** is written into `treasury-intent-controller/` except this plan and its spec (already present).
- Model is exactly `claude-opus-4-8`; `max_tokens=64000`; `thinking={"type": "adaptive", "display": "summarized"}`; streaming only.
- System prompt: framing paragraph + the three docs in order README → CONTRACT → CONTRACT-DURABILITY, `cache_control: {"type": "ephemeral"}` on the **last** block only. No per-request interpolation anywhere in the prefix.
- Auth: bare `anthropic.Anthropic()` — never read or store keys in this tool. Construct the client lazily (not at import) so tests import cleanly without credentials.
- **Git: NEVER run `git commit`/`git push`.** At each commit checkpoint, output the exact command for Hossain to run and continue working. No attribution trailers in suggested messages.
- Windows-local: keep any console `print()` ASCII; all files written UTF-8.

---

### Task 1: Scaffold + context.py (system-prompt builder)

**Files:**
- Create: `C:\Users\hossa\dev\tic-concept-chat\requirements.txt`
- Create: `C:\Users\hossa\dev\tic-concept-chat\context.py`
- Test: `C:\Users\hossa\dev\tic-concept-chat\test_context.py`

**Interfaces:**
- Consumes: `../treasury-intent-controller/{README.md,CONTRACT.md,CONTRACT-DURABILITY.md}` on disk.
- Produces: `context.DOC_NAMES: tuple[str, ...]`, `context.load_docs(base: Path | None = None) -> dict[str, str]` (raises `FileNotFoundError` naming the missing path), `context.build_system_prompt(base: Path | None = None) -> list[dict]` (Anthropic system-block list, cache breakpoint on last block). Task 2 imports `build_system_prompt`.

- [ ] **Step 1: Write requirements.txt**

```
fastapi
uvicorn
anthropic
# dev/test
pytest
```

- [ ] **Step 2: Write the failing tests**

```python
# test_context.py
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
```

- [ ] **Step 3: Run tests to verify they fail**

Run (from `C:\Users\hossa\dev\tic-concept-chat`): `python -m pytest test_context.py -v`
Expected: FAIL / ERROR with `ModuleNotFoundError: No module named 'context'`

- [ ] **Step 4: Write context.py**

```python
"""Builds the Opus system prompt from treasury-intent-controller's authoritative docs."""
from __future__ import annotations

import os
from pathlib import Path

DOC_NAMES = ("README.md", "CONTRACT.md", "CONTRACT-DURABILITY.md")

FRAMING = """\
You are a design discussion partner for the concepts underlying
treasury-intent-controller ("tic"), the authorization plane of the ATLAS
Treasury intent-gated action loop. The three authoritative documents follow
(README, CONTRACT, CONTRACT-DURABILITY; where the contracts disagree, CONTRACT-DURABILITY wins).

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
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `python -m pytest test_context.py -v`
Expected: 3 passed

- [ ] **Step 6: Commit checkpoint (output only — do not run)**

```bash
git -C C:/Users/hossa/dev add tic-concept-chat/requirements.txt tic-concept-chat/context.py tic-concept-chat/test_context.py
git -C C:/Users/hossa/dev commit -m "feat: tic-concept-chat context builder"
```

---

### Task 2: server.py (FastAPI + SSE streaming endpoint)

**Files:**
- Create: `C:\Users\hossa\dev\tic-concept-chat\server.py`
- Test: `C:\Users\hossa\dev\tic-concept-chat\test_server.py`

**Interfaces:**
- Consumes: `context.build_system_prompt()` from Task 1.
- Produces: FastAPI `app` with `GET /` (serves `static/index.html`) and `POST /chat` (body `{"messages": [{"role": ..., "content": ...}, ...]}` → SSE stream of `data: {json}\n\n` payloads with `type` ∈ `thinking | text | done | error`). `sse(payload: dict) -> str` helper. Task 3's page consumes exactly this wire format.

- [ ] **Step 1: Write the failing tests**

```python
# test_server.py
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `python -m pytest test_server.py -v`
Expected: ERROR with `ModuleNotFoundError: No module named 'server'`

- [ ] **Step 3: Write server.py**

```python
"""FastAPI shell for tic-concept-chat: serves the page + one streaming endpoint."""
from __future__ import annotations

import functools
import json
from pathlib import Path
from typing import Iterator

import anthropic
from fastapi import FastAPI
from fastapi.responses import FileResponse, StreamingResponse
from pydantic import BaseModel

from context import build_system_prompt

MODEL = "claude-opus-4-8"
MAX_TOKENS = 64000
STATIC = Path(__file__).resolve().parent / "static"

# Built at import so a missing doc fails the server at startup, loudly.
SYSTEM = build_system_prompt()

app = FastAPI()


@functools.lru_cache(maxsize=1)
def get_client() -> anthropic.Anthropic:
    # Lazy: resolves ANTHROPIC_API_KEY or an `ant auth login` profile at first use.
    return anthropic.Anthropic()


class ChatRequest(BaseModel):
    messages: list[dict]


def sse(payload: dict) -> str:
    return f"data: {json.dumps(payload)}\n\n"


def stream_reply(messages: list[dict]) -> Iterator[str]:
    try:
        with get_client().messages.stream(
            model=MODEL,
            max_tokens=MAX_TOKENS,
            system=SYSTEM,
            thinking={"type": "adaptive", "display": "summarized"},
            messages=messages,
        ) as stream:
            for event in stream:
                if event.type == "content_block_delta":
                    if event.delta.type == "thinking_delta":
                        yield sse({"type": "thinking", "text": event.delta.thinking})
                    elif event.delta.type == "text_delta":
                        yield sse({"type": "text", "text": event.delta.text})
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


@app.post("/chat")
def chat(req: ChatRequest) -> StreamingResponse:
    return StreamingResponse(stream_reply(req.messages), media_type="text/event-stream")


@app.get("/")
def index() -> FileResponse:
    return FileResponse(STATIC / "index.html")
```

- [ ] **Step 4: Run all tests to verify they pass**

Run: `python -m pytest -v`
Expected: 5 passed (3 from Task 1 + 2 new)

- [ ] **Step 5: Commit checkpoint (output only — do not run)**

```bash
git -C C:/Users/hossa/dev add tic-concept-chat/server.py tic-concept-chat/test_server.py
git -C C:/Users/hossa/dev commit -m "feat: tic-concept-chat streaming server"
```

---

### Task 3: static/index.html (chat page)

**Files:**
- Create: `C:\Users\hossa\dev\tic-concept-chat\static\index.html`

**Interfaces:**
- Consumes: `POST /chat` SSE wire format from Task 2 (`data: {json}\n\n`, `type` ∈ `thinking | text | done | error`).
- Produces: the complete UI — no later task depends on its internals.

No automated test (per spec: light testing). Manual verification in Task 4.

- [ ] **Step 1: Write static/index.html**

```html
<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>tic concept chat</title>
<script src="https://cdn.jsdelivr.net/npm/marked/marked.min.js"></script>
<style>
  :root { --bg:#111418; --panel:#1a1f26; --fg:#e6e8eb; --dim:#8b949e; --accent:#f59e0b; }
  * { box-sizing: border-box; }
  body { margin:0; height:100vh; display:flex; flex-direction:column;
         background:var(--bg); color:var(--fg);
         font:15px/1.55 system-ui, "Segoe UI", sans-serif; }
  header { display:flex; justify-content:space-between; align-items:center;
           padding:10px 16px; background:var(--panel); border-bottom:1px solid #2b3138; }
  header h1 { font-size:15px; margin:0; }
  header h1 small { color:var(--dim); font-weight:normal; margin-left:8px; }
  header button { margin-left:8px; }
  button { background:#2b3138; color:var(--fg); border:1px solid #3a4149;
           border-radius:6px; padding:6px 12px; cursor:pointer; }
  button:hover { border-color:var(--accent); }
  button:disabled { opacity:.5; cursor:default; }
  main { flex:1; overflow-y:auto; padding:16px; }
  .msg { max-width:860px; margin:0 auto 14px; padding:10px 14px; border-radius:8px; }
  .msg.user { background:#22303c; white-space:pre-wrap; }
  .msg.assistant { background:var(--panel); }
  .msg pre { background:#0d1117; padding:10px; border-radius:6px; overflow-x:auto; }
  .msg code { font:13px/1.5 Consolas, monospace; }
  details { margin-bottom:8px; color:var(--dim); }
  details pre { white-space:pre-wrap; font-size:13px; }
  .usage { color:var(--dim); font-size:12px; margin-top:6px; }
  .error { color:#f85149; }
  footer { display:flex; gap:8px; padding:12px 16px; background:var(--panel);
           border-top:1px solid #2b3138; }
  textarea { flex:1; resize:none; background:#0d1117; color:var(--fg);
             border:1px solid #3a4149; border-radius:6px; padding:8px; font:inherit; }
</style>
</head>
<body>
<header>
  <h1>tic concept chat <small>claude-opus-4-8 · README + CONTRACT + CONTRACT-DURABILITY</small></h1>
  <div>
    <button id="export">Export .md</button>
    <button id="reset">New conversation</button>
  </div>
</header>
<main id="log"></main>
<footer>
  <textarea id="input" rows="3"
    placeholder="Ask about the gate's concepts... (Ctrl+Enter to send)"></textarea>
  <button id="send">Send</button>
</footer>
<script>
const log = document.getElementById('log');
const input = document.getElementById('input');
const sendBtn = document.getElementById('send');
let messages = [];
let busy = false;

function addBubble(role) {
  const div = document.createElement('div');
  div.className = 'msg ' + role;
  log.appendChild(div);
  return div;
}

async function send() {
  const text = input.value.trim();
  if (!text || busy) return;
  busy = true; sendBtn.disabled = true;
  input.value = '';
  messages.push({role: 'user', content: text});
  addBubble('user').textContent = text;

  const bubble = addBubble('assistant');
  const think = document.createElement('details');
  think.innerHTML = '<summary>thinking…</summary><pre></pre>';
  const body = document.createElement('div');
  bubble.append(think, body);
  let answer = '', thinking = '';

  try {
    const resp = await fetch('/chat', {
      method: 'POST',
      headers: {'Content-Type': 'application/json'},
      body: JSON.stringify({messages}),
    });
    if (!resp.ok) throw new Error('HTTP ' + resp.status);
    const reader = resp.body.getReader();
    const dec = new TextDecoder();
    let buf = '';
    for (;;) {
      const {done, value} = await reader.read();
      if (done) break;
      buf += dec.decode(value, {stream: true});
      const parts = buf.split('\n\n');
      buf = parts.pop();
      for (const part of parts) {
        const line = part.split('\n').find(l => l.startsWith('data: '));
        if (!line) continue;
        const ev = JSON.parse(line.slice(6));
        if (ev.type === 'thinking') {
          thinking += ev.text;
          think.querySelector('pre').textContent = thinking;
        } else if (ev.type === 'text') {
          answer += ev.text;
          body.innerHTML = marked.parse(answer);
        } else if (ev.type === 'error') {
          const p = document.createElement('p');
          p.className = 'error';
          p.textContent = ev.message;
          bubble.appendChild(p);
        } else if (ev.type === 'done') {
          const u = ev.usage;
          const p = document.createElement('p');
          p.className = 'usage';
          p.textContent = `in ${u.input_tokens} / out ${u.output_tokens} / ` +
            `cache write ${u.cache_creation_input_tokens} / cache read ${u.cache_read_input_tokens}`;
          bubble.appendChild(p);
        }
      }
      log.scrollTop = log.scrollHeight;
    }
  } catch (e) {
    const p = document.createElement('p');
    p.className = 'error';
    p.textContent = 'stream failed: ' + e;
    bubble.appendChild(p);
  }
  if (!thinking) think.remove();
  if (answer) messages.push({role: 'assistant', content: answer});
  busy = false; sendBtn.disabled = false;
  input.focus();
}

sendBtn.addEventListener('click', send);
input.addEventListener('keydown', e => {
  if (e.key === 'Enter' && e.ctrlKey) { e.preventDefault(); send(); }
});

document.getElementById('reset').addEventListener('click', () => {
  messages = [];
  log.innerHTML = '';
  input.focus();
});

document.getElementById('export').addEventListener('click', () => {
  if (!messages.length) return;
  const md = messages.map(m =>
    (m.role === 'user' ? '## You\n\n' : '## Opus\n\n') + m.content
  ).join('\n\n---\n\n');
  const stamp = new Date().toISOString().slice(0, 16).replace('T', '-').replace(':', '');
  const a = document.createElement('a');
  a.href = URL.createObjectURL(new Blob([md], {type: 'text/markdown'}));
  a.download = `tic-chat-${stamp}.md`;
  a.click();
  URL.revokeObjectURL(a.href);
});

input.focus();
</script>
</body>
</html>
```

- [ ] **Step 2: Commit checkpoint (output only — do not run)**

```bash
git -C C:/Users/hossa/dev add tic-concept-chat/static/index.html
git -C C:/Users/hossa/dev commit -m "feat: tic-concept-chat web UI"
```

---

### Task 4: README + live smoke verification

**Files:**
- Create: `C:\Users\hossa\dev\tic-concept-chat\README.md`

**Interfaces:**
- Consumes: everything above.
- Produces: verified working tool.

- [ ] **Step 1: Write README.md**

```markdown
# tic-concept-chat

Local web chat for discussing treasury-intent-controller's design concepts
(intent lifecycle, tri-state fail-closed scoring, idempotency by
construction, stable/volatile criteria, deterministic replay) with Claude
Opus 4.8, primed on the repo's README + CONTRACT.md + CONTRACT-DURABILITY.md.

## Run

    pip install -r requirements.txt
    uvicorn server:app --port 8765

Open http://localhost:8765. Auth resolves from `ANTHROPIC_API_KEY` or an
`ant auth login` profile - the tool never stores keys.

## Notes

- Stateless server: the browser holds the conversation; refresh loses it.
  Use **Export .md** to save a transcript.
- The system prompt is prompt-cached; the usage line under each reply shows
  cache write/read tokens (expect a write on turn 1, reads after).
- Docs are read from `../treasury-intent-controller` at startup (override
  with `TIC_DIR`); the server refuses to start if any doc is missing.
- Design spec: `../treasury-intent-controller/docs/2026-07-05-tic-concept-chat-design.md`

## Test

    python -m pytest -v
```

- [ ] **Step 2: Run the full test suite**

Run (from `C:\Users\hossa\dev\tic-concept-chat`): `python -m pytest -v`
Expected: 5 passed

- [ ] **Step 3: Start the server in the background**

Run: `uvicorn server:app --port 8765` (background)
Expected: startup log line `Uvicorn running on http://127.0.0.1:8765`; no `FileNotFoundError`.

- [ ] **Step 4: Live smoke turn via curl**

```bash
curl -s -N http://localhost:8765/chat \
  -H "Content-Type: application/json" \
  -d '{"messages":[{"role":"user","content":"In one sentence: why does an absent idempotency key fail closed?"}]}'
```

Expected: a stream of `data: {"type": ...}` lines ending in a `done` payload whose usage shows `cache_creation_input_tokens > 0` (first turn writes the cache). Run it a second time and expect `cache_read_input_tokens > 0`.

Also: `curl -s http://localhost:8765/ | head -5` returns the HTML page.

- [ ] **Step 5: Stop the background server**

- [ ] **Step 6: Commit checkpoint (output only — do not run)**

```bash
git -C C:/Users/hossa/dev add tic-concept-chat/README.md
git -C C:/Users/hossa/dev commit -m "docs: tic-concept-chat README"
```
