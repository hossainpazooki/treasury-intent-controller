package main

// CONTRACT.md §2 server tests. All IO lives under t.TempDir() (wired through
// INTENT_DATA_DIR via t.Setenv, mirroring main's boot path); no test binds a port
// (httptest.NewRecorder + mux.ServeHTTP) and no test touches the network.
//
// §5.3 successors covered here:
//   - ACHIEVED   => achieved_seq >= 1 AND the ACHIEVED record is visible via
//     GET /v2/events?type=ACHIEVED (successor to "settlement present").
//   - FAILED_AT_DISPATCH => "achieved_seq" absent from the response JSON AND no
//     ACHIEVED record in the feed (successor to "settlement nil").
//   - Restart: reopening the stores over the SAME dir preserves events, keeps
//     GlobalSeq monotonic (continues at prevMax+1), and a same-key re-submit is
//     refused with "idempotency-collision" (at-most-once across restart).

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hossainpazooki/intent-plane/core/internal/durable"
	"github.com/hossainpazooki/intent-plane/core/internal/idempotency"
	"github.com/hossainpazooki/intent-plane/core/internal/intent"
	"github.com/hossainpazooki/intent-plane/core/internal/lifecycle"
	"github.com/hossainpazooki/intent-plane/plane"
)

// testKeyFile is a TEST-LOCAL signing seat. The core's tests must not import
// an application's authority package (layering: the SDK verifies what
// applications sign; core-neutrality keeps application vocabulary out of
// core/), so fixture signing lives here — same on-disk shape, test authority
// by construction.
type testKeyFile struct {
	KeyID        string `json:"keyid"`
	Public       string `json:"public"`
	Private      string `json:"private"`
	KeyAuthority string `json:"key_authority"`
}

func testKeygen(path string) (testKeyFile, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return testKeyFile{}, err
	}
	kf := testKeyFile{
		KeyID:        plane.KeyIDFor(pub),
		Public:       base64.StdEncoding.EncodeToString(pub),
		Private:      base64.StdEncoding.EncodeToString(priv),
		KeyAuthority: plane.KeyAuthorityTest,
	}
	raw, err := json.MarshalIndent(kf, "", "  ")
	if err != nil {
		return testKeyFile{}, err
	}
	return kf, os.WriteFile(path, raw, 0o600)
}

func testLoadKey(path string) (testKeyFile, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return testKeyFile{}, err
	}
	var kf testKeyFile
	return kf, json.Unmarshal(raw, &kf)
}

func (kf testKeyFile) TrustRootJSON() ([]byte, error) {
	return json.MarshalIndent(map[string]any{"keys": map[string]string{kf.KeyID: kf.Public}}, "", "  ")
}

func (kf testKeyFile) Attest(payload []byte) (plane.Envelope, string, error) {
	rawPriv, err := base64.StdEncoding.DecodeString(kf.Private)
	if err != nil {
		return plane.Envelope{}, "", err
	}
	sig := ed25519.Sign(ed25519.PrivateKey(rawPriv), plane.PAE(plane.PayloadType, payload))
	env := plane.Envelope{
		PayloadType: plane.PayloadType,
		Payload:     base64.StdEncoding.EncodeToString(payload),
		Signatures: []plane.Signature{{
			KeyID:        kf.KeyID,
			Sig:          base64.StdEncoding.EncodeToString(sig),
			KeyAuthority: kf.KeyAuthority,
		}},
	}
	return env, plane.SpecHash(payload), nil
}

// testServer is one booted server instance: the mux over the shared boot-time
// stores, exactly as main wires them.
type testServer struct {
	t      *testing.T
	mux    *http.ServeMux
	feed   *durable.Store
	istore *idempotency.Store
	specs  *plane.Store
	key    testKeyFile
}

