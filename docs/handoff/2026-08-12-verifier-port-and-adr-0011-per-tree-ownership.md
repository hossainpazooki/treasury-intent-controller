# Handoff — verifier ported to the SDK; ADR-0011 per-tree ownership

2026-08-12. Newest commits this brief describes: TIC `d0ad490`, intent-plane
`5cffb66` (both == origin at write time, gh-verified). Everything below is
UNCOMMITTED working-tree state on top of them in BOTH repos; the commit
blocks are at the bottom.

## The ruling (operator, 2026-08-12)

The two-sided sale exposed a layout defect: the published SDK shipped the
plane operator's reference implementation while BOTH consumer artifacts were
absent from it (verifier lived only here; declarant SDK unbuilt). Ruling —
**ADR-0011, Accepted** (`docs/adr/2026-08-12-...md`): consumer-facing
packages are born and evolve in `intent-plane`; plane internals (gate,
scorer, plane/) keep the monorepo-first flow; port direction runs per tree.
Chosen over port-everything-status-quo, collapse-to-one-repo, and
standalone-verifier-repo (independence optics deferred until a real consumer
asks).

## Current state

- **built — verifier cluster ported to `~/dev/intent-plane`** (first act
  under ADR-0011): `verifier/` tree (Go pkg + `cmd/intent-verify` +
  `pyverifier/`), refusal-hash gate change (`gate.go` emitFinal/
  transitionFinal + `store.go` comment), `feed_fixture_test.go` +
  `terminal_hash_test.go`, `core/contract/feed/` fixtures (all four files
  byte-identical to this repo's, cmp-verified), `.gitattributes` feed rule,
  boundary_test verifier pin. CONTRACT.md = this repo's current text with
  the 7 SDK-adaptation hunks re-applied + 2 quickstart references repointed
  to this monorepo (diff between the two contracts is exactly those hunks,
  verified).
  re-verify: `cd ~/dev/intent-plane && go build ./... && go vet ./... && go test ./... -count=1` (11 ok incl. verifier)
  re-verify: `cd ~/dev/intent-plane && ./core/scorer/.venv/Scripts/python -m pytest verifier/pyverifier -q` (6)
  re-verify: `cd ~/dev/intent-plane && go run ./verifier/cmd/intent-verify core/contract/feed/events-good.jsonl` (VERIFIED, exit 0; tampered fixture REFUTED, exit 1)
- **built — SDK docs repositioned to the shipped state**: README (verifier
  in what-ships, verifier test lanes, layout tree, per-tree ownership
  paragraph, 9/9 probe count, status line), docs/assurance.md (stage table
  row, "don't have to start from prose" paragraph, two new claim rows
  incl. the step-1 refusal residual, gap list minus the closed refusal-hash
  gap, re-run section, 9/9).
- **built — this repo's records**: ADR-0011 written; ROADMAP verifier row
  now says ported 2026-08-12; ROADMAP header ADR list refreshed
  (0009/0010 reserved, not yet written).
- **verified — full gates fresh-green both repos at this tree** (2026-08-12):
  intent-plane Go 11 ok + contractcheck 6/6 + scorer 42p/5s + pyverifier 6;
  TIC contractcheck 6/6 after doc edits; earlier same session the TIC full
  gate ran green (Go 11 ok, scorer 42p/5s, pyverifier 6, quickstart 9/9).
  WSL `-race` NOT run on the ported intent-plane tree — run it before
  trusting concurrency-touching changes (the port's gate.go change is the
  same code `-race`-verified in TIC on 2026-08-08, so risk is low, but the
  lane wasn't exercised here).

## Locked decisions

- **ADR-0011 (Accepted 2026-08-12): consumer packages live SDK-side; port
  direction per tree.** Reason: a one-directional port rule makes the
  published repo structurally stale on exactly the surfaces consumers touch.
- ADR-0009 (key authority) and ADR-0010 (approval artifact) numbers remain
  reserved; 0011 deliberately lands out of numeric order.
- Verifier §7.1 rule holds identically in both repos: imports NOTHING from
  the module outside its own tree, prod or test.
- `core/contract/feed/*` is `-text` in BOTH repos' `.gitattributes`; the
  fixtures are frozen bytes.

## Open / next

1. Operator reviews + runs the commit blocks below, pushes both repos.
2. **Declarant SDK (memo S4)** — first package BORN SDK-side under
   ADR-0011; TIC quickstart gains its consumption probe.
3. **ADR-0010 landing**: re-scope the parked drafts (old scratchpad
   `Temp/claude/C--Users-hossa-dev/c4006536-*/scratchpad/`
   `memo-addendum-2-draft.md` + `roadmap-row-draft.md`, which cite the
   pre-renumber label "ADR-0008") to the two-repo layout; consumer-facing
   text lands SDK-side per ADR-0011. **Copy the drafts out of Temp first —
   they are at cleanup risk.**
4. "option-3 chassis tests" (named in the 2026-08-08 session's next list) is
   defined nowhere in-repo — `treasury/authoring` has no test files and is
   the likely target; needs the operator's definition.
5. ADR-0006 ratification criteria; remaining ROADMAP decision rows.

## Commit blocks (operator runs; CLAUDE.md + docs/superpowers stay local-only)

```bash
cd ~/dev/intent-plane
git add CONTRACT.md core/internal core/contract/feed verifier .gitattributes
git commit -m "feat: verifier cluster ported from the testing monorepo (ADR-0011) - refusal-hash commitment, Go+Python twins, feed fixtures"
git add README.md docs/assurance.md
git commit -m "docs: verifier ships here - readme + assurance repositioned to per-tree ownership"
git push

cd ~/dev/treasury-intent-controller
git add docs/adr/2026-08-12-ADR-0011-consumer-packages-live-in-the-sdk-repo.md docs/ROADMAP.md docs/handoff
git commit -m "docs: ADR-0011 per-tree ownership; verifier-port roadmap row; handoff"
git push
```
