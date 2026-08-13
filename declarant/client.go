package declarant

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
)

// Terminal is the synchronous declaration response (§2.2 intentResponse).
type Terminal struct {
	Terminal       string `json:"terminal"`
	Reason         string `json:"reason"`
	TrajectoryHash string `json:"trajectory_hash"`
	AchievedSeq    int    `json:"achieved_seq"`
}

// Record mirrors the §2.3 feed record by a second, read-only declaration —
// this tree imports nothing from the module, so the wire tags are restated
// here. Unknown fields are ignored (forward-compatible read).
type Record struct {
	Seq              int    `json:"seq"`
	IntentSeq        int    `json:"intent_seq"`
	IntentID         string `json:"intent_id"`
	Type             string `json:"type"`
	Detail           string `json:"detail"`
	ScorerID         string `json:"scorer_id"`
	IdempotencyKey   string `json:"idempotency_key"`
	RuleArtifactHash string `json:"rule_artifact_hash"`
	IntentSpecHash   string `json:"intent_spec_hash"`
	TrajectoryHash   string `json:"trajectory_hash"`
}

// Result is one declaration's classified outcome.
type Result struct {
	HTTPStatus       int
	Terminal         *Terminal // nil when no synchronous terminal arrived (400/500)
	Class            Class
	SameKeyRetrySafe bool
	// FeedConsulted reports that the 500-edge per-intent feed consult ran
	// (§2.7); FeedRecords holds what it observed, for reconciliation.
	FeedConsulted bool
	FeedRecords   []Record
}

// Client is the declarant's wire client. BaseURL is the gate's origin
// (e.g. "http://127.0.0.1:8080"); HTTP nil means http.DefaultClient.
type Client struct {
	BaseURL string
	HTTP    *http.Client
}

func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

// Declare POSTs one declaration and classifies the outcome (§2.7).
//
//   - 200: classify the synchronous terminal.
//   - 400: the SDK marshaled something the server rejects — SDKBug (version
//     skew fails loudly by design; fix the SDK or amend §2.2 first).
//   - 500: NO terminal guarantee (§4.1) — the idempotency key may be
//     reserved with no ACHIEVED record. Declare consults
//     GET /v2/intents/{id}/events BEFORE returning: an observed
//     terminal-position record (trajectory_hash set) decides; anything else
//     is Indeterminate, and the caller decides nothing until a later
//     consult observes a terminal.
//
// A transport error returns error — the caller knows nothing and should
// consult the feed before any retry.
func (c *Client) Declare(ctx context.Context, r Request) (Result, error) {
	body, err := MarshalRequest(r)
	if err != nil {
		return Result{}, fmt.Errorf("declarant: marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/v2/intents", bytes.NewReader(body))
	if err != nil {
		return Result{}, fmt.Errorf("declarant: request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return Result{}, fmt.Errorf("declarant: declare: %w", err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusOK:
		var t Terminal
		if derr := json.NewDecoder(resp.Body).Decode(&t); derr != nil {
			return Result{HTTPStatus: resp.StatusCode, Class: Unknown}, fmt.Errorf("declarant: decode terminal: %w", derr)
		}
		class, safe := Classify(t.Terminal, t.Reason)
		return Result{HTTPStatus: resp.StatusCode, Terminal: &t, Class: class, SameKeyRetrySafe: safe}, nil
	case resp.StatusCode == http.StatusBadRequest:
		io.Copy(io.Discard, resp.Body)
		return Result{HTTPStatus: resp.StatusCode, Class: SDKBug, SameKeyRetrySafe: true}, nil
	default:
		// The 500 edge (and any other unexpected status): consult the feed
		// before deciding anything.
		io.Copy(io.Discard, resp.Body)
		res := Result{HTTPStatus: resp.StatusCode, Class: Indeterminate}
		recs, cerr := c.IntentEvents(ctx, IntentID(r.EpisodeSeed))
		if cerr != nil {
			return res, nil // unreachable feed decides nothing: Indeterminate stands
		}
		res.FeedConsulted = true
		res.FeedRecords = recs
		if term, ok := terminalPosition(recs); ok {
			res.Class, res.SameKeyRetrySafe = classifyFeedRecord(term)
		}
		return res, nil
	}
}

// terminalPosition returns the record with the maximum intent_seq iff it
// carries the trajectory-hash commitment (§2.3): only a committed final
// record decides an outcome.
func terminalPosition(recs []Record) (Record, bool) {
	if len(recs) == 0 {
		return Record{}, false
	}
	max := recs[0]
	for _, r := range recs[1:] {
		if r.IntentSeq > max.IntentSeq {
			max = r
		}
	}
	if max.TrajectoryHash == "" {
		return Record{}, false
	}
	return max, true
}

// IntentEvents reads the per-intent event slice
// (GET /v2/intents/{id}/events), ascending intent_seq.
func (c *Client) IntentEvents(ctx context.Context, intentID string) ([]Record, error) {
	var out struct {
		IntentID string   `json:"intent_id"`
		Events   []Record `json:"events"`
	}
	if err := c.getJSON(ctx, c.BaseURL+"/v2/intents/"+url.PathEscape(intentID)+"/events", &out); err != nil {
		return nil, err
	}
	return out.Events, nil
}

// PollAchieved reads ACHIEVED records past a cursor
// (GET /v2/events?since=&type=ACHIEVED) and returns them with the next
// cursor. Settlement discipline (§2.7): apply side effects from the returned
// records FIRST, persist nextSince AFTER — a crash between the two replays
// records (settlement must be key-idempotent), never loses them.
func (c *Client) PollAchieved(ctx context.Context, since int) (records []Record, nextSince int, err error) {
	var out struct {
		Events    []Record `json:"events"`
		NextSince int      `json:"next_since"`
	}
	u := c.BaseURL + "/v2/events?since=" + strconv.Itoa(since) + "&type=ACHIEVED"
	if err := c.getJSON(ctx, u, &out); err != nil {
		return nil, since, err
	}
	return out.Events, out.NextSince, nil
}

func (c *Client) getJSON(ctx context.Context, u string, into any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return fmt.Errorf("declarant: request: %w", err)
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("declarant: get: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, resp.Body)
		return fmt.Errorf("declarant: get %s: status %d", u, resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(into); err != nil {
		return fmt.Errorf("declarant: decode: %w", err)
	}
	return nil
}
