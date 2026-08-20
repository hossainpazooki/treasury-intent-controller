"""Quickstart probe 10: the gated MCP proxy LIVE (CONTRACT.md 2.7,
"MCP gate", gated_proxy).

An UNGATED inner FastMCP server - standing in for a server the operator
does NOT own - is fronted by gated_proxy pointed at the LIVE gate, and
driven through fastmcp.Client. The run id is DISTINCT from probe 9's, so
this probe derives its own idempotency key and gets its own authorization
rather than colliding with probe 9's reservation.

Two legs, plus one extra assertion:

  1. the first call passes through and the INNER counter is 1;
  2. the same-argument second call is refused ALREADY_RESERVED with the
     INNER counter still 1;
  extra. a call omitting a non-required property whose schema declares no
     default is refused BEFORE anything is declared (strict_args, on by
     default) - a remote schema cannot tell us that property's effective
     value, and keying it as spelled would fork the idempotency key.

The INNER counter is the load-bearing observable: it is what proves the
gate stopped the call before the server the operator does not own ever saw
it. This is the Tier 2 path by construction - a ProxyTool exposes a JSON
Schema and no callable.

Usage: mcp_proxy_live.py <gate_url> <spec_hash>. Exit 0 iff every leg
matches. ASCII output only (cp1252 console). Requires fastmcp (the
quickstart bootstraps it into the venv one-time).
"""

import asyncio
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[2] / "declarant" / "pydeclarant"))

import pydantic  # noqa: E402
from fastmcp import Client as MCPClient  # noqa: E402
from fastmcp import FastMCP  # noqa: E402
from fastmcp.exceptions import ToolError  # noqa: E402

from client import Client  # noqa: E402
from mcp_adapter import gated_proxy  # noqa: E402

SCOPE = "per-actor"
RUN_ID = "quickstart-mcp-proxy"
ARGS = {"amount": "25", "unit": "alpha"}


class Counter:
    """Execution counter for the INNER (ungated, un-owned) server."""

    def __init__(self):
        self.n = 0
        self.tagged = 0


def build_backend():
    """The server the operator does not own: ungated, and the only place a
    consequence can fire."""
    counter = Counter()
    inner = FastMCP("quickstart-mcp-backend")

    @inner.tool
    def proxied_transfer(amount: str, unit: str) -> str:
        """Move an amount of a unit (backend tool body). Only REQUIRED
        parameters, so the gate is what this probe exercises."""
        counter.n += 1
        return "moved %s %s" % (amount, unit)

    @inner.tool
    def tagged_transfer(amount: str,
                        tags: list = pydantic.Field(default_factory=list)) -> str:
        """A backend tool whose JSON Schema declares NO default for `tags`
        (pydantic emits none for a default_factory). Its effective value is
        unknowable from the proxy side, which is exactly what strict_args
        refuses on."""
        counter.tagged += 1
        return "tagged %s" % amount

    return inner, counter


async def call(server, name, args):
    async with MCPClient(server) as c:
        return await c.call_tool(name, args)


def report_refusal(label, exc, want, count_label, count):
    """Print one ASCII line for a refusal and say whether it was the
    refusal we required. The reason is asserted, not just the exception
    type: every refusal here raises ToolError, so the type alone would pass
    on an unrelated failure and still look like the gate working."""
    msg = str(exc).replace("\n", " ")
    if want in msg:
        print("%s: refused %s %s=%d" % (label, want, count_label, count))
        return True
    print("%s: refused UNEXPECTED %s=%d msg=%s" % (label, count_label, count, msg))
    return False


async def run(gate_url, spec_hash):
    inner, counter = build_backend()
    proxy = gated_proxy(
        inner,
        Client(gate_url, timeout=10.0),
        intent_spec_hash=spec_hash,
        scope=SCOPE,
        run_id=RUN_ID,
    )

    out = await call(proxy, "proxied_transfer", ARGS)
    result = getattr(out, "data", None)
    print("mcp proxy call 1: inner_executed=%d result=%s" % (counter.n, result))
    ok1 = counter.n == 1 and result == "moved 25 alpha"

    try:
        await call(proxy, "proxied_transfer", ARGS)
        print("mcp proxy call 2: UNEXPECTED EXECUTION inner_executed=%d" % counter.n)
        ok2 = False
    except ToolError as exc:
        ok2 = (report_refusal("mcp proxy call 2", exc, "class=ALREADY_RESERVED",
                              "inner_executed", counter.n)
               and counter.n == 1)

    # Extra assertion (not a ladder entry): the strict_args refusal, which
    # happens BEFORE any declaration - so it adds no ACHIEVED record.
    try:
        await call(proxy, "tagged_transfer", {"amount": "25"})
        print("mcp proxy strict: UNEXPECTED EXECUTION inner_tagged=%d" % counter.tagged)
        ok3 = False
    except ToolError as exc:
        ok3 = (report_refusal("mcp proxy strict", exc, "schema declares no default",
                              "inner_tagged", counter.tagged)
               and counter.tagged == 0)

    return 0 if (ok1 and ok2 and ok3) else 1


def main() -> int:
    gate_url, spec_hash = sys.argv[1], sys.argv[2]
    return asyncio.run(run(gate_url, spec_hash))


if __name__ == "__main__":
    sys.exit(main())
