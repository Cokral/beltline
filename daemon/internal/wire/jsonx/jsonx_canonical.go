package jsonx

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"unicode/utf8"
)

// scanner reads one JSON document and writes it in canonical compact form:
// no whitespace, object keys sorted, duplicate keys keeping the last value,
// and strings written as appendString writes them. Numbers and literals are
// kept as they are, as the decode-and-encode it replaces kept them.
type scanner struct {
	src []byte
	pos int
	// members holds the members of the objects being read, innermost last. An
	// object owns the entries from where it started; a nested object pushes
	// its own above them and pops them before the outer one adds its next.
	members []member
}

// member is one object member as written to the output: its key, and the span
// of its key and value in the output.
type member struct {
	key        []byte
	start, end int
}

// appendCanonical appends the canonical form of the JSON document src to dst.
// It is how a MarshalJSON method's output joins the encoder's output.
func appendCanonical(dst, src []byte) ([]byte, error) {
	s := &scanner{src: src}
	out, err := s.value(dst)
	if err != nil {
		return dst, err
	}
	s.skip()
	if s.pos != len(src) {
		return dst, s.fail("data after the value")
	}
	return out, nil
}

func (s *scanner) fail(what string) error {
	return fmt.Errorf("jsonx: invalid JSON at byte %d: %s", s.pos, what)
}

func (s *scanner) skip() {
	for s.pos < len(s.src) {
		switch s.src[s.pos] {
		case ' ', '\t', '\n', '\r':
			s.pos++
		default:
			return
		}
	}
}

func (s *scanner) value(dst []byte) ([]byte, error) {
	s.skip()
	if s.pos >= len(s.src) {
		return dst, s.fail("unexpected end")
	}
	switch s.src[s.pos] {
	case '{':
		return s.object(dst)
	case '[':
		return s.array(dst)
	case '"':
		start, end, plain, err := s.stringToken()
		if err != nil {
			return dst, err
		}
		if plain {
			return append(dst, s.src[start:end]...), nil
		}
		text, err := decodeString(s.src[start:end])
		if err != nil {
			return dst, err
		}
		return appendString(dst, text), nil
	}
	return s.literal(dst)
}

// object writes an object with its members sorted by key. A key that appears
// twice keeps its last value, as decoding into a map did.
func (s *scanner) object(dst []byte) ([]byte, error) {
	s.pos++
	s.skip()
	if s.pos < len(s.src) && s.src[s.pos] == '}' {
		s.pos++
		return append(dst, "{}"...), nil
	}
	base := len(dst)
	mark := len(s.members)
	out := append(dst, '{')
	for {
		s.skip()
		if s.pos >= len(s.src) || s.src[s.pos] != '"' {
			return dst, s.fail("object key")
		}
		start, end, plain, err := s.stringToken()
		if err != nil {
			return dst, err
		}
		var key []byte
		if plain {
			key = s.src[start+1 : end-1]
		} else {
			text, err := decodeString(s.src[start:end])
			if err != nil {
				return dst, err
			}
			key = []byte(text)
		}
		s.skip()
		if s.pos >= len(s.src) || s.src[s.pos] != ':' {
			return dst, s.fail("colon after key")
		}
		s.pos++
		if len(s.members) > mark {
			out = append(out, ',')
		}
		begin := len(out)
		if plain {
			out = append(out, s.src[start:end]...)
		} else {
			out = appendString(out, string(key))
		}
		out = append(out, ':')
		if out, err = s.value(out); err != nil {
			return dst, err
		}
		s.members = append(s.members, member{key: key, start: begin, end: len(out)})
		s.skip()
		if s.pos >= len(s.src) {
			return dst, s.fail("unexpected end in object")
		}
		if s.src[s.pos] == ',' {
			s.pos++
			continue
		}
		if s.src[s.pos] == '}' {
			s.pos++
			break
		}
		return dst, s.fail("object separator")
	}
	members := s.members[mark:]
	defer func() { s.members = s.members[:mark] }()
	sorted := true
	for i := 1; i < len(members); i++ {
		if bytes.Compare(members[i-1].key, members[i].key) >= 0 {
			sorted = false
			break
		}
	}
	if sorted {
		return append(out, '}'), nil
	}
	slices.SortStableFunc(members, func(a, b member) int { return bytes.Compare(a.key, b.key) })
	joined := make([]byte, 0, len(out)-base)
	joined = append(joined, '{')
	written := 0
	for i, m := range members {
		if i+1 < len(members) && bytes.Equal(members[i+1].key, m.key) {
			continue
		}
		if written > 0 {
			joined = append(joined, ',')
		}
		written++
		joined = append(joined, out[m.start:m.end]...)
	}
	joined = append(joined, '}')
	return append(dst[:base], joined...), nil
}

