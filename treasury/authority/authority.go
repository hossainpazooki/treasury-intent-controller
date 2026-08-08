// Package authority holds every private-key operation of the plane: key
// generation and envelope signing. ONLY the application's control seat (treasury/control) may import it; the
// contractcheck key-possession boundary (TestKeyPossessionBoundary) pins that
// treasury/authoring and ALL of core/ + plane/ structurally cannot — "holds no keys" as a code-graph
// fact. The deployment-graph half of that claim (workload identity) remains
// ROADMAP R2 and is NOT claimed here.
//
// Key authority is TEST-GRADE until ADR-0009 lands: keys are files on disk,
// key_authority stamps say "test", and provenance keeps saying so.
package authority

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"

	"github.com/hossainpazooki/intent-plane/plane"
)

// KeyFile is the on-disk shape of a test-authority keypair.
type KeyFile struct {
	KeyID        string `json:"keyid"`
	Public       string `json:"public"`  // base64
	Private      string `json:"private"` // base64 — test authority only
	KeyAuthority string `json:"key_authority"`
}

// Keygen generates an ed25519 keypair and writes it to path. It returns the
// key file. Refuses to overwrite an existing key.
func Keygen(path string) (KeyFile, error) {
	if _, err := os.Stat(path); err == nil {
		return KeyFile{}, fmt.Errorf("authority: key file %s already exists", path)
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return KeyFile{}, err
	}
	kf := KeyFile{
		KeyID:        plane.KeyIDFor(pub),
		Public:       base64.StdEncoding.EncodeToString(pub),
		Private:      base64.StdEncoding.EncodeToString(priv),
		KeyAuthority: plane.KeyAuthorityTest,
	}
	raw, err := json.MarshalIndent(kf, "", "  ")
	if err != nil {
		return KeyFile{}, err
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return KeyFile{}, err
	}
	return kf, nil
}

// LoadKey reads a key file from path.
func LoadKey(path string) (KeyFile, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return KeyFile{}, err
	}
	var kf KeyFile
	if err := json.Unmarshal(raw, &kf); err != nil {
		return KeyFile{}, err
	}
	return kf, nil
}

// TrustRootJSON renders the single-key trust-root file for this key.
func (kf KeyFile) TrustRootJSON() ([]byte, error) {
	return json.MarshalIndent(map[string]any{"keys": map[string]string{kf.KeyID: kf.Public}}, "", "  ")
}

func (kf KeyFile) private() (ed25519.PrivateKey, error) {
	raw, err := base64.StdEncoding.DecodeString(kf.Private)
	if err != nil {
		return nil, err
	}
	if len(raw) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("authority: bad private key length %d", len(raw))
	}
	return ed25519.PrivateKey(raw), nil
}

// Sign wraps payload bytes in a signed envelope of the given payloadType. The
// signature covers PAE(payloadType, payload); the signature block carries the
// test key-authority stamp.
func (kf KeyFile) Sign(payloadType string, payload []byte) (plane.Envelope, error) {
	priv, err := kf.private()
	if err != nil {
		return plane.Envelope{}, err
	}
	sig := ed25519.Sign(priv, plane.PAE(payloadType, payload))
	return plane.Envelope{
		PayloadType: payloadType,
		Payload:     base64.StdEncoding.EncodeToString(payload),
		Signatures: []plane.Signature{{
			KeyID:        kf.KeyID,
			Sig:          base64.StdEncoding.EncodeToString(sig),
			KeyAuthority: kf.KeyAuthority,
		}},
	}, nil
}

// Attest signs a spec payload: the attester becomes the artifact's author of
// record, and the payload bytes it signs are byte-for-byte the bytes the gate
// will execute (the content address seals them).
func (kf KeyFile) Attest(payload []byte) (plane.Envelope, string, error) {
	env, err := kf.Sign(plane.PayloadType, payload)
	if err != nil {
		return plane.Envelope{}, "", err
	}
	return env, plane.SpecHash(payload), nil
}

// RevocationTombstone signs a revocation of the given spec hash.
func (kf KeyFile) RevocationTombstone(hash, ref string) (plane.Envelope, error) {
	payload, err := json.Marshal(plane.RevocationPayload{Revoke: hash, Ref: ref})
	if err != nil {
		return plane.Envelope{}, err
	}
	return kf.Sign(plane.RevocationPayloadType, payload)
}
