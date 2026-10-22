// Tests of the server lifecycle that need no bound port. The listener cases are in
// server_listener_integration_test.go.

package server

import (
	"context"
	"errors"
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/server/routes"
)

// TestDefaultsToPort7331
func TestDefaultsToPort7331(t *testing.T) {
	t.Parallel()
	server := NewBeltlineServer(testToken, "dev", nil, 0, "", routes.Dependencies{})
	if DefaultPort != 7331 {
		t.Fatalf("DefaultPort %d", DefaultPort)
	}
	if server.StartingPort != 7331 {
		t.Fatalf("StartingPort %d", server.StartingPort)
	}
	if server.bindHost != "127.0.0.1" {
		t.Fatalf("bindHost %q", server.bindHost)
	}
}

// TestRefusesInvalidInput
func TestRefusesInvalidInput(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		token   string
		version string
		port    int
		want    StartError
	}{
		{"port zero", testToken, "dev", 0, StartError{Kind: StartErrorInvalidPort, Port: 0}},
		{"port too high", testToken, "dev", 65536, StartError{Kind: StartErrorInvalidPort, Port: 65536}},
		{"negative port", testToken, "dev", -1, StartError{Kind: StartErrorInvalidPort, Port: -1}},
		{"empty token", "", "dev", 7331, StartError{Kind: StartErrorEmptyToken}},
		{"empty version", testToken, "", 7331, StartError{Kind: StartErrorEmptyVersion}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			server := newBeltlineServerForTest(c.token, c.version, c.port, "127.0.0.1", testWebRoot(t), nil,
				routes.EventWriteConfigurationProduction, nil, nil, routes.Dependencies{}, nil)
			_, err := server.Start(context.Background())
			var got StartError
			if !errors.As(err, &got) || got != c.want {
				t.Fatalf("Start error %v, want %v", err, c.want)
			}
			if port, running := server.Port(); running {
				t.Fatalf("port %d published after a refused start", port)
			}
			server.Stop(context.Background())
		})
	}
}
