package gate

// §12 acceptance tests for the authorization gate.
//
// These tests pin the spec invariants from CONTRACT.md to executable assertions.
// They are written against the contract's Gate API only — never against gate.go's
// internals — using scoring.FakeScorer, adapter.NewReferenceAdapter(), and
// idempotency.NewStore(). They compile against the current stubs and PASS once
// gate.go implements the documented algorithm; do not weaken any assertion to make
// a stub pass.

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/pazooki/treasury-intent-controller/internal/adapter"
	"github.com/pazooki/treasury-intent-controller/internal/audit"
	"github.com/pazooki/treasury-intent-controller/internal/idempotency"
	"github.com/pazooki/treasury-intent-controller/internal/intent"
	"github.com/pazooki/treasury-intent-controller/internal/lifecycle"
	"github.com/pazooki/treasury-intent-controller/internal/scoring"
)

// --- helpers ---------------------------------------------------------------

func crit(name string, vol intent.Volatility) intent.Criterion {
	return intent.Criterion{Name: name, Threshold: 0.5, Volatility: vol}
}

// mkIntent builds an intent with the given seed, idempotency key, spec hash, and
// criteria. EpisodeSeed drives the deterministic ID; IntentSpecHash distinguishes
// near-duplicates for the idempotency-collision test.
func mkIntent(seed, key, specHash string, crits ...intent.Criterion) intent.Intent {
	return intent.Intent{
		EpisodeSeed: seed,
		Spec: intent.IntentSpecParams{
			ActionClass:      "payment",
			Criteria:         crits,
			IdempotencyScope: "payer",
		},
		IdempotencyKey:   intent.IdempotencyKey(key),
		RuleArtifactHash: "rule-" + specHash,
		IntentSpecHash:   specHash,
	}
}

func newScorer() *scoring.FakeScorer {
	return &scoring.FakeScorer{Results: map[scoring.ScoreKey]scoring.Score{}}
}

// countByCriterion counts how many times a criterion of the given name was scored,
// across all phases, in a FakeScorer's recorded call log.
func countByCriterion(calls []scoring.ScoreKey, name string) int {
	n := 0
	for _, k := range calls {
		if k.Criterion == name {
			n++
		}
	}
	return n
}

func hasEventType(events []audit.Event, typ string) bool {
	for _, e := range events {
		if e.Type == typ {
			return true
		}
	}
	return false
}

// countingAdapter wraps a real adapter.Adapter (a ReferenceAdapter here) and counts
// OnAchieved invocations. It lets the determinism test prove the RECOMPUTE path: an
// independent replaying Gate must CALL OnAchieved again, not re-read a stored event.
type countingAdapter struct {
	inner adapter.Adapter
	calls int
}

func (c *countingAdapter) OnAchieved(i intent.Intent) (adapter.SettlementEvent, error) {
	c.calls++
	return c.inner.OnAchieved(i)
}

// --- (a) determinism / replay ---------------------------------------------

