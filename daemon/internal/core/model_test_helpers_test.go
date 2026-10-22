package core

import (
	"encoding/json"
	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
	"reflect"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

// testDiffEmpty is a readable diff of two model values that treats a nil slice or
// map as equal to an empty one.
func testDiffEmpty(want, got any) string {
	return cmp.Diff(want, got, cmpopts.EquateEmpty(), cmp.Exporter(func(reflect.Type) bool { return true }))
}

// testDecode decodes a JSON literal into v and fails the test on error.
func testDecode(t *testing.T, data string, v any) {
	t.Helper()
	if err := json.Unmarshal([]byte(data), v); err != nil {
		t.Fatalf("decode %s: %v", data, err)
	}
}

// sameJSON reports whether two JSON documents are the same once their keys are
// sorted and their spacing dropped, so a test can state a wire form without
// depending on the order the encoder writes keys in.
func sameJSON(t *testing.T, got []byte, want string) bool {
	t.Helper()
	a, err := jsonx.Canonical(got)
	if err != nil {
		t.Fatalf("canonical %s: %v", got, err)
	}
	b, err := jsonx.Canonical([]byte(want))
	if err != nil {
		t.Fatalf("canonical %s: %v", want, err)
	}
	return string(a) == string(b)
}
