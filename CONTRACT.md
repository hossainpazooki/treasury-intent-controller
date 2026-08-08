# CONTRACT — the intent plane

This is the SINGLE SOURCE OF TRUTH for the intent plane. Code is written against
THIS document, not against other code. Do not change any exported name,
signature, package path, wire key, route, or normative rule fixed here without
amending this document FIRST.

- Module path: `github.com/hossainpazooki/intent-plane`. Go 1.26.
- **Go side: stdlib only — no external dependencies, no new modules.** The
  stdlib-only rule does NOT extend to the Python scorer service (§8).
- No network in any unit test; all test IO under `t.TempDir()`.
- Build agents never run git.

The plane is a domain-agnostic authorization layer: a declarant declares an
intent, the gate decides deterministically whether it is authorized, and the
decision is emitted exactly once to a durable append-only feed. The gate settles
nothing itself (**emit-and-observe**); a downstream consumer settles from the
feed. The `treasury/` directory in this repo is a demonstration deployment of
the plane, not part of it.

---

## §1 Roles & vocabulary

### §1.1 Actor roles

Four roles, and only these, for actors in normative text:

| Role | Meaning |
|---|---|
| **declarant** | the caller that declares an intent (`POST /v2/intents`) and consumes verdicts / the completion feed |
| **author** | the drafting function (authoring plane); proposes IntentSpecs, holds no keys, cannot sign/publish/activate |
| **attester** | the human author of record; what they sign is what the gate is meant to execute |
| **gate** | this repo's deterministic core; sole ACHIEVED authority |

Pre-existing non-actor senses (HTTP "client", Go-doc "caller", build-meta
"agent"/"owner") stay as they are — they denote no actor role. Mechanically
enforced by `core/internal/contractcheck/vocab_test.go` (required presence +
forbidden list).

**Role seats in code (2026-08-05 layering ruling; supersedes the plane-roles
amendment's seating):** the core is a minimal SDK for agentic deployments and
hosts NO human-authority seats. `core/` is the gate; `plane/` is the boundary
artifact — envelope, payload, spec store, resolver, **verification only**:
the core verifies what applications sign. The seats live in the APPLICATION
built on the core — in this repo, `treasury/`: the author chassis is
`treasury/authoring`; the attester's seat — the only production importer of
`treasury/authority`'s key operations — is `treasury/control` (attest,
publish, revoke, promote); `treasury/authority` holds every private-key
operation. Declarants are external agents on the wire. The core structurally
cannot sign and never imports `treasury/*` (`TestImportBoundary`,
`TestKeyPossessionBoundary`): "layers bind to the artifact, never to each
other" is the pinned import graph.

### §1.2 plane / gate / interface

Three words, each answering a different question; any sentence self-selects its
term:

- **plane** — *where does it sit?* The architectural position among peers: "the
  intent plane" (= gate + scorer + contracts, everything this repo ships),
  beside the authoring plane, the artifact plane (ATLAS), and the settlement
  plane (COMPASS). A "plane crossing" is a signed artifact moving between them.
  Never a code component — nothing inside the repo is "a plane."
- **gate** — *what decides?* The deterministic enforcement component within the
  plane, and the fourth role above (role and component are deliberately the same
  word — the role IS the deciding component). Use wherever agency appears: the
  gate refuses, emits, holds sole ACHIEVED authority. Corollary: the gate is
  *smaller than* the plane — the scorer is in the plane but not in the gate, so
  **the gate consults the scorer**, never "the gate scores facts."
- **interface** — *what do you code against?* Reserved for the contract surface
  only: the four routes, the wire DTOs, the JSONL feed shape, the `/ml/evaluate`
  seam, the pinned package adjacency, the role vocabulary itself. Always
  lowercase and descriptive ("the plane's interface"). This document is what
  *states* the interface.

The pre-repositioning proper noun for the contract surface is **retired**;
`TestRetiredProperNouns` pins it at zero in `README.md` and this file.

### §1.3 Public terminology

**`ACHIEVED` is the public API term** for the gate's success terminal — wire
values (`terminal`, feed record `type`, accepted `?type=` query), field names
(`achieved_seq`), exported identifiers, and the cross-repo trace contract all
speak it. `COMPLETED` is not used anywhere in this repo and MUST NOT be
introduced; buyer-facing prose yields to the wire, not the reverse.

### §1.4 Mechanization

| Gate | Test | Pins |
|---|---|---|
| vocabulary presence | `TestRoleVocabularyPresent` | `README.md` and `CONTRACT.md` speak declarant / attester / gate |
| forbidden actor nouns | `TestForbiddenActorNouns` | zero occurrences across `.go` / `.md` / `.py` |
| retired proper noun | `TestRetiredProperNouns` | zero occurrences in `README.md` / `CONTRACT.md` |
| import boundary | `TestImportBoundary` | §7 package set + adjacency |
| core neutrality | `TestCoreNeutrality` | no domain vocabulary under `core/` (§9 exemption) |

---

## §2 The interface

The gate's ONLY public surface, in full: four HTTP routes, plus two non-HTTP
wire surfaces (the durable feed's JSONL record format, §2.3, and the
`/ml/evaluate` scorer seam, §2.4). No Go symbol is public API: every package
except `core/cmd/server` lives under `core/internal/`.

### §2.1 HTTP routes

| Route | Purpose |
|---|---|
| `POST /v2/intents` | declaration in; verdict record out (`terminal`, `reason`, `trajectory_hash`, `achieved_seq`) |
| `GET /v2/events?since=&type=` | the completion feed, cursor read — a **by-design gate-free read** (emit-and-observe); note it is unauthenticated (deployment posture, see `docs/ROADMAP.md` findings) |
| `GET /v2/intents/{id}/events` | per-intent records, ascending `intent_seq` |
| `GET /healthz` | `200 "ok"` (`text/plain`) |

`core/cmd/server` is a thin shell; the gate is the substance. Routes are wired
with Go 1.22+ method patterns; the per-intent route reads `id` via
`r.PathValue("id")`.

**`GET /v2/events`** — `since` parses to int (absent/blank ⟹ 0, which returns
everything); optional `type` (e.g. `ACHIEVED`) filters by record type. Returns
`feed.Since(since, type)` serialized as the raw `durable.Record` JSON:

```jsonc
// 200 application/json
{
  "events": [
    {"seq": 6, "intent_seq": 0, "intent_id": "ab12…", "type": "DECLARED", "detail": "ab12…"},
    {"seq": 12, "intent_seq": 7, "intent_id": "ab12…", "type": "ACHIEVED", "detail": "ab12…",
     "idempotency_key": "key-1", "rule_artifact_hash": "rule-…", "intent_spec_hash": "…",
     "trajectory_hash": "…"}
  ],
  "next_since": 12   // max GlobalSeq returned, or the input `since` if none returned
}
```

**`GET /v2/intents/{id}/events`**:

```jsonc
// 200 application/json
{ "intent_id": "ab12…", "events": [ /* durable.Record objects, ascending intent_seq */ ] }
```

**Wire shape rule:** the objects inside `events[]` are the `durable.Record` JSON
verbatim (the tags in §2.3 ARE the wire contract); do not re-tag them in a DTO.

### §2.2 Declaration DTOs

**2026-08-04 plane-roles amendment:** the wire carries NO criteria, no action
class, and no posture. Those live ONLY in the attested spec payload and reach
the gate through §2.6 resolution. `DisallowUnknownFields` makes the old shape
a loud 400 (`json: unknown field "criteria"`) — P1 is closed at the type
level: the field a declarant would smuggle criteria through does not exist.

```go
package main // core/cmd/server

// specDTO carries ONLY the declarant-owned spec field.
type specDTO struct {
	IdempotencyScope string `json:"idempotency_scope"`
}

// forceScore carries the forced result for a single criterion, per phase. An
// empty string means "unspecified" and defaults to Pass.
type forceScore struct {
	Declaration string `json:"declaration"` // "PASS" | "FAIL" | "UNEVALUABLE" | ""
	Dispatch    string `json:"dispatch"`    // "PASS" | "FAIL" | "UNEVALUABLE" | ""
}

type intentRequest struct {
	EpisodeSeed      string                `json:"episode_seed"`
	IdempotencyKey   string                `json:"idempotency_key"`
	RuleArtifactHash string                `json:"rule_artifact_hash"`
	IntentSpecHash   string                `json:"intent_spec_hash"` // content address of the attested payload
	Spec             specDTO               `json:"spec"`
	SpecEnvelope     json.RawMessage       `json:"spec_envelope,omitempty"` // hybrid wire path (§2.6)
	ForceScores      map[string]forceScore `json:"force_scores"` // GUARDED (§2.5)
}

// intentResponse: the gate no longer settles, so there is no settlement field;
// achieved_seq is >=1 iff the terminal is ACHIEVED.
type intentResponse struct {
	Terminal       string `json:"terminal"`
	Reason         string `json:"reason"`
	TrajectoryHash string `json:"trajectory_hash"`
	AchievedSeq    int    `json:"achieved_seq,omitempty"` // >=1 iff ACHIEVED
}

// eventsResponse wraps the raw durable.Record objects (no re-tagging DTO).
type eventsResponse struct {
	Events    []durable.Record `json:"events"`
	NextSince int              `json:"next_since"` // max GlobalSeq returned, or the input since
}

type intentEventsResponse struct {
	IntentID string           `json:"intent_id"`
	Events   []durable.Record `json:"events"`
}
```

