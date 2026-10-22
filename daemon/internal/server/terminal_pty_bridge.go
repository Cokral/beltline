// A PTY running a command
// (`tmux attach-session`), relayed to the WebSocket. In Go the PTY is the
// creack/pty package (it takes the command as a []string). The
// array helper has no counterpart).
package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/coder/websocket"
	"github.com/creack/pty"
	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/shell"
)

// terminalSocket is the WebSocket side of a relay: what the bridge reads
// (keystrokes and resize messages) and writes (PTY output), and how it closes.
// *websocket.Conn satisfies it.
type terminalSocket interface {
	Read(ctx context.Context) (websocket.MessageType, []byte, error)
	Write(ctx context.Context, typ websocket.MessageType, p []byte) error
	Close(code websocket.StatusCode, reason string) error
}

// errTerminalMessageTooLarge is the read failure for an inbound message over
// terminalMaxMessageSize. The relay ends
// with it as any read failure: the socket closes with 1011.
var errTerminalMessageTooLarge = errors.New("terminal message is larger than 16 MiB")

// terminalLimitedSocket is the relay's view of the WebSocket. Its Read reads one
// whole data message, refusing one over terminalMaxMessageSize with
// errTerminalMessageTooLarge. The connection's own read limit is disabled, since
// the library closes an oversized read with 1009 itself and the relay would then
// have no say in the close code.
type terminalLimitedSocket struct {
	*websocket.Conn
}

// Read returns the next data message, or the error that ended the read.
func (s terminalLimitedSocket) Read(ctx context.Context) (websocket.MessageType, []byte, error) {
	kind, reader, err := s.Reader(ctx)
	if err != nil {
		return 0, nil, err
	}
	data, err := io.ReadAll(io.LimitReader(reader, terminalMaxMessageSize+1))
	if err != nil {
		return 0, nil, err
	}
	if len(data) > terminalMaxMessageSize {
		return 0, nil, errTerminalMessageTooLarge
	}
	return kind, data, nil
}

// PTYBridge is a child process on a pseudo-terminal, and the relay between it
// and the socket. Go keeps the PTY file and
// the command. The command is only the tmux client; cleanup signals and reaps
// that process, never a pane or a tmux session.
type PTYBridge struct {
	ptmx *os.File
	cmd  *exec.Cmd
}

// terminalWindowColumns and terminalWindowRows are the window size a new PTY starts with.
const (
	terminalWindowColumns = 80
	terminalWindowRows    = 24
)

// PTYBridgeLaunch starts command in
// directory on a new PTY, with the window's default size. The command runs as
// `/usr/bin/env <command…>` with the daemon's environment, TERM set to
// xterm-256color. A failure to allocate the PTY and a failure to start the
// process have their own messages.
func PTYBridgeLaunch(command []string, directory string) (*PTYBridge, error) {
	if len(command) == 0 {
		return nil, core.NewFailure("The terminal attach command is empty.")
	}
	ptmx, tty, err := pty.Open()
	if err != nil {
		return nil, core.NewFailure(fmt.Sprintf("Could not allocate a terminal: %s.", err))
	}
	defer func() { _ = tty.Close() }()
	size := &pty.Winsize{Cols: terminalWindowColumns, Rows: terminalWindowRows}
	if err := pty.Setsize(ptmx, size); err != nil {
		_ = ptmx.Close()
		return nil, core.NewFailure(fmt.Sprintf("Could not allocate a terminal: %s.", err))
	}
	cmd := exec.Command("/usr/bin/env", command...)
	cmd.Env = ptyBridgeEnvironment()
	cmd.Dir = directory
	cmd.Stdin, cmd.Stdout, cmd.Stderr = tty, tty, tty
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true}
	if err := cmd.Start(); err != nil {
		_ = ptmx.Close()
		return nil, core.NewFailure(fmt.Sprintf("Could not launch terminal client: %s.", err))
	}
	return &PTYBridge{ptmx: ptmx, cmd: cmd}, nil
}

