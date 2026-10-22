package runner

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/shell"
)

// ModelCatalogDiscovery lists the models a runner's own catalog offers. It is
// separate from AgentRunner: a catalog request cannot allocate a work session or
// send a prompt.
type ModelCatalogDiscovery interface {
	// Discover lists the models of the executable for runner.
	Discover(ctx context.Context, runner core.Backend, executable string) ([]core.ModelChoice, error)
}

// CLIModelCatalog discovers Pi's and Cursor's models by running their listing
// commands, and sends every other runner to the server transport.
type CLIModelCatalog struct {
	shell  shell.Runner
	server ModelCatalogDiscovery
}

var _ ModelCatalogDiscovery = CLIModelCatalog{}

// NewCLIModelCatalog is CLIModelCatalog(shell:server:). Nil arguments use
// CatalogProcessRunner and ServerModelCatalog.
func NewCLIModelCatalog(sh shell.Runner, server ModelCatalogDiscovery) CLIModelCatalog {
	if sh == nil {
		sh = CatalogProcessRunner{}
	}
	if server == nil {
		server = ServerModelCatalog{}
	}
	return CLIModelCatalog{shell: sh, server: server}
}

// Discover runs `pi --list-models` or `cursor-agent models` and parses it. A
// failing command is reported without its output, which can hold account details.
func (c CLIModelCatalog) Discover(ctx context.Context, runner core.Backend, executable string) ([]core.ModelChoice, error) {
	if runner != core.BackendPi && runner != core.BackendCursor {
		return c.server.Discover(ctx, runner, executable)
	}
	args := []string{"models"}
	if runner == core.BackendPi {
		args = []string{"--list-models"}
	}
	result := c.shell.Run(ctx, executable, args, "", nil)
	if !result.OK() {
		// Do not expose command output: it can contain account/credential details.
		return nil, core.NewFailure(fmt.Sprintf("Cannot discover %s models (exit %d). Check the configured executable, update %s and verify its sign-in in your terminal, then Retry.", runner.Label(), result.Status, runner.Label()))
	}
	if runner == core.BackendPi {
		return CLIModelCatalogParsePi(result.Stdout)
	}
	return CLIModelCatalogParseCursor(result.Stdout)
}

// CLIModelCatalogParseCursor parses `cursor-agent models` output: one choice per
// model and speed, whose thinking levels become variants.
func CLIModelCatalogParseCursor(text string) ([]core.ModelChoice, error) {
	var lines []string
	for _, line := range strings.FieldsFunc(text, core.IsNewline) {
		if trimWhitespaces(line) != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) == 0 || lines[0] != "Available models" {
		return nil, core.NewFailure("Unsupported Cursor catalog response; update Cursor and Retry")
	}
	var entries []cursorCatalogEntry
	for _, line := range lines[1:] {
		if strings.HasPrefix(line, "Tip: use --model ") {
			continue
		}
		separator := strings.Index(line, " - ")
		if separator < 0 {
			return nil, core.NewFailure("Malformed Cursor catalog response")
		}
		id := line[:separator]
		name := line[separator+len(" - "):]
		if id == "" || name == "" || strings.IndexFunc(id, unicode.IsSpace) >= 0 {
			return nil, core.NewFailure("Incomplete Cursor model entry")
		}
		entries = append(entries, cursorCatalogEntry{ID: id, Name: name})
	}
	return cursorCatalogGroup(entries)
}

// CLIModelCatalogParsePi parses `pi --list-models` output. A model listed by two
// providers is named with its provider; the context column is its one size.
func CLIModelCatalogParsePi(text string) ([]core.ModelChoice, error) {
	var lines [][]string
	for _, segment := range strings.FieldsFunc(text, core.IsNewline) {
		lines = append(lines, strings.FieldsFunc(segment, unicode.IsSpace))
	}
	header := []string{"provider", "model", "context", "max-out", "thinking", "images"}
	if len(lines) == 0 || !slices.Equal(lines[0], header) {
		return nil, core.NewFailure("Pi returned an unsupported or incomplete catalog. Update Pi and Retry.")
	}
	type piRow struct {
		provider string
		model    string
		context  *int
	}
	var rows []piRow
	for _, row := range lines[1:] {
		if len(row) != 6 || !isYesNo(row[4]) || !isYesNo(row[5]) {
			return nil, core.NewFailure("Pi returned a malformed catalog. The last successful catalog was not replaced. Update Pi and Retry.")
		}
		// The context column is this model's one reported size. max-out is not context,
		// and a capacity never becomes a second selectable id. A nonnumeric cell
		// such as "unknown" is an unspecified size, not a failed refresh.
		context, err := cliModelCatalogPiContext(row[2])
		if err != nil {
			return nil, err
		}
		rows = append(rows, piRow{provider: row[0], model: row[1], context: context})
	}
	// One name per model: the model alone, with its provider only where two providers share it.
	counts := map[string]int{}
	for _, row := range rows {
		counts[row.model]++
	}
	models := make([]core.ModelChoice, 0, len(rows))
	for _, row := range rows {
		id := row.provider + "/" + row.model
		name := row.model
		if counts[row.model] > 1 {
			name = id
		}
		models = append(models, core.ModelChoice{ID: id, Name: name, ContextTokens: row.context})
	}
	return models, nil
}

