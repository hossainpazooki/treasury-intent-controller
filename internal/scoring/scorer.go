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

	"github.com/pazooki/treasury-intent-controller/internal/intent"
)

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

// Score POSTs an EvalRequest to the configured endpoint and maps the response
// result to a Score. ANY transport error, non-2xx status, or decode failure maps
// to Unevaluable (fail-closed) — never a silent pass.
func (h *HTTPScorer) Score(ctx context.Context, i intent.Intent, c intent.Criterion, phase intent.Phase) Score {
	body, err := json.Marshal(EvalRequest{
		IntentID:  i.ID(),
		Criterion: c.Name,
		Threshold: c.Threshold,
		Phase:     string(phase),
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
		client = http.DefaultClient
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

// EvalRequest is the /ml/evaluate request JSON contract.
type EvalRequest struct {
	IntentID  string  `json:"intent_id"`
	Criterion string  `json:"criterion"`
	Threshold float64 `json:"threshold"`
	Phase     string  `json:"phase"`
}

// EvalResponse is the /ml/evaluate response JSON contract.
type EvalResponse struct {
	Result string `json:"result"`
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
