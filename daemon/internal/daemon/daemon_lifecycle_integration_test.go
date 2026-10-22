//go:build integration

package daemon

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"encoding/json"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/store"
	"github.com/robbertmanders/beltline/daemon/internal/kit/coordinator"
	"github.com/robbertmanders/beltline/daemon/internal/kit/demo"
	"github.com/robbertmanders/beltline/daemon/internal/kit/launch"
	"github.com/robbertmanders/beltline/daemon/internal/server"
	"github.com/robbertmanders/beltline/daemon/internal/server/routes"
	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// errInjected is the failure the lifecycle tests inject.
var errInjected = errors.New("injected failure")

// TestStartupFailuresRemoveReadinessAndReleaseOwnership
func TestStartupFailuresRemoveReadinessAndReleaseOwnership(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	root := t.TempDir()
	stale := launch.DaemonInfo{Pid: 1, Port: 7331, Token: strings.Repeat("f", 64), Version: "stale", APIVersion: 1}
	if err := stale.Publish(root); err != nil {
		t.Fatal(err)
	}

	tokenHooks := NewHooks()
	tokenHooks.TokenSource = func() (string, error) { return "", errInjected }
	tokenDaemon := NewDaemon(ModeLive, &store.Paths{Root: root}, false, tokenHooks)
	if err := tokenDaemon.Start(ctx); err == nil {
		t.Fatal("start succeeded without a token")
	}
	tokenDaemon.Shutdown(ctx)
	if _, err := os.Stat(filepath.Join(root, launch.DaemonInfoFileName)); err == nil {
		t.Fatal("discovery survived a token failure")
	}
	acquireAndRelease(t, root)

	listenerRoot := t.TempDir()
	readyLines := 0
	listenerHooks := NewHooks()
	listenerHooks.ReadyLine = func(int) { readyLines++ }
	listenerHooks.ServerFactory = func(c *coordinator.Coordinator, token, version string) *server.BeltlineServer {
		// An out-of-range starting port makes the listener refuse to start.
		return server.NewBeltlineServer(token, version, c, 70000, "", routes.Dependencies{})
	}
	listenerDaemon := NewDaemon(ModeLive, &store.Paths{Root: listenerRoot}, false, listenerHooks)
	if err := listenerDaemon.Start(ctx); err == nil {
		t.Fatal("start succeeded without a listener")
	}
	if readyLines != 0 {
		t.Fatalf("%d ready lines after a listener failure", readyLines)
	}
	listenerDaemon.Shutdown(ctx)
	if _, err := os.Stat(filepath.Join(listenerRoot, launch.DaemonInfoFileName)); err == nil {
		t.Fatal("discovery survived a listener failure")
	}
	acquireAndRelease(t, listenerRoot)

	publishRoot := t.TempDir()
	publishedPort := 0
	publishReadyLines := 0
	publishHooks := NewHooks()
	publishHooks.ListenerReady = func(port int) { publishedPort = port }
	publishHooks.ReadyLine = func(int) { publishReadyLines++ }
	publishHooks.Publish = func(launch.DaemonInfo, string) error { return errInjected }
	publishDaemon := NewDaemon(ModeLive, &store.Paths{Root: publishRoot}, false, publishHooks)
	if err := publishDaemon.Start(ctx); err == nil {
		t.Fatal("start succeeded without discovery")
	}
	if publishReadyLines != 0 {
		t.Fatalf("%d ready lines after a discovery failure", publishReadyLines)
	}
	publishDaemon.Shutdown(ctx)
	if _, err := os.Stat(filepath.Join(publishRoot, launch.DaemonInfoFileName)); err == nil {
		t.Fatal("discovery survived a publish failure")
	}
	acquireAndRelease(t, publishRoot)
	if publishedPort == 0 {
		t.Fatal("the discovery hook did not observe the ready listener")
	}
	if reply, err := tryAPIRequest(publishedPort, "not-a-real-token"); err == nil && reply.status == 200 {
		t.Fatal("the stopped listener still answers")
	}

	logRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(logRoot, DaemonLogFileName), 0o755); err != nil {
		t.Fatal(err)
	}
	logDaemon := NewDaemon(ModeLive, &store.Paths{Root: logRoot}, false, NewHooks())
	if err := logDaemon.Start(ctx); err == nil {
		t.Fatal("start succeeded without a log")
	}
	logDaemon.Shutdown(ctx)
	acquireAndRelease(t, logRoot)

	environment := processEnvironment()
	environment["BELTLINE_DATA_DIR"] = logRoot
	process := startDaemonProcess(t, nil, environment)
	t.Cleanup(process.forceStop)
	if status := process.waitExit(t); status == 0 {
		t.Fatal("a daemon without a log exited with 0")
	}
	if !strings.Contains(process.stderr.String(), DaemonLogFileName) {
		t.Fatalf("stderr %q does not name the log", process.stderr.String())
	}
	acquireAndRelease(t, logRoot)
}

