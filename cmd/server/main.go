// Command server exposes the authorization gate over HTTP. It is a thin shell:
// the gate (internal/gate) is the substance.
//
// Endpoints (CONTRACT-DURABILITY §V2.5):
//
//	GET  /healthz                 -> 200 "ok"
//	POST /v2/intents              -> decode an intent + IntentSpecParams, run
//	                                 gate.Authorize over the boot-time shared
//	                                 stores; respond JSON
//	                                 {terminal, reason, trajectory_hash, achieved_seq?}.
//	GET  /v2/events               -> cursor read over the durable feed
//	                                 (?since=<globalSeq>&type=<optional>).
//	GET  /v2/intents/{id}/events  -> per-intent records in ascending intent_seq.
//
// Boot wires the durable feed and the durable idempotency store ONCE (dir from
// TIC_DATA_DIR, default "./data"); handlers share them. The per-request Gate is
// a thin wrapper over those shared singletons.
//
// Scorer selection (CONTRACT-SCORER §S.0/§S.3): a request carrying
// "force_scores" (criterion -> {declaration, dispatch} results) scores through
// the inline forceScorer — the documented test affordance, preserved verbatim.
// Every other request scores through ONE boot-time shared HTTPScorer built from
// TIC_SCORER_URL. Unset TIC_SCORER_URL means an empty endpoint, every Score is
// Unevaluable, and the gate refuses everything: the zero-config server
// authorizes nothing.
//
package main

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/pazooki/treasury-intent-controller/internal/durable"
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

// intentResponse is the V2 response shape: settlement is removed (the gate no
// longer settles); achieved_seq is >=1 iff the terminal is ACHIEVED.
type intentResponse struct {
	Terminal       string `json:"terminal"`
	Reason         string `json:"reason"`
	TrajectoryHash string `json:"trajectory_hash"`
	AchievedSeq    int    `json:"achieved_seq,omitempty"` // >=1 iff ACHIEVED
}

// eventsResponse wraps the raw durable.Record objects (their §V2.1 JSON tags ARE
// the wire contract; no re-tagging DTO).
type eventsResponse struct {
	Events    []durable.Record `json:"events"`
	NextSince int              `json:"next_since"` // max GlobalSeq returned, or the input since
}

type intentEventsResponse struct {
	IntentID string           `json:"intent_id"`
	Events   []durable.Record `json:"events"`
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

// --- handlers (closing over the boot-time shared stores) ---

func handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, "ok")
}

// scorerFromEnv builds the boot-time shared scorer from TIC_SCORER_URL. Unset
// yields an empty endpoint whose every Score is Unevaluable (fail-closed;
// CONTRACT-SCORER §S.0: the zero-config server authorizes nothing).
func scorerFromEnv() *scoring.HTTPScorer {
	return scoring.NewHTTPScorer(os.Getenv("TIC_SCORER_URL"))
}

func handleIntents(feed *durable.Store, istore *idempotency.Store, live scoring.Scorer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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

		// Per-request gate over the SHARED boot-time stores (the slice-1
		// per-request fresh store is gone by contract). force_scores present
		// selects the forced scorer (test affordance); otherwise the shared
		// live scorer (CONTRACT-SCORER §S.3).
		var scorer scoring.Scorer = live
		if req.ForceScores != nil {
			scorer = forceScorer{scores: req.ForceScores}
		}
		g := gate.New(scorer, feed, istore)
		res, err := g.Authorize(r.Context(), i)
		if err != nil {
			http.Error(w, "authorize: "+err.Error(), http.StatusInternalServerError)
			return
		}

		resp := intentResponse{
			Terminal:       string(res.Terminal),
			Reason:         res.Reason,
			TrajectoryHash: res.TrajectoryHash,
			AchievedSeq:    res.AchievedSeq,
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}
}

func handleEvents(feed *durable.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		since := 0
		if raw := r.URL.Query().Get("since"); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil {
				http.Error(w, "bad since: "+err.Error(), http.StatusBadRequest)
				return
			}
			since = n
		}
		typ := r.URL.Query().Get("type")

		records := feed.Since(since, typ)
		next := since
		if len(records) > 0 {
			next = records[len(records)-1].GlobalSeq
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(eventsResponse{Events: records, NextSince: next})
	}
}

func handleIntentEvents(feed *durable.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(intentEventsResponse{
			IntentID: id,
			Events:   feed.ByIntent(id),
		})
	}
}

// newMux wires the routes over the boot-time shared stores. Split out so tests
// can drive it via httptest without binding a port.
func newMux(feed *durable.Store, istore *idempotency.Store, live scoring.Scorer) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", handleHealthz)
	mux.HandleFunc("POST /v2/intents", handleIntents(feed, istore, live))
	mux.HandleFunc("GET /v2/events", handleEvents(feed))
	mux.HandleFunc("GET /v2/intents/{id}/events", handleIntentEvents(feed))
	return mux
}

func main() {
	dir := os.Getenv("TIC_DATA_DIR")
	if dir == "" {
		dir = "./data"
	}
	feed, err := durable.Open(dir)
	if err != nil {
		log.Fatalf("open durable feed: %v", err)
	}
	istore, err := idempotency.OpenStore(dir)
	if err != nil {
		log.Fatalf("open idempotency store: %v", err)
	}

	// ONE shared scorer at boot, like the stores (CONTRACT-SCORER §S.3).
	live := scorerFromEnv()
	if live.Endpoint == "" {
		log.Printf("TIC_SCORER_URL unset: every non-forced score is UNEVALUABLE (gate refuses everything)")
	} else {
		log.Printf("live scorer at %s", live.Endpoint)
	}

	addr := ":8080"
	if v := os.Getenv("TIC_ADDR"); v != "" {
		addr = v
	}
	log.Printf("treasury-intent-controller listening on %s (data dir %s)", addr, dir)
	log.Fatal(http.ListenAndServe(addr, newMux(feed, istore, live)))
}
