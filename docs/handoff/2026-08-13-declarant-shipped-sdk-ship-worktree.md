# Handoff — declarant SDK shipped; honesty-hardened SDK docs; sdk-ship worktree staged

2026-08-13. Newest commits this brief describes: intent-plane `42ce556`,
TIC `1054e0b` — both == origin at write time (gh-verified), both trees clean.
Pick-up measures drift from those two SHAs.

## Current state

- **built + pushed — declarant SDK (memo S4), born SDK-side per ADR-0011**:
  `declarant/` in intent-plane (`5a39498`) and consumed back here
  (`2811126`) — exact §2.2 wire marshal (golden request bytes), `DeriveKey`,
  TOTAL terminal classification with fail-closed `Unknown`, 500-edge
  per-intent feed consult (call-order pinned), cursor poll,
  `cmd/intent-declare` CLI; CONTRACT §2.7 is the normative text (both
  repos); §7.1 consumer-trees rule ({verifier, declarant}, plant-red
  proven); §5.4 claim 15. Skeptic pass: trees byte-identical across repos,
  golden bytes match the live server DTO, table==code==gate-vocabulary.
  re-verify: `cd ~/dev/intent-plane && go test ./declarant -count=1` (11 tests)
  re-verify: `diff -r ~/dev/intent-plane/declarant ~/dev/treasury-intent-controller/declarant` (empty)
- **built + pushed — quickstart ladder is 10 probes**: probe 6 = declarant
  consumption (PROCEED then same-derived-key ALREADY_RESERVED), probe 9 =
  feed count exactly-2-ACHIEVED, probe 10 = verifier-twin recompute.
  Ran live 10/10 on BOTH OS lanes 2026-08-13 pre-commit.
  re-verify: `powershell -File treasury\quickstart.ps1` → `RESULT: 10/10 probes passed`
- **built + pushed — honesty-check fixes over the SDK repo** (intent-plane
  `ed1ec51`): §2.2 force_scores note corrected (guard BUILT, residual
  recorded — was contradicting §2.5), architecture's refusal branch now says
  "durably recorded … no ACHIEVED record" (was "no record"),
  exactly-once→at-most-once wording (2 sites), "wrongly executes" absolute
  qualified once, README commitment-2 seat-boundary marker, consumer-tree
  re-run list + declarant lanes in README/assurance.
  re-verify: `cd ~/dev/intent-plane && grep -c "no env/build/auth guard" CONTRACT.md` → 0
- **built + pushed — README hero diagram** (intent-plane `302f558` +
  `42ce556`): vertical two-sided-sale flowchart, repo palette, packages
  amber, operator-approved text pass.
  re-verify: read `~/dev/intent-plane/README.md` mermaid block (flowchart TD)
- **built (uncommitted here) — two learnings entries + this brief**:
  `docs/learnings/2026-08-13-mid-ladder-probe-renumbering-sweep.md`,
  `docs/learnings/2026-08-13-gap-record-outlives-gap-closure.md`, index rows,
  this file + its index row. Operator commits (block below).
- **built (outside any repo) — Trajectory deep-research report**: verdict =
  orthogonal continual-learning platform, DX/distribution lessons + ranked
  distribution avenues for the SDK. Preserved at
  `~/dev/briefs/2026-08-13-trajectory-sdk-distribution-research.md`
  (the raw workflow output was in a Temp task file at cleanup risk).
  re-verify: read that file.
