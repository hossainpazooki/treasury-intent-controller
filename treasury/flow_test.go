// The chassis flow test: the maker-checker chain is the unit.
//
// One linear chain — author → keygen → root → attest → publish → promote →
// revoke — drives the REAL seat binaries (built by TestMain, exec'd as the
// operator would run them; no production code is imported from the two
// `package main` seats and none was refactored for testability). Each
// refusal edge is exercised at the point in the chain where it naturally
// occurs: keygen refuses overwrite right after keygen, wrong-root publish
// right after publish, double-promotion right after promote.
//
// What the chain pins, in CONTRACT terms:
//   - authoring routes every provision (criterion / human-judgment / named
//     unknown), pins each to the sha256 of its exact passage, and defaults
//     new drafts to SHADOW (§1.1 authoring seat).
//   - the attested bytes ARE the executed bytes: the envelope payload is
//     byte-identical to the draft file, its hash is the content address, and
//     store resolution returns the same parsed payload (§2.6).
//   - attestation does not launder anything: the human-judgment entry
//     survives to resolution.
//   - promotion is a NEW authority act: new bytes, new hash, only the
//     posture differs, the shadow artifact is untouched (§ R3 / control
//     promote).
//   - revocation kills exactly the revoked artifact: the promoted sibling
//     still resolves (§2.6 tombstones).
//
// These are pins over existing behavior; their non-vacuity is proven by
// plant-red runs (inject the defect into a temp copy, watch this test fail),
// recorded in the session brief — green-on-first-run alone proves nothing.
package treasury

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/hossainpazooki/intent-plane/plane"
)

var (
	authoringBin string
	controlBin   string
)

// TestMain builds the two seat binaries once; every leg execs them as built.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "treasury-flow-bin")
	if err != nil {
		panic(err)
	}
	exe := ""
	if runtime.GOOS == "windows" {
		exe = ".exe"
	}
	authoringBin = filepath.Join(dir, "authoring"+exe)
	controlBin = filepath.Join(dir, "control"+exe)
	for bin, pkg := range map[string]string{authoringBin: "./authoring", controlBin: "./control"} {
		out, err := exec.Command("go", "build", "-o", bin, pkg).CombinedOutput()
		if err != nil {
			panic("build " + pkg + ": " + err.Error() + "\n" + string(out))
		}
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// run execs a seat binary and returns combined output; ok reports exit 0.
func run(t *testing.T, bin string, args ...string) (out string, ok bool) {
	t.Helper()
	b, err := exec.Command(bin, args...).CombinedOutput()
	return string(b), err == nil
}

func mustRun(t *testing.T, bin string, args ...string) string {
	t.Helper()
	out, ok := run(t, bin, args...)
	if !ok {
		t.Fatalf("%s %s failed:\n%s", filepath.Base(bin), strings.Join(args, " "), out)
	}
	return out
}

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func sha(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// verifyEnvelope reads an envelope file, verifies it against the trust root,
// and returns the raw payload bytes it signs.
func verifyEnvelope(t *testing.T, envPath string, root plane.TrustRoot) []byte {
	t.Helper()
	raw, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatal(err)
	}
	var env plane.Envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("envelope %s: %v", envPath, err)
	}
	payload, _, err := env.Verify(root, plane.PayloadType)
	if err != nil {
		t.Fatalf("envelope %s does not verify: %v", envPath, err)
	}
	return payload
}

