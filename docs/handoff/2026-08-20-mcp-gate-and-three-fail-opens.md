# Handoff — the MCP gate shipped, and three fail-opens found in code that was already live

2026-08-20 (UTC). Anchors: intent-plane **`d9041f5`** and this monorepo
**`b6a99a8`** — both HEADs are UNCHANGED by this session; every artifact
below is uncommitted working tree awaiting the operator's commit blocks at
the foot of this brief. Pick-up measures drift from those two SHAs.

One session, one workstream (operator ruling 7: build the MCP gate, full
scope including the proxy, fastmcp as the second sanctioned dependency
exception; Datadog and the R4 OTel exporter DEFERRED and kept supply-side).
The gate was built and live-proven. It also turned into the session that
found three fail-opens, two of them in code that had already shipped and
been live-proven on 2026-08-18.

## Current state

- **built — the MCP gate** (`declarant/pydeclarant/mcp_adapter.py`):
  `IntentGateMiddleware` gates tool calls on a fastmcp server you own;
  `gated_proxy` fronts a server you do NOT own so it is gated without any
  change to it — the backend never sees a refused call. Shared refusal core
  extracted to `gating.py`; shared test double to `_gate_double.py`.
  re-verify: `cd ~/dev/intent-plane && core/scorer/.venv/Scripts/python -m pytest declarant/pydeclarant -q` — 76 passed.
- **built — live legs, and they are green.** The quickstart is a **14-probe**
  ladder: probe 9 = MCP middleware live (three legs, including a SECOND
  independent middleware instance refusing the same retry — the
  multi-replica claim proven live), probe 10 = gated proxy live (the INNER
  server's counter is the observable, plus a strict-args refusal leg).
  Controller re-ran the ladder first-party: `RESULT: 14/14 probes passed`,
  `exactly 6 ACHIEVED records ... among 130 events`, and
  `VERIFIED intents=18 verified=18 refuted=0 unverifiable=0` with Go and
  Python reports identical. The intent count is the sharper evidence: 13 + 3
  + 2 = 18, and landing on 18 rather than 19 independently confirms the
  strict refusal declares NOTHING.
  re-verify: `cd ~/dev/treasury-intent-controller && powershell -File treasury\quickstart.ps1`
- **built — fail-open #1 CLOSED: the idempotency key forked.** One logical
  action derived two keys on four argument shapes (nested-object defaults, a
  top-level `default_factory` — pydantic emits no `"default"` for it — JSON
  numeric form `500` vs `500.0`, and a partially spelled nested object). The
  gate dedups BY KEY, so the second key had nothing to collide with and the
  tool executed twice. Fixed with a two-tier recipe: Tier 1 (a tool exposing
  a callable) validates against a model rebuilt from the tool's signature;
  Tier 2 (`ProxyTool` has NO `fn`, so the headline proxy path cannot use
  Tier 1) walks the JSON Schema recursively and refuses what it cannot key.
  re-verify: read `docs/learnings/2026-08-20-key-fork-is-fail-open-not-fail-closed.md` and run its re-verify line.
- **built — fail-open #2 CLOSED: `tools=` accepted a bare string.**
  `tools="wire_transfer"` parses as a set of CHARACTERS, so the named tool
  did not match and passed UNGATED — measured at zero gate declarations and
  one execution, with no error and no log line. Now refused at construction
  with a message naming the correct form.
  re-verify: `cd ~/dev/intent-plane && core/scorer/.venv/Scripts/python -c "import sys;sys.path.insert(0,'declarant/pydeclarant');from client import Client;from mcp_adapter import IntentGateMiddleware;IntentGateMiddleware(Client('http://x'),intent_spec_hash='a',scope='s',run_id='r',tools='name')"` — must raise TypeError.
- **built — fail-open #3 CLOSED: the clients followed HTTP redirects.** A
  301/302/303 answer downgraded POST to GET and DROPPED the declaration
  body; another origin's `ACHIEVED`-shaped 200 was then read as
  authorization, so the action fired having never been declared. The SAME
  hole existed on the gate's own outbound `/ml/evaluate` call, where ALL
  FIVE codes produced a criterion `PASS` from a responder that (for
  301/302/303) was never told which criterion it was answering about. Fixed
  in three places across both languages; both repos.
  re-verify: `cd ~/dev/intent-plane && go test ./core/internal/scoring -run TestHTTPScorerNeverFollowsRedirects -count=1` and `go test ./declarant -count=1`.
- **built — docs re-synced, numbers re-measured.** Every empirical number in
  the docs was re-measured rather than carried forward, including building a
  throwaway bare venv to actually observe the "skips visibly where the
  optional dependency is absent" claim (`17 passed, 2 skipped`, both skip
  reasons printed).