// boot mirrors main's boot path: set INTENT_DATA_DIR (t.Setenv), read it back from
// the environment, open the durable feed and the durable idempotency store ONCE
// over that dir, and build the mux over the shared stores. Call close() to
// simulate process shutdown before re-booting over the same dir.
func boot(t *testing.T, dir string) *testServer {
	t.Helper()
	t.Setenv("INTENT_DATA_DIR", dir)
	dataDir := os.Getenv("INTENT_DATA_DIR")
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
	// Attester key + trust root persist beside the data dir so a re-boot over
	// the same dir sees the same authority (restart tests depend on it).
	keyPath := filepath.Join(dataDir, "attester.key.json")
	var kf testKeyFile
	if _, statErr := os.Stat(keyPath); statErr == nil {
		kf, err = testLoadKey(keyPath)
	} else {
		kf, err = testKeygen(keyPath)
	}
	if err != nil {
		t.Fatalf("attester key: %v", err)
	}
	rootJSON, err := kf.TrustRootJSON()
	if err != nil {
		t.Fatal(err)
	}
	root, err := plane.ParseTrustRoot(rootJSON)
	if err != nil {
		t.Fatal(err)
	}
	specs, err := plane.OpenStore(filepath.Join(dataDir, "specs"), root)
	if err != nil {
		t.Fatalf("spec store: %v", err)
	}
	// Mirror main: the shared live scorer comes from INTENT_SCORER_URL (tests set
	// it via t.Setenv BEFORE boot; unset means zero-config refusal). The wire
	// tests drive terminals via force_scores, so they boot with the unsafe
	// flag ON — exactly the posture the guard exists to make explicit.
	ts := &testServer{t: t, mux: newMux(feed, istore, scorerFromEnv(), specs, true), feed: feed, istore: istore, specs: specs, key: kf}
	t.Cleanup(ts.close) // double Close is a no-op on both stores
	return ts
}

// attest signs and publishes a spec payload, returning its content address —
// the hash a declaration cites. Criteria arrive at the gate ONLY through this.
func (ts *testServer) attest(p plane.SpecPayload) string {
	ts.t.Helper()
	if p.SpecVersion == 0 {
		p.SpecVersion = 1
	}
	if p.ActionClass == "" {
		p.ActionClass = "sample-action"
	}
	if p.Posture == "" {
		p.Posture = plane.PostureEnforce
	}
	raw, err := json.Marshal(p)
	if err != nil {
		ts.t.Fatal(err)
	}
	env, _, err := ts.key.Attest(raw)
	if err != nil {
		ts.t.Fatal(err)
	}
	hash, err := ts.specs.Publish(env)
	if err != nil {
		ts.t.Fatal(err)
	}
	return hash
}

