// The global pause, AFK,
// quit and check-now commands.
package routes

import (
	"net/http"
	"time"

	"github.com/robbertmanders/beltline/daemon/internal/kit/coordinator"
	"github.com/robbertmanders/beltline/daemon/internal/server/api"
)

// quitDelay is how long the quit command waits after answering, so the answer is
// written before the listener closes.
const quitDelay = 200 * time.Millisecond

// AddGlobalPause POST api/v1/pause.
// Lock: the handler calls SetGloballyPaused and ReadStateSnapshot, which take the lock.
func AddGlobalPause(mux *http.ServeMux, c *coordinator.Coordinator) {
	mux.Handle("POST /api/v1/pause", JSONRoute(c, func(r *http.Request) (any, error) {
		body, err := DecodeBody[api.PauseRequest](r, DefaultBodyLimit)
		if err != nil {
			return nil, err
		}
		c.SetGloballyPaused(r.Context(), body.Paused)
		return api.NewStateResponse(c.ReadStateSnapshot(nil, nil)), nil
	}))
}

// AddGlobalAfk POST api/v1/afk.
// Lock: the handler calls SetGlobalAfk and ReadStateSnapshot, which take the lock.
func AddGlobalAfk(mux *http.ServeMux, c *coordinator.Coordinator) {
	mux.Handle("POST /api/v1/afk", JSONRoute(c, func(r *http.Request) (any, error) {
		body, err := DecodeBody[api.AfkRequest](r, DefaultBodyLimit)
		if err != nil {
			return nil, err
		}
		c.SetGlobalAfk(r.Context(), body.Enabled)
		return api.NewStateResponse(c.ReadStateSnapshot(nil, nil)), nil
	}))
}

// AddGlobalQuit POST api/v1/quit.
// It answers first, then stops the daemon on the same path as SIGTERM, so the
// response is not cut off by the listener closing.
// Lock: the handler calls ReadStateSnapshot, which takes the lock; the quit runs
// on its own goroutine through InvokeQuitHandler, which takes the lock to read the handler.
func AddGlobalQuit(mux *http.ServeMux, c *coordinator.Coordinator) {
	mux.Handle("POST /api/v1/quit", CoordinatorRoute(c, func(w http.ResponseWriter, r *http.Request) error {
		if err := DecodeEmptyObjectOrNoBody(r, DefaultBodyLimit); err != nil {
			return err
		}
		JSONResponse(w, api.NewStateResponse(c.ReadStateSnapshot(nil, nil)), http.StatusOK)
		go func() {
			time.Sleep(quitDelay)
			c.InvokeQuitHandler()
		}()
		return nil
	}))
}

// AddGlobalCheckNow POST api/v1/check-now.
// Lock: the handler calls FetchNow and ReadStateSnapshot, which take the lock.
func AddGlobalCheckNow(mux *http.ServeMux, c *coordinator.Coordinator) {
	mux.Handle("POST /api/v1/check-now", JSONRoute(c, func(r *http.Request) (any, error) {
		if err := DecodeEmptyObjectOrNoBody(r, DefaultBodyLimit); err != nil {
			return nil, err
		}
		c.FetchNow(r.Context())
		return api.NewStateResponse(c.ReadStateSnapshot(nil, nil)), nil
	}))
}
