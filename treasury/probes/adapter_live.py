"""Quickstart probe 8: the LangChain adapter LIVE (CONTRACT.md 2.7,
framework adapter; SDK claim 16).

A real LangChain tool wrapped by gate_tool runs against the live gate:
invoke 1 must execute the tool body EXACTLY ONCE on a live synchronous
Proceed; invoke 2 with the SAME args (same derived key) must raise
IntentRefused classified ALREADY_RESERVED with the tool body NOT re-fired.
The call counter is the load-bearing observable -- "refused" means the
consequence never happened, live, not only in a lab double.

Usage: adapter_live.py <gate_url> <spec_hash>. Exit 0 iff both legs match.
ASCII output only (cp1252 console). Requires langchain-core (the quickstart
bootstraps it into the venv one-time).
"""

import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[2] / "declarant" / "pydeclarant"))

from langchain_core.tools import tool as lc_tool  # noqa: E402

from client import Client  # noqa: E402
from declare import ALREADY_RESERVED  # noqa: E402
from langchain_adapter import IntentRefused, gate_tool  # noqa: E402

CALLS = {"n": 0}


@lc_tool
def sample_transfer(amount: str, unit: str) -> str:
    """Move an amount of a unit (probe tool body)."""
    CALLS["n"] += 1
    return "moved %s %s" % (amount, unit)


def main() -> int:
    gate_url, spec_hash = sys.argv[1], sys.argv[2]
    gated = gate_tool(
        sample_transfer,
        Client(gate_url, timeout=10.0),
        intent_spec_hash=spec_hash,
        scope="per-actor",
        run_id="quickstart-adapter",
    )

    out = gated.invoke({"amount": "25", "unit": "alpha"})
    print("adapter invoke 1: executed=%d result=%s" % (CALLS["n"], out))
    ok1 = CALLS["n"] == 1 and out == "moved 25 alpha"

    try:
        gated.invoke({"amount": "25", "unit": "alpha"})
        print("adapter invoke 2: UNEXPECTED EXECUTION (executed=%d)" % CALLS["n"])
        ok2 = False
    except IntentRefused as e:
        print(
            "adapter invoke 2: refused class=%s same_key_retry_safe=%s executed=%d"
            % (e.class_, str(e.same_key_retry_safe).lower(), CALLS["n"])
        )
        ok2 = (e.class_ == ALREADY_RESERVED
               and e.same_key_retry_safe is False and CALLS["n"] == 1)

    return 0 if (ok1 and ok2) else 1


if __name__ == "__main__":
    sys.exit(main())
