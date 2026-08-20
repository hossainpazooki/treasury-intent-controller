"""Quickstart probe 9: the MCP gate middleware LIVE (CONTRACT.md 2.7,
"MCP gate").

An in-process FastMCP server whose one counting tool is gated by
IntentGateMiddleware pointed at the LIVE gate. Three legs:

  1. call 1 executes the tool body EXACTLY ONCE on a live synchronous
     Proceed and the result comes back;
  2. call 2 with the SAME arguments raises ToolError classified
     ALREADY_RESERVED, with the counter still 1 - the consequence did not
     fire twice;
  3. a SECOND, independent middleware on its OWN server with its OWN
     counter - same scope, run_id and spec hash, sharing no state with the
     first - is ALSO refused ALREADY_RESERVED and its counter stays 0.

Leg 3 is the stateless / multi-replica claim proven live: the idempotency
key is DERIVED, not remembered, so a retry landing on any replica is
refused by the gate rather than by local memory. The counters are the
load-bearing observable - "refused" means the consequence never happened.

Usage: mcp_gate_live.py <gate_url> <spec_hash>. Exit 0 iff all three legs
match. ASCII output only (cp1252 console). Requires fastmcp (the quickstart
bootstraps it into the venv one-time).
"""

import asyncio
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[2] / "declarant" / "pydeclarant"))

from fastmcp import Client as MCPClient  # noqa: E402
from fastmcp import FastMCP  # noqa: E402
from fastmcp.exceptions import ToolError  # noqa: E402

from client import Client  # noqa: E402
from mcp_adapter import IntentGateMiddleware  # noqa: E402

SCOPE = "per-actor"
RUN_ID = "quickstart-mcp"
ARGS = {"amount": "25", "unit": "alpha"}


class Counter:
    """Execution counter for one server instance."""

    def __init__(self):
        self.n = 0


def build_server(gate_url, spec_hash):
    """One gated server + its own counter. Called twice to build two
    independent replicas sharing NO state - separate FastMCP instance,
    separate middleware instance, separate gate client, separate counter.

    The tool takes only REQUIRED parameters, so what a caller spells is the
    whole effective action: this probe exercises the gate, not the
    strict_args refusal (probe 10 covers that one).
    """
    counter = Counter()
    server = FastMCP("quickstart-mcp")

    @server.tool
    def sample_transfer(amount: str, unit: str) -> str:
        """Move an amount of a unit (probe tool body)."""
        counter.n += 1
        return "moved %s %s" % (amount, unit)

    server.add_middleware(
        IntentGateMiddleware(
            Client(gate_url, timeout=10.0),
            intent_spec_hash=spec_hash,
            scope=SCOPE,
            run_id=RUN_ID,
        )
    )
    return server, counter


async def call(server, args):
    async with MCPClient(server) as c:
        return await c.call_tool("sample_transfer", args)


def report_refusal(label, exc, count_label, count):
    """Print one ASCII line for a refusal and say whether it was the
    refusal we required. The CLASS is asserted, not merely the exception
    type: every refusal in the adapter raises ToolError, so the type alone
    would pass on an unrelated failure and still look like the gate
    working."""
    msg = str(exc).replace("\n", " ")
    if "class=ALREADY_RESERVED" in msg:
        print("%s: refused class=ALREADY_RESERVED %s=%d" % (label, count_label, count))
        return True
    print("%s: refused UNEXPECTED %s=%d msg=%s" % (label, count_label, count, msg))
    return False


async def run(gate_url, spec_hash):
    server, counter = build_server(gate_url, spec_hash)

    out = await call(server, ARGS)
    result = getattr(out, "data", None)
    print("mcp call 1: executed=%d result=%s" % (counter.n, result))
    ok1 = counter.n == 1 and result == "moved 25 alpha"

    try:
        await call(server, ARGS)
        print("mcp call 2: UNEXPECTED EXECUTION executed=%d" % counter.n)
        ok2 = False
    except ToolError as exc:
        ok2 = report_refusal("mcp call 2", exc, "executed", counter.n) and counter.n == 1

    # Leg 3: a second replica that shares no state with the first.
    replica, replica_counter = build_server(gate_url, spec_hash)
    try:
        await call(replica, ARGS)
        print("mcp replica: UNEXPECTED EXECUTION replica_executed=%d" % replica_counter.n)
        ok3 = False
    except ToolError as exc:
        ok3 = (report_refusal("mcp replica", exc, "replica_executed", replica_counter.n)
               and replica_counter.n == 0)

    return 0 if (ok1 and ok2 and ok3) else 1


def main() -> int:
    gate_url, spec_hash = sys.argv[1], sys.argv[2]
    return asyncio.run(run(gate_url, spec_hash))


if __name__ == "__main__":
    sys.exit(main())
