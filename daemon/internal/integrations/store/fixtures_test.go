package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/testkit"
)

// readGolden reads a server golden from daemon/testdata/golden.
func readGolden(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(testkit.Testdata(t, "golden", name))
	if err != nil {
		t.Fatalf("read golden %s: %v", name, err)
	}
	return data
}

// goldenBelt is the belt the belt-save golden carries as its draft, which the server
// writes as a stored belt.
func goldenBelt(t *testing.T) core.Belt {
	t.Helper()
	var wrap struct {
		Resource struct {
			Belt json.RawMessage `json:"draft"`
		} `json:"resource"`
	}
	if err := json.Unmarshal(readGolden(t, "belt-save.json"), &wrap); err != nil {
		t.Fatalf("belt-save golden: %v", err)
	}
	var belt core.Belt
	if err := json.Unmarshal(wrap.Resource.Belt, &belt); err != nil {
		t.Fatalf("belt from golden: %v", err)
	}
	return belt
}

// goldenRun is the run the run-abort golden carries.
func goldenRun(t *testing.T) core.Run {
	t.Helper()
	var wrap struct {
		Run json.RawMessage `json:"run"`
	}
	if err := json.Unmarshal(readGolden(t, "run-abort.json"), &wrap); err != nil {
		t.Fatalf("run-abort golden: %v", err)
	}
	var run core.Run
	if err := json.Unmarshal(wrap.Run, &run); err != nil {
		t.Fatalf("run from golden: %v", err)
	}
	return run
}

// goldenSettings is settings.json as the server golden holds it.
func goldenSettings(t *testing.T) Settings {
	t.Helper()
	var settings Settings
	if err := json.Unmarshal(readGolden(t, "settings.json"), &settings); err != nil {
		t.Fatalf("settings golden: %v", err)
	}
	return settings
}

// newTestStorage is a Storage over a folder of its own, removed with the test.
func newTestStorage(t *testing.T) Storage {
	t.Helper()
	return NewStorage(Paths{Root: t.TempDir()})
}

// seededStorage is a library with the built-in agents, as a first start of a demo has it.
func seededStorage(t *testing.T) Storage {
	t.Helper()
	s := newTestStorage(t)
	s.SeedAgents()
	return s
}

// noEnv is an environment with no variables set.
func noEnv(string) string { return "" }

// envOf is an environment with the given variables.
func envOf(vars map[string]string) func(string) string {
	return func(key string) string { return vars[key] }
}

// mustDecode decodes a JSON literal into a fresh value of T.
func mustDecode[T any](t *testing.T, text string) T {
	t.Helper()
	var value T
	if err := json.Unmarshal([]byte(text), &value); err != nil {
		t.Fatalf("decode %s: %v", text, err)
	}
	return value
}

// mustTime parses an RFC 3339 time, failing the test otherwise.
func mustTime(t *testing.T, text string) time.Time {
	t.Helper()
	when, err := time.Parse(time.RFC3339, text)
	if err != nil {
		t.Fatalf("time %s: %v", text, err)
	}
	return when
}

// fileText reads a file as a string, failing the test on error.
func fileText(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

// writeText writes text to path, creating its folder.
func writeText(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o777); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
