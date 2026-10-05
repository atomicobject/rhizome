package validate

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func brokenLinkRunContext(root string) RunContext {
	def := obsidian.VaultDefinition{Name: "test", Path: root, Links: obsidian.LinkTypeBoth}
	return RunContext{VaultDef: def, VaultPath: root, NoteReader: &obsidian.Note{}, MaxIssues: 100}
}

func fixByTarget(t *testing.T, fixes []FixAction, target string) FixAction {
	t.Helper()
	for _, fix := range fixes {
		if strings.Contains(fix.ID, ":"+target+"->") || strings.HasSuffix(fix.ID, ":"+target+":review") {
			return fix
		}
	}
	t.Fatalf("no fix for %q in %+v", target, fixes)
	return FixAction{}
}

// Retarget suggestions observed on a real vault that were wrong: self-links,
// entity to artifact, periodic dates, attachments, and missing block IDs.
// None of them may reach needs_confirmation.
func TestBrokenLinkRetargetsRequireStrongEvidence(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, writeFixtureFiles(root, map[string]string{
		"Log/Bee Fly sales call, 2024-09-06.md":       "# Call\n\nWith [[Bee Fly]].\n",
		"Notes/Succeeding with Agile by Mike Cohn.md": "# Book\n",
		"Notes/Reading.md":                            "[[Mike Cohn]]\n",
		"Log/Sales - fetch.ai, 2025-01-08.md":         "# Sales\n",
		"Daily Notes/2025-01-09.md":                   "Yesterday [[2025-01-08]]\n",
		"Daily Notes/2024-07-17.md":                   "# Day\n",
		"Notes/Dre.md":                                "[[Sync with Dre, 2024-07-17]]\n",
		"Notes/Ryan Boxeth.md":                        "[[2023-01-10 Ryan Boxeth Interview Prep.canvas]]\n",
		"Notes/Team Patterns.md":                      "# Team Patterns\n",
		"Notes/Rocks.md":                              "[[Team-Patterns#^2ECDBD93]]\n[[Team-Patterns]]\n",
	}))

	result := RunBrokenLinks(brokenLinkRunContext(root), Options{})

	require.Empty(t, result.Error)
	for _, target := range []string{"Bee Fly", "Mike Cohn", "2025-01-08", "Sync with Dre, 2024-07-17", "2023-01-10 Ryan Boxeth Interview Prep.canvas"} {
		fix := fixByTarget(t, result.Fixes, target)
		require.Equal(t, FixSafetyAgent, fix.Safety, target)
		require.Empty(t, fix.Edits, target)
		require.NotContains(t, fix.CandidatePaths, "Log/Bee Fly sales call, 2024-09-06.md", "never propose the source note")
	}
	require.Equal(t, candidateConfidenceLow, fixByTarget(t, result.Fixes, "Mike Cohn").Confidence)
	require.Empty(t, fixByTarget(t, result.Fixes, "2025-01-08").CandidatePaths, "a bare date only names a periodic note")
	require.Empty(t, fixByTarget(t, result.Fixes, "Sync with Dre, 2024-07-17").CandidatePaths)
	require.Empty(t, fixByTarget(t, result.Fixes, "2023-01-10 Ryan Boxeth Interview Prep.canvas").CandidatePaths)

	block := fixByTarget(t, result.Fixes, "Team-Patterns#^2ECDBD93")
	require.Equal(t, FixSafetyAgent, block.Safety, "the candidate lacks the linked block")
	require.Equal(t, []string{"Notes/Team Patterns.md"}, block.CandidatePaths)

	retarget := fixByTarget(t, result.Fixes, "Team-Patterns")
	require.Equal(t, FixSafetyConfirm, retarget.Safety)
	require.Equal(t, candidateConfidenceHigh, retarget.Confidence)
	applyCheckFixThroughPlan(t, brokenLinkRunContext(root), result, retarget)
	updated, err := os.ReadFile(filepath.Join(root, "Notes/Rocks.md"))
	require.NoError(t, err)
	require.Equal(t, "[[Team-Patterns#^2ECDBD93]]\n[[Team Patterns|Team-Patterns]]\n", string(updated),
		"the retarget keeps what the reader saw")
}

