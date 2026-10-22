package jsonx

import (
	"encoding"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"slices"
	"strconv"
	"strings"
)

func boolEncoder(e *encoder, v reflect.Value) error {
	e.buf = strconv.AppendBool(e.buf, v.Bool())
	return nil
}

func intEncoder(e *encoder, v reflect.Value) error {
	e.buf = strconv.AppendInt(e.buf, v.Int(), 10)
	return nil
}

func uintEncoder(e *encoder, v reflect.Value) error {
	e.buf = strconv.AppendUint(e.buf, v.Uint(), 10)
	return nil
}

func stringEncoder(e *encoder, v reflect.Value) error {
	e.reserve(v.Len() + 2)
	e.buf = appendString(e.buf, v.String())
	return nil
}

// numberEncoder writes a json.Number as its literal, checked as encoding/json
// checks it; an empty number is 0.
func numberEncoder(e *encoder, v reflect.Value) error {
	s := v.String()
	if s == "" {
		s = "0"
	}
	text, err := json.Marshal(json.Number(s))
	if err != nil {
		return err
	}
	e.buf = append(e.buf, text...)
	return nil
}

func floatEncoder(bits int) encodeFunc {
	return func(e *encoder, v reflect.Value) error {
		var err error
		e.buf, err = appendFloat(e.buf, v.Float(), bits)
		return err
	}
}

// arrayEncoder writes the elements of an array. encoding/json's copy never
// reached an array, so nil collections under one stay null.
func (b *builder) arrayEncoder(t reflect.Type) encodeFunc {
	elem := b.get(t.Elem())
	return func(e *encoder, v reflect.Value) error {
		saved := e.keepNil
		e.keepNil = true
		e.buf = append(e.buf, '[')
		for i := range v.Len() {
			if i > 0 {
				e.buf = append(e.buf, ',')
			}
			if err := elem(e, v.Index(i)); err != nil {
				return err
			}
		}
		e.buf = append(e.buf, ']')
		e.keepNil = saved
		return nil
	}
}

// sliceEncoder writes a slice as an array, or a byte slice as a base64
// string. A nil slice is [] (or "" for bytes), unless keepNil.
func (b *builder) sliceEncoder(t reflect.Type) encodeFunc {
	if t.Elem().Kind() == reflect.Uint8 {
		p := reflect.PointerTo(t.Elem())
		if !p.Implements(marshalerType) && !p.Implements(textMarshalerType) {
			return byteSliceEncoder
		}
	}
	elem := b.get(t.Elem())
	return func(e *encoder, v reflect.Value) error {
		if v.IsNil() {
			if e.keepNil {
				e.buf = append(e.buf, "null"...)
			} else {
				e.buf = append(e.buf, "[]"...)
			}
			return nil
		}
		if e.depth >= maxDepth {
			return &json.UnsupportedValueError{Value: v, Str: "encountered a cycle"}
		}
		e.depth++
		e.reserve(128)
		e.buf = append(e.buf, '[')
		for i := range v.Len() {
			if i > 0 {
				e.buf = append(e.buf, ',')
			}
			if err := elem(e, v.Index(i)); err != nil {
				return err
			}
		}
		e.buf = append(e.buf, ']')
		e.depth--
		return nil
	}
}

// byteSliceEncoder writes bytes as standard base64. A nil slice is "" here,
// because the copy made of it was never nil.
func byteSliceEncoder(e *encoder, v reflect.Value) error {
	if v.IsNil() {
		if e.keepNil {
			e.buf = append(e.buf, "null"...)
		} else {
			e.buf = append(e.buf, `""`...)
		}
		return nil
	}
	e.buf = append(e.buf, '"')
	e.buf = base64.StdEncoding.AppendEncode(e.buf, v.Bytes())
	e.buf = append(e.buf, '"')
	return nil
}

// mapEncoder writes a map as an object with its keys sorted. A nil map is {}
// (or null under keepNil).
func (b *builder) mapEncoder(t reflect.Type) encodeFunc {
	switch t.Key().Kind() {
	case reflect.String, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
	default:
		if !t.Key().Implements(textMarshalerType) {
			return unsupportedEncoder(t)
		}
	}
	elem := b.get(t.Elem())
	return func(e *encoder, v reflect.Value) error {
		if v.IsNil() {
			if e.keepNil {
				e.buf = append(e.buf, "null"...)
			} else {
				e.buf = append(e.buf, "{}"...)
			}
			return nil
		}
		if e.depth >= maxDepth {
			return &json.UnsupportedValueError{Value: v, Str: "encountered a cycle"}
		}
		type entry struct {
			name  string
			value reflect.Value
		}
		entries := make([]entry, 0, v.Len())
		for iter := v.MapRange(); iter.Next(); {
			name, err := keyName(iter.Key())
			if err != nil {
				return err
			}
			entries = append(entries, entry{name, iter.Value()})
		}
		slices.SortFunc(entries, func(a, b entry) int { return strings.Compare(a.name, b.name) })
		e.depth++
		e.reserve(128)
		e.buf = append(e.buf, '{')
		for i, en := range entries {
			if i > 0 {
				e.buf = append(e.buf, ',')
			}
			e.buf = appendString(e.buf, en.name)
			e.buf = append(e.buf, ':')
			if err := elem(e, en.value); err != nil {
				return err
			}
		}
		e.buf = append(e.buf, '}')
		e.depth--
		return nil
	}
}

// keyName is the object key of a map key, as encoding/json names it: a string
// as it is, else its MarshalText, else its decimal digits.
func keyName(k reflect.Value) (string, error) {
	if k.Kind() == reflect.String {
		return k.String(), nil
	}
	if tm, ok := k.Interface().(encoding.TextMarshaler); ok {
		if k.Kind() == reflect.Pointer && k.IsNil() {
			return "", nil
		}
		text, err := tm.MarshalText()
		if err != nil {
			return "", fmt.Errorf("json: error calling MarshalText for type %v: %w", k.Type(), err)
		}
		return string(text), nil
	}
	switch k.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(k.Int(), 10), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return strconv.FormatUint(k.Uint(), 10), nil
	}
	return "", &json.UnsupportedTypeError{Type: k.Type()}
}

// appendFloat writes f as encoding/json does: the shortest form that reads
// back exactly, in exponent form only for very small or very large values.
func appendFloat(dst []byte, f float64, bits int) ([]byte, error) {
	if math.IsInf(f, 0) || math.IsNaN(f) {
		return dst, &json.UnsupportedValueError{Str: strconv.FormatFloat(f, 'g', -1, bits)}
	}
	abs := math.Abs(f)
	format := byte('f')
	if abs != 0 {
		if bits == 64 && (abs < 1e-6 || abs >= 1e21) || bits == 32 && (float32(abs) < 1e-6 || float32(abs) >= 1e21) {
			format = 'e'
		}
	}
	start := len(dst)
	dst = strconv.AppendFloat(dst, f, format, -1, bits)
	if format == 'e' {
		// Clean up e-09 to e-9.
		if n := len(dst); n-start >= 4 && dst[n-4] == 'e' && dst[n-3] == '-' && dst[n-2] == '0' {
			dst[n-2] = dst[n-1]
			dst = dst[:n-1]
		}
	}
	return dst, nil
}