// ptyBridgeEnvironment is the daemon's environment with TERM set to xterm-256color.
func ptyBridgeEnvironment() []string {
	env := []string{}
	for _, entry := range shell.Environment() {
		if !strings.HasPrefix(entry, "TERM=") {
			env = append(env, entry)
		}
	}
	return append(env, "TERM=xterm-256color")
}

// relayEnd says how a relay ended.
type relayEnd int

const (
	relayNormal relayEnd = iota
	relayPolicyViolation
	relayFailure
)

// Relay copies
// PTY output to the socket and socket input to the PTY (dropped when read-only),
// applies resize messages, and ends when the child exits, the socket closes, or
// the session's mode changes so that it may no longer be attached. mode returns
// the session's current mode for a takeover (nil when it may no longer be
// attached); an error is a failure of the relay.
// The socket is closed with the code that ends the relay, and Relay returns
// once both of its goroutines and the output reader have ended.
// Lock: none (the coordinator is asked through mode, which takes the lock).
func (b *PTYBridge) Relay(ctx context.Context, socket terminalSocket, readOnly, takeOver bool,
	mode func() (*core.TerminalSessionMode, error)) {
	channel := newBoundedPTYChannel()
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		defer channel.Finish()
		buffer := make([]byte, 16*1024)
		for {
			count, err := b.ptmx.Read(buffer)
			if count > 0 {
				channel.Send(bytes.Clone(buffer[:count]))
			}
			if err != nil {
				return
			}
		}
	}()

	ends := make(chan relayEnd, 2)
	go func() {
		ends <- ptyBridgeEnd(ptyBridgeSendOutput(ctx, channel, func(data []byte) error {
			return socket.Write(ctx, websocket.MessageBinary, data)
		}))
	}()
	go func() { ends <- b.receiveInput(ctx, socket, readOnly, takeOver, mode) }()

	first := <-ends
	b.stop()
	channel.Finish()
	code, reason := relayClose(first)
	_ = socket.Close(code, reason)
	// The other goroutine ends with the socket: its read fails once the close
	// handshake completes, and its write fails once the socket is closing.
	<-ends
	<-readerDone
}

// relayClose is the close frame that ends a relay:
// a normal closure, a policy violation, or 1011 with "Terminal relay failed".
func relayClose(end relayEnd) (websocket.StatusCode, string) {
	switch end {
	case relayPolicyViolation:
		return websocket.StatusPolicyViolation, "Terminal policy violation"
	case relayFailure:
		return websocket.StatusInternalError, "Terminal relay failed"
	}
	return websocket.StatusNormalClosure, ""
}

// ptyBridgeEnd is the relay end for a goroutine's error: nil is a normal end.
func ptyBridgeEnd(err error) relayEnd {
	if err != nil {
		return relayFailure
	}
	return relayNormal
}

// ptyBridgeSendOutput forwards
// bounded PTY output chunks to the socket until the channel finishes. It returns
// nil when the channel finishes and the error of send otherwise.
func ptyBridgeSendOutput(ctx context.Context, ch *BoundedPTYChannel, send func([]byte) error) error {
	for {
		data := ch.Next()
		if data == nil {
			return nil
		}
		if err := send(data); err != nil {
			return err
		}
	}
}

