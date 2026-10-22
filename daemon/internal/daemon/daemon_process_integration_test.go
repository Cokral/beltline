//go:build integration

package daemon

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"encoding/json"
	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/store"
	"github.com/robbertmanders/beltline/daemon/internal/kit/demo"
	"github.com/robbertmanders/beltline/daemon/internal/kit/launch"
	"github.com/robbertmanders/beltline/daemon/internal/server"
	"github.com/robbertmanders/beltline/daemon/internal/server/api"
	"github.com/robbertmanders/beltline/daemon/internal/server/routes"
	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// TestLiveOwnerPublishesServesRefusesContendersAndRestartsCleanly
func TestLiveOwnerPublishesServesRefusesContendersAndRestartsCleanly(t *testing.T) {
	t.Parallel()
	root := filepath.Join(t.TempDir(), "live data with spaces")
	environment := processEnvironment()
	environment["BELTLINE_DATA_DIR"] = root
	environment["BELTLINE_VERSION"] = "test-build"

	first := startDaemonProcess(t, nil, environment)
	t.Cleanup(first.forceStop)
	firstInfo := waitForDiscovery(t, root, first)
	if firstInfo.Pid != first.pid() {
		t.Fatalf("pid %d, want %d", firstInfo.Pid, first.pid())
	}
	if firstInfo.Version != "test-build" {
		t.Fatalf("version %q", firstInfo.Version)
	}
	if firstInfo.APIVersion != launch.VersionCurrent {
		t.Fatalf("api version %d", firstInfo.APIVersion)
	}
	if len(firstInfo.Token) != 64 {
		t.Fatalf("token length %d", len(firstInfo.Token))
	}
	if firstInfo.Port < server.DefaultPort {
		t.Fatalf("port %d below the default", firstInfo.Port)
	}
	if mode := fileMode(t, filepath.Join(root, launch.DaemonInfoFileName)); mode != 0o600 {
		t.Fatalf("daemon.json mode %o", mode)
	}

	authorized := apiRequest(t, firstInfo.Port, firstInfo.Token, "", "")
	if authorized.status != 200 {
		t.Fatalf("authorized status %d", authorized.status)
	}
	var version launch.Version
	if err := json.Unmarshal(authorized.body, &version); err != nil {
		t.Fatal(err)
	}
	if version != (launch.Version{APIVersion: 1, Version: "test-build", Platform: (launch.Platform{}).Name()}) {
		t.Fatalf("version body %+v", version)
	}
	if reply := apiRequest(t, firstInfo.Port, "", "", ""); reply.status != 401 {
		t.Fatalf("no token: status %d", reply.status)
	}
	if reply := apiRequest(t, firstInfo.Port, "wrong", "", ""); reply.status != 401 {
		t.Fatalf("wrong token: status %d", reply.status)
	}

	lockFile := launch.OwnershipLockFile(root)
	lockBytes := readFile(t, lockFile)
	lockInode := fileInode(t, lockFile)
	daemonBytes := readFile(t, filepath.Join(root, launch.DaemonInfoFileName))
	runsBefore := folderTree(t, filepath.Join(root, "runs"))
	contender := startDaemonProcess(t, nil, environment)
	t.Cleanup(contender.forceStop)
	if status := contender.waitExit(t); status == 0 {
		t.Fatal("a contender exited with 0")
	}
	contenderError := contender.stderr.String()
	if !strings.Contains(contenderError, root) {
		t.Fatalf("contender error %q lacks the folder", contenderError)
	}
	if !strings.Contains(contenderError, fmt.Sprintf("owner pid: %d", first.pid())) {
		t.Fatalf("contender error %q lacks the owner", contenderError)
	}
	if fileInode(t, lockFile) != lockInode {
		t.Fatal("the lock file was replaced")
	}
	if !bytes.Equal(readFile(t, lockFile), lockBytes) {
		t.Fatal("the lock file changed")
	}
	if !bytes.Equal(readFile(t, filepath.Join(root, launch.DaemonInfoFileName)), daemonBytes) {
		t.Fatal("the discovery record changed")
	}
	if fmt.Sprint(folderTree(t, filepath.Join(root, "runs"))) != fmt.Sprint(runsBefore) {
		t.Fatal("the contender changed the runs folder")
	}

	// A second launch on the daemon's folder is refused and names the daemon as the owner.
	_, err := launch.LaunchSessionLive(store.Paths{Root: root}, func(string, string) {}, nil, nil)
	var failure launch.LaunchFailure
	if !errors.As(err, &failure) || failure.Kind != launch.LaunchFailureHeld || failure.Owner == nil || *failure.Owner != first.pid() {
		t.Fatalf("second launch error %v, want held by pid %d", err, first.pid())
	}
	discovery := newDaemonDiscovery()
	verified, err := discovery.VerifyOwnedBy(context.Background(), root, first.pid())
	if err != nil {
		t.Fatal(err)
	}
	if verified.Pid != first.pid() {
		t.Fatalf("verified pid %d", verified.Pid)
	}
	wantSignIn := fmt.Sprintf("http://127.0.0.1:%d/auth?token=%s", firstInfo.Port, firstInfo.Token)
	if verified.SignInURL != wantSignIn {
		t.Fatalf("sign-in URL %q", verified.SignInURL)
	}
	if _, err := discovery.VerifyOwnedBy(context.Background(), root, contender.pid()); err == nil || !strings.Contains(err.Error(), "is not the lock's owner") {
		t.Fatalf("a pid that does not own the folder verified: %v", err)
	}
	if !bytes.Equal(readFile(t, lockFile), lockBytes) {
		t.Fatal("verification changed the lock file")
	}

	// A check works while the daemon owns the folder and leaves it alone.
	beforeCheck := folderTree(t, root)
	checkOutput, checkStatus := runDaemonBinary(t, []string{"--check-library", root}, environment)
	shared := launch.LibraryCheckCommandCheck(store.Paths{Root: root})
	if checkOutput != shared.Output || checkStatus != int(shared.Status) {
		t.Fatalf("check %q/%d, want %q/%d", checkOutput, checkStatus, shared.Output, shared.Status)
	}
	if fmt.Sprint(folderTree(t, root)) != fmt.Sprint(beforeCheck) {
		t.Fatal("the check changed the data folder")
	}

	first.signal(syscall.SIGTERM)
	if status := first.waitExit(t); status != 0 {
		t.Fatalf("first daemon exited with %d", status)
	}
	if _, err := os.Stat(filepath.Join(root, launch.DaemonInfoFileName)); err == nil {
		t.Fatal("discovery survived the clean stop")
	}
	if reply, err := tryAPIRequest(firstInfo.Port, firstInfo.Token); err == nil && reply.status == 200 {
		t.Fatal("the stopped daemon still answers")
	}
	if _, err := os.Stat(filepath.Join(root, "beltlined.log")); err != nil {
		t.Fatal("beltlined.log is missing")
	}
	if _, err := os.Stat(lockFile); err != nil {
		t.Fatal("the lock file is missing")
	}
	if owner := launch.OwnershipLockRecordedOwner(root); owner == nil || *owner != first.pid() {
		t.Fatalf("recorded owner %v", owner)
	}
	acquireAndRelease(t, root)

	// SIGKILL leaves stale discovery but releases the kernel lock. The next daemon
	// replaces the record with a fresh token and pid.
	second := startDaemonProcess(t, nil, environment)
	t.Cleanup(second.forceStop)
	secondInfo := waitForDiscovery(t, root, second)
	if secondInfo.Token == firstInfo.Token {
		t.Fatal("the second daemon reused the first token")
	}
	if secondInfo.Pid != second.pid() {
		t.Fatalf("second pid %d", secondInfo.Pid)
	}
	second.signal(syscall.SIGKILL)
	if status := second.waitExit(t); status == 0 {
		t.Fatal("a killed daemon exited with 0")
	}
	if _, err := os.Stat(filepath.Join(root, launch.DaemonInfoFileName)); err != nil {
		t.Fatal("stale discovery was removed by a kill")
	}

	third := startDaemonProcess(t, nil, environment)
	t.Cleanup(third.forceStop)
	thirdInfo := waitForDiscovery(t, root, third)
	if thirdInfo.Pid != third.pid() {
		t.Fatalf("third pid %d", thirdInfo.Pid)
	}
	if thirdInfo.Token == secondInfo.Token {
		t.Fatal("the third daemon reused the second token")
	}
	if reply := apiRequest(t, thirdInfo.Port, secondInfo.Token, "", ""); reply.status != 401 {
		t.Fatalf("the previous token got %d", reply.status)
	}
	third.signal(syscall.SIGINT)
	if status := third.waitExit(t); status != 0 {
		t.Fatalf("third daemon exited with %d", status)
	}
	if _, err := os.Stat(filepath.Join(root, launch.DaemonInfoFileName)); err == nil {
		t.Fatal("discovery survived the third stop")
	}
	log := readFile(t, filepath.Join(root, "beltlined.log"))
	for _, want := range []string{"Daemon ready", "Daemon stopped"} {
		if !bytes.Contains(log, []byte(want)) {
			t.Fatalf("log lacks %q", want)
		}
	}
	for _, token := range []string{firstInfo.Token, secondInfo.Token, thirdInfo.Token} {
		if bytes.Contains(log, []byte(token)) {
			t.Fatal("the log holds a token")
		}
	}
}

