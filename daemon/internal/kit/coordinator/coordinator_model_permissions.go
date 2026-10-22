package coordinator

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/robbertmanders/beltline/daemon/internal/core"
)

// AllowedModels lists the models a runner's permissions allow, filtered by search.
// Lock: exported entry point; takes c.mu.
func (c *Coordinator) AllowedModels(runner core.Backend, search string) []core.ModelChoice {
	c.Lock()
	defer c.EndTurn()
	return c.settings.AllowedChoices(runner, search, c.env)
}

// SaveAssistantConfiguration saves the assistant's backend, model and thinking
// level, checked against the allowed models.
// Lock: exported entry point; it takes c.mu through ConfigureAssistant, so it does not lock
// itself. Writes: publishes settings.
func (c *Coordinator) SaveAssistantConfiguration(backend core.Backend, model string, thinking *core.Thinking) error {
	_, err := c.ConfigureAssistant(AssistantConfiguration{Backend: backend, Model: model, Thinking: thinking})
	return err
}

// SetModelAllowed allows or removes one model for a runner. It persists before
// publishing, so an unsuccessful write never changes the effective permissions.
// Removal also works for missing catalog entries.
// Lock: exported entry point; takes c.mu. Writes: publishes settings.
func (c *Coordinator) SetModelAllowed(identifier string, runner core.Backend, allowed bool) error {
	c.Lock()
	defer c.EndTurn()
	return c.setModelAllowedNow(identifier, runner, allowed)
}

// setModelAllowedNow is SetModelAllowed with c.mu held.
// Lock: runs with c.mu held. Writes: publishes settings.
func (c *Coordinator) setModelAllowedNow(identifier string, runner core.Backend, allowed bool) error {
	if strings.TrimSpace(identifier) == "" {
		return core.NewFailure("The runner's default — choose an allowed model.")
	}
	if allowed {
		snapshot, ok := c.settings.ModelCatalogs[string(runner)]
		if !ok || !snapshot.IsUsable(runner, runner.Executable(c.env)) || !catalogListsModel(snapshot, identifier) {
			return core.NewFailure(fmt.Sprintf(
				"%s is unavailable in catalog for %s. Refresh its catalog in Settings → Allowed models.", identifier, runner.Label()))
		}
	}
	draft := c.settings
	draft.AllowedModels = copyAllowedModels(c.settings.AllowedModels)
	set := draft.AllowedModels[string(runner)]
	if set == nil {
		set = map[string]struct{}{}
	}
	if allowed {
		set[identifier] = struct{}{}
	} else {
		delete(set, identifier)
	}
	draft.AllowedModels[string(runner)] = set
	return c.publishSettings(draft)
}

// SaveModelCatalog accepts only a complete successful response for the current
// executable. Async discovery must additionally reject superseded request identities.
// Lock: exported entry point; takes c.mu. Writes: publishes settings.
func (c *Coordinator) SaveModelCatalog(catalog core.ModelCatalogSnapshot) error {
	c.Lock()
	defer c.EndTurn()
	return c.saveModelCatalogNow(catalog)
}

// saveModelCatalogNow is SaveModelCatalog with c.mu held.
// Lock: runs with c.mu held. Writes: publishes settings.
func (c *Coordinator) saveModelCatalogNow(catalog core.ModelCatalogSnapshot) error {
	if !catalog.IsUsable(catalog.Runner, catalog.Runner.Executable(c.env)) {
		return core.NewFailure(fmt.Sprintf(
			"The executable changed while discovering %s models. Refresh again.", catalog.Runner.Label()))
	}
	draft := c.settings
	draft.ModelCatalogs = copyModelCatalogs(c.settings.ModelCatalogs)
	draft.ModelCatalogs[string(catalog.Runner)] = catalog
	return c.publishSettings(draft)
}

// catalogListsModel reports whether the catalog lists a model with identifier. Lock: none (pure).
func catalogListsModel(snapshot core.ModelCatalogSnapshot, identifier string) bool {
	return slices.ContainsFunc(snapshot.Models, func(model core.ModelChoice) bool {
		return model.ID == identifier
	})
}

// copyAllowedModels copies the allowed identifiers of every runner, so a draft never shares a set
// with the saved settings. Lock: none (pure).
func copyAllowedModels(in map[string]map[string]struct{}) map[string]map[string]struct{} {
	out := make(map[string]map[string]struct{}, len(in))
	for runner, set := range in {
		out[runner] = core.CopySet(set)
	}
	return out
}

// copyModelCatalogs copies the catalog of every runner (its models are immutable once saved). Lock: none (pure).
func copyModelCatalogs(in map[string]core.ModelCatalogSnapshot) map[string]core.ModelCatalogSnapshot {
	out := make(map[string]core.ModelCatalogSnapshot, len(in))
	maps.Copy(out, in)
	return out
}
