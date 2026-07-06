package durable

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// mustAppend appends a record and fails the test on error.
func mustAppend(t *testing.T, s *Store, r Record) Record {
	t.Helper()
	got, err := s.Append(r)
	if err != nil {
		t.Fatalf("Append(%+v): %v", r, err)
	}
	return got
}

// mustOpen opens a store over dir and fails the test on error.
func mustOpen(t *testing.T, dir string) *Store {
	t.Helper()
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("Open(%q): %v", dir, err)
	}
	return s
}

// TestRecoveryAcrossReopen: records appended before Close are all recovered by
// a fresh Open over the same dir, with max GlobalSeq preserved and the next
// append continuing at prevMax+1.
func TestRecoveryAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	s := mustOpen(t, dir)

	want := []Record{
		{IntentSeq: 0, IntentID: "intent-a", Type: "DECLARED", Detail: "intent-a"},
		{IntentSeq: 1, IntentID: "intent-a", Type: "SCORED", Detail: "balance:PASS"},
		{IntentSeq: 0, IntentID: "intent-b", Type: "DECLARED", Detail: "intent-b"},
		{IntentSeq: 2, IntentID: "intent-a", Type: "ACHIEVED", Detail: "intent-a",
			IdempotencyKey: "key-1", RuleArtifactHash: "rule-h", IntentSpecHash: "spec-h", TrajectoryHash: "traj-h"},
	}
	for i := range want {
		got := mustAppend(t, s, want[i])
		want[i].GlobalSeq = i + 1
		if !reflect.DeepEqual(got, want[i]) {
			t.Fatalf("Append returned %+v, want %+v", got, want[i])
		}
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Reopen and verify full recovery, including the ACHIEVED trace fields.
	s2 := mustOpen(t, dir)
	defer s2.Close()
	got := s2.Since(0, "")
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("after reopen, Since(0,\"\") = %+v, want %+v", got, want)
	}

	// GlobalSeq continues at prevMax+1.
	next := mustAppend(t, s2, Record{IntentSeq: 1, IntentID: "intent-b", Type: "SCORED", Detail: "fx:PASS"})
	if next.GlobalSeq != len(want)+1 {
		t.Fatalf("after reopen, next GlobalSeq = %d, want %d", next.GlobalSeq, len(want)+1)
	}

	// ByIntent recovered per-intent logs in IntentSeq order.
	a := s2.ByIntent("intent-a")
	if len(a) != 3 {
		t.Fatalf("ByIntent(intent-a) returned %d records, want 3", len(a))
	}
	for i, r := range a {
		if r.IntentSeq != i {
			t.Fatalf("ByIntent(intent-a)[%d].IntentSeq = %d, want %d", i, r.IntentSeq, i)
		}
	}
}

