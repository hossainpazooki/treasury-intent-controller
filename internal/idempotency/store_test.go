package idempotency

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

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

// --- CONTRACT-DURABILITY §V2.2 durable-store tests (OpenStore) ---

// storeFile returns the durable store's on-disk path for dir.
func storeFile(dir string) string {
	return filepath.Join(dir, "idempotency.jsonl")
}

// readStoreFile reads the durable file's raw bytes; a missing file is an error.
func readStoreFile(t *testing.T, dir string) []byte {
	t.Helper()
	b, err := os.ReadFile(storeFile(dir))
	if err != nil {
		t.Fatalf("read %s: %v", storeFile(dir), err)
	}
	return b
}

// mustOpen opens a durable store in dir or fails the test.
func mustOpen(t *testing.T, dir string) *Store {
	t.Helper()
	s, err := OpenStore(dir)
	if err != nil {
		t.Fatalf("OpenStore(%q): %v", dir, err)
	}
	return s
}

// TestOpenStoreRestartRecovery: a reservation made before Close must survive a
// process "restart" (OpenStore over the same dir) — the same key collides after
// reopen, and a genuinely fresh key still reserves (§V2.9 claim 3 probe).
func TestOpenStoreRestartRecovery(t *testing.T) {
	dir := t.TempDir()

	s1 := mustOpen(t, dir)
	if ok := s1.Reserve("intent-1", intent.IdempotencyKey("key-a")); !ok {
		t.Fatalf("first durable reserve: got ok=false, want ok=true")
	}
	if err := s1.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	s2 := mustOpen(t, dir)
	defer s2.Close()
	if ok := s2.Reserve("intent-2", intent.IdempotencyKey("key-a")); ok {
		t.Fatalf("after restart, same key: got ok=true, want ok=false (reservation must survive restart)")
	}
	if ok := s2.Reserve("intent-2", intent.IdempotencyKey("key-b")); !ok {
		t.Fatalf("after restart, fresh key: got ok=false, want ok=true")
	}
}

// TestOpenStoreRestartRecoveryMultipleKeys: every key reserved across the life
// of the first store is recovered, not just the last line.
func TestOpenStoreRestartRecoveryMultipleKeys(t *testing.T) {
	dir := t.TempDir()
	keys := []intent.IdempotencyKey{"k1", "k2", "k3", "k4"}

	s1 := mustOpen(t, dir)
	for _, k := range keys {
		if ok := s1.Reserve("intent-x", k); !ok {
			t.Fatalf("reserve %q: got ok=false, want ok=true", k)
		}
	}
	if err := s1.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	s2 := mustOpen(t, dir)
	defer s2.Close()
	for _, k := range keys {
		if ok := s2.Reserve("intent-y", k); ok {
			t.Fatalf("after restart, key %q: got ok=true, want ok=false", k)
		}
	}
}

// TestOpenStoreEmptyKeyNoWrite: an empty-key Reserve on a durable store must
// leave the file byte-identical (empty key never writes).
func TestOpenStoreEmptyKeyNoWrite(t *testing.T) {
	dir := t.TempDir()
	s := mustOpen(t, dir)
	defer s.Close()

	if ok := s.Reserve("intent-1", intent.IdempotencyKey("key-a")); !ok {
		t.Fatalf("setup reserve: got ok=false, want ok=true")
	}
	before := readStoreFile(t, dir)

	if ok := s.Reserve("intent-1", intent.IdempotencyKey("")); ok {
		t.Fatalf("empty key on durable store: got ok=true, want ok=false")
	}
	after := readStoreFile(t, dir)
	if string(before) != string(after) {
		t.Fatalf("empty-key Reserve wrote to disk:\nbefore: %q\nafter:  %q", before, after)
	}
}

// TestOpenStoreCollisionNoWrite: a colliding Reserve on a durable store must
// leave the file byte-identical (a collision never writes to disk).
func TestOpenStoreCollisionNoWrite(t *testing.T) {
	dir := t.TempDir()
	s := mustOpen(t, dir)
	defer s.Close()

	if ok := s.Reserve("intent-1", intent.IdempotencyKey("key-a")); !ok {
		t.Fatalf("first reserve: got ok=false, want ok=true")
	}
	before := readStoreFile(t, dir)

	if ok := s.Reserve("intent-2", intent.IdempotencyKey("key-a")); ok {
		t.Fatalf("collision on durable store: got ok=true, want ok=false")
	}
	after := readStoreFile(t, dir)
	if string(before) != string(after) {
		t.Fatalf("colliding Reserve wrote to disk:\nbefore: %q\nafter:  %q", before, after)
	}
}

