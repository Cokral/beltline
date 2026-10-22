package jsonx

import (
	"bytes"
	"encoding/json"
	"math"
	"math/rand/v2"
	"strings"
	"testing"
	"time"
)

// encodeCases are values whose encoding must match the reference encoder byte
// for byte: embedding, omitzero, nil and empty collections, pointers,
// interfaces, text keys, marshalers, escapes and numbers.
func encodeCases() map[string]any {
	inner := &referenceInner{}
	return map[string]any{
		"outer zero":       referenceOuter{},
		"outer full":       referenceOuter{referenceEmbedded: referenceEmbedded{Shared: "s"}, referenceInner: inner, Zeta: "<&> ", Alpha: []int{}, Bytes: []byte("hi"), NilBytes: []byte{}, Map: map[string]string{"b": "2", "a": "1"}, Keys: map[int]bool{10: true, 9: false}, Dates: []Date{}, Pointer: &referenceInner{Count: 3}, Any: []string(nil), Text: referenceText{"x"}, TextKeys: map[referenceText]int{{"k"}: 1}, Nested: map[string][]float64{"n": nil}, Skipped: "no", Private: "yes", hidden: "no"},
		"outer hidden set": referenceOuter{Hidden: []int{1}, Empty: []int{}, Dates: []Date{{}}},
		"unexported":       referenceUnexported{},
		"unexported set": referenceUnexported{Items: []string{}, Child: struct {
			List []int `json:"list"`
		}{}},
		"raw marshaler": referenceRaw{},
		"raw in struct": struct {
			R referenceRaw
			N []int `json:"n"`
		}{},
		"raw pointer nil": struct {
			R *referenceRaw `json:"r"`
		}{},
		"raw in slice":  []referenceRaw{{}, {}},
		"nil any":       nil,
		"nil slice":     []int(nil),
		"nil map":       map[string]int(nil),
		"nil bytes":     []byte(nil),
		"empty bytes":   []byte{},
		"nested nils":   []any{nil, []int(nil), map[string]int(nil), []any{nil}},
		"array of nils": [2][]int{nil, {1}},
		"struct array": struct {
			A [1][]string `json:"a"`
			B []string    `json:"b"`
		}{},
		"map of structs":   map[string]referenceInner{"b": {}, "a": {Items: []string{"x"}}},
		"int keys":         map[int64]string{-3: "x", 12: "y", 2: "z"},
		"uint keys":        map[uint8]string{200: "x", 7: "y"},
		"text keys":        map[referenceText]bool{{"b"}: true, {"a"}: false},
		"nil text key ptr": map[*referenceText]int{nil: 1},
		"strings":          "quote\" back\\ slash/ html<>& tab\t nl\n cr\r bs\b ff\f ctl\x01\x1f del\x7f",
		"invalid utf8":     "bad\xff\xfe\xc3(end",
		"line separators":  "a b c é 日本",
		"floats":           []float64{0, math.Copysign(0, -1), 1, 1.5, 123456789.125, 1e21, 1e20, 1e-6, 1e-7, 1.5e-9, 5e-324, math.MaxFloat64, -2.5e300},
		"float32s":         []float32{0.1, 1e-7, 3.4e38, 16777216},
		"ints":             []any{int8(-8), int16(16), int32(-32), int64(math.MinInt64), uint(7), uint64(math.MaxUint64), uintptr(9)},
		"bools":            []bool{true, false},
		"json numbers":     []json.Number{"1.50", "", "-0", "1e+9", "12345678901234567890"},
		"dates":            []Date{{Time: time.Date(2026, 10, 7, 8, 36, 40, 999, time.FixedZone("CEST", 2*3600))}},
		"nested maps":      map[string]map[string][]int{"z": {"y": nil}, "a": nil},
		"pointers":         []*int{nil, new(int)},
		"interface values": []any{referenceText{"v"}, referenceRaw{}, 1.25, "s", []any{map[string]any{}}},
		"raw top level":    json.RawMessage(`{"b":[1,2],"a":{"d":"A"}}`),
		"struct no fields": struct{}{},
		"embedded pointer": struct{ *referenceInner }{},
		"omitempty struct": struct {
			I *referenceInner `json:"i,omitempty"`
			S []int           `json:"s,omitempty"`
			Z struct{}        `json:"z,omitempty"`
		}{},
		"omitzero struct": struct {
			I referenceInner `json:"i,omitzero"`
			T time.Time      `json:"t,omitzero"`
			M map[string]int `json:"m,omitzero"`
		}{},
	}
}

