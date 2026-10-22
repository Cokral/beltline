// Converts Jira's rich text, the Atlassian Document Format, to plain Markdown-ish text.

package jira

import (
	"slices"
	"strconv"
	"strings"
)

// ADFText is the text of a Jira rich-text value: a string as it is, a document
// as Markdown-ish text with surrounding whitespace removed, and "" for anything else.
func ADFText(value any) string {
	if s, ok := value.(string); ok {
		return s
	}
	node := obj(value)
	if node == nil {
		return ""
	}
	return strings.TrimSpace(adfBlock(node))
}

// adfChildren is the node's content, as objects.
func adfChildren(node map[string]any) []map[string]any { return objs(node["content"]) }

// adfAttrs is the node's attributes, or an empty object.
func adfAttrs(node map[string]any) map[string]any {
	if a := obj(node["attrs"]); a != nil {
		return a
	}
	return map[string]any{}
}

// adfBlocks is the node's blocks, joined by blank lines.
func adfBlocks(node map[string]any) string {
	var parts []string
	for _, child := range adfChildren(node) {
		if text := adfBlock(child); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n\n")
}

// adfBlock is one block node as text.
func adfBlock(node map[string]any) string {
	typ, _ := str(node["type"])
	switch typ {
	case "paragraph":
		return adfInline(node)
	case "heading":
		level, ok := intOf(adfAttrs(node)["level"])
		if !ok {
			level = 2
		}
		level = max(min(level, 6), 1)
		return strings.Repeat("#", level) + " " + adfInline(node)
	case "bulletList":
		return adfList(node, func(int) string { return "- " })
	case "orderedList":
		start, ok := intOf(adfAttrs(node)["order"])
		if !ok {
			start = 1
		}
		return adfList(node, func(i int) string { return strconv.Itoa(start+i) + ". " })
	case "codeBlock":
		return "```\n" + adfInline(node) + "\n```"
	case "blockquote":
		lines := strings.Split(adfBlocks(node), "\n")
		for i, line := range lines {
			lines[i] = "> " + line
		}
		return strings.Join(lines, "\n")
	case "rule":
		return "---"
	case "table":
		var rows []string
		for _, row := range adfChildren(node) {
			var cells []string
			for _, cell := range adfChildren(row) {
				cells = append(cells, strings.ReplaceAll(adfBlocks(cell), "\n", " "))
			}
			rows = append(rows, strings.Join(cells, " | "))
		}
		return strings.Join(rows, "\n")
	case "text", "hardBreak", "mention", "emoji", "inlineCard", "date", "status":
		return adfInline(map[string]any{"content": []any{node}})
	default:
		return adfBlocks(node)
	}
}

// adfList is a list's items, each marked and indented under its marker.
func adfList(node map[string]any, marker func(int) string) string {
	var items []string
	for i, item := range adfChildren(node) {
		text := adfBlocks(item)
		mark := marker(i)
		indent := strings.Repeat(" ", len(mark))
		items = append(items, mark+strings.Join(strings.Split(text, "\n"), "\n"+indent))
	}
	return strings.Join(items, "\n")
}

// adfInline is a node's inline children as text.
func adfInline(node map[string]any) string {
	var b strings.Builder
	for _, child := range adfChildren(node) {
		typ, _ := str(child["type"])
		attrs := adfAttrs(child)
		switch typ {
		case "text":
			text := strOr(child["text"], "")
			marks := objs(child["marks"])
			var kinds []string
			for _, m := range marks {
				kinds = append(kinds, strOr(m["type"], ""))
			}
			if slices.Contains(kinds, "code") {
				b.WriteString("`" + text + "`")
				continue
			}
			var link map[string]any
			for _, m := range marks {
				if strOr(m["type"], "") == "link" {
					link = m
					break
				}
			}
			if link != nil {
				if href, ok := str(adfAttrs(link)["href"]); ok && href != text {
					b.WriteString("[" + text + "](" + href + ")")
					continue
				}
			}
			b.WriteString(text)
		case "hardBreak":
			b.WriteString("\n")
		case "mention", "emoji":
			b.WriteString(firstString(attrs["text"], attrs["shortName"]))
		case "inlineCard":
			b.WriteString(strOr(attrs["url"], ""))
		case "status":
			b.WriteString(strOr(attrs["text"], ""))
		default:
			b.WriteString(adfInline(child))
		}
	}
	return b.String()
}

// firstString is the first of a and b that is a string, or "".
func firstString(a, b any) string {
	if s, ok := a.(string); ok {
		return s
	}
	return strOr(b, "")
}
