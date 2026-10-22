// The body parsing,
// run and attempt lookups, and the success body the desktop commands share.
package routes

import (
	"bytes"
	"context"
	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
	"net/http"
	"path/filepath"
	"strings"

	"encoding/json"
	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/runner"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/shell"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/store"
	"github.com/robbertmanders/beltline/daemon/internal/kit/coordinator"
	"github.com/robbertmanders/beltline/daemon/internal/kit/launch"
	"github.com/robbertmanders/beltline/daemon/internal/server/api"
)

// desktopBody is a JSON object of
// the required and optional string keys only, else a 422.
func desktopBody(r *http.Request, required, optional []string) (map[string]string, error) {
	payload, err := readBodyLimited(r, desktopBodyLimit)
	if err != nil {
		return nil, err
	}
	malformed := core.NewFailure("Malformed request body or unsupported selectors.")
	members, err := jsonx.Members(payload)
	if err != nil {
		return nil, malformed
	}
	allowed := map[string]bool{}
	for _, key := range required {
		allowed[key] = true
	}
	for _, key := range optional {
		allowed[key] = true
	}
	for _, key := range required {
		if _, ok := members[key]; !ok {
			return nil, malformed
		}
	}
	out := make(map[string]string, len(members))
	for key, raw := range members {
		if !allowed[key] {
			return nil, malformed
		}
		text, ok := desktopJSONString(raw)
		if !ok {
			return nil, malformed
		}
		out[key] = text
	}
	return out, nil
}

// desktopJSONString is the value of a JSON string member, or false for any other JSON value.
func desktopJSONString(raw []byte) (string, bool) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '"' {
		return "", false
	}
	var text string
	if err := json.Unmarshal(trimmed, &text); err != nil {
		return "", false
	}
	return text, true
}

// desktopKeysAre reports whether the body has exactly the given keys.
func desktopKeysAre(body map[string]string, keys ...string) bool {
	if len(body) != len(keys) {
		return false
	}
	for _, key := range keys {
		if _, ok := body[key]; !ok {
			return false
		}
	}
	return true
}

// desktopRunCommand reads a body of runId and key only, and returns the run it
// names and the key's value; an unknown run is a 404.
func desktopRunCommand(c *coordinator.Coordinator, r *http.Request, key string) (core.Run, string, error) {
	payload, err := desktopBody(r, []string{"runId", key}, nil)
	if err != nil {
		return core.Run{}, "", err
	}
	run := c.ReadRun(payload["runId"])
	if run == nil {
		return core.Run{}, "", api.MissingResource{}
	}
	return *run, payload[key], nil
}

// desktopAttemptOf is the attempt with id on run.
func desktopAttemptOf(run core.Run, id string) (core.Attempt, bool) {
	for _, attempt := range run.Attempts {
		if attempt.ID == id {
			return attempt, true
		}
	}
	return core.Attempt{}, false
}

// desktopWorktreeTarget is the worktree target with id.
func desktopWorktreeTarget(targets []runner.WorktreeTarget, id string) (runner.WorktreeTarget, bool) {
	for _, target := range targets {
		if target.ID == id {
			return target, true
		}
	}
	return runner.WorktreeTarget{}, false
}

