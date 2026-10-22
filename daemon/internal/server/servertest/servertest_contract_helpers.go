// Shared helpers of the contract cases: the shape checks, which decode the response and compare it.

package servertest

import (
	"fmt"
	"reflect"
	"sync"
	"testing"

	"encoding/json"
	"github.com/robbertmanders/beltline/daemon/internal/server/routes"
	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// contractEndpoint is the registry entry of a contract case; it fails the test for an unknown case.
func contractEndpoint(t testing.TB, contractCase string) routes.Endpoint {
	t.Helper()
	for _, endpoint := range routes.Endpoints {
		if endpoint.Case == contractCase {
			return endpoint
		}
	}
	t.Fatalf("servertest: no registry entry for case %s", contractCase)
	return routes.Endpoint{}
}

// contractBody encodes a request value with the wire encoding.
func contractBody(t testing.TB, value any) []byte {
	t.Helper()
	data, err := jsonx.Marshal(value)
	if err != nil {
		t.Fatalf("encode request: %v", err)
	}
	return data
}

// contractTree decodes a JSON body into its generic tree (objects, arrays, scalars).
func contractTree(t testing.TB, body []byte) any {
	t.Helper()
	var tree any
	if err := json.Unmarshal(body, &tree); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return tree
}

// contractMember is the member key of a JSON object tree, failing the test when it is absent.
func contractMember(t testing.TB, tree any, key string) any {
	t.Helper()
	object, ok := tree.(map[string]any)
	if !ok {
		t.Fatalf("expected a JSON object with %q", key)
	}
	value, ok := object[key]
	if !ok {
		t.Fatalf("response has no %q member", key)
	}
	return value
}

// contractSameAsValue fails unless the tree equals the wire encoding of value (the coordinator's
// value as the server would write it).
func contractSameAsValue(t testing.TB, what string, tree any, value any) {
	t.Helper()
	want := contractTree(t, contractBody(t, value))
	if !reflect.DeepEqual(tree, want) {
		t.Fatalf("%s differs from the coordinator's value", what)
	}
}

// decodeContract decodes a response body into v with the wire decoding.
func decodeContract(body []byte, v any) error {
	return json.Unmarshal(body, v)
}

// contractOutcomeID is the stable outcome id the contract tests number a new stage's outcomes with.
func contractOutcomeID(n int) string {
	return fmt.Sprintf("00000000-0000-4000-8000-%012d", n)
}

// contractReplies are the replies of one request sequence, with the root of the fixture that served it.
// A golden check needs only the body and that root, so a reply is checked after its fixture is closed.
type contractReplies struct {
	root    string
	replies map[string]Reply
}

// contractSequences caches each named sequence's replies for the test binary: the first case that needs a
// sequence runs it on a fresh fixture, and every later case checks its own reply against its golden.
var (
	contractSequencesMu sync.Mutex
	contractSequences   = map[string]*contractReplies{}
)

// contractSequence returns the replies of the named sequence, running build the first time.
func contractSequence(t testing.TB, name string, build func(t testing.TB) (*Fixture, map[string]Reply)) *contractReplies {
	t.Helper()
	contractSequencesMu.Lock()
	defer contractSequencesMu.Unlock()
	if got, ok := contractSequences[name]; ok {
		return got
	}
	g, replies := build(t)
	got := &contractReplies{root: g.Root, replies: replies}
	contractSequences[name] = got
	return got
}
