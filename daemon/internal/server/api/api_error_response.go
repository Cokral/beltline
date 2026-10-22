package api

import "github.com/robbertmanders/beltline/daemon/internal/core"

// ErrorResponse is the one shape every API failure takes: a message, and the
// problems behind it in order, empty when there is no list.
type ErrorResponse struct {
	Message  string   `json:"message"`
	Problems []string `json:"problems"`
	// Locations has one entry per problem, in order, when any problem is about
	// a field; omitted otherwise. A nil entry is a problem with no location.
	Locations []*core.ProblemLocation `json:"locations,omitzero"`
}

// ErrorUnauthorized is the 401 body.
var ErrorUnauthorized = ErrorResponse{Message: "Unauthorized", Problems: []string{}}

// ErrorForbidden is the 403 body.
var ErrorForbidden = ErrorResponse{Message: "Forbidden", Problems: []string{}}

// ErrorNotFound is the 404 body.
var ErrorNotFound = ErrorResponse{Message: "Not found", Problems: []string{}}

// MissingResource is thrown by a handler when what the request names does not
// exist: a 404.
type MissingResource struct{}

// Error implements error for MissingResource.
func (MissingResource) Error() string {
	return "MissingResource"
}
