# A TRACKED doc with no pin drifted within hours of the contract moving: the assurance map still said NOT YET RUN for a probe that had run green

ts: 2026-08-22T00:45:00Z
commit: intent-plane 2fab226 + uncommitted working tree (approximate ts — captured between the 00:34Z mutant run and the fixes that landed in b3303c3; the basis is the controller's own grep, quoted)
session: 192b819d
status: verified
fact: When the live reporting-gate probe went green, the build's ladder
step updated the `Live:` clause in BOTH repos' `CONTRACT.md` from
`NOT YET RUN` to a dated clause with observed counters — and did not touch
`docs/assurance.md`, whose claim row for the same adapter carried the same
`NOT YET RUN` marker. The plan's step named the two CONTRACTs and not the
assurance map, so the public claim-to-mechanism document — the one written
specifically for people who check claims — told its reader a probe did not
exist that did. Three more surface drifts were found in the same pass: the
README's refuses-bullet claimed "refused however its bytes differ" after a
case fork had been recorded as an OPEN residual; the README's two-sides
table still named two adapters when three had shipped; and the monorepo's
untracked `CLAUDE.md` quoted a lane count of 124 against a measured 150.
The 2026-08-20 learning covered the UNTRACKED case; this one is the tracked
case: `docs/assurance.md` is in git, is scanned by the vocabulary pins, and
still drifted, because no pin reads the FRESHNESS of a claim row — only its
vocabulary.
basis: Controller's read-only checks against the working tree, verbatim:
```
== (6) assurance.md claim row: still NOT YET RUN? ==
122:| Reporting adapter fail-closed and content-blind — `gate_submission` invokes the caller's `submit()` only on a fr
== assurance row 122 full Live clause ==
Live: **NOT YET RUN.** The reporting-gate probe is specified, not built; until it runs green this claim rests on the pytest lane and mutants above.
== (a) TIC CLAUDE.md counts vs reality ==
46:`gating.py`, `_gate_double.py`); lane `pytest declarant/pydeclarant` = 124
108:the declarant-twin pytest (`... -m pytest declarant/pydeclarant` = 124
   actual lane: 150 passed, 2 skipped in 41.81s
== (4) README two-sides table adapter phrase ==
plus the LangChain and MCP adapters above
```
while at that moment `grep -c 'NOT YET RUN' CONTRACT.md` printed 0 in both
repos — the contract had moved, the assurance map had not.
re-verify: `cd ~/dev/intent-plane && grep -rn "NOT YET RUN" README.md CONTRACT.md docs/*.md | wc -l` prints 0 at b3303c3, and `git -C ~/dev/intent-plane show b3303c3 --stat -- docs/assurance.md | tail -1` shows the assurance map changed in the fixing commit.
lesson: The document with a mechanical gate stayed honest; the three
without one drifted within hours of the contract moving. Being tracked is
not being guarded: the pins scan `docs/assurance.md` for banned vocabulary
and never for whether a claim row's evidence clause is current. Two
practical rules follow. When a plan step dates a `Live:` clause, it must
name EVERY document that carries that clause, not just the contract. And
any `NOT YET RUN` marker should be greppable as a single exact string across
the whole tree, so that "zero occurrences after the probe lands" is one
read-only command a pick-up can run — which is what made this drift
findable in the first place, once somebody looked.
