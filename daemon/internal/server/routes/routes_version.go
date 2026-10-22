package routes

import (
	"net/http"

	"github.com/robbertmanders/beltline/daemon/internal/kit/launch"
)

// AddVersion GET api/v1/version answers
// the daemon's version record, with the system it runs on.
func AddVersion(mux *http.ServeMux, version string, platform launch.Platform) {
	mux.HandleFunc("GET /api/v1/version", func(w http.ResponseWriter, r *http.Request) {
		JSONResponse(w, launch.Version{APIVersion: launch.VersionCurrent, Version: version, Platform: platform.Name()}, http.StatusOK)
	})
}
