ts: 2026-07-13T22:37:24Z
commit: 6adff98
session: 71412dd0-24d9-4663-b2a9-0130c545ee1f (atlas-treasury-payment-loop)
status: verified

fact: (regulatory-rule-engine, recorded here because the treasury resolver found it) The 3-language `contract-test.sh` compares the three legs' verdicts for EQUALITY — an artifact all three legs identically REJECT is green. Combined with gen-golden appending attestations without verifying the set, and no ke-cli test publishing an IntentSpec, this let a total contradiction stay latent: R7 unconditionally demanded a `ScenarioCoverage` co-attestation with `PublicationApproval`, while accepted ADR-0021 §5 pins the IntentSpec set to `SourceFidelity + PublicationApproval` — so NO IntentSpec could ever verify or publish. Surfaced by this repo's reader slice running the first kind-correct folded verify (2026-07-12); fixed by kind-aware `co_attestation_types()` (ADR-0022, RRE PR #14, merged 2026-07-13). General lesson: an equality gate across implementations proves agreement, never acceptability — each artifact kind needs at least one test asserting a green verdict end-to-end.

basis: probe verdict under the kind-correct policy pre-fix: `verdict: rejected:Attestations([CoAttestationAbsent { missing: ScenarioCoverage }])`; `gh pr list` in ke-cli tests: zero IntentSpec publish tests; post-fix: `gh pr view 14` → `MERGED 2026-07-13T22:28:18Z`, remote main tip `86a77fc`. Non-vacuity of the fix's tests proven by mutation in a temp copy (each weakened arm turned its guarding test red).

re-verify: `gh pr view 14 --json state` in regulatory-rule-engine (MERGED); `cargo test -p ke-artifact --test attestation` — `r7_intentspec_approval_with_source_fidelity_accepted` green on main.
