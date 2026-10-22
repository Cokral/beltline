// The name rule for artifacts (validName), which decides both what an attempt lists and what it serves.

package store

import "strings"

// ServableArtifactName reports whether an artifact name may be listed and served: a
// non-empty name with no path separators, no `..`, no encoded separators, and an
// `.html` extension.
func ServableArtifactName(name string) bool {
	lower := strings.ToLower(name)
	return name != "" && !strings.Contains(name, "/") && !strings.Contains(name, "\\") &&
		!strings.Contains(name, "..") && !strings.Contains(lower, "%2f") && !strings.Contains(lower, "%5c") &&
		strings.ToLower(artifactPathExtension(name)) == "html"
}

// artifactPathExtension is NSString's pathExtension for a file name: the text after the
// last dot, empty when there is none or the dot leads the name.
func artifactPathExtension(name string) string {
	i := strings.LastIndex(name, ".")
	if i <= 0 {
		return ""
	}
	return name[i+1:]
}
