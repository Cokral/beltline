// The terminal
// socket's query, its first (mode) frame, and the resize message.
package server

import (
	"errors"
	"fmt"
	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
	"strconv"
	"strings"

	"encoding/json"
	"github.com/robbertmanders/beltline/daemon/internal/server/routes"
)

// TerminalResize is a window size: columns and rows, both 1 to 65535.
type TerminalResize struct {
	Columns uint16
	Rows    uint16
}

// TerminalQuery is the parsed `?session=…[&takeOver=1]` of the terminal socket.
type TerminalQuery struct {
	Session  string
	TakeOver bool
}

// errTerminalQueryInvalid is the invalid query error.
var errTerminalQueryInvalid = errors.New("invalid terminal query")

// TerminalProtocolQuery exactly one non-empty
// session, at most one takeOver, which must be `1` when present. Any other shape
// is an error.
func TerminalProtocolQuery(queryString string) (TerminalQuery, error) {
	sessions := routes.PercentDecodedQueryValues("session", queryString)
	takeOvers := routes.PercentDecodedQueryValues("takeOver", queryString)
	if len(sessions) != 1 || sessions[0] == "" {
		return TerminalQuery{}, errTerminalQueryInvalid
	}
	if len(takeOvers) > 1 || (len(takeOvers) == 1 && takeOvers[0] != "1") {
		return TerminalQuery{}, errTerminalQueryInvalid
	}
	return TerminalQuery{Session: sessions[0], TakeOver: len(takeOvers) > 0}, nil
}

// TerminalProtocolModeFrame the
// first text frame of every attachment, `{"type":"mode","readOnly":…}`, which
// carries the write permission the daemon accepted.
func TerminalProtocolModeFrame(readOnly bool) string {
	return fmt.Sprintf(`{"type":"mode","readOnly":%t}`, readOnly)
}

// TerminalProtocolResize is a resize message
// `{"type":"resize","cols":…,"rows":…}` with both sizes whole numbers in 1…65535,
// or false for anything else.
func TerminalProtocolResize(data []byte) (TerminalResize, bool) {
	members, err := jsonx.Members(data)
	if err != nil {
		return TerminalResize{}, false
	}
	var kind string
	if raw, ok := members["type"]; !ok || json.Unmarshal(raw, &kind) != nil || kind != "resize" {
		return TerminalResize{}, false
	}
	columns, ok := terminalProtocolInteger(members["cols"])
	if !ok {
		return TerminalResize{}, false
	}
	rows, ok := terminalProtocolInteger(members["rows"])
	if !ok {
		return TerminalResize{}, false
	}
	if columns < 1 || columns > 65535 || rows < 1 || rows > 65535 {
		return TerminalResize{}, false
	}
	return TerminalResize{Columns: uint16(columns), Rows: uint16(rows)}, true
}

// terminalProtocolInteger is a whole JSON number
// (not a boolean, not a float). It takes the member's raw JSON, because the
// the number's text decides what counts as a float: `120.0` and `1e2` are
// floats there, though their values are whole.
func terminalProtocolInteger(raw []byte) (int, bool) {
	text := strings.TrimSpace(string(raw))
	if text == "" || (text[0] != '-' && (text[0] < '0' || text[0] > '9')) {
		return 0, false
	}
	if strings.ContainsAny(text, ".eE") {
		return 0, false
	}
	value, err := strconv.Atoi(text)
	if err != nil {
		return 0, false
	}
	return value, true
}
