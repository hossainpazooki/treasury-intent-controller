// intent-verify re-derives the durable feed's commitments from the record
// bytes alone (CONTRACT.md §9.1): run it against an events.jsonl you were
// handed, without trusting the gate that wrote it.
//
//	intent-verify <path/to/events.jsonl>
//
// Prints the canonical report to stdout and exits 0 ONLY on VERIFIED.
package main

import (
	"fmt"
	"os"

	"github.com/hossainpazooki/intent-plane/verifier"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: intent-verify <events.jsonl>")
		os.Exit(2)
	}
	raw, err := os.ReadFile(os.Args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "intent-verify: %v\n", err)
		os.Exit(2)
	}
	rep := verifier.Verify(raw)
	fmt.Print(verifier.Render(rep))
	if rep.Overall != verifier.Verified {
		os.Exit(1)
	}
}
