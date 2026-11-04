package server

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"time"
)

// ListenUnix listens on the admin unix socket at path with mode 0660 (ADR 0019). A stale socket left
// by a crash is removed; a live one, or any other file at path, is an error.
func ListenUnix(path string) (net.Listener, error) {
	fi, err := os.Lstat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return nil, err
	case fi.Mode()&fs.ModeSocket == 0:
		return nil, fmt.Errorf("%s exists and is not a socket", path)
	default:
		if c, err := net.DialTimeout("unix", path, time.Second); err == nil {
			_ = c.Close()
			return nil, fmt.Errorf("%s is in use by another process", path)
		}
		if err := os.Remove(path); err != nil {
			return nil, err
		}
	}
	l, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	// Listen creates the file with the process umask; the directory must not be world-accessible.
	if err := os.Chmod(path, 0o660); err != nil {
		_ = l.Close()
		return nil, err
	}
	return l, nil
}
