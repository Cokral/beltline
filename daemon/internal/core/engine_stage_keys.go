package core

import (
	"path"
	"slices"
	"strings"
)

// StageKeysVersion is the version a run started with stage keys records in its
// references.
const StageKeysVersion = 1

// StageKeysChosen is the placeholder chosen on the stage. Empty when a belt file
// was saved before stages had one; deliberate conversion fills nil keys.
func StageKeysChosen(stage Stage) string {
	return ValueOf(stage.Key)
}

// StageKeysSuggestion is a one-time suggestion for a new stage. Legacy agent
// metadata is useful when it is a valid stage name; otherwise use the stable
// agent id, with a suffix only when that id is itself reserved.
func StageKeysSuggestion(agent Agent) string {
	if validationLooksLikePlaceholder(agent.Key) && !slices.Contains(BriefFixedPlaceholders, agent.Key) {
		return agent.Key
	}
	if validationLooksLikePlaceholder(agent.ID) && !slices.Contains(BriefFixedPlaceholders, agent.ID) {
		return agent.ID
	}
	return agent.ID + "-stage"
}

// StageKeysLegacyName is the legacy name only old belts and runs use: the pinned
// agent's key, or that agent's id when the version is missing. New runs never
// read this.
func StageKeysLegacyName(stage Stage, agents map[AgentRef]Agent) string {
	if stage.Kind() != StageKindAgent || stage.Agent == nil {
		return ""
	}
	if agent, ok := agents[*stage.Agent]; ok {
		return agent.Key
	}
	return stage.Agent.ID
}

// StageKeysName is the key a stage's output goes by in the stage-key reference
// model. The agents argument is unused.
func StageKeysName(stage Stage, agents map[AgentRef]Agent) string {
	return StageKeysChosen(stage)
}

// StageKeysPathOf is the path a stage sits on, as its route names joined by
// " / ", or "main line" when it is on none.
func StageKeysPathOf(stage Stage, layout BeltLayout) string {
	names := layout.PathOf(stage.ID)
	if len(names) == 0 {
		return "main line"
	}
	return strings.Join(names, " / ")
}

// StageKeysExclusive reports whether two stages sit on routes a single visit
// cannot take together. They share a fork where their routes differ, and no
// configured entry into that fork selects both routes.
func StageKeysExclusive(a, b Stage, layout BeltLayout) bool {
	left := layout.Ancestry(a.ID)
	right := layout.Ancestry(b.ID)
	count := min(len(left), len(right))
	if count == 0 {
		return false
	}
	for index := 0; index < count; index++ {
		if left[index].Fork.ID != right[index].Fork.ID {
			return false
		}
		if left[index].Route.ID != right[index].Route.ID {
			return !StageKeysTakenTogether(left[index].Fork, left[index].Route.ID, right[index].Route.ID, layout)
		}
	}
	return false
}

// StageKeysTakenTogether reports whether a belt-start fork runs routes left and
// right together. A belt-start fork runs every route but those that feed in. A
// route outcome that names both routes does too. Anything else leaves the
// routes as alternatives.
func StageKeysTakenTogether(fork Fork, left, right string, layout BeltLayout) bool {
	if pieces := layout.Pieces(nil); len(pieces) > 0 && pieces[0].Fork != nil && pieces[0].Fork.ID == fork.ID {
		return layout.Opens(left) && layout.Opens(right)
	}
	for _, stage := range layout.Stages {
		if next := layout.ForkAfter(stage.ID); next == nil || next.ID != fork.ID {
			continue
		}
		for _, outcome := range stage.Outcomes {
			if outcome.Then.Action != ThenActionRoute {
				continue
			}
			if slices.Contains(outcome.Then.Routes, left) && slices.Contains(outcome.Then.Routes, right) {
				return true
			}
		}
	}
	return false
}

