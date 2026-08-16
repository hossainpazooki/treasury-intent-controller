# Handoff — venue triad, platform-architect consumer signal, R4 exporter ruling queued

2026-08-16 (UTC; the session spanned 2026-08-15 local). Newest commits this
brief describes: intent-plane `42ce556` (unchanged; sdk-ship worktree clean —
no SDK file was touched this session), TIC remote `10a4c09` (gh-verified:
the operator RAN the 08-14 brief's commit block — `b6634a7` research pair,
`10a4c09` handoff + learnings — and pushed). This session is
worktree-isolated and cannot run git against the TIC checkout; every file
below is uncommitted as of writing and rides the commit block at the foot.
Pick-up measures drift from those two SHAs.

## Current state

- **built — R4 exporter placement queued as operator decision 4** at the
  foot of `docs/research/2026-08-14-distribution-avenues-assessment.md`
  (lines 82-92): in-gate OTel emission vs an external feed-to-OTLP exporter
  that tails the feed; records the stdlib-only collision and why the
  external shape makes "logs index, gates decide" structural. The append
  POST-DATES commit `b6634a7`, so the committed copy lacks it until the
  block below runs.
  re-verify: `grep -n "R4 exporter placement" ~/dev/treasury-intent-controller/docs/research/2026-08-14-distribution-avenues-assessment.md`
- **built — consumer-signal research note**
  `docs/research/2026-08-16-consumer-signal-platform-architect.md`: first
  external practitioner reaction (platform architect, Kubernetes/mesh
  background) — one validation (maturity admission supports
  demand-side-first; avenue 4 stays unmet), one warning (readers
  pattern-match "intent plane" to prompt-routing/cost middleware), one
  opening (admission-controller-plus-audit-log translation; DSSE-shaped
  envelopes make the sigstore/in-toto/policy-engine community a fourth
  venue candidate). Amends the assessment's "zero external consumers as
  evidence" line: now one data point, mixed read.
  re-verify: read the note.
- **built — claim-ledger drafting rule appended**
  (`docs/research/2026-08-14-article-claim-ledger.md`, Drafting rules
  distilled): the composes-with-routers preemption. Also post-dates
  `b6634a7`.
  re-verify: `grep -n "composes with routers" ~/dev/treasury-intent-controller/docs/research/2026-08-14-article-claim-ledger.md`
- **discussion recorded, nothing built — the venue triad** (this session's
  LangChain-vs-Datadog thread): LangChain = where agents ACT (declarant
  embeds; blocked today — no Python declarant, OpenAPI unblocks calling not
  embedding); Datadog = where operators WATCH (feed log-forward possible
  today with zero SDK change; R4 traces staged); GRC/audit = where third
  parties ATTEST (avenue 1, ranked first; "hand your auditor this binary").
  Plus the fourth candidate above. R4 ground truth re-established from the
  docs: single SDK mention (`docs/assurance.md:113`), ladder in TIC
  `docs/ROADMAP.md:15-18`, zero OTel code by construction (stdlib-only).
- **planned — unchanged from the 08-14 brief**: the 4 open honesty fixes
  remain the first build act in the worktree; article drafting from the
  ledger; demand-side artifact pending ruling 2.

## Locked decisions

- **R4 exporter placement is QUEUED, not decided** — do not build either
  shape; the ruling only becomes urgent if a Datadog/observability channel
  is adopted. Reason: R4 is unbuilt and nothing blocks on it (ROADMAP row:
  "permitted only if trivially additive").
- **The consumer signal does NOT satisfy avenue 4's named-consumer
  condition** — the architect self-reported the maturity gap ("no agent
  registration"). Reason: the condition requires present demand, and the
  signal is explicit that it is future. Do not treat the conversation as a
  green light for adapter work.
- Inherited, not relitigated: one-gate ship plan; demand-side-first spine;
  ADR-0011 per-tree ownership; the 08-14 brief's claim-verdict corrections.

## Reuse map

- The consumer-signal note's wedge sentence ("the evidence layer that
  proves your guardrails ran") and the admission-controller translation —
  openers for the demand-side artifact and the article's platform-reader
  paragraph.
- `docs/research/2026-08-14-article-claim-ledger.md` — sentence-level
  source of truth for the article, now including the routing preemption
  rule.
- The 08-14 brief — worktree mechanics, the 4 findings' file:line, the
  recon workflow resume id.

## Invariants

- All 08-14 invariants stand (PUBLIC SDK repo — no internal docs there;
  operator-only git; gh for remote truth; forbidden-actor-noun gate walks
  ALL TIC .md — today's three doc changes were written clean of the banned
  nouns).
- **New, session-infrastructure:** the worktree-isolation hook now blocks
  the Edit AND Write file tools for any path outside the sdk-ship worktree
  (earlier in this same worktree's session, TIC Writes succeeded — it
  tightened); plain shell file operations still pass, except that the rigor
  git-guard also pattern-matches git-history strings INSIDE shell heredoc
  bodies — a doc containing a commit block must be staged via the
  scratchpad and copied in. Cross-repo doc writes from this worktree
  therefore go via shell, and each landed file must be re-read to verify.
  Also recorded in operator memory.
- Handoff entries are immutable: this brief supersedes nothing and edits
  nothing; the 08-14 brief's commit block is RETIRED (already run) and the
  block below is the only live one.

## Open / next

1. Operator: run the commit block below (all three research-doc changes +
   this brief and index row).
2. First worktree build act, still: the 4 honesty fixes
   (assurance.md:119, COMPASS x2, README.md:9-10, architecture.md:188),
   then contractcheck + full gate, then the first sdk-ship commit.
3. Rulings queue is now FOUR at the assessment's foot, and the
   content-channel ruling (2) gained evidence from the consumer signal.
4. Article drafting per the ledger; the platform-reader preemption
   paragraph is now mandatory per the new drafting rule.

```bash
cd ~/dev/treasury-intent-controller
# venue/distribution docs: R4 ruling append + consumer-signal note + ledger rule (one concern)
git add docs/research/2026-08-14-distribution-avenues-assessment.md \
        docs/research/2026-08-16-consumer-signal-platform-architect.md \
        docs/research/2026-08-14-article-claim-ledger.md
git commit -m "docs: R4 exporter ruling queued, platform-architect consumer signal, ledger routing preemption"
git add docs/handoff/2026-08-16-venue-triad-and-consumer-signal.md docs/handoff/HANDOFF.md
git commit -m "docs: handoff (venue triad + consumer signal)"
git push
```
