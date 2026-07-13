ts: 2026-07-13T22:37:24Z
commit: 6adff98
session: 71412dd0-24d9-4663-b2a9-0130c545ee1f (atlas-treasury-payment-loop)
status: verified

fact: An ATLAS verification environment (the policy + context input pair) is KIND- and CORPUS-shaped: one static set correctly verifies exactly one artifact kind sharing one source corpus. The shared `scripts/contract-inputs/` set is the RULE-artifact environment and rejects the IntentSpec golden by construction (wrong required-attestation set AND wrong `current_legal_source_hash`); the IntentSpec environment is synthesized by the same procedure ATLAS itself uses — kind policy per ADR-0021 §5 (`ke-cli` `policy_for_kind`) + context hash = that artifact's `source_corpus_hash` (per `emit-contract-inputs.rs`). Consequence for the scorer: one configured `KeArtifactResolver` serves one kind-environment; a mixed-kind request (rule hash + spec hash, different corpora) fails closed to UNEVALUABLE. Recorded debt: a binding-side kind-aware environment (ATLAS follow-up), not a Python-side policy re-implementation.

basis: folded-verify probes 2026-07-12 against `fixtures/artifacts/intentspec_payment/artifact.kew` — under the shared inputs: `verdict: rejected:Attestations([LegalSourceHashChanged, LegalSourceHashChanged, RequiredTypeMissing { attestation_type: SourceFidelity, ... }, RequiredTypeMissing { attestation_type: ScenarioCoverage, ... }, ...])`; under the synthesized IntentSpec env (post-R7-fix wheel): `verify(None, c7a36959…) is True`, and the same env with registry status `Unknown` → False.

re-verify: in WSL, `python3 -m pytest scorer/tests/test_resolver.py -q -k wheel_lane` — `test_wheel_lane_rule_environment_rejects_intentspec` and `test_wheel_lane_golden_intentspec_verifies` pin both directions.
