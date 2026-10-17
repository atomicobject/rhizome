package obsidian

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveMdLinkCandidatesPreservesAuthoredAdmission(t *testing.T) {
	cache := BuildNotePathCacheWithAliases([]string{
		"specs/product.md", "archive/product.md",
		"a:b/Final.md", "Notes/a:b/Final.md", "a:b/Choice.md", "A:B/CHOICE.md",
		"https:/example.test/specs/product.md", "Notes/https:/example.test/specs/product.md",
	}, map[string][]string{"specs/product.md": {
		"ALIAS", "mailto:ALIAS", "custom+v1:ALIAS", "C:ALIAS", "a:b/ALIAS",
	}})
	for _, source := range []string{"Source.md", "Notes/Source.md"} {
		for _, target := range []string{
			"https://example.test/specs/product.md", "https%3A//example.test/specs/product.md",
			"https://example.test/../../specs/product.md", "https%3A//example.test/../../specs/product.md",
			"HTTPS%3a%2f%2fexample.test%2f..%2f..%2fspecs%2fproduct.md",
			"//example.test/../../specs/product.md", "%2F%2Fexample.test/../../specs/product.md",
			"\\\\example.test\\..\\..\\specs\\product.md", "/\\example.test/../../specs/product.md",
			"C:/../specs/product.md", "C%3A/../specs/product.md", "C%3A%2F%2E%2E%2Fspecs/product.md",
			"C:/specs/product.md", "C:product", "C%3Aproduct",
			"mailto:product", "mailto%3Aproduct", "custom+v1:product", "custom+v1%3Aproduct",
			"mailto:ALIAS", "custom+v1:ALIAS", "C:ALIAS", "a:b/ALIAS",
			"a:b/../specs/product.md", "a%3Ab/../specs/product.md", "a:b/./Final.md",
			"../specs/product.md/../../../../specs/product.md",
		} {
			t.Run(source+"/"+target, func(t *testing.T) {
				require.Empty(t, cache.ResolveMdLinkCandidates(target, source))
				got, ok := cache.ResolveMdLinkTarget(target, source)
				require.False(t, ok)
				require.Equal(t, ResolvedNoteTarget{}, got)
			})
		}
	}
	for _, tc := range []struct{ target, source, path string }{
		{"a:b/Final.md", "Source.md", "a:b/Final.md"},
		{"a%3Ab/Final", "Source.md", "a:b/Final.md"},
		{"a:b/Final.md", "Notes/Source.md", "Notes/a:b/Final.md"},
		{"a%3Ab/Final", "Notes/Source.md", "Notes/a:b/Final.md"},
		{"../a%3Ab/Final.md", "Notes/Source.md", "a:b/Final.md"},
		{"../specs/product.md", "Notes/Source.md", "specs/product.md"},
		{"../ALIAS", "Notes/Source.md", "specs/product.md"},
		{"ALIAS", "Source.md", "specs/product.md"},
		{"ALIAS", "Notes/Source.md", "specs/product.md"},
		{"/specs/product.md", "Notes/Source.md", "specs/product.md"},
	} {
		t.Run(tc.source+"/"+tc.target, func(t *testing.T) {
			require.Equal(t, []ResolvedNoteTarget{{Path: tc.path, Fragment: "heading"}}, cache.ResolveMdLinkCandidates(tc.target+"#heading", tc.source))
		})
	}
	require.Empty(t, cache.ResolveMdLinkCandidates("../specs/product.md", "Source.md"))
	ambiguous := BuildNotePathCache([]string{"specs/product.md", "archive/product.md"})
	require.ElementsMatch(t, []ResolvedNoteTarget{{Path: "specs/product.md"}, {Path: "archive/product.md"}}, ambiguous.ResolveMdLinkCandidates("product", "Notes/Source.md"))
	cache.AddOrUpdate("Notes/ALIAS.md", nil)
	require.Equal(t, []ResolvedNoteTarget{{Path: "Notes/ALIAS.md"}}, cache.ResolveMdLinkCandidates("ALIAS", "Notes/Source.md"))
	require.ElementsMatch(t, []ResolvedNoteTarget{{Path: "a:b/Choice.md"}, {Path: "A:B/CHOICE.md"}}, cache.ResolveMdLinkCandidates("a:b/choice.md", "Source.md"))
	require.ElementsMatch(t, []ResolvedNoteTarget{{Path: "a:b/Choice.md"}, {Path: "A:B/CHOICE.md"}}, cache.ResolveMdLinkCandidates("a:b/choice", "Source.md"))
	_, ok := cache.ResolveMdLinkTarget("a:b/choice.md", "Source.md")
	require.False(t, ok)
}
