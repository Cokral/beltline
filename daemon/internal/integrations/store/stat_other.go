//go:build !darwin

// The file's creation date off Darwin. No birth time is portable there, so the modification time stands in.

package store

import (
	"os"
	"time"
)

// birthTime is the file's modification time, or the zero time when it cannot be read.
func birthTime(path string) time.Time {
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}
