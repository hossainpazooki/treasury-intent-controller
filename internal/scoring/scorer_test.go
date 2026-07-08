package scoring

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pazooki/treasury-intent-controller/internal/intent"
)

func testIntent() intent.Intent {
	return intent.Intent{
		EpisodeSeed: "seed-1",
		Spec: intent.IntentSpecParams{
			ActionClass: "payment",
			Criteria:    []intent.Criterion{{Name: "balance", Threshold: 1.0, Volatility: intent.Stable}},
		},
		IdempotencyKey: "k1",
	}
}

func testCriterion() intent.Criterion {
	return intent.Criterion{Name: "balance", Threshold: 1.0, Volatility: intent.Stable}
}

func TestScoreString(t *testing.T) {
	cases := []struct {
		s    Score
		want string
	}{
		{Pass, "PASS"},
		{Fail, "FAIL"},
		{Unevaluable, "UNEVALUABLE"},
		{Score(99), "UNEVALUABLE"}, // out-of-range fails closed
	}
	for _, tc := range cases {
		if got := tc.s.String(); got != tc.want {
			t.Errorf("Score(%d).String() = %q, want %q", int(tc.s), got, tc.want)
		}
	}
}

func TestHTTPScorerDeadEndpoint(t *testing.T) {
	// A server that is created then immediately closed yields a dead endpoint:
	// the transport error MUST map to Unevaluable (fail-closed), never Pass.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	endpoint := srv.URL + "/ml/evaluate"
	srv.Close()

	h := &HTTPScorer{Endpoint: endpoint, Client: srv.Client()}
	got := h.Score(context.Background(), testIntent(), testCriterion(), intent.Declaration)
	if got != Unevaluable {
		t.Fatalf("dead endpoint: got %v, want UNEVALUABLE", got)
	}
}

func TestHTTPScorer500(t *testing.T) {
	// A non-2xx response MUST map to Unevaluable (fail-closed), never Pass, even
	// if the body happens to contain a PASS-shaped payload.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"result":"PASS"}`))
	}))
	defer srv.Close()

	h := &HTTPScorer{Endpoint: srv.URL + "/ml/evaluate", Client: srv.Client()}
	got := h.Score(context.Background(), testIntent(), testCriterion(), intent.Declaration)
	if got != Unevaluable {
		t.Fatalf("500 response: got %v, want UNEVALUABLE", got)
	}
}

