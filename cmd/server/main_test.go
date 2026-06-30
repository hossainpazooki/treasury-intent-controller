package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pazooki/treasury-intent-controller/internal/lifecycle"
)

// serve drives the mux through net/http/httptest WITHOUT binding a real port
// (httptest.NewRecorder + ServeHTTP, no listener), and decodes the JSON response
// when the body is JSON.
func serve(t *testing.T, method, target, body string) (*httptest.ResponseRecorder, intentResponse) {
	t.Helper()
	var r *strings.Reader
	if body != "" {
		r = strings.NewReader(body)
	} else {
		r = strings.NewReader("")
	}
	req := httptest.NewRequest(method, target, r)
	rr := httptest.NewRecorder()
	newMux().ServeHTTP(rr, req)

	var resp intentResponse
	if ct := rr.Header().Get("Content-Type"); strings.HasPrefix(ct, "application/json") {
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode response: %v (body=%q)", err, rr.Body.String())
		}
	}
	return rr, resp
}

func TestHealthz(t *testing.T) {
	rr, _ := serve(t, http.MethodGet, "/healthz", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("healthz status = %d, want 200", rr.Code)
	}
	if got := rr.Body.String(); got != "ok" {
		t.Fatalf("healthz body = %q, want %q", got, "ok")
	}
}

// TestIntentsAchieved: a volatile criterion that passes at BOTH declaration and
// the dispatch edge, with a valid idempotency key, reaches ACHIEVED and carries a
// settlement event.
func TestIntentsAchieved(t *testing.T) {
	body := `{
		"episode_seed": "seed-achieved",
		"idempotency_key": "key-achieved",
		"intent_spec_hash": "spec-hash-1",
		"spec": {
			"action_class": "payment",
			"idempotency_scope": "payer",
			"criteria": [
				{"name": "balance", "threshold": 1.0, "volatility": "volatile"}
			]
		},
		"force_scores": {
			"balance": {"declaration": "PASS", "dispatch": "PASS"}
		}
	}`

	rr, resp := serve(t, http.MethodPost, "/v2/intents", body)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%q)", rr.Code, rr.Body.String())
	}
	if resp.Terminal != string(lifecycle.Achieved) {
		t.Fatalf("terminal = %q, want %q", resp.Terminal, lifecycle.Achieved)
	}
	if resp.Settlement == nil {
		t.Fatalf("ACHIEVED must carry a settlement event, got nil")
	}
	if resp.Settlement.Key != "key-achieved" {
		t.Fatalf("settlement key = %q, want %q", resp.Settlement.Key, "key-achieved")
	}
	if resp.Settlement.Payload == "" {
		t.Fatalf("settlement payload must be non-empty (deterministic)")
	}
	if resp.TrajectoryHash == "" {
		t.Fatalf("trajectory_hash must be non-empty")
	}
}

// TestIntentsFailedAtDispatch: a volatile criterion that passes at declaration
// but FAILs at the dispatch-edge re-verify reaches FAILED_AT_DISPATCH and carries
// NO settlement event.
func TestIntentsFailedAtDispatch(t *testing.T) {
	body := `{
		"episode_seed": "seed-fad",
		"idempotency_key": "key-fad",
		"intent_spec_hash": "spec-hash-2",
		"spec": {
			"action_class": "payment",
			"idempotency_scope": "payer",
			"criteria": [
				{"name": "balance", "threshold": 1.0, "volatility": "volatile"}
			]
		},
		"force_scores": {
			"balance": {"declaration": "PASS", "dispatch": "FAIL"}
		}
	}`

	rr, resp := serve(t, http.MethodPost, "/v2/intents", body)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%q)", rr.Code, rr.Body.String())
	}
	if resp.Terminal != string(lifecycle.FailedAtDispatch) {
		t.Fatalf("terminal = %q, want %q", resp.Terminal, lifecycle.FailedAtDispatch)
	}
	if resp.Settlement != nil {
		t.Fatalf("FAILED_AT_DISPATCH must NOT carry a settlement event, got %+v", resp.Settlement)
	}
}
