# tic-concept-chat

Local web chat for discussing treasury-intent-controller's design concepts
(intent lifecycle, tri-state fail-closed scoring, idempotency by
construction, stable/volatile criteria, deterministic replay) with Claude
Opus 4.8, primed on the repo's README + CONTRACT.md + CONTRACT-DURABILITY.md.

## Contract-anchored citations

The three docs are attached as citation-enabled `document` blocks, so every
claim Opus makes is anchored to the exact contract passage it rests on. Each
reply shows inline `[n]` markers and a **Sources** list quoting the cited
text and its document. A claim with no citation is visibly Opus's own
inference, not contract text - which keeps the "the contract says" vs "Opus
thinks" boundary in view on every turn. Citations plumb the evidence for a
refute step; they do not perform it - a cited passage still has to actually
support the claim, so read the quote.

## Run

    pip install -r requirements.txt
    uvicorn server:app --port 8765

Open http://localhost:8765. Auth resolves from `ANTHROPIC_API_KEY` or an
`ant auth login` profile - the tool never stores keys.

## Notes

- Stateless server: the browser holds the conversation; refresh loses it.
  Use **Export .md** to save a transcript (sources included).
- The docs are attached as `document` blocks in the first user turn and
  injected server-side, so the browser never carries the ~18K-token context.
  The prefix is prompt-cached; the usage line under each reply shows cache
  write/read tokens (expect a write on turn 1, reads after).
- Docs are read from `../treasury-intent-controller` at startup (override
  with `TIC_DIR`); the server refuses to start if any doc is missing.
- Design spec: `../treasury-intent-controller/docs/2026-07-05-tic-concept-chat-design.md`

## Test

    python -m pytest -v
