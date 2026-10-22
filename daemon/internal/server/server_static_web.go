// The built React shell,
// served from the app compiled into beltlined or from a BELTLINE_WEB_DIR override.
package server

import (
	"fmt"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"github.com/robbertmanders/beltline/daemon/internal/server/api"
	"github.com/robbertmanders/beltline/daemon/internal/server/routes"
)

// staticWebFileResult is StaticWeb's private FileResult: a file's bytes, or why
// the lookup did not give one.
type staticWebFileResult struct {
	kind staticWebFileKind
	data []byte
}

// staticWebFileKind is which FileResult case a staticWebFileResult is.
type staticWebFileKind int

const (
	staticWebFile staticWebFileKind = iota
	staticWebMissing
	staticWebDirectory
	staticWebEscaped
)

// staticWebEncodedSeparator matches an encoded slash or backslash (case-insensitive).
var staticWebEncodedSeparator = regexp.MustCompile(`(?i)%2f|%5c`)

// staticWebEncodedSeparatorOrNul matches an encoded backslash or NUL.
var staticWebEncodedSeparatorOrNul = regexp.MustCompile(`(?i)%5c|%00`)

// staticWebHashedAsset matches a build hash before .js or .css.
var staticWebHashedAsset = regexp.MustCompile(`(?i)-[A-Za-z0-9_-]{8,}\.(js|css)$`)

// staticWebCSPTemplate is the Content-Security-Policy; %d is the port.
const staticWebCSPTemplate = "default-src 'none'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self'; connect-src 'self' ws://127.0.0.1:%d; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'"

// The instructions the not-built page gives: the override names the directory to
// fix, the bundled app names the build step (a Go-only build has no app to serve).
const (
	staticWebOverrideInstruction = "Check BELTLINE_WEB_DIR points to a readable web build containing index.html, then restart beltlined."
	staticWebBundledInstruction  = "Run make build, then restart beltlined; or set BELTLINE_WEB_DIR to a built web app."
)

// staticWebNotBuiltPage is the script-free page answered when the web app is missing.
func staticWebNotBuiltPage(instruction string) string {
	return `<!doctype html><html lang="en"><meta charset="utf-8"><title>Web app not built</title><h1>Web app not built</h1><p>` + instruction + `</p></html>`
}

// StaticWebDefaultRoot is the web root beltlined serves when none is passed in:
// BELTLINE_WEB_DIR when set and non-empty (made absolute from the working
// directory), else empty, which selects the bundled app. Nothing is looked up
// beside the executable. getenv is a parameter so tests need no process state;
// production passes os.Getenv.
func StaticWebDefaultRoot(getenv func(string) string) string {
	if override := getenv("BELTLINE_WEB_DIR"); override != "" {
		return staticWebAbsolute(override)
	}
	return ""
}

// staticWebAdd serves GET and HEAD for `/` and for every other path, answered from
// the web source with the index fallback for routes the app owns. Registered as the
// mux's catch-all; ServerApplicationRouter sends paths the mux would clean straight
// to the same handler.
func staticWebAdd(mux *http.ServeMux, web staticWebSource, port int) {
	mux.Handle("/", staticWebHandler(web, port))
}

// staticWebHandler answers every request that is not a registered route: GET and
// HEAD from the web source, anything else as the JSON 404.
func staticWebHandler(web staticWebSource, port int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead:
			staticWebResponse(w, r.URL.EscapedPath(), port, web, r.Method == http.MethodHead)
		default:
			routes.JSONError(w, api.ErrorNotFound, http.StatusNotFound)
		}
	})
}

// staticWebResponse is the static web response.
func staticWebResponse(w http.ResponseWriter, path string, port int, web staticWebSource, head bool) {
	decoded, ok := staticWebSafePath(path)
	if !ok {
		// A run's route carries its id, which holds encoded slashes. Such a
		// path never names a file, so it reloads through the route shell.
		if !staticWebRouteWithEncodedSeparator(path) {
			staticWebNotFound(w, head)
			return
		}
		if !web.ready() {
			staticWebNotBuilt(w, web, port, head)
			return
		}
		staticWebFallback(w, web, port, head)
		return
	}
	if staticWebIsReserved(decoded) {
		routes.JSONError(w, api.ErrorNotFound, http.StatusNotFound)
		return
	}

	if !web.ready() {
		staticWebNotBuilt(w, web, port, head)
		return
	}

	relative := nonEmptySegments(decoded)
	if len(relative) == 0 {
		staticWebFallback(w, web, port, head)
		return
	}
	name := strings.Join(relative, "/")
	result := web.readFile(relative)
	switch result.kind {
	case staticWebFile:
		staticWebFileResponse(w, result.data, name, port, head, strings.ToLower(filepath.Base(name)) == "index.html")
	case staticWebEscaped, staticWebDirectory:
		staticWebNotFound(w, head)
	default:
		// Missing asset-like paths are real 404s. Extensionless browser
		// paths are the SPA's route shell and reload through index.html.
		if staticWebLooksLikeAsset(relative[len(relative)-1]) {
			staticWebNotFound(w, head)
			return
		}
		staticWebFallback(w, web, port, head)
	}
}

