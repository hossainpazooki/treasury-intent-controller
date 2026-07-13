ts: 2026-07-13T22:37:24Z
commit: 6adff98
session: 71412dd0-24d9-4663-b2a9-0130c545ee1f (atlas-treasury-payment-loop)
status: verified

fact: The scorer's original wheel-lane test skipped on `pytest.importorskip("ke_artifact")`, but the wheel's module is `ke_artifact_py` (`[tool.maturin] module-name` in regulatory-rule-engine `crates/ke-artifact/pyproject.toml`). The lane could NEVER have run anywhere — a skip that read as by-design ("wheel absent") was actually unconditional. A visible skip is only honest if the skip condition itself is verified reachable-as-false somewhere.

basis: pre-fix test read `pytest.importorskip("ke_artifact", reason="ke-artifact-py wheel absent (Linux/CI-only by design)")`; pyproject reads `module-name = "ke_artifact_py"`. After the fix (commit 6adff98) the lane executed in WSL: `39 passed in 2.22s` with zero skips.

re-verify: `grep -n importorskip scorer/tests/test_resolver.py` — must name `ke_artifact_py`; then in WSL `python3 -m pytest scorer -q` shows no wheel-lane skips.
