package gate

// Plane-amendment acceptance: the gate refuses unattested and revoked specs,
// records shadow-posture intents without authorizing, and refuses unknown
// postures. All built red-first against the pre-amendment gate.

import (
	"context"
	"testing"

	"github.com/hossainpazooki/intent-plane/core/internal/durable"
	"github.com/hossainpazooki/intent-plane/core/internal/idempotency"
	"github.com/hossainpazooki/intent-plane/core/internal/intent"
	"github.com/hossainpazooki/intent-plane/core/internal/lifecycle"
	"github.com/hossainpazooki/intent-plane/core/internal/scoring"
)

// planeScorer is a local test scorer: default Pass, call counting, and an
// on-dispatch hook so a test can act BETWEEN declaration scoring and the
// dispatch edge.
type planeScorer struct {
	calls      int
	OnDispatch func()
}

func (p *planeScorer) Score(_ context.Context, _ intent.Intent, _ intent.Criterion, phase intent.Phase) scoring.Score {
	p.calls++
	if phase == intent.Dispatch && p.OnDispatch != nil {
		p.OnDispatch()
	}
	return scoring.Pass
}

func (p *planeScorer) CallCount() int { return p.calls }

// newGateForTest builds a gate over fresh stores with an all-Pass planeScorer.
func newGateForTest(t *testing.T, opts ...Option) (*Gate, *durable.Store, *planeScorer) {
	t.Helper()
	feed := openFeed(t)
	sc := &planeScorer{}
	return New(sc, feed, idempotency.NewStore(), opts...), feed, sc
}

// staticRevocations is a RevocationChecker over a fixed map, for the
// dispatch-edge re-check path.
type staticRevocations map[string]string

func (s staticRevocations) RevokedRef(hash string) (string, bool) {
	ref, ok := s[hash]
	return ref, ok
}

// mutableRevocations lets a test revoke BETWEEN declaration and dispatch by
// flipping after the declaration-time check has passed. The gate consults it
// once per phase, so a scorer hook flips it mid-lifecycle.
type mutableRevocations struct {
	ref string
	on  bool
}

func (m *mutableRevocations) RevokedRef(string) (string, bool) {
	if m.on {
		return m.ref, true
	}
	return "", false
}

// An unattested spec refuses before any scoring: unevaluable never passes, and
// "no verified spec" is unevaluable-shaped absence (P1's fail-closed floor).
func TestUnattestedSpecRefuses(t *testing.T) {
	g, _, sc := newGateForTest(t)
	i := mkIntent("seed-unattested", "key-ua", "spec-ua", crit("alpha", intent.Stable))
	i.Resolution = intent.Resolution{} // NOT attested
	res, err := g.Authorize(context.Background(), i)
	if err != nil {
		t.Fatal(err)
	}
	if res.Terminal != lifecycle.Failed || res.Reason != "unevaluable:unattested-spec" {
		t.Fatalf("terminal %s reason %q, want FAILED unevaluable:unattested-spec", res.Terminal, res.Reason)
	}
	if sc.CallCount() != 0 {
		t.Fatalf("scorer was called %d times on an unattested spec", sc.CallCount())
	}
}

// A spec revoked BEFORE declaration refuses at declaration.
func TestRevokedAtDeclarationRefuses(t *testing.T) {
	g, _, sc := newGateForTest(t, WithRevocations(staticRevocations{"spec-rv": "policy-superseded"}))
	i := mkIntent("seed-revoked", "key-rv", "spec-rv", crit("alpha", intent.Stable))
	res, err := g.Authorize(context.Background(), i)
	if err != nil {
		t.Fatal(err)
	}
	if res.Terminal != lifecycle.Failed || res.Reason != "revoked:policy-superseded" {
		t.Fatalf("terminal %s reason %q, want FAILED revoked:policy-superseded", res.Terminal, res.Reason)
	}
	if sc.CallCount() != 0 {
		t.Fatal("scorer was called for a revoked spec")
	}
}

// A spec revoked BETWEEN verification and dispatch is stopped at the edge:
// the reserved `revoked:<ref>` cause class, activated. FAILED_AT_DISPATCH, no
// ACHIEVED record, key NOT reserved.
func TestRevokedBetweenVerifyAndDispatch(t *testing.T) {
	rev := &mutableRevocations{ref: "emergency-pull"}
	g, feed, sc := newGateForTest(t, WithRevocations(rev))
	// Flip revocation on when the volatile recheck fires: that is after the
	// declaration-time check and immediately before the dispatch-edge check.
	sc.OnDispatch = func() { rev.on = true }
	i := mkIntent("seed-edge-rv", "key-edge-rv", "spec-edge-rv",
		crit("alpha", intent.Stable), crit("beta", intent.Volatile))
	res, err := g.Authorize(context.Background(), i)
	if err != nil {
		t.Fatal(err)
	}
	if res.Terminal != lifecycle.FailedAtDispatch || res.Reason != "revoked:emergency-pull" {
		t.Fatalf("terminal %s reason %q, want FAILED_AT_DISPATCH revoked:emergency-pull", res.Terminal, res.Reason)
	}
	if n := len(feed.Since(0, "ACHIEVED")); n != 0 {
		t.Fatalf("%d ACHIEVED records for a revoked-at-edge intent", n)
	}
	// The key must NOT be reserved: a same-key re-declare after re-attestation
	// is legitimate.
	i2 := mkIntent("seed-edge-rv-2", "key-edge-rv", "spec-fresh", crit("alpha", intent.Stable))
	rev.on = false
	res2, err := g.Authorize(context.Background(), i2)
	if err != nil {
		t.Fatal(err)
	}
	if res2.Terminal != lifecycle.Achieved {
		t.Fatalf("same-key re-declare after edge revocation: %s %q", res2.Terminal, res2.Reason)
	}
}

