// The bundled resources: the built-in agent files and the configuration
// assistant's prompt, embedded from the resources/ folder beside this file.

package core

import (
	"embed"
	"io/fs"
)

//go:embed resources/agents/*.md
var builtInAgentFiles embed.FS

//go:embed resources/assistant/prompt.md
var assistantPromptFile string

// BuiltInAgentFile is the bundled file of a built-in agent, and whether the app
// bundles one with that id, read from the embedded Agents/<id>.md.
func BuiltInAgentFile(id string) (string, bool) {
	data, err := fs.ReadFile(builtInAgentFiles, "resources/agents/"+id+".md")
	if err != nil {
		return "", false
	}
	return string(data), true
}

// AssistantPromptTemplate is the contract the configuration assistant's first
// message starts with, bundled as Assistant/prompt.md.
func AssistantPromptTemplate() string {
	return assistantPromptFile
}
