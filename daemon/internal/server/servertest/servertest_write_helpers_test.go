package servertest

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// contractInventory is the `inventory(root)` helper: every file under root with its
// bytes, keyed by its path below root, so a rejected request can prove nothing was written.
func contractInventory(t testing.TB, root string) map[string][]byte {
	t.Helper()
	files := map[string][]byte{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files[strings.TrimPrefix(path, root)] = data
		return nil
	})
	if err != nil {
		t.Fatalf("inventory %s: %v", root, err)
	}
	return files
}

// sameInventory fails unless the data folder holds exactly the files it held before.
func requireSameInventory(t testing.TB, before map[string][]byte, root string) {
	t.Helper()
	after := contractInventory(t, root)
	if len(before) != len(after) {
		t.Fatalf("the data folder changed: %d files, was %d", len(after), len(before))
	}
	for path, data := range before {
		if !bytes.Equal(after[path], data) {
			t.Fatalf("the data folder changed at %s", path)
		}
	}
}

// postJSON sends a POST with a bearer token and a JSON body (none when body is nil).
func postJSON(g *Fixture, path string, body []byte) Reply {
	return g.Do(Request{Method: "POST", Path: path, Body: body, Token: ContractToken})
}

// requireProblems fails unless the 422 reply's problems are exactly want and its message joins them.
func requireProblems(t testing.TB, reply Reply, want []string) {
	t.Helper()
	requireStatus(t, reply, 422)
	tree := contractTree(t, reply.Body)
	problems, _ := contractMember(t, tree, "problems").([]any)
	if len(problems) != len(want) {
		t.Fatalf("problems %v, want %v", problems, want)
	}
	for i, problem := range problems {
		if problem != want[i] {
			t.Fatalf("problem %d is %v, want %q", i, problem, want[i])
		}
	}
}
