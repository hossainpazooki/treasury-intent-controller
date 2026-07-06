package main

// CONTRACT-V2 §V2.5 server tests. All IO lives under t.TempDir() (wired through
// TIC_DATA_DIR via t.Setenv, mirroring main's boot path); no test binds a port
// (httptest.NewRecorder + mux.ServeHTTP) and no test touches the network.
//
// §V2.6 successors covered here:
//   - ACHIEVED   => achieved_seq >= 1 AND the ACHIEVED record is visible via
//     GET /v2/events?type=ACHIEVED (successor to "settlement present").
//   - FAILED_AT_DISPATCH => "achieved_seq" absent from the response JSON AND no
//     ACHIEVED record in the feed (successor to "settlement nil").
//   - Restart: reopening the stores over the SAME dir preserves events, keeps
//     GlobalSeq monotonic (continues at prevMax+1), and a same-key re-submit is
//     refused with "idempotency-collision" (at-most-once across restart).

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/pazooki/treasury-intent-controller/internal/durable"
	"github.com/pazooki/treasury-intent-controller/internal/idempotency"
	"github.com/pazooki/treasury-intent-controller/internal/intent"
	"github.com/pazooki/treasury-intent-controller/internal/lifecycle"
)

// testServer is one booted server instance: the mux over the shared boot-time
// stores, exactly as main wires them.
type testServer struct {
	t      *testing.T
	mux    *http.ServeMux
	feed   *durable.Store
	istore *idempotency.Store
}

// boot mirrors main's boot path: set TIC_DATA_DIR (t.Setenv), read it back from
// the environment, open the durable feed and the durable idempotency store ONCE
// over that dir, and build the mux over the shared stores. Call close() to
// simulate process shutdown before re-booting over the same dir.
func boot(t *testing.T, dir string) *testServer {
	t.Helper()
	t.Setenv("TIC_DATA_DIR", dir)
	dataDir := os.Getenv("TIC_DATA_DIR")
	if dataDir == "" {
		dataDir = "./data" // main's default; never reached under t.Setenv
	}
	feed, err := durable.Open(dataDir)
	if err != nil {
		t.Fatalf("durable.Open(%q): %v", dataDir, err)
	}
	istore, err := idempotency.OpenStore(dataDir)
	if err != nil {
		_ = feed.Close()
		t.Fatalf("idempotency.OpenStore(%q): %v", dataDir, err)
	}
	ts := &testServer{t: t, mux: newMux(feed, istore), feed: feed, istore: istore}
	t.Cleanup(ts.close) // double Close is a no-op on both stores
	return ts
}

// close releases the underlying files (simulated process exit).
func (ts *testServer) close() {
	_ = ts.feed.Close()
	_ = ts.istore.Close()
}

// do drives one request through the mux without binding a port.
func (ts *testServer) do(method, target, body string) *httptest.ResponseRecorder {
	ts.t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	rr := httptest.NewRecorder()
	ts.mux.ServeHTTP(rr, req)
	return rr
}

// postIntent POSTs an intent body and decodes the intentResponse, failing the
// test on a non-200 status or an undecodable body.
func (ts *testServer) postIntent(body string) intentResponse {
	ts.t.Helper()
	rr := ts.do(http.MethodPost, "/v2/intents", body)
	if rr.Code != http.StatusOK {
		ts.t.Fatalf("POST /v2/intents status = %d, want 200 (body=%q)", rr.Code, rr.Body.String())
	}
	var resp intentResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		ts.t.Fatalf("decode intentResponse: %v (body=%q)", err, rr.Body.String())
	}
	return resp
}

// getEvents GETs /v2/events with the given query and decodes the wrapper.
func (ts *testServer) getEvents(query string) eventsResponse {
	ts.t.Helper()
	rr := ts.do(http.MethodGet, "/v2/events"+query, "")
	if rr.Code != http.StatusOK {
		ts.t.Fatalf("GET /v2/events%s status = %d, want 200 (body=%q)", query, rr.Code, rr.Body.String())
	}
	var resp eventsResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		ts.t.Fatalf("decode eventsResponse: %v (body=%q)", err, rr.Body.String())
	}
	return resp
}

// intentBody builds the unchanged slice-1 request DTO with one volatile
// criterion driven by force_scores (documented probe affordance).
func intentBody(seed, key, specHash, declaration, dispatch string) string {
	return fmt.Sprintf(`{
		"episode_seed": %q,
		"idempotency_key": %q,
		"rule_artifact_hash": "rule-hash-1",
		"intent_spec_hash": %q,
		"spec": {
			"action_class": "payment",
			"idempotency_scope": "payer",
			"criteria": [
				{"name": "balance", "threshold": 1.0, "volatility": "volatile"}
			]
		},
		"force_scores": {
			"balance": {"declaration": %q, "dispatch": %q}
		}
	}`, seed, key, specHash, declaration, dispatch)
}

