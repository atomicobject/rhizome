package coderefs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRewriteBatch_DestinationSyntaxRescansAcrossRenames(t *testing.T) {
	for _, destination := range []string{
		"Archive/Budget#USD.md", "Archive/Budget|USD.md", "Archive/Budget]].md",
		"Archive/Budget].md", "Archive/Budget(.md", "Archive/Budget).md",
		"Archive$USD/Budget$USD.md", "Archive/预算 USD.md", "Archive/Budget%20USD.md",
		"Archive/Budget%USD.md", "Archive/Budget%.md", "Archive[[USD/Plan.md",
		"Budget:USD.md", "a:b/Final.md", "Budget:USD#part.md", "a:b/Final#part.md",
		"Archive/@Old#USD.md",
	} {
		t.Run(destination, func(t *testing.T) {
			content := "package fixture\n" +
				"// [[docs/Old.md#heading($1)[part]|caption [part] ($1)]]\n" +
				"// [[docs/Old]] [[oLd]] @docs/Old @Old\n" +
				"// [caption \\[part\\] ($1)](docs/Old.md#heading%28$1%29%5Bpart%5D) [extless](docs/Old)\n" +
				"// `[example](docs/Old.md)` `[[docs/Old.md]]`\n" +
				"// ![[docs/Old.md]] ![image @Old](docs/Old.md)\n" +
				"// user@Old.com @OldSuffix @docs/OldSuffix [[docs/Other|@Old]]\n" +
				"const decoy = \"[[docs/Old.md]] @Old [link](docs/Old.md)\"\n"
			vaultDir := t.TempDir()
			file := filepath.Join(vaultDir, "refs.go")
			require.NoError(t, os.WriteFile(file, []byte(content), 0o644))
			previous := "docs/Old.md"
			for _, next := range []string{destination, "Reviewed/Final.md"} {
				result, err := RewriteBatch(vaultDir, NewConfig(true, []string{"*.go"}, nil), []RefMapping{{OldPath: previous, NewPath: next}})
				require.NoError(t, err)
				require.Equal(t, RewriteResult{FilesUpdated: 1, RefsUpdated: 10}, result)
				updated, err := os.ReadFile(file)
				require.NoError(t, err)
				assert.Contains(t, string(updated), "![image @Old](docs/Old.md)")
				assert.Contains(t, string(updated), "user@Old.com @OldSuffix @docs/OldSuffix [[docs/Other|@Old]]")
				assert.Contains(t, string(updated), "const decoy = \"[[docs/Old.md]] @Old [link](docs/Old.md)\"")
				refs, err := ScanFile("refs.go", updated, buildTestCache([]string{
					next, "docs/Old.md", "docs/Other.md", "Archive.md", "Archive/Budget.md",
					"Budget.md", "Archive/Budget USD.md", "Archive/Budget%20USD.md", "Archive/Budget].md",
				}))
				require.NoError(t, err)
				require.Len(t, refs, 12)
				counts := map[string]int{}
				for _, ref := range refs {
					counts[ref.Target]++
					if ref.Line == 2 || (ref.Line == 4 && strings.Contains(ref.RawTarget, "#")) {
						assert.Equal(t, "heading($1)[part]", ref.Fragment)
					}
				}
				assert.Equal(t, map[string]int{next: 10, "docs/Old.md": 1, "docs/Other.md": 1}, counts)
				previous = next
			}
		})
	}
}

func TestRewriteBatch_EncodedMarkdownSpans(t *testing.T) {
	content := "package fixture\n" +
		"// [label \\[part\\] ($1)](<docs/Budget%23USD.md#heading%28$1%29> \"title $1\")\n" +
		"// [lower](docs/Budget%23USD%2emd) [mixed](docs/%42udget%23USD.md)\n" +
		"// [extless](docs/Budget%23USD) ![image](docs/Budget%23USD.md)\n"
	dir := t.TempDir()
	file := filepath.Join(dir, "refs.go")
	require.NoError(t, os.WriteFile(file, []byte(content), 0o644))
	result, err := RewriteBatch(dir, NewConfig(true, []string{"*.go"}, nil), []RefMapping{{OldPath: "docs/Budget#USD.md", NewPath: "Archive/Final($1).md"}})
	require.NoError(t, err)
	require.Equal(t, RewriteResult{FilesUpdated: 1, RefsUpdated: 4}, result)
	updated, err := os.ReadFile(file)
	require.NoError(t, err)
	assert.Equal(t, "package fixture\n"+
		"// [label \\[part\\] ($1)](<Archive/Final%28$1%29.md#heading%28$1%29> \"title $1\")\n"+
		"// [lower](Archive/Final%28$1%29.md) [mixed](Archive/Final%28$1%29.md)\n"+
		"// [extless](Archive/Final%28$1%29) ![image](docs/Budget%23USD.md)\n", string(updated))
	refs, err := ScanFile("refs.go", updated, buildTestCache([]string{"Archive/Final($1).md", "docs/Budget#USD.md"}))
	require.NoError(t, err)
	require.Len(t, refs, 5)
	assert.Equal(t, "heading($1)", refs[0].Fragment)
	assert.Equal(t, "Archive/Final($1).md", refs[0].Target)
}

