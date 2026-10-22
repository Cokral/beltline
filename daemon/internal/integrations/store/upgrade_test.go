package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/robbertmanders/beltline/daemon/internal/core"
)

// upgradeOldBelt is a belt version as it was saved with one trigger, the
// afkEnabled switch and a build stage from before agents had their own files.
const upgradeOldBelt = `{"id":"epics","name":"Epics","version":1,"afkEnabled":true,` +
	`"trigger":{"kind":"jira","jql":"project = PAY","poll":30,"repos":["acme/billing"]},` +
	`"stages":[{"id":"build","type":"build","name":"Build","outcomes":[]}]}`

// upgradeOldFolder is a data folder an older version saved: settings with a
// factory repository, the old belt with a state that only knows lastFired,
// and the golden run as it was saved before sources, agent copies and stage keys.
func upgradeOldFolder(t *testing.T) (Storage, string) {
	t.Helper()
	s := seededStorage(t)
	writeText(t, s.Paths.Settings(), `{"login":"me","factoryRepository":"owner/factories"}`)
	writeText(t, filepath.Join(s.Paths.Belts(), "epics", "v1.json"), upgradeOldBelt)
	writeText(t, filepath.Join(s.Paths.Belts(), "epics", "state.json"),
		`{"paused":false,"archived":false,"lastFired":"2026-01-01T00:00:00Z","watchOutcomes":true,"reviewOutcomes":true}`)
	run := goldenRun(t)
	var fields map[string]any
	data, err := json.Marshal(run)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	delete(fields, "references")
	delete(fields, "source")
	fields["agents"] = []any{}
	for _, stage := range fields["stages"].([]any) {
		delete(stage.(map[string]any), "key")
	}
	path := filepath.Join(s.Paths.RunFolder(run), "run.json")
	old, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	writeText(t, path, string(old))
	return s, path
}

// upgradeFields decodes a file into its top-level keys.
func upgradeFields(t *testing.T, path string) map[string]json.RawMessage {
	t.Helper()
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(fileText(t, path)), &fields); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return fields
}

