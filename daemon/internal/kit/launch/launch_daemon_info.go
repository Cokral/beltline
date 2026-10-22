package launch

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"encoding/json"
	"github.com/robbertmanders/beltline/daemon/internal/kit/coordinator"
	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// DaemonInfoFileName is the discovery record's name in the data folder.
const DaemonInfoFileName = "daemon.json"

// DaemonInfo is the discovery record a running beltlined publishes in its data
// folder: where its loopback listener is, the token that authenticates to it,
// and the build and API it serves. It is written
// with mode 0600.
type DaemonInfo struct {
	Pid        int32  `json:"pid"`
	Port       int    `json:"port"`
	Token      string `json:"token"`
	Version    string `json:"version"`
	APIVersion int    `json:"apiVersion"`
}

// DaemonInfoMakeToken returns a new 64-hex-character token from the system's
// secure random source.
// Lock: none.
func DaemonInfoMakeToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", daemonInfoError{Kind: "randomSource", Status: -1}
	}
	return hex.EncodeToString(bytes), nil
}

// Publish writes the record into folder atomically: a temporary file, fsynced,
// renamed over daemon.json, and the folder fsynced.
// Lock: none; the caller owns the folder's lock.
func (d DaemonInfo) Publish(folder string) error {
	if d.Version == "" {
		return daemonInfoError{Kind: "emptyVersion"}
	}
	destination := filepath.Join(folder, DaemonInfoFileName)
	temporary := filepath.Join(folder, fmt.Sprintf(".%s.%d.%s.tmp", DaemonInfoFileName, d.Pid, coordinator.RealIDs{}.UUID()))
	data, err := jsonx.Marshal(d)
	if err != nil {
		return err
	}
	descriptor, err := syscall.Open(temporary, syscall.O_WRONLY|syscall.O_CREAT|syscall.O_EXCL|syscall.O_CLOEXEC, 0o600)
	if err != nil {
		return daemonInfoError{Kind: "file", Path: temporary, Reason: StrerrorText(err)}
	}
	didClose := false
	defer func() {
		if !didClose {
			_ = syscall.Close(descriptor)
		}
		_ = os.Remove(temporary)
	}()
	if err := syscall.Fchmod(descriptor, 0o600); err != nil {
		return daemonInfoError{Kind: "file", Path: temporary, Reason: StrerrorText(err)}
	}
	if err := daemonInfoWriteAll(data, descriptor, temporary); err != nil {
		return err
	}
	if err := syscall.Fsync(descriptor); err != nil {
		return daemonInfoError{Kind: "file", Path: temporary, Reason: StrerrorText(err)}
	}
	didClose = true
	if err := syscall.Close(descriptor); err != nil {
		return daemonInfoError{Kind: "file", Path: temporary, Reason: StrerrorText(err)}
	}
	if err := os.Rename(temporary, destination); err != nil {
		return daemonInfoError{Kind: "file", Path: destination, Reason: StrerrorText(err)}
	}
	if directory, err := syscall.Open(folder, syscall.O_RDONLY|syscall.O_CLOEXEC, 0); err == nil {
		_ = syscall.Fsync(directory)
		_ = syscall.Close(directory)
	}
	return nil
}

// Remove deletes the record from folder when it still names this pid and token.
// Lock: none.
func (d DaemonInfo) Remove(folder string) {
	destination := filepath.Join(folder, DaemonInfoFileName)
	data, err := os.ReadFile(destination)
	if err != nil {
		return
	}
	var current DaemonInfo
	if err := json.Unmarshal(data, &current); err != nil {
		return
	}
	if current.Pid != d.Pid || current.Token != d.Token {
		return
	}
	_ = os.Remove(destination)
}

// DaemonInfoRemove deletes folder's discovery record whoever wrote it.
// Lock: none.
func DaemonInfoRemove(folder string) {
	_ = os.Remove(filepath.Join(folder, DaemonInfoFileName))
}

// daemonInfoWriteAll writes every byte of data to fd, retrying interrupted writes.
// Lock: none.
func daemonInfoWriteAll(data []byte, fd int, path string) error {
	written := 0
	for written < len(data) {
		count, err := syscall.Write(fd, data[written:])
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		if err != nil || count <= 0 {
			reason := "no bytes written"
			if err != nil {
				reason = StrerrorText(err)
			}
			return daemonInfoError{Kind: "file", Path: path, Reason: reason}
		}
		written += count
	}
	return nil
}

// StrerrorText is the message strerror gives for the errno inside err, as Darwin
// reads it: Darwin's messages start with a
// capital letter, while Go's errno table is lower case. A non-errno error gives its own text.
func StrerrorText(err error) string {
	var errno syscall.Errno
	if errors.As(err, &errno) {
		text := errno.Error()
		if text != "" {
			return strings.ToUpper(text[:1]) + text[1:]
		}
		return text
	}
	return err.Error()
}

// daemonInfoError is a failure to publish or to make the token.
// Kind is "randomSource", "emptyVersion" or "file"; Status is the random source's status; Path and Reason describe a file failure.
type daemonInfoError struct {
	Kind   string
	Status int32
	Path   string
	Reason string
}

// Error describes the failure.
// Lock: none (pure).
func (e daemonInfoError) Error() string {
	switch e.Kind {
	case "randomSource":
		return fmt.Sprintf("Secure random token generation failed (status %d)", e.Status)
	case "emptyVersion":
		return "The daemon version must not be empty"
	default:
		return fmt.Sprintf("Could not publish %s: %s", e.Path, e.Reason)
	}
}
