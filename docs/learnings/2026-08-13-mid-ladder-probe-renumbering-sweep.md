ts: 2026-08-13T18:18:50Z
commit: 1054e0b
session: c4006536-80d3-4cc9-baa6-1738ea99ae66 (intent-plane-sdk)
status: verified
fact: Inserting a probe mid-ladder renumbers every later ordinal, and the ordinals live in more surfaces than the RESULT count — doc ladder tables, CONTRACT claim rows (14/15), both quickstart scripts' own comments, and cross-file references in CLAUDE.md/README/assurance. Labeling the new probe by build order ("probe 10") while the ladder places it sixth produced eight contradictory sites, including scripts contradicting their own execution order; all were caught by an independent skeptic pass, none by the gates (comments and prose have no test).
basis: skeptic verdict (claim 7, REFUTED in part): "Probe-number contradictions (declarant is #6, twins are #10 per the ladder and both CLAUDE.md files): Both CONTRACT.md claim rows 14/15 ... The scripts contradict their own execution order in comments: quickstart.sh:135/quickstart.ps1:116 label the declarant probe 'Probe 10'; quickstart.sh:165/quickstart.ps1:146 label the recompute 'Probe 9'." (captured against the pre-commit working tree later landed as TIC 2811126 and intent-plane ed1ec51 — pre-dates this entry's anchor; all eight sites were fixed and the vocab pins re-run green before commit)
re-verify: grep -n "Probe 6\|Probe 10" treasury/quickstart.sh
