package core

import (
	"math"
	"testing"
)

// TestAddIntsReportsOverflowInBothDirections checks AddInts at the int limits
// in each direction: an exact sum is ok, and a sum past either end is not, with
// zero as the value returned in that case.
func TestAddIntsReportsOverflowInBothDirections(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		a, b   int
		sum    int
		wantOK bool
	}{
		{"positive sum", 2, 3, 5, true},
		{"negative sum", -2, -3, -5, true},
		{"mixed signs", 10, -4, 6, true},
		{"largest exact sum", math.MaxInt - 1, 1, math.MaxInt, true},
		{"overflows upwards", math.MaxInt, 1, 0, false},
		{"overflows downwards", math.MinInt, -1, 0, false},
		{"zero adds nothing", math.MaxInt, 0, math.MaxInt, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			sum, ok := AddInts(tc.a, tc.b)
			if ok != tc.wantOK || sum != tc.sum {
				t.Errorf("AddInts(%d, %d) = (%d, %t), want (%d, %t)", tc.a, tc.b, sum, ok, tc.sum, tc.wantOK)
			}
		})
	}
}

// prefix(n) counts characters, so a skin-toned emoji or a combining mark is one.
func TestPrefixCharactersCountsEmojiAndCombiningMarksAsOneCharacter(t *testing.T) {
	t.Parallel()
	text := "\U0001F44D\U0001F3FD" + "e\u0301" + "xyz"
	if got := PrefixCharacters(text, 2); got != "\U0001F44D\U0001F3FD"+"e\u0301" {
		t.Errorf("prefix(2) = %q", got)
	}
	if got := PrefixCharacters(text, 9); got != text {
		t.Errorf("prefix past the end = %q", got)
	}
}
