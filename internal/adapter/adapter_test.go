package adapter

import (
	"testing"

	"github.com/pazooki/treasury-intent-controller/internal/intent"
)

func mkIntent(seed, key string) intent.Intent {
	return intent.Intent{
		EpisodeSeed:    seed,
		IdempotencyKey: intent.IdempotencyKey(key),
		Spec: intent.IntentSpecParams{
			ActionClass:      "payment",
			IdempotencyScope: "payer",
			Criteria: []intent.Criterion{
				{Name: "balance", Threshold: 100, Volatility: intent.Volatile},
			},
		},
		RuleArtifactHash: "rule-abc",
		IntentSpecHash:   "spec-abc",
	}
}

// Two OnAchieved calls with the same key record exactly one settlement event and
// return identical events (same bytes).
func TestOnAchievedIdempotentSameKey(t *testing.T) {
	a := NewReferenceAdapter()
	i := mkIntent("seed-1", "key-1")

	first, err := a.OnAchieved(i)
	if err != nil {
		t.Fatalf("first OnAchieved: %v", err)
	}
	second, err := a.OnAchieved(i)
	if err != nil {
		t.Fatalf("second OnAchieved: %v", err)
	}

	if first != second {
		t.Fatalf("idempotent call returned a different event:\n first=%#v\nsecond=%#v", first, second)
	}
	if got := a.Settlement(); len(got) != 1 {
		t.Fatalf("expected exactly one settlement event, got %d: %#v", len(got), got)
	}
}

// A second call with the same key but a DIFFERENT intent still returns the
// originally recorded event and records no duplicate (at-most-once on the key).
func TestOnAchievedSameKeyDifferentIntent(t *testing.T) {
	a := NewReferenceAdapter()
	first, _ := a.OnAchieved(mkIntent("seed-a", "shared-key"))
	second, _ := a.OnAchieved(mkIntent("seed-b", "shared-key"))

	if first != second {
		t.Fatalf("same key must return the first recorded event:\n first=%#v\nsecond=%#v", first, second)
	}
	if got := a.Settlement(); len(got) != 1 {
		t.Fatalf("expected one settlement event for the key, got %d", len(got))
	}
}

// Different keys produce two distinct settlement events.
func TestOnAchievedDistinctKeys(t *testing.T) {
	a := NewReferenceAdapter()
	e1, _ := a.OnAchieved(mkIntent("seed-1", "key-1"))
	e2, _ := a.OnAchieved(mkIntent("seed-2", "key-2"))

	if e1.Key == e2.Key {
		t.Fatalf("expected distinct keys, both were %q", e1.Key)
	}
	got := a.Settlement()
	if len(got) != 2 {
		t.Fatalf("expected two settlement events, got %d: %#v", len(got), got)
	}
	if got[0].Key != "key-1" || got[1].Key != "key-2" {
		t.Fatalf("settlement order not deterministic by insertion: %#v", got)
	}
}

// Determinism: the same intent yields the same Payload across independent adapters.
func TestPayloadDeterministic(t *testing.T) {
	i := mkIntent("seed-det", "key-det")

	a1 := NewReferenceAdapter()
	a2 := NewReferenceAdapter()
	e1, _ := a1.OnAchieved(i)
	e2, _ := a2.OnAchieved(i)

	if e1.Payload != e2.Payload {
		t.Fatalf("payload not deterministic:\n a1=%q\n a2=%q", e1.Payload, e2.Payload)
	}
	if e1 != e2 {
		t.Fatalf("event not deterministic:\n a1=%#v\n a2=%#v", e1, e2)
	}
	if e1.Payload == "" {
		t.Fatal("payload must be non-empty")
	}
}

// Distinct intents yield distinct payloads (the payload actually binds the intent).
func TestPayloadDistinctIntents(t *testing.T) {
	a := NewReferenceAdapter()
	e1, _ := a.OnAchieved(mkIntent("seed-x", "key-x"))
	e2, _ := a.OnAchieved(mkIntent("seed-y", "key-y"))
	if e1.Payload == e2.Payload {
		t.Fatalf("distinct intents produced identical payloads: %q", e1.Payload)
	}
}
