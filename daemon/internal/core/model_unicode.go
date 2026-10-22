// The string operations that Unicode case mapping and collation decide: locale-aware
// compare and contains, case-insensitive folding, full uppercasing and lowercasing,
// and the first-letter capitalisation the presentation writes inline.
//
// The Go counterparts use golang.org/x/text: the Go project's implementation of the CLDR
// collation, Unicode normalisation and full case mapping tables.

package core

import (
	"strings"
	"sync"
	"unicode"

	"golang.org/x/text/cases"
	"golang.org/x/text/collate"
	"golang.org/x/text/language"
	"golang.org/x/text/unicode/norm"
)

// collators are the English collators the presentation sorts with. A collator keeps
// its own iterators, so each is used under its mutex only.
var (
	collatorMu       sync.Mutex
	collatorStandard = collate.New(language.English, collate.Numeric, collate.IgnoreCase)
	collatorFold     = collate.New(language.English, collate.IgnoreCase)
)

// LocalizedStandardCompare is Foundation's localizedStandardCompare: English collation with
// numbers compared by value ("2" before "12") and no regard to case. It returns -1, 0 or 1.
func LocalizedStandardCompare(a, b string) int {
	collatorMu.Lock()
	defer collatorMu.Unlock()
	return collatorStandard.CompareString(a, b)
}

// LocalizedCaseInsensitiveCompare is Foundation's localizedCaseInsensitiveCompare: English
// collation without regard to case, and digits compared one by one. It returns -1, 0 or 1.
func LocalizedCaseInsensitiveCompare(a, b string) int {
	collatorMu.Lock()
	defer collatorMu.Unlock()
	return collatorFold.CompareString(a, b)
}

// LocalizedCaseInsensitiveContains reports whether sub occurs in s when both are case folded
// and normalised to NFC. Diacritics still count. An empty sub is never contained: NSString
// finds nothing for an empty needle, and every caller checks for an empty query first.
func LocalizedCaseInsensitiveContains(s, sub string) bool {
	if sub == "" {
		return false
	}
	return strings.Contains(foldCase(norm.NFC.String(s)), foldCase(norm.NFC.String(sub)))
}

// FoldCaseDiacritics is String.folding(options: [.caseInsensitive, .diacriticInsensitive]):
// the text decomposed (NFD), its combining marks dropped and case folded. Letters with no
// decomposition, such as ø, æ and ł, stay as they are apart from their case.
func FoldCaseDiacritics(s string) string {
	var b strings.Builder
	for _, r := range norm.NFD.String(s) {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		b.WriteRune(r)
	}
	return foldCase(b.String())
}

// UppercasedFull is the full Unicode uppercase mapping, so ß becomes SS.
func UppercasedFull(s string) string {
	return cases.Upper(language.Und).String(s)
}

// LowercasedFull is the full Unicode lowercase mapping.
func LowercasedFull(s string) string {
	return cases.Lower(language.Und).String(s)
}

// CapitalizedFirst uppercases the first character in full, counted as a grapheme,
// and leaves the rest unchanged.
func CapitalizedFirst(s string) string {
	chars := Characters(s)
	if len(chars) == 0 {
		return s
	}
	return UppercasedFull(chars[0]) + s[len(chars[0]):]
}

// foldCase is Unicode full case folding (ß becomes ss, ø stays ø).
func foldCase(s string) string {
	return cases.Fold().String(s)
}