// receiveInput reads socket messages, writing
// keystrokes to the PTY (never when read-only) and applying resize messages. A
// keystroke on a takeover is dropped with a policy violation when the session may
// no longer be taken over. A close from the client is a normal end.
func (b *PTYBridge) receiveInput(ctx context.Context, socket terminalSocket, readOnly, takeOver bool,
	mode func() (*core.TerminalSessionMode, error)) relayEnd {
	for {
		kind, data, err := socket.Read(ctx)
		if err != nil {
			if websocket.CloseStatus(err) != -1 {
				return relayNormal
			}
			return relayFailure
		}
		switch kind {
		case websocket.MessageBinary:
			if takeOver {
				current, err := mode()
				if err != nil {
					return relayFailure
				}
				if current == nil || current.ReadOnly {
					return relayPolicyViolation
				}
			}
			if readOnly || len(data) == 0 {
				continue
			}
			if err := b.write(data); err != nil {
				return relayFailure
			}
		case websocket.MessageText:
			resize, ok := TerminalProtocolResize(data)
			if !ok || !b.resizeTerminal(resize.Columns, resize.Rows) {
				return relayPolicyViolation
			}
		}
	}
}

// write writes bytes to the PTY. Only the relay's
// input goroutine writes, so the writes are in order without a queue of their own.
func (b *PTYBridge) write(data []byte) error {
	if _, err := b.ptmx.Write(data); err != nil {
		return core.NewFailure("Could not write terminal input.")
	}
	return nil
}

// resizeTerminal sets the PTY's
// window size, then signals the child, since a fork child can inherit a blocked
// signal mask from the daemon.
func (b *PTYBridge) resizeTerminal(columns, rows uint16) bool {
	if err := pty.Setsize(b.ptmx, &pty.Winsize{Cols: columns, Rows: rows}); err != nil {
		return false
	}
	_ = syscall.Kill(b.cmd.Process.Pid, syscall.SIGWINCH)
	return true
}

// stop ends the child (hang up, then terminate, then
// kill, each after a one-second reap wait) and closes the PTY.
func (b *PTYBridge) stop() {
	pid := b.cmd.Process.Pid
	_ = syscall.Kill(pid, syscall.SIGHUP)
	if !ptyBridgeReap(b.cmd, time.Second) {
		_ = syscall.Kill(pid, syscall.SIGTERM)
		if !ptyBridgeReap(b.cmd, time.Second) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
			_, _ = syscall.Wait4(pid, nil, 0, nil)
		}
	}
	_ = b.ptmx.Close()
}

// ptyBridgeReap waits up to timeout for the
// child to exit, reporting whether it did. It polls with WNOHANG; nothing else
// waits on the child (exec.Cmd.Wait is never called), so the wait reaps it.
func ptyBridgeReap(cmd *exec.Cmd, timeout time.Duration) bool {
	pid := cmd.Process.Pid
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		reaped, err := syscall.Wait4(pid, nil, syscall.WNOHANG, nil)
		if reaped == pid || (reaped < 0 && err == syscall.ECHILD) {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

// BoundedPTYChannel is a one-slot hand-off of
// PTY output between the reader and the sender, so a slow socket back-pressures
// the PTY read instead of growing memory. chunks has no buffer: Send waits until
// Next takes the chunk. done is closed by Finish, which wakes both.
type BoundedPTYChannel struct {
	chunks chan []byte
	done   chan struct{}
	finish sync.Once
}

// newBoundedPTYChannel makes an unfinished channel with no chunk pending.
func newBoundedPTYChannel() *BoundedPTYChannel {
	return &BoundedPTYChannel{chunks: make(chan []byte), done: make(chan struct{})}
}

// Send waits until the previous chunk is
// taken, then holds data for next. After Finish it returns at once, dropping data.
func (c *BoundedPTYChannel) Send(data []byte) {
	select {
	case <-c.done:
		return
	default:
	}
	select {
	case c.chunks <- data:
	case <-c.done:
	}
}

// Next is the next chunk, or nil once finished.
func (c *BoundedPTYChannel) Next() []byte {
	select {
	case <-c.done:
		return nil
	default:
	}
	select {
	case data := <-c.chunks:
		return data
	case <-c.done:
		return nil
	}
}

// Finish ends the stream and wakes any waiter.
// It may be called more than once.
func (c *BoundedPTYChannel) Finish() {
	c.finish.Do(func() { close(c.done) })
}
