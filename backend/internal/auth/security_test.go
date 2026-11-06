package auth

import (
	"bytes"
	"testing"
	"time"
)

func TestTOTPRejectsReplayAndOutOfWindow(t *testing.T) {
	secret := "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"
	code, err := TOTPCode(secret, 1)
	if err != nil || code != "287082" {
		t.Fatalf("RFC4226/6238 vector: %q %v", code, err)
	}
	counter, ok := VerifyTOTP(secret, code, time.Unix(59, 0), -1)
	if !ok || counter != 1 {
		t.Fatal("valid code rejected")
	}
	for _, tc := range []struct {
		name, code string
		now        int64
		last       int64
	}{{"replay", code, 59, 1}, {"expired", code, 151, -1}, {"length", "28708", 59, -1}, {"non-digit", "abcdef", 59, -1}} {
		t.Run(tc.name, func(t *testing.T) {
			if _, ok := VerifyTOTP(secret, tc.code, time.Unix(tc.now, 0), tc.last); ok {
				t.Fatal("invalid code accepted")
			}
		})
	}
}
func TestSecretEncryptionAuthenticatesContext(t *testing.T) {
	key := []byte("server-key")
	plain := []byte("sensitive TOTP or bot secret")
	sealed, err := Seal(key, plain, "user:a")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, plain) {
		t.Fatal("plaintext secret stored")
	}
	got, err := Open(key, sealed, "user:a")
	if err != nil || !bytes.Equal(got, plain) {
		t.Fatalf("round trip: %q %v", got, err)
	}
	if _, err := Open(key, sealed, "user:b"); err == nil {
		t.Fatal("wrong context accepted")
	}
	sealed[len(sealed)-1] ^= 1
	if _, err := Open(key, sealed, "user:a"); err == nil {
		t.Fatal("tampering accepted")
	}
}
