package operations

import (
	"context"
	"fmt"
	"github.com/atomicobject/rhizome/internal/testkit"
	"github.com/atomicobject/rhizome/pkg/markdownsyntax"
	"github.com/atomicobject/rhizome/pkg/ontology/sectionparse"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
)

func TestRenameHeadingPlansAndAppliesInboundRewrites(t *testing.T) {
	vaultDir := t.TempDir()
	targetPath := filepath.Join(vaultDir, "target.md")
	sourcePath := filepath.Join(vaultDir, "source.md")
	targetBefore := "# Target\n\n## Old Heading\n\n## Old Heading Extra\n"
	sourceBefore := "# Source\n\n[[target#Old Heading]]\n[[target#Old Headng]]\n[[target#Old Heading Extra]]\n\n```md\n[[target#Old Heading]]\n```\n"
	require.NoError(t, os.WriteFile(targetPath, []byte(targetBefore), 0o644))
	require.NoError(t, os.WriteFile(sourcePath, []byte(sourceBefore), 0o644))

	plan, err := RenameHeading(stubVault{path: vaultDir}, RenameHeadingParams{
		NoteMetadata:     testkit.NoteMetadataIndexer(t),
		Path:             "target.md",
		OldHeading:       "Old Heading",
		NewHeading:       "New Heading",
		UpgradeToBlockID: HeadingRenameUpgradeNever,
		Fallback:         HeadingRenameFallbackHeading,
	})
	require.NoError(t, err)
	assert.False(t, plan.Applied)
	assert.Equal(t, 2, plan.MatchedReferences)
	assert.Equal(t, 1, plan.Rewritten)
	require.Len(t, plan.Skipped, 2)
	// The typo-near fragment is reported, not rewritten; the longer valid heading is neither.
	assert.Equal(t, "heading_rename_skipped", plan.Skipped[0].Code)
	assert.Equal(t, "ambiguous_target", plan.Skipped[0].Reason)
	assert.Equal(t, "source.md", plan.Skipped[0].SourceNote)
	assert.Equal(t, "heading_rename_skipped", plan.Skipped[1].Code)
	assert.Equal(t, "code_block", plan.Skipped[1].Reason)

	// Preview must not write either note.
	for path, want := range map[string]string{targetPath: targetBefore, sourcePath: sourceBefore} {
		got, readErr := os.ReadFile(path)
		require.NoError(t, readErr)
		assert.Equal(t, want, string(got))
	}

	applied, err := RenameHeading(stubVault{path: vaultDir}, RenameHeadingParams{
		NoteMetadata:     testkit.NoteMetadataIndexer(t),
		Path:             "target.md",
		OldHeading:       "Old Heading",
		NewHeading:       "New Heading",
		Apply:            true,
		UpgradeToBlockID: HeadingRenameUpgradeNever,
		Fallback:         HeadingRenameFallbackHeading,
	})
	require.NoError(t, err)
	assert.True(t, applied.Applied)
	target, err := os.ReadFile(targetPath)
	require.NoError(t, err)
	assert.Equal(t, "# Target\n\n## New Heading\n\n## Old Heading Extra\n", string(target))
	source, err := os.ReadFile(sourcePath)
	require.NoError(t, err)
	assert.Equal(t, "# Source\n\n[[target#New Heading]]\n[[target#Old Headng]]\n[[target#Old Heading Extra]]\n\n```md\n[[target#Old Heading]]\n```\n", string(source))
}

