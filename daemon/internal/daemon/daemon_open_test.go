package daemon

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/integrations/shell"
	"github.com/robbertmanders/beltline/daemon/internal/kit/launch"
	"github.com/robbertmanders/beltline/daemon/internal/testkit"
	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// openToken is the token the open tests' records carry.
var openToken = strings.Repeat("a", 64)

// openInfo is a discovery record for the open tests, changed by mutate.
func openInfo(mutate func(info *launch.DaemonInfo)) launch.DaemonInfo {
	info := launch.DaemonInfo{Pid: int32(os.Getpid()), Port: 7444, Token: openToken, Version: "build 65", APIVersion: launch.VersionCurrent}
	if mutate != nil {
		mutate(&info)
	}
	return info
}

// versionBody is a /api/v1/version body for version.
func versionBody(t *testing.T, apiVersion int, version string) []byte {
	t.Helper()
	data, err := jsonx.Marshal(launch.Version{APIVersion: apiVersion, Version: version})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// okProbe answers the version probe with the open tests' build.
func okProbe(t *testing.T) launch.DaemonDiscoveryProbeFunc {
	return func(context.Context, launch.DaemonInfo) (launch.DaemonDiscoveryReply, error) {
		return launch.DaemonDiscoveryReply{Status: 200, Body: versionBody(t, launch.VersionCurrent, "build 65")}, nil
	}
}

// publishDiscovery writes info into folder as the daemon does.
func publishDiscovery(t *testing.T, folder string, info launch.DaemonInfo) {
	t.Helper()
	if err := info.Publish(folder); err != nil {
		t.Fatal(err)
	}
}

// TestValidatesDiscoveryAndInvokesOpenWithOneEncodedSignInArgument
func TestValidatesDiscoveryAndInvokesOpenWithOneEncodedSignInArgument(t *testing.T) {
	t.Parallel()
	folder := filepath.Join(t.TempDir(), "selected demo folder with spaces")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	publishDiscovery(t, folder, openInfo(nil))
	before := testkit.FolderSnapshot(t, folder)
	runner := &shell.Recorder{}
	discovery := launch.NewDaemonDiscovery(nil, func(int32) bool { return true }, okProbe(t))
	if err := (DaemonOpen{Runner: runner, Discovery: discovery, Platform: launch.PlatformMacOS}).Run(context.Background(), folder); err != nil {
		t.Fatal(err)
	}
	calls := runner.Calls()
	if len(calls) != 1 {
		t.Fatalf("%d open calls, want 1", len(calls))
	}
	if calls[0].Executable != "/usr/bin/open" {
		t.Fatalf("executable %q", calls[0].Executable)
	}
	want := []string{"http://127.0.0.1:7444/auth?token=" + openToken}
	if !reflect.DeepEqual(calls[0].Args, want) {
		t.Fatalf("arguments %q, want %q", calls[0].Args, want)
	}
	if after := testkit.FolderSnapshot(t, folder); !reflect.DeepEqual(after, before) {
		t.Fatal("opening changed the data folder")
	}
}

// TestValidationFailuresAreClearRedactedAndDoNotLaunch
func TestValidationFailuresAreClearRedactedAndDoNotLaunch(t *testing.T) {
	t.Parallel()
	folder := t.TempDir()
	tokenSentinel := strings.Repeat("b", 64)
	valid := openInfo(func(info *launch.DaemonInfo) { info.Token = tokenSentinel })
	encode := func(info launch.DaemonInfo) []byte {
		data, err := jsonx.Marshal(info)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	reply := func(status int, body []byte) *launch.DaemonDiscoveryReply {
		return &launch.DaemonDiscoveryReply{Status: status, Body: body}
	}
	cases := []struct {
		label        string
		data         []byte
		alive        bool
		reply        *launch.DaemonDiscoveryReply
		reachesProbe bool
	}{
		{"missing daemon.json", nil, true, nil, false},
		{"malformed daemon.json", []byte("{"), true, nil, false},
		{"invalid pid", encode(openInfo(func(i *launch.DaemonInfo) { i.Pid = 0; i.Token = tokenSentinel })), true, nil, false},
		{"invalid port", encode(openInfo(func(i *launch.DaemonInfo) { i.Port = 0; i.Token = tokenSentinel })), true, nil, false},
		{"invalid token", encode(openInfo(func(i *launch.DaemonInfo) { i.Token = "bad" })), true, nil, false},
		{"unsupported API version", encode(openInfo(func(i *launch.DaemonInfo) {
			i.APIVersion = launch.VersionCurrent + 1
			i.Token = tokenSentinel
		})), true, nil, false},
		{"dead pid", encode(valid), false, nil, false},
		{"timeout", encode(valid), true, nil, true},
		{"redirect", encode(valid), true, reply(302, nil), true},
		{"rejected credentials", encode(valid), true, reply(401, nil), true},
		{"bad response JSON", encode(valid), true, reply(200, []byte("nope")), true},
		{"mismatched API version", encode(valid), true, reply(200, versionBody(t, launch.VersionCurrent+1, "build 65")), true},
		{"mismatched build version", encode(valid), true, reply(200, versionBody(t, launch.VersionCurrent, "another build")), true},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			t.Parallel()
			runner := &shell.Recorder{}
			discovery := launch.NewDaemonDiscovery(
				func(string) ([]byte, error) {
					if tc.data == nil {
						return nil, fmt.Errorf("missing")
					}
					return tc.data, nil
				},
				func(int32) bool { return tc.alive },
				func(context.Context, launch.DaemonInfo) (launch.DaemonDiscoveryReply, error) {
					if tc.reply != nil {
						return *tc.reply, nil
					}
					return launch.DaemonDiscoveryReply{}, fmt.Errorf("unreachable")
				},
			)
			before := testkit.FolderSnapshot(t, folder)
			err := DaemonOpen{Runner: runner, Discovery: discovery}.Run(context.Background(), folder)
			if err == nil {
				t.Fatal("unexpectedly succeeded")
			}
			message := err.Error()
			if !strings.Contains(message, folder) {
				t.Fatalf("message %q lacks the folder", message)
			}
			if strings.Contains(message, tokenSentinel) {
				t.Fatalf("message %q exposes the discovery token", message)
			}
			if !strings.Contains(strings.ToLower(message), "failed") {
				t.Fatalf("message %q does not say it failed", message)
			}
			if calls := runner.Calls(); len(calls) != 0 {
				t.Fatalf("browser launched: %v", calls)
			}
			if after := testkit.FolderSnapshot(t, folder); !reflect.DeepEqual(after, before) {
				t.Fatal("validation changed the data folder")
			}
		})
	}
}

