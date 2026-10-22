//go:build integration

package coordinator

import "os"

// fileExistsAt reports whether path exists. Lock: none (pure).
func fileExistsAt(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
