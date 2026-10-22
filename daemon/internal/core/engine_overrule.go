package core

// AfkTarget is an agent stage the person can send a run back to.
type AfkTarget struct {
	StageID   string
	StageName string
	// RouteID and RouteName are nil when the stage is on the main line.
	RouteID   *string
	RouteName *string
}

// Label is the stage name, with its route after " · " when it has one.
func (t AfkTarget) Label() string {
	if t.RouteName != nil {
		return t.StageName + " · " + *t.RouteName
	}
	return t.StageName
}

// AfkRefusalKind is which case of AfkRefusal a value is.
type AfkRefusalKind string

const (
	// AfkRefusalKindIneligible: the decision is not one that can be overruled.
	AfkRefusalKindIneligible AfkRefusalKind = "ineligible"
	// AfkRefusalKindUnavailable: the decision can be overruled in principle, but not now; Message says why.
	AfkRefusalKindUnavailable AfkRefusalKind = "unavailable"
)

// AfkRefusal is why an applied decision cannot be overruled. Other decisions
// offer no action. It is the error of AfkOfferFor. It is a struct with a
// Kind, and a Message for the unavailable case.
type AfkRefusal struct {
	Kind    AfkRefusalKind
	Message string
}

// Error is the refusal's message; empty for ineligible.
func (r AfkRefusal) Error() string {
	if r.Kind == AfkRefusalKindIneligible {
		return ""
	}
	return r.Message
}

// AfkOffer is what the overrule dialog needs, once a decision can be overruled.
type AfkOffer struct {
	// Previous is the agent to restart by default. Nil when none ran before the decision.
	Previous *AfkTarget
	// Choices is every agent stage in the run's saved belt, in belt order.
	Choices       []AfkTarget
	AttemptID     string
	SubjectRef    string
	SubjectTitle  string
	DecisionStage string
	CurrentStage  string
}

// AfkOverruleWarning is the sentence the overrule dialog shows about what stops.
const AfkOverruleWarning = "Current work will stop. Later stages may run again. Existing code, pushes, pull requests and comments are not undone."

// AfkOverruleAfkNotice is the sentence the overrule dialog shows about AFK mode.
const AfkOverruleAfkNotice = "AFK mode stays as configured."

// AfkOverruledAlready is the refusal for an applied decision that has already been overruled, which
// the run page and the overrule sheet both show.
const AfkOverruledAlready = "This decision was already overruled."

// AfkDecisionGone is the refusal when an AFK decision id no longer names a saved decision. The
// overrule commands, the AFK log and the run page's decision offers all refuse with it.
const AfkDecisionGone = "The AFK decision is no longer here."

// AfkOverruleNoAgentStage is the refusal when an overrule names a stage that is not an agent stage of
// the belt. The overrule command and the saved overrule's finish both refuse with it.
const AfkOverruleNoAgentStage = "Choose an agent stage in this belt."

// AfkOverruleFinished is the refusal when the run is no longer active by the time an overrule would
// restart it. The overrule command and its finish step both refuse with it.
const AfkOverruleFinished = "This run has finished and cannot be overruled."

// AfkOfferFor is whether decision can be overruled on run, and where it would
// restart. Returns an AfkRefusal as the error when it cannot.
// Run may be nil.
func AfkOfferFor(decision AfkDecision, run *Run) (AfkOffer, error) {
	if decision.Status != AfkDecisionStatusApplied || decision.Overrule != nil {
		return AfkOffer{}, AfkRefusal{Kind: AfkRefusalKindIneligible}
	}
	unavailable := func(message string) (AfkOffer, error) {
		return AfkOffer{}, AfkRefusal{Kind: AfkRefusalKindUnavailable, Message: message}
	}
	if run == nil {
		return unavailable("The run is no longer here.")
	}
	if run.State == RunStateShipped {
		return unavailable("This run has shipped and cannot be overruled.")
	}
	if run.State == RunStateDone {
		return unavailable(AfkOverruleFinished)
	}
	if run.State == RunStateDropped {
		return unavailable("This run was dropped and cannot be overruled.")
	}
	if !run.State.IsActive() {
		return unavailable("This run cannot be overruled.")
	}
	var linked *Attempt
	for i := range run.Attempts {
		if run.Attempts[i].AfkDecision != nil && *run.Attempts[i].AfkDecision == decision.ID {
			linked = &run.Attempts[i]
			break
		}
	}
	if linked == nil {
		return unavailable("The original attempt is missing, so this decision cannot be overruled.")
	}
	choices := AfkAgentTargets(*run)
	if len(choices) == 0 {
		return unavailable("This belt has no agent stage to restart.")
	}
	currentStage := run.StageID
	if stage := run.Stage(); stage != nil {
		currentStage = stage.Name
	}
	return AfkOffer{
		Previous: AfkPreviousAgent(decision, *run), Choices: choices, AttemptID: linked.ID,
		SubjectRef: decision.SubjectRef, SubjectTitle: decision.SubjectTitle,
		DecisionStage: decision.StageName, CurrentStage: currentStage,
	}, nil
}

// AfkPreviousAgent is the agent attempt the decision belongs to, or the latest
// agent attempt that started before the decision. An attempt that started later
// is never chosen.
func AfkPreviousAgent(decision AfkDecision, run Run) *AfkTarget {
	var linked *Attempt
	for i := range run.Attempts {
		if run.Attempts[i].AfkDecision != nil && *run.Attempts[i].AfkDecision == decision.ID {
			linked = &run.Attempts[i]
			break
		}
	}
	if linked == nil {
		return nil
	}
	if stage := nestedStageOf(run, linked.StageID); stage != nil && stage.Kind() == StageKindAgent {
		target := afkTarget(*stage, run)
		return &target
	}
	best := -1
	for i := range run.Attempts {
		attempt := run.Attempts[i]
		if !attempt.StartedAt.Before(decision.StartedAt.Time) {
			continue
		}
		stage := nestedStageOf(run, attempt.StageID)
		if stage == nil || stage.Kind() != StageKindAgent {
			continue
		}
		// Later starts win; on a tie the later attempt in the list wins.
		if best < 0 || !run.Attempts[i].StartedAt.Before(run.Attempts[best].StartedAt.Time) {
			best = i
		}
	}
	if best < 0 {
		return nil
	}
	stage := nestedStageOf(run, run.Attempts[best].StageID)
	if stage == nil {
		return nil
	}
	target := afkTarget(*stage, run)
	return &target
}

// AfkAgentTargets are the agent stages in the run's saved belt, in belt order,
// including other routes.
func AfkAgentTargets(run Run) []AfkTarget {
	var targets []AfkTarget
	for _, stage := range run.Stages {
		if stage.Kind() == StageKindAgent {
			targets = append(targets, afkTarget(stage, run))
		}
	}
	return targets
}

// afkTarget is the target for stage in run.
func afkTarget(stage Stage, run Run) AfkTarget {
	target := AfkTarget{StageID: stage.ID, StageName: stage.Name}
	if place := run.Layout().Place(stage.ID); place != nil {
		routeID := place.Route.ID
		routeName := place.Route.Name
		target.RouteID = &routeID
		target.RouteName = &routeName
	}
	return target
}