// TestFailingBrowserRunnerIsReportedWithoutEchoingItsUrl
func TestFailingBrowserRunnerIsReportedWithoutEchoingItsUrl(t *testing.T) {
	t.Parallel()
	folder := t.TempDir()
	publishDiscovery(t, folder, openInfo(nil))
	runner := &shell.Recorder{Reply: func(shell.Call) shell.Result {
		return shell.Result{Status: 1, Stderr: "browser not available"}
	}}
	discovery := launch.NewDaemonDiscovery(nil, func(int32) bool { return true }, okProbe(t))
	err := DaemonOpen{Runner: runner, Discovery: discovery}.Run(context.Background(), folder)
	if err == nil {
		t.Fatal("browser launch unexpectedly succeeded")
	}
	if !strings.Contains(err.Error(), "launch browser") || !strings.Contains(err.Error(), folder) {
		t.Fatalf("message %q lacks the step or folder", err)
	}
	if strings.Contains(err.Error(), openToken) {
		t.Fatalf("message %q exposes the token", err)
	}
	if len(runner.Calls()) != 1 {
		t.Fatalf("%d browser calls, want 1", len(runner.Calls()))
	}
}

// TestSelectedLiveFolderDoesNotCreateData
func TestSelectedLiveFolderDoesNotCreateData(t *testing.T) {
	t.Parallel()
	support := t.TempDir()
	if selected := DaemonOpenLiveFolder(map[string]string{}, support); selected != filepath.Join(support, "Beltline") {
		t.Fatalf("selected %q", selected)
	}
	if _, err := os.Stat(filepath.Join(support, "Beltline")); err == nil {
		t.Fatal("the current folder was created")
	}
	if selected := DaemonOpenLiveFolder(map[string]string{"BELTLINE_DATA_DIR": "/custom data"}, support); selected != "/custom data" {
		t.Fatalf("custom folder %q", selected)
	}
}