// desktopRevealTarget is the desktop reveal target:
// the path a reveal command opens (the run's folder, a worktree or an artifact).
func desktopRevealTarget(kind string, body map[string]string, run core.Run, c *coordinator.Coordinator) (string, error) {
	paths := c.Paths()
	switch kind {
	case "run":
		if !desktopKeysAre(body, "runId", "kind") {
			return "", core.NewFailure("The run selector does not accept other fields.")
		}
		return paths.RunFolder(run), nil
	case "attempt":
		if !desktopKeysAre(body, "runId", "kind", "attemptId") {
			return "", api.MissingResource{}
		}
		attempt, found := desktopAttemptOf(run, body["attemptId"])
		if !found {
			return "", api.MissingResource{}
		}
		return paths.AttemptFolder(run, attempt), nil
	case "output":
		id, ok := body["attemptId"]
		if !desktopKeysAre(body, "runId", "kind", "attemptId") || !ok {
			return "", core.NewFailure("The output selector requires attemptId.")
		}
		return desktopOutputTarget(id, run, c)
	case "artifact":
		id, hasID := body["attemptId"]
		name, hasName := body["name"]
		if !desktopKeysAre(body, "runId", "kind", "attemptId", "name") || !hasID || !hasName {
			return "", api.MissingResource{}
		}
		attempt, found := desktopAttemptOf(run, id)
		if !found {
			return "", api.MissingResource{}
		}
		if !desktopAwaitArtifactExists(c, run, attempt, name) {
			return "", api.MissingResource{}
		}
		target, ok := store.ArtifactsResolveConfined(name, paths.ArtifactsFolder(run, attempt), paths.Root)
		if !ok {
			return "", api.MissingResource{}
		}
		return target, nil
	case "worktree":
		id, ok := body["targetId"]
		if !desktopKeysAre(body, "runId", "kind", "targetId") || !ok {
			return "", core.NewFailure("The worktree selector requires targetId.")
		}
		target, found := desktopWorktreeTarget(runner.WorktreeTargetsFor(run, paths.WorkFolder(run)), id)
		if !found {
			return "", api.MissingResource{}
		}
		return target.Folder, nil
	case "afk":
		// An AFK decision's session files: its prompt, its decision and its log, in the run's afk folder.
		id, ok := body["targetId"]
		if !desktopKeysAre(body, "runId", "kind", "targetId") || !ok {
			return "", core.NewFailure("The AFK selector requires targetId.")
		}
		var decision *core.AfkDecision
		for _, candidate := range c.ReadAfkDecisions() {
			if candidate.ID == id && candidate.RunID == run.ID {
				found := candidate
				decision = &found
				break
			}
		}
		if decision == nil || decision.Folder == "" {
			return "", api.MissingResource{}
		}
		directory := true
		folder, ok := store.ArtifactsConfinedURL(filepath.Join(paths.RunFolder(run), decision.Folder), paths.Root, &directory)
		if !ok {
			return "", api.MissingResource{}
		}
		return folder, nil
	default:
		return "", core.NewFailure("Unsupported target selector.")
	}
}

// desktopOutputTarget is the desktop output target:
// the attempt's output file.
func desktopOutputTarget(attemptID string, run core.Run, c *coordinator.Coordinator) (string, error) {
	attempt, found := desktopAttemptOf(run, attemptID)
	if !found {
		return "", api.MissingResource{}
	}
	var stage *core.Stage
	for i := range run.Stages {
		if run.Stages[i].ID == attempt.StageID && run.Stages[i].Kind() == core.StageKindAgent {
			stage = &run.Stages[i]
			break
		}
	}
	if stage == nil || stage.Writes() == "" {
		return "", api.MissingResource{}
	}
	return filepath.Join(c.Paths().AttemptFolder(run, attempt), stage.Writes()), nil
}

// desktopAwaitArtifactExists is the desktop await artifact exists:
// the path checks only; the resolve walks the folder without following links.
func desktopAwaitArtifactExists(c *coordinator.Coordinator, run core.Run, attempt core.Attempt, name string) bool {
	return c.ConfinedArtifact(run.ID, attempt.ID, name) != nil
}

// desktopLauncher runs the platform's desktop commands with the shell runner.
type desktopLauncher struct {
	runner   shell.Runner
	platform launch.Platform
}

// launch runs command and answers `{"ok":true}`; a command that exited non-zero
// is a 422 naming what failed.
func (d desktopLauncher) launch(w http.ResponseWriter, r *http.Request, command launch.DesktopCommand) error {
	result := d.runner.Run(r.Context(), command.Executable, command.Arguments, "", nil)
	if !result.OK() {
		message := strings.TrimSpace(result.Message())
		if message == "" {
			message = "The desktop launch request failed."
		}
		return core.NewFailure(message)
	}
	JSONResponse(w, DesktopSuccess{OK: true}, http.StatusOK)
	return nil
}

// desktopSessionAvailable is the desktop session available.
func desktopSessionAvailable(ctx context.Context, name string, c *coordinator.Coordinator, lookup DesktopSessionLookup) bool {
	if lookup != nil {
		return lookup(ctx, name)
	}
	return c.DesktopSessionAvailable(ctx, name)
}

// DesktopSuccess is the body of a desktop command that succeeded: `{"ok":true}`
// .
type DesktopSuccess struct {
	OK bool `json:"ok"`
}
