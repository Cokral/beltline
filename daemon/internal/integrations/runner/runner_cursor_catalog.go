package runner

import (
	"fmt"
	"slices"
	"strings"
	"unicode"

	"github.com/robbertmanders/beltline/daemon/internal/core"
)

// Cursor lists one entry per model, thinking level and speed
// (`claude-opus-5-5-high-fast - Claude Opus 5.5 1M High Fast`), and takes no
// separate level. Its entries fold into one choice per model and speed, whose
// variants keep each level's exact identifier, so a stage picks the model and
// its Thinking separately and the session is given an id Cursor listed.

// cursorCatalogModifiers are the trailing parts of an id that name the model, not its level.
var cursorCatalogModifiers = map[string]struct{}{"fast": {}, "thinking": {}}

// cursorCatalogLevels maps a level word in an id to its level.
var cursorCatalogLevels = map[string]string{
	"none": "none", "minimal": "minimal", "low": "low", "medium": "medium", "high": "high", "xhigh": "xhigh", "max": "max",
}

// cursorCatalogLevelWords are the level words a display name may carry.
var cursorCatalogLevelWords = map[string]struct{}{
	"none": {}, "minimal": {}, "low": {}, "medium": {}, "high": {}, "max": {},
}

// cursorCatalogEntry is one line of `cursor-agent models`: its id and display name.
type cursorCatalogEntry struct {
	ID   string
	Name string
}

// cursorCatalogMember is one entry of a model group, with its level in the id.
type cursorCatalogMember struct {
	entry cursorCatalogEntry
	level *string
}

// cursorCatalogGroup folds the entries into one choice per model and speed.
// Fails when a thinking group mixes context sizes without distinct identifiers.
func cursorCatalogGroup(entries []cursorCatalogEntry) ([]core.ModelChoice, error) {
	var order []string
	groups := map[string][]cursorCatalogMember{}
	for _, entry := range entries {
		base, level := cursorCatalogSplit(entry.ID)
		if _, seen := groups[base]; !seen {
			order = append(order, base)
		}
		groups[base] = append(groups[base], cursorCatalogMember{entry: entry, level: level})
	}
	var choices []core.ModelChoice
	for _, base := range order {
		members := groups[base]
		contexts := make([]*int, 0, len(members))
		for _, member := range members {
			context, err := cursorCatalogReportedContext(member.entry.Name)
			if err != nil {
				return nil, err
			}
			contexts = append(contexts, context)
		}
		// One thinking group shares one context. Mixed sizes whose ids do not
		// already differ must fail, not collapse into a single choice.
		distinct := map[int]struct{}{}
		sawNil := false
		for _, context := range contexts {
			if context == nil {
				sawNil = true
			} else {
				distinct[*context] = struct{}{}
			}
		}
		count := len(distinct)
		if sawNil {
			count++
		}
		if count > 1 {
			return nil, core.NewFailure(fmt.Sprintf("Cursor catalog mixes context sizes for %s without distinct identifiers. The last successful catalog was not replaced. Update Cursor and Retry.", base))
		}
		context := contexts[0]
		if len(members) == 1 && members[0].level == nil {
			choices = append(choices, core.ModelChoice{ID: members[0].entry.ID, Name: cursorCatalogClean(members[0].entry.Name), ContextTokens: context})
			continue
		}
		variants := map[string]string{}
		var named *string
		for _, member := range members {
			// Cursor names its default variant without a level.
			isDefault := member.level == nil || !cursorCatalogNamesLevel(member.entry.Name)
			if isDefault {
				if _, ok := variants["default"]; !ok {
					variants["default"] = member.entry.ID
				}
				if named == nil {
					name := cursorCatalogClean(member.entry.Name)
					named = &name
				}
			}
			if member.level != nil && isThinkingValue(*member.level) {
				variants[*member.level] = member.entry.ID
			}
		}
		// An unsuffixed default stands for medium when no variant says so.
		if fallback, ok := variants["default"]; ok {
			_, hasMedium := variants[string(core.ThinkingMedium)]
			if !hasMedium && hasUnsuffixed(members) {
				variants[string(core.ThinkingMedium)] = fallback
			}
		}
		if !hasThinkingVariant(variants) {
			for _, member := range members {
				if member.level != nil && *member.level == "minimal" {
					variants[string(core.ThinkingLow)] = member.entry.ID
					break
				}
			}
		}
		name := core.ValueOr(named, cursorCatalogWithoutLevel(members[0].entry.Name))
		choices = append(choices, core.ModelChoice{ID: base, Name: name, Variants: variants, ContextTokens: context})
	}
	return choices, nil
}

// isThinkingValue reports whether a level word is a Thinking raw value.
func isThinkingValue(level string) bool {
	return slices.ContainsFunc(core.AllThinking, func(t core.Thinking) bool { return string(t) == level })
}

// hasUnsuffixed reports whether a group holds an entry with no level in its id.
func hasUnsuffixed(members []cursorCatalogMember) bool {
	return slices.ContainsFunc(members, func(member cursorCatalogMember) bool { return member.level == nil })
}

