//go:build !embedweb

// Without the embedweb tag beltlined carries no web app. A Go-only build (go test,
// go vet, go build with no tags) serves the API and answers the web app with the
// not-built page, unless BELTLINE_WEB_DIR names a build; make build embeds the app.
package server

import "io/fs"

// bundledWebFS reports no bundled web app in a Go-only build.
func bundledWebFS() (fs.FS, bool) {
	return nil, false
}
