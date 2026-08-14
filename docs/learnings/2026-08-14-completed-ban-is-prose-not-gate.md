# The COMPLETED ban is prose plus a zero-grep, not a mechanized gate

ts: 2026-08-14T03:59:59Z
commit: 42ce556 (intent-plane; captured in the sdk-ship worktree)
session: C:\Users\hossa\.claude\projects\C--Users-hossa-dev\0295a4ce-41cc-4e54-a674-1818455dbf7c.jsonl
status: verified
fact: The ban on the COMPLETED terminal is normative CONTRACT §1.3 text plus a verified zero-occurrence state — NOT a contractcheck gate. vocab_test.go mechanizes only the forbidden actor nouns (regex, line 43) and the retired proper noun "Intent Interface" (README.md/CONTRACT.md only, lines 87-88). A workflow skeptic overturned a recon finding that claimed the COMPLETED ban was mechanized — citation-fidelity failure, verdict itself stood. Consequence for prose work: article/docs authors get no test tripwire on COMPLETED; the check is the author.
basis: at 42ce556: `grep -c "COMPLETED" CONTRACT.md` → `1` (the banning sentence itself); vocab_test.go:43 defines the forbidden regex over exactly the two actor nouns (quoted with the nouns elided here — this repo's gate greps all .md, and quoting them verbatim tripped it live while writing this entry: `forbidden := regexp.MustCompile(...)`); vocab_test.go:87 `if strings.Contains(string(b), "Intent Interface") {`
re-verify: grep -c "COMPLETED" ~/dev/intent-plane/CONTRACT.md
