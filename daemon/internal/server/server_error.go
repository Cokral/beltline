package server

import (
	"net/http"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/server/api"
	"github.com/robbertmanders/beltline/daemon/internal/server/routes"
)

// ErrorMiddleware turns what a handler returns into the shared error shape. A
// domain error (core.Failure, core.ValidationFailure) is a
// 422 with its message and problems; api.MissingResource and an unknown route are
// 404s; an HTTPError keeps its status. Anything else, such as a transport failure
// or cancellation, is left to the server (a 500 with no body).
//
// Route handlers return their error (routes.RouteFunc); ServeHTTP gives the
// request an error slot, runs next, and writes the mapped response for whatever
// the routes recorded in it.
type ErrorMiddleware struct {
	next http.Handler
}

// ServeHTTP is the serve http.
func (m ErrorMiddleware) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	r, slot := routes.WithErrorSlot(r)
	m.next.ServeHTTP(w, r)
	err := slot.Err()
	if err == nil {
		return
	}
	status, body, ok := ErrorMiddlewareMap(err)
	if !ok {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	routes.JSONError(w, body, status)
}

// ErrorMiddlewareMap is the status and body for an
// error, or false when the error is not one the middleware answers (a
// cancellation or an unrecognised error).
func ErrorMiddlewareMap(err error) (int, api.ErrorResponse, bool) {
	switch e := err.(type) {
	case api.MissingResource:
		return http.StatusNotFound, api.ErrorNotFound, true
	case routes.HTTPError:
		if e.Status == http.StatusNotFound {
			return http.StatusNotFound, api.ErrorNotFound, true
		}
		message := e.Message
		if message == "" {
			message = routes.ReasonPhrase(e.Status)
		}
		return e.Status, api.ErrorResponse{Message: message, Problems: []string{}}, true
	case core.Failure:
		return http.StatusUnprocessableEntity, api.ErrorResponse{Message: e.Message, Problems: nonNilProblems(e.Problems)}, true
	case core.ValidationFailure:
		body := api.ErrorResponse{Message: e.Message, Problems: nonNilProblems(e.Problems)}
		for _, location := range e.Locations {
			if location != nil {
				body.Locations = e.Locations
				break
			}
		}
		return http.StatusUnprocessableEntity, body, true
	case localizedError:
		// An error that carries an ErrorDescription: a 422 with that message and no problems.
		message, ok := e.ErrorDescription()
		if !ok {
			break
		}
		return http.StatusUnprocessableEntity, api.ErrorResponse{Message: message, Problems: []string{}}, true
	}
	// A cancellation and any other error are not answered here.
	return 0, api.ErrorResponse{}, false
}

// localizedError is an error that carries an ErrorDescription. ok is false where
// the description is nil, which the middleware does not answer.
type localizedError interface {
	ErrorDescription() (message string, ok bool)
}

// nonNilProblems keeps an empty problem list encoding as [] rather than null.
func nonNilProblems(problems []string) []string {
	if problems == nil {
		return []string{}
	}
	return problems
}
