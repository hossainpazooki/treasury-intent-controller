# The adapter's first live run: the verifier refuted seed reuse the lab double could not see

ts: 2026-08-18T22:30:00Z
commit: intent-plane main (post-`0207a15` working tree) + this monorepo's working tree
session: 78bbff6c-2e2c-4e7f-b4ee-9fa22af6ee36
status: verified
fact: The LangChain adapter's first live quickstart run (new probe 8) went
11/12 — the adapter probe itself PASSED, but the final verifier-recompute
probe REFUTED the whole live feed: `intent-seq-dup:0`. Root cause: the
adapter derived `episode_seed` from the idempotency key ALONE (a deliberate
choice, made to fix an earlier fail-open where a wrap-time constant seed let
the 500-edge consult authorize one call with another call's terminal). A
same-args retry therefore redeclared the SAME intent id, the gate appended a
second lifecycle under it starting again at `intent_seq` 0, and the verifier
correctly refused sequence contiguity. §2.7's own reference probes had the
discipline all along: same-key retries are same-key/FRESH-EPISODE (probe 6
passes two different `-seed` values on purpose).

Why every lab gate was green while this shipped: the adapter's test double
scripts its own feed bodies and never enforces per-intent sequence
contiguity — only the real gate plus the real verifier compose into the
check that fires. The 30-test lane, the seven pins, and two skeptic passes
(which probed the fail-closed and key-dedup claims, not the record trail the
declarations leave) all passed over the defect. A component can satisfy its
own contract surface and still corrupt an invariant that only exists at the
system level; the live ladder is the only gate positioned to see it.

Fix (CONTRACT amended first, both repos): the episode seed is now the
idempotency key PLUS a fresh per-invocation uuid nonce — same-args
invocations share a key, never a seed, so no intent id is ever redeclared;
the 500-edge consult still reads only the calling invocation's own intent.
Regression pinned in the lane (the wire-bytes test rejects a bare-key seed;
the determinism test asserts same key/different seeds/different intent ids),
and the live ladder re-run green: 12/12 BOTH OS lanes, verifier VERIFIED
over all 13 intents.

re-verify: `cd ~/dev/treasury-intent-controller && powershell -File treasury\quickstart.ps1` — final lines `RESULT: VERIFIED intents=13 ...` and `RESULT: 12/12 probes passed`; and `git grep -n "uuid.uuid4" declarant/pydeclarant/langchain_adapter.py` — the nonce is present.