func TestDeterminismReplay(t *testing.T) {
	i := mkIntent("seed-alpha", "key-pay-1", "spec-hash-1",
		crit("balance", intent.Stable),
		crit("fx-rate", intent.Volatile),
	)

	// Two fully independent Gates: fresh scorer, fresh ReferenceAdapter, fresh
	// Store each. Replay = run the same intent through the second pipeline from
	// scratch.
	ref1 := adapter.NewReferenceAdapter()
	ref2 := adapter.NewReferenceAdapter()
	ca1 := &countingAdapter{inner: ref1}
	ca2 := &countingAdapter{inner: ref2}

	g1 := New(newScorer(), ca1, idempotency.NewStore())
	g2 := New(newScorer(), ca2, idempotency.NewStore())

	r1 := g1.Authorize(context.Background(), i)
	r2 := g2.Authorize(context.Background(), i)

	if r1.Terminal != lifecycle.Achieved {
		t.Fatalf("run 1 terminal = %q, want ACHIEVED (reason %q)", r1.Terminal, r1.Reason)
	}
	if r2.Terminal != lifecycle.Achieved {
		t.Fatalf("run 2 terminal = %q, want ACHIEVED (reason %q)", r2.Terminal, r2.Reason)
	}

	// Byte-identical event log and trajectory hash across independent runs.
	if !reflect.DeepEqual(r1.Events, r2.Events) {
		t.Fatalf("events differ across replay:\n run1=%+v\n run2=%+v", r1.Events, r2.Events)
	}
	if r1.TrajectoryHash != r2.TrajectoryHash {
		t.Fatalf("trajectory hash differs across replay: %q vs %q", r1.TrajectoryHash, r2.TrajectoryHash)
	}
	if r1.TrajectoryHash == "" {
		t.Fatal("trajectory hash is empty")
	}
	if !hasEventType(r1.Events, "ACHIEVED") {
		t.Fatalf("expected an ACHIEVED event in the log, got %+v", r1.Events)
	}

	// Settlement is byte-identical across independent runs (deterministic payload).
	if r1.Settlement == nil || r2.Settlement == nil {
		t.Fatalf("expected non-nil settlement on both runs, got %v / %v", r1.Settlement, r2.Settlement)
	}
	if *r1.Settlement != *r2.Settlement {
		t.Fatalf("settlement differs across replay:\n run1=%+v\n run2=%+v", *r1.Settlement, *r2.Settlement)
	}

	// RECOMPUTE, not re-read: each independent Gate must have CALLED OnAchieved
	// exactly once. A re-read of a shared stored event would not invoke the adapter.
	if ca1.calls != 1 {
		t.Fatalf("run 1 OnAchieved call count = %d, want 1 (recompute path)", ca1.calls)
	}
	if ca2.calls != 1 {
		t.Fatalf("run 2 OnAchieved call count = %d, want 1 (recompute path)", ca2.calls)
	}
	if got := len(ref1.Settlement()); got != 1 {
		t.Fatalf("ref adapter 1 recorded %d settlement events, want 1", got)
	}
	if got := len(ref2.Settlement()); got != 1 {
		t.Fatalf("ref adapter 2 recorded %d settlement events, want 1", got)
	}
}

// --- (b) fail-closed: unevaluable + absent key ----------------------------

func TestFailClosedUnevaluablePerCriterion(t *testing.T) {
	criteria := []intent.Criterion{
		crit("balance", intent.Stable),
		crit("sanctions", intent.Volatile),
	}

	// For EACH criterion in turn: make it Unevaluable at declaration and assert the
	// gate fails closed (FAILED, never ACHIEVED), names that criterion, emits an
	// UNEVALUABLE event, and never settles. Completion of the synchronous call is
	// itself the "never hang" assertion (the go test -timeout watchdog backs it).
	for _, target := range criteria {
		target := target
		t.Run(target.Name, func(t *testing.T) {
			s := newScorer()
			s.Results[scoring.ScoreKey{Criterion: target.Name, Phase: intent.Declaration}] = scoring.Unevaluable

			ref := adapter.NewReferenceAdapter()
			g := New(s, ref, idempotency.NewStore())

			r := g.Authorize(context.Background(), mkIntent("seed-"+target.Name, "key-u", "spec-u", criteria...))

			if r.Terminal != lifecycle.Failed {
				t.Fatalf("terminal = %q, want FAILED for unevaluable %q", r.Terminal, target.Name)
			}
			if r.Terminal == lifecycle.Achieved {
				t.Fatal("unevaluable criterion must never reach ACHIEVED")
			}
			if r.Reason != "unevaluable:"+target.Name {
				t.Fatalf("reason = %q, want %q", r.Reason, "unevaluable:"+target.Name)
			}
			if r.Settlement != nil {
				t.Fatalf("expected nil settlement on FAILED, got %+v", *r.Settlement)
			}
			if !hasEventType(r.Events, "UNEVALUABLE") {
				t.Fatalf("expected an UNEVALUABLE event in the log, got %+v", r.Events)
			}
			if len(ref.Settlement()) != 0 {
				t.Fatalf("adapter must record nothing on FAILED, got %+v", ref.Settlement())
			}
		})
	}
}

func TestFailClosedAbsentKey(t *testing.T) {
	ref := adapter.NewReferenceAdapter()
	g := New(newScorer(), ref, idempotency.NewStore())

	// Empty idempotency key ⟹ refuse at declaration, before any scoring.
	r := g.Authorize(context.Background(), mkIntent("seed-nokey", "", "spec-nokey",
		crit("balance", intent.Stable),
	))

	if r.Terminal != lifecycle.Failed {
		t.Fatalf("terminal = %q, want FAILED for absent key", r.Terminal)
	}
	if r.Reason != "unevaluable:absent-key" {
		t.Fatalf("reason = %q, want %q", r.Reason, "unevaluable:absent-key")
	}
	if r.Settlement != nil {
		t.Fatalf("expected nil settlement, got %+v", *r.Settlement)
	}
	if !hasEventType(r.Events, "UNEVALUABLE") {
		t.Fatalf("expected an UNEVALUABLE event for absent key, got %+v", r.Events)
	}
	if len(ref.Settlement()) != 0 {
		t.Fatalf("adapter must record nothing for absent key, got %+v", ref.Settlement())
	}
}