func TestHTTPScorerResultMapping(t *testing.T) {
	cases := []struct {
		result string
		want   Score
	}{
		{"PASS", Pass},
		{"FAIL", Fail},
		{"UNEVALUABLE", Unevaluable},
		{"garbage", Unevaluable}, // unknown result fails closed
	}
	for _, tc := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"result":"` + tc.result + `"}`))
		}))
		h := &HTTPScorer{Endpoint: srv.URL + "/ml/evaluate", Client: srv.Client()}
		got := h.Score(context.Background(), testIntent(), testCriterion(), intent.Declaration)
		srv.Close()
		if got != tc.want {
			t.Errorf("result %q: got %v, want %v", tc.result, got, tc.want)
		}
	}
}

func TestHTTPScorerBadJSON(t *testing.T) {
	// A 2xx response with an undecodable body fails closed.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`not json`))
	}))
	defer srv.Close()

	h := &HTTPScorer{Endpoint: srv.URL + "/ml/evaluate", Client: srv.Client()}
	got := h.Score(context.Background(), testIntent(), testCriterion(), intent.Declaration)
	if got != Unevaluable {
		t.Fatalf("bad JSON: got %v, want UNEVALUABLE", got)
	}
}

func TestFakeScorerDefaultPass(t *testing.T) {
	// An absent key defaults to Pass.
	f := &FakeScorer{}
	got := f.Score(context.Background(), testIntent(), testCriterion(), intent.Declaration)
	if got != Pass {
		t.Fatalf("absent key: got %v, want PASS", got)
	}
}

func TestFakeScorerConfiguredResult(t *testing.T) {
	f := &FakeScorer{Results: map[ScoreKey]Score{
		{Criterion: "balance", Phase: intent.Declaration}: Fail,
		{Criterion: "balance", Phase: intent.Dispatch}:    Unevaluable,
	}}
	if got := f.Score(context.Background(), testIntent(), testCriterion(), intent.Declaration); got != Fail {
		t.Errorf("declaration: got %v, want FAIL", got)
	}
	if got := f.Score(context.Background(), testIntent(), testCriterion(), intent.Dispatch); got != Unevaluable {
		t.Errorf("dispatch: got %v, want UNEVALUABLE", got)
	}
}

func TestFakeScorerCallsRecording(t *testing.T) {
	f := &FakeScorer{}
	c := testCriterion()
	f.Score(context.Background(), testIntent(), c, intent.Declaration)
	f.Score(context.Background(), testIntent(), c, intent.Dispatch)

	want := []ScoreKey{
		{Criterion: "balance", Phase: intent.Declaration},
		{Criterion: "balance", Phase: intent.Dispatch},
	}
	if len(f.Calls) != len(want) {
		t.Fatalf("Calls length = %d, want %d (%v)", len(f.Calls), len(want), f.Calls)
	}
	for i := range want {
		if f.Calls[i] != want[i] {
			t.Errorf("Calls[%d] = %v, want %v", i, f.Calls[i], want[i])
		}
	}
}

// FakeScorer must satisfy the Scorer interface.
var _ Scorer = (*FakeScorer)(nil)

// HTTPScorer must satisfy the Scorer interface.
var _ Scorer = (*HTTPScorer)(nil)

// --- CONTRACT-SCORER §S.2: constructor + full client fail-closed matrix ---

func TestNewHTTPScorerDefaults(t *testing.T) {
	// NewHTTPScorer pins the endpoint and bounds every call at DefaultTimeout.
	if DefaultTimeout != 5*time.Second {
		t.Fatalf("DefaultTimeout = %v, want 5s (CONTRACT-SCORER §S.0)", DefaultTimeout)
	}
	h := NewHTTPScorer("http://example.invalid/ml/evaluate")
	if h.Endpoint != "http://example.invalid/ml/evaluate" {
		t.Errorf("Endpoint = %q, want the constructor argument", h.Endpoint)
	}
	if h.Client == nil || h.Client.Timeout != DefaultTimeout {
		t.Errorf("Client timeout not pinned to DefaultTimeout: %+v", h.Client)
	}
}

func TestHTTPScorerFailClosedMatrix(t *testing.T) {
	// Every remaining row of the §S.1 client matrix maps to Unevaluable. The
	// refused/500/garbage/unknown-result rows are covered by the slice-1 tests
	// above (kept verbatim per §V2.6).
	t.Run("empty endpoint", func(t *testing.T) {
		h := NewHTTPScorer("")
		if got := h.Score(context.Background(), testIntent(), testCriterion(), intent.Declaration); got != Unevaluable {
			t.Fatalf("empty endpoint: got %v, want UNEVALUABLE", got)
		}
	})

	t.Run("context cancelled", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"result":"PASS"}`))
		}))
		defer srv.Close()
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // cancelled before the call: the transport error must fail closed
		h := &HTTPScorer{Endpoint: srv.URL + "/ml/evaluate", Client: srv.Client()}
		if got := h.Score(ctx, testIntent(), testCriterion(), intent.Declaration); got != Unevaluable {
			t.Fatalf("cancelled ctx: got %v, want UNEVALUABLE", got)
		}
	})

	for _, status := range []int{http.StatusBadRequest, http.StatusUnprocessableEntity, http.StatusServiceUnavailable} {
		status := status
		t.Run(http.StatusText(status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"result":"PASS"}`))
			}))
			defer srv.Close()
			h := &HTTPScorer{Endpoint: srv.URL + "/ml/evaluate", Client: srv.Client()}
			if got := h.Score(context.Background(), testIntent(), testCriterion(), intent.Declaration); got != Unevaluable {
				t.Fatalf("status %d: got %v, want UNEVALUABLE", status, got)
			}
		})
	}

	t.Run("truncated body", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"result":`)) // cut mid-value
		}))
		defer srv.Close()
		h := &HTTPScorer{Endpoint: srv.URL + "/ml/evaluate", Client: srv.Client()}
		if got := h.Score(context.Background(), testIntent(), testCriterion(), intent.Declaration); got != Unevaluable {
			t.Fatalf("truncated body: got %v, want UNEVALUABLE", got)
		}
	})

	t.Run("result field absent", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"basis":"no result at all"}`))
		}))
		defer srv.Close()
		h := &HTTPScorer{Endpoint: srv.URL + "/ml/evaluate", Client: srv.Client()}
		if got := h.Score(context.Background(), testIntent(), testCriterion(), intent.Declaration); got != Unevaluable {
			t.Fatalf("absent result: got %v, want UNEVALUABLE", got)
		}
	})
}

func TestHTTPScorerRequestCarriesAllFields(t *testing.T) {
	// Happy path: the marshaled request carries all seven §S.1 fields, and the
	// response Basis is decoded but DISCARDED (observability only — it must not
	// influence the Score and never reaches the caller).
	var captured []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":"PASS","basis":"balance=250.00 >= 100.00"}`))
	}))
	defer srv.Close()

	i := testIntent()
	i.RuleArtifactHash = "rule-hash-opaque"
	i.IntentSpecHash = "spec-hash-opaque"
	c := intent.Criterion{Name: "balance", Threshold: 100, Volatility: intent.Volatile}

	h := &HTTPScorer{Endpoint: srv.URL + "/ml/evaluate", Client: srv.Client()}
	if got := h.Score(context.Background(), i, c, intent.Dispatch); got != Pass {
		t.Fatalf("happy path: got %v, want PASS", got)
	}

	var req map[string]any
	if err := json.Unmarshal(captured, &req); err != nil {
		t.Fatalf("request body not JSON: %v\n%s", err, captured)
	}
	want := map[string]any{
		"intent_id":          i.ID(),
		"criterion":          "balance",
		"threshold":          100.0,
		"phase":              "dispatch",
		"volatility":         "volatile",
		"rule_artifact_hash": "rule-hash-opaque",
		"intent_spec_hash":   "spec-hash-opaque",
	}
	if len(req) != len(want) {
		t.Errorf("request has %d fields, want %d: %s", len(req), len(want), captured)
	}
	for k, v := range want {
		if req[k] != v {
			t.Errorf("request[%q] = %v, want %v", k, req[k], v)
		}
	}
}

