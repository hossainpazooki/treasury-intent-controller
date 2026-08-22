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
keygen (test key authority), trust root, attest + publish the five spec
drafts in `specs/` — builds and boots the gate with `INTENT_SCORER_URL` and
`INTENT_TRUST_ROOT`, runs the probe ladder (including a live signed
revocation, the declarant-SDK consumption probe, the LangChain and MCP
live legs, the reporting-gate probe, and the verifier-twin recompute), and
tears everything down.
Expected final line: `RESULT: 15/15 probes passed` — every probe asserts its terminal, so the
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
| 8 | Invoke a LangChain tool wrapped by `gate_tool` (the adapter), then repeat with the SAME args | first: tool body executes exactly once, result returned; second: `IntentRefused` `class=ALREADY_RESERVED`, body NOT re-fired | the adapter's LIVE leg (2026-08-18): a wrapped agent tool fires its consequence exactly once, and a refusal is a classified outcome, not an exception to debug. Its first live run failed the ladder at the verifier-recompute probe — the verifier refuted a reused episode seed the lab double could not see (`docs/learnings/2026-08-18-live-verifier-refutes-adapter-seed-reuse.md`) |
| 9 | Call a gated `fastmcp` tool through `IntentGateMiddleware`, repeat with the SAME args, then retry those args against a SECOND, independent middleware instance (same scope/run, its own server and counter) | first: tool body executes exactly once, result returned; second: `ToolError` carrying `class=ALREADY_RESERVED`, body NOT re-fired; third: `ALREADY_RESERVED` again with the second instance's counter still 0 | the MCP gate's LIVE leg (2026-08-20), and the stateless/multi-replica claim proven live: the idempotency key is DERIVED, not remembered, so a retry landing on a replica that shares no state is refused just the same (§2.7) |
| 10 | Front an UNGATED backend with `gated_proxy` under its own `run_id` and call it twice with the SAME args (plus a call omitting a property whose schema declares no default) | first: passes through, INNER counter 1; second: `ToolError` `class=ALREADY_RESERVED`, INNER counter still 1; the omitted-property call is refused BEFORE anything is declared (`strict_args`) | a server you do not own is gated without changing it — the INNER counter is what proves the refused call never reached the backend at all, and an unkeyable call earns no `ACHIEVED` (§2.7) |
| 11 | Submit a valuation (VALU) through the stdlib reporting adapter (`gate_submission`), repeat with the SAME identity but DIFFERENT report bytes, declare an erasure (EROR) under a spec carrying an unresolved human-judgment entry, then declare an unkeyable NEWT (an `as_of` on a type that does not key on it) — runs while the scorer is still live, since its `balance` criterion is scored at declaration | first: submits once, repository counter 1; second: refused `ALREADY_RESERVED`, counter still 1; third: refused by the GATE `class=HUMAN_JUDGMENT`, counter unchanged; fourth: `ReportUnkeyable` BEFORE any declaration | content-blind keying proven live — the second valuation is the duplicate a trade repository will NOT reject, refused by identity, not content; the erasure is refused by the plane's own abstention, not by adapter code (§2.7) |
| 12 | Kill the scorer, declare again | `FAILED` `unevaluable:` | fail-closed on outage, demonstrated live |
| 13 | Declare an attested-but-thin spec (zero criteria) | `FAILED` `unevaluable:empty-criteria` | attestation does not launder vacuity |
| 14 | Read `GET /v2/events?since=0` | exactly seven `ACHIEVED` — one per authorized key | emit-and-observe: consumers settle only from the feed, and no key ever settles twice |
| 15 | Run BOTH verifier twins (`bin/intent-verify` + `verifier/pyverifier/verify.py`) over the live `events.jsonl` | `RESULT: VERIFIED`, exit 0, reports byte-identical | an independent examiner re-derives every commitment — trajectory hashes on grants AND refusals, sequence contiguity, exactly-one-ACHIEVED — from the record bytes alone, with no trust in the gate |

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
