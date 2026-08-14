# The SDK repo is public; this monorepo is private

ts: 2026-08-14T04:00:35Z
commit: 42ce556 (intent-plane remote head at capture; verified via gh)
session: C:\Users\hossa\.claude\projects\C--Users-hossa-dev\0295a4ce-41cc-4e54-a674-1818455dbf7c.jsonl
status: verified
fact: hossainpazooki/intent-plane is PUBLIC (visibility "public"); hossainpazooki/treasury-intent-controller is PRIVATE. Consequences: (a) the registry-hygiene distribution avenue's prerequisite is already satisfied — pkg.go.dev can index the module now; (b) anything committed to the SDK repo is immediately public, so handoffs, learnings, and article drafts stay in this monorepo; (c) the SDK's docs are already serving as public marketing surface at every commit.
basis: `gh api repos/hossainpazooki/intent-plane --jq '{private,visibility}'` → `{"private":false,"visibility":"public"}`; `gh api repos/hossainpazooki/treasury-intent-controller --jq '{private}'` → `{"private":true}`
re-verify: gh api repos/hossainpazooki/intent-plane --jq .visibility
