package store

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// PendingErasure is an erasure that must finish before the same run identity
// can be used again. It lives outside the run tree, so deleting the run folder
// does not delete the intent, and loading runs does not treat it as one.
type PendingErasure struct {
	ID         string   `json:"id"`
	Generation int      `json:"generation"`
	Folder     string   `json:"folder"`
	Repos      []string `json:"repos"`
	// Sessions are the session names snapshotted when erasure began, including
	// continued and AFK sessions.
	Sessions         []string `json:"sessions"`
	StoppedSessions  bool     `json:"stoppedSessions"`
	RemovedWorkspace bool     `json:"removedWorkspace"`
	RemovedDecisions bool     `json:"removedDecisions"`
	RemovedRun       bool     `json:"removedRun"`
}

// erasureFile is the intent's file name: its id in base64, with "/" and "=" replaced.
func (s Storage) erasureFile(id string) string {
	name := strings.NewReplacer("/", "_", "=", "-").Replace(base64.StdEncoding.EncodeToString([]byte(id)))
	return filepath.Join(s.Paths.Erasures(), name+".json")
}

// LoadPendingErasures reads every intent, skipping files that do not decode
// and generations.json.
func (s Storage) LoadPendingErasures() []PendingErasure {
	entries, err := os.ReadDir(s.Paths.Erasures())
	if err != nil {
		return []PendingErasure{}
	}
	out := []PendingErasure{}
	for _, entry := range entries {
		name := entry.Name()
		if filepath.Ext(name) != ".json" || name == "generations.json" {
			continue
		}
		var pending PendingErasure
		if s.Read(filepath.Join(s.Paths.Erasures(), name), &pending) == nil {
			out = append(out, pending)
		}
	}
	return out
}

// PendingErasure is the intent for id, and whether one exists and decodes.
func (s Storage) PendingErasure(id string) (PendingErasure, bool) {
	var pending PendingErasure
	if s.Read(s.erasureFile(id), &pending) != nil {
		return PendingErasure{}, false
	}
	return pending, true
}

// SavePendingErasure writes an intent.
func (s Storage) SavePendingErasure(pending PendingErasure) error {
	return s.Write(pending, s.erasureFile(pending.ID))
}

// DeletePendingErasure removes an intent. A missing intent is already
// finished.
func (s Storage) DeletePendingErasure(id string) error {
	path := s.erasureFile(id)
	if _, err := os.Stat(path); err != nil {
		return nil
	}
	if err := os.Remove(path); err != nil {
		if _, statErr := os.Stat(path); statErr == nil {
			return Failure{Message: fmt.Sprintf("Could not record that erasure finished: %s. Try erasing the run again.", err)}
		}
	}
	return nil
}

// NextGeneration is the generation a new run with id must use: one past the
// last erased, or 0.
func (s Storage) NextGeneration(id string) int {
	last, ok := s.loadErasedGenerations()[id]
	if !ok {
		last = -1
	}
	return last + 1
}

// RecordErased notes that the run id reached generation, keeping the highest.
func (s Storage) RecordErased(id string, generation int) error {
	erased := s.loadErasedGenerations()
	if current, ok := erased[id]; !ok || generation > current {
		erased[id] = generation
	}
	return s.Write(erased, filepath.Join(s.Paths.Erasures(), "generations.json"))
}

// loadErasedGenerations is the generation each erased run reached. A missing
// or unreadable file is empty. Never nil.
func (s Storage) loadErasedGenerations() map[string]int {
	var erased map[string]int
	if s.Read(filepath.Join(s.Paths.Erasures(), "generations.json"), &erased) != nil || erased == nil {
		return map[string]int{}
	}
	return erased
}

// DeleteRunFolder deletes a run's saved folder under runs/. A folder that is
// already gone is success.
func (s Storage) DeleteRunFolder(folder string) error {
	path := filepath.Join(s.Paths.Runs(), folder)
	if _, err := os.Stat(path); err != nil {
		return nil
	}
	if err := os.RemoveAll(path); err != nil {
		if _, statErr := os.Stat(path); statErr == nil {
			return Failure{Message: fmt.Sprintf("Could not delete the run's saved folder: %s. The run is still here; try erasing it again.", err)}
		}
	}
	return nil
}
