// Command server exposes the authorization gate over HTTP. It is a thin shell:
// the gate (internal/gate) is the substance.
//
// Endpoints (CONTRACT.md §2):
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
// INTENT_DATA_DIR, default "./data"); handlers share them. The per-request Gate is
// a thin wrapper over those shared singletons.
//
// Spec resolution (CONTRACT.md §2.6): the wire carries NO criteria — the field
// does not exist in the DTO, so a request supplying them is a loud 400
// (DisallowUnknownFields). Criteria, posture, and action class come ONLY from
// the plane resolver: the spec store at INTENT_SPEC_DIR (default
// <data>/specs), verified against INTENT_TRUST_ROOT, with the hybrid wire
// path (spec_envelope) accepted iff verified AND pinned. No trust root means
// an empty root, every resolution is unattested, and the gate refuses
// everything: the zero-config server authorizes nothing.
//
// Scorer selection (CONTRACT.md §2.5): "force_scores" is a GUARDED test
// affordance — honored only when the server booted with
// INTENT_UNSAFE_FORCE_SCORES=1; otherwise a request carrying it is a loud
// 400. Every other request scores through ONE boot-time shared HTTPScorer
// built from INTENT_SCORER_URL (unset = every Score Unevaluable = refuse
// everything). Every SCORED/RECHECK feed record carries a scorer_id witness,
// so a forced grant is never byte-indistinguishable from a live-scored one.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/hossainpazooki/intent-plane/core/internal/durable"
	"github.com/hossainpazooki/intent-plane/core/internal/gate"
	"github.com/hossainpazooki/intent-plane/core/internal/idempotency"
	"github.com/hossainpazooki/intent-plane/core/internal/intent"
	"github.com/hossainpazooki/intent-plane/core/internal/scoring"
	"github.com/hossainpazooki/intent-plane/plane"
)

// --- request / response DTOs (snake_case JSON, decoupled from internal types) ---

// specDTO carries ONLY the declarant-owned spec field. Criteria, posture, and
// action class are NOT wire fields: they arrive solely through the resolver
// from the ATTESTED payload (P1 closed at the type level — a request carrying
// "criteria" is an unknown field and 400s).
type specDTO struct {
	IdempotencyScope string `json:"idempotency_scope"`
}

// forceScore carries the forced result for a single criterion, per phase. An
// empty string means "unspecified" and defaults to Pass.
type forceScore struct {
	Declaration string `json:"declaration"` // "PASS" | "FAIL" | "UNEVALUABLE" | ""
	Dispatch    string `json:"dispatch"`    // "PASS" | "FAIL" | "UNEVALUABLE" | ""
}

type intentRequest struct {
	EpisodeSeed      string  `json:"episode_seed"`
	IdempotencyKey   string  `json:"idempotency_key"`
	RuleArtifactHash string  `json:"rule_artifact_hash"`
	IntentSpecHash   string  `json:"intent_spec_hash"`
	Spec             specDTO `json:"spec"`
	// SpecEnvelope is the hybrid wire path: a full signed envelope, accepted
	// iff it verifies against the trust root AND its hash is pinned in the
	// store. Optional; the store path needs only intent_spec_hash.
	SpecEnvelope json.RawMessage       `json:"spec_envelope,omitempty"`
	ForceScores  map[string]forceScore `json:"force_scores"`
}

// intentResponse is the V2 response shape: settlement is removed (the gate no
// longer settles); achieved_seq is >=1 iff the terminal is ACHIEVED.
type intentResponse struct {
	Terminal       string `json:"terminal"`
	Reason         string `json:"reason"`
	TrajectoryHash string `json:"trajectory_hash"`
	AchievedSeq    int    `json:"achieved_seq,omitempty"` // >=1 iff ACHIEVED
}

// eventsResponse wraps the raw durable.Record objects (their §2.3 JSON tags ARE
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

// scorerFromEnv builds the boot-time shared scorer from INTENT_SCORER_URL. Unset
// yields an empty endpoint whose every Score is Unevaluable (fail-closed;
// CONTRACT.md §2.5: the zero-config server authorizes nothing).
func scorerFromEnv() *scoring.HTTPScorer {
	return scoring.NewHTTPScorer(os.Getenv("INTENT_SCORER_URL"))
}

