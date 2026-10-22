package server

import (
	"context"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/store"
	"github.com/robbertmanders/beltline/daemon/internal/kit/coordinator"
	"github.com/robbertmanders/beltline/daemon/internal/kit/demo"
	"github.com/robbertmanders/beltline/daemon/internal/kit/launch"
	"github.com/robbertmanders/beltline/daemon/internal/server/routes"
)

// benchScaledCopies is how many copies of each finished demo run the scaled read benchmarks add,
// as launch's scaled benchmarks do: a data folder some months into daily use.
const benchScaledCopies = 40

// benchScaledRouter is the production router over a demo coordinator whose folder also holds
// benchScaledCopies copies of every finished demo run, and the id of an active run.
func benchScaledRouter(b *testing.B) (http.Handler, *coordinator.Coordinator, string) {
	b.Helper()
	root := b.TempDir()
	setup, err := demo.Make(root, time.Now(), func(string) string { return "" })
	if err != nil {
		b.Fatal(err)
	}
	storage := store.NewStorage(store.Paths{Root: root})
	active := ""
	for _, run := range setup.Storage.LoadRuns() {
		if run.State.IsActive() {
			if active == "" || len(run.Attempts) > 0 {
				active = run.ID
			}
			continue
		}
		for n := 1; n <= benchScaledCopies; n++ {
			copied := run.Clone()
			copied.Subject.Number += 1000 * n
			if copied.Subject.Key != "" {
				copied.Subject.Key += "-" + strconv.Itoa(n)
			}
			copied.ID = core.RunID(copied.BeltID, copied.Subject)
			if err := storage.SaveRun(copied); err != nil {
				b.Fatal(err)
			}
		}
	}
	c, err := launch.LaunchSetupDemo(root, func(string, string) {}, nil)
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()
	c.Tick(ctx)
	old := time.Now().Add(-time.Hour)
	err = filepath.WalkDir(root, func(path string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		return os.Chtimes(path, old, old)
	})
	if err != nil {
		b.Fatal(err)
	}
	c.Tick(ctx)
	signer := routes.ArtifactLinkSigner{Key: testSigningKey, Clock: time.Now}
	handler := ServerApplicationRouter(testPort, testToken, "dev", "", c, signer, routes.Dependencies{}, nil)
	return handler, c, active
}

// BenchmarkScaledReads times each read the web app makes, through the production router, over the
// scaled folder. Run one with -bench 'ScaledReads/<name>' -cpuprofile to see where it goes.
func BenchmarkScaledReads(b *testing.B) {
	handler, c, active := benchScaledRouter(b)
	b.Logf("%d runs, active %s", len(c.Runs()), active)
	escaped := url.PathEscape(active)
	attempt := ""
	if run := c.ReadRun(active); run != nil && len(run.Attempts) > 0 {
		attempt = url.PathEscape(run.Attempts[len(run.Attempts)-1].ID)
	}
	paths := []struct{ name, path string }{
		{"state", "/api/v1/state"},
		{"overview", "/api/v1/views/overview"},
		{"work", "/api/v1/views/work"},
		{"runview", "/api/v1/views/run/" + escaped},
		{"sessions", "/api/v1/views/run/" + escaped + "/sessions"},
		{"run", "/api/v1/runs/" + escaped},
		{"output", "/api/v1/runs/" + escaped + "/attempts/" + attempt + "/output"},
		{"artifacts", "/api/v1/runs/" + escaped + "/attempts/" + attempt + "/artifacts"},
		{"runs", "/api/v1/runs"},
		{"waiting", "/api/v1/waiting"},
		{"afk", "/api/v1/afk/decisions"},
		{"belts", "/api/v1/belts"},
		{"agents", "/api/v1/agents"},
		{"settings", "/api/v1/settings"},
		{"libraryrepos", "/api/v1/library/repos"},
	}
	for _, p := range paths {
		b.Run(p.name, func(b *testing.B) {
			b.ReportAllocs()
			size := 0
			for i := 0; i < b.N; i++ {
				req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:"+strconv.Itoa(testPort)+p.path, nil)
				req.Host = "127.0.0.1:" + strconv.Itoa(testPort)
				req.Header.Set("Authorization", "Bearer "+testToken)
				rec := httptest.NewRecorder()
				handler.ServeHTTP(rec, req)
				if rec.Code != http.StatusOK {
					b.Fatalf("%s: %d %s", p.path, rec.Code, rec.Body.String())
				}
				size = rec.Body.Len()
			}
			b.ReportMetric(float64(size), "bytes/resp")
		})
	}
}
