# intent-plane

A domain-agnostic authorization layer for agentic deployments. Agents propose;
the plane's gate disposes — a deterministic core that decides whether a
declared intent is authorized, holds the sole authority to emit `ACHIEVED`
(written exactly once to a durable append-only feed), and never lets
*unevaluable* collapse into a pass. Nothing crosses the plane until the gate
says so — and everything the gate decides is recorded to be **re-derived
later by someone who does not trust it**.

```mermaid
flowchart LR
    A["agent declares<br/>an intent"] --> G{"gate scores it<br/>fail-closed"}
    G -->|"all criteria pass ·<br/>idempotency key fresh"| ACH["ACHIEVED<br/>exactly one durable record"]
    G -->|"any fail · any unevaluable ·<br/>duplicate key · unattested,<br/>revoked, or thin spec"| REF["refused<br/>no record — nothing settles"]
    ACH -->|"observed from the feed,<br/>never from a callback"| S["settle / re-verify"]

    classDef neutral fill:#e5e7eb,stroke:#6b7280,stroke-width:1.5px,color:#111827;
    classDef idem fill:#f59e0b,stroke:#b45309,stroke-width:3px,color:#111827;
    classDef good fill:#86efac,stroke:#15803d,stroke-width:2px,color:#111827;
    classDef bad fill:#fca5a5,stroke:#b91c1c,stroke-width:2px,color:#111827;
    class A,S neutral;
    class G idem;
    class ACH good;
    class REF bad;
```

**If you answer for agent actions after the fact** — audit, compliance, model
risk, a counterparty's diligence — this repo was built for your examination,
not just its own operation. Every decision leaves a record designed for
independent recomputation: a per-intent event log on a logical clock (no
wallclock anywhere), a trajectory hash over a length-prefixed byte encoding
with **no JSON canonicalization to argue about**, exactly one durable
`ACHIEVED` record per authorized action, and byte-frozen wire fixtures that
pin the cross-language seam. From the feed alone (`GET /v2/events?since=`)
you can re-derive the hashes, replay the lifecycle against the closed
transition graph, and count the grants — in your own language, against your
own copy, with no call into this code. Stated honestly: today that proves a
record is *self-consistent and its hashes recompute*; what it cannot yet
prove is listed below rather than hidden (the premise table's
**asserted, not enforced** rows, and `docs/ROADMAP.md`).

**If you own how your agents call tools** — the platform team wrapping an
agent runtime — you embed the gate once at the framework layer and every
agent inherits it: declare the intent, await the terminal (the synchronous
`POST /v2/intents` response *is* the terminal — no polling state machine to
build), proceed on `ACHIEVED` or surface the refusal. Refusal reasons are a
**closed, machine-parseable set** pinned by contract (`CONTRACT.md` §3.3 —
new cause classes must amend the table first), so the integration is a switch
over stable strings; settlement and observability come from polling the
durable feed by cursor, never from trusting the synchronous response.

```mermaid
flowchart LR
    subgraph RT["agent runtime — the platform team embeds once"]
        AG["agent proposes"] --> SDK["framework layer<br/>declare · await terminal ·<br/>proceed or surface"]
    end
    SDK -->|"POST /v2/intents"| G["gate<br/>sole ACHIEVED authority<br/>fail-closed · deterministic"]
    G -->|"synchronous terminal:<br/>ACHIEVED or a closed refusal set"| SDK
    G -->|"exactly one durable record<br/>per authorized action"| FEED[("append-only feed<br/>fsync per append · cursor seq")]
    FEED -->|"poll by cursor —<br/>settle only from observed ACHIEVED"| S["settlement consumer<br/>at-most-once ledger"]
    FEED -->|"re-derive hashes · replay lifecycle ·<br/>count grants — no trust in the gate"| V["verifier<br/>audit · compliance · model risk"]

    classDef neutral fill:#e5e7eb,stroke:#6b7280,stroke-width:1.5px,color:#111827;
    classDef durable fill:#93c5fd,stroke:#1d4ed8,stroke-width:2px,color:#111827;
    classDef star fill:#f59e0b,stroke:#b45309,stroke-width:3px,color:#111827;
    class AG,SDK,G,S neutral;
    class FEED durable;
    class V star;
    style RT fill:#f8fafc,stroke:#94a3b8,stroke-dasharray:6 4,color:#111827;
```

