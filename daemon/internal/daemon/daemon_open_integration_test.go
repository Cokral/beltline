//go:build integration

package daemon

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"encoding/json"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/shell"
	"github.com/robbertmanders/beltline/daemon/internal/kit/launch"
	"github.com/robbertmanders/beltline/daemon/internal/server"
	"github.com/robbertmanders/beltline/daemon/internal/server/routes"
)

// TestRealVersionProbeAndAuthExchangeSetOnlyTheApiCookie
func TestRealVersionProbeAndAuthExchangeSetOnlyTheApiCookie(t *testing.T) {
	t.Parallel()
	folder := t.TempDir()
	token := strings.Repeat("a", 64)
	webRoot := filepath.Join(folder, "missing web")
	listener := server.NewBeltlineServer(token, "open-integration-build", nil, 24000, webRoot, routes.Dependencies{})
	port, err := listener.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Stop(context.Background()) })

	info := launch.DaemonInfo{Pid: int32(os.Getpid()), Port: port, Token: token, Version: "open-integration-build", APIVersion: launch.VersionCurrent}
	if err := info.Publish(folder); err != nil {
		t.Fatal(err)
	}
	runner := &shell.Recorder{}
	if err := (DaemonOpen{Runner: runner, Discovery: newDaemonDiscovery()}).Run(context.Background(), folder); err != nil {
		t.Fatal(err)
	}
	calls := runner.Calls()
	if len(calls) == 0 || len(calls[0].Args) == 0 {
		t.Fatal("no browser call")
	}
	authURL := calls[0].Args[0]
	if want := fmt.Sprintf("http://127.0.0.1:%d/auth?token=%s", port, token); authURL != want {
		t.Fatalf("auth URL %q, want %q", authURL, want)
	}

	// The exchange must not follow the redirect, so the cookie is read from the first reply.
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	authResponse, err := client.Get(authURL)
	if err != nil {
		t.Fatal(err)
	}
	_ = authResponse.Body.Close() // the response is read; a close error has nothing left to report
	if authResponse.StatusCode != http.StatusFound {
		t.Fatalf("auth status %d, want 302", authResponse.StatusCode)
	}
	if location := authResponse.Header.Get("Location"); location != "/" {
		t.Fatalf("auth location %q", location)
	}
	var apiCookie *http.Cookie
	for _, cookie := range authResponse.Cookies() {
		if cookie.Name == "beltline_auth" {
			apiCookie = cookie
		}
	}
	if apiCookie == nil {
		t.Fatal("no beltline_auth cookie")
	}
	if !apiCookie.HttpOnly {
		t.Fatal("the API cookie is not HttpOnly")
	}
	if apiCookie.Path != "/api" {
		t.Fatalf("API cookie path %q", apiCookie.Path)
	}

	request, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/api/v1/version", port), nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Cookie", apiCookie.Name+"="+apiCookie.Value)
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }() // the response is read; a close error has nothing left to report
	if response.StatusCode != http.StatusOK {
		t.Fatalf("version status %d", response.StatusCode)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	var version launch.Version
	if err := json.Unmarshal(body, &version); err != nil {
		t.Fatal(err)
	}
	if version != (launch.Version{APIVersion: 1, Version: "open-integration-build", Platform: (launch.Platform{}).Name()}) {
		t.Fatalf("version %+v", version)
	}
}
