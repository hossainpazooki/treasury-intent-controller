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
    except Exception as e:  # e.g. SDK TypeError when no credentials resolve
        yield sse({"type": "error", "message": f"{type(e).__name__}: {e}"})


@app.post("/chat")
def chat(req: ChatRequest) -> StreamingResponse:
    return StreamingResponse(stream_reply(req.messages), media_type="text/event-stream")


@app.get("/")
def index() -> FileResponse:
    return FileResponse(STATIC / "index.html")
