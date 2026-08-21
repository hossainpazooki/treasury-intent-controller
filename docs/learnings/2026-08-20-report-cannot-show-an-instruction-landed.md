# A subagent report that omits an instruction is indistinguishable from one that completed it — only the artifact tells you which

ts: 2026-08-20T15:38:00Z
commit: intent-plane d9041f5 + uncommitted working tree (approximate ts — captured between the fix agent's first report and its second; the basis is the controller's own grep, quoted)
session: 52e04ce2-ba8f-439f-b44b-3b05d69cdd88
status: verified
fact: Mid-build, a fix agent was sent an ADDENDUM of four further defects
(one Critical: `tools="name"` silently ungating the named tool). Its
subsequent report was thorough about the ORIGINAL brief and did not
mention the addendum at all — no "done", no "skipped", no "pending".
Reading the report, the addendum's state was unknowable. A direct grep of
the artifacts at that moment returned NOT FOUND on all six addendum items.
The agent later demonstrated all six present, with file modification
times suggesting the work may have been landing while the grep ran. Which
of "omitted" or "in flight" was true cannot be settled from the record;
the operative fact does not depend on it: the report carried zero
information about the addendum either way, and a rewrite of the same
report with the work done would have read identically. The only signal was
the artifact.
basis: Controller grep, verbatim: "1. tools= str reject: NOT FOUND / 2. key-aware double: NOT FOUND (double still key-blind) / 3. test5 shared-key assert: NOT FOUND / 4. docstring claims: ... 14:This is the ONE pydeclarant module ... 272: Locked design decision 5 ... 298: Locked design decision 3" — against a fix report whose text contained no occurrence of the word "addendum".
re-verify: `grep -c "isinstance(tools, (str, bytes))" ~/dev/intent-plane/declarant/pydeclarant/mcp_adapter.py` prints 1 — the Critical item that the report was silent on is present in the committed tree (intent-plane 013dd0f).
lesson: Verify instructions against artifacts, never against reports —
and make the dispatch contract demand a per-instruction status line
("done / skipped because / pending") so that silence becomes a visible
defect instead of an ambiguity. The 2026-08-18 rule "a workflow's
self-reported success is a claim to verify" generalizes: a report's
SILENCE is not a claim at all, and so it cannot even be refuted.