func TestScanFile_CommentExamplesKeepOccurrenceContext(t *testing.T) {
	content := "package fixture\n/* `[first](Note.md)`\n * `[middle](Note.md)`\n * `[last](Note.md)` */\nconst decoy = \"[outside](Note.md)\"\n"
	refs, err := ScanFile("refs.go", []byte(content), buildTestCache([]string{"Note.md"}))
	require.NoError(t, err)
	require.Equal(t, []CodeRef{
		{SourceFile: "refs.go", Language: "go", Target: "Note.md", RawTarget: "Note.md", Kind: RefKindMdLink, Line: 2, Snippet: "/* `[first](Note.md)`"},
		{SourceFile: "refs.go", Language: "go", Target: "Note.md", RawTarget: "Note.md", Kind: RefKindMdLink, Line: 3, Snippet: "* `[middle](Note.md)`"},
		{SourceFile: "refs.go", Language: "go", Target: "Note.md", RawTarget: "Note.md", Kind: RefKindMdLink, Line: 4, Snippet: "* `[last](Note.md)` */"},
	}, refs)
}

func TestRewriteBatch_NestedLinkDataIsNotAMention(t *testing.T) {
	for _, tc := range []struct{ body, target string }{
		{`[before [[Other]] @Old after](Third.md)`, "Third.md"},
		{`[before [[Other]] after](Third.md "title @Old")`, "Third.md"},
		{`[before [[Other]] after](Archive/@Old.md)`, "Archive/@Old.md"},
	} {
		body := tc.body
		t.Run(body, func(t *testing.T) {
			content := "package fixture\n// " + body + " @Old\n"
			cache := buildTestCache([]string{"docs/Old.md", "Other.md", "Third.md", "Archive/@Old.md", "Archive/New.md"})
			refs, err := ScanFile("refs.go", []byte(content), cache)
			require.NoError(t, err)
			require.Len(t, refs, 3, "only the mention outside the nested links resolves Old")
			require.Equal(t, RefKindWikilink, refs[0].Kind)
			require.Equal(t, "Other.md", refs[0].Target)
			require.Equal(t, RefKindMdLink, refs[1].Kind)
			require.Equal(t, tc.target, refs[1].Target)
			require.Equal(t, "docs/Old.md", refs[2].Target)
			dir := t.TempDir()
			file := filepath.Join(dir, "refs.go")
			require.NoError(t, os.WriteFile(file, []byte(content), 0o644))
			result, err := RewriteBatch(dir, NewConfig(true, []string{"*.go"}, nil), []RefMapping{{OldPath: "docs/Old.md", NewPath: "Archive/New.md"}})
			require.NoError(t, err)
			require.Equal(t, RewriteResult{FilesUpdated: 1, RefsUpdated: 1}, result)
			updated, err := os.ReadFile(file)
			require.NoError(t, err)
			require.Equal(t, "package fixture\n// "+body+" @New\n", string(updated))
			rescanned, err := ScanFile("refs.go", updated, cache)
			require.NoError(t, err)
			require.Len(t, rescanned, 3)
			require.Equal(t, []string{"Other.md", tc.target, "Archive/New.md"}, []string{rescanned[0].Target, rescanned[1].Target, rescanned[2].Target})
		})
	}
}

func TestRewriteBatch_WikiEmbedFallbackPreservesLabelAndFragment(t *testing.T) {
	for _, tc := range []struct {
		destination, first, second string
		kind                       RefKind
	}{
		{
			destination: "Archive/Budget$USD.md", kind: RefKindWikilink,
			first:  `![[Archive/Budget$USD.md#heading($1)[part]%20|caption \ [part] ($1)]]`,
			second: `![[Reviewed/Final.md#heading($1)[part]%20|caption \ [part] ($1)]]`,
		},
		{
			destination: "Archive/Budget#USD.md", kind: RefKindMdLink,
			first:  `[caption \\ \[part\] ($1)](Archive/Budget%23USD.md#heading%28$1%29%5Bpart%5D%2520)`,
			second: `[caption \\ \[part\] ($1)](Reviewed/Final.md#heading%28$1%29%5Bpart%5D%2520)`,
		},
	} {
		t.Run(tc.destination, func(t *testing.T) {
			dir := t.TempDir()
			file := filepath.Join(dir, "refs.go")
			require.NoError(t, os.WriteFile(file, []byte("package fixture\n// "+`![[docs/Old.md#heading($1)[part]%20|caption \ [part] ($1)]]`+"\n"), 0o644))
			previous := "docs/Old.md"
			for i, next := range []string{tc.destination, "Reviewed/Final.md"} {
				result, err := RewriteBatch(dir, NewConfig(true, []string{"*.go"}, nil), []RefMapping{{OldPath: previous, NewPath: next}})
				require.NoError(t, err)
				assert.Equal(t, RewriteResult{FilesUpdated: 1, RefsUpdated: 1}, result)
				updated, err := os.ReadFile(file)
				require.NoError(t, err)
				want := tc.first
				if i == 1 {
					want = tc.second
				}
				assert.Equal(t, "package fixture\n// "+want+"\n", string(updated))
				refs, err := ScanFile("refs.go", updated, buildTestCache([]string{next, "Archive/Budget.md"}))
				require.NoError(t, err)
				require.Len(t, refs, 1)
				assert.Equal(t, next, refs[0].Target)
				assert.Equal(t, "heading($1)[part]%20", refs[0].Fragment)
				assert.Equal(t, tc.kind, refs[0].Kind)
				previous = next
			}
		})
	}
}

