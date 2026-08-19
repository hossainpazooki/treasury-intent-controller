# Handoff — the LangChain channel built and live-proven, six rulings executed

2026-08-18/19 (UTC). Newest commits this brief describes: intent-plane
**`d9041f5`** ("fix: fresh per-invocation episode seed in the adapter") and
TIC **`4827d3c`** ("feat: adapter live leg (probe 8) + fresh-episode seed
fix") — both operator-run; both trees clean at time of writing except this
brief, two learnings entries, and the two index rows. Pick-up measures
drift from those two SHAs.

One session ran the whole arc: pickup of the 08-16 brief → six operator
rulings → de-number/re-architect scrub + a seventh contractcheck pin →
release-kit regen (probed) → Python declarant twin (11-agent fanout) →
LangChain adapter (plan adversarially refuted 5×, then TDD) → post-build
rulings (timeout/kit/unicode) → live legs for twin AND adapter, during
which **the live verifier refuted the adapter's first run and forced a
design fix**. Eight operator-run commits landed along the way (SDK
`ce21168` `9bf8d16` `0207a15` `44c9ae2` `9271ec7` `d9041f5`; TIC `8926592`
`f3caae4` `4827d3c`).

## Current state

- **built — six queued rulings answered and recorded.** (1) ranking adopted
  with override: LangChain SDK first, Datadog roadmapped; (2) content
  channel split; (3) signing NOT FOR NOW; (4) R4 exporter EXTERNAL;
  (5) own-ADR numbers de-numbered; (6) third-project refs re-architected.
  Recorded in the 08-14 assessment's ADDENDUM + `docs/ROADMAP.md`
  (R4 row + Distribution block).
  re-verify: `grep -n "ADDENDUM" docs/research/2026-08-14-distribution-avenues-assessment.md && grep -n "Placement RULED" docs/ROADMAP.md`
- **built — SDK internal-reference scrub complete and MECHANIZED** (SDK
  `ce21168`): all 25 own-ADR/third-project refs gone (substance kept in
  place); wheel lane's ONLY wiring is `SCORER_GOLDENS_DIR` (sibling
  fallback deleted — CI must export it); SEVENTH pin
  `TestInternalReferencesAbsent` holds ATLAS/COMPASS/SCORER_ATLAS/the
  monorepo name/the third project/ke-cli/ADR-00 at zero tracked,
  mutation-proven.
  re-verify: `cd ~/dev/intent-plane && go test ./core/internal/contractcheck -count=1 && for p in ATLAS ADR-00 ke-cli; do git grep -c "$p" -- ':!core/internal/contractcheck/internal_refs_test.go' | wc -l; done` — pins ok, three zeros.
- **built — Python declarant twin** (SDK `9bf8d16`; consumed back TIC
  `f3caae4`): `declarant/pydeclarant/` declare.py + client.py, same frozen
  golden bytes, total classification w/ fail-closed UNKNOWN, 500-edge
  call-order proof. Built via an 11-agent fanout (contract/scaffold/4
  builders/integration-runner/4 skeptics — 3 SURVIVES, 1 refuted-on-wording:
  test files use pytest, so "stdlib-only" holds for shipped modules only,
  as docs now say exactly).
  re-verify: `cd ~/dev/intent-plane && core/scorer/.venv/Scripts/python -m pytest declarant/pydeclarant -q` — 30 passed.
