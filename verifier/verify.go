// Package verifier is the independent ex-post examiner of the durable feed
// (CONTRACT.md §1.1 verifier role, §9.1). It re-derives every commitment the
// feed makes — per-intent trajectory hashes, sequence contiguity, the
// exactly-one-ACHIEVED discipline — WITHOUT trusting the gate: this tree
// imports nothing from the module outside itself (§7.1), so its reading of
// the §6 hash encoding and the §2.3 record shape is a genuinely second
// implementation. Its Python twin (pyverifier/verify.py) is held to the same
// reading by the §9.1 feed fixtures.
//
// Verdicts are tri-state like the gate it audits: VERIFIED / REFUTED /
// UNVERIFIABLE, and unevaluable never collapses into pass — an empty feed, an
// unparseable non-trailing line, an unknown event type, or a missing terminal
// hash is UNVERIFIABLE, never VERIFIED. REFUTED is reserved for positive
// contradiction (a committed hash that fails recomputation, a sequence gap, a
// hash on a non-terminal record, a duplicated ACHIEVED).
package verifier

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Verdict is the tri-state outcome of one check scope.
type Verdict string

const (
	Verified     Verdict = "VERIFIED"
	Refuted      Verdict = "REFUTED"
	Unverifiable Verdict = "UNVERIFIABLE"
)

// record mirrors the §2.3 wire shape by a SECOND declaration — deliberately
// not imported from core/internal/durable. Unknown fields are ignored
// (forward-compatible read); missing detail decodes as "".
type record struct {
	Seq              int
	IntentSeq        int
	IntentID         string
	Type             string
	Detail           string
	IdempotencyKey   string
	RuleArtifactHash string
	IntentSpecHash   string
	TrajectoryHash   string
}

// knownTypes is the closed §4.2 event-type set; anything else is
// UNVERIFIABLE (never VERIFIED — the verifier does not guess).
var knownTypes = map[string]bool{
	"DECLARED": true, "RESOLVING": true, "ACTIVE": true, "VERIFYING": true,
	"SCORED": true, "RECHECK": true, "UNEVALUABLE": true, "REVOKED": true,
	"IDEMPOTENCY_RESERVED": true, "ACHIEVED": true, "FAILED": true,
	"FAILED_AT_DISPATCH": true, "SHADOW_RECORDED": true,
}

// terminalTypes are the record types that may legally sit in terminal
// position carrying the trajectory hash (§2.3 refusal-hash commitment).
var terminalTypes = map[string]bool{
	"ACHIEVED": true, "SHADOW_RECORDED": true, "FAILED": true,
	"FAILED_AT_DISPATCH": true, "UNEVALUABLE": true, "REVOKED": true,
}

// traceTypes are the record types required to carry the three provenance
// fields (§2.3).
var traceTypes = map[string]bool{"ACHIEVED": true, "SHADOW_RECORDED": true}

// Finding is one reported line: a scope ("feed" or an intent id), a verdict,
// and a closed-vocabulary reason ("" only for VERIFIED).
type Finding struct {
	Scope   string
	Verdict Verdict
	Reason  string
}

// Report is the full result of one verification run.
type Report struct {
	Feed    []Finding // feed-level findings, detection order; empty = clean
	Intents []Finding // one per intent, first-appearance order
	Overall Verdict
}

