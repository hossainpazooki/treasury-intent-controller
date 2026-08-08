package gate

// §12 acceptance tests for the authorization gate, rewritten per CONTRACT.md
// §4 + §5.3.
//
// Every slice-1 invariant (a)-(f) appears here with its exact CONTRACT.md §5.3 successor
// assertion. The gate is emit-and-observe: it never settles in-process, so every
// slice-1 "Settlement" assertion is replaced by its successor — the durable
// feed's ACHIEVED records plus a test-only feedConsumer that drains the feed and
// drives adapter.ReferenceAdapter.OnAchieved (the recompute path; the adapter is
// TEST-ONLY, CONTRACT.md §7.2).
//
// All test IO lives under t.TempDir(); no test touches ./data or the network.
// These tests are written against the contract's Gate API only — never against
// gate.go's internals. Do not weaken any assertion.

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/hossainpazooki/intent-plane/core/internal/adapter"
	"github.com/hossainpazooki/intent-plane/core/internal/audit"
	"github.com/hossainpazooki/intent-plane/core/internal/durable"
	"github.com/hossainpazooki/intent-plane/core/internal/idempotency"
	"github.com/hossainpazooki/intent-plane/core/internal/intent"
	"github.com/hossainpazooki/intent-plane/core/internal/lifecycle"
	"github.com/hossainpazooki/intent-plane/core/internal/scoring"
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
			ActionClass:      "sample-action",
			Criteria:         crits,
			IdempotencyScope: "per-actor",
			Posture:          intent.PostureEnforce,
		},
		IdempotencyKey:   intent.IdempotencyKey(key),
		RuleArtifactHash: "rule-" + specHash,
		IntentSpecHash:   specHash,
		// The package tests exercise gate semantics POST-resolution: attested,
		// enforce posture. The unattested/revoked/shadow paths are covered in
		// plane_acceptance_test.go.
		Resolution: intent.Resolution{Attested: true, Source: "store", KeyID: "test-key"},
	}
}

func newScorer() *scoring.FakeScorer {
	return &scoring.FakeScorer{Results: map[scoring.ScoreKey]scoring.Score{}}
}

// openFeed opens a durable feed in a fresh t.TempDir() and closes it at cleanup.
func openFeed(t *testing.T) *durable.Store {
	t.Helper()
	feed, err := durable.Open(t.TempDir())
	if err != nil {
		t.Fatalf("durable.Open: %v", err)
	}
	t.Cleanup(func() { _ = feed.Close() })
	return feed
}