The wire carries **no criteria** — the field does not exist in the request DTO
(the old shape gets a loud 400). The **declarant** (the caller declaring the
intent) supplies the spec's content address plus the idempotency key and scope;
criteria, action class, and enforcement posture reach the gate ONLY through
`CONTRACT.md` §2.6 resolution — an envelope signed by the **attester**, verified
against the trust root, its payload hashing byte-for-byte to the claimed
address (roles of the plane: declarant / author / attester / gate, per
`CONTRACT.md` §1; key authority is test-grade until ADR-0009 lands). Scoring is
fail-closed: any `Fail` or `Unevaluable` denies authorization, and `Unevaluable`
never collapses into a pass — and the refusal is *shape-deep*: an unattested
hash, a revoked spec, an unknown posture, an unresolved human-judgment entry,
**zero criteria**, or an **unknown volatility** all refuse at resolution
(`unevaluable:unattested-spec`, `revoked:<ref>`, `unevaluable:empty-criteria`,
`unevaluable:invalid-volatility:<name>`, …) instead of vacuously granting —
attestation does not launder vacuity, and "no criterion failed" is never
satisfied by "no criterion existed". Volatile facts are re-checked at the
dispatch edge by the same authority immediately before authorizing — and so is
revocation: a spec pulled between verification and dispatch stops at the edge.

### The distinctive feature — exactly-once *by construction*

What makes two actions "the same action" is a **declared idempotency key, treated as
a first-class gate criterion** — not adapter-local dedup logic. The key is **required**
(an absent key is unevaluable and fails closed) and is **reserved at the dispatch edge**.
A near-duplicate — same key, one changed field, hence a *different* intent hash —
**collides on the key and is refused** (`FAILED_AT_DISPATCH`). So at-most-once holds on
the settlement log by construction, not by assertion. The amber nodes below are the
gate's checkpoints — the two idempotency checks flanking the spec-shape
check; the key's governance as a signed, expert-attested criterion
lives in the attested spec payload (`CONTRACT.md` §2.6, per ADR-0007) — this
gate consumes and enforces it.

```mermaid
flowchart TD
    D[DECLARED] -->|key required| K{idempotency<br/>key present?}
    K -->|no — absent key| F[FAILED]
    K -->|yes| TS{spec resolved?<br/>attested · not revoked ·<br/>posture known · criteria<br/>non-empty · volatility known}
    TS -->|"unattested · revoked ·<br/>thin — refuses, scorer<br/>never consulted"| F
    TS -->|verified| R[RESOLVING] --> A[ACTIVE] --> V[VERIFYING]
    V -->|criterion failed / unevaluable| F
    V -->|all criteria pass| VR{volatile re-check ·<br/>revocation re-check}
    VR -->|fact drifted / spec pulled| FD[FAILED_AT_DISPATCH]
    VR -->|holds — shadow posture| SH["SHADOW_RECORDED — durable record,<br/>fully scored, NOT authorized (ADR-0006)"]
    VR -->|holds — enforce posture| IDEM{{"reserve idempotency key<br/>declared · first-class criterion"}}
    IDEM -->|collision — duplicate action| FD
    IDEM -->|fresh key| ACH["ACHIEVED — one durable record<br/>consumers settle from it"]

    classDef neutral fill:#e5e7eb,stroke:#6b7280,stroke-width:1.5px,color:#111827;
    classDef idem fill:#f59e0b,stroke:#b45309,stroke-width:3px,color:#111827;
    classDef good fill:#86efac,stroke:#15803d,stroke-width:2px,color:#111827;
    classDef bad fill:#fca5a5,stroke:#b91c1c,stroke-width:2px,color:#111827;
    classDef durable fill:#93c5fd,stroke:#1d4ed8,stroke-width:2px,color:#111827;
    class D,R,A,V,VR neutral;
    class K,TS,IDEM idem;
    class ACH good;
    class SH durable;
    class F,FD bad;
```

