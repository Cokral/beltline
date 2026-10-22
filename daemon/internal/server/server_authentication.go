// And the
// Credential helpers it uses.
package server

import (
	"crypto/subtle"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/robbertmanders/beltline/daemon/internal/server/api"
	"github.com/robbertmanders/beltline/daemon/internal/server/routes"
)

// AuthenticationMiddleware guards `/api` and everything under it, known path or
// not: the request needs `Authorization: Bearer <token>` or the `beltline_auth`
// cookie holding the token. An Authorization header, once sent, must be that
// bearer even when the cookie is right. Query tokens never count here.
type AuthenticationMiddleware struct {
	token string
	next  http.Handler
}

// ServeHTTP is a request
// outside /api, or one that authenticates, passes; any other /api request gets
// the unauthorized body with 401.
func (m AuthenticationMiddleware) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !authIsAPI(r.URL.EscapedPath()) || authAuthenticates(r.Header, m.token) {
		m.next.ServeHTTP(w, r)
		return
	}
	routes.JSONError(w, api.ErrorUnauthorized, http.StatusUnauthorized)
}

// authIsAPI judged on the decoded,
// collapsed path (up to eight rounds of percent-decoding), so slash and
// percent-encoded aliases cannot slip past this check. path is the raw path.
func authIsAPI(path string) bool {
	decoded := path
	for i := 0; i < 8; i++ {
		next, err := url.PathUnescape(decoded)
		if err != nil || next == decoded {
			break
		}
		decoded = next
	}
	for _, segment := range strings.Split(decoded, "/") {
		if segment == "" {
			continue
		}
		return strings.ToLower(segment) == "api"
	}
	return false
}

// authAuthenticates is a single
// valid bearer when an Authorization header is sent; otherwise every beltline_auth
// cookie must match the cookie value of the token.
func authAuthenticates(h http.Header, token string) bool {
	if authorizations := h.Values("Authorization"); len(authorizations) > 0 {
		if len(authorizations) != 1 {
			return false
		}
		bearer, ok := authBearer(authorizations[0])
		return ok && credentialMatches(bearer, token)
	}
	cookies := authCookies(credentialCookie, h)
	expected := credentialCookieValue(token)
	if len(cookies) == 0 {
		return false
	}
	return !slices.ContainsFunc(cookies, func(cookie string) bool {
		return !credentialMatches(cookie, expected)
	})
}

// authBearer everything after
// `Bearer `, as sent, so a token with spaces still matches.
func authBearer(value string) (string, bool) {
	scheme, token, found := strings.Cut(value, " ")
	if !found || strings.ToLower(scheme) != "bearer" || token == "" {
		return "", false
	}
	return token, true
}

// authCookies every value
// sent for the cookie name, across all Cookie headers.
func authCookies(name string, h http.Header) []string {
	var out []string
	for _, header := range h.Values("Cookie") {
		for _, piece := range strings.Split(header, ";") {
			if piece == "" {
				continue
			}
			pair := strings.Trim(piece, " \t")
			key, value, found := strings.Cut(pair, "=")
			if !found || key != name {
				continue
			}
			out = append(out, value)
		}
	}
	return out
}

// credentialCookie is Credential.cookie: the session cookie's name.
const credentialCookie = "beltline_auth"

// credentialMatches is a comparison whose time
// depends only on the lengths, not on where the values differ.
func credentialMatches(supplied, token string) bool {
	return subtle.ConstantTimeCompare([]byte(supplied), []byte(token)) == 1
}

// credentialCookieValue is the token as the
// cookie carries it, every byte outside RFC 6265's cookie-octet (and `%` itself)
// percent-encoded.
func credentialCookieValue(token string) string {
	var b strings.Builder
	for i := 0; i < len(token); i++ {
		c := token[i]
		plain := c == 0x21 || (c >= 0x23 && c <= 0x2B && c != 0x25) || (c >= 0x2D && c <= 0x3A) ||
			(c >= 0x3C && c <= 0x5B) || (c >= 0x5D && c <= 0x7E)
		if plain {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}
