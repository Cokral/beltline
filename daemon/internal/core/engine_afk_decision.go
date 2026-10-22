// The event that carries an option
// out, and reading the decision file.

package core

import (
	"strings"
)

// AfkEventFor is the event that carries out option with message; nil for leaving
// it to you. The message is trimmed, and a blank one is no note.
func AfkEventFor(option AfkOption, message *string) *Event {
	var note *string
	if message != nil {
		if text := strings.TrimSpace(*message); text != "" {
			note = &text
		}
	}
	switch option.Action.Kind {
	case AfkActionKindAnswer:
		return &Event{Kind: EventKindAnswered, Answer: note}
	case AfkActionKindRerun:
		event := EventResume(nil, note, nil)
		return &event
	case AfkActionKindDecide:
		return &Event{Kind: EventKindDecided, Outcome: option.Action.Outcome, Note: note}
	case AfkActionKindSend:
		stage := option.Action.Stage
		event := EventResume(&stage, note, nil)
		return &event
	case AfkActionKindDrop:
		return &Event{Kind: EventKindDrop, Reason: "dropped by the " + AfkName}
	}
	return nil
}

// AfkActionLabel is what an option did to run, for the log, such as "Approved: approved"
// or "Sent to Spec". The run's current stage and a send's target are named by their
// names when the run has them.
func AfkActionLabel(option AfkOption, run Run) string {
	stage := run.StageID
	if current := run.Stage(); current != nil {
		stage = current.Name
	}
	switch option.Action.Kind {
	case AfkActionKindAnswer:
		return "Answered " + stage
	case AfkActionKindRerun:
		return "Ran " + stage + " again"
	case AfkActionKindDecide:
		return "Chose “" + option.Action.Outcome + "” at " + stage
	case AfkActionKindSend:
		target := option.Action.Stage
		for _, candidate := range run.Stages {
			if candidate.ID == option.Action.Stage {
				target = candidate.Name
				break
			}
		}
		return "Sent to " + target
	case AfkActionKindDrop:
		return "Dropped the run"
	default:
		return "Left it for you"
	}
}

// AfkParsed is a decision read from the decision file: the option it names, and
// its ## Message and ## Why sections, when present.
type AfkParsed struct {
	Option  AfkOption
	Message *string
	Why     *string
}

// AfkParse is the option the last `Decision:` line names, with the `## Message`
// and `## Why` sections; nil when the line is missing or names no option. An
// outcome's name alone also picks that outcome.
func AfkParse(text string, options []AfkOption) *AfkParsed {
	lines := afkLines(text)
	for index := len(lines) - 1; index >= 0; index-- {
		value := afkValue(lines[index])
		if value == nil {
			continue
		}
		option, ok := afkOptionNamed(options, *value)
		if !ok {
			return nil
		}
		body := afkBody(text)
		return &AfkParsed{Option: option, Message: EngineSection("Message", body), Why: EngineSection("Why", body)}
	}
	return nil
}

// afkOptionNamed is the option whose id is value, else the one whose id is
// `decide <value>`.
func afkOptionNamed(options []AfkOption, value string) (AfkOption, bool) {
	for _, option := range options {
		if option.ID == value {
			return option, true
		}
	}
	for _, option := range options {
		if option.ID == "decide "+value {
			return option, true
		}
	}
	return AfkOption{}, false
}

// afkValue is the normalised value of a `Decision:` line, without markdown; nil
// when the line is not one.
func afkValue(line string) *string {
	text := trimBlanks(line)
	for _, prefix := range []string{"- ", "* ", "> "} {
		text = strings.TrimPrefix(text, prefix)
	}
	text = strings.ReplaceAll(text, "**", "")
	text = strings.ReplaceAll(text, "`", "")
	const label = "decision:"
	if !strings.HasPrefix(LowercasedFull(text), label) {
		return nil
	}
	runes := []rune(text)
	if len(runes) < len(label) {
		return nil
	}
	value := afkNormalize(strings.Trim(string(runes[len(label):]), "<>\"“”'."))
	return &value
}

// afkNormalize is text lowercased, with its whitespace runs collapsed to one space.
func afkNormalize(text string) string {
	return strings.Join(strings.Fields(LowercasedFull(text)), " ")
}

// afkLines splits text at its newline characters, a CRLF being one, and omits
// empty lines.
func afkLines(text string) []string {
	var lines []string
	var current strings.Builder
	runes := []rune(text)
	for index := 0; index < len(runes); index++ {
		r := runes[index]
		if r == '\r' && index+1 < len(runes) && runes[index+1] == '\n' {
			index++
			r = '\n'
		}
		if IsNewline(r) {
			if current.Len() > 0 {
				lines = append(lines, current.String())
			}
			current.Reset()
			continue
		}
		current.WriteRune(r)
	}
	if current.Len() > 0 {
		lines = append(lines, current.String())
	}
	return lines
}

// afkBody is the decision file without its decision lines, which the sections are
// read from. It splits at each "
// " that is not part of a CRLF, keeps empty pieces,
// and drops the lines `Decision:` makes.
func afkBody(text string) string {
	var kept []string
	start := 0
	for index := 0; index <= len(text); index++ {
		if index < len(text) && (text[index] != '\n' || (index != 0 && text[index-1] == '\r')) {
			continue
		}
		chunk := text[start:index]
		if afkValue(chunk) == nil {
			kept = append(kept, chunk)
		}
		start = index + 1
	}
	return strings.Join(kept, "\n")
}
