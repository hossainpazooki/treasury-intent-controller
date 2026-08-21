# An untracked CLAUDE.md is invisible to every gate, so its counts rotted silently to numbers two builds old

ts: 2026-08-20T15:20:00Z
commit: intent-plane d9041f5 + uncommitted working tree (approximate ts — the stale text was read when the file was surfaced mid-build; the docs sweep independently reported the same sites at 16:15Z)
session: 52e04ce2-ba8f-439f-b44b-3b05d69cdd88
status: verified
fact: Both repos keep `CLAUDE.md` local-only via `.git/info/exclude`, by
design (it is operator context, not product). The consequence nobody had
written down: an excluded file is invisible to `git grep`, to the
contractcheck markdown pins, and to every zero-grep the docs discipline
relies on. The SDK's copy therefore still said the pydeclarant lane was
"(15) + (13) = 28" and described a "12-probe quickstart ladder" while the
real numbers were 45 (then 76) and 14 — two builds of drift that no gate
could see and no sweep would catch. It is the first thing the next session
reads. Fixed by hand this session; nothing mechanical prevents a recurrence.
basis: intent-plane CLAUDE.md as surfaced mid-session: "core/scorer/.venv/Scripts/python -m pytest declarant/pydeclarant # declarant Python twin (15) + LangChain adapter (13) = 28" and "The live two-process smoke (12-probe quickstart ladder since 2026-08-18" — against a lane that measured "45 passed" at that moment and a ladder being renumbered to 14.
re-verify: `git -C ~/dev/intent-plane check-ignore -v CLAUDE.md` prints the `.git/info/exclude` rule, and `git -C ~/dev/intent-plane grep -l "pydeclarant" -- CLAUDE.md | wc -l` prints 0 — the file is present on disk yet absent from every git-backed check.
lesson: Any file deliberately kept outside version control is also outside
every check that reads version control. If such a file carries empirical
numbers, either stop putting numbers in it (point at the command instead of
quoting its count) or add a non-git check that reads it. Until one of those
happens, treat its counts as unverified on every pickup.
