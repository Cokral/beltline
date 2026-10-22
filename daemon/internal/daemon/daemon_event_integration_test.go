//go:build integration

package daemon

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/runner"
	"github.com/robbertmanders/beltline/daemon/internal/kit/coordinator"
	"github.com/robbertmanders/beltline/daemon/internal/kit/demo"
	"github.com/robbertmanders/beltline/daemon/internal/server"
	"github.com/robbertmanders/beltline/daemon/internal/server/routes"
	"github.com/robbertmanders/beltline/daemon/internal/testkit"
)

// eventFrameWait bounds each wait for a frame.
const eventFrameWait = 4 * time.Second

// socketFrames reads a WebSocket's text frames in order, from a goroutine, so a
// test can wait for the next one with a deadline.
type socketFrames struct {
	conn   *websocket.Conn
	frames chan string
	cancel context.CancelFunc
}

// newSocketFrames starts reading conn.
func newSocketFrames(conn *websocket.Conn) *socketFrames {
	ctx, cancel := context.WithCancel(context.Background())
	frames := &socketFrames{conn: conn, frames: make(chan string, 256), cancel: cancel}
	go func() {
		for {
			_, data, err := conn.Read(ctx)
			if err != nil {
				return
			}
			frames.frames <- string(data)
		}
	}()
	return frames
}

// next returns the next frame, or fails the test when none arrives within wait.
func (s *socketFrames) next(t *testing.T, wait time.Duration) string {
	t.Helper()
	select {
	case frame := <-s.frames:
		return frame
	case <-time.After(wait):
		t.Fatal("no frame within the wait")
		return ""
	}
}

// close stops reading and closes the socket.
func (s *socketFrames) close() {
	s.cancel()
	_ = s.conn.CloseNow()
}

// dialEvents opens the events socket of the server at port with token.
func dialEvents(t *testing.T, port int, token string) *socketFrames {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+token)
	headers.Set("Origin", fmt.Sprintf("http://127.0.0.1:%d", port))
	conn, _, err := websocket.Dial(ctx, fmt.Sprintf("ws://127.0.0.1:%d/api/v1/events", port), &websocket.DialOptions{HTTPHeader: headers})
	if err != nil {
		t.Fatalf("dial events: %v", err)
	}
	conn.SetReadLimit(16 << 20)
	return newSocketFrames(conn)
}

// notificationLines returns the log's notification lines.
func notificationLines(t *testing.T, root string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, DaemonLogFileName))
	if err != nil {
		return nil
	}
	var lines []string
	for _, line := range strings.Split(string(data), "\n") {
		if strings.Contains(line, "Notification: ") {
			lines = append(lines, line)
		}
	}
	return lines
}

