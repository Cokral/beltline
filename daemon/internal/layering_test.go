// Package internal_test checks the layering:
// core imports only jsonx and the standard library, integrations add core,
// kit adds integrations, and only server and the command see the HTTP,
// WebSocket and PTY dependencies.
package internal_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

const module = "github.com/robbertmanders/beltline/daemon/"

// allowed lists, per layer prefix, the module packages its files may import.
var allowed = map[string][]string{
	"internal/wire/":         {},
	"internal/core/":         {"internal/wire/", "internal/core/"},
	"internal/integrations/": {"internal/wire/", "internal/core/", "internal/integrations/"},
	"internal/kit/":          {"internal/wire/", "internal/core/", "internal/integrations/", "internal/kit/"},
	"internal/server":        {"internal/wire/", "internal/core/", "internal/integrations/", "internal/kit/", "internal/server"},
}

// external lists the third-party modules only the server and command may use.
var external = []string{"github.com/coder/websocket", "github.com/creack/pty"}

func TestLayering(t *testing.T) {
	t.Parallel()
	root := ".."
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		layer := ""
		for prefix := range allowed {
			if strings.HasPrefix(rel, prefix) {
				layer = prefix
			}
		}
		// internal/server/servertest is the contract harness, which only test files import; it reads testkit's golden folders.
		if strings.HasPrefix(rel, "internal/testkit/") ||
			strings.HasPrefix(rel, "internal/server/servertest/") || layer == "" {
			return nil
		}
		test := strings.HasSuffix(rel, "_test.go")
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, spec := range file.Imports {
			imp := strings.Trim(spec.Path.Value, `"`)
			if strings.HasPrefix(imp, module) {
				target := strings.TrimPrefix(imp, module)
				if test && strings.HasPrefix(target, "internal/testkit") {
					continue
				}
				ok := false
				// target+"/" so that importing a layer's root package (internal/core)
				// matches its "internal/core/" entry, as importing below it does.
				for _, a := range allowed[layer] {
					ok = ok || strings.HasPrefix(target+"/", a)
				}
				if !ok {
					t.Errorf("%s imports %s, which layer %s may not", rel, target, layer)
				}
				continue
			}
			for _, e := range external {
				if strings.HasPrefix(imp, e) && layer != "internal/server" {
					t.Errorf("%s imports %s; only internal/server may", rel, imp)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
