// The run reads, one
// registration function per read endpoint (see the registry for each method and
// path). The run commands and tasks are in routes_run_command_routes.go.
package routes

import (
	"net/http"

	"github.com/robbertmanders/beltline/daemon/internal/kit/coordinator"
	"github.com/robbertmanders/beltline/daemon/internal/server/api"
)

// AddRunList GET api/v1/runs, every
// run with the actions the daemon would accept now.
func AddRunList(mux *http.ServeMux, c *coordinator.Coordinator) {
	mux.Handle("GET /api/v1/runs", JSONRoute(c, func(r *http.Request) (any, error) {
		runs := c.ReadRuns()
		snapshots := make([]api.RunSnapshot, 0, len(runs))
		for _, run := range runs {
			snapshots = append(snapshots, runSnapshot(r.Context(), run, c))
		}
		return api.RunsResponse{Runs: snapshots}, nil
	}))
}

// AddRunDetail GET api/v1/runs/{id}.
// A run that does not exist is a 404.
func AddRunDetail(mux *http.ServeMux, c *coordinator.Coordinator) {
	mux.Handle("GET /api/v1/runs/{id}", JSONRoute(c, func(r *http.Request) (any, error) {
		id, err := ReadRouteSupportID(r)
		if err != nil {
			return nil, err
		}
		run := c.ReadRun(id)
		if run == nil {
			return nil, api.MissingResource{}
		}
		return api.RunResponse{Run: runSnapshot(r.Context(), *run, c)}, nil
	}))
}

// AddRunAttemptOutput GET api/v1/runs/{id}/attempts/{attempt}/output.
func AddRunAttemptOutput(mux *http.ServeMux, c *coordinator.Coordinator) {
	mux.Handle("GET /api/v1/runs/{id}/attempts/{attempt}/output", JSONRoute(c, func(r *http.Request) (any, error) {
		id, err := ReadRouteSupportID(r)
		if err != nil {
			return nil, err
		}
		attempt, err := ReadRouteSupportParameter(r, "attempt")
		if err != nil {
			return nil, err
		}
		snapshot := c.ReadAttemptOutputSnapshot(id, attempt)
		if snapshot == nil {
			return nil, api.MissingResource{}
		}
		return api.NewAttemptOutputResponse(*snapshot), nil
	}))
}

// AddRunAttemptArtifacts is the add run attempt artifacts:
// GET api/v1/runs/{id}/attempts/{attempt}/artifacts. Only the HTML files whose
// names the artifact route serves are listed, each with a signed link.
func AddRunAttemptArtifacts(mux *http.ServeMux, c *coordinator.Coordinator, signer ArtifactLinkSigner) {
	mux.Handle("GET /api/v1/runs/{id}/attempts/{attempt}/artifacts", JSONRoute(c, func(r *http.Request) (any, error) {
		id, err := ReadRouteSupportID(r)
		if err != nil {
			return nil, err
		}
		attempt, err := ReadRouteSupportParameter(r, "attempt")
		if err != nil {
			return nil, err
		}
		snapshot := c.ReadArtifactsSnapshot(id, attempt)
		if snapshot == nil {
			return nil, api.MissingResource{}
		}
		artifacts := make([]api.AttemptArtifactWire, 0, len(snapshot.Files))
		for _, file := range snapshot.Files {
			link := ArtifactSignedLink(snapshot.RunID, snapshot.Attempt.ID, file.Name, signer)
			artifacts = append(artifacts, api.AttemptArtifactWire{Name: file.Name, SizeBytes: file.SizeBytes, URL: link.URL, ExpiresAt: link.ExpiresAt})
		}
		return api.NewAttemptArtifactsResponse(snapshot.RunID, snapshot.Attempt.ID, artifacts), nil
	}))
}
