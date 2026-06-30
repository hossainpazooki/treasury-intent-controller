# BUILD CONTRACT — treasury-intent-controller (Go authorization gate, slice 1)

This is the SINGLE SOURCE OF TRUTH. Every build agent codes against THIS, not
against each other's files. Do not change any exported name, signature, or
package path defined here. Module path: `github.com/pazooki/treasury-intent-controller`.
Go 1.26. No external dependencies (stdlib only). Never run git.

This slice is the spec's **Authorization plane** (intent-controller, Go), built
greenfield. The gate reads NO artifacts: it receives criteria/thresholds/idempotency
as params (`IntentSpecParams`). The scorer (`/ml/evaluate`) and the reference
adapter are real interfaces here but driven by in-package fakes for tests; the
real Python scorer and the real COMPASS/TS adapter are LATER slices.

## Spec invariants this slice must make true by construction

1. **Single ACHIEVED authority.** The gate is the sole emitter of the `ACHIEVED`
   event; it is a single append-only event. The adapter acts ONLY after observing it.
2. **Tri-state scoring, fail-closed.** A criterion scores `Pass`, `Fail`, or
   `Unevaluable`. `allPassed` ⟺ every criterion `Pass`. ANY `Fail` or
   `Unevaluable` ⟹ not authorized. `Unevaluable` is logged distinctly and MUST
   NEVER collapse into pass.
3. **Stable vs volatile.** Stable criteria scored once (at declaration). Volatile
   criteria scored at declaration AND re-verified at the dispatch edge by the SAME
   gate before authorizing. A volatile criterion that is not `Pass` at re-verify ⟹
   `FAILED_AT_DISPATCH`, nothing dispatches.
4. **Idempotency by construction.** Key is required; empty key ⟹ refuse at
   declaration (`FAILED`, unevaluable: absent key). The key is reserved at the
   dispatch edge; a near-duplicate (same key, different intent hash) collides and
   is refused ⟹ `FAILED_AT_DISPATCH`. At-most-once holds on the settlement log.
5. **FAILED_AT_DISPATCH ⟹ no settlement event**, every time. Reachable ONLY from
   `VERIFYING` via the dispatch-edge path (volatile re-check fail OR idempotency
   collision). Never any other way.
6. **Determinism / replay.** Per-intent logical clock (seq 0,1,2…; never
   wallclock). IDs derived from `EpisodeSeed`. Same intent+seed ⟹ byte-identical
   event log, trajectory hash, and settlement event. Replay drives the adapter's
   RECOMPUTE path (calls `OnAchieved` again), never a re-read of a stored event.

---

## Package: internal/lifecycle  (OWNED: states.go = SCAFFOLD; transitions.go = build agent)

```go
package lifecycle

type State string

const (
	Declared         State = "DECLARED"
	Resolving        State = "RESOLVING"
	Active           State = "ACTIVE"
	Verifying        State = "VERIFYING"
	Achieved         State = "ACHIEVED"
	Failed           State = "FAILED"
	FailedAtDispatch State = "FAILED_AT_DISPATCH"
)

// IsTerminal reports whether s is one of ACHIEVED, FAILED, FAILED_AT_DISPATCH.
func (s State) IsTerminal() bool

// IsValidTransition reports whether from->to is permitted by the lifecycle graph.
//
// Permitted edges (and ONLY these):
//   DECLARED  -> RESOLVING
//   RESOLVING -> ACTIVE, FAILED
//   ACTIVE    -> VERIFYING, FAILED
//   VERIFYING -> ACHIEVED, FAILED, FAILED_AT_DISPATCH
// Terminal states have no outgoing edges.
// FAILED_AT_DISPATCH is reachable ONLY from VERIFYING (table-enforced); the gate
// further restricts it to the dispatch-edge path (code-enforced).
func IsValidTransition(from, to State) bool
```

## Package: internal/intent  (OWNED: types.go = SCAFFOLD)

