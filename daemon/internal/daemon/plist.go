// The property-list parts of the LaunchAgent: the XML property list written for it (sorted keys, tab
// indentation), and the reader that compares an existing plist with it.

package daemon

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"maps"
	"slices"
	"strconv"
	"strings"
)

const plistHeader = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
`

// plistEncode writes value as an XML property list, the way PropertyListSerialization
// does with the .xml format: dictionary keys sorted, tab indentation. Supported
// values are string, bool, int, map[string]any and []any (and the typed []string).
// Lock: none (pure).
func plistEncode(value any) ([]byte, error) {
	var b bytes.Buffer
	b.WriteString(plistHeader)
	if err := plistWrite(&b, value, 0); err != nil {
		return nil, err
	}
	b.WriteString("</plist>\n")
	return b.Bytes(), nil
}

// plistWrite writes one value at depth indentation.
// Lock: none (pure).
func plistWrite(b *bytes.Buffer, value any, depth int) error {
	indent := strings.Repeat("\t", depth)
	switch v := value.(type) {
	case string:
		b.WriteString(indent + "<string>" + plistEscape(v) + "</string>\n")
	case bool:
		if v {
			b.WriteString(indent + "<true/>\n")
		} else {
			b.WriteString(indent + "<false/>\n")
		}
	case int:
		b.WriteString(indent + "<integer>" + strconv.Itoa(v) + "</integer>\n")
	case map[string]any:
		if len(v) == 0 {
			b.WriteString(indent + "<dict/>\n")
			return nil
		}
		b.WriteString(indent + "<dict>\n")
		keys := slices.Sorted(maps.Keys(v))
		for _, key := range keys {
			b.WriteString(indent + "\t<key>" + plistEscape(key) + "</key>\n")
			if err := plistWrite(b, v[key], depth+1); err != nil {
				return err
			}
		}
		b.WriteString(indent + "</dict>\n")
	case []any:
		if len(v) == 0 {
			b.WriteString(indent + "<array/>\n")
			return nil
		}
		b.WriteString(indent + "<array>\n")
		for _, item := range v {
			if err := plistWrite(b, item, depth+1); err != nil {
				return err
			}
		}
		b.WriteString(indent + "</array>\n")
	case []string:
		items := make([]any, 0, len(v))
		for _, item := range v {
			items = append(items, item)
		}
		return plistWrite(b, items, depth)
	default:
		return fmt.Errorf("plist: unsupported value %T", value)
	}
	return nil
}

// plistEscape escapes the three characters a property list's text cannot hold raw.
// Lock: none (pure).
func plistEscape(text string) string {
	text = strings.ReplaceAll(text, "&", "&amp;")
	text = strings.ReplaceAll(text, "<", "&lt;")
	return strings.ReplaceAll(text, ">", "&gt;")
}

// plistDecode reads an XML property list into map[string]any, []any, string,
// bool, int64, float64 or []byte values. Dates and data come back as their text.
// Lock: none (pure).
func plistDecode(data []byte) (any, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			return nil, fmt.Errorf("plist: no root element")
		}
		if err != nil {
			return nil, err
		}
		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		if start.Name.Local != "plist" {
			return nil, fmt.Errorf("plist: root element is %s", start.Name.Local)
		}
		break
	}
	for {
		token, err := decoder.Token()
		if err != nil {
			return nil, fmt.Errorf("plist: missing value: %v", err)
		}
		if start, ok := token.(xml.StartElement); ok {
			return plistReadValue(decoder, start)
		}
	}
}

// plistReadValue reads the value whose start element is start, through its end element.
// Lock: none (pure).
func plistReadValue(d *xml.Decoder, start xml.StartElement) (any, error) {
	switch start.Name.Local {
	case "dict":
		result := map[string]any{}
		var key *string
		for {
			token, err := d.Token()
			if err != nil {
				return nil, err
			}
			switch t := token.(type) {
			case xml.StartElement:
				if t.Name.Local == "key" {
					text, err := plistText(d)
					if err != nil {
						return nil, err
					}
					key = &text
					continue
				}
				if key == nil {
					return nil, fmt.Errorf("plist: dict value without a key")
				}
				value, err := plistReadValue(d, t)
				if err != nil {
					return nil, err
				}
				result[*key] = value
				key = nil
			case xml.EndElement:
				return result, nil
			}
		}
	case "array":
		result := []any{}
		for {
			token, err := d.Token()
			if err != nil {
				return nil, err
			}
			switch t := token.(type) {
			case xml.StartElement:
				value, err := plistReadValue(d, t)
				if err != nil {
					return nil, err
				}
				result = append(result, value)
			case xml.EndElement:
				return result, nil
			}
		}
	case "true", "false":
		if err := d.Skip(); err != nil {
			return nil, err
		}
		return start.Name.Local == "true", nil
	case "integer":
		text, err := plistText(d)
		if err != nil {
			return nil, err
		}
		n, err := strconv.ParseInt(strings.TrimSpace(text), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("plist: bad integer %q", text)
		}
		return n, nil
	case "real":
		text, err := plistText(d)
		if err != nil {
			return nil, err
		}
		f, err := strconv.ParseFloat(strings.TrimSpace(text), 64)
		if err != nil {
			return nil, fmt.Errorf("plist: bad real %q", text)
		}
		return f, nil
	case "data":
		text, err := plistText(d)
		if err != nil {
			return nil, err
		}
		return []byte(strings.Join(strings.Fields(text), "")), nil
	case "string", "date":
		return plistText(d)
	}
	return nil, fmt.Errorf("plist: unknown element %s", start.Name.Local)
}

// plistText reads the character data of the element just opened, through its end element.
// Lock: none (pure).
func plistText(d *xml.Decoder) (string, error) {
	var text strings.Builder
	for {
		token, err := d.Token()
		if err != nil {
			return "", err
		}
		switch t := token.(type) {
		case xml.CharData:
			text.Write(t)
		case xml.EndElement:
			return text.String(), nil
		case xml.StartElement:
			return "", fmt.Errorf("plist: unexpected element %s", t.Name.Local)
		}
	}
}
