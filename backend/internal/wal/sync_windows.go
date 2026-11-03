//go:build windows

package wal

// syncDir is a no-op on Windows: NTFS journals metadata and directories cannot be fsynced.
// Production runs on Linux, where the directory is synced.
func syncDir(string) error { return nil }
