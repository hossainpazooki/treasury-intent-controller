package audit

import "testing"

// Seq is a logical clock that starts at 0 and increments by exactly 1 per
// Append, both in the returned Event and in the stored log.
func TestSeqMonotonicFromZero(t *testing.T) {
	l := NewEventLog()
	const n = 5
	for i := 0; i < n; i++ {
		got := l.Append("T", "d")
		if got.Seq != i {
			t.Fatalf("Append #%d returned Seq=%d, want %d", i, got.Seq, i)
		}
	}
	evs := l.Events()
	if len(evs) != n {
		t.Fatalf("len(Events)=%d, want %d", len(evs), n)
	}
	for i, e := range evs {
		if e.Seq != i {
			t.Fatalf("Events()[%d].Seq=%d, want %d", i, e.Seq, i)
		}
	}
}

// Events returns a defensive copy: mutating the returned slice or its elements
// must not change the log.
func TestEventsReturnsCopy(t *testing.T) {
	l := NewEventLog()
	l.Append("DECLARED", "x")
	l.Append("ACHIEVED", "y")

	hashBefore := l.TrajectoryHash()

	snap := l.Events()
	// Mutate the caller's copy aggressively.
	snap[0].Type = "TAMPERED"
	snap[0].Detail = "tampered"
	snap[1].Seq = 999

	again := l.Events()
	if again[0].Type != "DECLARED" || again[0].Detail != "x" {
		t.Fatalf("mutation leaked into log: got %+v", again[0])
	}
	if again[1].Seq != 1 {
		t.Fatalf("mutation leaked into log: got Seq=%d, want 1", again[1].Seq)
	}
	if h := l.TrajectoryHash(); h != hashBefore {
		t.Fatalf("hash changed after mutating Events() copy: before=%s after=%s", hashBefore, h)
	}
}

// Same events ⟹ identical hash, byte-for-byte, across independent logs.
func TestTrajectoryHashStable(t *testing.T) {
	build := func() *EventLog {
		l := NewEventLog()
		l.Append("DECLARED", "")
		l.Append("SCORED", "balance:PASS")
		l.Append("ACHIEVED", "")
		return l
	}
	a := build().TrajectoryHash()
	b := build().TrajectoryHash()
	if a != b {
		t.Fatalf("identical event sequences hashed differently: %s vs %s", a, b)
	}
	if a == "" {
		t.Fatal("non-empty log produced empty hash")
	}
	// Empty log is also stable and distinct from a populated one.
	e1 := NewEventLog().TrajectoryHash()
	e2 := NewEventLog().TrajectoryHash()
	if e1 != e2 {
		t.Fatalf("empty logs hashed differently: %s vs %s", e1, e2)
	}
	if e1 == a {
		t.Fatal("empty log collided with populated log hash")
	}
}

// The hash is order-sensitive: reordering the same events changes the hash.
func TestTrajectoryHashOrderSensitive(t *testing.T) {
	l1 := NewEventLog()
	l1.Append("A", "1")
	l1.Append("B", "2")

	l2 := NewEventLog()
	l2.Append("B", "2")
	l2.Append("A", "1")

	if l1.TrajectoryHash() == l2.TrajectoryHash() {
		t.Fatal("reordered event sequences produced the same hash")
	}
}

// Length-prefixed canonical encoding is injection-safe: contents containing the
// field separators ':' and '\n' cannot forge a different event sequence that
// collides.
func TestTrajectoryHashNoDelimiterInjection(t *testing.T) {
	l1 := NewEventLog()
	l1.Append("A", "x")
	l1.Append("B", "y")

	l2 := NewEventLog()
	// Try to smuggle the second event into the first's Detail field.
	l2.Append("A", "x\n1:B\n1:y")

	if l1.TrajectoryHash() == l2.TrajectoryHash() {
		t.Fatal("delimiter injection forged a colliding hash")
	}
}
