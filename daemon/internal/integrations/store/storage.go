// Storage, WriteFault,
// the runs and AFK decisions. Belts are in storage_belts.go and settings in
// settings.go.

package store

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"encoding/json"
	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// Storage reads and writes belts, runs and settings as JSON. Every write is
// atomic. Storage values share their WriteFault, as a pointer would be shared
// between copies of the struct.
type Storage struct {
	Paths      Paths
	WriteFault *WriteFault
}

// NewStorage returns a Storage over paths with a write fault that never fires.
func NewStorage(paths Paths) Storage {
	return Storage{Paths: paths, WriteFault: &WriteFault{}}
}

// WriteFault makes one write fail. Tests arm it with the name of the next
// file a write should fail on, such as state.json or v2.json.
type WriteFault struct {
	mu       sync.Mutex
	fileName string
}

// Arm makes the next write of a file named name fail once.
func (f *WriteFault) Arm(name string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fileName = name
}

// take reports whether the armed fault names this file, and disarms it.
func (f *WriteFault) take(name string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fileName != "" && f.fileName == name {
		f.fileName = ""
		return true
	}
	return false
}

// Write saves value as storage JSON at path, creating its folder, atomically.
func (s Storage) Write(value any, path string) error {
	if s.WriteFault != nil && s.WriteFault.take(filepath.Base(path)) {
		return &fs.PathError{Op: "write", Path: path, Err: fs.ErrPermission}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o777); err != nil {
		return err
	}
	data, err := jsonx.MarshalStorage(value)
	if err != nil {
		return err
	}
	return WriteFileAtomic(path, data)
}

// Read decodes the storage JSON at path into v.
func (s Storage) Read(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

// WriteFileAtomic writes data to a temporary file beside path and renames it
// over path, so readers see the old file or the new one. The temporary file is
// created with the default mode, as Foundation's atomic write leaves it.
func WriteFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	temp := filepath.Join(dir, "."+filepath.Base(path)+"."+core.NewUUID()+".tmp")
	file, err := os.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o666)
	if err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		_ = os.Remove(temp)
		return err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(temp)
		return err
	}
	if err := os.Rename(temp, path); err != nil {
		_ = os.Remove(temp)
		return err
	}
	return nil
}

// LoadRuns reads every run.json below runs/. Files that do not decode are
// skipped. The walk is in lexical order, so the result is deterministic.
func (s Storage) LoadRuns() []core.Run {
	var paths []string
	_ = filepath.WalkDir(s.Paths.Runs(), func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Name() != "run.json" {
			return nil
		}
		paths = append(paths, path)
		return nil
	})
	// Reading and decoding a run is most of a start, and each is independent of
	// the others, so they are spread over the processors; the runs keep the
	// walk's order, and an unreadable one is left out as before.
	loaded := make([]core.Run, len(paths))
	ok := make([]bool, len(paths))
	next := make(chan int)
	var workers sync.WaitGroup
	for range min(runtime.GOMAXPROCS(0), len(paths)) {
		workers.Go(func() {
			for i := range next {
				ok[i] = s.Read(paths[i], &loaded[i]) == nil
			}
		})
	}
	for i := range paths {
		next <- i
	}
	close(next)
	workers.Wait()
	runs := make([]core.Run, 0, len(paths))
	for i := range loaded {
		if ok[i] {
			runs = append(runs, loaded[i])
		}
	}
	return runs
}

// SaveRun writes a run to its folder. A run whose erasure is pending, or
// whose generation was erased, is refused.
func (s Storage) SaveRun(run core.Run) error {
	for _, pending := range s.LoadPendingErasures() {
		if pending.ID == run.ID && pending.Generation >= run.Generation {
			return Failure{Message: "This run is being erased and was not saved. Try again once erasure finishes."}
		}
	}
	if erased, ok := s.loadErasedGenerations()[run.ID]; ok && run.Generation <= erased {
		return Failure{Message: "This run was erased and was not saved."}
	}
	return s.Write(run, filepath.Join(s.Paths.RunFolder(run), "run.json"))
}

// LoadAfkDecisions reads afk/decisions.json. A missing or unreadable file is
// an empty history.
func (s Storage) LoadAfkDecisions() []core.AfkDecision {
	var decisions []core.AfkDecision
	if err := s.Read(s.Paths.AfkDecisions(), &decisions); err != nil || decisions == nil {
		return []core.AfkDecision{}
	}
	return decisions
}

// SaveAfkDecisions writes the whole decision history.
func (s Storage) SaveAfkDecisions(decisions []core.AfkDecision) error {
	if decisions == nil {
		decisions = []core.AfkDecision{}
	}
	return s.Write(decisions, s.Paths.AfkDecisions())
}
