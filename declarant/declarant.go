// Package declarant is the embedding side of the plane: the SDK a platform
// team wraps around an agent runtime so every irreversible tool call becomes
// declare -> await terminal -> proceed-or-surface (CONTRACT.md §2.7, the
// declarant discipline).
//
// This is a consumer tree (§7.1): it imports nothing from the module outside
// itself — the embedding team runs none of the plane's code, and everything
// the SDK can and cannot send is visible right here. In particular,
// force_scores exists in NO type in this package: the guarded test
// affordance is unreachable through the SDK by construction.
package declarant

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
)

// Request carries exactly the declarant-owned declaration fields (§2.2).
// Criteria, posture, and action class are NOT here — they reach the gate
// only through the attested spec payload (§2.6).
type Request struct {
	EpisodeSeed      string
	IdempotencyKey   string
	RuleArtifactHash string          // optional artifact-plane passthrough; omitted from the wire when empty
	IntentSpecHash   string          // content address of the attested payload
	IdempotencyScope string          // carried data, not a keyspace partition — fold scope into the KEY (DeriveKey)
	SpecEnvelope     json.RawMessage // optional §2.6 hybrid wire path
}

// wire mirrors the §2.2 DTO tags exactly. The server rejects unknown fields,
// so this marshal IS the compatibility surface: additive evolution amends
// §2.2 first, then this struct.
type wireSpec struct {
	IdempotencyScope string `json:"idempotency_scope"`
}

type wireRequest struct {
	EpisodeSeed      string          `json:"episode_seed"`
	IdempotencyKey   string          `json:"idempotency_key"`
	RuleArtifactHash string          `json:"rule_artifact_hash,omitempty"`
	IntentSpecHash   string          `json:"intent_spec_hash"`
	Spec             wireSpec        `json:"spec"`
	SpecEnvelope     json.RawMessage `json:"spec_envelope,omitempty"`
}

// MarshalRequest produces the exact wire bytes for a declaration (§2.7
// request-marshal rule; pinned by the golden-bytes test).
func MarshalRequest(r Request) ([]byte, error) {
	return json.Marshal(wireRequest{
		EpisodeSeed:      r.EpisodeSeed,
		IdempotencyKey:   r.IdempotencyKey,
		RuleArtifactHash: r.RuleArtifactHash,
		IntentSpecHash:   r.IntentSpecHash,
		Spec:             wireSpec{IdempotencyScope: r.IdempotencyScope},
		SpecEnvelope:     r.SpecEnvelope,
	})
}

// DeriveKey builds a deterministic idempotency key from the action's
// identity: <scope>:<agent-run-id>:<tool-name>:<sha256-hex(canonical-args)>
// (§2.7). Canonicalizing args is the CALLER's duty — hand this function a
// stable byte serialization, never a map-ordered or timestamped one. Never
// mint a fresh random key per attempt (it deletes dedup), and never reuse a
// key across genuinely distinct calls (reservations are permanent).
func DeriveKey(scope, agentRunID, toolName string, canonicalArgs []byte) string {
	sum := sha256.Sum256(canonicalArgs)
	return scope + ":" + agentRunID + ":" + toolName + ":" + hex.EncodeToString(sum[:])
}

// IntentID derives the deterministic intent identity from the episode seed —
// the first 16 hex characters of sha256(seed), matching the gate's
// derivation (§7.2 intent.ID). The declarant needs it to consult
// GET /v2/intents/{id}/events on the 500 edge without a response to read it
// from.
func IntentID(episodeSeed string) string {
	sum := sha256.Sum256([]byte(episodeSeed))
	return hex.EncodeToString(sum[:])[:16]
}

// Class is the closed classification of a declaration outcome (§2.7 table).
type Class string

