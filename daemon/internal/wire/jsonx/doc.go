// Package jsonx holds the JSON conventions shared by the wire and the data
// folder: dates as ISO 8601 in UTC, object keys sorted, nil slices and maps
// written as [] and {}, and the pretty form of data files. The encoder builds
// one plan per type from its struct tags and reuses it, so it is not a
// round trip through encoding/json.
package jsonx
