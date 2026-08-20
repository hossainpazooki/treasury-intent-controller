# A followed HTTP redirect turns "declaring an intent" into "not declaring it" — three call sites, both languages, in already-shipped code

ts: 2026-08-20T15:25:00Z
commit: intent-plane d9041f5 + uncommitted working tree (approximate ts — captured from a mutation pass during the MCP gate build; SDK HEAD had not moved all session because nothing was committed)
session: 52e04ce2-ba8f-439f-b44b-3b05d69cdd88
status: verified
fact: Both declarant clients (Go `declarant/client.go`, Python
`declarant/pydeclarant/client.py`) and the gate's OWN outbound scorer client
(`core/internal/scoring/scorer.go`) used a plain HTTP client that follows
redirects with no policy. When the counterparty answers with 301/302/303, the
client downgrades POST to GET and DROPS the request body, so the redirect
target receives a request that never carried the declaration (or the criterion
evaluation) at all — and its `ACHIEVED`-shaped or `{"result":"PASS"}` 200 is
then read back as a real authorization. On the declarant seam the action fires
having never been declared anywhere. On the scorer seam a criterion PASSES from
a responder that was never told which criterion it was answering about, and
there ALL FIVE codes fail open — 307/308 forward the body faithfully, but to
the wrong origin. This was in code that had already shipped and been live-proven --
the LangChain adapter's 2026-08-18 live run went green over the
very client carrying the hole. The realistic trigger is infrastructural, not
adversarial — an https-upgrade redirect, a moved path, or a proxy placed in
front of the gate.
basis: Declarant seam: "HTTP 301 -> EXECUTED executions=1 | gate saw POST /v2/intents (388 bytes) | OTHER ORIGIN saw GET /elsewhere (0 bytes)"; 302 and 303 identical; 307/308 refused. "FAIL-OPEN on 3 of 5 redirect codes". Scorer seam (throwaway Go probe, run then deleted): "HTTP 301 -> Score=PASS | other origin saw GET /elsewhere bodylen=0", same for 302/303, and "HTTP 307 -> Score=PASS ... HTTP 308 -> Score=PASS" with the body forwarded — FAIL-OPEN on all five.
re-verify: `grep -rn "CheckRedirect" ~/dev/intent-plane --include=*.go` returns the scorer and declarant policies, and `go test ./core/internal/scoring -run TestHTTPScorerNeverFollowsRedirects -count=1` passes; dropping `CheckRedirect` from `NewHTTPScorer` turns that test red on all five codes.
lesson: For any HTTP seam that carries an AUTHORIZATION or a DECISION, the
default redirect policy is a fail-open waiting to happen, because a redirect
silently changes BOTH the origin you are talking to and (for 301/302/303)
whether your payload is sent at all. Audit every HTTP client construction in a
security path for an explicit redirect policy — `grep` for the client
constructor, not for the word "redirect", since the defect is an ABSENT option
and absent options are invisible to a reader looking for a mistake. This is
also the second time in one session that a defect hid in what a component does
by DEFAULT rather than in what anyone wrote (see
[[2026-08-20-key-blind-double-asserted-no-at-most-once]], where the fixture's
default was key-blindness). Related: [[2026-08-20-key-fork-is-fail-open-not-fail-closed]].
