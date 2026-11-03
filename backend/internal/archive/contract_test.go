package archive

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"io"
	"sort"
	"testing"
	"testing/iotest"
)

// runContract checks one provider against the contract every implementation must meet. prefix
// isolates the run inside a shared bucket or directory.
func runContract(t *testing.T, p Provider, prefix string) {
	t.Helper()
	ctx := context.Background()
	data := make([]byte, 300*1024+17)
	if _, err := rand.Read(data); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	tmp, final := prefix+"tmp/a.parquet", prefix+"v1/x/a.parquet"

	t.Run("health", func(t *testing.T) {
		if err := p.Health(ctx); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("put verify promote get", func(t *testing.T) {
		if err := p.Put(ctx, tmp, bytes.NewReader(data), int64(len(data)), sum[:]); err != nil {
			t.Fatal(err)
		}
		if err := Verify(ctx, p, tmp, int64(len(data)), sum[:]); err != nil {
			t.Fatal(err)
		}
		if err := p.Promote(ctx, tmp, final); err != nil {
			t.Fatal(err)
		}
		if _, err := p.Stat(ctx, tmp); !errors.Is(err, ErrNotExist) {
			t.Fatalf("temporary object still there after promote: %v", err)
		}
		if err := Verify(ctx, p, final, int64(len(data)), sum[:]); err != nil {
			t.Fatal(err)
		}
		rc, err := p.Get(ctx, final)
		if err != nil {
			t.Fatal(err)
		}
		got, err := io.ReadAll(rc)
		rc.Close()
		if err != nil || !bytes.Equal(got, data) {
			t.Fatalf("read back %d bytes, err %v", len(got), err)
		}
	})

	t.Run("verify catches a different checksum", func(t *testing.T) {
		bad := sha256.Sum256([]byte("other"))
		if err := Verify(ctx, p, final, int64(len(data)), bad[:]); !errors.Is(err, ErrVerify) {
			t.Fatalf("got %v, want ErrVerify", err)
		}
		if err := Verify(ctx, p, final, int64(len(data))+1, sum[:]); !errors.Is(err, ErrVerify) {
			t.Fatalf("size mismatch: got %v, want ErrVerify", err)
		}
	})

	t.Run("short upload leaves no object", func(t *testing.T) {
		key := prefix + "tmp/short.parquet"
		half := data[:len(data)/2]
		err := p.Put(ctx, key, bytes.NewReader(half), int64(len(data)), sum[:])
		if err == nil {
			// A provider may accept the bytes it got; then verification must reject them.
			if verr := Verify(ctx, p, key, int64(len(data)), sum[:]); !errors.Is(verr, ErrVerify) && !errors.Is(verr, ErrNotExist) {
				t.Fatalf("short upload accepted and verified: %v", verr)
			}
			_ = p.Delete(ctx, key)
			return
		}
		if _, serr := p.Stat(ctx, key); !errors.Is(serr, ErrNotExist) {
			t.Fatalf("failed upload left an object: %v", serr)
		}
	})

	t.Run("reader error aborts the upload", func(t *testing.T) {
		key := prefix + "tmp/broken.parquet"
		r := io.MultiReader(bytes.NewReader(data[:1000]), iotest.ErrReader(errors.New("disk read failed")))
		if err := p.Put(ctx, key, r, int64(len(data)), sum[:]); err == nil {
			t.Fatal("upload with a failing reader succeeded")
		}
		if _, err := p.Stat(ctx, key); !errors.Is(err, ErrNotExist) {
			t.Fatalf("failed upload left an object: %v", err)
		}
	})

	t.Run("list", func(t *testing.T) {
		second := prefix + "v1/x/b.parquet"
		if err := p.Put(ctx, second, bytes.NewReader([]byte("b")), 1, nil); err != nil {
			t.Fatal(err)
		}
		var keys []string
		if err := p.List(ctx, prefix+"v1/", func(o ObjectInfo) error { keys = append(keys, o.Key); return nil }); err != nil {
			t.Fatal(err)
		}
		sort.Strings(keys)
		if len(keys) != 2 || keys[0] != final || keys[1] != second {
			t.Fatalf("list = %v", keys)
		}
	})

	t.Run("delete is idempotent", func(t *testing.T) {
		for i := 0; i < 2; i++ {
			if err := p.Delete(ctx, final); err != nil {
				t.Fatalf("delete %d: %v", i, err)
			}
		}
		_ = p.Delete(ctx, prefix+"v1/x/b.parquet")
		if _, err := p.Stat(ctx, final); !errors.Is(err, ErrNotExist) {
			t.Fatalf("stat after delete: %v", err)
		}
		if _, err := p.Get(ctx, final); !errors.Is(err, ErrNotExist) {
			t.Fatalf("get after delete: %v", err)
		}
	})

	t.Run("rejects unsafe keys", func(t *testing.T) {
		for _, k := range []string{"../etc/passwd", "/abs", "a//b", "a/../b", "A/b", "a/b/", "", "a\\b"} {
			if err := p.Put(ctx, k, bytes.NewReader(nil), 0, nil); err == nil {
				t.Errorf("Put accepted key %q", k)
			}
		}
	})
}

func TestLocalFSContract(t *testing.T) {
	p, err := NewLocalFS(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runContract(t, p, "")
}

func TestValidKey(t *testing.T) {
	good := []string{"v1/0190/0191/2026/11/03/g1-0192.parquet", "tmp/x.parquet", "a"}
	for _, k := range good {
		if err := ValidKey(k); err != nil {
			t.Errorf("%q: %v", k, err)
		}
	}
	bad := []string{"", "/a", "a/", "a//b", "./a", "a/./b", "a/../b", "..", "Ab", "a b", "a\x00b", "-a"}
	for _, k := range bad {
		if err := ValidKey(k); err == nil {
			t.Errorf("%q accepted", k)
		}
	}
}

func stringsReader(s string) io.Reader { return bytes.NewReader([]byte(s)) }

func sha(s string) []byte {
	h := sha256.Sum256([]byte(s))
	return h[:]
}
