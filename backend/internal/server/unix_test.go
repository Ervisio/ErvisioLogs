package server

import (
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestListenUnix(t *testing.T) {
	// t.TempDir can exceed the unix socket path limit on macOS.
	dir, err := os.MkdirTemp("", "evl")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	path := filepath.Join(dir, "admin.sock")

	// A socket left behind by a crashed process is replaced.
	stale, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	stale.SetUnlinkOnClose(false)
	_ = stale.Close()

	l, err := ListenUnix(path)
	if err != nil {
		t.Fatalf("stale socket: %v", err)
	}
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o660 {
		t.Fatalf("mode %v, %v", fi.Mode().Perm(), err)
	}
	if _, err := ListenUnix(path); err == nil {
		t.Fatal("a live socket must not be taken over")
	}
	_ = l.Close()

	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ListenUnix(file); err == nil {
		t.Fatal("a regular file must not be removed")
	}
}
