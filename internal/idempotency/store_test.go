package idempotency

import "testing"

import "github.com/pazooki/treasury-intent-controller/internal/intent"

func TestReserveFreshKey(t *testing.T) {
	s := NewStore()
	if ok := s.Reserve("intent-1", intent.IdempotencyKey("key-a")); !ok {
		t.Fatalf("fresh key: got ok=false, want ok=true")
	}
}

func TestReserveCollision(t *testing.T) {
	s := NewStore()
	if ok := s.Reserve("intent-1", intent.IdempotencyKey("key-a")); !ok {
		t.Fatalf("first reserve: got ok=false, want ok=true")
	}
	// Same key, different intent ID => collision, regardless of intent.
	if ok := s.Reserve("intent-2", intent.IdempotencyKey("key-a")); ok {
		t.Fatalf("collision (different intent, same key): got ok=true, want ok=false")
	}
	// Same key, same intent ID => still a collision (at-most-once on the key).
	if ok := s.Reserve("intent-1", intent.IdempotencyKey("key-a")); ok {
		t.Fatalf("collision (same intent, same key): got ok=true, want ok=false")
	}
}

func TestReserveEmptyKey(t *testing.T) {
	s := NewStore()
	if ok := s.Reserve("intent-1", intent.IdempotencyKey("")); ok {
		t.Fatalf("empty key: got ok=true, want ok=false")
	}
	// Refusing an empty key must not reserve anything: a distinct real key still works.
	if ok := s.Reserve("intent-1", intent.IdempotencyKey("key-a")); !ok {
		t.Fatalf("real key after empty-key refusal: got ok=false, want ok=true")
	}
}

func TestReservationPersistsAcrossCalls(t *testing.T) {
	s := NewStore()
	keys := []intent.IdempotencyKey{"k1", "k2", "k3"}
	for _, k := range keys {
		if ok := s.Reserve("intent-x", k); !ok {
			t.Fatalf("first reserve of %q: got ok=false, want ok=true", k)
		}
	}
	// Every previously reserved key must still collide on a later call,
	// proving reservations persist in the store across Reserve calls.
	for _, k := range keys {
		if ok := s.Reserve("intent-y", k); ok {
			t.Fatalf("re-reserve of %q: got ok=true, want ok=false (reservation should persist)", k)
		}
	}
}

func TestDistinctKeysAllReserve(t *testing.T) {
	s := NewStore()
	if ok := s.Reserve("i", intent.IdempotencyKey("alpha")); !ok {
		t.Fatalf("alpha: got ok=false, want ok=true")
	}
	if ok := s.Reserve("i", intent.IdempotencyKey("beta")); !ok {
		t.Fatalf("beta (distinct key): got ok=false, want ok=true")
	}
}
