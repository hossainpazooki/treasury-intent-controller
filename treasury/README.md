# treasury — a demonstration deployment of the intent plane

This directory shows the intent plane doing its job in a concrete domain:
**payment controls**. The control role's key attests a spec — a balance
floor, an fx-rate floor — and publishes it into the spec store; an agent (the
declarant) then declares a payment intent citing that spec's content address
plus a declared idempotency key (the wire carries no criteria at all). The
plane's gate resolves the signed spec, scores its criteria against facts
served by the scorer, refuses anything unevaluable or unattested, reserves
the key at the dispatch edge, and emits exactly one durable `ACHIEVED` record
that a settlement consumer can observe. Value moves only after that record
exists.

Nothing in `core/` knows any of these words: *payment*, *balance*, *fx rate*
live only here (mechanically enforced by `TestCoreNeutrality`). The one
deliberate exception: the frozen wire fixtures in `core/contract/scorer/`
carry the criterion name `"balance"` from their capture date — regenerating
them with neutral names is a recorded roadmap follow-up.

## Quickstart

Run from the repo root:

    # Windows
    powershell -File treasury\quickstart.ps1
    # Linux / macOS / WSL
    ./treasury/quickstart.sh

Requirements: Go and Python 3.11+. The script creates the scorer venv on
first run, boots the scorer with `facts.json` (`balance: 250.0`,
`fx_rate: 1.30`) injected via `SCORER_FACTS_JSON`, then runs the plane leg —
keygen (test key authority), trust root, attest + publish the four spec
drafts in `specs/` — builds and boots the gate with `INTENT_SCORER_URL` and
`INTENT_TRUST_ROOT`, runs the probe ladder (including a live signed
revocation, the declarant-SDK consumption probe, and the verifier-twin
recompute), and tears everything down. Expected final line:
`RESULT: 11/11 probes passed` — every probe asserts its terminal, so the
demo doubles as a smoke gate.

## The probe ladder

| # | Probe | Expected | What it demonstrates |
|---|---|---|---|
| 1 | Declare a payment within limits (signed spec) | `ACHIEVED` | the full lifecycle against an attested spec and real scored facts; one durable record |
| 2 | Near-duplicate: same key, different spec | `FAILED_AT_DISPATCH` `idempotency-collision` | at-most-once by construction, not by adapter dedup |
| 3 | Declare over-threshold | `FAILED`, reason names `balance` (and NOT `unevaluable`) | criteria actually bind |
| 4 | Declare citing a hash nobody attested | `FAILED` `unevaluable:unattested-spec` | no signature, no scoring — P1's fail-closed floor |
| 5 | Revoke the within-limits spec (signed tombstone), declare against it | `FAILED` `revoked:quickstart-pull` | authority is revocable; the tombstone's ref is witnessed |
| 6 | Declare through the published declarant SDK (`bin/intent-declare`, derived deterministic key), then repeat with the SAME derived key | first: `class=PROCEED terminal=ACHIEVED`, exit 0; second: `class=ALREADY_RESERVED`, `same_key_retry_safe=false`, exit 1 | the embedding half of the two-sided sale runs live: derived keys make dedup real, and a collision is classified, not mysterious (§2.7) |
| 7 | Declare through the Python declarant twin (`declarant/pydeclarant`'s `Client`, its own derived key), then repeat with the SAME key | first: `class=PROCEED terminal=ACHIEVED`; second: `class=ALREADY_RESERVED`, `same_key_retry_safe=false` | the twin's first LIVE leg (2026-08-18): the shared golden bytes speak the same wire against the real gate, not only in a lab byte-compare (§2.7) |
| 8 | Kill the scorer, declare again | `FAILED` `unevaluable:` | fail-closed on outage, demonstrated live |
| 9 | Declare an attested-but-thin spec (zero criteria) | `FAILED` `unevaluable:empty-criteria` | attestation does not launder vacuity |
| 10 | Read `GET /v2/events?since=0` | exactly three `ACHIEVED` — one per authorized key | emit-and-observe: consumers settle only from the feed, and no key ever settles twice |
| 11 | Run BOTH verifier twins (`bin/intent-verify` + `verifier/pyverifier/verify.py`) over the live `events.jsonl` | `RESULT: VERIFIED`, exit 0, reports byte-identical | an independent examiner re-derives every commitment — trajectory hashes on grants AND refusals, sequence contiguity, exactly-one-ACHIEVED — from the record bytes alone, with no trust in the gate |

`force_scores` (the guarded test affordance) is deliberately absent from this
showcase — the gate here boots WITHOUT `INTENT_UNSAFE_FORCE_SCORES`, so the
wire would refuse it anyway. The plane's spec attestation runs live above at
test key authority (`key_authority: "test"` until ADR-0009); what remains
absent is the SCORER-side ATLAS artifact verification — the scorer runs with
the null resolver and says so on the wire
(`resolver=null: verification skipped`), which is the honest boundary between
this quickstart and the extended demonstration below.

## The chassis flow test

The quickstart proves the *gate* half live. The seats themselves — authoring,
authority, control — are covered by `go test ./treasury` (`flow_test.go`),
which takes the **maker-checker chain as the unit**: one linear test builds
the real seat binaries and execs them exactly as an operator would (no
production code was refactored for testability), running
author → keygen → root → attest → publish → promote → revoke and asserting
each refusal edge where it naturally occurs (a second keygen over the same
file, a publish under a foreign trust root, a second promotion of an
already-enforcing payload).

What it pins is `CONTRACT.md` §5.4 claim 16: passage-exact source pins and
shadow-by-default out of authoring; attested bytes identical to executed
bytes through the store; promotion as a NEW artifact rather than an edit in
place; revocation scoped to exactly one content address. The pins are over
behavior that already existed, so green-on-first-run proves nothing about
them — non-vacuity comes from three plant-red runs recorded in the claim row.

## The extended demonstration (separate environments required)

Everything below is **built and was live-verified on 2026-07-12**, but needs
more than Go+Python on one machine — it is documented, not scripted here:

- **Signed-artifact verification**: the scorer's `KeArtifactResolver` binds
  the `ke-artifact-py` wheel (Linux/WSL only) and verifies the ATLAS-published
  `IntentSpec` artifact by content hash before scoring. Env:
  `SCORER_ARTIFACT_DIR`, `SCORER_ATLAS_INPUTS_DIR`, `SCORER_EXPORTED_AT_UNIX`
  (all-or-nothing; partial config refuses to boot). The wheel pytest lane
  requires a sibling `regulatory-rule-engine` checkout containing
  `fixtures/artifacts/intentspec_payment/`.
- **Settlement consumption**: the COMPASS settlement consumer (separate repo)
  polls `GET /v2/events?since=<cursor>` by cron, recomputes settlement from
  the `ACHIEVED` trace fields `{intent_id, idempotency_key,
  rule_artifact_hash, intent_spec_hash, trajectory_hash, seq}`, and writes a
  keyed at-most-once ledger. The full loop — declare through settle, with a
  restatement replay and a real scorer-kill negative — ran green end-to-end
  on 2026-07-12 (see `docs/handoff/2026-07-13-atlas-treasury-payment-loop.md`).

## Facts

`facts.json` is the only place treasury facts exist. The scorer's built-in
default is an **empty** fact map — a scorer that knows nothing scores
everything UNEVALUABLE and the gate refuses everything. Fail-closed is the
default posture; the demonstration opts *into* facts.
