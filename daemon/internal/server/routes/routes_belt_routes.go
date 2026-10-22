// The belt reads, the
// draft edits (one endpoint per operation, under belts/draft/{op}) and the belt
// commands.
package routes

import (
	"fmt"
	"net/http"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/store"
	"github.com/robbertmanders/beltline/daemon/internal/kit/coordinator"
	"github.com/robbertmanders/beltline/daemon/internal/server/api"
)

// AddBeltList GET api/v1/belts.
// Lock: the handler calls coordinator methods, which take the lock.
func AddBeltList(mux *http.ServeMux, c *coordinator.Coordinator) {
	mux.Handle("GET /api/v1/belts", JSONRoute(c, func(r *http.Request) (any, error) {
		search := ReadRouteSupportQuery("search", r.URL.RawQuery)
		filter := ReadRouteSupportQuery("filter", r.URL.RawQuery)
		searchText, filterText := "", "all"
		if search != nil {
			searchText = *search
		}
		if filter != nil {
			filterText = *filter
		}
		return api.NewBeltsResponseFromCoordinator(c, searchText, filterText), nil
	}))
}

// AddBeltDetail GET api/v1/belts/{id}.
// Lock: the handler calls coordinator methods, which take the lock.
func AddBeltDetail(mux *http.ServeMux, c *coordinator.Coordinator) {
	mux.Handle("GET /api/v1/belts/{id}", JSONRoute(c, func(r *http.Request) (any, error) {
		id, err := ReadRouteSupportID(r)
		if err != nil {
			return nil, err
		}
		record := c.ReadBelt(id)
		if record == nil {
			return nil, api.MissingResource{}
		}
		return api.NewBeltSnapshotFromCoordinator(*record, c), nil
	}))
}

// AddBeltDraft every POST
// api/v1/belts/draft/{op} (stage-add, stage-remove, stage-move, stage-update,
// fork-add, fork-remove, route-add, route-remove, route-update, trigger-add,
// trigger-remove, trigger-move, trigger-update, trigger-kind, repository-add,
// repository-remove, feed-into, policies-update, and goto-pick and goto-route,
// which the Operation(named:) lookup also accepts). The body is a draft and the
// result is the edited draft, so the editing rules stay in the daemon.
//
// Each operation is its own literal pattern rather than one `{op}` wildcard: a
// wildcard under belts/draft would overlap belts/{id}/pause and the other id
// routes, and ServeMux refuses overlapping patterns. An unknown operation is
// therefore an unknown route, answered 404 like MissingResource.
// Lock: the handler calls coordinator methods, which take the lock.
func AddBeltDraft(mux *http.ServeMux, c *coordinator.Coordinator) {
	for _, operation := range core.AllBeltDraftOperation {
		mux.Handle("POST /api/v1/belts/draft/"+string(operation), ErrorRoute(beltDraftRoute(c, operation)))
	}
}

// beltDraftRoute is the handler of one draft operation: the body must name that operation.
func beltDraftRoute(c *coordinator.Coordinator, operation core.BeltDraftOperation) RouteFunc {
	return guardCoordinator(c, func(w http.ResponseWriter, r *http.Request) error {
		body, err := ReadRouteSupportJSON[core.BeltDraftRequest](r, false)
		if err != nil {
			return err
		}
		if body.Op != operation {
			return ReadRouteSupportFail(fmt.Sprintf("The request body's op %s does not match %s.", body.Op, operation))
		}
		draft, stash, err := c.ApplyDraftResult(*body)
		if err != nil {
			return err
		}
		var help *core.PlaceholderHelp
		if body.HelpFor != nil {
			placeholders := c.PlaceholderHelp(draft, body.HelpFor)
			help = &placeholders
		}
		writeBeltDraft(w, c, draft, help, stash)
		return nil
	})
}

// writeBeltDraft answers with an edited draft, with the warnings and actions the coordinator gives it.
func writeBeltDraft(w http.ResponseWriter, c *coordinator.Coordinator, draft core.Belt, help *core.PlaceholderHelp, stash *core.TriggerStash) {
	JSONResponse(w, api.NewBeltDraftResponse(draft, c.Warnings(draft), c.DraftActions(draft), help, stash), http.StatusOK)
}

// AddBeltNew POST api/v1/belts/new.
// Lock: the handler calls coordinator methods, which take the lock.
func AddBeltNew(mux *http.ServeMux, c *coordinator.Coordinator) {
	mux.Handle("POST /api/v1/belts/new", CoordinatorRoute(c, func(w http.ResponseWriter, r *http.Request) error {
		if _, err := ReadRouteSupportJSON[api.BeltEmptyRequest](r, true); err != nil {
			return err
		}
		draft := c.NewBeltDraft()
		writeBeltDraft(w, c, draft, nil, nil)
		return nil
	}))
}