// TestReadyLineFollowsPublishedDiscoveryAndOpenFailureKeepsItsOwnDemoAlive
func TestReadyLineFollowsPublishedDiscoveryAndOpenFailureKeepsItsOwnDemoAlive(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	var events []string
	var openedFolder string
	openError := ""
	token := strings.Repeat("c", 64)
	hooks := NewHooks()
	hooks.TokenSource = func() (string, error) { return token, nil }
	hooks.Publish = func(info launch.DaemonInfo, root string) error {
		if err := info.Publish(root); err != nil {
			return err
		}
		events = append(events, "published")
		return nil
	}
	hooks.ReadyLine = func(int) { events = append(events, "ready") }
	hooks.OpenDaemon = func(folder string) error {
		events = append(events, "open")
		openedFolder = folder
		return errInjected
	}
	hooks.OpenFailed = func(message string) {
		events = append(events, "failed")
		openError = message
	}
	daemon := NewDaemon(ModeDemo, nil, true, hooks)
	if err := daemon.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if strings.Join(events, ",") != "published,ready" {
		t.Fatalf("events %v", events)
	}
	daemon.openBrowserIfRequested()
	if openedFolder == "" {
		t.Fatal("no browser folder")
	}
	if !strings.HasPrefix(filepath.Base(openedFolder), demo.DemoRootPrefix) {
		t.Fatalf("opened %q, not a demo folder", openedFolder)
	}
	if strings.Join(events, ",") != "published,ready,open,failed" {
		t.Fatalf("events %v", events)
	}
	if strings.Contains(strings.ToLower(openError), "token") {
		t.Fatalf("open error %q mentions the token", openError)
	}
	var info launch.DaemonInfo
	if err := json.Unmarshal(readFile(t, filepath.Join(openedFolder, launch.DaemonInfoFileName)), &info); err != nil {
		t.Fatal(err)
	}
	if info.Token != token {
		t.Fatal("the record holds another token")
	}
	if reply := apiRequest(t, info.Port, info.Token, "", ""); reply.status != 200 {
		t.Fatalf("demo status %d", reply.status)
	}
	daemon.Shutdown(ctx)
	if _, err := os.Stat(openedFolder); err == nil {
		t.Fatal("the demo folder survived shutdown")
	}
}

// TestDaemonServerFactoryReceivesTheLaunchedCoordinator checks that the server factory gets the coordinator
// the daemon launched, and the state route it serves answers with the published discovery's credentials.
func TestDaemonServerFactoryReceivesTheLaunchedCoordinator(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	var received *coordinator.Coordinator
	hooks := NewHooks()
	hooks.TokenSource = func() (string, error) { return "coordinator-hook-test-token", nil }
	hooks.ServerFactory = func(c *coordinator.Coordinator, token, version string) *server.BeltlineServer {
		received = c
		return server.NewBeltlineServer(token, version, c, 0, "", routes.Dependencies{})
	}
	daemon := NewDaemon(ModeLive, &store.Paths{Root: root}, false, hooks)
	ctx := context.Background()
	t.Cleanup(func() {
		daemon.Shutdown(ctx)
		acquireAndRelease(t, root)
	})
	if err := daemon.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if received == nil || received != daemon.LaunchedCoordinator() {
		t.Fatal("the server factory did not receive the launched coordinator")
	}

	info := discoveryIn(t, root)
	reply := apiRequest(t, info.Port, info.Token, "/api/v1/state", "")
	if reply.status != 200 {
		t.Fatalf("state: HTTP %d", reply.status)
	}
	members, err := jsonx.Members(reply.body)
	if err != nil {
		t.Fatalf("state: %v", err)
	}
	if string(members["globallyPaused"]) != "false" {
		t.Fatalf("globallyPaused %s, want false", members["globallyPaused"])
	}
	daemon.Shutdown(ctx)
	if _, err := os.Stat(filepath.Join(root, launch.DaemonInfoFileName)); err == nil {
		t.Fatal("discovery survived the shutdown")
	}
	acquireAndRelease(t, root)
}

// discoveryIn reads the published discovery record in folder.
func discoveryIn(t *testing.T, folder string) launch.DaemonInfo {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(folder, launch.DaemonInfoFileName))
	if err != nil {
		t.Fatalf("discovery: %v", err)
	}
	var info launch.DaemonInfo
	if err := json.Unmarshal(data, &info); err != nil {
		t.Fatalf("discovery: %v", err)
	}
	return info
}
