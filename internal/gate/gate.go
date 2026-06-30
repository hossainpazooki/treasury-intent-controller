// Package gate drives the full intent authorization lifecycle deterministically.
//
// The gate is the sole emitter of the ACHIEVED event and the single orchestrator
// of the DECLARED -> RESOLVING -> ACTIVE -> VERIFYING -> {ACHIEVED | FAILED |
// FAILED_AT_DISPATCH} lifecycle. It reads no artifacts: criteria, thresholds, and
// the idempotency key arrive on the Intent. Scoring is fail-closed (Unevaluable
// never collapses to pass), volatile criteria are re-verified at the dispatch
// edge, and the run is fully deterministic (logical clock, no wallclock, criteria
// iterated in slice order).
package gate

import (
	"context"
	"strings"

	"github.com/pazooki/treasury-intent-controller/internal/adapter"
	"github.com/pazooki/treasury-intent-controller/internal/audit"
	"github.com/pazooki/treasury-intent-controller/internal/idempotency"
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

// Gate authorizes intents against the scorer, adapter, and idempotency store.
type Gate struct {
	scorer  scoring.Scorer
	adapter adapter.Adapter
	store   *idempotency.Store
}

// New constructs a Gate over the given scorer, adapter, and store.
func New(s scoring.Scorer, a adapter.Adapter, store *idempotency.Store) *Gate {
	return &Gate{scorer: s, adapter: a, store: store}
}

// Authorize drives the full lifecycle deterministically and returns the terminal
// Result. See the build contract for the authoritative 5-step algorithm; this
// implementation iterates criteria in slice order and uses only the per-intent
// logical clock embedded in the event log (no wallclock).
func (g *Gate) Authorize(ctx context.Context, i intent.Intent) Result {
	log := audit.NewEventLog()
	state := lifecycle.Declared

	// finish builds the terminal Result from the current log. Settlement is left
	// nil for every non-ACHIEVED outcome (spec invariant 5).
	finish := func(terminal lifecycle.State, reason string, settlement *adapter.SettlementEvent) Result {
		return Result{
			Terminal:       terminal,
			Reason:         reason,
			Events:         log.Events(),
			TrajectoryHash: log.TrajectoryHash(),
			Settlement:     settlement,
		}
	}

	// advance performs a lifecycle transition, asserting it is permitted by the
	// graph before recording it. Used for every state move past DECLARED.
	advance := func(to lifecycle.State, typ, detail string) bool {
		if !lifecycle.IsValidTransition(state, to) {
			return false
		}
		state = to
		log.Append(typ, detail)
		return true
	}

	// Step 1: DECLARED.
	log.Append("DECLARED", i.ID())
	if i.IdempotencyKey == "" {
		// Refuse at declaration: the absent key is unevaluable, fail-closed.
		log.Append("UNEVALUABLE", "absent-key")
		log.Append(string(lifecycle.Failed), "unevaluable:absent-key")
		return finish(lifecycle.Failed, "unevaluable:absent-key", nil)
	}

	// Step 2: RESOLVING -> ACTIVE -> VERIFYING (each transition graph-checked).
	advance(lifecycle.Resolving, string(lifecycle.Resolving), "")
	advance(lifecycle.Active, string(lifecycle.Active), "")
	advance(lifecycle.Verifying, string(lifecycle.Verifying), "")

	// Step 3: declaration scoring of ALL criteria, in slice order.
	var failed []string
	for _, c := range i.Spec.Criteria {
		score := g.scorer.Score(ctx, i, c, intent.Declaration)
		log.Append("SCORED", c.Name+":"+score.String())

		switch score {
		case scoring.Unevaluable:
			// Fail-closed: unevaluable never collapses to pass.
			log.Append("UNEVALUABLE", c.Name)
			reason := "unevaluable:" + c.Name
			advance(lifecycle.Failed, string(lifecycle.Failed), reason)
			return finish(lifecycle.Failed, reason, nil)
		case scoring.Fail:
			failed = append(failed, c.Name)
		}
	}
	if len(failed) > 0 {
		reason := strings.Join(failed, ",")
		advance(lifecycle.Failed, string(lifecycle.Failed), reason)
		return finish(lifecycle.Failed, reason, nil)
	}

	// Step 4a: dispatch edge — re-verify VOLATILE criteria only, in slice order.
	for _, c := range i.Spec.Criteria {
		if c.Volatility != intent.Volatile {
			continue
		}
		score := g.scorer.Score(ctx, i, c, intent.Dispatch)
		log.Append("RECHECK", c.Name+":"+score.String())
		if score != scoring.Pass {
			reason := "volatile-recheck:" + c.Name
			advance(lifecycle.FailedAtDispatch, string(lifecycle.FailedAtDispatch), reason)
			return finish(lifecycle.FailedAtDispatch, reason, nil)
		}
	}

	// Step 4b: idempotency reserve at the dispatch edge.
	if ok := g.store.Reserve(i.ID(), i.IdempotencyKey); !ok {
		reason := "idempotency-collision"
		advance(lifecycle.FailedAtDispatch, string(lifecycle.FailedAtDispatch), reason)
		return finish(lifecycle.FailedAtDispatch, reason, nil)
	}
	log.Append("IDEMPOTENCY_RESERVED", string(i.IdempotencyKey))

	// Step 5: authorize. Emit the single ACHIEVED event, then drive the adapter.
	advance(lifecycle.Achieved, string(lifecycle.Achieved), i.ID())
	se, _ := g.adapter.OnAchieved(i)
	settlement := se
	return finish(lifecycle.Achieved, "", &settlement)
}
