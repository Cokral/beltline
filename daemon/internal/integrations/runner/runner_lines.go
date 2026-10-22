// Line handling for the runner adapters: splitting text into lines and checking for a trailing newline.
// A CRLF is one character, so a split does not split at it, and a text that ends in a CRLF has no
// newline suffix. Go's strings.Split and HasSuffix see bytes, so every site goes through these helpers.

package runner

import (
	"strings"
)

// newlineAt reports whether text[i] is a "
// " that splits: a line
// feed that does not follow a carriage return.
func newlineAt(text string, i int) bool {
	return text[i] == '\n' && (i == 0 || text[i-1] != '\r')
}

// splitLines is text split at each line break, as
// splitting on "
// " with empty pieces omitted when omitEmpty is set.
func splitLines(text string, omitEmpty bool) []string {
	var lines []string
	start := 0
	for i := 0; i < len(text); i++ {
		if !newlineAt(text, i) {
			continue
		}
		if piece := text[start:i]; !omitEmpty || piece != "" {
			lines = append(lines, piece)
		}
		start = i + 1
	}
	if piece := text[start:]; !omitEmpty || piece != "" {
		lines = append(lines, piece)
	}
	return lines
}

// firstLine is `split(separator: "\n", maxSplits: 1).first` with the
// default omittingEmptySubsequences: the first non-empty piece, the rest of the
// text being one piece once the single split is made. "" when there is none.
func firstLine(text string) string {
	for i := 0; i < len(text); i++ {
		if newlineAt(text, i) {
			if piece := text[:i]; piece != "" {
				return piece
			}
			return text[i+1:]
		}
	}
	return text
}

// hasNewlineSuffix reports whether text ends in "
// ": a text ending in a CRLF does not.
func hasNewlineSuffix(text string) bool {
	return strings.HasSuffix(text, "\n") && !strings.HasSuffix(text, "\r\n")
}
