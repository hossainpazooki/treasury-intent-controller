# Article claim ledger — what "Signed, Sealed, Deliverable" can print, at intent-plane `42ce556`

2026-08-14 (UTC). Derived from workflow run `wf_e9a59f3f-9e8` (10 agents: five
disjoint recon slices — signed / sealed / deliverable / embedding / seed-drift —
each refuted by a dedicated skeptic that re-ran the cited pinned tests in the
`sdk-ship` worktree at `42ce556`). 43 findings; 41 survived as filed; 2 were
corrected in refutation (marked below). Controller spot-checked both corrections
independently by grep. Full agent transcripts: session
`0295a4ce-41cc-4e54-a674-1818455dbf7c`, workflow journal `wf_e9a59f3f-9e8`.

Scope honesty: this ledger covers the SDK repo at one commit. It does NOT cover
the Python scorer service internals, any live end-to-end claim (quickstart
"10/10", probes 6/10 — cross-repo, verify in this monorepo at drafting time),
performance, or the R4 OTel story. Venue/positioning judgment was out of scope.

## Bucket A — honest present tense (print as-is; each has a pinned test)

1. The bytes a human attester signs are the bytes the gate executes — hash
   equality, not document alignment; one flipped payload byte refuses.
   (`TestTamperedPayloadRefuses`, `TestAttestVerifyRoundTrip`, plane/;
   CONTRACT §2.6.)
2. Criteria cannot ride the wire — the field does not exist in the DTO;
   unknown fields are a loud 400 (`DisallowUnknownFields`); the declarant
   types have no criteria field either (`TestGoldenRequestBytes`).
3. Revocation is an authority act — only a trust-root-signed tombstone
   revokes (`TestForgedTombstoneDoesNotRevoke`); re-checked at the dispatch
   edge; a mid-flight pull stops the action with the key unreserved
   (`TestRevokedBetweenVerifyAndDispatch`).
4. Zero-config authorizes nothing — no trust root ⟹ every spec unattested
   (`TestUnattestedSpecRefuses`); no scorer ⟹ every criterion unevaluable
   (`TestZeroConfigRefusesEverything`).
5. Attestation does not launder vacuity — a signed spec with zero criteria
   refuses before any scoring (`TestFailClosedEmptyCriteria`,
   `TestEmptyCriteriaRefusedOverWire`).
6. Volatile facts are re-verified at the last moment; anything short of exact
   pass is FAILED_AT_DISPATCH and nothing settles (`TestVolatileReVerify`,
   `TestTerminalSeparation` — zero ACHIEVED records).
7. At-most-once across requests and process restart — reservation fsynced
   before success (`TestRestartAtMostOnce`, `TestConcurrentReserveDurable`,
   idempotency store restart clause).
8. Every decision lands in a durable append-only feed, fsynced before the
   gate reports success, sequence recovered across restart
   (`TestRecoveryAcrossReopen`, `TestGlobalSeqMonotonic`).
9. Every completed authorization — grants, shadows, refusals — commits a
   recomputable trajectory hash, so a trimmed or edited log is DETECTABLE BY
   RECOMPUTATION (that exact phrase is the honest scope;
   `TestTerminalHashCommitment`, CONTRACT §2.3).
10. An independent verifier ships in the SDK — Go + stdlib-only Python twins,
    byte-identical canonical reports, import-pinned to run none of the gate's
    code (`TestImportBoundary` consumer-tree rule), with a frozen tampered
    fixture (one byte, `PASS`→`PAST`) that must refute forever in both
    languages (`TestFixtureTamperedRefutes`, pyverifier 6).
11. The verifier is itself tri-state fail-closed — empty feed, unknown event,
    missing terminal hash are unverifiable, never verified
    (`TestEmptyFeedUnverifiable`, `TestNoTerminalHashAbstains`).
12. The declarant SDK encodes the discipline — golden-pinned wire bytes,
    total terminal classification with fail-closed `Unknown`
    (`TestClassificationTotal`), and on HTTP 500 it consults the per-intent
    feed before deciding anything (`TestDeclare500ConsultsFeedBeforeDeciding`,
    call-order pinned).

## Bucket B — true only with the stated caveat

