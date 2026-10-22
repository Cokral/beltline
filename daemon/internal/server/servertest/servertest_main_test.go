package servertest

import (
	"os"
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/testkit"
)

// TestMain closes the shared fixtures and removes the template folders the contract demo is built in.
func TestMain(m *testing.M) {
	code := m.Run()
	closeSharedFixtures()
	testkit.Cleanup()
	os.Exit(code)
}
