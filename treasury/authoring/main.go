// Command authoring is the AUTHORING role of the plane: it reads a structured
// policy source and drafts a machine-checkable spec payload — every value
// pinned to the sha256 of the exact passage it came from, every unmapped
// provision surfaced as a named unknown rather than a silent omission, every
// deliberately-unquantified obligation routed to a human-judgment entry
// instead of an invented number (a spec carrying an unresolved human-judgment
// entry NEVER authorizes: the gate refuses it, and that abstention is the
// system working — the authority to decide what a rule means never belongs to
// the software).
//
// This role holds no keys. It cannot sign, publish, or activate anything, and
// in-repo that "cannot" is a property of the import graph
// (TestKeyPossessionBoundary): this package may not import treasury/authority.
// HONEST SCOPE: this command is the deterministic CHASSIS of the authoring
// role — the seat where a drafting AI sits. The drafting intelligence itself
// (reading free-text policy prose) is not in this repo; the input here is
// already-structured provisions, and the chassis guarantees the pinning,
// surfacing, and routing behavior around whatever fills the seat.
//
// Usage:
//
//	authoring draft -source <policy.json> -out <draft.json> [-posture shadow|enforce]
//
// Source shape (strict-decoded):
//
//	{"action_class": "...", "provisions": [
//	  {"id": "p1", "passage": "exact policy text ...",
//	   "criterion": {"name": "...", "threshold": 3.5, "volatility": "stable"}},   quantified+mapped
//	  {"id": "p2", "passage": "...", "judgment": {"name": "..."}},                deliberately unquantified
//	  {"id": "p3", "passage": "..."}                                              unmapped -> named unknown
//	]}
//
// New drafts default to SHADOW posture: enforcement is the attester's promotion
// decision, never the author's default.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"

	"github.com/hossainpazooki/intent-plane/plane"
)

type criterionSrc struct {
	Name       string  `json:"name"`
	Threshold  float64 `json:"threshold"`
	Volatility string  `json:"volatility"`
}

type judgmentSrc struct {
	Name string `json:"name"`
}

type provision struct {
	ID        string        `json:"id"`
	Passage   string        `json:"passage"`
	Criterion *criterionSrc `json:"criterion,omitempty"`
	Judgment  *judgmentSrc  `json:"judgment,omitempty"`
}

type policySource struct {
	ActionClass string      `json:"action_class"`
	Provisions  []provision `json:"provisions"`
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "authoring:", err)
	os.Exit(1)
}

func pin(passage string) string {
	sum := sha256.Sum256([]byte(passage))
	return hex.EncodeToString(sum[:])
}

func main() {
	if len(os.Args) < 2 || os.Args[1] != "draft" {
		fail(fmt.Errorf("usage: authoring draft -source <policy.json> -out <draft.json> [-posture shadow|enforce]"))
	}
	a := map[string]string{}
	for i := 2; i+1 < len(os.Args); i += 2 {
		a[os.Args[i]] = os.Args[i+1]
	}
	raw, err := os.ReadFile(a["-source"])
	if err != nil {
		fail(err)
	}
	var src policySource
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&src); err != nil {
		fail(fmt.Errorf("policy source: %w", err))
	}

	posture := plane.PostureShadow // author's default is shadow; enforce is a promotion
	if p := a["-posture"]; p != "" {
		posture = plane.Posture(p)
		if posture != plane.PostureShadow && posture != plane.PostureEnforce {
			fail(fmt.Errorf("posture %q: want shadow|enforce", p))
		}
	}

	draft := plane.SpecPayload{
		SpecVersion: 1,
		ActionClass: src.ActionClass,
		Posture:     posture,
	}
	for _, p := range src.Provisions {
		if p.Passage == "" {
			fail(fmt.Errorf("provision %s has no passage: a value with no source cannot be pinned", p.ID))
		}
		ph := pin(p.Passage)
		switch {
		case p.Criterion != nil && p.Judgment != nil:
			fail(fmt.Errorf("provision %s is both criterion and judgment", p.ID))
		case p.Criterion != nil:
			draft.Criteria = append(draft.Criteria, plane.CriterionSpec{
				Name:       p.Criterion.Name,
				Threshold:  p.Criterion.Threshold,
				Volatility: p.Criterion.Volatility,
			})
			draft.SourcePins = append(draft.SourcePins, plane.SourcePin{
				Name:          p.Criterion.Name,
				PassageSHA256: ph,
			})
		case p.Judgment != nil:
			// Deliberately-unquantified: routed to human judgment, never an
			// invented number. Attestation of a spec still carrying this entry
			// yields a spec the gate refuses (unevaluable:human-judgment:<n>).
			draft.HumanJudgment = append(draft.HumanJudgment, plane.HumanJudgment{
				Name:          p.Judgment.Name,
				PassageSHA256: ph,
			})
		default:
			// Unmapped: a NAMED unknown, never a silent omission.
			draft.NamedUnknowns = append(draft.NamedUnknowns, plane.NamedUnknown{
				ProvisionID:   p.ID,
				PassageSHA256: ph,
				Note:          "unmapped provision — surfaced by authoring, resolve before attestation or carry knowingly",
			})
		}
	}

	out, err := json.Marshal(draft)
	if err != nil {
		fail(err)
	}
	if err := os.WriteFile(a["-out"], out, 0o644); err != nil {
		fail(err)
	}
	fmt.Printf("draft %s: %d criteria, %d named unknowns, %d human-judgment (posture %s)\n",
		plane.SpecHash(out), len(draft.Criteria), len(draft.NamedUnknowns), len(draft.HumanJudgment), draft.Posture)
}
