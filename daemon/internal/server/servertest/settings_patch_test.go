// Contract tests of the settings patch: partial and persistent, refusal of invalid candidates, wrong
// methods, bodies and auth.

package servertest

import (
	"reflect"
	"testing"

	"encoding/json"
	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/store"
)

// settingsPatchPath is the settings patch command's path.
const settingsPatchPath = "/api/v1/settings/patch"

// postSettingsPatch sends body as a settings patch with the contract token.
func postSettingsPatch(f *Fixture, body string) Reply {
	return f.Do(Request{Method: "POST", Path: settingsPatchPath, Token: ContractToken, Body: []byte(body)})
}

// TestSettingsPatchIsPartialPersistentAndRejectsInvalidCandidates checks that settings patches are partial and persistent, and reject invalid candidates.
// settingsPatchIsPartialPersistentAndRejectsInvalidCandidates.
func TestSettingsPatchIsPartialPersistentAndRejectsInvalidCandidates(t *testing.T) {
	t.Parallel()
	f := Start(t, Options{})

	reply := postSettingsPatch(f, `{"notifications":true,"libraryRepositories":[{"repository":"owner/repo","path":"","role":"shared","publish":"pullRequest"}]}`)
	CheckGolden(t, "settings patch", "command-settings-patch.json", reply, f.Root)
	var response struct {
		Notifications       bool                     `json:"notifications"`
		LibraryRepositories []core.LibraryRepository `json:"libraryRepositories"`
	}
	if err := json.Unmarshal(reply.Body, &response); err != nil {
		t.Fatal(err)
	}
	if !response.Notifications || len(response.LibraryRepositories) != 1 || response.LibraryRepositories[0].Repository != "owner/repo" {
		t.Fatalf("patch: notifications %v, libraryRepositories %v", response.Notifications, response.LibraryRepositories)
	}
	if got := f.Coordinator.ReadSettings().LibraryRepositories; len(got) != 1 || got[0].Repository != "owner/repo" {
		t.Fatalf("coordinator holds libraryRepositories %v", got)
	}
	if got := store.NewStorage(store.Paths{Root: f.Root}).LoadSettings().LibraryRepositories; len(got) != 1 || got[0].Repository != "owner/repo" {
		t.Fatalf("accepted patches persist: saved %v", got)
	}

	unchanged := f.Coordinator.ReadSettings()
	empty := postSettingsPatch(f, `{}`)
	CheckGolden(t, "settings patch", "command-settings-patch.json", empty, f.Root)
	if !reflect.DeepEqual(f.Coordinator.ReadSettings(), unchanged) {
		t.Fatalf("an empty patch changed settings")
	}

	// A rejected candidate never partially writes the accepted fields.
	for _, invalid := range []string{
		`{"notifications":false,"afk":{"limit":11}}`,
		`{"notifications":false,"afk":{"limit":0}}`,
		`{"notifications":false,"afk":{"limit":2.5}}`,
		`{"notifications":false,"libraryRepositories":[{"repository":"not-a-repository","path":"","role":"shared","publish":"pullRequest"}]}`,
		`{"paused":true}`,
		`{"modelCatalogs":{}}`,
		`{"allowedModels":{}}`,
		`{"configurationBackend":"pi"}`,
		`{"notifications":"yes"}`,
		`{"notifications":null}`,
		`{"afk":{"thinking":"high","guidance":null}}`,
		`{"afk":null}`,
	} {
		rejected := postSettingsPatch(f, invalid)
		if rejected.Status != 422 {
			t.Fatalf("patch %s: status %d, want 422", invalid, rejected.Status)
		}
		if !reflect.DeepEqual(f.Coordinator.ReadSettings(), unchanged) {
			t.Fatalf("rejected patch %s changed settings", invalid)
		}
	}

	// Disabled blank AFK stays editable; it just does not authorize AFK.
	blank := postSettingsPatch(f, `{"afk":{"model":"","guidance":"keep it short"}}`)
	CheckGolden(t, "settings patch disabled", "command-settings-patch-disabled.json", blank, f.Root)
	var afk struct {
		Afk struct {
			Model string `json:"model"`
		} `json:"afk"`
	}
	if err := json.Unmarshal(blank.Body, &afk); err != nil {
		t.Fatal(err)
	}
	if afk.Afk.Model != "" {
		t.Fatalf("blank AFK model: %q", afk.Afk.Model)
	}
	if got := f.Coordinator.ReadSettings().Afk.Guidance; got != "keep it short" {
		t.Fatalf("AFK guidance %q", got)
	}
}

// TestSettingsPatchRejectsWrongMethodsBodiesAndAuth checks the settings lines of the wrong-method, body and auth cases.
// commandTransportRejectsWrongMethodsUnknownRunnersBodiesAndAuth.
func TestSettingsPatchRejectsWrongMethodsBodiesAndAuth(t *testing.T) {
	t.Parallel()
	f := Start(t, Options{})
	if got := f.Do(Request{Method: "POST", Path: settingsPatchPath}).Status; got != 401 {
		t.Fatalf("no token: status %d, want 401", got)
	}
	if got := f.Do(Request{Method: "POST", Path: settingsPatchPath, Token: "wrong", Body: []byte(`{}`)}).Status; got != 401 {
		t.Fatalf("wrong token: status %d, want 401", got)
	}
	// `{}` is a valid no-op patch; an unknown field is refused before the command runs.
	if got := postSettingsPatch(f, `{}`).Status; got != 200 {
		t.Fatalf("empty patch: status %d, want 200", got)
	}
	if got := postSettingsPatch(f, `{"unknownField":true}`).Status; got != 422 {
		t.Fatalf("unknown field: status %d, want 422", got)
	}
	if got := f.Do(Request{Method: "GET", Path: settingsPatchPath, Token: ContractToken}).Status; got != 404 {
		t.Fatalf("GET of the patch route: status %d, want 404", got)
	}
}
