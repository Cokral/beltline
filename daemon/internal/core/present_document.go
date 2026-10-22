package core

import "slices"
import "strings"

// DocumentHoistingDefaultSection is the section hoisted by default.
const DocumentHoistingDefaultSection = "Open questions"

// DocumentHoisting is the view-time ordering for agent documents: the named section moves
// up under the title. The saved output stays untouched.
func DocumentHoisting(text, section string) string {
	lines := strings.Split(text, "\n")
	wanted := LowercasedFull(section)
	start := -1
	for i, line := range lines {
		if strings.HasPrefix(line, "## ") && LowercasedFull(trimBlanks(line[3:])) == wanted {
			start = i
			break
		}
	}
	if start < 0 {
		return text
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "## ") {
			end = i
			break
		}
	}
	// Keep the document title above the questions, but not introductory prose.
	titleEnd := 0
	if strings.HasPrefix(lines[0], "# ") {
		titleEnd = 1
	}
	blank := !slices.ContainsFunc(lines[titleEnd:start], func(line string) bool {
		return trimBlanks(line) != ""
	})
	if blank {
		return text
	}
	title := documentBlock(lines[:titleEnd])
	questions := documentBlock(lines[start:end])
	rest := make([]string, 0, len(lines[titleEnd:start])+len(lines[end:]))
	rest = append(rest, lines[titleEnd:start]...)
	rest = append(rest, lines[end:]...)
	restBlock := documentBlock(rest)
	parts := []string{}
	for _, part := range []string{title, questions, restBlock} {
		if part != "" {
			parts = append(parts, part)
		}
	}
	return strings.Join(parts, "\n\n")
}

// documentBlock joins lines and trims the line breaks at both ends.
func documentBlock(lines []string) string {
	return strings.TrimFunc(strings.Join(lines, "\n"), IsNewline)
}