- **in-progress (ANOTHER session, verified only as listed) — sdk-ship
  worktree**: `~/dev/intent-plane/.claude/worktrees/sdk-ship`, branch
  `sdk-ship`, at `42ce556`, CLEAN at write time — staged, no divergent work
  yet. The ship plan ("remaining SDK work rides in the worktree, ships
  alongside the article") and "honesty-check 08-13: 4 findings open" are
  MEMORY-recorded by that session, not verified here — reconcile at pickup
  before building in the worktree.
  re-verify: `cd ~/dev/intent-plane && git worktree list && git -C .claude/worktrees/sdk-ship status --short`
- **built by concurrent sessions same day (context, not this session's
  claims)**: TIC chassis flow test (`d614cf7`, §5.4 claim 16), randomized
  loop driver fuzz (`1054e0b`), separating-surface positioning note
  (`21b646f`) + handoff addendum (`f723f89`), concept-chat prose
  (`5dcb210`/`5ed7148`).

## Locked decisions

- **ADR-0011 (Accepted)** — consumer packages are born SDK-side; declarant
  was the first package born under it. Reason: one-directional porting made
  the published repo structurally stale on exactly the consumer surfaces.
  `docs/adr/2026-08-12-ADR-0011-consumer-packages-live-in-the-sdk-repo.md`.
- **§2.7 classification is total with fail-closed `Unknown`; criterion names
  must not contain `:`** (mechanical PolicyDenied rule, documented after a
  skeptic residual). Reason: a new cause class amends §3.3 first; an
  unknown reason must never default to retry.
- **rule_artifact_hash stays an optional passthrough the verifier never
  demands** (§9.1 ruling, fixture-pinned). Reason: probe 10's predecessor
  refuted a correct feed by demanding it.
- **TIC's mirrored §2.2 stale force_scores note stays OPEN by scope ruling**
  (operator: SDK-focus session). Reason recorded in
  `docs/learnings/2026-08-13-gap-record-outlives-gap-closure.md`; the fix
  text to mirror is intent-plane `ed1ec51`.
- **Go-first with OpenAPI as the non-Go path; no Python/TS declarant twins
  until a named consumer asks** (research recommendation adopted as working
  posture, not an ADR). Reason: the wire is already contractually frozen;
  twins cost maintenance the OpenAPI surface doesn't.

## Reuse map

- `declarant/` (both repos, byte-identical): `Classify`/`classifyFeedRecord`
  for any terminal handling; `DeriveKey`; `IntentID`; the httptest gateStub
  pattern in `declarant_test.go` for wire tests.
- `verifier/` + `core/contract/feed/` fixtures: the conformance pattern
  (frozen bytes + tampered standing mutant + generator test) for any new
  frozen surface.
- `core/internal/contractcheck/boundary_test.go` `consumerTrees` map — add a
  future consumer tree there + §7.1, nothing else.
- `~/dev/briefs/2026-08-13-trajectory-sdk-distribution-research.md` — the
  distribution avenue ranking for sdk-ship scoping.
- `~/dev/briefs/2026-08-12-adr-0010-parked-drafts/` — ADR-0010 +
  memo-addendum + roadmap-row drafts (rescued from Temp; still need
  two-repo re-scope).
- Plant-red workflow: copy `go.mod` + `*.go` trees to scratchpad, inject,
  run the pin — used four times this session, all four fired.

## Invariants

- Amend CONTRACT.md FIRST, then pinned tables/code — both repos.
- Consumer trees import NOTHING from the module outside their own tree,
  prod AND test (`TestImportBoundary`); violating it silently breaks the
  "no trust in the gate" / "visible wire client" claims.
- `core/contract/feed/*` and `declarant/testdata/*` are frozen bytes;
  `.gitattributes -text` protects the feed fixtures — never let eol
  conversion touch them, never regenerate without the write-mode env vars
  and both-lane re-green in the same commit.
- The FULL TIC gate now includes `RESULT: 10/10` + declarant tests; a wire
  change is green only when Go and Python byte-compare the same fixture
  bytes AND the golden request bytes still match.
- Git discipline: operator writes history; remote truth via `gh`. This
  session's commit blocks excluded concurrent sessions' in-flight files —
  keep doing that; check `git status` against your OWN footprint before
  emitting blocks.
- The sdk-ship worktree belongs to another session's ship plan: do not
  build in it, rebase it, or fold it into main without reconciling that
  session's memory-recorded plan first.

## Open / next

1. **First**: reconcile the sdk-ship plan — verify what "honesty-check
   08-13: 4 findings open" refers to (memory-recorded by the concurrent
   session; not found in any repo doc at write time), then scope the
   worktree's work. The ranked distribution avenues in the Trajectory brief
   are the natural candidates: verifier static-binary releases → SDK-repo
   5-minute quickstart → OpenAPI wire surface → (named-consumer) framework
   adapter.
2. Operator: commit this brief + the two learnings entries (block below).
3. TIC §2.2 force_scores note mirror fix (one paragraph, text in
   intent-plane `ed1ec51`) — next TIC-scope session.
4. ADR-0010 re-scope from the parked drafts; ADR-0006 ratification
   criteria; Python declarant twin only on named demand (ROADMAP).

```bash
cd ~/dev/treasury-intent-controller
git add docs/handoff/2026-08-13-declarant-shipped-sdk-ship-worktree.md docs/handoff/HANDOFF.md \
        docs/learnings/2026-08-13-mid-ladder-probe-renumbering-sweep.md \
        docs/learnings/2026-08-13-gap-record-outlives-gap-closure.md docs/learnings/LEARNINGS.md
git commit -m "docs: handoff (declarant shipped, sdk-ship staged) + two learnings"
git push
```
