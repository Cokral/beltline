// The usage that Pi's and Copilot's session logs report.

package runner

import (
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"slices"

	"encoding/json"
	"github.com/robbertmanders/beltline/daemon/internal/core"
	"golang.org/x/text/unicode/norm"
)

// decimalNumber is a JSON number read exactly, from
// the number's own text, never through a float.
type decimalNumber big.Rat

// UnmarshalJSON reads the number's text as an exact decimal.
func (d *decimalNumber) UnmarshalJSON(data []byte) error {
	value, ok := new(big.Rat).SetString(string(data))
	if !ok {
		return errors.New("expected a number")
	}
	*d = decimalNumber(*value)
	return nil
}

// rat is the exact value as a big.Rat.
func (d *decimalNumber) rat() *big.Rat {
	value := big.Rat(*d)
	return &value
}

// piUsageEntry is one Pi assistant line, decoded for its usage.
type piUsageEntry struct {
	Message piUsageMessage `json:"message"`
}

// piUsageMessage is the usage report of a Pi assistant message.
type piUsageMessage struct {
	Usage *piUsageReport `json:"usage,omitzero"`
}

// piUsageReport is the counters and reported cost of one Pi call.
type piUsageReport struct {
	Cost        *piUsageCost `json:"cost,omitzero"`
	Input       *int         `json:"input,omitzero"`
	Output      *int         `json:"output,omitzero"`
	CacheRead   *int         `json:"cacheRead,omitzero"`
	CacheWrite  *int         `json:"cacheWrite,omitzero"`
	Reasoning   *int         `json:"reasoning,omitzero"`
	TotalTokens *int         `json:"totalTokens,omitzero"`
}

// piUsageCost is the reported dollar total of one Pi call.
type piUsageCost struct {
	Total *decimalNumber `json:"total,omitzero"`
}

// UsageMark is zero: Pi's usage is counted from the log's assistant messages.
func (PiAdapter) UsageMark(directory, sessionID, name string) int { return 0 }

// Usage sums the reported cost and counters of the assistant messages after the
// first `after`. Each message with a usable cost adds its own.
func (PiAdapter) Usage(directory, sessionID, name string, after, marked int) *core.Usage {
	text := piLog(directory, sessionID)
	if text == nil {
		return nil
	}
	return PiUsageOf(*text, after)
}

// PiUsageOf is the summed usage of a Pi session log's assistant messages after
// the first `after`.
func PiUsageOf(log string, after int) *core.Usage {
	var total *core.Usage
	seen := 0
	for _, line := range splitLines(log, true) {
		if piAssistantMessage(line) == nil {
			continue
		}
		seen++
		if seen <= after {
			continue
		}
		var entry piUsageEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		reported := entry.Message.Usage
		if reported == nil || reported.Cost == nil || reported.Cost.Total == nil {
			continue
		}
		cost := reported.Cost.Total.rat()
		if cost.Sign() < 0 {
			continue
		}
		nanos, ok := intFromBig(roundHalfUp(new(big.Rat).Mul(cost, big.NewRat(1_000_000_000, 1))))
		if !ok {
			continue
		}
		usage := core.NewUsage(core.UsageArgs{
			CostNanos: &nanos, Input: orZero(reported.Input), Output: orZero(reported.Output),
			CacheRead: orZero(reported.CacheRead), CacheWrite: orZero(reported.CacheWrite),
			Reasoning: orZero(reported.Reasoning), TotalTokens: orZero(reported.TotalTokens),
		})
		base := core.ValueOr(total, core.NewUsage(core.UsageArgs{CostNanos: new(0)}))
		sum := core.UsageAdd(base, usage)
		total = &sum
	}
	return total
}

// orZero is the count, or a reported zero when it is absent.
func orZero(count *int) *int {
	if count != nil {
		return count
	}
	return new(0)
}

// copilotUsageEntry is one Copilot events line that may carry a cumulative usage report.
type copilotUsageEntry struct {
	Type string            `json:"type"`
	Data *copilotUsageData `json:"data,omitzero"`
}

// copilotUsageData is the usage a Copilot event reports. Both spellings of the
// nano-AIU total are read, snake case first.
type copilotUsageData struct {
	TotalNanoAIUSnake *int `json:"total_nano_aiu,omitzero"`
	TotalNanoAIU      *int `json:"totalNanoAiu,omitzero"`
}

