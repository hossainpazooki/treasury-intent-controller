# CONTRACT-SCORER.md — the `/ml/evaluate` seam (Go gate ⇄ Python scorer)

> **This file AMENDS `CONTRACT.md` and `CONTRACT-DURABILITY.md`. It lists DELTAS
> ONLY.** Everything not named here is unchanged. Where a symbol appears in more
> than one contract, the **most recent contract wins** (this one, for the scoring
> seam). Go side: module `github.com/pazooki/treasury-intent-controller`, Go 1.26,
> **stdlib only, no new modules**. Python side: a NEW sibling project (§S.4) — the
> stdlib-only rule does NOT apply there. Never run git.

The gate's scoring authority (`scoring.Scorer`) is real today but only exercised
by fakes: `HTTPScorer` exists, is wired to nothing, and `cmd/server` always
scores from the request's `force_scores`. This slice makes the seam real:

1. Harden the existing wire contract (`EvalRequest`/`EvalResponse`) — additive
   fields only, and a pinned fail-closed matrix on BOTH sides of the network.
2. A production-shaped Go client path in `cmd/server` (`TIC_SCORER_URL`), with
   `force_scores` preserved verbatim as the documented test affordance.
3. A NEW Python scorer service (FastAPI, `POST /ml/evaluate`) with the artifact
   reader (`ke-artifact-py`) injected behind a protocol — pure-Python testable on
   Windows, real reader on Linux/CI only.
4. Cross-language wire fixtures so the two sides cannot drift silently.

**Spec invariant 2 is the soul of this seam**: `Unevaluable` never collapses
into pass, and EVERY failure of the network, the service, or the facts is
`Unevaluable`. A scorer outage must only ever make the gate refuse.

---

## §S.0 Locked decisions (do not relitigate)

- The wire shape is the EXISTING `EvalRequest`/`EvalResponse` JSON, extended
  **additively** (new fields; nothing renamed, retyped, or removed).
- Tri-state on the wire is the closed string set `"PASS" | "FAIL" |
  "UNEVALUABLE"`. Any other value, absent field, or malformed body ⟹ the client
  scores `Unevaluable`.
- **Service errors are still evaluations**: for a well-formed request the service
  returns `200` with `"UNEVALUABLE"` (unknown criterion, missing fact, resolver
  failure, internal exception). Non-2xx is reserved for malformed requests
  (`400`) and infrastructure failure — the client maps those to `Unevaluable`
  anyway, so both paths fail closed.
- `EvalResponse.basis` is observability ONLY. It MUST NEVER enter the gate's
  audit log, the durable feed, or the `TrajectoryHash` (it is free-text and
  would poison determinism).
- Client timeout is `scoring.DefaultTimeout = 5 * time.Second`, wired by the new
  constructor; a timeout is `Unevaluable` like any other transport error.
- `cmd/server` scorer selection: `force_scores` present ⟹ forced scorer
  (unchanged, test affordance); else `HTTPScorer` on `TIC_SCORER_URL`; unset
  `TIC_SCORER_URL` ⟹ empty endpoint ⟹ every Score is `Unevaluable` ⟹ the gate
  refuses everything. **The zero-config server authorizes nothing.**
- The Python service lives at `C:\Users\hossa\dev\treasury-intent-scorer`
  (greenfield sibling; FastAPI + pydantic + pytest; Python 3.11+). It is the
  spec's ONE resolver+scorer service: the `ke-artifact-py` reader is INJECTED
  behind a protocol and is **absent on Windows-local by design** (the wheel only
  builds on Linux/CI — never rebuild a binding, per program memory).
- Reader calls run off the event loop (executor thread): `ke-artifact-py`'s
  `verify()` holds the GIL through crypto, and must not stall a concurrent
  `/ml/evaluate` (recorded GIL caveat).
- Gate determinism (spec invariant 6) is **conditional on scores** at this seam:
  given the same score per (criterion, phase), the gate's events/hash are
  byte-identical regardless of which Scorer produced them. Live facts are where
  nondeterminism is ALLOWED to enter; nothing else here may add any.
- The gate acceptance tests (`internal/gate/acceptance_test.go`) keep using
  `FakeScorer` and are NOT touched by this slice.

## §S.1 Wire protocol — `POST /ml/evaluate`

Request (Go marshals; Python parses; unknown fields are IGNORED by the service
for forward compatibility; the five slice-1 fields are required):

```json
{
  "intent_id":          "5193ff14a8ec15d6",
  "criterion":          "balance",
  "threshold":          100.0,
  "phase":              "declaration",
  "volatility":         "stable",
  "rule_artifact_hash": "opaque-or-absent",
  "intent_spec_hash":   "opaque-or-absent"
}
```

- `phase` ∈ `"declaration" | "dispatch"` (mirrors `intent.Phase`).
- `volatility` ∈ `"stable" | "volatile"` — NEW, so the service can log/route
  without inferring from phase.
