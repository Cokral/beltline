package servertest

import (
	"net/http"
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/server/api"
	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// require422Problem checks a 422 answer that carries exactly message and problems.
func require422Problem(t testing.TB, reply Reply, message string, problems []string) {
	t.Helper()
	if reply.Status != http.StatusUnprocessableEntity {
		t.Fatalf("expected HTTP 422, got %d", reply.Status)
	}
	if got := reply.Header.Get("Content-Type"); got != "application/json" {
		t.Fatalf("422 Content-Type %q", got)
	}
	if got := reply.Header.Get("Cache-Control"); got != "no-store" {
		t.Fatalf("422 Cache-Control %q", got)
	}
	want, err := jsonx.Marshal(api.ErrorResponse{Message: message, Problems: problems})
	if err != nil {
		t.Fatalf("encode error: %v", err)
	}
	if string(reply.Body) != string(want) {
		t.Fatalf("422 body %s, want %s", reply.Body, want)
	}
}