// staticWebFallback index.html for
// an app route, or a not-built response when the build is missing.
func staticWebFallback(w http.ResponseWriter, web staticWebSource, port int, head bool) {
	result := web.readFile([]string{"index.html"})
	switch result.kind {
	case staticWebFile:
		staticWebFileResponse(w, result.data, "index.html", port, head, true)
	case staticWebEscaped:
		staticWebNotFound(w, head)
	default:
		staticWebNotBuilt(w, web, port, head)
	}
}

// staticWebReadConfinedFile reads a
// file only after its symlinks are resolved and it is confirmed to be inside the root.
func staticWebReadConfinedFile(file, root string) staticWebFileResult {
	rootParts := staticWebComponents(root)
	fileParts := staticWebComponents(file)
	prefix := root
	for _, component := range fileParts[len(rootParts):] {
		prefix = filepath.Join(prefix, component)
		if _, err := os.Lstat(prefix); err == nil {
			resolvedPrefix := staticWebResolve(prefix)
			if !staticWebIsWithin(resolvedPrefix, root) {
				return staticWebFileResult{kind: staticWebEscaped}
			}
		}
	}
	resolved := staticWebResolve(file)
	if !staticWebIsWithin(resolved, root) {
		// A nonexistent path also resolves lexically. Check existence
		// first so an absent browser route can use the index fallback.
		if _, err := os.Stat(file); err != nil {
			return staticWebFileResult{kind: staticWebMissing}
		}
		return staticWebFileResult{kind: staticWebEscaped}
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return staticWebFileResult{kind: staticWebMissing}
	}
	if !info.Mode().IsRegular() {
		return staticWebFileResult{kind: staticWebDirectory}
	}
	data, err := os.ReadFile(resolved)
	if err != nil {
		return staticWebFileResult{kind: staticWebMissing}
	}
	return staticWebFileResult{kind: staticWebFile, data: data}
}

// staticWebSafePath is the decoded, normalised path
// relative to the root, or false when it climbs out or names a reserved segment.
func staticWebSafePath(raw string) (string, bool) {
	if raw == "" || raw[0] != '/' || strings.Contains(raw, "\\") || strings.Contains(raw, "\x00") {
		return "", false
	}
	value := raw
	fullyDecoded := false
	for i := 0; i < 8; i++ {
		decoded, ok := routes.RemovingPercentEncoding(value)
		if !ok {
			return "", false
		}
		if strings.Contains(decoded, "\\") || strings.Contains(decoded, "\x00") {
			return "", false
		}
		// Encoded separators can change the component boundaries used by
		// a proxy or an upstream router, so reject them at every layer.
		if staticWebEncodedSeparator.MatchString(value) {
			return "", false
		}
		if decoded == value {
			fullyDecoded = true
			break
		}
		value = decoded
	}
	if !fullyDecoded || value == "" || value[0] != '/' || staticWebEncodedSeparator.MatchString(value) {
		return "", false
	}
	for _, component := range nonEmptySegments(value) {
		if component == "." || component == ".." {
			return "", false
		}
	}
	return value[1:], true
}

// staticWebRouteWithEncodedSeparator is the static web route with encoded separator:
// whether a raw path carries an encoded slash in app-route form (a plain first
// segment, not reserved, not an asset), which is answered as a route, not a file.
func staticWebRouteWithEncodedSeparator(raw string) bool {
	if raw == "" || raw[0] != '/' || strings.Contains(raw, "\\") || strings.Contains(raw, "\x00") {
		return false
	}
	if !strings.Contains(strings.ToLower(raw), "%2f") || staticWebEncodedSeparatorOrNul.MatchString(raw) {
		return false
	}
	segments := nonEmptySegments(raw)
	if len(segments) == 0 {
		return false
	}
	for _, r := range segments[0] {
		if !unicode.IsLetter(r) && !unicode.IsNumber(r) && r != '-' && r != '_' {
			return false
		}
	}
	if staticWebIsReserved(segments[0]) || staticWebLooksLikeAsset(segments[len(segments)-1]) {
		return false
	}
	for _, segment := range segments {
		decoded, ok := routes.RemovingPercentEncoding(segment)
		if !ok {
			return false
		}
		for _, part := range strings.Split(decoded, "/") {
			if part == "." || part == ".." {
				return false
			}
		}
	}
	return true
}

// staticWebIsReserved is a path whose first segment
// is api, auth or artifacts (any case) is never served from the web root.
func staticWebIsReserved(path string) bool {
	segments := nonEmptySegments(path)
	if len(segments) == 0 {
		return false
	}
	switch strings.ToLower(segments[0]) {
	case "api", "auth", "artifacts":
		return true
	}
	return false
}

// staticWebIsWithin file lies strictly below root.
func staticWebIsWithin(file, root string) bool {
	rootParts := staticWebComponents(root)
	fileParts := staticWebComponents(file)
	return len(fileParts) > len(rootParts) && slices.Equal(fileParts[:len(rootParts)], rootParts)
}