- The two hashes are NEW, optional (`omitempty`), and OPAQUE to the wire: they
  exist so the resolver can verify the governing `IntentSpec` artifact before
  scoring, once Stage A lands. **When present and a resolver is configured,
  verification failure ⟹ `UNEVALUABLE`.** When absent or no resolver: skip
  verification (recorded in `basis`), score from facts.
- **Acknowledged debt (ADR-0003)**: `threshold` is `float64` on this wire while
  ATLAS IntentSpecIR thresholds are exact `ScalarValue` (no floats) — a lossy
  boundary, consciously deferred to the resolver-extraction slice (where the
  exact scalar actually crosses), not fixed here.

Response (`200` for every well-formed request):

```json
{ "result": "PASS", "basis": "balance=250.00 >= 100.00" }
```

- `result` required, closed set. `basis` optional free-text, observability only
  (see §S.0). Unknown response fields are ignored by the Go client.
- `400` for a malformed request body; `422` (FastAPI validation) is acceptable
  as-is — the client treats any non-2xx identically.

**Client fail-closed matrix (each row is a test in `scorer_test.go`):**

| Failure | Client result |
|---|---|
| connection refused / DNS / TLS | `Unevaluable` |
| timeout (`DefaultTimeout`) or ctx cancel | `Unevaluable` |
| non-2xx status (400, 422, 500, 503…) | `Unevaluable` |
| body not JSON / truncated | `Unevaluable` |
| `result` absent or outside the closed set | `Unevaluable` |
| empty `Endpoint` | `Unevaluable` |

**Service fail-closed matrix (each row is a pytest):**

| Condition | Service response |
|---|---|
| unknown criterion | `200 {"result":"UNEVALUABLE","basis":"unknown criterion"}` |
| fact source has no fact | `200 {"result":"UNEVALUABLE", ...}` |
| resolver configured + verify fails | `200 {"result":"UNEVALUABLE", ...}` |
| evaluator raises | `200 {"result":"UNEVALUABLE", ...}` (handler catches all) |
| malformed request | `400`/`422` (client maps to `Unevaluable`) |

## §S.2 Go deltas — `internal/scoring` (file: `scorer.go`, `scorer_test.go`)

```go
package scoring

// DefaultTimeout bounds one /ml/evaluate call. A slower scorer is Unevaluable.
const DefaultTimeout = 5 * time.Second

// NewHTTPScorer returns an HTTPScorer whose client times out at DefaultTimeout.
func NewHTTPScorer(endpoint string) *HTTPScorer

// EvalRequest — ADDITIVE fields only; existing four unchanged.
type EvalRequest struct {
	IntentID         string  `json:"intent_id"`
	Criterion        string  `json:"criterion"`
	Threshold        float64 `json:"threshold"`
	Phase            string  `json:"phase"`
	Volatility       string  `json:"volatility"`                   // NEW: "stable" | "volatile"
	RuleArtifactHash string  `json:"rule_artifact_hash,omitempty"` // NEW: opaque passthrough
	IntentSpecHash   string  `json:"intent_spec_hash,omitempty"`   // NEW: opaque passthrough
}

// EvalResponse — Basis is observability only; it MUST NOT enter the audit log.
type EvalResponse struct {
	Result string `json:"result"`
	Basis  string `json:"basis,omitempty"` // NEW
}
```

`HTTPScorer.Score` populates the three new request fields from the intent
(`c.Volatility`, `i.RuleArtifactHash`, `i.IntentSpecHash`); its mapping logic is
otherwise UNCHANGED (default branch stays `Unevaluable`). `Scorer`, `Score`,
`FakeScorer`, `ScoreKey`: untouched.

`scorer_test.go` grows the full client matrix above using `net/http/httptest`
(no live network): a table test over refused/timeout/non-2xx/garbage/unknown-
result/empty-endpoint, plus one happy-path test asserting the marshaled request
bytes contain all seven fields and that `Basis` is decoded but discarded.

## §S.3 Go deltas — `cmd/server` (file: `main.go`, `main_test.go`)

- Scorer selection per §S.0. The `HTTPScorer` (when selected) is constructed
  ONCE at boot from `TIC_SCORER_URL` and shared, like the stores; `force_scores`
  requests keep constructing their per-request forced scorer exactly as today.
- New `main_test.go` cases (existing ones untouched, per the §V2.6 discipline):
  no `force_scores` + unset `TIC_SCORER_URL` ⟹ terminal `FAILED`,
  reason `unevaluable:<first criterion>`; no `force_scores` + `TIC_SCORER_URL`
  pointing at an `httptest` scorer ⟹ terminal follows the scorer's answers.

## §S.4 NEW Python service — `treasury-intent-scorer/`