func handleIntents(feed *durable.Store, istore *idempotency.Store, live scoring.Scorer, specs *plane.Store, allowForce bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req intentRequest
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}

		// force_scores is a GUARDED test affordance: without the unsafe boot
		// flag, carrying it is a loud 400 — never a silent ignore (a silently
		// dropped bypass is a bypass in waiting).
		if req.ForceScores != nil && !allowForce {
			http.Error(w, "bad request: force_scores requires the server to boot with INTENT_UNSAFE_FORCE_SCORES=1", http.StatusBadRequest)
			return
		}

		i := intent.Intent{
			EpisodeSeed:      req.EpisodeSeed,
			IdempotencyKey:   intent.IdempotencyKey(req.IdempotencyKey),
			RuleArtifactHash: req.RuleArtifactHash,
			IntentSpecHash:   req.IntentSpecHash,
			Spec: intent.IntentSpecParams{
				IdempotencyScope: req.Spec.IdempotencyScope,
			},
		}

		// Resolve the spec through the plane: store authoritative, wire
		// envelope accepted iff verified AND pinned. The gate receives ONLY
		// what came out of signature verification + content-address equality;
		// unattested and revoked are carried into the intent for the gate's
		// fail-closed refusals (the gate, not this handler, owns the event
		// log of the refusal).
		switch res, err := specs.Resolve(req.IntentSpecHash, req.SpecEnvelope); {
		case err == nil:
			i.Resolution = intent.Resolution{Attested: true, Source: res.Source, KeyID: res.KeyID}
			i.Spec.ActionClass = res.Payload.ActionClass
			i.Spec.Posture = intent.Posture(res.Payload.Posture)
			for _, c := range res.Payload.Criteria {
				i.Spec.Criteria = append(i.Spec.Criteria, intent.Criterion{
					Name:       c.Name,
					Threshold:  c.Threshold,
					Volatility: intent.Volatility(c.Volatility),
				})
			}
			for _, hj := range res.Payload.HumanJudgment {
				i.Spec.HumanJudgment = append(i.Spec.HumanJudgment, hj.Name)
			}
		default:
			var rv plane.RevokedError
			if errors.As(err, &rv) {
				i.Resolution = intent.Resolution{RevokedRef: rv.Ref}
			}
			// else: zero Resolution — the gate refuses unattested-spec.
		}

		var scorer scoring.Scorer = live
		scorerID := "live"
		if req.ForceScores != nil {
			scorer = forceScorer{scores: req.ForceScores}
			scorerID = "forced"
		}
		g := gate.New(scorer, feed, istore,
			gate.WithRevocations(specs),
			gate.WithScorerID(scorerID))
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
func newMux(feed *durable.Store, istore *idempotency.Store, live scoring.Scorer, specs *plane.Store, allowForce bool) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", handleHealthz)
	mux.HandleFunc("POST /v2/intents", handleIntents(feed, istore, live, specs, allowForce))
	mux.HandleFunc("GET /v2/events", handleEvents(feed))
	mux.HandleFunc("GET /v2/intents/{id}/events", handleIntentEvents(feed))
	return mux
}

func main() {
	dir := os.Getenv("INTENT_DATA_DIR")
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

	// The plane spec store + trust root (CONTRACT.md §2.6). No trust root =
	// empty root = every resolution unattested = the gate refuses everything.
	// Fail-closed boot posture, same shape as the scorer below.
	specDir := os.Getenv("INTENT_SPEC_DIR")
	if specDir == "" {
		specDir = dir + "/specs"
	}
	var root plane.TrustRoot
	if p := os.Getenv("INTENT_TRUST_ROOT"); p != "" {
		raw, rerr := os.ReadFile(p)
		if rerr != nil {
			log.Fatalf("read trust root: %v", rerr)
		}
		root, rerr = plane.ParseTrustRoot(raw)
		if rerr != nil {
			log.Fatalf("parse trust root: %v", rerr)
		}
		log.Printf("trust root: %d key(s) from %s", len(root.Keys), p)
	} else {
		log.Printf("INTENT_TRUST_ROOT unset: empty trust root, every spec is unattested (gate refuses everything)")
	}
	specs, err := plane.OpenStore(specDir, root)
	if err != nil {
		log.Fatalf("open spec store: %v", err)
	}

	// force_scores guard: the scoring bypass is honored ONLY behind this
	// explicit unsafe boot flag.
	allowForce := os.Getenv("INTENT_UNSAFE_FORCE_SCORES") == "1"
	if allowForce {
		log.Printf("INTENT_UNSAFE_FORCE_SCORES=1: force_scores accepted (TEST POSTURE — never production)")
	}

	// ONE shared scorer at boot, like the stores (CONTRACT.md §2.5).
	live := scorerFromEnv()
	if live.Endpoint == "" {
		log.Printf("INTENT_SCORER_URL unset: every non-forced score is UNEVALUABLE (gate refuses everything)")
	} else {
		log.Printf("live scorer at %s", live.Endpoint)
	}

	addr := ":8080"
	if v := os.Getenv("INTENT_ADDR"); v != "" {
		addr = v
	}
	log.Printf("intent-plane gate listening on %s (data dir %s)", addr, dir)
	log.Fatal(http.ListenAndServe(addr, newMux(feed, istore, live, specs, allowForce)))
}
