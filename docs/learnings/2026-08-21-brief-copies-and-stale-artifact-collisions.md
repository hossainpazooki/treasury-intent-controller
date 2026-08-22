# A task brief is a COPY, and two repos sharing one tool's default output directory collide silently

ts: 2026-08-22T00:08:00Z
commit: intent-plane 2fab226 / monorepo f93148c + uncommitted working tree
session: 192b819d
status: verified
fact: A twelve-task subagent build hit the same defect class three times.
The `task-brief` helper writes to `<git-dir>/sdd/` of the repo containing
the PLAN FILE. The plan existed in BOTH repos, so passing one path in one
invocation and the other path in another wrote briefs into two different
directories. Consequence one: correcting the plan did NOT correct the brief
copies two implementers were already reading, so tasks 3 and 5 received
stale predicted test counts. Consequence two, far worse: the monorepo's
`.git/sdd/` still held a COMPLETE prior fan-out's artifacts from 2026-08-03/04
under IDENTICAL filenames (`task-1-brief.md` .. `task-10-*`), so pointing an
agent at `task-8-brief.md` there handed it a brief for a different project's
"Task 8: READMEs". The agent refused to work from it and diagnosed the
collision from file mtimes and sibling artifacts. The same trap then caught
the CONTROLLER: a wait-loop matched `PASS` lines inside the stale
`task-9-report.md` and the controller briefly read a 17-day-old report as the
current task's output, noticing only because it named a module path
(`github.com/pazooki/...`) that no longer exists.
basis: `stat` on the monorepo's copies, verbatim: `2026-08-04 15:35:06
.git/sdd/task-9-report.md`, `2026-08-04 15:26:16 .git/sdd/task-9-brief.md`,
against a session running on 2026-08-21. The blocked agent's own evidence:
"its content is 'Task 8: READMEs -- root (the plane) and treasury (the
narrative)' ... mtime Aug 4 15:10, and there's already a matching
task-8-diff.txt, task-8-diff-v2.txt, and task-8-report.md all from Aug 4
15:16-15:23 sitting next to it". An earlier controller entry in the same
run's ledger diagnosed this WRONGLY as "regeneration did not overwrite" and
was corrected in place once the two-directory cause was found.
re-verify: `ls -l "$(git -C C:/Users/hossa/dev/treasury-intent-controller rev-parse --git-path sdd)" | head -20` -- the Aug-3/4 artifacts still sit beside this session's, under the same names.
lesson: Treat a brief as what it is -- a COPY that goes stale the moment the
source changes. Either regenerate every copy after any plan edit and verify
freshness, or pass an explicit per-plan output path so names cannot collide
across runs. More generally: before reading any artifact at a path that a
previous run also wrote, check freshness by BOTH mtime and a content
fingerprint, because a plausible-looking file at the expected path is the
failure mode that survives a quick glance. The agent that refused to
improvise from a wrong brief prevented an artifact inconsistent with the
contract; an agent that had guessed would have produced something that later
reviews might well have passed.
