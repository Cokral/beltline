package launch

// VersionCurrent is the API's version, which its paths also carry (`/api/v1`).
const VersionCurrent = 1

// Version is what `GET /api/v1/version` returns: the API's version and the
// build's. The server answers with it and the discovery check reads it, so it
// lives here, below both.
type Version struct {
	APIVersion int    `json:"apiVersion"`
	Version    string `json:"version"`
	// Platform is the system the daemon runs on, "macos" or "linux" (Platform.Name).
	Platform string `json:"platform"`
}