// cliModelCatalogPiContext is the reported input-context tokens of a Pi context
// cell, or nil when the cell names no size. A numeric cell that is not a positive
// exact token count fails discovery.
func cliModelCatalogPiContext(cell string) (*int, error) {
	if tokens := core.ContextSizeParse(cell, true); tokens != nil {
		return tokens, nil
	}
	if !strings.ContainsFunc(cell, unicode.IsNumber) {
		return nil, nil
	}
	return nil, core.NewFailure("Pi returned an invalid context capacity. The last successful catalog was not replaced. Update Pi and Retry.")
}

// catalogRunTimeout is the completion bound of a catalog command.
const catalogRunTimeout = 29 * time.Second

// CatalogProcessRunner runs a catalog command with a hard completion bound (29 s),
// in an isolated temporary folder and with its output in a file. This does not
// change ordinary backend execution or use tmux or workspaces. It is a
// shell.Runner, so it uses os/exec directly here, as the server catalog does.
type CatalogProcessRunner struct{}

var _ shell.Runner = CatalogProcessRunner{}

// Run runs the executable through /usr/bin/env, with the input on stdin. Status
// 127 when it cannot start or its folders cannot be made, 124 when it timed out.
func (CatalogProcessRunner) Run(ctx context.Context, executable string, args []string, dir string, input *string) shell.Result {
	// The command gets its own folder and an empty stdin: the input
	// is not passed on, so it is not used here either.
	isolated := filepath.Join(honoredTemporaryDirectory(), "beltline-catalog-"+catalogFolderID())
	if err := os.MkdirAll(isolated, 0o755); err != nil {
		return shell.Result{Status: 127}
	}
	defer func() { _ = os.RemoveAll(isolated) }() // best-effort cleanup of the folder this command alone used
	cwd := dir
	if cwd == "" {
		cwd = isolated
	}
	runCtx, cancel := context.WithTimeout(ctx, catalogRunTimeout)
	defer cancel()
	result := shell.ProcessRunner{}.Run(runCtx, executable, args, cwd, nil)
	if runCtx.Err() == context.DeadlineExceeded {
		return shell.Result{Status: 124}
	}
	// Stderr is not captured: the catalog run reads stdout only.
	return shell.Result{Status: result.Status, Stdout: result.Stdout}
}

// honoredTemporaryDirectory is TMPDIR when it is set and not empty, else the
// user's temporary folder, so a test run can keep these folders out of the real one.
func honoredTemporaryDirectory() string {
	if raw := os.Getenv("TMPDIR"); raw != "" {
		return raw
	}
	return os.TempDir()
}

// catalogFolderID is an upper-case version 4 UUID for a catalog folder or output
// name.
func catalogFolderID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand does not fail on supported platforms; the clock keeps names apart.
		return fmt.Sprintf("%032X", time.Now().UnixNano())
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	h := strings.ToUpper(hex.EncodeToString(b[:]))
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}

// FixtureModelCatalog lists the fixture models of demo mode and snapshots. Its
// ids are fixture choices, not capacities inferred for an installed runner.
type FixtureModelCatalog struct{}

var _ ModelCatalogDiscovery = FixtureModelCatalog{}

// FixtureModelCatalogModels are the demo's fixture models.
var FixtureModelCatalogModels = []core.ModelChoice{
	{ID: "fixture-model", Name: "Fixture model (demo)"},
	{ID: "openrouter/stealth/space-bunny-alpha", Name: "Fixture Pi model (demo)"},
	{ID: "opus-5.5-250k", Name: "Opus 5.5", ContextTokens: new(250000)},
	{ID: "opus-5.5-1m", Name: "Opus 5.5", ContextTokens: new(1000000)},
}

// Discover returns FixtureModelCatalogModels whatever the runner and executable.
func (FixtureModelCatalog) Discover(ctx context.Context, runner core.Backend, executable string) ([]core.ModelChoice, error) {
	models := make([]core.ModelChoice, 0, len(FixtureModelCatalogModels))
	for _, model := range FixtureModelCatalogModels {
		if model.ContextTokens != nil {
			model.ContextTokens = new(*model.ContextTokens)
		}
		models = append(models, model)
	}
	return models, nil
}

// isYesNo is whether a cell is "yes" or "no".
func isYesNo(cell string) bool { return cell == "yes" || cell == "no" }
