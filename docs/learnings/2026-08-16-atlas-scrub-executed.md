# The ATLAS scrub, executed — and the wider internal-reference class it exposed

ts: 2026-08-16T20:40:00Z
commit: intent-plane main 89443a3 (+ uncommitted working tree)
session: e6e0badf-83f6-46ed-aee7-1ce4b994c525
status: verified
fact: Operator ruled 2026-08-16 to resolve all three ATLAS categories and the
private-monorepo references in the same pass, overturning the deferral
recorded in `2026-08-16-atlas-codename-residual.md`. Result: `ATLAS`,
`COMPASS`, `SCORER_ATLAS*`, and `treasury-intent-controller` are now at ZERO
tracked occurrences in the public SDK. CONTRACT.md was amended first, then
code, then the pinned tables and docs, per the standing ordering rule.

Two env vars were renamed with NO compatibility shim, by explicit operator
ruling that overrode a fail-closed objection raised before the work started:
  SCORER_ATLAS_INPUTS_DIR -> SCORER_VERIFY_INPUTS_DIR
  SCORER_ATLAS_DIR        -> SCORER_GOLDENS_DIR
The objection, recorded because it remains true: `resolver_from_env()` treats
"none of the three resolver vars set" as `NullResolver`, so a deployment left
on a retired name does not fail loudly — it silently stops verifying. That is
the one place the repo's own "a server the operator configured to verify must
never silently not-verify" rule is unenforced against a stale deployment. It
is now written into CONTRACT.md's migration section rather than into code.

The wider finding: the scrub uncovered an internal-reference class MUCH larger
than the 26 ATLAS lines, in two kinds, NEITHER of which was in the ruling's
scope and neither of which was touched:
  (A) This program's own ADR numbers (ADR-0006/0007/0009) appear ~15 times
      across CONTRACT.md, README.md, docs/, and Go source, but the ADR files
      live in the private monorepo. A public reader sees "ADR-0006, Proposed"
      and cannot read it. This is a deliberate convention, not an accident;
      resolving it means either publishing the ADRs SDK-side or de-numbering
      the references.
  (B) A THIRD private project's internals: `regulatory-rule-engine`,
      `ADR-0019`, `ADR-0021`, `ke-cli policy.rs` — 8 refs, all under
      `core/scorer/`. Same leak class as ATLAS. One of them
      (`test_resolver.py:179`) is a LIVE sibling-checkout path the wheel lane
      depends on, so it cannot be renamed as prose.

Method note worth keeping: a blind `sed s/SCORER_ATLAS_DIR/.../` rewrote a
HISTORICAL migration line in CONTRACT.md so that it recorded a rename that
never happened (it claimed the 2026-08-03 rename landed on the 2026-08-16
name). Mechanical renames must not be run over lineage/history sections
without re-reading them; the record is a claim about the past, not a symbol.

basis: first-party at the working tree. `git grep -c` for each of ATLAS,
COMPASS, SCORER_ATLAS, treasury-intent-controller returns 0 files. Full gate
green after the scrub: go build/vet/test ./... all pass, six contractcheck
pins PASS, verifier pyverifier 6 passed, scorer 42 passed / 5 skipped
(identical to the pre-scrub baseline). Rename coverage proven NON-VACUOUS by
mutation: reverting `__main__.py` to the old name turns
`test_partial_resolver_config_refuses_to_boot[SCORER_VERIFY_INPUTS_DIR]` RED
(1 failed, 6 passed), and restoring gives 7 passed.

CAVEAT, stated not hidden: `SCORER_GOLDENS_DIR` is exercised ONLY by the
wheel lane, which skips on Windows (`ke-artifact-py` absent by design). That
half of the rename is unverified locally and first runs on Linux/CI.

re-verify: cd ~/dev/intent-plane && for p in ATLAS COMPASS SCORER_ATLAS treasury-intent-controller; do echo "$p: $(git grep -c "$p" | wc -l)"; done && go test ./... -count=1 && (cd core/scorer && .venv/Scripts/python -m pytest -q)
