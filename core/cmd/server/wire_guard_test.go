package main

// Wire-guard pins (adopted from the 2026-08-05 skeptic pass, which proved
// these paths real-but-untested): the loud-400 contract for unknown fields
// and the force_scores boot guard. A guard nobody tests is a guard that can
// be deleted with every gate staying green.

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hossainpazooki/intent-plane/core/internal/durable"
	"github.com/hossainpazooki/intent-plane/core/internal/idempotency"
	"github.com/hossainpazooki/intent-plane/plane"
)

// Old wire shape: criteria nested under spec. CONTRACT.md §2.2 claims
// DisallowUnknownFields 400s it loudly — P1's type-level close.
func TestOldWireShapeCriteria400(t *testing.T) {
	ts := boot(t, t.TempDir())
	body := `{
		"episode_seed": "seed-old-shape",
		"idempotency_key": "key-old-shape",
		"rule_artifact_hash": "rule-hash-1",
		"intent_spec_hash": "deadbeef",
		"spec": {
			"idempotency_scope": "per-actor",
			"criteria": [{"name": "alpha", "threshold": 1.0, "volatility": "volatile"}]
		}
	}`
	rr := ts.do(http.MethodPost, "/v2/intents", body)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("old wire shape (spec.criteria) status = %d, want 400 (body=%q)", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "unknown field") {
		t.Fatalf("400 body does not name the unknown field: %q", rr.Body.String())
	}
}

// Top-level criteria field: also an unknown field, also must 400.
func TestTopLevelCriteria400(t *testing.T) {
	ts := boot(t, t.TempDir())
	body := `{
		"episode_seed": "seed-top-shape",
		"idempotency_key": "key-top-shape",
		"rule_artifact_hash": "rule-hash-1",
		"intent_spec_hash": "deadbeef",
		"criteria": [{"name": "alpha"}],
		"spec": {"idempotency_scope": "per-actor"}
	}`
	rr := ts.do(http.MethodPost, "/v2/intents", body)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("top-level criteria status = %d, want 400 (body=%q)", rr.Code, rr.Body.String())
	}
}

// force_scores without the unsafe boot flag must 400 loudly, naming the flag
// (CONTRACT.md §2.5) — including the empty-map form, which is non-nil after
// decode. The shared test server boots allowForce=true (it drives terminals
// via force_scores), so this is the one place the OFF posture is exercised.
func TestForceScoresRefusedWithoutFlag(t *testing.T) {
	dir := t.TempDir()
	feed, err := durable.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer feed.Close()
	istore, err := idempotency.OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer istore.Close()
	specs, err := plane.OpenStore(filepath.Join(dir, "specs"), plane.TrustRoot{})
	if err != nil {
		t.Fatal(err)
	}
	mux := newMux(feed, istore, scorerFromEnv(), specs, false) // allowForce OFF

	body := `{
		"episode_seed": "seed-guard",
		"idempotency_key": "key-guard",
		"rule_artifact_hash": "rule-hash-1",
		"intent_spec_hash": "deadbeef",
		"spec": {"idempotency_scope": "per-actor"},
		"force_scores": {"alpha": {"declaration": "PASS", "dispatch": "PASS"}}
	}`
	req := httptest.NewRequest(http.MethodPost, "/v2/intents", strings.NewReader(body))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("force_scores without flag status = %d, want 400 (body=%q)", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "INTENT_UNSAFE_FORCE_SCORES") {
		t.Fatalf("guard 400 body does not name the flag: %q", rr.Body.String())
	}
	empty := strings.Replace(body, `{"alpha": {"declaration": "PASS", "dispatch": "PASS"}}`, `{}`, 1)
	req2 := httptest.NewRequest(http.MethodPost, "/v2/intents", strings.NewReader(empty))
	rr2 := httptest.NewRecorder()
	mux.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusBadRequest {
		t.Fatalf("empty force_scores map without flag status = %d, want 400 (body=%q)", rr2.Code, rr2.Body.String())
	}
}
