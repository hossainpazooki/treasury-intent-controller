# CONTRACT-DURABILITY.md — treasury-intent-controller (durability + emit-and-observe)

> **This file AMENDS `CONTRACT.md`. It lists DELTAS ONLY.** Every package, type, signature, and invariant in `CONTRACT.md` not named here is UNCHANGED and remains the source of truth. Where a symbol appears in both files, the declaration **here** wins. Module path `github.com/pazooki/treasury-intent-controller`, Go 1.26, **stdlib only, no new modules**, never run git. All test IO uses `t.TempDir()`; no test writes `./data`.

This slice makes the gate's authority **durable** and **observable** so a later external consumer (COMPASS, polling via cron) can settle from it. Four changes:

1. A durable, file-backed, append-only per-intent event log (`internal/durable`), carrying a global monotonic `GlobalSeq` in addition to the preserved per-intent `Seq`. Per-intent determinism invariants (logical clock, `TrajectoryHash`) are **preserved byte-for-byte**.
2. A cursor read surface: `GET /v2/events?since=<globalSeq>` and `GET /v2/intents/{id}/events`.
3. A durable, boot-time, file-backed idempotency store (fixes the per-request fresh-store bug at `cmd/server/main.go:159`).
4. Emit-and-observe: the gate STOPS at appending the single `ACHIEVED` record to the durable log and no longer calls `adapter.OnAchieved` in-process. The `ReferenceAdapter` becomes TEST-ONLY, driven by a test consumer that reads the durable feed.

---

## §V2.0 Locked decisions (do not relitigate)

- stdlib only; no new modules. JSONL, one JSON object per line, `\n`-terminated.
- Single-process server; a **mutex-guarded single writer** per durable store. Reads (`Since`, `ByIntent`) take the same lock.
- fsync (`*os.File.Sync()`) after **every** append, before returning success.
- Full-scan recovery on `Open`: read the file start-to-end, recover the max `GlobalSeq`, all reserved keys, and (for the feed) all records for reads.
- No wallclock anywhere; no `math/rand`; no map-iteration order in any log.
- The server wires the **durable feed** and the **durable idempotency store** ONCE at boot. The per-request `Gate` value is a thin wrapper over those shared singletons (it may be constructed per request to carry the per-request scorer; the **stores** are never per-request).
- `GlobalSeq` starts at **1** (first record). `since=0` returns everything. `GlobalSeq` is **never** part of the per-intent `TrajectoryHash` (it is non-deterministic across replay by design).

---

## §V2.1 NEW package: `internal/durable`

The durable, append-only feed. One physical file `<dir>/events.jsonl`. Every gate event for every intent is mirrored here with a `GlobalSeq`. This is the shared substrate every other V2 agent reads/writes.