Both `FAILED` and `FAILED_AT_DISPATCH` guarantee **no `ACHIEVED` record exists** in the
durable feed — so no consumer ever settles. The audit reading is unambiguous: a
duplicate or drifted intent ⟹ **no value moved**.

## Invariants (enforced by construction, pinned by tests)

1. The gate is the **sole emitter** of the single `ACHIEVED` record, fsynced to the
   durable feed before success; consumers act only after observing it.
2. **Tri-state, fail-closed** scoring: any `Fail` or `Unevaluable` ⟹ not authorized.
3. **Stable vs volatile**: stable criteria scored once (declaration); volatile scored
   at declaration *and* re-verified at the dispatch edge by the same authority.
4. **Idempotency by construction**: key required; reserved at the dispatch edge; a
   near-duplicate (same key, different intent hash) collides ⟹ `FAILED_AT_DISPATCH`,
   at-most-once on the settlement log.
5. **Determinism / replay**: per-intent logical clock, IDs from the episode seed, no
   wallclock; replay drives **recompute** (not a re-read). The feed's global cursor
   (`seq`) never enters the per-intent trajectory hash.
6. **Durability**: the event feed and the idempotency reservations survive process
   restart over the same `INTENT_DATA_DIR` (kill/restart proven — byte-identical
   events, same-key re-dispatch still refused).
7. **Thin-spec defense** (step 1b): zero criteria ⟹ `FAILED`
   `unevaluable:empty-criteria`; unknown volatility ⟹ `FAILED`
   `unevaluable:invalid-volatility:<name>`. Both refuse **before any scoring** —
   the scorer is never consulted.
8. **Core neutrality, pinned mechanically**: `core/` carries no domain
   vocabulary (`TestCoreNeutrality`), alongside the pinned import boundary and
   role vocabulary — amend `CONTRACT.md` first, then the pinned tables, never
   the reverse.

## Emit-and-observe

The gate's job ends at the durable `ACHIEVED` record. Settlement belongs to a
consumer that **pulls** the feed by cursor and recomputes — the gate never calls
out, and a crash on either side loses nothing:

```mermaid
flowchart LR
    subgraph TIC["intent plane — this repo"]
        G["gate<br/>sole ACHIEVED authority"] -->|"mirrors every event,<br/>stops at ACHIEVED"| FEED[("events.jsonl<br/>append-only · fsync per append<br/>global cursor seq")]
        FEED --> API["core/cmd/server<br/>GET /v2/events?since=cursor"]
        FEED -.- NOTE["kill/restart over the same INTENT_DATA_DIR:<br/>records + reservations recover from disk,<br/>seq continues gapless at prevMax+1"]
    end
    subgraph EXT["decision/execution plane — separate slice (COMPASS)"]
        C["settlement consumer<br/>cron · pull/reconcile"] -->|recompute, never re-read| LED[("keyed settlement ledger<br/>at-most-once")]
    end
    API -.->|"polled by cursor — the consumer initiates;<br/>the gate never calls out"| C

    classDef neutral fill:#e5e7eb,stroke:#6b7280,stroke-width:1.5px,color:#111827;
    classDef durable fill:#93c5fd,stroke:#1d4ed8,stroke-width:2px,color:#111827;
    classDef idem fill:#f59e0b,stroke:#b45309,stroke-width:3px,color:#111827;
    classDef note fill:#f3f4f6,stroke:#9ca3af,stroke-width:1.5px,stroke-dasharray:4 4,color:#111827;
    class G,API,C neutral;
    class FEED durable;
    class LED idem;
    class NOTE note;
    style TIC fill:#f8fafc,stroke:#94a3b8,color:#111827;
    style EXT fill:#f8fafc,stroke:#94a3b8,stroke-dasharray:6 4,color:#111827;
```

