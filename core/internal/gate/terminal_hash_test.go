package gate

// Refusal-hash commitment (CONTRACT.md §2.3 2026-08-08 amendment; §5.4 claim
// 13): the terminal-position record of EVERY completed authorization carries
// trajectory_hash, it equals Result.TrajectoryHash, it recomputes from the
// per-intent feed records alone, and no non-terminal record carries one.

import (
	"testing"

	"github.com/hossainpazooki/intent-plane/core/internal/audit"
	"github.com/hossainpazooki/intent-plane/core/internal/durable"
	"github.com/hossainpazooki/intent-plane/core/internal/idempotency"
	"github.com/hossainpazooki/intent-plane/core/internal/intent"
	"github.com/hossainpazooki/intent-plane/core/internal/scoring"
)

// terminalRecord returns the record with the maximum IntentSeq for one intent —
// the terminal-position record per §2.3.
func terminalRecord(t *testing.T, feed *durable.Store, intentID string) durable.Record {
	t.Helper()
	recs := feed.ByIntent(intentID)
	if len(recs) == 0 {
		t.Fatalf("no feed records for intent %s", intentID)
	}
	return recs[len(recs)-1]
}

// recomputeHash rebuilds the per-intent audit log from the feed records (in
// IntentSeq order) and returns its TrajectoryHash — the recomputation a
// verifier performs, done here with the repo's own encoder.
func recomputeHash(t *testing.T, feed *durable.Store, intentID string) string {
	t.Helper()
	log := audit.NewEventLog()
	for i, r := range feed.ByIntent(intentID) {
		if r.IntentSeq != i {
			t.Fatalf("intent %s: records not gap-free at position %d (intent_seq %d)", intentID, i, r.IntentSeq)
		}
		log.Append(r.Type, r.Detail)
	}
	return log.TrajectoryHash()
}

// assertTerminalHash asserts the §2.3 placement rule for one completed intent:
// hash on the terminal-position record only, equal to Result.TrajectoryHash,
// and recomputable from the feed records alone.
func assertTerminalHash(t *testing.T, feed *durable.Store, intentID, wantType string, r Result) {
	t.Helper()
	term := terminalRecord(t, feed, intentID)
	if term.Type != wantType {
		t.Fatalf("intent %s: terminal-position record type = %q, want %q", intentID, term.Type, wantType)
	}
	if term.TrajectoryHash == "" {
		t.Fatalf("intent %s: terminal-position %s record carries no trajectory_hash", intentID, term.Type)
	}
	if term.TrajectoryHash != r.TrajectoryHash {
		t.Fatalf("intent %s: committed hash %q != Result.TrajectoryHash %q", intentID, term.TrajectoryHash, r.TrajectoryHash)
	}
	if got := recomputeHash(t, feed, intentID); got != term.TrajectoryHash {
		t.Fatalf("intent %s: committed hash %q does not recompute from the feed (got %q)", intentID, term.TrajectoryHash, got)
	}
	recs := feed.ByIntent(intentID)
	for _, rec := range recs[:len(recs)-1] {
		if rec.TrajectoryHash != "" {
			t.Fatalf("intent %s: non-terminal record (intent_seq %d, type %s) carries trajectory_hash", intentID, rec.IntentSeq, rec.Type)
		}
	}
}

func TestTerminalHashCommitment(t *testing.T) {
	feed := openFeed(t)
	store := idempotency.NewStore()
	fs := newScorer()
	// alpha fails at declaration for the FAILED intent; gamma (volatile) fails
	// only at dispatch for the volatile-recheck intent. FakeScorer defaults to
	// Pass for unset keys, so every other criterion passes.
	fs.Results[scoring.ScoreKey{Criterion: "fail-alpha", Phase: intent.Declaration}] = scoring.Fail
	fs.Results[scoring.ScoreKey{Criterion: "drift-gamma", Phase: intent.Dispatch}] = scoring.Fail
	g := New(fs, feed, store)

	// ACHIEVED.
	achieved := mkIntent("seed-ach", "key-ach", "hash-ach", crit("alpha", intent.Stable))
	rAch := mustAuthorize(t, g, achieved)
	assertTerminalHash(t, feed, achieved.ID(), "ACHIEVED", rAch)

	// Criteria-FAILED: the FAILED transition record is terminal.
	failed := mkIntent("seed-fail", "key-fail", "hash-fail", crit("fail-alpha", intent.Stable))
	rFail := mustAuthorize(t, g, failed)
	assertTerminalHash(t, feed, failed.ID(), "FAILED", rFail)

	// FAILED_AT_DISPATCH via volatile recheck.
	drift := mkIntent("seed-drift", "key-drift", "hash-drift", crit("drift-gamma", intent.Volatile))
	rDrift := mustAuthorize(t, g, drift)
	assertTerminalHash(t, feed, drift.ID(), "FAILED_AT_DISPATCH", rDrift)

	// FAILED_AT_DISPATCH via idempotency collision (reuses the ACHIEVED key).
	collide := mkIntent("seed-collide", "key-ach", "hash-collide", crit("alpha", intent.Stable))
	rCollide := mustAuthorize(t, g, collide)
	assertTerminalHash(t, feed, collide.ID(), "FAILED_AT_DISPATCH", rCollide)

	// SHADOW_RECORDED.
	shadow := mkIntent("seed-shadow", "key-shadow", "hash-shadow", crit("alpha", intent.Stable))
	shadow.Spec.Posture = intent.PostureShadow
	rShadow := mustAuthorize(t, g, shadow)
	assertTerminalHash(t, feed, shadow.ID(), "SHADOW_RECORDED", rShadow)

	// Step-1 refusal with NO terminal transition record: empty criteria. The
	// final UNEVALUABLE record is the terminal-position record.
	thin := mkIntent("seed-thin", "key-thin", "hash-thin")
	rThin := mustAuthorize(t, g, thin)
	assertTerminalHash(t, feed, thin.ID(), "UNEVALUABLE", rThin)

	// Step-1 refusal before the key exists: absent key.
	nokey := mkIntent("seed-nokey", "", "hash-nokey", crit("alpha", intent.Stable))
	rNokey := mustAuthorize(t, g, nokey)
	assertTerminalHash(t, feed, nokey.ID(), "UNEVALUABLE", rNokey)
}