// --- CONTRACT-SCORER §S.5: cross-language golden fixtures ---

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "contract", "scorer", name))
	if err != nil {
		t.Fatalf("fixture %s: %v", name, err)
	}
	return bytes.TrimSpace(b)
}

func TestRequestFixturesByteIdentical(t *testing.T) {
	// EvalRequest must marshal byte-identically to each request fixture — the
	// fixture bytes ARE the wire contract shared with the Python service.
	cases := []struct {
		name string
		req  EvalRequest
	}{
		{"request-pass.json", EvalRequest{IntentID: "itx-pass-0001", Criterion: "balance", Threshold: 100, Phase: "declaration", Volatility: "stable"}},
		{"request-fail.json", EvalRequest{IntentID: "itx-fail-0001", Criterion: "balance", Threshold: 100, Phase: "declaration", Volatility: "stable"}},
		{"request-unevaluable-unknown-criterion.json", EvalRequest{IntentID: "itx-unev-0001", Criterion: "nonexistent", Threshold: 10, Phase: "declaration", Volatility: "stable"}},
		{"request-volatile-dispatch.json", EvalRequest{IntentID: "itx-vol-0001", Criterion: "fx_rate", Threshold: 1.25, Phase: "dispatch", Volatility: "volatile"}},
		{"request-hashes-present.json", EvalRequest{
			IntentID: "itx-hash-0001", Criterion: "balance", Threshold: 100, Phase: "declaration", Volatility: "stable",
			RuleArtifactHash: "13a414cf7f6b25c6b6049c0953a83ff5697044aabafbd44b87e87fc4ed90f8a9",
			IntentSpecHash:   "c7a36959bfaafc03e0abfb86fce7e1c0c6efebc812b55f83313724c3d024dc51",
		}},
	}
	for _, tc := range cases {
		got, err := json.Marshal(tc.req)
		if err != nil {
			t.Fatalf("%s: marshal: %v", tc.name, err)
		}
		if want := fixture(t, tc.name); !bytes.Equal(got, want) {
			t.Errorf("%s drifted:\n got: %s\nwant: %s", tc.name, got, want)
		}
	}
}

func TestResponseFixturesDriveScore(t *testing.T) {
	// Each response fixture, served verbatim, must decode to the expected Score
	// through the REAL client path (mapping logic, not just struct decode).
	cases := []struct {
		name string
		want Score
	}{
		{"response-pass.json", Pass},
		{"response-fail.json", Fail},
		{"response-unevaluable-unknown-criterion.json", Unevaluable},
		{"response-volatile-dispatch.json", Pass},
		{"response-hashes-present.json", Pass},
	}
	for _, tc := range cases {
		body := fixture(t, tc.name)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(body)
		}))
		h := &HTTPScorer{Endpoint: srv.URL + "/ml/evaluate", Client: srv.Client()}
		got := h.Score(context.Background(), testIntent(), testCriterion(), intent.Declaration)
		srv.Close()
		if got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}