// mustAuthorize runs Authorize and fails the test on a non-nil error (the
// happy-path tests exercise no durable-IO failure).
func mustAuthorize(t *testing.T, g *Gate, i intent.Intent) Result {
	t.Helper()
	r, err := g.Authorize(context.Background(), i)
	if err != nil {
		t.Fatalf("Authorize(%s): unexpected error: %v", i.EpisodeSeed, err)
	}
	return r
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

// achievedFor returns the feed's ACHIEVED records for one intent.
func achievedFor(feed *durable.Store, intentID string) []durable.Record {
	out := []durable.Record{}
	for _, r := range feed.Since(0, "ACHIEVED") {
		if r.IntentID == intentID {
			out = append(out, r)
		}
	}
	return out
}

// feedConsumer drains ACHIEVED records from a durable feed past a cursor and
// calls OnAchieved on a ReferenceAdapter (recompute path), enforcing
// at-most-once via the adapter's key-idempotency PLUS its own cursor. Poll(feed)
// is safe to call repeatedly and after a feed reopen; it never double-settles a
// key. It looks the original intent up by record.IntentID from the map the test
// populated at submit time (it cannot invert ID() from the record). calls counts
// OnAchieved invocations, for recompute-path assertions.
type feedConsumer struct {
	ref     *adapter.ReferenceAdapter
	cursor  int
	intents map[string]intent.Intent
	calls   int
}

func newConsumer() *feedConsumer {
	return &feedConsumer{
		ref:     adapter.NewReferenceAdapter(),
		intents: map[string]intent.Intent{},
	}
}

// track registers an intent at submit time so Poll can resolve its ID.
func (c *feedConsumer) track(is ...intent.Intent) {
	for _, i := range is {
		c.intents[i.ID()] = i
	}
}

// Poll drains ACHIEVED records past the cursor and settles them via OnAchieved.
func (c *feedConsumer) Poll(t *testing.T, feed *durable.Store) {
	t.Helper()
	for _, rec := range feed.Since(c.cursor, "ACHIEVED") {
		if rec.GlobalSeq > c.cursor {
			c.cursor = rec.GlobalSeq
		}
		i, ok := c.intents[rec.IntentID]
		if !ok {
			t.Fatalf("feed consumer: ACHIEVED record for unknown intent id %q", rec.IntentID)
		}
		c.calls++
		if _, err := c.ref.OnAchieved(i); err != nil {
			t.Fatalf("feed consumer: OnAchieved(%s): %v", rec.IntentID, err)
		}
	}
}

// settlementsForKey counts the consumer's recorded settlements for one key.
func settlementsForKey(ref *adapter.ReferenceAdapter, key intent.IdempotencyKey) int {
	n := 0
	for _, ev := range ref.Settlement() {
		if ev.Key == key {
			n++
		}
	}
	return n
}

// assertNoSettlement is the §5.3 successor to slice-1's "Settlement == nil":
// the feed holds NO ACHIEVED record for the intent, AND a consumer draining the
// feed settles nothing for it.
func assertNoSettlement(t *testing.T, feed *durable.Store, i intent.Intent) {
	t.Helper()
	if recs := achievedFor(feed, i.ID()); len(recs) != 0 {
		t.Fatalf("feed has %d ACHIEVED record(s) for intent %s, want 0: %+v", len(recs), i.ID(), recs)
	}
	c := newConsumer()
	c.track(i)
	c.Poll(t, feed)
	if got := settlementsForKey(c.ref, i.IdempotencyKey); got != 0 {
		t.Fatalf("consumer settled %d event(s) for key %q, want 0", got, i.IdempotencyKey)
	}
}

// durabilityLanded reports whether the durable substrate persists across reopen.
// It is a guard from the build era, when the stores were still in-memory
// scaffolds: the CONTRACT.md §5.3(e) across-restart clause binds only against a
// substrate that actually persists. The probe runs in its own t.TempDir() and
// never touches ./data.
func durabilityLanded(t *testing.T) bool {
	t.Helper()
	dir := t.TempDir()

	f1, err := durable.Open(dir)
	if err != nil {
		t.Fatalf("durability probe: durable.Open: %v", err)
	}
	if _, err := f1.Append(durable.Record{IntentID: "probe", Type: "DECLARED"}); err != nil {
		t.Fatalf("durability probe: Append: %v", err)
	}
	if err := f1.Close(); err != nil {
		t.Fatalf("durability probe: Close: %v", err)
	}
	f2, err := durable.Open(dir)
	if err != nil {
		t.Fatalf("durability probe: reopen: %v", err)
	}
	feedDurable := len(f2.Since(0, "")) == 1
	_ = f2.Close()

	s1, err := idempotency.OpenStore(dir)
	if err != nil {
		t.Fatalf("durability probe: idempotency.OpenStore: %v", err)
	}
	if !s1.Reserve("probe-id", "probe-key") {
		t.Fatal("durability probe: Reserve on a fresh store must succeed")
	}
	s2, err := idempotency.OpenStore(dir)
	if err != nil {
		t.Fatalf("durability probe: idempotency.OpenStore reopen: %v", err)
	}
	idemDurable := !s2.Reserve("probe-id-2", "probe-key")
	_ = s1.Close()
	_ = s2.Close()

	return feedDurable && idemDurable
}

// --- (a) determinism / replay ---------------------------------------------

func TestDeterminismReplay(t *testing.T) {
	i := mkIntent("seed-alpha", "key-pay-1", "spec-hash-1",
		crit("alpha", intent.Stable),
		crit("beta", intent.Volatile),
	)

	// Two fully independent Gates: each its OWN durable feed (own t.TempDir())
	// and own idempotency store. Replay = run the same intent through the second
	// pipeline from scratch.
	feed1 := openFeed(t)
	feed2 := openFeed(t)

	// Offset feed2's GlobalSeq with an unrelated record so the two runs' global
	// sequence numbers DIFFER: the per-intent Events/TrajectoryHash equality
	// below then proves GlobalSeq is excluded from both by construction.
	if _, err := feed2.Append(durable.Record{IntentID: "unrelated-intent", IntentSeq: 0, Type: "DECLARED"}); err != nil {
		t.Fatalf("pre-seed append: %v", err)
	}

	g1 := New(newScorer(), feed1, idempotency.NewStore())
	g2 := New(newScorer(), feed2, idempotency.NewStore())

	r1 := mustAuthorize(t, g1, i)
	r2 := mustAuthorize(t, g2, i)

	if r1.Terminal != lifecycle.Achieved {
		t.Fatalf("run 1 terminal = %q, want ACHIEVED (reason %q)", r1.Terminal, r1.Reason)
	}
	if r2.Terminal != lifecycle.Achieved {
		t.Fatalf("run 2 terminal = %q, want ACHIEVED (reason %q)", r2.Terminal, r2.Reason)
	}

	// Byte-identical per-intent event log and trajectory hash across independent
	// runs (GlobalSeq explicitly excluded from the compare: audit.Event has none).
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

	// GlobalSeq is non-deterministic across replay BY DESIGN: with feed2 offset,
	// the two AchievedSeq values must differ while the hash does not.
	if r1.AchievedSeq < 1 || r2.AchievedSeq < 1 {
		t.Fatalf("AchievedSeq must be >= 1 on ACHIEVED, got %d / %d", r1.AchievedSeq, r2.AchievedSeq)
	}
	if r1.AchievedSeq == r2.AchievedSeq {
		t.Fatalf("AchievedSeq should differ across offset feeds (GlobalSeq is not replay-deterministic), both = %d", r1.AchievedSeq)
	}

	// Every in-memory append is mirrored to the durable feed 1:1, preserving the
	// per-intent Seq as IntentSeq (CONTRACT.md §4.2).
	recs := feed1.ByIntent(i.ID())
	if len(recs) != len(r1.Events) {
		t.Fatalf("feed mirrors %d records for the intent, want %d (one per event)", len(recs), len(r1.Events))
	}
	for k, e := range r1.Events {
		rec := recs[k]
		if rec.IntentSeq != e.Seq || rec.Type != e.Type || rec.Detail != e.Detail {
			t.Fatalf("mirror mismatch at index %d: event=%+v record=%+v", k, e, rec)
		}
	}

	// Exactly one ACHIEVED record per feed; it carries the four trace fields and
	// its trajectory_hash equals Result.TrajectoryHash (§5.3(a)).
	for run, pair := range []struct {
		feed *durable.Store
		r    Result
	}{{feed1, r1}, {feed2, r2}} {
		achs := achievedFor(pair.feed, i.ID())
		if len(achs) != 1 {
			t.Fatalf("run %d: feed has %d ACHIEVED records, want 1", run+1, len(achs))
		}
		ach := achs[0]
		if ach.TrajectoryHash != pair.r.TrajectoryHash {
			t.Fatalf("run %d: ACHIEVED record trajectory_hash = %q, want Result.TrajectoryHash %q", run+1, ach.TrajectoryHash, pair.r.TrajectoryHash)
		}
		if ach.GlobalSeq != pair.r.AchievedSeq {
			t.Fatalf("run %d: ACHIEVED record GlobalSeq = %d, want AchievedSeq %d", run+1, ach.GlobalSeq, pair.r.AchievedSeq)
		}
		if ach.IdempotencyKey != string(i.IdempotencyKey) {
			t.Fatalf("run %d: ACHIEVED record idempotency_key = %q, want %q", run+1, ach.IdempotencyKey, i.IdempotencyKey)
		}
		if ach.RuleArtifactHash != i.RuleArtifactHash {
			t.Fatalf("run %d: ACHIEVED record rule_artifact_hash = %q, want %q", run+1, ach.RuleArtifactHash, i.RuleArtifactHash)
		}
		if ach.IntentSpecHash != i.IntentSpecHash {
			t.Fatalf("run %d: ACHIEVED record intent_spec_hash = %q, want %q", run+1, ach.IntentSpecHash, i.IntentSpecHash)
		}
	}

	// Settlement-bytes successor (§5.3(a)): a feedConsumer draining each
	// independent feed calls OnAchieved exactly once per key (recompute path —
	// each consumer CALLS the adapter; nothing is re-read from a stored
	// settlement) and the resulting SettlementEvents are byte-identical.
	c1 := newConsumer()
	c1.track(i)
	c1.Poll(t, feed1)
	c2 := newConsumer()
	c2.track(i)
	c2.Poll(t, feed2)

	if c1.calls != 1 {
		t.Fatalf("consumer 1 OnAchieved call count = %d, want 1 (recompute path)", c1.calls)
	}
	if c2.calls != 1 {
		t.Fatalf("consumer 2 OnAchieved call count = %d, want 1 (recompute path)", c2.calls)
	}
	s1 := c1.ref.Settlement()
	s2 := c2.ref.Settlement()
	if len(s1) != 1 || len(s2) != 1 {
		t.Fatalf("settlement counts = %d / %d, want 1 / 1", len(s1), len(s2))
	}
	if s1[0] != s2[0] {
		t.Fatalf("settlement differs across replay:\n run1=%+v\n run2=%+v", s1[0], s2[0])
	}

	// Repeat-poll safety: a second Poll past the cursor settles nothing new.
	c1.Poll(t, feed1)
	if c1.calls != 1 {
		t.Fatalf("repeat poll re-invoked OnAchieved (calls = %d), want cursor to skip drained records", c1.calls)
	}
	if got := len(c1.ref.Settlement()); got != 1 {
		t.Fatalf("repeat poll produced %d settlements, want 1", got)
	}
}

// --- (b) fail-closed: unevaluable + absent key ----------------------------

func TestFailClosedUnevaluablePerCriterion(t *testing.T) {
	criteria := []intent.Criterion{
		crit("alpha", intent.Stable),
		crit("gamma", intent.Volatile),
	}

	// For EACH criterion in turn: make it Unevaluable at declaration and assert
	// the gate fails closed (FAILED, never ACHIEVED), names that criterion, emits
	// an UNEVALUABLE event, and never settles. Completion of the synchronous call
	// is itself the "never hang" assertion (the go test -timeout watchdog backs
	// it).
	for _, target := range criteria {
		target := target
		t.Run(target.Name, func(t *testing.T) {
			s := newScorer()
			s.Results[scoring.ScoreKey{Criterion: target.Name, Phase: intent.Declaration}] = scoring.Unevaluable

			feed := openFeed(t)
			g := New(s, feed, idempotency.NewStore())
			i := mkIntent("seed-"+target.Name, "key-u", "spec-u", criteria...)

			r := mustAuthorize(t, g, i)

			if r.Terminal != lifecycle.Failed {
				t.Fatalf("terminal = %q, want FAILED for unevaluable %q", r.Terminal, target.Name)
			}
			if r.Terminal == lifecycle.Achieved {
				t.Fatal("unevaluable criterion must never reach ACHIEVED")
			}
			if r.Reason != "unevaluable:"+target.Name {
				t.Fatalf("reason = %q, want %q", r.Reason, "unevaluable:"+target.Name)
			}
			if r.AchievedSeq != 0 {
				t.Fatalf("AchievedSeq = %d, want 0 on FAILED", r.AchievedSeq)
			}
			if !hasEventType(r.Events, "UNEVALUABLE") {
				t.Fatalf("expected an UNEVALUABLE event in the log, got %+v", r.Events)
			}
			// §5.3(b) no-settlement successor: no ACHIEVED record in the feed
			// for this intent AND the consumer's ledger stays empty.
			assertNoSettlement(t, feed, i)
		})
	}
}

func TestFailClosedAbsentKey(t *testing.T) {
	feed := openFeed(t)
	g := New(newScorer(), feed, idempotency.NewStore())

	// Empty idempotency key ⟹ refuse at declaration, before any scoring.
	i := mkIntent("seed-nokey", "", "spec-nokey", crit("alpha", intent.Stable))
	r := mustAuthorize(t, g, i)

	if r.Terminal != lifecycle.Failed {
		t.Fatalf("terminal = %q, want FAILED for absent key", r.Terminal)
	}
	if r.Reason != "unevaluable:absent-key" {
		t.Fatalf("reason = %q, want %q", r.Reason, "unevaluable:absent-key")
	}
	if r.AchievedSeq != 0 {
		t.Fatalf("AchievedSeq = %d, want 0 on FAILED", r.AchievedSeq)
	}
	if !hasEventType(r.Events, "UNEVALUABLE") {
		t.Fatalf("expected an UNEVALUABLE event for absent key, got %+v", r.Events)
	}
	assertNoSettlement(t, feed, i)
}

// --- (c) verification failure ---------------------------------------------

func TestVerificationFailure(t *testing.T) {
	s := newScorer()
	s.Results[scoring.ScoreKey{Criterion: "alpha", Phase: intent.Declaration}] = scoring.Fail

	feed := openFeed(t)
	g := New(s, feed, idempotency.NewStore())

	i := mkIntent("seed-fail", "key-fail", "spec-fail",
		crit("alpha", intent.Stable),
		crit("beta", intent.Volatile),
	)
	r := mustAuthorize(t, g, i)

	if r.Terminal != lifecycle.Failed {
		t.Fatalf("terminal = %q, want FAILED for a failing criterion", r.Terminal)
	}
	if !strings.Contains(r.Reason, "alpha") {
		t.Fatalf("reason = %q, want it to name the failed criterion %q", r.Reason, "alpha")
	}
	if r.AchievedSeq != 0 {
		t.Fatalf("AchievedSeq = %d, want 0 on FAILED", r.AchievedSeq)
	}
	// §5.3(c) no-settlement successor.
	assertNoSettlement(t, feed, i)
}

// --- (d) volatile re-verify -----------------------------------------------

func TestVolatileReVerify(t *testing.T) {
	cases := []struct {
		name     string
		dispatch scoring.Score
	}{
		{"fail-at-dispatch", scoring.Fail},
		{"unevaluable-at-dispatch", scoring.Unevaluable},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			s := newScorer()
			// Stable passes; volatile passes at declaration but is not Pass at
			// the dispatch edge.
			s.Results[scoring.ScoreKey{Criterion: "beta", Phase: intent.Dispatch}] = tc.dispatch

			feed := openFeed(t)
			g := New(s, feed, idempotency.NewStore())

			i := mkIntent("seed-vol-"+tc.name, "key-vol", "spec-vol",
				crit("alpha", intent.Stable),
				crit("beta", intent.Volatile),
			)
			r := mustAuthorize(t, g, i)

			if r.Terminal != lifecycle.FailedAtDispatch {
				t.Fatalf("terminal = %q, want FAILED_AT_DISPATCH for volatile recheck (reason %q)", r.Terminal, r.Reason)
			}
			if r.Reason != "volatile-recheck:beta" {
				t.Fatalf("reason = %q, want %q", r.Reason, "volatile-recheck:beta")
			}
			if r.AchievedSeq != 0 {
				t.Fatalf("AchievedSeq = %d, want 0 on FAILED_AT_DISPATCH", r.AchievedSeq)
			}
			if tc.dispatch == scoring.Unevaluable && !hasEventType(r.Events, "UNEVALUABLE") {
				t.Fatalf("dispatch-edge Unevaluable must be logged distinctly, got %+v", r.Events)
			}

			// The STABLE criterion is scored exactly once (declaration only, no
			// re-verify); the VOLATILE criterion exactly twice (declaration +
			// dispatch). §5.3(d): unchanged from slice 1.
			if got := countByCriterion(s.Calls, "alpha"); got != 1 {
				t.Fatalf("stable criterion scored %d times, want exactly 1; calls=%+v", got, s.Calls)
			}
			if got := countByCriterion(s.Calls, "beta"); got != 2 {
				t.Fatalf("volatile criterion scored %d times, want exactly 2; calls=%+v", got, s.Calls)
			}

			// §5.3(d) no-settlement successor.
			assertNoSettlement(t, feed, i)
		})
	}
}

