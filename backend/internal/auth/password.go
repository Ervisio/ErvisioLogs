// Package auth holds credential primitives: password hashing, session and API token handling,
// permissions and rate limiting. It has no database or HTTP dependencies.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Params are the Argon2id cost parameters (memory in KiB).
type Params struct {
	Memory  uint32
	Time    uint32
	Threads uint8
}

// DefaultParams follow the OWASP "m=64 MiB, t=3, p=2" recommendation range.
var DefaultParams = Params{Memory: 64 * 1024, Time: 3, Threads: 2}

const (
	keyLen = 32
	// Upper bounds applied to parameters read back from storage, so a corrupted or malicious
	// hash cannot make verification allocate unbounded memory.
	maxMemory  = 256 * 1024
	maxTime    = 10
	maxThreads = 16
)

// ErrBusy means no hashing slot could be obtained: the wait queue is full or the context ended.
var ErrBusy = errors.New("password hashing is saturated")

// waitersPerSlot sizes the default wait queue of NewHasher.
const waitersPerSlot = 8

// Hasher hashes and verifies passwords, limiting concurrent Argon2 runs (each uses Memory KiB) and
// the requests allowed to wait for one, so a burst of logins is refused instead of piling up.
type Hasher struct {
	params Params
	sem    chan struct{}
	admit  chan struct{} // running plus waiting
}

func NewHasher(p Params, maxConcurrent int) *Hasher {
	if maxConcurrent < 1 {
		maxConcurrent = 1
	}
	return NewHasherWithQueue(p, maxConcurrent, maxConcurrent*waitersPerSlot)
}

// NewHasherWithQueue is NewHasher with an explicit number of requests allowed to wait.
func NewHasherWithQueue(p Params, maxConcurrent, maxWaiting int) *Hasher {
	if maxConcurrent < 1 {
		maxConcurrent = 1
	}
	if maxWaiting < 0 {
		maxWaiting = 0
	}
	return &Hasher{params: p, sem: make(chan struct{}, maxConcurrent), admit: make(chan struct{}, maxConcurrent+maxWaiting)}
}

// Acquire takes a hashing slot, waiting at most until ctx ends. It returns ErrBusy when the queue is
// full or ctx has ended. Call the returned function to release the slot.
func (h *Hasher) Acquire(ctx context.Context) (func(), error) {
	if ctx.Err() != nil {
		return nil, ErrBusy
	}
	select {
	case h.admit <- struct{}{}:
	default:
		return nil, ErrBusy
	}
	select {
	case h.sem <- struct{}{}:
		return func() { <-h.sem; <-h.admit }, nil
	case <-ctx.Done():
		<-h.admit
		return nil, ErrBusy
	}
}

// Hash returns a PHC-formatted argon2id hash with a fresh random salt.
func (h *Hasher) Hash(ctx context.Context, password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	release, err := h.Acquire(ctx)
	if err != nil {
		return "", err
	}
	defer release()
	key := argon2.IDKey([]byte(password), salt, h.params.Time, h.params.Memory, h.params.Threads, keyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version,
		h.params.Memory, h.params.Time, h.params.Threads,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

var ErrMismatch = errors.New("password mismatch")

// Verify checks password against an encoded hash in constant time.
func (h *Hasher) Verify(ctx context.Context, password, encoded string) error {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return errors.New("unsupported hash format")
	}
	var v int
	var p Params
	if _, err := fmt.Sscanf(parts[2], "v=%d", &v); err != nil || v != argon2.Version {
		return errors.New("unsupported argon2 version")
	}
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.Memory, &p.Time, &p.Threads); err != nil {
		return errors.New("bad argon2 parameters")
	}
	if p.Memory == 0 || p.Memory > maxMemory || p.Time == 0 || p.Time > maxTime || p.Threads == 0 || p.Threads > maxThreads {
		return errors.New("argon2 parameters out of range")
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return err
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(want) == 0 {
		return errors.New("bad hash")
	}
	release, err := h.Acquire(ctx)
	if err != nil {
		return err
	}
	defer release()
	got := argon2.IDKey([]byte(password), salt, p.Time, p.Memory, p.Threads, uint32(len(want)))
	if subtle.ConstantTimeCompare(got, want) != 1 {
		return ErrMismatch
	}
	return nil
}

// Burn spends the time of one verification, so unknown accounts are not distinguishable by latency.
func (h *Hasher) Burn(ctx context.Context, password string) error {
	release, err := h.Acquire(ctx)
	if err != nil {
		return err
	}
	defer release()
	salt := make([]byte, 16)
	argon2.IDKey([]byte(password), salt, h.params.Time, h.params.Memory, h.params.Threads, keyLen)
	return nil
}

// ValidatePassword enforces the password policy (length over composition rules, as in NIST 800-63B).
func ValidatePassword(pw, email, username string) error {
	switch {
	case len([]rune(pw)) < 10:
		return errors.New("password must be at least 10 characters")
	case len(pw) > 128:
		return errors.New("password must be at most 128 bytes")
	case strings.EqualFold(pw, email) || strings.EqualFold(pw, username):
		return errors.New("password must not equal the email or username")
	}
	return nil
}
