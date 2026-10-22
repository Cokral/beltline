// Decoding fallbacks for stored JSON: an absent or null field leaves the default, and a field of the
// wrong type is an error. Every JSON read and write of this package goes through here or through Storage.

package store

import (
	"bytes"
	"encoding/json"
)

// isAbsent reports whether a field is missing or null; decodeIfPresent treats both alike.
func isAbsent(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) == 0 || string(trimmed) == "null"
}

// decodeIfPresent: an absent or
// null field leaves dst alone, and a field of the wrong type is an error.
func decodeIfPresent(raw json.RawMessage, dst any) error {
	if isAbsent(raw) {
		return nil
	}
	return json.Unmarshal(raw, dst)
}

// tryDecode decodes with a fallback: an absent,
// null or malformed field gives the fallback, whatever the reason.
func tryDecode[T any](raw json.RawMessage, fallback T) T {
	if isAbsent(raw) {
		return fallback
	}
	var value T
	if err := json.Unmarshal(raw, &value); err != nil {
		return fallback
	}
	return value
}
