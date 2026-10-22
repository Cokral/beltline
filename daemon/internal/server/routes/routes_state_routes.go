package routes

import (
	"net/http"

	"github.com/robbertmanders/beltline/daemon/internal/kit/coordinator"
	"github.com/robbertmanders/beltline/daemon/internal/server/api"
)

// AddState GET api/v1/state, the
// global state resource (counts, notices, settings, global actions, navigation).
// Lock: the handler calls ReadStateSnapshot, which takes the lock.
func AddState(mux *http.ServeMux, c *coordinator.Coordinator) {
	mux.Handle("GET /api/v1/state", JSONRoute(c, func(r *http.Request) (any, error) {
		return api.NewStateResponse(c.ReadStateSnapshot(nil, nil)), nil
	}))
}