func TestRenameHeadingAppliesExactWikilinkTargetOnly(t *testing.T) {
	vaultDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(vaultDir, "target.md"), []byte("# Target\n\n## Old Heading\n\n## Old Heading Extra\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(vaultDir, "source.md"), []byte("# Source\n\n[[target#Old Heading Extra]]\n[[target#Old Heading|old]]\n"), 0o644))

	applied, err := RenameHeading(stubVault{path: vaultDir}, RenameHeadingParams{
		NoteMetadata:     testkit.NoteMetadataIndexer(t),
		Path:             "target.md",
		OldHeading:       "Old Heading",
		NewHeading:       "New Heading",
		Apply:            true,
		UpgradeToBlockID: HeadingRenameUpgradeNever,
		Fallback:         HeadingRenameFallbackHeading,
	})
	require.NoError(t, err)
	assert.True(t, applied.Applied)

	source, err := os.ReadFile(filepath.Join(vaultDir, "source.md"))
	require.NoError(t, err)
	assert.Contains(t, string(source), "[[target#Old Heading Extra]]")
	assert.Contains(t, string(source), "[[target#New Heading|old]]")
	assert.NotContains(t, string(source), "[[target#New Heading Extra]]")
}

func TestRenameHeadingRefusesAmbiguousOldHeading(t *testing.T) {
	vaultDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(vaultDir, "target.md"), []byte("# Target\n\n## Old Heading\n\n## Old Heading\n"), 0o644))

	_, err := RenameHeading(stubVault{path: vaultDir}, RenameHeadingParams{
		NoteMetadata: testkit.NoteMetadataIndexer(t),
		Path:         "target.md",
		OldHeading:   "Old Heading",
		NewHeading:   "New Heading",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ambiguous")
}

func TestRenameHeadingIgnoresHeadingsInsideFencedCodeBlocks(t *testing.T) {
	vaultDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(vaultDir, "target.md"), []byte("# Target\n\n```md\n## Old Heading\n```\n\n## Old Heading\n"), 0o644))

	applied, err := RenameHeading(stubVault{path: vaultDir}, RenameHeadingParams{
		NoteMetadata:     testkit.NoteMetadataIndexer(t),
		Path:             "target.md",
		OldHeading:       "Old Heading",
		NewHeading:       "New Heading",
		Apply:            true,
		UpgradeToBlockID: HeadingRenameUpgradeNever,
		Fallback:         HeadingRenameFallbackHeading,
	})
	require.NoError(t, err)
	assert.True(t, applied.Applied)

	target, err := os.ReadFile(filepath.Join(vaultDir, "target.md"))
	require.NoError(t, err)
	assert.Contains(t, string(target), "```md\n## Old Heading\n```")
	assert.Contains(t, string(target), "## New Heading")
}

func TestRenameHeadingApplyPreservesSurroundingBytes(t *testing.T) {
	cases := map[string]struct{ before, want string }{
		"blank line before next heading": {"# T\n\n## Old Heading\n\n## Next\n", "# T\n\n## New Heading\n\n## Next\n"},
		"eof with trailing newline":      {"# T\n\n## Old Heading\n", "# T\n\n## New Heading\n"},
		"eof without trailing newline":   {"# T\n\n## Old Heading", "# T\n\n## New Heading"},
		"crlf line endings":              {"# T\r\n\r\n## Old Heading\r\n\r\nBody\r\n", "# T\r\n\r\n## New Heading\r\n\r\nBody\r\n"},
		"trailing spaces on heading":     {"# T\n\n## Old Heading  \n\nBody\n", "# T\n\n## New Heading  \n\nBody\n"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			vaultDir := t.TempDir()
			targetPath := filepath.Join(vaultDir, "target.md")
			require.NoError(t, os.WriteFile(targetPath, []byte(tc.before), 0o644))

			_, err := RenameHeading(stubVault{path: vaultDir}, RenameHeadingParams{
				NoteMetadata:     testkit.NoteMetadataIndexer(t),
				Path:             "target.md",
				OldHeading:       "Old Heading",
				NewHeading:       "New Heading",
				Apply:            true,
				UpgradeToBlockID: HeadingRenameUpgradeNever,
				Fallback:         HeadingRenameFallbackHeading,
			})
			require.NoError(t, err)
			got, err := os.ReadFile(targetPath)
			require.NoError(t, err)
			assert.Equal(t, tc.want, string(got))
		})
	}
}

