package server

import (
	"fmt"
	"net/http"

	"github.com/robbertmanders/beltline/daemon/internal/server/api"
	"github.com/robbertmanders/beltline/daemon/internal/server/routes"
)

// AuthRoute is `GET /auth?token=<token>`: it trades the token for the
// `beltline_auth` session cookie and sends the browser to `/`, so the token
// leaves the address bar. The cookie goes only to `/api`, never across sites and
// never to scripts. Secure is left off: this listener speaks plain HTTP on loopback.
//
// AddAuthRoute is the add auth route.
func AddAuthRoute(mux *http.ServeMux, token string) {
	mux.HandleFunc("GET /auth", func(w http.ResponseWriter, r *http.Request) {
		tokens := routes.PercentDecodedQueryValues("token", r.URL.RawQuery)
		if len(tokens) != 1 || tokens[0] == "" || !credentialMatches(tokens[0], token) {
			routes.JSONError(w, api.ErrorUnauthorized, http.StatusUnauthorized)
			return
		}
		h := w.Header()
		h.Set("Location", "/")
		h.Set("Set-Cookie", fmt.Sprintf("%s=%s; Path=/api; HttpOnly; SameSite=Strict", credentialCookie, credentialCookieValue(token)))
		h.Set("Cache-Control", "no-store")
		h.Set("Referrer-Policy", "no-referrer")
		w.WriteHeader(http.StatusFound)
	})
}
