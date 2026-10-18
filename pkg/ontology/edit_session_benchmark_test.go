package ontology

import (
	"context"
	"fmt"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

type editBenchmarkReader struct {
	obsidian.NoteReader
	reads int
}

func (r *editBenchmarkReader) GetContents(vault obsidian.VaultDefinition, note string) (string, error) {
	r.reads++
	return r.NoteReader.GetContents(vault, note)
}

func BenchmarkEditSessionFirstCurrentPreview(b *testing.B) {
	root := b.TempDir()
	schemaDir := filepath.Join(root, ".rhizome", "ontology")
	if err := os.MkdirAll(schemaDir, 0755); err != nil {
		b.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".rhizome", "config.yml"), []byte("notes:\n  includes: [\"**/*.md\"]\n"), 0644); err != nil {
		b.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(schemaDir, "schema.graphql"), []byte(`type Spec @node(paths: ["*.md"]) { summary: String }`), 0644); err != nil {
		b.Fatal(err)
	}
	source := "---\nsummary: Original\n---\n\n# Spec\n\n" + strings.Repeat("Paragraph with narrative text.\n\n", 100)
	if err := os.WriteFile(filepath.Join(root, "note.md"), []byte(source), 0644); err != nil {
		b.Fatal(err)
	}
	schema, err := ontoschema.LoadSchema(root)
	if err != nil {
		b.Fatal(err)
	}
	reader := &editBenchmarkReader{NoteReader: &obsidian.Note{}}
	vault := obsidian.VaultDefinition{Path: root}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		session := NewEditSession(vault, reader, schema)
		if err := session.SetScalarField(NodeRef{NotePath: "note.md", Kind: NodeKindNote}, "summary", "Updated"); err != nil {
			b.Fatal(err)
		}
		plan, conflicts, err := session.PreviewCurrent(context.Background())
		if err != nil || len(conflicts) != 0 || len(plan.Files) != 1 || !strings.Contains(plan.Files[0].UpdatedContentPreview, "summary: Updated") {
			b.Fatalf("preview failed: %v %v", err, conflicts)
		}
	}
	b.StopTimer()
	b.ReportMetric(float64(reader.reads)/float64(b.N), "reads/op")
}

func BenchmarkEditSessionMultiFilePreview(b *testing.B) {
	root := b.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".rhizome", "ontology"), 0755); err != nil {
		b.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".rhizome", "config.yml"), []byte("notes:\n  includes: [\"**/*.md\"]\n"), 0644); err != nil {
		b.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".rhizome", "ontology", "schema.graphql"), []byte(`type Spec @node(paths: ["*.md"]) { summary: String }`), 0644); err != nil {
		b.Fatal(err)
	}
	const files = 16
	source := "---\nsummary: Original\n---\n\n# Spec\n\n" + strings.Repeat("Paragraph with narrative text.\n\n", 100)
	for index := range files {
		if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("note-%d.md", index)), []byte(source), 0644); err != nil {
			b.Fatal(err)
		}
	}
	schema, err := ontoschema.LoadSchema(root)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		session := NewEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema)
		for index := range files {
			if err := session.SetScalarField(NodeRef{NotePath: fmt.Sprintf("note-%d.md", index), Kind: NodeKindNote}, "summary", "Updated"); err != nil {
				b.Fatal(err)
			}
		}
		plan, conflicts, err := session.PreviewCurrent(context.Background())
		if err != nil || len(conflicts) != 0 || len(plan.Files) != files {
			b.Fatalf("preview failed: %v %v", err, conflicts)
		}
		for _, file := range plan.Files {
			if !strings.Contains(file.UpdatedContentPreview, "summary: Updated") {
				b.Fatal("missing updated source")
			}
		}
	}
	b.StopTimer()
	b.ReportMetric(files, "files/op")
}