// UsageMark is the last cumulative nano-AIU total in the session's events, or zero.
func (c CopilotAdapter) UsageMark(directory, sessionID, name string) int {
	text := c.events(name, nil)
	if text == nil {
		return 0
	}
	if total := CopilotNanoAIU(*text); total != nil {
		return *total
	}
	return 0
}

// Usage is the cost since the marked total, as nano-dollars at 100 nano-AIU per nano-dollar.
func (c CopilotAdapter) Usage(directory, sessionID, name string, after, marked int) *core.Usage {
	text := c.events(name, nil)
	if text == nil {
		return nil
	}
	return CopilotUsageOf(*text, marked)
}

// UsageCheckpoint is the previous checkpoint with the completed line count and the exact identity, when one matches.
func (c CopilotAdapter) UsageCheckpoint(directory, sessionID, name string, previous *core.UsageCheckpoint) *core.UsageCheckpoint {
	checkpoint := core.ValueOr(previous, core.UsageCheckpoint{})
	lines := c.completeLineCount(name, nil)
	checkpoint.Lines = &lines
	if id := c.exactIdentity(name, nil); id != nil {
		checkpoint.SessionID = id
	}
	return &checkpoint
}

// UsageIdentity is the checkpoint's session id.
func (c CopilotAdapter) UsageIdentity(directory string, checkpoint *core.UsageCheckpoint) *string {
	if checkpoint == nil {
		return nil
	}
	return checkpoint.SessionID
}

// events is the session's events.jsonl text, or nil when no session matches.
func (c CopilotAdapter) events(name string, root *string) *string {
	match := c.locate(name, c.rootOr(root))
	if match.Kind != copilotSessionFound {
		return nil
	}
	text, ok := readText(filepath.Join(match.Folder, "events.jsonl"))
	if !ok {
		return nil
	}
	return &text
}

// rootOr is the given state root, or the adapter's own when none is given.
func (c CopilotAdapter) rootOr(root *string) string {
	if root != nil {
		return *root
	}
	return c.StateRoot
}

// completion is the finish recorded after `after` complete lines, or a failure
// when two sessions match. Nil when the session is missing or has not finished.
func (c CopilotAdapter) completion(name string, after int, root *string) *LogEnding {
	match := c.locate(name, c.rootOr(root))
	switch match.Kind {
	case copilotSessionAmbiguous:
		return &LogEnding{Kind: LogEndingFailed, Message: "more than one Copilot session matches " + name}
	case copilotSessionMissing:
		return nil
	}
	text, ok := readText(filepath.Join(match.Folder, "events.jsonl"))
	if !ok {
		return nil
	}
	return copilotCompletionIn(text, after)
}

// didAbort reports an abort recorded after `after` complete lines.
func (c CopilotAdapter) didAbort(name string, after int, root *string) bool {
	match := c.locate(name, c.rootOr(root))
	if match.Kind != copilotSessionFound {
		return false
	}
	text, ok := readText(filepath.Join(match.Folder, "events.jsonl"))
	if !ok {
		return false
	}
	return copilotDidAbortIn(text, after)
}

// completeLineCount is the number of complete lines in the session's events.
func (c CopilotAdapter) completeLineCount(name string, root *string) int {
	text := c.events(name, root)
	if text == nil {
		return 0
	}
	return len(copilotCompleteLines(*text))
}

// exactIdentity is the session folder name when exactly one session matches, or nil.
func (c CopilotAdapter) exactIdentity(name string, root *string) *string {
	match := c.locate(name, c.rootOr(root))
	if match.Kind != copilotSessionFound {
		return nil
	}
	base := filepath.Base(match.Folder)
	return &base
}

// CopilotNanoAIU is the last cumulative nano-AIU total in a log, from the
// completion, checkpoint or shutdown events.
func CopilotNanoAIU(log string) *int {
	var total *int
	for _, line := range splitLines(log, true) {
		var event copilotUsageEntry
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}
		if !slices.Contains([]string{"session.task_complete", "session.usage_checkpoint", "session.shutdown"}, event.Type) {
			continue
		}
		var value *int
		if event.Data != nil {
			value = event.Data.TotalNanoAIU
			if value == nil {
				value = event.Data.TotalNanoAIUSnake
			}
		}
		if value == nil || *value < 0 {
			continue
		}
		total = value
	}
	return total
}