```go
package intent

type Volatility string

const (
	Stable   Volatility = "stable"
	Volatile Volatility = "volatile"
)

type Phase string

const (
	Declaration Phase = "declaration" // first scoring pass
	Dispatch    Phase = "dispatch"    // volatile re-verify at the dispatch edge
)

type Criterion struct {
	Name       string
	Threshold  float64
	Volatility Volatility
}

type IdempotencyKey string

type IntentSpecParams struct {
	ActionClass      string // "payment" for slice 1
	Criteria         []Criterion
	IdempotencyScope string // e.g. "payer"
}

// Intent is pure data. It carries NO mutable lifecycle state; the gate's runtime
// owns the state machine. The three audit hashes are opaque to this slice.
type Intent struct {
	EpisodeSeed      string // determinism source; the intent ID derives from this
	Spec             IntentSpecParams
	IdempotencyKey   IdempotencyKey // required; "" is invalid
	RuleArtifactHash string         // opaque
	IntentSpecHash   string         // opaque
}

// ID is deterministically derived from EpisodeSeed (stable across runs).
func (i Intent) ID() string
```

## Package: internal/audit  (OWNED: eventlog.go = build agent)

```go
package audit

// Event is one entry in a per-intent append-only log. Seq is a logical clock
// (0,1,2,…), NEVER wallclock.
type Event struct {
	Seq    int
	Type   string // e.g. "DECLARED","VERIFYING","SCORED","RECHECK","UNEVALUABLE","IDEMPOTENCY_RESERVED","ACHIEVED","FAILED","FAILED_AT_DISPATCH"
	Detail string // free-form, deterministic (e.g. "balance:PASS")
}

type EventLog struct{ /* unexported */ }

func NewEventLog() *EventLog

// Append adds an event with the next sequence number and returns it.
func (l *EventLog) Append(typ, detail string) Event

// Events returns the events in order (a copy; callers must not mutate the log).
func (l *EventLog) Events() []Event

// TrajectoryHash returns a deterministic hash over the canonical serialization of
// the events (stdlib crypto/sha256 over a fixed, documented encoding). Same events
// ⟹ same hash, byte-for-byte.
func (l *EventLog) TrajectoryHash() string
```

## Package: internal/scoring  (OWNED: scorer.go = build agent)

```go
package scoring

import (
	"context"
	"github.com/pazooki/treasury-intent-controller/internal/intent"
)

type Score int

const (
	Pass Score = iota
	Fail
	Unevaluable
)

func (s Score) String() string // "PASS","FAIL","UNEVALUABLE"

// Scorer scores ONE named criterion for an intent in a given phase. It is the
// single scoring authority, invoked by the gate at declaration and again (volatile
// only) at the dispatch edge. A transport/timeout error MUST surface as Unevaluable
// (fail-closed), never as a silent pass.
type Scorer interface {
	Score(ctx context.Context, i intent.Intent, c intent.Criterion, phase intent.Phase) Score
}

// HTTPScorer calls the Python "/ml/evaluate" endpoint. For slice 1 it is wired but
// not exercised by the gate tests (the real Python scorer is a later slice). On any
// HTTP/transport/decode error or non-2xx it returns Unevaluable.
type HTTPScorer struct {
	Endpoint string // e.g. "http://localhost:9000/ml/evaluate"
	Client   *http.Client
}

func (h *HTTPScorer) Score(ctx context.Context, i intent.Intent, c intent.Criterion, phase intent.Phase) Score

// EvalRequest/EvalResponse define the /ml/evaluate JSON contract.
type EvalRequest struct {
	IntentID  string  `json:"intent_id"`
	Criterion string  `json:"criterion"`
	Threshold float64 `json:"threshold"`
	Phase     string  `json:"phase"`
}
type EvalResponse struct {
	Result string `json:"result"` // "PASS" | "FAIL" | "UNEVALUABLE"
}

// FakeScorer is the in-package test double used by the gate acceptance tests.
// Results is keyed by (criterion name, phase). A key absent from Results defaults
// to Pass (documented ergonomic default; tests set only the failing/unevaluable ones).
type FakeScorer struct {
	Results map[ScoreKey]Score
	Calls   []ScoreKey // appended on every Score call, in order, for call-count assertions
}
type ScoreKey struct {
	Criterion string
	Phase     intent.Phase
}

func (f *FakeScorer) Score(ctx context.Context, i intent.Intent, c intent.Criterion, phase intent.Phase) Score
```

## Package: internal/adapter  (OWNED: adapter.go = build agent)

