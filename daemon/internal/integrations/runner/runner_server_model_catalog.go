package runner

import (
	"context"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/robbertmanders/beltline/daemon/internal/core"
)

// ServerModelCatalog reads the models through the read-only catalog transports
// the official SDKs use. No session/create, thread/start, user message, prompt or
// credential request is sent. Protocol references: github/copilot-sdk
// nodejs/src/client.ts (models.list, StreamMessageReader); Claude Agent SDK
// supportedModels (initialize control response); openai/codex
// codex-rs/app-server (initialize, model/list). Using their wire interfaces
// avoids adding an SDK/runtime package dependency. Unsupported installed versions
// fail with upgrade guidance, never guessed IDs.
//
// The channel is a long-lived process with two pipes, which the shell.Runner
// contract cannot express, so runner_server_model_catalog_channel.go uses os/exec
// directly.
type ServerModelCatalog struct{}

var _ ModelCatalogDiscovery = ServerModelCatalog{}

// Discover runs the transport for runner and blocks until it answers or fails.
// Failures are wrapped with upgrade and sign-in guidance.
func (ServerModelCatalog) Discover(ctx context.Context, runner core.Backend, executable string) ([]core.ModelChoice, error) {
	type answer struct {
		models []core.ModelChoice
		err    error
	}
	done := make(chan answer, 1)
	go func() {
		models, err := serverModelCatalogDiscoverSync(runner, executable)
		done <- answer{models, err}
	}()
	select {
	case <-ctx.Done():
		return nil, core.NewFailure(fmt.Sprintf("Cannot discover %s models. Check its configured executable, update the CLI and verify sign-in in your terminal, then Retry. %s", runner.Label(), ctx.Err().Error()))
	case result := <-done:
		if result.err != nil {
			return nil, core.NewFailure(fmt.Sprintf("Cannot discover %s models. Check its configured executable, update the CLI and verify sign-in in your terminal, then Retry. %s", runner.Label(), result.err.Error()))
		}
		return result.models, nil
	}
}

// serverModelCatalogDiscoverSync is the transport for one runner: its arguments,
// its handshake, and the model listing it reads.
func serverModelCatalogDiscoverSync(runner core.Backend, executable string) ([]core.ModelChoice, error) {
	arguments, err := serverModelCatalogArguments(runner)
	if err != nil {
		return nil, err
	}
	channel, err := newCatalogChannel(executable, arguments, runner == core.BackendCopilot)
	if err != nil {
		return nil, err
	}
	defer channel.Close()
	switch runner {
	case core.BackendClaude:
		return serverModelCatalogClaude(channel)
	case core.BackendCodex:
		return serverModelCatalogCodex(channel)
	default:
		return serverModelCatalogCopilot(channel)
	}
}

