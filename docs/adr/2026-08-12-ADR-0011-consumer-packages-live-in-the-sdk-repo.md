# ADR-0011 — Consumer-facing packages live in the SDK repo; port direction runs per tree

- **Status: Accepted** (2026-08-12, operator ruling — chosen over
  "port-everything-from-the-monorepo", "collapse to one repo", and
  "standalone verifier repo").
- Numbering note: ADR-0009 (production key authority) and ADR-0010
  (per-action approval as an attested artifact) are reserved and not yet
  written; this ADR intentionally lands out of numeric order.

## Context

The 2026-08-05 two-repo split (ADR-0008 layering; `~/dev/intent-plane` =
published minimal SDK, this repo = testing monorepo) came with a single
port-flow rule: everything is born here and manually ported to the SDK once
settled. Applied uniformly, that rule produced a published repo that pitched
the two-sided sale — the accountability function (demand side) running the
verifier, the platform team (supply side) embedding the declarant SDK —
while shipping **neither consumer's artifact**: the verifier cluster
(2026-08-08) lived only here, and the declarant SDK (consumer-research memo
S4) is unbuilt. The published repo carried the plane operator's reference
implementation and prose about the consumers. That gap is not one session's
lag; it is the steady state of a one-directional port rule, and it is the
correct-shaped-lie class the 2026-08-04 critique already named.

## Decision

Ownership of a tree follows its audience:

- **Consumer-facing packages are born and evolve in the SDK repo**
  (`intent-plane`): the `verifier/` tree today, the declarant SDK when it
  lands, and any future package whose importer is a consumer rather than the
  plane operator. The monorepo consumes them back — its treasury quickstart
  probes are the live integration test that the published packages work
  against a running plane.
- **Plane internals keep the monorepo-first flow** (gate, scorer, `plane/`,
  wire/feed fixtures they generate): experiment here, port to the SDK once
  settled, exactly as ruled in 2026-08-05.
- Both repos continue to share the module path by design; ports remain
  copy-clean in both directions.

First act under this ruling: the verifier cluster (refusal-hash gate change,
`verifier/` tree, `core/contract/feed/` fixtures, §1.1/§2.3/§4.2/§5.4/§7.1/
§9.1 contract text) ported to `intent-plane` on 2026-08-12.

## Consequences

- The SDK repo is the canonical home of what consumers import; a consumer
  artifact existing only in this monorepo is a defect, not a pending port.
- ADR-0010 (approval artifact) grows the verifier's checkable set, so when
  it lands its consumer-facing text and code land SDK-side too.
- Same-named trees in the two repos may briefly diverge in either direction;
  the byte-frozen fixtures and shared contractcheck pins are what keep a
  divergence loud. The `verifier/` §7.1 rule (imports nothing from the
  module outside its own tree) holds identically in both repos.
- Independence optics (the demand side's "no trust in the gate" vs the
  verifier shipping from the gate authors' repo) were considered and
  deferred: the §7.1 import pin and byte-frozen fixtures are the answer
  until a real consumer asks for a standalone verifier distribution.
