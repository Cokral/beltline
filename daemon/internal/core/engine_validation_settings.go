// The belt's and agent's
// settings checks: placeholders, thinking, alternates, the source of a belt, and
// the agent's own checks.

package core

import (
	"fmt"
	"slices"
	"strings"
)

// ValidationMaxAgentSlots is the most runs an agent may take at once, as the
// editor's field allows.
const ValidationMaxAgentSlots = 6

// validationSlug is the characters a placeholder may use: lowercase letters,
// digits, - and _.
var validationSlug = "abcdefghijklmnopqrstuvwxyz0123456789-_"

// validationBlank reports whether s is empty once its whitespace is trimmed.
// newlines selects all whitespace, including newlines; otherwise only
// spaces, tabs and other horizontal spaces are trimmed.
func validationBlank(s string, newlines bool) bool {
	if newlines {
		return strings.TrimSpace(s) == ""
	}
	return trimBlanks(s) == ""
}

// ValidationWarnings are the pure save-time checks on a belt: each stage's
// outcomes, placeholders and settings. keys are the stage keys a brief may use;
// agents are the pinned versions, never latest agents.
func ValidationWarnings(belt Belt, keys []string, agents map[AgentRef]Agent) []string {
	var warnings []string
	stages := belt.Stages
	if validationBlank(belt.Name, false) {
		warnings = append(warnings, "Give the belt a name.")
	}
	if len(stages) == 0 {
		warnings = append(warnings, "Add at least one stage.")
	}
	warnings = append(warnings, validationTriggerWarnings(belt)...)
	ids := make([]string, len(stages))
	for i, s := range stages {
		ids[i] = s.ID
	}
	if validationHasDuplicate(ids) {
		warnings = append(warnings, "Two stages have the same id.")
	}

	layout := belt.Layout()
	for index, stage := range stages {
		warnings = append(warnings, validationStageWarnings(stage, index, layout, agents)...)
	}
	warnings = append(warnings, validationForks(belt)...)
	warnings = append(warnings, validationInputs(belt)...)
	warnings = append(warnings, validationPlaceholders(belt, keys, agents)...)
	warnings = append(warnings, stageKeysDuplicateWarnings(belt, agents)...)
	return warnings
}

// validationMovesOn reports whether one outcome lets a run move on.
func validationMovesOn(outcomes []Outcome) bool {
	return slices.ContainsFunc(outcomes, func(o Outcome) bool { return o.Then.Action.MovesOn() })
}

// validationHasGood reports whether one outcome is good.
func validationHasGood(outcomes []Outcome) bool {
	return slices.ContainsFunc(outcomes, func(o Outcome) bool { return o.Tone == ToneGood })
}

// validationThinkingWarnings is the thinking level check of one setting: it must
// suit the runner. supported is the runner's own list when nil.
func validationThinkingWarnings(name string, runner Backend, thinking *Thinking, supported map[Thinking]struct{}) []string {
	// A runner with no thinking setting ignores it, keeping it for another runner.
	if supported == nil {
		supported = runner.SupportedThinking()
	}
	if thinking == nil || len(supported) == 0 {
		return nil
	}
	if _, ok := supported[*thinking]; ok {
		return nil
	}
	return []string{fmt.Sprintf("%s: %s doesn't take the thinking level %s.", name, runner.Label(), thinking.Label())}
}

// validationAlternateWarnings is each alternate's thinking level, which must suit
// its runner.
func validationAlternateWarnings(name string, alternates []SessionSettings) []string {
	var out []string
	for _, a := range alternates {
		runner := ValueOr(a.Runner, BackendPi)
		out = append(out, validationThinkingWarnings(name, runner, a.Thinking, nil)...)
	}
	return out
}

// validationLooksLikePlaceholder reports whether name is a placeholder.
func validationLooksLikePlaceholder(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if !strings.ContainsRune(validationSlug, r) {
			return false
		}
	}
	return true
}

// validationPlaceholders are the pure save-time checks on the placeholders of a
// belt's briefs. keys are the stage keys a brief may use; a stage id is not one of
// them unless that id is also a chosen key.
func validationPlaceholders(belt Belt, keys []string, agents map[AgentRef]Agent) []string {
	known := map[string]struct{}{}
	for _, name := range StageAdjustPlaceholders(keys, nil) {
		known[name] = struct{}{}
	}
	var warnings []string
	for _, stage := range belt.Stages {
		if stage.Kind() != StageKindAgent || stage.Agent == nil {
			continue
		}
		agent, ok := agents[*stage.Agent]
		if !ok {
			continue
		}
		brief := StageAdjustEffectiveBrief(stage, &agent)
		seen := map[string]struct{}{}
		tokens := validationTokens(brief)
		for _, name := range tokens {
			if !validationLooksLikePlaceholder(name) {
				continue
			}
			if _, isKnown := known[name]; isKnown {
				continue
			}
			if _, dup := seen[name]; dup {
				continue
			}
			seen[name] = struct{}{}
			warnings = append(warnings, fmt.Sprintf("%s: {%s} isn't a placeholder on this belt, so the agent reads it as text.", stage.Name, name))
		}
		// An inherited brief still naming its pinned agent's old key would
		// read the wrong stage's file or nothing after a rename. Only an
		// explicit stage override may deliberately name another stage.
		chosen := StageKeysChosen(stage)
		legacy := agent.Key
		if stage.Brief == "" && legacy != "" && legacy != chosen && !slices.Contains(BriefFixedPlaceholders, legacy) &&
			slices.Contains(tokens, legacy) {
			warnings = append(warnings, fmt.Sprintf("%s: the brief of %s writes its output to {%s}, but this stage's placeholder is {%s}. Use {output} in %s's brief for its own output, or override the brief on the stage.",
				stage.Name, agent.Name, legacy, chosen, agent.Name))
		}
	}
	return warnings
}

