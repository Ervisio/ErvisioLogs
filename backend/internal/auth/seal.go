package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"errors"
)

func secretCipher(secret []byte) (cipher.AEAD, error) {
	key := sha256.Sum256(append([]byte("ervisio-logs:encrypted-secrets:"), secret...))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
func Seal(secret, plaintext []byte, context string) ([]byte, error) {
	aead, err := secretCipher(secret)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return aead.Seal(nonce, nonce, plaintext, []byte(context)), nil
}
func Open(secret, ciphertext []byte, context string) ([]byte, error) {
	aead, err := secretCipher(secret)
	if err != nil {
		return nil, err
	}
	if len(ciphertext) < aead.NonceSize() {
		return nil, errors.New("invalid encrypted secret")
	}
	return aead.Open(nil, ciphertext[:aead.NonceSize()], ciphertext[aead.NonceSize():], []byte(context))
}
