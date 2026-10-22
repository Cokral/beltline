// The current executable, and the symlink resolution that its path goes through.

package daemon

import (
	"os"
	"path/filepath"
	"strings"
)

// CurrentExecutable is the real path of the running executable, however it was
// launched, so a launch by bare name from PATH still resolves for the
// assistant's self-check and the LaunchAgent.
// Lock: none.
func CurrentExecutable() string {
	path, err := os.Executable()
	if err != nil {
		if len(os.Args) > 0 {
			return os.Args[0]
		}
		return ""
	}
	return resolveSymlinks(path)
}

// resolveSymlinks resolves the symbolic links in path, after standardizing it. A path that does not
// exist resolves to itself. It drops the /private prefix from /var, /tmp and /etc, so /var/folders
// stays spelled as the user named it.
// Lock: none (pure apart from the file system reads).
func resolveSymlinks(path string) string {
	standard := filepath.Clean(path)
	resolved, err := filepath.EvalSymlinks(standard)
	if err != nil {
		resolved = standard
	}
	for _, prefix := range []string{"var", "tmp", "etc"} {
		private := "/private/" + prefix
		if resolved == private || strings.HasPrefix(resolved, private+"/") {
			return strings.TrimPrefix(resolved, "/private")
		}
	}
	return resolved
}
