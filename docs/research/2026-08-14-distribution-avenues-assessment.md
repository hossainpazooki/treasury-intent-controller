# Distribution avenues — assessment of the Trajectory-derived ranking

2026-08-14 (UTC). Discussion record: the operator presented an LLM evaluation
ranking six distribution avenues for the intent-plane SDK (derived from the
Trajectory research, `~/dev/briefs/2026-08-13-trajectory-sdk-distribution-research.md`);
this note records the controller's critique. Status: ASSESSED — the ranking's
adoption and the corrections below await an operator ruling. Nothing here is
built.

## The ranking's spine, accepted

Trajectory's buyer self-serves bottom-up through a registry; the intent-plane
buyer adopts under accountability pressure. Demand-side channels therefore
outrank developer-discovery channels — the verifier side generates demand,
the declarant side removes the friction of saying yes. This matches the
2026-08-04 consumer-research memo independently. The deprioritizations also
hold: standalone verifier repo already parked by ADR-0011 until a real
consumer asks; registry browsing is not how compliance-driven products spread.

Fact verified 2026-08-14 (gh): `hossainpazooki/intent-plane` is PUBLIC;
the monorepo is private — avenue 5's prerequisite already holds, and
pkg.go.dev can index the module today.

## Corrections by avenue

1. **Verifier as hand-deliverable binary (ranked #1 — right, but as pitched
   it is a correct-shaped lie.** "Check me without trusting me," handed to an
   audit function, meets the one audience trained to ask what the check
   proves — and at `42ce556` the answer is self-consistency, not
   never-rewritten (R1 staged), not sole-writer (R2 staged), with every
   signature stamped `key_authority: "test"`. Fix: ship the binary WITH the
   frozen fixture pair and the assurance stage table — "verify the good feed,
   flip one byte, watch it refute, and here is precisely what this does and
   does not prove yet." Second-order: an UNSIGNED release binary from a
   signed-artifacts product invites the obvious skeptic question — checksums
   minimum, signed releases ideally (drags a slice of ADR-0009 forward).
   Mark the premise honestly: "this channel creates requirements inside buyer
   orgs" is a HYPOTHESIS transferred from Trajectory with zero external
   consumers as evidence; it ranks first as the cheapest test of the
   two-sided thesis, not as proven leverage.
2. **SDK quickstart (ranked #2) — undercosted and mis-sequenced.** The live
   demo needs the Python scorer; the tempting shortcuts are bad
   (`INTENT_UNSAFE_FORCE_SCORES=1` showcases the bypass as the front door; a
   stub Go scorer weakens "real gate, real scorer" into a caveat). Clean
   version: docker-compose or a clearly-labeled test-posture example scorer,
   domain-neutral (core neutrality applies — treasury facts stay here).
   Sequencing: whoever receives the #1 binary lands on the README next, so
   #2 is #1's landing zone — build order is 2-with-1, even if priority is
   1-then-2.
3. **OpenAPI wire surface (ranked #3) — claim needs shrinking.** It unblocks
   CALLING, not EMBEDDING: §2.7's discipline (derive-don't-mint keys,
   consult-the-feed-on-500, total classification) is code, and a TS team
   hand-rolling from OpenAPI reintroduces exactly the mistakes the declarant
   prevents. Publish it, scoped — and build it the house way: generated or
   fixture-validated against the golden request bytes in CI, or it becomes a
   second source of truth that drifts from CONTRACT.md.
4. **Framework adapter / MCP gateway (ranked #4) — unchanged.** The
   named-consumer condition is right; do not build speculatively.
5. **Registry hygiene (ranked #5) — prerequisite already satisfied** (repo is
   public); remains low as a discovery channel for the stated reasons.
6. **Content channel (ranked #6) — quietly re-targets the in-flight
   article; surface as a decision.** The current seed prompt is aimed at a
   practitioner venue; the evaluation wants model-risk readers who write
   diligence checklists. One piece cannot serve both. Natural resolution:
   the current article stays the supply-side piece; a shorter demand-side
   artifact ("what to ask your agent vendors for") becomes a second
   deliverable — by this ranking's own logic, arguably the higher-leverage
   one. OPERATOR DECISION PENDING.

## Gap the list does not cover

Avenues 1 and 2 create long-lived EXTERNAL artifacts (release binaries, a
quickstart) that drift from the repo unless the ship gate grows criteria for
them. If adopted, the article-ship gate adds: "release binaries rebuilt and
fixture-verified against the shipped commit."

## Decisions awaiting the operator

1. Adopt the corrected ranking (1+2 co-built, 3 scoped, 4 on named demand)?
2. Split the content channel into supply-side article + demand-side artifact?
3. Add release-binary criteria to the ship gate; signed or checksummed?
