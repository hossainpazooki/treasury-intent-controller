# Go's build ID defeats `-trimpath` reproducibility — and the earlier hash in the trimpath learning does not reproduce

ts: 2026-08-16T19:20:00Z
commit: intent-plane main 89443a3
session: e6e0badf-83f6-46ed-aee7-1ce4b994c525
status: verified
fact: Partially supersedes `2026-08-16-plain-gobuild-irreproducible.md`
(keeps its direction, corrects its flag set and refutes its recorded hash).
Rebuilding `verifier/cmd/intent-verify` at a fixed commit and toolchain
with `-trimpath` alone does NOT give a stable sha256 across build
environments. THREE flags are each independently load-bearing, and only all
three together make a checkout build and an extracted-source-archive build
byte-identical:
  -trimpath           drops the builder's absolute checkout path
  -buildvcs=false     a build inside a git checkout stamps vcs.revision /
                      vcs.time / vcs.modified and resolves the main module to
                      a VCS pseudo-version; a build from an archive stamps
                      none and resolves to (devel)
  -ldflags=-buildid=  Go stamps a build ID whose ACTION-ID component varies
                      with the build directory even under -trimpath
The build-ID case is the subtle one: two builds of identical source differed
in exactly 59 bytes out of 3401199, all inside the build ID, while the build
ID's own CONTENT component was identical — i.e. the compiled content was
already reproducible and only the stamp was not. A kit that tells an auditor
to compare sha256 after a plain `-trimpath` rebuild therefore fails for an
auditor working from a tarball, which is the normal auditor posture.

Second, corrective finding: the hash recorded in
`2026-08-16-plain-gobuild-irreproducible.md`
(sha256(iv-trim) = 883560cf48b0e2c4dafc894fc4cb69722850d9be838fd82425629e97e89ddf89)
does NOT reproduce at the same commit, same go1.26.0, same
CGO_ENABLED=0/GOOS=linux/GOARCH=amd64, under any of -trimpath (0cd1cac3...),
-trimpath -buildvcs=false (3a1eb204...), or an archive build (217e305b...) —
while my own builds are byte-stable across repeated runs, so this is not
local nondeterminism. That entry's corroboration argument is also
self-undermining: it cites the SAME -trimpath hash at 42ce556 and at 89443a3
as confirmation, but VCS stamping is on by default in a checkout and
vcs.revision differs between those commits, so identical hashes across them
imply the measured builds carried no VCS stamp. The original entry's
`re-verify:` line is now unrunnable regardless: it points at
`.claude/worktrees/sdk-ship`, which was removed when sdk-ship was
fast-forwarded into main, and it greps binaries for the literal "worktrees",
which only ever appeared because the build happened under that path.

Consequence already applied (intent-plane, uncommitted at time of writing):
`verifier/KIT.md` documents all three flags with the measurement behind each;
`scripts/release.sh` builds with them and records `buildflags` in the kit's
VERSION file alongside commit and toolchain.

basis: first-party at 89443a3 on main. Corrected flag set reproduces
byte-identically from two different source roots — a git checkout
(~/dev/intent-plane) and a `git archive HEAD` extraction with no .git present
— both sha256
f5647aa1744f63ebd68cae11dbdc1f7021bc4a57592f2eeee7973bec3be2e8a1, which also
equals the linux/amd64 binary in the assembled kit. Byte delta between the
two uncorrected builds measured with `cmp -l | wc -l` = 59; build IDs read
with `go tool buildid`; embedded metadata read with `go version -m`. Ruled
out as causes by direct test: source line endings (CRLF-converting the
archive's .go files changed nothing), go.work/GOFLAGS (absent/empty), and
source drift (`diff -r --strip-trailing-cr` clean but for `.worktreeinclude`).

re-verify: cd ~/dev/intent-plane && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -buildvcs=false -ldflags=-buildid= -o /tmp/iv-a ./verifier/cmd/intent-verify && rm -rf /tmp/tb && mkdir -p /tmp/tb && git archive HEAD | tar -x -C /tmp/tb && (cd /tmp/tb && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -buildvcs=false -ldflags=-buildid= -o /tmp/iv-b ./verifier/cmd/intent-verify) && cmp /tmp/iv-a /tmp/iv-b && echo REPRODUCIBLE
