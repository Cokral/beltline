package routes

import (
	"net/http"

	"github.com/robbertmanders/beltline/daemon/internal/kit/coordinator"
	"github.com/robbertmanders/beltline/daemon/internal/server/api"
)

// AddAssistantRead GET api/v1/assistant.
// Lock: the handler calls coordinator methods, which take the lock.
func AddAssistantRead(mux *http.ServeMux, c *coordinator.Coordinator) {
	mux.Handle("GET /api/v1/assistant", JSONRoute(c, func(r *http.Request) (any, error) {
		return api.NewAssistantResponse(c.ReadAssistant()), nil
	}))
}

// AddAssistantConfiguration POST api/v1/assistant/configuration.
// Lock: the handler calls coordinator methods, which take the lock.
func AddAssistantConfiguration(mux *http.ServeMux, c *coordinator.Coordinator) {
	mux.Handle("POST /api/v1/assistant/configuration", JSONRoute(c, func(r *http.Request) (any, error) {
		configuration, err := DecodeBody[coordinator.AssistantConfiguration](r, DefaultBodyLimit)
		if err != nil {
			return nil, err
		}
		if _, err := c.ConfigureAssistant(configuration); err != nil {
			return nil, err
		}
		return api.NewAssistantResponse(c.ReadAssistant()), nil
	}))
}

// AddAssistantStart POST api/v1/assistant/start.
// Lock: the handler calls coordinator methods, which take the lock.
func AddAssistantStart(mux *http.ServeMux, c *coordinator.Coordinator) {
	mux.Handle("POST /api/v1/assistant/start", JSONRoute(c, func(r *http.Request) (any, error) {
		body, err := DecodeBody[api.AssistantStartRequest](r, DefaultBodyLimit)
		if err != nil {
			return nil, err
		}
		if err := c.StartAssistant(r.Context(), body.Request); err != nil {
			return nil, err
		}
		return api.NewAssistantResponse(c.ReadAssistant()), nil
	}))
}

// AddAssistantStop POST api/v1/assistant/stop.
// Lock: the handler calls coordinator methods, which take the lock.
func AddAssistantStop(mux *http.ServeMux, c *coordinator.Coordinator) {
	mux.Handle("POST /api/v1/assistant/stop", JSONRoute(c, func(r *http.Request) (any, error) {
		if err := DecodeEmptyObjectOrNoBody(r, DefaultBodyLimit); err != nil {
			return nil, err
		}
		if err := c.StopAssistant(r.Context()); err != nil {
			return nil, err
		}
		return api.NewAssistantResponse(c.ReadAssistant()), nil
	}))
}
