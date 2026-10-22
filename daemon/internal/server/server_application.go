// ServerApplication: the
// responder for one listener, and the WebSocket upgrade dispatch.
package server

import (
	"net/http"
	"net/url"
	"path"
	"slices"
	"strings"

	"github.com/robbertmanders/beltline/daemon/internal/kit/coordinator"
	"github.com/robbertmanders/beltline/daemon/internal/server/routes"
)

// ServerApplicationRouter is the server application router.
// The order is the security contract: Host and Origin first, then shared error
// mapping, then credentials for `/api`, then the routes (the registry, then any
// test configure hook), then the static web app with its index fallback.
func ServerApplicationRouter(port int, token, version, webRoot string, c *coordinator.Coordinator,
	signer routes.ArtifactLinkSigner, deps routes.Dependencies, configure func(mux *http.ServeMux)) http.Handler {
	return serverApplicationRouter(port, token, version, staticWebSelect(webRoot, staticWebBundled()), c, signer, deps, configure)
}

// serverApplicationRouter is ServerApplicationRouter with its web source already
// chosen, so tests can serve an in-memory tree through the same router.
func serverApplicationRouter(port int, token, version string, web staticWebSource, c *coordinator.Coordinator,
	signer routes.ArtifactLinkSigner, deps routes.Dependencies, configure func(mux *http.ServeMux)) http.Handler {
	deps.Token = token
	deps.Version = version
	deps.Coordinator = c
	deps.ArtifactSigner = signer
	mux := http.NewServeMux()
	routes.RegisterAll(mux, deps)
	AddAuthRoute(mux, token)
	if configure != nil {
		configure(mux)
	}
	staticWebAdd(mux, web, port)
	root := staticWebRoot{mux: mux, web: web, port: port}
	var handler http.Handler = AuthenticationMiddleware{token: token, next: root}
	handler = ErrorMiddleware{next: handler}
	return NewHostOriginMiddleware(port, handler)
}

// staticWebRoot is the routing step under the middleware: paths the mux would
// clean (net/http redirects `..`, `//` and similar) go straight to the static
// handler, so a traversal attempt is answered by StaticWeb's own checks rather
// than a redirect; every other path goes through the mux, whose catch-all is
// the static handler too.
type staticWebRoot struct {
	mux  *http.ServeMux
	web  staticWebSource
	port int
}

// ServeHTTP routes one request (see staticWebRoot).
func (s staticWebRoot) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if path.Clean(r.URL.Path) == r.URL.Path {
		s.mux.ServeHTTP(w, r)
		return
	}
	if clean, ok := staticWebAPIRequest(r); ok {
		s.mux.ServeHTTP(w, clean)
		return
	}
	staticWebHandler(s.web, s.port).ServeHTTP(w, r)
}

// staticWebAPIRequest reads a path the way the router does: it splits on "/" and omits
// empty subsequences, so an authenticated //api/v1/version is api/v1/version and
// answers as that route does (StaticWebTests.reservedNamespacesAndHostOriginKeepTheirExistingPolicies).
// Only an api path with no "." or ".." segment qualifies; the cleaned copy keeps the
// request's values and escapes, and is routed by the mux like any other request.
func staticWebAPIRequest(r *http.Request) (*http.Request, bool) {
	var kept []string
	for _, segment := range strings.Split(r.URL.EscapedPath(), "/") {
		switch segment {
		case "":
			continue
		case ".", "..":
			return nil, false
		}
		kept = append(kept, segment)
	}
	if len(kept) == 0 || kept[0] != "api" {
		return nil, false
	}
	escaped := "/" + strings.Join(kept, "/")
	decoded, err := url.PathUnescape(escaped)
	if err != nil {
		return nil, false
	}
	clean := r.Clone(r.Context())
	clean.URL.Path = decoded
	clean.URL.RawPath = escaped
	return clean, true
}

// upgradeOrRouter sends a WebSocket upgrade to the upgrade handler and every other
// request to the router, as Hummingbird's upgrade channel does.
type upgradeOrRouter struct {
	upgrade http.Handler
	router  http.Handler
}

// ServeHTTP dispatches one request (see upgradeOrRouter).
func (u upgradeOrRouter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if isWebSocketUpgrade(r) {
		u.upgrade.ServeHTTP(w, r)
		return
	}
	u.router.ServeHTTP(w, r)
}

// isWebSocketUpgrade reports whether r asks to switch to the WebSocket protocol.
func isWebSocketUpgrade(r *http.Request) bool {
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		return false
	}
	return slices.ContainsFunc(strings.Split(r.Header.Get("Connection"), ","), func(token string) bool {
		return strings.EqualFold(strings.TrimSpace(token), "upgrade")
	})
}

// ServerApplicationWebSocket is the server application web socket:
// the handler that serves the events path through the event hub and any other
// path through the terminal route, whose relays are registered with terminals so
// the listener's shutdown ends them. Go has no protocol-error channel to put it
// behind (net/http answers the protocol itself), so the events route answers its
// own rejections.
func ServerApplicationWebSocket(port int, token string, c *coordinator.Coordinator, hub *EventHub,
	rejections *EventRejectionRegistry, terminals *terminalTracker) http.Handler {
	terminal := terminalRouteTracked(port, token, c, terminals)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() == routes.EventRoutesPath {
			serveEvents(port, token, hub, rejections, w, r)
			return
		}
		terminal.ServeHTTP(w, r)
	})
}
