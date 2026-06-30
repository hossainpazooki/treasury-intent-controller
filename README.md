# treasury-intent-controller

The **authorization plane** of the ATLAS Treasury intent-gated action loop: a
deterministic Go gate that authorizes an irreversible treasury action (payment as
class 1) only when every declared criterion passes. It reads no artifacts — criteria,
thresholds, and the idempotency key arrive as params. Scoring is fail-closed
(`Unevaluable` never collapses to pass), volatile criteria are re-verified at the
dispatch edge, and `ACHIEVED` is a single append-only event emitted by this gate
alone; the adapter records a settlement event only after observing it. The whole run
is reconstructable from a logical-clock event log and replays byte-identically.

```mermaid
stateDiagram-v2
    [*] --> DECLARED
    DECLARED --> RESOLVING
    RESOLVING --> ACTIVE
    RESOLVING --> FAILED
    ACTIVE --> VERIFYING
    ACTIVE --> FAILED
    VERIFYING --> ACHIEVED: all criteria pass incl. volatile re-check<br/>+ idempotency reserved → settle
    VERIFYING --> FAILED: a criterion failed / unevaluable
    VERIFYING --> FAILED_AT_DISPATCH: volatile re-check failed<br/>OR idempotency collision (no value moved)
    ACHIEVED --> [*]
    FAILED --> [*]
    FAILED_AT_DISPATCH --> [*]
```

`FAILED_AT_DISPATCH` is reachable **only** from `VERIFYING` via the dispatch edge,
and it guarantees no settlement event exists — the audit reading is unambiguous:
`FAILED_AT_DISPATCH ⟹ no value moved`. (An absent idempotency key short-circuits to
a `FAILED` result at declaration — a terminal outcome, not a graph edge.)

## Invariants (enforced by construction, pinned by tests)

1. The gate is the **sole emitter** of the single, append-only `ACHIEVED` event.
2. **Tri-state, fail-closed** scoring: any `Fail` or `Unevaluable` ⟹ not authorized.
3. **Stable vs volatile**: stable criteria scored once (declaration); volatile scored
   at declaration *and* re-verified at the dispatch edge by the same authority.
4. **Idempotency by construction**: key required; reserved at the dispatch edge; a
   near-duplicate (same key, different intent hash) collides ⟹ `FAILED_AT_DISPATCH`,
   at-most-once on the settlement log.
5. **Determinism / replay**: per-intent logical clock, IDs from the episode seed, no
   wallclock; replay drives the adapter's **recompute** path (not a re-read).

## Layout

| Package | Responsibility |
|---|---|
| `internal/lifecycle` | states + the `validTransitions` graph |
| `internal/intent` | intent / criterion / spec-param data types |
| `internal/audit` | append-only event log + trajectory hash |
| `internal/scoring` | `Scorer` interface, `HTTPScorer` (`/ml/evaluate`), test `FakeScorer` |
| `internal/adapter` | settlement event + idempotent `ReferenceAdapter` |
| `internal/idempotency` | dispatch-edge key reservation store |
| `internal/gate` | the authorization engine + §12 acceptance tests |
| `cmd/server` | thin HTTP shell (`POST /v2/intents`, `GET /healthz`) |

`CONTRACT.md` is the authoritative type/signature contract the implementation codes
against.

## Build & test

```bash
go build ./...
go vet ./...
go test ./... -count=1
```

## Status

**Built and verified** — this is slice 1 of a larger program. The criterion scorer
(`/ml/evaluate`) and the reference adapter are real interfaces here, exercised by
in-package fakes; the production scorer (Python) and the execution-side adapter
(COMPASS/TypeScript) are separate slices, as is the ATLAS `IntentSpec` artifact type
that publishes the criteria this gate consumes.
