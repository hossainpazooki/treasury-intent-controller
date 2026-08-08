// Command control is the CONTROL role of the plane: attest, publish, revoke,
// promote. It is the only production code allowed to import treasury/authority
// (TestKeyPossessionBoundary pins this): the sole holder of signing
// operations. The authoring role drafts; nothing becomes enforceable until
// this role's designated key attests it — maker-checker discipline applied to
// what automated agents act on.
//
// Key authority is TEST-GRADE until ADR-0009 lands; every artifact this tool
// signs carries the "test" key_authority stamp.
//
// Subcommands:
//
//	keygen  -key <path>                          generate an attester keypair (refuses overwrite)
//	root    -key <path> -out <path>              write the trust-root file for a key
//	attest  -key <path> -draft <path> -out <p>   sign a draft spec payload into an envelope
//	publish -root <path> -store <dir> -env <p>   verify + publish an envelope into the spec store
//	revoke  -key <path> -root <path> -store <d>  sign + install a revocation tombstone
//	        -hash <spec-hash> -ref <reason>
//	promote -key <path> -draft <path> -out <p>   re-attest a draft with enforcement_posture
//	                                             flipped shadow->enforce (a NEW artifact with a
//	                                             NEW hash: promotion is an authority act)
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/hossainpazooki/intent-plane/plane"
	"github.com/hossainpazooki/intent-plane/treasury/authority"
)

func fail(err error) {
	fmt.Fprintln(os.Stderr, "control:", err)
	os.Exit(1)
}

// args parses "-k v" pairs after the subcommand.
func args(argv []string) map[string]string {
	m := map[string]string{}
	for i := 0; i+1 < len(argv); i += 2 {
		m[argv[i]] = argv[i+1]
	}
	return m
}

func loadRoot(path string) plane.TrustRoot {
	raw, err := os.ReadFile(path)
	if err != nil {
		fail(err)
	}
	root, err := plane.ParseTrustRoot(raw)
	if err != nil {
		fail(err)
	}
	return root
}

func writeJSON(path string, v any) {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		fail(err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		fail(err)
	}
}

func main() {
	if len(os.Args) < 2 {
		fail(fmt.Errorf("usage: control <keygen|root|attest|publish|revoke|promote> ..."))
	}
	a := args(os.Args[2:])
	switch os.Args[1] {

	case "keygen":
		kf, err := authority.Keygen(a["-key"])
		if err != nil {
			fail(err)
		}
		fmt.Printf("keyid %s (key_authority: %s)\n", kf.KeyID, kf.KeyAuthority)

	case "root":
		kf, err := authority.LoadKey(a["-key"])
		if err != nil {
			fail(err)
		}
		raw, err := kf.TrustRootJSON()
		if err != nil {
			fail(err)
		}
		if err := os.WriteFile(a["-out"], raw, 0o644); err != nil {
			fail(err)
		}
		fmt.Printf("trust root for %s -> %s\n", kf.KeyID, a["-out"])

	case "attest", "promote":
		kf, err := authority.LoadKey(a["-key"])
		if err != nil {
			fail(err)
		}
		draft, err := os.ReadFile(a["-draft"])
		if err != nil {
			fail(err)
		}
		payload, err := plane.ParseSpecPayload(draft)
		if err != nil {
			fail(err)
		}
		if os.Args[1] == "promote" {
			if payload.Posture != plane.PostureShadow {
				fail(fmt.Errorf("promote: draft posture is %q, not shadow", payload.Posture))
			}
			payload.Posture = plane.PostureEnforce
			// Promotion produces NEW payload bytes and hence a NEW hash: the
			// shadow artifact stays what it was; enforcement is a fresh
			// authority act, never an edit in place.
			b, err := json.Marshal(payload)
			if err != nil {
				fail(err)
			}
			draft = b
		}
		env, hash, err := kf.Attest(draft)
		if err != nil {
			fail(err)
		}
		writeJSON(a["-out"], env)
		fmt.Printf("attested %s by %s -> %s\n", hash, kf.KeyID, a["-out"])

	case "publish":
		store, err := plane.OpenStore(a["-store"], loadRoot(a["-root"]))
		if err != nil {
			fail(err)
		}
		raw, err := os.ReadFile(a["-env"])
		if err != nil {
			fail(err)
		}
		var env plane.Envelope
		if err := json.Unmarshal(raw, &env); err != nil {
			fail(err)
		}
		hash, err := store.Publish(env)
		if err != nil {
			fail(err)
		}
		fmt.Printf("published + pinned %s\n", hash)

	case "revoke":
		kf, err := authority.LoadKey(a["-key"])
		if err != nil {
			fail(err)
		}
		store, err := plane.OpenStore(a["-store"], loadRoot(a["-root"]))
		if err != nil {
			fail(err)
		}
		tomb, err := kf.RevocationTombstone(a["-hash"], a["-ref"])
		if err != nil {
			fail(err)
		}
		rp, err := store.Revoke(tomb)
		if err != nil {
			fail(err)
		}
		fmt.Printf("revoked %s (ref %s)\n", rp.Revoke, rp.Ref)

	default:
		fail(fmt.Errorf("unknown subcommand %q", os.Args[1]))
	}
}
