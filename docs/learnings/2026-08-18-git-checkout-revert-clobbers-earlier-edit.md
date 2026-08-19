# Reverting a mutation probe with git checkout clobbers every other uncommitted edit in the file

ts: 2026-08-18T19:05:00Z
commit: intent-plane 60ffe38 + uncommitted working tree (the de-number edits; approximate ts — no clock was read at capture, but the basis pre-dates ce21168 by construction)
session: 3ca345ea-7cd4-4186-aa6e-809fb72dad15 (continued as 78bbff6c-2e2c-4e7f-b4ee-9fa22af6ee36)
status: verified
fact: A non-vacuity mutation probe (append a banned string to README.md, watch
the new contractcheck pin go red) was reverted with `git checkout --
README.md`. That command discards ALL uncommitted changes to the file — it
also silently wiped the session's earlier real de-number edit to README:213,
which had never been committed. The pin run that had just passed was
evidence about a tree that no longer existed. The loss was caught only
because a later zero-grep re-count returned `ADR-00: 1 files` where the
edit should have made it 0.
basis: "git grep -n 'ADR-00' -- ':!core/internal/contractcheck/internal_refs_test.go'" -> "README.md:213:test-grade until ADR-0009 (every signature says so); workload identity and" — the pre-edit text, back on disk after the checkout.
re-verify: `git -C ~/dev/intent-plane grep -n "test-grade until production key authority lands" ce21168 -- README.md` — the re-applied edit is in the commit that shipped.
lesson: Revert a planted mutation by editing the planted lines back out (the
same tool that planted them), never with `git checkout -- <file>` — checkout
restores the INDEX state, not the pre-mutation state, and in a session that
edits before it probes, those differ. After any file-level git restore,
re-run the cheapest whole-tree check (the zero-grep, the pin) before
trusting earlier evidence about that file.
