// Package gate drives the full intent authorization lifecycle deterministically.
//
// The gate is the sole emitter of the ACHIEVED event and the single orchestrator
// of the DECLARED -> RESOLVING -> ACTIVE -> VERIFYING -> {ACHIEVED | FAILED |
// FAILED_AT_DISPATCH} lifecycle. Under CONTRACT-V2 it is emit-and-observe: it
// mirrors every event to the durable feed, stops at appending the single
// ACHIEVED record, and never settles in-process. A downstream consumer settles
// from the feed.
package gate

import (
	"context"
	"fmt"
	"strings"

	"github.com/pazooki/treasury-intent-controller/internal/audit"
	"github.com/pazooki/treasury-intent-controller/internal/durable"
	"github.com/pazooki/treasury-intent-controller/internal/idempotency"
	"github.com/pazooki/treasury-intent-controller/internal/intent"
	"github.com/pazooki/treasury-intent-controller/internal/lifecycle"
	"github.com/pazooki/treasury-intent-controller/internal/scoring"
)

// Result is the terminal outcome of one authorization. Settlement is REMOVED
// (the gate no longer settles; a downstream consumer settles from the feed).
// Events + TrajectoryHash are the per-intent log (no GlobalSeq) and are
// byte-identical across replay, exactly as in slice 1.
type Result struct {
	Terminal       lifecycle.State // ACHIEVED | FAILED | FAILED_AT_DISPATCH
	Reason         string          // failed criterion names / "unevaluable:<crit>" / "idempotency-collision" / ""
	Events         []audit.Event   // per-intent append-only log (unchanged shape)
	TrajectoryHash string          // per-intent hash over Events (unchanged)
	AchievedSeq    int             // GlobalSeq of the emitted ACHIEVED record; 0 if not ACHIEVED
}

// Gate authorizes intents against the scorer, the durable feed, and the
// idempotency store. It holds NO settlement dependency (emit-and-observe).
type Gate struct {
	scorer scoring.Scorer
	feed   *durable.Store
	store  *idempotency.Store
}

// New constructs a Gate over the scorer, the (shared, durable) feed, and the
// (shared, durable) idempotency store.
func New(s scoring.Scorer, feed *durable.Store, store *idempotency.Store) *Gate {
	return &Gate{scorer: s, feed: feed, store: store}
}

