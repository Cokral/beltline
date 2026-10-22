// Small helpers the model needs in place of a standard library: optional
// comparison, max and min, the whitespace set, and value copies of arrays,
// dictionaries and structs, shared by the model, engine and presentation code.
//
// Go slices, maps and pointers alias, so copies are made explicitly.
// cloneValue is the deep copy behind the Clone methods, which sit beside their
// types.

package core

import (
	"crypto/rand"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"unicode"
)

// isBlank is a character CharacterSet.whitespaces holds: a space
// separator (Zs) or a tab. Used by the agent file, the draft editor and subject
// code, which trims with that set.
func isBlank(r rune) bool {
	return r == '\t' || unicode.Is(unicode.Zs, r)
}

// trimBlanks trims CharacterSet.whitespaces (isBlank) from
// both ends, leaving line breaks. The whitespace and newline set is exactly
// unicode.IsSpace, so strings.TrimSpace trims that set.
func trimBlanks(s string) string {
	return strings.TrimFunc(s, isBlank)
}

// IsNewline is Character.isNewline for one scalar: a line feed, carriage
// return, vertical tab, form feed, U+0085, U+2028 or U+2029.
func IsNewline(r rune) bool {
	switch r {
	case '\n', '\r', '\v', '\f', 0x85, 0x2028, 0x2029:
		return true
	}
	return false
}

// CopySet is a new set holding the members of set; nil becomes an empty set.
func CopySet[K comparable](set map[K]struct{}) map[K]struct{} {
	out := make(map[K]struct{}, len(set))
	for k := range set {
		out[k] = struct{}{}
	}
	return out
}

// CopyPointer is a fresh pointer to a copy of *p, or nil, so the copy shares
// nothing with the original. Replaces forkCopyString and runStringCopy.
func CopyPointer[T any](p *T) *T {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

// ValueOf is the value p points to, or the zero value for nil
// Replaces runStringValue and draftString.
func ValueOf[T any](p *T) T {
	var zero T
	if p == nil {
		return zero
	}
	return *p
}

// ValueOr is the value p points to, or def for nil.
func ValueOr[T any](p *T, def T) T {
	if p == nil {
		return def
	}
	return *p
}

// SamePointee reports whether two optional values are both nil, or both set and
// equal. Replaces
// forkSameRoute, runSameOptional, modelContextEqual and the other packages'
// optional compares.
func SamePointee[T comparable](a, b *T) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// pickBy returns a when better(a, b), else b: the max or min of two values,
// with better as the strict comparison. Replaces pauseLater and pauseEarlier:
// pickBy(a, b, time.Time.After) is max(a, b), pickBy(a, b, time.Time.Before) is min(a, b).
func pickBy[T any](a, b T, better func(x, y T) bool) T {
	if better(a, b) {
		return a
	}
	return b
}

// AddInts is a + b, reporting false when the sum overflows int. Replaces the
// overflow checks of usageAdding (signed counts) and contextSizeAdd (counts that
// are not negative, where the check is the same). The runner's usage and rate
// sums use it too.
func AddInts(a, b int) (int, bool) {
	sum := a + b
	if (b > 0 && sum < a) || (b < 0 && sum > a) {
		return 0, false
	}
	return sum, true
}

// cloneEach is a new slice with clone applied to each element; nil stays nil.
// Replaces draftCloneSlice, draftKeep and the element copies of the Clone methods.
func cloneEach[T any](s []T, clone func(T) T) []T {
	if s == nil {
		return nil
	}
	out := make([]T, len(s))
	for i, v := range s {
		out[i] = clone(v)
	}
	return out
}

// cloneStrings is a new slice holding the same strings; nil stays nil (encoders
// write [] for it anyway). Replaces cloneRepos and draftCopyStrings.
func cloneStrings(s []string) []string {
	if s == nil {
		return nil
	}
	out := make([]string, len(s))
	copy(out, s)
	return out
}

// cloneValue is a deep copy of v: every slice, map and pointer reachable through
// exported fields is copied, so the copy shares no backing storage with v. Used
// by the Clone methods. Unexported fields are
// copied shallowly, so a type with an unexported slice, map or pointer clones
// that field itself. T is a struct or slice type (an interface value is copied
// as its dynamic value).
func cloneValue[T any](v T) T {
	src := reflect.ValueOf(&v).Elem()
	dst := reflect.New(src.Type()).Elem()
	copyDeep(dst, src)
	return dst.Interface().(T)
}

// copyDeep sets dst (settable) to a deep copy of src.
func copyDeep(dst, src reflect.Value) {
	switch src.Kind() {
	case reflect.Slice:
		if src.IsNil() {
			return
		}
		out := reflect.MakeSlice(src.Type(), src.Len(), src.Len())
		for i := range src.Len() {
			copyDeep(out.Index(i), src.Index(i))
		}
		dst.Set(out)
	case reflect.Map:
		if src.IsNil() {
			return
		}
		out := reflect.MakeMapWithSize(src.Type(), src.Len())
		for iter := src.MapRange(); iter.Next(); {
			value := reflect.New(src.Type().Elem()).Elem()
			copyDeep(value, iter.Value())
			out.SetMapIndex(iter.Key(), value)
		}
		dst.Set(out)
	case reflect.Pointer:
		if src.IsNil() {
			return
		}
		p := reflect.New(src.Type().Elem())
		copyDeep(p.Elem(), src.Elem())
		dst.Set(p)
	case reflect.Struct:
		dst.Set(src)
		plan := structCopyPlanOf(src.Type())
		if !plan.deep {
			return
		}
		for _, i := range plan.exported {
			copyDeep(dst.Field(i), src.Field(i))
		}
	default:
		dst.Set(src)
	}
}

// structCopy is what copyDeep needs to know about a struct type, worked out once per type:
// the indices of its exported fields, and whether any of them holds something a deep copy
// must replace. A struct with no such field is assigned whole, which is what the field walk
// would have produced, without a Field lookup (and its allocation) per field per copy.
type structCopy struct {
	exported []int
	deep     bool
}

// structCopyPlans caches a structCopy per reflect.Type; the field layout never changes.
var structCopyPlans sync.Map

// structCopyPlanOf is the cached structCopy for t. Concurrent first calls may each
// compute it; they store the same plan.
// Lock: none (a sync.Map).
func structCopyPlanOf(t reflect.Type) structCopy {
	if plan, ok := structCopyPlans.Load(t); ok {
		return plan.(structCopy)
	}
	var plan structCopy
	for i := range t.NumField() {
		field := t.Field(i)
		if !field.IsExported() {
			continue
		}
		plan.exported = append(plan.exported, i)
		if holdsCopiedReferences(field.Type) {
			plan.deep = true
		}
	}
	structCopyPlans.Store(t, plan)
	return plan
}

// holdsCopiedReferences reports whether copyDeep replaces a value of type t with a copy
// that shares no storage: a slice, map or pointer, or a struct with one in an exported field.
// Lock: none (pure).
func holdsCopiedReferences(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.Slice, reflect.Map, reflect.Pointer:
		return true
	case reflect.Struct:
		return structCopyPlanOf(t).deep
	default:
		return false
	}
}

// NewUUID returns a random version 4 UUID in upper case text form.
// It is one of core's permitted uses of system randomness (see doc.go).
func NewUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return strings.ToUpper(fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]))
}
