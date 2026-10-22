package shell

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Result is a finished command's exit status and output. Stopped is set when the
// command was stopped at its limit or cancelled (TimedOutReason, CancelledReason).
type Result struct {
	Status  int
	Stdout  string
	Stderr  string
	Stopped string
}

// OK reports a zero exit status.
func (r Result) OK() bool { return r.Status == 0 }

// TimedOut reports whether the command was stopped at its limit or a deadline.
func (r Result) TimedOut() bool { return r.Stopped == TimedOutReason }

// IsStopped reports whether the command was stopped, at its limit or by its caller
// cancelling. Its output then answers nothing it was asked.
func (r Result) IsStopped() bool { return r.Stopped != "" }

// MessageFor is Message for a command named operation, such as "gh pr create". A
// stopped command reads "gh pr create: command timed out", so the stop is the tool's own.
func (r Result) MessageFor(operation string) string {
	if r.IsStopped() {
		return operation + ": " + r.Stopped
	}
	return r.Message()
}

// Message is the first line of whatever explains a failure: why the command was
// stopped, else stderr, else stdout, else "exit <status>".
func (r Result) Message() string {
	if r.Stopped != "" {
		return r.Stopped
	}
	text := strings.TrimSpace(r.Stderr)
	if text == "" {
		text = strings.TrimSpace(r.Stdout)
	}
	// Splits at "
	// " but not inside a "\r
	// ", which is one character.
	for _, line := range splitLines(text) {
		if line != "" {
			return line
		}
	}
	return "exit " + strconv.Itoa(r.Status)
}

// Runner runs command-line tools. dir "" runs in the
// current directory; input nil leaves stdin empty. It blocks; callers that must
// not wait run it in a goroutine. Tests replace it with a Recorder.
type Runner interface {
	Run(ctx context.Context, executable string, args []string, dir string, input *string) Result
}

// ProcessRunner runs tools through /usr/bin/env with the login shell's PATH,
// so a daemon started by launchd finds Homebrew and npm tools too.
type ProcessRunner struct{}

var (
	pathOnce  sync.Once
	pathValue string
)

// Path is the PATH every tool runs with: ~/.local/bin first, then the login
// shell's PATH, then the usual system folders. Computed once per process.
func Path() string {
	pathOnce.Do(func() {
		var login []string
		sh := os.Getenv("SHELL")
		if sh == "" {
			sh = defaultShell()
		}
		out, err := exec.Command(sh, "-lc", `printf %s "$PATH"`).Output()
		if err == nil {
			for _, part := range strings.Split(string(out), ":") {
				if part != "" {
					login = append(login, part)
				}
			}
		}
		home, _ := os.UserHomeDir()
		pathValue = PathFrom(login, home)
	})
	return pathValue
}

// PathFrom builds the PATH from a login shell's entries. ~/.local/bin comes
// first: native installers (Claude Code's among them) put the current version
// there, and a login shell, which skips .zshrc, lists Homebrew ahead of it or
// leaves it out, so an older Homebrew copy of the same tool would win.
func PathFrom(login []string, home string) string {
	local := home + "/.local/bin"
	parts := []string{local}
	for _, p := range login {
		if p != local {
			parts = append(parts, p)
		}
	}
	// Homebrew's folders on macOS and on Linux come first and last, so a Mac finds Homebrew's tools
	// ahead of the system's, and Linux finds its packaged tools ahead of Linuxbrew's.
	for _, extra := range []string{"/opt/homebrew/bin", "/usr/local/bin", "/usr/bin", "/bin", "/usr/sbin", "/sbin", "/home/linuxbrew/.linuxbrew/bin"} {
		if !slices.Contains(parts, extra) {
			parts = append(parts, extra)
		}
	}
	return strings.Join(parts, ":")
}

// defaultShell is the login shell to ask for PATH when SHELL is unset: zsh, macOS's default, where
// it is installed, else sh.
func defaultShell() string {
	if _, err := os.Stat("/bin/zsh"); err == nil {
		return "/bin/zsh"
	}
	return "/bin/sh"
}

// LookPath reports whether name is an executable in a folder of Path(), the PATH tools run with,
// rather than this process's own, which a service manager may keep short.
func LookPath(name string) bool {
	for _, folder := range filepath.SplitList(Path()) {
		if folder == "" {
			continue
		}
		info, err := os.Stat(filepath.Join(folder, name))
		if err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return true
		}
	}
	return false
}

// Environment is this process's environment with PATH replaced by Path().
func Environment() []string {
	env := []string{}
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "PATH=") {
			env = append(env, kv)
		}
	}
	return append(env, "PATH="+Path())
}

// waitDelay bounds how long a finished command waits for its output pipes. A
// child that outlived it (one that keeps stdout open after its parent exits)
// no longer holds the call, so this bound
// is the one place where the runner stops waiting for output, and only for such children.
const waitDelay = 2 * time.Second

// Run starts executable through /usr/bin/env. A process that cannot start
// gives status 127 and a "Could not start" message. A command
// bounded by a deadline runs in its own process
// group: when ctx ends, the whole group is killed (a background child that kept
// stdout open dies with its parent), and the call returns without waiting for
// pipes a survivor still holds. A command without a deadline stays in the
// daemon's group, so a tool that asks on
// the terminal (git asking for a password under `make run`) is not stopped by
// SIGTTIN.
func (ProcessRunner) Run(ctx context.Context, executable string, args []string, dir string, input *string) Result {
	cmd := exec.CommandContext(ctx, "/usr/bin/env", append([]string{executable}, args...)...)
	cmd.Env = Environment()
	cmd.Dir = dir
	if _, bounded := ctx.Deadline(); bounded {
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		cmd.Cancel = func() error {
			if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
				return cmd.Process.Kill()
			}
			return nil
		}
		cmd.WaitDelay = waitDelay
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if input != nil {
		cmd.Stdin = strings.NewReader(*input)
	}
	// Run can return exec.ErrWaitDelay after a successful exit; the process state
	// is then set, so the exit status still decides the result.
	err := cmd.Run()
	if cmd.ProcessState == nil {
		return Result{Status: 127, Stderr: "Could not start " + executable + ": " + err.Error()}
	}
	return Result{Status: exitStatus(cmd.ProcessState), Stdout: stdout.String(), Stderr: stderr.String()}
}

// exitStatus is the process's exit code, or for a process ended by a signal
// the signal number (9 for SIGKILL).
func exitStatus(state *os.ProcessState) int {
	if ws, ok := state.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		return int(ws.Signal())
	}
	return state.ExitCode()
}

// splitLines splits text at each "
// " that does not follow a "\r",
// which is one character.
func splitLines(text string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(text); i++ {
		if text[i] == '\n' && (i == 0 || text[i-1] != '\r') {
			lines = append(lines, text[start:i])
			start = i + 1
		}
	}
	return append(lines, text[start:])
}
