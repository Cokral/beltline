// Tests of the launch setup that do not build a coordinator: New reads the library's signature and the
// factory origin. The tests that build a coordinator (demo or live) are in the build and integration
// files; the live ones need tmux and sit behind the integration tag.

package launch

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/integrations/store"
	"github.com/robbertmanders/beltline/daemon/internal/kit/coordinator"
)

// noNotify discards notifications.
func noNotify(string, string) {}

func TestLiveSelectionUsesANonEmptyOverride(t *testing.T) {
	t.Parallel()
	support := t.TempDir()
	custom := t.TempDir()
	env := func(values map[string]string) func(string) string {
		return func(key string) string { return values[key] }
	}
	if got := store.StandardPaths(env(map[string]string{"BELTLINE_DATA_DIR": custom}), support).Root; got != custom {
		t.Errorf("override root = %q", got)
	}
	fallback := store.StandardPaths(env(map[string]string{"BELTLINE_DATA_DIR": ""}), support).Root
	if want := filepath.Join(support, "Beltline"); fallback != want {
		t.Errorf("empty override root = %q, want %q", fallback, want)
	}
	if got := store.StandardPaths(env(nil), support).Root; got != fallback {
		t.Errorf("no override root = %q, want %q", got, fallback)
	}
}

func TestAHeldFolderBuildsNothingAndWritesNothing(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "settings.json"), []byte("{}"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	holder := startLockHolder(t, root)
	if got := holder.line(); got != "ready" {
		t.Fatalf("holder says %q", got)
	}
	before := snapshotFolder(t, root)
	builds := 0
	build := func(string, Notifier, []string) (*coordinator.Coordinator, error) {
		builds++
		return nil, nil
	}
	_, err := LaunchSessionLive(store.Paths{Root: root}, noNotify, nil, build)
	wantHeld(t, err, root, new(holder.pid))
	_, err = LaunchSessionDemo(&root, "", noNotify, nil, build)
	wantHeld(t, err, root, new(holder.pid))
	if builds != 0 {
		t.Errorf("builds = %d", builds)
	}
	if after := snapshotFolder(t, root); !reflect.DeepEqual(after, before) {
		t.Error("a refused launch changed the folder")
	}
}

func TestAFailedBuildLetsTheFolderGo(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	_, err := LaunchSessionLive(store.Paths{Root: root}, noNotify, nil,
		func(string, Notifier, []string) (*coordinator.Coordinator, error) {
			return nil, fmt.Errorf("broken")
		})
	failure, ok := err.(LaunchFailure)
	if !ok || failure.Kind != LaunchFailureIO {
		t.Fatalf("error = %v, want an io LaunchFailure", err)
	}
	if got := lockProbe(t, root); got != "free" {
		t.Errorf("after a failed build the folder is %q", got)
	}
}

func TestALiveLaunchUpgradesTheFolderBeforeBuilding(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "settings.json"), []byte(`{"factoryRepository":"owner/factories"}`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	formatted := false
	build := func(string, Notifier, []string) (*coordinator.Coordinator, error) {
		_, err := os.Stat(store.Paths{Root: root}.Format())
		formatted = err == nil
		return nil, nil
	}
	session, err := LaunchSessionLive(store.Paths{Root: root}, noNotify, nil, build)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Lock.Release()
	if !formatted || !session.Upgrade.Ran || !session.Upgrade.Settings || session.UpgradeError != nil {
		t.Fatalf("upgraded before the build %v, report %+v, error %v", formatted, session.Upgrade, session.UpgradeError)
	}
}

func TestALiveLaunchRefusesAFolderInANewerFormat(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "format.json"), []byte(`{"version":99}`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "settings.json"), []byte(`{"factoryRepository":"owner/factories"}`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	before := snapshotFolder(t, root)
	builds := 0
	_, err := LaunchSessionLive(store.Paths{Root: root}, noNotify, nil,
		func(string, Notifier, []string) (*coordinator.Coordinator, error) {
			builds++
			return nil, nil
		})
	failure, ok := err.(LaunchFailure)
	if !ok || failure.Kind != LaunchFailureNewerFormat || !strings.Contains(failure.Error(), "format 99") {
		t.Fatalf("error = %v, want a newer-format LaunchFailure", err)
	}
	if builds != 0 {
		t.Errorf("builds = %d", builds)
	}
	if got := lockProbe(t, root); got != "free" {
		t.Errorf("after a refused launch the folder is %q", got)
	}
	after := snapshotFolder(t, root)
	delete(after, "daemon.lock")
	delete(before, "daemon.lock")
	if !reflect.DeepEqual(after, before) {
		t.Error("a refused launch changed the folder")
	}
}
