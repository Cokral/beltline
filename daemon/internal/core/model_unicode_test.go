// Tests for the Unicode helpers of model_unicode.go.
// Expected orders are the CLDR root collation; they are not checked against
// another implementation, so each one is marked by its reason below.

package core

import "testing"

// TestLocalizedStandardCompareOrdersAccentedNamesByBaseLetter: "Émile" has the primary
// weight of "e", so it sorts before "Zed". The hand approximation put it after.
func TestLocalizedStandardCompareOrdersAccentedNamesByBaseLetter(t *testing.T) {
	t.Parallel()
	if got := LocalizedStandardCompare("Émile", "Zed"); got != -1 {
		t.Errorf("LocalizedStandardCompare(Émile, Zed) = %d, want -1", got)
	}
}

// TestLocalizedStandardCompareSortsPunctuationAsCLDRDoes: in CLDR root order "_" (low line)
// comes before "/" (solidus), so acme_web sorts before acme/web. The hand approximation
// compared the code points and reversed them.
func TestLocalizedStandardCompareSortsPunctuationAsCLDRDoes(t *testing.T) {
	t.Parallel()
	if got := LocalizedStandardCompare("acme_web", "acme/web"); got != -1 {
		t.Errorf("LocalizedStandardCompare(acme_web, acme/web) = %d, want -1", got)
	}
}

// TestLocalizedStandardCompareCountsNumbersByValueAndIgnoresCase.
func TestLocalizedStandardCompareCountsNumbersByValueAndIgnoresCase(t *testing.T) {
	t.Parallel()
	cases := []struct {
		a, b string
		want int
	}{
		{"file2", "file10", -1},
		{"file10", "file2", 1},
		{"a", "A", 0},
		{"apple", "Banana", -1},
	}
	for _, c := range cases {
		if got := LocalizedStandardCompare(c.a, c.b); got != c.want {
			t.Errorf("LocalizedStandardCompare(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

// TestLocalizedCaseInsensitiveCompareReadsDigitsOneByOne: no numeric ordering, so "file10"
// is before "file2"; an accent is a secondary difference, so the accented name is after.
func TestLocalizedCaseInsensitiveCompareReadsDigitsOneByOne(t *testing.T) {
	t.Parallel()
	cases := []struct {
		a, b string
		want int
	}{
		{"file2", "file10", 1},
		{"a", "A", 0},
		{"apple", "Banana", -1},
		{"Émile", "Emile", 1},
	}
	for _, c := range cases {
		if got := LocalizedCaseInsensitiveCompare(c.a, c.b); got != c.want {
			t.Errorf("LocalizedCaseInsensitiveCompare(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

// TestFoldCaseDiacriticsDropsMarksAndCase: Foundation's folding with diacritic and case
// insensitivity. Hội and Ó decompose and lose their marks; ø, æ and ł have no
// decomposition and keep their letters; ß folds to ss.
func TestFoldCaseDiacriticsDropsMarksAndCase(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"Émile":   "emile",
		"Hội":     "hoi",
		"Straße":  "strasse",
		"Øresund": "øresund",
		"Æble":    "æble",
		"Łódź":    "łodz",
		"plain":   "plain",
	}
	for input, want := range cases {
		if got := FoldCaseDiacritics(input); got != want {
			t.Errorf("FoldCaseDiacritics(%q) = %q, want %q", input, got, want)
		}
	}
}

// TestLocalizedCaseInsensitiveContainsIgnoresCaseButNotAccents.
func TestLocalizedCaseInsensitiveContainsIgnoresCaseButNotAccents(t *testing.T) {
	t.Parallel()
	cases := []struct {
		s, sub string
		want   bool
	}{
		{"Acme/Billing", "BILL", true},
		{"Émile", "mile", true},
		{"Émile", "emile", false},
		{"Hội", "hội", true},
		{"café", "café", true},
		{"Straße", "STRASSE", true},
		{"abc", "", false},
	}
	for _, c := range cases {
		if got := LocalizedCaseInsensitiveContains(c.s, c.sub); got != c.want {
			t.Errorf("LocalizedCaseInsensitiveContains(%q, %q) = %v, want %v", c.s, c.sub, got, c.want)
		}
	}
}

// TestUppercasedUsesFullCaseMapping: ß becomes SS.
func TestUppercasedUsesFullCaseMapping(t *testing.T) {
	t.Parallel()
	if got := UppercasedFull("Straße"); got != "STRASSE" {
		t.Errorf("UppercasedFull(Straße) = %q, want STRASSE", got)
	}
	if got := LowercasedFull("İ"); got != "i̇" {
		t.Errorf("LowercasedFull(İ) = %q, want i followed by a combining dot", got)
	}
}

// TestCapitalizedFirstCountsACharacterWithItsMarks: the first character is a grapheme, so
// a decomposed é keeps its combining mark after the capital.
func TestCapitalizedFirstCountsACharacterWithItsMarks(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"éclair":  "Éclair",
		"éclair": "Éclair",
		"ßa":      "SSa",
		"held":    "Held",
		"":        "",
	}
	for input, want := range cases {
		if got := CapitalizedFirst(input); got != want {
			t.Errorf("CapitalizedFirst(%q) = %q, want %q", input, got, want)
		}
	}
}
