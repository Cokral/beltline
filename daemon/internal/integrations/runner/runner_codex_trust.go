package runner

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// CodexTrusting is a Codex command line that trusts directory for this launch
// only, so the interactive interface starts without asking whether to trust the
// folder (headless `codex exec` never asks). The `-c projects=…` override goes
// right after the bypass flag, before the prompt, and is never written to the
// user's config. arguments without the bypass flag, or nil, come back as they are.
func CodexTrusting(arguments []string, directory string) []string {
	at := slices.Index(arguments, codexBypass)
	projects := codexTrustedProjects(directory)
	if at < 0 || len(projects) == 0 {
		return arguments
	}
	entries := make([]string, 0, len(projects))
	for _, project := range projects {
		entries = append(entries, tomlBasicString(project)+`={trust_level="trusted"}`)
	}
	override := []string{"-c", "projects={" + strings.Join(entries, ",") + "}"}
	return slices.Concat(arguments[:at+1], override, arguments[at+1:])
}

// codexTrustedProjects are the folders Codex looks up the trust of: the checkout
// itself and, when it is a git worktree, the repository it belongs to, which is
// where Codex records trust for every worktree of it. Paths are resolved the way
// Codex records them (`/var` as `/private/var`).
func codexTrustedProjects(directory string) []string {
	checkout := codexCanonicalPath(directory)
	if checkout == nil {
		return nil
	}
	projects := []string{*checkout}
	if root := worktreeRepository(*checkout); root != "" && root != *checkout {
		projects = append(projects, root)
	}
	return projects
}

// worktreeRepository is the folder of the repository a git worktree's `.git` file
// points into (`gitdir: <repo>/.git/worktrees/<name>`, with its `commondir`), or
// "" when checkout is no worktree.
func worktreeRepository(checkout string) string {
	link, err := os.ReadFile(filepath.Join(checkout, ".git"))
	if err != nil {
		return ""
	}
	gitdir, ok := strings.CutPrefix(strings.TrimSpace(string(link)), "gitdir:")
	if !ok {
		return ""
	}
	gitdir = strings.TrimSpace(gitdir)
	if !filepath.IsAbs(gitdir) {
		gitdir = filepath.Join(checkout, gitdir)
	}
	common := gitdir
	if relative, err := os.ReadFile(filepath.Join(gitdir, "commondir")); err == nil {
		common = strings.TrimSpace(string(relative))
		if !filepath.IsAbs(common) {
			common = filepath.Join(gitdir, common)
		}
	}
	common = filepath.Clean(common)
	if filepath.Base(common) != ".git" {
		return ""
	}
	if resolved := codexCanonicalPath(filepath.Dir(common)); resolved != nil {
		return *resolved
	}
	return ""
}

// tomlBasicString quotes s as a TOML basic string.
func tomlBasicString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"' || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\u%04X`, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
