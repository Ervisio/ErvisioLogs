package api

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"testing"

	"github.com/Ervisio/ErvisioLogs/backend/internal/store"
	"github.com/fxamacker/cbor/v2"
	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"
)

type virtualPasskey struct {
	key     *ecdsa.PrivateKey
	id      []byte
	counter uint32
}

func newVirtualPasskey(t *testing.T) *virtualPasskey {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return &virtualPasskey{key: key, id: []byte("ervisio-test-credential")}
}
func encodeURL(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }
func (v *virtualPasskey) create(t *testing.T, challenge, origin string) map[string]any {
	t.Helper()
	client, err := json.Marshal(map[string]string{"type": "webauthn.create", "challenge": challenge, "origin": origin})
	if err != nil {
		t.Fatal(err)
	}
	x, y := make([]byte, 32), make([]byte, 32)
	v.key.X.FillBytes(x)
	v.key.Y.FillBytes(y)
	cose, err := cbor.Marshal(map[int]any{1: 2, 3: -7, -1: 1, -2: x, -3: y})
	if err != nil {
		t.Fatal(err)
	}
	rp := sha256.Sum256([]byte("localhost"))
	data := append(rp[:], byte(0x45))
	data = append(data, 0, 0, 0, 0)
	data = append(data, make([]byte, 16)...)
	var length [2]byte
	binary.BigEndian.PutUint16(length[:], uint16(len(v.id)))
	data = append(data, length[:]...)
	data = append(data, v.id...)
	data = append(data, cose...)
	attestation, err := cbor.Marshal(map[string]any{"fmt": "none", "authData": data, "attStmt": map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	return map[string]any{"id": encodeURL(v.id), "rawId": encodeURL(v.id), "type": "public-key", "authenticatorAttachment": "cross-platform", "clientExtensionResults": map[string]any{}, "response": map[string]any{"clientDataJSON": encodeURL(client), "attestationObject": encodeURL(attestation), "transports": []string{"usb"}}}
}
func (v *virtualPasskey) assertion(t *testing.T, challenge, origin string) map[string]any {
	t.Helper()
	v.counter++
	client, err := json.Marshal(map[string]string{"type": "webauthn.get", "challenge": challenge, "origin": origin})
	if err != nil {
		t.Fatal(err)
	}
	rp := sha256.Sum256([]byte("localhost"))
	data := append(rp[:], byte(0x05))
	var count [4]byte
	binary.BigEndian.PutUint32(count[:], v.counter)
	data = append(data, count[:]...)
	digest := sha256.Sum256(client)
	signed := append(append([]byte(nil), data...), digest[:]...)
	sum := sha256.Sum256(signed)
	sig, err := ecdsa.SignASN1(rand.Reader, v.key, sum[:])
	if err != nil {
		t.Fatal(err)
	}
	return map[string]any{"id": encodeURL(v.id), "rawId": encodeURL(v.id), "type": "public-key", "authenticatorAttachment": "cross-platform", "clientExtensionResults": map[string]any{}, "response": map[string]any{"clientDataJSON": encodeURL(client), "authenticatorData": encodeURL(data), "signature": encodeURL(sig)}}
}

func TestPasskeyProtocolVerifiesOriginRPChallengeAndSignature(t *testing.T) {
	a := &API{origin: "http://localhost"}
	wa, err := a.webAuthn()
	if err != nil {
		t.Fatal(err)
	}
	u := passkeyUser{User: store.User{ID: uuid.New(), Username: "user"}}
	registration, session, err := wa.BeginRegistration(u)
	if err != nil {
		t.Fatal(err)
	}
	v := newVirtualPasskey(t)
	credentialJSON, err := json.Marshal(v.create(t, encodeURL(registration.Response.Challenge), "http://localhost"))
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := protocol.ParseCredentialCreationResponseBytes(credentialJSON)
	if err != nil {
		t.Fatal(err)
	}
	credential, err := wa.CreateCredential(u, *session, parsed)
	if err != nil {
		t.Fatal(err)
	}
	u.credentials = []webauthn.Credential{*credential}
	for _, origin := range []string{"https://attacker.example", "http://localhost"} {
		assertion, login, err := wa.BeginLogin(u, webauthn.WithUserVerification(protocol.VerificationRequired))
		if err != nil {
			t.Fatal(err)
		}
		body, err := json.Marshal(v.assertion(t, encodeURL(assertion.Response.Challenge), origin))
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := protocol.ParseCredentialRequestResponseBytes(body)
		if err != nil {
			t.Fatal(err)
		}
		_, err = wa.ValidateLogin(u, *login, parsed)
		if origin == "http://localhost" && err != nil {
			t.Fatalf("valid assertion rejected: %v", err)
		}
		if origin != "http://localhost" && err == nil {
			t.Fatal("foreign origin accepted")
		}
		login.Challenge = "different-challenge"
		if _, err := wa.ValidateLogin(u, *login, parsed); err == nil {
			t.Fatal("wrong challenge accepted")
		}
	}
}