// serverModelCatalogArguments is the command line that starts runner's catalog
// transport.
func serverModelCatalogArguments(runner core.Backend) ([]string, error) {
	switch runner {
	case core.BackendCodex:
		return []string{"app-server"}, nil
	case core.BackendCopilot:
		return []string{"--headless", "--stdio", "--no-auto-update"}, nil
	// Not `--bare`: it narrows the list to the picker's handful of aliases.
	case core.BackendClaude:
		return []string{"--print", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose", "--no-session-persistence"}, nil
	default:
		return nil, core.NewFailure("Unsupported catalog transport")
	}
}

// serverModelCatalogClaude sends the initialize control request and returns the
// supportedModels answer, skipping any other message on the channel.
func serverModelCatalogClaude(channel *catalogChannel) ([]core.ModelChoice, error) {
	if err := channel.Send(map[string]any{"type": "control_request", "request_id": "catalog", "request": map[string]any{"subtype": "initialize"}}); err != nil {
		return nil, err
	}
	for {
		response, err := channel.Receive()
		if err != nil {
			return nil, err
		}
		if typ, _ := asString(response["type"]); typ != "control_response" {
			continue
		}
		envelope, _ := asObject(response["response"])
		requestID, _ := asString(envelope["request_id"])
		subtype, _ := asString(envelope["subtype"])
		result, hasResult := asObject(envelope["response"])
		if requestID != "catalog" || subtype != "success" || !hasResult {
			return nil, core.NewFailure("Unsupported supportedModels response")
		}
		return serverCatalogDecodeClaude(result["models"])
	}
}

// serverModelCatalogCodex runs the app-server handshake and reads model/list
// pages until the cursor is exhausted.
func serverModelCatalogCodex(channel *catalogChannel) ([]core.ModelChoice, error) {
	if _, err := channel.RPC(1, "initialize", map[string]any{"clientInfo": map[string]any{"name": "beltline_catalog", "version": "1"}}); err != nil {
		return nil, err
	}
	if err := channel.Send(map[string]any{"method": "initialized", "params": map[string]any{}}); err != nil {
		return nil, err
	}
	var cursor *string
	cursors := map[string]struct{}{}
	var models []core.ModelChoice
	id := 2
	for {
		params := map[string]any{"limit": 100}
		if cursor != nil {
			params["cursor"] = *cursor
		}
		result, err := channel.RPC(id, "model/list", params)
		if err != nil {
			return nil, err
		}
		// `model` is the CLI identifier, not the internal catalog `id`.
		page, err := serverCatalogDecodeModels(result["data"], "model", "displayName")
		if err != nil {
			return nil, err
		}
		models = append(models, page...)
		next, present := result["nextCursor"]
		if !present {
			return nil, core.NewFailure("Incomplete paginated catalog")
		}
		if next == nil {
			cursor = nil
		} else if text, ok := next.(string); ok {
			cursor = &text
		} else {
			return nil, core.NewFailure("Malformed catalog cursor")
		}
		if cursor != nil {
			if _, repeated := cursors[*cursor]; repeated {
				return nil, core.NewFailure("Repeated catalog cursor")
			}
			cursors[*cursor] = struct{}{}
		}
		id++
		if cursor == nil {
			break
		}
	}
	return models, nil
}

// serverModelCatalogCopilot runs the connect handshake, falling back to ping
// when the server predates connect, and reads models.list.
func serverModelCatalogCopilot(channel *catalogChannel) ([]core.ModelChoice, error) {
	if _, err := channel.RPC(1, "connect", map[string]any{"supportedTaskKinds": []any{"agent", "client", "shell"}}); err != nil {
		// The official SDK's legacy handshake, not an inferred catalog.
		rpcErr, isRPC := err.(serverCatalogRPCError)
		if !isRPC || rpcErr.Code != -32601 {
			return nil, err
		}
		if _, err := channel.RPC(2, "ping", map[string]any{}); err != nil {
			return nil, err
		}
	}
	result, err := channel.RPC(3, "models.list", map[string]any{})
	if err != nil {
		return nil, err
	}
	return serverCatalogDecodeCopilot(result["models"])
}

// serverCatalogDecodeCopilot decodes Copilot's models.list. A model with a
// long-context tier becomes two choices: the model as listed, and
// `<id>[long_context]`, which CopilotAdapter starts with `--context long_context`.
// The tier's size is its prompt budget plus the model's output limit.
func serverCatalogDecodeCopilot(value any) ([]core.ModelChoice, error) {
	models, err := serverCatalogDecodeModels(value, "id", "name")
	if err != nil {
		return nil, err
	}
	rows, ok := asObjects(value)
	if !ok {
		return models, nil
	}
	var out []core.ModelChoice
	for i, model := range models {
		row := rows[i]
		prices, _ := asObject(dictOrNil(row["billing"])["tokenPrices"])
		long, hasLong := asObject(prices["longContext"])
		tiers, _ := asStrings(row["supportedContextTiers"])
		if !hasLong && !slices.Contains(tiers, CopilotLongContext) {
			out = append(out, model)
			continue
		}
		output, err := serverCatalogReported(dictOrNil(dictOrNil(row["capabilities"])["limits"])["max_output_tokens"])
		if err != nil {
			return nil, err
		}
		outputTokens := core.ValueOf(output)
		// Once tiers are priced, the default size is the default tier's
		// budget, not a window that may already be the long one.
		standard := model
		budget, err := serverCatalogPrompt(prices)
		if err != nil {
			return nil, err
		}
		if budget != nil {
			size := *budget + outputTokens
			standard.ContextTokens = &size
		}
		longBudget, err := serverCatalogPrompt(long)
		if err != nil {
			return nil, err
		}
		var size *int
		if longBudget != nil {
			total := *longBudget + outputTokens
			size = &total
		}
		if size != nil && core.SamePointee(size, standard.ContextTokens) {
			out = append(out, standard)
			continue
		}
		name := model.Name
		if size == nil {
			name = model.Name + " (long context)"
		}
		out = append(out, standard, core.ModelChoice{ID: CopilotLongContextID(model.ID), Name: name, ContextTokens: size})
	}
	return out, nil
}

// dictOrNil is the object, or nil when the value is not an object.
func dictOrNil(value any) map[string]any {
	object, _ := asObject(value)
	return object
}

// serverCatalogPrompt is a tier's prompt budget, from maxPromptTokens or the deprecated contextMax.
func serverCatalogPrompt(tier map[string]any) (*int, error) {
	budget, err := serverCatalogReported(tier["maxPromptTokens"])
	if err != nil {
		return nil, err
	}
	if budget != nil {
		return budget, nil
	}
	return serverCatalogReported(tier["contextMax"])
}

// serverCatalogReported is a reported token count: nil when absent or null, an
// error when present but not a positive whole number.
func serverCatalogReported(value any) (*int, error) {
	if value == nil {
		return nil, nil
	}
	tokens := serverCatalogWholeTokens(value)
	if tokens == nil {
		return nil, core.NewFailure("Invalid reported context capacity. The last successful catalog was not replaced.")
	}
	return tokens, nil
}

// serverCatalogDecodeModels decodes a model list, reading each entry's id and name keys.
func serverCatalogDecodeModels(value any, id, name string) ([]core.ModelChoice, error) {
	rows, ok := asObjects(value)
	if !ok {
		return nil, core.NewFailure("Malformed catalog response")
	}
	models := make([]core.ModelChoice, 0, len(rows))
	for _, row := range rows {
		identifier, idOK := asString(row[id])
		label, nameOK := asString(row[name])
		if !idOK || identifier == "" || !nameOK || label == "" {
			return nil, core.NewFailure("Incomplete model entry")
		}
		tokens, err := serverCatalogContextTokens(row)
		if err != nil {
			return nil, err
		}
		models = append(models, core.ModelChoice{ID: identifier, Name: label, ContextTokens: tokens})
	}
	return models, nil
}

// serverCatalogContextTokens is the context window a row reports at
// capabilities.limits.max_context_window_tokens, or nil when it reports none.
func serverCatalogContextTokens(row map[string]any) (*int, error) {
	capabilities, ok := asObject(row["capabilities"])
	if !ok {
		return nil, nil
	}
	limits, ok := asObject(capabilities["limits"])
	if !ok {
		return nil, nil
	}
	value, present := limits["max_context_window_tokens"]
	if !present {
		return nil, nil
	}
	return serverCatalogReported(value)
}

// serverCatalogWholeTokens is a positive whole token count, or nil.
func serverCatalogWholeTokens(value any) *int {
	number, ok := value.(float64)
	if !ok {
		return nil
	}
	if number <= 0 || number > math.MaxInt64 || math.Trunc(number) != number {
		return nil
	}
	tokens := int(number)
	return &tokens
}

// serverCatalogDecodeClaude decodes Claude's supportedModels. Aliases resolve
// to the model they name now; `default` is not a model; `[1m]` is a size.
func serverCatalogDecodeClaude(value any) ([]core.ModelChoice, error) {
	rows, ok := asObjects(value)
	if !ok {
		return nil, core.NewFailure("Malformed catalog response")
	}
	var models []core.ModelChoice
	for _, row := range rows {
		alias, aliasOK := asString(row["value"])
		label, labelOK := asString(row["displayName"])
		if !aliasOK || alias == "" || !labelOK || label == "" {
			return nil, core.NewFailure("Incomplete model entry")
		}
		if alias == "default" {
			continue
		}
		resolved, _ := asString(row["resolvedModel"])
		pinned := alias
		if resolved != "" {
			pinned = resolved
			if strings.HasPrefix(alias, resolved) {
				pinned = alias
			}
		}
		// `opus[1m]` is a different choice from the unsuffixed model. Pinning
		// must not drop the exact suffix the catalog listed.
		id := pinned
		if strings.HasSuffix(alias, "[1m]") && !strings.HasSuffix(pinned, "[1m]") {
			id = alias
		}
		// "Opus 5.5 · Best for everyday, complex tasks · …" names the version.
		// Only the exact `[1m]` identity reports a size; an unsuffixed id does not.
		description, _ := asString(row["description"])
		versioned := trimWhitespaces(strings.Split(description, " · ")[0])
		if description == "" {
			versioned = ""
		}
		name := label
		if strings.HasPrefix(versioned, label) {
			name = versioned
		}
		var tokens *int
		if strings.HasSuffix(id, "[1m]") {
			million := 1_000_000
			tokens = &million
		}
		models = append(models, core.ModelChoice{ID: id, Name: name, ContextTokens: tokens})
	}
	return models, nil
}
