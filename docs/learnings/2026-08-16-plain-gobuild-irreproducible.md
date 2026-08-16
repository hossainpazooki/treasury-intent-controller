# Plain `go build` is irreproducible by construction — release kits need -trimpath plus a pinned toolchain

ts: 2026-08-16T17:52:30Z
commit: intent-plane sdk-ship 89443a3
session: 0295a4ce-41cc-4e54-a674-1818455dbf7c
status: verified
fact: On the same machine, same commit, same toolchain (go1.26.0), building
verifier/cmd/intent-verify with CGO_ENABLED=0 GOOS=linux GOARCH=amd64 yields
DIFFERENT sha256 hashes with and without -trimpath, because the plain build
embeds the absolute checkout path into the binary — so a kit README that
says "rebuild with plain go build to reproduce the hash" is false on every
machine, including the builder's own. Reproduce-my-hash instructions must
pin the exact flag set (CGO_ENABLED=0, GOOS/GOARCH, -trimpath) AND record
the toolchain version, since the Go version is embedded per patch release.
Triad-plan consequence already applied: KIT.md rebuild command corrected,
release.sh VERSION file records commit + `go env GOVERSION`.
basis: first-party run at 89443a3: sha256(iv-trim) =
883560cf48b0e2c4dafc894fc4cb69722850d9be838fd82425629e97e89ddf89,
sha256(iv-plain) =
5a2d5b57be11399df6a29e37f0b17d79de0fa46f2c045442f0657298d57a613f;
`grep -ac worktrees` = 1 in the plain binary, 0 in the trimpath binary.
Hashes independently match a skeptic agent's earlier run at 42ce556 (the
intervening commit touched only .md files).
re-verify: cd ~/dev/intent-plane/.claude/worktrees/sdk-ship && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o "$TMPDIR/iv-plain" ./verifier/cmd/intent-verify && grep -ac worktrees "$TMPDIR/iv-plain"