// --- (e) idempotency collision (+ across-restart clause) -------------------

func TestIdempotencyCollision(t *testing.T) {
	ctx := context.Background()
	key := "key-shared"
	// Same idempotency key, different IntentSpecHash (a near-duplicate), distinct
	// seeds (distinct IDs). All share ONE feed and ONE idempotency store.
	i1 := mkIntent("seed-collide-1", key, "spec-hash-A", crit("alpha", intent.Stable), crit("beta", intent.Volatile))
	i2 := mkIntent("seed-collide-2", key, "spec-hash-B", crit("alpha", intent.Stable), crit("beta", intent.Volatile))
	i3 := mkIntent("seed-collide-3", key, "spec-hash-C", crit("alpha", intent.Stable), crit("beta", intent.Volatile))

	feedDir := t.TempDir()
	storeDir := t.TempDir()

	feed, err := durable.Open(feedDir)
	if err != nil {
		t.Fatalf("durable.Open: %v", err)
	}
	t.Cleanup(func() { _ = feed.Close() })
	istore, err := idempotency.OpenStore(storeDir)
	if err != nil {
		t.Fatalf("idempotency.OpenStore: %v", err)
	}
	t.Cleanup(func() { _ = istore.Close() })

	g := New(newScorer(), feed, istore)
	r1, err := g.Authorize(ctx, i1)
	if err != nil {
		t.Fatalf("Authorize(i1): %v", err)
	}
	r2, err := g.Authorize(ctx, i2)
	if err != nil {
		t.Fatalf("Authorize(i2): %v", err)
	}

	if r1.Terminal != lifecycle.Achieved {
		t.Fatalf("first intent terminal = %q, want ACHIEVED (reason %q)", r1.Terminal, r1.Reason)
	}
	if r1.AchievedSeq < 1 {
		t.Fatalf("first intent AchievedSeq = %d, want >= 1", r1.AchievedSeq)
	}
	if r2.Terminal != lifecycle.FailedAtDispatch {
		t.Fatalf("second intent terminal = %q, want FAILED_AT_DISPATCH", r2.Terminal)
	}
	if r2.Reason != "idempotency-collision" {
		t.Fatalf("second intent reason = %q, want %q", r2.Reason, "idempotency-collision")
	}
	if r2.AchievedSeq != 0 {
		t.Fatalf("collided intent AchievedSeq = %d, want 0", r2.AchievedSeq)
	}

	// §5.3(e): the feed has EXACTLY ONE ACHIEVED record for the key.
	countForKey := func(f *durable.Store) int {
		n := 0
		for _, rec := range f.Since(0, "ACHIEVED") {
			if rec.IdempotencyKey == key {
				n++
			}
		}
		return n
	}
	if got := countForKey(feed); got != 1 {
		t.Fatalf("feed has %d ACHIEVED records for key %q, want exactly 1", got, key)
	}
	if recs := achievedFor(feed, i2.ID()); len(recs) != 0 {
		t.Fatalf("collided intent has %d ACHIEVED records, want 0", len(recs))
	}

	// The consumer records exactly ONE settlement for the key (at-most-once);
	// double-poll must not change that.
	c := newConsumer()
	c.track(i1, i2, i3)
	c.Poll(t, feed)
	c.Poll(t, feed)
	if got := len(c.ref.Settlement()); got != 1 {
		t.Fatalf("consumer settlement log has %d events for shared key, want exactly 1: %+v", got, c.ref.Settlement())
	}
	if c.ref.Settlement()[0].Key != intent.IdempotencyKey(key) {
		t.Fatalf("settled event key = %q, want %q", c.ref.Settlement()[0].Key, key)
	}

	// §5.3(e) NEW restart clause: at-most-once holds ACROSS process restart.
	// Reopen the feed (Close + durable.Open over the same dir) and the
	// idempotency store (OpenStore over the same dir); a third intent with the
	// same key must STILL collide, the reopened feed must still hold exactly one
	// ACHIEVED for the key, and a re-poll from cursor 0 must not settle anew.
	t.Run("across-restart", func(t *testing.T) {
		if !durabilityLanded(t) {
			t.Skip("durable stores do not persist across reopen here: the CONTRACT.md §5.3(e) across-restart clause binds only against a persisting substrate")
		}

		if err := feed.Close(); err != nil {
			t.Fatalf("feed.Close: %v", err)
		}
		if err := istore.Close(); err != nil {
			t.Fatalf("istore.Close: %v", err)
		}
		feed2, err := durable.Open(feedDir)
		if err != nil {
			t.Fatalf("durable.Open (reopen): %v", err)
		}
		t.Cleanup(func() { _ = feed2.Close() })
		istore2, err := idempotency.OpenStore(storeDir)
		if err != nil {
			t.Fatalf("idempotency.OpenStore (reopen): %v", err)
		}
		t.Cleanup(func() { _ = istore2.Close() })

		g2 := New(newScorer(), feed2, istore2)
		r3, err := g2.Authorize(ctx, i3)
		if err != nil {
			t.Fatalf("Authorize(i3): %v", err)
		}
		if r3.Terminal != lifecycle.FailedAtDispatch {
			t.Fatalf("post-restart intent terminal = %q, want FAILED_AT_DISPATCH (reservation must survive restart)", r3.Terminal)
		}
		if r3.Reason != "idempotency-collision" {
			t.Fatalf("post-restart intent reason = %q, want %q", r3.Reason, "idempotency-collision")
		}

		// The recovered feed still holds exactly one ACHIEVED for the key.
		if got := countForKey(feed2); got != 1 {
			t.Fatalf("reopened feed has %d ACHIEVED records for key %q, want exactly 1", got, key)
		}

		// Re-poll from cursor 0 over the reopened feed: the adapter's
		// key-idempotency absorbs the replayed record — OnAchieved is invoked
		// again (recompute) but records NO new settlement.
		callsBefore := c.calls
		c.cursor = 0
		c.Poll(t, feed2)
		if c.calls != callsBefore+1 {
			t.Fatalf("re-poll from cursor 0 invoked OnAchieved %d more time(s), want exactly 1 (recompute of the one ACHIEVED record)", c.calls-callsBefore)
		}
		if got := len(c.ref.Settlement()); got != 1 {
			t.Fatalf("after restart re-poll, settlement log has %d events, want still exactly 1", got)
		}
	})
}

