ts: 2026-07-13T22:37:24Z
commit: 6adff98
session: 71412dd0-24d9-4663-b2a9-0130c545ee1f (atlas-treasury-payment-loop)
status: verified

fact: `TIC_SCORER_URL` is used verbatim as the POST target (`internal/scoring/scorer.go` posts to `h.Endpoint` unmodified) — it must be the FULL endpoint including `/ml/evaluate`. A base URL is not an error at boot: every score becomes a 404 → non-2xx → `Unevaluable`, and the gate refuses everything. Fail-closed exactly as the §S.1 matrix promises, but easy to misread as a scorer bug.

basis: live probe 2026-07-12 — scorer access log with `TIC_SCORER_URL=http://localhost:8000`:
```
INFO: 127.0.0.1:47470 - "POST / HTTP/1.1" 404 Not Found
```
gate answer: `{"terminal":"FAILED","reason":"unevaluable:amount_under_ceiling",...}`. Same declaration with `TIC_SCORER_URL=http://127.0.0.1:8000/ml/evaluate`: `{"terminal":"ACHIEVED","reason":"","achieved_seq":9,...}`.

re-verify: `grep -n "h.Endpoint" internal/scoring/scorer.go` — the request is built on `h.Endpoint` with no path joined.