- **built — LangChain adapter** (SDK `0207a15` + hardened + `d9041f5`):
  `gate_tool` executes ONLY on a fresh synchronous `Proceed`; IntentRefused
  carries class + `same_key_retry_safe`; consult-path `Proceed` refuses
  `ALREADY_ACHIEVED`; canonicalization recipe (model_dump → sorted-key
  compact JSON) cannot fork keys; string input refused pre-declaration;
  **episode seed = key + fresh uuid nonce per invocation** (see the
  refutation below). CONTRACT claim 16 with the mutant named inline.
  re-verify: same lane as above (the adapter's 15 tests ride in it) plus `git -C ~/dev/intent-plane grep -n "uuid.uuid4" declarant/pydeclarant/langchain_adapter.py`.
- **built — bounded default client timeout, both SDKs** (SDK `44c9ae2`,
  ported to TIC in `f3caae4`): 30s (`DefaultTimeout`/`DEFAULT_TIMEOUT`);
  unbounded = explicit opt-in. CONTRACT §2.7 client-timeout rule.
  re-verify: `cd ~/dev/intent-plane && go test ./declarant -run TestDefaultClientIsBounded -count=1 -v`
- **built — the quickstart is a 12-probe ladder, 12/12 BOTH OS lanes**
  (TIC `f3caae4` + `4827d3c`): probe 7 = twin live
  (`treasury/probes/pydeclarant_live.py`), probe 8 = adapter live
  (`treasury/probes/adapter_live.py`), feed-count expects 4 ACHIEVED,
  verifier recompute is probe 12 and covered 13 intents VERIFIED with
  byte-identical twin reports. The ladder bootstraps langchain-core
  one-time where absent.
  re-verify: `cd ~/dev/treasury-intent-controller && powershell -File treasury\quickstart.ps1` — final line `RESULT: 12/12 probes passed`.
- **built — the adapter's first live run was REFUTED and fixed.** Run 1 was
  11/12: the verifier refuted the whole live feed (`intent-seq-dup:0`)
  because the key-derived episode seed redeclared one intent id on
  same-args retries — invisible to all 30 lane tests, 7 pins, and 2
  skeptic passes (per-intent seq contiguity exists only at the system
  level). Fix: fresh per-invocation nonce, CONTRACT amended FIRST both
  repos, regression pinned in the lane.
  re-verify: read `docs/learnings/2026-08-18-live-verifier-refutes-adapter-seed-reuse.md` and run its re-verify line.
- **built — release kit regenerated AND probed at `ce21168`** (sha256 10/10
  OK, four fixtures cmp-identical, exit codes 0/1/1; the stale pre-scrub
  `89443a3` kit deleted by ruling). NOTE: head has moved 4 commits since —
  the kit is stale again BY DESIGN; regeneration is a ship-time act per
  the ship-gate criterion, not a per-commit one.
  re-verify: `ls ~/dev/intent-plane/dist/` — exactly `intent-verify-kit-ce21168`.
- **planned — NOT started:** the demand-side artifact and the supply-side
  article (ruling 2's split); the R4 external exporter (ruled, unbuilt);
  Datadog beyond feed log-forward.

## Locked decisions

- **The six 2026-08-18 rulings** (assessment addendum) — reasons recorded
  there; ranking override note: LangChain was built AHEAD of the
  named-consumer gate by explicit operator instruction.
- **Fresh-episode seed rule (CONTRACT §2.7, both repos):** same-key retries
  are same-key/fresh-episode; a reused seed redeclares an intent id and the
  verifier refutes the feed. Do not "simplify" the adapter's nonce away —
  the live ladder proved why it exists.
- **`ALREADY_ACHIEVED` (adapter-level):** a consult-path `Proceed` is
  historical; the consequence is never re-fired. Execution requires a
  synchronous `Proceed`. Reason: at-most-once at the adapter layer
  (lost-response case).
- **Bounded 30s timeout default** — operator ruling superseding
  faithful-to-Go `None`; unbounded is opt-in only.
- **NFC/NFD unicode keys are CORRECT AS-IS** (distinct code points =
  distinct calls); no normalization without a new ruling.
- **Signing stays NOT FOR NOW** — checksums remain the release floor.
- **Claims tables are NUMBER-DIVERGED by design:** TIC claim 16 = chassis
  flow test; SDK claim 16 = adapter. Never mechanically "sync" claim
  numbers across the repos.
- Inherited, unchanged: ADR-0011 per-tree ownership; shim-free env rename +
  its accepted silent-not-verify cost; operator-only git; handoff entries
  immutable.

## Reuse map

- `declarant/pydeclarant/` (both repos, diff-identical): the twin + adapter.
  `test_langchain_adapter.py`'s `_Script`/`_serve` double captures
  method/path/body and has an `own_feed` mode keyed to the last-POSTed
  seed — reuse it for any future adapter-shaped test.
- `treasury/probes/pydeclarant_live.py` + `adapter_live.py` — the live-leg
  probe pattern (self-asserting, ASCII, exit-code + parseable line).
- The adversarially-verified plan with all addenda:
  `docs/superpowers/plans/2026-08-18-langchain-adapter.md` (untracked by
  design, byte-identical in both repos) — five pre-build refutations and
  the live-leg misfire are recorded there; read it before extending the
  adapter.
- `core/internal/contractcheck/internal_refs_test.go` (SDK) — the pattern
  for banning strings repo-wide with walk-skips and self-exemption.
- `scripts/release.sh` (SDK) + `verifier/KIT.md` — the kit assembly and its
  reproducible-build flags.

## Invariants

- **intent-plane is PUBLIC**; the seventh pin now mechanically bans every
  internal-reference class — a mechanical TIC→SDK port cannot silently
  revert the scrub (TIC's `core/scorer` copy STILL carries the old
  `SCORER_ATLAS_*` names; that divergence is recorded, the pin is the
  guard).
- `CONTRACT.md` amended FIRST, then code, then pinned tables — held for
  every change this session, including mid-incident.
- The declarant trees stay copy-clean:
  `diff -r declarant ../intent-plane/declarant --exclude=__pycache__ --exclude=.pytest_cache`
  must be empty; a change on either side ports whole-file.
- The quickstart's 12/12 IS the gate: feed-count expects exactly 4
  ACHIEVED, and any new declaring probe must bump it AND land BEFORE the
  feed-count/recompute probes so the verifier covers it.
- Adapter tests skip VISIBLY without langchain-core; the shipped twin
  modules stay stdlib-only; the adapter is the one sanctioned exception.
- `core/contract/feed/*` byte-frozen; goldens never regenerated casually
  (`DECLARANT_GOLDEN_WRITE=1` is the only path).
- Operator-only git; cp1252 console (ASCII probe output); the `!` marker
  hazard in emitted commit subjects (08-17 learning).

## Open / next

1. **Operator: run this brief's commit block** (below) — the brief, two
   learnings entries, and the two index rows are the only uncommitted
   files in TIC; the SDK tree is clean.
2. **Next build session, operator's pick:** the demand-side artifact or the
   supply-side article (ruling 2's split). The article now has a strong
   verified spine: two "first live run refuted something real" episodes
   (the verifier's 08-08, the adapter's 08-18) — the honest-sales line the
   research memo wanted.
3. **Standing unexercised surfaces:** the wheel lane's `SCORER_GOLDENS_DIR`
   half still awaits its first Linux/CI run; `spec_envelope` byte-parity is
   unpinned (no golden exercises it); the release kit is regenerated at
   ship time (currently at `ce21168`, 4 commits behind — deliberate).
4. **Recorded divergence to watch:** TIC's `core/scorer` still carries
   pre-rename env names; a future TIC→SDK scorer port must rename, and the
   SDK pin will catch it if it doesn't.

```bash
cd ~/dev/treasury-intent-controller
git add docs/handoff/2026-08-18-langchain-channel-live-and-rulings-executed.md \
        docs/handoff/HANDOFF.md \
        docs/learnings/2026-08-18-git-checkout-revert-clobbers-earlier-edit.md \
        docs/learnings/2026-08-18-py314-function-local-models-break-tool-schemas.md \
        docs/learnings/LEARNINGS.md
git commit -m "docs: handoff (LangChain channel live, six rulings) + 2 learnings"
git push
```
