# A conventional-commits `!` breaking marker breaks the emitted commit command in interactive Git Bash

ts: 2026-08-17T02:05:43Z
commit: intent-plane d36321d
session: e6e0badf-83f6-46ed-aee7-1ce4b994c525
status: verified
fact: Emitting `git commit -m "refactor!: ..."` for the operator to paste into
interactive Git Bash FAILS. Interactive bash performs history expansion before
quote processing, and it applies inside DOUBLE quotes, so `refactor!:` is read
as the history modifier `!:` with an empty event designator. The shell prints
`bash: : unrecognized history modifier`, aborts the command, and — because the
message was multi-line — spills the remaining body lines into the shell as
commands (`bash: Renames: command not found`, `bash: deployment: command not
found`), leaving the terminal stuck at a `>` continuation prompt. The `git add`
on the preceding line had already succeeded, so the failure mode is "staged but
not committed", which looks like a partial commit and is not one.

Consequences for how commit commands get emitted here: a `!` breaking marker in
a subject line needs `set +H` on a preceding line, or single quotes (which
defeat history expansion but then collide with any apostrophe in the body), or
`git commit -F -` with a heredoc, or simply dropping the marker and stating the
break in the body. Single quotes were NOT viable in this instance because the
body contained "CONTRACT.md's". This is a Git-Bash-interactive property, not a
git property: the same string in a script file, or via the Bash tool
non-interactively, works fine — which is exactly why it survives authoring and
only fails at the operator's prompt.

basis: observed first-party in the operator's pasted transcript 2026-08-16:
`bash:  : unrecognized history modifier` / `bash: Renames: command not found` /
`bash: SCORER_ATLAS_DIR: command not found` / `bash: deployment: command not
found`, followed by a `>` prompt. State confirmed read-only immediately after:
`git diff --cached --name-only` listed all 8 files staged while
`git log --oneline -1` still showed `d36321d`, i.e. the commit had not run.
Mechanism confirmed by probe: `bash -ic 'case $- in *H*) ...'` prints
"H flag SET - history expansion active", and the same probe after `set +H`
prints "H CLEARED". NOTE, stated rather than hidden: `bash -ic` does NOT
reproduce the failure itself (history expansion needs a real interactive
session with loaded history), so the probe below verifies the MECHANISM and the
fix, not the crash; the crash's evidence is the transcript above.
re-verify: bash -ic 'case $- in *H*) echo "H set: bare ! in a double-quoted commit subject will expand";; *) echo "H clear";; esac' 2>&1 | grep -v "ioctl\|job control"
