package skilloverlay_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestSkillCompositionImportBoundaries(t *testing.T) {
	for _, rule := range []struct{ root, forbidden string }{
		{".", "github.com/atomicobject/rhizome/pkg/app/"},
		{"../validate", "github.com/atomicobject/rhizome/pkg/app/"},
		{"../app/bootstrap", "github.com/atomicobject/rhizome/pkg/app/cli/init"},
	} {
		err := filepath.WalkDir(rule.root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
			if err != nil {
				return err
			}
			for _, spec := range file.Imports {
				imported, err := strconv.Unquote(spec.Path.Value)
				if err != nil {
					return err
				}
				if strings.HasPrefix(imported, rule.forbidden) {
					t.Errorf("%s imports application package %s", path, imported)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
