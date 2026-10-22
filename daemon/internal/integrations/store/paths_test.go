package store

import (
	"path/filepath"
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/core"
)

// TestStandardPathsUsesTheDataDirOverride checks BELTLINE_DATA_DIR wins over the support folder.
func TestStandardPathsUsesTheDataDirOverride(t *testing.T) {
	t.Parallel()
	got := StandardPaths(envOf(map[string]string{"BELTLINE_DATA_DIR": "/custom/folder"}), t.TempDir())
	if got.Root != "/custom/folder" {
		t.Fatalf("root %q", got.Root)
	}
}

// TestStandardPathsMakesTheOverrideAbsolute checks that a relative BELTLINE_DATA_DIR names the absolute folder it stands for.
func TestStandardPathsMakesTheOverrideAbsolute(t *testing.T) {
	t.Parallel()
	got := StandardPaths(envOf(map[string]string{"BELTLINE_DATA_DIR": "relative/folder"}), t.TempDir())
	want, err := filepath.Abs("relative/folder")
	if err != nil {
		t.Fatal(err)
	}
	if got.Root != want || !filepath.IsAbs(got.Root) {
		t.Fatalf("root %q, want %q", got.Root, want)
	}
}

// TestStandardPathsIgnoresAnEmptyOverride checks an empty BELTLINE_DATA_DIR is no override.
func TestStandardPathsIgnoresAnEmptyOverride(t *testing.T) {
	t.Parallel()
	support := t.TempDir()
	got := StandardPaths(envOf(map[string]string{"BELTLINE_DATA_DIR": ""}), support)
	if got.Root != filepath.Join(support, "Beltline") {
		t.Fatalf("root %q", got.Root)
	}
}

// TestPathsNameTheLibraryFolders checks the layout the data folder keeps.
func TestPathsNameTheLibraryFolders(t *testing.T) {
	t.Parallel()
	p := Paths{Root: "/data"}
	cases := map[string]struct{ got, want string }{
		"belts":        {p.Belts(), "/data/belts"},
		"agents":       {p.Agents(), "/data/agents"},
		"runs":         {p.Runs(), "/data/runs"},
		"settings":     {p.Settings(), "/data/settings.json"},
		"erasures":     {p.Erasures(), "/data/erasures"},
		"afkDecisions": {p.AfkDecisions(), "/data/afk/decisions.json"},
		"libraryRepos": {p.LibraryRepos(), "/data/library-repos"},
		"clone":        {p.Clone("acme/web"), "/data/repos/acme/web"},
	}
	for name, c := range cases {
		if c.got != c.want {
			t.Errorf("%s: got %s, want %s", name, c.got, c.want)
		}
	}
}

// TestRunFoldersFollowTheRunsKey checks where a run's folders are for an issue run.
func TestRunFoldersFollowTheRunsKey(t *testing.T) {
	t.Parallel()
	p := Paths{Root: "/data"}
	run := core.Run{BeltID: "issues", Subject: core.Subject{Kind: core.SubjectKindIssue, Repo: "acme/web", Number: 7}}
	if got := p.RunFolder(run); got != "/data/runs/issues/acme/web/7" {
		t.Fatalf("run folder %s", got)
	}
	if got := p.WorkFolder(run); got != "/data/work/issues/acme/web/7" {
		t.Fatalf("work folder %s", got)
	}
}

// TestWorkFolderLegUsesItsOwnFolder checks each leg of a route has a folder of
// its own, and the main leg shares the run's folder.
func TestWorkFolderLegUsesItsOwnFolder(t *testing.T) {
	t.Parallel()
	p := Paths{Root: "/data"}
	run := core.Run{BeltID: "issues", Subject: core.Subject{Kind: core.SubjectKindIssue, Repo: "acme/web", Number: 7}}
	main := "main"
	leg := "r:2"
	if got := p.WorkFolderLeg(run, nil); got != p.WorkFolder(run) {
		t.Fatalf("nil leg: %s", got)
	}
	if got := p.WorkFolderLeg(run, &main); got != p.WorkFolder(run) {
		t.Fatalf("main leg: %s", got)
	}
	if got := p.WorkFolderLeg(run, &leg); got != filepath.Join(p.WorkFolder(run), "2") {
		t.Fatalf("route leg: %s", got)
	}
}

func TestSupportFolderIsApplicationSupportOnMacOSAndXDGDataHomeOnLinux(t *testing.T) {
	t.Parallel()
	none := func(string) string { return "" }
	xdg := func(key string) string {
		if key == "XDG_DATA_HOME" {
			return "/data/home"
		}
		return ""
	}
	relative := func(string) string { return "relative/data" }
	for _, c := range []struct {
		goos string
		env  func(string) string
		want string
	}{
		{"darwin", xdg, "/Users/u/Library/Application Support"},
		{"linux", none, "/home/u/.local/share"},
		{"linux", xdg, "/data/home"},
		{"linux", relative, "/home/u/.local/share"},
	} {
		home := "/home/u"
		if c.goos == "darwin" {
			home = "/Users/u"
		}
		if got := SupportFolderFor(c.goos, home, c.env); got != c.want {
			t.Errorf("SupportFolderFor(%s) = %q, want %q", c.goos, got, c.want)
		}
	}
}
