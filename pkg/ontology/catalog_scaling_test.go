package ontology

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// BenchmarkCatalogResolverScaling isolates catalog construction from schema
// compilation, Markdown parsing, and initial root projection.
func BenchmarkCatalogResolverScaling(b *testing.B) {
	root := b.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	if err := os.MkdirAll(dir, 0755); err != nil {
		b.Fatal(err)
	}
	sdl := `type Person @node(paths: ["people/*.md"]) { name: String! }
type ActionItem implements Section @node(locator: EMBEDDED) @source(shape: CHECKBOX_ITEM, marker: "#action-item", paths: ["notes/**/*.md"]) {
  done: Boolean! @field(sourceKind: CHECKBOX)
  assignee: Person @link
}`
	if err := os.WriteFile(filepath.Join(dir, "schema.graphql"), []byte(sdl), 0644); err != nil {
		b.Fatal(err)
	}
	schema, err := LoadSchema(root)
	if err != nil {
		b.Fatal(err)
	}
	for _, count := range []int{10, 100, 500} {
		b.Run(fmt.Sprintf("descendants=%d", count), func(b *testing.B) {
			var content strings.Builder
			content.WriteString("# Scratch\n\n")
			for i := 0; i < count; i++ {
				fmt.Fprintf(&content, "- [ ] Task %d #action-item\n  assignee:: [[people/Alice]]\n", i)
			}
			snapshot, err := BuildDocumentSnapshot("notes/scratch.md", content.String(), time.Time{})
			if err != nil {
				b.Fatal(err)
			}
			projection, err := ProjectNodeFromSnapshot(snapshot, schema, NodeRef{NotePath: snapshot.NotePath, Kind: NodeKindNote})
			if err != nil {
				b.Fatal(err)
			}
			model, err := BuildIntelOntologyNodeReadModel(schema, projection, 1)
			if err != nil {
				b.Fatal(err)
			}
			children := 0
			for _, node := range model.Nodes {
				if node.TypeName == "ActionItem" {
					children++
				}
			}
			if children != count {
				b.Fatalf("got %d ActionItems, want %d", children, count)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := BuildIntelOntologyNodeReadModel(schema, projection, 1); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