// TestRelayFansOutAndLogsOnceWithEscapedNewlinesEvenWithoutClients
func TestRelayFansOutAndLogsOnceWithEscapedNewlinesEvenWithoutClients(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	token := "daemon-event-test-token"
	listener := server.NewBeltlineServer(token, "test", nil, 0, "", routes.Dependencies{})
	port, err := listener.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	relay := newNotificationRelay()
	log, err := OpenDaemonLog(root)
	if err != nil {
		t.Fatal(err)
	}
	relay.SetLog(log)
	var observed []string
	relay.SetObserver(func(title, body string) { observed = append(observed, title+"|"+body) })
	relay.Connect(listener)

	first := dialEvents(t, port, token)
	second := dialEvents(t, port, token)
	t.Cleanup(func() {
		first.close()
		second.close()
	})
	const hello = `{"apiVersion":1,"type":"hello"}`
	if got := first.next(t, eventFrameWait); got != hello {
		t.Fatalf("first hello %q", got)
	}
	if got := second.next(t, eventFrameWait); got != hello {
		t.Fatalf("second hello %q", got)
	}

	relay.Publish("Build ⚙️", "First line\nsecond line\r\n✓")
	relay.Publish("Second notice", "same turn")
	const expected = `{"body":"First line\nsecond line\r\n✓","title":"Build ⚙️","type":"notification"}`
	if got := first.next(t, eventFrameWait); got != expected {
		t.Fatalf("first notification %q", got)
	}
	if got := second.next(t, eventFrameWait); got != expected {
		t.Fatalf("second notification %q", got)
	}
	const secondExpected = `{"body":"same turn","title":"Second notice","type":"notification"}`
	if got := first.next(t, eventFrameWait); got != secondExpected {
		t.Fatalf("first second notification %q", got)
	}
	if got := second.next(t, eventFrameWait); got != secondExpected {
		t.Fatalf("second second notification %q", got)
	}
	if len(observed) != 2 {
		t.Fatalf("observed %d, want 2", len(observed))
	}

	first.close()
	second.close()
	relay.Publish("No clients", "Still logged")

	lines := notificationLines(t, root)
	if len(lines) != 3 {
		t.Fatalf("%d notification lines, want 3: %q", len(lines), lines)
	}
	if !strings.Contains(lines[0], `Notification: Build ⚙️ — First line\nsecond line\r\n✓`) {
		t.Fatalf("line 0 %q", lines[0])
	}
	if !strings.Contains(lines[1], "Notification: Second notice — same turn") {
		t.Fatalf("line 1 %q", lines[1])
	}
	if !strings.Contains(lines[2], "Notification: No clients — Still logged") {
		t.Fatalf("line 2 %q", lines[2])
	}
	if len(observed) != 3 {
		t.Fatalf("observed %d, want 3", len(observed))
	}

	relay.Disconnect()
	listener.Stop(context.Background())
	log.Close()
}

// observedNotices records the notifications a relay's observer sees.
type observedNotices struct {
	mu     sync.Mutex
	values []string
}

// add records one notification (the relay's observer).
func (o *observedNotices) add(title, body string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.values = append(o.values, title+"|"+body)
}

// count is the number of notifications recorded so far.
func (o *observedNotices) count() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return len(o.values)
}

// isNotification reports whether frame is a notification message that names text.
func isNotification(frame, text string) bool {
	return strings.Contains(frame, `"type":"notification"`) && strings.Contains(frame, text)
}

// receiveNotification reads frames until a notification that names text arrives,
// skipping the changed messages before it.
func receiveNotification(t *testing.T, frames *socketFrames, text string) string {
	t.Helper()
	deadline := time.Now().Add(eventFrameWait)
	for time.Now().Before(deadline) {
		frame := frames.next(t, time.Until(deadline))
		if isNotification(frame, text) {
			return frame
		}
	}
	t.Fatalf("no notification naming %q", text)
	return ""
}

// requireChangedFrame reads frames until the changed message for resource arrives.
// A notification before it fails: a notification raised in a turn is queued before
// that turn's changed messages, so none may come first.
func requireChangedFrame(t *testing.T, frames *socketFrames, resource string) {
	t.Helper()
	deadline := time.Now().Add(eventFrameWait)
	for time.Now().Before(deadline) {
		frame := frames.next(t, time.Until(deadline))
		if strings.Contains(frame, `"type":"notification"`) {
			t.Fatalf("unexpected notification before the %s change: %s", resource, frame)
		}
		if strings.HasPrefix(frame, fmt.Sprintf(`{"resource":%q,`, resource)) {
			return
		}
	}
	t.Fatalf("no changed message for %s", resource)
}

// waitForEventClients waits until the listener has count event clients.
func waitForEventClients(t *testing.T, s *server.BeltlineServer, count int) {
	t.Helper()
	testkit.Eventually(t, 5*time.Second, fmt.Sprintf("the listener did not reach %d event clients", count), func() bool {
		return s.EventClientCount() == count
	})
}

// waitForNotificationLines waits until the log holds count notification lines.
func waitForNotificationLines(t *testing.T, root string, count int) {
	t.Helper()
	testkit.Eventually(t, 5*time.Second, fmt.Sprintf("the log did not reach %d notification lines", count), func() bool {
		return len(notificationLines(t, root)) == count
	})
}

// findRun returns the first run of c that matches, in the order c lists them.
func findRun(c *coordinator.Coordinator, match func(run core.Run) bool) (core.Run, bool) {
	for _, run := range c.ReadRuns() {
		if match(run) {
			return run, true
		}
	}
	return core.Run{}, false
}