The amber ledger is the consumer-side twin of the amber checkpoints above: the
same declared key that gates dispatch keys the settlement ledger, so at-most-once
holds end to end.

## The premise, mapped to code (clause → enforcement)

The plane's premise clauses (P1–P7, from the intent-plane spec; normative role
vocabulary in `CONTRACT.md` §1) with the code that enforces each —
and an honest list of what is **asserted, not enforced**. Line numbers are as
of the 2026-08-03 repositioning; symbols outlive lines.

| Clause | Status | Enforcement |
|---|---|---|
| **P1** one signed object — what the attester signed is what the gate executes | **enforced (gate-side, test key authority)** | The wire has NO criteria field (`core/cmd/server/main.go` `specDTO`; the old shape 400s via `DisallowUnknownFields`). Criteria reach the gate ONLY through §2.6 resolution: ed25519 envelope verification + content-address equality (`plane/store.go` `Resolve`); an unattested hash refuses before any scoring (`gate.go` step 1a3, `TestUnattestedSpecRefuses`). Byte-for-byte is a hash equality (`TestTamperedPayloadRefuses`). Key authority is TEST-GRADE until ADR-0009 (R1). |
| **P2** artifacts are the only crossings | enforced gate-side | Four routes only (`core/cmd/server/main.go:241` `newMux`) + the durable feed; package set and import adjacency pinned mechanically (`core/internal/contractcheck/boundary_test.go` `TestImportBoundary`). |
| **P3** authority is key possession | **partially enforced: code graph in-repo; deployment graph asserted** | Key operations live in the APPLICATION, never the SDK: `treasury/authority` is the sole signing seat and only `treasury/control` may import it in production (`TestKeyPossessionBoundary`, a name-free rule — any `<tree>/authority` is importable only from `<tree>/control`); the core imports no application package at all (`TestImportBoundary`), so the gate, the plane artifact, and the drafting chassis structurally cannot reach a signing seam — an import-graph fact, not a review promise. What the graph pins is access to that seam; stdlib crypto stays reachable by any Go code, so "cannot sign at all" is the DEPLOYMENT half (workload identity, R2) and remains asserted; do not claim it. |
| **P4** fail-closed twice | enforced | Tri-state scoring, every transport/decode/non-2xx error ⇒ `Unevaluable` (`core/internal/scoring/scorer.go:70`); dispatch-edge re-verify (`core/internal/gate/gate.go:217` step 4a); distinct `FAILED_AT_DISPATCH` terminal (`core/internal/lifecycle/transitions.go`). |
| **P5** one byte-exact event | enforced | The single ACHIEVED event and its durable record are one emit (`core/internal/gate/gate.go:255` step 5); byte-identity pinned by `TestDeterminismReplay`. |
| **P6** abstention is a success state | enforced | `Unevaluable` is a first-class score, never a pass; AND the authoring chassis routes deliberately-unquantified obligations to `human_judgment` entries the gate refuses (`unevaluable:human-judgment:<name>`, `TestHumanJudgmentRefuses`) — an invented number cannot replace a human decision. The drafting INTELLIGENCE that fills the authoring seat is not in this repo; the chassis around it is. |
| **P7** unevaluable-shaped absence | enforced | Empty criteria and unknown volatility refuse at resolution (`core/internal/gate/gate.go:142` step 1b, `TestFailClosedEmptyCriteria` / `TestFailClosedInvalidVolatility`); absent key refuses at declaration; unknown scorer result strings ⇒ `Unevaluable` (`core/internal/scoring/scorer.go:110`). Bounds: the *thinned* set (fewer criteria than the source requires) is ATLAS-side; *semantic* volatility mislabeling is authoring/attestation-side. |

Known production-posture gaps, recorded rather than hidden (`docs/ROADMAP.md`):
`force_scores` is now GUARDED (`INTENT_UNSAFE_FORCE_SCORES=1` at boot, else a
loud 400) and witnessed (`scorer_id` on every SCORED/RECHECK feed record), but
remains a total scoring bypass wherever that flag is set; key authority is
test-grade (envelopes carry `key_authority: "test"`) until ADR-0009 lands; the
deployment-graph half of P3 (workload identity, R2) is asserted, not built;
the feed read surface is unauthenticated by design (emit-and-observe).