```
treasury-intent-scorer/
  pyproject.toml            # fastapi, pydantic, uvicorn; dev: pytest, httpx
  src/tis/models.py         # EvalRequest / EvalResponse (pydantic, extra="ignore")
  src/tis/evaluator.py      # evaluate(req, fact) -> "PASS"|"FAIL"|"UNEVALUABLE"
  src/tis/facts.py          # FactSource protocol + StaticFactSource(dict)
  src/tis/resolver.py       # ArtifactResolver protocol + NullResolver; ke-artifact-py
                            #   impl behind an import guard (Linux-only, executor-run)
  src/tis/app.py            # FastAPI: POST /ml/evaluate, GET /healthz
  tests/                    # pure-Python: matrix of §S.1, no wheel, no network
```

- `evaluate`: fact `>= threshold` ⟹ `PASS`; `< threshold` ⟹ `FAIL`; fact
  `None` or any exception ⟹ `UNEVALUABLE`. Deterministic given the same fact
  (`basis` formatting fixed-precision, no wallclock, no randomness).
- `FactSource.get(criterion, intent_id) -> float | None`. `StaticFactSource` is
  both the test double and the demo configuration; a live fact source is a LATER
  slice — do not fake one.
- `ArtifactResolver.verify(rule_artifact_hash, intent_spec_hash) -> bool`, run
  via `loop.run_in_executor` (GIL caveat). `NullResolver` (skip + basis note) is
  the Windows-local default. The `ke-artifact-py` implementation is written but
  imported lazily and **exercised only where the wheel exists (Linux/CI)** —
  its tests skip cleanly elsewhere, with the skip visible, never faked green.

## §S.5 Cross-language fixtures — `contract/scorer/` (in THIS repo)

Golden JSON pairs (`request-<case>.json` / `response-<case>.json`) for: pass,
fail, unevaluable-unknown-criterion, volatile-dispatch, hashes-present. The Go
side (`scorer_test.go`) asserts `EvalRequest` marshals byte-identically to each
request fixture and decodes each response fixture to the expected `Score`. The
Python side parses each request fixture (pydantic) and asserts its serialized
response equals the response fixture. Python locates the fixtures via
`TIC_CONTRACT_DIR` (default: the sibling checkout path); if absent, those tests
**skip visibly** — CI runs both repos together so the skip never hides drift.

## §S.6 File → owner map (NO overlaps)

| File(s) | Owner |
|---|---|
| `contract/scorer/*.json` (fixtures) | scaffold |
| `internal/scoring/scorer.go` + `scorer_test.go` | build-go-client |
| `cmd/server/main.go` + `main_test.go` (scorer wiring only) | build-go-server |
| `treasury-intent-scorer/**` | build-py-service |
| live two-process probe (gate + service) | integrate |

## §S.7 Hard rules

- Go: stdlib only, no new modules; all prior contracts' hard rules hold.
- No live network in ANY unit test — `httptest` (Go) / `TestClient` (Python).
- `basis` never enters `audit.Event.Detail`, the durable feed, or any hash.
- Never weaken the closed result set; the default branch of every mapping is
  `Unevaluable`.
- The Python service never imports from the Go repo and vice versa — the wire
  fixtures are the ONLY shared surface.
- Implemented-vs-planned stays visible: `NullResolver` and `StaticFactSource`
  are labeled as such in `basis`; wheel-dependent tests skip loudly, never fake.

## §S.8 Load-bearing claims (one skeptic each; non-vacuity by mutating a COPY)

1. **Client fail-closed, total**: every row of the client matrix yields
   `Unevaluable`. Probe: the httptest table test. Mutant: default branch → `Pass`.
2. **Service fail-closed, total**: every row of the service matrix yields
   `UNEVALUABLE`/4xx. Probe: pytest matrix. Mutant: unknown criterion → `PASS`.
3. **Wire agreement**: both sides accept every fixture with identical meaning.
   Probe: §S.5 tests green on both sides against the SAME fixture bytes.
   Mutant: rename one JSON key on one side.
4. **Determinism conditional on scores**: gate over `FakeScorer` vs gate over
   `HTTPScorer`+httptest returning the same scores ⟹ byte-identical `Events`
   and `TrajectoryHash` (and `basis` appears nowhere). Mutant: append basis to
   the SCORED detail.
5. **Stable-once / volatile-twice crosses the wire**: a counting httptest scorer
   sees exactly one `declaration` call per criterion and exactly one extra
   `dispatch` call per volatile criterion. Mutant: drop the phase guard.
6. **Live outage refuses, never grants**: two-process probe — gate `ACHIEVED`
   with the service up and facts passing; kill the service; the same intent
   (fresh key) ⟹ `FAILED`, `unevaluable:<criterion>`, no ACHIEVED record in the
   feed. Probe is the integrate step; the kill is real (`taskkill`), not mocked.
