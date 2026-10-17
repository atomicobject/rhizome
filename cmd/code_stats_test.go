package cmd

import (
	"bytes"
	"encoding/json"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCodeStatsExcludeCatalogFilter(t *testing.T) {

	// Write a minimal catalog: `empty` is a language-builtin, should be excluded;
	// `missingFunc` lives under real-gap, should NOT be excluded.
	catalog := `# Test catalog
## language-builtin
- empty — language construct, dominates the unresolved list

## real-gap
- ` + "`missingFunc`" + ` — should still show in unresolved
`
	catalogPath := filepath.Join(t.TempDir(), "catalog.md")
	if err := os.WriteFile(catalogPath, []byte(catalog), 0o644); err != nil {
		t.Fatal(err)
	}

	excludeSet, err := loadExcludeCatalog(catalogPath)
	if err != nil {
		t.Fatalf("load catalog: %v", err)
	}
	if !excludeSet["empty"] {
		t.Errorf("expected `empty` in exclude set, got %v", excludeSet)
	}
	if excludeSet["missingFunc"] {
		t.Errorf("missingFunc was under real-gap; should NOT be excluded")
	}

}

func TestLoadExcludeCatalogEmpty(t *testing.T) {
	set, err := loadExcludeCatalog("")
	if err != nil {
		t.Fatal(err)
	}
	if set != nil {
		t.Errorf("expected nil exclude set for empty path, got %v", set)
	}
}
func TestReferenceClassificationTextSplitsRelationKinds(t *testing.T) {
	stats := &CodeStats{CodeStats: semdb.CodeStats{
		Languages: map[string]semdb.LanguageCounts{"ts": {}},
		Edges:     map[string]semdb.EdgeCounts{},
		ReferenceClassifications: map[string]semdb.ReferenceClassificationStats{
			"ts": {
				ByKind: map[string]semdb.ReferenceKindClassificationStats{
					"calls": {}, "type_ref": {}, "member_ref": {}, "imports": {},
				},
			},
		},
		UnresolvedCallees: map[string][]semdb.UnresolvedCallee{},
	}}
	var out bytes.Buffer
	writeTextOutput(&out, stats)
	for _, kind := range []string{"calls", "type_ref", "member_ref", "imports"} {
		if !strings.Contains(out.String(), kind) {
			t.Errorf("text output missing %q:\n%s", kind, out.String())
		}
	}
}

func TestCodeStatsOutputContract(t *testing.T) {
	stats := CodeStats{Vault: "/tmp/fixture", IndexPath: "/tmp/fixture/db", CodeStats: semdb.CodeStats{
		Languages: map[string]semdb.LanguageCounts{}, Edges: map[string]semdb.EdgeCounts{},
		ReferenceClassifications: map[string]semdb.ReferenceClassificationStats{},
		UnresolvedCallees:        map[string][]semdb.UnresolvedCallee{}, ByDirectory: map[string][]semdb.DirectoryDistribution{},
	}}
	var out bytes.Buffer
	if err := writeJSONOutput(&out, &stats); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"vault", "indexPath", "languages", "edges", "referenceClassifications", "unresolvedCallees", "byDirectory"} {
		if _, ok := got[key]; !ok {
			t.Errorf("JSON missing key %q", key)
		}
	}
}
