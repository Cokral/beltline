// The run page's read-only facts: the action table and its context, the
// continue-session refusal, the erased-run check and the overrule offers. Each
// takes c.mu and reads; none starts work (coordinator_run_commands.go holds the
// commands that change a run).

package coordinator

import (
	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/runner"
	"slices"
)

// RunActionContext gathers the immutable facts the run page and the API decide
// with: checkout existence, frozen model choices and AFK decisions. Reading
// changes nothing and starts no work.
// Lock: exported entry point; takes c.mu.
func (c *Coordinator) RunActionContext(run core.Run) core.RunActionContext {
	c.Lock()
	defer c.EndTurn()
	return c.runActionContext(run)
}

// runActionContext is RunActionContext with c.mu held.
// Lock: runs with c.mu held. Suspends: none; adjustmentModels is read under the lock.
func (c *Coordinator) runActionContext(run core.Run) core.RunActionContext {
	current := run
	if latest := c.book.Run(run.ID); latest != nil {
		current = *latest
	}
	continueRefusals := map[string]string{}
	for _, attempt := range current.Attempts {
		if attempt.Session == nil {
			continue
		}
		if reason := c.continueSessionRefusal(current, attempt); reason != nil {
			continueRefusals[attempt.ID] = *reason
		}
	}
	adjustRefusals := map[string]string{}
	for _, stage := range current.Stages {
		if stage.Kind() != core.StageKindAgent {
			continue
		}
		if !core.StageAdjustCanAdjust(stage, current) {
			adjustRefusals[stage.ID] = "This stage cannot be adjusted."
			continue
		}
		hasModels := false
		for _, backend := range core.AllBackend {
			b := backend
			if len(c.adjustmentModels(current, stage, &b)) > 0 {
				hasModels = true
				break
			}
		}
		if !hasModels {
			adjustRefusals[stage.ID] = core.StageAdjustNoModelsAllowed
		}
	}
	return core.RunActionContext{
		Run:              current,
		GlobalPause:      c.globallyPaused,
		Busy:             watchesHas(c.book.busy, current.ID),
		Erasing:          c.book.isErasing(current.ID),
		ContinueRefusals: continueRefusals,
		AdjustRefusals:   adjustRefusals,
		Overrules:        c.overruleOffers(current),
		TmuxRunner:       c.isTmuxRunner(),
	}
}

// isTmuxRunner reports whether the configured runner is tmux-backed.
// Lock: none (pure).
func (c *Coordinator) isTmuxRunner() bool {
	_, ok := tmuxRunnerOf(c.runner)
	return ok
}

// tmuxRunnerOf returns the tmux runner behind r, in its value or pointer form.
// Lock: none (pure).
func tmuxRunnerOf(r runner.AgentRunner) (runner.TmuxRunner, bool) {
	switch value := r.(type) {
	case runner.TmuxRunner:
		return value, true
	case *runner.TmuxRunner:
		if value != nil {
			return *value, true
		}
	}
	return runner.TmuxRunner{}, false
}

// RunActions returns a run's ordered actions, for a resource or a control state.
// Lock: exported entry point; takes c.mu.
func (c *Coordinator) RunActions(run core.Run) []core.RunAction {
	c.Lock()
	defer c.EndTurn()
	return c.runActionContext(run).Actions()
}

// RunActionContextByID returns the context of a run by id, or nil when the run is gone.
// Lock: exported entry point; takes c.mu.
func (c *Coordinator) RunActionContextByID(id string) *core.RunActionContext {
	c.Lock()
	defer c.EndTurn()
	return c.runActionContextByID(id)
}

// runActionContextByID is RunActionContextByID with c.mu held.
// Lock: runs with c.mu held.
func (c *Coordinator) runActionContextByID(id string) *core.RunActionContext {
	run := c.book.Run(id)
	if run == nil {
		return nil
	}
	context := c.runActionContext(*run)
	return &context
}

// RequiredActionContext returns the context a command must have; a missing run is
// refused before any target is looked at.
// Lock: exported entry point; takes c.mu.
func (c *Coordinator) RequiredActionContext(id string) (core.RunActionContext, error) {
	c.Lock()
	defer c.EndTurn()
	return c.requiredActionContext(id)
}

