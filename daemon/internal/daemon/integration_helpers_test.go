//go:build integration

package daemon

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"encoding/json"
	"github.com/robbertmanders/beltline/daemon/internal/kit/coordinator"
	"github.com/robbertmanders/beltline/daemon/internal/kit/demo"
	"github.com/robbertmanders/beltline/daemon/internal/kit/launch"
	"github.com/robbertmanders/beltline/daemon/internal/testkit"
)

// daemonBinary is the beltlined binary the process tests run, built once by TestMain.
var daemonBinary string

// TestMain builds cmd/beltlined once for the process tests. Discovery waits allow 30 s:
// a demo seeds before it publishes, and make test runs every package at once.
func TestMain(m *testing.M) {
	folder, err := os.MkdirTemp("", "beltlined-test-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	daemonBinary = filepath.Join(folder, "beltlined")
	build := exec.Command("go", "build", "-o", daemonBinary, "./cmd/beltlined")
	build.Dir = filepath.Join("..", "..")
	if output, err := build.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "build beltlined: %v\n%s", err, output)
		_ = os.RemoveAll(folder)
		os.Exit(1)
	}
	code := m.Run()
	_ = os.RemoveAll(folder)
	os.Exit(code)
}

// lockedBuffer is a buffer a process writes to while the test reads it.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

// Write appends p.
func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

// String returns everything written so far.
func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// daemonProcess is a beltlined process the test started.
type daemonProcess struct {
	cmd    *exec.Cmd
	stdout *lockedBuffer
	stderr *lockedBuffer
	done   chan struct{}
	status int
}

// startDaemonProcess starts beltlined with args and environment, capturing its output.
func startDaemonProcess(t *testing.T, args []string, environment map[string]string) *daemonProcess {
	t.Helper()
	cmd := exec.Command(daemonBinary, args...)
	cmd.Env = environmentList(environment)
	p := &daemonProcess{cmd: cmd, stdout: &lockedBuffer{}, stderr: &lockedBuffer{}, done: make(chan struct{})}
	cmd.Stdout = p.stdout
	cmd.Stderr = p.stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start beltlined: %v", err)
	}
	go func() {
		defer close(p.done)
		_ = cmd.Wait()
		p.status = exitStatusOf(cmd)
	}()
	return p
}

// exitStatusOf is the exit code, or the signal number for a process a signal ended.
func exitStatusOf(cmd *exec.Cmd) int {
	if cmd.ProcessState == nil {
		return -1
	}
	if status, ok := cmd.ProcessState.Sys().(syscall.WaitStatus); ok && status.Signaled() {
		return int(status.Signal())
	}
	return cmd.ProcessState.ExitCode()
}

// pid is the process id.
func (p *daemonProcess) pid() int32 { return int32(p.cmd.Process.Pid) }

// running reports whether the process has not exited yet.
func (p *daemonProcess) running() bool {
	select {
	case <-p.done:
		return false
	default:
		return true
	}
}

// waitExit waits for the process to exit and returns its status.
func (p *daemonProcess) waitExit(t *testing.T) int {
	t.Helper()
	select {
	case <-p.done:
	case <-time.After(20 * time.Second):
		t.Fatal("beltlined did not exit")
	}
	return p.status
}

// signal sends sig to the process.
func (p *daemonProcess) signal(sig syscall.Signal) {
	_ = syscall.Kill(int(p.pid()), sig)
}

// forceStop kills the process if it still runs; registered as a cleanup.
func (p *daemonProcess) forceStop() {
	if p.running() {
		_ = syscall.Kill(int(p.pid()), syscall.SIGKILL)
		<-p.done
	}
}

// environmentList turns a map into KEY=VALUE pairs.
func environmentList(environment map[string]string) []string {
	var pairs []string
	for key, value := range environment {
		pairs = append(pairs, key+"="+value)
	}
	return pairs
}

// processEnvironment is the test process's environment with a private tmux socket and a fixed PATH.
func processEnvironment() map[string]string {
	environment := map[string]string{}
	for _, pair := range os.Environ() {
		key, value, _ := strings.Cut(pair, "=")
		environment[key] = value
	}
	environment["BELTLINE_TMUX_SOCKET"] = "beltline-daemon-test-" + strings.ToLower(coordinator.RealIDs{}.UUID()[:8])
	environment["PATH"] = "/usr/bin:/bin"
	return environment
}

