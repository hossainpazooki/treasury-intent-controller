# Consumer signal — first external practitioner read on intent-plane (platform architect)

2026-08-16 (UTC; the conversation was relayed 2026-08-15 local). Discussion
record: the operator relayed transcript excerpts of a conversation with
Jordan, a platform architect — deep Kubernetes/service-mesh background, runs
an internal LLM platform product under central-team guardrail mandates (a
list of ~20). This is the FIRST external practitioner reaction to
intent-plane, and it amends the 2026-08-14 distribution assessment's
avenue-1 premise line "zero external consumers as evidence": there is now
one data point, and its read is mixed. Status: ASSESSED — nothing here is
built; the positioning consequence is recorded as a drafting rule in the
article claim ledger (same day).

## The signal, in three parts

1. **Validation — the maturity admission.** A near-ideal embedding-side
   profile (platform owner, mesh-fluent, mandate-holding) still said "I am
   not even here yet ... I don't even have an agent registration", and
   described converging on the capability piecemeal across "eight different
   services, not one service". This independently supports the
   demand-side-first spine: the embedding channel has a maturity
   prerequisite most orgs fail, while the audit channel (avenue 1) needs no
   integration budget at all. Avenue 4's named-consumer condition remains
   UNMET — this is a warm future consumer, not a present one, by his own
   account.
2. **Warning — the mental-model mismatch (positioning hazard).** His
   spontaneous design for an "intent plane" is a routing/DLP mesh layer:
   semantic/prompt routing for cost (his stat: ~72% of recent traffic on
   Opus-class models that could downshift), PII/MNPI scrubbing, Bedrock-style
   content guardrails, observability export, CRDs. intent-plane routes
   nothing, scrubs nothing, and saves no spend; the overlap is only "policy
   decision point in the request path". A platform-architect reader will
   pattern-match the name to the prompt router they are currently costing
   out and judge it against that need — where it loses. The article must
   preempt this early: the gate COMPOSES WITH a router (the router decides
   WHERE a request goes; the gate decides WHETHER the action is authorized),
   and core neutrality forbids scope-creep toward routing.
3. **Opening — the right vocabulary, and a fourth venue candidate.** His
   middleware-vs-backbone question has a precise answer in his own terms:
   not Istio (the mesh) — the ADMISSION CONTROLLER plus the AUDIT LOG. That
   is the OPA/Gatekeeper shape applied to agent actions: policy as signed
   artifacts, fail-closed admission at the dispatch edge, verifiable
   decision records. The analogy is more than pedagogy: the envelopes are
   already DSSE-shaped, the native wire format of the sigstore/in-toto/SLSA
   ecosystem. That names a FOURTH venue beyond the LangChain (embed) /
   Datadog (watch) / GRC-audit (attest) triad: the policy-engine and
   software-attestation community, the one audience needing zero
   translation ("in-toto for agent actions" is a one-line pitch there). His
   CRD remark sketches the eventual Kubernetes-native packaging (specs as
   CRDs, gate as an admission-webhook-shaped service) — aspirational
   vocabulary, not a build item.

## Two reusable artifacts from the exchange

- **The wedge sentence for the demand-side artifact:** none of the ~20
  mandated guardrails produce a record a third party can verify — the pitch
  to central-mandate teams is not "guardrail #21" but "the evidence layer
  that proves your guardrails ran".
- **The layering story is tellable in one breath:** the operator's
  core/domain-split articulation (tight domain-agnostic Go library; author/
  verifier roles layered on top — the ADR-0008/0011 layering) landed with
  immediate assent.

## Consequences for standing decisions

- Avenue 4 (framework adapter): stays parked; the condition it waits on was
  probed and not met.
- Queued ruling 2 (content-channel split): strengthened — the demand-side
  artifact's audience spoke in this transcript, mandate list in hand.
- The venue triad + fourth candidate feed the article-audience choice; if
  the attestation-community venue is ever pursued, the DSSE-shaped register
  ("shaped", not "compliant") from the claim ledger governs.
