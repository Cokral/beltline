package daemon

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/integrations/shell"
)

// TestOpenTargetsExplicitDemoFolderAndLiveSelectionWithoutStartingAnotherDaemon
func TestOpenTargetsExplicitDemoFolderAndLiveSelectionWithoutStartingAnotherDaemon(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	demo := filepath.Join(root, "demo folder with spaces")
	recorder := &commandRecorder{}
	open := func(folder string, _ shell.Runner) error {
		recorder.open(folder)
		return nil
	}
	explicit := runCommand(t, []string{"open", "--demo-folder", demo}, map[string]string{"BELTLINE_DATA_DIR": "/live selection"}, recorder, open)
	if explicit.status != 0 {
		t.Fatalf("explicit open status %d", explicit.status)
	}
	if want := []string{filepath.Clean(demo)}; !reflect.DeepEqual(recorder.Opened(), want) {
		t.Fatalf("opened %v, want %v", recorder.Opened(), want)
	}
	if len(recorder.Started()) != 0 {
		t.Fatalf("explicit open started a daemon: %v", recorder.Started())
	}

	recorder.Clear()
	live := runCommand(t, []string{"open"}, map[string]string{"BELTLINE_DATA_DIR": "/live data with spaces"}, recorder, open)
	if live.status != 0 {
		t.Fatalf("live open status %d", live.status)
	}
	if want := []string{"/live data with spaces"}; !reflect.DeepEqual(recorder.Opened(), want) {
		t.Fatalf("live opened %v, want %v", recorder.Opened(), want)
	}
	if len(recorder.Started()) != 0 {
		t.Fatalf("live open started a daemon: %v", recorder.Started())
	}
}

// TestStartupOpenFlagsAreParsedAndInvalidCombinationsReturnUsageStatus
func TestStartupOpenFlagsAreParsedAndInvalidCombinationsReturnUsageStatus(t *testing.T) {
	t.Parallel()
	recorder := &commandRecorder{}
	launched := runCommand(t, []string{"--demo", "--open"}, map[string]string{}, recorder, nil)
	if launched.status != 0 {
		t.Fatalf("launch status %d", launched.status)
	}
	started := recorder.Started()
	if len(started) != 1 || started[0].mode != "demo" || !started[0].opening {
		t.Fatalf("started %v, want one demo start opening the browser", started)
	}

	invalid := [][]string{
		{"open", "--demo-folder"},
		{"open", "--demo-folder", ""},
		{"--demo", "--demo"},
		{"--demo-folder", "somewhere"},
		{"--open", "--open"},
	}
	for _, args := range invalid {
		recorder.Clear()
		result := runCommand(t, args, map[string]string{}, recorder, nil)
		if result.status != 2 {
			t.Fatalf("%v: status %d, want 2", args, result.status)
		}
		if !strings.Contains(result.errors, "Usage: beltlined") {
			t.Fatalf("%v: error %q lacks usage", args, result.errors)
		}
		if len(recorder.Started()) != 0 || len(recorder.Opened()) != 0 {
			t.Fatalf("%v: started %v opened %v", args, recorder.Started(), recorder.Opened())
		}
	}
}

// TestDemoEnvironmentVariableSelectsDemoModeLikeTheFlag
func TestDemoEnvironmentVariableSelectsDemoModeLikeTheFlag(t *testing.T) {
	t.Parallel()
	recorder := &commandRecorder{}
	launched := runCommand(t, nil, map[string]string{"BELTLINE_DEMO": "1"}, recorder, nil)
	if launched.status != 0 {
		t.Fatalf("launch status %d", launched.status)
	}
	if started := recorder.Started(); len(started) == 0 || started[0].mode != "demo" {
		t.Fatalf("started %v, want demo", started)
	}
	recorder.Clear()
	other := runCommand(t, nil, map[string]string{"BELTLINE_DEMO": "0"}, recorder, nil)
	if other.status != 0 {
		t.Fatalf("other status %d", other.status)
	}
	if started := recorder.Started(); len(started) == 0 || started[0].mode != "live" {
		t.Fatalf("started %v, want live", started)
	}
}
