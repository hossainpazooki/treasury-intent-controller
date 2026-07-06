// Package durable is the durable, append-only event feed (CONTRACT-V2 §V2.1).
//
// One physical file <dir>/events.jsonl mirrors every gate event for every
// intent, carrying a global monotonic GlobalSeq alongside the preserved
// per-intent Seq. GlobalSeq starts at 1 and never enters the per-intent
// TrajectoryHash.
//
// Encoding: one JSON object per line, json.Marshal(Record) + '\n'. Every
// Append fsyncs before returning success. Open full-scans the file to recover
// the max GlobalSeq and all prior records; a torn/blank trailing line is
// tolerated and ignored, and recovery never rewrites or re-fsyncs existing
// lines. Because a torn tail is left in place, the first Append after
// recovering one writes a leading '\n' so new records never glue onto it.
package durable

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// Record is one durable, append-only event line (JSONL). Field order below IS the
// on-wire and on-disk order. GlobalSeq (json "seq") is monotonic across ALL
// intents; IntentSeq (json "intent_seq") is the per-intent logical clock, copied
// UNCHANGED from audit.Event.Seq. The four trace fields are populated ONLY on the
// ACHIEVED record (omitted otherwise). "seq" is always >=1; "intent_seq" may be 0
// (the DECLARED event), so it is NOT omitempty.
type Record struct {
	GlobalSeq        int    `json:"seq"`
	IntentSeq        int    `json:"intent_seq"`
	IntentID         string `json:"intent_id"`
	Type             string `json:"type"`
	Detail           string `json:"detail,omitempty"`
	IdempotencyKey   string `json:"idempotency_key,omitempty"`    // ACHIEVED only
	RuleArtifactHash string `json:"rule_artifact_hash,omitempty"` // ACHIEVED only
	IntentSpecHash   string `json:"intent_spec_hash,omitempty"`   // ACHIEVED only
	TrajectoryHash   string `json:"trajectory_hash,omitempty"`    // ACHIEVED only
}

// Store is the durable, append-only JSONL feed. Single-process, mutex-guarded
// single writer; reads take the same lock. GlobalSeq is persisted and recovered
// by full-scan on Open (recovered max + 1, then monotonic; first ever = 1).
type Store struct {
	mu        sync.Mutex
	f         *os.File
	globalSeq int
	records   []Record
	// needNewline is true when the recovered file ends in a torn line (no
	// trailing '\n'); the next Append writes a leading '\n' so the new record
	// starts on its own line. Recovery itself never writes.
	needNewline bool
}

// Open opens (creating dir and file if absent) <dir>/events.jsonl, full-scans it
// to recover the max GlobalSeq and all prior records, and returns a Store ready
// to append. The file is opened O_APPEND|O_CREATE|O_RDWR. A trailing
// partial/blank line is ignored (torn last write); everything before it is
// authoritative. Recovery does not re-fsync or rewrite existing lines.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(dir, "events.jsonl"), os.O_APPEND|os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}

	s := &Store{f: f}
	br := bufio.NewReader(f)
	var lastByte byte
	sawContent := false
	for {
		line, rerr := br.ReadBytes('\n')
		if len(line) > 0 {
			sawContent = true
			lastByte = line[len(line)-1]
			trimmed := bytes.TrimRight(line, "\r\n")
			if len(trimmed) > 0 {
				var rec Record
				if uerr := json.Unmarshal(trimmed, &rec); uerr == nil {
					s.records = append(s.records, rec)
					if rec.GlobalSeq > s.globalSeq {
						s.globalSeq = rec.GlobalSeq
					}
				}
				// An unparseable line is a torn write: tolerated and
				// ignored, never rewritten.
			}
		}
		if rerr != nil {
			if rerr == io.EOF {
				break
			}
			f.Close()
			return nil, rerr
		}
	}
	s.needNewline = sawContent && lastByte != '\n'
	return s, nil
}

// Append writes ONE record, assigning the next GlobalSeq (recovered max + 1,
// then monotonic; first ever = 1), fsyncs the file, appends to the in-memory
// index, and returns the stored record with GlobalSeq set. Any caller-set
// GlobalSeq is ignored and overwritten.
func (s *Store) Append(r Record) (Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r.GlobalSeq = s.globalSeq + 1
	b, err := json.Marshal(r)
	if err != nil {
		return Record{}, err
	}
	buf := make([]byte, 0, len(b)+2)
	if s.needNewline {
		buf = append(buf, '\n')
	}
	buf = append(buf, b...)
	buf = append(buf, '\n')
	if _, err := s.f.Write(buf); err != nil {
		return Record{}, err
	}
	if err := s.f.Sync(); err != nil {
		return Record{}, err
	}
	s.needNewline = false
	s.globalSeq = r.GlobalSeq
	s.records = append(s.records, r)
	return r, nil
}

// Since returns all records with GlobalSeq > sinceGlobalSeq, in ascending
// GlobalSeq order. If typ != "", only records whose Type == typ are returned.
// The returned slice is a fresh copy.
func (s *Store) Since(sinceGlobalSeq int, typ string) []Record {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Record, 0)
	for _, r := range s.records {
		if r.GlobalSeq <= sinceGlobalSeq {
			continue
		}
		if typ != "" && r.Type != typ {
			continue
		}
		out = append(out, r)
	}
	return out
}

// ByIntent returns all records for intentID, in ascending IntentSeq order (the
// per-intent event log). Fresh copy.
func (s *Store) ByIntent(intentID string) []Record {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Record, 0)
	for _, r := range s.records {
		if r.IntentID == intentID {
			out = append(out, r)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].IntentSeq < out[j].IntentSeq })
	return out
}

// Close closes the underlying file.
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
