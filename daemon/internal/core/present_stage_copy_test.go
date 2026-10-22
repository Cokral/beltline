package core

import (
	"reflect"
	"testing"
	"time"
)

// TestInlineSpansSplitStrongTextAndCodeAndKeepUnclosedMarkersAsText
func TestInlineSpansSplitStrongTextAndCodeAndKeepUnclosedMarkersAsText(t *testing.T) {
	t.Parallel()
	cases := []struct {
		text string
		want []InlineSpan
	}{
		{"Create `feat/x` and have **Build** run Build.", []InlineSpan{
			{InlineSpanKindText, "Create "}, {InlineSpanKindCode, "feat/x"}, {InlineSpanKindText, " and have "},
			{InlineSpanKindStrong, "Build"}, {InlineSpanKindText, " run Build."},
		}},
		{"plain", []InlineSpan{{InlineSpanKindText, "plain"}}},
		{"open ** and `tick", []InlineSpan{{InlineSpanKindText, "open ** and `tick"}}},
		{"", []InlineSpan{}},
	}
	for _, c := range cases {
		if got := InlineSpanParse(c.text); !reflect.DeepEqual(got, c.want) {
			t.Fatalf("InlineSpanParse(%q) = %+v, want %+v", c.text, got, c.want)
		}
	}
}

// TestRelativeTimeUsesTheAppWords
func TestRelativeTimeUsesTheAppWords(t *testing.T) {
	t.Parallel()
	now := time.Unix(100_000, 0)
	cases := map[time.Duration]string{
		-30 * time.Second:        "just now",
		-240 * time.Second:       "4 min ago",
		-7200 * time.Second:      "2 h ago",
		-3 * 86400 * time.Second: "3 d ago",
	}
	for offset, want := range cases {
		if got := RelativeTimeAgo(now.Add(offset), now); got != want {
			t.Fatalf("RelativeTimeAgo(%v) = %q, want %q", offset, got, want)
		}
	}
}

// TestDecidedLineNamesWhoDecided
func TestDecidedLineNamesWhoDecided(t *testing.T) {
	t.Parallel()
	now := time.Unix(100_000, 0)
	if got := StageCopyDecidedLine("You", now.Add(-120*time.Second), now); got != "You decided 2 min ago." {
		t.Fatalf("decided line = %q", got)
	}
	if got := StageCopyMessageLine("hi"); got != "Your message: hi" {
		t.Fatalf("message line = %q", got)
	}
	if got := StageCopyDetailLine("hi", true); got != "Your note: hi" {
		t.Fatalf("gate detail = %q", got)
	}
	if got := StageCopyDetailLine("hi", false); got != "hi" {
		t.Fatalf("agent detail = %q", got)
	}
}

// TestInlineSpanKeepsACombiningMarkWithItsCharacter: the line is split into characters,
// so a marker followed by a combining mark is not the marker, and the text stays one span.
func TestInlineSpanKeepsACombiningMarkWithItsCharacter(t *testing.T) {
	t.Parallel()
	cases := map[string][]InlineSpan{
		"**a**́": {{Kind: InlineSpanKindText, Text: "**a**́"}},
		"`x`́":   {{Kind: InlineSpanKindText, Text: "`x`́"}},
		"**a**":  {{Kind: InlineSpanKindStrong, Text: "a"}},
	}
	for text, want := range cases {
		if got := InlineSpanParse(text); !reflect.DeepEqual(got, want) {
			t.Fatalf("InlineSpanParse(%q) = %+v, want %+v", text, got, want)
		}
	}
}

// TestRelativeTimeBoundaries is the boundary table of RelativeTime.ago: the step to the
// next word falls at 60 s, 60 min and 24 h, and a date after now reads as just now.
func TestRelativeTimeBoundaries(t *testing.T) {
	t.Parallel()
	now := time.Unix(100_000, 0)
	cases := []struct {
		seconds int
		want    string
	}{
		{-5, "just now"},
		{59, "just now"},
		{60, "1 min ago"},
		{3599, "59 min ago"},
		{3600, "1 h ago"},
		{86399, "23 h ago"},
		{86400, "1 d ago"},
	}
	for _, c := range cases {
		if got := RelativeTimeAgo(now.Add(-time.Duration(c.seconds)*time.Second), now); got != c.want {
			t.Errorf("RelativeTimeAgo(%d s) = %q, want %q", c.seconds, got, c.want)
		}
	}
}
