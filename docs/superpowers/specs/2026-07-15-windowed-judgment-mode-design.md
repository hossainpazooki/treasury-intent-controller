# Windowed judgment mode, measured against excerpt-only — design

2026-07-15. Approved by Hossain in-session. Follows the 2026-07-08 model-tiers
/ skeptic-pass plan and the 2026-07-14 live-smoke verification (13/13).

## Problem

The judgment lane (fable-5) sees **excerpts only** — claim text plus
`cited_text` — never the reply or the docs. The README carries this as the
accepted limitation: excerpt-only judging can miss context-dependent
overreach. A quote that is a genuine doc substring and *locally* carries a
claim can be inverted by the words immediately around it; no excerpt-only
judge can detect that, because the inverting context is outside the excerpt
by construction. Until now this was argued, not measured. The credentialed
probe (`scripts/live_smoke.py`) makes it measurable.

## Deliverable (decided)

**Feature + measurement**: ship a windowed judgment mode in the app, extend
the live probe to measure both modes on identical inputs, and **flip the
default to `window`** in this change if and only if the measurement's gating
checks pass. If they fail, the default stays `excerpt` and the status says so.

## The trap (real doc bytes, known-correct verdict)

Primary — `CONTRACT-DURABILITY.md` refactor goal 4:

> "the gate STOPS at appending the single `ACHIEVED` record to the durable
> log and **no longer** calls `adapter.OnAchieved` in-process."

- planted claim c4: "The gate calls `adapter.OnAchieved` in-process during
  authorization." — status `cited`
- cited_text: `` calls `adapter.OnAchieved` in-process `` — a genuine
  contiguous substring that locally carries the claim
- correct verdict: `overreach` — the inverting "no longer" sits immediately
  before the quote, **outside any excerpt by construction**

Backup with the same shape (if the doc line ever changes):
"`GlobalSeq` is **never** part of the per-intent `TrajectoryHash`" —
cited_text `part of the per-intent `TrajectoryHash``.

Honesty note: if the excerpt judge nonetheless returns `overreach` on c4, it
can only be from a prior (e.g. refusing fragmentary quotes), not from
evidence — the information is absent from its input. The probe records that
outcome as a finding; it is not a probe failure and does not rescue
excerpt-only judging from the argument.

## Window mode — `skeptic.py`

Mechanical, server-side context; never agentic (the judge gets no tools and
no discretion over what context it sees — rejected alternative: tool-use
context fetching, which adds calls, nondeterministic cost, and hands the
expensive model exactly the discretion this design withholds).

- New pure function `doc_windows(claims, docs, radius=400) -> dict[str, str]`
  (claim id → window). For each **cited** claim: exact substring search for
  `cited_text` in the doc named by `title` (the live smoke proved every real
  citation is a literal substring; first occurrence wins), then ±`radius`
  chars snapped outward to line boundaries. Uncited claims get no window.
- A cited_text that cannot be located, or a title not in the doc set,
  **raises** `ValueError` → surfaces as `skeptic_error` on the stream
  (fail-loud; skeptic failure still never costs the delivered reply).
- `judge_claims(client, claims, *, mode, reply_text=None, docs=None)`:
  - `excerpt`: today's behavior, byte-for-byte prompt unchanged.
  - `window`: prompt carries (a) the claims JSON as today, (b) the full
    rendered reply (`render_reply` output — same text the worker saw),
    (c) per cited claim, its doc window, labeled by claim id. Instructions:
    a cited claim is `supported` only if the quote carries it as stated AND
    the surrounding passage does not qualify or negate it; the reply context
    disambiguates referents and whether a claim is actually asserted.
    Default remains refutation.
- **Unchanged**: verdict vocabulary (`supported`/`overreach`/
  `marked-inference`/`unmarked-inference`), `VERDICT_TOOL` schema,
  `validate_claims`/`validate_verdicts`, all SSE event names and shapes, the
  worker lane, `JUDGMENT_MAX_TOKENS` semantics (bump only if the live run
  shows truncation).

## Config

`config/models.json` stays flat role→model (deliberate, sync-tested — do not
touch). New `config/skeptic.json`:

```json
{ "judgment_context": "window" }
```

Resolved fail-loud at import (same pattern as `models.py`): missing file,
missing key, or a value outside `{"excerpt", "window"}` fails server startup,
never mid-request. `server.py` passes the resolved mode plus `reply_text`
and the startup-loaded docs into `judge_claims`.

## Measurement — extends `scripts/live_smoke.py`

Both modes run in the same probe over identical inputs: planted c1–c3
(existing trio, known verdicts), planted c4 (the trap), and the organic
reply.

Gating checks (hard PASS/FAIL, added to the existing 13):
1. window mode returns `overreach` on c4;
2. window mode matches all three known verdicts on c1–c3 (no regression);
3. every pre-existing check stays green (citations real, cache prefix
   engaged/read, lanes ordered after `done`).

Reported, never gated:
- excerpt mode's verdict on c4 (expected miss → `supported`; any other
  outcome recorded per the honesty note above);
- organic-reply verdict distribution per mode, side by side;
- measured token cost per judgment call per mode (from `usage`).

Expected cost shape: window adds the reply (~1–2K tokens) plus ~150
tokens per cited claim to the fable-5 call — a small multiple of excerpt,
roughly an order of magnitude under attaching the full ~18K-token doc set.
Rate-limit handling unchanged: a rate-limited call is INCONCLUSIVE
environment evidence, never app evidence; pace with sleeps as the probe
already does.

## Offline tests (mocked; extend the 35)

- `doc_windows`: exact location; radius and line-boundary snapping; first
  occurrence wins on duplicates; unknown title raises; absent substring
  raises; uncited claims get no window.
- `config/skeptic.json` resolver: fail-loud on missing/invalid; value pinned
  (changing the mode is a deliberate config edit, same discipline as the
  model pins).
- window-mode `judge_claims` prompt assembly: the request body actually
  contains the reply text and each cited claim's window (assert on the
  mocked client's captured request).
- excerpt path unchanged: existing tests keep passing without edits.

## Docs honesty

README changes land **only after** the live run passes:
- limitation section rewritten to the residual: negation/qualification
  outside the window radius, and cross-document context, remain uncaught;
- skeptic-pass status block gains the windowed-mode evidence (date, probe,
  trap outcome in both modes);
- Test section documents the extended probe.

If the measurement fails gating, the default stays `excerpt`, the limitation
section stays as-is, and the probe's findings are recorded in the README
status verbatim (a measured "window mode did not win" is a publishable
result).

## Non-goals

- Full-docs judgment mode (measured ladder rejected — cost defeats the
  smallest-context economy).
- UI changes beyond none (event shapes unchanged; a mode label in the UI is
  optional follow-up, not this slice).
- Any change to the worker lane, the discussion lane, or the cached document
  prefix (the prefix must stay byte-identical — the probe will catch a
  violation via the cache-read check).

## Acceptance

`pytest` green (35 existing + new offline tests); `scripts/live_smoke.py`
green on all gated checks with the comparison table printed; README updated
per above; default `judgment_context` = `window` iff gating passed.
