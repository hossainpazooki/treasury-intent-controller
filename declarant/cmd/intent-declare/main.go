// intent-declare declares one intent against a running gate through the
// declarant SDK (CONTRACT.md §2.7): derive-or-take an idempotency key, POST
// the exact §2.2 wire shape, classify the terminal, and say what a caller
// may do next.
//
//	intent-declare -gate http://127.0.0.1:8080 -seed run-42 -spec-hash <hex> \
//	    -scope per-actor [-key K | -run R -tool T -args '<canonical-json>'] \
//	    [-rule-hash H]
//
// Prints one line:
//
//	class=<CLASS> terminal=<T> reason=<R> same_key_retry_safe=<bool>
//
// Exit 0 iff the class is PROCEED or SHADOW_RECORDED; 1 otherwise; 2 usage.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/hossainpazooki/intent-plane/declarant"
)

func main() {
	gate := flag.String("gate", "", "gate origin, e.g. http://127.0.0.1:8080")
	seed := flag.String("seed", "", "episode seed (drives the deterministic intent id)")
	specHash := flag.String("spec-hash", "", "content address of the attested spec payload")
	scope := flag.String("scope", "", "idempotency scope (carried data; folded into a derived key)")
	key := flag.String("key", "", "explicit idempotency key (mutually exclusive with -run/-tool/-args)")
	run := flag.String("run", "", "agent run id (key derivation)")
	tool := flag.String("tool", "", "tool name (key derivation)")
	args := flag.String("args", "", "canonical action args (key derivation; caller-canonicalized bytes)")
	ruleHash := flag.String("rule-hash", "", "optional rule artifact hash passthrough")
	flag.Parse()

	if *gate == "" || *seed == "" || *specHash == "" {
		fmt.Fprintln(os.Stderr, "usage: intent-declare -gate URL -seed S -spec-hash H -scope SC [-key K | -run R -tool T -args A]")
		os.Exit(2)
	}
	k := *key
	if k == "" {
		if *run == "" || *tool == "" {
			fmt.Fprintln(os.Stderr, "intent-declare: need -key, or -run and -tool (with -args) to derive one")
			os.Exit(2)
		}
		k = declarant.DeriveKey(*scope, *run, *tool, []byte(*args))
	}

	c := &declarant.Client{BaseURL: *gate}
	res, err := c.Declare(context.Background(), declarant.Request{
		EpisodeSeed:      *seed,
		IdempotencyKey:   k,
		RuleArtifactHash: *ruleHash,
		IntentSpecHash:   *specHash,
		IdempotencyScope: *scope,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "intent-declare: %v\n", err)
		os.Exit(2)
	}
	terminal, reason := "", ""
	if res.Terminal != nil {
		terminal, reason = res.Terminal.Terminal, res.Terminal.Reason
	}
	fmt.Printf("class=%s terminal=%s reason=%s same_key_retry_safe=%v\n", res.Class, terminal, reason, res.SameKeyRetrySafe)
	if res.Class == declarant.Proceed || res.Class == declarant.ShadowRecorded {
		os.Exit(0)
	}
	os.Exit(1)
}
