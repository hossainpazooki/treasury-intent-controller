# Handoff — the MCP gate build is COMMITTED AND PUSHED; README rewritten visual-first

2026-08-21 (UTC; the work is the 2026-08-20 session, the commits landed
~03:00Z). Newest commits this brief describes: intent-plane **`2fab226`**
("docs: README rewrite (visual-first, distribution)…") and the monorepo
**`fe53003`** ("docs: handoff + 3 learnings…"). Both trees clean; both
remotes verified via `gh` to sit at exactly those SHAs (0 ahead / 0 behind).
Pick-up measures drift from these two.

This brief SUPERSEDES the anchors of
`2026-08-20-mcp-gate-and-three-fail-opens.md`, which was written while
everything was still uncommitted and says so in its header. That brief
remains the narrative of record for WHAT was built and WHY (three
fail-opens, the withdrawn claim, the fix design); this one records that it
landed, what landed after it, and what the next session starts from. Read
that one for the story, this one for the anchors.

## Current state

- **built — everything in the 08-20 brief is now in history.** SDK:
  `2484a22` (CONTRACT: MCP gate, redirect rules, fork-vs-collision),
  `5759dec` (fix: never follow redirects, both seams), `013dd0f` (feat: MCP
  gate), `5425719` + `2fab226` (docs, incl. two README passes). Monorepo:
  `49fa6b1` (CONTRACT port), `c82f677` (consume back + redirect fix),
  `c749f7a` (live probes 9–10, ladder 14/14), `fe53003` (handoff + 3
  learnings).
  re-verify: `git -C ~/dev/intent-plane log --oneline d9041f5..2fab226` — five commits; `git -C ~/dev/treasury-intent-controller log --oneline b6a99a8..fe53003` — four.
- **built — remotes match.** Verified read-only via the GitHub API, never a
  local fetch (repo rule).
  re-verify: `for r in intent-plane treasury-intent-controller; do echo "$r $(gh api repos/hossainpazooki/$r/commits/main --jq .sha | cut -c1-7) $(git -C ~/dev/$r rev-parse --short HEAD)"; done` — each line's two SHAs equal.
- **built — both gates green at the committed trees** (re-run after the
  last README edit): Go build/vet/test clean in both repos; Python lanes
  124 passed / 5 skipped in both (pydeclarant 76 · pyverifier 6 · scorer
  42+5 skipped, wheel lane Linux/CI-only by design).
  re-verify: `cd ~/dev/intent-plane && go test ./... -count=1 && core/scorer/.venv/Scripts/python -m pytest declarant/pydeclarant verifier/pyverifier core/scorer -q`
