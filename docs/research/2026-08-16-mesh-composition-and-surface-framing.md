# Mesh composition and the surface framing — Istio/Astio follow-on

2026-08-16 (UTC). Discussion record, follow-on to the 2026-08-16
consumer-signal note: the operator asked how intent-plane is used on Istio,
then clarified the real target is **Astio** — Jordan's agentic Istio
implementation (the name is now recorded at first mention in that note).
Status: DISCUSSION RECORD — nothing here is built, and nothing today blocks
on any of it.

## Composition with a mesh (generic Istio), assessed

Ordered by how much real value each adds:

1. **No-bypass made structural.** An `AuthorizationPolicy` admitting only
   the gate's workload identity (SPIFFE via mTLS) to each action-executing
   service turns "agents go through the gate" from an encoded pattern into a
   transport-layer invariant the agent cannot opt out of. The mesh decides
   who may reach what; the gate decides whether the action is authorized.
   Composes, doesn't compete.
2. **R2 discharge candidate.** Mesh-issued workload identity plus a reach
   policy on the feed store and scorer is a concrete, third-party-verifiable
   shape for the currently-asserted R2 claim ("only the gate writes
   records"). A line for whenever R2 moves from asserted to built; not a
   build item.
3. **ext_authz is a partial fit.** Envoy external authorization can front an
   egress choke point, but it models stateless allow/deny over request
   context, not declare→verify→dispatch with idempotent settlement. Workable
   only as a thin adapter (intent content address carried in a header; the
   settlement lifecycle stays on the gate's own wire). Fail-closed landmine:
   ext_authz **fails OPEN on timeout unless `failure_mode_allow: false`** —
   any future mesh paragraph in the SDK's `docs/integration.md` must say so,
   or the mesh hook quietly inverts the product's central property.
4. **Zero SDK change, which is the point.** The gate is a plain HTTP
   service; the mesh is infrastructure around the process, not a dependency
   inside it. stdlib-only untouched — same shape as the Datadog log-forward
   story.

Anti-pattern: re-implementing gate checks as an Envoy/WASM filter or a mesh
service "so it's all one mesh" — a contract fork with no golden-byte pins,
the same drift the OpenAPI correction already warned about. The mesh routes
TO the gate; it never becomes the gate.

## Astio specifically

Astio sits on the **prompt path** (agent→model: semantic routing for cost,
PII/MNPI scrubbing, content guardrails); intent-plane sits on the **action
path** (agent→world). They do not intercept the same traffic, which is why
they compose. "Used on Astio" therefore means:

- the gate as Astio's dispatch-edge service — the admission controller plus
  the audit log; a ninth service in his architecture, but the only one whose
  output a third party can verify without trusting the platform;
- the evidence layer for his ~20 mandates — Astio enforces guardrails
  in-line, the feed + verifier prove enforcement ran (the wedge sentence
  applied to his own product), and this needs none of the declarant side;
- his missing agent registration does NOT block adoption — agents hold no
  keys; the attester signs the spec; the prerequisite he named as missing is
  one this design does not have. A tellable line if the conversation
  resumes;
- speculative only, flagged as such: Astio guardrail verdicts ("scrub
  completed") as volatile facts the scorer consults, so dispatch refuses
  unless the platform's checks ran. Nothing in either system supports this
  today.

## The "policy layer" exchange — surface framing validated

The operator test-compressed the positioning to "intent-plane is the policy
layer." Assessment: half right, and the dropped half (the verifiable record)
is the differentiator — "policy layer" alone prices the product against OPA
and re-triggers the guardrail-#21 hazard from the consumer-signal note.
Checked against `2026-08-12-plane-as-separating-surface.md`: fully
consistent, and the older doc is sharper — the compression is precisely the
containment error its §3 diagnoses (a layer is a volume; the plane is the
surface policy crosses), and "admission controller + audit log" is that
doc's ZOOM MID formulation, licensed but not the concept answer. The surface
framing survived its first contact with a live compression attempt; a
matching drafting rule was added to the article claim ledger the same day.

## Venue triad, reconfirmed with one precision

The third channel remains GRC/audit (attest) — and it is deliberately NOT an
integration: no SDK, no pipeline change, no platform budget, which is
exactly why it survives the maturity problem the consumer signal
personified. The integration-shaped fourth candidate (the
in-toto/sigstore/policy-engine community, with Kubernetes/mesh admission as
its variant) stays parked behind the named-consumer condition, still unmet.
