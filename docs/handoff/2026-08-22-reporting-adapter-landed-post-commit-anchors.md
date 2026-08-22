# Handoff — the reporting adapter and CDM bridge are COMMITTED AND PUSHED; anchors superseded

2026-08-22 (UTC, ~02:50Z). Newest commits this brief describes: intent-plane
**`b3303c3`** ("docs: reporting adapter - README, assurance row (live-dated),
integration") and the monorepo **`6f108fe`** ("docs: handoff for the reporting
adapter build"). Both trees clean; both remotes verified via `gh` to sit at
exactly those SHAs (0 ahead / 0 behind). Pick-up measures drift from these two.

This brief SUPERSEDES the anchors of
`2026-08-22-regulatory-reporting-adapter-built-and-live.md`, which was written
while everything was uncommitted and says so in its header. That brief
remains the narrative of record for WHAT was built, the two key forks, the
skeptic pass, and the five surface-doc fixes; this one records that it all
landed, what was learned at curation, and what the next session starts from.

## Current state

- **built — nine commits landed, spec-first in both repos.** SDK: `7057123`
  (CONTRACT: reporting adapter + CDM bridge, claim 18, case residual),
  `d854cd9` (feat: reporting adapter), `f03028b` (feat: CDM bridge +
  `gate_cdm_event`), `b3303c3` (docs, incl. the five post-build fixes).
  Monorepo: `6c68757` (CONTRACT port, claim 17, probes named in prose),
  `5a84aa5` (consume back), `7cc92b6` (live probe, ladder 15/15, asserts
  intents=21), `8eb6367` (three learnings), `6f108fe` (handoff).
  re-verify: `git -C ~/dev/intent-plane log --oneline 2fab226..b3303c3` — four; `git -C ~/dev/treasury-intent-controller log --oneline f93148c..6f108fe` — five.
- **built — remotes match.** Read-only via the GitHub API, never a local fetch.
  re-verify: `for r in intent-plane treasury-intent-controller; do echo "$r $(gh api repos/hossainpazooki/$r/commits/main --jq .sha | cut -c1-7) $(git -C ~/dev/$r rev-parse --short HEAD)"; done` — each line's two SHAs equal.
- **built — both lanes green AT the committed trees** (re-captured after the
  commits, not carried over): 150 passed / 2 skipped in both repos; the 2
  skips are the CDM oracle lane, which cannot pass anywhere (measured);
  contractcheck 7 pins ok; declarant trees byte-identical.
  re-verify: `cd ~/dev/intent-plane && core/scorer/.venv/Scripts/python -m pytest declarant/pydeclarant -q && go test ./core/internal/contractcheck -count=1`; `diff -r ~/dev/intent-plane/declarant ~/dev/treasury-intent-controller/declarant -x __pycache__ -x .pytest_cache` prints nothing.
- **built — the live ladder, both OS lanes, run first-party before the
  commits** (unchanged since: the post-commit tree is byte-identical): 15/15,
  exactly 7 ACHIEVED, `VERIFIED intents=21`, and the ladder ASSERTS that
  count.
  re-verify: `cd ~/dev/treasury-intent-controller && powershell -File treasury/quickstart.ps1` prints `RESULT: 15/15 probes passed`.
- **built — all three of claim 18's mutants are now EXECUTED**, not asserted:
  the first by Task 3 (9 red), the other two in a post-build scratch run
  against the tree that became `d854cd9` (36 red; 4 red). See the new
  learning.
- **built — learnings: five dated entries from this build** (three landed in
  `8eb6367`; two written at this hand-off, below), each with a captured
  basis and a read-only re-verify line that was executed before being
  written down.
  re-verify: `ls ~/dev/treasury-intent-controller/docs/learnings/2026-08-2[12]-*.md | wc -l` prints 5.
- **not built, by measurement:** the CDM-validated oracle lane (`finos-cdm`
  7.1.0 fails identically on Linux and Windows). **not built, deferred:**
  Datadog log-forward, R4 OTel exporter; the audit-side artifact and the
  platform-side article (the standing fork).

## Locked decisions

- **Refusing beats normalizing when closing a key fork; letter case is an
  operator ruling, not a library default.** Whitespace and non-NFC identity
  fields are refused; case is keyed as given and recorded as a RESIDUAL in
  both CONTRACTs, pinned by a test, and now stated on every surface
  (README, assurance, integration). Reason: refusing can never merge two
  distinct identifiers; folding case could brick a legitimate distinct
  report forever, since reservations never expire.
- **A `NOT YET RUN` marker is the only honest way to write an evidence clause
  ahead of its evidence, and it must be grep-to-zero before a claim is called
  proven.** Reason: the new learning — two asserted-but-unrun mutants
  survived six reviews because nothing distinguished them from executed ones.
- **A step that dates a `Live:` clause names EVERY document carrying it.**
  Reason: the assurance map said NOT YET RUN for hours after the contract
  was dated; tracked is not guarded.
- **Brief files are per-plan, or get an explicit outfile.** Reason: the
  shared-default-directory collision handed an agent a 17-day-old project's
  brief and fooled the controller's own wait-loop.
- Inherited, unchanged: payload-blind plane, content-blind adapter, closed
  EMIR-*shaped* action table with no sufficiency claim, `gate_batch` no
  atomicity, prose names probes, operator-only git, CONTRACT first, consumer
  trees born SDK-side and consumed back byte-identical.

## Reuse map

- `docs/handoff/2026-08-22-regulatory-reporting-adapter-built-and-live.md` —
  the narrative, the skeptic verdicts, the fix design, and the addendum
  listing the five surface-doc drifts with their re-verify lines.
- `declarant/pydeclarant/_gate_double.py` `key_aware=True` — still the only
  in-process way to observe a duplicate authorization.
- The scratch-copy mutant recipe (copy `pydeclarant/` to scratch, patch by
  exact text, run, restore, `cmp` against the tree) — run a mutant without
  ever touching the real tree; the post-build evaluation used it.
- `treasury/specs/08-erasure-human-judgment.spec.json` — make the GATE
  abstain via an attested `human_judgment` entry instead of adapter logic.
- The controller's six-check CDM spike, with its FAIL-must-be-attributed
  discipline (one of its own checks was a bad fixture, not a package defect).

## Invariants

- `declarant/` trees and `core/internal/scoring/scorer.go` byte-identical
  across repos at these SHAs; a change on either side ports whole-file.
- Execution only on a FRESH SYNCHRONOUS Proceed; a feed-consult Proceed is
  historical and never executes.
- Fresh episode seed per invocation (key + nonce, never the bare key).
- A key FORK is fail-OPEN; a COLLISION is fail-closed but bricks.
- The ladder's 15/15, the 7 ACHIEVED and `intents=21` move together, by
  design; a new declaring probe bumps all three deliberately.
- `NOT YET RUN` is grep-to-zero across README, CONTRACT and `docs/` in the
  SDK at `b3303c3` — and must stay zero unless a new unrun claim is added.
- The public SDK carries zero banned strings outside the pin's own denylist.

## Open / next

1. **Three operator rulings, none blocking:** (a) letter-case normalization
   of identity fields — residual, pinned, breaking if folded; (b) the
   ambiguous-union residual on the MCP proxy path; (c) the unescaped `:` in
   the key format, sighted a second time in `reconcile`'s prefix filter.
2. **Six coverage-gap tests** (four from the 08-20 mutation pass; the
   `ALREADY_ACHIEVED` feed-consult branch in the reporting adapter's own
   suite; `_spec_hash_for` accepting a non-string mapping value).
3. **The monorepo's §5.4 table has no rows for the LangChain adapter or the
   MCP gate** though probes 8–10 run there — pre-existing; it understates what
   that repo proves.
4. **Numbered-in-prose ordinals remain** in claim rows 15–17 of both CONTRACTs,
   three `docs/ROADMAP.md` sites, and `CLAUDE.md` — correct today, same decay
   class.
5. **Process, for the next build:** plan code was handed to implementers
   untested and three of the worst defects were in it (date validator, README
   import, probe placement). Smoke-run plan code before dispatch, or plan
   behaviour + tests and let implementers write the code.
6. **The standing fork** — audit-side artifact vs platform-side article — now
   has, in the article's spine, two more verification-caught episodes, both in
   the author's own text.
