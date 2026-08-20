// Package scoring is the single scoring authority for intent criteria.
//
// A criterion scores Pass, Fail, or Unevaluable. The gate treats Unevaluable as
// fail-closed: any transport/timeout/decode/non-2xx error MUST surface as
// Unevaluable, never as a silent pass.
package scoring

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/hossainpazooki/intent-plane/core/internal/intent"
)

// DefaultTimeout bounds one /ml/evaluate call. A slower scorer is Unevaluable
// (CONTRACT.md §2.4: a timeout is a transport error like any other).
const DefaultTimeout = 5 * time.Second

// Score is the tri-state result of scoring a criterion.
type Score int

const (
	Pass Score = iota
	Fail
	Unevaluable
)

// String renders the score as "PASS","FAIL","UNEVALUABLE".
func (s Score) String() string {
	switch s {
	case Pass:
		return "PASS"
	case Fail:
		return "FAIL"
	case Unevaluable:
		return "UNEVALUABLE"
	default:
		return "UNEVALUABLE"
	}
}

// Scorer scores ONE named criterion for an intent in a given phase.
type Scorer interface {
	Score(ctx context.Context, i intent.Intent, c intent.Criterion, phase intent.Phase) Score
}

// HTTPScorer calls the Python "/ml/evaluate" endpoint. On any error it returns
// Unevaluable (fail-closed).
type HTTPScorer struct {
	Endpoint string
	Client   *http.Client
}

// NewHTTPScorer returns an HTTPScorer whose client times out at DefaultTimeout.
// An empty endpoint yields a scorer whose every Score is Unevaluable — the
// zero-config server authorizes nothing (CONTRACT.md §2.5).
func NewHTTPScorer(endpoint string) *HTTPScorer {
	return &HTTPScorer{
		Endpoint: endpoint,
		Client:   &http.Client{Timeout: DefaultTimeout, CheckRedirect: noRedirect},
	}
}

// noRedirect refuses to follow a 3xx from the scorer (CONTRACT.md §2.4).
// A followed redirect sends the criterion evaluation to an origin that is
// NOT the configured scorer — and with 301/302/303 the POST is downgraded
// to a body-less GET, so that origin is never even told which criterion it
// is answering about. Its `{"result":"PASS"}` would then bind as a real
// criterion PASS. Returning ErrUseLastResponse hands the 3xx back
// unfollowed, and the non-2xx check below fails it closed to Unevaluable.
// Measured 2026-08-20 before this existed: all five redirect codes scored
// Pass. Do not "simplify" this away.
func noRedirect(*http.Request, []*http.Request) error {
	return http.ErrUseLastResponse
}

// Score POSTs an EvalRequest to the configured endpoint and maps the response
// result to a Score. ANY transport error, non-2xx status, or decode failure maps
// to Unevaluable (fail-closed) — never a silent pass.
func (h *HTTPScorer) Score(ctx context.Context, i intent.Intent, c intent.Criterion, phase intent.Phase) Score {
	body, err := json.Marshal(EvalRequest{
		IntentID:         i.ID(),
		Criterion:        c.Name,
		Threshold:        c.Threshold,
		Phase:            string(phase),
		Volatility:       string(c.Volatility),
		RuleArtifactHash: i.RuleArtifactHash,
		IntentSpecHash:   i.IntentSpecHash,
	})
	if err != nil {
		return Unevaluable
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.Endpoint, bytes.NewReader(body))
	if err != nil {
		return Unevaluable
	}
	req.Header.Set("Content-Type", "application/json")

	client := h.Client
	if client == nil {
		// NOT http.DefaultClient: it follows redirects, which would reopen
		// the fail-open noRedirect exists to close for a zero-value scorer.
		client = &http.Client{Timeout: DefaultTimeout, CheckRedirect: noRedirect}
	}

	resp, err := client.Do(req)
	if err != nil {
		return Unevaluable
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Unevaluable
	}

	var out EvalResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return Unevaluable
	}

	switch out.Result {
	case "PASS":
		return Pass
	case "FAIL":
		return Fail
	default:
		return Unevaluable
	}
}

// EvalRequest is the /ml/evaluate request JSON contract (CONTRACT.md §2.4;
// additive evolution only — nothing renamed, retyped, or removed).
type EvalRequest struct {
	IntentID         string  `json:"intent_id"`
	Criterion        string  `json:"criterion"`
	Threshold        float64 `json:"threshold"`
	Phase            string  `json:"phase"`
	Volatility       string  `json:"volatility"`                   // "stable" | "volatile"
	RuleArtifactHash string  `json:"rule_artifact_hash,omitempty"` // opaque passthrough
	IntentSpecHash   string  `json:"intent_spec_hash,omitempty"`   // opaque passthrough
}

// EvalResponse is the /ml/evaluate response JSON contract. Basis is
// observability only: it MUST NEVER enter the audit log, the durable feed, or
// any hash (CONTRACT.md §2.4).
type EvalResponse struct {
	Result string `json:"result"`
	Basis  string `json:"basis,omitempty"`
}

// ScoreKey identifies a (criterion name, phase) pair.
type ScoreKey struct {
	Criterion string
	Phase     intent.Phase
}

// FakeScorer is the in-package test double used by the gate acceptance tests.
// Results is keyed by (criterion name, phase); a key absent from Results defaults
// to Pass (documented ergonomic default; tests set only the failing/unevaluable
// ones). Every call is appended to Calls in order for call-count assertions.
type FakeScorer struct {
	Results map[ScoreKey]Score
	Calls   []ScoreKey
}

// Score records the call and returns the configured Score for (c.Name, phase),
// defaulting to Pass when the key is absent.
func (f *FakeScorer) Score(ctx context.Context, i intent.Intent, c intent.Criterion, phase intent.Phase) Score {
	key := ScoreKey{Criterion: c.Name, Phase: phase}
	f.Calls = append(f.Calls, key)
	if s, ok := f.Results[key]; ok {
		return s
	}
	return Pass
}