// validationTokens are the {name} tokens a brief uses outside fenced code, without
// braces.
func validationTokens(brief string) []string {
	var result []string
	for _, line := range briefUnfenced(brief) {
		runes := []rune(line)
		index := 0
		for index < len(runes) {
			if runes[index] != '{' {
				index++
				continue
			}
			start := index + 1
			end := start
			for end < len(runes) && strings.ContainsRune(validationSlug, runes[end]) {
				end++
			}
			if end < len(runes) && runes[end] == '}' {
				result = append(result, string(runes[start:end]))
			}
			index = end
		}
	}
	return result
}

// ValidationSourceWarnings are the checks on where a belt's work comes from: its
// trigger and repositories.
func ValidationSourceWarnings(trigger Trigger, repos []string) []string {
	var warnings []string
	kind := trigger.Kind
	if len(repos) == 0 {
		if kind.OneRepo() {
			warnings = append(warnings, "Set the repository the work happens in.")
		} else {
			warnings = append(warnings, "Add at least one repository to the trigger.")
		}
	} else if kind.OneRepo() && len(repos) > 1 {
		warnings = append(warnings, "A "+LowercasedFull(kind.Title())+" belt works in one repository. Remove the others.")
	}
	switch kind {
	case TriggerKindIssues:
		if validationBlank(trigger.issueSettings().Label, true) {
			warnings = append(warnings, "Set the label the trigger watches for.")
		}
	case TriggerKindJira:
		if validationBlank(trigger.jiraSettings().Jql, true) {
			warnings = append(warnings, "Write the JQL query that picks the tickets.")
		}
	case TriggerKindSchedule:
		task := trigger.scheduleSettings()
		if validationBlank(task.Title, true) {
			warnings = append(warnings, "Give the scheduled task a title.")
		}
		if validationBlank(task.Task, true) {
			warnings = append(warnings, "Describe what the scheduled task should do.")
		}
		s := task.Schedule
		if s.Hour < 0 || s.Hour > 23 || s.Minute < 0 || s.Minute > 59 || s.Weekday < 1 || s.Weekday > 7 {
			warnings = append(warnings, "The schedule's time isn't a real time of day.")
		}
	case TriggerKindReviews, TriggerKindManual:
	}
	return warnings
}

// ValidationAgentWarnings are the checks an agent must pass before it can be saved.
// others are the library's other agents, whose ids it must not reuse. The default
// placeholder may match another agent's.
func ValidationAgentWarnings(agent Agent, others []Agent) []string {
	var warnings []string
	if !validationLooksLikePlaceholder(agent.ID) {
		warnings = append(warnings, "The agent's id may only use lowercase letters, digits, - and _.")
	} else {
		for _, o := range others {
			if o.ID == agent.ID {
				warnings = append(warnings, "Another agent already has the id “"+agent.ID+"”.")
				break
			}
		}
	}
	if validationBlank(agent.Name, false) {
		warnings = append(warnings, "Give the agent a name.")
	}
	warnings = append(warnings, validationThinkingWarnings(agent.Name, agent.Runner, agent.Thinking, nil)...)
	warnings = append(warnings, validationAlternateWarnings(agent.Name, agent.Alternates)...)
	if agent.Slots < 1 {
		warnings = append(warnings, "Allow at least one run at once.")
	}
	if agent.Slots > ValidationMaxAgentSlots {
		warnings = append(warnings, fmt.Sprintf("Allow at most %d runs at once.", ValidationMaxAgentSlots))
	}
	if len(agent.Outcomes) == 0 {
		warnings = append(warnings, "Add at least one outcome.")
	}
	names := make([]string, len(agent.Outcomes))
	for i, o := range agent.Outcomes {
		names[i] = outcomeNormalize(o.Name)
	}
	if slices.Contains(names, "") {
		warnings = append(warnings, "An outcome has no name.")
	}
	if validationHasDuplicate(names) {
		warnings = append(warnings, "Two outcomes have the same name.")
	}
	if len(agent.Outcomes) > 0 && !agentHasGood(agent.Outcomes) {
		warnings = append(warnings, "Add a good outcome. The run page uses it to know when the stage went well.")
	}
	if len(agent.Outcomes) > 0 && !agentMovesOn(agent.Outcomes) {
		warnings = append(warnings, "Add an outcome that lets a run move on.")
	}
	for _, o := range agent.Outcomes {
		if o.Then.Action == ThenActionGoto && len(o.To) == 0 {
			warnings = append(warnings, "“"+o.Name+"” goes to a stage, but names no agent.")
		}
	}
	for _, o := range agent.Outcomes {
		if o.Then.Action == ThenActionRoute {
			warnings = append(warnings, "“"+o.Name+"” takes a route, which only a stage on a belt can do. Pick the route there.")
		}
	}
	if validationBlank(agent.Brief, true) {
		warnings = append(warnings, "Write the agent's brief.")
	}
	return warnings
}

// agentHasGood reports whether one agent outcome is good.
func agentHasGood(outcomes []AgentOutcome) bool {
	return slices.ContainsFunc(outcomes, func(o AgentOutcome) bool { return o.Tone == ToneGood })
}

// agentMovesOn reports whether one agent outcome lets a run move on.
func agentMovesOn(outcomes []AgentOutcome) bool {
	return slices.ContainsFunc(outcomes, func(o AgentOutcome) bool { return o.Then.Action.MovesOn() })
}
