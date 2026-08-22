# Handoff — the regulatory-reporting adapter and the CDM bridge are BUILT and LIVE; nothing is committed

2026-08-22 (UTC). Written at the end of a twelve-task subagent-driven build.
**Everything below is in the WORKING TREES of both repos and NOT in git** —
the operator owns history here. Anchors are therefore the pre-build HEADs:
intent-plane **`2fab226`**, monorepo **`f93148c`**. The commit blocks are in
the foot of this brief; until they are run, `git log` shows none of this.

## Current state

- **built — a stdlib-only reporting adapter**, `declarant/pydeclarant/reporting_adapter.py`,
  born SDK-side and consumed back whole-file. It gates regulatory-report
  submissions by keying the report's regulatory IDENTITY (reporting entity,
  UTI, action type, rule set, delegated-reporting principal, plus the
  discriminator its action type keys on) and never its content, so the
  duplicate it refuses is "the same logical report again, however its bytes
  differ" — precisely the class a trade repository will not reject for you.
  It adds NO dependency: the LangChain and MCP adapters remain the only two
  sanctioned exceptions.
  re-verify: `cd ~/dev/intent-plane && core/scorer/.venv/Scripts/python -m pytest declarant/pydeclarant -q` — 150 passed, 2 skipped.
- **built — `gate_submission`, `gate_batch`, `reconcile`.** Execution happens
  only on a fresh synchronous Proceed; every other outcome refuses with the
  submit callable never invoked. `gate_batch` runs records INDEPENDENTLY and
  promises NO atomicity (a regulatory file is acknowledged per record, so a
  batch is neither wholly authorized nor wholly refused); a non-gate
  exception propagates rather than being laundered into an orderly refusal.
  `reconcile` recomputes every logged submission's key and reports the
  bijection against the feed's ACHIEVED records in five named defect classes
  — the auditor-facing half.
- **built — a dict-shaped CDM bridge**, `declarant/pydeclarant/cdm_adapter.py`,
  mapping FINOS Common Domain Model `WorkflowStep` dicts to and from the
  adapter's identity, plus `gate_cdm_event` (CDM step in, gated submission,
  CDM step out). It imports no CDM package. A gate refusal is RETURNED as a
  `rejected` step carrying the same `eventIdentifier` (the key) an ACHIEVED
  step would, so a consumer can correlate them; a pre-declaration refusal
  raises, because there is no record to emit when nothing was declared.
- **built — LIVE, both operating systems.** The ladder is 15 probes with the
  reporting-gate probe at position 11, and the feed carries exactly 7
  ACHIEVED. Its four legs: a valuation submits once; the same identity with
  DIFFERENT bytes is refused `ALREADY_RESERVED` with the repository counter
  unmoved; an erasure is refused by the GATE's own human-judgment abstention
  against an attested spec, not by adapter code; and an unkeyable report
  declares nothing.
  re-verify: `cd ~/dev/treasury-intent-controller && powershell -File treasury/quickstart.ps1` and the WSL twin — `RESULT: 15/15 probes passed` on each, `exactly 7 ACHIEVED`, `VERIFIED intents=21`.
- **built — the ladder now ASSERTS `intents=21`.** Both lanes check it in the
  recompute probe. That number is load-bearing: it lands on 21 rather than 22
  precisely because the unkeyable report is refused before it declares. It
  was previously a fact a human had read in a transcript; it is now
  mechanical, and a new declaring probe must bump it deliberately.
  re-verify: the guard accepts `intents=21 ` and rejects both `intents=22` and the `intents=2` prefix — proven in both shells.
- **built — specs first, in both repos.** SDK `CONTRACT.md` §2.7 gained the
  reporting-adapter and CDM-bridge paragraphs and §5.4 gained claim 18; the
  monorepo gained ported paragraphs and its own claim 17 (numbers differ
  between the repos BY DESIGN). Both claims' `Live:` clauses are dated with
  observed numbers, having been marked `NOT YET RUN` until the probe actually
  ran.
