package launch

import (
	"os"
	"strings"

	"github.com/robbertmanders/beltline/daemon/internal/integrations/store"
	"github.com/robbertmanders/beltline/daemon/internal/kit/coordinator"
)

// LibraryCheckCommandFlag is the flag that runs the library check.
const LibraryCheckCommandFlag = "--check-library"

// LibraryCheckCommandResult is what the check prints and the status it exits with.
type LibraryCheckCommandResult struct {
	// Output is exactly what goes to standard output.
	Output string
	// Status is 0 for a clean library, 1 otherwise.
	Status int32
}

// LibraryCheckCommandRun returns the check arguments ask for, or nil when they do
// not name the flag. A folder after the flag wins over standard, which is the
// standard folder when nil.
// Lock: none; it reads the folder only.
func LibraryCheckCommandRun(arguments []string, standard func() store.Paths) *LibraryCheckCommandResult {
	index := -1
	for i, argument := range arguments {
		if argument == LibraryCheckCommandFlag {
			index = i
			break
		}
	}
	if index < 0 {
		return nil
	}
	if index+1 < len(arguments) {
		result := LibraryCheckCommandCheck(store.Paths{Root: arguments[index+1]})
		return &result
	}
	if standard == nil {
		standard = launchStandardPaths
	}
	result := LibraryCheckCommandCheck(standard())
	return &result
}

// LibraryCheckCommandCheck checks the library in paths: "No problems." and 0 for a
// clean library, otherwise one problem a line and 1.
// Lock: none; it reads the folder only and takes no lock.
func LibraryCheckCommandCheck(paths store.Paths) LibraryCheckCommandResult {
	problems := coordinator.LibraryAssistantCheck(paths)
	if len(problems) == 0 {
		return LibraryCheckCommandResult{Output: "No problems.\n", Status: 0}
	}
	return LibraryCheckCommandResult{Output: strings.Join(problems, "\n") + "\n", Status: 1}
}

// launchStandardPaths is Paths.standard(): BELTLINE_DATA_DIR when set, else the
// Beltline folder under Application Support.
func launchStandardPaths() store.Paths {
	support, err := store.SupportFolder()
	if err != nil {
		support = os.TempDir()
	}
	return store.StandardPaths(os.Getenv, support)
}
