# Roadmap — recorded intent and its honest status

Entries here are design intent with named blockers; each row carries its own
status (a row saying BUILT names what was built and what still blocks it —
presenting anything beyond a row's stated status as done is a status-honesty
violation). Source: the intent-plane gate spec §3 (R1–R4) plus session
findings. Fork F4's routing decision is recorded in `CONTRACT.md` §3.3;
structural rulings live in `docs/adr/` (0006 Proposed; 0007, 0008, 0011
Accepted; 0009/0010 reserved, not yet written).

## Roadmap entries (spec §3)

| # | Entry | Blocker |
|---|---|---|
| R1 | **Standard signing envelopes** (DSSE/in-toto bridge) for attestations | **Envelopes BUILT 2026-08-04 at test grade** (`plane/envelope.go`: DSSE-shaped, PAE v1, ed25519; every signature stamped `key_authority: "test"`). Still blocked for production on ADR-0009 production key authority (RRE ADR-0025, PR #19 — verified still OPEN 2026-08-02). Test keys stay and provenance keeps saying so until it lands. **ADR-0009 scope additions (2026-08-05 whole-contract review):** (i) key CLASSES — attestation (control's), record-signing (the GATE's, for tamper-evident records; note the current `TestKeyPossessionBoundary` rule "only control imports plane/authority" must be amended per-class then, since record signing puts a key in the gate), workload identity (R2); (ii) revocation-authority semantics — today ANY trust-root key can revoke ANY spec (invisible with one key; a theory of who-may-revoke is required before a second key enters a root); (iii) the ADR-0007 exact-decimal payload trigger fires BEFORE this lands. |
| R2 | **Workload identity** for role separation (SPIFFE-style) — makes P3 "cannot sign" a deployment-graph fact | Deployment-infrastructure decisions outside this repo. Until R2, key-possession separation is documented, never claimed "enforced". |
| R3 | **Shadow mode as a signed authority state** — enforcement posture inside the spec payload | **BUILT 2026-08-04** behind ADR-0006 (**Proposed** — `docs/adr/2026-08-04-ADR-0006-shadow-recorded-terminal.md`; the canon bump is implemented-and-pinned but the ADR is not ratified; do not quote SHADOW_RECORDED as settled practice until it is). `enforcement_posture` lives inside the signed payload; promotion is a new attestation with a new hash (`control promote`). **Config-toggled shadow mode remains explicitly forbidden — it is a bypass.** |
| R4 | **OTel trace emission as index** ("logs index, gates decide") | **Placement RULED 2026-08-18: EXTERNAL** — a feed-to-OTLP exporter that tails the feed; never in-gate (the gate never learns OTel exists; emission failure cannot touch decisions; core stays stdlib-only). Unbuilt; the near-term Datadog story (log-forward the feed) needs no SDK change. |

## Findings (recorded production-posture gaps)

| Finding | Status | Consequence until closed |
|---|---|---|
| `force_scores` scoring bypass | **CLOSED 2026-08-04; witness POSITIVELY PINNED 2026-08-05**: guarded behind boot env `INTENT_UNSAFE_FORCE_SCORES=1` (else a loud 400 — pinned by `wire_guard_test.go` incl. the empty-map form) and witnessed in the feed (`scorer_id` on every SCORED/RECHECK record, asserted present in `TestDeterminismConditionalOnScores` per `CONTRACT.md` §5.3(h); a 2026-08-05 skeptic pass proved the witness was deletable-with-all-gates-green before the pin). Residual: wherever the flag IS set, the bypass is total; the flag must never reach production. |
| Feed read surface (`GET /v2/events`, `GET /v2/intents/{id}/events`) is unauthenticated | by design (emit-and-observe) | Any network peer can read all ACHIEVED trace fields. A deployment posture decision, not a code defect — record it in any deployment doc. |
| Thinned-set blindness: the gate cannot see that a spec carries fewer criteria than its source document requires | structural | Belongs to the authoring/attestation side: coverage against the source document is what `source_pins` + attester review are for; the gate proves provenance, not coverage. `CONTRACT.md` §4.2 step 1b closed the *vacuous* case only. |
| Criteria are declarant-supplied (P1 asserted, not enforced) | **CLOSED gate-side 2026-08-04**: the wire DTO has no criteria field (loud-400 pinned 2026-08-05, `wire_guard_test.go`); resolution is §2.6 (signature + content address) only. Residuals re-homed by **ADR-0007** (2026-08-05): the scorer-side extraction twin is RETIRED (SpecPayload is THE signed object); the ADR-0003 float debt moves inside the signed payload with a hard pre-R1 exact-decimal trigger; name-shape validation moves to `plane.ParseSpecPayload` (row below). | The thinned-set row above is NOT closed by this: provenance is proven, coverage is not. |
| Empty-string and duplicate criterion NAMES pass the thin-spec defense and can ACHIEVE (2026-08-02 skeptic finding) | recorded, not built | These criteria ARE consulted against the scorer (a live scorer fails closed on unknown names), so they are not vacuous grants — but they are "thin" in a sense step 1b does not cover. Per ADR-0007, name-shape validation now belongs to `plane.ParseSpecPayload` (refuse empty/duplicate names at parse, before attestation can seal them). |
| Draft crossing is unsigned (pitch: "every crossing — including into the AI that drafts — is a signed artifact") | staged, decision recorded 2026-08-05 | `authoring` emits an unsigned draft file; control attests bytes with no provenance chain from the drafting run. Intended close: an authoring INTEGRITY key class (no authority standing — never in the gate's trust root) signing drafts, defined within ADR-0009's key-class work. Until then the pitch sentence is ahead of the mechanism for this one crossing. |
| Terminal records are unwitnessed for type-filtered readers | open decision | `scorer_id` rides SCORED/RECHECK only; a consumer reading `?type=ACHIEVED` (the contract's own settlement pattern) or SHADOW_RECORDED sees no witness. Either add the witness to terminal records (a §2.3 amendment + §5.3 row) or document that witness checks require the per-intent event slice. |
| Refusal decisions are second-class records | **CLOSED 2026-08-08** (memo Q2 answered YES): the refusal-hash commitment is built — the terminal-position record of EVERY completed authorization carries `trajectory_hash` (§2.3 amendment, §4.2 terminal-record rule, §5.4 claim 13, red-first `TestTerminalHashCommitment`). "The decision cannot exist without the record" now covers refusals. | Residual, honestly: step-1 refusals still log no FAILED transition *event* (the terminal classification lives in the synchronous response, §4.2 step 1) — the trajectory is committed, the terminal name is not in the feed for those paths. |
| Verifier cluster (memo seeds S1/S2 + Q4/Q5) | **BUILT 2026-08-08 test-grade**: `verifier/` Go + Python twins, import-pinned to nothing outside their own tree (§7.1 — plant-red proven); golden feed fixture + tampered standing mutant + frozen reports under `core/contract/feed/` (§9.1, generator-tested against the real gate); quickstart probe 9 runs BOTH twins over the live feed and byte-compares (9/9 both OS lanes). First live run refuted a correct feed on `rule_artifact_hash` absence — optionality ruling recorded in §9.1 and pinned by a fixture exemplar. | What it proves today: the record is self-consistent and its hashes recompute. NOT yet provable: the log was never rewritten (R1 signing) or the gate was the sole writer (R2). **Ported to the SDK repo 2026-08-12** under ADR-0011 (consumer packages live SDK-side; the SDK is now the verifier's home, this repo consumes it back — quickstart probe 10 after the declarant landed). |
| Declarant SDK (memo S4) | **BUILT 2026-08-12 test-grade, born SDK-side per ADR-0011** (first package born under the ruling): `declarant/` Go pkg + `intent-declare` CLI — exact §2.2 wire marshal (golden request bytes), `DeriveKey` (deterministic `<scope>:<run>:<tool>:<sha256(args)>`), TOTAL terminal classification with fail-closed `Unknown` (mutant-proven), 500-edge per-intent feed consult (call-order pinned), cursor poll; `force_scores` in NO declarant type (reflection-pinned); §2.7 contract section is the normative text; §7.1 consumer-tree import pin (plant-red proven); consumed back here via quickstart probe 6 (declare ⇒ PROCEED, same-key ⇒ ALREADY_RESERVED, 10/10 both OS lanes). | **Python twin BUILT SDK-side + consumed back 2026-08-18** (`declarant/pydeclarant/`: same golden bytes, total classification, 500-edge call-order proof; LIVE via quickstart probe 7; the LangChain adapter rides in the tree and is LIVE via probe 8 — green on both OS lanes, `langchain-core` in this venv since the same date; joined 2026-08-20 by the MCP gate, LIVE via probes 9 and 10). The adapter's first live run was refuted by the verifier on a reused episode seed (fixed with a per-invocation nonce — see the 2026-08-18 learning). The classification is total over TODAY's closed vocabulary; a new cause class amends §3.3 first, then the table. **Both clients REFUSE to follow HTTP redirects since 2026-08-20** (§2.7): a 3xx answer to a declaration dropped the POST body and let an `ACHIEVED`-shaped 200 from another origin read as authorization for an action never declared — found by a mutation pass in code that had already shipped and been live-proven, and pinned in each lane. |
| Stuck-key remediation is unrecorded | open (runbook gap) | Reservations are permanent by design and `Reserve`-then-`feed.Append`-fail leaves a key reserved behind an HTTP 500 with no ACHIEVED record. The declarant-side rule (consult the feed before any retry) is documented; the OPERATOR-side remediation (how to retire a dead reservation, under whose authority) is not. Day-two question for any real deployment. |
| Human-judgment entries: incompleteness marker or approval channel? | open design question (2026-08-05) | Built semantics: any `human_judgment` entry refuses until a human resolves it at AUTHORING time (new attestation without the entry). The pitch's "routed to a human-judgment check" also supports intent-time human approval as a first-class criterion (per-action officer sign-off) — a core financial-services maker-checker shape the plane cannot currently express. Choosing (ii) later shapes the payload schema; choose before schema freeze at R1. |
| Wire fixtures carry treasury names | open | `core/contract/scorer/*.json` pin `"criterion":"balance"`; exempt from `TestCoreNeutrality`. Follow-up: regenerate with neutral exemplar names in one change that re-greens BOTH byte-compare lanes. |
| Citation and comment polish backlog (2026-08-04 whole-branch review) | open | Small non-behavioral items bundled so they don't scatter: neutrality-test header note for the regex-invisible `intentspec_payment` carve-out; `key-pay-1` test-literal rename (bundle with fixture neutralization); `regexp.Find` -> `FindAll` in the neutrality gate (report all violations, not the first); stale comment cites (`main_test.go` `TestForceScoresStillWins` says §2.4, is §2.2/§2.5; `acceptance_test.go:3` external `§12` anchor; three chain-era phrasings in CONTRACT.md); README invariant list numbering is a restatement, not §5.1's order — add a one-line note; design doc §9 acceptance counts are capture-time (41/5, 46/46) — pointer to the 2026-08-04 handoff for measured values (42/5, 47/0). |

## Next slices (carried from the 2026-07-13 handoff, still current)

1. Durable KV/Postgres settlement-ledger adapter (COMPASS side).
2. ~~Resolver-extraction slice~~ — superseded 2026-08-05 by ADR-0007 (P1
   closed via §2.6; scorer-side extraction retired). Its surviving residuals:
   exact-decimal payload thresholds (hard pre-R1 trigger, ADR-0007) and
   name-shape validation in `plane.ParseSpecPayload`.
3. CI wheel-lane job (Linux). SDK-side (where the lane now lives) the
   lane's ONLY wiring since 2026-08-18 is `SCORER_GOLDENS_DIR` — the job
   must export it; there is no sibling-checkout fallback anymore.
   `_wheel_lane()` already skips-with-reason. NOTE: this repo's `core/scorer`
   copy still carries the pre-rename env names (`SCORER_ATLAS_*`) — the
   trees have diverged since the SDK's 2026-08-16 scrub; the SDK's
   `TestInternalReferencesAbsent` pin (2026-08-18) is what stops a
   mechanical TIC→SDK port from reverting it.

## Distribution (rulings 2026-08-18, recorded in
`docs/research/2026-08-14-distribution-avenues-assessment.md` addendum)

- **LangChain adapter SDK: SHIPPED 2026-08-18** (live-proven, quickstart
  probe 8; the operator override of “avenue 4 on named demand” is spent).
- **MCP gate: SHIPPED 2026-08-20** (ruling 7 of the same date, built the
  same day; born SDK-side per ADR-0011 and consumed back here): fastmcp
  intent-gate middleware (`IntentGateMiddleware`) for a server the operator
  owns, plus `gated_proxy` for one the operator does NOT own — the fronted
  backend never changes and never sees a refused call. Live-proven by
  quickstart probes 9 and 10 (the proxy's INNER counter is the observable,
  and probe 9 carries a second, independent middleware instance refusing the
  same retry — the stateless multi-replica leg). `fastmcp` is the second
  sanctioned exception to pydeclarant's stdlib-only rule; its tests skip
  visibly where it is absent. Recorded residual, NOT closed: on the proxy
  path an ambiguous `anyOf`/`oneOf` union is keyed as spelled — a standing
  fail-open (`CONTRACT.md` §2.7).
- **Datadog + R4 OTel exporter: DEFERRED (ruling 7, 2026-08-20), kept
  platform-side (the declarant's side)** — near-term Datadog story stays
  log-forward the feed (no SDK change); exporter stays external per R4; no
  build until a new ruling.
- **Content channel: split** — platform-side article + audit-side artifact.
  (Naming ruling 2026-08-21: the two sides are **platform / audit** on the
  product surface and **declarant / verifier** in documentation; the
  former "supply / demand" pair is retired from living docs — dated records
  keep it. First audit-side artifact target: regulatory reporting, per the
  2026-08-21 plan.)
- **Release signing: deferred** ("not for now") — checksums stay the floor.