// requiredActionContext is RequiredActionContext with c.mu held.
// Lock: runs with c.mu held.
func (c *Coordinator) requiredActionContext(id string) (core.RunActionContext, error) {
	context := c.runActionContextByID(id)
	if context == nil {
		return core.RunActionContext{}, core.NewValidationFailure(runGone, nil)
	}
	return *context, nil
}

// ErasedID reports whether id names an erased run, or one whose erasure is still
// in flight. Erase alone uses this before its generic missing-run check.
// Lock: exported entry point; takes c.mu.
func (c *Coordinator) ErasedID(id string) bool {
	c.Lock()
	defer c.EndTurn()
	if c.storage.NextGeneration(id) > 0 {
		return true
	}
	if _, pending := c.storage.PendingErasure(id); pending {
		return true
	}
	return c.book.isErasing(id)
}

// ContinueSessionRefusal returns why a saved session cannot be continued, or nil
// when it can. The same sentences gate the Continue button and answer a refused command.
// Lock: exported entry point; takes c.mu.
func (c *Coordinator) ContinueSessionRefusal(run core.Run, attempt core.Attempt) *string {
	c.Lock()
	defer c.EndTurn()
	return c.continueSessionRefusal(run, attempt)
}

// continueSessionRefusal is ContinueSessionRefusal with c.mu held.
// Lock: runs with c.mu held.
func (c *Coordinator) continueSessionRefusal(run core.Run, attempt core.Attempt) *string {
	found := slices.ContainsFunc(run.Attempts, func(saved core.Attempt) bool {
		return saved.ID == attempt.ID
	})
	if !found {
		text := core.RunAvailabilitySessionGone
		return &text
	}
	if _, _, err := c.sessions.continueEligibility(run, attempt); err != nil {
		text := err.Error()
		return &text
	}
	return nil
}

// OverruleOffers lists every applied AFK decision of the run, in stored order,
// with the agent destinations it may restart at.
// Lock: exported entry point; takes c.mu.
func (c *Coordinator) OverruleOffers(run core.Run) []core.RunOverruleOffer {
	c.Lock()
	defer c.EndTurn()
	return c.overruleOffers(run)
}

// overruleOffers is OverruleOffers with c.mu held.
// Lock: runs with c.mu held.
func (c *Coordinator) overruleOffers(run core.Run) []core.RunOverruleOffer {
	var offers []core.RunOverruleOffer
	for _, decision := range c.autopilot.DecisionsFor(run.ID) {
		if decision.Status != core.AfkDecisionStatusApplied {
			continue
		}
		var current *core.Run
		if found := c.book.Run(run.ID); found != nil {
			current = found
		}
		offer, err := core.AfkOfferFor(decision, current)
		if err == nil {
			offers = append(offers, offerOf(decision, offer))
			continue
		}
		refusal, _ := err.(core.AfkRefusal)
		if refusal.Kind == core.AfkRefusalKindIneligible {
			text := core.AfkOverruledAlready
			offers = append(offers, core.RunOverruleOffer{Decision: decision.ID, Refusal: &text})
			continue
		}
		why := refusal.Message
		offers = append(offers, core.RunOverruleOffer{Decision: decision.ID, Attempt: linkedAttempt(decision, run), Refusal: &why})
	}
	return offers
}

// offerOf is the overrule record of an offer the decision may use.
// Lock: none (pure).
func offerOf(decision core.AfkDecision, offer core.AfkOffer) core.RunOverruleOffer {
	destinations := make([]core.RunOverruleDestination, 0, len(offer.Choices))
	for _, choice := range offer.Choices {
		destinations = append(destinations, core.RunOverruleDestination{Stage: choice.StageID, Label: choice.Label()})
	}
	out := core.RunOverruleOffer{Decision: decision.ID, Attempt: offer.AttemptID, Destinations: destinations}
	if offer.Previous != nil {
		previous := offer.Previous.StageID
		out.Previous = &previous
	}
	return out
}
