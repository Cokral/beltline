// The stdio transport of the server model catalog: the JSON-RPC and framed channel to one catalog
// process, its errors and its timeout.

package runner

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/shell"
	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// serverCatalogRPCError is a JSON-RPC error answer, by its code.
type serverCatalogRPCError struct {
	Code int
}

// Error explains the refusal and asks for an upgrade or a sign-in check.
func (e serverCatalogRPCError) Error() string {
	return fmt.Sprintf("Catalog interface refused request (code %d); update the CLI or verify sign-in.", e.Code)
}

// ErrorDescription is the message the user sees, which
// the error middleware answers as a 422.
func (e serverCatalogRPCError) ErrorDescription() (string, bool) {
	return e.Error(), true
}

// catalogChannel is the stdio channel to one catalog process: its JSON or framed
// messages, with a 28-second kill timer. Its folder is removed on Close.
type catalogChannel struct {
	process   *exec.Cmd
	input     io.WriteCloser
	output    io.ReadCloser
	framed    bool
	directory string
	timeout   *time.Timer
	reader    *bufio.Reader
}

// catalogChannelTimeout is when a catalog process is killed if it has not finished.
const catalogChannelTimeout = 28 * time.Second

// catalogChannelLimit bounds one catalog message, in bytes.
const catalogChannelLimit = 2_000_000

// newCatalogChannel starts executable with arguments in a fresh folder. framed
// selects Content-Length framing (Copilot). Fails when the folder or process cannot start.
func newCatalogChannel(executable string, arguments []string, framed bool) (*catalogChannel, error) {
	directory := filepath.Join(honoredTemporaryDirectory(), "beltline-catalog-"+catalogFolderID())
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return nil, err
	}
	cmd := exec.Command("/usr/bin/env", append([]string{executable}, arguments...)...)
	cmd.Env = shell.Environment()
	cmd.Dir = directory
	input, err := cmd.StdinPipe()
	if err != nil {
		_ = os.RemoveAll(directory)
		return nil, err
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		_ = os.RemoveAll(directory)
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		_ = os.RemoveAll(directory)
		return nil, core.NewFailure("Executable unavailable")
	}
	channel := &catalogChannel{
		process: cmd, input: input, output: output, framed: framed, directory: directory,
		reader: bufio.NewReader(output),
	}
	channel.timeout = time.AfterFunc(catalogChannelTimeout, func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	})
	return channel, nil
}

// Close stops the process, closes the pipes and removes the folder.
func (c *catalogChannel) Close() {
	c.timeout.Stop()
	if c.process.Process != nil {
		_ = c.process.Process.Kill()
	}
	_ = c.input.Close()
	_ = c.process.Wait()
	_ = os.RemoveAll(c.directory)
}

// Send writes one message: a line of JSON, or a Content-Length frame.
func (c *catalogChannel) Send(object map[string]any) error {
	data, err := jsonx.Marshal(object)
	if err != nil {
		return err
	}
	if c.framed {
		if _, err := io.WriteString(c.input, fmt.Sprintf("Content-Length: %d\r\n\r\n", len(data))); err != nil {
			return core.NewFailure("Catalog process ended or timed out")
		}
		if _, err := c.input.Write(data); err != nil {
			return core.NewFailure("Catalog process ended or timed out")
		}
		return nil
	}
	if _, err := c.input.Write(append(data, '\n')); err != nil {
		return core.NewFailure("Catalog process ended or timed out")
	}
	return nil
}

// Line reads one newline-terminated line, failing at end of output or past 2 MB.
func (c *catalogChannel) Line() (string, error) {
	var data []byte
	for {
		b, err := c.reader.ReadByte()
		if err != nil {
			return "", core.NewFailure("Catalog process ended or timed out")
		}
		if b == '\n' {
			return strings.Trim(string(data), "\r\n"), nil
		}
		data = append(data, b)
		if len(data) >= catalogChannelLimit {
			return "", core.NewFailure("Catalog response too large")
		}
	}
}

// Receive reads one message as a JSON object.
func (c *catalogChannel) Receive() (map[string]any, error) {
	var data []byte
	if c.framed {
		header, err := c.Line()
		if err != nil {
			return nil, err
		}
		size := 0
		if strings.HasPrefix(header, "Content-Length:") {
			size, err = strconv.Atoi(strings.TrimSpace(header[len("Content-Length:"):]))
		}
		if err != nil || size <= 0 || size >= catalogChannelLimit {
			return nil, core.NewFailure("Unsupported catalog framing")
		}
		separator, err := c.Line()
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(separator) != "" {
			return nil, core.NewFailure("Malformed catalog framing")
		}
		data = make([]byte, size)
		if _, err := io.ReadFull(c.reader, data); err != nil {
			return nil, core.NewFailure("Incomplete catalog response")
		}
	} else {
		line, err := c.Line()
		if err != nil {
			return nil, err
		}
		data = []byte(line)
	}
	response := jsonObject(string(data))
	if response == nil {
		return nil, core.NewFailure("Malformed catalog response")
	}
	return response, nil
}

// RPC sends a request and returns the result that answers its id. Other messages
// are skipped. An error answer becomes a serverCatalogRPCError.
func (c *catalogChannel) RPC(id int, method string, params map[string]any) (map[string]any, error) {
	if err := c.Send(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}); err != nil {
		return nil, err
	}
	for {
		response, err := c.Receive()
		if err != nil {
			return nil, err
		}
		if number, ok := response["id"].(float64); !ok || int(number) != id || float64(int(number)) != number {
			continue
		}
		if errObject, ok := asObject(response["error"]); ok {
			code, ok := errObject["code"].(float64)
			if !ok {
				code = -1
			}
			return nil, serverCatalogRPCError{Code: int(code)}
		}
		result, isObject := asObject(response["result"])
		if _, hasError := response["error"]; hasError || !isObject {
			return nil, core.NewFailure("Catalog interface refused request; update the CLI or verify sign-in")
		}
		return result, nil
	}
}