// --- (c) verification failure ---------------------------------------------

func TestVerificationFailure(t *testing.T) {
	s := newScorer()
	s.Results[scoring.ScoreKey{Criterion: "balance", Phase: intent.Declaration}] = scoring.Fail

	ref := adapter.NewReferenceAdapter()
	g := New(s, ref, idempotency.NewStore())

	r := g.Authorize(context.Background(), mkIntent("seed-fail", "key-fail", "spec-fail",
		crit("balance", intent.Stable),
		crit("fx-rate", intent.Volatile),
	))

	if r.Terminal != lifecycle.Failed {
		t.Fatalf("terminal = %q, want FAILED for a failing criterion", r.Terminal)
	}
	if !strings.Contains(r.Reason, "balance") {
		t.Fatalf("reason = %q, want it to name the failed criterion %q", r.Reason, "balance")
	}
	if r.Settlement != nil {
		t.Fatalf("expected nil settlement on FAILED, got %+v", *r.Settlement)
	}
	if len(ref.Settlement()) != 0 {
		t.Fatalf("adapter must record nothing on FAILED, got %+v", ref.Settlement())
	}
}

// --- (d) volatile re-verify -----------------------------------------------

func TestVolatileReVerify(t *testing.T) {
	s := newScorer()
	// Stable passes; volatile passes at declaration but FAILS at the dispatch edge.
	s.Results[scoring.ScoreKey{Criterion: "fx-rate", Phase: intent.Dispatch}] = scoring.Fail

	ref := adapter.NewReferenceAdapter()
	g := New(s, ref, idempotency.NewStore())

	r := g.Authorize(context.Background(), mkIntent("seed-vol", "key-vol", "spec-vol",
		crit("balance", intent.Stable),
		crit("fx-rate", intent.Volatile),
	))

	if r.Terminal != lifecycle.FailedAtDispatch {
		t.Fatalf("terminal = %q, want FAILED_AT_DISPATCH for volatile recheck failure (reason %q)", r.Terminal, r.Reason)
	}
	if r.Settlement != nil {
		t.Fatalf("expected nil settlement on FAILED_AT_DISPATCH, got %+v", *r.Settlement)
	}
	if len(ref.Settlement()) != 0 {
		t.Fatalf("adapter must record nothing on FAILED_AT_DISPATCH, got %+v", ref.Settlement())
	}

	// The STABLE criterion is scored exactly once (declaration only, no re-verify);
	// the VOLATILE criterion is scored exactly twice (declaration + dispatch).
	if got := countByCriterion(s.Calls, "balance"); got != 1 {
		t.Fatalf("stable criterion scored %d times, want exactly 1; calls=%+v", got, s.Calls)
	}
	if got := countByCriterion(s.Calls, "fx-rate"); got != 2 {
		t.Fatalf("volatile criterion scored %d times, want exactly 2; calls=%+v", got, s.Calls)
	}
}

// --- (e) idempotency collision --------------------------------------------

func TestIdempotencyCollision(t *testing.T) {
	key := "key-shared"
	// Same idempotency key, different IntentSpecHash (a near-duplicate), distinct
	// seeds (distinct IDs). Both share ONE store and ONE reference adapter.
	i1 := mkIntent("seed-collide-1", key, "spec-hash-A", crit("balance", intent.Stable), crit("fx-rate", intent.Volatile))
	i2 := mkIntent("seed-collide-2", key, "spec-hash-B", crit("balance", intent.Stable), crit("fx-rate", intent.Volatile))

	store := idempotency.NewStore()
	ref := adapter.NewReferenceAdapter()
	g := New(newScorer(), ref, store)

	r1 := g.Authorize(context.Background(), i1)
	r2 := g.Authorize(context.Background(), i2)

	if r1.Terminal != lifecycle.Achieved {
		t.Fatalf("first intent terminal = %q, want ACHIEVED (reason %q)", r1.Terminal, r1.Reason)
	}
	if r1.Settlement == nil {
		t.Fatal("first intent must settle (non-nil Settlement)")
	}

	if r2.Terminal != lifecycle.FailedAtDispatch {
		t.Fatalf("second intent terminal = %q, want FAILED_AT_DISPATCH", r2.Terminal)
	}
	if r2.Reason != "idempotency-collision" {
		t.Fatalf("second intent reason = %q, want %q", r2.Reason, "idempotency-collision")
	}
	if r2.Settlement != nil {
		t.Fatalf("collided intent must not settle, got %+v", *r2.Settlement)
	}

	// At-most-once on the settlement log: exactly ONE event for the shared key.
	settled := ref.Settlement()
	if len(settled) != 1 {
		t.Fatalf("settlement log has %d events for shared key, want exactly 1: %+v", len(settled), settled)
	}
	if settled[0].Key != intent.IdempotencyKey(key) {
		t.Fatalf("settled event key = %q, want %q", settled[0].Key, key)
	}
}

