package api

import (
	"github.com/robbertmanders/beltline/daemon/internal/integrations/runner"
	"github.com/robbertmanders/beltline/daemon/internal/kit/tools"
)

// ToolResponse is a tool diagnostic row as the API returns it.
type ToolResponse struct {
	Name     string `json:"name"`
	Purpose  string `json:"purpose"`
	OK       bool   `json:"ok"`
	Detail   string `json:"detail"`
	Optional bool   `json:"optional"`
}

// NewToolResponse builds the tool response from its arguments.
func NewToolResponse(result tools.Result) ToolResponse {
	return ToolResponse{
		Name: result.Name, Purpose: result.Purpose, OK: result.OK, Detail: result.Detail, Optional: result.Optional,
	}
}

// ToolsResponse is the body of GET /api/v1/tools.
type ToolsResponse struct {
	Tools []ToolResponse `json:"tools"`
	// TmuxSocket is the private tmux server name agents run on (`tmux -L <name>`).
	TmuxSocket string `json:"tmuxSocket"`
}

// NewToolsResponse builds the value from its arguments; an empty
// socket name defaults to the TmuxRunner socket.
func NewToolsResponse(tools []ToolResponse, tmuxSocket string) ToolsResponse {
	if tmuxSocket == "" {
		tmuxSocket = runner.TmuxSocket()
	}
	if tools == nil {
		tools = []ToolResponse{}
	}
	return ToolsResponse{Tools: tools, TmuxSocket: tmuxSocket}
}
