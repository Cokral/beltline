package launch

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// BenchmarkDemoTick times one coordinator tick over the demo folder: the two-second loop's
// work with the demo's seeded runs in flight. Run it with -cpuprofile to see where a tick goes.
// The folder's times are moved an hour back first, as a daemon's would be some seconds after its
// last write, so caches that keep a folder's names while it is unchanged hold as they do there.
func BenchmarkDemoTick(b *testing.B) {
	root := b.TempDir()
	c, err := LaunchSetupDemo(root, func(string, string) {}, nil)
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()
	c.Tick(ctx)
	settleTimes(b, root)
	c.Tick(ctx)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.Tick(ctx)
	}
}

// settleTimes moves every entry under root an hour back.
func settleTimes(b *testing.B, root string) {
	b.Helper()
	old := time.Now().Add(-time.Hour)
	err := filepath.WalkDir(root, func(path string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		return os.Chtimes(path, old, old)
	})
	if err != nil {
		b.Fatal(err)
	}
}