The decoder rejects unknown fields (`dec.DisallowUnknownFields()`); a decode
failure is `400`. An `Authorize` error is `500`.

**`force_scores`** is the documented test affordance, preserved verbatim: a
request carrying it scores through the inline `forceScorer` (criterion →
`{declaration, dispatch}` result), so a probe can drive any terminal without a
live scorer. A missing criterion, or a missing/blank/unrecognized result for the
phase, defaults to `Pass`.

> **Production-posture note (recorded, not closed).** `force_scores` is a
> wire-reachable **total scoring bypass with no env/build/auth guard**. It
> qualifies "unevaluable never passes", "fail-closed twice", and "artifacts are
> the only crossings" in any production claim, until guarded. Removing or
> guarding it is a contract change; this document records the gap, it does not
> close it. See `docs/ROADMAP.md`.

### §2.3 The durable feed record (JSONL)

One physical file `<dir>/events.jsonl`. Every gate event for every intent is
mirrored here with a `GlobalSeq`. This is the shared substrate every consumer
reads.

```go
package durable // core/internal/durable

// Record is one durable, append-only event line (JSONL). Field order below IS the
// on-wire and on-disk order. GlobalSeq (json "seq") is monotonic across ALL
// intents; IntentSeq (json "intent_seq") is the per-intent logical clock, copied
// UNCHANGED from audit.Event.Seq. The four trace fields are populated ONLY on the
// ACHIEVED record (omitted otherwise). "seq" is always >=1; "intent_seq" may be 0
// (the DECLARED event), so it is NOT omitempty.
type Record struct {
	GlobalSeq        int    `json:"seq"`
	IntentSeq        int    `json:"intent_seq"`
	IntentID         string `json:"intent_id"`
	Type             string `json:"type"`
	Detail           string `json:"detail,omitempty"`
	ScorerID         string `json:"scorer_id,omitempty"`          // SCORED/RECHECK only
	IdempotencyKey   string `json:"idempotency_key,omitempty"`    // SHADOW_RECORDED + ACHIEVED
	RuleArtifactHash string `json:"rule_artifact_hash,omitempty"` // SHADOW_RECORDED + ACHIEVED
	IntentSpecHash   string `json:"intent_spec_hash,omitempty"`   // SHADOW_RECORDED + ACHIEVED
	TrajectoryHash   string `json:"trajectory_hash,omitempty"`    // SHADOW_RECORDED + ACHIEVED
}
```

`scorer_id` ("forced" | "live") witnesses WHICH scoring authority answered a
SCORED/RECHECK event, so a forced grant is never byte-indistinguishable from a
live-scored one in the feed. Like `seq` it is **feed-level and hash-exempt**
(§6): it never enters the in-memory event log or the TrajectoryHash, and the
determinism-conditional-on-scores claim (§5.4 claim 10) holds over every field
EXCEPT it.

The four trace fields `{idempotency_key, rule_artifact_hash, intent_spec_hash,
trajectory_hash}` plus `intent_id` and `seq` are the **cross-repo trace
contract** external consumers code against.

**Encoding and recovery rules (contractual):**

- Each line is `json.Marshal(Record)` + `'\n'`. The file is opened
  `O_APPEND|O_CREATE|O_RDWR`.
- `GlobalSeq` starts at **1** (first record ever) and is monotonic thereafter,
  across intents and across reopen (`recovered max + 1`). `since=0` returns
  everything.
- fsync (`*os.File.Sync()`) after **every** append, before returning success.
- Single-process; a **mutex-guarded single writer**. Reads (`Since`, `ByIntent`)
  take the same lock.
- Full-scan recovery on `Open`: read the file start-to-end line by line (raise
  the `bufio.Scanner` buffer or use `bufio.Reader` — a record line may exceed
  the default limit), `json.Unmarshal` each line, recover `max(GlobalSeq)` and
  retain records for reads. A trailing
  partial/blank line is ignored (torn last write); everything before it is
  authoritative. **Recovery MUST NOT re-fsync or rewrite existing lines.**
  Because a torn tail is left in place, the first append after recovering one
  writes a leading `'\n'` so new records never glue onto it.
- `GlobalSeq` is **never** part of the per-intent `TrajectoryHash` (it is
  non-deterministic across replay by design).

### §2.4 The `/ml/evaluate` seam

The gate's scoring authority. `Unevaluable` never collapses into pass, and EVERY
failure of the network, the service, or the facts is `Unevaluable`. **A scorer
outage must only ever make the gate refuse.**

```go
package scoring // core/internal/scoring

// DefaultTimeout bounds one /ml/evaluate call. A slower scorer is Unevaluable
// (a timeout is a transport error like any other).
const DefaultTimeout = 5 * time.Second

// Score is the tri-state result of scoring a criterion.
type Score int

const (
	Pass Score = iota
	Fail
	Unevaluable
)

func (s Score) String() string // "PASS","FAIL","UNEVALUABLE"

// Scorer scores ONE named criterion for an intent in a given phase. It is the
// single scoring authority, consulted by the gate at declaration and again
// (volatile only) at the dispatch edge. A transport/timeout error MUST surface
// as Unevaluable (fail-closed), never as a silent pass.
type Scorer interface {
	Score(ctx context.Context, i intent.Intent, c intent.Criterion, phase intent.Phase) Score
}

// HTTPScorer calls the Python "/ml/evaluate" endpoint. On any
// HTTP/transport/decode error or non-2xx it returns Unevaluable.
type HTTPScorer struct {
	Endpoint string // e.g. "http://localhost:8000/ml/evaluate"
	Client   *http.Client
}

// NewHTTPScorer returns an HTTPScorer whose client times out at DefaultTimeout.
// An empty endpoint yields a scorer whose every Score is Unevaluable — the
// zero-config server authorizes nothing.
func NewHTTPScorer(endpoint string) *HTTPScorer

func (h *HTTPScorer) Score(ctx context.Context, i intent.Intent, c intent.Criterion, phase intent.Phase) Score

// EvalRequest is the /ml/evaluate request JSON contract. Evolution is ADDITIVE
// ONLY — nothing renamed, retyped, or removed.
type EvalRequest struct {
	IntentID         string  `json:"intent_id"`
	Criterion        string  `json:"criterion"`
	Threshold        float64 `json:"threshold"`
	Phase            string  `json:"phase"`
	Volatility       string  `json:"volatility"`                   // "stable" | "volatile"
	RuleArtifactHash string  `json:"rule_artifact_hash,omitempty"` // opaque passthrough
	IntentSpecHash   string  `json:"intent_spec_hash,omitempty"`   // opaque passthrough
}

// EvalResponse — Basis is observability only; it MUST NOT enter the audit log.
type EvalResponse struct {
	Result string `json:"result"`
	Basis  string `json:"basis,omitempty"`
}

// ScoreKey identifies a (criterion name, phase) pair.
type ScoreKey struct {
	Criterion string
	Phase     intent.Phase
}

// FakeScorer is the in-package test double used by the gate acceptance tests.
// Results is keyed by (criterion name, phase). A key absent from Results defaults
// to Pass (documented ergonomic default; tests set only the failing/unevaluable
// ones). Calls is appended on every Score call, in order, for call-count
// assertions.
type FakeScorer struct {
	Results map[ScoreKey]Score
	Calls   []ScoreKey
}

func (f *FakeScorer) Score(ctx context.Context, i intent.Intent, c intent.Criterion, phase intent.Phase) Score
```

`HTTPScorer.Score` populates `Volatility` from `c.Volatility` and the two hashes
from the intent; the default branch of its result mapping is `Unevaluable`.

Wire example (the service IGNORES unknown fields for forward compatibility; the
five original fields are required):

```json
{
  "intent_id":          "5193ff14a8ec15d6",
  "criterion":          "alpha",
  "threshold":          100.0,
  "phase":              "declaration",
  "volatility":         "stable",
  "rule_artifact_hash": "opaque-or-absent",
  "intent_spec_hash":   "opaque-or-absent"
}
```

```json
{ "result": "PASS", "basis": "alpha=250.00 >= 100.00" }
```

- `phase` ∈ `"declaration" | "dispatch"` (mirrors `intent.Phase`).
- `volatility` ∈ `"stable" | "volatile"` — so the service can log/route without
  inferring from phase.
- The two hashes are optional (`omitempty`) and OPAQUE to the wire: they exist
  so the resolver can verify the governing `IntentSpec` artifact before scoring.
  **When present and a resolver is configured, verification failure ⟹
  `UNEVALUABLE`.** When absent or no resolver: skip verification (recorded in
  `basis`), score from facts.
- Tri-state on the wire is the closed string set `"PASS" | "FAIL" |
  "UNEVALUABLE"`. Any other value, absent field, or malformed body ⟹ the client
  scores `Unevaluable`.
- `result` required, closed set. `basis` optional free-text, observability only.
  Unknown response fields are ignored by the Go client.