// trajectoryHash recomputes the §6 canonical encoding over (seq, type,
// detail) triples: <len>:<bytes>\n per field, SHA-256, lowercase hex. Byte
// lengths — Go strings decoded from JSON are UTF-8, so len(s) IS the byte
// length.
func trajectoryHash(recs []record) string {
	h := sha256.New()
	writeField := func(s string) {
		h.Write([]byte(strconv.Itoa(len(s))))
		h.Write([]byte{':'})
		h.Write([]byte(s))
		h.Write([]byte{'\n'})
	}
	for _, r := range recs {
		writeField(strconv.Itoa(r.IntentSeq))
		writeField(r.Type)
		writeField(r.Detail)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// intField extracts a required integer-LITERAL field ("3", never "3.0" —
// both twins reject non-integer literals identically) with a minimum value.
func intField(m map[string]json.RawMessage, key string, min int) (int, bool) {
	raw, ok := m[key]
	if !ok {
		return 0, false
	}
	n, err := strconv.Atoi(string(raw))
	if err != nil || n < min {
		return 0, false
	}
	return n, true
}

// strField extracts an optional string field; a present non-string value is a
// shape violation (ok=false). Absent yields "".
func strField(m map[string]json.RawMessage, key string) (string, bool) {
	raw, ok := m[key]
	if !ok {
		return "", true
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", false
	}
	return s, true
}

// parseLine decodes one JSONL line in two phases — parse, then shape-check —
// so the twins classify malformed lines identically: invalid JSON is
// "unparseable", valid JSON with wrong field shapes is "bad-record".
func parseLine(trimmed string) (record, bool, bool) { // (rec, parseOK, shapeOK)
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(trimmed), &m); err != nil {
		return record{}, false, false
	}
	var r record
	var ok bool
	if r.Seq, ok = intField(m, "seq", 1); !ok {
		return record{}, true, false
	}
	if r.IntentSeq, ok = intField(m, "intent_seq", 0); !ok {
		return record{}, true, false
	}
	for _, f := range []struct {
		key      string
		dst      *string
		required bool
	}{
		{"intent_id", &r.IntentID, true},
		{"type", &r.Type, true},
		{"detail", &r.Detail, false},
		{"idempotency_key", &r.IdempotencyKey, false},
		{"rule_artifact_hash", &r.RuleArtifactHash, false},
		{"intent_spec_hash", &r.IntentSpecHash, false},
		{"trajectory_hash", &r.TrajectoryHash, false},
	} {
		s, ok := strField(m, f.key)
		if !ok || (f.required && s == "") {
			return record{}, true, false
		}
		*f.dst = s
	}
	return r, true, true
}

// parseFeed splits raw events.jsonl bytes into records, reproducing the §2.3
// read tolerance exactly: lines split on '\n', trailing '\r'/'\n' trimmed,
// blank lines skipped, a torn TRAILING line (nonblank, unparseable, no final
// newline) tolerated. An unparseable or shape-invalid line anywhere else is a
// feed-level finding.
func parseFeed(raw []byte) ([]record, []Finding) {
	var recs []record
	var findings []Finding
	lines := strings.Split(string(raw), "\n")
	for idx, line := range lines {
		trimmed := strings.TrimRight(line, "\r\n")
		if trimmed == "" {
			continue
		}
		lineno := idx + 1
		tornTail := idx == len(lines)-1 // no final newline: a torn last write
		r, parseOK, shapeOK := parseLine(trimmed)
		if !parseOK {
			if tornTail {
				continue
			}
			findings = append(findings, Finding{"feed", Unverifiable, "unparseable-line:" + strconv.Itoa(lineno)})
			continue
		}
		if !shapeOK {
			findings = append(findings, Finding{"feed", Refuted, "bad-record:" + strconv.Itoa(lineno)})
			continue
		}
		recs = append(recs, r)
	}
	return recs, findings
}

// verifyIntent runs the per-intent checks in their FIXED order (the twins
// must agree on order, or their first-finding verdicts diverge). recs are the
// intent's records in file order.
func verifyIntent(id string, recs []record) Finding {
	sorted := make([]record, len(recs))
	copy(sorted, recs)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].IntentSeq < sorted[j].IntentSeq })

	// 1. intent_seq contiguity: gap-free 0..n, no duplicates.
	for i, r := range sorted {
		if r.IntentSeq != i {
			if i > 0 && r.IntentSeq == sorted[i-1].IntentSeq {
				return Finding{id, Refuted, "intent-seq-dup:" + strconv.Itoa(r.IntentSeq)}
			}
			return Finding{id, Refuted, fmt.Sprintf("intent-seq-gap:%d:%d", r.IntentSeq, i)}
		}
	}
	// 2. DECLARED first, detail = intent id (the declaration binding).
	if sorted[0].Type != "DECLARED" {
		return Finding{id, Refuted, "missing-declared"}
	}
	if sorted[0].Detail != id {
		return Finding{id, Refuted, "declared-detail-mismatch"}
	}
	// 3. Closed event-type set.
	for _, r := range sorted {
		if !knownTypes[r.Type] {
			return Finding{id, Unverifiable, "unknown-event-type:" + r.Type}
		}
	}
	// 4. Hash placement: only the terminal-position record may carry one.
	last := sorted[len(sorted)-1]
	for _, r := range sorted[:len(sorted)-1] {
		if r.TrajectoryHash != "" {
			return Finding{id, Refuted, "hash-on-nonterminal:" + strconv.Itoa(r.IntentSeq)}
		}
	}
	// 5. Terminal hash present (absence = pre-amendment or torn intent:
	// honest abstention, never a pass).
	if last.TrajectoryHash == "" {
		return Finding{id, Unverifiable, "no-terminal-hash"}
	}
	if !terminalTypes[last.Type] {
		return Finding{id, Refuted, "terminal-type:" + last.Type}
	}
	// 6. ACHIEVED discipline: at most one, terminal-position, provenance
	// fields present (same for SHADOW_RECORDED's provenance).
	achieved := 0
	for _, r := range sorted {
		if r.Type == "ACHIEVED" {
			achieved++
		}
	}
	if achieved > 1 {
		return Finding{id, Refuted, "duplicate-achieved"}
	}
	if achieved == 1 && last.Type != "ACHIEVED" {
		return Finding{id, Refuted, "achieved-not-terminal"}
	}
	if traceTypes[last.Type] {
		// Only the fields the gate STRUCTURALLY guarantees: an empty key is
		// refused at declaration and an unresolved spec never reaches
		// ACHIEVED, so these two are nonempty on every grant.
		// rule_artifact_hash is an opaque artifact-plane passthrough that a
		// deployment may legitimately omit — absence is not contradiction
		// (learned live: probe 9's first run refuted a correct feed on it).
		// Fixed field order — the twins' first-missing-field reasons must
		// agree byte-for-byte, so no map iteration here.
		for _, fv := range [][2]string{
			{"idempotency_key", last.IdempotencyKey},
			{"intent_spec_hash", last.IntentSpecHash},
		} {
			if fv[1] == "" {
				return Finding{id, Refuted, "trace-field-missing:" + last.Type + ":" + fv[0]}
			}
		}
	}
	// 7. The commitment itself: the committed hash must recompute from the
	// records alone.
	if got := trajectoryHash(sorted); got != last.TrajectoryHash {
		return Finding{id, Refuted, "hash-mismatch"}
	}
	return Finding{id, Verified, ""}
}