- **planned — NOT started:** Datadog log-forward and the R4 OTel exporter
  (deferred by ruling 7); the demand-side artifact and the supply-side
  article (ruling 2's split, still the open fork).

## Locked decisions

- **Fork ⇒ FAIL-OPEN; collision ⇒ fail-closed.** Now normative in §2.7. The
  earlier inverted claim is WITHDRAWN IN PLACE — quoted verbatim in the spec
  so a reader sees what was wrong, not a silently different document.
- **Redirects are NEVER followed**, on the declarant seam (§2.7) and the
  scorer seam (§2.4). A 3xx is treated like any other non-200: consult the
  feed, and an unreachable feed decides nothing.
- **`strict_args=True` by default** on the MCP proxy path: an omitted
  non-required property whose schema declares no default is refused before
  declaring. `strict_args=False` is the explicit opt-out and accepts, in
  writing, that such a call is keyed as spelled.
- **An absent `required` property is refused pre-declaration in BOTH tiers**
  and is NOT subject to `strict_args` — otherwise the gate authorizes a call
  the framework then rejects, leaving a durable `ACHIEVED` for a consequence
  that never fired.
- **Tier 1 forbids undeclared arguments** (`extra="forbid"`); Tier 2 keys
  them as spelled, because a fronted schema may legitimately accept extras.
  Cross-tier byte equality therefore holds for DECLARED arguments.
- **RECORDED RESIDUAL, stated as fail-OPEN, not excused:** on the proxy path
  an ambiguous union (`anyOf`/`oneOf` with more than one non-null branch) is
  left as spelled, so two equivalent spellings would fork. Tier 1 is
  unaffected. Closing it needs a ruling.
- Claim tables stay NUMBER-DIVERGED by design (SDK claim 17 = the MCP gate;
  the monorepo records adapters in §2.7 prose, not a claim row).
- Inherited, unchanged: fresh per-invocation episode seed; per-tree
  ownership; operator-only git; handoff and learnings entries immutable.

## Reuse map

- `declarant/pydeclarant/_gate_double.py` — the shared scripted gate double,
  now with an OPT-IN `key_aware` mode (default off). **Use `key_aware=True`
  for anything asserting at-most-once**; the default mode structurally
  cannot see a duplicate.
- `treasury/probes/mcp_gate_live.py` / `mcp_proxy_live.py` — the live-probe
  pattern, including the multi-replica leg and an inner-counter observable.
- The two controller probes kept in scratchpad, worth re-creating if you
  touch canonicalization or transport: an independent key recompute from the
  CONTRACT formula (never via the adapter's helper), and a two-origin
  redirect probe.
- `core/internal/scoring/scorer_test.go::TestHTTPScorerNeverFollowsRedirects`
  — the shape for pinning an ABSENT-option defect.

## Invariants

- **CONTRACT.md amended FIRST** — held for every change here, including the
  retraction of my own wrong claim.
- The declarant trees and `core/internal/scoring/scorer.go` stay byte-identical
  across the two repos (verified at hand-off; re-verify at commit time,
  because several agents wrote these trees concurrently today).
- intent-plane is PUBLIC: zero banned strings, verified 0/7.
- The ladder's 14/14 IS the gate; a new declaring probe must bump the
  ACHIEVED count and land BEFORE the feed-count and recompute probes.
- Prose narrating a past incident NAMES the probe; only ladder tables and
  scripts carry ordinals. (Three sites that said "probe 9" meaning the
  recompute probe as numbered on 2026-08-08 were corrected this session —
  the second recurrence of that decay.)

## Open / next

1. **Operator: run the two commit blocks below.**
2. **Three items need a ruling, none blocking:**
   (a) the ambiguous-union residual — refuse under `strict_args`, or leave
   recorded; (b) the key format does not escape `:`, so
   `scope="tenant-a:prod"` + `run="run-9"` collides with `scope="tenant-a"` +
   `run="prod:run-9"` — spec-level, shared with the Go SDK, fails CLOSED
   (bricks), and changing it is a breaking key-format change; (c) the
   synchronous 200 carries NO intent identity, so a gate answering with
   another call's key still authorizes — the adapter structurally cannot
   verify "for THAT call", which makes claim 17's wording stronger than what
   the code can check. A wire-format change.
3. **Unexercised surfaces:** the mutation pass left four survivors that are
   coverage gaps rather than behaviour bugs — nothing drives the client into
   its transport-error path, a loose `ACHIEVED` substring match authorizes,
   and an undecodable 200 body authorizes. Worth closing next session; each
   is a small test.
4. **The standing fork is unchanged:** demand-side artifact vs supply-side
   article. The article's spine got materially stronger today — it now has
   three "the verification found something real" episodes, one of which the
   author (me) had argued was safe.

## Addendum — closed after the docs sweep reported (2026-08-20)

- **Claim 17 now carries its Live pointer.** It was deliberately withheld
  while the probes did not exist; they exist and passed, so the row now
  names them, including the intent-count evidence for the strict refusal.
- **Cross-repo claim citations disambiguated.** The ported modules cited
  "CONTRACT.md section 5.4 claim 16/17", but the two repos' claim tables are
  number-diverged BY DESIGN: in the testing monorepo, claim 16 is the
  maker-checker chassis and there is no claim 17, so a reader following that
  citation there landed on the wrong claim or on nothing. All four ported
  module docstrings now say the number belongs to the published SDK's table
  and point at section 2.7, which resolves in both repos. This did not sync
  the numbers — that remains forbidden.
- **The shell lane is now first-party evidence.** The docs sweep could not
  run `quickstart.sh` and said so rather than restating someone else's
  number. The controller ran it under WSL: `RESULT: 14/14 probes passed`,
  `exactly 6 ACHIEVED`, `VERIFIED intents=18 verified=18 refuted=0`. So
  "both OS lanes" rests on two runs the controller executed, not on a
  subagent's report. (Invocation note: `$PATH` must be escaped as `\$PATH`
  or the outer shell expands it, and this host's WSL reaches the tree at
  /mnt/c/..., not via $HOME.)
- **Deliberately NOT changed, and why:** the 2026-08-14 research
  assessment's ADDENDUM 2 still calls the MCP gate the "next build
  workstream". It is a dated record of a ruling AS RECEIVED; editing it
  would falsify what was ruled. ROADMAP carries the current status instead.
- **Recorded weakness, unguarded:** each repo's CLAUDE.md is untracked by
  design, so neither `git grep` nor the contractcheck markdown pins can see
  it — which is exactly how it rotted to a stale lane count and a 12-probe
  ladder before this session fixed it. The same rot will recur; nothing
  mechanically prevents it.

## Commit blocks (operator runs these; agents wrote no history)

Verified with read-only `git status` at hand-off time. Remotes NOT checked,
so `git push -u` covers either case. Re-run the tree-identity diff first —
several agents wrote both trees concurrently today.

```bash
# pre-flight: the two copies must be identical before either repo is committed
cd ~/dev/treasury-intent-controller
diff -r declarant ../intent-plane/declarant --exclude=__pycache__ --exclude=.pytest_cache \
  && diff core/internal/scoring/scorer.go ../intent-plane/core/internal/scoring/scorer.go \
  && echo TREES-IDENTICAL
```

```bash
cd ~/dev/intent-plane

# 1. the spec first -- this repo's standing rule is CONTRACT before code.
#    Carries: the MCP gate section, the fork-vs-collision rule with the
#    withdrawn claim quoted in place, both redirect rules, claims 15 + 17.
git add CONTRACT.md
git commit -m "docs(contract): MCP gate, redirect rules, fork-vs-collision"

# 2. the redirect fail-open, both languages and all three call sites.
git add core/internal/scoring/scorer.go core/internal/scoring/scorer_test.go \
        declarant/client.go declarant/declarant_test.go \
        declarant/pydeclarant/client.py declarant/pydeclarant/test_client.py
git commit -m "fix: never follow HTTP redirects on the declarant or scorer seam"

# 3. the MCP gate itself, plus the two shared pieces it forced out of the
#    LangChain adapter (gating.py, _gate_double.py).
git add declarant/pydeclarant/mcp_adapter.py declarant/pydeclarant/test_mcp_adapter.py \
        declarant/pydeclarant/gating.py declarant/pydeclarant/_gate_double.py \
        declarant/pydeclarant/langchain_adapter.py declarant/pydeclarant/test_langchain_adapter.py
git commit -m "feat: MCP gate -- intent-gate middleware and gated proxy"

# 4. shipped docs, INCLUDING the operator-requested README rewrite (it now
#    leads with the three embed shapes and a "What it refuses" section; the
#    two-sides framing, three commitments, decision flow and status-honestly
#    foot are kept). verifier/verify.go is a COMMENT-only change (a stale
#    probe ordinal), which is why it rides with the docs, not the code.
git add README.md docs/assurance.md docs/integration.md verifier/verify.go
git commit -m "docs: README rewrite; MCP + redirect guidance; claim 17; 14-probe ladder"

git push -u origin HEAD
```

```bash
cd ~/dev/treasury-intent-controller

# 1. spec first here too (the ported 2.4/2.7 text).
git add CONTRACT.md
git commit -m "docs(contract): port MCP gate and redirect rules"

# 2. consume the SDK packages back + the same redirect fix in this tree's Go
#    copies. These files must stay byte-identical to the SDK's.
git add declarant/ core/internal/scoring/scorer.go core/internal/scoring/scorer_test.go
git commit -m "feat: consume MCP gate back; refuse redirects in both clients"

# 3. the live legs and the ladder renumber (12 -> 14, feed count 4 -> 6).
git add treasury/probes/mcp_gate_live.py treasury/probes/mcp_proxy_live.py \
        treasury/quickstart.ps1 treasury/quickstart.sh treasury/README.md
git commit -m "feat: live probes 9-10 (MCP middleware, gated proxy); ladder 14/14"

# 4. the record: handoff, three learnings, and the docs that carry counts.
git add docs/handoff/2026-08-20-mcp-gate-and-three-fail-opens.md docs/handoff/HANDOFF.md \
        docs/learnings/2026-08-20-key-fork-is-fail-open-not-fail-closed.md \
        docs/learnings/2026-08-20-key-blind-double-asserted-no-at-most-once.md \
        docs/learnings/2026-08-20-redirect-following-clients-turn-declaring-into-not-declaring.md \
        docs/learnings/LEARNINGS.md docs/ROADMAP.md README.md
git commit -m "docs: handoff + 3 learnings (fork-is-open, key-blind double, redirects)"

git push -u origin HEAD
```

**Not folded in, left to you:** nothing untracked in either repo is being
staged that this session did not create — I checked. The plan file
`docs/superpowers/plans/2026-08-20-mcp-adapter.md` stays untracked by
standing policy (`.git/info/exclude`), in both repos.