// Authorize drives the full lifecycle deterministically (CONTRACT.md §gate
// algorithm + CONTRACT-V2 §V2.3 deltas). It mirrors EVERY event to the durable
// feed as it appends to the in-memory per-intent log, preserving the per-intent
// Seq and TrajectoryHash exactly as in slice 1.
//
//  1. DECLARED. Empty idempotency key -> append UNEVALUABLE, FAILED with reason
//     "unevaluable:absent-key". Return.
//  2. RESOLVING -> ACTIVE -> VERIFYING, each a logged, IsValidTransition-checked
//     transition.
//  3. Declaration scoring for EACH criterion in slice order (never map order):
//     append SCORED "<name>:<score>". Unevaluable -> append UNEVALUABLE, FAILED
//     "unevaluable:<name>" (fail-closed; never a pass). Any Fail -> after all
//     criteria, FAILED with the joined failed names.
//  4. Dispatch edge: (a) re-score VOLATILE criteria only, append RECHECK
//     "<name>:<score>"; any not Pass -> FAILED_AT_DISPATCH
//     "volatile-recheck:<name>". (b) reserve the idempotency key; collision ->
//     FAILED_AT_DISPATCH "idempotency-collision"; success -> append
//     IDEMPOTENCY_RESERVED.
//  5. EMIT-ONLY authorize: append the single ACHIEVED event in-memory, compute
//     the TrajectoryHash (which includes ACHIEVED, same value as slice 1), then
//     feed.Append the ACHIEVED durable.Record carrying the four trace fields
//     {IdempotencyKey, RuleArtifactHash, IntentSpecHash, TrajectoryHash}. Set
//     Result.AchievedSeq to that record's GlobalSeq. Nothing settles in-process.
//
// Any feed.Append error aborts: the partial Result built so far is returned with
// a non-nil error; no terminal guarantee is implied. Determinism: per-intent
// Events and TrajectoryHash are byte-identical across independent runs;
// GlobalSeq never enters Events or the hash.
func (g *Gate) Authorize(ctx context.Context, i intent.Intent) (Result, error) {
	id := i.ID()
	log := audit.NewEventLog()
	state := lifecycle.Declared

	// partial snapshots the log built so far, for the abort-on-feed-error path.
	partial := func() Result {
		return Result{Events: log.Events(), TrajectoryHash: log.TrajectoryHash()}
	}

	// emit appends to the in-memory per-intent log and mirrors the event to the
	// durable feed, preserving the per-intent Seq as IntentSeq. GlobalSeq is
	// assigned by the feed and never enters the in-memory log or the hash.
	emit := func(typ, detail string) error {
		e := log.Append(typ, detail)
		_, err := g.feed.Append(durable.Record{
			IntentID:  id,
			IntentSeq: e.Seq,
			Type:      e.Type,
			Detail:    e.Detail,
		})
		return err
	}

	// transition moves the lifecycle to `to` (IsValidTransition-checked) and
	// logs it as an event of type string(to).
	transition := func(to lifecycle.State, detail string) error {
		if !lifecycle.IsValidTransition(state, to) {
			return fmt.Errorf("gate: invalid lifecycle transition %s -> %s", state, to)
		}
		state = to
		return emit(string(to), detail)
	}

	terminal := func(term lifecycle.State, reason string) Result {
		return Result{
			Terminal:       term,
			Reason:         reason,
			Events:         log.Events(),
			TrajectoryHash: log.TrajectoryHash(),
		}
	}

	// Step 1: DECLARED. An absent idempotency key is refused at declaration,
	// before any scoring (the key is unevaluable; fail-closed). Terminal is
	// FAILED in the Result; DECLARED->FAILED is not a lifecycle edge, so no
	// FAILED transition event is logged on this path (contract step 1 appends
	// UNEVALUABLE only).
	if err := emit("DECLARED", id); err != nil {
		return partial(), err
	}
	if i.IdempotencyKey == "" {
		if err := emit("UNEVALUABLE", "absent-key"); err != nil {
			return partial(), err
		}
		return terminal(lifecycle.Failed, "unevaluable:absent-key"), nil
	}

	// Step 2: DECLARED -> RESOLVING -> ACTIVE -> VERIFYING.
	for _, next := range []lifecycle.State{lifecycle.Resolving, lifecycle.Active, lifecycle.Verifying} {
		if err := transition(next, ""); err != nil {
			return partial(), err
		}
	}

	// Step 3: declaration scoring, every criterion (stable AND volatile), in
	// slice order. Unevaluable fails closed immediately; Fails are collected so
	// the reason names every failed criterion.
	var failed []string
	for _, c := range i.Spec.Criteria {
		score := g.scorer.Score(ctx, i, c, intent.Declaration)
		if err := emit("SCORED", c.Name+":"+score.String()); err != nil {
			return partial(), err
		}
		switch score {
		case scoring.Unevaluable:
			reason := "unevaluable:" + c.Name
			if err := emit("UNEVALUABLE", c.Name); err != nil {
				return partial(), err
			}
			if err := transition(lifecycle.Failed, reason); err != nil {
				return partial(), err
			}
			return terminal(lifecycle.Failed, reason), nil
		case scoring.Fail:
			failed = append(failed, c.Name)
		}
	}
	if len(failed) > 0 {
		reason := strings.Join(failed, ",")
		if err := transition(lifecycle.Failed, reason); err != nil {
			return partial(), err
		}
		return terminal(lifecycle.Failed, reason), nil
	}

	// Step 4a: dispatch-edge re-verify, VOLATILE criteria only (stable criteria
	// are NOT re-scored). Any non-Pass at the edge is FAILED_AT_DISPATCH.
	for _, c := range i.Spec.Criteria {
		if c.Volatility != intent.Volatile {
			continue
		}
		score := g.scorer.Score(ctx, i, c, intent.Dispatch)
		if err := emit("RECHECK", c.Name+":"+score.String()); err != nil {
			return partial(), err
		}
		if score == scoring.Pass {
			continue
		}
		if score == scoring.Unevaluable {
			// Unevaluable is logged distinctly (never collapses into pass).
			if err := emit("UNEVALUABLE", c.Name); err != nil {
				return partial(), err
			}
		}
		reason := "volatile-recheck:" + c.Name
		if err := transition(lifecycle.FailedAtDispatch, reason); err != nil {
			return partial(), err
		}
		return terminal(lifecycle.FailedAtDispatch, reason), nil
	}

	// Step 4b: idempotency reserve at the dispatch edge.
	if !g.store.Reserve(id, i.IdempotencyKey) {
		reason := "idempotency-collision"
		if err := transition(lifecycle.FailedAtDispatch, reason); err != nil {
			return partial(), err
		}
		return terminal(lifecycle.FailedAtDispatch, reason), nil
	}
	if err := emit("IDEMPOTENCY_RESERVED", string(i.IdempotencyKey)); err != nil {
		return partial(), err
	}

	// Step 5: EMIT-ONLY authorize. Append the single ACHIEVED event in-memory,
	// compute the hash INCLUDING it (same value as slice 1), then emit the
	// durable ACHIEVED record carrying the four trace fields. Nothing settles
	// in-process; a downstream consumer settles from the feed.
	if !lifecycle.IsValidTransition(state, lifecycle.Achieved) {
		return partial(), fmt.Errorf("gate: invalid lifecycle transition %s -> %s", state, lifecycle.Achieved)
	}
	e := log.Append("ACHIEVED", id)
	th := log.TrajectoryHash()
	rec, err := g.feed.Append(durable.Record{
		IntentID:         id,
		IntentSeq:        e.Seq,
		Type:             e.Type,
		Detail:           e.Detail,
		IdempotencyKey:   string(i.IdempotencyKey),
		RuleArtifactHash: i.RuleArtifactHash,
		IntentSpecHash:   i.IntentSpecHash,
		TrajectoryHash:   th,
	})
	if err != nil {
		return partial(), err
	}
	return Result{
		Terminal:       lifecycle.Achieved,
		Reason:         "",
		Events:         log.Events(),
		TrajectoryHash: th,
		AchievedSeq:    rec.GlobalSeq,
	}, nil
}
