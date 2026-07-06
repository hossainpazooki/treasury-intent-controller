# Design: tic-concept-chat

*2026-07-05 — approved design for a local chat tool to discuss the concepts
underlying treasury-intent-controller with Claude Opus 4.8.*

## Purpose

A local web chat UI for conceptual design discussions about this repo's ideas —
the intent lifecycle state machine, tri-state fail-closed scoring,
idempotency-by-construction at the dispatch edge, stable-vs-volatile criteria,
and deterministic replay from a logical-clock event log — with Claude Opus 4.8
primed on the repo's authoritative documents.

The tool is an auxiliary; it lives **outside** this repo (which stays
stdlib-only Go) in a sibling directory. Only this design doc lives here.

## Decisions (locked)

| Decision | Choice |
|---|---|
| Interface | Local web chat UI (single page, localhost) |
| Model | `claude-opus-4-8`, streaming, adaptive thinking with `display: "summarized"` |
| Context | `README.md`, `CONTRACT.md`, `CONTRACT-V2.md` baked into a prompt-cached system prompt |
| Persistence | Session-only; export button downloads a markdown transcript |
| Stack / location | Python + FastAPI in `C:\Users\hossa\dev\tic-concept-chat\` |

## Layout

```
tic-concept-chat/
  server.py          # FastAPI app: serves the page + one streaming chat endpoint
  context.py         # loads the three docs and builds the system prompt
  static/index.html  # single-page chat UI (vanilla JS, rendered markdown, export)
  requirements.txt   # fastapi, uvicorn, anthropic
  README.md
  test_context.py    # pytest for context loading / prompt assembly
```

## Architecture

- **Stateless server.** The browser holds the full `messages` array and sends
  it with every `POST /chat`. The server prepends the system prompt and streams
  the reply back as SSE (`text/event-stream`). No server-side session state —
  "session-only" persistence falls out naturally.
- **System prompt** = a short framing paragraph ("You are discussing the design
  concepts underlying this treasury authorization gate…") followed by the three
  docs, read from `../treasury-intent-controller/` at server startup, with
  `cache_control: {"type": "ephemeral"}` on the **last** system block so
  repeat turns read the ~15–20K-token context from cache (~0.1× input price;
  well above Opus 4.8's 4096-token cacheable minimum). The framing text and doc
  order are fixed at startup — no per-request interpolation, so the prefix
  stays byte-identical across turns.
- **API call**: `client.messages.stream(...)` with `model="claude-opus-4-8"`,
  `max_tokens=64000`, `thinking={"type": "adaptive", "display": "summarized"}`.
  Thinking deltas stream to a collapsible "thinking…" section in the UI; text
  deltas stream as rendered markdown below it.
- **Auth**: bare `anthropic.Anthropic()` — resolves `ANTHROPIC_API_KEY` or an
  `ant auth login` profile. The tool never handles keys itself.
- **Export**: client-side; serializes the conversation to
  `tic-chat-YYYY-MM-DD-HHMM.md` and triggers a download. A "New conversation"
  button clears the client-held array.
- **Errors**: typed SDK exceptions caught most-specific-first
  (`RateLimitError`, `AuthenticationError`, `APIStatusError`,
  `APIConnectionError`) and mapped to an SSE `error` event rendered inline in
  the chat — no dead spinners.
- **Startup guard**: the server fails loudly at startup if any of the three
  source docs is missing, printing the resolved path it looked for.

## Run

```sh
pip install -r requirements.txt
uvicorn server:app --port 8765     # then open http://localhost:8765
```

Windows-native (no WSL). The browser renders Unicode, so the cp1252 console
caveat does not apply to chat output.

## Testing

Light, matching the tool's stakes:

- `test_context.py`: the three docs are found, the system prompt is non-empty,
  and exactly one `cache_control` breakpoint sits on the last system block.
- Manual smoke turn against the live API on first run (verify a streamed
  response and a cache write on turn 1 / cache read on turn 2 via `usage`).

## Non-goals

- No multi-conversation storage or server-side history.
- No repo file-browsing tools for the model (context is the three docs only).
- No auth/multi-user concerns beyond binding to localhost.
- No changes to treasury-intent-controller itself — contracts and the named
  gate are untouched.
