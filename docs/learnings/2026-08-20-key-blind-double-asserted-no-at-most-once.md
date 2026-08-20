# The test double, not the tests, was the blind spot: 45 green tests asserted at-most-once nowhere

ts: 2026-08-20T15:05:00Z
commit: intent-plane d9041f5 + uncommitted working tree (approximate ts — captured from a code review delivered during the MCP gate build, same session as the sibling entries)
session: 52e04ce2-ba8f-439f-b44b-3b05d69cdd88
status: verified
fact: The shared in-process gate double (`declarant/pydeclarant/_gate_double.py`)
popped scripted responses in order and never read `idempotency_key`. It had no
key memory. Consequently NO test in the lane could observe a duplicate
authorization at all: a forked key received exactly the same scripted PROCEED
as a shared key. That is the structural reason four key-fork defects shipped
under 45 green tests — the individual tests were fine; the fixture they all
shared could not express the property. The property in question is
at-most-once, which is the entire reason the module exists. Compounding it, the
one test whose NAME claimed to guard it
(`test_same_key_retry_500_with_own_achieved_refuses_not_reexecutes`) never
asserted the two calls shared a key, and passed identically when the second
call had different arguments — it was testing the consult-classification path,
not at-most-once. It was also, separately, the strongest test in the file: it
alone killed five of twenty-one mutants.
basis: Same two calls, same adapter, same shared key, run against both doubles: key-aware -> "call 1 -> OK / call 2 -> REFUSED intent refused: class=ALREADY_RESERVED ... TOOL BODY EXECUTIONS: 1"; key-blind (the old default) -> "call 1 -> OK / call 2 -> OK / TOOL BODY EXECUTIONS: 2". After the fix, forcing the double back to key-blind turns eight tests red with "the repeat was authorized".
re-verify: `grep -n "key_aware" ~/dev/intent-plane/declarant/pydeclarant/_gate_double.py` — the opt-in mode exists (default off, so sibling lanes are unaffected), and the fork battery's tests are named `..._executes_once` / `..._executes_twice` rather than comparing keys.
lesson: Ask of a test suite not "does each test assert something" but "can the
shared fixture EXPRESS the property the module exists to provide?" A double
that cannot distinguish the failure from the success makes every test built on
it silently weaker than its name. Two specific habits fall out: (1) when a test
asserts equality of derived values (keys, hashes, ids) as a proxy for a
behaviour, prefer asserting the BEHAVIOUR against a fixture that models the
real counterparty — assert non-duplication, not key equality; (2) a test whose
name states an invariant must assert the precondition of that invariant, or
the name is documentation that lies. Both are cheap to check by mutating the
fixture rather than the code under test.
