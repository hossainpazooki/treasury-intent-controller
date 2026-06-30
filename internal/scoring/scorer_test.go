package scoring

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

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
