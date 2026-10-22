package store

import "testing"

// TestServableArtifactName checks the file-name rule that decides what an attempt lists and serves.
func TestServableArtifactName(t *testing.T) {
	t.Parallel()
	for name, want := range map[string]bool{
		"page.html": true, "space café ☃.html": true, "PAGE.HTML": true,
		"": false, "sub/page.html": false, `sub\page.html`: false, "../page.html": false,
		"page.txt": false, "page": false, "a%2Fb.html": false, "a%5Cb.html": false,
		".html": false,
	} {
		if got := ServableArtifactName(name); got != want {
			t.Fatalf("ServableArtifactName(%q) = %v", name, got)
		}
	}
}
