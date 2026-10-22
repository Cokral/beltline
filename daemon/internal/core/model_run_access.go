// The accessors of Run: its stage
// and attempts, its legs and focus, and its folder, requested base and parking.

package core

import "slices"
import "strconv"

// Stage is the stage the run is at, or nil.
func (r Run) Stage() *Stage {
	index := r.StageIndex()
	if index == nil {
		return nil
	}
	stage := r.Stages[*index].Clone()
	return &stage
}

// stageRef is the run's stage like Stage, but the run's own rather than a copy, so a read that only
// looks at it does not deep-copy the stage on every call. Callers read it and never change it.
func (r Run) stageRef() *Stage {
	for i := range r.Stages {
		if r.Stages[i].ID == r.StageID {
			return &r.Stages[i]
		}
	}
	return nil
}

// StageIndex is the position of the run's stage in Stages, or nil.
func (r Run) StageIndex() *int {
	for i := range r.Stages {
		if r.Stages[i].ID == r.StageID {
			index := i
			return &index
		}
	}
	return nil
}

// focusLegID is the id of the focus leg, or nil when there is none.
func (r Run) focusLegID() *string {
	focus := r.Focus()
	if focus == nil {
		return nil
	}
	return &focus.ID
}

// CurrentAttempt is the open attempt at the current stage and focus leg, or nil.
func (r Run) CurrentAttempt() *Attempt {
	key := AttemptKey(r.StageID, r.focusLegID())
	for i := len(r.Attempts) - 1; i >= 0; i-- {
		attempt := r.Attempts[i].Clone()
		if AttemptKey(attempt.StageID, attempt.Leg) == key && attempt.IsOpen() {
			return &attempt
		}
	}
	return nil
}

// LastAttempt is the last attempt at stageID, or nil.
func (r Run) LastAttempt(stageID string) *Attempt {
	for i := len(r.Attempts) - 1; i >= 0; i-- {
		if r.Attempts[i].StageID == stageID {
			attempt := r.Attempts[i].Clone()
			return &attempt
		}
	}
	return nil
}

// LegAt is the leg working at a stage, when the run is working it.
func (r Run) LegAt(stageID *string) *Leg {
	if stageID == nil {
		return nil
	}
	for i := range r.Legs {
		leg := cloneValue(r.Legs[i])
		if leg.StageID != nil && *leg.StageID == *stageID && !leg.IsFinished() {
			return &leg
		}
	}
	if focus := r.Focus(); focus != nil && focus.StageID != nil && *focus.StageID == *stageID {
		return focus
	}
	return nil
}

// ClosedAttempts is the earlier work available when you send the run to this
// stage, optionally on one leg.
func (r Run) ClosedAttempts(stageID string, legID *string) []Attempt {
	var out []Attempt
	want := LegFromID(legID)
	for _, attempt := range r.Attempts {
		if attempt.StageID != stageID || attempt.IsOpen() {
			continue
		}
		if legID != nil && !SamePointee(attempt.Leg, want) {
			continue
		}
		out = append(out, attempt)
	}
	return out
}

// ReachedWatch reports whether the run has been at a watch stage: from then on
// its pull request, not what it is about, decides how it ends.
func (r Run) ReachedWatch() bool {
	watches := map[string]struct{}{}
	for _, stage := range r.Stages {
		if stage.Kind() == StageKindWatch {
			watches[stage.ID] = struct{}{}
		}
	}
	for _, attempt := range r.Attempts {
		if _, ok := watches[attempt.StageID]; ok {
			return true
		}
	}
	return false
}

// ChangeCurrentAttempt changes the open attempt at the current stage, if there
// is one.
func (r *Run) ChangeCurrentAttempt(change func(*Attempt)) {
	key := AttemptKey(r.StageID, r.focusLegID())
	for i := len(r.Attempts) - 1; i >= 0; i-- {
		attempt := &r.Attempts[i]
		if AttemptKey(attempt.StageID, attempt.Leg) == key && attempt.IsOpen() {
			change(attempt)
			return
		}
	}
}

// AttemptByID is the attempt with id, or nil.
func (r Run) AttemptByID(id *string) *Attempt {
	if id == nil {
		return nil
	}
	for i := range r.Attempts {
		if r.Attempts[i].ID == *id {
			attempt := r.Attempts[i]
			return &attempt
		}
	}
	return nil
}

// LegByID is the leg with id, or nil.
func (r Run) LegByID(id *string) *Leg {
	if id == nil {
		return nil
	}
	for i := range r.Legs {
		if r.Legs[i].ID == *id {
			leg := r.Legs[i]
			return &leg
		}
	}
	return nil
}

// StageIndexOf is the position in Stages of the stage with id, or -1.
func (r Run) StageIndexOf(id string) int {
	for i := range r.Stages {
		if r.Stages[i].ID == id {
			return i
		}
	}
	return -1
}

// HasAgentStage reports whether the run has an agent stage with id.
func (r Run) HasAgentStage(id string) bool {
	return slices.ContainsFunc(r.Stages, func(stage Stage) bool {
		return stage.ID == id && stage.Kind() == StageKindAgent
	})
}