// CopilotUsageOf is the cost of a log's last cumulative total less the marked
// total, in nano-dollars (100 nano-AIU each).
func CopilotUsageOf(log string, marked int) *core.Usage {
	total := CopilotNanoAIU(log)
	if total == nil {
		return nil
	}
	nanos := max(0, *total-max(0, marked)) / 100
	usage := core.NewUsage(core.UsageArgs{CostNanos: &nanos})
	return &usage
}

// copilotCompletionIn is the finish after `after` complete lines, or nil.
func copilotCompletionIn(log string, after int) *LogEnding {
	lines := copilotCompleteLines(log)
	if after >= len(lines) {
		return nil
	}
	for _, line := range lines[max(0, after):] {
		if CopilotIsCompletion(line) {
			return &LogEnding{Kind: LogEndingFinished}
		}
	}
	return nil
}

// copilotDidAbortIn reports an abort or session.abort event after `after` complete lines.
func copilotDidAbortIn(log string, after int) bool {
	lines := copilotCompleteLines(log)
	if after >= len(lines) {
		return false
	}
	for _, line := range lines[max(0, after):] {
		object := jsonObject(line)
		typ, ok := asString(object["type"])
		if !ok {
			continue
		}
		if typ == "abort" || typ == "session.abort" {
			return true
		}
	}
	return false
}

// copilotCompleteLines are the complete, non-empty lines of a log: a partial
// trailing write is neither a boundary nor a completion.
func copilotCompleteLines(log string) []string {
	if log == "" {
		return nil
	}
	lines := splitLines(log, false)
	if !hasNewlineSuffix(log) && len(lines) > 0 {
		lines = lines[:len(lines)-1]
	}
	var complete []string
	for _, line := range lines {
		if line != "" {
			complete = append(complete, line)
		}
	}
	return complete
}

// copilotSessionKind is whether a name matched one session, none, or several.
type copilotSessionKind string

const (
	copilotSessionFound     copilotSessionKind = "found"
	copilotSessionMissing   copilotSessionKind = "missing"
	copilotSessionAmbiguous copilotSessionKind = "ambiguous"
)

// copilotSessionMatch is the outcome of locating a session folder.
type copilotSessionMatch struct {
	Kind   copilotSessionKind
	Folder string
}

// locate finds the session folder named `name` under root: an exact folder name,
// else exactly one workspace.yaml with that name. Copilot truncates --name to 100
// characters, and a prefix is not a match.
func (c CopilotAdapter) locate(name, root string) copilotSessionMatch {
	key := core.PrefixCharacters(name, 100)
	if key == "" {
		return copilotSessionMatch{Kind: copilotSessionMissing}
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		entries = nil
	}
	for _, entry := range entries {
		if canonicalTextEqual(entry.Name(), key) {
			return copilotSessionMatch{Kind: copilotSessionFound, Folder: filepath.Join(root, entry.Name())}
		}
	}
	var named []string
	for _, entry := range entries {
		folder := filepath.Join(root, entry.Name())
		metadata, ok := readText(filepath.Join(folder, "workspace.yaml"))
		if !ok {
			continue
		}
		for _, line := range splitLines(metadata, true) {
			if canonicalTextEqual(line, "name: "+key) || canonicalTextEqual(line, `name: "`+key+`"`) || canonicalTextEqual(line, "name: '"+key+"'") {
				named = append(named, folder)
				break
			}
		}
	}
	switch {
	case len(named) > 1:
		return copilotSessionMatch{Kind: copilotSessionAmbiguous}
	case len(named) == 1:
		return copilotSessionMatch{Kind: copilotSessionFound, Folder: named[0]}
	}
	return copilotSessionMatch{Kind: copilotSessionMissing}
}

// canonicalTextEqual compares text by canonical equivalence, so a name written
// with a decomposed letter matches its composed form. Both sides are NFC-normalised.
func canonicalTextEqual(a, b string) bool {
	return norm.NFC.String(a) == norm.NFC.String(b)
}
