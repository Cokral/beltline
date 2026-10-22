// It lives in this package,
// not in server, because the route handlers write JSON and server imports
// routes (an import cycle otherwise); server uses it from here.
package routes

import (
	"net/http"

	"github.com/robbertmanders/beltline/daemon/internal/server/api"
	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// internalErrorBody is what a response answers when its value cannot be encoded.
var internalErrorBody = []byte(`{"message":"Internal error","problems":[]}`)

// JSONEncode is the server's wire encoding (sorted
// keys, no slash escaping, ISO 8601 dates), through jsonx.Marshal.
func JSONEncode(value any) ([]byte, error) {
	return jsonx.Marshal(value)
}

// JSONDecode decodes a command body into v: undeclared keys are ignored, every
// enum string must be a case of its enum, and a field tagged required must be
// present and not null.
func JSONDecode(data []byte, v any) error {
	return decodeRequest(data, v, false)
}

// JSONResponse writes the value as the
// JSON body with `Content-Type: application/json` and `Cache-Control: no-store`.
// An encoding failure, a programming error, answers with the internal error body.
func JSONResponse(w http.ResponseWriter, value any, status int) {
	data, err := JSONEncode(value)
	if err != nil {
		data = internalErrorBody
	}
	h := w.Header()
	h.Set("Content-Type", "application/json")
	h.Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write(data)
}

// JSONError JSONResponse of an error body.
func JSONError(w http.ResponseWriter, body api.ErrorResponse, status int) {
	JSONResponse(w, body, status)
}
