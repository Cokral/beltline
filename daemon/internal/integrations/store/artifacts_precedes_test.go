// Tests of the artifact list's ordering: names compare case-insensitively on their composed characters,
// so a decomposed accented name sorts after z.

package store

import "testing"

// TestArtifactPrecedesComposesNamesBeforeComparing checks that a decomposed accented name sorts as its composed form does.
func TestArtifactPrecedesComposesNamesBeforeComparing(t *testing.T) {
	t.Parallel()
	decomposed := "Été.html" // Été, decomposed as the filesystem lists it
	if artifactPrecedes(decomposed, "Zeta.HTML") {
		t.Fatal("Été sorted before Zeta, want after")
	}
	if !artifactPrecedes("Zeta.HTML", decomposed) {
		t.Fatal("Zeta did not sort before Été")
	}
	if !artifactPrecedes("contract.html", "Zeta.HTML") || artifactPrecedes("Zeta.HTML", "contract.html") {
		t.Fatal("ASCII names are not ordered case-insensitively")
	}
}