// waitForDiscovery waits until folder's discovery record names process p, and returns it.
func waitForDiscovery(t *testing.T, folder string, p *daemonProcess) launch.DaemonInfo {
	t.Helper()
	file := filepath.Join(folder, launch.DaemonInfoFileName)
	var info launch.DaemonInfo
	var found bool
	testkit.Eventually(t, 30*time.Second, "daemon discovery", func() bool {
		if !p.running() {
			t.Fatalf("beltlined exited with status %d before publishing discovery", p.status)
		}
		data, err := os.ReadFile(file)
		if err != nil {
			return false
		}
		var decoded launch.DaemonInfo
		if err := json.Unmarshal(data, &decoded); err != nil || decoded.Pid != p.pid() {
			return false
		}
		info = decoded
		found = true
		return true
	})
	if !found {
		t.Fatal("no discovery record")
	}
	return info
}

// waitForDemoFolder waits until a demo folder owned by p holds a discovery record, and returns it.
func waitForDemoFolder(t *testing.T, parent string, p *daemonProcess, excluding ...string) string {
	t.Helper()
	var folder string
	testkit.Eventually(t, 30*time.Second, "demo discovery", func() bool {
		if !p.running() {
			t.Fatalf("beltlined exited with status %d before its demo was ready", p.status)
		}
		entries, err := os.ReadDir(parent)
		if err != nil {
			return false
		}
		for _, entry := range entries {
			name := entry.Name()
			if !strings.HasPrefix(name, demo.DemoRootPrefix) || slices.Contains(excluding, filepath.Join(parent, name)) {
				continue
			}
			if owner, ok := demo.DemoRootOwner(name); !ok || int32(owner) != p.pid() {
				continue
			}
			if _, err := os.Stat(filepath.Join(parent, name, launch.DaemonInfoFileName)); err == nil {
				folder = filepath.Join(parent, name)
				return true
			}
		}
		return false
	})
	return folder
}

// httpReply is a response from the daemon's API.
type httpReply struct {
	status       int
	body         []byte
	contentType  string
	cacheControl string
}

// apiRequest calls the daemon at port with token (none when empty) and optional origin.
func apiRequest(t *testing.T, port int, token, path, origin string) httpReply {
	t.Helper()
	if path == "" {
		path = "/api/v1/version"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d%s", port, path), nil)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	if origin != "" {
		request.Header.Set("Origin", origin)
	}
	response, err := (&http.Client{}).Do(request)
	if err != nil {
		t.Fatalf("request %s: %v", path, err)
	}
	defer func() { _ = response.Body.Close() }() // the response is read; a close error has nothing left to report
	body, _ := io.ReadAll(response.Body)
	return httpReply{
		status:       response.StatusCode,
		body:         body,
		contentType:  response.Header.Get("Content-Type"),
		cacheControl: response.Header.Get("Cache-Control"),
	}
}

// tryAPIRequest is apiRequest that reports a transport error instead of failing the test.
func tryAPIRequest(port int, token string) (httpReply, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/api/v1/version", port), nil)
	if err != nil {
		return httpReply{}, err
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := (&http.Client{}).Do(request)
	if err != nil {
		return httpReply{}, err
	}
	defer func() { _ = response.Body.Close() }() // the response is read; a close error has nothing left to report
	body, _ := io.ReadAll(response.Body)
	return httpReply{status: response.StatusCode, body: body}, nil
}

// folderTree maps every entry under root to its mode, modification time and content.
func folderTree(t *testing.T, root string) map[string]string {
	t.Helper()
	tree := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, _ := filepath.Rel(root, path)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if entry.IsDir() {
			tree[relative] = fmt.Sprintf("dir|%o|%d", info.Mode().Perm(), info.ModTime().UnixNano())
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		tree[relative] = fmt.Sprintf("%o|%d|%s", info.Mode().Perm(), info.ModTime().UnixNano(), data)
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return tree
}

// acquireAndRelease takes folder's lock and releases it again, failing when another launch holds it.
func acquireAndRelease(t *testing.T, folder string) {
	t.Helper()
	// A child another parallel test forks holds a copy of every descriptor until it
	// execs (close-on-exec), and so the lock, for that moment; retry briefly.
	deadline := time.Now().Add(2 * time.Second)
	for {
		lock, err := launch.OwnershipLockAcquire(folder, int32(os.Getpid()))
		if err == nil {
			lock.Release()
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("folder lock not released: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
