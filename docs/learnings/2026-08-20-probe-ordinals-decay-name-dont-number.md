# Ladder ordinals in prose decayed AGAIN: three "probe 9" sites pointed at the wrong probe before the MCP renumber even started

ts: 2026-08-20T14:20:00Z
commit: intent-plane d9041f5 / monorepo b6a99a8 (approximate ts — captured during pre-dispatch planning of the MCP build, before any file in either tree had changed; the basis was re-read against those exact HEADs)
session: 52e04ce2-ba8f-439f-b44b-3b05d69cdd88
status: verified
fact: Three sites narrated a 2026-08-08 incident as "probe 9's first live
run" — meaning the VERIFIER RECOMPUTE probe as it was numbered that day.
By 2026-08-20 that probe was #12 (two live legs had been inserted ahead of
it on 08-18), so all three already pointed at the scorer-outage probe, and
the MCP renumber about to land would have re-pointed them at the brand-new
MCP middleware probe. The 2026-08-13 renumbering-sweep learning had been
applied to tables and scripts; it did not reach prose that NARRATES
history, and prose has no gate. Fix adopted: prose names the probe ("the
verifier recompute probe"); only ladder tables and scripts carry ordinals,
because those are what the mechanical sweep covers.
basis: "git grep -nE 'probe 9|probes 9' -- ." across both repos, pre-change: "verifier/verify.go:271: // (learned live: probe 9's first run refuted a correct feed on it)." / intent-plane "CONTRACT.md:1522: monorepo's quickstart probe 9's" / monorepo "CONTRACT.md:1473: legitimately omit: its absence is never a finding (ruled after probe 9's" — while the ladder's own scripts at that moment read "# Probe 12: the recompute probe" and "RESULT: $pass/12 probes passed".
re-verify: `git -C ~/dev/intent-plane grep -c "probe 9's" | wc -l` prints 0 and `git -C ~/dev/intent-plane grep -c "recompute probe" -- verifier/verify.go` prints 1 — the three sites now name the probe (landed in intent-plane 2484a22 / 5425719).
lesson: A renumbering sweep that greps for the OLD ordinal finds only the
sites written with that ordinal at that time; a site written two renumbers
ago carries an ordinal nobody is searching for. The durable fix is not a
better sweep but removing the decaying reference class: historical prose
names its subject, and numbers live only where a mechanical check reads
them. Related: the 2026-08-13 entry this recurs from.
