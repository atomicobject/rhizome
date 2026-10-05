package obsidian

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"
)

func writeVaultFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, body := range files {
		full := filepath.Join(root, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, []byte(body), 0o644))
	}
}

func brokenTargets(links []BrokenLink) []string {
	out := make([]string, 0, len(links))
	for _, link := range links {
		target := link.Target
		if link.Fragment != "" {
			target += "#" + link.Fragment
		}
		out = append(out, target)
	}
	sort.Strings(out)
	return out
}

// Links resolve the way Obsidian resolves them: code is not prose, names
// compare case-insensitively, only `.md` is implied, attachments and files
// under ignored paths are real targets, and duplicate names still resolve.
func TestFindBrokenLinksUsesObsidianResolution(t *testing.T) {
	root := t.TempDir()
	writeVaultFiles(t, root, map[string]string{
		".rhizome/ignore":                     "/_Archive/\n",
		"_Archive/2025-Q3.md":                 "# Q3\n\n## Goals\n",
		"_Archive/Notes/Old.md":               "# Old\n",
		"_Attachments/Ladders.xlsx":           "binary",
		"Log/fetch.ai call, 2025-01-20.md":    "# Call\n",
		"Notes/Nonfunctional Requirements.md": "## Change Agent: Orientation\n",
		"Notes/CLAUDE.md":                     "# a\n",
		"Log/CLAUDE.md":                       "# b\n",
		"Log/Huddle.md": "# Huddle\n\n" +
			"```dataviewjs\n" +
			"dv.paragraph(`[[${p.file.name}|${prop}:]]`)\n" +
			"text.replace(/x/, \"[[$1]]\")\n" +
			"```\n\n" +
			"Inline `![[embed]]`s stay code.\n\n" +
			"- parent [[Nonfunctional Requirements]]\n" +
			"\t- nested [[Missing In Nested List]]\n\n" +
			"[[fetch.ai call, 2025-01-20]] [[Ladders.xlsx]] [[2025-Q3]] [[_Archive/Notes/Old]]\n" +
			"[[2025-Q3#Goals]] [[2025-Q3#Nowhere]] [[Ladders.xlsx#Sheet1]]\n" +
			"[[nonfunctional requirements]] [[Claude]]\n" +
			"[[Nonfunctional Requirements#Change Agent Orientation]]\n" +
			"[[Nonfunctional Requirements#Nowhere]]\n" +
			"[archived](../_Archive/Notes/Old.md) [gone](Gone.md) [web](x-devonthink-item://ABC)\n",
	})

	broken, err := FindBrokenLinks(VaultDefinition{Name: "obsidian", Path: root}, &Note{}, DefaultBrokenLinksOptions)
	require.NoError(t, err)
	require.Equal(t, []string{
		"2025-Q3#Nowhere",
		"Gone.md",
		"Missing In Nested List",
		"Nonfunctional Requirements#Nowhere",
	}, brokenTargets(broken), "fragments in notes under ignored paths are checked too")
	for _, link := range broken {
		if link.Target == "Gone.md" {
			require.True(t, link.Markdown)
			require.Equal(t, "[gone](Gone.md)", link.Raw)
		}
	}
}

func TestVaultFileIndexResolveMatchesObsidian(t *testing.T) {
	index := NewVaultFileIndex([]string{
		"Notes/Fetch AI.md",
		"Log/fetch.ai call.md",
		"_Attachments/Public Ladders.xlsx",
		"CLAUDE.md",
		"Log/CLAUDE.md",
		"Makefile",
		"Notes/deep/Idea.md",
		"Log/Idea.md",
	})
	tests := []struct {
		link, source, want string
	}{
		{"fetch.ai call", "Notes/a.md", "Log/fetch.ai call.md"},
		{"Public Ladders.xlsx", "Notes/a.md", "_Attachments/Public Ladders.xlsx"},
		{"public ladders", "Notes/a.md", ""},
		{"fetch", "Notes/a.md", ""},
		{"claude", "Log/2025-08-15.md", "CLAUDE.md"},
		{"Idea", "Log/2025-08-15.md", "Log/Idea.md"},
		{"Idea", "Other/x.md", "Log/Idea.md"},
		{"Claude", "Notes/a.md", "CLAUDE.md"},
		{"/Log/Claude", "Notes/a.md", "Log/CLAUDE.md"},
		{"/Claude.md", "Log/x.md", "CLAUDE.md"},
		{"Makefile", "Notes/a.md", ""},
		{"Notes/fetch ai", "x.md", "Notes/Fetch AI.md"},
	}
	for _, test := range tests {
		got, ok := index.Resolve(test.link, test.source)
		require.Equal(t, test.want != "", ok, test.link)
		require.Equal(t, test.want, got, test.link)
	}
}

func TestIndentedCodeKeepsNestedListLinks(t *testing.T) {
	content := "- item\n\t- [[Nested]]\n    - [[Spaced]]\n\nParagraph\n    [[Lazy]]\n\n    [[Code]]\n" +
		"- another list\n# Heading ends the list\n    [[CodeAfterHeading]]\n"
	var targets []string
	for _, link := range ScanWikilinks(content, DefaultWikilinkOptions) {
		if !link.InsideCodeBlock {
			targets = append(targets, link.Target)
		}
	}
	require.Equal(t, []string{"Nested", "Spaced", "Lazy"}, targets)
}

// Markdown links in repository docs name GitHub heading slugs or HTML anchors.
func TestFindBrokenLinksAcceptsMarkdownAnchorStyles(t *testing.T) {
	root := t.TempDir()
	writeVaultFiles(t, root, map[string]string{
		"Review.md": "1. [Obs](#key-observations)\n2. [Intro](#introduction--motivation)\n" +
			"6. [Second setup](#setup-1)\n7. [Third setup](#setup-2)\n" +
			"3. [One](#1)\n4. [Gone](#no-such-heading)\n5. [[#key-observations]]\n\n" +
			"## Key Observations\n\n## Introduction & Motivation\n\n<a name=\"1\"></a>\n### 1. First\n" +
			"## Setup\n\n## Setup\n",
	})

	broken, err := FindBrokenLinks(VaultDefinition{Name: "anchors", Path: root}, &Note{}, DefaultBrokenLinksOptions)
	require.NoError(t, err)
	require.Equal(t, []string{"#key-observations", "#no-such-heading", "#setup-2"}, brokenTargets(broken),
		"wikilinks keep Obsidian's heading rules; Markdown links also accept slugs and anchors")
}

// A retarget rewrites prose links only, never links shown as code.
func TestRewriteLinksPreservingDisplaySkipsCode(t *testing.T) {
	content := "See [[Old]].\n\nIntro:\n\n    [[Old]] in an indented sample\n\n`[[Old]]` inline\n"
	updated, count := RewriteLinksPreservingDisplay(content, "Old", "New", true)
	require.Equal(t, 1, count)
	require.Equal(t, "See [[New|Old]].\n\nIntro:\n\n    [[Old]] in an indented sample\n\n`[[Old]]` inline\n", updated)
}