const (
	Proceed         Class = "PROCEED"          // ACHIEVED: authorized, exactly one durable record
	ShadowRecorded  Class = "SHADOW_RECORDED"  // fully scored, durably recorded, NOT authorized
	PolicyDenied    Class = "POLICY_DENIED"    // criteria bound and failed
	SDKBug          Class = "SDK_BUG"          // key construction / marshaling defect — fix the caller
	SpecUnattested  Class = "SPEC_UNATTESTED"  // no verified spec for the claimed hash
	SpecDefect      Class = "SPEC_DEFECT"      // thin/malformed spec — route to the spec owner
	HumanJudgment   Class = "HUMAN_JUDGMENT"   // deliberately unquantified obligation awaits a human
	Unevaluable     Class = "UNEVALUABLE"      // scorer outage / missing fact — backoff, never a pass
	Revoked         Class = "REVOKED"          // authority withdrawn — needs a fresh attestation
	FactDrift       Class = "FACT_DRIFT"       // volatile fact moved between scoring and dispatch
	AlreadyReserved Class = "ALREADY_RESERVED" // this action already happened or was reserved
	Indeterminate   Class = "INDETERMINATE"    // no terminal observed — consult the feed, decide nothing
	Unknown         Class = "UNKNOWN"          // outside the closed vocabulary — surface, no auto-retry
)

// Classify maps a synchronous terminal + reason to its class and reports
// whether a SAME-KEY retry is safe, meaning this outcome did NOT reserve the
// key (§2.7 retry-by-reservation-position). Total over the closed §3.2/§3.3
// vocabulary; anything else is Unknown, fail-closed (no retry, surface it).
// Specific prefixes are matched before the generic `unevaluable:<criterion>`.
func Classify(terminal, reason string) (class Class, sameKeyRetrySafe bool) {
	switch terminal {
	case "ACHIEVED":
		return Proceed, false // the key IS reserved; a same-key retry collides, correctly
	case "SHADOW_RECORDED":
		return ShadowRecorded, true
	case "FAILED":
		switch {
		case reason == "unevaluable:absent-key":
			return SDKBug, true
		case reason == "unevaluable:unattested-spec":
			return SpecUnattested, true
		case reason == "unevaluable:invalid-posture",
			reason == "unevaluable:empty-criteria",
			strings.HasPrefix(reason, "unevaluable:invalid-volatility:"):
			return SpecDefect, true
		case strings.HasPrefix(reason, "unevaluable:human-judgment:"):
			return HumanJudgment, true
		case strings.HasPrefix(reason, "revoked:"):
			return Revoked, true
		case strings.HasPrefix(reason, "unevaluable:"):
			return Unevaluable, true
		case reason != "" && !strings.Contains(reason, ":"):
			return PolicyDenied, true // joined failed-criterion names
		default:
			return Unknown, false
		}
	case "FAILED_AT_DISPATCH":
		switch {
		case strings.HasPrefix(reason, "volatile-recheck:"):
			return FactDrift, true // the key was NOT reserved on this path
		case reason == "idempotency-collision":
			return AlreadyReserved, false
		case strings.HasPrefix(reason, "revoked:"):
			return Revoked, true
		default:
			return Unknown, false
		}
	default:
		return Unknown, false
	}
}

// classifyFeedRecord classifies from an observed terminal-position feed
// record (the 500-edge path, §2.7): the record's Type/Detail carry the
// step-1 refusal shapes that never got a FAILED transition event, so they
// are mapped back onto the synchronous vocabulary before classifying.
func classifyFeedRecord(rec Record) (Class, bool) {
	switch rec.Type {
	case "ACHIEVED", "SHADOW_RECORDED", "FAILED", "FAILED_AT_DISPATCH":
		return Classify(rec.Type, rec.Detail)
	case "UNEVALUABLE":
		switch {
		case rec.Detail == "absent-key":
			return Classify("FAILED", "unevaluable:absent-key")
		case strings.HasPrefix(rec.Detail, "unattested-spec:"):
			return Classify("FAILED", "unevaluable:unattested-spec")
		case strings.HasPrefix(rec.Detail, "invalid-posture:"):
			return Classify("FAILED", "unevaluable:invalid-posture")
		case strings.HasPrefix(rec.Detail, "empty-criteria:"):
			return Classify("FAILED", "unevaluable:empty-criteria")
		default:
			// invalid-volatility:<name>:<raw>, human-judgment:<name>, and a
			// bare criterion name all classify correctly under the generic
			// prefix rules once re-prefixed.
			return Classify("FAILED", "unevaluable:"+rec.Detail)
		}
	case "REVOKED":
		return Classify("FAILED", "revoked:"+rec.Detail)
	default:
		return Unknown, false
	}
}
