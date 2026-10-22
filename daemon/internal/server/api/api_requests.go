package api

// PauseRequest is the body of POST /api/v1/pause.
type PauseRequest struct {
	Paused bool `json:"paused" required:"true"`
}

// AfkRequest is the body of POST /api/v1/afk.
type AfkRequest struct {
	Enabled bool `json:"enabled" required:"true"`
}

// ModelAllowRequest is the body of POST /api/v1/models/{runner}/allow.
type ModelAllowRequest struct {
	Model   string `json:"model" required:"true"`
	Allowed bool   `json:"allowed" required:"true"`
}

// AssistantStartRequest is the body of POST /api/v1/assistant/start.
type AssistantStartRequest struct {
	Request string `json:"request" required:"true"`
}
