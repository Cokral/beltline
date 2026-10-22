package launch

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

const discoveryToken = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

// discoveryFolder is the folder the tests name; its files are never read, the reads are injected.
const discoveryFolder = "/Data/discovery folder"

// discoveryInfo is a discovery record with the given fields, the others valid.
func discoveryInfo(pid int32, port int, token, version string, apiVersion int) DaemonInfo {
	if token == "" {
		token = discoveryToken
	}
	if port == 0 {
		port = 7444
	}
	if pid == 0 {
		pid = 42
	}
	if version == "" {
		version = "build 65"
	}
	if apiVersion == 0 {
		apiVersion = VersionCurrent
	}
	return DaemonInfo{Pid: pid, Port: port, Token: token, Version: version, APIVersion: apiVersion}
}

// encodeInfo is the record as the daemon writes it.
func encodeInfo(t *testing.T, info DaemonInfo) []byte {
	t.Helper()
	data, err := jsonx.Marshal(info)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	return data
}

// versionBody is a version response with the given build.
func versionBody(t *testing.T, version string, apiVersion int) []byte {
	t.Helper()
	data, err := jsonx.Marshal(Version{APIVersion: apiVersion, Version: version})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	return data
}

// probeRecorder records the discovery records a probe received, and never answers.
type probeRecorder struct {
	mu    sync.Mutex
	infos []DaemonInfo
	reply DaemonDiscoveryReply
	err   error
}

// probe records info and answers with the recorder's reply, or its error.
func (r *probeRecorder) probe(_ context.Context, info DaemonInfo) (DaemonDiscoveryReply, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.infos = append(r.infos, info)
	if r.err != nil {
		return DaemonDiscoveryReply{}, r.err
	}
	return r.reply, nil
}

// received is the records the probe got so far.
func (r *probeRecorder) received() []DaemonInfo {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]DaemonInfo(nil), r.infos...)
}

// unreachable is a recorder whose probe fails, so a test that reaches it fails loudly.
func unreachable() *probeRecorder {
	return &probeRecorder{err: errors.New("cannot connect")}
}

// expectDiscoveryFailure asserts verify fails at step with detail, names the folder, and never the token.
func expectDiscoveryFailure(t *testing.T, discovery DaemonDiscovery, step, detail string) {
	t.Helper()
	_, err := discovery.Verify(context.Background(), discoveryFolder)
	failure, ok := err.(DaemonDiscoveryFailure)
	if !ok {
		t.Fatalf("error = %v, want a DaemonDiscoveryFailure", err)
	}
	if failure.Step != step || failure.Detail != detail || failure.Folder != discoveryFolder {
		t.Errorf("failure = %+v, want step %q detail %q", failure, step, detail)
	}
	if strings.Contains(failure.Error(), discoveryToken) {
		t.Error("the failure names the token")
	}
}

// reading returns a discovery read that gives data.
func reading(data []byte) DaemonDiscoveryReadFunc {
	return func(string) ([]byte, error) { return data, nil }
}

// alive reports every pid as a live process.
func alive(int32) bool { return true }

func TestAMissingDiscoveryFileFailsBeforeAnyProbe(t *testing.T) {
	t.Parallel()
	probes := unreachable()
	discovery := NewDaemonDiscovery(func(string) ([]byte, error) { return nil, os.ErrNotExist }, nil, probes.probe)
	expectDiscoveryFailure(t, discovery, "read daemon.json", "file is missing or unreadable")
	if n := len(probes.received()); n != 0 {
		t.Errorf("probes = %d", n)
	}
}

func TestAnUnreadableDiscoveryFileFailsBeforeAnyProbe(t *testing.T) {
	t.Parallel()
	probes := unreachable()
	discovery := NewDaemonDiscovery(func(string) ([]byte, error) { return nil, os.ErrPermission }, nil, probes.probe)
	expectDiscoveryFailure(t, discovery, "read daemon.json", "file is missing or unreadable")
	if n := len(probes.received()); n != 0 {
		t.Errorf("probes = %d", n)
	}
}

func TestAMalformedRecordFailsBeforeAnyProbe(t *testing.T) {
	t.Parallel()
	probes := unreachable()
	discovery := NewDaemonDiscovery(reading([]byte("{")), nil, probes.probe)
	expectDiscoveryFailure(t, discovery, "read daemon.json", "record is malformed")
	if n := len(probes.received()); n != 0 {
		t.Errorf("probes = %d", n)
	}
}