// TestEncoderMatchesReference checks every case in both forms against the
// reference encoder: the same bytes, or both fail.
func TestEncoderMatchesReference(t *testing.T) {
	t.Parallel()
	for name, v := range encodeCases() {
		want, wantErr := refMarshal(v)
		got, gotErr := Marshal(v)
		if (wantErr == nil) != (gotErr == nil) || !bytes.Equal(got, want) {
			t.Errorf("%s: Marshal\n got %s (%v)\nwant %s (%v)", name, got, gotErr, want, wantErr)
		}
		wantStorage, wantErr := refMarshalStorage(v)
		gotStorage, gotErr := MarshalStorage(v)
		if (wantErr == nil) != (gotErr == nil) || !bytes.Equal(gotStorage, wantStorage) {
			t.Errorf("%s: MarshalStorage\n got %q (%v)\nwant %q (%v)", name, gotStorage, gotErr, wantStorage, wantErr)
		}
	}
}

// TestEncoderFailsLikeReference checks the values JSON cannot write.
func TestEncoderFailsLikeReference(t *testing.T) {
	t.Parallel()
	for name, v := range map[string]any{
		"nan":     math.NaN(),
		"inf":     struct{ F float64 }{math.Inf(1)},
		"chan":    make(chan int),
		"func":    struct{ F func() }{},
		"complex": complex(1, 2),
	} {
		_, wantErr := refMarshal(v)
		_, gotErr := Marshal(v)
		if wantErr == nil || gotErr == nil {
			t.Errorf("%s: reference err %v, new err %v; both must fail", name, wantErr, gotErr)
		}
	}
}

// TestEncoderKeepsItsPlansAcrossCalls checks that a type's plan, built once,
// encodes the same bytes on every later call.
func TestEncoderKeepsItsPlansAcrossCalls(t *testing.T) {
	t.Parallel()
	value := referenceOuter{Zeta: "z", Map: map[string]string{"k": "v"}}
	first, err := MarshalStorage(value)
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		again, err := MarshalStorage(value)
		if err != nil || !bytes.Equal(first, again) {
			t.Fatalf("second encoding differs: %v", err)
		}
	}
}

// TestAppendStringMatchesEncodingJSON checks string escaping against
// encoding/json with HTML escaping off, for every byte and a random mix.
func TestAppendStringMatchesEncodingJSON(t *testing.T) {
	t.Parallel()
	check := func(s string) {
		t.Helper()
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(s); err != nil {
			t.Fatal(err)
		}
		want := bytes.TrimSuffix(buf.Bytes(), []byte("\n"))
		if got := appendString(nil, s); !bytes.Equal(got, want) {
			t.Fatalf("appendString(%q) = %s, want %s", s, got, want)
		}
	}
	for b := range 256 {
		check(string([]byte{byte(b)}))
		check("x" + string([]byte{byte(b)}) + "y")
	}
	check("  �\xe2\x80")
	rng := rand.New(rand.NewPCG(1, 2))
	alphabet := []string{"a", "\"", "\\", "<", "&", "/", "\n", "\x00", " ", "\xff", "é", "\U0001F600"}
	for range 2000 {
		var sb strings.Builder
		for range rng.IntN(12) {
			sb.WriteString(alphabet[rng.IntN(len(alphabet))])
		}
		check(sb.String())
	}
}

