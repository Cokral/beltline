// Checks of LibraryCheckChangedProblems: only the changed files are reported, and
// a broken file that is not changed hides the others (it
// returns the filtered factory problems before any warning is computed). The

package core

import (
	"reflect"
	"slices"
	"strings"
	"testing"
)

// libraryCheckFiles is a library with one valid agent and one unreadable agent.
func libraryCheckFiles() map[string][]byte {
	return map[string][]byte{
		"agents/build/v1.md": []byte("---\nname: Build\n---\nBrief\n"),
		"agents/other/v1.md": []byte("malformed"),
	}
}

// TestLibraryCheckReportsOnlyChangedFiles checks that an unreadable file that is not
// changed is not reported.
func TestLibraryCheckReportsOnlyChangedFiles(t *testing.T) {
	t.Parallel()
	files := libraryCheckFiles()
	files["agents/other/v1.md"] = []byte("malformed")
	changed := map[string]struct{}{"agents/build/v1.md": {}}
	if got := LibraryCheckChangedProblems(files, changed, nil); len(got) != 0 {
		t.Fatalf("unchanged broken file reported: %v", got)
	}
	changed = map[string]struct{}{"agents/other/v1.md": {}}
	want := []string{"agents/other/v1.md: The file doesn't start with a --- frontmatter block."}
	if got := LibraryCheckChangedProblems(files, changed, nil); !reflect.DeepEqual(got, want) {
		t.Fatalf("changed broken file %v, want %v", got, want)
	}
}

// TestLibraryCheckWarnsAboutAChangedAgent checks that a readable changed agent gets its own warnings.
func TestLibraryCheckWarnsAboutAChangedAgent(t *testing.T) {
	t.Parallel()
	files := map[string][]byte{"agents/build/v1.md": []byte("---\nname: Build\n---\nBrief\n")}
	got := LibraryCheckChangedProblems(files, map[string]struct{}{"agents/build/v1.md": {}}, nil)
	if !slices.Contains(got, "agents/build/v1.md: Add at least one outcome.") {
		t.Fatalf("problems %v, want the outcome warning", got)
	}
	for _, problem := range got {
		if !strings.HasPrefix(problem, "agents/build/v1.md: ") {
			t.Fatalf("problem %q does not name the changed file", problem)
		}
	}
}
