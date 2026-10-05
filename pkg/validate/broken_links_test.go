package validate

import (
	"encoding/json"
	"testing"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunBrokenLinks_IssueCodesAndPayloads(t *testing.T) {
	tests := []struct {
		name      string
		files     map[string]string
		wantCount int
		wantCodes []string
		assert    func(*testing.T, []Issue)
	}{
		{
			name: "clean vault",
			files: map[string]string{
				"a.md": "# A\n\nSee [[b]].\n",
				"b.md": "# B\n",
			},
		},
		{
			name: "missing note",
			files: map[string]string{
				"a.md": "# A\n\nSee [[missing#Authored Fragment]].\n",
			},
			wantCount: 1,
			wantCodes: []string{"broken_note_link"},
			assert: func(t *testing.T, issues []Issue) {
				data := decodeBrokenLinkData(t, issues[0])
				assert.Equal(t, "missing", data.Target)
				assert.Equal(t, "missing#Authored Fragment", issues[0].Target)
				assert.Equal(t, "Authored Fragment", data.Fragment)
				assert.Contains(t, issues[0].Message, "Missing note")
				assert.Contains(t, issues[0].Message, "[[missing#Authored Fragment]]")
				assert.Equal(t, string(obsidian.BrokenLinkReasonNoteMissing), data.Reason)
			},
		},
		{
			name: "missing heading and block fragments",
			files: map[string]string{
				"a.md":      "# A\n\nSee [[target#Missing Heading]] and [[target#^missing-block]].\n",
				"target.md": "# Target\n\n## Existing Heading\n\nParagraph ^existing-block\n",
			},
			wantCount: 2,
			wantCodes: []string{"broken_heading_link", "broken_block_link"},
			assert: func(t *testing.T, issues []Issue) {
				reasons := map[string]string{}
				for _, issue := range issues {
					data := decodeBrokenLinkData(t, issue)
					reasons[data.Fragment] = data.Reason
					assert.Empty(t, data.Candidates)
					assert.Equal(t, data.Target+"#"+data.Fragment, issue.Target)
				}
				assert.Equal(t, string(obsidian.BrokenLinkReasonHeadingMissing), reasons["Missing Heading"])
				assert.Equal(t, string(obsidian.BrokenLinkReasonBlockMissing), reasons["^missing-block"])
				assert.Contains(t, issues[0].Message, "Missing heading fragment")
				assert.Contains(t, issues[0].Message, "[[target#Missing Heading]]")
				assert.Contains(t, issues[1].Message, "Missing block fragment")
				assert.Contains(t, issues[1].Message, "[[target#^missing-block]]")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := runBrokenLinksFixture(t, tt.files, 100)
			assert.True(t, result.OK)
			assert.Equal(t, tt.wantCount, result.IssueCount)
			codes := make([]string, 0, len(result.Issues))
			for _, issue := range result.Issues {
				codes = append(codes, issue.Code)
			}
			if len(tt.wantCodes) == 0 {
				assert.Empty(t, codes)
			} else {
				assert.Equal(t, tt.wantCodes, codes)
			}
			if tt.assert != nil {
				tt.assert(t, result.Issues)
			}
		})
	}
}

func TestRunBrokenLinksPreservesIssuesForCentralizedTruncation(t *testing.T) {
	result := runBrokenLinksFixture(t, map[string]string{
		"a.md": "# A\n\n[[missing-one]] [[missing-two]] [[missing-three]]\n",
	}, 2)

	assert.True(t, result.OK)
	assert.Equal(t, 3, result.IssueCount)
	require.Len(t, result.Issues, 3)
	finalized := finalizeCheckResult(CheckBrokenLinks, result, 0, 2)
	assert.Len(t, finalized.Issues, 2)
	assert.Len(t, finalized.allIssueKeys, 3)
}

func runBrokenLinksFixture(t *testing.T, files map[string]string, maxIssues int) CheckResult {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, writeFixtureFiles(root, files))
	def := obsidian.VaultDefinition{Name: "test", Path: root, Links: obsidian.LinkTypeBoth}
	return RunBrokenLinks(RunContext{
		VaultDef:   def,
		VaultPath:  root,
		VaultMgr:   &fixedVaultMgr{def: def},
		NoteReader: &obsidian.Note{},
		MaxIssues:  maxIssues,
	}, Options{})
}

func decodeBrokenLinkData(t *testing.T, issue Issue) BrokenLinkData {
	t.Helper()
	var data BrokenLinkData
	require.NoError(t, json.Unmarshal(issue.Data, &data))
	return data
}
