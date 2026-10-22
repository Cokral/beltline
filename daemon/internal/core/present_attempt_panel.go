package core

import "strconv"

// AttemptPanelContentKind is which content the attempt panel shows.
type AttemptPanelContentKind string

const (
	AttemptPanelContentRunning  AttemptPanelContentKind = "running"
	AttemptPanelContentDecision AttemptPanelContentKind = "decision"
	AttemptPanelContentAttempt  AttemptPanelContentKind = "attempt"
	AttemptPanelContentEmpty    AttemptPanelContentKind = "empty"
)

// AttemptPanelContent is what the attempt panel shows. Attempt and More are set only for
// the attempt kind; More is the open attempt's number when a later one is shown.
type AttemptPanelContent struct {
	Kind    AttemptPanelContentKind
	Attempt *Attempt
	More    *int
}

// AttemptPanelAttempts are the attempts at stage in run.
func AttemptPanelAttempts(stage string, run Run) []Attempt {
	attempts := []Attempt{}
	for _, attempt := range run.Attempts {
		if attempt.StageID == stage {
			attempts = append(attempts, attempt)
		}
	}
	return attempts
}

// AttemptPanelShown is the attempt shown. An explicit history choice wins; nil follows
// the open or latest attempt. IDs are local to a run: callers must clear their choice
// on navigation.
func AttemptPanelShown(attempts []Attempt, picking *string) *Attempt {
	if picking != nil {
		for i := range attempts {
			if attempts[i].ID == *picking {
				shown := attempts[i]
				return &shown
			}
		}
	}
	if live := lastOpenAttempt(attempts); live != nil {
		shown := *live
		return &shown
	}
	if len(attempts) > 0 {
		shown := attempts[len(attempts)-1]
		return &shown
	}
	return nil
}

// AttemptPanelSessions are the attempts with a session, in stage order.
func AttemptPanelSessions(run Run) []Attempt {
	sessions := []Attempt{}
	for _, stage := range run.Stages {
		for _, attempt := range AttemptPanelAttempts(stage.ID, run) {
			if attempt.Session != nil {
				sessions = append(sessions, attempt)
			}
		}
	}
	return sessions
}

// AttemptPanelCanContinue is nil when Continue is allowed; otherwise the reason the
// terminal bar shows.
func AttemptPanelCanContinue(run Run, attempt Attempt, directoryExists bool) *string {
	if run.Pause != nil && run.Pause.BlocksProgress() {
		return new("Paused by you")
	}
	if run.State == RunStateShipped {
		return new("the run is shipped")
	}
	if run.State == RunStateDone {
		return new("the run is done")
	}
	if run.State == RunStateDropped {
		return new("the run is dropped")
	}
	if run.Parking != nil {
		return new("the run is parked")
	}
	if !AttemptPanelBelongsToCurrentStage(run, attempt) {
		name := attempt.StageID
		for _, stage := range run.Stages {
			if stage.ID == attempt.StageID {
				name = stage.Name
				break
			}
		}
		return new("the run has left " + name)
	}
	if !directoryExists {
		return new("the run's checkout is gone")
	}
	return nil
}

// AttemptPanelBelongsToCurrentStage reports whether attempt is the run's current
// attempt at its stage, on a run that has not finished.
func AttemptPanelBelongsToCurrentStage(run Run, attempt Attempt) bool {
	current := AttemptPanelCurrentID(run)
	return current != nil && *current == attempt.ID
}

// AttemptPanelCurrentID is the id of the run's current attempt at its stage, or nil when the
// run has finished or has no attempt there. A pass that asks about every attempt of a run
// asks once for this, as AttemptPanelBelongsToCurrentStage copies the attempt it finds.
func AttemptPanelCurrentID(run Run) *string {
	if !run.State.IsActive() {
		return nil
	}
	// The last attempt at the run's stage, as LastAttempt finds it, read without the copy.
	for i := len(run.Attempts) - 1; i >= 0; i-- {
		if run.Attempts[i].StageID == run.StageID {
			id := run.Attempts[i].ID
			return &id
		}
	}
	return nil
}

// AttemptPanelContentFor is what the panel shows for attempts with picking chosen and
// step the stage's status step.
func AttemptPanelContentFor(attempts []Attempt, picking *string, step StatusStep) AttemptPanelContent {
	shown := AttemptPanelShown(attempts, picking)
	if shown == nil {
		return AttemptPanelContent{Kind: AttemptPanelContentEmpty}
	}
	live := lastOpenAttempt(attempts)
	if live != nil && shown.ID == live.ID {
		if step == StatusStepRunning {
			return AttemptPanelContent{Kind: AttemptPanelContentRunning}
		}
		if step == StatusStepWaiting {
			return AttemptPanelContent{Kind: AttemptPanelContentDecision}
		}
	}
	var more *int
	if live != nil && live.ID != shown.ID {
		number := live.Number
		more = &number
	}
	return AttemptPanelContent{Kind: AttemptPanelContentAttempt, Attempt: shown, More: more}
}

// AttemptPanelCaption is "Attempt 2 of 3".
func AttemptPanelCaption(attempt Attempt, attempts []Attempt) string {
	return "Attempt " + strconv.Itoa(attempt.Number) + " of " + strconv.Itoa(len(attempts))
}

// AttemptPanelRanOn is the runner an agent attempt ran on, and its model when the stage
// spreads its sessions across several. label names a model; a nil result keeps the
// model's own id.
func AttemptPanelRanOn(attempt *Attempt, stage Stage, label func(model string, runner Backend) *string) string {
	var settings *SessionSettings
	if attempt != nil {
		settings = attempt.Settings
	}
	runner := ValueOr(stage.Runner, BackendPi)
	if settings != nil && settings.Runner != nil {
		runner = *settings.Runner
	}
	if len(stage.Pool()) <= 1 || settings == nil || settings.Model == "" {
		return runner.Label()
	}
	model := settings.Model
	shown := model
	if label != nil {
		if named := label(model, runner); named != nil {
			shown = *named
		}
	}
	return runner.Label() + " · " + shown
}

// lastOpenAttempt is the last attempt in attempts that has not ended, or nil.
func lastOpenAttempt(attempts []Attempt) *Attempt {
	for i := len(attempts) - 1; i >= 0; i-- {
		if attempts[i].IsOpen() {
			return &attempts[i]
		}
	}
	return nil
}