// TestAppendFloatMatchesEncodingJSON checks float formatting against encoding/json.
func TestAppendFloatMatchesEncodingJSON(t *testing.T) {
	t.Parallel()
	values := []float64{0, 1, -1, 0.5, 1e-6, 1e-7, 9.999e-7, 1e20, 1e21, 1.5e21, 2.5e-300, 1.0 / 3, 100, 1e6, 123456.789}
	rng := rand.New(rand.NewPCG(3, 4))
	for range 500 {
		values = append(values, rng.NormFloat64()*math.Pow(10, float64(rng.IntN(40)-20)))
	}
	for _, f := range values {
		want, err := json.Marshal(f)
		if err != nil {
			t.Fatal(err)
		}
		got, err := appendFloat(nil, f, 64)
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("float %v = %s (%v), want %s", f, got, err, want)
		}
		want32, err := json.Marshal(float32(f))
		if err != nil {
			t.Fatal(err)
		}
		got32, err := appendFloat(nil, float64(float32(f)), 32)
		if err != nil || !bytes.Equal(got32, want32) {
			t.Fatalf("float32 %v = %s (%v), want %s", float32(f), got32, err, want32)
		}
	}
}

// TestCanonicalMatchesReference checks that Canonical sorts, unescapes and
// normalizes a document the way the decode-and-encode it replaces did.
func TestCanonicalMatchesReference(t *testing.T) {
	t.Parallel()
	for _, doc := range []string{
		`{ "b": 1.50, "a": [1, 2] }`,
		`{"z":{"y":[],"x":{}},"a":null,"m":true,"f":false}`,
		`{"a":"<\/é","b":"x y"}`,
		`{"dup":1,"a":2,"dup":3}`,
		"{\"k\":\"tab\\tnl\\n\",\"c\":\"\xff\"}",
		`[{"b":{"d":1,"c":2}},{"a":-0.0e-3}]`,
		`  "plain"  `,
		`12345678901234567890.5e+1`,
		`{}`,
		`[]`,
	} {
		want, err := refMarshalCanonical([]byte(doc))
		if err != nil {
			t.Fatalf("%s: reference: %v", doc, err)
		}
		got, err := Canonical([]byte(doc))
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("%s: Canonical = %s (%v), want %s", doc, got, err, want)
		}
	}
	for _, bad := range []string{``, `{"a":1`, `{"a" 1}`, `[1,]`, `{"a":1} x`, `tru`, `01`, `"a`} {
		if _, err := Canonical([]byte(bad)); err == nil {
			t.Errorf("%q: Canonical accepted invalid JSON", bad)
		}
	}
	if got, err := Canonical(nil); err != nil || string(got) != "null" {
		t.Fatalf("Canonical(nil) = %s, %v", got, err)
	}
}

// refMarshalCanonical is the reference canonical form: decode, then encode.
func refMarshalCanonical(raw []byte) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var tree any
	if err := dec.Decode(&tree); err != nil {
		return nil, err
	}
	return refWrite(tree, "")
}

// TestStringOptionMatchesEncodingJSON checks the ",string" tag option: bools, numbers and strings are
// written again as JSON strings, a nil pointer stays null, and other types ignore the option.
func TestStringOptionMatchesEncodingJSON(t *testing.T) {
	t.Parallel()
	type quoted struct {
		A bool           `json:"a,string"`
		B int            `json:"b,string"`
		C float64        `json:"c,string"`
		D string         `json:"d,string"`
		E *int           `json:"e,string"`
		F *int           `json:"f,string"`
		G json.Number    `json:"g,string"`
		H []int          `json:"h,string"` //nolint:staticcheck // SA5008: the test checks the option is ignored on a list
		I map[string]int `json:"i,string"` //nolint:staticcheck // SA5008: and on a map
	}
	seven := 7
	values := []quoted{
		{},
		{A: true, B: -3, C: 1.5e-9, D: `say "hi" <b>`, E: &seven, G: "12.5", H: []int{1}, I: map[string]int{"x": 1}},
	}
	for _, v := range values {
		var want bytes.Buffer
		enc := json.NewEncoder(&want)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(v); err != nil {
			t.Fatal(err)
		}
		expected := bytes.TrimSuffix(want.Bytes(), []byte("\n"))
		if v.H == nil { // jsonx writes nil collections as [] and {}
			expected = bytes.Replace(bytes.Replace(expected, []byte(`"h":null`), []byte(`"h":[]`), 1), []byte(`"i":null`), []byte(`"i":{}`), 1)
		}
		got, err := Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(expected) {
			t.Errorf("Marshal(%+v)\n got %s\nwant %s", v, got, expected)
		}
	}
}
