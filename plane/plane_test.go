package plane_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/hossainpazooki/intent-plane/plane"
)

// testKeyFile is a TEST-LOCAL signing seat: the plane is verification-only
// and its tests must not import an application's authority package (the SDK
// verifies what applications sign), so envelope signing for fixtures lives
// here, test authority by construction.
type testKeyFile struct {
	KeyID   string
	private ed25519.PrivateKey
	public  ed25519.PublicKey
}

func (kf testKeyFile) TrustRootJSON() ([]byte, error) {
	pub := base64.StdEncoding.EncodeToString(kf.public)
	return json.Marshal(map[string]any{"keys": map[string]string{kf.KeyID: pub}})
}

func (kf testKeyFile) sign(payloadType string, payload []byte) (plane.Envelope, error) {
	sig := ed25519.Sign(kf.private, plane.PAE(payloadType, payload))
	return plane.Envelope{
		PayloadType: payloadType,
		Payload:     base64.StdEncoding.EncodeToString(payload),
		Signatures: []plane.Signature{{
			KeyID:        kf.KeyID,
			Sig:          base64.StdEncoding.EncodeToString(sig),
			KeyAuthority: plane.KeyAuthorityTest,
		}},
	}, nil
}

func (kf testKeyFile) Attest(payload []byte) (plane.Envelope, string, error) {
	env, err := kf.sign(plane.PayloadType, payload)
	return env, plane.SpecHash(payload), err
}

func (kf testKeyFile) RevocationTombstone(hash, ref string) (plane.Envelope, error) {
	payload, err := json.Marshal(plane.RevocationPayload{Revoke: hash, Ref: ref})
	if err != nil {
		return plane.Envelope{}, err
	}
	return kf.sign(plane.RevocationPayloadType, payload)
}

func testKey(t *testing.T) (testKeyFile, plane.TrustRoot) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("keygen: %v", err)
	}
	kf := testKeyFile{KeyID: plane.KeyIDFor(pub), private: priv, public: pub}
	rootJSON, err := kf.TrustRootJSON()
	if err != nil {
		t.Fatalf("trust root: %v", err)
	}
	root, err := plane.ParseTrustRoot(rootJSON)
	if err != nil {
		t.Fatalf("parse trust root: %v", err)
	}
	return kf, root
}

