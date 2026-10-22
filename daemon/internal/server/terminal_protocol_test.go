package server

import (
	"fmt"
	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
	"strings"
	"testing"

	"encoding/json"
)

func TestAcceptsResizeBoundsAsIntegerCharacterCells(t *testing.T) {
	t.Parallel()
	for _, value := range []int{1, 65535} {
		bytes := []byte(fmt.Sprintf(`{"type":"resize","cols":%d,"rows":%d}`, value, value))
		got, ok := TerminalProtocolResize(bytes)
		want := TerminalResize{Columns: uint16(value), Rows: uint16(value)}
		if !ok || got != want {
			t.Fatalf("resize(%d) = %+v, %v; want %+v", value, got, ok, want)
		}
	}
}

func TestRejectsMalformedUnsupportedAndInvalidDimensions(t *testing.T) {
	t.Parallel()
	invalid := []string{
		"{",
		`{"type":"input","cols":120,"rows":40}`,
		`{"type":"resize","cols":0,"rows":40}`,
		`{"type":"resize","cols":-1,"rows":40}`,
		`{"type":"resize","cols":65536,"rows":40}`,
		`{"type":"resize","cols":1.5,"rows":40}`,
		`{"type":"resize","cols":"120","rows":40}`,
		`{"type":"resize","cols":true,"rows":40}`,
		`{"type":"resize","cols":120}`,
		`{"type":"resize","rows":40}`,
		`{"type":"resize","cols":null,"rows":40}`,
	}
	for _, text := range invalid {
		if got, ok := TerminalProtocolResize([]byte(text)); ok {
			t.Errorf("resize(%s) = %+v, want rejected", text, got)
		}
	}
}

func TestModeFrameReportsTheAcceptedWritePermissionAsTheFirstControlFrame(t *testing.T) {
	t.Parallel()
	if got := TerminalProtocolModeFrame(true); got != `{"type":"mode","readOnly":true}` {
		t.Fatalf("read-only mode frame = %s", got)
	}
	if got := TerminalProtocolModeFrame(false); got != `{"type":"mode","readOnly":false}` {
		t.Fatalf("writable mode frame = %s", got)
	}
	for _, readOnly := range []bool{true, false} {
		frame := []byte(TerminalProtocolModeFrame(readOnly))
		members, err := jsonx.Members(frame)
		if err != nil {
			t.Fatalf("mode frame is not an object: %v", err)
		}
		var kind string
		var got bool
		if err := json.Unmarshal(members["type"], &kind); err != nil || kind != "mode" {
			t.Fatalf("mode frame type = %q, %v", kind, err)
		}
		if err := json.Unmarshal(members["readOnly"], &got); err != nil || got != readOnly {
			t.Fatalf("mode frame readOnly = %v, %v; want %v", got, err, readOnly)
		}
		if _, ok := TerminalProtocolResize(frame); ok {
			t.Fatal("a mode frame is not a resize message")
		}
	}
}

// TestTerminalProtocolQueryShapes covers the query rules the terminal security
// tests exercise over the wire, without a socket.
func TestTerminalProtocolQueryShapes(t *testing.T) {
	t.Parallel()
	valid := map[string]TerminalQuery{
		"session=bl-run":               {Session: "bl-run"},
		"session=bl-run&takeOver=1":    {Session: "bl-run", TakeOver: true},
		"takeOver=1&session=bl%2Drun":  {Session: "bl-run", TakeOver: true},
		"session=bl-run&other=ignored": {Session: "bl-run"},
	}
	for query, want := range valid {
		got, err := TerminalProtocolQuery(query)
		if err != nil || got != want {
			t.Errorf("query(%q) = %+v, %v; want %+v", query, got, err, want)
		}
	}
	invalid := []string{
		"", "takeOver=1", "session=", "session=a&session=b",
		"session=a&takeOver=0", "session=a&takeOver=1&takeOver=1",
		"session=a&takeOver", "session=%FF", "session=a%",
	}
	for _, query := range invalid {
		if got, err := TerminalProtocolQuery(query); err == nil {
			t.Errorf("query(%q) = %+v, want an error", query, got)
		} else if !strings.Contains(err.Error(), "invalid terminal query") {
			t.Errorf("query(%q) error = %v", query, err)
		}
	}
}
