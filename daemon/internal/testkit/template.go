package testkit

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"testing"
)

// template is one built folder. once makes concurrent first calls for the same
// key wait for a single build; err records a failed or unfinished build.
type template struct {
	once sync.Once
	dir  string
	err  error
}

var (
	templatesMu sync.Mutex
	templates   = map[string]*template{}
	// templateRoot is the folder under os.TempDir() that holds the built
	// templates of this process; empty until the first build.
	templateRoot string
)

// errUnfinished is the build result when a build panicked before it finished.
var errUnfinished = errors.New("template build did not finish")

// Template builds a folder once per test binary and hands each caller a
// private copy of it. The first call for a key runs build(dir) into a folder
// under os.TempDir() owned by this process; every call returns a fresh
// CopyTree of that folder inside t.TempDir(), so writes to one copy never
// reach another.
//
// Template folders outlive the test that built them. A package's TestMain
// should call Cleanup after m.Run to remove them.
func Template(t testing.TB, key string, build func(dir string) error) string {
	t.Helper()
	tpl := templateFor(key)
	tpl.once.Do(func() {
		tpl.err = errUnfinished
		dir, err := newTemplateDir(key)
		if err != nil {
			tpl.err = err
			return
		}
		if err := build(dir); err != nil {
			tpl.err = fmt.Errorf("template %q: %w", key, err)
			return
		}
		tpl.dir = dir
		tpl.err = nil
	})
	if tpl.err != nil {
		t.Fatalf("testkit: %v", tpl.err)
		return ""
	}
	// The copy lives under the process's template root rather than t.TempDir():
	// removing a seeded copy file by file after every test cost about a fifth of
	// a package's CPU; Cleanup removes them all at once when the binary ends.
	dst, err := newCopyDir()
	if err != nil {
		t.Fatalf("testkit: %v", err)
		return ""
	}
	CopyTree(t, tpl.dir, dst)
	return dst
}

// newCopyDir is a fresh empty folder for one copy under the template root.
func newCopyDir() (string, error) {
	templatesMu.Lock()
	defer templatesMu.Unlock()
	if templateRoot == "" {
		return "", fmt.Errorf("copy: no template root")
	}
	return os.MkdirTemp(templateRoot, "copy-")
}

// templateFor returns the entry for key, creating it when absent.
func templateFor(key string) *template {
	templatesMu.Lock()
	defer templatesMu.Unlock()
	tpl, ok := templates[key]
	if !ok {
		tpl = &template{}
		templates[key] = tpl
	}
	return tpl
}

// newTemplateDir creates a fresh folder for one template under this process's
// root, creating the root on first use.
func newTemplateDir(key string) (string, error) {
	templatesMu.Lock()
	defer templatesMu.Unlock()
	if templateRoot == "" {
		root, err := os.MkdirTemp(os.TempDir(), fmt.Sprintf("beltline-testkit-%d-", os.Getpid()))
		if err != nil {
			return "", fmt.Errorf("template root: %w", err)
		}
		templateRoot = root
	}
	dir, err := os.MkdirTemp(templateRoot, "tpl-")
	if err != nil {
		return "", fmt.Errorf("template %q: %w", key, err)
	}
	return dir, nil
}

// Cleanup removes every template folder this process built and forgets them,
// so a later Template call builds again. Call it from TestMain after m.Run.
func Cleanup() {
	templatesMu.Lock()
	defer templatesMu.Unlock()
	if templateRoot != "" {
		removeInBackground(templateRoot)
	}
	templateRoot = ""
	templates = map[string]*template{}
}

// removeInBackground removes root without waiting: the templates and every copy
// of them are tens of thousands of files, and the test binary need not wait for
// them. It moves root aside first, so a later root never meets the old one, and
// falls back to removing it here when no remover can start.
func removeInBackground(root string) {
	gone := root + ".removing"
	if err := os.Rename(root, gone); err != nil {
		gone = root
	}
	remover := exec.Command("/bin/rm", "-rf", gone)
	if err := remover.Start(); err != nil {
		_ = os.RemoveAll(gone) // fallback when no background remover runs; the test binary exits either way
		return
	}
	go func() { _ = remover.Wait() }() // the background removal is not waited for, so its exit status is not used
}
