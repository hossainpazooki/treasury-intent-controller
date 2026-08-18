"""Quickstart probe 7: the Python declarant twin LIVE (CONTRACT.md 2.7).

Declares against the real gate through pydeclarant's stdlib Client -- the
twin's first live leg. Two steps under the twin's OWN derived key (distinct
from probe 6's Go-SDK key): a fresh declare must classify PROCEED with a
synchronous ACHIEVED terminal; a same-key re-declare (fresh episode) must
classify ALREADY_RESERVED with same_key_retry_safe false.

Usage: pydeclarant_live.py <gate_url> <spec_hash>. Exit 0 iff both steps
match. ASCII output only (cp1252 console).
"""

import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[2] / "declarant" / "pydeclarant"))

from client import Client  # noqa: E402
from declare import ALREADY_RESERVED, PROCEED, Request, derive_key  # noqa: E402


def main() -> int:
    gate_url, spec_hash = sys.argv[1], sys.argv[2]
    key = derive_key(
        "per-actor", "quickstart-pytwin", "sample.transfer",
        b'{"amount":"25","unit":"alpha"}',
    )
    c = Client(gate_url, timeout=10.0)

    r1 = c.declare(Request(
        episode_seed="quickstart-pytwin",
        idempotency_key=key,
        intent_spec_hash=spec_hash,
        idempotency_scope="per-actor",
    ))
    t1 = (r1.terminal or {}).get("terminal", "")
    print("pytwin declare 1: class=%s terminal=%s" % (r1.class_, t1))

    r2 = c.declare(Request(
        episode_seed="quickstart-pytwin-2",
        idempotency_key=key,
        intent_spec_hash=spec_hash,
        idempotency_scope="per-actor",
    ))
    print("pytwin declare 2: class=%s same_key_retry_safe=%s"
          % (r2.class_, str(r2.same_key_retry_safe).lower()))

    ok = (r1.class_ == PROCEED and t1 == "ACHIEVED"
          and r2.class_ == ALREADY_RESERVED and r2.same_key_retry_safe is False)
    return 0 if ok else 1


if __name__ == "__main__":
    sys.exit(main())
