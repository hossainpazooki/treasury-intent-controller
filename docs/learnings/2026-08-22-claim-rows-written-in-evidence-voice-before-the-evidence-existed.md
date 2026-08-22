# Claim rows were written in evidence-voice before the evidence existed, twice in one build: a probe that had not run, and two mutants nobody had executed

ts: 2026-08-22T00:34:03Z
commit: intent-plane working tree that became d854cd9 (reporting_adapter.py byte-identical to that blob, verified post-commit; the CONTRACT clause history is in 7057123)
session: 192b819d
status: verified
fact: The plan's text for CONTRACT claim 18 was drafted in the same
past-tense, dated voice as the neighbouring rows 16 and 17, whose evidence
already existed. Two of its statements were therefore assertions dressed as
results. First, its `Live:` clause narrated a probe's four legs in flat
present tense before the probe had been written; the Task 1 reviewer caught
it and the clause was changed to `NOT YET RUN` until the probe ran green.
Second, its `Mutants:` clause listed three mutants as if executed —
"execute `submit()` on `Indeterminate` ⇒ the matrix goes red; include the
non-keyed discriminator in the payload instead of refusing ⇒ the strictness
battery goes red; drop `rule_set` ⇒ the negative control goes red" — of
which only the FIRST was ever run (Task 3 Step 5). The other two stood as
asserted evidence for roughly seven hours, through six task reviews and an
integration gate, until the controller's post-build evaluation ran them in
a scratch copy. Both were in fact killed. The claim was TRUE; it was not
yet EVIDENCED, and nothing in the build distinguished the two.
basis: Controller's scratch-copy mutant runs against the tree that became
d854cd9, verbatim:
```
== baseline in scratch copy ==
52 passed in 7.47s
== MUTANT 3: drop rule_set from the keyed base fields ==
mutant 3 applied
4 failed, 48 passed in 7.00s
== MUTANT 2: include a non-keyed discriminator in the payload instead of refusing ==
mutant 2 applied (exact-text)
36 failed, 16 passed in 8.62s
== scratch restored; real tree untouched ==
identical-to-tree
```
The Task 3 report carries the first mutant's own run ("neutering
`require_fresh_proceed(res)` -> `pass` turned 9 tests red"). The original
present-tense `Live:` clause and its `NOT YET RUN` replacement are both in
the history of 7057123.
re-verify: `cd ~/dev/intent-plane && grep -o 'Mutants: [^|]*' CONTRACT.md | grep -c 'drop \`rule_set\`'` prints 1 — the three-mutant clause is still there; a verifier re-runs the two scratch mutants above against HEAD and expects red both times.
lesson: A claim row's evidence column is a list of things that HAPPENED,
and a plan author writing it before the build has no such things to list.
The `NOT YET RUN` marker solved this for the `Live:` clause because it is
one exact string a pick-up can grep to zero; the `Mutants:` clause had no
equivalent marker, so an unexecuted mutant read identically to an executed
one. The rule that follows: any evidence clause written ahead of its
evidence carries an explicit NOT YET RUN (or equivalent greppable marker)
per item, and a build's integration gate greps that marker to zero before
anyone calls the claim proven. Verification that is merely described is a
plan; only verification that was run is evidence.
