package gate

// Golden feed fixture generator + drift gate (CONTRACT.md §9.1). The frozen
// core/contract/feed/events-good.jsonl is byte-compared against a fresh
// re-drive of its scenarios through the REAL gate, so the fixture can never
// drift from the gate that emits it. A missing fixture FAILS (it is in-repo
// and load-bearing for both verifier twins — a skipped witness is a deletable
// witness); regeneration is explicit:
//
//	INTENT_FEED_FIXTURE_WRITE=1 go test ./core/internal/gate -run FeedFixture
//
// Scenario names are NEUTRAL exemplars (§9.1: no neutrality exemption needed).

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hossainpazooki/intent-plane/core/internal/durable"
	"github.com/hossainpazooki/intent-plane/core/internal/idempotency"
	"github.com/hossainpazooki/intent-plane/core/internal/intent"
	"github.com/hossainpazooki/intent-plane/core/internal/scoring"
)

// driveFixtureScenarios runs one intent per terminal class — ACHIEVED,
// criteria-FAILED, volatile-recheck FAILED_AT_DISPATCH, idempotency
// collision, SHADOW_RECORDED, and a step-1 thin-spec refusal — into dir's
// feed. Everything is deterministic (fixed order, logical clocks, FakeScorer),
// so two drives produce byte-identical events.jsonl.
func driveFixtureScenarios(t *testing.T, dir string) {
	t.Helper()
	feed, err := durable.Open(dir)
	if err != nil {
		t.Fatalf("durable.Open: %v", err)
	}
	defer feed.Close()
	fs := newScorer()
	fs.Results[scoring.ScoreKey{Criterion: "fail-alpha", Phase: intent.Declaration}] = scoring.Fail
	fs.Results[scoring.ScoreKey{Criterion: "drift-gamma", Phase: intent.Dispatch}] = scoring.Fail
	g := New(fs, feed, idempotency.NewStore(), WithScorerID("live"))

	shadow := mkIntent("fixture-shadow", "key-shadow", "hash-shadow", crit("alpha", intent.Stable))
	shadow.Spec.Posture = intent.PostureShadow
	// A grant with NO rule_artifact_hash: the artifact-plane passthrough is
	// optional, and this exemplar pins the §9.1 ruling that its absence is
	// never a finding.
	noRule := mkIntent("fixture-no-rule-hash", "key-no-rule", "hash-no-rule", crit("alpha", intent.Stable))
	noRule.RuleArtifactHash = ""

	for _, i := range []intent.Intent{
		mkIntent("fixture-achieved", "key-achieved", "hash-achieved",
			crit("alpha", intent.Stable), crit("gamma", intent.Volatile)),
		mkIntent("fixture-failed", "key-failed", "hash-failed", crit("fail-alpha", intent.Stable)),
		mkIntent("fixture-drift", "key-drift", "hash-drift", crit("drift-gamma", intent.Volatile)),
		mkIntent("fixture-collide", "key-achieved", "hash-collide", crit("alpha", intent.Stable)),
		shadow,
		mkIntent("fixture-thin", "key-thin", "hash-thin"),
		noRule,
	} {
		mustAuthorize(t, g, i)
	}
}

func TestFeedFixtureMatchesGate(t *testing.T) {
	fixture := filepath.Join("..", "..", "contract", "feed", "events-good.jsonl")

	tmp := t.TempDir()
	driveFixtureScenarios(t, tmp)
	produced, err := os.ReadFile(filepath.Join(tmp, "events.jsonl"))
	if err != nil {
		t.Fatalf("reading produced feed: %v", err)
	}

	if os.Getenv("INTENT_FEED_FIXTURE_WRITE") == "1" {
		if err := os.MkdirAll(filepath.Dir(fixture), 0o755); err != nil {
			t.Fatalf("mkdir fixture dir: %v", err)
		}
		if err := os.WriteFile(fixture, produced, 0o644); err != nil {
			t.Fatalf("writing fixture: %v", err)
		}
		t.Logf("fixture regenerated: %s (%d bytes)", fixture, len(produced))
	}

	frozen, err := os.ReadFile(fixture)
	if err != nil {
		// FAIL, not skip: the fixture is in-repo and both verifier twins
		// hang their conformance on it.
		t.Fatalf("frozen fixture missing (%v) - regenerate with INTENT_FEED_FIXTURE_WRITE=1", err)
	}
	if string(frozen) != string(produced) {
		t.Fatalf("frozen fixture does not match a fresh gate drive (%d vs %d bytes): the gate's feed encoding drifted, or the fixture was edited; amend CONTRACT.md first, then regenerate", len(frozen), len(produced))
	}
}
