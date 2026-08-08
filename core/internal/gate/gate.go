// Package gate drives the full intent authorization lifecycle deterministically.
//
// The gate is the sole emitter of the ACHIEVED event and the single orchestrator
// of the DECLARED -> RESOLVING -> ACTIVE -> VERIFYING -> {ACHIEVED | FAILED |
// FAILED_AT_DISPATCH} lifecycle. It is emit-and-observe (CONTRACT.md §4): it
// mirrors every event to the durable feed, stops at appending the single
// ACHIEVED record, and never settles in-process. A downstream consumer settles
// from the feed.
package gate

import (
	"context"
	"fmt"
	"strings"

	"github.com/hossainpazooki/intent-plane/core/internal/audit"
	"github.com/hossainpazooki/intent-plane/core/internal/durable"
	"github.com/hossainpazooki/intent-plane/core/internal/idempotency"
	"github.com/hossainpazooki/intent-plane/core/internal/intent"
	"github.com/hossainpazooki/intent-plane/core/internal/lifecycle"
	"github.com/hossainpazooki/intent-plane/core/internal/scoring"
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

// RevocationChecker answers whether a spec hash has a VERIFIED revocation
// tombstone. The gate consults it at declaration and re-consults it at the
// dispatch edge — the same last-moment re-verification discipline as volatile
// criteria, applied to authority itself.
type RevocationChecker interface {
	RevokedRef(hash string) (ref string, revoked bool)
}

// Gate authorizes intents against the scorer, the durable feed, and the
// idempotency store. It holds NO settlement dependency (emit-and-observe) and
// NO signing keys: it verifies (via the resolver upstream) and decides.
type Gate struct {
	scorer      scoring.Scorer
	feed        *durable.Store
	store       *idempotency.Store
	revocations RevocationChecker // nil = no revocation signal reaches this gate
	scorerID    string            // witness stamped on SCORED/RECHECK feed records
}

// Option configures a Gate at construction.
type Option func(*Gate)

// WithRevocations wires the revocation signal (typically the plane spec
// store) into the gate.
func WithRevocations(r RevocationChecker) Option {
	return func(g *Gate) { g.revocations = r }
}

// WithScorerID sets the scorer-identity witness stamped on SCORED/RECHECK
// feed records: the feed-level answer to "which scoring authority produced
// this score" (a forced grant and a live-scored grant must not be
// byte-indistinguishable in the feed).
func WithScorerID(id string) Option {
	return func(g *Gate) { g.scorerID = id }
}

// New constructs a Gate over the scorer, the (shared, durable) feed, and the
// (shared, durable) idempotency store.
func New(s scoring.Scorer, feed *durable.Store, store *idempotency.Store, opts ...Option) *Gate {
	g := &Gate{scorer: s, feed: feed, store: store}
	for _, o := range opts {
		o(g)
	}
	return g
}

// revokedRef consults the revocation signal; a nil checker means no signal
// reaches the gate (and the docs must say so — signal absence is not
// non-revocation being proven).
func (g *Gate) revokedRef(hash string) (string, bool) {
	if g.revocations == nil {
		return "", false
	}
	return g.revocations.RevokedRef(hash)
}

// Authorize drives the full lifecycle deterministically (CONTRACT.md §4.2, the
// gate algorithm). It mirrors EVERY event to the durable
// feed as it appends to the in-memory per-intent log, preserving the per-intent
// Seq and TrajectoryHash exactly as in slice 1.
//
//  1. DECLARED. Empty idempotency key -> append UNEVALUABLE, FAILED with reason
//     "unevaluable:absent-key". Return.
//     1b. Thin-spec defense (CONTRACT.md §4.2 step 1b): zero criteria -> append
//     UNEVALUABLE "empty-criteria:<intent_spec_hash>", FAILED
//     "unevaluable:empty-criteria". A criterion whose volatility is neither
//     "stable" nor "volatile" -> append UNEVALUABLE
//     "invalid-volatility:<name>:<raw>", FAILED
//     "unevaluable:invalid-volatility:<name>". Both refuse before any scoring.
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
		rec := durable.Record{
			IntentID:  id,
			IntentSeq: e.Seq,
			Type:      e.Type,
			Detail:    e.Detail,
		}
		// Scorer-identity witness on scoring events only. Feed-level: never
		// enters the in-memory log or the TrajectoryHash.
		if typ == "SCORED" || typ == "RECHECK" {
			rec.ScorerID = g.scorerID
		}
		_, err := g.feed.Append(rec)
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

	// Step 1a2: revocation-at-resolution defense. The resolver may have found
	// a verified tombstone and refused before producing any payload: that
	// arrives as Attested:false + RevokedRef, and the revocation must win the
	// cause — `revoked:<ref>` names the tombstone; collapsing it into
	// unattested-spec would erase a fact the feed exists to witness. (Ordering
	// bug found by end-to-end smoke; pinned by
	// TestRevokedResolutionWinsOverUnattested.)
	if ref := i.Resolution.RevokedRef; ref != "" {
		if err := emit("REVOKED", ref); err != nil {
			return partial(), err
		}
		return terminal(lifecycle.Failed, "revoked:"+ref), nil
	}

	// Step 1a3: attestation defense (CONTRACT.md §4.2 step 1a3). An intent
	// whose spec was not resolved from a VERIFIED envelope (store or pinned
	// wire) refuses before any scoring: no verified spec is unevaluable-shaped
	// absence, and unevaluable never passes. This is P1's fail-closed floor —
	// criteria that did not arrive through signature verification and
	// content-address equality never reach the scorer.
	if !i.Resolution.Attested {
		if err := emit("UNEVALUABLE", "unattested-spec:"+i.IntentSpecHash); err != nil {
			return partial(), err
		}
		return terminal(lifecycle.Failed, "unevaluable:unattested-spec"), nil
	}
	// Step 1a3b: revocation at declaration via the live checker (the resolver
	// path is step 1a2 above; this consults the signal directly).
	if ref, ok := g.revokedRef(i.IntentSpecHash); ok {
		if err := emit("REVOKED", ref); err != nil {
			return partial(), err
		}
		return terminal(lifecycle.Failed, "revoked:"+ref), nil
	}
	// Step 1a4: posture defense. Enforcement posture comes from inside the
	// attested payload; an unknown posture is unevaluable-shaped absence and
	// refuses — the zero value never silently becomes "enforce" (a posture
	// default would be a config toggle wearing a trench coat).
	if i.Spec.Posture != intent.PostureEnforce && i.Spec.Posture != intent.PostureShadow {
		if err := emit("UNEVALUABLE", "invalid-posture:"+string(i.Spec.Posture)); err != nil {
			return partial(), err
		}
		return terminal(lifecycle.Failed, "unevaluable:invalid-posture"), nil
	}

	// Step 1a5: human-judgment defense. A spec still carrying a
	// deliberately-unquantified obligation refuses before any scoring: the
	// check was routed to a human and no human resolved it. This abstention
	// is a SUCCESS state of the plane (P6), not a coverage gap.
	if len(i.Spec.HumanJudgment) > 0 {
		name := i.Spec.HumanJudgment[0]
		if err := emit("UNEVALUABLE", "human-judgment:"+name); err != nil {
			return partial(), err
		}
		return terminal(lifecycle.Failed, "unevaluable:human-judgment:"+name), nil
	}

	// Step 1b: thin-spec defense (CONTRACT.md §4.2 step 1b). Spec resolution
	// today happens at declaration decode, so the spec-shape checks live here
	// and move with resolution if resolution ever moves. A spec with zero
	// criteria is refused: "no criterion failed" must never be satisfied by
	// "no criterion existed" (the scorer-side twin of this guard is the
	// hashless-verify refusal in core/scorer/src/scorer/resolver.py). The UNEVALUABLE
	// detail binds the claimed spec hash so the refusal record witnesses WHICH
	// signed spec was thin; a blank hash yields the bare "empty-criteria:",
	// witnessing that none was claimed.
	if len(i.Spec.Criteria) == 0 {
		if err := emit("UNEVALUABLE", "empty-criteria:"+i.IntentSpecHash); err != nil {
			return partial(), err
		}
		return terminal(lifecycle.Failed, "unevaluable:empty-criteria"), nil
	}
	// An unknown volatility is an unknown kind and denies explicitly: a typo'd
	// "volatile" must not silently become stable and skip the dispatch-edge
	// re-verify. This closes the typo case only — a criterion semantically
	// mislabeled stable is authoring/attestation territory the string cannot
	// reveal.
	for _, c := range i.Spec.Criteria {
		if c.Volatility != intent.Stable && c.Volatility != intent.Volatile {
			if err := emit("UNEVALUABLE", "invalid-volatility:"+c.Name+":"+string(c.Volatility)); err != nil {
				return partial(), err
			}
			return terminal(lifecycle.Failed, "unevaluable:invalid-volatility:"+c.Name), nil
		}
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
		case scoring.Pass:
			// Scored Pass — nothing to collect.
		case scoring.Fail:
			failed = append(failed, c.Name)
		default:
			// Unevaluable AND any out-of-domain Score value fail closed alike:
			// the scoring domain is closed tri-state, so an unknown score is
			// unevaluable-shaped absence. The dispatch edge already refuses any
			// non-Pass by exact match; this mirrors that semantics at
			// declaration (before it, an out-of-domain value fell through as an
			// implicit pass — "SCORED x:UNEVALUABLE" followed by ACHIEVED).
			reason := "unevaluable:" + c.Name
			if err := emit("UNEVALUABLE", c.Name); err != nil {
				return partial(), err
			}
			if err := transition(lifecycle.Failed, reason); err != nil {
				return partial(), err
			}
			return terminal(lifecycle.Failed, reason), nil
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

	// Step 4a2: revocation re-check at the dispatch edge. The pinned spec may
	// have been revoked between verification and dispatch; the same authority
	// signal is consulted at the last moment before the consequence fires.
	// This ACTIVATES the reserved `revoked:<ref>` cause class (CONTRACT.md
	// §3.3). The key is NOT reserved on this path: re-declaring after a fresh
	// attestation is legitimate.
	if ref, ok := g.revokedRef(i.IntentSpecHash); ok {
		reason := "revoked:" + ref
		if err := emit("REVOKED", ref); err != nil {
			return partial(), err
		}
		if err := transition(lifecycle.FailedAtDispatch, reason); err != nil {
			return partial(), err
		}
		return terminal(lifecycle.FailedAtDispatch, reason), nil
	}

	// Step 4a3: shadow posture. The intent was fully scored (declaration AND
	// dispatch-edge recheck) and would have authorized — record exactly that,
	// durably, and authorize NOTHING: no key reservation, no ACHIEVED, the
	// consequence never fires. Promotion to enforce is a NEW attestation with
	// a NEW hash (ADR-0006, Proposed): enforcement posture is an authority
	// decision, not a config toggle.
	if i.Spec.Posture == intent.PostureShadow {
		if !lifecycle.IsValidTransition(state, lifecycle.ShadowRecorded) {
			return partial(), fmt.Errorf("gate: invalid lifecycle transition %s -> %s", state, lifecycle.ShadowRecorded)
		}
		e := log.Append("SHADOW_RECORDED", id)
		th := log.TrajectoryHash()
		if _, err := g.feed.Append(durable.Record{
			IntentID:         id,
			IntentSeq:        e.Seq,
			Type:             e.Type,
			Detail:           e.Detail,
			IdempotencyKey:   string(i.IdempotencyKey),
			RuleArtifactHash: i.RuleArtifactHash,
			IntentSpecHash:   i.IntentSpecHash,
			TrajectoryHash:   th,
		}); err != nil {
			return partial(), err
		}
		return Result{
			Terminal:       lifecycle.ShadowRecorded,
			Reason:         "",
			Events:         log.Events(),
			TrajectoryHash: th,
		}, nil
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
