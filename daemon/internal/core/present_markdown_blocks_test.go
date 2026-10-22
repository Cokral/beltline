package core

import (
	"reflect"
	"strings"
	"testing"
)

// markdownTestCheck fails the test when the parsed blocks differ from the expected ones.
func markdownTestCheck(t *testing.T, text string, want []MarkdownBlock) {
	t.Helper()
	got := MarkdownBlockParse(text)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("MarkdownBlockParse:\n got %#v\nwant %#v", got, want)
	}
}

func markdownTestParagraph(text string) MarkdownBlock {
	return MarkdownBlock{Kind: MarkdownBlockKindParagraph, Text: text}
}

func markdownTestItem(marker, text string) MarkdownBlock {
	return MarkdownBlock{Kind: MarkdownBlockKindItem, Marker: marker, Text: text}
}

// TestProseWrappedAtAColumnFillsThePage
func TestProseWrappedAtAColumnFillsThePage(t *testing.T) {
	t.Parallel()
	text := strings.Join([]string{
		"From Settings a user can open a dedicated conversation with Pi or",
		"Copilot, describe the belts and agents they want, and watch the assistant",
		"change a private draft of their local library.",
	}, "\n")
	markdownTestCheck(t, text, []MarkdownBlock{markdownTestParagraph(
		"From Settings a user can open a dedicated conversation with Pi or Copilot, describe the belts and agents they want, and watch the assistant change a private draft of their local library.")})
}

// TestLinesWrittenOneByOneKeepTheirBreaks
func TestLinesWrittenOneByOneKeepTheirBreaks(t *testing.T) {
	t.Parallel()
	markdownTestCheck(t, "1 → a unit test for the new path.\n2 → the existing suite passes.",
		[]MarkdownBlock{markdownTestParagraph("1 → a unit test for the new path.\n2 → the existing suite passes.")})
	// A long line followed by a short word that would have fitted: a break on purpose.
	long := "This sentence is long enough to look like it was wrapped at a column, but"
	markdownTestCheck(t, long+"\nit\nended early.", []MarkdownBlock{markdownTestParagraph(long + " it\nended early.")})
}

// TestHardBreaksStay
func TestHardBreaksStay(t *testing.T) {
	t.Parallel()
	first := "A line that is long enough to have been wrapped at the usual column  "
	markdownTestCheck(t, first+"\nfollowed by another line.",
		[]MarkdownBlock{markdownTestParagraph(strings.TrimSpace(first) + "\nfollowed by another line.")})
	slash := "A line that is long enough to have been wrapped at the usual column\\"
	markdownTestCheck(t, slash+"\nfollowed by another line.",
		[]MarkdownBlock{markdownTestParagraph("A line that is long enough to have been wrapped at the usual column\nfollowed by another line.")})
}

// TestAnItemKeepsItsContinuationLines
func TestAnItemKeepsItsContinuationLines(t *testing.T) {
	t.Parallel()
	text := strings.Join([]string{
		"1. **Entry point.** Settings → Factories shows a \"Configure with agent\"",
		"   action for the local library whenever one exists.",
		"2. **Isolated draft.** Starting the assistant copies the current belts",
		"   and agents into a draft of its own.",
		"",
		"After the list.",
	}, "\n")
	markdownTestCheck(t, text, []MarkdownBlock{
		markdownTestItem("1.", "**Entry point.** Settings → Factories shows a \"Configure with agent\" action for the local library whenever one exists."),
		markdownTestItem("2.", "**Isolated draft.** Starting the assistant copies the current belts and agents into a draft of its own."),
		markdownTestParagraph("After the list."),
	})
}

// TestHeadingsAndCodeAreUntouched
func TestHeadingsAndCodeAreUntouched(t *testing.T) {
	t.Parallel()
	markdownTestCheck(t, "# Title\n## Goal\n```\nlet a = 1\n  let b = 2\n```\n- one\n- two", []MarkdownBlock{
		{Kind: MarkdownBlockKindHeading, Level: 1, Text: "Title"},
		{Kind: MarkdownBlockKindHeading, Level: 2, Text: "Goal"},
		{Kind: MarkdownBlockKindCode, Text: "let a = 1\n  let b = 2"},
		markdownTestItem("•", "one"),
		markdownTestItem("•", "two"),
	})
}

func TestCharactersKeepsEmojiWithItsSkinToneAndCombiningMarksTogether(t *testing.T) {
	t.Parallel()
	cases := []struct {
		text string
		want []string
	}{
		{"\U0001F44D\U0001F3FDx", []string{"\U0001F44D\U0001F3FD", "x"}},
		{"e\u0301x", []string{"e\u0301", "x"}},
		{"a\r\nb", []string{"a", "\r\n", "b"}},
	}
	for _, c := range cases {
		if got := Characters(c.text); !reflect.DeepEqual(got, c.want) {
			t.Errorf("Characters(%q) = %q, want %q", c.text, got, c.want)
		}
	}
}
