// Contract tests of the artifact and desktop endpoints: the artifact link is issued and the desktop
// commands answer.

package servertest

import (
	"bytes"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"encoding/json"
	"github.com/robbertmanders/beltline/daemon/internal/testkit"
	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// TestArtifactAndDesktopEndpointsHaveProductionContractGoldens
func TestArtifactAndDesktopEndpointsHaveProductionContractGoldens(t *testing.T) {
	t.Parallel()
	f := Start(t, Options{})
	runsBefore := f.Coordinator.ReadRuns()
	settingsBefore := f.Coordinator.ReadSettings()

	issue := f.IssueArtifactLink(t, f.ArtifactName(), ContractToken, "", "")
	if issue.Status != http.StatusOK {
		t.Fatalf("artifact link: HTTP %d", issue.Status)
	}
	if got := issue.Header.Get("Content-Type"); got != "application/json" {
		t.Fatalf("artifact link Content-Type %q", got)
	}
	if got := issue.Header.Get("Cache-Control"); got != "no-store" {
		t.Fatalf("artifact link Cache-Control %q", got)
	}
	if got := issue.Header.Get("Set-Cookie"); got != "" {
		t.Fatalf("artifact link sets a cookie: %q", got)
	}
	CheckGolden(t, "artifactLink", "artifact-link.json", issue, f.Root)
	var link struct {
		URL       string     `json:"url"`
		ExpiresAt jsonx.Date `json:"expiresAt"`
	}
	if err := json.Unmarshal(issue.Body, &link); err != nil {
		t.Fatalf("artifact link: %v", err)
	}
	if !link.ExpiresAt.Equal(ContractNow.Add(time.Minute)) {
		t.Fatalf("expiresAt %v, want one minute after the contract clock", link.ExpiresAt.Time)
	}
	if !strings.HasPrefix(link.URL, "/artifacts/") {
		t.Fatalf("url %q", link.URL)
	}
	if strings.Contains(link.URL, ContractToken) {
		t.Fatal("the link exposes the token")
	}

	artifact := f.Do(Request{Method: "GET", Path: link.URL})
	if artifact.Status != http.StatusOK {
		t.Fatalf("artifact: HTTP %d", artifact.Status)
	}
	headers := map[string]string{
		"Content-Type":            "text/html; charset=utf-8",
		"Content-Security-Policy": "sandbox allow-scripts",
		"X-Content-Type-Options":  "nosniff",
		"Cache-Control":           "no-store",
		"Referrer-Policy":         "no-referrer",
	}
	for name, want := range headers {
		if got := artifact.Header.Get(name); got != want {
			t.Fatalf("artifact %s %q, want %q", name, got, want)
		}
	}
	for _, name := range []string{"Set-Cookie", "Location"} {
		if got := artifact.Header.Get(name); got != "" {
			t.Fatalf("artifact sets %s: %q", name, got)
		}
	}
	want, err := os.ReadFile(filepath.Join(testkit.Testdata(t, "golden"), "artifact.html"))
	if err != nil {
		t.Fatalf("artifact golden: %v", err)
	}
	if !bytes.Equal(artifact.Body, want) {
		t.Fatalf("artifact body %q, want the golden", artifact.Body)
	}

	for _, desktopCase := range desktopCases {
		f.Exercise(t, desktopCase)
	}
	if n := len(f.Commands.Calls()); n != 6 {
		t.Fatalf("desktop commands ran %d times, want 6", n)
	}
	if !reflect.DeepEqual(f.Coordinator.ReadRuns(), runsBefore) {
		t.Fatal("the desktop commands changed the runs")
	}
	if !reflect.DeepEqual(f.Coordinator.ReadSettings(), settingsBefore) {
		t.Fatal("the desktop commands changed the settings")
	}
}