func gitCommitAll(t *testing.T, root, message string) {
	t.Helper()
	for _, args := range [][]string{{"add", "-A"}, {"commit", "-q", "-m", message}} {
		cmd := exec.Command("git", append([]string{"-C", root, "-c", "user.name=t", "-c", "user.email=t@example.com"}, args...)...)
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, string(out))
	}
}

// Links that broke fail the default gate; links that never had a target are
// placeholders reported only by the audit check.
func TestBrokenLinksSeparatesPlaceholdersUsingGitHistory(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	out, err := exec.Command("git", "init", "-q", root).CombinedOutput()
	require.NoError(t, err, string(out))
	require.NoError(t, writeFixtureFiles(root, map[string]string{
		"Old Name.md":          "# Project roadmap for the platform team\n\nLong enough to be detected as a rename.\n",
		"Hiring Plan Draft.md": "# Quarterly hiring plan and interview loop\n\nLong enough to be detected as a rename.\n",
		"Deleted.md":           "# Deleted\n",
		"Café — ✅.md":          "# Accented\n",
		"Source.md":            "[[Old Name]] [[Deleted]] [[Café — ✅]] [[Never Existed]]\n",
	}))
	gitCommitAll(t, root, "initial")
	require.NoError(t, os.Rename(filepath.Join(root, "Old Name.md"), filepath.Join(root, "New Name.md")))
	require.NoError(t, os.Rename(filepath.Join(root, "Hiring Plan Draft.md"), filepath.Join(root, "Hiring Plan Draft Final.md")))
	require.NoError(t, os.Remove(filepath.Join(root, "Deleted.md")))
	require.NoError(t, os.Remove(filepath.Join(root, "Café — ✅.md")))
	gitCommitAll(t, root, "rename and delete")
	require.NoError(t, writeFixtureFiles(root, map[string]string{"Later.md": "[[Hiring Plan Draft]]\n"}))
	gitCommitAll(t, root, "link written after the rename")

	runCtx := brokenLinkRunContext(root)
	missingRuntime := RunBrokenLinks(runCtx, Options{})
	require.False(t, missingRuntime.OK)
	require.Contains(t, missingRuntime.Error, "historical rename evidence requires RunContext.NoteMetadata")
	require.Empty(t, missingRuntime.Fixes)
	runCtx.NoteMetadata = testNoteMetadata(t)
	broken := RunBrokenLinks(runCtx, Options{})
	require.Empty(t, broken.Error)
	require.Equal(t, []string{"Café — ✅", "Deleted", "Hiring Plan Draft", "Old Name"}, sortedIssueTargets(broken.Issues),
		"a deleted non-ASCII name broke; git must not escape it")
	require.Len(t, broken.Notes, 1)
	require.Contains(t, broken.Notes[0], "1 placeholder link")
	rename := fixByTarget(t, broken.Fixes, "Old Name")
	require.Equal(t, FixSafetyConfirm, rename.Safety, "the rename orphaned a link that already existed")
	require.Equal(t, []string{"New Name.md"}, rename.CandidatePaths)
	later := fixByTarget(t, broken.Fixes, "Hiring Plan Draft")
	require.Equal(t, FixSafetyAgent, later.Safety, "a link written after the rename did not break because of it")
	require.Equal(t, []string{"Hiring Plan Draft Final.md"}, later.CandidatePaths, "the rename destination is listed once")
	require.Equal(t, candidateConfidenceLow, later.Confidence)

	placeholders := RunPlaceholderLinks(context.Background(), runCtx, Options{})
	require.Empty(t, placeholders.Error)
	require.Equal(t, 1, placeholders.IssueCount)
	require.Equal(t, IssueCodePlaceholderNoteLink, placeholders.Issues[0].Code)
	require.Equal(t, "Never Existed", placeholders.Issues[0].Target)

	require.NoError(t, writeFixtureFiles(root, map[string]string{
		".rhizome/config.yml": "validation:\n  brokenLinks:\n    placeholders: strict\n",
	}))
	strict := RunBrokenLinks(runCtx, Options{})
	require.Equal(t, []string{"Café — ✅", "Deleted", "Hiring Plan Draft", "Never Existed", "Old Name"}, sortedIssueTargets(strict.Issues))
	require.Empty(t, strict.Notes)
	require.Zero(t, RunPlaceholderLinks(context.Background(), runCtx, Options{}).IssueCount)
}

