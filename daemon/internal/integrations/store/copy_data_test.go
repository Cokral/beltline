package store

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"encoding/json"
	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// TestCopyOfADataFolderRoundTrips loads every file of a COPY of a
// data folder written by the app, decodes it, encodes it again with MarshalStorage
// and compares the bytes. It is skipped unless BELTLINE_GO_DATA_COPY names the
// copy. The real data folder is never opened by this test.
func TestCopyOfADataFolderRoundTrips(t *testing.T) {
	t.Parallel()
	dir := os.Getenv("BELTLINE_GO_DATA_COPY")
	if dir == "" {
		t.Skip("BELTLINE_GO_DATA_COPY is not set")
	}
	checked, skipped := 0, 0
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		got, known, err := roundTripFile(filepath.ToSlash(rel), data)
		if !known {
			skipped++
			return nil
		}
		checked++
		if err != nil {
			t.Errorf("%s: %v", rel, err)
			return nil
		}
		if string(got) != string(data) {
			t.Errorf("%s: %s", rel, firstDifference(data, got))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}
	t.Logf("round-tripped %d files, skipped %d", checked, skipped)
}

// roundTripFile decodes one data file by its place in the folder and encodes it
// again. known is false for files this test does not model.
func roundTripFile(rel string, data []byte) (out []byte, known bool, err error) {
	parts := strings.Split(rel, "/")
	storage := func(value any) ([]byte, bool, error) {
		encoded, err := jsonx.MarshalStorage(value)
		return encoded, true, err
	}
	switch {
	case rel == "settings.json":
		var settings Settings
		if err := json.Unmarshal(data, &settings); err != nil {
			return nil, true, err
		}
		return storage(settings)
	case len(parts) == 3 && parts[0] == "belts" && strings.HasPrefix(parts[2], "v") && strings.HasSuffix(parts[2], ".json"):
		var belt core.Belt
		if err := json.Unmarshal(data, &belt); err != nil {
			return nil, true, err
		}
		return storage(belt)
	case len(parts) == 3 && parts[0] == "belts" && parts[2] == "state.json":
		var state beltState
		if err := json.Unmarshal(data, &state); err != nil {
			return nil, true, err
		}
		return storage(state)
	case len(parts) == 3 && parts[0] == "agents" && strings.HasPrefix(parts[2], "v") && strings.HasSuffix(parts[2], ".md"):
		version, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(parts[2], "v"), ".md"))
		if err != nil {
			return nil, false, nil
		}
		agent, err := core.AgentFileParse(string(data), parts[1], version)
		if err != nil {
			return nil, true, err
		}
		return []byte(core.AgentFileWrite(agent)), true, nil
	case len(parts) == 3 && parts[0] == "agents" && parts[2] == "state.json":
		var state agentState
		if err := json.Unmarshal(data, &state); err != nil {
			return nil, true, err
		}
		return storage(state)
	case parts[0] == "runs" && parts[len(parts)-1] == "run.json":
		var run core.Run
		if err := json.Unmarshal(data, &run); err != nil {
			return nil, true, err
		}
		return storage(run)
	case len(parts) == 2 && parts[0] == "erasures" && parts[1] == "generations.json":
		var erased map[string]int
		if err := json.Unmarshal(data, &erased); err != nil {
			return nil, true, err
		}
		return storage(erased)
	case len(parts) == 2 && parts[0] == "erasures":
		var pending PendingErasure
		if err := json.Unmarshal(data, &pending); err != nil {
			return nil, true, err
		}
		return storage(pending)
	case rel == "afk/decisions.json":
		var decisions []core.AfkDecision
		if err := json.Unmarshal(data, &decisions); err != nil {
			return nil, true, err
		}
		return storage(decisions)
	}
	return nil, false, nil
}

// firstDifference describes where two byte strings first differ, with context.
func firstDifference(want, got []byte) string {
	n := min(len(want), len(got))
	at := 0
	for at < n && want[at] == got[at] {
		at++
	}
	return fmt.Sprintf("first difference at byte %d (lengths %d and %d): want %q, got %q",
		at, len(want), len(got), context(want, at), context(got, at))
}

// context is up to 40 bytes around position at.
func context(b []byte, at int) string {
	start := max(at-20, 0)
	end := min(at+20, len(b))
	return string(b[start:end])
}