// staticWebIsDirectory is the static web is directory.
func staticWebIsDirectory(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// staticWebLooksLikeAsset is a path whose last
// segment has an extension, or the path `assets`, is an asset and gets no index fallback.
func staticWebLooksLikeAsset(path string) bool {
	return pathExtension(path) != "" || path == "assets"
}

// pathExtension is NSString's pathExtension for the last segment of a path: the
// text after its last dot, empty when there is none or the dot leads the name.
func pathExtension(path string) string {
	name := path
	if i := strings.LastIndex(path, "/"); i >= 0 {
		name = path[i+1:]
	}
	i := strings.LastIndex(name, ".")
	if i <= 0 {
		return ""
	}
	return name[i+1:]
}

// staticWebFileResponse is the static web file response.
func staticWebFileResponse(w http.ResponseWriter, data []byte, path string, port int, head, index bool) {
	h := staticWebHeaders(port)
	h.Set("Content-Type", staticWebContentType(path))
	h.Set("Content-Length", fmt.Sprint(len(data)))
	switch {
	case index:
		h.Set("Cache-Control", "no-store")
	case staticWebIsHashedAsset(path):
		h.Set("Cache-Control", "public, max-age=31536000, immutable")
	default:
		h.Set("Cache-Control", "no-cache")
	}
	writeStaticHeader(w, h, http.StatusOK)
	if !head {
		_, _ = w.Write(data)
	}
}

// staticWebNotBuilt is the 503 for a missing
// build, naming the step that fixes the source it was asked to serve.
func staticWebNotBuilt(w http.ResponseWriter, web staticWebSource, port int, head bool) {
	body := staticWebNotBuiltPage(staticWebOverrideInstruction)
	if web.bundled {
		body = staticWebNotBuiltPage(staticWebBundledInstruction)
	}
	h := staticWebHeaders(port)
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("Content-Length", fmt.Sprint(len(body)))
	writeStaticHeader(w, h, http.StatusServiceUnavailable)
	if !head {
		_, _ = w.Write([]byte(body))
	}
}

// staticWebNotFound is the static web not found.
func staticWebNotFound(w http.ResponseWriter, head bool) {
	h := http.Header{}
	h.Set("Content-Type", "text/plain; charset=utf-8")
	h.Set("Content-Length", "9")
	writeStaticHeader(w, h, http.StatusNotFound)
	if !head {
		_, _ = w.Write([]byte("Not found"))
	}
}

// writeStaticHeader copies h onto w and writes the status.
func writeStaticHeader(w http.ResponseWriter, h http.Header, status int) {
	dst := w.Header()
	maps.Copy(dst, h)
	w.WriteHeader(status)
}

// staticWebHeaders is the Content-Security-Policy
// (scripts and connections from this origin, the event socket on the port, no
// objects or framing), nosniff and no-referrer.
func staticWebHeaders(port int) http.Header {
	h := http.Header{}
	h.Set("Content-Security-Policy", fmt.Sprintf(staticWebCSPTemplate, port))
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "no-referrer")
	return h
}

// staticWebIsHashedAsset whether the file name
// carries a build hash (a dash, then 8 or more letters, digits, `_` or `-`, before
// `.js` or `.css`).
func staticWebIsHashedAsset(path string) bool {
	name := filepath.Base(path)
	return staticWebHashedAsset.MatchString(name)
}

// staticWebContentType is the static web content type.
func staticWebContentType(path string) string {
	switch strings.ToLower(pathExtension(path)) {
	case "html":
		return "text/html; charset=utf-8"
	case "js", "mjs":
		return "text/javascript; charset=utf-8"
	case "css":
		return "text/css; charset=utf-8"
	case "json":
		return "application/json; charset=utf-8"
	case "svg":
		return "image/svg+xml"
	case "png":
		return "image/png"
	case "jpg", "jpeg":
		return "image/jpeg"
	case "ico":
		return "image/x-icon"
	case "woff":
		return "font/woff"
	case "woff2":
		return "font/woff2"
	case "ttf":
		return "font/ttf"
	case "otf":
		return "font/otf"
	default:
		return "application/octet-stream"
	}
}

// staticWebAbsolute is a path made absolute and clean, as standardizedFileURL gives.
func staticWebAbsolute(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return filepath.Clean(path)
}

// staticWebResolve is resolvingSymlinksInPath on an absolute path: the real path
// when it exists, else the cleaned path (a missing path has no links to follow).
func staticWebResolve(path string) string {
	abs := staticWebAbsolute(path)
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved
	}
	return abs
}

// staticWebComponents splits an absolute, clean path into its components, the
// root first, as pathComponents does (the leading empty element is the root).
func staticWebComponents(path string) []string {
	return strings.Split(filepath.Clean(path), "/")
}

// nonEmptySegments splits path on "/" and drops the empty segments.
func nonEmptySegments(path string) []string {
	var out []string
	for _, segment := range strings.Split(path, "/") {
		if segment != "" {
			out = append(out, segment)
		}
	}
	return out
}
