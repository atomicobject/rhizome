package obsidian_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestVaultPrimitivesRemainIndependentOfConfigurationAndApplications(t *testing.T) {
	const prefix = "github.com/atomicobject/rhizome/pkg/"
	allowed := map[string]bool{"markdownsyntax": true, "paths": true, "vault/ignore": true}
	for _, folder := range []string{".", "../../markdownsyntax"} {
		entries, err := os.ReadDir(folder)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
				continue
			}
			name := filepath.Join(folder, entry.Name())
			source, err := parser.ParseFile(token.NewFileSet(), name, nil, parser.ImportsOnly)
			if err != nil {
				t.Fatal(err)
			}
			for _, imp := range source.Imports {
				path, err := strconv.Unquote(imp.Path.Value)
				if err != nil {
					t.Fatal(err)
				}
				if !strings.HasPrefix(path, prefix) {
					continue
				}
				dependency := strings.TrimPrefix(path, prefix)
				if folder != "." || !allowed[dependency] {
					t.Errorf("%s imports %s; keep Markdown syntax pure and move configuration, stores, health and graph orchestration to their owners", name, path)
				}
			}
		}
	}
}
