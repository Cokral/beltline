package jsonx

import "unicode/utf8"

const hexDigits = "0123456789abcdef"

// appendString writes s as a JSON string exactly as encoding/json does with
// HTML escaping off: quotes, backslashes and control characters are escaped,
// invalid UTF-8 becomes �, and U+2028 and U+2029 are escaped. "<", ">",
// "&" and "/" are written as they are.
func appendString(dst []byte, s string) []byte {
	dst = append(dst, '"')
	start := 0
	for i := 0; i < len(s); {
		if b := s[i]; b < utf8.RuneSelf {
			if b >= 0x20 && b != '"' && b != '\\' {
				i++
				continue
			}
			dst = append(dst, s[start:i]...)
			switch b {
			case '\\', '"':
				dst = append(dst, '\\', b)
			case '\b':
				dst = append(dst, '\\', 'b')
			case '\f':
				dst = append(dst, '\\', 'f')
			case '\n':
				dst = append(dst, '\\', 'n')
			case '\r':
				dst = append(dst, '\\', 'r')
			case '\t':
				dst = append(dst, '\\', 't')
			default:
				dst = append(dst, '\\', 'u', '0', '0', hexDigits[b>>4], hexDigits[b&0xF])
			}
			i++
			start = i
			continue
		}
		c, size := utf8.DecodeRuneInString(s[i:])
		if c == utf8.RuneError && size == 1 {
			dst = append(dst, s[start:i]...)
			dst = append(dst, `�`...)
			i += size
			start = i
			continue
		}
		if c == ' ' || c == ' ' {
			dst = append(dst, s[start:i]...)
			dst = append(dst, '\\', 'u', '2', '0', '2', hexDigits[c&0xF])
			i += size
			start = i
			continue
		}
		i += size
	}
	dst = append(dst, s[start:]...)
	return append(dst, '"')
}

// appendIndented rewrites compact JSON in the data folder's pretty form: two
// spaces an indent, one member or element per line, " : " between a key and
// its value, and an empty object or array as a blank line and an indent
// before its close bracket. The input has no whitespace outside strings.
func appendIndented(dst, src []byte) []byte {
	if dst == nil {
		// The pretty form is about twice the compact form.
		dst = make([]byte, 0, 2*len(src)+64)
	}
	depth := 0
	for i := 0; i < len(src); i++ {
		c := src[i]
		switch c {
		case '"':
			j := i + 1
			for j < len(src) && src[j] != '"' {
				if src[j] == '\\' {
					j++
				}
				j++
			}
			dst = append(dst, src[i:j+1]...)
			i = j
		case '{', '[':
			closer := byte('}')
			if c == '[' {
				closer = ']'
			}
			if i+1 < len(src) && src[i+1] == closer {
				dst = append(dst, c, '\n', '\n')
				dst = appendIndent(dst, depth)
				dst = append(dst, closer)
				i++
				continue
			}
			depth++
			dst = append(dst, c, '\n')
			dst = appendIndent(dst, depth)
		case '}', ']':
			depth--
			dst = append(dst, '\n')
			dst = appendIndent(dst, depth)
			dst = append(dst, c)
		case ',':
			dst = append(dst, ',', '\n')
			dst = appendIndent(dst, depth)
		case ':':
			dst = append(dst, ' ', ':', ' ')
		default:
			dst = append(dst, c)
		}
	}
	return dst
}

// appendIndent writes depth two-space indents.
func appendIndent(dst []byte, depth int) []byte {
	for range depth {
		dst = append(dst, ' ', ' ')
	}
	return dst
}
