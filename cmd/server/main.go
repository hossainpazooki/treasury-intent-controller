// Command server exposes the authorization gate over HTTP for the integration
// live-probe. It is a thin shell: the gate (internal/gate) is the substance.
//
// Endpoints:
//
//	GET  /healthz    -> 200 "ok"
//	POST /v2/intents -> decode an intent + IntentSpecParams, run gate.Authorize
//	                    with a fresh ReferenceAdapter and Store, and respond JSON
//	                    {terminal, reason, trajectory_hash, settlement?}.
//
// The probe drives a deterministic terminal WITHOUT a live Python scorer by
// passing an optional "force_scores" map (criterion -> {declaration, dispatch}
// results). That map backs a small inline Scorer (forceScorer) which defaults a
// missing criterion/result to Pass. This is a documented test affordance; the
// real /ml/evaluate HTTPScorer is a later slice.
package main

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/pazooki/treasury-intent-controller/internal/adapter"
	"github.com/pazooki/treasury-intent-controller/internal/gate"
	"github.com/pazooki/treasury-intent-controller/internal/idempotency"
	"github.com/pazooki/treasury-intent-controller/internal/intent"
	"github.com/pazooki/treasury-intent-controller/internal/scoring"
)

// --- request / response DTOs (snake_case JSON, decoupled from internal types) ---

type criterionDTO struct {
	Name       string  `json:"name"`
	Threshold  float64 `json:"threshold"`
	Volatility string  `json:"volatility"` // "stable" | "volatile"
}

type specDTO struct {
	ActionClass      string         `json:"action_class"`
	Criteria         []criterionDTO `json:"criteria"`
	IdempotencyScope string         `json:"idempotency_scope"`
}

// forceScore carries the forced result for a single criterion, per phase. An
// empty string means "unspecified" and defaults to Pass.
type forceScore struct {
	Declaration string `json:"declaration"` // "PASS" | "FAIL" | "UNEVALUABLE" | ""
	Dispatch    string `json:"dispatch"`    // "PASS" | "FAIL" | "UNEVALUABLE" | ""
}

type intentRequest struct {
	EpisodeSeed      string                `json:"episode_seed"`
	IdempotencyKey   string                `json:"idempotency_key"`
	RuleArtifactHash string                `json:"rule_artifact_hash"`
	IntentSpecHash   string                `json:"intent_spec_hash"`
	Spec             specDTO               `json:"spec"`
	ForceScores      map[string]forceScore `json:"force_scores"`
}

type settlementDTO struct {
	IntentID string `json:"intent_id"`
	Key      string `json:"key"`
	Payload  string `json:"payload"`
}

type intentResponse struct {
	Terminal       string         `json:"terminal"`
	Reason         string         `json:"reason"`
	TrajectoryHash string         `json:"trajectory_hash"`
	Settlement     *settlementDTO `json:"settlement,omitempty"`
}

// --- inline scorer driven by force_scores ---

// forceScorer is a deterministic scoring.Scorer backed by the request's
// force_scores map. It reads the forced result for (criterion, phase); a missing
// criterion, or a missing/blank result for the phase, defaults to Pass. This lets
// the probe drive any terminal without a live Python scorer.
type forceScorer struct {
	scores map[string]forceScore
}

// parseScore maps a wire result string to a scoring.Score. ok=false when the
// string is blank or unrecognized (caller then defaults to Pass).
func parseScore(s string) (scoring.Score, bool) {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "PASS":
		return scoring.Pass, true
	case "FAIL":
		return scoring.Fail, true
	case "UNEVALUABLE":
		return scoring.Unevaluable, true
	default:
		return scoring.Pass, false
	}
}

func (f forceScorer) Score(_ context.Context, _ intent.Intent, c intent.Criterion, phase intent.Phase) scoring.Score {
	fs, ok := f.scores[c.Name]
	if !ok {
		return scoring.Pass
	}
	raw := fs.Declaration
	if phase == intent.Dispatch {
		raw = fs.Dispatch
	}
	if sc, ok := parseScore(raw); ok {
		return sc
	}
	return scoring.Pass
}

// --- handlers ---

func handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, "ok")
}

func handleIntents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req intentRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}

	i := intent.Intent{
		EpisodeSeed:      req.EpisodeSeed,
		IdempotencyKey:   intent.IdempotencyKey(req.IdempotencyKey),
		RuleArtifactHash: req.RuleArtifactHash,
		IntentSpecHash:   req.IntentSpecHash,
		Spec: intent.IntentSpecParams{
			ActionClass:      req.Spec.ActionClass,
			IdempotencyScope: req.Spec.IdempotencyScope,
		},
	}
	for _, c := range req.Spec.Criteria {
		i.Spec.Criteria = append(i.Spec.Criteria, intent.Criterion{
			Name:       c.Name,
			Threshold:  c.Threshold,
			Volatility: intent.Volatility(c.Volatility),
		})
	}

	// Fresh adapter + store per request: each authorization is self-contained and
	// deterministic from the intent + seed.
	scorer := forceScorer{scores: req.ForceScores}
	g := gate.New(scorer, adapter.NewReferenceAdapter(), idempotency.NewStore())
	res := g.Authorize(r.Context(), i)

	resp := intentResponse{
		Terminal:       string(res.Terminal),
		Reason:         res.Reason,
		TrajectoryHash: res.TrajectoryHash,
	}
	if res.Settlement != nil {
		resp.Settlement = &settlementDTO{
			IntentID: res.Settlement.IntentID,
			Key:      string(res.Settlement.Key),
			Payload:  res.Settlement.Payload,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// newMux wires the routes. Split out so tests can drive it via httptest without
// binding a port.
func newMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", handleHealthz)
	mux.HandleFunc("/v2/intents", handleIntents)
	return mux
}

func main() {
	addr := ":8080"
	if v := os.Getenv("TIC_ADDR"); v != "" {
		addr = v
	}
	log.Printf("treasury-intent-controller listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, newMux()))
}
