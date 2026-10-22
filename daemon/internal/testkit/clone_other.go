//go:build !darwin

package testkit

import "errors"

// cloneTree is only available on APFS; elsewhere CopyTree copies file by file.
func cloneTree(src, dst string) error { return errors.New("testkit: no clone copy here") }
