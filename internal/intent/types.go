// Package intent holds the pure-data description of an authorization intent.
// Intent carries no mutable lifecycle state; the gate's runtime owns the state
// machine.
package intent

import (
	"crypto/sha256"
	"encoding/hex"
)

// Volatility marks whether a criterion is scored once (stable) or re-verified at
// the dispatch edge (volatile).
type Volatility string

const (
	Stable   Volatility = "stable"
	Volatile Volatility = "volatile"
)

// Phase identifies which scoring pass a criterion is being scored in.
type Phase string

const (
	Declaration Phase = "declaration" // first scoring pass
	Dispatch    Phase = "dispatch"    // volatile re-verify at the dispatch edge
)

// Criterion is one named condition the intent must satisfy.
type Criterion struct {
	Name       string
	Threshold  float64
	Volatility Volatility
}

// IdempotencyKey is the required at-most-once key for an intent.
type IdempotencyKey string

// IntentSpecParams carries the criteria/thresholds/idempotency the gate consumes
// directly (no artifact reads in this slice).
type IntentSpecParams struct {
	ActionClass      string // "payment" for slice 1
	Criteria         []Criterion
	IdempotencyScope string // e.g. "payer"
}

// Intent is pure data. It carries NO mutable lifecycle state; the gate's runtime
// owns the state machine. The three audit hashes are opaque to this slice.
type Intent struct {
	EpisodeSeed      string // determinism source; the intent ID derives from this
	Spec             IntentSpecParams
	IdempotencyKey   IdempotencyKey // required; "" is invalid
	RuleArtifactHash string         // opaque
	IntentSpecHash   string         // opaque
}

// ID is deterministically derived from EpisodeSeed (stable across runs). It is
// the hex prefix of sha256(EpisodeSeed).
func (i Intent) ID() string {
	sum := sha256.Sum256([]byte(i.EpisodeSeed))
	return hex.EncodeToString(sum[:])[:16]
}
