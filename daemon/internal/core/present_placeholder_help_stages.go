package core

import "slices"

// stageHelp is what the stage help needs to describe each name: the belt's agent
// stages, the key each one resolves to, the route each sits on, the stage being
// configured and whether the run predates stage keys.
type stageHelp struct {
	agents  []Stage
	keys    map[string]string
	current *string
	routeOf func(stage Stage) string
	legacy  bool
}

func newStageHelp(stages []Stage, current *string, key func(stage Stage) *string, route func(stage Stage) string, legacy bool) stageHelp {
	routeOf := route
	if routeOf == nil {
		routeOf = func(Stage) string { return "main line" }
	}
	var agents []Stage
	for _, stage := range stages {
		if stage.Kind() == StageKindAgent {
			agents = append(agents, stage)
		}
	}
	// Resolve once so provenance and chips always describe the same version.
	keys := map[string]string{}
	for _, agent := range agents {
		value := ""
		if k := key(agent); k != nil {
			value = *k
		}
		keys[agent.ID] = value
	}
	return stageHelp{agents: agents, keys: keys, current: current, routeOf: routeOf, legacy: legacy}
}

// chipKey is the key a chip for the stage resolves to, nil for a stage that is not an agent.
func (h stageHelp) chipKey(stage Stage) *string {
	value, ok := h.keys[stage.ID]
	if !ok {
		return nil
	}
	return &value
}

func (h stageHelp) isCurrent(id string) bool {
	return h.current != nil && id == *h.current
}

func (h stageHelp) anyCurrent(stages []Stage) bool {
	return slices.ContainsFunc(stages, func(stage Stage) bool { return h.isCurrent(stage.ID) })
}

// agentsWhere returns the agent stages that keep accepts, in the belt's order.
func (h stageHelp) agentsWhere(keep func(agent Stage) bool) []Stage {
	var out []Stage
	for _, agent := range h.agents {
		if keep(agent) {
			out = append(out, agent)
		}
	}
	return out
}

func (h stageHelp) keyedBy(name string) []Stage {
	return h.agentsWhere(func(agent Stage) bool { return h.keys[agent.ID] == name })
}

func (h stageHelp) identifiedBy(name string) []Stage {
	return h.agentsWhere(func(agent Stage) bool { return agent.ID == name })
}

// PlaceholderHelpStages is the help for a belt's stages: the chips in order, with
// each name's description. current is the stage being configured (nil when none).
// route names the route a stage sits on (nil means "main line").
// legacy is for a run saved before stage keys.
func PlaceholderHelpStages(stages []Stage, current *string, key func(stage Stage) *string, route func(stage Stage) string, legacy bool) PlaceholderHelp {
	h := newStageHelp(stages, current, key, route, legacy)
	var chipNames []string
	for _, chip := range StageAdjustEntries(stages, h.chipKey, h.routeOf, legacy) {
		chipNames = append(chipNames, chip.Key)
	}
	names := placeholderHelpDistinct(append(chipNames, BriefFixedPlaceholders...))
	fixedEntries := PlaceholderHelpFixed()
	entries := make([]PlaceholderHelpEntry, 0, len(names))
	for _, name := range names {
		entries = append(entries, PlaceholderHelpEntry{Name: name, Description: h.description(name, fixedEntries)})
	}
	return PlaceholderHelp{Entries: entries, Qualification: nil, Legacy: legacy, Prose: PlaceholderHelpProseMake(legacy)}
}

// description is the help line for one name: a fixed name's text, or what an
// agent key or a stage id resolves to.
func (h stageHelp) description(name string, fixedEntries []PlaceholderHelpEntry) string {
	keyed, identified := h.keyedBy(name), h.identifiedBy(name)
	fixed := placeholderHelpFixedNamed(fixedEntries, name)
	isFixed := fixed != nil && (name != "artifacts" || (len(keyed) == 0 && (!h.legacy || len(identified) == 0)))
	var description string
	switch {
	case isFixed:
		description = h.fixedDescription(fixed.Description, name)
	case len(keyed) > 0:
		description = h.keyedDescription(name, keyed, identified)
	default:
		description = h.identifiedDescription(identified)
	}
	if h.legacy && name == "artifacts" && !isFixed {
		description += " Legacy artifacts ownership: this output reference wins over the artifact folder and does not request artifact instructions."
	}
	return description
}

func placeholderHelpFixedNamed(fixedEntries []PlaceholderHelpEntry, name string) *PlaceholderHelpEntry {
	for index := range fixedEntries {
		if fixedEntries[index].Name == name {
			return &fixedEntries[index]
		}
	}
	return nil
}

// fixedDescription is a fixed name's text; for a run saved before stage keys it
// also says which output references the fixed name shadows.
func (h stageHelp) fixedDescription(description, name string) string {
	if !h.legacy {
		return description
	}
	shadowed := h.agentsWhere(func(agent Stage) bool { return h.keys[agent.ID] == name || agent.ID == name })
	if len(shadowed) > 0 {
		description += " Collision: the fixed name wins over output references from " + placeholderHelpLabels(shadowed) + "."
	}
	return description
}

// keyedDescription says which stages a shared key resolves to and which output it
// shows; for a run saved before stage keys it also names the stage ids it shadows.
func (h stageHelp) keyedDescription(name string, keyed, identified []Stage) string {
	var description string
	if h.legacy {
		description = "Agent key for " + placeholderHelpLabels(keyed) + ": "
	} else {
		description = "Stage key for " + placeholderHelpPlaces(keyed, h.routeOf) + ": "
	}
	description += h.keyedOutput(name, keyed)
	if h.legacy {
		description += h.keyCollision(name, keyed, identified)
	}
	return description
}

// keyedOutput says which output the key shows: this attempt's own when a keyed
// stage is the current one, otherwise the latest finished output on the run.
func (h stageHelp) keyedOutput(name string, keyed []Stage) string {
	var output string
	if h.anyCurrent(keyed) {
		if h.legacy {
			output = "this attempt's output path (the current stage uses this key)."
		} else {
			output = "This stage's own key is this attempt's output path. "
		}
	} else if h.legacy {
		output = "the latest output on the run across stages using this key; if missing, (no " + keyed[0].Writes() + " yet)."
	}
	if !h.legacy {
		output += "On a run, a shared key is the latest finished output of the stage on the route the run took, including after the routes join. Another route's output is not used. If that stage has not finished, the brief shows (no " + keyed[0].Writes() + " yet). If no route has selected one writer, it shows (no " + name + " output yet)."
	}
	return output
}

// keyCollision names the stage ids that a legacy agent key wins over.
func (h stageHelp) keyCollision(name string, keyed, identified []Stage) string {
	var shadowed []Stage
	for _, agent := range identified {
		if h.keys[agent.ID] != name || len(keyed) > 1 {
			shadowed = append(shadowed, agent)
		}
	}
	if len(shadowed) == 0 {
		return ""
	}
	return " Collision: this agent key wins over the stage id of " + placeholderHelpLabels(shadowed) + "; the name resolves across the key's stages, not to that particular stage."
}

// identifiedDescription says what a stage id shows: this attempt's output path for
// the current stage, otherwise the stage's latest finished output.
func (h stageHelp) identifiedDescription(identified []Stage) string {
	stage := identified[0]
	description := "Stage id for " + placeholderHelpLabels(identified) + ": "
	if h.isCurrent(stage.ID) {
		return description + "this attempt's output path."
	}
	return description + "this stage's latest finished output, or (no " + stage.Writes() + " yet)."
}
