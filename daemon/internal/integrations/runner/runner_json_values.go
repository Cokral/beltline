// Helpers the runner adapters share for reading JSON values, trimming text and reading files.

package runner

import (
	"encoding/json"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"
)

// asString is v as a string.
func asString(v any) (string, bool) {
	s, ok := v.(string)
	return s, ok
}

// asObject is v as an object.
func asObject(v any) (map[string]any, bool) {
	m, ok := v.(map[string]any)
	return m, ok
}

// asObjects is v as a list of objects. Any other element makes it not a list of
// objects, which is nil with false.
func asObjects(v any) ([]map[string]any, bool) {
	list, ok := v.([]any)
	if !ok {
		return nil, false
	}
	out := make([]map[string]any, 0, len(list))
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, false
		}
		out = append(out, m)
	}
	return out, true
}

// asStrings is v as a list of strings, with the same all-or-nothing rule as asObjects.
func asStrings(v any) ([]string, bool) {
	list, ok := v.([]any)
	if !ok {
		return nil, false
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		s, ok := item.(string)
		if !ok {
			return nil, false
		}
		out = append(out, s)
	}
	return out, true
}

// firstString is the first of values that is a string, and whether there is one.
func firstString(values ...any) (string, bool) {
	for _, v := range values {
		if s, ok := v.(string); ok {
			return s, true
		}
	}
	return "", false
}

// jsonObject parses text as one JSON object, as `JSONSerialization.jsonObject(with:)
// as? [String: Any]` does. Nil for anything else, including text that is not JSON.
func jsonObject(text string) map[string]any {
	var value any
	if err := json.Unmarshal([]byte(text), &value); err != nil {
		return nil
	}
	object, _ := value.(map[string]any)
	return object
}

// trimWhitespaces trims spaces and tabs, which
// leaves line breaks alone.
func trimWhitespaces(text string) string {
	return strings.TrimFunc(text, func(r rune) bool {
		return r == '\t' || unicode.Is(unicode.Zs, r)
	})
}

// readText reads a file as UTF-8 text, as `try? String(contentsOf:encoding: .utf8)`
// does: false when the file is missing or is not valid UTF-8.
func readText(path string) (string, bool) {
	data, err := os.ReadFile(path)
	if err != nil || !utf8.Valid(data) {
		return "", false
	}
	return string(data), true
}

// realPath resolves symbolic links in an existing path, as realpath(3) does.
func realPath(path string) (string, bool) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", false
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", false
	}
	return resolved, true
}

// userHome is the user's home folder, or empty when it cannot be found.
func userHome() string {
	home, _ := os.UserHomeDir()
	return home
}

// roundHalfUp rounds a rational to the nearest integer, halves away from zero,
// as NSDecimalRound with .plain does.
func roundHalfUp(r *big.Rat) *big.Int {
	negative := r.Sign() < 0
	magnitude := new(big.Rat).Abs(r)
	// floor(x + 1/2) for x >= 0, which is (2n + d) / (2d).
	num := new(big.Int).Mul(magnitude.Num(), big.NewInt(2))
	num.Add(num, magnitude.Denom())
	den := new(big.Int).Mul(magnitude.Denom(), big.NewInt(2))
	out := new(big.Int).Quo(num, den)
	if negative {
		out.Neg(out)
	}
	return out
}

// intFromBig is the int an integer holds, or false when it does not fit.
func intFromBig(n *big.Int) (int, bool) {
	if !n.IsInt64() {
		return 0, false
	}
	v := n.Int64()
	if int64(int(v)) != v {
		return 0, false
	}
	return int(v), true
}
