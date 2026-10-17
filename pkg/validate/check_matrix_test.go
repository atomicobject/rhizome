package validate

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestPreparedChecksProduceCompleteEvidenceBeforeTruncation(t *testing.T) {
	companionSchema := func(target string) string {
		return fmt.Sprintf(`type Project @node(paths: ["projects/*.md"]) @companionDocs(paths: [%q], purpose: "workflow") { name: String }`, target)
	}
	for _, test := range []struct {
		name, check string
		files       map[string]string
		prepare     func(*testing.T, string)
		wantCodes   []string
		wantNotes   string
		wantAbsent  bool
	}{
		{name: "clean links", check: CheckBrokenLinks, files: map[string]string{"source.md": "[[target]]", "target.md": "# Target"}},
		{name: "missing links", check: CheckBrokenLinks, files: map[string]string{"source.md": "[[missing-a]] [[missing-b]]"}, wantCodes: []string{IssueCodeBrokenNoteLink, IssueCodeBrokenNoteLink}},
		{name: "clean heading", check: CheckFragileExternal, files: map[string]string{"source.md": "[[target#Current]]", "target.md": "# Target\n\n## Current\n"}},
		{name: "stale and ambiguous headings", check: CheckFragileExternal, files: map[string]string{"source.md": "[[target#Old]] [[target#Duplicate]]", "target.md": "# Target\n\n## Duplicate\n\n## Duplicate\n"}, wantCodes: []string{CheckFragileExternal, CheckFragileExternal}},
		{name: "clean frontmatter", check: CheckCodeFrontmatter, files: map[string]string{"note.md": "---\ntitle: Fine\n---\n# Fine"}},
		{name: "invalid frontmatter", check: CheckCodeFrontmatter, files: map[string]string{"a.md": "---\ntags: [\n---\n", "b.md": "---\ncode-anchors: [\n---\n"}, wantCodes: []string{issueCodeFrontmatterYAML, issueCodeFrontmatterYAML}},
		{name: "template frontmatter", check: CheckCodeFrontmatter, files: map[string]string{"template.md": "---\ntitle: {{title}}\n---\n"}, wantNotes: "template.md"},
		{name: "no notes", check: CheckCodeFrontmatter, wantAbsent: true},
		{name: "companion absent schema", check: CheckCompanionDocs, wantAbsent: true},
		{name: "companion present", check: CheckCompanionDocs, files: map[string]string{".rhizome/ontology/schema.graphql": companionSchema("guide.md"), "guide.md": "# Guide"}},
		{name: "companion parent escape", check: CheckCompanionDocs, files: map[string]string{".rhizome/ontology/schema.graphql": companionSchema("../outside.md")}, wantCodes: []string{"companion_doc_outside_vault"}},
		{name: "companion absolute escape", check: CheckCompanionDocs, files: map[string]string{".rhizome/ontology/schema.graphql": companionSchema(filepath.Join(t.TempDir(), "outside.md"))}, wantCodes: []string{"companion_doc_outside_vault"}},
		{name: "companion missing", check: CheckCompanionDocs, files: map[string]string{".rhizome/ontology/schema.graphql": companionSchema("missing.md")}, wantCodes: []string{"companion_doc_missing"}},
		{name: "companion directory", check: CheckCompanionDocs, files: map[string]string{".rhizome/ontology/schema.graphql": companionSchema("guide")}, prepare: func(t *testing.T, root string) { require.NoError(t, os.Mkdir(filepath.Join(root, "guide"), 0o755)) }, wantCodes: []string{"companion_doc_directory"}},
		{name: "companion stat failure", check: CheckCompanionDocs, files: map[string]string{".rhizome/ontology/schema.graphql": companionSchema("guide/child.md"), "guide": "not a directory"}, wantCodes: []string{"companion_doc_stat_failed"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			require.NoError(t, writeFixtureFiles(root, test.files))
			if test.prepare != nil {
				test.prepare(t, root)
			}
			run := RunContext{VaultDef: obsidian.VaultDefinition{Path: root, Links: obsidian.LinkTypeBoth}, VaultPath: root, NoteReader: &obsidian.Note{}, NoteMetadata: testNoteMetadata(t)}
			result := preparedCheck(t, t.Context(), run, ScanOptions{MaxIssues: 1}, test.check)
			require.Empty(t, result.Error)
			var codes []string
			for _, issue := range result.fullIssues {
				codes = append(codes, issue.Code)
				require.NotEmpty(t, issue.Key)
			}
			require.ElementsMatch(t, test.wantCodes, codes)
			require.Equal(t, len(test.wantCodes), result.IssueCount)
			require.LessOrEqual(t, len(result.Issues), 1)
			require.Equal(t, test.wantAbsent, result.NotApplicable != "")
			if test.wantNotes != "" {
				require.Contains(t, fmt.Sprint(result.Notes), test.wantNotes)
			}
		})
	}
}

func TestFinalizeCheckResultDerivesCountsAndPreservesFullEvidence(t *testing.T) {
	for _, supplied := range []int{0, 1, 99} {
		t.Run(fmt.Sprint(supplied), func(t *testing.T) {
			result := finalizeCheckResult(CheckViews, CheckResult{OK: true, IssueCount: supplied, Issues: []Issue{
				{Code: "first", Path: "a.md"}, {Code: "second", Path: "b.md"},
			}}, 0, 1)
			require.False(t, result.OK)
			require.Equal(t, 2, result.IssueCount)
			require.Len(t, result.Issues, 1)
			require.Len(t, result.fullIssues, 2)
			require.Len(t, result.allIssueKeys, 2)
		})
	}
}
