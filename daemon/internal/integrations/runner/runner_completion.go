// The handoff an interactive stage session ends with.
// These runners run headless, and their exit code is read.

package runner

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/robbertmanders/beltline/daemon/internal/core"
)

// CompletionRecord is the attempt folder's handoff record.
func CompletionRecord(attemptFolder string) string {
	return filepath.Join(attemptFolder, core.BriefHandoffFile)
}

// CompletionHandedBack reports whether the attempt folder's handoff record holds
// token. A record left by an earlier launch, or one still being written, holds
// something else, so it never ends this launch.
func CompletionHandedBack(attemptFolder, token string) bool {
	if strings.TrimSpace(token) == "" {
		return false
	}
	data, err := os.ReadFile(CompletionRecord(attemptFolder))
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(data)) == token
}