// Shadow posture: full scoring runs, the terminal is SHADOW_RECORDED with a
// durable record, but NOTHING authorizes — no ACHIEVED, no key reservation.
// Record-only shadow mode as a signed authority state, never a config toggle.
func TestShadowPostureRecordsWithoutAuthorizing(t *testing.T) {
	g, feed, sc := newGateForTest(t)
	i := mkIntent("seed-shadow", "key-shadow", "spec-shadow",
		crit("alpha", intent.Stable), crit("beta", intent.Volatile))
	i.Spec.Posture = intent.PostureShadow
	res, err := g.Authorize(context.Background(), i)
	if err != nil {
		t.Fatal(err)
	}
	if res.Terminal != lifecycle.ShadowRecorded {
		t.Fatalf("terminal %s, want SHADOW_RECORDED", res.Terminal)
	}
	if res.AchievedSeq != 0 {
		t.Fatal("shadow posture produced an achieved_seq")
	}
	if n := len(feed.Since(0, "ACHIEVED")); n != 0 {
		t.Fatalf("%d ACHIEVED records under shadow posture", n)
	}
	if n := len(feed.Since(0, "SHADOW_RECORDED")); n != 1 {
		t.Fatalf("%d SHADOW_RECORDED records, want exactly 1", n)
	}
	// Scoring DID run in full: declaration for both criteria + dispatch
	// recheck for the volatile one.
	if sc.CallCount() != 3 {
		t.Fatalf("scorer calls %d, want 3 (shadow scores fully)", sc.CallCount())
	}
	// The key was NOT reserved: the same key must still be reservable by a
	// later enforce-posture intent.
	i2 := mkIntent("seed-shadow-2", "key-shadow", "spec-enforce", crit("alpha", intent.Stable))
	res2, err := g.Authorize(context.Background(), i2)
	if err != nil {
		t.Fatal(err)
	}
	if res2.Terminal != lifecycle.Achieved {
		t.Fatalf("enforce after shadow with same key: %s %q (shadow must not reserve)", res2.Terminal, res2.Reason)
	}
}

// An unknown posture is unevaluable-shaped absence: refuse, never default.
func TestInvalidPostureRefuses(t *testing.T) {
	g, _, _ := newGateForTest(t)
	i := mkIntent("seed-posture", "key-posture", "spec-posture", crit("alpha", intent.Stable))
	i.Spec.Posture = intent.Posture("observe") // not a posture
	res, err := g.Authorize(context.Background(), i)
	if err != nil {
		t.Fatal(err)
	}
	if res.Terminal != lifecycle.Failed || res.Reason != "unevaluable:invalid-posture" {
		t.Fatalf("terminal %s reason %q, want FAILED unevaluable:invalid-posture", res.Terminal, res.Reason)
	}
}

// A spec still carrying an unresolved human-judgment obligation NEVER
// authorizes: the deliberately-unquantified check belongs to a human, and the
// gate's refusal of it is the system working (P6 made mechanical).
func TestHumanJudgmentRefuses(t *testing.T) {
	g, feed, sc := newGateForTest(t)
	i := mkIntent("seed-hj", "key-hj", "spec-hj", crit("alpha", intent.Stable))
	i.Spec.HumanJudgment = []string{"fairness-review"}
	res, err := g.Authorize(context.Background(), i)
	if err != nil {
		t.Fatal(err)
	}
	if res.Terminal != lifecycle.Failed || res.Reason != "unevaluable:human-judgment:fairness-review" {
		t.Fatalf("terminal %s reason %q, want FAILED unevaluable:human-judgment:fairness-review", res.Terminal, res.Reason)
	}
	if sc.CallCount() != 0 {
		t.Fatal("scorer consulted a human-judgment spec")
	}
	if n := len(feed.Since(0, "ACHIEVED")); n != 0 {
		t.Fatal("human-judgment spec reached ACHIEVED")
	}
}

// The SERVER path for a revoked spec arrives with Attested:false AND
// RevokedRef set (the resolver refused before any payload was produced). The
// refusal cause must witness the TRUTH — revoked, not merely unattested: both
// refuse, but `revoked:<ref>` names the tombstone while unattested-spec would
// erase it. Found by end-to-end smoke, not unit tests: the ordering of the
// two checks was wrong.
func TestRevokedResolutionWinsOverUnattested(t *testing.T) {
	g, _, _ := newGateForTest(t)
	i := mkIntent("seed-rv-res", "key-rv-res", "spec-rv-res", crit("alpha", intent.Stable))
	i.Resolution = intent.Resolution{Attested: false, RevokedRef: "emergency-pull"}
	res, err := g.Authorize(context.Background(), i)
	if err != nil {
		t.Fatal(err)
	}
	if res.Terminal != lifecycle.Failed || res.Reason != "revoked:emergency-pull" {
		t.Fatalf("terminal %s reason %q, want FAILED revoked:emergency-pull", res.Terminal, res.Reason)
	}
}
