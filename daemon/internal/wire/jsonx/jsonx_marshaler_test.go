package jsonx

import (
	"strings"
	"testing"
)

// TestMarshalerOutputIsCanonical checks that a MarshalJSON method's output is
// written sorted, unescaped and compact, both in the wire form and the storage form.
func TestMarshalerOutputIsCanonical(t *testing.T) {
	t.Parallel()
	value := struct {
		R referenceRaw `json:"r"`
	}{}
	got, err := Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	// The method's keys are unsorted and its string is escaped differently;
	// the output has sorted keys, "</" as is, and U+2028 escaped.
	if want := "{\"r\":{\"a\":\"</\\u2028\",\"b\":[1,2.50],\"c\":{}}}"; string(got) != want {
		t.Fatalf("marshaler output = %s, want %s", got, want)
	}
	storage, err := MarshalStorage(value)
	if err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"r\" : {\n    \"a\" : \"</\\u2028\",\n    \"b\" : [\n      1,\n      2.50\n    ],\n    \"c\" : {\n\n    }\n  }\n}"
	if string(storage) != want {
		t.Fatalf("storage form = %q, want %q", storage, want)
	}
	if strings.Contains(string(got), " ") {
		t.Fatalf("compact output has whitespace: %s", got)
	}
}