// attestVolatileAlpha publishes the standard one-volatile-criterion spec most
// wire tests use, returning its hash.
func (ts *testServer) attestVolatileAlpha() string {
	return ts.attest(plane.SpecPayload{Criteria: []plane.CriterionSpec{
		{Name: "alpha", Threshold: 1.0, Volatility: "volatile"},
	}})
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

// intentBody builds the plane-amendment request DTO: the wire carries the
// spec HASH (an attested artifact's content address), never criteria. The one
// volatile criterion lives in the published spec; force_scores drives its
// result (guarded probe affordance — the test server boots with the flag on).
func intentBody(seed, key, specHash, declaration, dispatch string) string {
	return fmt.Sprintf(`{
		"episode_seed": %q,
		"idempotency_key": %q,
		"rule_artifact_hash": "rule-hash-1",
		"intent_spec_hash": %q,
		"spec": {
			"idempotency_scope": "per-actor"
		},
		"force_scores": {
			"alpha": {"declaration": %q, "dispatch": %q}
		}
	}`, seed, key, specHash, declaration, dispatch)
}

// intentID computes the deterministic intent ID exactly as the gate does.
func intentID(seed string) string {
	return intent.Intent{EpisodeSeed: seed}.ID()
}

// intentBodyNoForce builds the same request WITHOUT force_scores: the server
// must route scoring to the boot-time shared scorer (CONTRACT.md §2.5).
func intentBodyNoForce(seed, key, specHash string) string {
	return fmt.Sprintf(`{
		"episode_seed": %q,
		"idempotency_key": %q,
		"rule_artifact_hash": "rule-hash-1",
		"intent_spec_hash": %q,
		"spec": {
			"idempotency_scope": "per-actor"
		}
	}`, seed, key, specHash)
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
// fields) is visible via GET /v2/events?type=ACHIEVED — the §5.3 successor to
// the slice-1 "settlement present" assertion.
func TestIntentsAchieved(t *testing.T) {
	ts := boot(t, t.TempDir())
	resp := ts.postIntent(intentBody("seed-achieved", "key-achieved", ts.attestVolatileAlpha(), "PASS", "PASS"))

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
// NO ACHIEVED record in the feed — the §5.3 successor to "settlement nil".
func TestIntentsFailedAtDispatch(t *testing.T) {
	ts := boot(t, t.TempDir())
	rr := ts.do(http.MethodPost, "/v2/intents", intentBody("seed-fad", "key-fad", ts.attestVolatileAlpha(), "PASS", "FAIL"))
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
	if resp.Reason != "volatile-recheck:alpha" {
		t.Fatalf("reason = %q, want %q", resp.Reason, "volatile-recheck:alpha")
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
	ts.postIntent(intentBody("seed-page-1", "key-page-1", ts.attestVolatileAlpha(), "PASS", "PASS"))
	ts.postIntent(intentBody("seed-page-2", "key-page-2", ts.attestVolatileAlpha(), "PASS", "PASS"))

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
// ordered after the volatile RECHECK (§5.3(f)).
func TestIntentEventsOrder(t *testing.T) {
	ts := boot(t, t.TempDir())
	// A second intent first, so the endpoint must actually filter by id.
	ts.postIntent(intentBody("seed-other", "key-other", ts.attestVolatileAlpha(), "PASS", "PASS"))
	resp := ts.postIntent(intentBody("seed-order", "key-order", ts.attestVolatileAlpha(), "PASS", "PASS"))
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

// TestZeroConfigRefusesEverything (CONTRACT.md §2.5): no force_scores
// and no INTENT_SCORER_URL means the shared HTTPScorer has an empty endpoint, so
// every criterion scores Unevaluable and the gate refuses at declaration. The
// zero-config server authorizes nothing.
func TestZeroConfigRefusesEverything(t *testing.T) {
	t.Setenv("INTENT_SCORER_URL", "")
	ts := boot(t, t.TempDir())

	resp := ts.postIntent(intentBodyNoForce("seed-zeroconf", "key-zeroconf", ts.attestVolatileAlpha()))
	if resp.Terminal != string(lifecycle.Failed) {
		t.Fatalf("terminal = %q, want %q (zero-config must refuse)", resp.Terminal, lifecycle.Failed)
	}
	if resp.Reason != "unevaluable:alpha" {
		t.Fatalf("reason = %q, want %q", resp.Reason, "unevaluable:alpha")
	}
	if ev := ts.getEvents("?type=ACHIEVED"); len(ev.Events) != 0 {
		t.Fatalf("zero-config run must leave no ACHIEVED record, got %+v", ev.Events)
	}
}

// TestEmptyCriteriaRefusedOverWire (CONTRACT.md §4.2 step 1b): a declaration whose
// spec carries zero criteria terminates FAILED with the pinned reason — for a
// `"criteria": []` body AND a body omitting the field entirely (both decode to
// zero criteria; the gate must not distinguish them). Deliberately run on a
// ZERO-CONFIG server with no force_scores: before the thin-spec defense, this
// exact request ACHIEVEs on the server that "authorizes nothing" — the scorer
// is never consulted, so no fail-closed layer ever fires.
func TestEmptyCriteriaRefusedOverWire(t *testing.T) {
	t.Setenv("INTENT_SCORER_URL", "")
	ts := boot(t, t.TempDir())

	// The wire can no longer even EXPRESS criteria; the thin-spec case is now
	// an ATTESTED spec whose payload carries zero criteria (empty array or
	// field absent — both marshal to the same refusal), signed by the real
	// attester. Attested-but-thin still refuses: attestation does not launder
	// vacuity.
	thinHash := ts.attest(plane.SpecPayload{Criteria: []plane.CriterionSpec{}})
	thinBody := intentBodyNoForce("seed-thin-wire-a", "key-thin-wire-a", thinHash)

	for name, body := range map[string]string{
		"attested-but-thin": thinBody,
	} {
		t.Run(name, func(t *testing.T) {
			resp := ts.postIntent(body)
			if resp.Terminal == string(lifecycle.Achieved) {
				t.Fatal("a zero-criteria declaration must never reach ACHIEVED (vacuous grant over the wire)")
			}
			if resp.Terminal != string(lifecycle.Failed) {
				t.Fatalf("terminal = %q, want %q", resp.Terminal, lifecycle.Failed)
			}
			if resp.Reason != "unevaluable:empty-criteria" {
				t.Fatalf("reason = %q, want %q", resp.Reason, "unevaluable:empty-criteria")
			}
			if resp.AchievedSeq != 0 {
				t.Fatalf("achieved_seq = %d, want absent/0 on FAILED", resp.AchievedSeq)
			}
		})
	}
	if ev := ts.getEvents("?type=ACHIEVED"); len(ev.Events) != 0 {
		t.Fatalf("thin-spec declarations must leave no ACHIEVED record, got %+v", ev.Events)
	}
}

// TestInvalidVolatilityRefusedOverWire (CONTRACT.md §4.2 step 1b): a criterion
// with an unknown volatility string — a typo, or the field omitted (decodes to
// "") — is refused with the pinned reason instead of being silently treated as
// stable and skipping the dispatch-edge re-verify.
func TestInvalidVolatilityRefusedOverWire(t *testing.T) {
	t.Setenv("INTENT_SCORER_URL", "")
	ts := boot(t, t.TempDir())

	// The typo now lives in the ATTESTED payload: the attester signed a spec
	// whose volatility string is wrong (or absent). Signature verification
	// passes — the bytes are exactly what was signed — and the gate still
	// refuses: attestation vouches for provenance, not for shape; shape is
	// the gate's own defense.
	typoHash := ts.attest(plane.SpecPayload{Criteria: []plane.CriterionSpec{
		{Name: "alpha", Threshold: 1.0, Volatility: "volatil"},
	}})
	omittedHash := ts.attest(plane.SpecPayload{Criteria: []plane.CriterionSpec{
		{Name: "alpha", Threshold: 1.0},
	}})
	cases := map[string]string{
		"typo":    intentBodyNoForce("seed-vol-wire-a", "key-vol-wire-a", typoHash),
		"omitted": intentBodyNoForce("seed-vol-wire-b", "key-vol-wire-b", omittedHash),
	}
	for name, b := range cases {
		t.Run(name, func(t *testing.T) {
			resp := ts.postIntent(b)
			if resp.Terminal != string(lifecycle.Failed) {
				t.Fatalf("terminal = %q, want %q", resp.Terminal, lifecycle.Failed)
			}
			if resp.Reason != "unevaluable:invalid-volatility:alpha" {
				t.Fatalf("reason = %q, want %q", resp.Reason, "unevaluable:invalid-volatility:alpha")
			}
		})
	}
	if ev := ts.getEvents("?type=ACHIEVED"); len(ev.Events) != 0 {
		t.Fatalf("invalid-volatility declarations must leave no ACHIEVED record, got %+v", ev.Events)
	}
}

// TestLiveScorerDrivesTerminal (CONTRACT.md §2.5): with no force_scores and
// INTENT_SCORER_URL pointing at an httptest scorer, the terminal follows the
// scorer's answers — and the calls actually cross the HTTP seam.
func TestLiveScorerDrivesTerminal(t *testing.T) {
	t.Run("scorer PASS everywhere yields ACHIEVED", func(t *testing.T) {
		calls := 0
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"result":"PASS","basis":"test scorer"}`))
		}))
		defer srv.Close()
		t.Setenv("INTENT_SCORER_URL", srv.URL)
		ts := boot(t, t.TempDir())

		resp := ts.postIntent(intentBodyNoForce("seed-live-pass", "key-live-pass", ts.attestVolatileAlpha()))
		if resp.Terminal != string(lifecycle.Achieved) {
			t.Fatalf("terminal = %q, want ACHIEVED", resp.Terminal)
		}
		// One volatile criterion: declaration + dispatch-edge recheck must BOTH
		// cross the wire (spec invariant 3 over the live seam).
		if calls != 2 {
			t.Fatalf("scorer saw %d calls, want 2 (declaration + volatile recheck)", calls)
		}
	})

	t.Run("scorer FAIL at declaration yields FAILED", func(t *testing.T) {
		calls := 0
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"result":"FAIL","basis":"test scorer"}`))
		}))
		defer srv.Close()
		t.Setenv("INTENT_SCORER_URL", srv.URL)
		ts := boot(t, t.TempDir())

		resp := ts.postIntent(intentBodyNoForce("seed-live-fail", "key-live-fail", ts.attestVolatileAlpha()))
		if resp.Terminal != string(lifecycle.Failed) {
			t.Fatalf("terminal = %q, want FAILED", resp.Terminal)
		}
		if calls == 0 {
			t.Fatalf("the scorer was never called: terminal did not come from the HTTP seam")
		}
		if ev := ts.getEvents("?type=ACHIEVED"); len(ev.Events) != 0 {
			t.Fatalf("failed run must leave no ACHIEVED record, got %+v", ev.Events)
		}
	})
}

