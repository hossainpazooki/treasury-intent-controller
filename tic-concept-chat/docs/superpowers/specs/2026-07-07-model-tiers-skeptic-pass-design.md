# Design — model tiers + producer≠judge skeptic pass

Date: 2026-07-07 · Status: **approved design, NOT implemented**
Architectural parallel to rigor `f5d9783` ("build tier on Sonnet 5"): model
choice is a config-centralized, role-based decision, and the judgment tier is
never spent producing the work it will later judge.

## Why

`server.py` hardcodes `MODEL = "claude-opus-4-8"` and `context.py`'s framing
asks that single call to be both the design-discussion producer **and** its
own skeptic ("be skeptical… mark uncited claims as inference") — the
fox-guarding-the-henhouse pattern rigor's judgment-dispatch exists to forbid.
This design ports the substance in two stages: (A) the config substrate, then
(B) a three-lane producer/worker/judge pipeline.

## Stage A — config substrate

### `config/models.json`

```json
{
  "discussion": "claude-opus-4-8",
  "worker": "claude-sonnet-5",
  "judgment": "claude-fable-5"
}
```

Deliberately **flat** (role → model). Rigor separates tier→model from
agent→tier; three roles in one service do not earn that indirection. The
parallel kept: model choice lives in exactly one place — never in code
literals, never in prose.

### `models.py`

- `resolve(role: str) -> str` reading `config/models.json`.
- Loaded **at import** in `server.py`, so a missing/malformed config or an
  unknown role fails the server at startup, loudly — same posture as
  `context.py`'s doc loading.

### `server.py` change

`MODEL = "claude-opus-4-8"` → `MODEL = models.resolve("discussion")`.
No behavior change in Stage A.

### check-tier-sync analog (test)

A test asserting bidirectional sync:
- every role referenced in code exists in the config, and
- the config has **no orphan roles** (an entry no code path dispatches to is
  a decorative tier — the exact flaw this analog mechanically prevents).

Note: until Stage B lands, `worker` and `judgment` are orphans by this test's
definition. Either ship Stage A's config with only `discussion` and add the
other two roles in Stage B (preferred — keeps the sync test green and honest),
or land A and B together.

## Stage B — three-lane skeptic pass

### Placement: server-side, same SSE stream

After the producer's `done` event, the **server keeps streaming**: skeptic
events follow on the same response.

- Reason: citations live on the final message's content blocks server-side;
  client-initiated verification would force the browser to reconstruct
  claim↔citation pairing it never had.
- The reply is fully delivered before the skeptic starts, so skeptic
  latency/failure never costs the user the answer.

### Lane 1 — discussion (producer), `discussion` role

Unchanged from today: streams `thinking` / `text` / `citation` / `done`.
The framing in `context.py` keeps "be direct and skeptical" as discussion
*style*, but the closing paragraph's **verification duty** ("mark uncited
claims as inference") is no longer load-bearing — lanes 2–3 now enforce it
mechanically. Keep the instruction (it improves producer behavior); stop
*trusting* it.

### Lane 2 — worker (extraction), `worker` role (Sonnet 5)

- **Input:** the final reply's content blocks (text + attached citations).
- **Output:** schema-validated JSON via a **forced tool call**:

```json
{
  "claims": [
    {
      "id": "c1",
      "claim_text": "…",
      "status": "cited | uncited",
      "citation": { "title": "…", "cited_text": "…" }
    }
  ]
}
```

(`citation` is `null` when `status` is `uncited`.)

- Mechanical transcription against the schema — **no judgment**. This is the
  build-tier analog: the worker lane does the cheap structured work so the
  judgment lane never has to.

### Lane 3 — judgment (refutation), `judgment` role (Fable 5)

- **Input:** the claims JSON only — each cited claim paired with its
  `cited_text` excerpt. **Not** the ~18K-token docs, not the full reply. The
  expensive model gets the smallest context; that economy is the point.
- **Output:** verdict per claim, forced-tool JSON:
  - cited claims → `supported | overreach`
  - uncited claims → `marked-inference | unmarked-inference`
- **Posture:** default refuted — a claim is `supported` only if the excerpt
  actually carries it.
- **Stated limitation:** judging from excerpts alone can miss
  context-dependent overreach. Accepted cost of the cheap-context design;
  carried in the README.

### New SSE events

| event | payload | when |
|---|---|---|
| `skeptic_claims` | the extraction JSON | after worker call returns |
| `skeptic_verdict` | one claim id + verdict | per claim, as judged |
| `skeptic_done` | summary counts | end of skeptic pass |
| `skeptic_error` | message | any skeptic-lane failure |

### UI

Verdict panel under the reply — per-claim chips:
✓ supported · ✗ overreach · ○ inference · ⚠ unmarked.

## Error handling

Skeptic-lane exceptions emit `skeptic_error` and end the stream. They **never
retroactively taint the delivered reply**. Producer-lane error handling is
unchanged from today (`server.py`'s existing except-chain).

## Testing — honest boundary

Offline only, mocked API (same as the repo today):

- extraction/verdict **schema validation**
- SSE **event shapes** (including the four new skeptic events)
- the **role-sync test** (check-tier-sync analog)
- `inject_documents` regression (cached-prefix invariant untouched)

**Live effect stays unverified until an API key exists.** This app has never
had its streaming verified end-to-end; Stage B inherits that. The README
carries built-vs-verified tags.

## Non-goals

- No stakes rubric or dispatcher (no per-turn tier routing).
- No cheap-vote `skeptic-verifier-fast` analog — a single-user concept chat
  has no dispatcher to route votes; dead weight.
- No change to the cached-prefix document injection design.

## Same-change obligations

- README updated in the same change as each stage (docs-honesty rule):
  Stage A → the tier map and why; Stage B → the three-lane flow, the
  excerpt-only judging limitation, and built-vs-verified status tags.