// sameWire reports whether a and b encode to the same JSON.
func sameWire(t *testing.T, a, b any) bool {
	t.Helper()
	left, err := jsonx.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	right, err := jsonx.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	return bytes.Equal(left, right)
}

// TestLiveDaemonServesItsLoadedStateAndRunsWithDiscoveryCredentials checks that a live daemon process serves the
// state and the runs it loaded from the data folder, to the discovery's token only.
func TestLiveDaemonServesItsLoadedStateAndRunsWithDiscoveryCredentials(t *testing.T) {
	t.Parallel()
	root := filepath.Join(t.TempDir(), "daemon api fixture")
	storage := store.NewStorage(store.Paths{Root: root})
	pausedAt := jsonx.Date{Time: time.Unix(1_791_356_400, 0).UTC()}
	settings := store.DefaultSettings()
	settings.Paused = true
	settings.PausedAt = &pausedAt
	if err := storage.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}

	stage := core.NewStage(core.StageArgs{ID: "triage", Type: core.StageTypeAgent,
		Agent: &core.AgentRef{ID: "triage", Version: 1}, Outcomes: []core.Outcome{}})
	belt, err := storage.SaveNewVersion(core.Belt{
		ID: "daemon-contract", Name: "Daemon contract", Version: 1, Policies: core.DefaultPolicies(),
		Triggers: []core.TriggerEntry{{ID: "1", Trigger: core.Trigger{Kind: core.TriggerKindManual}, Repos: []string{"acme/daemon"}}},
		Stages:   []core.Stage{stage},
	})
	if err != nil {
		t.Fatal(err)
	}
	subject := core.Subject{Kind: core.SubjectKindManual, Repo: "acme/daemon", Key: "persisted-daemon-fixture",
		Title: "Coordinator data fixture", IsOpen: true}
	savedRun := core.NewRun(core.RunArgs{Belt: belt, Subject: subject, Now: time.Unix(1_791_356_400, 0).UTC()})
	reason := "Fixture waits while globally paused"
	savedRun.State = core.RunStateWaiting
	savedRun.Reason = &reason
	savedRun.Legs[0].State = core.RunStateWaiting
	savedRun.Legs[0].Reason = &reason
	if err := storage.SaveRun(savedRun); err != nil {
		t.Fatal(err)
	}

	environment := processEnvironment()
	environment["BELTLINE_DATA_DIR"] = root
	environment["BELTLINE_VERSION"] = "daemon-api-test"
	process := startDaemonProcess(t, nil, environment)
	t.Cleanup(process.forceStop)
	info := waitForDiscovery(t, root, process)

	state := apiRequest(t, info.Port, info.Token, "/api/v1/state", "")
	if state.status != 200 || state.contentType != "application/json" || state.cacheControl != "no-store" {
		t.Fatalf("state: HTTP %d %q %q", state.status, state.contentType, state.cacheControl)
	}
	if bytes.Contains(state.body, []byte(info.Token)) {
		t.Fatal("the state exposes the token")
	}
	members, err := jsonx.Members(state.body)
	if err != nil {
		t.Fatalf("state: %v", err)
	}
	if string(members["globallyPaused"]) != "true" {
		t.Fatalf("globallyPaused %s, want true", members["globallyPaused"])
	}

	runs := apiRequest(t, info.Port, info.Token, "/api/v1/runs", "")
	if runs.status != 200 || runs.contentType != "application/json" || runs.cacheControl != "no-store" {
		t.Fatalf("runs: HTTP %d %q %q", runs.status, runs.contentType, runs.cacheControl)
	}
	if bytes.Contains(runs.body, []byte(info.Token)) {
		t.Fatal("the runs expose the token")
	}
	var list api.RunsResponse
	if err := routes.JSONDecode(runs.body, &list); err != nil {
		t.Fatalf("runs: %v", err)
	}
	var returned *api.RunSnapshot
	for i := range list.Runs {
		if list.Runs[i].Run.ID == savedRun.ID {
			returned = &list.Runs[i]
		}
	}
	if returned == nil {
		t.Fatalf("the saved run %s is not served", savedRun.ID)
	}
	// The served values are compared as their wire forms: an empty list and a nil one are the same there.
	for _, pair := range []struct {
		name      string
		got, want any
	}{
		{"subject", returned.Run.Subject, savedRun.Subject},
		{"legs", returned.Run.Legs, savedRun.Legs},
		{"stages", returned.Run.Stages, savedRun.Stages},
	} {
		if !sameWire(t, pair.got, pair.want) {
			t.Fatalf("the served %s are not the saved ones", pair.name)
		}
	}
	if returned.Run.StageID != stage.ID || returned.Run.State != core.RunStateWaiting {
		t.Fatalf("stage %v, state %v: want %s and waiting", returned.Run.StageID, returned.Run.State, stage.ID)
	}
	if len(returned.Actions) == 0 {
		t.Fatal("the served run carries no actions")
	}

	for _, path := range []string{"/api/v1/state", "/api/v1/runs"} {
		if reply := apiRequest(t, info.Port, "", path, ""); reply.status != 401 {
			t.Fatalf("%s without a token: HTTP %d", path, reply.status)
		}
		if reply := apiRequest(t, info.Port, "wrong-token", path, ""); reply.status != 401 {
			t.Fatalf("%s with the wrong token: HTTP %d", path, reply.status)
		}
	}
	if reply := apiRequest(t, info.Port, info.Token, "/api/v1/state", "https://foreign.invalid"); reply.status != 403 {
		t.Fatalf("state from a foreign origin: HTTP %d", reply.status)
	}

	process.signal(syscall.SIGTERM)
	if code := process.waitExit(t); code != 0 {
		t.Fatalf("exit status %d", code)
	}
	if _, err := os.Stat(filepath.Join(root, launch.DaemonInfoFileName)); err == nil {
		t.Fatal("the discovery record survived the exit")
	}
	acquireAndRelease(t, root)
}