// TestForceScoresStillWins (CONTRACT.md §2.4): force_scores present must
// select the forced scorer even when INTENT_SCORER_URL is configured — the
// documented test affordance is preserved verbatim.
func TestForceScoresStillWins(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("live scorer must NOT be called when force_scores is present")
	}))
	defer srv.Close()
	t.Setenv("INTENT_SCORER_URL", srv.URL)
	ts := boot(t, t.TempDir())

	resp := ts.postIntent(intentBody("seed-force-wins", "key-force-wins", ts.attestVolatileAlpha(), "PASS", "PASS"))
	if resp.Terminal != string(lifecycle.Achieved) {
		t.Fatalf("terminal = %q, want ACHIEVED via force_scores", resp.Terminal)
	}
}

// TestDeterminismConditionalOnScores (CONTRACT.md §5.4 claim 10): given the
// same score per (criterion, phase), the gate's events and TrajectoryHash are
// byte-identical whether the forced scorer or the live HTTPScorer produced
// them — and the scorer's free-text basis appears NOWHERE in the durable feed.
func TestDeterminismConditionalOnScores(t *testing.T) {
	const poison = "POISON-BASIS-MARKER-must-never-be-durable"

	// Run A: forced scorer, PASS at both phases.
	t.Setenv("INTENT_SCORER_URL", "")
	tsA := boot(t, t.TempDir())
	respA := tsA.postIntent(intentBody("seed-det", "key-det", tsA.attestVolatileAlpha(), "PASS", "PASS"))
	eventsA := tsA.getEvents("?since=0").Events
	tsA.close()

	// Run B: live httptest scorer answering the SAME scores, with a poisoned
	// basis that must stay observability-only.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":"PASS","basis":"` + poison + `"}`))
	}))
	defer srv.Close()
	t.Setenv("INTENT_SCORER_URL", srv.URL)
	tsB := boot(t, t.TempDir())
	respB := tsB.postIntent(intentBodyNoForce("seed-det", "key-det", tsB.attestVolatileAlpha()))
	eventsB := tsB.getEvents("?since=0").Events

	if respA.Terminal != respB.Terminal || respA.TrajectoryHash != respB.TrajectoryHash {
		t.Fatalf("terminal/hash diverged across scorers:\n forced: %+v\n live:   %+v", respA, respB)
	}
	if len(eventsA) != len(eventsB) {
		t.Fatalf("event counts diverged: forced %d, live %d", len(eventsA), len(eventsB))
	}
	for i := range eventsA {
		a, b := eventsA[i], eventsB[i]
		// scorer_id is the ONE field whose purpose is to differ across
		// scorers: it witnesses WHICH authority answered, so a forced grant
		// is never byte-indistinguishable from a live-scored one. Like
		// GlobalSeq it is feed-level and hash-exempt; determinism-conditional-
		// on-scores holds over everything else, byte for byte.
		//
		// The witness must be POSITIVELY present on every scoring event
		// (CONTRACT.md §5.3 row (h)): a skeptic pass proved the whole suite
		// stayed green with the stamping deleted, because empty==empty
		// satisfied the equality below. Assert presence BEFORE zeroing.
		if a.Type == "SCORED" || a.Type == "RECHECK" {
			if a.ScorerID != "forced" {
				t.Fatalf("event %d (%s): forced-run scorer_id = %q, want \"forced\" — the witness is missing", i, a.Type, a.ScorerID)
			}
			if b.ScorerID != "live" {
				t.Fatalf("event %d (%s): live-run scorer_id = %q, want \"live\" — the witness is missing", i, b.Type, b.ScorerID)
			}
			a.ScorerID, b.ScorerID = "", ""
		}
		if a != b {
			t.Fatalf("event %d diverged across scorers:\n forced: %+v\n live:   %+v", i, eventsA[i], eventsB[i])
		}
	}

	// basis must be nowhere in the durable feed — not as a field, not as text.
	raw, err := json.Marshal(eventsB)
	if err != nil {
		t.Fatalf("marshal events: %v", err)
	}
	if strings.Contains(string(raw), poison) || strings.Contains(string(raw), `"basis"`) {
		t.Fatalf("basis leaked into the durable feed: %s", raw)
	}
}

