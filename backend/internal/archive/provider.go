// Package archive moves hot events into immutable Parquet segments on a remote provider, keeps the
// segment catalog, reads segments back for search and enforces retention (ADR 0008).
package archive

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"
)

var (
	// ErrNotExist is returned by Stat and Get for a missing object.
	ErrNotExist = errors.New("object does not exist")
	// ErrVerify means the stored object differs from what was uploaded.
	ErrVerify = errors.New("remote object failed verification")
)

// Capabilities describe what a provider guarantees, so callers do not assume more.
type Capabilities struct {
	// ServerChecksum: Stat returns a SHA-256 computed by the server.
	ServerChecksum bool `json:"server_checksum"`
	// AtomicRename: Promote is a single atomic rename.
	AtomicRename bool `json:"atomic_rename"`
	// RangeReads: the provider can read part of an object. The reader does not rely on it yet.
	RangeReads bool `json:"range_reads"`
}

// ObjectInfo is what Stat and List report. SHA256 is nil when the provider has no server checksum.
type ObjectInfo struct {
	Key     string
	Size    int64
	SHA256  []byte
	ModTime time.Time
}

// Provider is the remote storage contract from STORAGE_AND_RETENTION.md. Keys are always checked
// with ValidKey before they reach an implementation.
type Provider interface {
	// Put stores size bytes from r under key. sum is the SHA-256 of the content; providers that
	// support it hand it to the server so a corrupted upload is refused there.
	Put(ctx context.Context, key string, r io.Reader, size int64, sum []byte) error
	// Promote moves a verified temporary object to its final key.
	Promote(ctx context.Context, tmpKey, finalKey string) error
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	Stat(ctx context.Context, key string) (ObjectInfo, error)
	// List calls fn for every object under prefix.
	List(ctx context.Context, prefix string, fn func(ObjectInfo) error) error
	// Delete removes key. A missing object is not an error, so retries are safe.
	Delete(ctx context.Context, key string) error
	Health(ctx context.Context) error
	Capabilities() Capabilities
	Close() error
}

var keyRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._/-]{0,511}$`)

// ValidKey accepts the object keys this package builds: lower-case letters, digits, '.', '_', '-'
// and '/', with no empty, '.' or '..' segment. Anything else is a bug or an attack.
func ValidKey(key string) error {
	if !keyRe.MatchString(key) || strings.HasSuffix(key, "/") {
		return fmt.Errorf("invalid object key %q", key)
	}
	for _, seg := range strings.Split(key, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return fmt.Errorf("invalid object key %q", key)
		}
	}
	return nil
}

// Verify checks that key holds exactly size bytes with SHA-256 sum. It trusts a server checksum
// when the provider has one; otherwise it reads the object back and hashes it.
func Verify(ctx context.Context, p Provider, key string, size int64, sum []byte) error {
	info, err := p.Stat(ctx, key)
	if err != nil {
		return err
	}
	if info.Size != size {
		return fmt.Errorf("%w: %s has %d bytes, expected %d", ErrVerify, key, info.Size, size)
	}
	if info.SHA256 != nil {
		if !bytes.Equal(info.SHA256, sum) {
			return fmt.Errorf("%w: %s checksum differs", ErrVerify, key)
		}
		return nil
	}
	rc, err := p.Get(ctx, key)
	if err != nil {
		return err
	}
	defer rc.Close()
	h := sha256.New()
	n, err := io.Copy(h, rc)
	if err != nil {
		return err
	}
	if n != size || !bytes.Equal(h.Sum(nil), sum) {
		return fmt.Errorf("%w: %s content differs on read-back", ErrVerify, key)
	}
	return nil
}
