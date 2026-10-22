// Contract tests of the pause, AFK, check-now, quit, models and tools commands. The settings-patch,
// assistant and AFK-review tests are in their own files.

package servertest

import (
	"context"
	"reflect"
	"slices"
	"testing"
	"time"

	"encoding/json"
	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/store"
	"github.com/robbertmanders/beltline/daemon/internal/kit/coordinator"
	"github.com/robbertmanders/beltline/daemon/internal/server/api"
	"github.com/robbertmanders/beltline/daemon/internal/server/routes"
	"github.com/robbertmanders/beltline/daemon/internal/testkit"
	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// quitWait is how long a test waits for the quit handler (the route answers, then stops after 200 milliseconds).
const quitWait = 5 * time.Second

// stateWire is the part of the state resource these tests read.
type stateWire struct {
	GloballyPaused bool `json:"globallyPaused"`
	Settings       struct {
		AfkEnabled bool `json:"afkEnabled"`
	} `json:"settings"`
}

// modelRunnerWire is the part of a runner's models resource these tests read.
type modelRunnerWire struct {
	Runner     core.Backend `json:"runner"`
	Executable string       `json:"executable"`
	Usable     bool         `json:"usable"`
	Loading    bool         `json:"loading"`
	Error      *string      `json:"error,omitzero"`
	Catalog    *struct {
		RefreshedAt jsonx.Date `json:"refreshedAt"`
	} `json:"catalog,omitzero"`
	AllowedModels     []string `json:"allowedModels"`
	UnavailableModels []string `json:"unavailableModels"`
	Choices           []struct {
		ID string `json:"id"`
	} `json:"choices"`
}

// commandPost sends a command with body (nil for none) under the contract token.
func commandPost(f *Fixture, path string, body []byte) Reply {
	return f.Do(Request{Method: "POST", Path: path, Token: ContractToken, Body: body})
}

// runFolders is each run's id and folder, the identity a command must never relocate.
func runFolders(f *Fixture) []string {
	var out []string
	for _, run := range f.Coordinator.ReadRuns() {
		out = append(out, run.ID+"|"+run.Folder())
	}
	return out
}

// stateOf decodes the state response a command answered.
func stateOf(t testing.TB, reply Reply) stateWire {
	t.Helper()
	var state stateWire
	if err := json.Unmarshal(reply.Body, &state); err != nil {
		t.Fatalf("state body %s: %v", reply.Body, err)
	}
	return state
}

// stateBody is the state resource's body as GET api/v1/state writes it, read from the coordinator.
func stateBody(t testing.TB, f *Fixture) []byte {
	t.Helper()
	body, err := routes.JSONEncode(api.NewStateResponse(f.Coordinator.ReadStateSnapshot(nil, nil)))
	if err != nil {
		t.Fatalf("state: %v", err)
	}
	return body
}

// jsonxModelRunner decodes a runner's models resource.
func jsonxModelRunner(t testing.TB, reply Reply) modelRunnerWire {
	t.Helper()
	var resource modelRunnerWire
	if err := json.Unmarshal(reply.Body, &resource); err != nil {
		t.Fatalf("models body %s: %v", reply.Body, err)
	}
	return resource
}

// sameLastCheck reports whether two lastCheck values are equal, nil included.
func sameLastCheck(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Equal(*b)
}

// requireStatus fails unless reply has status.
func requireStatus(t testing.TB, reply Reply, status int) {
	t.Helper()
	if reply.Status != status {
		t.Fatalf("status %d, want %d (body %s)", reply.Status, status, reply.Body)
	}
}

func TestPauseAndResumeReturnStateKeepHistoryAndNeverRelocateRuns(t *testing.T) {
	t.Parallel()
	f := Start(t, Options{})
	runsBefore := runFolders(f)
	paused := commandPost(f, "/api/v1/pause", []byte(`{"paused":true}`))
	if !stateOf(t, paused).GloballyPaused {
		t.Fatal("pause did not answer globallyPaused")
	}
	if pausedAt := f.Coordinator.ReadSettings().PausedAt; pausedAt == nil || !pausedAt.Equal(ContractNow) {
		t.Fatalf("pausedAt = %v, want %v", pausedAt, ContractNow)
	}

	// Repeating the same value changes neither the start nor history.
	history := f.Coordinator.ReadSettings().Pauses
	commandPost(f, "/api/v1/pause", []byte(`{"paused":true}`))
	if pausedAt := f.Coordinator.ReadSettings().PausedAt; pausedAt == nil || !pausedAt.Equal(ContractNow) {
		t.Fatalf("a repeated pause moved pausedAt to %v", pausedAt)
	}
	if got := f.Coordinator.ReadSettings().Pauses; !reflect.DeepEqual(got, history) {
		t.Fatal("a repeated pause changed the history")
	}

	resumed := commandPost(f, "/api/v1/pause", []byte(`{"paused":false}`))
	requireStatus(t, resumed, 200)
	if stateOf(t, resumed).GloballyPaused {
		t.Fatal("resume still answers globallyPaused")
	}
	if f.Coordinator.ReadSettings().Paused {
		t.Fatal("resume left the settings paused")
	}

	// The test does not tick: a tick would start a trigger poll that the test does not await.
	if got := runFolders(f); !slices.Equal(got, runsBefore) {
		t.Fatalf("runs changed: %v, want %v", got, runsBefore)
	}
}

func TestAfkCommandPersistsAndDisablesEvenWhenItsModelDisappears(t *testing.T) {
	t.Parallel()
	f := Start(t, Options{})
	// Pausing first makes the kicked tick a no-op, so the command response is deterministic while AFK still turns on.
	f.Coordinator.SetGloballyPaused(context.Background(), true)
	enabled := commandPost(f, "/api/v1/afk", []byte(`{"enabled":true}`))
	if !stateOf(t, enabled).Settings.AfkEnabled {
		t.Fatal("afk did not answer afkEnabled")
	}
	if !f.Coordinator.ReadSettings().Afk.Enabled {
		t.Fatal("afk is not enabled on the coordinator")
	}
	if !store.NewStorage(store.Paths{Root: f.Root}).LoadSettings().Afk.Enabled {
		t.Fatal("afk is not saved")
	}
	if label := globalActionLabel(f, "afk"); label != "GLOBAL AFK ON" {
		t.Fatalf("afk label %q, want GLOBAL AFK ON", label)
	}

	// Its model becomes unavailable; disabling must still work.
	if err := f.Coordinator.SetModelAllowed("opus-5.5-250k", core.BackendPi, false); err != nil {
		t.Fatalf("disallow: %v", err)
	}
	disabled := commandPost(f, "/api/v1/afk", []byte(`{"enabled":false}`))
	CheckGolden(t, "afk disabled", "command-afk-disabled.json", disabled, f.Root)
	if stateOf(t, disabled).Settings.AfkEnabled {
		t.Fatal("afk still answers enabled")
	}
	if f.Coordinator.ReadSettings().Afk.Enabled {
		t.Fatal("afk is still enabled on the coordinator")
	}
}

func TestAfkCommandEnablesAsTheAppDidAndRejectsUnknownFields(t *testing.T) {
	t.Parallel()
	f := Start(t, Options{})
	if f.Coordinator.ReadSettings().Afk.Enabled {
		t.Fatal("afk starts enabled")
	}
	// The app's switch turned AFK on without checking its model, so each decision reports a model it cannot start.
	empty := ""
	if _, err := f.Coordinator.PatchAfk(coordinator.AfkSettingsPatch{Model: &empty}); err != nil {
		t.Fatalf("blank model: %v", err)
	}
	commandPost(f, "/api/v1/afk", []byte(`{"enabled":false}`))
	enabled := commandPost(f, "/api/v1/afk", []byte(`{"enabled":true}`))
	requireStatus(t, enabled, 200)
	if !f.Coordinator.ReadSettings().Afk.Enabled {
		t.Fatal("afk did not turn on")
	}
	commandPost(f, "/api/v1/afk", []byte(`{"enabled":false}`))

	// Unknown enable field.
	before := f.Coordinator.ReadSettings()
	requireStatus(t, commandPost(f, "/api/v1/afk", []byte(`{"enabled":true,"extra":1}`)), 422)
	if f.Coordinator.ReadSettings().Afk.Enabled {
		t.Fatal("a rejected afk command turned AFK on")
	}
	if !reflect.DeepEqual(f.Coordinator.ReadSettings(), before) {
		t.Fatal("a rejected afk command changed the settings")
	}
}

func TestCheckNowForcesFetchReturnsStateAndRetainsPausedNoProgress(t *testing.T) {
	t.Parallel()
	f := Start(t, Options{})
	f.Coordinator.SetGloballyPaused(context.Background(), true)
	runsBefore := f.Coordinator.ReadRuns()
	lastCheck := f.Coordinator.LastCheck()
	pausedCheck := commandPost(f, "/api/v1/check-now", nil)
	CheckGolden(t, "check-now paused", "command-check-now-paused.json", pausedCheck, f.Root)
	if !stateOf(t, pausedCheck).GloballyPaused {
		t.Fatal("paused check-now answers not paused")
	}
	if !sameLastCheck(f.Coordinator.LastCheck(), lastCheck) {
		t.Fatal("a paused check-now changed lastCheck")
	}
	if !slices.Equal(runIDs(f.Coordinator.ReadRuns()), runIDs(runsBefore)) {
		t.Fatal("a paused check-now changed the runs")
	}

	f.Coordinator.SetGloballyPaused(context.Background(), false)
	f.Coordinator.Tick(context.Background())
	f.Coordinator.Idle()
	reply := commandPost(f, "/api/v1/check-now", []byte("{}"))
	if string(reply.Body) != string(stateBody(t, f)) {
		t.Fatalf("check-now answered a state the coordinator does not hold:\n%s\n%s", reply.Body, stateBody(t, f))
	}
}

func TestQuitAnswersThenAsksTheDaemonToStop(t *testing.T) {
	t.Parallel()
	f := Start(t, Options{})
	quitted := make(chan struct{}, 1)
	f.Coordinator.SetQuitHandler(func() { quitted <- struct{}{} })
	reply := commandPost(f, "/api/v1/quit", nil)
	_ = stateOf(t, reply)
	// The handler runs after a short delay so the answer is written before the listener closes.
	testkit.Eventually(t, quitWait, "the quit handler to run", func() bool {
		select {
		case <-quitted:
			return true
		default:
			return false
		}
	})
	f.Coordinator.SetQuitHandler(nil)
}

func TestModelRefreshAndAllowReturnRunnerResourcesAndPersist(t *testing.T) {
	t.Parallel()
	f := Start(t, Options{})
	refreshed := commandPost(f, "/api/v1/models/pi/refresh", nil)
	resource := jsonxModelRunner(t, refreshed)
	if resource.Runner != core.BackendPi || !resource.Usable {
		t.Fatalf("refresh answered runner %s usable %t", resource.Runner, resource.Usable)
	}
	if resource.Catalog == nil || !resource.Catalog.RefreshedAt.Equal(ContractNow) {
		t.Fatal("refresh does not use the coordinator clock")
	}
	saved := store.NewStorage(store.Paths{Root: f.Root}).LoadSettings()
	if catalog, ok := saved.ModelCatalogs["pi"]; !ok || !catalog.RefreshedAt.Equal(ContractNow) {
		t.Fatal("the refreshed catalog is not saved")
	}

	removed := commandPost(f, "/api/v1/models/pi/allow", []byte(`{"model":"fixture-model","allowed":false}`))
	after := jsonxModelRunner(t, removed)
	if slices.Contains(after.AllowedModels, "fixture-model") {
		t.Fatal("fixture-model is still allowed")
	}
	for _, choice := range after.Choices {
		if choice.ID == "fixture-model" {
			t.Fatal("fixture-model is still a choice")
		}
	}
	if _, ok := f.Coordinator.ReadSettings().AllowedModels["pi"]["fixture-model"]; ok {
		t.Fatal("fixture-model is still allowed on the coordinator")
	}

	prior := f.Coordinator.ReadSettings()
	for _, body := range []string{
		`{"model":"not-in-catalog","allowed":true}`, `{"model":"","allowed":true}`,
		`{"model":"fixture-model","allowed":"yes"}`, `{"model":"fixture-model"}`,
	} {
		requireStatus(t, commandPost(f, "/api/v1/models/pi/allow", []byte(body)), 422)
		if !reflect.DeepEqual(f.Coordinator.ReadSettings(), prior) {
			t.Fatalf("rejected allow %s changed the settings", body)
		}
	}
}

func TestCommandTransportRejectsWrongMethodsUnknownRunnersBodiesAndAuth(t *testing.T) {
	t.Parallel()
	f := Start(t, Options{})
	get := func(path string) Reply { return f.Do(Request{Method: "GET", Path: path, Token: ContractToken}) }
	post := func(path string, body []byte) Reply {
		return f.Do(Request{Method: "POST", Path: path, Token: ContractToken, Body: body})
	}
	requireStatus(t, get("/api/v1/pause"), 404)
	requireStatus(t, post("/api/v1/tools", nil), 404)
	requireStatus(t, get("/api/v1/models/nonesuch"), 404)
	requireStatus(t, post("/api/v1/models/nonesuch/refresh", nil), 404)

	for _, path := range []string{"/api/v1/pause", "/api/v1/afk", "/api/v1/models/pi/allow"} {
		if reply := f.Do(Request{Method: "POST", Path: path}); reply.Status != 401 {
			t.Fatalf("%s without a token: status %d, want 401", path, reply.Status)
		}
		if reply := f.Do(Request{Method: "POST", Path: path, Token: "wrong", Body: []byte("{}")}); reply.Status != 401 {
			t.Fatalf("%s with a wrong token: status %d, want 401", path, reply.Status)
		}
	}

	// The other bodies need their fields.
	for _, path := range []string{"/api/v1/pause", "/api/v1/afk", "/api/v1/models/pi/allow"} {
		requireStatus(t, post(path, nil), 422)
		requireStatus(t, post(path, []byte("not json")), 422)
		requireStatus(t, post(path, []byte("{}")), 422)
		requireStatus(t, post(path, []byte(`{"unknownField":true}`)), 422)
	}

	requireStatus(t, post("/api/v1/check-now", nil), 200)
	for _, tc := range []struct{ path, body string }{
		{"/api/v1/check-now", "not json"},
		{"/api/v1/check-now", `{"extra":true}`},
		{"/api/v1/models/pi/refresh", "not json"},
		{"/api/v1/models/pi/refresh", `{"extra":true}`},
	} {
		requireStatus(t, post(tc.path, []byte(tc.body)), 422)
	}
}

// globalActionLabel is the label of the global action with id.
func globalActionLabel(f *Fixture, id string) string {
	for _, action := range f.Coordinator.ReadGlobalActions() {
		if action.ID == id {
			return action.Label
		}
	}
	return ""
}