func TestMakerCheckerFlow(t *testing.T) {
	dir := t.TempDir()
	p := func(name string) string { return filepath.Join(dir, name) }

	// ---- author: three provisions, one of each routing kind ----------------
	const (
		passCriterion = "the balance shall not fall below the floor"
		passJudgment  = "transfers requiring officer discretion are reviewed"
		passUnmapped  = "records are retained per the applicable schedule"
	)
	policy := `{"action_class":"flow.test","provisions":[
	 {"id":"p1","passage":"` + passCriterion + `","criterion":{"name":"flow_ok","threshold":1.5,"volatility":"stable"}},
	 {"id":"p2","passage":"` + passJudgment + `","judgment":{"name":"manual_review"}},
	 {"id":"p3","passage":"` + passUnmapped + `"}]}`
	writeFile(t, p("policy.json"), []byte(policy))
	mustRun(t, authoringBin, "draft", "-source", p("policy.json"), "-out", p("draft.json"))

	draftBytes, err := os.ReadFile(p("draft.json"))
	if err != nil {
		t.Fatal(err)
	}
	draft, err := plane.ParseSpecPayload(draftBytes)
	if err != nil {
		t.Fatalf("draft does not parse: %v", err)
	}
	if len(draft.Criteria) != 1 || draft.Criteria[0].Name != "flow_ok" {
		t.Fatalf("criterion not routed: %+v", draft.Criteria)
	}
	if len(draft.HumanJudgment) != 1 || draft.HumanJudgment[0].Name != "manual_review" {
		t.Fatalf("judgment not routed: %+v", draft.HumanJudgment)
	}
	if len(draft.NamedUnknowns) != 1 || draft.NamedUnknowns[0].ProvisionID != "p3" {
		t.Fatalf("unmapped provision not surfaced as a named unknown: %+v", draft.NamedUnknowns)
	}
	if len(draft.SourcePins) != 1 || draft.SourcePins[0].PassageSHA256 != sha(passCriterion) {
		t.Fatalf("criterion pin is not the sha256 of its exact passage: %+v", draft.SourcePins)
	}
	if draft.HumanJudgment[0].PassageSHA256 != sha(passJudgment) ||
		draft.NamedUnknowns[0].PassageSHA256 != sha(passUnmapped) {
		t.Fatal("judgment/unknown pins are not the sha256 of their exact passages")
	}
	if draft.Posture != plane.PostureShadow {
		t.Fatalf("new draft posture = %q, want shadow (enforcement is the attester's promotion, never the author's default)", draft.Posture)
	}

	// ---- keygen + trust root ------------------------------------------------
	mustRun(t, controlBin, "keygen", "-key", p("key.json"))
	if out, ok := run(t, controlBin, "keygen", "-key", p("key.json")); ok {
		t.Fatalf("second keygen over the same file succeeded — overwrite refusal is gone:\n%s", out)
	}
	mustRun(t, controlBin, "root", "-key", p("key.json"), "-out", p("root.json"))
	rootRaw, err := os.ReadFile(p("root.json"))
	if err != nil {
		t.Fatal(err)
	}
	root, err := plane.ParseTrustRoot(rootRaw)
	if err != nil {
		t.Fatalf("trust root does not parse: %v", err)
	}

	// ---- attest: the signed bytes ARE the draft bytes ----------------------
	attestOut := mustRun(t, controlBin, "attest", "-key", p("key.json"), "-draft", p("draft.json"), "-out", p("env.json"))
	hash := plane.SpecHash(draftBytes)
	if !strings.Contains(attestOut, hash) {
		t.Fatalf("attest output does not carry the draft's content address %s:\n%s", hash, attestOut)
	}
	payload := verifyEnvelope(t, p("env.json"), root)
	if string(payload) != string(draftBytes) {
		t.Fatal("attested payload is not byte-identical to the draft file — attested bytes must be the executed bytes")
	}

	// ---- publish + resolve --------------------------------------------------
	mustRun(t, controlBin, "publish", "-root", p("root.json"), "-store", p("store"), "-env", p("env.json"))
	store, err := plane.OpenStore(p("store"), root)
	if err != nil {
		t.Fatal(err)
	}
	if !store.Pinned(hash) {
		t.Fatal("published spec is not pinned in the store")
	}
	res, err := store.Resolve(hash, nil)
	if err != nil {
		t.Fatalf("published spec does not resolve: %v", err)
	}
	if res.Source != "store" {
		t.Fatalf("resolution source = %q, want store", res.Source)
	}
	if !reflect.DeepEqual(res.Payload, draft) {
		t.Fatalf("resolved payload differs from the authored draft:\n got %+v\nwant %+v", res.Payload, draft)
	}
	if len(res.Payload.HumanJudgment) != 1 {
		t.Fatal("human-judgment entry did not survive attest→publish→resolve — attestation must not launder it")
	}

	// Refusal edge: an envelope signed by a key outside the trust root never
	// enters a store. (Second key, second root, publish the FIRST envelope.)
	mustRun(t, controlBin, "keygen", "-key", p("key2.json"))
	mustRun(t, controlBin, "root", "-key", p("key2.json"), "-out", p("root2.json"))
	if out, ok := run(t, controlBin, "publish", "-root", p("root2.json"), "-store", p("store2"), "-env", p("env.json")); ok {
		t.Fatalf("publish under a foreign trust root succeeded — fail-closed store entry is gone:\n%s", out)
	}

	// ---- promote: a NEW authority act, never an edit in place ---------------
	storeFileBefore, err := os.ReadFile(filepath.Join(p("store"), hash+".env.json"))
	if err != nil {
		t.Fatal(err)
	}
	mustRun(t, controlBin, "promote", "-key", p("key.json"), "-draft", p("draft.json"), "-out", p("env2.json"))
	promoted := verifyEnvelope(t, p("env2.json"), root)
	hash2 := plane.SpecHash(promoted)
	if hash2 == hash {
		t.Fatal("promotion did not change the content address — it must produce a NEW artifact")
	}
	promotedPayload, err := plane.ParseSpecPayload(promoted)
	if err != nil {
		t.Fatal(err)
	}
	if promotedPayload.Posture != plane.PostureEnforce {
		t.Fatalf("promoted posture = %q, want enforce", promotedPayload.Posture)
	}
	shadowTwin := promotedPayload
	shadowTwin.Posture = plane.PostureShadow
	if !reflect.DeepEqual(shadowTwin, draft) {
		t.Fatalf("promotion changed something besides posture:\n got %+v\nwant %+v", promotedPayload, draft)
	}
	mustRun(t, controlBin, "publish", "-root", p("root.json"), "-store", p("store"), "-env", p("env2.json"))
	storeFileAfter, err := os.ReadFile(filepath.Join(p("store"), hash+".env.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(storeFileBefore) != string(storeFileAfter) {
		t.Fatal("promotion touched the shadow artifact's stored bytes — the shadow artifact stays what it was")
	}

	// Refusal edge: promoting an already-enforce payload is refused.
	writeFile(t, p("draft-enforce.json"), promoted)
	if out, ok := run(t, controlBin, "promote", "-key", p("key.json"), "-draft", p("draft-enforce.json"), "-out", p("env3.json")); ok {
		t.Fatalf("promote of a non-shadow draft succeeded:\n%s", out)
	}

	// ---- revoke: kills exactly the revoked artifact --------------------------
	mustRun(t, controlBin, "revoke", "-key", p("key.json"), "-root", p("root.json"), "-store", p("store"),
		"-hash", hash, "-ref", "flow-test")
	if ref, ok := store.RevokedRef(hash); !ok || ref != "flow-test" {
		t.Fatalf("revocation tombstone not effective: ref=%q ok=%v", ref, ok)
	}
	if _, err := store.Resolve(hash, nil); err == nil {
		t.Fatal("revoked spec still resolves")
	} else if re, isRevoked := err.(plane.RevokedError); !isRevoked || re.Ref != "flow-test" {
		t.Fatalf("revoked spec resolution error = %v, want RevokedError{flow-test}", err)
	}
	if _, err := store.Resolve(hash2, nil); err != nil {
		t.Fatalf("promoted sibling stopped resolving after revocation of the shadow artifact: %v", err)
	}
}

// TestAuthoringRefusals pins the chassis's fail-closed edges: a value with no
// source cannot be pinned; a provision cannot be both quantified and
// deliberately-unquantified; posture vocabulary is closed.
func TestAuthoringRefusals(t *testing.T) {
	dir := t.TempDir()
	p := func(name string) string { return filepath.Join(dir, name) }
	cases := []struct {
		name, policy string
		posture      []string
	}{
		{"empty passage", `{"action_class":"x","provisions":[{"id":"p1","passage":"","criterion":{"name":"c","threshold":1,"volatility":"stable"}}]}`, nil},
		{"criterion and judgment both", `{"action_class":"x","provisions":[{"id":"p1","passage":"text","criterion":{"name":"c","threshold":1,"volatility":"stable"},"judgment":{"name":"j"}}]}`, nil},
		{"unknown source field", `{"action_class":"x","provisions":[],"extra":true}`, nil},
		{"invalid posture", `{"action_class":"x","provisions":[{"id":"p1","passage":"text"}]}`, []string{"-posture", "audit"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := p("policy.json")
			writeFile(t, src, []byte(tc.policy))
			args := append([]string{"draft", "-source", src, "-out", p("draft.json")}, tc.posture...)
			if out, ok := run(t, authoringBin, args...); ok {
				t.Fatalf("authoring accepted a %s:\n%s", tc.name, out)
			}
		})
	}
}