// --- (f) terminal separation ----------------------------------------------

func TestTerminalSeparation(t *testing.T) {
	// One shared feed and one shared idempotency store across every path, so the
	// feed-level assertions run against a realistic multi-intent substrate.
	feed := openFeed(t)
	store := idempotency.NewStore()

	iAbsent := mkIntent("seed-sep-absent", "", "spec", crit("alpha", intent.Stable))
	iUneval := mkIntent("seed-sep-uneval", "key-sep-u", "spec", crit("alpha", intent.Stable))
	iFail := mkIntent("seed-sep-fail", "key-sep-f", "spec", crit("alpha", intent.Stable))
	iVol := mkIntent("seed-sep-vol", "key-sep-v", "spec", crit("alpha", intent.Stable), crit("beta", intent.Volatile))
	iOK := mkIntent("seed-sep-ok-1", "key-sep-c", "spec-A", crit("beta", intent.Volatile))
	iCollide := mkIntent("seed-sep-ok-2", "key-sep-c", "spec-B", crit("beta", intent.Volatile))

	// FAILED: absent key.
	absentKey := mustAuthorize(t, New(newScorer(), feed, store), iAbsent)

	// FAILED: unevaluable at declaration.
	sUneval := newScorer()
	sUneval.Results[scoring.ScoreKey{Criterion: "alpha", Phase: intent.Declaration}] = scoring.Unevaluable
	uneval := mustAuthorize(t, New(sUneval, feed, store), iUneval)

	// FAILED: verification failure.
	sFail := newScorer()
	sFail.Results[scoring.ScoreKey{Criterion: "alpha", Phase: intent.Declaration}] = scoring.Fail
	verifFail := mustAuthorize(t, New(sFail, feed, store), iFail)

	// FAILED_AT_DISPATCH: volatile recheck failure.
	sVol := newScorer()
	sVol.Results[scoring.ScoreKey{Criterion: "beta", Phase: intent.Dispatch}] = scoring.Fail
	volFail := mustAuthorize(t, New(sVol, feed, store), iVol)

	// ACHIEVED, then FAILED_AT_DISPATCH: idempotency collision on the shared store.
	gShared := New(newScorer(), feed, store)
	achieved := mustAuthorize(t, gShared, iOK)
	collision := mustAuthorize(t, gShared, iCollide)

	failures := []struct {
		name string
		i    intent.Intent
		r    Result
		want lifecycle.State
	}{
		{"absent-key", iAbsent, absentKey, lifecycle.Failed},
		{"unevaluable", iUneval, uneval, lifecycle.Failed},
		{"verification-fail", iFail, verifFail, lifecycle.Failed},
		{"volatile-recheck", iVol, volFail, lifecycle.FailedAtDispatch},
		{"idempotency-collision", iCollide, collision, lifecycle.FailedAtDispatch},
	}
	for _, f := range failures {
		if f.r.Terminal != f.want {
			t.Errorf("%s: terminal = %q, want %q", f.name, f.r.Terminal, f.want)
		}
		// §5.3(f): the feed is the successor to Settlement==nil — NO ACHIEVED
		// record for any FAILED/FAILED_AT_DISPATCH intent, and AchievedSeq stays 0.
		if f.r.AchievedSeq != 0 {
			t.Errorf("%s: AchievedSeq = %d, want 0 for every FAILED/FAILED_AT_DISPATCH", f.name, f.r.AchievedSeq)
		}
		if recs := achievedFor(feed, f.i.ID()); len(recs) != 0 {
			t.Errorf("%s: feed has %d ACHIEVED record(s), want 0 for every FAILED/FAILED_AT_DISPATCH", f.name, len(recs))
		}
	}

	// The lone ACHIEVED path (volatile criterion that passed re-check).
	if achieved.Terminal != lifecycle.Achieved {
		t.Fatalf("expected ACHIEVED on the passing path, got %q (reason %q)", achieved.Terminal, achieved.Reason)
	}
	if achieved.AchievedSeq < 1 {
		t.Fatalf("ACHIEVED must carry AchievedSeq >= 1, got %d", achieved.AchievedSeq)
	}

	// §5.3(f): exactly one ACHIEVED record for the passing intent, ordered AFTER
	// the volatile criterion's RECHECK record — asserted via ByIntent order,
	// IntentSeq, and GlobalSeq.
	recs := feed.ByIntent(iOK.ID())
	idxRecheck, idxAchieved := -1, -1
	var recheckRec, achievedRec durable.Record
	for k, rec := range recs {
		switch rec.Type {
		case "RECHECK":
			if idxRecheck == -1 {
				idxRecheck = k
				recheckRec = rec
			}
		case "ACHIEVED":
			if idxAchieved != -1 {
				t.Fatalf("more than one ACHIEVED record for intent %s in ByIntent: %+v", iOK.ID(), recs)
			}
			idxAchieved = k
			achievedRec = rec
		}
	}
	if idxRecheck == -1 {
		t.Fatalf("no RECHECK record for the volatile criterion in ByIntent: %+v", recs)
	}
	if idxAchieved == -1 {
		t.Fatalf("no ACHIEVED record in ByIntent: %+v", recs)
	}
	if idxAchieved <= idxRecheck {
		t.Fatalf("ACHIEVED (index %d) must be ordered after RECHECK (index %d) in ByIntent", idxAchieved, idxRecheck)
	}
	if achievedRec.IntentSeq <= recheckRec.IntentSeq {
		t.Fatalf("ACHIEVED IntentSeq %d must exceed RECHECK IntentSeq %d", achievedRec.IntentSeq, recheckRec.IntentSeq)
	}
	if achievedRec.GlobalSeq <= recheckRec.GlobalSeq {
		t.Fatalf("ACHIEVED GlobalSeq %d must exceed RECHECK GlobalSeq %d", achievedRec.GlobalSeq, recheckRec.GlobalSeq)
	}

	// Consumer over the whole shared feed: exactly one settlement, for the one
	// ACHIEVED key; nothing for any failed intent's key.
	c := newConsumer()
	c.track(iAbsent, iUneval, iFail, iVol, iOK, iCollide)
	c.Poll(t, feed)
	if got := len(c.ref.Settlement()); got != 1 {
		t.Fatalf("consumer settled %d event(s) over the shared feed, want exactly 1: %+v", got, c.ref.Settlement())
	}
	if k := c.ref.Settlement()[0].Key; k != intent.IdempotencyKey("key-sep-c") {
		t.Fatalf("settled key = %q, want %q", k, "key-sep-c")
	}
	for _, f := range failures {
		if f.i.IdempotencyKey == "key-sep-c" {
			continue // the collided near-duplicate shares the settled key by design
		}
		if got := settlementsForKey(c.ref, f.i.IdempotencyKey); got != 0 {
			t.Errorf("%s: consumer settled %d event(s) for key %q, want 0", f.name, got, f.i.IdempotencyKey)
		}
	}
}