```go
package adapter

import "github.com/pazooki/treasury-intent-controller/internal/intent"

// SettlementEvent is what the adapter records on ACHIEVED. Deterministic from the
// intent + key + seed.
type SettlementEvent struct {
	IntentID string
	Key      intent.IdempotencyKey
	Payload  string // deterministic, derived from intent (no wallclock, no randomness)
}

// Adapter records a settlement event on ACHIEVED. No network in slice 1.
type Adapter interface {
	OnAchieved(i intent.Intent) (SettlementEvent, error)
}

// ReferenceAdapter is the deterministic, idempotent reference adapter. It is
// idempotent on the declared key: a second OnAchieved with the same key returns the
// SAME event and records NO duplicate. Settlement returns the recorded events for
// at-most-once assertions.
type ReferenceAdapter struct{ /* unexported */ }

func NewReferenceAdapter() *ReferenceAdapter
func (a *ReferenceAdapter) OnAchieved(i intent.Intent) (SettlementEvent, error)

// Settlement returns all distinct settlement events recorded so far (one per key).
func (a *ReferenceAdapter) Settlement() []SettlementEvent
```

## Package: internal/idempotency  (OWNED: store.go = build agent)

```go
package idempotency

import "github.com/pazooki/treasury-intent-controller/internal/intent"

// Store tracks reserved idempotency keys. Reserve is the dispatch-edge gate.
type Store struct{ /* unexported */ }

func NewStore() *Store

// Reserve attempts to claim key for the given intent ID. It returns ok=true on a
// fresh key (now reserved), and ok=false on collision (key already reserved, by any
// intent). Empty key ⟹ ok=false (absent key is unevaluable).
func (s *Store) Reserve(id string, key intent.IdempotencyKey) (ok bool)
```

## Package: internal/gate  (OWNED: gate.go = build agent; acceptance_test.go = build agent)

```go
package gate

import (
	"context"
	"github.com/pazooki/treasury-intent-controller/internal/adapter"
	"github.com/pazooki/treasury-intent-controller/internal/audit"
	"github.com/pazooki/treasury-intent-controller/internal/intent"
	"github.com/pazooki/treasury-intent-controller/internal/lifecycle"
	"github.com/pazooki/treasury-intent-controller/internal/scoring"
)

// Result is the terminal outcome of one authorization.
type Result struct {
	Terminal       lifecycle.State          // ACHIEVED | FAILED | FAILED_AT_DISPATCH
	Reason         string                   // failed criterion names / "unevaluable:<crit>" / "idempotency-collision" / ""
	Events         []audit.Event            // the full append-only log
	TrajectoryHash string                   // hash over Events
	Settlement     *adapter.SettlementEvent // non-nil IFF Terminal == ACHIEVED
}

type Gate struct{ /* unexported: scorer, adapter, store */ }

func New(s scoring.Scorer, a adapter.Adapter, store *idempotency.Store) *Gate

// Authorize drives the full lifecycle deterministically and returns the terminal
// Result. Algorithm (every step appends to the event log via internal/audit):
//
//   1. DECLARED. If i.IdempotencyKey == "" -> FAILED, reason "unevaluable:absent-key"
//      (append UNEVALUABLE), no settlement. Return.
//   2. RESOLVING -> ACTIVE -> VERIFYING (each a logged transition; all
//      IsValidTransition-checked).
//   3. Declaration scoring: for EACH criterion (stable and volatile), call
//      scorer.Score(.., Declaration). Append a SCORED event "<name>:<score>".
//        - On Unevaluable: append UNEVALUABLE, set Terminal=FAILED,
//          reason "unevaluable:<name>", NO settlement. Return. (Fail-closed; never pass.)
//        - On Fail: collect the name. After all criteria, if any failed:
//          Terminal=FAILED, reason = joined failed names, no settlement. Return.
//   4. Dispatch edge (only if all criteria Passed at declaration):
//        a. For each VOLATILE criterion ONLY, call scorer.Score(.., Dispatch),
//           append RECHECK "<name>:<score>". If any not Pass ->
//           Terminal=FAILED_AT_DISPATCH, reason "volatile-recheck:<name>",
//           no settlement. Return. (Stable criteria are NOT re-scored.)
//        b. Idempotency reserve: store.Reserve(i.ID(), i.IdempotencyKey). On
//           collision (ok==false) -> append, Terminal=FAILED_AT_DISPATCH,
//           reason "idempotency-collision", no settlement. Return.
//           On success append IDEMPOTENCY_RESERVED.
//   5. Authorize: append the single ACHIEVED event. Terminal=ACHIEVED. Call
//      adapter.OnAchieved(i); set Settlement to the returned event. Return.
//
// Determinism: no wallclock, no map-iteration-order dependence in the log (iterate
// criteria in slice order). Replaying Authorize with a FRESH Gate (fresh store) on
// the same intent yields byte-identical Events/TrajectoryHash/Settlement.
func (g *Gate) Authorize(ctx context.Context, i intent.Intent) Result
```