// LegIndexOf is the position in Legs of the leg with id, or -1.
func (r Run) LegIndexOf(id string) int {
	for i := range r.Legs {
		if r.Legs[i].ID == id {
			return i
		}
	}
	return -1
}

// JoinIndexOf is the position in Joins of the join with id, or -1.
func (r Run) JoinIndexOf(id string) int {
	for i := range r.Joins {
		if r.Joins[i].ID == id {
			return i
		}
	}
	return -1
}

// HasLeg reports whether the run has a leg with id.
func (r Run) HasLeg(id string) bool {
	return r.LegIndexOf(id) >= 0
}

// Focus is the leg the run's own stage, state and note speak for: the first leg
// still going, or the last one when every leg has finished.
func (r Run) Focus() *Leg {
	for i := range r.Legs {
		if r.Legs[i].State.IsActive() {
			leg := r.Legs[i]
			return &leg
		}
	}
	if len(r.Legs) == 0 {
		return nil
	}
	last := r.Legs[len(r.Legs)-1]
	return &last
}

// LegOn is the leg for a route, or the single leg on the line when route is
// nil.
func (r Run) LegOn(route *string) *Leg {
	if route == nil {
		if len(r.Legs) == 1 {
			leg := r.Legs[0]
			return &leg
		}
		for i := range r.Legs {
			if r.Legs[i].Route == nil {
				leg := r.Legs[i]
				return &leg
			}
		}
		return nil
	}
	for i := range r.Legs {
		if r.Legs[i].Route != nil && *r.Legs[i].Route == *route {
			leg := r.Legs[i]
			return &leg
		}
	}
	return nil
}

// WorkingLegs are the legs as the run's own stage and state speak for them.
func (r Run) WorkingLegs() []Leg {
	if r.IsSplit() || len(r.Legs) == 0 {
		return cloneEach(r.Legs, cloneValue[Leg])
	}
	focus := r.Legs[0]
	stageID := r.StageID
	return []Leg{{
		ID: focus.ID, Route: focus.Route, StageID: &stageID, State: r.State, Reason: r.Reason, Note: r.Note,
		Continues: r.Continues, Replays: r.Replays, Branch: focus.Branch,
	}}
}

// ActiveLegs are the legs still going, which is what holds a run at a fork's
// join.
func (r Run) ActiveLegs() []Leg {
	var out []Leg
	for _, leg := range r.Legs {
		if !leg.IsFinished() {
			out = append(out, leg)
		}
	}
	return out
}

// WaitingForRoutes are the routes the run still has to finish before the routes
// meet.
func (r Run) WaitingForRoutes(layout BeltLayout) []string {
	if !r.IsSplit() {
		return nil
	}
	var out []string
	for _, leg := range r.ActiveLegs() {
		if name := leg.RouteName(layout); name != nil {
			out = append(out, *name)
		}
	}
	return out
}

// Answerable is the held agent attempt you can answer: its session continues
// with your answer instead of starting over. Nil when there is none.
func (r Run) Answerable() *Attempt {
	if r.State != RunStateHeld {
		return nil
	}
	last := r.LastAttempt(r.StageID)
	if last == nil || last.IsOpen() || last.SessionID == nil {
		return nil
	}
	return last
}

// Folder is the run's folder name inside the runs directory.
func (r Run) Folder() string {
	switch r.Subject.Kind {
	case SubjectKindIssue:
		return r.BeltID + "/" + r.Subject.Repo + "/" + strconv.Itoa(r.Subject.Number)
	case SubjectKindPullRequest:
		return r.BeltID + "/" + r.Subject.Repo + "/pr-" + strconv.Itoa(r.Subject.Number) + "-" + ValueOf(r.Subject.Occurrence)
	case SubjectKindJira, SubjectKindManual, SubjectKindScheduled:
		return r.BeltID + "/" + string(r.Subject.Kind) + "/" + r.Subject.Key
	}
	return ""
}

// RequestedBase is the branch asked for in repo, first match wins: a pull
// request's own base, the base: label the run started with, the repository's
// override from the belt, then the belt's shared base. Nil means the repository's
// default; a requested branch that is missing there falls back to that default,
// not to a lower choice.
func (r Run) RequestedBase(repo string) *string {
	if r.Subject.Base != nil && *r.Subject.Base != "" {
		base := *r.Subject.Base
		return &base
	}
	if r.BaseFromLabel != nil && *r.BaseFromLabel && r.Base != "" {
		base := r.Base
		return &base
	}
	if override := RepositoryBasesClean(r.RepositoryBases)[repo]; override != "" {
		return &override
	}
	if r.Base == "" {
		return nil
	}
	base := r.Base
	return &base
}

// CanButtonPark reports whether Park is offered: some unfinished route is not
// already parked. Finished and dropped runs, and a run
// parked only by an outcome, are not.
func (r Run) CanButtonPark() bool {
	if r.Parking != nil || !r.State.IsActive() {
		return false
	}
	return slices.ContainsFunc(r.WorkingLegs(), func(leg Leg) bool {
		return leg.State.IsActive() && leg.State != RunStateParked
	})
}