// AddBeltCopy POST api/v1/belts/copy.
// Lock: the handler calls coordinator methods, which take the lock.
func AddBeltCopy(mux *http.ServeMux, c *coordinator.Coordinator) {
	mux.Handle("POST /api/v1/belts/copy", CoordinatorRoute(c, func(w http.ResponseWriter, r *http.Request) error {
		body, err := ReadRouteSupportJSON[api.BeltDraftOnlyRequest](r, false)
		if err != nil {
			return err
		}
		draft := c.CopyBeltDraft(body.Draft)
		writeBeltDraft(w, c, draft, nil, nil)
		return nil
	}))
}

// AddBeltUpdateStage POST api/v1/belts/update-stage.
// Lock: the handler calls coordinator methods, which take the lock.
func AddBeltUpdateStage(mux *http.ServeMux, c *coordinator.Coordinator) {
	mux.Handle("POST /api/v1/belts/update-stage", CoordinatorRoute(c, func(w http.ResponseWriter, r *http.Request) error {
		body, err := ReadRouteSupportJSON[api.BeltUpdateStageRequest](r, false)
		if err != nil {
			return err
		}
		draft, err := c.UpdatingStageDraft(body.Draft, body.StageID, body.Index)
		if err != nil {
			return err
		}
		writeBeltDraft(w, c, draft, nil, nil)
		return nil
	}))
}

// AddBeltUpdateAllStages POST api/v1/belts/update-all-stages.
// Lock: the handler calls coordinator methods, which take the lock.
func AddBeltUpdateAllStages(mux *http.ServeMux, c *coordinator.Coordinator) {
	mux.Handle("POST /api/v1/belts/update-all-stages", CoordinatorRoute(c, func(w http.ResponseWriter, r *http.Request) error {
		body, err := ReadRouteSupportJSON[api.BeltDraftOnlyRequest](r, false)
		if err != nil {
			return err
		}
		draft, err := c.UpdatingAllStagesDraft(body.Draft)
		if err != nil {
			return err
		}
		writeBeltDraft(w, c, draft, nil, nil)
		return nil
	}))
}

// AddBeltSave POST api/v1/belts/save.
// Lock: the handler calls coordinator methods, which take the lock.
func AddBeltSave(mux *http.ServeMux, c *coordinator.Coordinator) {
	mux.Handle("POST /api/v1/belts/save", JSONRoute(c, func(r *http.Request) (any, error) {
		body, err := ReadRouteSupportJSON[api.BeltSaveRequest](r, false)
		if err != nil {
			return nil, err
		}
		record, err := c.SaveBeltCommand(body.Belt)
		if err != nil {
			return nil, err
		}
		return api.NewBeltResourceResponseFromCoordinator(record, c), nil
	}))
}

// AddBeltArchive POST
// api/v1/belts/{id}/archive and api/v1/belts/{id}/unarchive.
// Lock: the handler calls coordinator methods, which take the lock.
func AddBeltArchive(mux *http.ServeMux, c *coordinator.Coordinator) {
	for action, archived := range map[string]bool{"archive": true, "unarchive": false} {
		mux.Handle("POST /api/v1/belts/{id}/"+action, JSONRoute(c, func(r *http.Request) (any, error) {
			id, err := ReadRouteSupportID(r)
			if err != nil {
				return nil, err
			}
			if _, err := ReadRouteSupportJSON[api.BeltEmptyRequest](r, true); err != nil {
				return nil, err
			}
			return beltSetArchived(c, id, archived)
		}))
	}
}

// beltSetArchived is the shared archive and unarchive handler.
// The caller has read the optional empty body; this checks the belt exists, then runs the command.
// Lock: calls coordinator methods, which take the lock.
func beltSetArchived(c *coordinator.Coordinator, id string, archived bool) (api.BeltResourceResponse, error) {
	if c.ReadBelt(id) == nil {
		return api.BeltResourceResponse{}, api.MissingResource{}
	}
	record, err := c.ArchiveBeltCommand(id, archived)
	if err != nil {
		return api.BeltResourceResponse{}, err
	}
	return api.NewBeltResourceResponseFromCoordinator(record, c), nil
}

// AddBeltPause POST api/v1/belts/{id}/pause.
// Lock: the handler calls coordinator methods, which take the lock.
func AddBeltPause(mux *http.ServeMux, c *coordinator.Coordinator) {
	mux.Handle("POST /api/v1/belts/{id}/pause", beltSettingRoute(c, func(id string, body api.BeltPauseRequest) (store.BeltRecord, error) {
		return c.PauseBeltCommand(id, body.Paused)
	}))
}

// AddBeltAfk POST api/v1/belts/{id}/afk.
// Lock: the handler calls coordinator methods, which take the lock.
func AddBeltAfk(mux *http.ServeMux, c *coordinator.Coordinator) {
	mux.Handle("POST /api/v1/belts/{id}/afk", beltSettingRoute(c, func(id string, body api.BeltAfkRequest) (store.BeltRecord, error) {
		return c.AfkBeltCommand(id, body.Afk)
	}))
}

