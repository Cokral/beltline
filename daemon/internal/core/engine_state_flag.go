// The flags of a state.json file: archived (library check) and paused and
// archived (factory export). Those flags read a JSON 0 or 1 as a Bool, as well as true and false.

package core

import (
	"strings"

	"encoding/json"
)

// StateFlag reads the flag key of a state.json object: the value is
// StateFlag reads the flag key of a state.json object: true for the literal true or 1,
// false for false, 0, any other value, a missing key, and data that is not a JSON object.
// It never fails.
func StateFlag(data []byte, key string) bool {
	var members map[string]json.RawMessage
	if err := json.Unmarshal(data, &members); err != nil {
		return false
	}
	raw, ok := members[key]
	if !ok {
		return false
	}
	switch strings.TrimSpace(string(raw)) {
	case "true", "1":
		return true
	default:
		return false
	}
}