```go
package durable

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
	IdempotencyKey   string `json:"idempotency_key,omitempty"`   // ACHIEVED only
	RuleArtifactHash string `json:"rule_artifact_hash,omitempty"` // ACHIEVED only
	IntentSpecHash   string `json:"intent_spec_hash,omitempty"`   // ACHIEVED only
	TrajectoryHash   string `json:"trajectory_hash,omitempty"`    // ACHIEVED only
}

// Store is the durable, append-only JSONL feed. Single-process, mutex-guarded
// single writer; reads take the same lock. GlobalSeq is persisted and recovered by
// full-scan on Open.
type Store struct{ /* unexported: mu sync.Mutex; f *os.File; globalSeq int; records []Record */ }

// Open opens (creating dir and file if absent) <dir>/events.jsonl, full-scans it to
// recover the max GlobalSeq and all prior records, and returns a Store ready to
// append. The file is opened O_APPEND|O_CREATE|O_RDWR.
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

**Recovery/encoding rules (contractual):** each line is `json.Marshal(Record)` + `'\n'`. On `Open`, scan line-by-line (raise the `bufio.Scanner` buffer or use `bufio.Reader`), `json.Unmarshal` each into a `Record`, track `max(GlobalSeq)` and retain records for reads. A trailing partial/blank line is ignored (torn last write); everything before it is authoritative. Recovery MUST NOT re-fsync or rewrite existing lines.

---

## §V2.2 CHANGED package: `internal/idempotency`

`Store`, `NewStore`, and `Reserve` keep their **exact** slice-1 signatures and semantics. `NewStore()` remains the **in-memory** store (no file IO) used by unit tests. Two additions:

- `Store` gains an unexported `sync.Mutex`; `Reserve` is now mutex-guarded (the boot-time store is shared across concurrent requests). Reserve semantics are UNCHANGED: fresh key ⟹ `ok=true` (now reserved); collision ⟹ `ok=false`; empty key ⟹ `ok=false`.
- A new durable constructor:

```go
// OpenStore opens (creating dir/file if absent) a durable, file-backed idempotency
// store at <dir>/idempotency.jsonl, full-scans it to recover ALL previously
// reserved keys, and returns a Store whose successful Reserve appends
// {"key":..,"id":..} and fsyncs BEFORE returning ok=true. Reserve semantics are
// unchanged (fresh ok; collision refused; empty refused). Reservations survive
// process restart. A store from NewStore() is in-memory only; a store from
// OpenStore is durable — both satisfy the same Reserve contract.
func OpenStore(dir string) (*Store, error)
```

`Reserve` on a file-backed store: under the mutex, check the map; on a fresh non-empty key, append the line, `Sync()`, then insert into the map and return `ok=true`; on collision/empty, no write, return `ok=false`. A collision must never write to disk.

---

## §V2.3 CHANGED package: `internal/gate`

Breaking changes: the gate no longer holds or calls an adapter, `Authorize` returns an error (durable IO can fail), and `Result.Settlement` is **removed** (cleanest breaking shape — the gate emits and stops; a downstream consumer settles from the feed).

```go
package gate

import (
	"context"

	"github.com/pazooki/treasury-intent-controller/internal/audit"
	"github.com/pazooki/treasury-intent-controller/internal/durable"
	"github.com/pazooki/treasury-intent-controller/internal/idempotency"
	"github.com/pazooki/treasury-intent-controller/internal/intent"
	"github.com/pazooki/treasury-intent-controller/internal/lifecycle"
	"github.com/pazooki/treasury-intent-controller/internal/scoring"
	// NOTE: internal/adapter is NO LONGER imported here.
)

// Result is the terminal outcome of one authorization. Settlement is REMOVED: the
// gate no longer settles. Events + TrajectoryHash are the per-intent log (no
// GlobalSeq) and are byte-identical across replay, exactly as in slice 1.
type Result struct {
	Terminal       lifecycle.State // ACHIEVED | FAILED | FAILED_AT_DISPATCH
	Reason         string
	Events         []audit.Event   // per-intent append-only log (unchanged shape)
	TrajectoryHash string          // per-intent hash over Events (unchanged)
	AchievedSeq    int             // GlobalSeq of the emitted ACHIEVED record; 0 if not ACHIEVED
}

// Gate authorizes intents against the scorer, the durable feed, and the idempotency
// store. It has NO adapter.
type Gate struct{ /* unexported: scorer scoring.Scorer; feed *durable.Store; store *idempotency.Store */ }

// New constructs a Gate over the scorer, the (shared, durable) feed, and the
// (shared, durable) idempotency store.
func New(s scoring.Scorer, feed *durable.Store, store *idempotency.Store) *Gate