// TestGlobalSeqMonotonic: GlobalSeq strictly increases 1,2,3,... across
// interleaved intents, and continues at prevMax+1 across reopen with no reset
// or gap.
func TestGlobalSeqMonotonic(t *testing.T) {
	dir := t.TempDir()
	s := mustOpen(t, dir)

	const n = 10
	for i := 0; i < n; i++ {
		id := "intent-a"
		if i%2 == 1 {
			id = "intent-b"
		}
		got := mustAppend(t, s, Record{IntentSeq: i / 2, IntentID: id, Type: "SCORED", Detail: "c:PASS"})
		if got.GlobalSeq != i+1 {
			t.Fatalf("append %d: GlobalSeq = %d, want %d (strictly monotonic, no gap)", i, got.GlobalSeq, i+1)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	s2 := mustOpen(t, dir)
	defer s2.Close()
	got := mustAppend(t, s2, Record{IntentSeq: 5, IntentID: "intent-a", Type: "SCORED", Detail: "c:PASS"})
	if got.GlobalSeq != n+1 {
		t.Fatalf("after reopen, GlobalSeq = %d, want %d (continue at prevMax+1)", got.GlobalSeq, n+1)
	}

	// The full feed is strictly ascending with no duplicates or gaps.
	all := s2.Since(0, "")
	if len(all) != n+1 {
		t.Fatalf("Since(0,\"\") returned %d records, want %d", len(all), n+1)
	}
	for i, r := range all {
		if r.GlobalSeq != i+1 {
			t.Fatalf("feed[%d].GlobalSeq = %d, want %d", i, r.GlobalSeq, i+1)
		}
	}
}

// TestSinceCursorAndTypeFilter: Since returns exactly GlobalSeq > cursor in
// ascending order, filters by Type when typ != "", and returns fresh copies.
func TestSinceCursorAndTypeFilter(t *testing.T) {
	dir := t.TempDir()
	s := mustOpen(t, dir)
	defer s.Close()

	types := []string{"DECLARED", "SCORED", "ACHIEVED", "DECLARED", "SCORED", "ACHIEVED"}
	for i, typ := range types {
		mustAppend(t, s, Record{IntentSeq: i, IntentID: "intent-a", Type: typ, Detail: "d"})
	}

	// Cursor exactness: since=2 returns seq 3..6 only, ascending.
	got := s.Since(2, "")
	if len(got) != 4 {
		t.Fatalf("Since(2,\"\") returned %d records, want 4", len(got))
	}
	for i, r := range got {
		if r.GlobalSeq != i+3 {
			t.Fatalf("Since(2,\"\")[%d].GlobalSeq = %d, want %d", i, r.GlobalSeq, i+3)
		}
	}

	// Cursor at max: empty (and non-nil).
	if got := s.Since(6, ""); len(got) != 0 {
		t.Fatalf("Since(6,\"\") returned %d records, want 0", len(got))
	}

	// Type filter: only ACHIEVED records, still respecting the cursor.
	ach := s.Since(0, "ACHIEVED")
	if len(ach) != 2 || ach[0].GlobalSeq != 3 || ach[1].GlobalSeq != 6 {
		t.Fatalf("Since(0,\"ACHIEVED\") = %+v, want seqs [3 6]", ach)
	}
	ach = s.Since(3, "ACHIEVED")
	if len(ach) != 1 || ach[0].GlobalSeq != 6 {
		t.Fatalf("Since(3,\"ACHIEVED\") = %+v, want seq [6]", ach)
	}
	for _, r := range ach {
		if r.Type != "ACHIEVED" {
			t.Fatalf("type filter leaked record %+v", r)
		}
	}

	// Fresh copy: mutating the returned slice must not affect the store.
	first := s.Since(0, "")
	first[0].Type = "MUTATED"
	first[0].GlobalSeq = 999
	again := s.Since(0, "")
	if again[0].Type != "DECLARED" || again[0].GlobalSeq != 1 {
		t.Fatalf("Since does not return a fresh copy: got %+v after caller mutation", again[0])
	}
}

// TestByIntentOrder: ByIntent returns only that intent's records, in ascending
// IntentSeq order, even when appends were interleaved across intents.
func TestByIntentOrder(t *testing.T) {
	dir := t.TempDir()
	s := mustOpen(t, dir)
	defer s.Close()

	// Interleave two intents.
	mustAppend(t, s, Record{IntentSeq: 0, IntentID: "intent-a", Type: "DECLARED"})
	mustAppend(t, s, Record{IntentSeq: 0, IntentID: "intent-b", Type: "DECLARED"})
	mustAppend(t, s, Record{IntentSeq: 1, IntentID: "intent-b", Type: "SCORED"})
	mustAppend(t, s, Record{IntentSeq: 1, IntentID: "intent-a", Type: "SCORED"})
	mustAppend(t, s, Record{IntentSeq: 2, IntentID: "intent-a", Type: "ACHIEVED"})
	mustAppend(t, s, Record{IntentSeq: 2, IntentID: "intent-b", Type: "FAILED"})

	a := s.ByIntent("intent-a")
	if len(a) != 3 {
		t.Fatalf("ByIntent(intent-a) returned %d records, want 3", len(a))
	}
	wantTypes := []string{"DECLARED", "SCORED", "ACHIEVED"}
	for i, r := range a {
		if r.IntentID != "intent-a" {
			t.Fatalf("ByIntent(intent-a) leaked record for %q", r.IntentID)
		}
		if r.IntentSeq != i || r.Type != wantTypes[i] {
			t.Fatalf("ByIntent(intent-a)[%d] = {IntentSeq:%d Type:%q}, want {IntentSeq:%d Type:%q}",
				i, r.IntentSeq, r.Type, i, wantTypes[i])
		}
	}

	b := s.ByIntent("intent-b")
	if len(b) != 3 || b[2].Type != "FAILED" {
		t.Fatalf("ByIntent(intent-b) = %+v, want 3 records ending FAILED", b)
	}

	if got := s.ByIntent("intent-absent"); len(got) != 0 {
		t.Fatalf("ByIntent(intent-absent) returned %d records, want 0", len(got))
	}

	// Fresh copy.
	a[0].IntentID = "mutated"
	if got := s.ByIntent("intent-a"); got[0].IntentID != "intent-a" {
		t.Fatalf("ByIntent does not return a fresh copy")
	}
}

// TestTornLastLineTolerated: a torn (partial, unterminated) or blank trailing
// line is ignored on Open; everything before it is authoritative, GlobalSeq is
// recovered, and the next append lands on its own line (verified by a second
// reopen).
func TestTornLastLineTolerated(t *testing.T) {
	dir := t.TempDir()
	s := mustOpen(t, dir)
	mustAppend(t, s, Record{IntentSeq: 0, IntentID: "intent-a", Type: "DECLARED", Detail: "d0"})
	mustAppend(t, s, Record{IntentSeq: 1, IntentID: "intent-a", Type: "SCORED", Detail: "d1"})
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Simulate a torn last write: a partial JSON line with no trailing newline.
	path := filepath.Join(dir, "events.jsonl")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open for torn write: %v", err)
	}
	if _, err := f.WriteString(`{"seq":3,"intent_seq":2,"intent_id":"intent-a","ty`); err != nil {
		t.Fatalf("torn write: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close torn file: %v", err)
	}

	s2 := mustOpen(t, dir)
	got := s2.Since(0, "")
	if len(got) != 2 || got[0].GlobalSeq != 1 || got[1].GlobalSeq != 2 {
		t.Fatalf("after torn line, Since(0,\"\") = %+v, want the 2 intact records", got)
	}
	// Next append continues at prevMax+1 (torn line contributes nothing).
	r3 := mustAppend(t, s2, Record{IntentSeq: 2, IntentID: "intent-a", Type: "ACHIEVED", Detail: "d2"})
	if r3.GlobalSeq != 3 {
		t.Fatalf("append after torn recovery: GlobalSeq = %d, want 3", r3.GlobalSeq)
	}
	if err := s2.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// A second reopen proves the post-torn append did not glue onto the torn
	// bytes: all 3 intact records recover.
	s3 := mustOpen(t, dir)
	got = s3.Since(0, "")
	if len(got) != 3 || got[2].GlobalSeq != 3 || got[2].Type != "ACHIEVED" {
		t.Fatalf("after reopen post-torn-append, Since(0,\"\") = %+v, want 3 records ending ACHIEVED seq 3", got)
	}
	if err := s3.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Blank trailing line is also tolerated.
	f, err = os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open for blank write: %v", err)
	}
	if _, err := f.WriteString("\n"); err != nil {
		t.Fatalf("blank write: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close blank file: %v", err)
	}
	s4 := mustOpen(t, dir)
	defer s4.Close()
	if got := s4.Since(0, ""); len(got) != 3 {
		t.Fatalf("after blank trailing line, Since(0,\"\") returned %d records, want 3", len(got))
	}
	if r := mustAppend(t, s4, Record{IntentSeq: 0, IntentID: "intent-b", Type: "DECLARED"}); r.GlobalSeq != 4 {
		t.Fatalf("append after blank-line recovery: GlobalSeq = %d, want 4", r.GlobalSeq)
	}
}

