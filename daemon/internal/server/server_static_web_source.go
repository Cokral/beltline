// Where the web app's files come from. The app compiled into beltlined (built with
// the embedweb tag, see server_web_assets_embedded.go) is the default; a nonempty
// BELTLINE_WEB_DIR names a directory read from disk instead, and never falls back
// to the bundled app. Tests serve an in-memory tree through the same path.
package server

import (
	"io/fs"
	"path/filepath"
	"strings"
)

// staticWebSource is where the web app's files come from.
type staticWebSource struct {
	// files holds the app's files in memory: the bundled app, or a test's tree.
	files fs.FS
	// root is the directory the app is read from when files is nil: the override.
	root string
	// bundled marks the app compiled into beltlined (or its absence), so a
	// missing build names the build step rather than BELTLINE_WEB_DIR.
	bundled bool
}

// staticWebBundled is the app compiled into this executable. A build without the
// embedweb tag has none, so its source is unavailable and the not-built page says
// how to get one.
func staticWebBundled() staticWebSource {
	files, ok := bundledWebFS()
	if !ok {
		return staticWebSource{bundled: true}
	}
	return staticWebSource{files: files, bundled: true}
}

// staticWebSelect picks the source for a web root: a nonempty override directory
// replaces the bundled app outright, so a missing override is reported rather than
// served from the bundle; an empty root is the bundled app.
func staticWebSelect(root string, bundled staticWebSource) staticWebSource {
	if root != "" {
		return staticWebSource{root: root}
	}
	return bundled
}

// ready reports whether there is a web app to serve: files in memory, or an
// override directory that exists now (checked per request, as the disk always was).
func (s staticWebSource) ready() bool {
	if s.files != nil {
		return true
	}
	return s.root != "" && staticWebIsDirectory(staticWebResolve(s.root))
}

// readFile reads the file at rel, its segments below the web root, through the
// source's own confinement: symlinks in a directory are followed only while they
// stay inside it, and memory has no links to follow.
func (s staticWebSource) readFile(rel []string) staticWebFileResult {
	if s.files != nil {
		return staticWebReadMemoryFile(s.files, rel)
	}
	if s.root == "" {
		return staticWebFileResult{kind: staticWebMissing}
	}
	rootURL := staticWebResolve(s.root)
	return staticWebReadConfinedFile(filepath.Join(append([]string{rootURL}, rel...)...), rootURL)
}

// staticWebReadMemoryFile reads a file from an in-memory tree with the same result
// kinds as the disk reader. A path fs.FS would not accept is treated as escaping
// the root, as a path that climbs out of a directory is.
func staticWebReadMemoryFile(files fs.FS, rel []string) staticWebFileResult {
	name := strings.Join(rel, "/")
	if !fs.ValidPath(name) {
		return staticWebFileResult{kind: staticWebEscaped}
	}
	info, err := fs.Stat(files, name)
	if err != nil {
		return staticWebFileResult{kind: staticWebMissing}
	}
	if !info.Mode().IsRegular() {
		return staticWebFileResult{kind: staticWebDirectory}
	}
	data, err := fs.ReadFile(files, name)
	if err != nil {
		return staticWebFileResult{kind: staticWebMissing}
	}
	return staticWebFileResult{kind: staticWebFile, data: data}
}