// --- (g) thin-spec defense (CONTRACT.md §4.2 step 1b) ----------------------

// eventDetail returns the Detail of the first event of the given type, and
// whether one exists.
func eventDetail(events []audit.Event, typ string) (string, bool) {
	for _, e := range events {
		if e.Type == typ {
			return e.Detail, true
		}
	}
	return "", false
}

// TestFailClosedEmptyCriteria pins the property, not the seat: a resolved spec
// with zero criteria never reaches ACHIEVED, regardless of where resolution
// happens. "No criterion failed" must never be satisfied by "no criterion
// existed". The refusal record witnesses WHICH signed spec was thin (the
// UNEVALUABLE detail binds the claimed intent_spec_hash; a blank hash yields
// the bare "empty-criteria:" — witnessing that none was even claimed), the
// scorer is never consulted, and nothing settles. Covers nil and the empty
// slice — Go distinguishes them, the gate must not.
func TestFailClosedEmptyCriteria(t *testing.T) {
	cases := []struct {
		name       string
		crits      []intent.Criterion
		specHash   string
		wantDetail string
	}{
		{"nil-criteria", nil, "spec-thin", "empty-criteria:spec-thin"},
		{"empty-slice", []intent.Criterion{}, "spec-thin", "empty-criteria:spec-thin"},
		{"no-spec-hash-claimed", nil, "", "empty-criteria:"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			s := newScorer()
			feed := openFeed(t)
			g := New(s, feed, idempotency.NewStore())

			i := mkIntent("seed-thin-"+tc.name, "key-thin-"+tc.name, tc.specHash, tc.crits...)
			r := mustAuthorize(t, g, i)

			if r.Terminal == lifecycle.Achieved {
				t.Fatal("a spec with zero criteria must never reach ACHIEVED (vacuous grant)")
			}
			if r.Terminal != lifecycle.Failed {
				t.Fatalf("terminal = %q, want FAILED", r.Terminal)
			}
			if r.Reason != "unevaluable:empty-criteria" {
				t.Fatalf("reason = %q, want %q", r.Reason, "unevaluable:empty-criteria")
			}
			if r.AchievedSeq != 0 {
				t.Fatalf("AchievedSeq = %d, want 0 on FAILED", r.AchievedSeq)
			}
			detail, ok := eventDetail(r.Events, "UNEVALUABLE")
			if !ok {
				t.Fatalf("expected an UNEVALUABLE event, got %+v", r.Events)
			}
			if detail != tc.wantDetail {
				t.Fatalf("UNEVALUABLE detail = %q, want %q (the refusal must witness the thin spec's hash)", detail, tc.wantDetail)
			}
			if hasEventType(r.Events, "SCORED") {
				t.Fatalf("no criterion exists, so nothing may be SCORED: %+v", r.Events)
			}
			if len(s.Calls) != 0 {
				t.Fatalf("scorer consulted %d time(s) for a zero-criteria spec, want 0", len(s.Calls))
			}
			assertNoSettlement(t, feed, i)
		})
	}
}

