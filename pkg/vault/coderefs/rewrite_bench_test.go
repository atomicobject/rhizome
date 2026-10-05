package coderefs

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func BenchmarkRewriteBatchSizeAdmission(b *testing.B) {
	for _, tc := range []struct {
		name string
		size int
	}{
		{name: "small4KiB", size: 4 << 10},
		{name: "oversized16MiB", size: 16 << 20},
	} {
		b.Run(tc.name, func(b *testing.B) {
			root := b.TempDir()
			if err := os.Mkdir(filepath.Join(root, "docs"), 0o755); err != nil {
				b.Fatal(err)
			}
			for _, note := range []string{"Old.md", "New.md"} {
				if err := os.WriteFile(filepath.Join(root, "docs", note), []byte("# Fixture\n"), 0o644); err != nil {
					b.Fatal(err)
				}
			}
			prefix := "package fixture\n// [[docs/Old.md]]\n"
			content := []byte(prefix + strings.Repeat(" ", tc.size-len(prefix)))
			if err := os.WriteFile(filepath.Join(root, "refs.go"), content, 0o644); err != nil {
				b.Fatal(err)
			}
			cfg := NewConfig(true, []string{"*.go"}, nil)
			forward := []RefMapping{{OldPath: "docs/Old.md", NewPath: "docs/New.md"}}
			reverse := []RefMapping{{OldPath: "docs/New.md", NewPath: "docs/Old.md"}}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				mappings := forward
				want := RewriteResult{}
				if tc.size <= MaxFileSizeBytes {
					// Alternate real renames so each measured small-file call
					// rewrites a matching reference without fixture-reset work.
					if i%2 != 0 {
						mappings = reverse
					}
					want = RewriteResult{FilesUpdated: 1, RefsUpdated: 1}
				}
				result, err := RewriteBatch(root, cfg, mappings)
				if err != nil || result != want {
					b.Fatalf("rewrite = %+v, %v; want %+v", result, err, want)
				}
			}
		})
	}
}

func BenchmarkRewriteBatchManyMappings(b *testing.B) {
	for _, matches := range []bool{true, false} {
		name := "matched"
		if !matches {
			name = "unmatched"
		}
		b.Run(name, func(b *testing.B) {
			var content strings.Builder
			content.WriteString("package fixture\n")
			refs := 0
			for i := 0; content.Len() < 1<<20; i++ {
				n := i % 100
				fmt.Fprintf(&content, "// [[docs/Note%03d.md]] [details](docs/Note%03d.md) @docs/Note%03d\n", n, n, n)
				refs += 3
			}
			raw := []byte(content.String())
			dir := b.TempDir()
			file := filepath.Join(dir, "refs.go")
			cfg := NewConfig(true, []string{"*.go"}, nil)
			mappings := make([]RefMapping, 100)
			for i := range mappings {
				old := fmt.Sprintf("docs/Note%03d.md", i)
				if !matches {
					old = fmt.Sprintf("docs/Absent%03d.md", i)
				}
				mappings[i] = RefMapping{OldPath: old, NewPath: fmt.Sprintf("docs/Renamed%03d.md", i)}
			}
			want := RewriteResult{}
			if matches {
				want = RewriteResult{FilesUpdated: 1, RefsUpdated: refs}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				if err := os.WriteFile(file, raw, 0o600); err != nil {
					b.Fatal(err)
				}
				b.StartTimer()
				result, err := RewriteBatch(dir, cfg, mappings)
				if err != nil || result != want {
					b.Fatalf("rewrite = %+v, %v; want %+v", result, err, want)
				}
			}
		})
	}
}

func BenchmarkRewriteBatchMentions(b *testing.B) {
	for _, tc := range []struct {
		name, old, new string
	}{
		{name: "ordinary4KiB", old: "Old", new: "New"},
		{name: "longPath4KiB", old: strings.Repeat("old-folder/", 8) + "Old", new: strings.Repeat("new-folder/", 8) + "New"},
	} {
		b.Run(tc.name, func(b *testing.B) {
			root := b.TempDir()
			prefix := "package fixture\n// @" + tc.old + ",\n"
			content := []byte(prefix + strings.Repeat(" ", 4<<10-len(prefix)))
			if err := os.WriteFile(filepath.Join(root, "refs.go"), content, 0o644); err != nil {
				b.Fatal(err)
			}
			cfg := NewConfig(true, []string{"*.go"}, nil)
			forward := []RefMapping{{OldPath: tc.old + ".md", NewPath: tc.new + ".md"}}
			reverse := []RefMapping{{OldPath: tc.new + ".md", NewPath: tc.old + ".md"}}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				mappings := forward
				if i%2 != 0 {
					mappings = reverse
				}
				result, err := RewriteBatch(root, cfg, mappings)
				want := RewriteResult{FilesUpdated: 1, RefsUpdated: 1}
				if err != nil || result != want {
					b.Fatalf("rewrite = %+v, %v; want %+v", result, err, want)
				}
			}
		})
	}
}
