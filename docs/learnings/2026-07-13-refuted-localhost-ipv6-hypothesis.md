ts: 2026-07-13T22:37:24Z
commit: 6adff98
session: 71412dd0-24d9-4663-b2a9-0130c545ee1f (atlas-treasury-payment-loop)
status: refuted-assumption

fact: The first live-probe failure (gate → WSL scorer) was initially attributed to "Go resolves `localhost` to ::1 while the WSL2 forward is IPv4-only". REFUTED: the scorer's own access log showed the gate's POSTs *arriving* (as 404s) under `localhost` — the connection succeeded; the sole verified cause was the missing `/ml/evaluate` path (see [2026-07-13-tic-scorer-url-full-endpoint.md]). Recorded because the wrong version briefly entered program memory before being corrected — a pattern-matched network hypothesis survived one probe cycle without being checked against the log that refuted it.

basis: same captured log lines as the sibling entry — `POST / HTTP/1.1" 404 Not Found` from `127.0.0.1:47470` while the gate ran with `TIC_SCORER_URL=http://localhost:8000` (a request that reached the server cannot be a failed dial).

re-verify: run the gate with `TIC_SCORER_URL=http://localhost:8000/ml/evaluate` (hostname `localhost`, full path) against the WSL scorer and observe ACHIEVED — the combination that isolates hostname from path.
