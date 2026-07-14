ts: 2026-07-14T14:49:35Z
commit: 9ac6fb8
session: 495274ae-4189-4c09-b42d-8027685f9f5b (rigor-loop-engineering)
status: verified

fact: The payment loop's fail-closed guarantee is now demonstrated by a LIVE probe with two paired negative controls, each a one-input delta, each shown red — re-running the 2026-07-13 brief's probe recipe with output captured (the brief asserted these negatives in a single clause with no output quoted; that is now evidence). Gate → WSL scorer → real `ke_artifact_py` wheel → folded verify of the signed, `Published` golden IntentSpec `c7a36959…dc51` under a kind-correct environment. Two further facts fell out: (1) the resolver verifies EVERY hash the gate sends, so a declaration carrying the fixture's placeholder `rule_artifact_hash: "rule-hash-1"` fails closed — correct behavior, but it means a real end-to-end declaration must send either a real rule hash or an empty one (empty hashes are dropped from the requested set); (2) one static inputs directory is one kind-environment — `~/tis-inputs` did not verify the IntentSpec; the env must be synthesized per `_intentspec_env()` (kind-aware policy: SourceFidelity + PublicationApproval, NO ScenarioCoverage; context's `current_legal_source_hash` = that artifact's `source_corpus_hash`).

basis: probe 2026-07-14, gate on :8080 with `TIC_SCORER_URL=http://127.0.0.1:8000/ml/evaluate`, scorer in WSL with the synthesized env:
```
PROBE 1  positive, golden spec hash:
  {"terminal":"ACHIEVED","reason":"","achieved_seq":14}
PROBE 2  negative control (a), unknown spec hash (64 zeros), one-input delta:
  {"terminal":"FAILED","reason":"unevaluable:amount_under_ceiling"}
PROBE 3  negative control (b), REAL scorer killed, identical golden declaration:
  {"terminal":"FAILED","reason":"unevaluable:amount_under_ceiling"}
PROBE 4  recovery, scorer restored, identical golden declaration:
  {"terminal":"ACHIEVED","reason":"","achieved_seq":35}
durable feed /v2/events after the run — the consumer-visible path:
  {'DECLARED':5,'RESOLVING':5,'ACTIVE':5,'VERIFYING':5,'SCORED':5,
   'UNEVALUABLE':3,'FAILED':3,'IDEMPOTENCY_RESERVED':2,'ACHIEVED':2}
```
2 grants, 3 refusals; PROBE 4 proves the outage control non-vacuous (the same declaration goes green once the scorer returns, so the kill — not the declaration — was the cause).

re-verify: the brief's probe recipe (`docs/handoff/2026-07-13-atlas-treasury-payment-loop.md`), with two corrections it lacks: send `"rule_artifact_hash": ""` (a placeholder hash fails closed), and point `TIS_ATLAS_INPUTS_DIR` at a SYNTHESIZED IntentSpec env, not the shared contract-inputs.