// AddBeltShadow POST api/v1/belts/{id}/shadow/save, /accept and /discard. Save takes the
// shadow's definition, whose id must be the belt's, and starts a test when none runs.
// Lock: the handler calls coordinator methods, which take the lock.
func AddBeltShadow(mux *http.ServeMux, c *coordinator.Coordinator) {
	mux.Handle("POST /api/v1/belts/{id}/shadow/save", beltSettingRoute(c, func(id string, body api.BeltSaveRequest) (store.BeltRecord, error) {
		if body.Belt.ID != id {
			return store.BeltRecord{}, ReadRouteSupportFail("The shadow's id " + body.Belt.ID + " is not the belt's id " + id + ".")
		}
		return c.SaveShadowCommand(body.Belt)
	}))
	mux.Handle("POST /api/v1/belts/{id}/shadow/accept", beltBodilessRoute(c, c.AcceptShadowCommand))
	mux.Handle("POST /api/v1/belts/{id}/shadow/discard", beltBodilessRoute(c, c.DiscardShadowCommand))
}

// beltBodilessRoute answers a command on an existing belt that takes no operands: the body
// may be absent or any object. 404 when the belt is missing.
func beltBodilessRoute(c *coordinator.Coordinator, command func(id string) (store.BeltRecord, error)) http.Handler {
	return JSONRoute(c, func(r *http.Request) (any, error) {
		id, err := ReadRouteSupportID(r)
		if err != nil {
			return nil, err
		}
		if _, err := ReadRouteSupportJSON[api.BeltEmptyRequest](r, true); err != nil {
			return nil, err
		}
		if c.ReadBelt(id) == nil {
			return nil, api.MissingResource{}
		}
		record, err := command(id)
		if err != nil {
			return nil, err
		}
		return api.NewBeltResourceResponseFromCoordinator(record, c), nil
	})
}

// beltSettingRoute answers a command on an existing belt: 404 when the belt is missing, else the
// belt as command leaves it, given the request body.
func beltSettingRoute[B any](c *coordinator.Coordinator, command func(id string, body B) (store.BeltRecord, error)) http.Handler {
	return JSONRoute(c, func(r *http.Request) (any, error) {
		id, err := ReadRouteSupportID(r)
		if err != nil {
			return nil, err
		}
		if c.ReadBelt(id) == nil {
			return nil, api.MissingResource{}
		}
		body, err := ReadRouteSupportJSON[B](r, false)
		if err != nil {
			return nil, err
		}
		record, err := command(id, *body)
		if err != nil {
			return nil, err
		}
		return api.NewBeltResourceResponseFromCoordinator(record, c), nil
	})
}

// AddBeltContext POST api/v1/belts/context.
// Lock: the handler calls coordinator methods, which take the lock.
func AddBeltContext(mux *http.ServeMux, c *coordinator.Coordinator) {
	mux.Handle("POST /api/v1/belts/context", JSONRoute(c, func(r *http.Request) (any, error) {
		body, err := ReadRouteSupportJSON[api.BeltContextRequest](r, false)
		if err != nil {
			return nil, err
		}
		context := c.BeltContext(body.Draft, body.StageID)
		if body.Shadow {
			context.ShadowDifferences = c.ShadowDifferences(body.Draft)
		}
		return context, nil
	}))
}

// AddBeltStageTemplate POST api/v1/belts/stage-template.
// Lock: the handler calls coordinator methods, which take the lock.
func AddBeltStageTemplate(mux *http.ServeMux, c *coordinator.Coordinator) {
	mux.Handle("POST /api/v1/belts/stage-template", JSONRoute(c, func(r *http.Request) (any, error) {
		body, err := ReadRouteSupportJSON[api.BeltStageTemplateRequest](r, false)
		if err != nil {
			return nil, err
		}
		stage, err := c.StageTemplate(body.Draft, body.Type, body.Agent, body.Route, body.Index)
		if err != nil {
			return nil, err
		}
		return api.BeltStageTemplateResponse{Stage: stage}, nil
	}))
}

// AddPlaceholders GET api/v1/placeholders.
// Lock: the handler calls coordinator methods, which take the lock.
func AddPlaceholders(mux *http.ServeMux, c *coordinator.Coordinator) {
	mux.Handle("GET /api/v1/placeholders", JSONRoute(c, func(r *http.Request) (any, error) {
		beltID := ReadRouteSupportQuery("belt", r.URL.RawQuery)
		if beltID == nil {
			return nil, ReadRouteSupportFail("A belt id is required.")
		}
		stage := ReadRouteSupportQuery("stage", r.URL.RawQuery)
		help := c.PlaceholderHelpByID(*beltID, stage)
		if help == nil {
			return nil, api.MissingResource{}
		}
		return api.PlaceholdersResponse{Placeholders: *help}, nil
	}))
}
