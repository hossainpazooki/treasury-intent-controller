# Handoff — triad build setup (GRC kit · Datadog doc · LangChain-to-gate)

2026-08-16 (UTC). Newest commits this brief describes: intent-plane
`sdk-ship` branch at **`89443a3`** ("docs: honesty fixes …" — the operator
ran it mid-session; tree verified clean after), TIC remote **`191a13f`**
(gh-verified: both earlier 08-16 blocks ran — the venue handoff is on the
remote and the mesh/Astio research commit is head). Pick-up measures drift
from those two SHAs. Only the files in THIS brief's commit block are
uncommitted as of writing.

## Current state

- **built — the four honesty fixes, committed** at intent-plane `89443a3`:
  CONTRACT §1.2 codenames out (COMPASS zero tracked hits), README
  "commits to a durable record" wording, assurance declarant row names its
  blocker (live-path proof is cross-repo), architecture cites
  `TestRestartAtMostOnce` + `TestRecoveryAcrossReopen`. Full gate + all six
  contractcheck pins re-run green after the edits.
  re-verify: `git -C ~/dev/intent-plane log --oneline -1 sdk-ship` shows
  `89443a3`; `git -C ~/dev/intent-plane grep -c COMPASS sdk-ship` exits 1.
- **built — the triad build plan, adversarially verified**:
  `docs/superpowers/plans/2026-08-16-triad-sdk-development.md` in the
  sdk-ship WORKTREE (`~/dev/intent-plane/.claude/worktrees/sdk-ship`),
  untracked by design (`docs/superpowers/` in `.git/info/exclude` — the
  repo is PUBLIC). Refutation pass ran the four moves: gate re-executed,
  fixture byte-exactness re-run first-party, two skeptics dispatched; three
  refutations found (ATLAS scope, KIT.md reproducibility instruction,
  "pinned" overclaim on the Python twin) and all folded back into the plan.
  re-verify: read the plan; its Task 1 is checked off by `89443a3`; grep it
  for "Gate zero" and "Full ATLAS scrub".
- **built — two learnings entries** (this repo, `docs/learnings/`):
  `2026-08-16-atlas-codename-residual.md` (26 lines/6 files incl.
  `SCORER_ATLAS_*` env vars survive in the public repo) and
  `2026-08-16-plain-gobuild-irreproducible.md` (plain vs `-trimpath`
  hashes differ on the same machine/commit/toolchain; checkout path
  embedded). Both carry first-party bases at `89443a3`.
  re-verify: run each entry's own `re-verify:` line.
- **built — SDK worktree CLAUDE.md note (2026-08-16)** recording all of the
  above for the next SDK session (local-only file).
  re-verify: read the worktree `CLAUDE.md`, second note block.
- **planned — plan Tasks 2–4, NOT started**: Task 2 `verifier/KIT.md`
  (auditor kit doc, corrected text ready in the plan), Task 3
  `scripts/release.sh` (cross-compile + fixture self-verify + SHA256SUMS;
  script code POSIX-attacked and ready), Task 4 `docs/integration.md`
  observability section (text ready; feed filename verified at
  `core/internal/durable/store.go:75`). Blocked SOLELY on Gate zero below.
- **planned/deferred — four workstreams behind gates** (plan foot):
  LangChain (named consumer unmet), R4 exporter (ruling 4), signed releases
  (ruling 3), full ATLAS scrub (new ruling — breaking config rename).

## Locked decisions

- **Task 1 ran inline and is committed** — do not redo or amend `89443a3`.
- **Gate zero is NOT cleared**: executing Tasks 2–4 constitutes adopting
  queued ruling 1 (the corrected avenue ranking). Reason: the ranking's
  adoption is an explicitly queued operator decision; the build session
  must get a yes before Task 2. The operator has SEEN the plan and the
  gate-zero question; no answer yet.
- **ATLAS scrub deferred behind its own ruling** — reason: 26 residual
  lines include `SCORER_ATLAS_INPUTS_DIR`/`SCORER_ATLAS_DIR`, a breaking
  config-surface rename with a deprecation story, not a doc pass. Do NOT
  chase it as edits; the learnings entry is the evidence.
- **Release integrity floor is checksums, not signing** — reason: signing
  drags ADR-0009 forward and is queued ruling 3; the script ships
  SHA256SUMS either way.
- **LangChain is planned only to its gate** — reason: avenue-4
  named-consumer condition probed 2026-08-16 (consumer-signal note) and
  unmet; first deliverable when it clears is the Python declarant twin,
  never a direct adapter.
- Inherited, not relitigated: one-gate ship plan; demand-side-first spine;
  ADR-0011 per-tree ownership; handoff entries immutable.

## Reuse map