- **Service errors are still evaluations**: for a well-formed request the
  service returns `200` with `"UNEVALUABLE"` (unknown criterion, missing fact,
  resolver failure, internal exception). Non-2xx is reserved for malformed
  requests (`400`; FastAPI's `422` is acceptable as-is) and infrastructure
  failure — the client maps those to `Unevaluable` anyway, so both paths fail
  closed.
- **Acknowledged debt (ADR-0003)**: `threshold` is `float64` on this wire while
  ATLAS IntentSpecIR thresholds are exact `ScalarValue` (no floats) — a lossy
  boundary, consciously deferred to the resolver-extraction slice (where the
  exact scalar actually crosses), not fixed here.

**Client fail-closed matrix** (each row is a test in `core/internal/scoring/scorer_test.go`):

| Failure | Client result |
|---|---|
| connection refused / DNS / TLS | `Unevaluable` |
| timeout (`DefaultTimeout`) or ctx cancel | `Unevaluable` |
| non-2xx status (400, 422, 500, 503…) | `Unevaluable` |
| body not JSON / truncated | `Unevaluable` |
| `result` absent or outside the closed set | `Unevaluable` |
| empty `Endpoint` | `Unevaluable` |

**Service fail-closed matrix** (each row is a pytest):

| Condition | Service response |
|---|---|
| unknown criterion | `200 {"result":"UNEVALUABLE","basis":"unknown criterion"}` |
| fact source has no fact | `200 {"result":"UNEVALUABLE", ...}` |
| resolver configured + verify fails | `200 {"result":"UNEVALUABLE", ...}` |
| evaluator raises | `200 {"result":"UNEVALUABLE", ...}` (handler catches all) |
| malformed request | `400`/`422` (client maps to `Unevaluable`) |

**`basis` never enters `audit.Event.Detail`, the durable feed, or any hash** —
it is free text and would poison determinism.

### §2.5 Environment

| Variable | Side | Meaning |
|---|---|---|
| `INTENT_DATA_DIR` | gate | directory for `events.jsonl` + `idempotency.jsonl`; default `./data` (for `main` only — no test may write it) |
| `INTENT_SCORER_URL` | gate | the FULL `/ml/evaluate` endpoint (e.g. `http://host:8000/ml/evaluate`). Unset ⟹ empty endpoint ⟹ every non-forced Score is `Unevaluable` ⟹ **the zero-config server authorizes nothing** |
| `INTENT_ADDR` | gate | listen address; default `:8080` |
| `INTENT_TRUST_ROOT` | gate | path to the trust-root file (`{"keys": {"<keyid>": "<b64 ed25519 pub>"}}`). Unset ⟹ EMPTY trust root ⟹ every spec is unattested ⟹ **the zero-config server authorizes nothing** (§2.6) |
| `INTENT_SPEC_DIR` | gate | spec-store directory; default `<INTENT_DATA_DIR>/specs` |
| `INTENT_UNSAFE_FORCE_SCORES` | gate | `1` ⟹ `force_scores` accepted (TEST POSTURE). Any other value ⟹ a request carrying `force_scores` is a loud 400. **Never set in production** |
| `SCORER_HOST`, `SCORER_PORT` | scorer | uvicorn bind; defaults `127.0.0.1`, `8000` |
| `SCORER_FACTS_JSON` | scorer | criterion → number JSON object. Unset ⟹ EMPTY fact map ⟹ every criterion `UNEVALUABLE` (§8) |
| `SCORER_ARTIFACT_DIR`, `SCORER_ATLAS_INPUTS_DIR`, `SCORER_EXPORTED_AT_UNIX` | scorer | resolver config, all-or-nothing (§8) |
| `SCORER_CONTRACT_DIR` | scorer tests | override for the §9 fixture directory |
| `SCORER_ATLAS_DIR` | scorer tests | override for the wheel-lane ATLAS goldens |

**Scorer selection in `core/cmd/server`:** `force_scores` present AND the
server booted with `INTENT_UNSAFE_FORCE_SCORES=1` ⟹ the per-request forced
scorer (guarded test affordance; the feed witnesses it via `scorer_id:
"forced"`). `force_scores` present WITHOUT the flag ⟹ 400 — never a silent
ignore (a silently dropped bypass is a bypass in waiting). Otherwise ONE
boot-time shared `HTTPScorer` built from `INTENT_SCORER_URL`.

**Boot:** `dir := os.Getenv("INTENT_DATA_DIR")` defaulting to `"./data"`;
`durable.Open(dir)`; `idempotency.OpenStore(dir)` (fatal on error). The durable
feed and the durable idempotency store are wired **ONCE at boot** and shared by
every handler; the per-request `Gate` value is a thin wrapper over those shared
singletons (it may be constructed per request to carry the per-request scorer;
the **stores are never per-request**).


### §2.6 Spec resolution — the plane (2026-08-04 amendment)

The signed artifact and its stores live in the top-level `plane` package
(core-side, verification only); the application seats `treasury/control`
(attest, publish, revoke, promote — the ONLY production importer of
`treasury/authority`) and `treasury/authoring` (drafts; holds no keys by
import graph, `TestKeyPossessionBoundary`) operate on it.

**Envelope (DSSE-shaped).** `{"payloadType", "payload" (b64), "signatures":
[{"keyid", "sig", "key_authority"}]}`. The signature covers
`PAE(payloadType, payload)` (DSSE v1 pre-authentication encoding), ed25519.
`payloadType` is `application/vnd.intent-plane.spec+json` for specs and
`application/vnd.intent-plane.revocation+json` for tombstones. `keyid` =
first 16 hex of sha256(pubkey). **`key_authority` is `"test"` until ADR-0009
production key authority lands (R1)** — every envelope says so.

**Content address.** `intent_spec_hash` = lowercase-hex sha256 over the RAW
payload bytes inside the envelope. The bytes a human attested are the bytes
the gate executes: byte-for-byte is a hash equality, not a metaphor.

**Spec payload** (strict-decoded; unknown fields refuse): `spec_version`,
`action_class`, `enforcement_posture` (`enforce`|`shadow`), `criteria`
(name/threshold/volatility), `source_pins` (name + `passage_sha256` of the
exact policy passage), `named_unknowns` (unmapped provisions, surfaced not
omitted), `human_judgment` (deliberately-unquantified obligations; any
unresolved entry ⟹ the gate refuses, §3.3).

**Spec store** (`INTENT_SPEC_DIR`): `<hash>.env.json` (published envelope),
`<hash>.pin` (pin marker), `<hash>.revoked.json` (SIGNED revocation
tombstone; a stranger-signed tombstone does not revoke). No wallclock
anywhere. Publish is verify-then-write: unverifiable envelopes never enter.

**Resolution (hybrid rule).** Given a declared `intent_spec_hash` and an
optional wire `spec_envelope`: (1) a verified tombstone ⟹ `RevokedError`;
(2) the store's envelope, if it verifies against the trust root AND its
payload hashes to the claimed hash ⟹ resolve, source `store`; (3) a wire
envelope ⟹ resolve source `wire` ONLY if it verifies against the SAME trust
root, hashes to the claimed hash, AND that hash is pinned in the store —
the store stays authoritative; (4) otherwise `ErrUnattested` ⟹ the gate
refuses `unevaluable:unattested-spec`. Bare criteria cannot arrive at all:
the wire DTO has no such field (§2.2).

---

## §3 Lifecycle & cause classes

### §3.1 States and transitions

```go
package lifecycle // core/internal/lifecycle

type State string

const (
	Declared         State = "DECLARED"
	Resolving        State = "RESOLVING"
	Active           State = "ACTIVE"
	Verifying        State = "VERIFYING"
	Achieved         State = "ACHIEVED"
	Failed           State = "FAILED"
	FailedAtDispatch State = "FAILED_AT_DISPATCH"
	ShadowRecorded   State = "SHADOW_RECORDED" // ADR-0006 (Proposed): 2026-08-04 canon bump
)

// IsTerminal reports whether s is one of ACHIEVED, FAILED, FAILED_AT_DISPATCH,
// SHADOW_RECORDED.
func (s State) IsTerminal() bool

// IsValidTransition reports whether from->to is permitted by the lifecycle graph.
//
// Permitted edges (and ONLY these):
//   DECLARED  -> RESOLVING
//   RESOLVING -> ACTIVE, FAILED
//   ACTIVE    -> VERIFYING, FAILED
//   VERIFYING -> ACHIEVED, FAILED, FAILED_AT_DISPATCH, SHADOW_RECORDED
// Terminal states have no outgoing edges.
// FAILED_AT_DISPATCH is reachable ONLY from VERIFYING (table-enforced); the gate
// further restricts it to the dispatch-edge path (code-enforced).
func IsValidTransition(from, to State) bool
```

The transition table is used purely for membership lookups, so its
map-iteration order never reaches the event log.

### §3.2 Terminals

`ACHIEVED`, `FAILED`, `FAILED_AT_DISPATCH`, `SHADOW_RECORDED`.
**`FAILED_AT_DISPATCH` ⟹ no settlement event, every time** — no `ACHIEVED`
record exists in the feed, so no consumer ever settles.

**`SHADOW_RECORDED`** (ADR-0006, Proposed) is the terminal of a
shadow-posture intent: fully scored — declaration AND dispatch-edge recheck —
then durably recorded with the four trace fields, and **NOT authorized**: no
`ACHIEVED` event, no idempotency-key reservation, no consumer ever settles.
Enforcement posture lives INSIDE the signed payload (`enforcement_posture`);
promotion shadow→enforce is a NEW attestation with a NEW hash — an authority
act, never a config toggle (config-toggled shadow remains forbidden, R3).

Refusals that occur before any lifecycle transition (absent key, thin spec)
carry terminal `FAILED` in the `Result` without logging a `FAILED` transition
event: `DECLARED -> FAILED` is not a lifecycle edge, and those paths append
`UNEVALUABLE` only.

### §3.3 FAILED_AT_DISPATCH cause classes

`FAILED_AT_DISPATCH` names **where the error entered** — the dispatch edge — via
a **closed set** of reason cause classes:

| Cause class | Meaning | Status |
|---|---|---|
| `volatile-recheck:<name>` | volatile fact drifted between scoring and dispatch | built |
| `idempotency-collision` | key already reserved (near-duplicate) | built |
| `revoked:<ref>` | the pinned spec was revoked between verification and dispatch | **ACTIVATED 2026-08-04** — the gate re-consults the revocation signal (the spec store's verified tombstones) at the dispatch edge (`gate.go` step 4a2, `TestRevokedBetweenVerifyAndDispatch`). The key is NOT reserved on this path. When the gate is built without a revocation checker, no signal reaches it — signal absence is not non-revocation being proven |

Revocation observed at the edge IS a volatile-fact drift, routed exactly as
the reserved row recorded. **Adding any other cause class means amending this
table first.**

**Declaration-side refusal causes** (terminal `FAILED`, closed set; the first
four append `UNEVALUABLE` only, before any lifecycle transition):

| Cause | Meaning |
|---|---|
| `unevaluable:absent-key` | no idempotency key |
| `unevaluable:unattested-spec` | no VERIFIED spec for the claimed hash — not in the store, no pinned+verified wire envelope (§2.6). Criteria that did not survive signature verification + content-address equality never reach the scorer |
| `unevaluable:invalid-posture` | the attested payload's `enforcement_posture` is neither `enforce` nor `shadow` — the zero value never silently becomes enforce |
| `unevaluable:human-judgment:<name>` | the attested payload carries an unresolved deliberately-unquantified obligation; abstention is the plane working (P6) |
| `unevaluable:empty-criteria`, `unevaluable:invalid-volatility:<name>` | thin-spec defense (§4.2 step 1b) — attestation does not launder vacuity |
| `revoked:<ref>` | a verified tombstone existed at declaration (resolver or live checker); the ref names it. Ordering: a revoked resolution WINS over unattested — collapsing it into `unevaluable:unattested-spec` would erase a fact the feed exists to witness (`TestRevokedResolutionWinsOverUnattested`) |

---

## §4 Gate algorithm

### §4.1 Types

```go
package gate // core/internal/gate

import (
	"context"

	"github.com/hossainpazooki/intent-plane/core/internal/audit"
	"github.com/hossainpazooki/intent-plane/core/internal/durable"
	"github.com/hossainpazooki/intent-plane/core/internal/idempotency"
	"github.com/hossainpazooki/intent-plane/core/internal/intent"
	"github.com/hossainpazooki/intent-plane/core/internal/lifecycle"
	"github.com/hossainpazooki/intent-plane/core/internal/scoring"
	// NOTE: core/internal/adapter is NOT imported here (emit-and-observe).
)

// Result is the terminal outcome of one authorization. There is no Settlement
// field: the gate emits and stops; a downstream consumer settles from the feed.
// Events + TrajectoryHash are the per-intent log (no GlobalSeq) and are
// byte-identical across replay.
type Result struct {
	Terminal       lifecycle.State // ACHIEVED | FAILED | FAILED_AT_DISPATCH
	Reason         string          // failed criterion names / "unevaluable:<crit>" / "idempotency-collision" / ""
	Events         []audit.Event   // per-intent append-only log
	TrajectoryHash string          // per-intent hash over Events
	AchievedSeq    int             // GlobalSeq of the emitted ACHIEVED record; 0 if not ACHIEVED
}

// Gate authorizes intents against the scorer, the durable feed, and the
// idempotency store. It holds NO settlement dependency.
type Gate struct{ /* unexported: scorer scoring.Scorer; feed *durable.Store; store *idempotency.Store */ }

// New constructs a Gate over the scorer, the (shared, durable) feed, and the
// (shared, durable) idempotency store.
func New(s scoring.Scorer, feed *durable.Store, store *idempotency.Store) *Gate

// Authorize drives the full lifecycle deterministically and returns the terminal
// Result. Any feed.Append error aborts: the partial Result built so far is
// returned with a non-nil error, and no terminal guarantee is implied.
func (g *Gate) Authorize(ctx context.Context, i intent.Intent) (Result, error)
```

### §4.2 The algorithm

Every step appends to the in-memory per-intent log via `core/internal/audit`
**and** mirrors the event to the durable feed as a `durable.Record`
`{IntentID: i.ID(), IntentSeq: e.Seq, Type: e.Type, Detail: e.Detail}`,
preserving the per-intent `Seq` and `TrajectoryHash` exactly.

1. **DECLARED.** Append `DECLARED` with detail `i.ID()`. If
   `i.IdempotencyKey == ""` ⟹ append `UNEVALUABLE` detail `absent-key`,
   terminal `FAILED`, reason `unevaluable:absent-key`, no settlement. Return.

   **1a2. Revocation-at-resolution.** `Resolution.RevokedRef != ""` (the
   resolver found a verified tombstone) ⟹ append `REVOKED` detail `<ref>`,
   terminal `FAILED`, reason `revoked:<ref>`. Revocation WINS over unattested
   (§3.3 ordering note).

   **1a3. Attestation defense.** `!Resolution.Attested` ⟹ append
   `UNEVALUABLE` detail `unattested-spec:<intent_spec_hash>`, terminal
   `FAILED`, reason `unevaluable:unattested-spec`. **The scorer is never
   consulted:** criteria that did not arrive through §2.6 resolution do not
   exist as far as scoring is concerned (P1's fail-closed floor).

   **1a3b. Revocation at declaration (live checker).** The gate's
   `RevocationChecker` (the spec store) answers ⟹ append `REVOKED`, terminal
   `FAILED`, reason `revoked:<ref>`.

   **1a4. Posture defense.** `Spec.Posture` neither `enforce` nor `shadow`
   (including the zero value) ⟹ append `UNEVALUABLE` detail
   `invalid-posture:<raw>`, terminal `FAILED`, reason
   `unevaluable:invalid-posture`. A posture default would be a config toggle
   wearing a trench coat.

   **1a5. Human-judgment defense.** `len(Spec.HumanJudgment) > 0` ⟹ append
   `UNEVALUABLE` detail `human-judgment:<first>`, terminal `FAILED`, reason
   `unevaluable:human-judgment:<first>`. Abstention as a success state (P6).

   **1b. Thin-spec defense.** After the resolution defenses, before any
   lifecycle transition or scoring. These checks apply to the RESOLVED payload
   (the pinned property is *a resolved spec with zero criteria never
   reaches ACHIEVED, regardless of where resolution happens*) — attestation
   does not launder vacuity.

   - **Empty criteria** (`len(Spec.Criteria) == 0`, nil and empty alike): append
     `UNEVALUABLE` with detail `empty-criteria:<intent_spec_hash>` (the refusal
     record witnesses WHICH claimed spec was thin; a blank hash yields the bare
     `empty-criteria:`), terminal `FAILED`, reason
     `unevaluable:empty-criteria`. **The scorer is never consulted.**
   - **Unknown volatility** (neither `stable` nor `volatile`, including blank /
     field omitted): append `UNEVALUABLE` with detail
     `invalid-volatility:<name>:<raw>`, terminal `FAILED`, reason
     `unevaluable:invalid-volatility:<name>`. This closes the stale-pass hole
     where a typo'd `volatile` silently became stable and skipped the
     dispatch-edge re-verify. **Behavior change on the wire:** a criterion with
     omitted/blank volatility previously scored as stable; it now refuses.

2. **`DECLARED -> RESOLVING -> ACTIVE -> VERIFYING`**, each a logged,
   `IsValidTransition`-checked transition (event type = the destination state).

3. **Declaration scoring.** For EACH criterion (stable AND volatile) in **slice
   order** (never map order), consult `scorer.Score(.., Declaration)` and append
   `SCORED` with detail `<name>:<score>`.
   - On `Unevaluable` (or any out-of-domain score, §4.3): append `UNEVALUABLE`
     detail `<name>`, transition to `FAILED`, reason `unevaluable:<name>`, no
     settlement. Return. (Fail-closed; never a pass.)
   - On `Fail`: collect the name. After all criteria, if any failed: transition
     to `FAILED`, reason = the joined failed names (comma-separated), no
     settlement. Return.

4. **Dispatch edge** (only if all criteria passed at declaration):
   1. For each **VOLATILE** criterion ONLY, consult
      `scorer.Score(.., Dispatch)` and append `RECHECK` detail
      `<name>:<score>`. Any score that is not exactly `Pass` ⟹ transition to
      `FAILED_AT_DISPATCH`, reason `volatile-recheck:<name>`, no settlement.
      Return. (`Unevaluable` additionally appends a distinct `UNEVALUABLE`
      event before the terminal transition. **Stable criteria are NOT
      re-scored.**)
   2. **Revocation re-check (4a2).** Re-consult the `RevocationChecker` at the
      last moment before the consequence: a verified tombstone ⟹ append
      `REVOKED`, transition to `FAILED_AT_DISPATCH`, reason `revoked:<ref>`,
      no settlement, **key NOT reserved** (re-declaring after a fresh
      attestation is legitimate). This is the §3.3 reserved cause class,
      activated — the same last-moment discipline as volatile criteria,
      applied to authority itself.
   3. **Shadow posture (4a3).** `Spec.Posture == shadow` ⟹ append
      `SHADOW_RECORDED` (detail `i.ID()`), compute the TrajectoryHash
      INCLUDING it, `feed.Append` the SHADOW_RECORDED record carrying the four
      trace fields, terminal `SHADOW_RECORDED`, reason `""`. **No idempotency
      reservation, no ACHIEVED, nothing settles** — fully scored, durably
      recorded, not authorized (§3.2).
   4. **Idempotency reserve:** `store.Reserve(i.ID(), i.IdempotencyKey)`. On
      collision (`ok == false`) ⟹ transition to `FAILED_AT_DISPATCH`, reason
      `idempotency-collision`, no settlement. Return. On success append
      `IDEMPOTENCY_RESERVED` with detail = the key.

5. **Authorize — EMIT-ONLY.** Append the single `ACHIEVED` event in-memory
   (detail `i.ID()`), compute `th := log.TrajectoryHash()` (which INCLUDES the
   ACHIEVED event), then `feed.Append` the ACHIEVED `durable.Record` carrying
   the four trace fields `{IdempotencyKey, RuleArtifactHash, IntentSpecHash,
   TrajectoryHash: th}`. Set `Result.AchievedSeq` to that record's `GlobalSeq`.
   Terminal `ACHIEVED`, reason `""`. **The gate calls no adapter and settles
   nothing in-process.**

Event types appearing in the log: `DECLARED`, `RESOLVING`, `ACTIVE`,
`VERIFYING`, `SCORED`, `RECHECK`, `UNEVALUABLE`, `REVOKED`,
`IDEMPOTENCY_RESERVED`, `ACHIEVED`, `FAILED`, `FAILED_AT_DISPATCH`,
`SHADOW_RECORDED`.

### §4.3 Out-of-domain scores fail closed

`scoring.Score` is an open `int`. The declaration switch treats anything that is
not exactly `Pass` or `Fail` as unevaluable — mirroring the dispatch edge's
exact-`Pass` check. Before this rule, a custom `Scorer` returning `Score(3)`
fell through as an implicit pass, yielding the self-contradictory log
`SCORED <name>:UNEVALUABLE` → `ACHIEVED`. Unreachable via any in-repo scorer;
closed anyway (pinned by `TestFailClosedOutOfDomainScore`, proven red-first).

---

## §5 Invariants

### §5.1 The eight invariants

1. **Single ACHIEVED authority.** The gate is the sole emitter of the `ACHIEVED`
   event; it is a single append-only event, fsynced to the durable feed before
   success. A consumer acts ONLY after observing it.
2. **Tri-state scoring, fail-closed, non-vacuously.** A criterion scores `Pass`,
   `Fail`, or `Unevaluable`. ANY `Fail` or `Unevaluable` ⟹ not authorized.
   `Unevaluable` is logged distinctly and MUST NEVER collapse into pass. In
   full:

   > Authorized ⟺ the criteria set is **non-empty**, every criterion is validly
   > shaped, and every criterion scores `Pass` (volatile ones again at the
   > dispatch edge). **"No criterion failed" is never satisfied by "no criterion
   > existed."**

   This wording supersedes any reading of "`allPassed` ⟺ every criterion
   `Pass`" that an empty set would satisfy. The scorer-side twin of this guard
   is `core/scorer/src/scorer/resolver.py`'s hashless-verify refusal
   (`all([]) is True` would be fail-open).
3. **Stable vs volatile.** Stable criteria scored once (at declaration).
   Volatile criteria scored at declaration AND re-verified at the dispatch edge
   by the SAME gate before authorizing. A volatile criterion that is not `Pass`
   at re-verify ⟹ `FAILED_AT_DISPATCH`, nothing dispatches.
4. **Idempotency by construction.** The key is required; an empty key ⟹ refuse
   at declaration (`FAILED`, unevaluable: absent key). The key is reserved at
   the dispatch edge; a near-duplicate (same key, different intent hash)
   collides and is refused ⟹ `FAILED_AT_DISPATCH`. At-most-once holds on the
   settlement log, and holds **across requests and across process restart**.
5. **`FAILED_AT_DISPATCH` ⟹ no settlement event**, every time. Reachable ONLY
   from `VERIFYING` via the dispatch-edge path (volatile re-check fail OR
   idempotency collision). Never any other way.
6. **Determinism / replay.** See §6.
7. **Thin-spec defense (step 1b).** Zero criteria ⟹ `FAILED`
   `unevaluable:empty-criteria` (the `UNEVALUABLE` detail binds the claimed spec
   hash); unknown volatility ⟹ `FAILED`
   `unevaluable:invalid-volatility:<name>`. Both refuse BEFORE any scoring; the
   scorer is never consulted. Out-of-domain `scoring.Score` values fail closed
   at declaration (§4.3).
8. **The boundary and the vocabulary are pinned mechanically.** The import
   adjacency and package set by `core/internal/contractcheck/boundary_test.go`
   (§7); the role vocabulary and retired proper noun by `vocab_test.go` (§1);
   core neutrality by `neutrality_test.go` (this invariant; the fixture
   exemption lives in §9). **Amend this document FIRST,
   then the pinned tables — never the reverse.**

**Honesty bounds on invariant 7.** Step 1b closes the *vacuous* case only: a
**thinned** set (three criteria where the source document requires five) is
structurally invisible gate-side and belongs to the ATLAS-side
minimum-criteria/coverage invariant. The volatility check closes the *typo* case
only: a criterion semantically mislabeled stable is authoring/attestation
territory the string cannot reveal. Every gate-side `unevaluable:empty-criteria`
event is evidence a spec escaped compile that shouldn't have — a measurable
cross-repo signal.

### §5.2 Locked decisions and hard rules

- Code against THIS contract, NOT against other agents' files. Do not change any
  exported name, signature, or package path fixed here.
- stdlib only on the Go side; no new modules; **no network in any unit test**
  (`httptest` in Go, `TestClient` in Python).
- JSONL everywhere durable: one JSON object per line, `\n`-terminated.
- Single-process server; a mutex-guarded single writer per durable store; reads
  take the same lock. No sleeps to paper over a race.
- fsync after **every** durable append, before returning success.
- Full-scan recovery on `Open`; recovery never rewrites or re-fsyncs.
- **All test IO under `t.TempDir()` (wired through `INTENT_DATA_DIR`). No test
  may write `./data` or any repo-relative path.** The `./data` default is for
  `main` only.
- Deterministic only: no wallclock, no unseeded `math/rand`, no map-iteration
  order in any log. `GlobalSeq` never enters the per-intent `TrajectoryHash`.
- Never weaken the closed result set; the default branch of every mapping is
  `Unevaluable`.
- Never weaken or skip an existing assertion — every invariant keeps the
  successor assertion in §5.3.
- The Python service never imports from the Go tree and vice versa — the wire
  fixtures (§9) are the ONLY shared surface.
- Implemented-vs-planned stays visible: `NullResolver` and `StaticFactSource`
  are labeled as such in `basis`; wheel-dependent tests skip loudly, never fake.
- Never run git.

### §5.3 Acceptance-assertion discipline

The gate acceptance suite (`core/internal/gate/acceptance_test.go`) uses
`scoring.FakeScorer`, `durable.Open(t.TempDir())`, `idempotency.NewStore()` (and
`idempotency.OpenStore(t.TempDir())` for the restart case). Because the gate no
longer settles in-process, every "no settlement" assertion is expressed against
the feed plus a **test-only feed consumer** — the successor to the in-process
adapter call:

```go
// feedConsumer drains ACHIEVED records from a durable feed past a cursor and calls
// OnAchieved on a ReferenceAdapter (recompute path), enforcing at-most-once via the
// adapter's key-idempotency PLUS its own cursor. Poll(feed) is safe to call
// repeatedly and after a feed reopen; it never double-settles a key.
type feedConsumer struct{ ref *adapter.ReferenceAdapter; cursor int; intents map[string]intent.Intent }
```

The consumer looks up the original `intent.Intent` by `record.IntentID` from the
map the test populated at submit time (it cannot invert `ID()` from the record),
advances its cursor to `max(GlobalSeq)` seen, and calls `ref.OnAchieved`.

| Invariant | The assertion that must exist |
|---|---|
| **(a) Determinism / replay** | Two Gates, **each with its own `durable.Open(t.TempDir())`** + own idempotency store, same intent ⟹ equal per-intent `Events` and `TrajectoryHash` (byte-identical; `GlobalSeq` explicitly excluded from the compare). The ACHIEVED record's `trajectory_hash` == `Result.TrajectoryHash`. A `feedConsumer` draining each independent feed calls `OnAchieved` exactly once per key and the resulting `SettlementEvent`s are byte-identical (payload determinism), proving the RECOMPUTE path ran rather than a re-read. |
| **(b) Fail-closed unevaluable + absent key** | For EACH criterion, `FakeScorer` `Unevaluable` at declaration ⟹ `FAILED`, never `ACHIEVED`, never a hang; the log contains `UNEVALUABLE`. Empty idempotency key ⟹ `FAILED` `unevaluable:absent-key`. No-settlement successor: `feed.Since(0,"ACHIEVED")` has zero records for that intent AND the consumer's `Settlement()` is empty. |
| **(c) Verification failure** | A criterion `Fail` at declaration ⟹ `FAILED` naming it; no ACHIEVED record in the feed for that intent; consumer ledger empty for its key. |
| **(d) Volatile re-verify** | Volatile criterion `Pass` at declaration, `Fail`/`Unevaluable` at dispatch ⟹ `FAILED_AT_DISPATCH`; a STABLE criterion is scored exactly once (no Dispatch call) and a VOLATILE one exactly twice (via `FakeScorer.Calls`). No-settlement successor as in (c). |
| **(e) Idempotency collision** | Two intents, same key, different `IntentSpecHash`, **SHARED** store ⟹ first `ACHIEVED`, second `FAILED_AT_DISPATCH` `idempotency-collision`. The feed has **exactly one** ACHIEVED record for the key; the consumer records **exactly one** settlement. **Restart clause:** `Close`+`OpenStore` the idempotency store from the same dir, submit a third intent with the same key ⟹ still `idempotency-collision`; reopen the feed, re-poll the consumer from cursor 0 ⟹ still one ACHIEVED, no new settlement (at-most-once **across process restart**). |
| **(f) Terminal separation** | For every `FAILED` / `FAILED_AT_DISPATCH` result, `feed.Since(0,"ACHIEVED")` contains **no** record for that intent (the feed is the successor to `Settlement == nil`). The ACHIEVED path has exactly one ACHIEVED record, ordered **after** the `RECHECK` record of the volatile criterion (asserted by `IntentSeq` / `GlobalSeq` order in `ByIntent`). |
| **(g) Thin-spec defense** | `TestFailClosedEmptyCriteria`, `TestFailClosedInvalidVolatility`, `TestFailClosedOutOfDomainScore` — with wire twins `TestEmptyCriteriaRefusedOverWire`, `TestInvalidVolatilityRefusedOverWire` in `core/cmd/server/main_test.go`. All demonstrated red against the pre-amendment gate, then green. |
| **(h) Scorer-identity witness + wire guards** | `TestDeterminismConditionalOnScores` asserts `scorer_id` POSITIVELY on every SCORED/RECHECK record ("forced"/"live") before its equality carve-out — deleting the stamping fails the suite (plant-proven in a temp copy, 2026-08-05; before this row the deletion left every gate green). The loud-400 wire guards are pinned by `TestOldWireShapeCriteria400`, `TestTopLevelCriteria400`, `TestForceScoresRefusedWithoutFlag` (incl. the empty-map form) in `core/cmd/server/wire_guard_test.go`. Discipline note: a new §2.3 wire field MUST land with its §5.3 row in the same amendment — this row exists because `scorer_id` initially did not. |

`core/cmd/server/main_test.go` drives the shared-store server via `httptest`,
using `t.Setenv("INTENT_DATA_DIR", t.TempDir())` and a boot helper that builds
the mux over stores opened in that temp dir (no `./data`, no bound port). It
covers: healthz; ACHIEVED (terminal + `achieved_seq >= 1` + the record appears
in `GET /v2/events?type=ACHIEVED`); FAILED_AT_DISPATCH (no `achieved_seq`, no
ACHIEVED record in the feed); cursor paging (`since` advances, `next_since`
correct); per-intent endpoint order; the **restart** case (reopen stores over
the same dir; at-most-once must hold); zero-config refusal (no `force_scores` +
unset `INTENT_SCORER_URL` ⟹ `FAILED`, reason `unevaluable:<first criterion>`);
and a live scorer (`INTENT_SCORER_URL` pointing at an `httptest` scorer ⟹ the
terminal follows the scorer's answers).

### §5.4 Load-bearing claims and their probes

Each claim is verified by re-running its probe, and each probe is proven
non-vacuous by mutating a COPY of the tree and watching it go red.

| # | Claim | Probe |
|---|---|---|
| 1 | Feed durability/recovery: events survive process restart with `GlobalSeq` intact. | `go test ./core/internal/durable -run Recovery` — append N records across two intents, `Close`, `Open` same dir, assert `ByIntent`/`Since` return all N and `max(GlobalSeq)` is preserved; truncate the last line and assert everything before it still recovers. |
| 2 | `GlobalSeq` is globally monotonic with no reset or gap, across intents and across reopen. | `go test ./core/internal/durable -run GlobalSeq` — interleave appends from two intent IDs, assert `seq` strictly increases 1,2,3,…; `Close`, `Open`, append once more, assert it continues at `prevMax+1`. |
| 3 | Idempotency at-most-once holds ACROSS requests AND ACROSS process restart. | `go test ./core/internal/idempotency -run Restart` — `OpenStore`, `Reserve(k)==true`, `Close`, `OpenStore` same dir, `Reserve(k)==false`. Server probe: `POST /v2/intents` same key, reboot over the same `INTENT_DATA_DIR`, `POST` again ⟹ `FAILED_AT_DISPATCH` `idempotency-collision`. The store never writes outside `INTENT_DATA_DIR`, and no per-request store is constructed in `core/cmd/server/main.go`. |
| 4 | Per-intent determinism is preserved and independent of `GlobalSeq`. | `go test ./core/internal/gate -run Determinism` — two Gates over separate temp feeds, same intent ⟹ equal `Events` + `TrajectoryHash`; the two runs' `GlobalSeq` values may differ while the hash does not. |
| 5 | Emit-and-observe: the gate never settles in-process, and settlement is at-most-once from the feed. | `grep -n "adapter" core/internal/gate/gate.go` returns **nothing** (no import, no `OnAchieved` call); exactly one ACHIEVED record per key in the feed; a `feedConsumer` that polls twice AND after a reopen records exactly one settlement per key. |
| 6 | Cursor reads are correct and ordered. | `go test ./core/cmd/server` — `GET /v2/events?since=N` returns exactly `GlobalSeq>N` ascending; `type=ACHIEVED` filters to ACHIEVED only; `next_since` equals the max returned; `GET /v2/intents/{id}/events` returns ascending `intent_seq` with the ACHIEVED record after its `RECHECK` record. |
| 7 | Client fail-closed, total: every row of the §2.4 client matrix yields `Unevaluable`. | The `httptest` table test. Mutant: default branch → `Pass`. |
| 8 | Service fail-closed, total: every row of the §2.4 service matrix yields `UNEVALUABLE`/4xx. | The pytest matrix. Mutant: unknown criterion → `PASS`. |
| 9 | Wire agreement: both sides accept every fixture with identical meaning. | The §9 tests green on both sides against the SAME fixture bytes. Mutant: rename one JSON key on one side. |
| 10 | Determinism conditional on scores: gate over `FakeScorer` vs gate over `HTTPScorer`+`httptest` returning the same scores ⟹ byte-identical `Events` and `TrajectoryHash`, and `basis` appears nowhere. | `TestDeterminismConditionalOnScores`. Mutant: append `basis` to the SCORED detail. |
| 11 | Stable-once / volatile-twice crosses the wire: a counting `httptest` scorer sees exactly one `declaration` call per criterion and exactly one extra `dispatch` call per volatile criterion. | `TestStableOnceVolatileTwiceAcrossWire`. Mutant: drop the phase guard. |
| 12 | Live outage refuses, never grants. | Two-process probe: gate `ACHIEVED` with the service up and facts passing; kill the service; the same intent (fresh key) ⟹ `FAILED`, `unevaluable:<criterion>`, no ACHIEVED record in the feed. The kill is real (`taskkill` / `kill`), not mocked. |

---

## §6 Determinism & replay

**Invariant 6.** Per-intent logical clock (`Seq` = 0,1,2,…; **never**
wallclock). IDs derive from `EpisodeSeed`. Same intent + seed ⟹ byte-identical
event log, trajectory hash, and settlement payload. Replay drives the consumer's
RECOMPUTE path (calls `OnAchieved` again), never a re-read of a stored event.

- Criteria are iterated in **slice order**; no map-iteration order ever reaches
  the log.
- `GlobalSeq` is explicitly **NOT** part of `Events` or the `TrajectoryHash`: it
  is non-deterministic across replay by design. Two independent runs may carry
  different `GlobalSeq` values and MUST carry the same hash.
- `basis` from the scorer never enters the log, the feed, or any hash.
- Determinism is **conditional on scores** at the `/ml/evaluate` seam: given the
  same score per (criterion, phase), the gate's events and hash are
  byte-identical regardless of which `Scorer` produced them. **Live facts are
  the only place nondeterminism is ALLOWED to enter**; nothing else may add any.

**`TrajectoryHash` canonical encoding (FIXED — changing it rehashes every golden
value).** Events are encoded in order. For each event, in field order `Seq`,
`Type`, `Detail`, write the field's byte length in decimal, a `':'` separator,
the field's raw bytes, then a `'\n'` terminator. `Seq` is first rendered to its
decimal-string form, then length-prefixed like the others:

```
<len(seqStr)>:<seqStr>\n<len(Type)>:<Type>\n<len(Detail)>:<Detail>\n
```

The digest is SHA-256 (stdlib `crypto/sha256`) over that byte stream, lowercase
hex. Length-prefixing every variable-width field makes the encoding
injection-safe: no choice of `Type`/`Detail` contents (including `':'` or
`'\n'`) can forge an event boundary, so distinct event sequences always produce
distinct byte streams. The hash is therefore stable (same events ⟹ same hash)
and order-sensitive.

---

## §7 Package boundary

**2026-08-04 plane-roles amendment, re-seated by the 2026-08-05 layering
ruling:** the CORE owns exactly one tree outside `core/` beside
`core/cmd/server`: `plane` (envelope, payload, store, resolver —
verification only), the boundary artifact the gate consumes. The authority
seats live in the APPLICATION: `treasury/authority` (EVERY private-key
operation; production-importable ONLY from `treasury/control`,
`TestKeyPossessionBoundary`), `treasury/control`
(attest/publish/revoke/promote), `treasury/authoring` (drafting chassis;
holds no keys by import graph). Production edges: `core/cmd/server → plane`,
`treasury/authority → plane`, `treasury/control → {plane,
treasury/authority}`, `treasury/authoring → plane`. The core never imports
`treasury/*` — applications depend on the SDK, never the reverse. The
mechanical pin (`boundary_test.go`) is two-part and deliberately asymmetric:
CORE packages are pinned exactly, by table; application trees are pinned BY
RULE, never by name — the checks under `core/` carry no application
vocabulary (core-neutrality keeps that true), so a second application tree
is governed the day it appears, without amending the core's tables.

### §7.1 Package set and import adjacency

The intra-repo import adjacency and the package set are **pinned** by
`core/internal/contractcheck/boundary_test.go` (stdlib `go/parser`; runs inside
the named gate). A package missing from the table — or present in it but missing
on disk — is a boundary change. **Adding a package or an edge means amending
this document first**, then the pinned table; never the reverse.

Production edges (intra-module only):

| Package | May import |
|---|---|
| `core/cmd/server` | `core/internal/{durable, gate, idempotency, intent, scoring}`, `plane` |
| `plane` | — (leaf) |
| `core/internal/gate` | `core/internal/{audit, durable, idempotency, intent, lifecycle, scoring}` |
| `core/internal/adapter` | `core/internal/intent` |
| `core/internal/idempotency` | `core/internal/intent` |
| `core/internal/scoring` | `core/internal/intent` |
| `core/internal/audit` | — (leaf) |
| `core/internal/durable` | — (leaf) |
| `core/internal/intent` | — (leaf) |
| `core/internal/lifecycle` | — (leaf) |
| `core/internal/contractcheck` | — (leaf, test-only package) |

Sanctioned test-only extras (edges that exist ONLY in `_test.go` files):
`core/internal/gate` → `core/internal/{adapter, lifecycle}`; `core/cmd/server` →
`core/internal/lifecycle`. Core tests sign fixtures with TEST-LOCAL helpers
(`testKeyFile` in `core/cmd/server/main_test.go` and `plane/plane_test.go`) —
a core test importing an application tree is a layering violation, not a
sanctionable extra.

Application trees (rule-pinned, current instantiation `treasury/`): an
application package may import only `plane` and packages within its own
tree — never `core/internal/*` or `core/cmd/*`; within any tree, only
`<tree>/control` may import `<tree>/authority` (`TestKeyPossessionBoundary`).

Outside `core/internal/` live only `core/cmd/server`, `plane` (core), and
application trees. `contractcheck` ships no production code and no package
imports it.

### §7.2 Pinned package surfaces

Each package's exported surface is fixed. Surfaces already stated in full
elsewhere are cross-referenced rather than repeated: `core/internal/lifecycle` →
§3.1, `core/internal/durable`'s `Record` → §2.3, `core/internal/scoring` → §2.4,
`core/internal/gate` → §4.1.

```go
package intent // core/internal/intent

// Intent is pure data. It carries NO mutable lifecycle state; the gate's runtime
// owns the state machine. The three audit hashes are opaque to this slice.

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
	ActionClass      string // domain action class supplied by the declarant, e.g. "sample-action"
	Criteria         []Criterion
	IdempotencyScope string // e.g. "per-actor"
}

type Intent struct {
	EpisodeSeed      string // determinism source; the intent ID derives from this
	Spec             IntentSpecParams
	IdempotencyKey   IdempotencyKey // required; "" is invalid
	RuleArtifactHash string         // opaque
	IntentSpecHash   string         // opaque
}

// ID is deterministically derived from EpisodeSeed (stable across runs): the
// first 16 hex characters of sha256(EpisodeSeed).
func (i Intent) ID() string
```

```go
package audit // core/internal/audit

// Event is one entry in a per-intent append-only log. Seq is a logical clock
// (0,1,2,…), NEVER wallclock.
type Event struct {
	Seq    int
	Type   string // "DECLARED","RESOLVING","ACTIVE","VERIFYING","SCORED","RECHECK","UNEVALUABLE","IDEMPOTENCY_RESERVED","ACHIEVED","FAILED","FAILED_AT_DISPATCH"
	Detail string // free-form, deterministic (e.g. "alpha:PASS")
}

type EventLog struct{ /* unexported */ }

func NewEventLog() *EventLog

// Append adds an event with the next sequence number and returns it. The first
// appended event has Seq 0, the next 1, and so on (monotonic, gap-free).
func (l *EventLog) Append(typ, detail string) Event

// Events returns the events in order (a copy; callers must not mutate the log).
func (l *EventLog) Events() []Event

// TrajectoryHash returns a deterministic hash over the canonical serialization of
// the events (§6). Same events ⟹ same hash, byte-for-byte.
func (l *EventLog) TrajectoryHash() string
```

```go
package durable // core/internal/durable — Record is §2.3

// Store is the durable, append-only JSONL feed. Single-process, mutex-guarded
// single writer; reads take the same lock. GlobalSeq is persisted and recovered
// by full-scan on Open.
type Store struct{ /* unexported */ }

// Open opens (creating dir and file if absent) <dir>/events.jsonl, full-scans it
// to recover the max GlobalSeq and all prior records, and returns a Store ready
// to append. The file is opened O_APPEND|O_CREATE|O_RDWR.
func Open(dir string) (*Store, error)

// Append writes ONE record, assigning the next GlobalSeq (recovered max + 1, then
// monotonic; first ever = 1), fsyncs the file, appends to the in-memory index, and
// returns the stored record with GlobalSeq set. The caller supplies IntentID,
// IntentSeq, Type, Detail, and (on ACHIEVED only) the four trace fields; the
// caller MUST leave r.GlobalSeq == 0 — Append assigns it and any caller-set value
// is ignored/overwritten.
func (s *Store) Append(r Record) (Record, error)

// Since returns all records with GlobalSeq > sinceGlobalSeq, in ascending GlobalSeq
// order. If typ != "", only records whose Type == typ are returned. The returned
// slice is a fresh copy.
func (s *Store) Since(sinceGlobalSeq int, typ string) []Record

// ByIntent returns all records for intentID, in ascending IntentSeq order (the
// per-intent event log). Fresh copy.
func (s *Store) ByIntent(intentID string) []Record

// Close closes the underlying file.
func (s *Store) Close() error
```

```go
package idempotency // core/internal/idempotency

// Store tracks reserved idempotency keys. Reserve is the dispatch-edge gate.
// A store from NewStore is in-memory only; a store from OpenStore is durable.
// Both satisfy the identical Reserve contract. Reserve is guarded by an
// unexported sync.Mutex: the boot-time store is shared across concurrent
// requests.
type Store struct{ /* unexported */ }

// NewStore returns a fresh, empty in-memory store (no file IO).
func NewStore() *Store

// OpenStore opens (creating dir/file if absent) a durable, file-backed idempotency
// store at <dir>/idempotency.jsonl, full-scans it to recover ALL previously
// reserved keys, and returns a Store whose successful Reserve appends
// {"key":..,"id":..} and fsyncs BEFORE returning ok=true. Reserve semantics are
// unchanged (fresh ok; collision refused; empty refused). Reservations survive
// process restart.
func OpenStore(dir string) (*Store, error)

// Close closes the underlying file of a durable store; a no-op in-memory.
func (s *Store) Close() error

// Reserve attempts to claim key for the given intent ID. It returns ok=true on a
// fresh key (now reserved), and ok=false on collision (key already reserved, by any
// intent). Empty key ⟹ ok=false (absent key is unevaluable).
func (s *Store) Reserve(id string, key intent.IdempotencyKey) (ok bool)
```

On a durable store, `Reserve` takes the mutex, checks the map, and on a fresh
non-empty key appends the line, `Sync()`s, then inserts into the map and returns
`ok=true`. **A collision or empty key must never write to disk.** A durable
write/fsync failure is fail-closed: the key is NOT reserved and `Reserve`
returns `ok=false`.

```go
package adapter // core/internal/adapter — TEST-ONLY

// The package is TEST-ONLY: no non-test .go file imports it (the gate emits and
// stops). It is exercised solely by its own tests and by the acceptance suite's
// feedConsumer (§5.3).

// SettlementEvent is what a consumer records on ACHIEVED. Deterministic from the
// intent + key + seed.
type SettlementEvent struct {
	IntentID string
	Key      intent.IdempotencyKey
	Payload  string // deterministic, derived from intent (no wallclock, no randomness)
}

type Adapter interface {
	OnAchieved(i intent.Intent) (SettlementEvent, error)
}

// ReferenceAdapter is the deterministic, idempotent reference consumer. It is
// idempotent on the declared key: a second OnAchieved with the same key returns the
// SAME event and records NO duplicate.
type ReferenceAdapter struct{ /* unexported */ }

func NewReferenceAdapter() *ReferenceAdapter
func (a *ReferenceAdapter) OnAchieved(i intent.Intent) (SettlementEvent, error)

// Settlement returns all distinct settlement events recorded so far (one per key),
// in the order their keys were first recorded.
func (a *ReferenceAdapter) Settlement() []SettlementEvent
```

---

## §8 Scorer service contract

The Python resolver+scorer service lives at `core/scorer/` in THIS repo —
distribution `intent-scorer`, package `scorer`, FastAPI + pydantic + uvicorn,
Python 3.11+, pytest for the gate. It is the plane's ONE resolver+scorer
service. The Go stdlib-only rule does not apply here; the Python side never
imports from the Go tree and vice versa.

```
core/scorer/
  pyproject.toml              # fastapi, pydantic, uvicorn; dev: pytest, httpx
  src/scorer/models.py        # EvalRequest / EvalResponse (pydantic, extra="ignore")
  src/scorer/evaluator.py     # evaluate(req, fact) -> "PASS"|"FAIL"|"UNEVALUABLE"
  src/scorer/facts.py         # FactSource protocol + StaticFactSource(dict)
  src/scorer/resolver.py      # ArtifactResolver protocol + NullResolver + KeArtifactResolver
                              #   (ke-artifact-py behind a lazy import, Linux-only, executor-run)
  src/scorer/app.py           # FastAPI: POST /ml/evaluate, GET /healthz
  src/scorer/__main__.py      # boot: env config, all-or-nothing resolver
  tests/                      # pure-Python: the §2.4 matrix, no wheel, no network
```

**Evaluation.** `evaluate`: fact `>= threshold` ⟹ `PASS`; `< threshold` ⟹
`FAIL`; fact `None` or any exception ⟹ `UNEVALUABLE`. Deterministic given the
same fact — fixed 2-decimal `basis` rendering, no wallclock, no randomness. The
app's catch-all around the whole handler IS the contract: every internal failure
is an evaluation that answers `UNEVALUABLE`.

**Facts.** `FactSource.get(criterion, intent_id) -> float | None`.
`StaticFactSource` is both the test double and a deployment's configuration; a
live fact source is a LATER slice — **do not fake one**. `SCORER_FACTS_JSON`
carries a criterion → number JSON object; **unset means an EMPTY fact map**, so
every criterion is `UNEVALUABLE` and the gate refuses everything. That
fail-closed posture is what a neutral core boots into; a demonstration
deployment injects its own facts file through the same seam (this repo's
`treasury/facts.json`). A non-object value is a boot refusal.

**Resolver, all-or-nothing.** `ArtifactResolver.verify(rule_artifact_hash,
intent_spec_hash) -> bool`, run via `loop.run_in_executor` — `ke-artifact-py`'s
`verify()` holds the GIL through crypto and must not stall a concurrent
`/ml/evaluate` (recorded GIL caveat).

- `SCORER_ARTIFACT_DIR`, `SCORER_ATLAS_INPUTS_DIR`, `SCORER_EXPORTED_AT_UNIX`:
  set **all three or none**. Partially set ⟹ refuse to boot. Set without the
  `ke-artifact-py` wheel ⟹ the lazy import crashes the boot loudly. **A server
  the operator configured to verify must never silently not-verify.**
- Unset ⟹ `NullResolver`, the Windows-local default: verification is **skipped
  and said so on the wire** (`basis` carries `resolver=null: verification
  skipped; `). `NullResolver.verify` raises if ever called — calling it is a
  bug, and the app's catch-all fails that closed to `UNEVALUABLE`.
- `KeArtifactResolver` indexes a directory of `.kew` files once, lazily, by
  manifest artifact hash; a file that does not decode as a canonical artifact is
  simply absent from the index, so requesting its hash fails closed. `verify()`
  returns `True` only if EVERY hash the gate sent is present, verdict-`verified`
  under the configured keydir/context/policy/registry evidence at the configured
  export instant, and re-addresses to the exact hash requested. The export
  instant is explicit — **no wallclock**, so determinism stays testable.
  Malformed input JSON raises rather than returning `False`: a config bug is an
  internal error, which also fails closed but with a distinct, diagnosable
  `basis`.
- **Hashless verify is refused.** `verify()` with no hash at all returns
  `False`: `all([])` is `True`, which would be fail-open. This is the
  scorer-side twin of invariant 2's non-vacuity (§5.1).

**The wheel is absent on Windows-local BY DESIGN** (it only builds on
Linux/CI — never rebuild a binding). Wheel-dependent tests **skip visibly**; a
skipped test is never a green one.

The service is run with `python -m scorer` (`SCORER_HOST`/`SCORER_PORT`); the
FastAPI title is `intent-scorer`.

---

## §9 Fixture discipline

Golden JSON pairs live at `core/contract/scorer/` — `request-<case>.json` /
`response-<case>.json` for five cases: **pass, fail,
unevaluable-unknown-criterion, volatile-dispatch, hashes-present**.

- The Go side (`core/internal/scoring/scorer_test.go`) asserts `EvalRequest`
  marshals **byte-identically** to each request fixture and decodes each
  response fixture to the expected `Score`.
- The Python side (`core/scorer/tests/test_fixtures.py`) parses each request
  fixture with pydantic and asserts its serialized response equals the response
  fixture byte-for-byte.
- Python locates them at `core/contract/scorer` by default;
  `SCORER_CONTRACT_DIR` overrides. **If absent, those tests skip visibly — a
  skipped fixture test is never a green one.**

These fixture bytes are the ONLY surface shared between the two languages, and
they are **frozen**: changing them is a wire change requiring both lanes to
re-green in the same commit.

**Neutrality exemption.** The fixtures carry a domain criterion name
(`"balance"`) from the plane's original deployment, so `core/contract/scorer/`
is exempt from the core-neutrality gate (§5.1 invariant 8), along with the two
files that reproduce those bytes:
`core/internal/scoring/scorer_test.go` and
`core/scorer/tests/test_fixtures.py`. The exemption is byte-pinned and
fixture-coupled, not a judgment call — it is stated explicitly in
`neutrality_test.go`. Regenerating the fixtures with neutral exemplar names
(both lanes re-greening in the same change) is a recorded `docs/ROADMAP.md`
follow-up.

---

## §10 Provenance

This document was consolidated on **2026-08-03** from a four-file amendment
chain, which it **replaces in full**:

| Replaced file | Contributed |
|---|---|
| `CONTRACT.md` (slice 1) | lifecycle, intent/audit/scoring/adapter/idempotency surfaces, invariants 1–6, the gate algorithm |
| `CONTRACT-DURABILITY.md` (durability + emit-and-observe) | `internal/durable`, `OpenStore`, the emit-only gate, the V2 server surface, the acceptance successor map |
| `CONTRACT-SCORER.md` (the `/ml/evaluate` seam) | the wire protocol, both fail-closed matrices, the Python service, the golden fixtures |
| `CONTRACT-INTERFACE.md` (2026-08-02) | role vocabulary, the four-route surface, the thin-spec defense, cause classes, `ACHIEVED` as the public term |

The chain resolved conflicts by **later-wins**; this document states only the
winners, so the amendment framing is gone. **Git history holds the originals**
(baseline commit `a290d17`); it is the only lineage record and no summary here
substitutes for it.

**Amendments folded in by the 2026-08-03 repositioning**, applied throughout
above rather than annotated inline:

- Module path `github.com/hossainpazooki/treasury-intent-controller` →
  `github.com/hossainpazooki/intent-plane`.
- Go tree moved under `core/`: `cmd/server` → `core/cmd/server`, `internal/...`
  → `core/internal/...`, `contract/scorer` → `core/contract/scorer`.
- Gate env `TIC_DATA_DIR`/`TIC_SCORER_URL`/`TIC_ADDR` →
  `INTENT_DATA_DIR`/`INTENT_SCORER_URL`/`INTENT_ADDR`.
- Python service `scorer/` → `core/scorer/`, package `tis` → `scorer`,
  distribution `treasury-intent-scorer` → `intent-scorer`, env `TIS_*` →
  `SCORER_*`, test env `TIC_CONTRACT_DIR`/`TIC_ATLAS_DIR` →
  `SCORER_CONTRACT_DIR`/`SCORER_ATLAS_DIR`.
- Server binary `bin/tic.exe` → `bin/intent-gate.exe`.
- Domain exemplars in core vocabulary neutralized (`sample-action`,
  `alpha`/`beta`/`gamma`, `per-actor`); the frozen wire fixtures keep theirs
  under the §9 exemption.

**Deliberately NOT carried forward** (build-phase scaffolding, spent once the
build landed; recoverable from git history if ever needed): the three
file → owner maps that allocated files to parallel build agents, and the
phase-0/phase-1 scaffold-then-build narrative that went with them. No exported
name, signature, wire key, route, env name, or normative rule was dropped with
them.