func TestBrokenLinksWithoutGitHistoryCountEveryUnresolvedLink(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, writeFixtureFiles(root, map[string]string{"Source.md": "[[Never Existed]]\n"}))

	result := RunBrokenLinks(brokenLinkRunContext(root), Options{})

	require.Equal(t, 1, result.IssueCount)
	require.Len(t, result.Notes, 1)
	require.Contains(t, result.Notes[0], "Git history is unavailable")
	var data BrokenLinkData
	require.NoError(t, json.Unmarshal(result.Issues[0].Data, &data))
	require.Equal(t, "Never Existed", data.Target)
}

// A rename destination whose basename is shared by another note must keep its
// path in the rewritten link, or the link would resolve ambiguously.
func TestBrokenLinkRenameRetargetKeepsPathWhenBasenameIsAmbiguous(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	out, err := exec.Command("git", "init", "-q", root).CombinedOutput()
	require.NoError(t, err, string(out))
	require.NoError(t, writeFixtureFiles(root, map[string]string{
		"Old Name.md":       "# Project roadmap for the platform team\n\nLong enough to be detected as a rename.\n",
		"notes/New Name.md": "# Unrelated note with the same basename\n",
		"Source.md":         "[[Old Name]]\n",
	}))
	gitCommitAll(t, root, "initial")
	require.NoError(t, os.MkdirAll(filepath.Join(root, "docs"), 0o755))
	require.NoError(t, os.Rename(filepath.Join(root, "Old Name.md"), filepath.Join(root, "docs/New Name.md")))
	gitCommitAll(t, root, "rename")

	runCtx := brokenLinkRunContext(root)
	runCtx.NoteMetadata = testNoteMetadata(t)
	result := RunBrokenLinks(runCtx, Options{})
	require.Empty(t, result.Error)
	rename := fixByTarget(t, result.Fixes, "Old Name")
	require.Equal(t, FixSafetyConfirm, rename.Safety)
	require.Equal(t, []string{"docs/New Name.md"}, rename.CandidatePaths)
	applyCheckFixThroughPlan(t, runCtx, result, rename)
	updated, err := os.ReadFile(filepath.Join(root, "Source.md"))
	require.NoError(t, err)
	require.Equal(t, "[[docs/New Name|Old Name]]\n", string(updated))
}

func sortedIssueTargets(issues []Issue) []string {
	out := make([]string, 0, len(issues))
	for _, issue := range issues {
		out = append(out, issue.Target)
	}
	return sortedUnique(out)
}

// Starter AGENTS.md blocks link relative to the repository that installs them,
// so they are exempt however the vault is rooted; an ordinary agents/AGENTS.md
// is not.
func TestBrokenLinksExemptStarterAgentBlocks(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, writeFixtureFiles(root, map[string]string{
		"core/template.yaml":     "id: core\n",
		"core/agents/AGENTS.md":  "[[Starter Target]]\n",
		"tools/agents/AGENTS.md": "[[Ordinary Target]]\n",
		"site/template.yaml":     "title: Site\n",
		"site/agents/AGENTS.md":  "[[Lookalike Target]]\n",
	}))

	result := RunBrokenLinks(brokenLinkRunContext(root), Options{})

	require.Equal(t, []string{"Lookalike Target", "Ordinary Target"}, sortedIssueTargets(result.Issues))
}
