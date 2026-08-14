# intent-declare exit 0 includes SHADOW_RECORDED, and no test pins the CLI

ts: 2026-08-14T04:00:14Z
commit: 42ce556 (intent-plane; captured in the sdk-ship worktree)
session: C:\Users\hossa\.claude\projects\C--Users-hossa-dev\0295a4ce-41cc-4e54-a674-1818455dbf7c.jsonl
status: verified
fact: The intent-declare CLI exits 0 for class PROCEED OR SHADOW_RECORDED — exit 0 means "durably recorded", not "authorized"; a naive `if $? -eq 0 then execute` script fires an unauthorized action under a shadow-posture spec, and posture is invisible to the declarant (it lives in the signed payload). The CLI package has NO test files, so nothing pins this behavior. Scripts must parse `class=`; docs/demos must never equate exit 0 with authorization.
basis: at 42ce556: main.go:14 `// Exit 0 iff the class is PROCEED or SHADOW_RECORDED; 1 otherwise; 2 usage.`; main.go:68-70 `if res.Class == declarant.Proceed || res.Class == declarant.ShadowRecorded { os.Exit(0) }`; `go test ./declarant/cmd/intent-declare -count=1` → `?   github.com/hossainpazooki/intent-plane/declarant/cmd/intent-declare  [no test files]`
re-verify: sed -n '14p;66,72p' ~/dev/intent-plane/declarant/cmd/intent-declare/main.go