// TestDemoFoldersAreIndependentSweptAndRemovedOnShutdown
func TestDemoFoldersAreIndependentSweptAndRemovedOnShutdown(t *testing.T) {
	t.Parallel()
	temporary := filepath.Join(t.TempDir(), "daemon temp with spaces")
	if err := os.MkdirAll(temporary, 0o755); err != nil {
		t.Fatal(err)
	}
	liveSentinel := filepath.Join(temporary, "live sentinel")
	if err := os.MkdirAll(liveSentinel, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFixtureFile(t, filepath.Join(liveSentinel, "sentinel.txt"), "leave me")
	sentinelBefore := readFile(t, filepath.Join(liveSentinel, "sentinel.txt"))
	marker := filepath.Join(temporary, "external-cli-called")
	fakeCLI := filepath.Join(temporary, "fake-agent.sh")
	if err := os.WriteFile(fakeCLI, []byte("#!/bin/sh\nprintf x >> \"$CALLED_MARKER\"\nexit 91\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	environment := processEnvironment()
	environment["TMPDIR"] = temporary
	environment["BELTLINE_DATA_DIR"] = liveSentinel
	environment["CALLED_MARKER"] = marker
	for _, key := range []string{"BELTLINE_PI", "BELTLINE_COPILOT", "BELTLINE_CLAUDE", "BELTLINE_CODEX", "BELTLINE_CURSOR"} {
		environment[key] = fakeCLI
	}
	parent := demo.DemoRootParent(func(key string) string { return environment[key] }, temporary)

	first := startDaemonProcess(t, []string{"--demo"}, environment)
	t.Cleanup(first.forceStop)
	firstFolder := waitForDemoFolder(t, parent, first)
	if _, err := os.Stat(filepath.Join(firstFolder, "runs")); err != nil {
		t.Fatal("the demo has no runs folder")
	}
	if _, err := os.Stat(filepath.Join(firstFolder, launch.DaemonInfoFileName)); err != nil {
		t.Fatal("the demo has no discovery record")
	}

	second := startDaemonProcess(t, []string{"--demo"}, environment)
	t.Cleanup(second.forceStop)
	secondFolder := waitForDemoFolder(t, parent, second, firstFolder)
	if firstFolder == secondFolder {
		t.Fatal("two demos share a folder")
	}
	if _, err := os.Stat(firstFolder); err != nil {
		t.Fatal("the first demo folder was removed while its daemon ran")
	}

	first.signal(syscall.SIGKILL)
	first.waitExit(t)
	if _, err := os.Stat(firstFolder); err != nil {
		t.Fatal("a killed demo's folder was removed at once")
	}

	third := startDaemonProcess(t, []string{"--demo"}, environment)
	t.Cleanup(third.forceStop)
	thirdFolder := waitForDemoFolder(t, parent, third, secondFolder, firstFolder)
	if _, err := os.Stat(firstFolder); err == nil {
		t.Fatal("the next demo did not sweep the abandoned folder")
	}
	if _, err := os.Stat(secondFolder); err != nil {
		t.Fatal("the running second demo was swept")
	}
	if _, err := os.Stat(thirdFolder); err != nil {
		t.Fatal("the third demo folder is missing")
	}

	second.signal(syscall.SIGTERM)
	if status := second.waitExit(t); status != 0 {
		t.Fatalf("second demo exited with %d", status)
	}
	if _, err := os.Stat(secondFolder); err == nil {
		t.Fatal("a stopped demo's folder survived")
	}
	if _, err := os.Stat(thirdFolder); err != nil {
		t.Fatal("the running third demo was removed")
	}
	third.signal(syscall.SIGTERM)
	if status := third.waitExit(t); status != 0 {
		t.Fatalf("third demo exited with %d", status)
	}
	if _, err := os.Stat(thirdFolder); err == nil {
		t.Fatal("the third demo folder survived its stop")
	}
	if !bytes.Equal(readFile(t, filepath.Join(liveSentinel, "sentinel.txt")), sentinelBefore) {
		t.Fatal("the live folder's sentinel changed")
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("demo mode called an external CLI")
	}
}

// TestCheckLibraryMatchesTheSharedCommandAndDoesNotCreateItsStandardFolder
func TestCheckLibraryMatchesTheSharedCommandAndDoesNotCreateItsStandardFolder(t *testing.T) {
	t.Parallel()
	temporary := filepath.Join(t.TempDir(), "check with spaces")
	standard := filepath.Join(temporary, "not created")
	environment := processEnvironment()
	environment["BELTLINE_DATA_DIR"] = standard
	before, beforeStatus := runDaemonBinary(t, []string{"--check-library"}, environment)
	expected := launch.LibraryCheckCommandCheck(store.Paths{Root: standard})
	if before != expected.Output || beforeStatus != int(expected.Status) {
		t.Fatalf("standard check %q/%d, want %q/%d", before, beforeStatus, expected.Output, expected.Status)
	}
	if _, err := os.Stat(standard); err == nil {
		t.Fatal("the check created its standard folder")
	}

	explicit := filepath.Join(temporary, "explicit clean folder")
	check, checkStatus := runDaemonBinary(t, []string{"--check-library", explicit}, environment)
	explicitResult := launch.LibraryCheckCommandCheck(store.Paths{Root: explicit})
	if check != explicitResult.Output || checkStatus != int(explicitResult.Status) {
		t.Fatalf("explicit check %q/%d", check, checkStatus)
	}
	if _, err := os.Stat(explicit); err == nil {
		t.Fatal("the check created the explicit folder")
	}

	broken := filepath.Join(temporary, "broken folder")
	brokenFile := filepath.Join(broken, "belts", "broken", "v1.json")
	if err := os.MkdirAll(filepath.Dir(brokenFile), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFixtureFile(t, brokenFile, "{}")
	brokenResult := launch.LibraryCheckCommandCheck(store.Paths{Root: broken})
	brokenRun, brokenStatus := runDaemonBinary(t, []string{"--check-library", broken}, environment)
	if brokenRun != brokenResult.Output || brokenStatus != int(brokenResult.Status) {
		t.Fatalf("broken check %q/%d", brokenRun, brokenStatus)
	}
	if brokenStatus != 1 {
		t.Fatalf("broken status %d, want 1", brokenStatus)
	}
}

// TestProcessPrintsExactlyOneTokenFreeReadyLineAfterDiscovery
func TestProcessPrintsExactlyOneTokenFreeReadyLineAfterDiscovery(t *testing.T) {
	t.Parallel()
	root := filepath.Join(t.TempDir(), "ready line data")
	environment := processEnvironment()
	environment["BELTLINE_DATA_DIR"] = root
	environment["BELTLINE_VERSION"] = "ready-line-test"
	process := startDaemonProcess(t, nil, environment)
	t.Cleanup(process.forceStop)
	info := waitForDiscovery(t, root, process)
	process.signal(syscall.SIGINT)
	process.waitExit(t)
	output := process.stdout.String()
	lines := strings.Split(strings.TrimSuffix(output, "\n"), "\n")
	want := fmt.Sprintf("Beltline ready at http://127.0.0.1:%d/", info.Port)
	if len(lines) != 1 || lines[0] != want {
		t.Fatalf("stdout lines %q, want [%q]", lines, want)
	}
	if !strings.HasSuffix(output, "\n") {
		t.Fatal("the ready line lacks its newline")
	}
	if strings.Contains(output, info.Token) || strings.Contains(output, "/auth?token=") {
		t.Fatal("the ready line exposes the token")
	}
	errorText := process.stderr.String()
	if strings.Contains(errorText, info.Token) || strings.Contains(errorText, "/auth?token=") {
		t.Fatal("stderr exposes the token")
	}
}

// runDaemonBinary runs the built beltlined with args and returns its standard output and status.
func runDaemonBinary(t *testing.T, args []string, environment map[string]string) (string, int) {
	t.Helper()
	cmd := exec.Command(daemonBinary, args...)
	cmd.Env = environmentList(environment)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &bytes.Buffer{}
	_ = cmd.Run()
	return stdout.String(), exitStatusOf(cmd)
}

// readFile returns the contents of path.
func readFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// fileMode returns the permission bits of path.
func fileMode(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm()
}

// fileInode returns the inode number of path.
func fileInode(t *testing.T, path string) uint64 {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatal("no inode")
	}
	return uint64(stat.Ino)
}