func payloadFor(t *testing.T, p plane.SpecPayload) []byte {
	t.Helper()
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func enforceSpec() plane.SpecPayload {
	return plane.SpecPayload{
		SpecVersion: 1,
		ActionClass: "sample-action",
		Posture:     plane.PostureEnforce,
		Criteria: []plane.CriterionSpec{
			{Name: "alpha", Threshold: 1, Volatility: "stable"},
			{Name: "beta", Threshold: 2, Volatility: "volatile"},
		},
	}
}

func TestAttestVerifyRoundTrip(t *testing.T) {
	kf, root := testKey(t)
	payload := payloadFor(t, enforceSpec())
	env, hash, err := kf.Attest(payload)
	if err != nil {
		t.Fatalf("attest: %v", err)
	}
	if hash != plane.SpecHash(payload) {
		t.Fatalf("attest hash %s != content address %s", hash, plane.SpecHash(payload))
	}
	got, keyid, err := env.Verify(root, plane.PayloadType)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if string(got) != string(payload) {
		t.Fatal("verified payload is not byte-identical to the attested payload")
	}
	if keyid != kf.KeyID {
		t.Fatalf("keyid %s != %s", keyid, kf.KeyID)
	}
	if env.Signatures[0].KeyAuthority != plane.KeyAuthorityTest {
		t.Fatal("envelope must carry the test key-authority stamp until ADR-0009 lands")
	}
}

// One flipped payload byte must refuse: byte-for-byte is the claim.
func TestTamperedPayloadRefuses(t *testing.T) {
	kf, root := testKey(t)
	payload := payloadFor(t, enforceSpec())
	env, _, err := kf.Attest(payload)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := base64.StdEncoding.DecodeString(env.Payload)
	raw[0] ^= 0x01
	env.Payload = base64.StdEncoding.EncodeToString(raw)
	if _, _, err := env.Verify(root, plane.PayloadType); err == nil {
		t.Fatal("tampered payload verified — byte-for-byte is broken")
	}
}

func TestUnknownKeyAndNoSignaturesRefuse(t *testing.T) {
	kf, _ := testKey(t)
	_, otherRoot := testKey(t) // a root that does not know kf
	payload := payloadFor(t, enforceSpec())
	env, _, err := kf.Attest(payload)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := env.Verify(otherRoot, plane.PayloadType); err == nil {
		t.Fatal("unknown keyid verified")
	}
	env.Signatures = nil
	_, root := testKey(t)
	if _, _, err := env.Verify(root, plane.PayloadType); err == nil {
		t.Fatal("zero signatures verified — vacuous pass")
	}
}

func TestStorePublishResolveStoreSource(t *testing.T) {
	kf, root := testKey(t)
	store, err := plane.OpenStore(t.TempDir(), root)
	if err != nil {
		t.Fatal(err)
	}
	payload := payloadFor(t, enforceSpec())
	env, hash, _ := kf.Attest(payload)
	pub, err := store.Publish(env)
	if err != nil || pub != hash {
		t.Fatalf("publish: %v (%s vs %s)", err, pub, hash)
	}
	res, err := store.Resolve(hash, nil)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if res.Source != "store" || len(res.Payload.Criteria) != 2 {
		t.Fatalf("resolution %+v", res)
	}
}

func TestResolveUnattestedRefuses(t *testing.T) {
	_, root := testKey(t)
	store, _ := plane.OpenStore(t.TempDir(), root)
	// Unknown hash, no wire envelope.
	if _, err := store.Resolve(plane.SpecHash([]byte("never-published")), nil); err != plane.ErrUnattested {
		t.Fatalf("want ErrUnattested, got %v", err)
	}
	// Malformed hash string.
	if _, err := store.Resolve("not-a-hash", nil); err != plane.ErrUnattested {
		t.Fatalf("want ErrUnattested for malformed hash, got %v", err)
	}
}

// The hybrid rule: a wire envelope resolves ONLY if verified AND pinned.
func TestHybridWireEnvelopeRules(t *testing.T) {
	kf, root := testKey(t)
	store, _ := plane.OpenStore(t.TempDir(), root)
	payload := payloadFor(t, enforceSpec())
	env, hash, _ := kf.Attest(payload)
	wire, _ := json.Marshal(env)

	// Verified but NOT pinned -> unattested (store is authoritative).
	if _, err := store.Resolve(hash, wire); err != plane.ErrUnattested {
		t.Fatalf("unpinned wire envelope must refuse, got %v", err)
	}

	// Publish (pins), then delete the stored envelope to force the wire path.
	if _, err := store.Publish(env); err != nil {
		t.Fatal(err)
	}
	// Simulate a store that has the pin but not the envelope body by resolving
	// a copy opened over the same dir after removing the env file.
	// (Removing via os is fine in-test; the pin remains.)
	plane.RemoveEnvFileForTest(t, store, hash)
	res, err := store.Resolve(hash, wire)
	if err != nil {
		t.Fatalf("pinned+verified wire envelope must resolve: %v", err)
	}
	if res.Source != "wire" {
		t.Fatalf("source %s, want wire", res.Source)
	}

	// A wire envelope signed by an unknown key refuses even when pinned.
	stranger, _ := testKey(t)
	sEnv, _, _ := stranger.Attest(payload)
	sWire, _ := json.Marshal(sEnv)
	if _, err := store.Resolve(hash, sWire); err != plane.ErrUnattested {
		t.Fatalf("stranger-signed wire envelope must refuse, got %v", err)
	}
}

func TestRevocationTombstone(t *testing.T) {
	kf, root := testKey(t)
	store, _ := plane.OpenStore(t.TempDir(), root)
	payload := payloadFor(t, enforceSpec())
	env, hash, _ := kf.Attest(payload)
	if _, err := store.Publish(env); err != nil {
		t.Fatal(err)
	}
	tomb, err := kf.RevocationTombstone(hash, "policy-superseded")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Revoke(tomb); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	ref, ok := store.RevokedRef(hash)
	if !ok || ref != "policy-superseded" {
		t.Fatalf("revoked ref %q ok=%v", ref, ok)
	}
	if _, err := store.Resolve(hash, nil); err == nil {
		t.Fatal("revoked spec resolved")
	} else if re, isRe := err.(plane.RevokedError); !isRe || re.Ref != "policy-superseded" {
		t.Fatalf("want RevokedError{policy-superseded}, got %v", err)
	}
}

// An UNSIGNED (stranger-signed) tombstone must NOT revoke: revocation is an
// authority act too.
func TestForgedTombstoneDoesNotRevoke(t *testing.T) {
	kf, root := testKey(t)
	store, _ := plane.OpenStore(t.TempDir(), root)
	payload := payloadFor(t, enforceSpec())
	env, hash, _ := kf.Attest(payload)
	if _, err := store.Publish(env); err != nil {
		t.Fatal(err)
	}
	stranger, _ := testKey(t)
	forged, _ := stranger.RevocationTombstone(hash, "forged")
	if _, err := store.Revoke(forged); err == nil {
		t.Fatal("store accepted a stranger-signed tombstone")
	}
	if _, ok := store.RevokedRef(hash); ok {
		t.Fatal("forged tombstone revoked the spec")
	}
}
