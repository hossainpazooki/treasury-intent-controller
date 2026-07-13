# Handoff — ATLAS treasury payment loop: reader slice + Stage C(b) + loop green

2026-07-13 (22:37 UTC). Newest commits this brief describes — measure drift
from these: **tic `6adff98`** (main, in sync with origin), **regulatory-rule-engine
remote main `86a77fc`** (PR #14 squash; the local RRE checkout sits on a NEW
branch `adr/0023-graph-export` this brief knows nothing about), **COMPASS
`bf2b330`** on `feat/treasury-stage-c-settlement` (**PR #7 OPEN**). Program
memory: `treasury-intent-loop`. Session: `71412dd0-24d9-4663-b2a9-0130c545ee1f`
("atlas-treasury-payment-loop").

## Current state

- **[built + merged] Reader slice** — `scorer/src/tis/resolver.py`
  `KeArtifactResolver` (lazy `ke_artifact_py` import; content-addressed `.kew`
  store indexed once via `from_bytes`, non-canonical files silently absent so
  their hashes fail closed at lookup; folded `verify_artifact` per requested
  hash; True only on verdict `verified` AND re-addressed hash == requested;
  hashless call refuses). All-or-nothing boot config in `__main__.py`
  (`TIS_ARTIFACT_DIR` + `TIS_ATLAS_INPUTS_DIR` + `TIS_EXPORTED_AT_UNIX`;
  partial ⇒ refuse to boot; none ⇒ NullResolver with the skip visible in
  `basis`) + `TIS_FACTS_JSON` demo-facts override. Committed by Hossain as
  `6adff98`, pushed.
  re-verify: Windows `cd scorer && .venv/Scripts/python -m pytest -q` → 41
  passed / 5 VISIBLE wheel-lane skips; WSL
  `wsl -e bash -lc 'cd /mnt/c/Users/hossa/dev/treasury-intent-controller/scorer && python3 -m pytest -q'`
  → 39 passed, ZERO skips (wheel lane executes).
- **[built + merged] ATLAS R7 kind-aware fix (ADR-0022, Accepted)** — before
  it, NO IntentSpec could verify or publish (R7 unconditionally demanded a
  ScenarioCoverage co-attestation; ADR-0021 §5's two-type set could never
  satisfy it). Kind-aware `co_attestation_types()` in
  `crates/ke-artifact/src/attestation.rs`; both arms' tests proven non-vacuous
  by mutation in a temp copy. Merged to RRE main via **PR #14** (26 checks
  green per Hossain's sign-off note in the ADR); schema doc §6B/§7 amended at
  acceptance.
  re-verify: `gh pr view 14 --json state,mergedAt` in regulatory-rule-engine →
  MERGED 2026-07-13T22:28:18Z; `cargo test -p ke-artifact --test attestation`
  green incl. `r7_intentspec_*`.
- **[built, PR OPEN] COMPASS Stage C(b)** —
  `src/features/treasury-settlement/` (gate wire types verbatim; fail-loud
  `HttpTreasuryGateClient`; keyed first-wins `SettlementLedger`
  (Memory + File impls; same-key-different-content = surfaced conflict, never
  overwritten; monotonic cursor; atomic tmp+rename persist; cold-start
  reload); `reconcileOnce` settles ONLY observed ACHIEVED, writes settlements
  BEFORE advancing the cursor, outage throws with cursor untouched;
  `decisionAgent` resolves the pinned signed IntentSpec via
  `provenanceForRegime('treasury_payments_v1')`, refuses unpinned, holds no
  dispatch handle) + routes `/treasury/{declare,reconcile,settlements}` +
  vercel.json cron (`*/5 * * * *`). Gate green at commit `bf2b330`:
  typecheck / lint / 272 tests / build.
  re-verify: `gh pr view 7` in cross-border-compliance-navigator (state) and
  `npm run typecheck && npm run lint && npm test` on the branch.
- **[built, probed live 2026-07-12] The full payment loop** — COMPASS declare
  → Go gate → WSL scorer (real wheel verify of golden `c7a36959…dc51`) →
  ACHIEVED in the durable feed → reconcile applied settlements carrying the
  full trace contract. Negatives probed live: unknown spec hash ⇒ FAILED
  `unevaluable:`; registry `Unknown` ⇒ verify False; cursor-rewind
  restatement ⇒ duplicates counted, ledger byte-identical; REAL scorer kill ⇒
  next declaration FAILED, zero new settlements.
  re-verify (the probe recipe, ~5 min): WSL scorer:
  `TIS_ARTIFACT_DIR=/mnt/c/Users/hossa/dev/regulatory-rule-engine/fixtures/artifacts TIS_ATLAS_INPUTS_DIR=~/tis-inputs TIS_EXPORTED_AT_UNIX=1750000000 TIS_FACTS_JSON='{"amount_under_ceiling":2000000,"fx_rate_within_band":1.10}' python3 -m tis`
  (`~/tis-inputs` = shared keydir/registry + IntentSpec-shaped policy/context —
  build per `scorer/tests/test_resolver.py::_intentspec_env`); Windows gate:
  `TIC_SCORER_URL=http://127.0.0.1:8000/ml/evaluate ./bin/tic.exe`; declare the
  golden spec at `POST /v2/intents` → expect ACHIEVED.
- **[not run] `scripts/contract-test.sh` locally** — its fail-fast guard
  correctly refused: the sibling platform checkout HEAD (`323c7f9`) has
  drifted from the pinned SOURCE.md SHA (`f73b940`). PR #14's CI ran the
  3-leg gate instead (green). Do not "fix" this by moving the platform
  checkout — it has uncommitted work.
- **[planned — next slices]** (1) durable KV/Postgres ledger adapter — the
  file ledger is local-durable ONLY; the Vercel cron cannot claim the
  at-most-once-across-cold-starts invariant in production until this exists;
  (2) resolver-extraction slice — criteria/thresholds read from the IntentSpec
  payload via `intent_spec()`/`iter_criteria()` (retires BOTH the ADR-0003
  float-threshold wire debt and COMPASS's `GOLDEN_PAYMENT_CONFIG` parity
  debt); (3) CI wheel-lane job (Linux, `TIC_ATLAS_DIR` pointing at an RRE
  checkout) so the wheel lane runs unskipped in CI; (4) binding-side
  kind-aware verification environment (see learnings) for mixed-kind
  requests; (5) live registry evidence for the resolver (today: static
  `registry.json` — fine for goldens, wrong for a live registry).

## Locked decisions

- **One-repo intent layer** (Hossain, 2026-07-08): tic holds gate + scorer +
  contracts + fixtures. Reason: his deployment concept — one repo per plane.
  (CONTRACT-SCORER §S.0 amendment records it.)
- **ACHIEVED transport = pull/reconcile by cursor** from the gate's durable
  `/v2/events`. Reason: spec §10 replay already requires the durable log;
  pull is the minimal satisfier. (Stage-C brief 2026-07-07.)
- **Reader = the existing `ke-artifact-py` wheel, injected behind
  `ArtifactResolver`** — never a new binding, never rebuilt on Windows
  (Linux-only by design). Reason: extraction must go through the same Rust
  codec as every other consumer.
- **ADR-0022 (Accepted 2026-07-13): R7 co-attestation is kind-selected** —
  IntentSpec ⇒ SourceFidelity only. Reason: ADR-0021 §5's attestation set has
  no scenario dimension; without this no IntentSpec verifies at all.
- **Resolver boot config is all-or-nothing, fail-loud** — a server configured
  to verify must never silently not-verify. Reason: silent downgrade to
  NullResolver would be a fail-open config path in a fail-closed system.
- **Agent criteria are app config bound to the pinned hash** (COMPASS
  `GOLDEN_PAYMENT_CONFIG`) *until* the resolver-extraction slice. Reason:
  COMPASS is verify-only and deliberately cannot read artifact payloads;
  recorded parity debt, not an oversight.
- **File ledger honesty**: `FileSettlementLedger` is the reference/local
  implementation; production durability requires the KV/Postgres adapter.
  Never present the cron as production-durable before that lands.

## Reuse map

- `scorer/src/tis/resolver.py` — the resolver seam; extraction slice extends
  it (the binding's `intent_spec()` / `iter_criteria()` accessors already
  exist in RRE `crates/ke-artifact/src/python.rs`, merged with Stage A).
- `scorer/tests/test_resolver.py` — `_intentspec_env()` synthesizes a
  kind-correct verification environment from the shared contract-inputs +
  manifest (the same procedure `emit-contract-inputs.rs` uses); `_StubBinding`
  is the Windows-runnable unit-lane double.
- WSL lanes: user-space wheel toolchain (see learnings entry) and
  `/usr/local/go` for the `-race` leg. `~/tis-inputs` in WSL home holds the
  built IntentSpec env from the live probe.
- COMPASS `src/features/treasury-settlement/` — ledger/reconcile/agent
  modules are dependency-injected and directly unit-testable; the KV adapter
  slots in behind the existing `SettlementLedger` interface, nothing else
  changes.
- The gate wire, probed shapes included, lives in COMPASS
  `src/features/treasury-settlement/types.ts` — mirror of tic `cmd/server`.

## Invariants

- **Fail-closed is total across the loop**: every failure — transport, 404,
  unknown criterion, absent fact, absent/rejected artifact, resolver config
  bug — lands `Unevaluable`/refusal, never a grant. Weakening any default
  branch breaks spec invariant 2.
- **`basis` never enters the audit log, the durable feed, or any hash.**
- **Settlement only from an OBSERVED ACHIEVED record**; a declare response is
  never a settlement input. Consumer writes settlements durably BEFORE
  advancing the cursor; ledger is first-wins keyed at-most-once; same-key
  conflicts are surfaced, never resolved silently.
- **The full intent-layer gate** = Go native + WSL `-race` + scorer pytest
  (both lanes); a wire-touching change is green only when all pass. COMPASS
  gate = typecheck/lint/test/build; RRE gate = per-gate branch → PR → Hossain
  merges, `cargo test --workspace` + CI contract test.
- **Verification environments are kind-shaped** — do not point one resolver
  config at mixed-kind stores and expect both to verify (it fails closed; see
  learnings).
- **Git**: Claude never commits/pushes in any of these repos; remote truth
  via `gh`, never local refs (git-guard blocks fetch).

## Open / next

1. **Merge COMPASS PR #7** (Hossain) — the only pending merge; everything
   else described here is on a main.
2. Then, in order of leverage: **KV/Postgres ledger adapter** (unlocks honest
   production cron), **resolver-extraction slice** (kills two recorded debts
   at once), **CI wheel-lane job**. No blocker on any of them beyond the PR.
3. Heads-up for pick-up: the RRE local checkout is on `adr/0023-graph-export`
   — a lane started after this session's work; this brief makes no claims
   about it.