// hasThinkingVariant reports whether any variant key is a Thinking raw value.
func hasThinkingVariant(variants map[string]string) bool {
	for key := range variants {
		if isThinkingValue(key) {
			return true
		}
	}
	return false
}

// cursorCatalogReportedContext is the k/M size word in a display name ("1M",
// "250k"), or nil. Bare numbers are versions, not sizes. Fails on two sizes.
func cursorCatalogReportedContext(name string) (*int, error) {
	var found []int
	for _, word := range cursorCatalogWords(name) {
		if size := core.ContextSizeParse(word, false); size != nil {
			found = append(found, *size)
		}
	}
	if len(found) > 1 {
		return nil, core.NewFailure(fmt.Sprintf("Cursor catalog reports contradictory context sizes in \"%s\". The last successful catalog was not replaced. Update Cursor and Retry.", name))
	}
	if len(found) == 0 {
		return nil, nil
	}
	return &found[0], nil
}

// cursorCatalogSplit splits an id into its base and its level:
// `claude-4.6-opus-high-thinking` gives (`claude-4.6-opus-thinking`, high);
// `gpt-5.5-extra-high-fast` gives (`gpt-5.5-fast`, xhigh).
func cursorCatalogSplit(id string) (base string, level *string) {
	var parts []string
	for _, part := range strings.Split(id, "-") {
		if part != "" {
			parts = append(parts, part)
		}
	}
	var trailing []string
	for len(parts) > 1 {
		last := parts[len(parts)-1]
		if _, modifier := cursorCatalogModifiers[last]; !modifier {
			break
		}
		trailing = append([]string{last}, trailing...)
		parts = parts[:len(parts)-1]
	}
	var found *string
	if n := len(parts); n > 2 && parts[n-2] == "extra" && parts[n-1] == "high" {
		parts = parts[:n-2]
		xhigh := "xhigh"
		found = &xhigh
	} else if n > 1 {
		if mapped, ok := cursorCatalogLevels[parts[n-1]]; ok {
			parts = parts[:n-1]
			found = &mapped
		}
	}
	return strings.Join(append(parts, trailing...), "-"), found
}

// cursorCatalogNamesLevel reports whether a display name says its level ("… High", "… Extra High").
func cursorCatalogNamesLevel(name string) bool {
	for _, word := range cursorCatalogWords(name) {
		if _, ok := cursorCatalogLevelWords[strings.ToLower(word)]; ok {
			return true
		}
	}
	return false
}

// cursorCatalogWithoutLevel is the display name with its level words removed.
func cursorCatalogWithoutLevel(name string) string {
	var kept []string
	for _, word := range cursorCatalogWords(name) {
		lower := strings.ToLower(word)
		if lower == "high" && len(kept) > 0 && strings.ToLower(kept[len(kept)-1]) == "extra" {
			kept = kept[:len(kept)-1]
			continue
		}
		if _, ok := cursorCatalogLevelWords[lower]; ok {
			continue
		}
		kept = append(kept, word)
	}
	return strings.Join(kept, " ")
}

// cursorCatalogWords splits a name on whitespace, after dropping zero-width characters.
func cursorCatalogWords(name string) []string {
	var visible strings.Builder
	for _, r := range name {
		if isDefaultIgnorable(r) {
			continue
		}
		visible.WriteRune(r)
	}
	return strings.FieldsFunc(visible.String(), unicode.IsSpace)
}

// cursorDefaultIgnorable are the ranges of Unicode's Default_Ignorable_Code_Point
// property (DerivedCoreProperties).
var cursorDefaultIgnorable = [][2]rune{
	{0x00AD, 0x00AD}, {0x034F, 0x034F}, {0x061C, 0x061C}, {0x115F, 0x1160},
	{0x17B4, 0x17B5}, {0x180B, 0x180F}, {0x200B, 0x200F}, {0x202A, 0x202E},
	{0x2060, 0x206F}, {0x3164, 0x3164}, {0xFE00, 0xFE0F}, {0xFEFF, 0xFEFF},
	{0xFFA0, 0xFFA0}, {0xFFF0, 0xFFF8}, {0x1BCA0, 0x1BCA3}, {0x1D173, 0x1D17A},
	{0xE0000, 0xE0FFF},
}

// isDefaultIgnorable reports whether r has the Default_Ignorable_Code_Point property:
// the invisible characters a catalog name may carry (joiners, variation selectors).
func isDefaultIgnorable(r rune) bool {
	return slices.ContainsFunc(cursorDefaultIgnorable, func(span [2]rune) bool {
		return r >= span[0] && r <= span[1]
	})
}

// cursorCatalogClean is a display name without zero-width characters, doubled
// spaces and "(current)".
func cursorCatalogClean(name string) string {
	var kept []string
	for _, word := range cursorCatalogWords(name) {
		if word != "(current)" {
			kept = append(kept, word)
		}
	}
	return strings.Join(kept, " ")
}