// scorerFunc adapts a function to scoring.Scorer, for probing the gate with
// scorer behavior no in-repo scorer exhibits.
type scorerFunc func(c intent.Criterion, phase intent.Phase) scoring.Score

func (f scorerFunc) Score(_ context.Context, _ intent.Intent, c intent.Criterion, phase intent.Phase) scoring.Score {
	return f(c, phase)
}

// TestFailClosedOutOfDomainScore: scoring.Score is an open int; a Scorer
// returning a value outside the tri-state domain must fail CLOSED at
// declaration exactly as at the dispatch edge (which already refuses any
// non-Pass by exact match). Before the fix, an out-of-domain value fell
// through the declaration switch as an implicit pass, yielding the
// self-contradictory log "SCORED <name>:UNEVALUABLE" followed by ACHIEVED
// (found by the 2026-08-02 skeptic pass).
func TestFailClosedOutOfDomainScore(t *testing.T) {
	feed := openFeed(t)
	rogue := scorerFunc(func(c intent.Criterion, phase intent.Phase) scoring.Score {
		return scoring.Score(3) // out-of-domain: renders "UNEVALUABLE", is none of Pass/Fail/Unevaluable
	})
	g := New(rogue, feed, idempotency.NewStore())

	i := mkIntent("seed-rogue", "key-rogue", "spec-rogue", crit("alpha", intent.Stable))
	r := mustAuthorize(t, g, i)

	if r.Terminal == lifecycle.Achieved {
		t.Fatal("an out-of-domain score must never reach ACHIEVED (implicit-pass fall-through)")
	}
	if r.Terminal != lifecycle.Failed {
		t.Fatalf("terminal = %q, want FAILED", r.Terminal)
	}
	if r.Reason != "unevaluable:alpha" {
		t.Fatalf("reason = %q, want %q (out-of-domain scores are unevaluable-shaped)", r.Reason, "unevaluable:alpha")
	}
	if !hasEventType(r.Events, "UNEVALUABLE") {
		t.Fatalf("expected an UNEVALUABLE event, got %+v", r.Events)
	}
	assertNoSettlement(t, feed, i)
}

