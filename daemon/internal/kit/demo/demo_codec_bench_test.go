package demo

import (
	"testing"
	"time"

	"encoding/json"
	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// benchDemoRuns writes a fresh demo and returns the runs it saved, as the
// coordinator loads them: the codec benchmarks encode and decode these.
func benchDemoRuns(b *testing.B) []core.Run {
	b.Helper()
	now := time.Date(2026, 10, 13, 8, 0, 0, 0, time.UTC)
	setup, err := Make(b.TempDir(), now, func(string) string { return "" })
	if err != nil {
		b.Fatal(err)
	}
	runs := setup.Storage.LoadRuns()
	if len(runs) == 0 {
		b.Fatal("the demo saved no runs")
	}
	return runs
}

// BenchmarkDemoRunsEncode encodes every demo run in the storage form, as
// Storage saves a run.json.
func BenchmarkDemoRunsEncode(b *testing.B) {
	runs := benchDemoRuns(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, run := range runs {
			if _, err := jsonx.MarshalStorage(run); err != nil {
				b.Fatal(err)
			}
		}
	}
}

// BenchmarkDemoRunsDecode decodes every demo run from its storage bytes, as
// Storage reads a run.json.
func BenchmarkDemoRunsDecode(b *testing.B) {
	runs := benchDemoRuns(b)
	encoded := make([][]byte, len(runs))
	for i, run := range runs {
		data, err := jsonx.MarshalStorage(run)
		if err != nil {
			b.Fatal(err)
		}
		encoded[i] = data
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, data := range encoded {
			var run core.Run
			if err := json.Unmarshal(data, &run); err != nil {
				b.Fatal(err)
			}
		}
	}
}

// BenchmarkDemoRunsClone deep-copies every demo run, as the coordinator copies a run out of the
// book for a view, a command or a tick pass.
func BenchmarkDemoRunsClone(b *testing.B) {
	runs := benchDemoRuns(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, run := range runs {
			_ = run.Clone()
		}
	}
}