`acceptance_test.go` (package gate) MUST cover, each as a distinct test, using
`scoring.FakeScorer`, `adapter.NewReferenceAdapter()`, `idempotency.NewStore()`:

- **Determinism/replay**: two independent Gates (fresh stores) over the same intent
  ⟹ equal `Events`, `TrajectoryHash`, and `Settlement` bytes. Assert `OnAchieved`
  actually ran (recompute path), not a re-read.
- **Fail-closed unevaluable**: for EACH criterion, FakeScorer Unevaluable at
  declaration ⟹ `FAILED`, never `ACHIEVED`, never hang; log contains `UNEVALUABLE`.
  Also: empty idempotency key ⟹ `FAILED` "unevaluable:absent-key".
- **Verification failure**: a criterion `Fail` at declaration ⟹ `FAILED` with that
  name; no settlement.
- **Volatile re-verify**: volatile criterion `Pass` at declaration, `Fail`/`Unevaluable`
  at dispatch ⟹ `FAILED_AT_DISPATCH`, no settlement. Assert a STABLE criterion is
  scored exactly once (no Dispatch call) and a VOLATILE one exactly twice (via
  `FakeScorer.Calls`).
- **Idempotency collision**: two intents, same key, different `IntentSpecHash`,
  SHARED store ⟹ first `ACHIEVED`, second `FAILED_AT_DISPATCH` "idempotency-collision";
  `ReferenceAdapter.Settlement()` has exactly ONE event for the key (at-most-once).
- **Terminal separation**: assert `Settlement == nil` for every `FAILED` and
  `FAILED_AT_DISPATCH` result; `ACHIEVED` reached only after the volatile re-check
  passed.

## Package: cmd/server  (OWNED: main.go = build agent)

```go
package main
```
A minimal HTTP server exposing the gate for the integration live-probe:
- `POST /v2/intents` — decode an intent (JSON mapping to `intent.Intent` +
  `IntentSpecParams`), run `gate.Authorize` with an `HTTPScorer` (endpoint from
  `TIC_SCORER_URL`, default unused in probe), a `ReferenceAdapter`, and a fresh
  `Store`; respond JSON `{terminal, reason, trajectory_hash, settlement?}`.
  For the probe, accept an optional `"force_scores"` map so the probe can drive a
  deterministic terminal WITHOUT a live Python scorer (documented test affordance).
- `GET /healthz` — `200 ok`.
Keep it small; the gate is the substance, the server is a thin shell.

## File → owner map (NO overlaps)

| File | Owner |
|---|---|
| `go.mod` | scaffold (done) |
| `internal/lifecycle/states.go` | scaffold (full impl) |
| `internal/intent/types.go` | scaffold (full impl) |
| `internal/lifecycle/transitions.go` (+`transitions_test.go`) | build |
| `internal/audit/eventlog.go` (+`eventlog_test.go`) | build |
| `internal/scoring/scorer.go` (+`scorer_test.go`) | build |
| `internal/adapter/adapter.go` (+`adapter_test.go`) | build |
| `internal/idempotency/store.go` (+`store_test.go`) | build |
| `internal/gate/gate.go` | build |
| `internal/gate/acceptance_test.go` | build |
| `cmd/server/main.go` | build |

## Hard rules for every agent

- Code against THIS contract, NOT each other's files. You own ONLY your file(s).
- Do not change any exported name/signature/package path above.
- stdlib only; no new modules; no network in tests.
- Never weaken or skip a test; never add a sleep to "fix" a race.
- Deterministic only: no wallclock, no `math/rand` without a fixed seed, no
  map-iteration-order leaking into the event log.
- Never run git.