// TestStableOnceVolatileTwiceAcrossWire (CONTRACT.md §5.4 claim 11): a
// counting scorer sees exactly one declaration call per criterion and exactly
// one extra dispatch call per VOLATILE criterion — spec invariant 3 holds
// across the live seam, as an exact call multiset.
func TestStableOnceVolatileTwiceAcrossWire(t *testing.T) {
	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Criterion string `json:"criterion"`
			Phase     string `json:"phase"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		calls = append(calls, req.Criterion+"/"+req.Phase)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":"PASS"}`))
	}))
	defer srv.Close()
	t.Setenv("INTENT_SCORER_URL", srv.URL)
	ts := boot(t, t.TempDir())

	twoCritHash := ts.attest(plane.SpecPayload{Criteria: []plane.CriterionSpec{
		{Name: "alpha", Threshold: 1.0, Volatility: "stable"},
		{Name: "beta", Threshold: 1.0, Volatility: "volatile"},
	}})
	resp := ts.postIntent(intentBodyNoForce("seed-multiset", "key-multiset", twoCritHash))
	if resp.Terminal != string(lifecycle.Achieved) {
		t.Fatalf("terminal = %q, want ACHIEVED", resp.Terminal)
	}

	want := map[string]int{
		"alpha/declaration": 1, // stable: once, never re-verified
		"beta/declaration":  1,
		"beta/dispatch":     1, // volatile: exactly one dispatch-edge recheck
	}
	got := map[string]int{}
	for _, c := range calls {
		got[c]++
	}
	if len(got) != len(want) {
		t.Fatalf("call multiset = %v, want %v", got, want)
	}
	for k, n := range want {
		if got[k] != n {
			t.Fatalf("call %q seen %d times, want %d (full multiset %v)", k, got[k], n, got)
		}
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
	first := s1.postIntent(intentBody("seed-restart-1", "key-restart", s1.attestVolatileAlpha(), "PASS", "PASS"))
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
	// A DIFFERENT attested spec (distinct threshold, distinct hash) with the
	// SAME key: still a collision — the key, not the spec, is the identity.
	otherHash := s2.attest(plane.SpecPayload{Criteria: []plane.CriterionSpec{
		{Name: "alpha", Threshold: 2.0, Volatility: "volatile"},
	}})
	second := s2.postIntent(intentBody("seed-restart-2", "key-restart", otherHash, "PASS", "PASS"))
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