// Authorize drives the full lifecycle deterministically. It mirrors EVERY event to
// the durable feed as it appends to the in-memory per-intent log, preserving the
// per-intent Seq and TrajectoryHash exactly (slice-1 §invariants 1-6 all hold).
//
// Deltas from CONTRACT.md's algorithm:
//   - For each in-memory log.Append(typ, detail), also feed.Append a durable.Record
//     {IntentID: i.ID(), IntentSeq: e.Seq, Type: e.Type, Detail: e.Detail}.
//   - Step 5 (authorize) becomes EMIT-ONLY: append the ACHIEVED event in-memory,
//     compute th := log.TrajectoryHash() (includes ACHIEVED, same value as slice 1),
//     then feed.Append the ACHIEVED durable.Record carrying the four trace fields
//     {IdempotencyKey, RuleArtifactHash, IntentSpecHash, TrajectoryHash: th}. Set
//     Result.AchievedSeq = that record's GlobalSeq. It does NOT call any adapter.
//   - Any feed.Append error aborts: return the partial Result built so far and a
//     non-nil error. A non-nil error means the authorization did not complete and
//     no terminal guarantee is implied.
//
// Determinism: per-intent Events and TrajectoryHash are byte-identical across
// independent runs; GlobalSeq is explicitly NOT part of Events or the hash.
func (g *Gate) Authorize(ctx context.Context, i intent.Intent) (Result, error)
```

### `internal/gate/acceptance_test.go` — REWRITTEN

Uses `scoring.FakeScorer`, `durable.Open(t.TempDir())`, `idempotency.NewStore()` (and `idempotency.OpenStore(t.TempDir())` for the restart case). It defines a **test-only feed consumer** helper (the successor to the in-process adapter call):

```go
// feedConsumer drains ACHIEVED records from a durable feed past a cursor and calls
// OnAchieved on a ReferenceAdapter (recompute path), enforcing at-most-once via the
// adapter's key-idempotency PLUS its own cursor. Poll(feed) is safe to call
// repeatedly and after a feed reopen; it never double-settles a key.
type feedConsumer struct{ ref *adapter.ReferenceAdapter; cursor int; intents map[string]intent.Intent }
```

The consumer looks up the original `intent.Intent` by `record.IntentID` from the map the test populated at submit time (it cannot invert `ID()` from the record), advances its cursor to `max(GlobalSeq)` seen, and calls `ref.OnAchieved`. Every non-ACHIEVED "no settlement" assertion becomes: `feed.Since(0, "ACHIEVED")` for that intent is empty **and** `consumer.ref.Settlement()` is empty for that key.

---

## §V2.4 UNCHANGED, RECLASSIFIED package: `internal/adapter`

`adapter.go` and `adapter_test.go` are **byte-unchanged**. The package is now **TEST-ONLY**: no non-test `.go` file imports it (`internal/gate/gate.go` and `cmd/server/main.go` drop the import). It is exercised solely by `internal/adapter/adapter_test.go` and by the gate acceptance test's `feedConsumer`. `SettlementEvent`, `Adapter`, `ReferenceAdapter`, `NewReferenceAdapter`, `OnAchieved`, and `Settlement` keep their exact signatures.

---

## §V2.5 CHANGED package: `cmd/server`

Boot wires the durable stores ONCE; handlers share them; the response shape drops `settlement`; two read endpoints are added.

**Boot (`main`)**: `dir := os.Getenv("TIC_DATA_DIR")` defaulting to `"./data"`; `feed, _ := durable.Open(dir)`; `istore, _ := idempotency.OpenStore(dir)` (fatal on error). Handlers close over `feed` and `istore`. **The per-request `idempotency.NewStore()` at slice-1 `main.go:159` is DELETED** — the shared `istore` is passed to `gate.New` instead.

**`POST /v2/intents`** — decode as slice 1 (`intentRequest` unchanged), build the `forceScorer` from `force_scores`, construct `gate.New(scorer, feed, istore)` over the **shared** stores, run `Authorize` (now `(Result, error)`; on error → `500`). Response DTO (settlement removed, `achieved_seq` added):

```go
type intentResponse struct {
	Terminal       string `json:"terminal"`
	Reason         string `json:"reason"`
	TrajectoryHash string `json:"trajectory_hash"`
	AchievedSeq    int    `json:"achieved_seq,omitempty"` // >=1 iff ACHIEVED
}
```

**`GET /v2/events?since=<globalSeq>&type=<optional>`** — `since` parses to int (absent/blank ⟹ 0); optional `type` (e.g. `ACHIEVED`). Returns `feed.Since(since, type)` serialized as the raw `durable.Record` JSON, wrapped:

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

**`GET /v2/intents/{id}/events`** — Go 1.22+ pattern `GET /v2/intents/{id}/events`, `id := r.PathValue("id")`. Returns `feed.ByIntent(id)` in per-intent `Seq` order:

```jsonc
// 200 application/json
{ "intent_id": "ab12…", "events": [ /* durable.Record objects, ascending intent_seq */ ] }
```

**`GET /healthz`** — unchanged (`200 "ok"`).

Wire shape rule: the objects inside `events[]` are the `durable.Record` JSON verbatim (the tags in §V2.1 ARE the wire contract); do not re-tag them in a DTO.

### `cmd/server/main_test.go` — REWRITTEN

Drives the shared-store server via `httptest`, using `t.Setenv("TIC_DATA_DIR", t.TempDir())` and a boot helper that builds the mux over stores opened in that temp dir (no `./data`, no bound port). Covers: healthz; ACHIEVED (terminal + `achieved_seq >= 1` + the record appears in `GET /v2/events?type=ACHIEVED`); FAILED_AT_DISPATCH (no `achieved_seq`, no ACHIEVED record in the feed); cursor paging (`since` advances, `next_since` correct); per-intent endpoint order; and the **restart** case (reopen stores over the same dir; at-most-once must hold).

---

## §V2.6 Invariant successor map (slice-1 §12 a–f → V2)

| Slice-1 acceptance invariant | V2 successor assertion |
|---|---|
| **(a) Determinism/replay** — two fresh Gates ⟹ equal `Events`, `TrajectoryHash`, `Settlement` bytes; `OnAchieved` ran (recompute). | Two Gates, **each with its own `durable.Open(t.TempDir())`** + own idempotency store, same intent ⟹ equal per-intent `Events` and `TrajectoryHash` (byte-identical; `GlobalSeq` explicitly excluded from the compare). The ACHIEVED record's `trajectory_hash` == `Result.TrajectoryHash`. **Settlement-bytes successor:** a `feedConsumer` draining each independent feed calls `OnAchieved` exactly once per key and the resulting `SettlementEvent`s are byte-identical (payload determinism unchanged). |
| **(b) Fail-closed unevaluable + absent key** — `FAILED`, never `ACHIEVED`, `UNEVALUABLE` event present, no settlement. | Terminal/reason/`UNEVALUABLE`-event assertions unchanged. **No-settlement successor:** `feed.Since(0,"ACHIEVED")` has zero records for that intent AND the consumer's `Settlement()` is empty. |
| **(c) Verification failure** — `FAILED` naming the criterion, no settlement. | Unchanged terminal/reason. No-settlement successor: no ACHIEVED record in the feed for that intent; consumer ledger empty for its key. |
| **(d) Volatile re-verify** — `FAILED_AT_DISPATCH`, no settlement, stable scored once / volatile twice (`FakeScorer.Calls`). | **Unchanged** (scoring path untouched by emit-and-observe): same terminal, same call counts. No-settlement successor as in (c). |
| **(e) Idempotency collision** — first `ACHIEVED`, second `FAILED_AT_DISPATCH`, exactly one settlement for the key. | Same terminals with a **shared** store. Feed has **exactly one** ACHIEVED record for the key; the consumer records **exactly one** settlement. **NEW restart clause:** `Close`+`OpenStore` the idempotency store from the same dir, submit a third intent with the same key ⟹ still `idempotency-collision`; reopen the feed, re-poll the consumer from cursor 0 ⟹ still one ACHIEVED, no new settlement (at-most-once **across process restart**). |
| **(f) Terminal separation** — `Settlement == nil` for every FAILED/FAILED_AT_DISPATCH; `ACHIEVED` only after volatile re-check passed. | For every FAILED/FAILED_AT_DISPATCH result, `feed.Since(0,"ACHIEVED")` contains **no** record for that intent (the feed is the successor to `Settlement==nil`). The ACHIEVED path has exactly one ACHIEVED record, ordered **after** the `RECHECK` record of the volatile criterion (assert by `IntentSeq` / `GlobalSeq` order in `ByIntent`). |
| **main_test (slice 1):** `TestIntentsAchieved` asserts `settlement` present; `TestIntentsFailedAtDispatch` asserts `settlement` nil. | `settlement` field is gone. Successors: ACHIEVED ⟹ `achieved_seq >= 1` + record visible via `GET /v2/events?type=ACHIEVED`; FAILED_AT_DISPATCH ⟹ `achieved_seq` absent + no ACHIEVED record in the feed. |

---

## §V2.7 File → owner map (NO build-phase overlaps)

Two phases. **Phase 0 (scaffold)** brings the whole tree to a COMPILING baseline (`go build ./...` clean; pre-existing test files owned by build agents may not compile until phase 1 rewrites them) and ships two things in full: this contract and the durable leaf's in-process semantics. **Phase 1 (build)** replaces each owned file with its real implementation + tests; each build agent owns a disjoint set and its package tests run against the scaffold baseline while the others are still stubbed.

### scaffold (phase 0)
| File | State scaffold leaves it in |
|---|---|
| `CONTRACT-DURABILITY.md` | this document, in full |
| `internal/durable/store.go` | **FULL in-memory-correct implementation**: `Record` with exact JSON tags; `Open`/`Append`/`Since`/`ByIntent`/`Close` with correct in-process semantics and GlobalSeq assignment, but **NO file persistence yet** (a slice + counter). Compiles; in-process cursor tests pass. build-durable adds file/fsync/recovery. |
| `internal/gate/gate.go` | new `Result` (no `Settlement`, `+AchievedSeq`), new `New(scorer, *durable.Store, *idempotency.Store)`, `Authorize` returning `(Result, error)` — **stub body** returns `Result{}, nil`; `adapter` import dropped. |
| `internal/idempotency/store.go` | add `sync.Mutex` field + mutex-guard `Reserve`; add `OpenStore` **stub** that delegates to the in-memory path (no file yet). `NewStore`/`Reserve` semantics preserved. |
| `cmd/server/main.go` | edited to compile against the new gate signature + boot-time shared stores + the three endpoints (handlers may return minimal bodies). build-server writes the real handlers. |

### build agents (phase 1, disjoint, parallel)
| Agent | Files owned | Independently testable because |
|---|---|---|
| **build-durable** | `internal/durable/store.go`, `internal/durable/store_test.go` | Leaf; stdlib only. Adds file/O_APPEND/fsync/full-scan recovery to scaffold's in-memory impl. Tests (recovery across reopen, GlobalSeq monotonic across intents & reopen, `Since` cursor+type filter, `ByIntent` order) use `t.TempDir()` and depend on nothing else. |
| **build-idempotency** | `internal/idempotency/store.go`, `internal/idempotency/store_test.go` | Deps: stdlib + `internal/intent` only. Implements `OpenStore` persistence + recovery; keeps all slice-1 tests green; adds restart-recovery + empty-key-no-write + concurrent-Reserve tests. |
| **build-gate** | `internal/gate/gate.go`, `internal/gate/acceptance_test.go` | Deps: real `audit`/`scoring`/`lifecycle`/`intent` (unchanged), `adapter` (test-only), and the durable + idempotency stores — the **in-process** semantics scaffold already provides suffice for every gate assertion (feed observation is same-process). Builds the `feedConsumer`; rewrites §12 with the successor map. (The across-restart clause it exercises via `Close`/reopen of a real `durable.Store` + `OpenStore` once those are built; scheduled after build-durable/idempotency in the pipeline, or validated at the gate.) |
| **build-server** | `cmd/server/main.go`, `cmd/server/main_test.go` | Integrative: real handlers over shared stores + real gate. Depends on build-gate + build-durable + build-idempotency; runs last in the pipeline. Tests use `t.Setenv("TIC_DATA_DIR", t.TempDir())`. |

`internal/adapter/{adapter.go,adapter_test.go}` — **unchanged, no owner** (test-only reclassification only). `internal/{lifecycle,intent,audit,scoring}/*` — **unchanged**.

Ordering note (honesty): perfect "test in isolation" holds for build-durable and build-idempotency (leaves) and for build-gate (against scaffold's in-process durable). build-server is inherently integrative and is the terminal build stage; its green state is confirmed only at the integration gate over the real stack.

---

## §V2.8 Hard rules for every agent

- Code against THIS contract + `CONTRACT.md`, NOT each other's files. Own ONLY your file(s). Do not change any exported name/signature/package path fixed here.
- stdlib only; no new modules; no network in tests.
- **All test IO under `t.TempDir()` via `TIC_DATA_DIR`. No test may write `./data` or any repo-relative path.** Server default `./data` is for `main` only.
- fsync after every durable append, before returning success. Mutex-guard the single writer; reads take the same lock. No sleeps to paper over a race.
- Deterministic only: no wallclock, no unseeded `math/rand`, no map-iteration order in any log. `GlobalSeq` never enters the per-intent `TrajectoryHash`.
- Never weaken or skip an existing assertion; every slice-1 invariant must have the §V2.6 successor. Never run git.

---

## §V2.9 Load-bearing claims (each with a skeptic's probe)

1. **Feed durability/recovery.** Events survive process restart with `GlobalSeq` intact. *Probe:* `go test ./internal/durable -run Recovery` — append N records across two intents, `Close`, `Open` same dir, assert `ByIntent`/`Since` return all N and `max(GlobalSeq)` is preserved; corrupt/truncate the last line and assert everything before it still recovers.
2. **GlobalSeq is globally monotonic with no reset or gap, across intents and across reopen.** *Probe:* `go test ./internal/durable -run GlobalSeq` — interleave appends from two intent IDs, assert `seq` strictly increases 1,2,3,…; `Close`, `Open`, append once more, assert it continues at `prevMax+1`.
3. **Idempotency at-most-once now holds ACROSS requests AND ACROSS process restart.** *Probe:* `go test ./internal/idempotency -run Restart` — `OpenStore`, `Reserve(k)==true`, `Close`, `OpenStore` same dir, `Reserve(k)==false`. Server probe: `POST /v2/intents` same key, reboot the server over the same `TIC_DATA_DIR`, `POST` again ⟹ second is `FAILED_AT_DISPATCH` "idempotency-collision". *Skeptic:* confirm the store never writes outside `TIC_DATA_DIR`; assert the `main.go:159` fresh-store construction is gone (`grep -n "NewStore\|OpenStore" cmd/server/main.go`).
4. **Per-intent determinism is preserved and independent of GlobalSeq.** *Probe:* `go test ./internal/gate -run Determinism` — two Gates over separate temp feeds, same intent ⟹ equal `Events` + `TrajectoryHash`; assert the two runs' `GlobalSeq` values may differ while the hash does not.
5. **Emit-and-observe: the gate never settles in-process, and settlement is at-most-once from the feed.** *Probe:* `grep -n "adapter" internal/gate/gate.go` returns **nothing** (no import, no `OnAchieved` call); `go test ./internal/gate -run Idempotency|Consumer` — exactly one ACHIEVED record per key in the feed; a `feedConsumer` that polls the feed twice AND after a reopen records exactly one settlement per key. *Skeptic:* double-poll + reopen, `len(ref.Settlement())==1`.
6. **Cursor reads are correct and ordered.** *Probe:* `go test ./cmd/server` — `GET /v2/events?since=N` returns exactly `GlobalSeq>N` ascending; `type=ACHIEVED` filters to ACHIEVED only; `next_since` equals the max returned; `GET /v2/intents/{id}/events` returns that intent's records in ascending `intent_seq` with the ACHIEVED record ordered after its `RECHECK` record.
