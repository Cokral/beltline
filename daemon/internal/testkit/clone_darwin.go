//go:build darwin

package testkit

import (
	"os"

	"golang.org/x/sys/unix"
)

// cloneTree clones the whole directory src as dst with one clonefile(2) call:
// APFS shares the blocks, so a seeded demo folder of hundreds of files copies
// in about a millisecond. dst may exist only as an empty directory (as
// t.TempDir() gives); it is replaced. Any error leaves the caller to copy
// file by file.
func cloneTree(src, dst string) error {
	if entries, err := os.ReadDir(dst); err == nil {
		if len(entries) != 0 {
			return os.ErrExist
		}
		if err := os.Remove(dst); err != nil {
			return err
		}
	}
	if err := unix.Clonefile(src, dst, unix.CLONE_NOFOLLOW); err != nil {
		_ = os.MkdirAll(dst, 0o755)
		return err
	}
	return nil
}