// stageKeysDuplicateWarnings are the warnings about stage keys: missing,
// malformed, reserved and duplicated placeholders.
func stageKeysDuplicateWarnings(belt Belt, agents map[AgentRef]Agent) []string {
	layout := belt.Layout()
	var writers []Stage
	for _, stage := range belt.Stages {
		if stage.Kind() == StageKindAgent {
			writers = append(writers, stage)
		}
	}
	warnings := []string{}
	for _, stage := range writers {
		// Validation sees a deliberately converted legacy definition.
		key := StageKeysChosen(stage)
		if key == "" {
			// The pinned definition is missing when the stage has no pin, or its pin is not in agents.
			unavailable := true
			if stage.Agent != nil {
				_, found := agents[*stage.Agent]
				unavailable = !found
			}
			if stage.Key == nil && unavailable {
				warnings = append(warnings, stage.Name+" has no output placeholder because its pinned agent definition is unavailable.")
			} else {
				warnings = append(warnings, stage.Name+" has no output placeholder.")
			}
		} else if !validationLooksLikePlaceholder(key) {
			warnings = append(warnings, stage.Name+": the placeholder may only use lowercase letters, digits, - and _.")
		} else if slices.Contains(BriefFixedPlaceholders, key) {
			warnings = append(warnings, stage.Name+": {"+key+"} is already a placeholder of its own. Pick another.")
		}
	}
	for left := range writers {
		for right := left + 1; right < len(writers); right++ {
			a, b := writers[left], writers[right]
			key := StageKeysName(a, agents)
			if key == "" || key != StageKeysName(b, agents) || !validationLooksLikePlaceholder(key) ||
				slices.Contains(BriefFixedPlaceholders, key) || StageKeysExclusive(a, b, layout) {
				continue
			}
			warnings = append(warnings, "{"+key+"} is used by "+a.Name+" ("+StageKeysPathOf(a, layout)+") and "+
				b.Name+" ("+StageKeysPathOf(b, layout)+").")
		}
	}
	return warnings
}

// StageKeysExplicit fills a key the belt never had (nil) from the pinned agent's
// legacy name. Saving and starting a run call this; reading a file does not. An
// explicitly empty key stays invalid, and a missing pinned definition stays nil
// for validation to report rather than inventing a name. The belt is copied, so
// the caller's stages are not changed.
func StageKeysExplicit(belt Belt, agents map[AgentRef]Agent) Belt {
	out := belt.Clone()
	for index := range out.Stages {
		stage := &out.Stages[index]
		if stage.Kind() != StageKindAgent || stage.Key != nil || stage.Agent == nil {
			continue
		}
		agent, ok := agents[*stage.Agent]
		if !ok {
			continue
		}
		key := agent.Key
		stage.SetKey(&key)
	}
	return out
}

// StageKeysPathOfKey is the output path for key on a run that uses stage keys.
// The current stage's own key is this attempt. Another stage's key is the latest
// finished output of the writer on the route this run took, from any earlier
// pass: going back into a fork keeps what its stages wrote until they write
// again. Outputs from an earlier visit of a fork are not hidden, so an approved
// spec survives a verify send-back.
func StageKeysPathOfKey(key string, run Run, stage Stage, context BriefContext) string {
	if StageKeysChosen(stage) == key {
		return BriefOutputPath(stage, context)
	}
	writers := StageKeysSelected(key, run, stage, context)
	if len(writers) != 1 {
		return "(no " + key + " output yet)"
	}
	writer := writers[0]
	for i := len(run.Attempts) - 1; i >= 0; i-- {
		attempt := run.Attempts[i]
		if attempt.StageID == writer.ID && !attempt.IsOpen() {
			return path.Join(context.RunFolder, attempt.Folder, writer.Writes())
		}
	}
	return "(no " + writer.Writes() + " yet)"
}

// StageKeysSelected are the writers of key whose route is the one this leg, or
// the joined run, selected.
func StageKeysSelected(key string, run Run, stage Stage, context BriefContext) []Stage {
	layout := run.Layout()
	here := layout.Ancestry(stage.ID)
	routes := run.Routes()
	var out []Stage
	for _, other := range run.Stages {
		if other.Kind() == StageKindAgent && other.ID != stage.ID && StageKeysChosen(other) == key &&
			stageKeysOnSelectedRoute(other, layout, here, routes) {
			out = append(out, other)
		}
	}
	return out
}

// stageKeysOnSelectedRoute reports whether writer is on the routes `here` and
// `routes` select.
func stageKeysOnSelectedRoute(writer Stage, layout BeltLayout, here []BeltLayoutWay, routes map[string][]string) bool {
	for _, way := range layout.Ancestry(writer.ID) {
		var mine *BeltLayoutWay
		for i := range here {
			if here[i].Fork.ID == way.Fork.ID {
				mine = &here[i]
				break
			}
		}
		if mine != nil {
			if mine.Route.ID != way.Route.ID {
				return false
			}
		} else if taken, ok := routes[way.Fork.ID]; ok && len(taken) == 1 && taken[0] == way.Route.ID {
			continue
		} else {
			return false
		}
	}
	return true
}
