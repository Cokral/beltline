package routes

import (
	"net/http"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/kit/coordinator"
	"github.com/robbertmanders/beltline/daemon/internal/server/api"
)

// AfkReviewRequest is the body of POST /api/v1/afk/review: the decisions to mark
// as checked, or none (nil) for all of them.
type AfkReviewRequest struct {
	IDs *[]string `json:"ids,omitzero"`
}

// AddAfkDecisions GET api/v1/afk/decisions?filter=review|all.
// It reads the decision history in the chosen filter's order and never marks anything reviewed.
// Lock: the handler calls the coordinator's AFK reads, each of which takes the lock.
func AddAfkDecisions(mux *http.ServeMux, c *coordinator.Coordinator) {
	mux.Handle("GET /api/v1/afk/decisions", JSONRoute(c, func(r *http.Request) (any, error) {
		filter, err := afkFilter(r)
		if err != nil {
			return nil, err
		}
		log := c.ReadAfkLog()
		return api.NewAfkDecisionsResponse(log.Decisions, log.Runs, log.FoldersPresent, filter), nil
	}))
}

// afkFilter is the AFK log's filter from the query,
// All by default; an unknown value is a validation failure.
func afkFilter(r *http.Request) (core.AfkLogFilter, error) {
	raw, ok := HummingbirdQueryParameter("filter", r.URL.RawQuery)
	if !ok {
		return core.AfkLogFilterAll, nil
	}
	for _, filter := range []core.AfkLogFilter{core.AfkLogFilterReview, core.AfkLogFilterAll} {
		if string(filter) == raw {
			return filter, nil
		}
	}
	return "", core.NewFailure("Unknown AFK filter \"" + raw + "\".")
}

// AddAfkReview POST api/v1/afk/review.
// Marks decisions as checked: the named ones, or every one still to check when
// `ids` is absent, as the app's "Looks right" and "Mark all as checked" did.
// Answers with the decision history after it.
// Lock: the handler calls the coordinator's AFK methods, each of which takes the lock.
func AddAfkReview(mux *http.ServeMux, c *coordinator.Coordinator) {
	mux.Handle("POST /api/v1/afk/review", JSONRoute(c, func(r *http.Request) (any, error) {
		body, err := DecodeOptionalBody(r, AfkReviewRequest{}, DefaultBodyLimit)
		if err != nil {
			return nil, err
		}
		var ids map[string]struct{}
		if body.IDs != nil {
			ids = make(map[string]struct{}, len(*body.IDs))
			for _, id := range *body.IDs {
				ids[id] = struct{}{}
			}
		}
		c.ReviewAfk(ids)
		log := c.ReadAfkLog()
		return api.NewAfkDecisionsResponse(log.Decisions, log.Runs, log.FoldersPresent, core.AfkLogFilterAll), nil
	}))
}