// concurrentReserve races N goroutines on the same fresh key and returns the
// number that observed ok=true.
func concurrentReserve(t *testing.T, s *Store, key intent.IdempotencyKey, n int) int {
	t.Helper()
	var (
		start sync.WaitGroup
		done  sync.WaitGroup
		mu    sync.Mutex
		wins  int
	)
	start.Add(1)
	for g := 0; g < n; g++ {
		done.Add(1)
		id := g
		go func() {
			defer done.Done()
			start.Wait()
			if s.Reserve("intent-"+string(rune('a'+id%26)), key) {
				mu.Lock()
				wins++
				mu.Unlock()
			}
		}()
	}
	start.Done()
	done.Wait()
	return wins
}

// TestConcurrentReserveInMemory: N goroutines racing Reserve on the same fresh
// key against an in-memory store yield exactly one ok=true.
func TestConcurrentReserveInMemory(t *testing.T) {
	s := NewStore()
	const n = 32
	if wins := concurrentReserve(t, s, intent.IdempotencyKey("race-key"), n); wins != 1 {
		t.Fatalf("concurrent Reserve (in-memory, %d goroutines): got %d ok=true, want exactly 1", n, wins)
	}
}

// TestConcurrentReserveDurable: the same race against a durable store also
// yields exactly one ok=true, and exactly one line lands on disk.
func TestConcurrentReserveDurable(t *testing.T) {
	dir := t.TempDir()
	s := mustOpen(t, dir)
	defer s.Close()

	const n = 32
	if wins := concurrentReserve(t, s, intent.IdempotencyKey("race-key"), n); wins != 1 {
		t.Fatalf("concurrent Reserve (durable, %d goroutines): got %d ok=true, want exactly 1", n, wins)
	}
	// Exactly one reservation line must have been written.
	b := readStoreFile(t, dir)
	lines := 0
	for _, c := range b {
		if c == '\n' {
			lines++
		}
	}
	if lines != 1 {
		t.Fatalf("durable file after race: got %d lines, want exactly 1 (%q)", lines, b)
	}
}

// TestOpenStoreInMemoryAndDurableSameContract: the two constructors satisfy the
// identical Reserve contract (fresh ok, collision refused, empty refused).
func TestOpenStoreInMemoryAndDurableSameContract(t *testing.T) {
	stores := map[string]*Store{
		"in-memory": NewStore(),
		"durable":   mustOpen(t, t.TempDir()),
	}
	names := []string{"in-memory", "durable"} // fixed order; no map-iteration order
	for _, name := range names {
		s := stores[name]
		if ok := s.Reserve("i", intent.IdempotencyKey("")); ok {
			t.Fatalf("%s: empty key: got ok=true, want ok=false", name)
		}
		if ok := s.Reserve("i", intent.IdempotencyKey("k")); !ok {
			t.Fatalf("%s: fresh key: got ok=false, want ok=true", name)
		}
		if ok := s.Reserve("j", intent.IdempotencyKey("k")); ok {
			t.Fatalf("%s: collision: got ok=true, want ok=false", name)
		}
		if err := s.Close(); err != nil {
			t.Fatalf("%s: Close: %v", name, err)
		}
	}
}

// TestOpenStoreIgnoresTornTrailingLine: a torn (partial, unparseable) last line
// is ignored on recovery; everything before it is authoritative.
func TestOpenStoreIgnoresTornTrailingLine(t *testing.T) {
	dir := t.TempDir()
	s1 := mustOpen(t, dir)
	if ok := s1.Reserve("intent-1", intent.IdempotencyKey("key-a")); !ok {
		t.Fatalf("setup reserve: got ok=false, want ok=true")
	}
	if err := s1.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// Simulate a torn last write: a partial JSON fragment with no newline.
	f, err := os.OpenFile(storeFile(dir), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open for torn append: %v", err)
	}
	if _, err := f.WriteString(`{"key":"key-torn","id":"in`); err != nil {
		t.Fatalf("torn append: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close torn file: %v", err)
	}

	s2 := mustOpen(t, dir)
	if ok := s2.Reserve("intent-2", intent.IdempotencyKey("key-a")); ok {
		t.Fatalf("recovered key after torn line: got ok=true, want ok=false")
	}
	if ok := s2.Reserve("intent-2", intent.IdempotencyKey("key-torn")); !ok {
		t.Fatalf("torn-line key must NOT be recovered: got ok=false, want ok=true")
	}
	if err := s2.Close(); err != nil {
		t.Fatalf("Close s2: %v", err)
	}

	// The reservation made AFTER the torn line must itself survive a further
	// restart (appends must not fuse with the torn fragment).
	s3 := mustOpen(t, dir)
	defer s3.Close()
	if ok := s3.Reserve("intent-3", intent.IdempotencyKey("key-torn")); ok {
		t.Fatalf("post-torn reservation lost across restart: got ok=true, want ok=false")
	}
	if ok := s3.Reserve("intent-3", intent.IdempotencyKey("key-a")); ok {
		t.Fatalf("original reservation lost across restart: got ok=true, want ok=false")
	}
}
