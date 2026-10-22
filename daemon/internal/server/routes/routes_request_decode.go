package routes

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strconv"
)

// missingFieldError is a request whose field tagged required:"true" is absent or null.
type missingFieldError string

func (e missingFieldError) Error() string {
	return "missing field " + strconv.Quote(string(e))
}

// decodeRequest decodes a request body into v, which is a pointer, and then walks
// the body against v (checkRequestValue). At every level the walk reaches, every
// string enum the body names must be one of its cases, a field tagged
// required:"true" must be present and not null, and, when strict, a key v does not
// declare is an error; strict also refuses anything after the value. Without
// strict, undeclared keys are ignored. Data files do not come through here.
func decodeRequest(payload []byte, v any, strict bool) error {
	dec := json.NewDecoder(bytes.NewReader(payload))
	if err := dec.Decode(v); err != nil {
		// The decoder can refuse a body that also lacks a required member (a stage with
		// no type). Main reported that member as missing, so it is named first.
		var tree any
		if json.Unmarshal(payload, &tree) == nil {
			if missing := missingRequiredMember(tree, reflect.TypeOf(v).Elem()); missing != nil {
				return missing
			}
		}
		return err
	}
	if strict {
		if _, err := dec.Token(); err != io.EOF {
			return errors.New("invalid character after the top-level value")
		}
	}
	var tree any
	if err := json.Unmarshal(payload, &tree); err != nil {
		return err
	}
	return checkRequestValue(tree, reflect.ValueOf(v).Elem(), strict)
}