func TestAnInvalidRecordNamesTheFieldThatFails(t *testing.T) {
	t.Parallel()
	cases := []struct {
		info   DaemonInfo
		detail string
	}{
		{withPid(discoveryInfo(0, 0, "", "", 0), 0), "pid must be positive"},
		{withPort(0), "port is invalid"},
		{withPort(65536), "port is invalid"},
		{withToken("short"), "token is invalid"},
		{withToken(strings.Repeat("A", 64)), "token is invalid"},
		{withVersion(""), "build version is empty"},
		{withAPI(VersionCurrent + 1), fmt.Sprintf("unsupported API version %d", VersionCurrent+1)},
	}
	for _, c := range cases {
		probes := unreachable()
		discovery := NewDaemonDiscovery(reading(encodeInfo(t, c.info)), alive, probes.probe)
		expectDiscoveryFailure(t, discovery, "validate daemon.json", c.detail)
		if n := len(probes.received()); n != 0 {
			t.Errorf("%s: probes = %d", c.detail, n)
		}
	}
}

// withPid, withPort, withToken, withVersion and withAPI are the valid record with one field changed.
func withPid(info DaemonInfo, pid int32) DaemonInfo { info.Pid = pid; return info }
func withPort(port int) DaemonInfo {
	info := discoveryInfo(0, 0, "", "", 0)
	info.Port = port
	return info
}
func withToken(token string) DaemonInfo {
	info := discoveryInfo(0, 0, "", "", 0)
	info.Token = token
	return info
}
func withVersion(version string) DaemonInfo {
	info := discoveryInfo(0, 0, "", "", 0)
	info.Version = version
	return info
}
func withAPI(api int) DaemonInfo {
	info := discoveryInfo(0, 0, "", "", 0)
	info.APIVersion = api
	return info
}

func TestAStaleRecordWithADeadPidFailsBeforeAnyProbe(t *testing.T) {
	t.Parallel()
	probes := unreachable()
	discovery := NewDaemonDiscovery(reading(encodeInfo(t, discoveryInfo(99, 0, "", "", 0))),
		func(int32) bool { return false }, probes.probe)
	expectDiscoveryFailure(t, discovery, "check daemon pid", "process 99 is not running")
	if n := len(probes.received()); n != 0 {
		t.Errorf("probes = %d", n)
	}
}

func TestALiveRecordWhoseLockOwnerIsAnotherPidFailsBeforeAnyProbe(t *testing.T) {
	t.Parallel()
	probes := unreachable()
	discovery := NewDaemonDiscovery(reading(encodeInfo(t, discoveryInfo(42, 0, "", "", 0))), alive, probes.probe)
	owner := int32(4343)
	_, err := discovery.VerifyOwnedBy(context.Background(), discoveryFolder, owner)
	failure, ok := err.(DaemonDiscoveryFailure)
	if !ok || failure.Step != "check daemon pid" || failure.Detail != "process 42 is not the lock's owner 4343" {
		t.Fatalf("error = %v", err)
	}
	if n := len(probes.received()); n != 0 {
		t.Errorf("probes = %d", n)
	}
}

func TestAProbeThatCannotReachTheListenerFails(t *testing.T) {
	t.Parallel()
	discovery := NewDaemonDiscovery(reading(encodeInfo(t, discoveryInfo(0, 0, "", "", 0))), alive, unreachable().probe)
	expectDiscoveryFailure(t, discovery, "check daemon API", "listener is unreachable or timed out")
}

func TestAProbeThatIsNotA200FailsWithItsStatus(t *testing.T) {
	t.Parallel()
	for _, status := range []int{302, 401, 404, 500} {
		probes := &probeRecorder{reply: DaemonDiscoveryReply{Status: status, Body: versionBody(t, "build 65", VersionCurrent)}}
		discovery := NewDaemonDiscovery(reading(encodeInfo(t, discoveryInfo(0, 0, "", "", 0))), alive, probes.probe)
		expectDiscoveryFailure(t, discovery, "check daemon API", fmt.Sprintf("version request returned HTTP %d", status))
	}
}

func TestAMalformedVersionResponseFails(t *testing.T) {
	t.Parallel()
	probes := &probeRecorder{reply: DaemonDiscoveryReply{Status: 200, Body: []byte("nope")}}
	discovery := NewDaemonDiscovery(reading(encodeInfo(t, discoveryInfo(0, 0, "", "", 0))), alive, probes.probe)
	expectDiscoveryFailure(t, discovery, "check daemon API", "version response is malformed")
}

