//go:build embedweb

// The web app compiled into beltlined. scripts/web.sh stages the production build in
// webdist/ before an embedweb build (make build, make install), and this package does
// not compile without that tree, so an embedded executable always carries its app.
package server

import (
	"embed"
	"io/fs"
)

// bundledWebFiles is the production web app, staged in webdist/ by scripts/web.sh.
//
//go:embed all:webdist
var bundledWebFiles embed.FS

// bundledWebFS is the bundled web app, rooted at its index.html.
func bundledWebFS() (fs.FS, bool) {
	files, err := fs.Sub(bundledWebFiles, "webdist")
	if err != nil {
		return nil, false
	}
	return files, true
}
