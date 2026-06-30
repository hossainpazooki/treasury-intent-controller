// Package idempotency tracks reserved idempotency keys at the dispatch edge.
//
// Reserve is the dispatch-edge gate that enforces at-most-once: a key may be
// claimed exactly once. The store is deterministic and holds no wallclock or
// randomness; reservation order does not affect the outcome of any single key.
package idempotency

import "github.com/pazooki/treasury-intent-controller/internal/intent"

// Store tracks reserved idempotency keys. Reserve is the dispatch-edge gate.
type Store struct {
	reserved map[intent.IdempotencyKey]string
}

// NewStore returns a fresh, empty store.
func NewStore() *Store {
	return &Store{reserved: make(map[intent.IdempotencyKey]string)}
}

// Reserve attempts to claim key for the given intent ID. It returns ok=true on a
// fresh key (now reserved), and ok=false on collision (key already reserved, by any
// intent). Empty key => ok=false (absent key is unevaluable).
func (s *Store) Reserve(id string, key intent.IdempotencyKey) (ok bool) {
	if key == "" {
		return false
	}
	if _, exists := s.reserved[key]; exists {
		return false
	}
	s.reserved[key] = id
	return true
}
