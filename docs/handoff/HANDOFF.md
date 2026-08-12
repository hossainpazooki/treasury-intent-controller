# Handoff index

Pointers only; state and evidence live in the dated entries. Entries are
immutable once written — a later session writes a new entry, never edits an
old one. Pick up with `/rigor:pickup`, which re-verifies a brief's claims
instead of trusting them.

| brief | scope | one-line hook |
|---|---|---|
| [2026-07-13-atlas-treasury-payment-loop.md](2026-07-13-atlas-treasury-payment-loop.md) | tic + RRE + COMPASS | reader slice merged, ADR-0022/PR #14 merged, Stage C(b) at PR #7, full loop probed green live; next = merge #7, KV ledger, extraction slice |
| [2026-08-02-intent-plane-gate-interface.md](2026-08-02-intent-plane-gate-interface.md) | tic | thin-spec defense (empty-criteria + volatility, red-first), boundary+vocab gates, CONTRACT-INTERFACE, P1 falsified in pitch register; commits pending operator |
| [2026-08-04-intent-plane-repositioning.md](2026-08-04-intent-plane-repositioning.md) | tic -> intent-plane | core/treasury split, one CONTRACT.md, neutral core; 7 acceptance lanes green (WSL wheel 47/0); ALL uncommitted on `restructure/intent-plane`; commit blocks + GitHub rename = operator |
| [2026-08-04-plane-roles-amendment-design.md](2026-08-04-plane-roles-amendment-design.md) | intent-plane | the amendment's design record (built in a web session on `bec7589`): role trees, signed specs (§2.6), shadow posture, guards+witness; what it did NOT make true |
| [2026-08-05-plane-roles-amendment-verified-unpushed.md](2026-08-05-plane-roles-amendment-verified-unpushed.md) | intent-plane | amendment landed locally + fully verified (both quickstarts 8/8, skeptic F1-F4 pinned, ADR-0007 Accepted); DELIBERATELY UNPUSHED — tomorrow = review + the 6-commit sequence in the brief; tree is uncommitted, do not stash/checkout |
| [2026-08-12-verifier-port-and-adr-0011-per-tree-ownership.md](2026-08-12-verifier-port-and-adr-0011-per-tree-ownership.md) | both repos | ADR-0011 Accepted (consumer packages live SDK-side, port per tree); verifier cluster ported to intent-plane, all gates green both repos; commits pending operator; next = declarant SDK (born SDK-side), ADR-0010 re-scope (rescue drafts from Temp) |
