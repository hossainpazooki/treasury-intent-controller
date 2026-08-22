# An adapter that takes on canonicalization must finish the job: unnormalized identity strings forked the key four ways, fail-OPEN

ts: 2026-08-22T00:05:00Z
commit: intent-plane 2fab226 / monorepo f93148c + uncommitted working tree
session: 192b819d
status: verified
fact: The reporting adapter keys a regulatory report by its IDENTITY rather
than its content, and `canonical_identity` performs that canonicalization
itself. It applied NO normalization to the identity's string fields, so four
spellings a real system treats as ONE report each derived a DIFFERENT key:
letter case (`529900T8BM49AURSDO55` vs the same LEI lowercased), a trailing
space, a leading space, and Unicode NFC vs NFD (byte-different, visually
identical). A forked key is FAIL-OPEN and is the worst failure class here:
the second spelling has no reservation to collide with, so the gate
authorizes it and the report is submitted twice. This is a DISTINCT fork
from the date-spelling fork closed earlier the same day; that one was about
`as_of` calendar formats, this one about every keyed string field. Found by
an adversarial skeptic dispatched to refute the claim "one logical report
derives one key" -- the suite, the mutation pass, and the controller's own
probes had all passed over it, because every fixture happened to spell each
identifier one way. Closed by REFUSING whitespace-padded and non-NFC fields
before declaring. Letter case was deliberately NOT closed: see the residual
below.
basis: Controller's first-party reproduction against the real module,
verbatim:
```
lowercase LEI+UTI        forks: True
trailing space on LEI    forks: True
leading space on UTI     forks: True
NFC vs NFD equal strings? False | same rendering, forks: True
```
and the skeptic's end-to-end run through `gate_submission` against a
KEY-AWARE gate double (the only mode that refuses a repeated key), which
showed both spellings drawing a fresh Proceed and `submit()` firing TWICE
for one logical report. After the fix, same probe:
```
trailing space   refused: reporting_entity has leading or trailing whitespace
leading space    refused: reporting_entity has leading or trailing whitespace
NFD (non-NFC)    refused: reporting_entity is not Unicode NFC
clean value still keys: True
case STILL forks (documented residual, not silently folded): True
```
re-verify: `cd C:/Users/hossa/dev/intent-plane/declarant/pydeclarant && ../../core/scorer/.venv/Scripts/python -m pytest test_reporting_adapter.py -q` -- the strictness battery carries the whitespace and NFC cases, and a dedicated test PINS the case residual so that folding case later fails loudly until the contract is updated.
lesson: An adapter that takes canonicalization onto itself owns it
completely; a partial canonicalization is worse than none, because callers
reasonably assume the normalization they can see extends to the fields they
cannot. Two further rules came out of this. First, REFUSING is the safe way
to close a fork and NORMALIZING is not: refusing a bad spelling can never
merge two genuinely distinct identifiers, while folding case could merge two
that some scheme legitimately distinguishes and brick one forever, since
reservations never expire -- so case-folding is an operator ruling recorded
as a residual, not a library decision taken quietly. Second, a test suite
whose fixtures each spell an identifier exactly one way cannot find a
spelling fork, no matter how many tests it has; only an adversary told to
BREAK the claim went looking for a second spelling. Related: the
date-spelling fork closed the same day, and the 2026-08-20 rule that a key
fork is fail-open while a collision is fail-closed.
