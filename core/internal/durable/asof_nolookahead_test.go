package durable

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// no-lookahead applied to the durable feed (rigor skill, non-origin firing).
//
//	move 1 — the as-of key is GlobalSeq: the feed's point-in-time coordinate.
//	move 2 — leak candidates: Since()'s filter, Open()'s recovery renumbering,
//	         and the shared `records` slice handed to readers.
//	move 3 — exercised with RESTATEMENT (close, reopen, append later-arriving
//	         data), not with append-only forward data inside one session.
//
// The invariant: the set of records visible as of GlobalSeq C is IMMUTABLE.
// Data that arrives after C must never become visible at C.

// asOf is the point-in-time read the feed implies but does not expose: every
// record whose as-of key is at or before the instant.
func asOf(s *Store, c int) []Record {
	out := make([]Record, 0)
	for _, r := range s.Since(0, "") {
		if r.GlobalSeq <= c {
			out = append(out, r)
		}
	}
	return out
}

func seed(t *testing.T, s *Store, intentID string, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		if _, err := s.Append(Record{IntentID: intentID, Type: "SCORED"}); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
}

func TestAsOfViewIsImmutableAcrossRestatement(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	seed(t, s, "intent-a", 5)
	const cursor = 3
	before := asOf(s, cursor)
	if len(before) != cursor {
		t.Fatalf("as-of %d should see %d records, saw %d", cursor, cursor, len(before))
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// RESTATEMENT: a later session appends more history under the same feed.
	s2, err := Open(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()
	seed(t, s2, "intent-b", 4)

	after := asOf(s2, cursor)
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("LOOKAHEAD: the as-of view at seq %d changed after later data arrived\n before=%+v\n after =%+v",
			cursor, before, after)
	}
	for _, r := range after {
		if r.IntentID == "intent-b" {
			t.Fatalf("LOOKAHEAD: intent-b arrived after seq %d yet is visible at it", cursor)
		}
	}
}

func TestRecoveryDoesNotRenumberHistoricalSeqs(t *testing.T) {
	dir := t.TempDir()
	s, _ := Open(dir)
	seed(t, s, "intent-a", 4)
	before := asOf(s, 4)
	s.Close()

	s2, _ := Open(dir)
	defer s2.Close()
	if !reflect.DeepEqual(before, asOf(s2, 4)) {
		t.Fatal("recovery renumbered or reordered historical records")
	}
	// The next append must land strictly after the recovered history.
	r, _ := s2.Append(Record{IntentID: "intent-b", Type: "SCORED"})
	if r.GlobalSeq <= 4 {
		t.Fatalf("post-recovery append got seq %d, which backdates into history", r.GlobalSeq)
	}
}

// POLARITY: the check must be able to SEE a leak. A backdated record — one that
// arrives later but carries an as-of key inside the sealed window — is exactly
// the violation, planted here on a twin feed and required to be caught.
func TestPlantedBackdatedRecordIsCaught(t *testing.T) {
	dir := t.TempDir()
	s, _ := Open(dir)
	seed(t, s, "intent-a", 5)
	const cursor = 3
	before := asOf(s, cursor)
	s.Close()

	// Plant: append a record whose GlobalSeq backdates into the sealed window.
	path := filepath.Join(dir, "events.jsonl")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open feed: %v", err)
	}
	line, _ := json.Marshal(Record{GlobalSeq: 2, IntentSeq: 1, IntentID: "planted", Type: "SCORED"})
	if _, err := f.Write(append(line, '\n')); err != nil {
		t.Fatalf("plant: %v", err)
	}
	f.Close()

	s2, err := Open(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()

	after := asOf(s2, cursor)
	if reflect.DeepEqual(before, after) {
		t.Fatal("VACUOUS: the planted backdated record was not visible to the check - " +
			"a check that cannot see this leak cannot credit its own green")
	}
	found := false
	for _, r := range after {
		if r.IntentID == "planted" {
			found = true
		}
	}
	if !found {
		t.Fatal("VACUOUS: planted record absent from the as-of view")
	}
	t.Logf("planted backdated record CAUGHT: as-of %d went from %d to %d records",
		cursor, len(before), len(after))
}
