package core

import "slices"

// AfkLogFilter is the AFK page's filter: those you still need to look at, or all of them.
type AfkLogFilter string

const (
	AfkLogFilterReview AfkLogFilter = "review"
	AfkLogFilterAll    AfkLogFilter = "all"
)

// Copy used by AfkLog. Literal text is exact.
const (
	afkLogDetailSeparator = ": "
)

// AfkLogRows is the decisions to list, newest first: those you still need to look at, or all
// of them. A decision still being made shows in both, and one the run outgrew only in all.
func AfkLogRows(decisions []AfkDecision, filter AfkLogFilter) []AfkDecision {
	rows := []AfkDecision{}
	for i := len(decisions) - 1; i >= 0; i-- {
		decision := decisions[i]
		if filter == AfkLogFilterAll || decision.NeedsReview() || decision.Status == AfkDecisionStatusDeciding {
			rows = append(rows, decision)
		}
	}
	return rows
}

// AfkLogLabel is the list label of a decision: its overrule when it has one, else its status.
func AfkLogLabel(decision AfkDecision) string {
	if decision.Overrule != nil {
		if decision.Overrule.Phase == AfkOverrulePhaseHeld {
			return "HELD"
		}
		return "OVERRULED"
	}
	return AfkLogStatusLabel(decision.Status)
}

// AfkLogStatusLabel is the list label of a decision status.
func AfkLogStatusLabel(status AfkDecisionStatus) string {
	switch status {
	case AfkDecisionStatusDeciding:
		return "DECIDING"
	case AfkDecisionStatusApplied:
		return "DONE"
	case AfkDecisionStatusLeftForYou:
		return "LEFT FOR YOU"
	case AfkDecisionStatusFailed:
		return "FAILED"
	case AfkDecisionStatusSuperseded:
		return "NOT NEEDED"
	}
	return ""
}

// AfkLogTone is the tone of a decision: its overrule's when it has one, else its status'.
func AfkLogTone(decision AfkDecision) Tone {
	if decision.Overrule != nil {
		if decision.Overrule.Phase == AfkOverrulePhaseHeld {
			return ToneProblem
		}
		return ToneYou
	}
	return AfkLogStatusTone(decision.Status)
}

// AfkLogStatusTone is the tone of a decision status.
func AfkLogStatusTone(status AfkDecisionStatus) Tone {
	switch status {
	case AfkDecisionStatusDeciding:
		return ToneNeutral
	case AfkDecisionStatusApplied:
		return ToneGood
	case AfkDecisionStatusLeftForYou:
		return ToneYou
	case AfkDecisionStatusFailed:
		return ToneProblem
	case AfkDecisionStatusSuperseded:
		return ToneNeutral
	}
	return ToneNeutral
}

// AfkLogOverruleLine is the overrule line under the original headline; nil when the decision
// was not overruled. Comments stay on the decision itself.
func AfkLogOverruleLine(decision AfkDecision) *string {
	overrule := decision.Overrule
	if overrule == nil {
		return nil
	}
	var line string
	if overrule.Phase == AfkOverrulePhaseHeld {
		line = "Overrule held at " + overrule.StageLabel()
		if overrule.Detail != nil {
			line += afkLogDetailSeparator + *overrule.Detail
		}
	} else {
		line = "Overruled, restarting " + overrule.StageLabel()
	}
	return &line
}

// AfkLogStageExists says whether the run still has the stage the decision was made at. A nil run has none.
func AfkLogStageExists(decision AfkDecision, run *Run) bool {
	if run == nil {
		return false
	}
	return slices.ContainsFunc(run.Stages, func(stage Stage) bool { return stage.ID == decision.StageID })
}

// AfkLogRevealUnavailable is why "Show session files" is unavailable, or nil when the decision's
// session folder is on disk. A decision that kept no folder says so; one whose folder is gone says that.
func AfkLogRevealUnavailable(decision AfkDecision, folderExists bool) *string {
	if folderExists {
		return nil
	}
	reason := "Its session files are no longer on disk."
	if decision.Folder == "" {
		reason = "This decision kept no session files."
	}
	return &reason
}

// AfkLogHeadline is what the decision did, in one line, such as "Chose “approved” at Approve"
// or why it gave up.
func AfkLogHeadline(decision AfkDecision) string {
	switch decision.Status {
	case AfkDecisionStatusDeciding:
		return "Deciding at " + decision.StageName + "…"
	case AfkDecisionStatusApplied:
		if decision.Action != nil {
			return *decision.Action
		}
		return "Decided at " + decision.StageName
	case AfkDecisionStatusLeftForYou:
		if decision.Detail != nil {
			return "Left it for you: " + *decision.Detail
		}
		return "Left it for you at " + decision.StageName
	case AfkDecisionStatusFailed:
		return "Could not decide at " + decision.StageName + afkLogDetailTail(decision.Detail)
	case AfkDecisionStatusSuperseded:
		return "Stopped at " + decision.StageName + afkLogDetailTail(decision.Detail)
	}
	return ""
}

// afkLogDetailTail is ": detail" when there is a detail, else nothing.
func afkLogDetailTail(detail *string) string {
	if detail == nil {
		return ""
	}
	return afkLogDetailSeparator + *detail
}
