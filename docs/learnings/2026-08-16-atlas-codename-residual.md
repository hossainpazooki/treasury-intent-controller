# ATLAS codename residual in the public SDK repo — the honesty finding undercounted

ts: 2026-08-16T17:52:11Z
commit: intent-plane sdk-ship 89443a3 (clean tree; this repo's ledger, finding is about the SDK repo)
session: 0295a4ce-41cc-4e54-a674-1818455dbf7c
status: verified
fact: After the honesty-fix commit removed the two finding-3 codename sites
(COMPASS x2 plus the CONTRACT §1.2 ATLAS parenthetical), ATLAS still appears
on 26 matching lines across 6 tracked files in the PUBLIC intent-plane repo —
CONTRACT.md (7 lines) and the core/scorer tree, including the load-bearing
env-var names SCORER_ATLAS_INPUTS_DIR / SCORER_ATLAS_DIR wired through
__main__.py, resolver.py, and two test files, plus an internal ADR-number
leak in core/scorer/README.md. The original honesty finding (08-14 recon)
counted the codename leak as 3 doc sites; that assumption is refuted — the
full scrub is a breaking config-surface rename, not a doc edit, and is
parked as a deferred workstream needing an operator ruling.
basis: at 89443a3, `git grep -c "ATLAS"` printed: "CONTRACT.md:7 /
core/scorer/README.md:5 / core/scorer/src/scorer/__main__.py:3 /
core/scorer/src/scorer/resolver.py:2 /
core/scorer/tests/test_main_config.py:1 /
core/scorer/tests/test_resolver.py:8" (sum 26, 6 files); `git grep -n
"COMPASS"` exited 1 (zero hits). Found by skeptic dispatch during the
adversarial review of the triad build plan; counts recomputed first-party.
re-verify: git -C ~/dev/intent-plane grep -c ATLAS sdk-ship