- **not built, by measurement — the CDM-validated oracle lane.** `finos-cdm`
  7.1.0 cannot construct a `WorkflowStep` carrying a populated
  `eventIdentifier`; measured on Linux 3.12.3 AND Windows 3.14.2 with
  byte-identical errors, so it is a property of the distribution, not a
  platform artifact. The two oracle tests SKIP with a reason that says
  installing the package would not help.
- **not built:** Datadog log-forward and the R4 OTel exporter (still deferred
  by ruling 7); the demand-side artifact and the platform-side article.

- **fixed after the build (2026-08-22), five surface-doc drifts** found by
  evaluating the report against the trees rather than from memory: the SDK
  README's refuses-bullet overstated (it now states the case limit); the
  README's two-sides table named only two adapters (now three plus the CDM
  bridge); `docs/assurance.md`'s claim row still said `NOT YET RUN` for a
  probe that had run green (now dated with the observed numbers, because the
  plan's ladder step named both CONTRACTs and not this file); `docs/integration.md`
  said nothing about the case residual (one paragraph added — the adapter's
  central property is now a precondition on the caller, and the integration
  doc is where a caller reads); and the monorepo's untracked `CLAUDE.md`
  quoted a lane count of 124 against a real 150 (converted to command
  pointers, as the SDK's copy already was). The pattern is the 2026-08-20
  learning recurring inside the build that cited it: the document with a
  mechanical gate stayed honest; the three without one drifted within hours.
  re-verify: `cd ~/dev/intent-plane && grep -rn "NOT YET RUN" README.md CONTRACT.md docs/*.md | wc -l` prints 0; `grep -c "= 124" ~/dev/treasury-intent-controller/CLAUDE.md` prints 0.

## Locked decisions

- **The plane stays payload-blind; the adapter is content-blind by
  construction.** `submit` takes no arguments — the caller closes over the
  report bytes and the module never sees them. Reason: the gate cannot
  validate content it never receives, and the duplicate worth refusing is
  identity-shaped, not byte-shaped.
- **The action-type table is a CLOSED SET and EMIR-Refit-SHAPED, not
  EMIR-verified.** It is the author's reading and every document says it must
  be checked against the current ESMA validation rules before any production
  claim. No text in either repo claims regulatory sufficiency.
- **Refusing beats normalizing when closing a key fork.** Whitespace-padded
  and non-NFC identity fields are REFUSED; letter case is deliberately NOT
  folded. Reason: refusing can never merge two genuinely distinct
  identifiers, whereas folding case could merge two that some scheme
  distinguishes and brick one forever, since reservations never expire.
- **`gate_batch` promises no atomicity**, and says so in its docstring;
  partial results after a propagating exception are recovered from the FEED
  via `reconcile`, never from its return value.
- **Prose NAMES probes; only ladder tables and scripts carry ordinals.**
  Applied again here — three more numbered-in-prose sites were renamed.
- Inherited and unchanged: operator-only git; CONTRACT amended FIRST;
  consumer trees born SDK-side and consumed back whole-file, byte-identical;
  the public SDK never names the private monorepo.

## Reuse map

- `declarant/pydeclarant/_gate_double.py` `key_aware=True` — still the ONLY
  way an in-process test can observe a duplicate authorization. Every
  at-most-once assertion in this build uses it; the default double is
  key-blind and cannot see duplication at all.
- `treasury/specs/08-erasure-human-judgment.spec.json` — the pattern for
  making the GATE abstain rather than writing approval logic into an adapter:
  put a `human_judgment` entry in the attested spec and declare under it.
- `treasury/probes/reporting_gate_live.py` — the four-leg probe shape, where
  every leg's observable is a COUNTER, so "refused" means the consequence
  demonstrably did not happen.
- The controller's spike script (`cdm_ctrl_spike.py`, scratchpad) — six
  PASS/FAIL checks that attribute each failure before counting it.

## Invariants

- The two `declarant/` trees are BYTE-IDENTICAL across repos, and
  `core/internal/scoring/scorer.go` likewise. The live probe imports the
  monorepo's copy, so drift breaks it silently.
- Execution requires a FRESH SYNCHRONOUS Proceed. A Proceed read back from
  the 500-edge feed consult is a historical achieved and must never execute.
- Every invocation declares under its own fresh intent: episode seed = key
  plus a per-invocation nonce, never the bare key.
- A key FORK is fail-OPEN and is the worst failure class here; a COLLISION is
  fail-closed but bricks a legitimate action forever.
- The ladder's 15/15, the 7 ACHIEVED, and `intents=21` move together — a new
  declaring probe changes all three, deliberately.
- The public SDK carries zero banned strings; `go test ./core/internal/contractcheck` pins it.

## Open / next

1. **Three operator rulings, none blocking.** (a) **NEW: letter-case
   normalization of identity fields** — currently a recorded residual in both
   CONTRACTs and pinned by a test; folding it is a breaking key-format change
   and risks merging distinct identifiers. (b) The ambiguous-union residual
   on the MCP proxy path. (c) The key format does not escape `:` — sighted a
   SECOND time this build, in `reconcile`'s prefix filter.
2. **Four coverage-gap tests** carried from the 2026-08-20 mutation pass,
   plus two found here: no test drives the adapter's `ALREADY_ACHIEVED`
   (feed-consult Proceed) branch, and `_spec_hash_for` does not reject a
   non-string mapping value.
3. **The monorepo's §5.4 table has NO rows for the LangChain adapter or the
   MCP gate**, though probes 8, 9 and 10 run THERE. Pre-existing gap from an
   earlier port; the monorepo's claim table understates what that repo proves.
4. **Numbered-in-prose ordinals remain widespread** — claim rows 15–17 in both
   CONTRACTs, three sites in `docs/ROADMAP.md`, one in `CLAUDE.md`. All
   correct today, all the same decay class.
5. **The standing fork** — demand-side artifact vs platform-side article — is
   unchanged, but the article's spine gained two more episodes where
   verification caught something real, both of them in the author's own plan
   text rather than in the implementation.

## Commit blocks

```bash
cd ~/dev/intent-plane
git add CONTRACT.md
git commit -m "docs(contract): reporting adapter + CDM bridge (2.7) + claim 18"
git add declarant/pydeclarant/reporting_adapter.py declarant/pydeclarant/test_reporting_adapter.py
git commit -m "feat(pydeclarant): stdlib reporting adapter - identity-keyed, content-blind"
git add declarant/pydeclarant/cdm_adapter.py declarant/pydeclarant/test_cdm_adapter.py
git commit -m "feat(pydeclarant): dict-shaped CDM WorkflowStep bridge + gate_cdm_event"
git add README.md docs/assurance.md docs/integration.md
git commit -m "docs: reporting adapter - README fourth way, assurance row, integration"
git push
```

```bash
cd ~/dev/treasury-intent-controller
git add CONTRACT.md
git commit -m "docs(contract): port reporting adapter + CDM bridge; claim 17"
git add declarant/pydeclarant/reporting_adapter.py declarant/pydeclarant/test_reporting_adapter.py \
        declarant/pydeclarant/cdm_adapter.py declarant/pydeclarant/test_cdm_adapter.py
git commit -m "feat(declarant): consume back reporting adapter + CDM bridge whole-file"
git add treasury/specs/08-erasure-human-judgment.spec.json treasury/probes/reporting_gate_live.py \
        treasury/quickstart.sh treasury/quickstart.ps1 treasury/README.md README.md
git commit -m "feat(treasury): live reporting-gate probe; ladder 15/15, asserts intents=21"
git add docs/learnings/2026-08-21-finos-cdm-python-unusable-on-windows.md \
        docs/learnings/2026-08-21-identity-keying-needs-normalization-or-refusal.md \
        docs/learnings/2026-08-21-brief-copies-and-stale-artifact-collisions.md \
        docs/learnings/LEARNINGS.md
git commit -m "docs(learnings): CDM measurement, identity key fork, brief collisions"
git add docs/handoff/2026-08-22-regulatory-reporting-adapter-built-and-live.md docs/handoff/HANDOFF.md
git commit -m "docs: handoff for the reporting adapter build"
git push
```

Not folded in, left for the operator: `CLAUDE.md` in both repos was edited on
disk (both are untracked by design and will not appear in `git status`); the
`.git/sdd/` task reports and briefs are session artifacts and are not part of
either commit set.