| claim | exact qualifier |
|---|---|
| "unevaluable never passes", system-wide | on any boot without `INTENT_UNSAFE_FORCE_SCORES=1`; under that test flag force_scores is a total bypass (feed-witnessed via scorer_id) |
| "the drafting side structurally cannot sign" | a code-graph fact (import pins); the deployment half — workload identity, R2 — is asserted, not demonstrated |
| "standard DSSE envelopes" | say DSSE-SHAPED: signatures cover the exact DSSE v1 PAE, but each signature block carries an extra `key_authority` field |
| "deterministic, byte-identical replay" | conditional on scores — live fact drift is the one permitted nondeterminism source (the dispatch recheck exists to catch it) |
| "the feed alone tells an auditor how every intent ended" | step-1 refusals commit the hash but log no FAILED transition event — their classification lives in the synchronous response (CONTRACT §3.2) |
| "DeriveKey makes retries dedup" | canonicalizing args is the CALLER's duty; the SDK hashes the bytes it is given |
| "declare once, every agent inherits it" | an architectural pattern the SDK encodes, not something it enforces; live proof is quickstart probe 6 (cross-repo) |
| revocation, generalized to multi-key roots | revocation authority is flat today (any root key revokes any spec) — who-may-revoke is ADR-0009 scope |

## Bucket C — correct-shaped lies (never in present tense; print the replacement)

1. "Agents cryptographically sign their intents" — INVERTED. Agents hold no
   keys and sign nothing; the human attester signs the SPEC, and the agent's
   declaration must resolve to it.
2. "The gate holds the signing keys" — INVERTED. The gate verifies against a
   trust root; the SDK repo contains no signing seat at all.
3. "Enterprise-grade key authority" — every signature stamps
   `key_authority: "test"`; production key authority is ADR-0009/R1.
   (CORRECTED IN REFUTATION: filed as needs-caveat; the skeptic showed the
   caveat negates the load-bearing adjective rather than bounding it.)
4. "Records are signed / the log is tamper-proof" — record signing is staged
   (R1); the verifier proves self-consistency, and a consistently rewritten
   log would pass today. Say "tamper-evident by recomputation".
5. "Workload identity ensures only the gate writes records" — staged (R2);
   today sole-writer is a single-process mutex code fact.
6. "Exactly-once execution" — false on both words: AT-MOST-once, and
   AUTHORIZATION — the gate executes nothing (emit-and-observe).
7. "A duplicate is absorbed and returns the original decision" — no; refused
   loudly (idempotency-collision); the caller reconciles from the feed. The
   gate never silently replays a grant.
8. "FAILED_AT_DISPATCH rolls the action back" — nothing is ever in flight;
   the drift is caught BEFORE authorization; there is nothing to roll back.
9. "Human approval workflows route judgment calls to officers" — what exists
   is refusal (`unevaluable:human-judgment:<name>`); the routing/approval
   channel is open work (ADR-0010, unlanded).
10. "The SDK ships in Go and Python" — the VERIFIER has a Python twin; the
    DECLARANT is Go-only (Python twin recorded future work).
11. "exit 0 from intent-declare means authorized" — exit 0 includes
    SHADOW_RECORDED, which authorizes nothing; scripts must parse `class=`.
    The CLI package has no test files — do not showcase what nothing pins.
12. "The authoring pipeline is signed end to end" — authoring is keyless BY
    DESIGN; there is exactly one signed crossing (attestation), and that is
    the point, not a gap.
13. Vocabulary: "Intent Interface" (retired; test-banned in README/CONTRACT)
    and "COMPLETED" (the wire terminal is ACHIEVED). NOTE (corrected in
    refutation): the COMPLETED ban is CONTRACT §1.3 prose plus a verified
    zero-occurrence grep, NOT a mechanized gate — the vocab test pins only
    the forbidden actor nouns and the retired proper noun. The article will
    not trip a test; it has to trip the author.

## Drafting rules distilled

- Attach "signed" to the SPEC (attester's act), never to intents, records,
  or the pipeline.
- Prefer the repo's own phrases — they were engineered honest: "detectable by
  recomputation", "DSSE-shaped", "at-most-once", "abstention is the system
  working", "the worst case is an action that wrongly waits".
- Any live-demo number ("10/10") gets re-verified in this monorepo the day
  the article cites it.