// intentID computes the deterministic intent ID exactly as the gate does.
func intentID(seed string) string {
	return intent.Intent{EpisodeSeed: seed}.ID()
}

func TestHealthz(t *testing.T) {
	ts := boot(t, t.TempDir())
	rr := ts.do(http.MethodGet, "/healthz", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("healthz status = %d, want 200", rr.Code)
	}
	if got := rr.Body.String(); got != "ok" {
		t.Fatalf("healthz body = %q, want %q", got, "ok")
	}
}

// TestIntentsAchieved: a volatile criterion passing at both phases reaches
// ACHIEVED with achieved_seq >= 1, and the ACHIEVED record (with its trace
// fields) is visible via GET /v2/events?type=ACHIEVED — the §V2.6 successor to
// the slice-1 "settlement present" assertion.
func TestIntentsAchieved(t *testing.T) {
	ts := boot(t, t.TempDir())
	resp := ts.postIntent(intentBody("seed-achieved", "key-achieved", "spec-hash-1", "PASS", "PASS"))

	if resp.Terminal != string(lifecycle.Achieved) {
		t.Fatalf("terminal = %q, want %q", resp.Terminal, lifecycle.Achieved)
	}
	if resp.AchievedSeq < 1 {
		t.Fatalf("achieved_seq = %d, want >= 1", resp.AchievedSeq)
	}
	if resp.TrajectoryHash == "" {
		t.Fatalf("trajectory_hash must be non-empty")
	}

	ev := ts.getEvents("?type=ACHIEVED")
	if len(ev.Events) != 1 {
		t.Fatalf("GET /v2/events?type=ACHIEVED returned %d records, want 1: %+v", len(ev.Events), ev.Events)
	}
	rec := ev.Events[0]
	if rec.Type != "ACHIEVED" {
		t.Fatalf("record type = %q, want ACHIEVED", rec.Type)
	}
	if rec.IntentID != intentID("seed-achieved") {
		t.Fatalf("record intent_id = %q, want %q", rec.IntentID, intentID("seed-achieved"))
	}
	if rec.GlobalSeq != resp.AchievedSeq {
		t.Fatalf("record seq = %d, want achieved_seq %d", rec.GlobalSeq, resp.AchievedSeq)
	}
	if rec.TrajectoryHash != resp.TrajectoryHash {
		t.Fatalf("record trajectory_hash = %q, want %q", rec.TrajectoryHash, resp.TrajectoryHash)
	}
	if rec.IdempotencyKey != "key-achieved" {
		t.Fatalf("record idempotency_key = %q, want %q", rec.IdempotencyKey, "key-achieved")
	}
	if rec.RuleArtifactHash == "" || rec.IntentSpecHash == "" {
		t.Fatalf("ACHIEVED record must carry rule_artifact_hash and intent_spec_hash, got %+v", rec)
	}
}

// TestIntentsFailedAtDispatch: a volatile criterion failing the dispatch-edge
// re-verify reaches FAILED_AT_DISPATCH with NO achieved_seq key in the JSON and
// NO ACHIEVED record in the feed — the §V2.6 successor to "settlement nil".
func TestIntentsFailedAtDispatch(t *testing.T) {
	ts := boot(t, t.TempDir())
	rr := ts.do(http.MethodPost, "/v2/intents", intentBody("seed-fad", "key-fad", "spec-hash-2", "PASS", "FAIL"))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%q)", rr.Code, rr.Body.String())
	}
	var resp intentResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode intentResponse: %v (body=%q)", err, rr.Body.String())
	}
	if resp.Terminal != string(lifecycle.FailedAtDispatch) {
		t.Fatalf("terminal = %q, want %q", resp.Terminal, lifecycle.FailedAtDispatch)
	}
	if resp.Reason != "volatile-recheck:balance" {
		t.Fatalf("reason = %q, want %q", resp.Reason, "volatile-recheck:balance")
	}

	// achieved_seq is omitempty: the KEY itself must be absent on non-ACHIEVED.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rr.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode raw response: %v", err)
	}
	if _, present := raw["achieved_seq"]; present {
		t.Fatalf("achieved_seq must be absent for FAILED_AT_DISPATCH, body=%q", rr.Body.String())
	}

	// No ACHIEVED record anywhere in the feed for this run.
	if ev := ts.getEvents("?type=ACHIEVED"); len(ev.Events) != 0 {
		t.Fatalf("feed must hold no ACHIEVED record, got %+v", ev.Events)
	}
	// But the FAILED_AT_DISPATCH record itself was durably mirrored.
	found := false
	for _, r := range ts.getEvents("?since=0").Events {
		if r.IntentID == intentID("seed-fad") && r.Type == "FAILED_AT_DISPATCH" {
			found = true
		}
	}
	if !found {
		t.Fatalf("feed must hold the FAILED_AT_DISPATCH record for the intent")
	}
}