// array writes an array's elements in order.
func (s *scanner) array(dst []byte) ([]byte, error) {
	s.pos++
	s.skip()
	if s.pos < len(s.src) && s.src[s.pos] == ']' {
		s.pos++
		return append(dst, "[]"...), nil
	}
	out := append(dst, '[')
	var err error
	for n := 0; ; n++ {
		if n > 0 {
			out = append(out, ',')
		}
		if out, err = s.value(out); err != nil {
			return dst, err
		}
		s.skip()
		if s.pos >= len(s.src) {
			return dst, s.fail("unexpected end in array")
		}
		if s.src[s.pos] == ',' {
			s.pos++
			continue
		}
		if s.src[s.pos] == ']' {
			s.pos++
			return append(out, ']'), nil
		}
		return dst, s.fail("array separator")
	}
}

// literal writes a number, true, false or null.
func (s *scanner) literal(dst []byte) ([]byte, error) {
	start := s.pos
	for s.pos < len(s.src) && !isDelimiter(s.src[s.pos]) {
		s.pos++
	}
	tok := s.src[start:s.pos]
	switch string(tok) {
	case "true", "false", "null":
		return append(dst, tok...), nil
	}
	if !isNumber(tok) {
		s.pos = start
		return dst, s.fail("unexpected token")
	}
	return append(dst, tok...), nil
}

// stringToken checks the string at the current position and returns its span,
// including both quotes. plain reports that its bytes are already the
// canonical form: no escapes, no control characters, valid UTF-8, and no
// U+2028 or U+2029.
func (s *scanner) stringToken() (start, end int, plain bool, err error) {
	start = s.pos
	plain = true
	for i := s.pos + 1; i < len(s.src); {
		c := s.src[i]
		switch {
		case c == '"':
			s.pos = i + 1
			return start, s.pos, plain, nil
		case c == '\\':
			plain = false
			if i+1 >= len(s.src) {
				break
			}
			switch s.src[i+1] {
			case '"', '\\', '/', 'b', 'f', 'n', 'r', 't':
				i += 2
				continue
			case 'u':
				if i+6 <= len(s.src) && isHex(s.src[i+2:i+6]) {
					i += 6
					continue
				}
			}
		case c < 0x20:
		case c < utf8.RuneSelf:
			i++
			continue
		default:
			r, size := utf8.DecodeRune(s.src[i:])
			if r == utf8.RuneError && size == 1 || r == ' ' || r == ' ' {
				plain = false
			}
			i += size
			continue
		}
		s.pos = i
		return 0, 0, false, s.fail("bad string")
	}
	s.pos = len(s.src)
	return 0, 0, false, s.fail("unterminated string")
}

// decodeString reads a string token with its quotes, as encoding/json reads it.
func decodeString(token []byte) (string, error) {
	var text string
	if err := json.Unmarshal(token, &text); err != nil {
		return "", err
	}
	return text, nil
}

func isDelimiter(c byte) bool {
	switch c {
	case ' ', '\t', '\n', '\r', ',', ':', '[', ']', '{', '}', '"':
		return true
	}
	return false
}

func isHex(b []byte) bool {
	for _, c := range b {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return true
}

// isNumber reports whether tok is a JSON number.
func isNumber(tok []byte) bool {
	i, n := 0, len(tok)
	if i < n && tok[i] == '-' {
		i++
	}
	switch {
	case i < n && tok[i] == '0':
		i++
	case i < n && tok[i] >= '1' && tok[i] <= '9':
		for i < n && tok[i] >= '0' && tok[i] <= '9' {
			i++
		}
	default:
		return false
	}
	if i < n && tok[i] == '.' {
		i++
		if !digitsFrom(tok, &i) {
			return false
		}
	}
	if i < n && (tok[i] == 'e' || tok[i] == 'E') {
		i++
		if i < n && (tok[i] == '+' || tok[i] == '-') {
			i++
		}
		if !digitsFrom(tok, &i) {
			return false
		}
	}
	return i == n
}

// digitsFrom reports whether at least one digit starts at *i, and moves *i past them.
func digitsFrom(tok []byte, i *int) bool {
	start := *i
	for *i < len(tok) && tok[*i] >= '0' && tok[*i] <= '9' {
		*i++
	}
	return *i > start
}
