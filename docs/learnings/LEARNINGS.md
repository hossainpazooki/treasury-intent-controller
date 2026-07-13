# Learnings ledger — index

Pointers only; evidence lives in the dated entries. Entries are immutable: a
wrong entry is superseded by a new dated entry carrying a `kills:` reference,
never edited. Sole writer: `/rigor:handoff` at session close.

| entry | status | one-line hook |
|---|---|---|
| [2026-07-13-tic-scorer-url-full-endpoint.md](2026-07-13-tic-scorer-url-full-endpoint.md) | verified | `TIC_SCORER_URL` is the verbatim POST target — base URL ⇒ 404 ⇒ gate refuses everything (fail-closed, looks like a scorer bug) |
| [2026-07-13-refuted-localhost-ipv6-hypothesis.md](2026-07-13-refuted-localhost-ipv6-hypothesis.md) | refuted-assumption | the ::1/localhost dial theory was wrong — the scorer log showed the requests arriving; missing path was the sole cause |
| [2026-07-13-wheel-lane-import-name-bug.md](2026-07-13-wheel-lane-import-name-bug.md) | verified | wheel-lane skip imported `ke_artifact` but the module is `ke_artifact_py` — the lane could never run anywhere |
| [2026-07-13-atlas-verification-env-is-kind-shaped.md](2026-07-13-atlas-verification-env-is-kind-shaped.md) | verified | one (policy, context) input set verifies ONE artifact kind/corpus; mixed-kind fails closed — binding-side kind-aware env is the recorded debt |
| [2026-07-13-contract-test-equality-blindspot.md](2026-07-13-contract-test-equality-blindspot.md) | verified | RRE's 3-leg equality gate proves agreement, not acceptability — all-legs-identical rejection is green (how the R7/ADR-0021 contradiction stayed latent) |
| [2026-07-13-wsl-userspace-wheel-toolchain.md](2026-07-13-wsl-userspace-wheel-toolchain.md) | verified | ke-artifact-py wheel builds in WSL user-space, no sudo (rustup + musl maturin + get-pip); the standing local Linux lane |