// --- (f) terminal separation ----------------------------------------------

func TestTerminalSeparation(t *testing.T) {
	ctx := context.Background()

	// Collect one result from every non-ACHIEVED terminal path and assert each has a
	// nil Settlement. Also assert the one ACHIEVED path has a non-nil Settlement,
	// reached only after the volatile re-check passed.

	// FAILED: absent key.
	absentKey := New(newScorer(), adapter.NewReferenceAdapter(), idempotency.NewStore()).
		Authorize(ctx, mkIntent("seed-sep-absent", "", "spec", crit("balance", intent.Stable)))

	// FAILED: unevaluable at declaration.
	sUneval := newScorer()
	sUneval.Results[scoring.ScoreKey{Criterion: "balance", Phase: intent.Declaration}] = scoring.Unevaluable
	uneval := New(sUneval, adapter.NewReferenceAdapter(), idempotency.NewStore()).
		Authorize(ctx, mkIntent("seed-sep-uneval", "key-sep-u", "spec", crit("balance", intent.Stable)))

	// FAILED: verification failure.
	sFail := newScorer()
	sFail.Results[scoring.ScoreKey{Criterion: "balance", Phase: intent.Declaration}] = scoring.Fail
	verifFail := New(sFail, adapter.NewReferenceAdapter(), idempotency.NewStore()).
		Authorize(ctx, mkIntent("seed-sep-fail", "key-sep-f", "spec", crit("balance", intent.Stable)))

	// FAILED_AT_DISPATCH: volatile recheck failure.
	sVol := newScorer()
	sVol.Results[scoring.ScoreKey{Criterion: "fx-rate", Phase: intent.Dispatch}] = scoring.Fail
	volFail := New(sVol, adapter.NewReferenceAdapter(), idempotency.NewStore()).
		Authorize(ctx, mkIntent("seed-sep-vol", "key-sep-v", "spec",
			crit("balance", intent.Stable), crit("fx-rate", intent.Volatile)))

	// FAILED_AT_DISPATCH: idempotency collision (second intent on a shared store).
	sharedStore := idempotency.NewStore()
	sharedRef := adapter.NewReferenceAdapter()
	gShared := New(newScorer(), sharedRef, sharedStore)
	achieved := gShared.Authorize(ctx, mkIntent("seed-sep-ok-1", "key-sep-c", "spec-A", crit("fx-rate", intent.Volatile)))
	collision := gShared.Authorize(ctx, mkIntent("seed-sep-ok-2", "key-sep-c", "spec-B", crit("fx-rate", intent.Volatile)))

	failures := []struct {
		name string
		r    Result
		want lifecycle.State
	}{
		{"absent-key", absentKey, lifecycle.Failed},
		{"unevaluable", uneval, lifecycle.Failed},
		{"verification-fail", verifFail, lifecycle.Failed},
		{"volatile-recheck", volFail, lifecycle.FailedAtDispatch},
		{"idempotency-collision", collision, lifecycle.FailedAtDispatch},
	}
	for _, f := range failures {
		if f.r.Terminal != f.want {
			t.Errorf("%s: terminal = %q, want %q", f.name, f.r.Terminal, f.want)
		}
		if f.r.Settlement != nil {
			t.Errorf("%s: Settlement = %+v, want nil for every FAILED/FAILED_AT_DISPATCH", f.name, *f.r.Settlement)
		}
	}

	// The lone ACHIEVED path (volatile criterion that passed re-check) must settle.
	if achieved.Terminal != lifecycle.Achieved {
		t.Fatalf("expected ACHIEVED on the passing path, got %q (reason %q)", achieved.Terminal, achieved.Reason)
	}
	if achieved.Settlement == nil {
		t.Fatal("ACHIEVED must carry a non-nil Settlement")
	}
}
