//go:build integration

package daemon

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/shell"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/store"
	"github.com/robbertmanders/beltline/daemon/internal/kit/coordinator"
	"github.com/robbertmanders/beltline/daemon/internal/testkit"
)

// tmuxRecoveryEnabled says whether tmux is installed.
// It waits for make test's private socket and stand-in agent; this test makes its own.
func tmuxRecoveryEnabled() bool {
	_, err := exec.LookPath("tmux")
	return err == nil
}

// recoverySocket is a private tmux server for this process, removed when the test ends.
func recoverySocket(t *testing.T) string {
	socket := fmt.Sprintf("beltline-test-%d-recovery", os.Getpid())
	t.Cleanup(func() {
		_ = shell.ProcessRunner{}.Run(context.Background(), "tmux", []string{"-L", socket, "kill-server"}, "", nil)
	})
	return socket
}

// TestDaemonTicksWithoutUIAndRecoversRunningSessionsWithoutDuplicates
func TestDaemonTicksWithoutUIAndRecoversRunningSessionsWithoutDuplicates(t *testing.T) {
	t.Parallel()
	if !tmuxRecoveryEnabled() {
		t.Skip("requires tmux")
	}
	ctx := context.Background()
	tmux := shell.ProcessRunner{}
	root := filepath.Join(t.TempDir(), "tmux recovery data")
	socket := recoverySocket(t)
	paths := store.Paths{Root: root}
	storage := store.NewStorage(paths)
	storage.SeedAgents()
	spec := seededAgent(t, storage, "spec")
	stage := core.DefaultsStageForAgent(spec, nil, nil, nil, core.TriggerKindManual)
	stage.Runner = new(core.BackendPi)
	stage.Model = "fixture-model"
	stage.Workspace = core.WorkspaceReadOnly
	belt, err := storage.SaveNewVersion(core.NewBeltFromTrigger(core.BeltFromTriggerArgs{
		ID: "daemon-recovery", Name: "Daemon recovery", Trigger: core.Trigger{Kind: core.TriggerKindManual},
		Repos: []string{"acme/web"}, Base: "main", Stages: []core.Stage{stage},
	}))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	run := core.NewRun(core.RunArgs{
		Belt:    belt,
		Subject: core.Subject{Kind: core.SubjectKindManual, Repo: "acme/web", Key: "recovery", Title: "Daemon recovery fixture"},
		Now:     now,
	})
	core.EngineStart(&run, now)
	sessionID := strings.ToLower(coordinator.RealIDs{}.UUID())
	session := "bl-daemon-recovery-web-spec-main-1-" + sessionID
	t.Cleanup(func() { stopTmuxSession(socket, session) })
	core.EngineAdvance(&run, core.Event{Kind: core.EventKindSessionStarted, Session: session, SessionID: sessionID, Folder: "01-spec-1"}, now)
	run.Attempts[0].Settings = &core.SessionSettings{Model: "fixture-model", Runner: new(core.BackendPi)}
	if err := os.MkdirAll(paths.AttemptFolder(run, run.Attempts[0]), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := storage.SaveRun(run); err != nil {
		t.Fatal(err)
	}

	endedRun := core.NewRun(core.RunArgs{
		Belt:    belt,
		Subject: core.Subject{Kind: core.SubjectKindManual, Repo: "acme/web", Key: "ended", Title: "Ended session fixture"},
		Now:     now,
	})
	core.EngineStart(&endedRun, now)
	endedSessionID := strings.ToLower(coordinator.RealIDs{}.UUID())
	core.EngineAdvance(&endedRun, core.Event{Kind: core.EventKindSessionStarted, Session: "bl-ended-session-" + endedSessionID, SessionID: endedSessionID, Folder: "01-spec-1"}, now)
	endedRun.Attempts[0].Settings = &core.SessionSettings{Model: "fixture-model", Runner: new(core.BackendPi)}
	if err := storage.SaveRun(endedRun); err != nil {
		t.Fatal(err)
	}
	started := tmux.Run(ctx, "tmux", []string{"-L", socket, "new-session", "-d", "-s", session, "-x", "200", "-y", "50", "-c", root, "/bin/sleep", "120"}, "", nil)
	if !started.OK() {
		t.Fatalf("Could not create fixture session: %s", started.Message())
	}

	environment := processEnvironment()
	environment["BELTLINE_DATA_DIR"] = root
	environment["BELTLINE_VERSION"] = "tmux-recovery-test"
	environment["BELTLINE_TMUX_SOCKET"] = socket
	standIn, err := filepath.Abs(filepath.Join("..", "..", "testdata", "fixtures", "stand-in-agent.sh"))
	if err != nil {
		t.Fatal(err)
	}
	environment["BELTLINE_PI"] = standIn
	first := startDaemonProcess(t, nil, environment)
	t.Cleanup(first.forceStop)
	info := waitForDiscovery(t, root, first)
	// The daemon ticks on its own; wait until the ended session's run is held.
	testkit.Eventually(t, 10*time.Second, "the ended session's run is held", func() bool {
		ended := findStoredRun(storage, endedRun.ID)
		return ended != nil && ended.State == core.RunStateHeld
	})
	initialRun := findStoredRun(storage, run.ID)
	if initialRun == nil || initialRun.State != core.RunStateRunning || len(initialRun.Attempts) != 1 {
		t.Fatalf("initial run after the first tick: %+v", initialRun)
	}
	if ended := findStoredRun(storage, endedRun.ID); len(ended.Attempts) != 1 {
		t.Fatalf("ended run attempts %d, want 1", len(ended.Attempts))
	}
	if !tmux.Run(ctx, "tmux", []string{"-L", socket, "has-session", "-t", session}, "", nil).OK() {
		t.Fatal("the session ended with the first tick")
	}

	first.signal(syscall.SIGTERM)
	if status := first.waitExit(t); status != 0 {
		t.Fatalf("first daemon exited with %d", status)
	}
	if !tmux.Run(ctx, "tmux", []string{"-L", socket, "has-session", "-t", session}, "", nil).OK() {
		t.Fatal("the session ended with the daemon")
	}

	second := startDaemonProcess(t, nil, environment)
	t.Cleanup(second.forceStop)
	waitForDiscovery(t, root, second)
	testkit.Eventually(t, 10*time.Second, "the running session is recovered", func() bool {
		recovered := findStoredRun(storage, run.ID)
		if recovered == nil || recovered.State != core.RunStateRunning || len(recovered.Attempts) != 1 {
			return false
		}
		attempt := recovered.CurrentAttempt()
		return attempt != nil && attempt.Session != nil && *attempt.Session == session
	})
	recovered := findStoredRun(storage, run.ID)
	attempt := recovered.CurrentAttempt()
	if attempt.SessionID == nil || *attempt.SessionID != sessionID {
		t.Fatalf("recovered session id %v, want %s", attempt.SessionID, sessionID)
	}
	if !tmux.Run(ctx, "tmux", []string{"-L", socket, "has-session", "-t", session}, "", nil).OK() {
		t.Fatal("the recovered session is gone")
	}
	listed := tmux.Run(ctx, "tmux", []string{"-L", socket, "list-sessions", "-F", "#S"}, "", nil)
	matching := 0
	for _, name := range strings.Split(listed.Stdout, "\n") {
		if strings.HasPrefix(name, "bl-daemon-recovery-web-") {
			matching++
		}
	}
	if matching != 1 {
		t.Fatalf("%d matching sessions, want 1", matching)
	}

	second.signal(syscall.SIGTERM)
	if status := second.waitExit(t); status != 0 {
		t.Fatalf("second daemon exited with %d", status)
	}
	if !tmux.Run(ctx, "tmux", []string{"-L", socket, "has-session", "-t", session}, "", nil).OK() {
		t.Fatal("the session ended with the second daemon")
	}
	if info.Version != "tmux-recovery-test" {
		t.Fatalf("version %q", info.Version)
	}
}

// seededAgent returns the latest saved version of the agent id, after the built-ins are seeded.
func seededAgent(t *testing.T, storage store.Storage, id string) core.Agent {
	t.Helper()
	versions := storage.AgentVersions(id)
	if len(versions) == 0 {
		t.Fatalf("no seeded agent %s", id)
	}
	agent, ok := storage.Agent(id, versions[len(versions)-1])
	if !ok {
		t.Fatalf("agent %s version %d is missing", id, versions[len(versions)-1])
	}
	return agent
}

// findStoredRun returns the saved run with id, nil when there is none.
func findStoredRun(storage store.Storage, id string) *core.Run {
	for _, run := range storage.LoadRuns() {
		if run.ID == id {
			saved := run
			return &saved
		}
	}
	return nil
}

// stopTmuxSession kills session on socket, ignoring failures (the fixture may be gone already).
func stopTmuxSession(socket, session string) {
	_ = shell.ProcessRunner{}.Run(context.Background(), "tmux", []string{"-L", socket, "kill-session", "-t", session}, "", nil)
}
