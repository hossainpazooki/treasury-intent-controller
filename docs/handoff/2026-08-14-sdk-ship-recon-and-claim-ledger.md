# Handoff — sdk-ship worktree: honesty-check verdicts, article claim ledger, distribution assessment

2026-08-14 (UTC; the working session spanned 2026-08-13 local). Newest commits
this brief describes: intent-plane `42ce556` (== origin, gh-verified at write
time; PUBLIC repo), TIC remote head `22905d6` (gh-verified; this session is
worktree-isolated and cannot run git against the TIC checkout — local TIC tree
NOT inspected here). Pick-up measures drift from those SHAs.

This brief is the reconciliation the 2026-08-13 brief's Open item 1 asked for:
it is written BY the sdk-ship session whose plan that brief could only see via
memory. The two briefs are complementary; read that one for the declarant
ship, this one for the worktree's forward plan.

## Current state

- **built — sdk-ship worktree, entered and baseline-verified (this
  session)**: `~/dev/intent-plane/.claude/worktrees/sdk-ship`, branch
  `sdk-ship` at `42ce556`, created manually (`git worktree add`) then entered
  via the native worktree tool by path — the create-mode would have
  worktree'd the launch repo `~/dev`, not the nested SDK. Baseline green in
  the worktree: Go 12 packages ok (incl. declarant, verifier, contractcheck)
  + pyverifier 6/6 via the MAIN checkout's venv (`.venv` does not propagate
  into worktrees; the twin is stdlib-only so any interpreter works).
  `.worktreeinclude` (CLAUDE.md, docs/superpowers/**) at the SDK root;
  `.worktreeinclude` and `.claude/` added to `.git/info/exclude`.
  re-verify: `cd ~/dev/intent-plane && git worktree list && git -C .claude/worktrees/sdk-ship status --short`
  re-verify: `cd ~/dev/intent-plane/.claude/worktrees/sdk-ship && go test ./... -count=1`
- **built — honesty-check over the SDK README + docs at `42ce556`, then a
  refute pass over the fix-session's work**: boundary well-marked overall;
  the fix commit `ed1ec51` shipped real corrections (including one my check
  missed: the architecture hero diagram had said refusals leave "no record",
  contradicting §2.3). **Four findings remain OPEN at `42ce556`** — the
  concrete referent of the memory line the 08-13 brief could not resolve:
  1. `docs/assurance.md:119` — declarant claim-map row `built · test-grade`
     names no production blocker (violates the table's own legend; verifier
     row names R1/R2).
  2. "COMPASS" leaks: `docs/architecture.md:121` + `CONTRACT.md:66` — an
     internal codename, defined nowhere in the public SDK.
  3. `README.md:9-10` — "Every decision lands as exactly one durable record"
     sits ahead of the §2.3 step-1 residual (classification is
     synchronous-only for door refusals).
  4. `docs/architecture.md:188` — invariant 6 says "kill/restart proven"
     without naming where the proof lives.
  re-verify: `grep -n "COMPASS" ~/dev/intent-plane/docs/architecture.md ~/dev/intent-plane/CONTRACT.md`
  re-verify: `grep -n "kill/restart proven" ~/dev/intent-plane/docs/architecture.md`
- **built — article claim ledger** at
  `docs/research/2026-08-14-article-claim-ledger.md`: three buckets (12
  honest-present-tense / 8 needs-caveat / 13 correct-shaped-lies) from
  workflow `wf_e9a59f3f-9e8` — 10 agents, five disjoint slices, one skeptic
  per slice re-running the cited pinned tests at `42ce556`; 41/43 findings
  survived, 2 corrected (both corrections independently re-grepped by the
  controller). The raw workflow output lived in a Temp task file —
  the ledger doc is the durable copy.
  re-verify: read the doc; deep re-verify: run any evidence-named test, e.g.
  `cd ~/dev/intent-plane && go test -run TestTerminalHashCommitment ./core/internal/gate -count=1`
- **built — distribution avenues assessment** at
  `docs/research/2026-08-14-distribution-avenues-assessment.md`: critique of
  the Trajectory-derived six-avenue ranking — spine accepted; corrections:
  verifier-binary pitch needs the honest-scope packaging (and
  signed/checksummed releases), quickstart is undercosted and co-builds with
  the binary, OpenAPI unblocks calling not embedding, content channel splits
  into supply-side + demand-side artifacts. Three operator decisions listed
  at its foot. re-verify: read the doc.
- **built — three learnings entries** (2026-08-14: completed-ban-is-prose,
  intent-declare-exit0, sdk-public) + index rows; this brief + index row.
  Operator commits (block below).
- **verified — repo visibility**: intent-plane PUBLIC, TIC private (gh,
  2026-08-14T04:00:35Z). Every SDK commit is public marketing surface.
  re-verify: `gh api repos/hossainpazooki/intent-plane --jq .visibility`
- **planned — nothing beyond the docs above is built in the worktree**: the
  4 honesty fixes, ship-gate criteria list, SDK quickstart, OpenAPI surface,
  release binaries, demand-side artifact, and the article draft are all
  planned, none started.

## Locked decisions

- **Ship plan (operator, 2026-08-13)**: remaining SDK work rides the
  `sdk-ship` worktree and ships alongside the article — one ship gate, no
  interim pushes to SDK main. Reason: the article and the repo must describe
  the same commit. (Note the declarant push happened BEFORE this plan was
  set; the plan governs from `42ce556` forward.)
- **Claim-verdict corrections (skeptic-derived, controller-re-verified)**:
  "enterprise-grade key authority" is a correct-shaped lie, not a
  caveat-saveable claim (`key_authority: "test"` on every signature); the
  COMPLETED ban is prose + zero-grep, not a mechanized gate. Reasons in the
  claim ledger and the two learnings entries.
- **Worktree mechanics**: create nested-repo worktrees manually, enter by
  path; keep `.claude/` + `.worktreeinclude` in `info/exclude`; Python lanes
  in the worktree run on the main checkout's venv. Reason: launch-dir repo
  mismatch (recorded above) and venv non-propagation, both hit live.
- Inherited, not relitigated here: ADR-0011 per-tree ownership; §9.1
  rule_artifact_hash ruling; "Go-first with OpenAPI, twins on named demand"
  working posture (08-13 brief) — my assessment REFINES its scope (calling
  vs embedding), it does not reverse it.

## Reuse map

- `docs/research/2026-08-14-article-claim-ledger.md` — the article's
  sentence-level source of truth; also the honest-scope text for verifier
  release packaging (avenue 1).
- Workflow script (re-runnable after SDK changes to re-audit claims):
  `C:\Users\hossa\.claude\projects\C--Users-hossa-dev-intent-plane--claude-worktrees-sdk-ship\0295a4ce-41cc-4e54-a674-1818455dbf7c\workflows\scripts\article-claim-recon-wf_e9a59f3f-9e8.js`
  (resume id `wf_e9a59f3f-9e8`; unchanged agents replay from cache).
- `~/dev/briefs/2026-08-13-trajectory-sdk-distribution-research.md` — the
  full research behind the ranking; my assessment doc amends, not replaces.
- `docs/research/2026-08-12-plane-as-separating-surface.md` +
  `docs/handoff/2026-08-12-...` drift catalogue — article positioning
  inputs, alongside the claim ledger.
- `~/dev/briefs/2026-08-12-adr-0010-parked-drafts/` — still awaiting two-repo
  re-scope.

## Invariants

- All of the 08-13 brief's invariants stand (contract-first amendment order,
  consumer-tree import pins, frozen fixture bytes, operator-only git, gh for
  remote truth). Additions from this session:
- The sdk-ship session's git operations are hook-confined to its own
  worktree — TIC state must be read via `gh` or file reads, never local git
  from that session.
- The SDK repo is PUBLIC: no handoffs, learnings, drafts, or internal
  codenames land there (open finding 2 exists because "COMPASS" already
  leaked).
- The claim ledger is commit-anchored to `42ce556`: any SDK change that
  touches a claimed mechanism invalidates the affected rows — re-run the
  recon workflow (cached agents make this cheap) before the article cites
  them.
- TIC's forbidden-actor-noun gate walks ALL .md: the two research docs and
  this brief were written to avoid the banned nouns; keep future edits
  clean (`go test ./core/internal/contractcheck -count=1` in TIC gates it).

## Open / next

1. **First build act in the worktree: apply the 4 open honesty fixes** (all
   small; files and lines above), re-run the SDK contractcheck + full gate,
   and make it the first `sdk-ship` commit (operator runs it; block below is
   for TIC docs only — the worktree commit command comes with that change).
2. **Operator rulings queued** (distribution assessment doc, foot): adopt
   corrected ranking; split the content channel; ship-gate criteria for
   release binaries. Plus, still standing from earlier briefs: "option-3
   chassis tests" definition; ADR-0006 ratification; ADR-0010 re-scope.
3. **Article drafting** from the claim ledger; any live number ("10/10")
   gets re-verified in TIC the day it is cited; the demand-side artifact
   decision shapes whether one piece or two get drafted.
4. TIC §2.2 force_scores mirror fix (carried from the 08-13 brief, still
   open at `22905d6` as far as this session can see — verify at pickup).

```bash
cd ~/dev/treasury-intent-controller
# docs from the sdk-ship session: research pair, then ledger+brief (two concerns)
git add docs/research/2026-08-14-article-claim-ledger.md docs/research/2026-08-14-distribution-avenues-assessment.md
git commit -m "docs: article claim ledger (42ce556) + distribution avenues assessment"
git add docs/handoff/2026-08-14-sdk-ship-recon-and-claim-ledger.md docs/handoff/HANDOFF.md \
        docs/learnings/2026-08-14-completed-ban-is-prose-not-gate.md \
        docs/learnings/2026-08-14-intent-declare-exit0-includes-shadow.md \
        docs/learnings/2026-08-14-sdk-public-monorepo-private.md docs/learnings/LEARNINGS.md
git commit -m "docs: sdk-ship handoff + three learnings"
git push
```
