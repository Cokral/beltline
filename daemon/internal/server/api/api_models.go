package api

import (
	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/kit/coordinator"
)

// ModelActionResponse says whether one model command may run, and the
// daemon's reason when it may not. `refusalReason` is written as an explicit
// null when there is none.
type ModelActionResponse struct {
	Available     bool    `json:"available"`
	RefusalReason *string `json:"refusalReason"`
}

// NewModelActionResponse builds the model action response from its arguments.
func NewModelActionResponse(decision coordinator.ModelActionDecision) ModelActionResponse {
	return ModelActionResponse{Available: decision.Available, RefusalReason: core.CopyPointer(decision.RefusalReason)}
}

// ModelCandidateActionResponse is one candidate's allow/remove availability.
// `allowed` says whether it is currently admitted; a candidate absent from the
// catalog can still be removed.
type ModelCandidateActionResponse struct {
	ID            string  `json:"id"`
	Allowed       bool    `json:"allowed"`
	Available     bool    `json:"available"`
	RefusalReason *string `json:"refusalReason"`
}

// NewModelCandidateActionResponse builds the model candidate action response from its arguments.
func NewModelCandidateActionResponse(decision coordinator.ModelCandidateActionDecision) ModelCandidateActionResponse {
	return ModelCandidateActionResponse{
		ID:            decision.ID,
		Allowed:       decision.Allowed,
		Available:     decision.Available,
		RefusalReason: core.CopyPointer(decision.RefusalReason),
	}
}

// ModelActionsResponse are the commands the Allowed models card may send,
// named by the daemon rather than reconstructed by the client.
type ModelActionsResponse struct {
	Refresh    ModelActionResponse            `json:"refresh"`
	Candidates []ModelCandidateActionResponse `json:"candidates"`
}

// NewModelActionsResponse builds the model actions response from its arguments.
func NewModelActionsResponse(actions coordinator.ModelRunnerActions) ModelActionsResponse {
	candidates := make([]ModelCandidateActionResponse, 0, len(actions.Candidates))
	for _, candidate := range actions.Candidates {
		candidates = append(candidates, NewModelCandidateActionResponse(candidate))
	}
	return ModelActionsResponse{Refresh: NewModelActionResponse(actions.Refresh), Candidates: candidates}
}

// ModelListingResponse is one catalog model in the Allowed models list.
type ModelListingResponse struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Allowed bool   `json:"allowed"`
}

// NewModelListingResponse builds the model listing response from its arguments.
func NewModelListingResponse(listing coordinator.ModelListing) ModelListingResponse {
	return ModelListingResponse{ID: listing.ID, Label: listing.Label, Allowed: listing.Allowed}
}

// ModelRunnerResponse is one runner's models resource. Optional fields keep
// explicit nulls on the wire so a client can tell "no catalog" from "not
// reported": catalog, error and thinkingNote are written as null when nil.
type ModelRunnerResponse struct {
	Runner            core.Backend               `json:"runner"`
	Executable        string                     `json:"executable"`
	Catalog           *core.ModelCatalogSnapshot `json:"catalog"`
	Usable            bool                       `json:"usable"`
	Loading           bool                       `json:"loading"`
	Error             *string                    `json:"error"`
	AllowedModels     []string                   `json:"allowedModels"`
	Choices           []core.ModelChoice         `json:"choices"`
	UnavailableModels []string                   `json:"unavailableModels"`
	Thinking          []core.ThinkingChoice      `json:"thinking"`
	ThinkingNote      *string                    `json:"thinkingNote"`
	Listing           []ModelListingResponse     `json:"listing"`
	Actions           ModelActionsResponse       `json:"actions"`
}

// NewModelRunnerResponse builds the model runner response from its arguments.
func NewModelRunnerResponse(resource coordinator.ModelRunnerResource) ModelRunnerResponse {
	catalog := core.CopyPointer(resource.Catalog)
	listing := make([]ModelListingResponse, 0, len(resource.Listing))
	for _, item := range resource.Listing {
		listing = append(listing, NewModelListingResponse(item))
	}
	return ModelRunnerResponse{
		Runner:            resource.Runner,
		Executable:        resource.Executable,
		Catalog:           catalog,
		Usable:            resource.Usable,
		Loading:           resource.Loading,
		Error:             core.CopyPointer(resource.Error),
		AllowedModels:     append([]string{}, resource.AllowedModels...),
		Choices:           append([]core.ModelChoice{}, resource.Choices...),
		UnavailableModels: append([]string{}, resource.UnavailableModels...),
		Thinking:          append([]core.ThinkingChoice{}, resource.Thinking...),
		ThinkingNote:      core.CopyPointer(resource.ThinkingNote),
		Listing:           listing,
		Actions:           NewModelActionsResponse(resource.Actions),
	}
}
