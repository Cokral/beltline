package daemon

import (
	"context"
	"reflect"
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/integrations/shell"
)

// TestScriptNamesTheBodyAndTitleAndEscapesEveryDynamicCharacter
func TestScriptNamesTheBodyAndTitleAndEscapesEveryDynamicCharacter(t *testing.T) {
	t.Parallel()
	if got, want := MacNotificationScript("Run done", "acme/app #7"), `display notification "acme/app #7" with title "Run done"`; got != want {
		t.Fatalf("script %q, want %q", got, want)
	}
	if got, want := MacNotificationLiteral(`say "hi" \ now`), `"say \"hi\" \\ now"`; got != want {
		t.Fatalf("literal %q, want %q", got, want)
	}
	if got, want := MacNotificationLiteral("one\ntwo\r\tthree"), `"one\ntwo\r\tthree"`; got != want {
		t.Fatalf("literal %q, want %q", got, want)
	}
	// A quote in a run's title cannot end the literal and add a statement.
	hostile := MacNotificationScript(`x" & do shell script "touch y" & `, "b")
	if want := `display notification "b" with title "x\" & do shell script \"touch y\" & "`; hostile != want {
		t.Fatalf("hostile script %q, want %q", hostile, want)
	}
}

// TestPostRunsOsascriptWithTheScript
func TestPostRunsOsascriptWithTheScript(t *testing.T) {
	t.Parallel()
	runner := &shell.Recorder{}
	MacNotification{Runner: runner}.Post(context.Background(), "Title", "Body")
	var calls [][]string
	for _, call := range runner.Calls() {
		calls = append(calls, append([]string{call.Executable}, call.Args...))
	}
	want := [][]string{{"/usr/bin/osascript", "-e", `display notification "Body" with title "Title"`}}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls %q, want %q", calls, want)
	}
}