## See it work — the treasury demonstration

The `treasury/` directory is a demonstration deployment of the intent plane:
payment controls over static facts (a balance, an fx rate). One command boots
the real gate and the real scorer, then runs a narrated, self-asserting probe
ladder — the full plane: keygen → attest → publish, then authorization against
a SIGNED spec, idempotency collision, a binding criterion, an unattested-hash
refusal, a signed revocation, a live scorer outage (fail-closed), the
attested-but-thin refusal, the declarant-SDK consumption probe (declare
through the published `declarant/` package: derived key ⇒ `PROCEED`,
same-key re-declare ⇒ `ALREADY_RESERVED`), and the recompute probe (BOTH
verifier twins re-derive every commitment from the feed bytes alone and must
agree byte-for-byte — 10 probes):

    # Windows
    powershell -File treasury\quickstart.ps1
    # Linux / macOS / WSL
    ./treasury/quickstart.sh

Expected final line: `RESULT: 10/10 probes passed`. The narrative, the probe
ladder, and the extended demonstration (signed-artifact verification,
settlement consumption) are documented in `treasury/README.md`.

## Project structure

```
intent-plane/
├── CONTRACT.md            # the single current-state contract (§1–§10)
├── core/                  # the plane itself — carries no domain vocabulary
│   │                      #   (mechanically gated: TestCoreNeutrality)
│   ├── cmd/server/        # HTTP shell — the 4 routes, INTENT_* env
│   ├── internal/          # gate · lifecycle · audit · durable feed · scoring ·
│   │                      #   idempotency · contractcheck (test-only pins)
│   ├── scorer/            # Python resolver+scorer service — SCORER_* env
│   ├── contract/scorer/   # golden wire fixtures — byte-frozen, cross-language
│   └── contract/feed/     # golden feed fixtures + tampered mutant + frozen
│                          #   reports (§9.1) — the verifier twins' conformance
├── plane/                 # the signed artifact: envelope, spec payload, store,
│                          #   resolver (verification ONLY — the SDK holds no keys)
├── verifier/              # the independent examiner: Go pkg + intent-verify CLI
│   └── pyverifier/        #   + its Python twin — imports NOTHING outside its
│                          #   tree (§7.1); tri-state VERIFIED/REFUTED/UNVERIFIABLE
├── declarant/             # the embedding SDK (born SDK-side, consumed back here —
│                          #   ADR-0011): exact wire marshal, DeriveKey, total
│                          #   terminal classification, 500-edge feed consult (§2.7)
├── treasury/              # THE APPLICATION built on the SDK — its seats and its demo:
│   ├── authority/         #   EVERY private-key operation; only treasury/control
│   │                      #   may import it (TestKeyPossessionBoundary)
│   ├── control/           #   attest · publish · revoke · promote (the sole key holder)
│   ├── authoring/         #   drafting chassis — pins passages, surfaces unknowns,
│   │                      #   routes judgment calls to humans; holds no keys
│   └── ...                #   facts, specs, probes, quickstarts
└── docs/                  # ROADMAP, ADRs, handoff briefs, learnings, research, design docs
```