// TestEventsCursorPaging: since=0 returns everything ascending; since=N returns
// exactly the records with seq > N; next_since is the max returned GlobalSeq, or
// the input since when nothing is returned; type filters records.
func TestEventsCursorPaging(t *testing.T) {
	ts := boot(t, t.TempDir())
	ts.postIntent(intentBody("seed-page-1", "key-page-1", "spec-hash-p1", "PASS", "PASS"))
	ts.postIntent(intentBody("seed-page-2", "key-page-2", "spec-hash-p2", "PASS", "PASS"))

	all := ts.getEvents("?since=0")
	if len(all.Events) == 0 {
		t.Fatalf("since=0 must return every record")
	}
	for idx, r := range all.Events {
		if r.GlobalSeq != idx+1 {
			t.Fatalf("since=0 records must be seq 1..N ascending with no gap; index %d has seq %d", idx, r.GlobalSeq)
		}
	}
	max := all.Events[len(all.Events)-1].GlobalSeq
	if all.NextSince != max {
		t.Fatalf("next_since = %d, want max returned seq %d", all.NextSince, max)
	}

	// Mid-cursor page: exactly the records with seq > mid, same order.
	mid := max / 2
	page := ts.getEvents(fmt.Sprintf("?since=%d", mid))
	if want := max - mid; len(page.Events) != want {
		t.Fatalf("since=%d returned %d records, want %d", mid, len(page.Events), want)
	}
	for idx, r := range page.Events {
		if r.GlobalSeq != mid+idx+1 {
			t.Fatalf("since=%d page out of order: index %d has seq %d, want %d", mid, idx, r.GlobalSeq, mid+idx+1)
		}
	}
	if page.NextSince != max {
		t.Fatalf("page next_since = %d, want %d", page.NextSince, max)
	}

	// Exhausted cursor: no records; next_since echoes the input since.
	empty := ts.getEvents(fmt.Sprintf("?since=%d", max))
	if len(empty.Events) != 0 {
		t.Fatalf("since=max must return nothing, got %+v", empty.Events)
	}
	if empty.NextSince != max {
		t.Fatalf("empty-page next_since = %d, want input since %d", empty.NextSince, max)
	}

	// Type filter: exactly the two ACHIEVED records, ascending.
	ach := ts.getEvents("?since=0&type=ACHIEVED")
	if len(ach.Events) != 2 {
		t.Fatalf("type=ACHIEVED returned %d records, want 2", len(ach.Events))
	}
	if ach.Events[0].GlobalSeq >= ach.Events[1].GlobalSeq {
		t.Fatalf("type-filtered records out of ascending order: %+v", ach.Events)
	}
	for _, r := range ach.Events {
		if r.Type != "ACHIEVED" {
			t.Fatalf("type filter leaked a %q record", r.Type)
		}
	}
	if ach.NextSince != ach.Events[1].GlobalSeq {
		t.Fatalf("filtered next_since = %d, want %d", ach.NextSince, ach.Events[1].GlobalSeq)
	}
}

