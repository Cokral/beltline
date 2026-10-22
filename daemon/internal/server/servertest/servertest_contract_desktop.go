// Contract cases for the desktop endpoints.

package servertest

import (
	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
	"testing"
)

// The desktop area's contract cases.
var desktopCases = []string{"desktopReveal", "desktopRevealData", "desktopVSCode", "desktopGhostty", "desktopOpenFile", "desktopOpenURL"}

func init() {
	registerArea(desktopCases, exerciseDesktop)
}

// exerciseDesktop serves one desktop case and checks its golden; it reports whether the case is the desktop area's.
func exerciseDesktop(f *Fixture, t testing.TB, contractCase string) bool {
	payloads := map[string]struct {
		path string
		body map[string]string
	}{
		"desktopReveal":     {"/api/v1/desktop/reveal", map[string]string{"runId": f.ActionRunID, "kind": "run"}},
		"desktopRevealData": {"/api/v1/desktop/reveal-data", map[string]string{}},
		"desktopVSCode":     {"/api/v1/desktop/vscode", map[string]string{"runId": f.ActionRunID, "targetId": f.VSCodeTargetID}},
		"desktopGhostty":    {"/api/v1/desktop/ghostty", map[string]string{"runId": f.ActionRunID, "attemptId": f.ActionAttemptID}},
		"desktopOpenFile":   {"/api/v1/desktop/open-file", map[string]string{"runId": f.ActionRunID, "attemptId": f.ActionAttemptID}},
		"desktopOpenURL":    {"/api/v1/desktop/open-url", map[string]string{"runId": f.ActionRunID, "targetId": f.LinkTargetID}},
	}
	payload, ok := payloads[contractCase]
	if !ok {
		return false
	}
	body, err := jsonx.Marshal(payload.body)
	if err != nil {
		t.Fatalf("%s body: %v", contractCase, err)
	}
	reply := f.Do(Request{Method: "POST", Path: payload.path, Token: ContractToken, Body: body})
	CheckGolden(t, contractCase, "desktop-ok.json", reply, f.Root)
	return true
}