func TestRenameHeadingComposesSelfLinksAndRepeatedTargets(t *testing.T) {
	for _, item := range []struct {
		name, originalBlock, wantBlock, fragment string
		upgrade                                  HeadingRenameUpgradeMode
		fallback                                 HeadingRenameFallbackMode
	}{
		{"auto heading fallback", "", "", "New Heading", HeadingRenameUpgradeAuto, HeadingRenameFallbackHeading},
		{"auto block fallback", "", "New-Heading", "^New-Heading", HeadingRenameUpgradeAuto, HeadingRenameFallbackBlockID},
		{"auto existing block", "stable", "stable", "^stable", HeadingRenameUpgradeAuto, HeadingRenameFallbackHeading},
		{"always block", "", "New-Heading", "^New-Heading", HeadingRenameUpgradeAlways, HeadingRenameFallbackHeading},
		{"never block", "", "", "New Heading", HeadingRenameUpgradeNever, HeadingRenameFallbackBlockID},
		{"never retains existing block", "stable", "stable", "New Heading", HeadingRenameUpgradeNever, HeadingRenameFallbackBlockID},
	} {
		t.Run(item.name, func(t *testing.T) {
			root := t.TempDir()
			oldHeading := "## Old Heading"
			if item.originalBlock != "" {
				oldHeading += " ^" + item.originalBlock
			}
			targetPath, sourcePath := filepath.Join(root, "target.md"), filepath.Join(root, "source.md")
			protectedSelfLinks := "\n`[[#Old Heading|inline]]`\n\n```md\n[[#Old Heading|fenced]]\n```\n\n    [[#Old Heading|indented]]\n"
			targetBefore := "# Target\n\n" + oldHeading + "\n\n[[#Old Heading|local]] [[#Old Heading]] [[target#Old Heading|self]] [[target#Old Heading|again]]\n" + protectedSelfLinks
			sourceBefore := "# Source\n\n## Old Heading\n\n[[#Old Heading|unrelated]] [[target#Old Heading]] [[target#Old Heading|alias]] `[[target#Old Heading]]`\n"
			require.NoError(t, os.WriteFile(targetPath, []byte(targetBefore), 0o600))
			require.NoError(t, os.WriteFile(sourcePath, []byte(sourceBefore), 0o640))
			modes := make(map[string]os.FileMode)
			for _, path := range []string{targetPath, sourcePath} {
				info, err := os.Stat(path)
				require.NoError(t, err)
				modes[path] = info.Mode().Perm()
			}
			params := RenameHeadingParams{NoteMetadata: testkit.NoteMetadataIndexer(t), Path: "target.md", OldHeading: "Old Heading", NewHeading: "New Heading", UpgradeToBlockID: item.upgrade, Fallback: item.fallback}
			plan, err := RenameHeading(stubVault{path: root}, params)
			require.NoError(t, err)
			require.False(t, plan.Applied)
			require.Equal(t, 10, plan.MatchedReferences)
			require.Equal(t, 6, plan.Rewritten)
			for path, want := range map[string]string{targetPath: targetBefore, sourcePath: sourceBefore} {
				got, readErr := os.ReadFile(path)
				require.NoError(t, readErr)
				require.Equal(t, want, string(got))
			}
			params.Apply = true
			result, err := RenameHeading(stubVault{path: root}, params)
			require.NoError(t, err)
			require.True(t, result.Applied)
			require.Equal(t, plan.Rewrites, result.Rewrites)
			require.Equal(t, plan.Skipped, result.Skipped)
			require.Equal(t, 10, result.MatchedReferences)
			require.Equal(t, 6, result.Rewritten)
			require.Len(t, result.Skipped, 4)
			for _, skipped := range result.Skipped {
				require.Equal(t, "code_block", skipped.Reason)
			}
			upgraded := 0
			if item.fragment[0] == '^' {
				upgraded = 6
			}
			require.Equal(t, upgraded, plan.UpgradedToBlockID)
			require.Equal(t, upgraded, result.UpgradedToBlockID)
			require.Equal(t, item.wantBlock, result.BlockID)
			newHeading := "## New Heading"
			if item.originalBlock != "" {
				newHeading += " ^" + item.originalBlock
			} else if item.wantBlock != "" {
				newHeading += "\n^" + item.wantBlock
			}
			for path, want := range map[string]string{
				targetPath: "# Target\n\n" + newHeading + fmt.Sprintf("\n\n[[#%s|local]] [[#%s]] [[target#%s|self]] [[target#%s|again]]\n", item.fragment, item.fragment, item.fragment, item.fragment) + protectedSelfLinks,
				sourcePath: fmt.Sprintf("# Source\n\n## Old Heading\n\n[[#Old Heading|unrelated]] [[target#%s]] [[target#%s|alias]] `[[target#Old Heading]]`\n", item.fragment, item.fragment),
			} {
				got, readErr := os.ReadFile(path)
				require.NoError(t, readErr)
				require.Equal(t, want, string(got))
				info, statErr := os.Stat(path)
				require.NoError(t, statErr)
				require.Equal(t, modes[path], info.Mode().Perm())
			}
		})
	}
}

