package jsonx

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// dateLayout is the wire and data form of a date: UTC, whole seconds.
const dateLayout = "2006-01-02T15:04:05Z"

// Date is a time written as ISO 8601 in UTC with whole seconds, for example
// "2026-10-07T06:36:40Z". Reading accepts any RFC 3339 time, with an offset
// or fractional seconds, so older files and other tools' output still decode.
type Date struct{ time.Time }

// MarshalJSON writes the date as a string in the dateLayout form.
func (d Date) MarshalJSON() ([]byte, error) {
	t := d.UTC()
	if y := t.Year(); y < 0 || y > 9999 {
		return nil, fmt.Errorf("jsonx: date %s is outside the years 0 to 9999", t.Format(time.RFC3339))
	}
	return json.Marshal(t.Format(dateLayout))
}

// UnmarshalJSON reads a date string. null leaves the date unchanged.
func (d *Date) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		return nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return fmt.Errorf("jsonx: %q is not an ISO 8601 date: %w", strings.TrimSpace(s), err)
	}
	d.Time = t
	return nil
}
