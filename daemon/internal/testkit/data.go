package testkit

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// DataDir returns an empty data folder for one test. Each call gets its own
// folder, removed when the test ends.
func DataDir(t testing.TB) string {
	t.Helper()
	return t.TempDir()
}

// CopyTree copies the directory src into dst, creating dst if it is missing.
// Regular files, directories and symbolic links are copied with their file
// modes; the copy is independent of src. Directory modes are applied after
// their contents are written, so a read-only directory can be copied.
func CopyTree(t testing.TB, src, dst string) {
	t.Helper()
	if cloneTree(src, dst) == nil {
		return
	}
	if err := copyTree(src, dst); err != nil {
		t.Fatalf("testkit: copy %s to %s: %v", src, dst, err)
	}
}

// WriteFile writes text to path, creating its parent folders first, so a test can
// place a file in a folder it has not made yet.
func WriteFile(t testing.TB, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// FolderSnapshot maps every file under root, by its path relative to root, to its
// bytes. A missing root is an empty snapshot, so a test can compare a folder before
// and after an action that may have created it.
func FolderSnapshot(t testing.TB, root string) map[string][]byte {
	t.Helper()
	files := map[string][]byte{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files[rel] = data
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("testkit: snapshot %s: %v", root, err)
	}
	return files
}

// dirMode is a directory whose permission bits and modification time are applied after the copy.
type dirMode struct {
	path     string
	perm     fs.FileMode
	modified time.Time
}

func copyTree(src, dst string) error {
	var dirs []dirMode
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		switch {
		case d.IsDir():
			if err := os.MkdirAll(target, 0o700); err != nil {
				return err
			}
			dirs = append(dirs, dirMode{path: target, perm: info.Mode().Perm(), modified: info.ModTime()})
			return nil
		case d.Type()&fs.ModeSymlink != 0:
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		case d.Type().IsRegular():
			if err := copyFile(path, target, info.Mode().Perm()); err != nil {
				return err
			}
			// The modification time too, as a clone keeps it: the library orders and reloads by it.
			return os.Chtimes(target, info.ModTime(), info.ModTime())
		default:
			return fmt.Errorf("%s: unsupported file type %v", path, info.Mode().Type())
		}
	})
	if err != nil {
		return err
	}
	// Deepest directories first, so a parent's mode never blocks its children.
	for i := len(dirs) - 1; i >= 0; i-- {
		if err := os.Chmod(dirs[i].path, dirs[i].perm); err != nil {
			return err
		}
		if err := os.Chtimes(dirs[i].path, dirs[i].modified, dirs[i].modified); err != nil {
			return err
		}
	}
	return nil
}

// copyFile writes a copy of src to dst with exactly the permission bits perm.
func copyFile(src, dst string, perm fs.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }() // read-only: nothing is written, so a close error loses nothing
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close() // the copy error is the one returned
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	// Chmod, not the create mode: the umask must not change the copy.
	return os.Chmod(dst, perm)
}