func TestRenameHeadingAddsBlockIDOnlyForUpgradedReferences(t *testing.T) {
	const frontmatter = "---\ntype: IPOpportunity\n---\n"
	for _, item := range []struct {
		name, oldHeading, newHeading, extra, source string
		upgrade                                     HeadingRenameUpgradeMode
		wantHeading, wantFragment, wantBlock        string
	}{
		// The reported case: default options and no inbound references.
		{name: "zero references default", oldHeading: "Helping clients turn AI and software into business improvements", newHeading: "AI Transformation Service Offering",
			wantHeading: "# AI Transformation Service Offering"},
		{name: "zero references always", oldHeading: "Old", newHeading: "AI Transformation Service Offering", upgrade: HeadingRenameUpgradeAlways,
			wantHeading: "# AI Transformation Service Offering\n^AI-Transformation-Service-Offering", wantBlock: "AI-Transformation-Service-Offering"},
		{name: "upgraded reference", oldHeading: "Old", newHeading: "AI Transformation Service Offering", source: "[[target#Old]]",
			wantHeading: "# AI Transformation Service Offering\n^AI-Transformation-Service-Offering", wantFragment: "^AI-Transformation-Service-Offering", wantBlock: "AI-Transformation-Service-Offering"},
		{name: "non-ASCII ID the parser cannot read", oldHeading: "Old", newHeading: "Café Plan", source: "[[target#Old]]",
			wantHeading: "# Café Plan", wantFragment: "Café Plan"},
		{name: "ID already used in note", oldHeading: "Old", newHeading: "New Plan", extra: "\nBody ^New-Plan\n", source: "[[target#Old]]",
			wantHeading: "# New Plan", wantFragment: "New Plan"},
	} {
		t.Run(item.name, func(t *testing.T) {
			root := t.TempDir()
			targetPath, sourcePath := filepath.Join(root, "target.md"), filepath.Join(root, "source.md")
			require.NoError(t, os.WriteFile(targetPath, []byte(frontmatter+"# "+item.oldHeading+"\n"+item.extra+"\n## Details\n"), 0o644))
			require.NoError(t, os.WriteFile(sourcePath, []byte("# Source\n\n"+item.source+"\n"), 0o644))
			params := RenameHeadingParams{NoteMetadata: testkit.NoteMetadataIndexer(t), Path: "target.md", OldHeading: item.oldHeading, NewHeading: item.newHeading, UpgradeToBlockID: item.upgrade}
			plan, err := RenameHeading(stubVault{path: root}, params)
			require.NoError(t, err)
			assert.Equal(t, item.wantBlock, plan.BlockID)
			params.Apply = true
			result, err := RenameHeading(stubVault{path: root}, params)
			require.NoError(t, err)
			assert.Equal(t, item.wantBlock, result.BlockID)

			target, err := os.ReadFile(targetPath)
			require.NoError(t, err)
			assert.Equal(t, frontmatter+item.wantHeading+"\n"+item.extra+"\n## Details\n", string(target))
			assert.Equal(t, item.newHeading, markdownsyntax.SingleMarkdownH1Title(string(target)))
			sections := sectionparse.Parse("target.md", string(target))
			require.NotEmpty(t, sections)
			assert.Equal(t, item.newHeading, sections[0].Title)
			assert.Empty(t, sections[0].MalformedBlockID)
			if item.wantBlock != "" {
				assert.Equal(t, item.wantBlock, sections[0].BlockID)
			}
			if item.source != "" {
				source, err := os.ReadFile(sourcePath)
				require.NoError(t, err)
				assert.Equal(t, "# Source\n\n[[target#"+item.wantFragment+"]]\n", string(source))
			}
		})
	}
}

