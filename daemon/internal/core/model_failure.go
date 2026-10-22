// Failure is the error for an operation that is refused. It lives
// in core so every layer (store, GitHub, Jira, runners, coordinator, server)
// throws and maps the same type: the server answers it with 422.

package core

// Failure is a refusal the user sees: a message, and the ordered problems of a
// rejected save (empty for a plain failure).
type Failure struct {
	Message  string
	Problems []string
}

// Error returns the message, which is what the user is shown.
func (f Failure) Error() string { return f.Message }

// NewFailure is a Failure with the message and problems.
func NewFailure(message string, problems ...string) Failure {
	return Failure{Message: message, Problems: append([]string{}, problems...)}
}