- The plan file IS the build script for later this week: exact old/new doc
  texts, the full `release.sh` body, the full KIT.md body, the Datadog
  section text — all adversarially corrected. Do not re-derive them.
- Frozen fixture pair + reports (`core/contract/feed/`, all four `-text`
  verified via `git check-attr`) — the kit packages them as-is.
- Research docs in this repo: `2026-08-16-mesh-composition-and-surface-framing.md`
  (Istio/Astio + ext_authz fail-open landmine for any future mesh doc),
  `2026-08-16-consumer-signal-platform-architect.md` (names Astio),
  `2026-08-14-article-claim-ledger.md` (drafting rules incl. policy-layer
  and category-not-empty).
- The 08-14 and 08-16 handoff briefs for deeper history; this brief
  supersedes their "next" sections.

## Invariants

- intent-plane is PUBLIC: no handoffs, learnings, drafts, or internal
  codenames in tracked files. The ATLAS residual is a KNOWN, ruled-deferred
  exception — not a license to add more, not a quick fix.
- Operator-only git in both repos; SDK pushes expose branch names too.
- `CONTRACT.md` amended first, then pinned tables/docs. `core/contract/feed/*`
  stay byte-frozen. Go side stays stdlib-only — the kit script adds no deps.
- "COMPLETED" is NOT mechanically pinned (prose + grep discipline only) —
  the author catches it, not the gate. "Intent Interface" IS pinned.
- SDK docs are CRLF on disk; the plan's quote blocks are LF — use
  content-matching edit tools, never byte-literal sed against them.
- Session infrastructure (if resuming in the sdk-ship worktree): the
  isolation hook blocks Edit/Write outside the worktree and refuses
  compound shell commands touching other repos; the rigor git-guard
  pattern-matches git-history strings inside heredoc bodies — cross-repo
  docs go scratchpad-first, then a plain single-command `cat` copy; re-read
  every landed file. Also: this monorepo's noun gate walks ALL .md — the
  files in this brief's block were scanned clean before writing this.

## Open / next

1. **Operator: answer Gate zero** (ratify ruling 1 or stop the triad at
   Task 1). This is the single blocker for the build session.
2. **Operator: run the commit block below** (this brief + two learnings +
   two index rows — research docs are already on the remote at `191a13f`).
3. **Build session (later this week)**: execute plan Tasks 2–4 from the
   worktree — subagent-per-task per the plan header, or inline; each task
   ends with the full gate and an operator commit command. Estimated one
   session. Then re-run `/rigor:verify-claim` on the assembled kit (run
   `scripts/release.sh` and `sha256sum -c` yourself — the script's exit 0
   is a claim, not evidence).
4. Rulings queue for the operator now effectively FIVE: the four at the
   assessment's foot plus the ATLAS-scrub ruling recorded in the plan's
   deferred section — promote it into the assessment doc's queue next time
   that file is touched.

## Addendum, same day (2026-08-16, later session)

This brief's blockers were both answered the same day; the body above is left
as written. Recorded here, not edited in above:

1. **Gate zero: RATIFIED.** Ruling 1 adopted; plan Tasks 2–4 executed and
   gate-green (`verifier/KIT.md`, `scripts/release.sh` + `/dist/` ignore,
   `docs/integration.md` observability section). One correction folded in —
   `-trimpath` alone does not reproduce; `-buildvcs=false` and
   `-ldflags=-buildid=` are each load-bearing
   (`docs/learnings/2026-08-16-buildid-defeats-trimpath-reproducibility.md`).
2. **ATLAS scrub: RULED AND EXECUTED**, overturning this brief's "deferred,
   do NOT chase" locked decision. All three categories plus the
   private-monorepo references; zero tracked hits remain. Env vars renamed
   with no compatibility shim by explicit ruling
   (`docs/learnings/2026-08-16-atlas-scrub-executed.md`, which also records
   the accepted silent-not-verify cost and a LARGER internal-reference class
   left untouched and needing its own ruling).

The `sdk-ship` branch and worktree this brief describes no longer exist —
fast-forwarded into `main` at `89443a3` after this brief was written, so the
`re-verify:` lines naming `sdk-ship` fail; use `main`.

```bash
cd ~/dev/treasury-intent-controller
# handoff + learnings only: research docs already pushed (remote head 191a13f)
git add docs/handoff/2026-08-16-triad-build-setup.md docs/handoff/HANDOFF.md \
        docs/learnings/2026-08-16-atlas-codename-residual.md \
        docs/learnings/2026-08-16-plain-gobuild-irreproducible.md \
        docs/learnings/LEARNINGS.md
git commit -m "docs: handoff (triad build setup) + learnings (ATLAS residual, trimpath)"
git push
```
