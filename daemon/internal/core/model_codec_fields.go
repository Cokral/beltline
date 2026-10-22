// Helpers for the decoders that need to tell an absent key from a null one,
// which runs and belts saved by older versions depend on.

package core

import (
	"bytes"
	"encoding/json"
	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
	"slices"
)

// codecFields is one JSON object's members, so a decoder can tell an absent
// key from a null one.
type codecFields map[string]json.RawMessage

// codecFieldsOf reads data as a JSON object's members.
func codecFieldsOf(data []byte) (codecFields, error) {
	var f codecFields
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, err
	}
	return f, nil
}

// has reports whether key is present, even as null.
func (f codecFields) has(key string) bool {
	_, ok := f[key]
	return ok
}

// present reports whether key holds a value other than null.
func (f codecFields) present(key string) bool {
	raw, ok := f[key]
	return ok && !codecIsNull(raw)
}

// codecIsNull reports whether raw is the JSON literal null.
func codecIsNull(raw json.RawMessage) bool {
	return string(bytes.TrimSpace(raw)) == "null"
}

// enumKnown reports whether x is one of all, the cases of its enum.
func enumKnown[T comparable](x T, all []T) bool {
	return slices.Contains(all, x)
}

// codecDecodeRaw decodes raw into v, unless raw is absent or null, which leave v as it is.
func codecDecodeRaw(raw json.RawMessage, v any) error {
	if len(raw) == 0 || codecIsNull(raw) {
		return nil
	}
	return json.Unmarshal(raw, v)
}

// setOf is a set saved as a sorted array. The map's keys are
// its members.
type setOf[T ~string] map[T]struct{}

// MarshalJSON writes the set as a sorted array of its members.
func (s setOf[T]) MarshalJSON() ([]byte, error) {
	members := make([]T, 0, len(s))
	for m := range s {
		members = append(members, m)
	}
	slices.Sort(members)
	return jsonx.Marshal(members)
}

// UnmarshalJSON reads the members from an array.
func (s *setOf[T]) UnmarshalJSON(data []byte) error {
	var list []T
	if err := json.Unmarshal(data, &list); err != nil {
		return err
	}
	set := make(setOf[T], len(list))
	for _, m := range list {
		set[m] = struct{}{}
	}
	*s = set
	return nil
}

// codecTry decodes raw into a T when it holds a value of that type. Absent,
// null and unreadable values give false, so a caller can fall back.
func codecTry[T any](raw json.RawMessage) (T, bool) {
	var v T
	if len(raw) == 0 || codecIsNull(raw) || json.Unmarshal(raw, &v) != nil {
		var zero T
		return zero, false
	}
	return v, true
}
