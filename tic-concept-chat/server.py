"""FastAPI shell for tic-concept-chat: serves the page + one streaming endpoint.

The docs are attached as citation-enabled `document` blocks, so the SSE stream
carries `citation` events (each an anchor into README/CONTRACT/CONTRACT-V2)
alongside `thinking` and `text`.
"""
from __future__ import annotations

import functools
import json
from pathlib import Path
from typing import Iterator

import anthropic
from fastapi import FastAPI
from fastapi.responses import FileResponse, StreamingResponse
from pydantic import BaseModel

import context
import models
import skeptic

# Resolved at import so a missing/malformed config or unknown role fails the
# server at startup, loudly - same posture as the doc loading below.
MODEL = models.resolve("discussion")
MAX_TOKENS = 64000
STATIC = Path(__file__).resolve().parent / "static"

# Built at import so a missing doc fails the server at startup, loudly.
SYSTEM = context.FRAMING
DOCUMENTS = context.build_document_blocks()
DOCS = context.load_docs()  # raw doc text for the window-mode judge

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
        verdicts = skeptic.judge_claims(
            get_client(),
            claims,
            mode=skeptic.JUDGMENT_CONTEXT,
            reply_text=skeptic.render_reply(final_content),
            docs=DOCS,
        )
        counts: dict[str, int] = {}
        for v in verdicts:
            counts[v["verdict"]] = counts.get(v["verdict"], 0) + 1
            yield sse({"type": "skeptic_verdict", "id": v["id"], "verdict": v["verdict"]})
        yield sse({"type": "skeptic_done", "counts": counts})
    except Exception as e:
        yield sse({"type": "skeptic_error", "message": f"{type(e).__name__}: {e}"})


@app.post("/chat")
def chat(req: ChatRequest) -> StreamingResponse:
    return StreamingResponse(stream_reply(req.messages), media_type="text/event-stream")


@app.get("/")
def index() -> FileResponse:
    return FileResponse(STATIC / "index.html")
