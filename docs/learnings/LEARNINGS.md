# Learnings ledger — index

anchor-rule-since: 2026-07-14

Pointers only; evidence lives in the dated entries. Entries are immutable: a
wrong entry is superseded by a new dated entry carrying a `kills:` reference,
never edited. Sole writer: `/rigor:handoff`.

Each entry is anchored to the moment its basis was **captured** — `ts:` is when
the finding landed and `commit:` is what HEAD was then. The six 2026-07-13
entries below predate that rule: they were written as one batch at session
close and all carry the same `ts:` and `commit:`, which is how a mid-session
test count came to be stamped with a commit that post-dated it (killed by
`2026-07-14-wheel-lane-count-corrected.md`). They are immutable and stay as
written; `anchor-rule-since:` above is the dated line from which
`check-learnings` enforces distinct capture instants.

| entry | status | one-line hook |
|---|---|---|
| [2026-07-13-tic-scorer-url-full-endpoint.md](2026-07-13-tic-scorer-url-full-endpoint.md) | verified | `TIC_SCORER_URL` is the verbatim POST target — base URL ⇒ 404 ⇒ gate refuses everything (fail-closed, looks like a scorer bug) |
| [2026-07-13-refuted-localhost-ipv6-hypothesis.md](2026-07-13-refuted-localhost-ipv6-hypothesis.md) | refuted-assumption | the ::1/localhost dial theory was wrong — the scorer log showed the requests arriving; missing path was the sole cause |
| [2026-07-13-wheel-lane-import-name-bug.md](2026-07-13-wheel-lane-import-name-bug.md) | verified | wheel-lane skip imported `ke_artifact` but the module is `ke_artifact_py` — the lane could never run anywhere |
| [2026-07-13-atlas-verification-env-is-kind-shaped.md](2026-07-13-atlas-verification-env-is-kind-shaped.md) | verified | one (policy, context) input set verifies ONE artifact kind/corpus; mixed-kind fails closed — binding-side kind-aware env is the recorded debt |
| [2026-07-13-contract-test-equality-blindspot.md](2026-07-13-contract-test-equality-blindspot.md) | verified | RRE's 3-leg equality gate proves agreement, not acceptability — all-legs-identical rejection is green (how the R7/ADR-0021 contradiction stayed latent) |
| [2026-07-13-wsl-userspace-wheel-toolchain.md](2026-07-13-wsl-userspace-wheel-toolchain.md) | verified | ke-artifact-py wheel builds in WSL user-space, no sudo (rustup + musl maturin + get-pip); the standing local Linux lane |
| [2026-07-14-wheel-lane-count-corrected.md](2026-07-14-wheel-lane-count-corrected.md) | verified | **kills** the 07-13 count: the WSL lane is **46 passed**, not 39 — the basis was captured before the commit it was stamped with |
| [2026-07-14-wheel-lane-depends-on-sibling-checkout-state.md](2026-07-14-wheel-lane-depends-on-sibling-checkout-state.md) | verified | the wheel lane FAILS (not skips) when the sibling ATLAS checkout lacks `fixtures/artifacts/intentspec_payment/` — local main is behind origin/main |
| [2026-07-14-payment-loop-live-probe-with-controls.md](2026-07-14-payment-loop-live-probe-with-controls.md) | verified | loop re-probed live with output captured: 2 grants / 3 refusals, outage control non-vacuous (kill ⇒ refuse, restore ⇒ ACHIEVED) |
| [2026-08-02-vacuity-asymmetry-gate-vs-resolver.md](2026-08-02-vacuity-asymmetry-gate-vs-resolver.md) | verified | the Go gate ACHIEVEd zero-criteria declarations (even zero-config) while the Python resolver had already closed the identical `all([])` hole — closed by gate step 1b, red-first |
| [2026-08-04-wheel-lane-count-47.md](2026-08-04-wheel-lane-count-47.md) | verified | **kills** the 07-14 count (count only): the zero-skip lane is **47** at this tree; and the post-move `parents[N]` off-by-one made a live auto-detect skip read as by-design -- the 07-13 masked-skip shape recurred |
| [2026-08-04-second-platform-first-run.md](2026-08-04-second-platform-first-run.md) | verified | quickstart.sh's FIRST WSL run silently broke the Windows venv (re-venv of an existing gitignored dir -- invisible to git status); fix = never re-venv + explicit fallback; detection = re-run the other platform's lane |
| [2026-08-05-split-patch-equivalence-check.md](2026-08-05-split-patch-equivalence-check.md) | verified | applying split patches then reverse-checking the FULL patch proves the splits byte-equivalent -- "the five commits equal the one diff" as a two-second proof |
| [2026-08-05-untested-guards-deletable-witness.md](2026-08-05-untested-guards-deletable-witness.md) | verified | the amendment red-first-tested its refusals but shipped guards untested: the scorer_id witness was deletable with every gate green until positively asserted (plant-red proven) |
| [2026-08-05-import-graph-pins-only-the-seam.md](2026-08-05-import-graph-pins-only-the-seam.md) | refuted-assumption | "cannot sign -- an import-graph fact" refuted: stdlib ed25519 compiles in authoring with contractcheck green; the graph pins the plane/authority SEAM, capability claims belong to R2 |
| [2026-08-05-float-debt-in-signed-bytes.md](2026-08-05-float-debt-in-signed-bytes.md) | verified | float64 thresholds replicated INTO the signed payload -- representation debt in content-addressed bytes costs re-attest-everything; ADR-0007 pins the pre-R1 exact-decimal trigger |
| [2026-08-13-mid-ladder-probe-renumbering-sweep.md](2026-08-13-mid-ladder-probe-renumbering-sweep.md) | verified | inserting a probe mid-ladder renumbers every later ordinal across doc tables, contract claim rows, and the scripts' OWN comments -- 8 contradictory sites, caught only by a skeptic (prose has no gate) |
| [2026-08-13-gap-record-outlives-gap-closure.md](2026-08-13-gap-record-outlives-gap-closure.md) | verified | the note that RECORDS a gap survives its closure unless the closing change greps for it -- CONTRACT §2.2 still said "no guard" 9 days after the force_scores guard shipped, contradicting §2.5 in the same file |
