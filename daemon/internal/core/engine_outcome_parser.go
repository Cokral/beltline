package core

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// OutcomeParserParse is the outcome the last `Outcome:` line names, if it names
// one of outcomes. The name may be followed by a reason, as in
// `Outcome: needs info — no reproduction steps`. Nil when the last Outcome line
// names no known outcome.
func OutcomeParserParse(output string, outcomes []Outcome) *Outcome {
	lines := outcomeParserNewlineSplit(output)
	for i := len(lines) - 1; i >= 0; i-- {
		value := outcomeParserValue(lines[i])
		if value == nil {
			continue
		}
		for j := range outcomes {
			if outcomeNormalize(outcomes[j].Name) == *value {
				exact := outcomes[j].Clone()
				return &exact
			}
		}
		var best *Outcome
		bestLength := 0
		for j := range outcomes {
			name := outcomeNormalize(outcomes[j].Name)
			if name == "" || !outcomeParserStartsWithName(*value, name) {
				continue
			}
			// The first of equal lengths is kept.
			if best == nil || outcomeParserCharacterCount(outcomes[j].Name) > bestLength {
				copied := outcomes[j].Clone()
				best = &copied
				bestLength = outcomeParserCharacterCount(outcomes[j].Name)
			}
		}
		if best != nil {
			return best
		}
		// The last Outcome line decides, even when it names nothing known.
		return nil
	}
	return nil
}

// outcomeParserValue is the lowercased value of an `Outcome:` line, without
// markdown, or nil when the line is not one.
func outcomeParserValue(line string) *string {
	text := trimBlanks(line)
	for _, prefix := range []string{"- ", "* ", "> "} {
		text = strings.TrimPrefix(text, prefix)
	}
	text = strings.ReplaceAll(text, "**", "")
	text = strings.ReplaceAll(text, "`", "")
	if !strings.HasPrefix(LowercasedFull(text), "outcome:") {
		return nil
	}
	// The prefix is ASCII, so its byte length is its character count.
	value := strings.TrimFunc(text[len("outcome:"):], func(r rune) bool {
		return isBlank(r) || strings.ContainsRune("<>\"“”'.", r)
	})
	lowered := LowercasedFull(value)
	return &lowered
}

// outcomeParserStartsWithName reports whether value starts with name and the
// name ends at a word boundary (the next character is not a letter or digit).
// Whole characters are compared, so a next rune that extends the previous
// character (a combining mark, a zero-width joiner or a variation selector) is
// part of a different character and the name does not match.
func outcomeParserStartsWithName(value, name string) bool {
	if !strings.HasPrefix(value, name) {
		return false
	}
	rest := value[len(name):]
	if rest == "" {
		return true
	}
	next, _ := utf8.DecodeRuneInString(rest)
	if outcomeParserExtends(next) {
		return false
	}
	return !unicode.IsLetter(next) && !unicode.IsNumber(next)
}

// outcomeParserExtends reports whether r joins the character before it rather
// than starting one: a combining mark, a zero-width joiner or a variation
// selector 16. These are the runes that make a grapheme cluster longer.
func outcomeParserExtends(r rune) bool {
	return unicode.In(r, unicode.Mn, unicode.Mc, unicode.Me) || r == 0x200D || r == 0xFE0F
}

// outcomeParserCharacterCount counts the characters of s,
// approximately: each rune that does not extend the one before it starts a
// character. Used for the longest-name tie-break.
func outcomeParserCharacterCount(s string) int {
	n := 0
	for _, r := range s {
		if !outcomeParserExtends(r) {
			n++
		}
	}
	return n
}

// OutcomeParserCompleting is output with the `Outcome:` line of reply added, for
// an agent that ended its reply with the line but left it out of the file. Nil
// when the output has an `Outcome:` line of its own, which decides, or the reply
// names no known outcome.
func OutcomeParserCompleting(output, reply string, outcomes []Outcome) *string {
	for _, line := range outcomeParserNewlineSplit(output) {
		if outcomeParserValue(line) != nil {
			return nil
		}
	}
	if OutcomeParserParse(reply, outcomes) == nil {
		return nil
	}
	var line *string
	for _, candidate := range outcomeParserNewlineSplit(reply) {
		if outcomeParserValue(candidate) != nil {
			c := candidate
			line = &c
		}
	}
	if line == nil {
		return nil
	}
	body := strings.TrimSpace(output)
	result := body + "\n\n" + trimBlanks(*line) + "\n"
	return &result
}

// OutcomeParserStrip is the output without its `Outcome:` line.
func OutcomeParserStrip(output string) string {
	var kept []string
	for _, line := range outcomeParserLFSplit(output) {
		if outcomeParserValue(line) == nil {
			kept = append(kept, line)
		}
	}
	joined := strings.Join(kept, "\n")
	return strings.TrimSpace(joined)
}

// outcomeParserNewlineSplit splits text at newline characters and drops the
// empty pieces, as split(whereSeparator: \.isNewline) does.
func outcomeParserNewlineSplit(text string) []string {
	return strings.FieldsFunc(text, IsNewline)
}

// outcomeParserLFSplit splits at "
// " only: a "\r
// " pair is one character,
// so it is not split.
func outcomeParserLFSplit(text string) []string {
	var parts []string
	start := 0
	for i := 0; i < len(text); i++ {
		if text[i] == '\n' && (i == 0 || text[i-1] != '\r') {
			parts = append(parts, text[start:i])
			start = i + 1
		}
	}
	return append(parts, text[start:])
}