- **built — the live ladder, both OS lanes, run first-party by the
  controller:** `RESULT: 14/14 probes passed`, exactly 6 ACHIEVED, verifier
  `VERIFIED intents=18 verified=18 refuted=0`, Go and Python reports
  identical. (WSL invocation needs `\$PATH` escaped and the tree reached
  via `/mnt/c/...` — the 08-20 brief's addendum records it.)
  re-verify: `cd ~/dev/treasury-intent-controller && powershell -File treasury\quickstart.ps1`
- **built — README rewritten, visual-first** (`2fab226`): the two-sides
  diagram is the first thing after the one-line pitch (line 12, before any
  heading) with a caption that states the value in five sentences; then
  "Gate the call, three ways" (LangChain · own MCP server · MCP server you
  don't own); a "What it refuses" section with the 08-20 hardening as
  product properties; and a **Distribution** section at position 4 of 9
  with a second diagram (what ships → route → who takes it, each edge
  labelled with its integrity mechanism) plus the honest negatives.
  re-verify: `cd ~/dev/intent-plane && grep -n '^```mermaid' README.md | head -1` prints `12:`; `grep -n "^## " README.md` lists Distribution fourth; `go test ./core/internal/contractcheck -count=1` ok (the pins scan README).
- **built — learnings ledger: six 08-20 entries.** Three landed in
  `fe53003` (fork-is-fail-open, key-blind double, redirect-following
  clients); three more written at this hand-off (ordinals decay again,
  untracked CLAUDE.md rots unguarded, a report cannot show an instruction
  landed), each with a captured basis and a read-only re-verify line that
  was executed before being written down.
  re-verify: `ls ~/dev/treasury-intent-controller/docs/learnings/2026-08-20-*.md | wc -l` prints 6.
- **planned — NOT started:** Datadog log-forward and the R4 OTel exporter
  (deferred by ruling 7); the demand-side artifact and the supply-side
  article (ruling 2's split — still the open fork); the four coverage-gap
  tests below.

## Locked decisions

- **README is visual-first.** The two-sides diagram precedes every heading;
  its caption, not a later section, carries the value statement. Reason:
  the operator's instruction ("bring the main visual up top so the value is
  made clear"), after two text-first versions buried it.
- **"Distribution" in the public README means consumer ROUTES** (go get ·
  vendor the tree · build the kit · read the contract), never the internal
  go-to-market avenues of the 08-14 assessment. Reason: the latter is
  private strategy; publishing it would leak the monorepo's planning into
  the product surface. The operator's phrase "distribution avenues" was
  read this way and the reading was stated aloud at the time; nobody
  overruled it.
- **The README states its negatives in the same breath as its positives:**
  no published kit artifact (`dist/` is gitignored, no release automation),
  no PyPI package, checksums-not-signatures, the live ladder is private.
  Reason: this repo's honesty rule — every negative was verified before
  being written (`git check-ignore dist/`, no `pyproject`, no workflows).
- **Prose NAMES probes; only tables and scripts number them.** Reason: the
  ordinal-decay learning recurred (second time) — see the new entry.
- Inherited from the 08-20 brief, unchanged: fork ⇒ fail-open / collision
  ⇒ fail-closed (normative in §2.7, withdrawn claim quoted in place);
  redirects never followed on either seam; `strict_args=True` default;
  absent `required` refused in both tiers; Tier 1 `extra="forbid"`; the
  ambiguous-union residual recorded as fail-open; claim tables
  number-diverged by design; fresh per-invocation episode seed;
  operator-only git; handoff and learnings entries immutable.

## Reuse map

- `docs/handoff/2026-08-20-mcp-gate-and-three-fail-opens.md` — the
  narrative, fix design, and the commit blocks as emitted; its addendum
  has the WSL invocation gotcha.
- `declarant/pydeclarant/_gate_double.py` `key_aware=True` — the ONLY way
  an in-process test can observe a duplicate authorization. Default is
  off; anything asserting at-most-once must opt in.
- `README.md`'s Distribution diagram — house-style mermaid (quoted labels,
  labelled edges, `classDef`/`class`/`style`); the "you build it" node uses
  a distinct pale-dashed class so a non-download route reads as one. Copy
  the pattern rather than the look if you add a diagram.
- The structural mermaid lint used at hand-off (a 30-line Python check
  that every line of a block matches the constructs the already-rendering
  diagrams use, and every referenced id is declared) — there is no local
  renderer on this host; the lint is the available substitute.
- The controller probes worth re-creating when touching canonicalization
  or transport: an independent key recompute from the CONTRACT formula
  (never via the adapter's helper), and a two-origin redirect probe.

## Invariants

- **CONTRACT.md amended FIRST** — every commit sequence above is ordered
  spec → code → docs in both repos for this reason.
- The declarant trees and `core/internal/scoring/scorer.go` are
  byte-identical across the two repos at these SHAs; a change on either
  side ports whole-file.
- intent-plane is PUBLIC: zero banned strings (7th pin), no handoffs,
  learnings, plans, or codenames — and `CLAUDE.md`/`docs/superpowers/` stay
  excluded. Consequence recorded in a new learning: excluded files are
  invisible to every git-backed check, so their counts are unverified on
  every pickup until someone reads them.
- The ladder's 14/14 IS the gate; a new declaring probe bumps the ACHIEVED
  count (now 6) and lands before the feed-count and recompute probes.
- Remote truth comes from `gh`, never a local fetch.

## Open / next

1. **Three operator rulings, none blocking** (carried from the 08-20
   brief): (a) the ambiguous-union residual on the proxy path — refuse
   under `strict_args` or leave recorded; (b) the key format does not
   escape `:` — spec-level, shared with the Go SDK, fails CLOSED, a
   breaking key-format change; (c) the synchronous 200 carries no intent
   identity, so claim 17's "for that specific call" is stronger than what
   the code can check — a wire-format change.
2. **Four coverage-gap tests, each small, from the 21-mutant pass:**
   nothing drives the client into its transport-error path (close the gate
   socket, assert zero executions); a loose `ACHIEVED` substring match on
   the body authorizes; an undecodable 200 body authorizes; the `tools=`
   filter is untested for a MISMATCHED name (typo, rename, empty set — all
   pass ungated with no diagnostic). Good first task for a session that
   wants a clean win.
3. **The standing fork:** demand-side artifact vs supply-side article. The
   article's spine is now four "verification found something real"
   episodes — the 08-08 verifier, the 08-18 seed reuse, and the 08-20 key
   fork and redirect — one of which the author had argued was safe in
   writing. That is the honest-sales line the research memo wanted.
4. **Recorded weakness, unguarded:** both repos' `CLAUDE.md` carry counts
   that no gate reads. Either strip the numbers or add a non-git check.
