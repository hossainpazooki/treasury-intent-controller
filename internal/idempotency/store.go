// Package idempotency tracks reserved idempotency keys at the dispatch edge.
//
// Reserve is the dispatch-edge gate that enforces at-most-once: a key may be
// claimed exactly once. The store is deterministic and holds no wallclock or
// randomness; reservation order does not affect the outcome of any single key.
//
// Under CONTRACT-DURABILITY §V2.2 the store is mutex-guarded (it is shared across
// concurrent requests at boot time) and gains a durable constructor, OpenStore.
// NewStore remains the in-memory store used by unit tests; both satisfy the same
// Reserve contract.
package idempotency

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"github.com/pazooki/treasury-intent-controller/internal/intent"
)

// reservation is one durable JSONL line: {"key":..,"id":..}.
type reservation struct {
	Key string `json:"key"`
	ID  string `json:"id"`
}

// Store tracks reserved idempotency keys. Reserve is the dispatch-edge gate.
// A store from NewStore is in-memory only (f == nil); a store from OpenStore is
// durable. Both satisfy the identical Reserve contract.
type Store struct {
	mu       sync.Mutex
	reserved map[intent.IdempotencyKey]string
	f        *os.File // nil for in-memory stores
	needsNL  bool     // file ends in a torn (non-\n-terminated) line; prepend \n on next append
}

// NewStore returns a fresh, empty in-memory store (no file IO).
func NewStore() *Store {
	return &Store{reserved: make(map[intent.IdempotencyKey]string)}
}

// OpenStore opens (creating dir/file if absent) a durable, file-backed
// idempotency store at <dir>/idempotency.jsonl, full-scans it to recover ALL
// previously reserved keys, and returns a Store whose successful Reserve appends
// {"key":..,"id":..} and fsyncs BEFORE returning ok=true. Reserve semantics are
// unchanged (fresh ok; collision refused; empty refused). Reservations survive
// process restart.
func OpenStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "idempotency.jsonl")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	s := &Store{reserved: make(map[intent.IdempotencyKey]string), f: f}
	if err := s.recover(); err != nil {
		f.Close()
		return nil, err
	}
	return s, nil
}

// recover full-scans the file start-to-end and loads every reserved key.
// A trailing partial/blank line (torn last write) is ignored; everything before
// it is authoritative. Recovery never rewrites or re-fsyncs existing lines.
func (s *Store) recover() error {
	if _, err := s.f.Seek(0, 0); err != nil {
		return err
	}
	sc := bufio.NewScanner(s.f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var r reservation
		if err := json.Unmarshal(line, &r); err != nil {
			// Torn/partial line: ignore it (and anything after would be
			// unreachable garbage from the same torn write).
			continue
		}
		if r.Key == "" {
			continue // an empty key is never a valid reservation
		}
		s.reserved[intent.IdempotencyKey(r.Key)] = r.ID
	}
	if err := sc.Err(); err != nil {
		return err
	}
	// If the file ends in a torn (non-newline-terminated) write, the next append
	// must start on a fresh line or it would fuse with the garbage fragment and
	// be lost to the NEXT recovery. Recovery itself never rewrites bytes.
	st, err := s.f.Stat()
	if err != nil {
		return err
	}
	if size := st.Size(); size > 0 {
		last := make([]byte, 1)
		if _, err := s.f.ReadAt(last, size-1); err != nil {
			return err
		}
		s.needsNL = last[0] != '\n'
	}
	return nil
}

// Close closes the underlying file of a durable store. On an in-memory store it
// is a no-op.
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.f == nil {
		return nil
	}
	err := s.f.Close()
	s.f = nil
	return err
}

// Reserve attempts to claim key for the given intent ID. It returns ok=true on a
// fresh key (now reserved), and ok=false on collision (key already reserved, by any
// intent). Empty key => ok=false (absent key is unevaluable). Reserve is
// mutex-guarded: the boot-time store is shared across concurrent requests.
//
// On a durable store, a successful Reserve appends {"key":..,"id":..} and
// fsyncs BEFORE returning ok=true. A collision or empty key never writes to
// disk. A durable write/fsync failure is fail-closed: the key is NOT reserved
// and Reserve returns ok=false.
func (s *Store) Reserve(id string, key intent.IdempotencyKey) (ok bool) {
	if key == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.reserved[key]; exists {
		return false
	}
	if s.f != nil {
		line, err := json.Marshal(reservation{Key: string(key), ID: id})
		if err != nil {
			return false
		}
		buf := make([]byte, 0, len(line)+2)
		if s.needsNL {
			buf = append(buf, '\n')
		}
		buf = append(buf, line...)
		buf = append(buf, '\n')
		if _, err := s.f.Write(buf); err != nil {
			return false
		}
		if err := s.f.Sync(); err != nil {
			return false
		}
		s.needsNL = false
	}
	s.reserved[key] = id
	return true
}
