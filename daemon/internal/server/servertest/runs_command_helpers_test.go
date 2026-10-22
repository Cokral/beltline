// Helpers for the run command contract tests.

package servertest

import (
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/server/api"
	"github.com/robbertmanders/beltline/daemon/internal/server/routes"
)

// postTwice posts body to path twice at once, as two clicks land together, and returns the replies.
func postTwice(f *Fixture, path string, body []byte) []Reply {
	replies := make([]Reply, 2)
	var wg sync.WaitGroup
	for i := range replies {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			replies[i] = postRunCase(f, path, body)
		}(i)
	}
	wg.Wait()
	return replies
}

// concurrentWinner checks that exactly one of two concurrent clicks was accepted, the other refused
// with its reason, and returns the accepted reply.
func concurrentWinner(t *testing.T, replies []Reply, contractCase string) Reply {
	t.Helper()
	var accepted []Reply
	for _, reply := range replies {
		if reply.Status == http.StatusOK {
			accepted = append(accepted, reply)
		}
		if reply.Status != http.StatusOK && reply.Status != http.StatusUnprocessableEntity {
			t.Fatalf("%s: unexpected response %d", contractCase, reply.Status)
		}
		if reply.Status == http.StatusUnprocessableEntity && errorMessage(t, reply) == "" {
			t.Fatalf("%s: refusal omitted its reason", contractCase)
		}
	}
	if len(accepted) != 1 {
		t.Fatalf("%s: expected one accepted click, got %d", contractCase, len(accepted))
	}
	return accepted[0]
}

// concurrentReusable checks that the accepted clicks number acceptedCount and returns them
// .
func concurrentReusable(t *testing.T, replies []Reply, contractCase string, acceptedCount int) []Reply {
	t.Helper()
	var accepted []Reply
	for _, reply := range replies {
		if reply.Status == http.StatusOK {
			accepted = append(accepted, reply)
		}
		if reply.Status != http.StatusOK && reply.Status != http.StatusUnprocessableEntity {
			t.Fatalf("%s: unexpected response %d", contractCase, reply.Status)
		}
	}
	if len(accepted) != acceptedCount {
		t.Fatalf("%s: expected %d reusable clicks, got %d", contractCase, acceptedCount, len(accepted))
	}
	return accepted
}

// checkRunCommand checks a command's reply: a 200 JSON reply with no-store, and its golden
// .
func checkRunCommand(t *testing.T, f *Fixture, reply Reply, contractCase string) {
	t.Helper()
	CheckGolden(t, contractCase, caseEndpoint(contractCase).GoldenName, reply, f.Root)
}

// decodeRunReply is the run of a RunResponse reply.
func decodeRunReply(t *testing.T, reply Reply) core.Run {
	t.Helper()
	var response api.RunResponse
	if err := routes.JSONDecode(reply.Body, &response); err != nil {
		t.Fatalf("run response: %v", err)
	}
	return response.Run.Run
}

// errorMessage is the message of an error reply ("" when it has none).
func errorMessage(t *testing.T, reply Reply) string {
	t.Helper()
	var body struct {
		Message string `json:"message"`
	}
	if err := routes.JSONDecode(reply.Body, &body); err != nil {
		t.Fatalf("error response: %v", err)
	}
	return body.Message
}

// inventory is every file under the data folder, keyed by its path relative to it, with its bytes
// .
func inventory(t *testing.T, root string) map[string]string {
	t.Helper()
	files := map[string]string{}
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
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files[rel] = string(data)
		return nil
	})
	if err != nil {
		t.Fatalf("inventory: %v", err)
	}
	return files
}

// sameRun reports whether two runs are equal.
func sameRun(a, b core.Run) bool { return reflect.DeepEqual(a, b) }
