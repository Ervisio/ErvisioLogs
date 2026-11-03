//go:build !windows

package wal

import "os"

// syncDir makes a create, rename or remove in dir durable.
func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
