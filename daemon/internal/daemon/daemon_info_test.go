package daemon

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"encoding/json"
	"github.com/robbertmanders/beltline/daemon/internal/kit/launch"
)

// TestTokensContainThirtyTwoFreshSecureBytes
func TestTokensContainThirtyTwoFreshSecureBytes(t *testing.T) {
	t.Parallel()
	first, err := launch.DaemonInfoMakeToken()
	if err != nil {
		t.Fatal(err)
	}
	second, err := launch.DaemonInfoMakeToken()
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 64 {
		t.Fatalf("token length %d, want 64", len(first))
	}
	if _, err := hex.DecodeString(first); err != nil {
		t.Fatalf("token %q is not hex: %v", first, err)
	}
	if len(second) != 64 || first == second {
		t.Fatalf("tokens %q and %q should be fresh 64-character values", first, second)
	}
}

// TestPublicationUsesAnAtomicMode0600Record
func TestPublicationUsesAnAtomicMode0600Record(t *testing.T) {
	t.Parallel()
	folder := t.TempDir()
	file := filepath.Join(folder, launch.DaemonInfoFileName)
	info := launch.DaemonInfo{Pid: 42, Port: 7332, Token: strings.Repeat("a", 64), Version: "test", APIVersion: 1}
	if err := info.Publish(folder); err != nil {
		t.Fatal(err)
	}
	stat, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	if mode := stat.Mode().Perm(); mode != 0o600 {
		t.Fatalf("mode %o, want 600", mode)
	}
	if got := readDiscoveryRecord(t, file); !reflect.DeepEqual(got, info) {
		t.Fatalf("record %+v, want %+v", got, info)
	}

	replacement := launch.DaemonInfo{Pid: 43, Port: 7332, Token: strings.Repeat("b", 64), Version: "next", APIVersion: 1}
	if err := replacement.Publish(folder); err != nil {
		t.Fatal(err)
	}
	if got := readDiscoveryRecord(t, file); !reflect.DeepEqual(got, replacement) {
		t.Fatalf("record %+v, want %+v", got, replacement)
	}
	entries, err := os.ReadDir(folder)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != launch.DaemonInfoFileName {
		t.Fatalf("folder holds %v, want only %s", entries, launch.DaemonInfoFileName)
	}
}

// readDiscoveryRecord decodes the discovery record in file.
func readDiscoveryRecord(t *testing.T, file string) launch.DaemonInfo {
	t.Helper()
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var info launch.DaemonInfo
	if err := json.Unmarshal(data, &info); err != nil {
		t.Fatalf("decode %s: %v", file, err)
	}
	return info
}

// TestReadersNeverSeePartialRecordsDuringReplacement
func TestReadersNeverSeePartialRecordsDuringReplacement(t *testing.T) {
	t.Parallel()
	folder := t.TempDir()
	file := filepath.Join(folder, launch.DaemonInfoFileName)
	if err := (launch.DaemonInfo{Pid: 1, Port: 7331, Token: strings.Repeat("0", 64), Version: "0", APIVersion: 1}).Publish(folder); err != nil {
		t.Fatal(err)
	}

	var (
		failureMu sync.Mutex
		failure   string
	)
	record := func(err error) {
		failureMu.Lock()
		defer failureMu.Unlock()
		if failure == "" {
			failure = err.Error()
		}
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for index := 1; index <= 250; index++ {
			info := launch.DaemonInfo{
				Pid:        int32(index),
				Port:       7331 + index,
				Token:      fmt.Sprintf("%064x", index),
				Version:    fmt.Sprintf("version-%d", index),
				APIVersion: 1,
			}
			if err := info.Publish(folder); err != nil {
				record(err)
			}
		}
	}()
	go func() {
		defer wg.Done()
		for range 1000 {
			data, err := os.ReadFile(file)
			if err != nil {
				record(err)
				continue
			}
			var decoded launch.DaemonInfo
			if err := json.Unmarshal(data, &decoded); err != nil {
				record(err)
				continue
			}
			if decoded.Pid <= 0 || decoded.Port <= 0 || decoded.Version == "" {
				record(fmt.Errorf("partial record %+v", decoded))
			}
		}
	}()
	wg.Wait()
	failureMu.Lock()
	defer failureMu.Unlock()
	if failure != "" {
		t.Fatalf("reader saw a failure: %s", failure)
	}
}

// TestLogAppendsLifecycleAndNotifierTextAcrossOpens
func TestLogAppendsLifecycleAndNotifierTextAcrossOpens(t *testing.T) {
	t.Parallel()
	folder := t.TempDir()
	file := filepath.Join(folder, DaemonLogFileName)
	if err := os.WriteFile(file, []byte("earlier run\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	log, err := OpenDaemonLog(folder)
	if err != nil {
		t.Fatal(err)
	}
	log.Append("Daemon ready")
	log.Notification("Needs attention", "Check the build")
	log.Close()

	reopened, err := OpenDaemonLog(folder)
	if err != nil {
		t.Fatal(err)
	}
	reopened.Append("Daemon restarted")
	reopened.Close()

	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	contents := string(data)
	for _, want := range []string{"earlier run", "Daemon ready", "Notification: Needs attention — Check the build", "Daemon restarted"} {
		if !strings.Contains(contents, want) {
			t.Fatalf("log lacks %q:\n%s", want, contents)
		}
	}
}

// TestAssistantCheckCommandKeepsPathsWithSpacesAsOneAbsoluteArgument
func TestAssistantCheckCommandKeepsPathsWithSpacesAsOneAbsoluteArgument(t *testing.T) {
	t.Parallel()
	command := CheckCommand("relative path/beltlined")
	if len(command) != 2 {
		t.Fatalf("command %q, want two arguments", command)
	}
	if !strings.HasPrefix(command[0], "/") || !strings.HasSuffix(command[0], "/relative path/beltlined") {
		t.Fatalf("executable argument %q", command[0])
	}
	if command[1] != "--check-library" {
		t.Fatalf("flag %q", command[1])
	}
}