func TestRenameHeadingUnchangedApplyKeepsSourceIdentity(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "target.md")
	content := "# Target\n\n## Old Heading\n"
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	before, err := os.Stat(path)
	require.NoError(t, err)
	result, err := RenameHeading(stubVault{path: root}, RenameHeadingParams{NoteMetadata: testkit.NoteMetadataIndexer(t), Path: "target.md", OldHeading: "Old Heading", NewHeading: "Old Heading", Apply: true, UpgradeToBlockID: HeadingRenameUpgradeNever, Fallback: HeadingRenameFallbackHeading})
	require.NoError(t, err)
	require.False(t, result.Applied)
	after, err := os.Stat(path)
	require.NoError(t, err)
	require.True(t, os.SameFile(before, after))
	require.Equal(t, before.ModTime(), after.ModTime())
	got, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, content, string(got))
}

func TestRenameHeadingCancellationLeavesEverySource(t *testing.T) {
	root := t.TempDir()
	before := map[string]string{"target.md": "# Target\n\n## Old Heading\n", "source.md": "[[target#Old Heading]]\n"}
	for path, content := range before {
		require.NoError(t, os.WriteFile(filepath.Join(root, path), []byte(content), 0o644))
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := RenameHeading(stubVault{path: root}, RenameHeadingParams{Context: ctx, NoteMetadata: testkit.NoteMetadataIndexer(t), Path: "target.md", OldHeading: "Old Heading", NewHeading: "New Heading", Apply: true})
	require.ErrorIs(t, err, context.Canceled)
	require.False(t, result.Applied)
	for path, want := range before {
		got, readErr := os.ReadFile(filepath.Join(root, path))
		require.NoError(t, readErr)
		require.Equal(t, want, string(got))
	}
}

func TestRenameHeadingReadOnlyBacklinkHasOnePublicationOutcome(t *testing.T) {
	root := t.TempDir()
	before := map[string]string{"target.md": "# Target\n\n## Old Heading\n", "aa-good.md": "[[target#Old Heading]]\n", "zz-readonly.md": "[[target#Old Heading]]\n"}
	for path, content := range before {
		require.NoError(t, os.WriteFile(filepath.Join(root, path), []byte(content), 0o644))
	}
	readOnly := filepath.Join(root, "zz-readonly.md")
	require.NoError(t, os.Chmod(readOnly, 0o444))
	t.Cleanup(func() { _ = os.Chmod(readOnly, 0o644) })
	result, err := RenameHeading(stubVault{path: root}, RenameHeadingParams{NoteMetadata: testkit.NoteMetadataIndexer(t), Path: "target.md", OldHeading: "Old Heading", NewHeading: "New Heading", Apply: true, UpgradeToBlockID: HeadingRenameUpgradeNever, Fallback: HeadingRenameFallbackHeading})
	want := before
	if err == nil {
		require.True(t, result.Applied)
		want = map[string]string{"target.md": "# Target\n\n## New Heading\n", "aa-good.md": "[[target#New Heading]]\n", "zz-readonly.md": "[[target#New Heading]]\n"}
	} else {
		require.False(t, result.Applied)
	}
	for path, expected := range want {
		got, readErr := os.ReadFile(filepath.Join(root, path))
		require.NoError(t, readErr)
		require.Equal(t, expected, string(got), path)
	}
}
