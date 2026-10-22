package runner

import (
	"github.com/robbertmanders/beltline/daemon/internal/core"
)

// ConversationSpec describes a conversation, which has no work run, attempt,
// output, outcome or completion marker. It has no initialiser, so
// it is a plain struct literal. Directory and PromptFile are filesystem paths
// .
type ConversationSpec struct {
	// Name is the conversation's session name.
	Name string
	// SessionID is the id Beltline proposes for the conversation.
	SessionID string
	// Backend is the CLI the conversation runs in.
	Backend core.Backend
	// Model is the explicit model; a conversation with none is refused.
	Model string
	// Thinking is the reasoning level, or nil for the runner's default.
	Thinking *core.Thinking
	// Prompt is the first message, written to PromptFile for the command line to read.
	Prompt string
	// Directory is where the conversation works.
	Directory string
	// PromptFile is where the prompt is written, outside Directory.
	PromptFile string
}
