// Package indexingworkload builds a deterministic, service-free indexing corpus.
package indexingworkload

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Write creates typed linked notes and Go files. Both semantic lanes use the
// deterministic provider so measurements exclude network and provider variance.
func Write(tb testing.TB, root string, notes, code int) {
	tb.Helper()
	write := func(path, content string) {
		tb.Helper()
		abs := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			tb.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(content), 0o600); err != nil {
			tb.Fatal(err)
		}
	}
	write(".rhizome/config.yml", "notes:\n  includes: [\"notes/*.md\"]\n  links: both\ncode:\n  enabled: true\n  goRoots: [src]\nnoteEmbeddings:\n  enabled: true\n  provider: test\n  model: test\n  dimensions: 8\n  maxConcurrency: 4\ncodeEmbeddings:\n  enabled: true\n  provider: test\n  model: test\n  dimensions: 8\n  maxConcurrency: 4\n")
	write(".rhizome/ontology/schema.graphql", "type Project @node(paths: [\"notes/*.md\"]) { name: String! }\n")
	for i := range notes {
		write(fmt.Sprintf("notes/project-%04d.md", i), Note(i, notes, "Initial"))
	}
	for i := range code {
		write(fmt.Sprintf("src/unit_%04d.go", i), fmt.Sprintf("package workload\n\n// Unit%d computes a stable fixture result.\nfunc Unit%d(value int) int { return value + %d }\n", i, i, i))
	}
}

// Note keeps content size constant when changing a marker.
func Note(index, count int, marker string) string {
	return fmt.Sprintf("---\ntype: Project\nname: Project %04d %s\n---\n# Project %04d %s\n\nSee [[project-%04d]].\n\n## Status\n\n%s\n", index, marker, index, marker, (index+1)%count,
		strings.Repeat("The project tracks ownership, delivery, and structural indexing independently of semantic availability. ", 12))
}
