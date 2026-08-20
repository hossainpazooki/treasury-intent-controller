# A forked idempotency key is FAIL-OPEN; only a collision is fail-closed — I wrote the spec backwards

ts: 2026-08-20T15:00:00Z
commit: intent-plane d9041f5 + uncommitted working tree (the MCP gate build; approximate ts — no clock was read at capture, but the basis landed between the adapter build going green and the CONTRACT correction, and it is the same session as the entries below)
session: 52e04ce2-ba8f-439f-b44b-3b05d69cdd88
status: verified
fact: An idempotency gate deduplicates BY KEY. Therefore a **fork** — one
logical action canonicalizing to two different keys — is FAIL-OPEN: the second
key has no reservation to collide with, the gate authorizes it, and the
consequence fires twice. A **collision** — two distinct actions sharing one key
— is the fail-closed direction: the second action is refused permanently
(reservations never expire). I authored the opposite into CONTRACT §2.7, where
it excused a known un-normalized-nested-default residual as "yields a duplicate
declaration (refused) and never a double execution, so the residual errs
fail-closed". A fork is precisely the case where no duplicate declaration can
occur. That inverted sentence passed a code review, was copy-pasted into the
module docstring, and was the stated justification for shipping the residual.
The same document argued two paragraphs earlier that top-level default
injection was necessary BECAUSE one logical call getting two keys is a bug — if
forked keys were self-refusing, that injection would have been unnecessary. The
contradiction was sitting in one section and nobody read the two halves against
each other.
basis: Controller probe against a live fastmcp 3.4.7 server, tool body receiving IDENTICAL values in every pair: "A nested object: {} vs fully spelled -> FORK (fail-OPEN); B top-level default_factory: absent vs [] -> FORK; C numeric form: 500 vs 500.0 -> FORK; D nested partial -> FORK; E Optional=None absent vs explicit null -> same key (ok)" / "FORKS CONFIRMED: 4 of 5 probed shapes". An independent pass with a key-aware gate double measured the direction directly: "distinct keys reserved: 2 | TOOL BODY EXECUTIONS: 2" against a control of "distinct keys reserved: 1 | TOOL BODY EXECUTIONS: 1".
re-verify: `git -C ~/dev/intent-plane grep -n "Withdrawn (2026-08-20)" CONTRACT.md` — the retraction is in the spec, quoting the withdrawn claim verbatim rather than deleting it.
lesson: When writing a safety argument about an identity/dedup scheme, state
which direction each failure mode runs BEFORE excusing any residual, and write
that rule into the spec as a rule — not as an aside inside the excuse. The
rule ("fork ⇒ open, collision ⇒ closed") is now normative text in §2.7, which
is the durable half of this lesson; the residual it originally excused was
closed on the direct path and refused on the proxy path. Retract errors IN
PLACE: the withdrawn sentence stays quoted in the spec so a future reader sees
what was wrong and why, rather than finding a silently different document.
