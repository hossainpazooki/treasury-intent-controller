# Handoff — triad build shipped (Tasks 2–4) and the ATLAS scrub executed

2026-08-16/17 (UTC). Newest commits this brief describes: intent-plane
**`60ffe38`** ("refactor!: de-name the public SDK …") and TIC **`2755384`**
("docs: ATLAS scrub executed …") — both gh-verified on their remotes, both
trees clean at time of writing except this brief's own commit block. Pick-up
measures drift from those two SHAs.

Two operator rulings landed this session that reverse the previous brief
(`2026-08-16-triad-build-setup.md`, which now carries an addendum saying so).
Do not read that brief's "Open / next" as current.

## Current state

- **built — plan Tasks 2–4, committed and pushed** (intent-plane `d558683`,
  `e8ca9d1`, `d36321d`): `verifier/KIT.md` (auditor kit doc),
  `scripts/release.sh` + `/dist/` ignore rule, and the observability
  forwarding section appended to `docs/integration.md`. Gate zero (queued
  ruling 1, the corrected avenue ranking) was **RATIFIED** by the operator
  before any of it ran.
  re-verify: `git -C ~/dev/intent-plane log --oneline -4` shows those three
  above `89443a3`; `ls ~/dev/intent-plane/verifier/KIT.md
  ~/dev/intent-plane/scripts/release.sh`.
- **built — the release kit assembles and self-verifies.** `sh scripts/release.sh`
  exits 0 after cross-compiling four targets, copying the byte-frozen fixture
  pair, and proving good⇒VERIFIED / tampered⇒refuted with the host binary.
  Independently probed rather than trusted: `sha256sum -c SHA256SUMS` all OK,
  all four fixtures `cmp`-identical to `core/contract/feed/`, and exit codes
  0 / 1 / 1 for good / tampered / empty.
  re-verify: `cd ~/dev/intent-plane && sh scripts/release.sh && cd dist/intent-verify-kit-$(git rev-parse --short HEAD) && sha256sum -c SHA256SUMS`
- **built — reproducible-build flag set, corrected mid-plan.** The plan's own
  instruction (`-trimpath` alone) was insufficient; `-buildvcs=false` and
  `-ldflags=-buildid=` are each independently load-bearing. With all three, a
  git-checkout build and a `git archive` extraction build are byte-identical.
  This is now in KIT.md with the measurement behind each flag, and in
  `release.sh`, which records `buildflags` in the kit's VERSION.
  re-verify: run the `re-verify:` line of
  `docs/learnings/2026-08-16-buildid-defeats-trimpath-reproducibility.md`.
- **built — the ATLAS scrub, all three categories plus the private-monorepo
  references** (intent-plane `60ffe38`). `ATLAS`, `COMPASS`, `SCORER_ATLAS*`
  and `treasury-intent-controller` are at ZERO tracked occurrences in the
  public SDK. `SCORER_ATLAS_INPUTS_DIR` → `SCORER_VERIFY_INPUTS_DIR`,
  `SCORER_ATLAS_DIR` → `SCORER_GOLDENS_DIR`, no compatibility shim.
  re-verify: `cd ~/dev/intent-plane && for p in ATLAS COMPASS SCORER_ATLAS treasury-intent-controller; do echo "$p: $(git grep -c "$p" | wc -l)"; done` — all zero.
- **built — full gate green at `60ffe38`**, re-run first-party after the last
  edit: `go build/vet/test ./... -count=1` all pass; the six contractcheck
  pins PASS; `verifier/pyverifier` 6 passed; scorer 42 passed / 5 skipped —
  **identical to the pre-scrub baseline**, which is the evidence the rename
  broke nothing.
  re-verify: `cd ~/dev/intent-plane && go test ./... -count=1 && go test ./core/internal/contractcheck -count=1 && (cd core/scorer && .venv/Scripts/python -m pytest -q)`
- **built — rename coverage proven NON-VACUOUS by mutation.** Reverting
  `__main__.py` to the old env name turns
  `test_partial_resolver_config_refuses_to_boot[SCORER_VERIFY_INPUTS_DIR]`
  red (1 failed, 6 passed); restoring gives 7 passed. The probe was reverted.
  re-verify: `grep -c SCORER_ATLAS ~/dev/intent-plane/core/scorer/src/scorer/__main__.py` ⇒ 0.
- **planned — NOT started: the two internal-reference classes the scrub
  exposed.** (a) This program's own ADR numbers (0006/0007/0009, ~15 refs
  across CONTRACT/README/docs/Go source) point at ADR files that live in the
  PRIVATE monorepo — a public reader cannot follow them. (b) A third private
  project's internals (`regulatory-rule-engine`, ADR-0019/0021, `ke-cli`) —
  8 refs under `core/scorer/`, one of which is a LIVE sibling-checkout path
  the wheel lane needs and cannot be reworded away. Each needs its own ruling.
  re-verify: `cd ~/dev/intent-plane && git grep -n "regulatory-rule-engine\|ADR-00\|ke-cli" | wc -l` ⇒ 24.
- **planned — deferred workstreams unchanged** (plan foot): LangChain (named
  consumer still unmet), R4 exporter (ruling 4), signed releases (ruling 3),
  quickstart landing zone.

## Locked decisions