func TestRewriteBatch_NormalizedMappingPaths(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "refs.go")
	require.NoError(t, os.WriteFile(file, []byte("package fixture\n// [[docs/Old.md]] [label](docs/Old.md)\n"), 0o644))
	result, err := RewriteBatch(dir, NewConfig(true, []string{"*.go"}, nil), []RefMapping{{OldPath: `./docs\Old.md`, NewPath: `./Archive\New.md`}})
	require.NoError(t, err)
	assert.Equal(t, RewriteResult{FilesUpdated: 1, RefsUpdated: 2}, result)
	updated, err := os.ReadFile(file)
	require.NoError(t, err)
	assert.Equal(t, "package fixture\n// [[Archive/New.md]] [label](Archive/New.md)\n", string(updated))
}

func TestRewriteBatch_SequentialMappingsMatchEncodedDestination(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "refs.go")
	content := "package fixture\n// [caption](docs/Old.md#part) @docs/Old [[docs/Old]] [bare](Old.md)\n"
	require.NoError(t, os.WriteFile(file, []byte(content), 0o644))
	result, err := RewriteBatch(dir, NewConfig(true, []string{"*.go"}, nil), []RefMapping{
		{OldPath: "docs/Absent.md", NewPath: "Archive/Unused.md"},
		{OldPath: "docs/Old.md", NewPath: "Archive/Budget#USD.md"},
		{OldPath: "docs/AnotherAbsent.md", NewPath: "Archive/StillUnused.md"},
		{OldPath: "Archive/Budget#USD.md", NewPath: "Reviewed/Final.md"},
		{OldPath: "docs/LastAbsent.md", NewPath: "Archive/UnusedAgain.md"},
	})
	require.NoError(t, err)
	assert.Equal(t, RewriteResult{FilesUpdated: 1, RefsUpdated: 6}, result)
	updated, err := os.ReadFile(file)
	require.NoError(t, err)
	assert.Equal(t, "package fixture\n// [caption](Reviewed/Final.md#part) [Budget#USD](Reviewed/Final.md) [Budget#USD](Reviewed/Final.md) [bare](Old.md)\n", string(updated))
	refs, err := ScanFile("refs.go", updated, buildTestCache([]string{"Reviewed/Final.md", "docs/Old.md", "Archive/Budget.md"}))
	require.NoError(t, err)
	require.Len(t, refs, 4)
	for _, ref := range refs[:3] {
		assert.Equal(t, "Reviewed/Final.md", ref.Target)
	}
	assert.Equal(t, "docs/Old.md", refs[3].Target)
}

func TestRewriteBatch_NestedLinkTargetsRemainSeparateEdits(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "refs.go")
	content := "package fixture\n// " + `[before [[docs/Old.md#part|caption]] after](docs/Old.md "title [[Old]] @Old") @Old` + "\n"
	require.NoError(t, os.WriteFile(file, []byte(content), 0o644))
	result, err := RewriteBatch(dir, NewConfig(true, []string{"*.go"}, nil), []RefMapping{
		{OldPath: "docs/Old.md", NewPath: "Archive/New.md"},
		{OldPath: "Archive/New.md", NewPath: "Reviewed/Final.md"},
	})
	require.NoError(t, err)
	require.Equal(t, RewriteResult{FilesUpdated: 1, RefsUpdated: 8}, result)
	updated, err := os.ReadFile(file)
	require.NoError(t, err)
	require.Equal(t, "package fixture\n// "+`[before [[Reviewed/Final.md#part|caption]] after](Reviewed/Final.md "title [[Final]] @Old") @Final`+"\n", string(updated))
	refs, err := ScanFile("refs.go", updated, buildTestCache([]string{"Reviewed/Final.md", "docs/Old.md"}))
	require.NoError(t, err)
	require.Len(t, refs, 4)
	for _, ref := range refs {
		require.Equal(t, "Reviewed/Final.md", ref.Target)
	}
	require.Equal(t, "part", refs[0].Fragment)
}