func TestAnAPIVersionMismatchFails(t *testing.T) {
	t.Parallel()
	probes := &probeRecorder{reply: DaemonDiscoveryReply{Status: 200, Body: versionBody(t, "build 65", VersionCurrent+1)}}
	discovery := NewDaemonDiscovery(reading(encodeInfo(t, discoveryInfo(0, 0, "", "", 0))), alive, probes.probe)
	expectDiscoveryFailure(t, discovery, "check daemon API", "API version does not match daemon.json")
}

func TestABuildVersionMismatchFails(t *testing.T) {
	t.Parallel()
	probes := &probeRecorder{reply: DaemonDiscoveryReply{Status: 200, Body: versionBody(t, "another build", VersionCurrent)}}
	discovery := NewDaemonDiscovery(reading(encodeInfo(t, discoveryInfo(0, 0, "", "", 0))), alive, probes.probe)
	expectDiscoveryFailure(t, discovery, "check daemon API", "build version does not match daemon.json")
}

func TestAVerifiedDaemonReturnsItsPortAndLoopbackSignInURL(t *testing.T) {
	t.Parallel()
	probes := &probeRecorder{reply: DaemonDiscoveryReply{Status: 200, Body: versionBody(t, "build 65", VersionCurrent)}}
	discovery := NewDaemonDiscovery(reading(encodeInfo(t, discoveryInfo(42, 7444, "", "", 0))), alive, probes.probe)
	verified, err := discovery.Verify(context.Background(), discoveryFolder)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	want := DaemonDiscoveryVerified{Pid: 42, Port: 7444, Version: "build 65", SignInURL: "http://127.0.0.1:7444/auth?token=" + discoveryToken}
	if verified != want {
		t.Errorf("verified = %+v, want %+v", verified, want)
	}
	var ports []int
	var tokens []string
	for _, info := range probes.received() {
		ports = append(ports, info.Port)
		tokens = append(tokens, info.Token)
	}
	if !reflect.DeepEqual(ports, []int{7444}) || !reflect.DeepEqual(tokens, []string{discoveryToken}) {
		t.Errorf("probes saw ports %v tokens %v", ports, tokens)
	}
}

func TestAVerifiedOwnerMatchingTheLockPasses(t *testing.T) {
	t.Parallel()
	probes := &probeRecorder{reply: DaemonDiscoveryReply{Status: 200, Body: versionBody(t, "build 65", VersionCurrent)}}
	discovery := NewDaemonDiscovery(reading(encodeInfo(t, discoveryInfo(42, 0, "", "", 0))), alive, probes.probe)
	verified, err := discovery.VerifyOwnedBy(context.Background(), discoveryFolder, 42)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if verified.Pid != 42 {
		t.Errorf("pid = %d", verified.Pid)
	}
}

func TestVerifyingNeverChangesTheFolder(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	self := int32(os.Getpid())
	if err := discoveryInfo(self, 0, "", "", 0).Publish(root); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if err := os.WriteFile(OwnershipLockFile(root), []byte(fmt.Sprintf("%d\n", self)), 0o644); err != nil {
		t.Fatalf("lock file: %v", err)
	}
	before := snapshotFolder(t, root)
	ok := NewDaemonDiscovery(nil, alive, (&probeRecorder{reply: DaemonDiscoveryReply{Status: 200, Body: versionBody(t, "build 65", VersionCurrent)}}).probe)
	if _, err := ok.VerifyOwnedBy(context.Background(), root, self); err != nil {
		t.Fatalf("verify: %v", err)
	}
	failing := NewDaemonDiscovery(nil, func(int32) bool { return false }, nil)
	_, _ = failing.Verify(context.Background(), root)
	if after := snapshotFolder(t, root); !reflect.DeepEqual(after, before) {
		t.Errorf("the folder changed")
	}
}

func TestAFailureNamesTheFolderAndNeverTheToken(t *testing.T) {
	t.Parallel()
	failure := DaemonDiscoveryFailure{Folder: discoveryFolder, Step: "check daemon API", Detail: "listener is unreachable or timed out"}
	message := failure.Error()
	if !strings.Contains(message, discoveryFolder) || !strings.Contains(message, "failed") {
		t.Errorf("message = %q", message)
	}
	if strings.Contains(message, discoveryToken) {
		t.Errorf("message names the token: %q", message)
	}
}