- **Gate zero was ratified; Tasks 2–4 are committed** — do not re-litigate the
  avenue ranking or redo `d558683`/`e8ca9d1`/`d36321d`.
- **The env rename ships with NO compatibility shim** — reason: explicit
  operator ruling, given after the fail-closed objection below was raised and
  heard. Do not add a shim back without a new ruling.
- **Accepted cost of that ruling, recorded not enforced:** a deployment left
  on a retired env name leaves all three resolver vars unset, so §8's
  all-or-nothing rule selects `NullResolver` and a server that was verifying
  **silently stops verifying** instead of refusing to boot. This is written
  into CONTRACT.md's migration section and is the one place the repo's own
  "never silently not-verify" rule is unenforced against a stale deployment.
  If a future session finds this intolerable, that is a NEW ruling, not a bug
  to quietly fix.
- **Release integrity floor is checksums, not signing** — unchanged; signing
  is queued ruling 3.
- **The ADR-number and third-project reference classes stay untouched** —
  reason: they were discovered mid-scrub and are outside the ruling that was
  given; one of them is load-bearing code. Surface them, do not chase them.
- Inherited, not relitigated: ADR-0011 per-tree ownership; one-gate ship plan;
  handoff entries immutable (this session added an ADDENDUM to the 08-16
  brief rather than editing its body — that is the sanctioned pattern).

## Reuse map

- `~/dev/intent-plane/docs/superpowers/plans/2026-08-16-triad-sdk-development.md`
  (untracked by design, byte-identical copy in this repo's
  `docs/superpowers/plans/`): all 20 steps now checked, with a "Correction
  applied during execution" block recording the build-flag fix. Read it
  before re-deriving any of the shipped text.
- `verifier/KIT.md` — the auditor-facing doc; `scripts/release.sh` copies it
  into the kit as `README.md`. Change one, check the other.
- `scripts/release.sh` — the whole kit assembly, including the explicit
  file list for `sha256sum` (a bare glob would pass the `fixtures/` DIR and
  kill the script under `set -e`).
- Learnings written this session, each with an executable `re-verify:`:
  `2026-08-16-buildid-defeats-trimpath-reproducibility.md`,
  `2026-08-16-atlas-scrub-executed.md`,
  `2026-08-17-bang-in-commit-subject-breaks-git-bash.md`.

## Invariants

- **intent-plane is PUBLIC.** No handoffs, learnings, drafts, or internal
  codenames in tracked files. The ATLAS exception is now CLOSED (zero hits) —
  there is no longer a sanctioned residual, so any new hit is a regression.
- `CONTRACT.md` is amended FIRST, then code, then the pinned tables/docs —
  never the reverse. The scrub followed this order.
- **Never run a mechanical rename over a lineage/history section.** A blind
  `sed` rewrote a CONTRACT migration line so it recorded a rename that never
  happened. History is a claim about the past, not a symbol to substitute.
- `core/contract/feed/*` stay byte-frozen (`-text` in `.gitattributes`); the
  kit copies them with plain `cp`, never rewrites them.
- Go side stays stdlib-only; the kit script adds no dependencies.
- "COMPLETED" is NOT mechanically pinned (prose + grep discipline only);
  "Intent Interface" IS pinned. The author catches the former, not the gate.
- SDK docs are CRLF on disk — use content-matching edit tools, never
  byte-literal `sed` against quoted LF blocks.
- Operator-only git in both repos. When emitting a commit command, a
  conventional-commits `!` marker needs `set +H` or a heredoc or it dies to
  bash history expansion at the operator's prompt — see the 08-17 learning.
- The `sdk-ship` branch and worktree are GONE (fast-forwarded into `main`).
  Any older brief's `re-verify:` line naming `sdk-ship` will fail; use `main`.

## Open / next

1. **Operator: run this brief's commit block** (below) — the brief, the new
   learning, and the index row are the only uncommitted files.
2. **Operator: two new rulings queued by the scrub** — (a) the own-ADR-number
   class: publish the ADRs SDK-side, or de-number the public references?
   (b) the third-private-project class under `core/scorer/`, noting one ref
   is a live path. The rulings queue is now effectively SIX (the four at the
   distribution assessment's foot, plus these two); the ATLAS-scrub ruling
   that was item five is DISCHARGED.
3. **Next build session: regenerate the release kit at the current head.**
   The `dist/` kit on disk was assembled at `89443a3`, pre-scrub, and is
   stale. Rebuild and re-run the independent probes (`sha256sum -c`, the
   fixture `cmp`s, the three exit codes) — `release.sh` exiting 0 is a claim,
   not evidence.
4. **Unverified surface worth closing:** `SCORER_GOLDENS_DIR` is exercised
   ONLY by the wheel lane, which skips on Windows (`ke-artifact-py` absent by
   design). That half of the rename has never run. First Linux/CI run is its
   real test — treat a green there as the confirmation, and a red as expected
   news rather than a surprise.

```bash
cd ~/dev/treasury-intent-controller
git add docs/handoff/2026-08-16-triad-shipped-and-atlas-scrubbed.md \
        docs/handoff/HANDOFF.md \
        docs/learnings/2026-08-17-bang-in-commit-subject-breaks-git-bash.md \
        docs/learnings/LEARNINGS.md
git commit -m "docs: handoff (triad shipped, ATLAS scrubbed) + git-bash bang learning"
git push
```