// TestIntentEventsOrder: GET /v2/intents/{id}/events returns that intent's
// records in ascending intent_seq, DECLARED (seq 0) first, ACHIEVED last and
// ordered after the volatile RECHECK (§V2.6(f)).
func TestIntentEventsOrder(t *testing.T) {
	ts := boot(t, t.TempDir())
	// A second intent first, so the endpoint must actually filter by id.
	ts.postIntent(intentBody("seed-other", "key-other", "spec-hash-o", "PASS", "PASS"))
	resp := ts.postIntent(intentBody("seed-order", "key-order", "spec-hash-3", "PASS", "PASS"))
	if resp.Terminal != string(lifecycle.Achieved) {
		t.Fatalf("terminal = %q, want ACHIEVED", resp.Terminal)
	}

	id := intentID("seed-order")
	rr := ts.do(http.MethodGet, "/v2/intents/"+id+"/events", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /v2/intents/{id}/events status = %d, want 200", rr.Code)
	}
	var per intentEventsResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &per); err != nil {
		t.Fatalf("decode intentEventsResponse: %v (body=%q)", err, rr.Body.String())
	}
	if per.IntentID != id {
		t.Fatalf("intent_id = %q, want %q", per.IntentID, id)
	}
	if len(per.Events) == 0 {
		t.Fatalf("no events returned for intent %q", id)
	}
	recheckIdx, achievedIdx := -1, -1
	for idx, r := range per.Events {
		if r.IntentID != id {
			t.Fatalf("record %d belongs to intent %q, want only %q", idx, r.IntentID, id)
		}
		if r.IntentSeq != idx {
			t.Fatalf("intent_seq must ascend 0..N-1; index %d has intent_seq %d", idx, r.IntentSeq)
		}
		switch r.Type {
		case "RECHECK":
			recheckIdx = idx
		case "ACHIEVED":
			achievedIdx = idx
		}
	}
	if first := per.Events[0]; first.Type != "DECLARED" || first.IntentSeq != 0 {
		t.Fatalf("first record must be DECLARED at intent_seq 0, got %+v", first)
	}
	if last := per.Events[len(per.Events)-1]; last.Type != "ACHIEVED" {
		t.Fatalf("last record must be ACHIEVED, got %+v", last)
	}
	if recheckIdx == -1 || achievedIdx == -1 || achievedIdx <= recheckIdx {
		t.Fatalf("ACHIEVED (idx %d) must be ordered after the volatile RECHECK (idx %d)", achievedIdx, recheckIdx)
	}
}

// TestRestartAtMostOnce: reboot the server (rebuild mux + stores) over the SAME
// data dir. The same key must collide (at-most-once across process restart),
// prior events must be preserved, and GlobalSeq must continue at prevMax+1 with
// no reset.
func TestRestartAtMostOnce(t *testing.T) {
	dir := t.TempDir()

	// First process lifetime: reserve the key via an ACHIEVED intent.
	s1 := boot(t, dir)
	first := s1.postIntent(intentBody("seed-restart-1", "key-restart", "spec-hash-r1", "PASS", "PASS"))
	if first.Terminal != string(lifecycle.Achieved) {
		t.Fatalf("first terminal = %q, want ACHIEVED", first.Terminal)
	}
	before := s1.getEvents("?since=0")
	prevMax := before.NextSince
	if prevMax < 1 {
		t.Fatalf("prevMax = %d, want >= 1", prevMax)
	}
	s1.close() // simulated process exit

	// Second process lifetime over the SAME dir.
	s2 := boot(t, dir)

	// Same key, different intent (different seed + spec hash) => collision.
	second := s2.postIntent(intentBody("seed-restart-2", "key-restart", "spec-hash-r2", "PASS", "PASS"))
	if second.Terminal != string(lifecycle.FailedAtDispatch) {
		t.Fatalf("post-restart terminal = %q, want FAILED_AT_DISPATCH", second.Terminal)
	}
	if second.Reason != "idempotency-collision" {
		t.Fatalf("post-restart reason = %q, want %q", second.Reason, "idempotency-collision")
	}
	if second.AchievedSeq != 0 {
		t.Fatalf("post-restart achieved_seq = %d, want 0", second.AchievedSeq)
	}

	after := s2.getEvents("?since=0")

	// Events preserved: every pre-restart record is still there, verbatim.
	if len(after.Events) <= len(before.Events) {
		t.Fatalf("post-restart feed has %d records, want > %d (preserved + new)", len(after.Events), len(before.Events))
	}
	for idx, r := range before.Events {
		if after.Events[idx] != r {
			t.Fatalf("pre-restart record %d changed across restart: before=%+v after=%+v", idx, r, after.Events[idx])
		}
	}

	// GlobalSeq continues: the second intent's records start at prevMax+1 and
	// stay strictly monotonic (no reset, no gap).
	newRecords := after.Events[len(before.Events):]
	for idx, r := range newRecords {
		if r.GlobalSeq != prevMax+idx+1 {
			t.Fatalf("post-restart seq must continue at prevMax+1 with no gap; index %d has seq %d, want %d",
				idx, r.GlobalSeq, prevMax+idx+1)
		}
		if r.IntentID != intentID("seed-restart-2") {
			t.Fatalf("unexpected post-restart record owner %q: %+v", r.IntentID, r)
		}
	}

	// At-most-once across restart: still exactly ONE ACHIEVED record for the key.
	ach := s2.getEvents("?since=0&type=ACHIEVED")
	if len(ach.Events) != 1 {
		t.Fatalf("feed must hold exactly one ACHIEVED record across restart, got %d: %+v", len(ach.Events), ach.Events)
	}
	if ach.Events[0].IdempotencyKey != "key-restart" || ach.Events[0].IntentID != intentID("seed-restart-1") {
		t.Fatalf("the single ACHIEVED record must belong to the first intent, got %+v", ach.Events[0])
	}
}