// Verify runs the full §9.1 check set over raw events.jsonl bytes.
func Verify(raw []byte) Report {
	recs, feedFindings := parseFeed(raw)
	rep := Report{Feed: feedFindings}

	if len(recs) == 0 {
		rep.Feed = append(rep.Feed, Finding{"feed", Unverifiable, "empty"})
	}

	// Global seq: strictly ascending, gap-free from 1, in file order.
	for i, r := range recs {
		if r.Seq != i+1 {
			rep.Feed = append(rep.Feed, Finding{"feed", Refuted, fmt.Sprintf("seq-gap:%d:%d", r.Seq, i+1)})
			break // one report; everything after the first gap is suspect
		}
	}
	// Exactly-one-ACHIEVED per idempotency key, feed-wide.
	seenKey := map[string]bool{}
	for _, r := range recs {
		if r.Type != "ACHIEVED" || r.IdempotencyKey == "" {
			continue
		}
		if seenKey[r.IdempotencyKey] {
			rep.Feed = append(rep.Feed, Finding{"feed", Refuted, "duplicate-achieved-key:" + r.IdempotencyKey})
		}
		seenKey[r.IdempotencyKey] = true
	}

	// Per-intent checks, first-appearance order.
	byIntent := map[string][]record{}
	var order []string
	for _, r := range recs {
		if _, ok := byIntent[r.IntentID]; !ok {
			order = append(order, r.IntentID)
		}
		byIntent[r.IntentID] = append(byIntent[r.IntentID], r)
	}
	for _, id := range order {
		rep.Intents = append(rep.Intents, verifyIntent(id, byIntent[id]))
	}

	// Aggregate: REFUTED beats UNVERIFIABLE beats VERIFIED.
	rep.Overall = Verified
	for _, f := range append(append([]Finding{}, rep.Feed...), rep.Intents...) {
		switch f.Verdict {
		case Refuted:
			rep.Overall = Refuted
		case Unverifiable:
			if rep.Overall != Refuted {
				rep.Overall = Unverifiable
			}
		}
	}
	return rep
}

// Render prints the canonical report — the byte surface the twins are held
// to: feed findings first, then one line per intent in first-appearance
// order, then the RESULT line. LF line endings, no trailing spaces.
func Render(rep Report) string {
	var b strings.Builder
	line := func(f Finding) {
		b.WriteString(f.Scope)
		b.WriteString(" ")
		b.WriteString(string(f.Verdict))
		if f.Reason != "" {
			b.WriteString(" ")
			b.WriteString(f.Reason)
		}
		b.WriteString("\n")
	}
	for _, f := range rep.Feed {
		line(f)
	}
	for _, f := range rep.Intents {
		line(Finding{"intent " + f.Scope, f.Verdict, f.Reason})
	}
	v, r, u := 0, 0, 0
	for _, f := range rep.Intents {
		switch f.Verdict {
		case Verified:
			v++
		case Refuted:
			r++
		case Unverifiable:
			u++
		}
	}
	fmt.Fprintf(&b, "RESULT: %s intents=%d verified=%d refuted=%d unverifiable=%d\n",
		rep.Overall, len(rep.Intents), v, r, u)
	return b.String()
}