// onPullRequestWatch reports whether run's current stage watches its pull request.
func onPullRequestWatch(run core.Run) bool {
	stage := run.Stage()
	if stage == nil || stage.WatchTarget() == nil {
		return false
	}
	return *stage.WatchTarget() == core.WatchTargetPullRequest
}

// awaitingApproval reports whether run waits at an approval (gate) stage.
func awaitingApproval(run core.Run) bool {
	stage := run.Stage()
	return run.State == core.RunStateWaiting && stage != nil && stage.Kind() == core.StageKindGate
}

// TestCoordinatorEngineAndAfkNotificationsRespectPreferencesAndSurviveClientLoss
// The engine transition and the AFK decisions reach the notifier synchronously, the
// notifier only sends when notifications are on, and clients that drop away do not
// stop the log or the other clients.
func TestCoordinatorEngineAndAfkNotificationsRespectPreferencesAndSurviveClientLoss(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	setup, err := demo.Make(root, time.Now(), os.Getenv)
	if err != nil {
		t.Fatalf("demo: %v", err)
	}
	if err := setup.Storage.SaveAfkDecisions(nil); err != nil {
		t.Fatalf("clear AFK decisions: %v", err)
	}
	relay := newNotificationRelay()
	log, err := OpenDaemonLog(root)
	if err != nil {
		t.Fatalf("open log: %v", err)
	}
	t.Cleanup(log.Close)
	relay.SetLog(log)
	observed := &observedNotices{}
	relay.SetObserver(observed.add)

	// The AFK sessions leave their decision, so each one ends as an AFK decision.
	afkRunner := runner.NewSimulatedRunner(func(runner.SessionSpec) float64 { return 0 }, time.Now,
		func(spec runner.SessionSpec) *string {
			if spec.AgentID != nil && *spec.AgentID == "afk" {
				leave := "leave"
				return &leave
			}
			return nil
		})
	c, err := coordinator.New(coordinator.Services{
		Storage:        setup.Storage,
		GitHub:         setup.Services.GitHub(),
		Jira:           setup.Services.Jira(),
		Runner:         afkRunner,
		Workspaces:     setup.Workspaces,
		LibraryURL:     setup.LibraryURL,
		ModelDiscovery: runner.FixtureModelCatalog{},
		Notifier:       func(title, body string) { relay.Publish(title, body) },
	})
	if err != nil {
		t.Fatalf("coordinator: %v", err)
	}
	t.Cleanup(c.Close)
	c.WaitForStartupProbeForContract()
	for _, run := range c.ReadRuns() {
		c.UpdateRunForContract(run.ID, func(r *core.Run) { r.SetAfk(core.AfkModeNever) })
	}
	on := true
	if _, err := c.ApplySettingsPatch(coordinator.SettingsPatch{Notifications: &on}); err != nil {
		t.Fatalf("notifications on: %v", err)
	}
	c.SetGlobalAfk(context.Background(), true)

	const token = "generated-daemon-event-token"
	listener := server.NewBeltlineServer(token, "test", c, 0, "", routes.Dependencies{})
	port, err := listener.Start(context.Background())
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer listener.Stop(context.Background())
	relay.Connect(listener)
	defer relay.Disconnect()

	first := dialEvents(t, port, token)
	healthy := dialEvents(t, port, token)
	defer first.close()
	defer healthy.close()
	const hello = `{"apiVersion":1,"type":"hello"}`
	if got := first.next(t, eventFrameWait); got != hello {
		t.Fatalf("first hello %q", got)
	}
	if got := healthy.next(t, eventFrameWait); got != hello {
		t.Fatalf("healthy hello %q", got)
	}

	engineRun, ok := findRun(c, func(run core.Run) bool { return run.State == core.RunStateRunning && onPullRequestWatch(run) })
	if !ok {
		t.Fatal("demo fixture has no active pull request watch for an engine notification")
	}
	if !c.ApplyEventForContract(engineRun.ID, core.Event{Kind: core.EventKindBlocked, Reason: "engine event notification"}) {
		t.Fatal("the engine event was not applied")
	}
	if observed.count() != 1 {
		t.Fatalf("observed %d notifications after the engine event, want 1", observed.count())
	}
	firstEngine := receiveNotification(t, first, engineRun.Subject.Ref())
	secondEngine := receiveNotification(t, healthy, engineRun.Subject.Ref())
	if firstEngine != secondEngine {
		t.Fatalf("clients got different notifications: %q and %q", firstEngine, secondEngine)
	}
	if observed.count() != 1 || len(notificationLines(t, root)) != 1 {
		t.Fatalf("observed %d, log lines %d; want 1 and 1", observed.count(), len(notificationLines(t, root)))
	}

	first.close()
	waitForEventClients(t, listener, 1)

	afkRun, ok := findRun(c, awaitingApproval)
	if !ok {
		t.Fatal("demo fixture has no run waiting at an approval")
	}
	for _, item := range c.ReadRuns() {
		if awaitingApproval(item) && item.ID != afkRun.ID {
			c.UpdateRunForContract(item.ID, func(r *core.Run) { r.SetAfk(core.AfkModeNever) })
		}
	}
	c.UpdateRunForContract(afkRun.ID, func(r *core.Run) { r.SetAfk(core.AfkModeAlways) })
	c.AutopilotCheckForContract()
	c.AutopilotCheckForContract()
	afkNotification := receiveNotification(t, healthy, afkRun.Subject.Ref())
	if !strings.Contains(afkNotification, `"type":"notification"`) {
		t.Fatalf("AFK notification %q", afkNotification)
	}
	if observed.count() != 2 || len(notificationLines(t, root)) != 2 {
		t.Fatalf("observed %d, log lines %d after the AFK decision; want 2 and 2", observed.count(), len(notificationLines(t, root)))
	}

	// With notifications off, the AFK decision still changes the AFK resource, but
	// nothing is notified or logged.
	off := false
	if _, err := c.ApplySettingsPatch(coordinator.SettingsPatch{Notifications: &off}); err != nil {
		t.Fatalf("notifications off: %v", err)
	}
	disabledAfkRun, ok := findRun(c, func(run core.Run) bool { return awaitingApproval(run) && run.ID != afkRun.ID })
	if !ok {
		t.Fatal("demo fixture has no second approval run for disabled AFK notification")
	}
	for _, item := range c.ReadRuns() {
		if awaitingApproval(item) && item.ID != disabledAfkRun.ID {
			c.UpdateRunForContract(item.ID, func(r *core.Run) { r.SetAfk(core.AfkModeNever) })
		}
	}
	c.UpdateRunForContract(disabledAfkRun.ID, func(r *core.Run) { r.SetAfk(core.AfkModeAlways) })
	c.AutopilotCheckForContract()
	c.AutopilotCheckForContract()
	requireChangedFrame(t, healthy, "afk")
	if observed.count() != 2 || len(notificationLines(t, root)) != 2 {
		t.Fatalf("observed %d, log lines %d with notifications off; want 2 and 2", observed.count(), len(notificationLines(t, root)))
	}

	// Without clients the engine notification is still logged and observed.
	on = true
	if _, err := c.ApplySettingsPatch(coordinator.SettingsPatch{Notifications: &on}); err != nil {
		t.Fatalf("notifications on again: %v", err)
	}
	healthy.close()
	waitForEventClients(t, listener, 0)
	noClientEngineRun, ok := findRun(c, func(run core.Run) bool { return run.State == core.RunStateRunning && onPullRequestWatch(run) })
	if !ok {
		t.Fatal("demo fixture has no second watch for a no-client notification")
	}
	if !c.ApplyEventForContract(noClientEngineRun.ID, core.Event{Kind: core.EventKindBlocked, Reason: "logged without clients"}) {
		t.Fatal("the no-client engine event was not applied")
	}
	waitForNotificationLines(t, root, 3)
	if observed.count() != 3 {
		t.Fatalf("observed %d notifications, want 3", observed.count())
	}
}