| Path | Responsibility |
|---|---|
| `core/internal/lifecycle` | states + the `validTransitions` graph |
| `core/internal/intent` | intent / criterion / spec-param data types |
| `core/internal/audit` | append-only event log + trajectory hash |
| `core/internal/durable` | durable JSONL event feed: `GlobalSeq`, fsync-per-append, restart recovery |
| `core/internal/scoring` | `Scorer` interface, `HTTPScorer` (`/ml/evaluate`), test `FakeScorer` |
| `core/internal/adapter` | **test-only** reference settlement consumer (recompute path in replay tests) |
| `core/internal/idempotency` | dispatch-edge key reservation store (in-memory + durable file-backed) |
| `core/internal/gate` | the authorization engine + the acceptance suite pinning the `CONTRACT.md` §5 invariants |
| `core/internal/contractcheck` | test-only: pins the import-graph boundary (§7), the role vocabulary (§1), and core neutrality (§5.1 invariant 8; the fixture exemption lives in §9) per `CONTRACT.md` |
| `core/cmd/server` | HTTP shell: `POST /v2/intents`, `GET /v2/events`, `GET /v2/intents/{id}/events`, `GET /healthz`; state under `INTENT_DATA_DIR`; live scorer from `INTENT_SCORER_URL` (unset = refuse everything) |
| `core/scorer/` | the Python resolver+scorer service (`POST /ml/evaluate`, FastAPI) — see `core/scorer/README.md` |
| `core/contract/scorer/` | golden wire fixtures — the byte-level seam both sides test against |
| `plane/` | the signed artifact: DSSE-shaped envelope, spec payload, content-addressed store, hybrid resolver, revocation tombstones — verification only; with `core/`, this is the whole SDK |
| `treasury/authority/` | application seat: every private-key operation (keygen, attest, tombstone) — production-importable ONLY from `treasury/control` |
| `treasury/control/` | application seat, CLI: keygen · root · attest · publish · revoke · promote (promotion = new attestation, new hash) |
| `treasury/authoring/` | application seat, CLI: deterministic drafting chassis — source pins, named unknowns, human-judgment routing; holds no keys by import graph |
| `treasury/` | the application built on the SDK: the seats above plus quickstart, specs, probes, facts |
| `CONTRACT.md` | the plane's contract — see below |

`CONTRACT.md` is the single current-state contract — roles, wire, lifecycle,
algorithm, invariants, boundary, scorer seam, fixtures (consolidated
2026-08-03; the prior amendment chain lives in git history).

## Build & test

```bash
go build ./...
go vet ./...
go test ./... -count=1
go test ./... -count=1 -race   # needs cgo; on a Windows host without a C compiler, run via WSL
```

The Python scorer has its own gate (see `core/scorer/README.md`):

```bash
cd core/scorer && .venv/Scripts/python -m pytest   # unit + service matrix + wire fixtures
```

And the treasury demonstration doubles as the third gate — a live two-process
smoke over the real gate and the real scorer:

```bash
powershell -File treasury\quickstart.ps1   # Windows
./treasury/quickstart.sh                   # Linux / macOS / WSL
# expected final line: RESULT: 10/10 probes passed
```

## Status

**Built and verified** — slice 1, the durability + emit-and-observe refactor,
and the live scoring seam. The gate stops at appending `ACHIEVED`; settlement
happens only in a consumer observing the durable feed (test-only reference
consumer in-repo). The criterion scorer (`/ml/evaluate`) is live end-to-end:
`core/cmd/server` selects the shared `HTTPScorer` from `INTENT_SCORER_URL`
(zero-config refuses everything; `force_scores` remains the test affordance),
and the Python service in `core/scorer/` answers it per `CONTRACT.md` §8,
verified two-process with a real service kill. This repo is the **testing
monorepo** for the intent plane: the full stack — gate, scorer, contract,
wire fixtures, the treasury application seats and demonstration, and the
concept-discussion chat (`tic-concept-chat/`) — lives and evolves here; the
published minimal SDK (core + plane only) is `github.com/hossainpazooki/intent-plane`,
and core changes are ported there once they settle. Still separate: the settlement consumer
(COMPASS/TypeScript) and the wheel-backed artifact reader inside `core/scorer/`
(`ke-artifact-py` — built and live-verified 2026-07-12, but Linux/CI-only:
its test lane skips visibly on hosts without the wheel);
the ATLAS `IntentSpec` artifact type is merged upstream (ADR-0021, canon-5)
but is RETIRED as a criteria source (ADR-0007): criteria reach this gate only
through §2.6 spec resolution; `rule_artifact_hash` keeps pointing at the
upstream rule artifact as provenance.
