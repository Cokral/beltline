// Reads of acli's JSON. Numbers are float64, as encoding/json gives them. These keep
// Jira's nil-tolerant reads for chained lookups.

package jira

// obj is v as an object, or nil.
func obj(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

// str is v as a string.
func str(v any) (string, bool) {
	s, ok := v.(string)
	return s, ok
}

// strOr is v as a string, or def when it is not one.
func strOr(v any, def string) string {
	if s, ok := v.(string); ok {
		return s
	}
	return def
}

// objs is v as a list of objects. Like `as? [[String: Any]]`, a list with any
// other element is not one: nil.
func objs(v any) []map[string]any {
	list, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(list))
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			return nil
		}
		out = append(out, m)
	}
	return out
}

// strs is v as a list of strings, with the same all-or-nothing rule as objs.
func strs(v any) []string {
	list, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		s, ok := item.(string)
		if !ok {
			return nil
		}
		out = append(out, s)
	}
	return out
}

// intOf is v as an integer when it is a whole number.
func intOf(v any) (int, bool) {
	f, ok := v.(float64)
	if !ok || f != float64(int(f)) {
		return 0, false
	}
	return int(f), true
}
