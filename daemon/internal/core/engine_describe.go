package core

import (
	"strings"
)

// EngineDescribe is a short description of what then does, such as "Next: Spec",
// for a run that took routes.
func EngineDescribe(then Then, layout BeltLayout, current string, routes map[string][]string) string {
	name := func(id *string) string {
		if id == nil {
			return "a missing stage"
		}
		if stage := layout.stageNamed(*id); stage != nil {
			return stage.Name
		}
		return "a missing stage"
	}
	switch then.Action {
	case ThenActionNext:
		next := layout.Next(current)
		if next == nil {
			return "Next stage"
		}
		if next.Stage != nil {
			return "Next: " + name(next.Stage)
		}
		return "Done"
	case ThenActionGoto:
		text := "Back to " + name(layout.Target(then.To, routes, nil))
		if then.WithOutput {
			text += " with the output"
		}
		return text
	case ThenActionRoute:
		var names []string
		for _, id := range then.Routes {
			if route := layout.Route(id); route != nil {
				names = append(names, route.Name)
			}
		}
		if len(names) == 0 {
			return "Take a missing route"
		}
		if len(names) == 1 {
			return "Take the " + names[0] + " route"
		}
		last := names[len(names)-1]
		return "Take the " + strings.Join(names[:len(names)-1], " and ") + " and " + last + " routes"
	case ThenActionRetry:
		return "Running it again"
	case ThenActionHold:
		if then.Comment {
			return "Held, with a comment on the issue"
		}
		return "Held for you"
	case ThenActionPark:
		return "Parked"
	case ThenActionDone:
		return "Done"
	case ThenActionShip:
		return "Shipped"
	case ThenActionDrop:
		return "Dropped"
	}
	return ""
}

// engineHoldComment is the comment posted on the subject when an outcome holds
// it: the output's open questions, or the whole output.
func engineHoldComment(subject Subject, stage Stage, outcome Outcome, output *string) string {
	text := "Beltline paused this " + subject.Kind.Noun() + " at **" + stage.Name + "**: " + outcome.Name + "."
	if output != nil {
		body := ""
		if section := EngineSection("Open questions", *output); section != nil {
			body = *section
		} else {
			body = OutcomeParserStrip(*output)
		}
		if body != "" {
			text += "\n\n" + body
		}
	}
	return text
}

// EngineSection is the body of a `## <title>` section, if the text has one; nil
// when the body is empty.
func EngineSection(title, text string) *string {
	var lines []string
	inside := false
	for _, line := range outcomeParserLFSplit(text) {
		if strings.HasPrefix(line, "## ") {
			if inside {
				break
			}
			heading := trimBlanks(line[len("## "):])
			inside = LowercasedFull(heading) == LowercasedFull(title)
			continue
		}
		if inside {
			lines = append(lines, line)
		}
	}
	body := strings.TrimSpace(strings.Join(lines, "\n"))
	if body == "" {
		return nil
	}
	return &body
}