// TestCallerSetGlobalSeqIgnored: Append assigns GlobalSeq itself; any
// caller-set value is ignored and overwritten, in the returned record, the read
// surface, and on disk.
func TestCallerSetGlobalSeqIgnored(t *testing.T) {
	dir := t.TempDir()
	s := mustOpen(t, dir)
	got := mustAppend(t, s, Record{GlobalSeq: 999, IntentSeq: 0, IntentID: "intent-a", Type: "DECLARED"})
	if got.GlobalSeq != 1 {
		t.Fatalf("Append with caller-set GlobalSeq=999 returned seq %d, want 1", got.GlobalSeq)
	}
	got = mustAppend(t, s, Record{GlobalSeq: -7, IntentSeq: 1, IntentID: "intent-a", Type: "SCORED"})
	if got.GlobalSeq != 2 {
		t.Fatalf("Append with caller-set GlobalSeq=-7 returned seq %d, want 2", got.GlobalSeq)
	}
	all := s.Since(0, "")
	if len(all) != 2 || all[0].GlobalSeq != 1 || all[1].GlobalSeq != 2 {
		t.Fatalf("stored seqs = %+v, want [1 2]", all)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// On-disk lines carry the assigned seqs, one JSON object per '\n' line.
	raw, err := os.ReadFile(filepath.Join(dir, "events.jsonl"))
	if err != nil {
		t.Fatalf("read events.jsonl: %v", err)
	}
	if len(raw) == 0 || raw[len(raw)-1] != '\n' {
		t.Fatalf("events.jsonl must end with a newline")
	}
	lines := 0
	for _, line := range splitLines(raw) {
		var rec Record
		if err := json.Unmarshal(line, &rec); err != nil {
			t.Fatalf("on-disk line %q not valid JSON: %v", line, err)
		}
		lines++
		if rec.GlobalSeq != lines {
			t.Fatalf("on-disk line %d has seq %d, want %d", lines, rec.GlobalSeq, lines)
		}
	}
	if lines != 2 {
		t.Fatalf("events.jsonl has %d lines, want 2", lines)
	}
}

// splitLines splits raw into its non-empty '\n'-terminated lines.
func splitLines(raw []byte) [][]byte {
	var out [][]byte
	start := 0
	for i, b := range raw {
		if b == '\n' {
			if i > start {
				out = append(out, raw[start:i])
			}
			start = i + 1
		}
	}
	return out
}
