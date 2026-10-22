package runner

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// Runners count characters and compare them canonically, and split at
// "\n" without splitting a CRLF (runner_lines.go, runner_json_values.go).

func TestClaudeSessionDirectoryOnePerCharacter(t *testing.T) {
	t.Parallel()
	// A path that does not exist resolves to itself. Each Character that is not a
	// single ASCII letter or digit becomes one dash: the decomposed e is one, and the
	// skin-toned emoji is one.
	cases := map[string]string{
		"/nonexistent-beltline/a/e\u0301":             "-nonexistent-beltline-a--",
		"/nonexistent-beltline/\U0001F44D\U0001F3FDx": "-nonexistent-beltline--x",
	}
	for path, want := range cases {
		if got := filepath.Base(ClaudeSessionDirectory(path)); got != want {
			t.Errorf("ClaudeSessionDirectory(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestTmuxSessionNameKeepsWholeCharacters(t *testing.T) {
	t.Parallel()
	got := TmuxSessionName([]string{"x\U0001F44D\U0001F3FDy", "e\u0301"})
	if want := "bl-x-y-e\u0301"; got != want {
		t.Errorf("TmuxSessionName = %q, want %q", got, want)
	}
}

func TestCopilotLocateMatchesCanonicalNames(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	composed := filepath.Join(root, "caf\u00e9")
	if err := os.MkdirAll(composed, 0o755); err != nil {
		t.Fatal(err)
	}
	c := CopilotAdapter{StateRoot: root}
	// Comparison is canonical: the decomposed name finds the composed folder.
	match := c.locate("cafe\u0301", root)
	if match.Kind != copilotSessionFound || match.Folder != composed {
		t.Fatalf("exact folder = %+v, want %s", match, composed)
	}
	other := filepath.Join(root, "other")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(other, "workspace.yaml"), []byte("name: caf\u00e9\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(composed); err != nil {
		t.Fatal(err)
	}
	match = c.locate("cafe\u0301", root)
	if match.Kind != copilotSessionFound || match.Folder != other {
		t.Fatalf("named folder = %+v, want %s", match, other)
	}
}

func TestSplitLinesKeepsCRLFWhole(t *testing.T) {
	t.Parallel()
	cases := []struct {
		text      string
		omitEmpty bool
		want      []string
	}{
		{"a\r\nb\nc", false, []string{"a\r\nb", "c"}},
		{"a\n\nb", false, []string{"a", "", "b"}},
		{"a\n\nb\n", true, []string{"a", "b"}},
		{"a\r\r\nb", false, []string{"a\r\r\nb"}},
	}
	for _, c := range cases {
		if got := splitLines(c.text, c.omitEmpty); !reflect.DeepEqual(got, c.want) {
			t.Errorf("splitLines(%q, %v) = %q, want %q", c.text, c.omitEmpty, got, c.want)
		}
	}
}

func TestFirstLineAndNewlineSuffix(t *testing.T) {
	t.Parallel()
	if got := firstLine("{\"a\":1}\r\n{\"b\":2}\n"); got != "{\"a\":1}\r\n{\"b\":2}" {
		t.Errorf("first line across a CRLF = %q", got)
	}
	if got := firstLine("{\"a\":1}\n{\"b\":2}"); got != "{\"a\":1}" {
		t.Errorf("first line = %q", got)
	}
	if hasNewlineSuffix("a\r\n") || !hasNewlineSuffix("a\n") {
		t.Error("hasSuffix(\"\\n\") is wrong for a CRLF ending")
	}
}

// A CRLF log loses its last line in completeLines: the CRLF is one character,
// so the text is one piece that does not end in "\n".
func TestCopilotCompleteLinesDropsACRLFTail(t *testing.T) {
	t.Parallel()
	if got := copilotCompleteLines("a\r\nb\r\n"); got != nil {
		t.Errorf("CRLF log lines = %q, want none", got)
	}
	if got := copilotCompleteLines("a\nb\r\n"); !reflect.DeepEqual(got, []string{"a"}) {
		t.Errorf("mixed log lines = %q, want [a]", got)
	}
	if got := copilotCompleteLines("a\nb\n"); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Errorf("LF log lines = %q", got)
	}
}

func TestClaudeUsageLinesKeepsACRLF(t *testing.T) {
	t.Parallel()
	if got := ClaudeUsageLines("a\r\nb\n"); !reflect.DeepEqual(got, []string{"a\r\nb"}) {
		t.Errorf("ClaudeUsageLines = %q", got)
	}
}

func TestDefaultIgnorableCodePointRanges(t *testing.T) {
	t.Parallel()
	for _, r := range []rune{0x00AD, 0x200B, 0x2065, 0xFE0F, 0xFEFF, 0xE0100, 0x1D173} {
		if !isDefaultIgnorable(r) {
			t.Errorf("U+%04X is default ignorable", r)
		}
	}
	// Format characters that are not default ignorable stay visible.
	for _, r := range []rune{0x0600, 0x1F3FD, 'a'} {
		if isDefaultIgnorable(r) {
			t.Errorf("U+%04X is not default ignorable", r)
		}
	}
}

func TestSymlinksAreNotRealDirectoriesOrRollouts(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	folder := filepath.Join(root, "folder")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(folder, link); err != nil {
		t.Fatal(err)
	}
	if !runnerIsDirectory(link) || runnerIsRealDirectory(link) {
		t.Error("a link to a folder is a directory to Stat, not to Lstat")
	}
	if !runnerIsRealDirectory(folder) {
		t.Error("a folder is not a real directory")
	}
}