// TestFailClosedInvalidVolatility (B2b): a criterion whose volatility is
// neither "stable" nor "volatile" is an unknown kind and denies explicitly at
// resolution (P7) — a typo'd "volatile" must not silently become stable and
// skip the dispatch-edge re-verify. The refusal happens before any scoring
// (spec-shape validation, like empty-criteria). Honesty bound: this closes the
// typo case only; a criterion semantically mislabeled stable is
// authoring/attestation territory the string cannot reveal.
func TestFailClosedInvalidVolatility(t *testing.T) {
	cases := []struct {
		name string
		vol  intent.Volatility
	}{
		{"typo", intent.Volatility("volatil")},
		{"blank", intent.Volatility("")},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			s := newScorer()
			feed := openFeed(t)
			g := New(s, feed, idempotency.NewStore())

			i := mkIntent("seed-vol-"+tc.name, "key-vol-"+tc.name, "spec-vol",
				crit("alpha", intent.Stable),
				intent.Criterion{Name: "beta", Threshold: 0.5, Volatility: tc.vol},
			)
			r := mustAuthorize(t, g, i)

			if r.Terminal == lifecycle.Achieved {
				t.Fatal("an unknown volatility must never reach ACHIEVED (it would silently skip the dispatch-edge re-verify)")
			}
			if r.Terminal != lifecycle.Failed {
				t.Fatalf("terminal = %q, want FAILED", r.Terminal)
			}
			if r.Reason != "unevaluable:invalid-volatility:beta" {
				t.Fatalf("reason = %q, want %q", r.Reason, "unevaluable:invalid-volatility:beta")
			}
			if r.AchievedSeq != 0 {
				t.Fatalf("AchievedSeq = %d, want 0 on FAILED", r.AchievedSeq)
			}
			detail, ok := eventDetail(r.Events, "UNEVALUABLE")
			if !ok {
				t.Fatalf("expected an UNEVALUABLE event, got %+v", r.Events)
			}
			want := "invalid-volatility:beta:" + string(tc.vol)
			if detail != want {
				t.Fatalf("UNEVALUABLE detail = %q, want %q (the record witnesses what arrived)", detail, want)
			}
			if len(s.Calls) != 0 {
				t.Fatalf("scorer consulted %d time(s) before the spec was validly resolved, want 0", len(s.Calls))
			}
			assertNoSettlement(t, feed, i)
		})
	}
}