// TestUpgradeRewritesAnOldFolderOnce checks every old file is written in the
// current format, its original kept in the backup, and that a folder already
// upgraded is left alone.
func TestUpgradeRewritesAnOldFolderOnce(t *testing.T) {
	t.Parallel()
	s, runPath := upgradeOldFolder(t)
	beltPath := filepath.Join(s.Paths.Belts(), "epics", "v1.json")
	originalRun := fileText(t, runPath)
	report, err := s.Upgrade(time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if !report.Ran || !report.Settings || report.Belts != 1 || report.States != 1 || report.Runs != 1 || len(report.Unreadable) != 0 {
		t.Fatalf("report %+v", report)
	}
	if want := filepath.Join(s.Paths.UpgradeBackups(), "20261016T090000Z"); report.Backup != want {
		t.Fatalf("backup %q, want %q", report.Backup, want)
	}
	if fileText(t, filepath.Join(report.Backup, "belts", "epics", "v1.json")) != upgradeOldBelt ||
		fileText(t, filepath.Join(report.Backup, "runs", s.Paths.RunFolder(goldenRun(t))[len(s.Paths.Runs())+1:], "run.json")) != originalRun {
		t.Fatal("the backup does not hold the files as they were")
	}

	belt := upgradeFields(t, beltPath)
	if _, ok := belt["trigger"]; ok || string(belt["afk"]) != `"always"` {
		t.Fatalf("belt %s", fileText(t, beltPath))
	}
	build, _ := s.AgentRef(core.AgentRef{ID: "build", Version: 1})
	var stored core.Belt
	if err := json.Unmarshal([]byte(fileText(t, beltPath)), &stored); err != nil {
		t.Fatal(err)
	}
	if len(stored.Triggers) != 1 || stored.Triggers[0].Repos[0] != "acme/billing" || stored.Stages[0].Type != core.StageTypeAgent ||
		core.ValueOf(stored.Stages[0].Key) != build.Key {
		t.Fatalf("belt %+v", stored)
	}

	state, ok := s.StoredState("epics")
	if fired, has := state.Schedules["1"]; !ok || len(state.Schedules) != 1 || !has || !fired.Equal(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("state %s", fileText(t, filepath.Join(s.Paths.Belts(), "epics", "state.json")))
	}
	settings := upgradeFields(t, s.Paths.Settings())
	if _, ok := settings["factoryRepository"]; ok || len(s.LoadSettings().LibraryRepositories) != 1 {
		t.Fatalf("settings %s", fileText(t, s.Paths.Settings()))
	}

	var run core.Run
	if err := json.Unmarshal([]byte(fileText(t, runPath)), &run); err != nil {
		t.Fatal(err)
	}
	if !run.UsesStageKeys() || run.Source == nil || run.Source.Entry != "1" || len(run.Agents) == 0 {
		t.Fatalf("run references %v source %+v agents %d", run.References, run.Source, len(run.Agents))
	}
	if key := core.StageKeysChosen(run.Stages[0]); key != "plan" {
		t.Errorf("plan-epic key %q, want plan", key)
	}
	for _, stage := range run.Stages {
		if stage.Kind() == core.StageKindAgent && core.StageFile(stage.Key, stage.Type) != stage.Writes() {
			t.Errorf("%s: key %v moved its file to %s", stage.ID, stage.Key, stage.Writes())
		}
	}
	if format := fileText(t, s.Paths.Format()); format == "" {
		t.Fatal("no format.json")
	}

	again, err := s.Upgrade(time.Now())
	if err != nil || again.Ran {
		t.Fatalf("second upgrade %+v %v", again, err)
	}
	if err := os.Remove(s.Paths.Format()); err != nil {
		t.Fatal(err)
	}
	third, err := s.Upgrade(time.Now())
	if err != nil || !third.Ran || third.Settings || third.Belts+third.States+third.Runs != 0 || third.Backup != "" {
		t.Fatalf("an upgraded folder changed again: %+v %v", third, err)
	}
}

// TestUpgradeLeavesAnUnreadableFileAsItIs checks a file that does not decode
// is reported and kept, and the upgrade still finishes.
func TestUpgradeLeavesAnUnreadableFileAsItIs(t *testing.T) {
	t.Parallel()
	s := newTestStorage(t)
	broken := filepath.Join(s.Paths.Belts(), "broken", "v1.json")
	writeText(t, broken, `{"id":`)
	report, err := s.Upgrade(time.Now())
	if err != nil || !report.Ran || len(report.Unreadable) != 1 || report.Unreadable[0] != filepath.Join("belts", "broken", "v1.json") {
		t.Fatalf("report %+v %v", report, err)
	}
	if fileText(t, broken) != `{"id":` {
		t.Fatal("the unreadable file was changed")
	}
}

// TestUpgradeRefusesAFolderInANewerFormat checks a folder a newer version saved
// is refused and left exactly as it is.
func TestUpgradeRefusesAFolderInANewerFormat(t *testing.T) {
	t.Parallel()
	s, path := upgradeOldFolder(t)
	writeText(t, s.Paths.Format(), `{"version":`+strconv.Itoa(core.UpgradeFormatVersion+1)+`}`)
	before := fileText(t, path)
	report, err := s.Upgrade(time.Now())
	var newer NewerFormat
	if !errors.As(err, &newer) || newer.Found != core.UpgradeFormatVersion+1 || newer.Current != core.UpgradeFormatVersion || report.Ran {
		t.Fatalf("report %+v %v, want a NewerFormat refusal", report, err)
	}
	if fileText(t, path) != before {
		t.Fatal("a refused upgrade changed a run")
	}
	if _, err := os.Stat(s.Paths.UpgradeBackups()); err == nil {
		t.Fatal("a refused upgrade made a backup")
	}
}
