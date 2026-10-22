//go:build darwin

// The file's creation date on Darwin: its birth time.

package store

import (
	"os"
	"syscall"
	"time"
)

// birthTime is when the file was created, or the zero time when it cannot be read.
func birthTime(path string) time.Time {
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return info.ModTime()
	}
	return time.Unix(stat.Birthtimespec.Sec, stat.Birthtimespec.Nsec)
}
