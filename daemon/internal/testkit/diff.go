package testkit

import (
	"reflect"

	"github.com/google/go-cmp/cmp"
)

// Diff returns a readable go-cmp diff of two values, "" when they are equal.
// Lines marked "-" come from want and lines marked "+" from got. Unexported
// fields are compared too, so a diff never panics on a struct from another
// package.
func Diff(want, got any) string {
	return cmp.Diff(want, got, cmp.Exporter(func(reflect.Type) bool { return true }))
}
