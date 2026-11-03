package archive

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// LocalFS stores objects in a directory, for tests and for a mounted network share. It is only
// as durable as that directory: on the same disk as ClickHouse it is not a remote copy.
type LocalFS struct{ root string }

func NewLocalFS(root string) (*LocalFS, error) {
	if root == "" || !filepath.IsAbs(root) {
		return nil, errors.New("local provider needs an absolute directory")
	}
	if err := os.MkdirAll(root, 0o750); err != nil {
		return nil, err
	}
	return &LocalFS{root: root}, nil
}

func (l *LocalFS) path(key string) (string, error) {
	if err := ValidKey(key); err != nil {
		return "", err
	}
	return filepath.Join(l.root, filepath.FromSlash(key)), nil
}

func (l *LocalFS) Put(ctx context.Context, key string, r io.Reader, size int64, _ []byte) error {
	p, err := l.path(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(p), ".put-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	n, err := io.Copy(f, &ctxReader{ctx: ctx, r: r})
	if err == nil && n != size {
		err = fmt.Errorf("short upload: wrote %d of %d bytes", n, size)
	}
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

func (l *LocalFS) Promote(_ context.Context, tmpKey, finalKey string) error {
	src, err := l.path(tmpKey)
	if err != nil {
		return err
	}
	dst, err := l.path(finalKey)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		return err
	}
	if err := os.Rename(src, dst); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return ErrNotExist
		}
		return err
	}
	return nil
}

func (l *LocalFS) Get(_ context.Context, key string) (io.ReadCloser, error) {
	p, err := l.path(key)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(p)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNotExist
	}
	return f, err
}

func (l *LocalFS) Stat(_ context.Context, key string) (ObjectInfo, error) {
	p, err := l.path(key)
	if err != nil {
		return ObjectInfo{}, err
	}
	st, err := os.Stat(p)
	if errors.Is(err, fs.ErrNotExist) {
		return ObjectInfo{}, ErrNotExist
	}
	if err != nil {
		return ObjectInfo{}, err
	}
	return ObjectInfo{Key: key, Size: st.Size(), ModTime: st.ModTime()}, nil
}

func (l *LocalFS) List(ctx context.Context, prefix string, fn func(ObjectInfo) error) error {
	err := filepath.WalkDir(l.root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() || strings.HasPrefix(d.Name(), ".put-") {
			return nil
		}
		rel, err := filepath.Rel(l.root, p)
		if err != nil {
			return err
		}
		key := path.Clean(filepath.ToSlash(rel))
		if !strings.HasPrefix(key, prefix) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		return fn(ObjectInfo{Key: key, Size: info.Size(), ModTime: info.ModTime()})
	})
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

func (l *LocalFS) Delete(_ context.Context, key string) error {
	p, err := l.path(key)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

func (l *LocalFS) Health(context.Context) error {
	st, err := os.Stat(l.root)
	if err != nil {
		return err
	}
	if !st.IsDir() {
		return errors.New("archive root is not a directory")
	}
	f, err := os.CreateTemp(l.root, ".health-*")
	if err != nil {
		return fmt.Errorf("archive root is not writable: %w", err)
	}
	name := f.Name()
	f.Close()
	return os.Remove(name)
}

func (l *LocalFS) Capabilities() Capabilities {
	return Capabilities{AtomicRename: true, RangeReads: true}
}

func (l *LocalFS) Close() error { return nil }

// ctxReader stops a copy when the context ends.
type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c *ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}
