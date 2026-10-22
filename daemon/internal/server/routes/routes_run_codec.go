// The helpers the run reads and
// commands share: the snapshot, the body decoders and the id and command guard.
package routes

import (
	"context"
	"fmt"
	"net/http"
	"reflect"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/kit/coordinator"
	"github.com/robbertmanders/beltline/daemon/internal/server/api"
)

// runSnapshot is the run with the actions the
// daemon would accept now.
func runSnapshot(ctx context.Context, run core.Run, c *coordinator.Coordinator) api.RunSnapshot {
	_ = ctx
	return api.NewRunSnapshot(run, c.RunActions(run))
}

// runDecodeThinking is an absent or empty
// thinking is nil; an unknown level is a validation failure.
func runDecodeThinking(raw *string) (*core.Thinking, error) {
	if raw == nil {
		return nil, nil
	}
	for _, level := range core.AllThinking {
		if string(level) == *raw {
			return &level, nil
		}
	}
	return nil, core.NewValidationFailure("thinking must be low, medium, high, xhigh or max.", nil)
}

// runDecodeBackend is an absent or empty runner
// is nil; an unknown runner is a validation failure.
func runDecodeBackend(raw *string) (*core.Backend, error) {
	if raw == nil {
		return nil, nil
	}
	for _, backend := range core.AllBackend {
		if string(backend) == *raw {
			return &backend, nil
		}
	}
	return nil, core.NewValidationFailure("runner must name a supported backend.", nil)
}

// runAfkMode is AfkMode(rawValue:): the mode named by raw, if there is one.
func runAfkMode(raw string) (core.AfkMode, bool) {
	for _, mode := range core.AllAfkMode {
		if string(mode) == raw {
			return mode, true
		}
	}
	return "", false
}

// legOf is the leg of an optional leg body, nil when the body was absent.
func legOf(decoded *api.RunLegRequest) *string {
	if decoded == nil {
		return nil
	}
	return decoded.Leg
}

// runPrepared reads the run named by the id
// path segment, and its command body (required or optional), before the command
// runs. A missing run is a 404 before the body is looked at. It takes the
// coordinator because the run's existence is read from it.
func runPrepared(c *coordinator.Coordinator, r *http.Request, required bool) (string, []byte, error) {
	if c == nil {
		return "", nil, ReadRouteSupportUnavailable()
	}
	id, err := ReadRouteSupportID(r)
	if err != nil {
		return "", nil, err
	}
	if c.ReadRun(id) == nil {
		return "", nil, api.MissingResource{}
	}
	body, err := ReadRouteSupportCommandBody(r, required)
	if err != nil {
		return "", nil, err
	}
	return id, body, nil
}

// runTypeName is the name of the decoded type, which the Go type shares.
func runTypeName[T any]() string {
	return reflect.TypeFor[T]().Name()
}

// runDecode is a required command body decoded
// as JSON for T. Undeclared members are ignored, as the JSON codec's decode does
// ignores them; a body that does not decode is a 400 naming the type.
func runDecode[T any](body []byte) (T, error) {
	var v T
	if body == nil {
		return v, HTTPError{Status: http.StatusBadRequest, Message: "The request body is required."}
	}
	if err := JSONDecode(body, &v); err != nil {
		return v, HTTPError{Status: http.StatusBadRequest, Message: fmt.Sprintf("The request body is not valid JSON for %s.", runTypeName[T]())}
	}
	return v, nil
}

// runDecodeOptional is an optional command
// body decoded as JSON for T, nil when the body is absent.
func runDecodeOptional[T any](body []byte) (*T, error) {
	if body == nil {
		return nil, nil
	}
	var v T
	if err := JSONDecode(body, &v); err != nil {
		return nil, HTTPError{Status: http.StatusBadRequest, Message: fmt.Sprintf("The request body is not valid JSON for %s.", runTypeName[T]())}
	}
	return &v, nil
}
