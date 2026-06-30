// Package adapter records a settlement event when an intent reaches ACHIEVED.
//
// The reference adapter is deterministic and idempotent on the declared key: a
// second OnAchieved with the same key returns the SAME event and records no
// duplicate. Nothing here uses the wallclock or randomness.
package adapter

import (
	"crypto/sha256"
	"encoding/hex"

	"github.com/pazooki/treasury-intent-controller/internal/intent"
)

// SettlementEvent is what the adapter records on ACHIEVED. Deterministic from the
// intent + key + seed.
type SettlementEvent struct {
	IntentID string
	Key      intent.IdempotencyKey
	Payload  string // deterministic, derived from intent (no wallclock, no randomness)
}

// Adapter records a settlement event on ACHIEVED. No network in slice 1.
type Adapter interface {
	OnAchieved(i intent.Intent) (SettlementEvent, error)
}

// ReferenceAdapter is the deterministic, idempotent reference adapter. It is
// idempotent on the declared key: a second OnAchieved with the same key returns the
// SAME event and records NO duplicate. Settlement returns the recorded events for
// at-most-once assertions.
type ReferenceAdapter struct {
	events map[intent.IdempotencyKey]SettlementEvent
	order  []intent.IdempotencyKey // insertion order, so Settlement() is deterministic
}

// NewReferenceAdapter returns a fresh reference adapter.
func NewReferenceAdapter() *ReferenceAdapter {
	return &ReferenceAdapter{
		events: make(map[intent.IdempotencyKey]SettlementEvent),
	}
}

// OnAchieved records a settlement event for the intent's key. It is idempotent on
// the key: the first call records and returns the event; any later call with the
// same key returns the previously recorded event and records no duplicate.
func (a *ReferenceAdapter) OnAchieved(i intent.Intent) (SettlementEvent, error) {
	if existing, ok := a.events[i.IdempotencyKey]; ok {
		return existing, nil
	}
	ev := SettlementEvent{
		IntentID: i.ID(),
		Key:      i.IdempotencyKey,
		Payload:  payload(i),
	}
	a.events[i.IdempotencyKey] = ev
	a.order = append(a.order, i.IdempotencyKey)
	return ev, nil
}

// Settlement returns all distinct settlement events recorded so far (one per key),
// in the order their keys were first recorded.
func (a *ReferenceAdapter) Settlement() []SettlementEvent {
	out := make([]SettlementEvent, 0, len(a.order))
	for _, k := range a.order {
		out = append(out, a.events[k])
	}
	return out
}

// payload derives a deterministic settlement payload from the intent. It depends
// only on stable intent fields (ID, key, spec hashes) -- never on the wallclock or
// randomness -- so the same intent always yields byte-identical bytes.
func payload(i intent.Intent) string {
	h := sha256.New()
	// Length-prefixed, fixed field order so distinct inputs cannot alias.
	writeField(h, i.ID())
	writeField(h, string(i.IdempotencyKey))
	writeField(h, i.Spec.ActionClass)
	writeField(h, i.Spec.IdempotencyScope)
	writeField(h, i.RuleArtifactHash)
	writeField(h, i.IntentSpecHash)
	return hex.EncodeToString(h.Sum(nil))
}

func writeField(h interface{ Write([]byte) (int, error) }, s string) {
	var lenBuf [8]byte
	n := uint64(len(s))
	for j := 0; j < 8; j++ {
		lenBuf[j] = byte(n >> (8 * j))
	}
	_, _ = h.Write(lenBuf[:])
	_, _ = h.Write([]byte(s))
}
