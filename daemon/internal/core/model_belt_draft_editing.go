// RepositoryInput, the
// BeltDraftEditing namespace and the Belt extension for drafts. The types are in
// model_belt_draft_request.go.
//
// Mutating functions are pointer-receiver methods. The static
// functions of BeltDraftEditing are package functions prefixed with
// BeltDraftEditing; the throwing ones return an error.
//
// A request's draft is cloned before it is edited, so the request a client
// sent is never changed.

package core

import (
	"strings"
)

// RepositoryInputNormalize is the repository input as the editor reads it:
// whitespace, the GitHub URL prefix and surrounding slashes are stripped, and the
// rest must be owner/repository. Nil when it is not.
func RepositoryInputNormalize(raw string) *string {
	value := strings.TrimSpace(raw)
	for _, prefix := range []string{"https://github.com/", "http://github.com/", "github.com/"} {
		value = strings.TrimPrefix(value, prefix)
	}
	value = strings.Trim(value, "/")
	var parts []string
	for _, p := range strings.Split(value, "/") {
		if p != "" {
			parts = append(parts, p)
		}
	}
	if len(parts) != 2 {
		return nil
	}
	normalized := parts[0] + "/" + parts[1]
	return &normalized
}

// BeltDraftEditingApplyResult is the edited draft and, for trigger-kind, the
// stash the client keeps for this entry: the outgoing kind is stored in it, so
// switching back restores what the entry was set to.
// A request with problems returns the
// ValidationFailure and the zero draft.
func BeltDraftEditingApplyResult(request BeltDraftRequest) (Belt, *TriggerStash, error) {
	draft, stash, err := beltDraftEditingApply(request)
	if err == nil {
		// Whichever operation removed a listing, an override left without one goes.
		draft.pruneRepositoryBases()
	}
	return draft, stash, err
}

// beltDraftEditingValidStageID reports whether id is not blank once spaces and
// tabs are trimmed.
func beltDraftEditingValidStageID(id string) bool {
	return trimBlanks(id) != ""
}
